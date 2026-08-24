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

// COMPONENT tier: the REAL provisioning service
// behind the REAL production handler chain — faked auth → contract validation
// → the deny-by-default tenant gate in ENFORCE → the strict handlers
// (handlers_provisioning.go) and their mapProvisionError dialect — driven
// in-process via the componenttest harness with the service's out-of-process
// ports faked. The provisioning behavior itself is unit-proven in
// provisioning_test.go; this tier pins the HTTP contract: routes, status
// codes, the flat error envelope, and the no-claims 401.
//
// External test package: the harness imports api, which imports provisioning —
// an in-package test file would be an import cycle. (The in-package fakes in
// provisioning_test.go are not visible here; this file keeps its own minimal
// set.)
package provisioning_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	dephttpapi "github.com/wso2/aep/aep-api/internal/dependencies/httpapi"
	"github.com/wso2/aep/aep-api/internal/dependencies/provisioning"
	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// ----- minimal port fakes ------------------------------------------------------

// cRTCatalog fakes provisioning.ExternalRTCatalog — the OC-RT-backed
// org-settings list+delete surface (Task 5) — with RT fixtures in place of DB
// rows.
type cRTCatalog struct {
	defs    []openchoreo.ExternalResourceDefinition
	deleted []string
}

func (f *cRTCatalog) List(_ context.Context, _ string) ([]openchoreo.ExternalResourceDefinition, error) {
	return f.defs, nil
}

func (f *cRTCatalog) Delete(_ context.Context, _, name string) error {
	f.deleted = append(f.deleted, name)
	return nil
}

type cDesign struct{ comps []spec.DesignComponent }

func (f cDesign) ReadDesignComponents(context.Context, string, string) ([]spec.DesignComponent, error) {
	return f.comps, nil
}

type cProjects struct{ refs []provisioning.ProjectRef }

func (f cProjects) ListProjects(_ context.Context, orgID string) ([]provisioning.ProjectRef, error) {
	var out []provisioning.ProjectRef
	for _, r := range f.refs {
		if r.OrgID == orgID {
			out = append(out, r)
		}
	}
	return out, nil
}

type cBindings struct {
	byName map[string]*openchoreo.ResourceReleaseBinding
}

func (f *cBindings) GetBinding(_ context.Context, _, name string) (*openchoreo.ResourceReleaseBinding, error) {
	return f.byName[name], nil
}

type cIssues struct{}

func (cIssues) ListIssues(context.Context, string, string, []string) ([]sourcecontrol.IssueInfo, error) {
	return nil, nil
}
func (cIssues) CreateIssue(context.Context, string, string, sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error) {
	return &sourcecontrol.IssueResult{Number: 1}, nil
}
func (cIssues) CloseIssue(context.Context, string, string, int, string) error   { return nil }
func (cIssues) CommentIssue(context.Context, string, string, int, string) error { return nil }
func (cIssues) AddLabels(context.Context, string, string, int, []string) error  { return nil }

type cRepos struct{}

func (cRepos) RepoFullName(context.Context, string, string) (string, error) { return "acme/shop", nil }
func (cRepos) ByFullName(context.Context, string) (string, string, error) {
	return "acme", "shop", nil
}

func readyBindingWith(outputs ...string) *openchoreo.ResourceReleaseBinding {
	st := &openchoreo.ResourceReleaseBindingStatus{
		Conditions: []openchoreo.OCCondition{{Type: "Ready", Status: "True"}},
	}
	for _, o := range outputs {
		st.Outputs = append(st.Outputs, openchoreo.ResolvedOutput{Name: o})
	}
	return &openchoreo.ResourceReleaseBinding{Status: st}
}

// stripeConsumerDesign declares an external dep "stripe" on project "proj"'s
// component "orders" — the consumer the catalog list/delete guard scans for.
func stripeConsumerDesign() []spec.DesignComponent {
	return []spec.DesignComponent{{
		Name: "orders",
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "stripe", Config: []spec.ConfigKey{
				{Key: "api_key", Secret: true}, {Key: "region"},
			}},
		},
	}}
}

func newProvHarness(t *testing.T, svc *provisioning.Service) *componenttest.Harness {
	t.Helper()
	deps, err := dephttpapi.New(dephttpapi.Deps{ProvisioningSvc: svc})
	if err != nil {
		t.Fatalf("assemble dependencies domain: %v", err)
	}
	return componenttest.New(t, componenttest.Options{Deps: edge.Deps{Dependencies: deps}})
}

// ----- tests --------------------------------------------------------------------

// A nil service keeps every provisioning route present but unwired — 503 with
// the flat envelope, mirroring the retired RegisterResources nil guard.
func TestProvisioningComponent_Unconfigured503(t *testing.T) {
	t.Parallel()
	h := componenttest.New(t, componenttest.Options{Deps: edge.Deps{}})

	resp := h.AsOrg("acme").Get("/api/v1/dependencies/external-resources")
	if resp.Code != 503 {
		t.Fatalf("want 503, got %d body=%s", resp.Code, resp.Body.String())
	}
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if e.Code != "service_unavailable" || e.Message != "provisioning is not configured" {
		t.Fatalf("503 envelope = %+v", e)
	}
}

func TestProvisioningComponent_NoClaims401(t *testing.T) {
	t.Parallel()
	h := newProvHarness(t, provisioning.NewService(provisioning.Deps{}))

	if resp := h.NoAuth().Get("/api/v1/dependencies/external-resources"); resp.Code != 401 {
		t.Fatalf("claimless: want the gate's ENFORCE 401, got %d body=%s", resp.Code, resp.Body.String())
	}
}

// list-external-resources: the catalog entry rides the wire with its config
// schema (never values) and its design-scanned consumers.
func TestProvisioningComponent_ListExternalResources(t *testing.T) {
	t.Parallel()
	svc := provisioning.NewService(provisioning.Deps{
		RTCatalog: &cRTCatalog{defs: []openchoreo.ExternalResourceDefinition{
			{Name: "stripe", Description: "payments", Config: []openchoreo.ExternalResourceConfigKey{
				{Key: "api_key", Secret: true}, {Key: "region"},
			}},
		}},
		Design:   cDesign{comps: stripeConsumerDesign()},
		Projects: cProjects{refs: []provisioning.ProjectRef{{OrgID: "acme", ProjectID: "proj"}}},
	})
	h := newProvHarness(t, svc)

	resp := h.AsOrg("acme").Get("/api/v1/dependencies/external-resources")
	if resp.Code != 200 {
		t.Fatalf("list: got %d body=%s", resp.Code, resp.Body.String())
	}
	var got []gen.ExternalResourceDTO
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v\n%s", err, resp.Body.String())
	}
	if len(got) != 1 || got[0].Name != "stripe" || got[0].Description != "payments" {
		t.Fatalf("resources = %+v, want the stripe entry", got)
	}
	if len(got[0].Config) != 2 || got[0].Config[0].Key != "api_key" || !got[0].Config[0].Secret {
		t.Errorf("config schema = %+v", got[0].Config)
	}
	if len(got[0].Consumers) != 1 || got[0].Consumers[0].ProjectID != "proj" || got[0].Consumers[0].ComponentName != "orders" {
		t.Errorf("consumers = %+v, want the design-scanned orders consumer", got[0].Consumers)
	}
}

// delete-external-resource: in use → 409 conflict envelope, nothing deleted;
// unused → 204 and the catalog entry is gone.
func TestProvisioningComponent_DeleteExternalResource(t *testing.T) {
	t.Parallel()
	rtCatalog := &cRTCatalog{}
	svc := provisioning.NewService(provisioning.Deps{
		RTCatalog: rtCatalog,
		Design:    cDesign{comps: stripeConsumerDesign()},
		Projects:  cProjects{refs: []provisioning.ProjectRef{{OrgID: "acme", ProjectID: "proj"}}},
	})
	h := newProvHarness(t, svc)

	resp := h.AsOrg("acme").Delete("/api/v1/dependencies/external-resources/stripe")
	if resp.Code != 409 {
		t.Fatalf("in-use delete: want 409, got %d body=%s", resp.Code, resp.Body.String())
	}
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if e.Code != "conflict" || !strings.Contains(e.Message, "in use") {
		t.Fatalf("409 envelope = %+v", e)
	}
	if len(rtCatalog.deleted) != 0 {
		t.Fatalf("an in-use resource must not delete any ResourceType")
	}

	resp = h.AsOrg("acme").Delete("/api/v1/dependencies/external-resources/unused")
	if resp.Code != 204 {
		t.Fatalf("unused delete: want 204, got %d body=%s", resp.Code, resp.Body.String())
	}
	if len(rtCatalog.deleted) != 1 || rtCatalog.deleted[0] != "unused" {
		t.Fatalf("unused resource's ResourceType must be deleted, got %v", rtCatalog.deleted)
	}
}

// get-dependency-status: a ready binding reports ready with outputs masked to
// names (the default environment applies when the query param is absent).
func TestProvisioningComponent_DependencyStatus(t *testing.T) {
	t.Parallel()
	svc := provisioning.NewService(provisioning.Deps{
		Bindings: &cBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{
			"proj-orders-db-development": readyBindingWith("host", "port"),
		}},
	})
	h := newProvHarness(t, svc)

	resp := h.AsOrg("acme").Get("/api/v1/projects/proj/components/orders/dependencies/orders-db/status")
	if resp.Code != 200 {
		t.Fatalf("status: got %d body=%s", resp.Code, resp.Body.String())
	}
	var st gen.DependencyStatus
	if err := json.Unmarshal(resp.Body.Bytes(), &st); err != nil {
		t.Fatalf("body: %v\n%s", err, resp.Body.String())
	}
	if !st.Ready || st.Status != "ready" {
		t.Fatalf("want ready, got %+v", st)
	}
	if len(st.Outputs) != 2 || st.Outputs[0] != "host" {
		t.Fatalf("outputs must be the masked names, got %+v", st.Outputs)
	}
}

func TestProvisioningComponent_ProjectDependencyReadiness(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(map[string]string{"region": "", openchoreo.SecretStorePathField: ""})
	if err != nil {
		t.Fatal(err)
	}
	svc := provisioning.NewService(provisioning.Deps{
		Design: cDesign{comps: stripeConsumerDesign()},
		Bindings: &cBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{
			"proj-stripe-development": {Spec: openchoreo.ResourceReleaseBindingSpec{ResourceTypeEnvironmentConfigs: raw}},
		}},
	})
	h := newProvHarness(t, svc)

	resp := h.AsOrg("acme").Get("/api/v1/projects/proj/dependencies/readiness?environment=development")
	if resp.Code != 200 {
		t.Fatalf("readiness: got %d body=%s", resp.Code, resp.Body.String())
	}
	var got gen.ProjectDependencyReadiness
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v", err)
	}
	if got.Configured || len(got.Dependencies) != 1 || got.Dependencies[0].State != "unset" {
		t.Fatalf("readiness = %+v", got)
	}
	if len(got.Dependencies[0].MissingKeys) != 2 {
		t.Fatalf("missing keys = %v", got.Dependencies[0].MissingKeys)
	}

	status := h.AsOrg("acme").Get("/api/v1/projects/proj/components/orders/dependencies/stripe/status?environment=development")
	if status.Code != 200 {
		t.Fatalf("status: got %d body=%s", status.Code, status.Body.String())
	}
	var dependencyStatus gen.DependencyStatus
	if err := json.Unmarshal(status.Body.Bytes(), &dependencyStatus); err != nil {
		t.Fatalf("status body: %v", err)
	}
	if dependencyStatus.ValueState != "unset" {
		t.Fatalf("dependency valueState = %q, want unset", dependencyStatus.ValueState)
	}
}

// provision-platform-resource on an external dependency is wrong-kind → 400
// bad_request carrying the sentinel text.
func TestProvisioningComponent_ProvisionWrongKind400(t *testing.T) {
	t.Parallel()
	svc := provisioning.NewService(provisioning.Deps{
		Issues: cIssues{},
		Repos:  cRepos{},
		Design: cDesign{comps: stripeConsumerDesign()},
	})
	h := newProvHarness(t, svc)

	resp := h.AsOrg("acme").Post("/api/v1/projects/proj/components/orders/dependencies/stripe/provision", "")
	if resp.Code != 400 {
		t.Fatalf("wrong-kind provision: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if e.Code != "bad_request" || !strings.Contains(e.Message, "dependency kind does not support this action") {
		t.Fatalf("400 envelope = %+v", e)
	}
}

// request-org-service-access with no resolvable provider → 404 not_found; the
// consumer project's request list stays an empty (non-null) array.
func TestProvisioningComponent_AccessRequests(t *testing.T) {
	t.Parallel()
	svc := provisioning.NewService(provisioning.Deps{
		Design: cDesign{comps: stripeConsumerDesign()},
	})
	h := newProvHarness(t, svc)

	resp := h.AsOrg("acme").Post("/api/v1/projects/proj/components/web/dependencies/inventory/access-request", "")
	if resp.Code != 404 {
		t.Fatalf("unresolvable provider: want 404, got %d body=%s", resp.Code, resp.Body.String())
	}
	if e := componenttest.DecodeEnvelope(t, resp.Body.String()); e.Code != "not_found" {
		t.Fatalf("404 envelope = %+v", e)
	}

	resp = h.AsOrg("acme").Get("/api/v1/projects/proj/dependencies/access-requests")
	if resp.Code != 200 {
		t.Fatalf("list access requests: got %d body=%s", resp.Code, resp.Body.String())
	}
	if body := strings.TrimSpace(resp.Body.String()); body != "[]" {
		t.Fatalf("empty list must serialize as [], got %s", body)
	}
}
