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

// COMPONENT tier (bff-component-testing.md §4): the REAL designService behind
// the REAL production handler chain — faked auth → orgensure → contract
// validation → the deny-by-default tenant gate in ENFORCE → the strict handler
// (handlers_design.go) — driven in-process via the componenttest harness, with
// the design tree behind a real ArtifactStore (the artifact service + the
// org-service/external-resource resolver ports are the only faked seams).
//
// This tier pins the list-design-dependencies HTTP contract: every dependency
// kind's read-time computed status/reason rides the wire alongside its stored
// intent fields (ComputeDependencyStatus's precedence table is unit-proven in
// models/dependency_status_test.go and the resolver-wiring fail-open behavior
// in artifacts/resolve_external_dependencies_test.go; this tier proves the
// HTTP surface projects what ReadDesign already computed, unchanged).
//
// External test package: the harness imports api, which imports design — an
// in-package test file would be an import cycle.
package spec_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
	"github.com/wso2/aep/aep-api/internal/spec"
	"github.com/wso2/aep/aep-api/internal/spec/artifactstest"
)

const depsPath = "/api/v1/projects/web/design/dependencies"

// staticOrgResolver is a minimal spec.OrgServiceResolver: `visible` names
// are namespace-visible (→ resolved); everything else is absent (→
// unresolved/not-found).
type staticOrgResolver struct{ visible map[string]bool }

func (r staticOrgResolver) IsNamespaceVisible(_ context.Context, _, name string) (bool, error) {
	return r.visible[name], nil
}
func (r staticOrgResolver) ExistsAnyVisibility(_ context.Context, _, name string) (bool, error) {
	return r.visible[name], nil
}

// newDesignHarness assembles the real designService (wrapping the REAL
// ArtifactStore over the given fake artifact service, with the org-service
// resolver port wired) behind the real handler chain. External-resource status
// derives from stored intent here — no ExternalResourceResolver is wired, so
// rule 2 (registry reuse) does not fire.
func newDesignHarness(t *testing.T, files map[string]string, orgVisible map[string]bool) *componenttest.Harness {
	t.Helper()
	fake := &artifactstest.FakeArtifactService{
		ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return files, nil
		},
	}
	store := spec.NewArtifactStore(fake)
	store.SetOrgServiceResolver(staticOrgResolver{visible: orgVisible})
	svc := spec.NewDesignService(store, fake)
	return componenttest.New(t, componenttest.Options{Deps: edge.Deps{DesignSvc: svc}})
}

// mixedDependencyDesignFiles is a design tree for one "checkout" service
// component whose dependencies span every kind + every external precedence
// outcome relevant to the read-time status computation.
func mixedDependencyDesignFiles() map[string]string {
	return map[string]string{
		spec.DesignRootFile: "Overview.\n",
		"components/checkout/design.json": `{
  "name": "checkout",
  "type": "service",
  "dependencies": [
    {"kind": "org-service", "name": "billing"},
    {"kind": "external", "name": "stripe", "style": "rest-api", "specPath": "dependencies/stripe.openapi.yaml"},
    {"kind": "external", "name": "sendgrid"},
    {"kind": "external", "name": "salesforce", "style": "rest-api", "specPath": "dependencies/salesforce.openapi.yaml"},
    {"kind": "platform-resource", "name": "orders-db", "resourceType": "postgres-cnpg"}
  ]
}
`,
	}
}

// --- unconfigured / auth -------------------------------------------------------

func TestDesignComponent_Unconfigured503(t *testing.T) {
	t.Parallel()
	h := componenttest.New(t, componenttest.Options{Deps: edge.Deps{}})
	resp := h.AsOrg("acme").Get(depsPath)
	if resp.Code != 503 {
		t.Fatalf("want 503, got %d body=%s", resp.Code, resp.Body.String())
	}
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if e.Code != "service_unavailable" || e.Message != "design service is not configured" {
		t.Fatalf("503 envelope = %+v", e)
	}
}

func TestDesignComponent_NoClaims401(t *testing.T) {
	t.Parallel()
	h := newDesignHarness(t, mixedDependencyDesignFiles(), nil)
	if resp := h.NoAuth().Get(depsPath); resp.Code != 401 {
		t.Fatalf("claimless: want the gate's ENFORCE 401, got %d body=%s", resp.Code, resp.Body.String())
	}
}

// --- list-design-dependencies ---------------------------------------------------

// TestDesignComponent_ListDependencies_ComputesStatusPerKind proves the HTTP
// contract: every dependency kind's read-time status/reason (already computed
// by ReadDesign → AssembleDesignFrom before the handler ever runs) plus its
// stored intent fields ride the wire.
func TestDesignComponent_ListDependencies_ComputesStatusPerKind(t *testing.T) {
	t.Parallel()
	h := newDesignHarness(t, mixedDependencyDesignFiles(),
		map[string]bool{"billing": true}, // org-service: namespace-visible → resolved
	)

	resp := h.AsOrg("acme").Get(depsPath)
	if resp.Code != 200 {
		t.Fatalf("list: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	var got []gen.ComponentDependencies
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v\n%s", err, resp.Body.String())
	}
	if len(got) != 1 || got[0].ComponentName != "checkout" {
		t.Fatalf("components = %+v, want one entry for checkout", got)
	}
	deps := got[0].Dependencies
	if len(deps) != 5 {
		t.Fatalf("dependencies = %+v, want 5", deps)
	}

	byName := map[string]spec.Dependency{}
	for _, d := range deps {
		byName[d.Name] = d
	}

	if d := byName["billing"]; d.Status != spec.DependencyStatusResolved {
		t.Errorf("billing (org-service, visible): status = %q, want %q", d.Status, spec.DependencyStatusResolved)
	}
	if d := byName["stripe"]; d.Status != spec.DependencyStatusResolved || d.SpecPath != "dependencies/stripe.openapi.yaml" {
		t.Errorf("stripe (external, rest-api + specPath): status=%q specPath=%q, want %q/dependencies/stripe.openapi.yaml",
			d.Status, d.SpecPath, spec.DependencyStatusResolved)
	}
	if d := byName["sendgrid"]; d.Status != spec.DependencyStatusUnresolved || d.Reason != spec.DependencyReasonNeedsInput {
		t.Errorf("sendgrid (external, no style): status/reason = %q/%q, want %q/%q",
			d.Status, d.Reason, spec.DependencyStatusUnresolved, spec.DependencyReasonNeedsInput)
	}
	if d := byName["salesforce"]; d.Status != spec.DependencyStatusResolved || d.SpecPath != "dependencies/salesforce.openapi.yaml" {
		t.Errorf("salesforce (external, rest-api + specPath): status=%q specPath=%q, want %q/dependencies/salesforce.openapi.yaml",
			d.Status, d.SpecPath, spec.DependencyStatusResolved)
	}
	if d := byName["orders-db"]; d.Status != spec.DependencyStatusResolved || d.ResourceType != "postgres-cnpg" {
		t.Errorf("orders-db (platform-resource): status=%q resourceType=%q, want %q/postgres-cnpg",
			d.Status, d.ResourceType, spec.DependencyStatusResolved)
	}
}

// TestDesignComponent_ListDependencies_NoDesignIs404 asserts an absent design
// (no design.cell at all) surfaces as 404, not an empty list or a 500.
func TestDesignComponent_ListDependencies_NoDesignIs404(t *testing.T) {
	t.Parallel()
	h := newDesignHarness(t, map[string]string{}, nil)
	resp := h.AsOrg("acme").Get(depsPath)
	if resp.Code != 404 {
		t.Fatalf("no design: want 404, got %d body=%s", resp.Code, resp.Body.String())
	}
	if e := componenttest.DecodeEnvelope(t, resp.Body.String()); e.Message != "design not found" {
		t.Fatalf("404 message: got %q", e.Message)
	}
}
