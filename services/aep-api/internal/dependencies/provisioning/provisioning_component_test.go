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
	"errors"
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

// cWorkloads fakes provisioning.WorkloadDepSource — deployed Workload consumer
// refs plus GetResource / GetResourceType, in place of live OC.
type cWorkloads struct {
	deps      []openchoreo.WorkloadConsumerDep
	resources map[string]*openchoreo.Resource
	types     map[string]*openchoreo.ResourceType
	listErr   error
}

func (f *cWorkloads) ListWorkloadConsumerDeps(_ context.Context, _, projectName string) ([]openchoreo.WorkloadConsumerDep, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	// Mirror ResourceClient: org-scoped list, keep spec.owner.projectName == path.
	out := make([]openchoreo.WorkloadConsumerDep, 0)
	for _, d := range f.deps {
		if d.OwnerProject == projectName {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *cWorkloads) GetResource(_ context.Context, _, name string) (*openchoreo.Resource, error) {
	if r, ok := f.resources[name]; ok {
		return r, nil
	}
	return nil, openchoreo.ErrNotFound
}

func (f *cWorkloads) GetResourceType(_ context.Context, _, name string) (*openchoreo.ResourceType, error) {
	if rt, ok := f.types[name]; ok {
		return rt, nil
	}
	return nil, openchoreo.ErrNotFound
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

// designWithUnconsumedGhost is design.json with a unique unresolved name that
// no deployed Workload carries. Used to pin that workload-dependencies does
// not consult DesignReader.
func designWithUnconsumedGhost() []spec.DesignComponent {
	return []spec.DesignComponent{{
		Name: "orders",
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "stripe", Config: []spec.ConfigKey{
				{Key: "api_key", Secret: true}, {Key: "region"},
			}},
			{Kind: spec.DependencyKindExternal, Name: "ghost"},
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

	resp = h.AsOrg("acme").Get("/api/v1/projects/shop/workload-dependencies")
	if resp.Code != 503 {
		t.Fatalf("workload-dependencies nil svc: want 503, got %d body=%s", resp.Code, resp.Body.String())
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

const workloadDepsPath = "/api/v1/projects/shop/workload-dependencies"

func TestProvisioningComponent_ListWorkloadDependencies_NoClaims401(t *testing.T) {
	t.Parallel()
	h := newProvHarness(t, provisioning.NewService(provisioning.Deps{}))

	if resp := h.NoAuth().Get(workloadDepsPath); resp.Code != 401 {
		t.Fatalf("claimless: want the gate's ENFORCE 401, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestProvisioningComponent_ListWorkloadDependencies_Empty200(t *testing.T) {
	t.Parallel()
	// Org has deployed workloads, but none owned by path project "shop".
	// A nil Workloads port is a lister failure, not 200 []; this fake must
	// list-and-filter so other projects' rows cannot leak.
	h := newProvHarness(t, provisioning.NewService(provisioning.Deps{
		Workloads: otherProjectWorkloadSource(),
	}))

	resp := h.AsOrg("acme").Get(workloadDepsPath)
	if resp.Code != 200 {
		t.Fatalf("no deployed workloads: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if body := strings.TrimSpace(resp.Body.String()); body != "[]" {
		t.Fatalf("empty list must serialize as [], got %s", body)
	}
}

func TestProvisioningComponent_ListWorkloadDependencies_UnknownProject200Empty(t *testing.T) {
	t.Parallel()
	// Shop workloads exist in the org. GET a name that was never created must
	// still be 200 []: listing is org-scoped then filtered by path projectName
	// (sibling policy to components when OC does not 404). Wiring the shop
	// source pins that the path name is passed through — an unfiltered fake
	// would return shop rows.
	h := newProvHarness(t, provisioning.NewService(provisioning.Deps{
		Workloads: shopWorkloadSource(),
	}))

	// Listing org workloads filtered by owner.projectName naturally yields []
	// when the project has no deployed workloads — including a name that was
	// never created. GET /projects/{name} 404s because it is a point-get;
	// ListComponents 404s only when OC's project-scoped list 404s. This list
	// is org-scoped, so unknown project is 200 [].
	resp := h.AsOrg("acme").Get("/api/v1/projects/does-not-exist-xyz/workload-dependencies")
	if resp.Code != 200 {
		t.Fatalf("unknown project: want 200 [], got %d body=%s", resp.Code, resp.Body.String())
	}
	if body := strings.TrimSpace(resp.Body.String()); body != "[]" {
		t.Fatalf("unknown project must serialize as [], got %s", body)
	}
}

// otherProjectWorkloadSource is deployed Workloads owned by "other", not "shop".
// GET /projects/shop must list-and-filter to [] — if the fake ignored
// projectName these rows would leak (platform postgres + org-service).
func otherProjectWorkloadSource() *cWorkloads {
	return &cWorkloads{
		deps: []openchoreo.WorkloadConsumerDep{
			{
				OwnerProject:   "other",
				OwnerComponent: "api",
				ResourceRefs:   []string{"other-pg"},
				Endpoints: []openchoreo.WorkloadConsumerEndpoint{
					{Project: "inventory", Component: "inventory-api", Name: "http", Visibility: "namespace"},
				},
			},
		},
		resources: map[string]*openchoreo.Resource{
			"other-pg": {Spec: openchoreo.ResourceSpec{
				Type: openchoreo.ResourceTypeRef{Kind: "ClusterResourceType", Name: "postgres-cnpg"},
			}},
		},
	}
}

func shopWorkloadSource() *cWorkloads {
	return &cWorkloads{
		deps: []openchoreo.WorkloadConsumerDep{
			{
				OwnerProject:   "shop",
				OwnerComponent: "orders",
				ResourceRefs:   []string{"shop-pg", "shop-stripe", "shop-gone"},
				Endpoints: []openchoreo.WorkloadConsumerEndpoint{
					{Project: "inventory", Component: "inventory-api", Name: "http", Visibility: "namespace"},
					{Project: "shop", Component: "web", Name: "http", Visibility: "project"},
				},
			},
			{
				OwnerProject:   "shop",
				OwnerComponent: "billing",
				ResourceRefs:   []string{"shop-pg"},
				Endpoints: []openchoreo.WorkloadConsumerEndpoint{
					{Project: "inventory", Component: "inventory-api", Name: "grpc", Visibility: "namespace"},
				},
			},
		},
		resources: map[string]*openchoreo.Resource{
			"shop-pg": {Spec: openchoreo.ResourceSpec{
				Type: openchoreo.ResourceTypeRef{Kind: "ClusterResourceType", Name: "postgres-cnpg"},
			}},
			"shop-stripe": {Spec: openchoreo.ResourceSpec{
				Type: openchoreo.ResourceTypeRef{Kind: "ResourceType", Name: "stripe-rt"},
			}},
		},
		types: map[string]*openchoreo.ResourceType{
			"stripe-rt": {Metadata: openchoreo.OCObjectMeta{
				Name:        "stripe-rt",
				Annotations: map[string]string{"aep.wso2.com/external-name": "stripe"},
			}},
		},
	}
}

func TestProvisioningComponent_ListWorkloadDependencies_ResourceAndOrgServiceRows(t *testing.T) {
	t.Parallel()
	svc := provisioning.NewService(provisioning.Deps{
		Workloads: shopWorkloadSource(),
		// Design declares an extra unresolved name ("ghost") that no Workload
		// consumes — it must not appear (source is deployed Workloads, not
		// design.json). "stripe" is already a Workload consumer, so it cannot
		// pin DesignReader absence: a duplicate would collapse under dedup.
		Design: cDesign{comps: designWithUnconsumedGhost()},
	})
	h := newProvHarness(t, svc)

	resp := h.AsOrg("acme").Get(workloadDepsPath)
	if resp.Code != 200 {
		t.Fatalf("list: got %d body=%s", resp.Code, resp.Body.String())
	}

	var got []gen.WorkloadDependencyDTO
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v\n%s", err, resp.Body.String())
	}

	// Not the design-dependencies wrapper (componentName + dependencies[]).
	var wrapper []map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &wrapper); err != nil {
		t.Fatalf("body objects: %v", err)
	}
	for _, item := range wrapper {
		if _, ok := item["componentName"]; ok {
			t.Fatalf("payload must not be design-dependencies shape, got %s", resp.Body.String())
		}
		if _, ok := item["dependencies"]; ok {
			t.Fatalf("payload must not wrap dependencies[], got %s", resp.Body.String())
		}
		if _, ok := item["kind"]; !ok {
			t.Fatalf("items must have kind, got %s", resp.Body.String())
		}
	}

	if len(got) != 3 {
		t.Fatalf("rows = %+v, want 3 (platform + external + org-service, deduped; dangling omitted)", got)
	}

	var platform, external, orgSvc *gen.WorkloadDependencyDTO
	for i := range got {
		row := &got[i]
		switch {
		case row.Kind == gen.Resource && row.Tag == gen.Platform:
			platform = row
		case row.Kind == gen.Resource && row.Tag == gen.External:
			external = row
		case row.Kind == gen.OrgService:
			orgSvc = row
		}
	}
	if platform == nil || platform.Ref != "postgres-cnpg" || platform.Name != "postgres-cnpg" {
		t.Fatalf("platform resource row = %+v, want ref/name postgres-cnpg", platform)
	}
	if external == nil || external.Ref != "stripe" || external.Name != "stripe" {
		t.Fatalf("external resource row = %+v, want ref/name stripe (logical catalog name)", external)
	}
	if orgSvc == nil || orgSvc.Project != "inventory" || orgSvc.Component != "inventory-api" {
		t.Fatalf("org-service row = %+v, want provider inventory/inventory-api", orgSvc)
	}
	if orgSvc.Name == "" {
		t.Fatalf("org-service row must carry a name, got %+v", orgSvc)
	}
	for _, row := range got {
		if row.Kind == gen.OrgService && row.Project == "shop" {
			t.Fatalf("same-project visibility:project endpoint must be omitted, got %+v", row)
		}
		if row.Name == "ghost" || row.Ref == "ghost" || row.Name == "orders" && row.Kind != gen.OrgService {
			t.Fatalf("design-only dep must not appear: %+v", row)
		}
	}
}

func TestProvisioningComponent_ListWorkloadDependencies_ListerFailure500(t *testing.T) {
	t.Parallel()
	svc := provisioning.NewService(provisioning.Deps{
		Workloads: &cWorkloads{listErr: errors.New("oc workloads down")},
	})
	h := newProvHarness(t, svc)

	resp := h.AsOrg("acme").Get(workloadDepsPath)
	if resp.Code != 500 {
		t.Fatalf("lister failure: want 500, got %d body=%s", resp.Code, resp.Body.String())
	}
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if e.Code != "internal_error" {
		t.Fatalf("500 envelope = %+v, want code internal_error", e)
	}
}

func TestProvisioningComponent_ListWorkloadDependencies_NilWorkloadsPort500(t *testing.T) {
	t.Parallel()
	// Non-nil service, unwired Workloads port: not "nothing deployed" (200 []).
	h := newProvHarness(t, provisioning.NewService(provisioning.Deps{}))

	resp := h.AsOrg("acme").Get(workloadDepsPath)
	if resp.Code != 500 {
		t.Fatalf("nil Workloads port: want 500, got %d body=%s", resp.Code, resp.Body.String())
	}
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if e.Code != "internal_error" {
		t.Fatalf("500 envelope = %+v, want code internal_error", e)
	}
}

func TestProvisioningComponent_ListWorkloadDependencies_ExternalFallsBackToTypeName(t *testing.T) {
	t.Parallel()
	svc := provisioning.NewService(provisioning.Deps{
		Workloads: &cWorkloads{
			deps: []openchoreo.WorkloadConsumerDep{
				{
					OwnerProject:   "shop",
					OwnerComponent: "orders",
					ResourceRefs:   []string{"shop-custom", "shop-gone"},
				},
			},
			resources: map[string]*openchoreo.Resource{
				"shop-custom": {Spec: openchoreo.ResourceSpec{
					Type: openchoreo.ResourceTypeRef{Kind: "ResourceType", Name: "custom-rt"},
				}},
			},
			types: map[string]*openchoreo.ResourceType{
				"custom-rt": {Metadata: openchoreo.OCObjectMeta{Name: "custom-rt"}},
			},
		},
	})
	h := newProvHarness(t, svc)

	resp := h.AsOrg("acme").Get(workloadDepsPath)
	if resp.Code != 200 {
		t.Fatalf("list: got %d body=%s", resp.Code, resp.Body.String())
	}

	var got []gen.WorkloadDependencyDTO
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v\n%s", err, resp.Body.String())
	}
	if len(got) != 1 {
		t.Fatalf("rows = %+v, want 1 live ResourceType (dangling GetResource 404 omitted)", got)
	}
	row := got[0]
	if row.Kind != gen.Resource || row.Tag != gen.External || row.Ref != "custom-rt" || row.Name != "custom-rt" {
		t.Fatalf("external without annotation = %+v, want ref/name custom-rt (spec.type.Name)", row)
	}
}
