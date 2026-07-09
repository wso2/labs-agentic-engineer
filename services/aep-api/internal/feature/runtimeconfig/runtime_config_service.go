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
	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/internal/feature/dependencies/resources"
	"github.com/wso2/aep/aep-api/internal/platform/k8sname"
	"github.com/wso2/aep/aep-api/models"
)

const (
	// bindingEnv is the single environment runtime-config targets (mirrors
	// provisioning.defaultEnv). A web-app's platform-resource binding whose
	// outputs drive the SPA lives in this env.
	bindingEnv = "development"
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
	store   *artifacts.ArtifactStore
}

// resourceMarkerCatalog is runtimeconfig's narrow consumer port over the
// dependencies/resources catalog (mirrors design_service's port of the same
// name): it returns the PE-authored CRT marker map (resources.TypeMarkers keyed
// by resourceType name) the consumer-URL patch keys on. *resources.ResourceTypeCatalog
// satisfies it structurally. Wired via SetResourceCatalog at the composition
// root so runtimeconfig needn't import the concrete catalog.
type resourceMarkerCatalog interface {
	MarkersByName(ctx context.Context) (map[string]resources.TypeMarkers, error)
}

func NewRuntimeConfigService(componentClient openchoreo.ComponentClient, resourceClient openchoreo.ResourceClient, store *artifacts.ArtifactStore) *RuntimeConfigService {
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

// EmitForComponent computes the env-config.js content for the named
// component and writes it onto each of the component's ReleaseBindings.
// No-op when the component is not a web-app.
//
// Idempotent + best-effort. The OC client returns a soft no-op when no
// ReleaseBindings exist yet — the cascade hook re-fires on every deploy
// in the project so the file lands after the first build catches up.
func (s *RuntimeConfigService) EmitForComponent(ctx context.Context, orgID, projectID, componentName string) error {
	if s == nil {
		return nil
	}
	if orgID == "" || projectID == "" || componentName == "" {
		return fmt.Errorf("runtime_config: empty orgID/projectID/componentName")
	}

	design, err := s.store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		if artifacts.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("runtime_config: read design: %w", err)
	}
	if design == nil {
		return nil
	}

	var match *models.DesignComponent
	for i := range design.Components {
		if k8sname.ToK8sName(design.Components[i].Name) == componentName {
			match = &design.Components[i]
			break
		}
	}
	if match == nil || match.ComponentType != models.ComponentTypeWebApplication {
		return nil
	}

	envValues, ready := s.buildEnvValues(ctx, orgID, projectID, match, design)
	if !ready {
		// One or more required keys couldn't be populated yet (transient
		// OC error, SPA URL not resolved, etc.). DO NOT write a partial
		// env-config.js — that would either blank previously valid keys
		// or ship a window._env_ that the SPA's src/env.ts throws on at
		// module load. The cascade hook re-fires on every deploy event,
		// so the next sibling deploy (or this SPA's own follow-up
		// reconcile) will retry.
		slog.InfoContext(ctx, "runtime_config: required keys not yet ready; deferring env-config.js write",
			"orgID", orgID,
			"projectID", projectID,
			"component", componentName,
			"keys", sortedKeys(envValues),
		)
		return nil
	}
	file := models.WorkflowFileVar{
		Key:       "env-config.js",
		MountPath: "/usr/share/nginx/html/",
		Value:     renderEnvConfigJS(envValues),
	}

	if err := s.componentClient.UpdateComponentWorkflowFiles(ctx, orgID, projectID, componentName, []models.WorkflowFileVar{file}); err != nil {
		return fmt.Errorf("runtime_config: update workflow files: %w", err)
	}

	slog.InfoContext(ctx, "emitting env-config.js",
		"orgID", orgID,
		"projectID", projectID,
		"component", componentName,
		"keys", sortedKeys(envValues),
	)
	return nil
}

// EmitForProjectSPAs re-emits env-config.js on every web-app component in
// the project. Called from the dispatch cascade so that when ANY component
// lands `deployed` (especially a sibling service whose external URL just
// resolved), every SPA picks up the new value in its ReleaseBinding without
// waiting for the SPA itself to re-deploy.
//
// Idempotent + best-effort: a failure on one component logs and continues.
func (s *RuntimeConfigService) EmitForProjectSPAs(ctx context.Context, orgID, projectID string) error {
	if s == nil {
		return nil
	}
	if orgID == "" || projectID == "" {
		return fmt.Errorf("runtime_config: empty orgID/projectID")
	}
	design, err := s.store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		if artifacts.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("runtime_config: read design: %w", err)
	}
	if design == nil {
		return nil
	}
	for _, c := range design.Components {
		if c.ComponentType != models.ComponentTypeWebApplication {
			continue
		}
		k8sName := k8sname.ToK8sName(c.Name)
		if err := s.EmitForComponent(ctx, orgID, projectID, k8sName); err != nil {
			slog.WarnContext(ctx, "runtime_config: per-SPA emit failed; continuing",
				"orgID", orgID,
				"projectID", projectID,
				"componentName", k8sName,
				"error", err,
			)
		}
	}
	return nil
}

// buildEnvValues assembles the map that becomes `window._env_`.
//   - `API_BASE_URL` — first sibling service dep's external URL (the
//     conventional name for the primary backend).
//   - `<UPPER_SNAKE_NAME>_URL` — every dep, keyed by component name. Lets
//     a SPA with multiple backends address each one explicitly.
//   - `<DEP>_<OUTPUT>` — every platform-resource dependency's resolved binding
//     outputs, keyed generically (resources.EnvVarName — the same convention
//     wiring.go injects pod env vars with). No resource-type name is hardcoded:
//     an OIDC dependency's client_id/issuer/scopes and a database dependency's
//     host/port land through the identical mechanism. The values come from each
//     dependency's provisioned binding outputs (resolved by OC); the BFF never
//     calls the upstream from this path.
//
// buildEnvValues returns the map + a `ready` flag. The flag is false
// when a required key couldn't be populated yet (transient OC error,
// SPA URL not yet resolved, etc.). The caller must NOT write a
// partial env-config.js on `!ready` — see EmitForComponent.
func (s *RuntimeConfigService) buildEnvValues(ctx context.Context, orgID, projectID string, webapp *models.DesignComponent, design *artifacts.DesignFile) (out map[string]interface{}, ready bool) {
	out = map[string]interface{}{}
	ready = true

	// Index sibling components by name for type lookup.
	byName := make(map[string]models.DesignComponent, len(design.Components))
	for _, c := range design.Components {
		byName[c.Name] = c
	}

	var firstServiceURL string
	for _, dep := range webapp.ComponentDependsOn() {
		sibling, ok := byName[dep]
		if !ok {
			continue
		}
		// Skip non-service deps (peer webapps aren't called over HTTP).
		if sibling.ComponentType != models.ComponentTypeService {
			continue
		}
		k8sName := k8sname.ToK8sName(dep)
		list, err := s.componentClient.ListDeployments(ctx, orgID, projectID, k8sName)
		if err != nil {
			// Transient OC failure on a required dep. Mark not-ready so
			// the caller skips the write and preserves the previously
			// valid env-config.js for the pod.
			slog.WarnContext(ctx, "runtime_config: ListDeployments error for dep; deferring",
				"projectID", projectID, "component", webapp.Name, "dep", dep, "error", err)
			ready = false
			continue
		}
		if list == nil {
			// A required dep with no deployment list is not resolvable
			// yet — defer rather than ship an incomplete env-config.js.
			ready = false
			continue
		}
		url := ""
		for _, d := range list.Items {
			if d.EndpointURL != "" {
				url = strings.TrimRight(d.EndpointURL, "/")
				break
			}
		}
		if url == "" {
			// Dep not yet deployed — not an error, but we don't have a
			// URL for it. Defer rather than ship a window._env_ that
			// will throw at module load.
			ready = false
			continue
		}
		out[upperSnakeKey(dep)+"_URL"] = url
		if firstServiceURL == "" {
			firstServiceURL = url
		}
	}
	if firstServiceURL != "" {
		out["API_BASE_URL"] = firstServiceURL
	}

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
func platformResourceDeps(c *models.DesignComponent) []models.Dependency {
	if c == nil || c.ComponentType != models.ComponentTypeWebApplication {
		return nil
	}
	var out []models.Dependency
	for i := range c.Dependencies {
		if c.Dependencies[i].Kind == models.DependencyKindPlatformResource {
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
// It fetches the CRT marker catalog ONCE for the whole pass (only reached when
// the web-app has ≥1 platform-resource dep). Fail-open-with-retry: a nil or
// unreachable catalog, a failed patch, or an unresolved binding all return
// false ("defer the env-config.js write") rather than erroring — this is a
// retried cascade hook, not a user-facing save gate, so deferring simply waits
// for the next deploy event (contrast design-save, which fails CLOSED on an
// unreachable catalog because a silent skip there could expose an API).
//
// All-or-nothing at the write level: any not-ready dependency sets ready=false,
// and the caller (EmitForComponent) then skips the whole write — a deferring
// dependency contributes NO keys of its own (`continue` before its outputs),
// and sibling deps' keys already in `out` are never shipped because the write
// is gated. The SPA is thus never handed a partial window._env_.
func (s *RuntimeConfigService) layerPlatformResources(ctx context.Context, orgID, projectID string, webapp *models.DesignComponent, deps []models.Dependency, out map[string]interface{}) bool {
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
		bindingName := resources.ExternalResourceBindingName(projectID, dep.Name, bindingEnv)
		m := markers[dep.ResourceType]

		// Annotation-driven consumer-URL patch. Gate on ConsumerURLEnvConfig (the
		// path is already defaulted into the markers when the env-config marker is
		// set — never branch on the path alone).
		if m.ConsumerURLEnvConfig != "" {
			if !spaResolved {
				spaOrigin = strings.TrimRight(s.componentExternalURL(ctx, orgID, projectID, webapp.Name), "/")
				spaResolved = true
			}
			if spaOrigin == "" {
				slog.InfoContext(ctx, "runtime_config: SPA external URL not yet resolved; will retry on next cascade",
					"projectID", projectID, "component", webapp.Name, "dep", dep.Name)
				ready = false
				continue
			}
			// Idempotent inside the client (no-op when already present), so
			// re-running on every cascade doesn't churn the CR. On failure DEFER.
			if perr := s.resourceClient.PatchBindingEnvironmentConfigs(ctx, orgID, bindingName,
				map[string]string{m.ConsumerURLEnvConfig: spaOrigin + m.ConsumerURLPath}); perr != nil {
				slog.WarnContext(ctx, "runtime_config: patch binding consumer URL failed; deferring",
					"projectID", projectID, "component", webapp.Name, "dep", dep.Name,
					"binding", bindingName, "envConfig", m.ConsumerURLEnvConfig, "error", perr)
				ready = false
				continue
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
			out[resources.EnvVarName(dep.Name, o.Name)] = o.Value
		}
	}
	return ready
}

// catalogMarkers reads the PE-authored CRT marker map once. A nil catalog is a
// deferrable error (not a panic): the composition root wires it, but a
// nil-catalog test or a mis-wire must defer-and-retry, never crash the cascade.
func (s *RuntimeConfigService) catalogMarkers(ctx context.Context) (map[string]resources.TypeMarkers, error) {
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
		// upperSnakeKey returns only [A-Z0-9_].
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

// upperSnakeKey converts a component name (kebab- or camelCase) into the
// upper-snake form used as a `window._env_` key prefix. Drops any chars
// outside [A-Za-z0-9_] so the result is a safe JS identifier.
func upperSnakeKey(name string) string {
	var b strings.Builder
	prevAlnum := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
			prevAlnum = true
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevAlnum = true
		case r == '-' || r == '_':
			if prevAlnum {
				b.WriteRune('_')
			}
			prevAlnum = false
		default:
			prevAlnum = false
		}
	}
	return strings.TrimRight(b.String(), "_")
}
