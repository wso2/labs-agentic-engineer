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

// UNIT tier for the runtime-config emit pipeline.
// runtimeconfig is BACKEND-ONLY: it has no *_huma.go (no HTTP surface — it is
// driven by the codingagent dispatch cascade, not a router route) and no SQL,
// so there is deliberately NO component tier and NO dbtest here — the correct
// exception per ADR-0003 §9, not a coverage gap.
//
// The out-of-process seams are doubled exactly at the process boundary:
//   - openchoreo.ComponentClient → the generated moq (ListDeployments to read a
//     component's resolved external URL; UpdateComponentWorkflowFiles to emit
//     env-config.js onto the ReleaseBindings — we CAPTURE that payload and
//     assert its window._env_ shape).
//   - openchoreo.ResourceClient → the generated moq (PatchBindingEnvironmentConfigs
//     for the annotation-driven consumer-URL patch; GetBinding to read a
//     platform-resource dependency's resolved outputs). The <DEP>_<OUTPUT> keys
//     are emitted purely from these binding outputs — there is no BFF→upstream
//     call on this path, and NO resource-type name is hardcoded.
//   - the CRT marker catalog (resourceMarkerCatalog port) → a hand fake keyed by
//     resourceType name; the consumer-URL patch keys on its markers.
//   - spec.ArtifactStore → the REAL spec.NewArtifactStore decorator
//     over artifactstest.FakeArtifactService, fed a valid design working-tree
//     map. The frontmatter → spec.DesignComponent parse (componentType,
//     dependencies) is therefore the real one, not a stub.
package runtimeconfig

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/platform/ocname"
	"github.com/wso2/aep/aep-api/internal/spec"
	"github.com/wso2/aep/aep-api/internal/spec/artifactstest"
)

// --- test doubles ------------------------------------------------------------

// storeWith wraps the REAL spec.NewArtifactStore decorator over a fake
// artifact service that serves the given design working-tree map. ReadDesign's
// frontmatter parse (AssembleDesign) therefore runs for real.
func storeWith(files map[string]string) *spec.ArtifactStore {
	return spec.NewArtifactStore(&artifactstest.FakeArtifactService{
		ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return files, nil
		},
	})
}

// ocResolving builds a ComponentClient moq whose ListDeployments returns the
// mapped external URL for a component (keyed by the k8s component name), or an
// empty deployment list (no URL resolved yet) for any unmapped component.
func ocResolving(urlsByComponent map[string]string) *ocmocks.ComponentClientMock {
	return &ocmocks.ComponentClientMock{
		ListDeploymentsFunc: func(_ context.Context, _, _, componentName string) (*gen.DeploymentList, error) {
			if u, ok := urlsByComponent[componentName]; ok && u != "" {
				return &gen.DeploymentList{Items: []gen.Deployment{{EndpointURL: u}}}, nil
			}
			return &gen.DeploymentList{}, nil
		},
	}
}

// rcOutputs builds a ResourceClient moq that returns the same status.outputs for
// EVERY binding (single-dep tests), recording patch calls. patchErr is returned
// by PatchBindingEnvironmentConfigs. A nil outputs map yields a binding with NO
// status (not ready yet).
func rcOutputs(outputs map[string]string, patchErr error) *ocmocks.ResourceClientMock {
	return rcBindings(map[string]map[string]string{"*": outputs}, patchErr)
}

// rcBindings builds a ResourceClient moq that returns per-binding status.outputs
// (keyed by binding name; the "*" key is a catch-all). A binding absent from the
// map (and with no "*" fallback) yields NO status (not ready). patchErr is
// returned by every PatchBindingEnvironmentConfigs call.
func rcBindings(byBinding map[string]map[string]string, patchErr error) *ocmocks.ResourceClientMock {
	return &ocmocks.ResourceClientMock{
		PatchBindingEnvironmentConfigsFunc: func(context.Context, string, string, map[string]string) error {
			return patchErr
		},
		GetBindingFunc: func(_ context.Context, _, bindingName string) (*openchoreo.ResourceReleaseBinding, error) {
			outputs, ok := byBinding[bindingName]
			if !ok {
				outputs, ok = byBinding["*"]
			}
			if !ok || outputs == nil {
				return &openchoreo.ResourceReleaseBinding{}, nil // no status → not ready
			}
			outs := make([]openchoreo.ResolvedOutput, 0, len(outputs))
			for _, k := range sortedStrs(outputs) {
				outs = append(outs, openchoreo.ResolvedOutput{Name: k, Value: outputs[k]})
			}
			return &openchoreo.ResourceReleaseBinding{
				Status: &openchoreo.ResourceReleaseBindingStatus{Outputs: outs},
			}, nil
		},
	}
}

func sortedStrs(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// fakeCatalog is a hand double of the resourceMarkerCatalog port. markers is
// keyed by resourceType name; err (when set) simulates an unreachable OC
// catalog. calls counts the reads so tests can assert the "once per pass" fetch.
type fakeCatalog struct {
	markers map[string]dependencies.TypeMarkers
	err     error
	calls   int
}

func (f *fakeCatalog) MarkersByName(context.Context) (map[string]dependencies.TypeMarkers, error) {
	f.calls++
	return f.markers, f.err
}

// authMarkers is the marker set a PE authors on an end-user-auth CRT: the
// consumer-URL patch annotation (env-config key + defaulted /callback path).
// Built through the REAL resources.MarkersFrom so the /callback default is
// exercised, not hand-stamped.
func authMarkers(resourceType string) map[string]dependencies.TypeMarkers {
	return map[string]dependencies.TypeMarkers{
		resourceType: dependencies.MarkersFrom(nil, map[string]string{
			dependencies.AnnotationConsumerURLEnvConfig: "redirectUris",
		}),
	}
}

// authOutputs is the full resolved-output set a ready end-user-auth binding
// carries. ALL outputs arrive as literal values (the CRT emits client_id via a
// CEL value OC resolves from the upstream's live status).
func authOutputs() map[string]string {
	return map[string]string{
		"client_id": "web-cid",
		"issuer":    "http://thunder.local",
		"jwks_url":  "http://thunder.local/jwks",
		"scopes":    "openid profile email",
	}
}

// readDesign parses a design working-tree map through the real store and
// returns the assembled DesignFile.
func readDesign(t *testing.T, files map[string]string) *spec.DesignFile {
	t.Helper()
	d, err := storeWith(files).ReadDesign(context.Background(), "acme", "proj")
	if err != nil {
		t.Fatalf("ReadDesign: %v", err)
	}
	if d == nil {
		t.Fatalf("ReadDesign returned nil design for fixture")
	}
	return d
}

func componentNamed(t *testing.T, d *spec.DesignFile, name string) *spec.DesignComponent {
	t.Helper()
	for i := range d.Components {
		if d.Components[i].Name == name {
			return &d.Components[i]
		}
	}
	t.Fatalf("component %q not present in assembled design", name)
	return nil
}

// svcWithCatalog builds a service and wires the catalog port (nil catalog left
// unwired to exercise the defer-when-unwired path).
func svcWithCatalog(oc openchoreo.ComponentClient, rc openchoreo.ResourceClient, store *spec.ArtifactStore, cat resourceMarkerCatalog) *RuntimeConfigService {
	s := NewRuntimeConfigService(oc, rc, store)
	if cat != nil {
		s.SetResourceCatalog(cat)
	}
	return s
}

func keySet(m map[string]interface{}) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

// --- design fixtures ---------------------------------------------------------

func rootDesignMd() string {
	return "---\nsourceSpec: v1\n---\n\nOverview prose.\n"
}

func serviceComponentMd() string {
	return buildComponentJSON("api", "service", nil, nil)
}

// prDep is a platform-resource dependency spec for the fixtures.
type prDep struct{ name, resourceType string }

// webappMd renders a web-application component with only component-kind
// dependencies (canonical type: spec.ComponentTypeWebApplication —
// OpenChoreo's own term).
func webappMd(name string, deps ...string) string {
	return buildComponentJSON(name, "web-application", deps, nil)
}

// webappWithPR renders a web-application that declares the given
// platform-resource dependencies, plus optional component-kind deps.
func webappWithPR(name string, prDeps []prDep, deps ...string) string {
	return buildComponentJSON(name, "web-application", deps, prDeps)
}

// buildComponentJSON assembles a `components/<name>/design.json` body:
// component-kind dependencies from deps, plus a platform-resource dependency per
// prDeps entry.
func buildComponentJSON(name, typ string, deps []string, prDeps []prDep) string {
	m := map[string]any{"name": name, "type": typ}
	if typ == "service" {
		m["language"] = "Go"
	}
	dependencies := make([]map[string]any, 0, len(deps)+len(prDeps))
	for _, d := range deps {
		dependencies = append(dependencies, map[string]any{"kind": "component", "name": d})
	}
	for _, p := range prDeps {
		dependencies = append(dependencies, map[string]any{
			"kind":         "platform-resource",
			"name":         p.name,
			"resourceType": p.resourceType,
		})
	}
	m["dependencies"] = dependencies
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(b) + "\n"
}

// --- platformResourceDeps ----------------------------------------------------

func Test_platformResourceDeps(t *testing.T) {
	t.Parallel()
	auth := spec.Dependency{Kind: spec.DependencyKindPlatformResource, Name: "user-auth", ResourceType: "thunder-app"}
	db := spec.Dependency{Kind: spec.DependencyKindPlatformResource, Name: "orders-db", ResourceType: "postgres-cnpg"}
	comp := spec.Dependency{Kind: spec.DependencyKindComponent, Name: "api"}

	cases := []struct {
		name  string
		in    *spec.DesignComponent
		wantN []string // dep names, in order
	}{
		{"nil component", nil, nil},
		{"web-application no deps", &spec.DesignComponent{ComponentType: spec.ComponentTypeWebApplication}, nil},
		{"service with a PR dep is not a web application", &spec.DesignComponent{ComponentType: spec.ComponentTypeService, Dependencies: []spec.Dependency{auth}}, nil},
		{"web-application with only component deps", &spec.DesignComponent{ComponentType: spec.ComponentTypeWebApplication, Dependencies: []spec.Dependency{comp}}, nil},
		{"web-application with two PR deps, in order", &spec.DesignComponent{ComponentType: spec.ComponentTypeWebApplication, Dependencies: []spec.Dependency{comp, auth, db}}, []string{"user-auth", "orders-db"}},
		// Retired spellings are not understood anywhere — no shims, no
		// normalization. Designs carrying them must be migrated.
		{"retired webapp spelling does not match", &spec.DesignComponent{ComponentType: "webapp", Dependencies: []spec.Dependency{auth}}, nil},
		{"retired web-app spelling does not match", &spec.DesignComponent{ComponentType: "web-app", Dependencies: []spec.Dependency{auth}}, nil},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := platformResourceDeps(c.in)
			if len(got) != len(c.wantN) {
				t.Fatalf("platformResourceDeps = %d deps; want %d (%v)", len(got), len(c.wantN), c.wantN)
			}
			for i, want := range c.wantN {
				if got[i].Name != want {
					t.Errorf("dep[%d] = %q; want %q", i, got[i].Name, want)
				}
			}
		})
	}
}

// --- naming consistency ------------------------------------------------------

// Test_outputKeyNaming_matchesWiringConvention pins the window._env_ key naming
// to the SAME helper wiring.go injects pod env vars with (ocname.EnvVarName — the
// single source of truth every consumer now calls directly, so the browser's
// window._env_ key and the pod env var for one dep+output cannot drift).
func Test_outputKeyNaming_matchesWiringConvention(t *testing.T) {
	t.Parallel()
	cases := []struct{ dep, out, want string }{
		{"user-auth", "client_id", "USER_AUTH_CLIENT_ID"},
		{"user-auth", "issuer", "USER_AUTH_ISSUER"},
		{"user-auth", "jwks_url", "USER_AUTH_JWKS_URL"},
		{"user-auth", "scopes", "USER_AUTH_SCOPES"},
		{"orders-db", "host", "ORDERS_DB_HOST"},
		{"orders-db", "port", "ORDERS_DB_PORT"},
	}
	for _, c := range cases {
		if got := ocname.EnvVarName(c.dep, c.out); got != c.want {
			t.Errorf("ocname.EnvVarName(%q,%q) = %q; want %q", c.dep, c.out, got, c.want)
		}
	}
}

// --- buildEnvValues: generic emission ---------------------------------------

func Test_buildEnvValues_genericEmission(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("single auth dep: exact USER_AUTH_* key set, no THUNDER_*, patch once", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			spec.DesignRootFile:          rootDesignMd(),
			"components/web/design.json": webappWithPR("web", []prDep{{"user-auth", "thunder-app"}}),
		}
		design := readDesign(t, files)
		web := componentNamed(t, design, "web")

		oc := ocResolving(map[string]string{"web": "http://web.local/"})
		rc := rcOutputs(authOutputs(), nil)
		cat := &fakeCatalog{markers: authMarkers("thunder-app")}
		svc := svcWithCatalog(oc, rc, nil, cat)

		out, ready := svc.buildEnvValues(ctx, "acme", "proj", web)
		if !ready {
			t.Fatalf("want ready=true; got false (out=%v)", out)
		}
		want := map[string]interface{}{
			"USER_AUTH_CLIENT_ID": "web-cid",
			"USER_AUTH_ISSUER":    "http://thunder.local",
			"USER_AUTH_JWKS_URL":  "http://thunder.local/jwks",
			"USER_AUTH_SCOPES":    "openid profile email",
		}
		if got := keySet(out); len(got) != len(want) {
			t.Fatalf("emitted key set = %v; want exactly %v", got, want)
		}
		for k, v := range want {
			if out[k] != v {
				t.Errorf("out[%q] = %v; want %v", k, out[k], v)
			}
		}
		for k := range out {
			if strings.HasPrefix(k, "THUNDER_") {
				t.Errorf("no legacy THUNDER_* key must be emitted; got %q", k)
			}
		}
		if cat.calls != 1 {
			t.Errorf("catalog must be read exactly once per pass; got %d", cat.calls)
		}
		calls := rc.PatchBindingEnvironmentConfigsCalls()
		if len(calls) != 1 {
			t.Fatalf("want exactly 1 consumer-URL patch; got %d", len(calls))
		}
		if got := calls[0].Configs["redirectUris"]; got != "http://web.local/callback" {
			t.Errorf("patched redirectUris = %q; want http://web.local/callback (default path)", got)
		}
		if calls[0].BindingName != "proj-user-auth-development" {
			t.Errorf("patched binding = %q; want proj-user-auth-development", calls[0].BindingName)
		}
	})

	t.Run("two PR deps: both output prefixes emitted; patch only the annotated dep", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			spec.DesignRootFile:          rootDesignMd(),
			"components/web/design.json": webappWithPR("web", []prDep{{"user-auth", "thunder-app"}, {"orders-db", "postgres-cnpg"}}),
		}
		design := readDesign(t, files)
		web := componentNamed(t, design, "web")

		oc := ocResolving(map[string]string{"web": "http://web.local"})
		rc := rcBindings(map[string]map[string]string{
			"proj-user-auth-development": authOutputs(),
			"proj-orders-db-development": {"host": "db.local", "port": "5432"},
		}, nil)
		// thunder-app carries the consumer-URL marker; postgres-cnpg carries none.
		cat := &fakeCatalog{markers: authMarkers("thunder-app")}
		svc := svcWithCatalog(oc, rc, nil, cat)

		out, ready := svc.buildEnvValues(ctx, "acme", "proj", web)
		if !ready {
			t.Fatalf("want ready=true; got false (out=%v)", out)
		}
		want := map[string]interface{}{
			"USER_AUTH_CLIENT_ID": "web-cid",
			"USER_AUTH_ISSUER":    "http://thunder.local",
			"USER_AUTH_JWKS_URL":  "http://thunder.local/jwks",
			"USER_AUTH_SCOPES":    "openid profile email",
			"ORDERS_DB_HOST":      "db.local",
			"ORDERS_DB_PORT":      "5432",
		}
		if got := keySet(out); len(got) != len(want) {
			t.Fatalf("emitted key set = %v; want exactly %v", got, want)
		}
		for k, v := range want {
			if out[k] != v {
				t.Errorf("out[%q] = %v; want %v", k, out[k], v)
			}
		}
		// Only the annotated (thunder-app) dep is patched; postgres has no marker.
		calls := rc.PatchBindingEnvironmentConfigsCalls()
		if len(calls) != 1 {
			t.Fatalf("want exactly 1 patch (annotated dep only); got %d", len(calls))
		}
		if calls[0].BindingName != "proj-user-auth-development" {
			t.Errorf("patched binding = %q; want proj-user-auth-development", calls[0].BindingName)
		}
	})

	t.Run("custom consumer-url-path patches origin+path", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			spec.DesignRootFile:          rootDesignMd(),
			"components/web/design.json": webappWithPR("web", []prDep{{"user-auth", "thunder-app"}}),
		}
		design := readDesign(t, files)
		web := componentNamed(t, design, "web")

		oc := ocResolving(map[string]string{"web": "http://web.local"})
		rc := rcOutputs(authOutputs(), nil)
		cat := &fakeCatalog{markers: map[string]dependencies.TypeMarkers{
			"thunder-app": dependencies.MarkersFrom(nil, map[string]string{
				dependencies.AnnotationConsumerURLEnvConfig: "redirectUris",
				dependencies.AnnotationConsumerURLPath:      "/oauth/callback",
			}),
		}}
		svc := svcWithCatalog(oc, rc, nil, cat)

		_, ready := svc.buildEnvValues(ctx, "acme", "proj", web)
		if !ready {
			t.Fatalf("want ready=true")
		}
		calls := rc.PatchBindingEnvironmentConfigsCalls()
		if len(calls) != 1 || calls[0].Configs["redirectUris"] != "http://web.local/oauth/callback" {
			t.Fatalf("want patch redirectUris=http://web.local/oauth/callback; got %v", calls)
		}
	})

	t.Run("absent annotation: outputs emitted, NO patch call", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			spec.DesignRootFile:          rootDesignMd(),
			"components/web/design.json": webappWithPR("web", []prDep{{"orders-db", "postgres-cnpg"}}),
		}
		design := readDesign(t, files)
		web := componentNamed(t, design, "web")

		oc := ocResolving(map[string]string{"web": "http://web.local"})
		rc := rcOutputs(map[string]string{"host": "db.local", "port": "5432"}, nil)
		cat := &fakeCatalog{markers: map[string]dependencies.TypeMarkers{}} // no markers for postgres
		svc := svcWithCatalog(oc, rc, nil, cat)

		out, ready := svc.buildEnvValues(ctx, "acme", "proj", web)
		if !ready {
			t.Fatalf("want ready=true; got false (out=%v)", out)
		}
		if out["ORDERS_DB_HOST"] != "db.local" || out["ORDERS_DB_PORT"] != "5432" {
			t.Errorf("want ORDERS_DB_* outputs emitted; got %v", out)
		}
		if n := len(rc.PatchBindingEnvironmentConfigsCalls()); n != 0 {
			t.Errorf("no consumer-URL patch expected without the annotation; got %d", n)
		}
	})

	t.Run("web-app with only component deps: no catalog read, no resource client touch", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			spec.DesignRootFile:          rootDesignMd(),
			"components/web/design.json": webappMd("web", "api"),
			"components/api/design.json": serviceComponentMd(),
		}
		design := readDesign(t, files)
		web := componentNamed(t, design, "web")

		oc := ocResolving(map[string]string{"api": "http://api.local"})
		rc := rcOutputs(authOutputs(), nil) // wired but must never be touched
		cat := &fakeCatalog{markers: authMarkers("thunder-app")}
		svc := svcWithCatalog(oc, rc, nil, cat)

		out, ready := svc.buildEnvValues(ctx, "acme", "proj", web)
		if !ready {
			t.Fatalf("want ready=true; got false (out=%v)", out)
		}
		if _, ok := out["API_BASE_URL"]; ok {
			t.Errorf("API_BASE_URL must not be emitted; browser uses same-origin /api; out=%v", out)
		}
		if _, ok := out["API_URL"]; ok {
			t.Errorf("API_URL must not be emitted; pod env is for nginx only; out=%v", out)
		}
		if len(out) != 0 {
			t.Errorf("want empty env map for a webapp with only a sibling service dep; got %v", out)
		}
		if n := len(oc.ListDeploymentsCalls()); n != 0 {
			t.Errorf("ListDeployments must not be called for a sibling service dep; got %d", n)
		}
		if cat.calls != 0 {
			t.Errorf("catalog must NOT be read when there is no platform-resource dep; got %d", cat.calls)
		}
		if n := len(rc.GetBindingCalls()); n != 0 {
			t.Errorf("no binding read expected; got %d", n)
		}
	})
}

// --- buildEnvValues: defer semantics ----------------------------------------

func Test_buildEnvValues_defers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	authWebFiles := map[string]string{
		spec.DesignRootFile:          rootDesignMd(),
		"components/web/design.json": webappWithPR("web", []prDep{{"user-auth", "thunder-app"}}, "api"),
		"components/api/design.json": serviceComponentMd(),
	}

	t.Run("unresolved sibling service dep does not gate emission", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			spec.DesignRootFile:          rootDesignMd(),
			"components/web/design.json": webappMd("web", "api"),
			"components/api/design.json": serviceComponentMd(),
		}
		design := readDesign(t, files)
		web := componentNamed(t, design, "web")

		oc := ocResolving(map[string]string{})
		out, ready := NewRuntimeConfigService(oc, nil, nil).buildEnvValues(ctx, "acme", "proj", web)
		if !ready {
			t.Fatalf("want ready=true; sibling API URL is not a window._env_ key; got false (out=%v)", out)
		}
		if _, ok := out["API_BASE_URL"]; ok {
			t.Errorf("API_BASE_URL must be absent; out=%v", out)
		}
		if n := len(oc.ListDeploymentsCalls()); n != 0 {
			t.Errorf("ListDeployments must not be called for a sibling service dep; got %d", n)
		}
	})

	t.Run("ListDeployments is not consulted for a sibling service dep", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			spec.DesignRootFile:          rootDesignMd(),
			"components/web/design.json": webappMd("web", "api"),
			"components/api/design.json": serviceComponentMd(),
		}
		design := readDesign(t, files)
		web := componentNamed(t, design, "web")

		oc := &ocmocks.ComponentClientMock{
			ListDeploymentsFunc: func(context.Context, string, string, string) (*gen.DeploymentList, error) {
				t.Errorf("ListDeployments must not be called for sibling service deps")
				return nil, errors.New("oc: transient")
			},
		}
		out, ready := NewRuntimeConfigService(oc, nil, nil).buildEnvValues(ctx, "acme", "proj", web)
		if !ready {
			t.Fatalf("want ready=true when sibling ListDeployments is unused; got false (out=%v)", out)
		}
	})

	t.Run("non-service component dep is skipped, not gated", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			spec.DesignRootFile:           rootDesignMd(),
			"components/web/design.json":  webappMd("web", "peer"),
			"components/peer/design.json": webappMd("peer"),
		}
		design := readDesign(t, files)
		web := componentNamed(t, design, "web")

		oc := &ocmocks.ComponentClientMock{}
		out, ready := NewRuntimeConfigService(oc, nil, nil).buildEnvValues(ctx, "acme", "proj", web)
		if !ready {
			t.Fatalf("want ready=true; a non-service dep is skipped, not deferred")
		}
		if len(out) != 0 {
			t.Errorf("want empty env map for a webapp with only a peer-webapp dep; got %v", out)
		}
		if len(oc.ListDeploymentsCalls()) != 0 {
			t.Errorf("ListDeployments must not be called for a non-service dep; got %d calls", len(oc.ListDeploymentsCalls()))
		}
	})

	t.Run("resourceClient not wired defers with no partial keys", func(t *testing.T) {
		t.Parallel()
		design := readDesign(t, authWebFiles)
		web := componentNamed(t, design, "web")

		oc := ocResolving(map[string]string{"api": "http://api.local", "web": "http://web.local"})
		cat := &fakeCatalog{markers: authMarkers("thunder-app")}
		out, ready := svcWithCatalog(oc, nil, nil, cat).buildEnvValues(ctx, "acme", "proj", web)
		if ready {
			t.Fatalf("want ready=false when the resource client is unwired")
		}
		if _, ok := out["USER_AUTH_CLIENT_ID"]; ok {
			t.Errorf("no partial output keys on defer; out=%v", out)
		}
	})

	t.Run("catalog UNWIRED defers (fail-open-with-retry, not an error)", func(t *testing.T) {
		t.Parallel()
		design := readDesign(t, authWebFiles)
		web := componentNamed(t, design, "web")

		oc := ocResolving(map[string]string{"api": "http://api.local", "web": "http://web.local"})
		rc := rcOutputs(authOutputs(), nil)
		// No SetResourceCatalog → nil catalog.
		out, ready := NewRuntimeConfigService(oc, rc, nil).buildEnvValues(ctx, "acme", "proj", web)
		if ready {
			t.Fatalf("want ready=false when the catalog is unwired")
		}
		if _, ok := out["USER_AUTH_CLIENT_ID"]; ok {
			t.Errorf("no partial output keys on defer; out=%v", out)
		}
	})

	t.Run("catalog FETCH FAILURE defers (does not surface as an error)", func(t *testing.T) {
		t.Parallel()
		design := readDesign(t, authWebFiles)
		web := componentNamed(t, design, "web")

		oc := ocResolving(map[string]string{"api": "http://api.local", "web": "http://web.local"})
		rc := rcOutputs(authOutputs(), nil)
		cat := &fakeCatalog{err: errors.New("oc: catalog unreachable")}
		out, ready := svcWithCatalog(oc, rc, nil, cat).buildEnvValues(ctx, "acme", "proj", web)
		if ready {
			t.Fatalf("want ready=false when the catalog fetch fails")
		}
		for k := range out {
			if strings.HasPrefix(k, "USER_AUTH_") {
				t.Errorf("no platform-resource output keys on a catalog-fetch defer; got %q", k)
			}
		}
	})

	t.Run("outputs not ready (no status) defers, no partial keys", func(t *testing.T) {
		t.Parallel()
		design := readDesign(t, authWebFiles)
		web := componentNamed(t, design, "web")

		oc := ocResolving(map[string]string{"api": "http://api.local", "web": "http://web.local"})
		rc := rcOutputs(nil, nil) // binding has no status yet
		cat := &fakeCatalog{markers: authMarkers("thunder-app")}
		out, ready := svcWithCatalog(oc, rc, nil, cat).buildEnvValues(ctx, "acme", "proj", web)
		if ready {
			t.Fatalf("want ready=false when the binding outputs are not ready")
		}
		for k := range out {
			if strings.HasPrefix(k, "USER_AUTH_") {
				t.Errorf("no partial output keys on defer; got %q", k)
			}
		}
	})

	t.Run("empty outputs defers", func(t *testing.T) {
		t.Parallel()
		design := readDesign(t, authWebFiles)
		web := componentNamed(t, design, "web")

		oc := ocResolving(map[string]string{"api": "http://api.local", "web": "http://web.local"})
		rc := rcOutputs(map[string]string{}, nil) // status present, zero outputs
		cat := &fakeCatalog{markers: authMarkers("thunder-app")}
		_, ready := svcWithCatalog(oc, rc, nil, cat).buildEnvValues(ctx, "acme", "proj", web)
		if ready {
			t.Fatalf("want ready=false when the binding has zero outputs")
		}
	})

	// The consumer-URL registration is the SOFT half: the dependency's own outputs
	// owe it nothing, so a failed patch must not hold back a file the SPA cannot
	// start without. The converge pass and the converge watcher both retry the
	// registration; nothing retries a SPA nobody handed a client_id to.
	t.Run("consumer-URL patch failure does not gate the outputs", func(t *testing.T) {
		t.Parallel()
		design := readDesign(t, authWebFiles)
		web := componentNamed(t, design, "web")

		oc := ocResolving(map[string]string{"api": "http://api.local", "web": "http://web.local"})
		rc := rcOutputs(authOutputs(), errors.New("oc: patch failed"))
		cat := &fakeCatalog{markers: authMarkers("thunder-app")}
		out, ready := svcWithCatalog(oc, rc, nil, cat).buildEnvValues(ctx, "acme", "proj", web)
		if !ready {
			t.Fatalf("want ready=true; a failed callback registration is not a missing start-up value")
		}
		if out["USER_AUTH_CLIENT_ID"] == nil {
			t.Errorf("the dependency's outputs must still be emitted; out=%v", out)
		}
		if n := len(rc.GetBindingCalls()); n != 1 {
			t.Errorf("the outputs must be read despite the patch failure; GetBinding calls = %d", n)
		}
	})

	// THE BLANK PAGE, pinned. A SPA's own external URL exists only once it has a
	// rendered binding, so demanding it before the SPA's first write is a demand
	// the SPA cannot satisfy until it has already been deployed. Grading that
	// chicken-and-egg as hard withheld window._env_ entirely — the bundle threw at
	// module load and the app served nothing until an out-of-band watcher repaired
	// it up to ten minutes later.
	t.Run("an unresolved SPA URL still emits the keys the SPA starts with", func(t *testing.T) {
		t.Parallel()
		design := readDesign(t, authWebFiles)
		web := componentNamed(t, design, "web")

		// api resolves (its wave went first); the SPA's own URL does not exist yet.
		oc := ocResolving(map[string]string{"api": "http://api.local"})
		rc := rcOutputs(authOutputs(), nil)
		cat := &fakeCatalog{markers: authMarkers("thunder-app")}
		out, ready := svcWithCatalog(oc, rc, nil, cat).buildEnvValues(ctx, "acme", "proj", web)
		if !ready {
			t.Fatalf("want ready=true; the SPA's own URL is not a value the SPA reads")
		}
		if out["USER_AUTH_CLIENT_ID"] == nil {
			t.Errorf("the OIDC client_id the bundle reads is missing; out=%v", out)
		}
		if n := len(rc.PatchBindingEnvironmentConfigsCalls()); n != 0 {
			t.Errorf("nothing can be registered before the SPA URL resolves; got %d patches", n)
		}
	})

	t.Run("GetBinding error defers", func(t *testing.T) {
		t.Parallel()
		design := readDesign(t, authWebFiles)
		web := componentNamed(t, design, "web")

		oc := ocResolving(map[string]string{"api": "http://api.local", "web": "http://web.local"})
		rc := rcOutputs(authOutputs(), nil)
		rc.GetBindingFunc = func(context.Context, string, string) (*openchoreo.ResourceReleaseBinding, error) {
			return nil, errors.New("oc down")
		}
		cat := &fakeCatalog{markers: authMarkers("thunder-app")}
		_, ready := svcWithCatalog(oc, rc, nil, cat).buildEnvValues(ctx, "acme", "proj", web)
		if ready {
			t.Fatalf("want ready=false when GetBinding errors")
		}
	})
}

// --- componentExternalURL ----------------------------------------------------

func Test_componentExternalURL(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("nil componentClient → empty", func(t *testing.T) {
		t.Parallel()
		svc := NewRuntimeConfigService(nil, nil, nil)
		if got := svc.componentExternalURL(ctx, "acme", "proj", "web"); got != "" {
			t.Errorf("want empty when componentClient is nil; got %q", got)
		}
	})

	t.Run("ListDeployments error → empty", func(t *testing.T) {
		t.Parallel()
		oc := &ocmocks.ComponentClientMock{ListDeploymentsFunc: func(context.Context, string, string, string) (*gen.DeploymentList, error) {
			return nil, errors.New("boom")
		}}
		if got := NewRuntimeConfigService(oc, nil, nil).componentExternalURL(ctx, "acme", "proj", "web"); got != "" {
			t.Errorf("want empty on error; got %q", got)
		}
	})

	t.Run("returns first non-empty EndpointURL verbatim", func(t *testing.T) {
		t.Parallel()
		oc := &ocmocks.ComponentClientMock{ListDeploymentsFunc: func(context.Context, string, string, string) (*gen.DeploymentList, error) {
			return &gen.DeploymentList{Items: []gen.Deployment{
				{EndpointURL: ""},                  // skipped
				{EndpointURL: "http://web.local/"}, // returned untrimmed
			}}, nil
		}}
		if got := NewRuntimeConfigService(oc, nil, nil).componentExternalURL(ctx, "acme", "proj", "web"); got != "http://web.local/" {
			t.Errorf("componentExternalURL = %q; want the first non-empty URL verbatim", got)
		}
	})
}

// mustFiles reduces FilesForComponent to its error, for the cases that only
// assert whether it refuses.
func mustFiles(t *testing.T, svc *RuntimeConfigService, ctx context.Context, org, proj, comp string) error {
	t.Helper()
	_, _, err := svc.FilesForComponent(ctx, org, proj, comp)
	return err
}

// --- FilesForComponent -------------------------------------------------------

func Test_FilesForComponent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// webAndAPI: a web-app depending on the `api` service, optionally declaring a
	// `user-auth` platform-resource dep of the given resourceType ("" ⇒ none).
	webAndAPI := func(resourceType string) map[string]string {
		var pr []prDep
		if resourceType != "" {
			pr = []prDep{{"user-auth", resourceType}}
		}
		return map[string]string{
			spec.DesignRootFile:          rootDesignMd(),
			"components/web/design.json": webappWithPR("web", pr, "api"),
			"components/api/design.json": serviceComponentMd(),
		}
	}

	t.Run("happy path emits env-config.js with the full window._env_ shape", func(t *testing.T) {
		t.Parallel()
		oc := ocResolving(map[string]string{
			"api": "http://api.local/todo",
			"web": "http://web.local/",
		})
		rc := rcOutputs(authOutputs(), nil)
		cat := &fakeCatalog{markers: authMarkers("thunder-app")}
		svc := svcWithCatalog(oc, rc, storeWith(webAndAPI("thunder-app")), cat)

		files, ready, err := svc.FilesForComponent(ctx, "acme", "proj", "web")
		if err != nil {
			t.Fatalf("FilesForComponent: %v", err)
		}
		if !ready {
			t.Fatal("want ready with every dependency resolved")
		}
		if len(files) != 1 {
			t.Fatalf("want 1 file composed; got %d", len(files))
		}
		f := files[0]
		if f.Key != "env-config.js" || f.MountPath != "/usr/share/nginx/html/" {
			t.Errorf("file identity drifted: key=%q mountPath=%q", f.Key, f.MountPath)
		}
		for _, want := range []string{
			"window._env_ = {",
			`USER_AUTH_CLIENT_ID: "web-cid"`,
			`USER_AUTH_ISSUER: "http://thunder.local"`,
			`USER_AUTH_JWKS_URL: "http://thunder.local/jwks"`,
			`USER_AUTH_SCOPES: "openid profile email"`,
		} {
			if !strings.Contains(f.Value, want) {
				t.Errorf("emitted env-config.js missing %q\ngot:\n%s", want, f.Value)
			}
		}
		if strings.Contains(f.Value, "API_BASE_URL") || strings.Contains(f.Value, "API_URL:") {
			t.Errorf("sibling API public URLs must not appear in env-config.js:\n%s", f.Value)
		}
		if strings.Contains(f.Value, "THUNDER_") {
			t.Errorf("no legacy THUNDER_* key must appear in env-config.js:\n%s", f.Value)
		}
	})

	t.Run("retired type spellings are not web applications: no emit", func(t *testing.T) {
		// Regression for the vocabulary-drift bug, inverted for the final
		// vocabulary: only the canonical "web-application" (OC's own term)
		// emits. The retired "webapp"/"web-app" spellings pass the codec
		// verbatim and are simply not web applications — designs carrying
		// them must be migrated.
		t.Parallel()
		for _, retired := range []string{"webapp", "web-app"} {
			files := map[string]string{
				spec.DesignRootFile:          rootDesignMd(),
				"components/web/design.json": buildComponentJSON("web", retired, []string{"api"}, nil),
				"components/api/design.json": serviceComponentMd(),
			}
			oc := ocResolving(map[string]string{"api": "http://api.local/todo"})
			svc := NewRuntimeConfigService(oc, nil, storeWith(files))

			got, ready, err := svc.FilesForComponent(ctx, "acme", "proj", "web")
			if err != nil {
				t.Fatalf("FilesForComponent(%q): %v", retired, err)
			}
			if !ready || len(got) != 0 {
				t.Fatalf("retired spelling %q must compose no env-config.js; got %d files (ready=%v)", retired, len(got), ready)
			}
		}
	})

	t.Run("unresolved sibling service dep still writes env-config.js", func(t *testing.T) {
		t.Parallel()
		oc := ocResolving(map[string]string{})
		svc := NewRuntimeConfigService(oc, nil, storeWith(webAndAPI("")))

		files, ready, err := svc.FilesForComponent(ctx, "acme", "proj", "web")
		if err != nil {
			t.Fatalf("FilesForComponent: %v", err)
		}
		if !ready {
			t.Fatalf("want ready=true when only a sibling service dep is unresolved; got false")
		}
		if len(files) != 1 {
			t.Fatalf("want 1 file when only a sibling service dep is unresolved; got %d", len(files))
		}
		val := files[0].Value
		if !strings.Contains(val, "window._env_ = {") {
			t.Errorf("want an env-config.js write; got %q", val)
		}
		if strings.Contains(val, "API_BASE_URL") {
			t.Errorf("API_BASE_URL must not appear:\n%s", val)
		}
	})

	t.Run("not-ready (auth outputs unresolved) defers the write", func(t *testing.T) {
		t.Parallel()
		oc := ocResolving(map[string]string{"api": "http://api.local", "web": "http://web.local"})
		rc := rcOutputs(nil, nil) // binding has no status yet
		cat := &fakeCatalog{markers: authMarkers("thunder-app")}
		svc := svcWithCatalog(oc, rc, storeWith(webAndAPI("thunder-app")), cat)

		files, ready, err := svc.FilesForComponent(ctx, "acme", "proj", "web")
		if err != nil {
			t.Fatalf("FilesForComponent: %v", err)
		}
		if ready || len(files) != 0 {
			t.Errorf("want ready=false and no files; got ready=%v files=%d", ready, len(files))
		}
	})

	t.Run("non-web-app component is a no-op", func(t *testing.T) {
		t.Parallel()
		oc := &ocmocks.ComponentClientMock{}
		svc := NewRuntimeConfigService(oc, nil, storeWith(webAndAPI("")))

		if _, _, err := svc.FilesForComponent(ctx, "acme", "proj", "api"); err != nil {
			t.Fatalf("FilesForComponent on a service should be nil; got %v", err)
		}
		if len(oc.ListDeploymentsCalls()) != 0 {
			t.Errorf("no OC reads expected for a non-web-app; got %d", len(oc.ListDeploymentsCalls()))
		}
	})

	t.Run("unknown component name is a no-op", func(t *testing.T) {
		t.Parallel()
		oc := &ocmocks.ComponentClientMock{}
		svc := NewRuntimeConfigService(oc, nil, storeWith(webAndAPI("")))
		if _, _, err := svc.FilesForComponent(ctx, "acme", "proj", "ghost"); err != nil {
			t.Fatalf("FilesForComponent on a missing component should be nil; got %v", err)
		}
		if len(oc.ListDeploymentsCalls()) != 0 {
			t.Errorf("no OC reads expected; got %d", len(oc.ListDeploymentsCalls()))
		}
	})

	t.Run("design absent (empty tree) is a no-op", func(t *testing.T) {
		t.Parallel()
		oc := &ocmocks.ComponentClientMock{}
		svc := NewRuntimeConfigService(oc, nil, storeWith(map[string]string{}))
		if err := mustFiles(t, svc, ctx, "acme", "proj", "web"); err != nil {
			t.Fatalf("want nil when there is no design yet; got %v", err)
		}
	})

	t.Run("design read not-found is swallowed", func(t *testing.T) {
		t.Parallel()
		store := spec.NewArtifactStore(&artifactstest.FakeArtifactService{
			ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
				return nil, spec.ErrArtifactNotFound
			},
		})
		oc := &ocmocks.ComponentClientMock{}
		svc := NewRuntimeConfigService(oc, nil, store)
		if err := mustFiles(t, svc, ctx, "acme", "proj", "web"); err != nil {
			t.Fatalf("ErrArtifactNotFound must be swallowed; got %v", err)
		}
	})

	t.Run("empty identifiers error", func(t *testing.T) {
		t.Parallel()
		svc := NewRuntimeConfigService(&ocmocks.ComponentClientMock{}, nil, storeWith(webAndAPI("")))
		for _, tc := range []struct{ org, proj, comp string }{
			{"", "proj", "web"},
			{"acme", "", "web"},
			{"acme", "proj", ""},
		} {
			if err := mustFiles(t, svc, ctx, tc.org, tc.proj, tc.comp); err == nil {
				t.Errorf("FilesForComponent(%q,%q,%q) should error on empty identifier", tc.org, tc.proj, tc.comp)
			}
		}
	})


	t.Run("nil service receiver is a no-op", func(t *testing.T) {
		t.Parallel()
		var svc *RuntimeConfigService
		if err := mustFiles(t, svc, ctx, "acme", "proj", "web"); err != nil {
			t.Errorf("nil receiver EmitForComponent should be nil; got %v", err)
		}
	})
}

