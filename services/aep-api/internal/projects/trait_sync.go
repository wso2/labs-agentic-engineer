// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package projects

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/k8sname"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// OrgPublisher is the narrow per-org Thunder publisher-provisioning surface
// the trait emitter consumes from the idp feature. Declared consumer-side so
// the component feature does not import idp's concrete service — the idp
// service satisfies it structurally and is injected via SetIDPService at the
// composition root.
type OrgPublisher interface {
	GetProfile(ctx context.Context, orgID string) (*organization.OrganizationIDPProfile, error)
	EnsureOrgPublisher(ctx context.Context, orgID, actor string) (clientID, clientSecret string, created bool, err error)
}

// TraitSyncService is the single shared emitter for `api-configuration`
// trait state on a Component CR + its per-environment ReleaseBindings.
//
// The two halves of that state are written at different times, because they
// have different write targets:
//
//  1. The Component CR's trait SHAPE — which is what makes OpenChoreo render
//     the RestApi at all — is set at component create by
//     `component_service.go`, from the same `exposesAPI.auth` this service
//     reads. That happens before the first build, and this service re-asserts
//     it on every reconcile.
//  2. The per-environment config — the `jwtAuth` policy the gateway enforces
//     and the sibling-SPA CORS allowlist — lands on the ReleaseBinding, which
//     OpenChoreo creates only once a build has produced a workload. It cannot
//     be written before then; UpdateComponentTraitEnvironmentConfigs soft
//     no-ops when the binding is absent.
//
// Because the two halves land on different objects, the WRITE ORDER is part of
// the contract: a trait attached without its per-environment config fails the
// whole ReleaseBinding render. SyncComponentTraits therefore writes config
// upserts → trait shape → config tombstones, and each write re-reads under
// retry (openchoreo.retryStaleWrite) because OC's own controllers rewrite the
// objects we patch. The full account of both hazards is at the two write sites.
//
// The trigger for (2) is `SyncProjectAPITraits`, called by the run
// supervisor when a cycle's builds go green (`delivery/run`, activity
// SyncAPITraits). That trigger is rail-coupled ON PURPOSE-FOR-NOW and it is
// the known weak point: the previous trigger hung off the ExecWatcher's build
// terminal and stopped firing — silently, for every project — when builds
// moved to the Temporal run loop, which writes no `kind=build` execution rows
// for that watcher to read. A missed write leaves a protected API's gateway
// passing every request through unauthenticated, so a rail-agnostic reconcile
// sweep over the component list is what should ultimately make the guarantee.
// Until then the failure is at least LOUD and retried: SyncProjectAPITraits
// returns its per-component failures, so the activity fails and Temporal
// re-runs the sweep. `traitDeployObserver` still routes the old ExecWatcher
// path here; it is inert for anything the run loop builds.
//
// Concurrency: every call acquires a per-component mutex keyed by
// `(orgID, projectID, componentName)`. We use a `sync.Map` of `*sync.Mutex`
// — NOT `singleflight`. Singleflight coalesces duplicate calls (returns
// the in-flight call's result to later callers and skips their work),
// which is wrong here: a design PUT that lands while the dispatch path
// is mid-flight must trigger its own read after the dispatch finishes,
// not piggyback on the dispatch's stale read.
//
// Protected components keep `autoDeploy: true`, accept the short
// first-deploy exposure window, and rely on this emitter + the drift
// watcher for convergence. autoDeploy is required because OC's project→
// environment→RB binding logic drives initial RB creation; BFF-managed
// RBs without autoDeploy are not supported by OC.
type TraitSyncService struct {
	componentClient openchoreo.ComponentClient
	store           *spec.ArtifactStore
	// idp, when non-nil, is invoked on every protected reconcile to
	// lazily ensure the org's Thunder publisher app exists. Failures
	// are logged but don't block the trait emit — the API stays
	// reachable, the org just lacks an outbound publisher identity
	// until a subsequent sync succeeds. Wired via SetIDPService.
	idp OrgPublisher

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// SetIDPService wires the per-org Thunder publisher provisioning hook.
// Optional — when not set the trait emit path skips the publisher
// EnsureOrgPublisher call entirely.
func (s *TraitSyncService) SetIDPService(idp OrgPublisher) {
	if s == nil {
		return
	}
	s.idp = idp
}

func NewTraitSyncService(componentClient openchoreo.ComponentClient, store *spec.ArtifactStore) *TraitSyncService {
	return &TraitSyncService{
		componentClient: componentClient,
		store:           store,
		locks:           make(map[string]*sync.Mutex),
	}
}

// SyncComponentTraits reconciles the OC Component CR + its ReleaseBindings
// against the desired state derived from `design.json`. Acquires the
// per-component mutex BEFORE reading design so a concurrent design edit
// doesn't write past us mid-PATCH.
//
// `componentName` is the user-friendly name (matches design.json component
// name); the OC client prefixes it with the project name internally.
//
// First-deploy race: when no ReleaseBindings exist yet for the component,
// the per-RB PATCH is a soft no-op (handled inside the OC client). The
// next dispatch — which is the only path that creates the Component CR
// with the trait already populated — closes that gap. The drift watcher
// catches anything that falls through.
//
// Errors are returned to the caller. Call sites in dispatch / design PUT
// log and continue (the design tree is the canonical source; the watcher
// will reconcile on the next sweep).
func (s *TraitSyncService) SyncComponentTraits(ctx context.Context, orgID, projectID, componentName string) error {
	if s == nil {
		return nil
	}
	if orgID == "" || projectID == "" || componentName == "" {
		return fmt.Errorf("trait_sync: empty orgID/projectID/componentName")
	}

	mu := s.lockFor(orgID, projectID, componentName)
	mu.Lock()
	defer mu.Unlock()

	// Read design AFTER lock acquisition — never read before locking.
	// Otherwise a concurrent edit can write a newer version while we're
	// mid-PATCH with a stale read.
	design, err := s.store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		if spec.IsNotFound(err) {
			// No design at all yet — nothing to reconcile. Reached from a
			// design PUT race where the controller hands us a stale path.
			return nil
		}
		return fmt.Errorf("trait_sync: read design: %w", err)
	}
	if design == nil {
		return nil
	}

	// Find the component in design by the k8s-shaped name (matches dispatch).
	var match *spec.DesignComponent
	for i := range design.Components {
		if k8sname.ToK8sName(design.Components[i].Name) == componentName {
			match = &design.Components[i]
			break
		}
	}
	if match == nil {
		// Component is gone from design — caller (DeleteComponent path)
		// owns the OC cleanup. Trait sync has nothing to do.
		return nil
	}

	desiredEnabled := spec.ResolveAPISecurityEnabled(*match)

	// Lazy provisioning of the org's Thunder publisher app: the first
	// protected reconcile in an org creates `aep-publisher-<orgID>`
	// (idempotent on subsequent calls). Failures are non-fatal — the
	// trait still emits, the API stays reachable; the publisher will
	// be retried on the next reconcile or via the drift watcher.
	var issuers []string
	if desiredEnabled && s.idp != nil {
		if _, _, _, err := s.idp.EnsureOrgPublisher(ctx, orgID, "trait_sync"); err != nil {
			slog.WarnContext(ctx, "trait_sync: EnsureOrgPublisher failed; continuing",
				"orgID", orgID, "error", err)
		}
		// When the org has a BYO-IDP (non-platform) profile, pass its
		// issuer into the trait so the RestApi pins JWT validation to
		// that issuer only. Platform-kind orgs keep `issuers` empty
		// (any cluster-configured keymanager).
		if profile, perr := s.idp.GetProfile(ctx, orgID); perr == nil && profile != nil {
			if profile.Kind != "" && profile.Kind != "platform" && profile.Issuer != "" {
				issuers = []string{profile.Issuer}
			}
		}
	}

	// Sibling-CORS: when this component is a service exposing a managed
	// API to BROWSER callers (end-user-required), populate
	// `cors.allowedOrigins` with every external web-app component in
	// the same project. Service-to-service APIs (`service-required`)
	// have no browser caller — emitting SPA origins there would
	// unnecessarily widen the CORS surface.
	//
	// If a sibling lookup fails transiently, return the error to the
	// caller (the watcher retries) — a partial allowlist would silently
	// block the missing SPA's preflight.
	var allowedOrigins []string
	if desiredEnabled && spec.ResolveAPISecurityCallerKind(*match) == "end-user" {
		origins, originsErr := s.siblingSPAOrigins(ctx, orgID, projectID, design)
		if originsErr != nil {
			return fmt.Errorf("trait_sync: sibling SPA origins: %w", originsErr)
		}
		allowedOrigins = origins
	}

	traits, configs := DesiredAPIConfigurationTraitWithIssuers(componentName, match.EndpointName(), desiredEnabled, issuers, allowedOrigins)

	// Auto-provision the default "error → RCA" observability-alert-rule trait
	// for service components (opt out per component via design.json
	// `disableAutoRca`). Appended to the SAME slice/map because
	// UpdateComponentTraits REPLACES spec.traits — emitting it in a separate
	// call would clobber the api-configuration trait above. OC's trait
	// controller renders each instance into an ObservabilityAlertRule scoped to
	// this component (component/project/env UIDs resolved from ${metadata.*}).
	if spec.ResolveAutoRCAEnabled(*match) {
		rcaTraits, rcaConfigs := DesiredObservabilityAlertRuleTraits(componentName)
		traits = append(traits, rcaTraits...)
		if configs == nil {
			configs = map[string]map[string]interface{}{}
		}
		for inst, cfg := range rcaConfigs {
			if _, exists := configs[inst]; exists {
				// Instance-name collision: the auto-RCA instance name
				// (<componentName>-auto-rca-error) matches an already-desired
				// config from another trait source. Overwriting silently would
				// swallow that other trait's config, so log loudly instead of
				// clobbering without a trace.
				slog.WarnContext(ctx, "trait_sync: auto-RCA instance name collides with an existing trait config; overwriting",
					"componentName", componentName, "instanceName", inst)
			}
			configs[inst] = cfg
		}
	}

	// Write order matters, because the two halves land on different objects and
	// either write can fail on its own. A trait instance attached to the
	// Component whose per-environment config is MISSING doesn't degrade
	// gracefully — it fails the whole ReleaseBinding render, taking every other
	// trait on that binding down with it. observability-alert-rule is the live
	// example: its schema rejects a rule with no notification channel, and the
	// channel is supplied by the env config, so trait-shape-first briefly (or,
	// if the second write fails, permanently) leaves the binding unrenderable:
	//
	//	Failed to render resources: trait observability-alert-rule/…-auto-rca-error
	//	validation failed: A notification channel is mandatory for alert rules
	//
	// So: never a trait without its config, and never a config stripped from
	// under a still-attached trait. Upserts go first, the trait shape second,
	// and removals last. Each phase is skipped when its set is empty, so the
	// common enabled path is still two writes.
	upserts, removals := splitTraitConfigs(configs)

	if len(upserts) > 0 {
		if err := s.componentClient.UpdateComponentTraitEnvironmentConfigs(ctx, orgID, projectID, componentName, upserts); err != nil {
			return fmt.Errorf("trait_sync: update trait env configs: %w", err)
		}
	}

	// Patch the Component CR's spec.traits. Skip when there's nothing to
	// change — but the OC client's GET-then-PUT is harmless so we always
	// fire to avoid bookkeeping drift between in-process state and OC.
	if err := s.componentClient.UpdateComponentTraits(ctx, orgID, projectID, componentName, traits); err != nil {
		return fmt.Errorf("trait_sync: update component traits: %w", err)
	}

	// Tombstones: the instance is off the Component now, so its stale env
	// config can go. The OC client returns a soft no-op when no ReleaseBinding
	// exists yet (first-deploy race) — expected, and the dispatch path creates
	// the RB with the right env config in place via the Component's autoDeploy
	// reconcile.
	if len(removals) > 0 {
		if err := s.componentClient.UpdateComponentTraitEnvironmentConfigs(ctx, orgID, projectID, componentName, removals); err != nil {
			return fmt.Errorf("trait_sync: clear trait env configs: %w", err)
		}
	}

	slog.InfoContext(ctx, "trait_sync: reconciled",
		"orgID", orgID,
		"projectID", projectID,
		"componentName", componentName,
		"apiSecurityEnabled", desiredEnabled,
	)
	return nil
}

// DeleteComponentCascade deletes the OC Component CR via OC's REST API.
//
// Cleanup chain — end-to-end via OC:
//
//	Component  → owner ref → ComponentRelease
//	                        → owner ref → ReleaseBinding
//	                                     → owner ref → RenderedRelease
//	                                                  → finalizer (DataPlaneCleanupFinalizer)
//	                                                    iterates Status.Resources
//	                                                    and deletes every tracked
//	                                                    resource in the dp-namespace
//	                                                    — including the trait-
//	                                                    emitted Backend +
//	                                                    RestApi.
//
// The trait template's `creates` resources are tracked in
// RenderedRelease.Status.Resources by the OC controller at apply time,
// so the finalizer covers them even though they don't carry explicit
// ownerReferences (see renderedrelease/controller_finalize.go in the OC
// tree).
//
// Acquires the per-component mutex BEFORE issuing the OC call so a
// concurrent SyncComponentTraits (e.g. from a late design PUT) doesn't
// race with the deletion.
func (s *TraitSyncService) DeleteComponentCascade(ctx context.Context, orgID, projectID, componentName string) error {
	if s == nil {
		return nil
	}
	if orgID == "" || projectID == "" || componentName == "" {
		return fmt.Errorf("trait_sync: empty orgID/projectID/componentName")
	}

	mu := s.lockFor(orgID, projectID, componentName)
	mu.Lock()
	defer mu.Unlock()

	if err := s.componentClient.DeleteComponent(ctx, orgID, projectID, componentName); err != nil {
		return fmt.Errorf("trait_sync: delete component: %w", err)
	}

	slog.InfoContext(ctx, "trait_sync: component deleted; OC RenderedRelease finalizer GCs trait resources",
		"orgID", orgID,
		"projectID", projectID,
		"componentName", componentName,
	)
	return nil
}

// SyncProjectAPITraits re-emits trait state on every service component in the
// project that needs it, from the dispatch path (when any component lands
// `deployed`). It covers two cases:
//   - API-exposing components (`exposesAPI.auth` set): so every protected API
//     picks up a freshly-added SPA's sibling origin in `cors.allowedOrigins`
//     (stale CORS otherwise silently breaks the new SPA's preflight).
//   - Auto-RCA-eligible components (all service components, unless opted out):
//     so a fresh deploy provisions the default error→RCA alert-rule trait
//     immediately, instead of waiting for the next reconcile-watcher sweep.
//
// Idempotent: a failure on one component logs and continues to the next, so one
// bad component can't stop the rest of the project from converging. The
// failures are then JOINED and RETURNED — they are not swallowed. That matters
// because this is the only trigger: the caller is the Temporal SyncAPITraits
// activity, and returning the error is what makes Temporal retry the sweep. A
// dropped write here leaves a protected API's gateway passing every request
// through unauthenticated, which is not a "log it and move on" outcome.
//
// No design ⇒ nothing to reconcile, returns nil.
func (s *TraitSyncService) SyncProjectAPITraits(ctx context.Context, orgID, projectID string) error {
	if s == nil {
		return nil
	}
	if orgID == "" || projectID == "" {
		return fmt.Errorf("trait_sync: empty orgID/projectID")
	}
	design, err := s.store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		if spec.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("trait_sync: read design: %w", err)
	}
	if design == nil {
		return nil
	}
	var failures []error
	for _, c := range design.Components {
		if c.ComponentType != spec.ComponentTypeService {
			continue
		}
		// Reconcile a service component when it needs EITHER trait: a managed
		// API (api-configuration) or the default error→RCA alert rule
		// (observability-alert-rule). Including auto-RCA-eligible components
		// here — not just API-exposing ones — makes a fresh deploy provision
		// the alert rule immediately via the dispatch path, instead of waiting
		// for the next reconcile-watcher sweep.
		if !spec.ResolveAPISecurityEnabled(c) && !spec.ResolveAutoRCAEnabled(c) {
			continue
		}
		k8sName := k8sname.ToK8sName(c.Name)
		if err := s.SyncComponentTraits(ctx, orgID, projectID, k8sName); err != nil {
			slog.WarnContext(ctx, "trait_sync: sibling re-emit failed; continuing",
				"orgID", orgID,
				"projectID", projectID,
				"componentName", k8sName,
				"error", err,
			)
			failures = append(failures, fmt.Errorf("component %q: %w", k8sName, err))
		}
	}
	return errors.Join(failures...)
}

// siblingSPAOrigins returns the external SPA origins for every web-app
// component declared in the project's design. Used as `cors.allowedOrigins`
// on protected API ReleaseBindings (sibling-CORS rule). Pulls live URLs
// from OC ListDeployments.
//
// When a SPA's lookup ERRORS transiently, the function returns the
// error rather than silently dropping that SPA — a partial allowlist
// would commit a CORS list that blocks the missing SPA's preflight.
// When a SPA simply hasn't deployed yet (no items, no error), it
// contributes nothing — the next cascade tick will pick it up. The
// returned slice is empty when no SPA exists yet in the project — the
// caller treats that as wildcard-CORS-fallback to keep dev curl
// working.
func (s *TraitSyncService) siblingSPAOrigins(ctx context.Context, orgID, projectID string, design *spec.DesignFile) ([]string, error) {
	if s.componentClient == nil || design == nil {
		return nil, nil
	}
	origins := make([]string, 0, len(design.Components))
	seen := make(map[string]struct{}, len(design.Components))
	for _, c := range design.Components {
		if c.ComponentType != spec.ComponentTypeWebApplication {
			continue
		}
		k8sName := k8sname.ToK8sName(c.Name)
		list, err := s.componentClient.ListDeployments(ctx, orgID, projectID, k8sName)
		if err != nil {
			return nil, fmt.Errorf("list deployments for %q: %w", c.Name, err)
		}
		if list == nil {
			continue
		}
		for _, d := range list.Items {
			origin := originFromEndpointURL(d.EndpointURL)
			if origin == "" {
				continue
			}
			if _, ok := seen[origin]; ok {
				continue
			}
			seen[origin] = struct{}{}
			origins = append(origins, origin)
		}
	}
	return origins, nil
}

// originFromEndpointURL extracts the scheme+host+port prefix from a
// ListDeployments-style URL like `http://todo-web-...localhost:19080/`.
// Returns "" when parsing fails — callers skip empty origins.
func originFromEndpointURL(u string) string {
	if u == "" {
		return ""
	}
	// Trim path/query: keep scheme://authority only.
	// Manual scan to avoid pulling net/url for this hot path.
	const sep = "://"
	i := strings.Index(u, sep)
	if i < 0 {
		return ""
	}
	rest := u[i+len(sep):]
	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		return u
	}
	return u[:i+len(sep)+end]
}

func (s *TraitSyncService) lockFor(orgID, projectID, componentName string) *sync.Mutex {
	key := orgID + "/" + projectID + "/" + componentName
	s.mu.Lock()
	defer s.mu.Unlock()
	if mu, ok := s.locks[key]; ok {
		return mu
	}
	mu := &sync.Mutex{}
	s.locks[key] = mu
	return mu
}

// -- Pure helpers ------------------------------------------------------------

// splitTraitConfigs partitions a desired trait-env-config map into the
// instances being written (`upserts`) and the instances being tombstoned
// (`removals` — the empty/nil values that mean "delete this key"). Callers use
// the split to order the two writes around the Component's trait-shape write;
// see SyncComponentTraits. Both maps are nil when their side is empty, so
// `len(...) > 0` is the phase gate.
func splitTraitConfigs(configs map[string]map[string]interface{}) (upserts, removals map[string]map[string]interface{}) {
	for inst, params := range configs {
		if len(params) == 0 {
			if removals == nil {
				removals = map[string]map[string]interface{}{}
			}
			removals[inst] = nil
			continue
		}
		if upserts == nil {
			upserts = map[string]map[string]interface{}{}
		}
		upserts[inst] = params
	}
	return upserts, removals
}

// APIConfigurationInstanceName returns the canonical trait instance name for
// the component's managed endpoint. Mirrors the POC manifests' naming
// (`<componentName>-<endpointName>`) so on-cluster resources are predictable.
// The trait template uses this as the prefix for the generated Backend and
// RestApi resources (`<instanceName>-api-gw-backend`, `<instanceName>`).
//
// `endpointName` is the design.json-declared workload endpoint name; an empty
// value defaults to spec.DefaultEndpointName ("http"), preserving the prior
// `<componentName>-http` naming for components that declare no endpoint.
func APIConfigurationInstanceName(componentName, endpointName string) string {
	componentName = strings.TrimSpace(componentName)
	if componentName == "" {
		componentName = "component"
	}
	endpointName = strings.TrimSpace(endpointName)
	if endpointName == "" {
		endpointName = spec.DefaultEndpointName
	}
	return componentName + "-" + endpointName
}

// DesiredAPIConfigurationTrait — convenience shim that calls
// DesiredAPIConfigurationTraitWithIssuers with no issuer pinning and
// no sibling origins (wildcard CORS). `endpointName` is the design.json
// workload endpoint name (empty ⇒ the default "http").
func DesiredAPIConfigurationTrait(componentName, endpointName string, enabled bool) (traits []openchoreo.ComponentTrait, configs map[string]map[string]interface{}) {
	return DesiredAPIConfigurationTraitWithIssuers(componentName, endpointName, enabled, nil, nil)
}

// DesiredAPIConfigurationTraitWithIssuers returns the BFF-internal
// desired state for the `api-configuration` trait. When `enabled` is
// true, the trait is attached + jwtAuth is enabled in the per-env
// config with `issuers` pinned to the supplied list (empty ⇒ accept
// any cluster-configured keymanager). When `enabled` is false, the
// function returns nil + a tombstone entry to strip any previously-set
// config.
//
// `allowedOrigins` lists the SPA hostnames the gateway should
// echo on CORS preflight. Empty/nil falls back to the trait schema's
// default of `["*"]` (wildcard, allowCredentials=false). When non-empty
// the BFF sets `allowCredentials: true` so browsers can send the
// `Authorization: Bearer …` header on cross-origin fetches (the WSO2
// platform forbids the `*` + credentials combo).
//
// `configs` is keyed by trait instance name; the value is the parameters
// block that lands at `ReleaseBinding.spec.traitEnvironmentConfigs[<inst>]`.
// The shape (cors / jwtAuth) matches the trait's environmentConfigSchema.
//
// `endpointName` is the design.json-declared workload endpoint name the trait
// binds to (it must match a key in the component's workload.yaml
// `spec.endpoints`). An empty value defaults to spec.DefaultEndpointName
// ("http"). This is the SINGLE point that decides the bound endpoint name — it
// is no longer hardcoded, so a component whose workload names its endpoint
// something other than "http" still renders (previously deploy rendering failed
// with `workload.endpoints["http"]: no such key`).
func DesiredAPIConfigurationTraitWithIssuers(componentName, endpointName string, enabled bool, issuers []string, allowedOrigins []string) (traits []openchoreo.ComponentTrait, configs map[string]map[string]interface{}) {
	endpointName = strings.TrimSpace(endpointName)
	if endpointName == "" {
		endpointName = spec.DefaultEndpointName
	}
	inst := APIConfigurationInstanceName(componentName, endpointName)
	if !enabled {
		// Clear both: empty traits + empty config marks the instance for
		// removal in the OC client's merge logic.
		return nil, map[string]map[string]interface{}{
			inst: nil,
		}
	}
	traits = []openchoreo.ComponentTrait{{
		InstanceName: inst,
		Kind:         "ClusterTrait",
		Name:         "api-configuration",
		Parameters: map[string]interface{}{
			"endpointName": endpointName,
		},
	}}
	issuersIface := make([]interface{}, 0, len(issuers))
	for _, iss := range issuers {
		issuersIface = append(issuersIface, iss)
	}
	cors := map[string]interface{}{
		"enabled": true,
	}
	if len(allowedOrigins) > 0 {
		originsIface := make([]interface{}, 0, len(allowedOrigins))
		for _, o := range allowedOrigins {
			originsIface = append(originsIface, o)
		}
		cors["allowedOrigins"] = originsIface
		cors["allowedMethods"] = []interface{}{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"}
		cors["allowedHeaders"] = []interface{}{"Authorization", "Content-Type", "Accept", "Origin"}
		cors["allowCredentials"] = true
	}
	configs = map[string]map[string]interface{}{
		inst: {
			"cors": cors,
			"jwtAuth": map[string]interface{}{
				"enabled": true,
				// jwt-auth v1 accepts `issuers` + `audience` arrays. Empty
				// issuers means "no per-RestApi filter; trust any cluster-
				// configured keymanager". BYO-IDP orgs populate this from
				// the org's IDP profile so each protected API only trusts
				// its org's IDP.
				"issuers":  issuersIface,
				"audience": []interface{}{},
			},
		},
	}
	return traits, configs
}
