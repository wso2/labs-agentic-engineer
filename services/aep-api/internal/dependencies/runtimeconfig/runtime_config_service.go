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

package runtimeconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/platform/k8sname"
	"github.com/wso2/aep/aep-api/internal/platform/ocname"
	"github.com/wso2/aep/aep-api/internal/spec"
)

const (
	// bindingEnv is the single environment runtime-config targets (mirrors
	// provisioning.defaultEnv). A web-app's platform-resource binding whose
	// outputs drive the SPA lives in this env.
	bindingEnv = openchoreo.DevEnvironmentName
)

// RuntimeConfigService emits the per-web-app `env-config.js` file onto
// each ReleaseBinding's `workloadOverrides.container.files`. The SPA's
// `index.html` loads `/env-config.js` synchronously before its bundle,
// so the values land on `window._env_` before any React module runs.
//
// The BFF plays the platform-engineer role here — the coding agent
// never sees the upstream URL, the OIDC client_id, or any redirect URI.
// One image runs unchanged in every environment; per-env values arrive
// at ReleaseBinding time.
type RuntimeConfigService struct {
	componentClient openchoreo.ComponentClient
	// resourceClient reads a web-app's platform-resource dependency's per-env
	// ResourceReleaseBinding — its status.outputs (resolved by OC) become the
	// generic <DEP>_<OUTPUT> window._env_ keys — and patches the binding's
	// env-configs declaratively (the annotation-driven consumer-URL patch) so
	// the operator registers the SPA's callback URL. nil in paths that never
	// consume platform-resource outputs.
	resourceClient openchoreo.ResourceClient
	// catalog resolves the PE-authored CRT metadata markers (see
	// resources.TypeMarkers) keyed by resourceType name. runtimeconfig keys the
	// consumer-URL patch on markers.ConsumerURLEnvConfig instead of a hardcoded
	// resourceType name. Wired via SetResourceCatalog; nil (or an unreachable
	// catalog) defers the emission with a warning — emission is a retried
	// cascade, NOT a save gate, so it fails OPEN-WITH-RETRY here (the opposite
	// of design-save's fail-closed ErrResourceCatalogUnavailable: a deferred
	// env-config.js write just retries on the next deploy event and never blocks
	// a user action).
	catalog resourceMarkerCatalog
	store   *spec.ArtifactStore
}

// resourceMarkerCatalog is runtimeconfig's narrow consumer port over the
// dependencies/resources catalog (mirrors design_service's port of the same
// name): it returns the PE-authored CRT marker map (resources.TypeMarkers keyed
// by resourceType name) the consumer-URL patch keys on. *resources.ResourceTypeCatalog
// satisfies it structurally. Wired via SetResourceCatalog at the composition
// root so runtimeconfig needn't import the concrete catalog.
type resourceMarkerCatalog interface {
	MarkersByName(ctx context.Context) (map[string]dependencies.TypeMarkers, error)
}

func NewRuntimeConfigService(componentClient openchoreo.ComponentClient, resourceClient openchoreo.ResourceClient, store *spec.ArtifactStore) *RuntimeConfigService {
	return &RuntimeConfigService{
		componentClient: componentClient,
		resourceClient:  resourceClient,
		store:           store,
	}
}

// SetResourceCatalog wires the CRT marker lookup the consumer-URL patch keys on.
// A nil catalog defers emission (with a warning) whenever the web-app declares a
// platform-resource dependency — never blocks, always retries.
func (s *RuntimeConfigService) SetResourceCatalog(c resourceMarkerCatalog) {
	s.catalog = c
}

// FilesForComponent computes the literal files the named component's
// ReleaseBinding must carry — today just env-config.js, for a web app.
//
// It COMPUTES and does not write. The binding has one writer (the deploy
// stage), which asks for these values while it is composing the whole desired
// state, so the file lands in the same object write as the release pin rather
// than in a follow-up patch that had to wait for the binding to exist.
//
// `ready` is false when a required key could not be populated yet — typically a
// platform-resource binding whose outputs are not yet resolved, or a transient
// OC error on that path. The caller must then leave the field unmanaged: a
// partially populated window._env_ is worse than a stale one, because the
// SPA's src/env.ts throws on it at module load.
// A non-web-app component wants no files at all, which is (nil, true).
func (s *RuntimeConfigService) FilesForComponent(ctx context.Context, orgID, projectID, componentName string) ([]openchoreo.WorkflowFileVar, bool, error) {
	if s == nil {
		return nil, true, nil
	}
	if orgID == "" || projectID == "" || componentName == "" {
		return nil, false, fmt.Errorf("runtime_config: empty orgID/projectID/componentName")
	}

	design, err := s.store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		if spec.IsNotFound(err) {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("runtime_config: read design: %w", err)
	}
	if design == nil {
		return nil, true, nil
	}

	var match *spec.DesignComponent
	for i := range design.Components {
		if k8sname.ToK8sName(design.Components[i].Name) == componentName {
			match = &design.Components[i]
			break
		}
	}
	if match == nil || match.ComponentType != spec.ComponentTypeWebApplication {
		return nil, true, nil
	}

	envValues, ready := s.buildEnvValues(ctx, orgID, projectID, match)
	if !ready {
		slog.InfoContext(ctx, "runtime_config: required keys not yet ready; deferring env-config.js",
			"orgID", orgID, "projectID", projectID, "component", componentName, "keys", sortedKeys(envValues))
		return nil, false, nil
	}
	slog.InfoContext(ctx, "runtime_config: env-config.js composed",
		"orgID", orgID, "projectID", projectID, "component", componentName, "keys", sortedKeys(envValues))
	return []openchoreo.WorkflowFileVar{{
		Key:       "env-config.js",
		MountPath: "/usr/share/nginx/html/",
		Value:     renderEnvConfigJS(envValues),
	}}, true, nil
}

// buildEnvValues assembles the map that becomes `window._env_`.
//   - `<DEP>_<OUTPUT>` — every platform-resource dependency's resolved binding
//     outputs, keyed generically (resources.EnvVarName — the same convention
//     wiring.go injects pod env vars with). No resource-type name is hardcoded:
//     an OIDC dependency's client_id/issuer/scopes and a database dependency's
//     host/port land through the identical mechanism. The values come from each
//     dependency's provisioned binding outputs (resolved by OC); the BFF never
//     calls the upstream from this path.
//
// Sibling service URLs are not emitted. The browser calls same-origin `/api`;
// SPA nginx reverse-proxies to the pod env `<DEP>_URL` OpenChoreo injects.
//
// buildEnvValues returns the map + a `ready` flag. The flag is false
// when a required key couldn't be populated yet (transient OC error,
// SPA URL not yet resolved for an OIDC consumer-URL patch, etc.). The
// caller must NOT write a partial env-config.js on `!ready` — see
// FilesForComponent.
func (s *RuntimeConfigService) buildEnvValues(ctx context.Context, orgID, projectID string, webapp *spec.DesignComponent) (out map[string]interface{}, ready bool) {
	out = map[string]interface{}{}
	ready = true

	// Layer every platform-resource dependency's binding outputs generically.
	// No resource-type name is hardcoded — an OIDC dependency and a database
	// dependency flow through the identical path (see layerPlatformResources).
	if deps := platformResourceDeps(webapp); len(deps) > 0 {
		if ok := s.layerPlatformResources(ctx, orgID, projectID, webapp, deps, out); !ok {
			ready = false
		}
	}

	return out, ready
}

// platformResourceDeps returns every `kind: platform-resource` dependency a
// web-app component declares, in declaration order (nil for a non-web-app or a
// web-app with none). Its emptiness is what gates the whole platform-resource
// layer — including the single CRT-marker catalog fetch — so an auth-free /
// resource-free SPA never touches the resource client or the catalog.
func platformResourceDeps(c *spec.DesignComponent) []spec.Dependency {
	if c == nil || c.ComponentType != spec.ComponentTypeWebApplication {
		return nil
	}
	var out []spec.Dependency
	for i := range c.Dependencies {
		if c.Dependencies[i].Kind == spec.DependencyKindPlatformResource {
			out = append(out, c.Dependencies[i])
		}
	}
	return out
}

// layerPlatformResources emits every platform-resource dependency's resolved
// binding outputs as generic <DEP>_<OUTPUT> keys, and — for any dependency
// whose CRT carries the consumer-URL-env-config annotation — patches the SPA's
// own <origin><consumer-url-path> into that env-config key on the dependency's
// dev binding (declarative: the operator registers the callback URL).
//
// The two halves are graded differently, and that is the point of the split.
//
//	The OUTPUTS are HARD. A SPA without its OIDC client_id cannot start; the
//	values come from the dependency's own provisioned binding and owe nothing to
//	any component being up. Missing them defers the whole file.
//
//	The consumer-URL REGISTRATION is SOFT. It needs the SPA's own external URL,
//	which exists only once the SPA has a rendered binding — so requiring it
//	before the SPA's first write is a demand the SPA cannot satisfy until it has
//	already been deployed. It is not needed before the SPA serves, only before
//	someone logs in, so the converge pass that follows the deploy waves does it.
//
// Grading the registration hard is what blanked a page: an unresolved SPA URL
// (soft) vetoed the client_id (hard) that had been sitting resolved all along,
// and the SPA was published with no window._env_ at all.
//
// It fetches the CRT marker catalog ONCE for the whole pass (only reached when
// the web-app has ≥1 platform-resource dep). Fail-open-with-retry: a nil or
// unreachable catalog or an unresolved binding defer the env-config.js write
// rather than erroring — this is a retried cascade hook, not a user-facing save
// gate (contrast design-save, which fails CLOSED on an unreachable catalog
// because a silent skip there could expose an API).
//
// All-or-nothing at the write level: any not-ready dependency sets ready=false,
// and the caller then skips the whole write — a deferring dependency contributes
// NO keys of its own, and keys already in `out` are never shipped because the
// write is gated. The SPA is thus never handed a partial window._env_.
func (s *RuntimeConfigService) layerPlatformResources(ctx context.Context, orgID, projectID string, webapp *spec.DesignComponent, deps []spec.Dependency, out map[string]interface{}) bool {
	if s.resourceClient == nil {
		slog.WarnContext(ctx, "runtime_config: resourceClient not wired; deferring platform-resource outputs",
			"projectID", projectID, "component", webapp.Name)
		return false
	}

	// One catalog read per emission pass. On failure DEFER (fail-open-with-retry):
	// without the markers we cannot know which deps need the consumer-URL patch,
	// so retrying is safer than emitting an unpatched binding's outputs.
	markers, err := s.catalogMarkers(ctx)
	if err != nil {
		slog.WarnContext(ctx, "runtime_config: CRT marker catalog unavailable; deferring platform-resource outputs",
			"projectID", projectID, "component", webapp.Name, "error", err)
		return false
	}

	// Resolve the SPA's public origin lazily + once — only the annotation-driven
	// patch needs it (the OC ReleaseBinding status fills the external URL after
	// the first reconcile).
	spaOrigin, spaResolved := "", false

	ready := true
	for i := range deps {
		dep := deps[i]
		bindingName := ocname.ExternalResourceBindingName(projectID, dep.Name, bindingEnv)
		m := markers[dep.ResourceType]

		// Annotation-driven consumer-URL patch — the SOFT half. Gate on
		// ConsumerURLEnvConfig (the path is already defaulted into the markers when
		// the env-config marker is set — never branch on the path alone).
		//
		// Neither an unresolved SPA URL nor a failed patch touches `ready`: the
		// registration is retried by the converge pass and by the converge watcher,
		// and holding the file back for it would keep a startable SPA off the air
		// for a fact it does not read.
		if m.ConsumerURLEnvConfig != "" {
			if !spaResolved {
				spaOrigin = strings.TrimRight(s.componentExternalURL(ctx, orgID, projectID, webapp.Name), "/")
				spaResolved = true
			}
			switch {
			case spaOrigin == "":
				slog.InfoContext(ctx, "runtime_config: SPA external URL not yet resolved; consumer URL registers on the converge pass",
					"projectID", projectID, "component", webapp.Name, "dep", dep.Name)
			default:
				// Idempotent inside the client (no-op when already present), so
				// re-running on every cascade doesn't churn the CR.
				if perr := s.resourceClient.PatchBindingEnvironmentConfigs(ctx, orgID, bindingName,
					map[string]string{m.ConsumerURLEnvConfig: spaOrigin + m.ConsumerURLPath}); perr != nil {
					slog.WarnContext(ctx, "runtime_config: patch binding consumer URL failed; will retry on the next converge",
						"projectID", projectID, "component", webapp.Name, "dep", dep.Name,
						"binding", bindingName, "envConfig", m.ConsumerURLEnvConfig, "error", perr)
				}
			}
		}

		// Read the resolved outputs. Not-ready (binding absent/without status, or
		// no outputs yet because the operator hasn't reconciled) → defer with NO
		// partial keys for this dep.
		b, berr := s.resourceClient.GetBinding(ctx, orgID, bindingName)
		if berr != nil {
			slog.WarnContext(ctx, "runtime_config: get platform-resource binding failed; deferring",
				"projectID", projectID, "component", webapp.Name, "dep", dep.Name, "binding", bindingName, "error", berr)
			ready = false
			continue
		}
		if b == nil || b.Status == nil || len(b.Status.Outputs) == 0 {
			slog.InfoContext(ctx, "runtime_config: platform-resource binding not ready; will retry on next cascade",
				"projectID", projectID, "component", webapp.Name, "dep", dep.Name, "binding", bindingName)
			ready = false
			continue
		}
		for _, o := range b.Status.Outputs {
			out[ocname.EnvVarName(dep.Name, o.Name)] = o.Value
		}
	}
	return ready
}

// catalogMarkers reads the PE-authored CRT marker map once. A nil catalog is a
// deferrable error (not a panic): the composition root wires it, but a
// nil-catalog test or a mis-wire must defer-and-retry, never crash the cascade.
func (s *RuntimeConfigService) catalogMarkers(ctx context.Context) (map[string]dependencies.TypeMarkers, error) {
	if s.catalog == nil {
		return nil, fmt.Errorf("runtime_config: no resource-type catalog wired")
	}
	return s.catalog.MarkersByName(ctx)
}

// componentExternalURL returns the first external URL OC has resolved
// for the named component, or "" when none is materialised yet.
func (s *RuntimeConfigService) componentExternalURL(ctx context.Context, orgID, projectID, componentName string) string {
	if s.componentClient == nil {
		return ""
	}
	k8sName := k8sname.ToK8sName(componentName)
	list, err := s.componentClient.ListDeployments(ctx, orgID, projectID, k8sName)
	if err != nil || list == nil {
		return ""
	}
	for _, d := range list.Items {
		if d.EndpointURL != "" {
			return d.EndpointURL
		}
	}
	return ""
}

// renderEnvConfigJS produces the literal JS the SPA's index.html loads
// synchronously before its bundle. Keys are sorted for byte-stable
// output so equality checks don't flap.
//
// Values that fail to marshal are emitted as `null` with a comment —
// silently dropping them would leave a trailing comma that aborts the
// SPA's <script> with a SyntaxError and blanks the page. `null` is at
// least a parseable value the SPA's typed env shim can throw on
// loudly.
func renderEnvConfigJS(values map[string]interface{}) string {
	var b strings.Builder
	b.WriteString("window._env_ = {\n")
	keys := sortedKeys(values)
	for i, k := range keys {
		raw, err := json.Marshal(values[k])
		if err != nil || len(raw) == 0 {
			raw = []byte("null")
		}
		b.WriteString("  ")
		// JS-side keys are bare identifiers — safe to emit unquoted since
		// ocname.EnvVarName returns only [A-Z0-9_].
		b.WriteString(k)
		b.WriteString(": ")
		b.Write(raw)
		if i < len(keys)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("};\n")
	return b.String()
}

func sortedKeys(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
