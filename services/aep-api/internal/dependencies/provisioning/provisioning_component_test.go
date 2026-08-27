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
	"github.com/wso2/aep/aep-api/internal/dependencies"
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
	defs     []openchoreo.ExternalResourceDefinition
	deleted  []string
	failOnce error
}

func (f *cRTCatalog) List(_ context.Context, _ string) ([]openchoreo.ExternalResourceDefinition, error) {
	return f.defs, nil
}

func (f *cRTCatalog) Ensure(_ context.Context, _ string, rt *openchoreo.ResourceType) error {
	if f.failOnce != nil {
		err := f.failOnce
		f.failOnce = nil
		return err
	}
	if rt == nil {
		return nil
	}
	def, ok := openchoreo.ExternalDefinitionFromRT(rt)
	if !ok {
		return nil
	}
	for _, existing := range f.defs {
		if strings.EqualFold(existing.Name, def.Name) {
			return nil // get-or-create: existing logical name is left in place
		}
	}
	f.defs = append(f.defs, def)
	return nil
}

func (f *cRTCatalog) Update(_ context.Context, _ string, rt *openchoreo.ResourceType) error {
	if rt == nil {
		return errors.New("cRTCatalog.Update: nil ResourceType")
	}
	def, ok := openchoreo.ExternalDefinitionFromRT(rt)
	if !ok {
		return errors.New("cRTCatalog.Update: not an external ResourceType")
	}
	for i, existing := range f.defs {
		if strings.EqualFold(existing.Name, def.Name) {
			f.defs[i] = def
			return nil
		}
	}
	return errors.New("cRTCatalog.Update: " + def.Name + " not found")
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

type cValuePlane struct {
	cells     map[string]map[string][]provisioning.EnvCell
	instances map[string]map[string][]provisioning.ResourceInstance
}

func (f *cValuePlane) EnvCells(orgID, name string) []provisioning.EnvCell {
	if f.cells == nil {
		return nil
	}
	return f.cells[orgID][name]
}

func (f *cValuePlane) Instances(orgID, name string) []provisioning.ResourceInstance {
	if f.instances == nil {
		return nil
	}
	return f.instances[orgID][name]
}

func (f *cValuePlane) PutEnvCells(orgID, name string, cells []provisioning.EnvCell) {
	if f.cells == nil {
		f.cells = map[string]map[string][]provisioning.EnvCell{}
	}
	if f.cells[orgID] == nil {
		f.cells[orgID] = map[string][]provisioning.EnvCell{}
	}
	f.cells[orgID][name] = append([]provisioning.EnvCell(nil), cells...)
}

func (f *cValuePlane) PutInstances(orgID, name string, instances []provisioning.ResourceInstance) {
	if f.instances == nil {
		f.instances = map[string]map[string][]provisioning.ResourceInstance{}
	}
	if f.instances[orgID] == nil {
		f.instances[orgID] = map[string][]provisioning.ResourceInstance{}
	}
	f.instances[orgID][name] = append([]provisioning.ResourceInstance(nil), instances...)
}

// cEnvs fakes provisioning.EnvironmentLister — ListNames returns the
// injected names (nil names is an empty list, not an error).
type cEnvs struct {
	names []string
}

func (f *cEnvs) ListNames(context.Context, string) ([]string, error) {
	return f.names, nil
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

// list-external-resources: Registered External (stripe) rides org value-plane
// cells + docs; Project External (github) omits envCells. Config schema and
// design-scanned consumers still ride the wire. Secret cells never include value.
func TestProvisioningComponent_ListExternalResources(t *testing.T) {
	t.Parallel()
	svc := provisioning.NewService(provisioning.Deps{
		RTCatalog: &cRTCatalog{defs: []openchoreo.ExternalResourceDefinition{
			{
				Name:        "stripe",
				Description: "payments",
				Config: []openchoreo.ExternalResourceConfigKey{
					{Key: "api_key", Secret: true}, {Key: "region"},
				},
				ConsumptionInstructions: "Use the secret key as Bearer.",
				ResourceDocs: []openchoreo.ResourceDoc{
					{Type: "openapi", URL: "https://example.com/openapi.yaml"},
				},
			},
			{
				Name: "github",
				Config: []openchoreo.ExternalResourceConfigKey{
					{Key: "token", Secret: true},
				},
			},
		}},
		CatalogValuePlane: &cValuePlane{
			cells: map[string]map[string][]provisioning.EnvCell{
				"acme": {
					"stripe": {
						{Environment: "development", Key: "api_key", Status: "configured", Value: "sk_live"},
						{Environment: "development", Key: "region", Status: "configured", Value: "us"},
						{Environment: "production", Key: "api_key", Status: "unset"},
						{Environment: "production", Key: "region", Status: "unset"},
					},
				},
			},
			instances: map[string]map[string][]provisioning.ResourceInstance{
				"acme": {
					"stripe": {
						{Project: "shop", Environment: "development", Status: "Ready"},
					},
				},
			},
		},
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
	if len(got) != 2 {
		t.Fatalf("resources = %+v, want stripe and github", got)
	}
	byName := make(map[string]gen.ExternalResourceDTO, len(got))
	for _, r := range got {
		byName[r.Name] = r
	}
	stripe, ok := byName["stripe"]
	if !ok || stripe.Description != "payments" {
		t.Fatalf("resources = %+v, want the stripe Registered External entry", got)
	}
	github, ok := byName["github"]
	if !ok {
		t.Fatalf("resources = %+v, want the github Project External entry", got)
	}
	if len(stripe.Config) != 2 || stripe.Config[0].Key != "api_key" || !stripe.Config[0].Secret {
		t.Errorf("config schema = %+v", stripe.Config)
	}
	if len(stripe.Consumers) != 1 || stripe.Consumers[0].ProjectID != "proj" || stripe.Consumers[0].ComponentName != "orders" {
		t.Errorf("consumers = %+v, want the design-scanned orders consumer", stripe.Consumers)
	}

	if stripe.ConsumptionInstructions != "Use the secret key as Bearer." {
		t.Fatalf("stripe consumptionInstructions = %#v", stripe.ConsumptionInstructions)
	}
	if len(stripe.EnvCells) != 4 {
		t.Fatalf("Registered External stripe envCells = %#v, want 2 keys × 2 envs", stripe.EnvCells)
	}
	secretByKey := make(map[string]bool, len(stripe.Config))
	for _, k := range stripe.Config {
		secretByKey[k.Key] = k.Secret
	}
	var regionDev *gen.EnvValueCellDTO
	for i := range stripe.EnvCells {
		c := &stripe.EnvCells[i]
		if c.Status != gen.EnvValueCellDTOStatusConfigured && c.Status != gen.EnvValueCellDTOStatusUnset {
			t.Errorf("cell status = %q, want configured|unset", c.Status)
		}
		if secretByKey[c.Key] && c.Value != "" {
			t.Fatalf("secret cell must not carry value: %+v", c)
		}
		if c.Key == "region" && c.Environment == "development" {
			regionDev = c
		}
	}
	if regionDev == nil || regionDev.Value != "us" {
		t.Fatalf("plain region development cell = %+v, want value us", regionDev)
	}
	if len(stripe.ResourceDocs) != 1 || stripe.ResourceDocs[0].Type != gen.ResourceDocPointerDTOTypeOpenapi || stripe.ResourceDocs[0].URL != "https://example.com/openapi.yaml" {
		t.Errorf("resourceDocs = %+v", stripe.ResourceDocs)
	}
	if len(stripe.Instances) != 1 || stripe.Instances[0].Project != "shop" || stripe.Instances[0].Environment != "development" || stripe.Instances[0].Status != "Ready" {
		t.Errorf("instances = %+v", stripe.Instances)
	}

	if len(github.EnvCells) != 0 {
		t.Fatalf("Project External github envCells must be omitted or empty, got %#v", github.EnvCells)
	}
	if github.ConsumptionInstructions != "" {
		t.Errorf("Project External github must omit consumptionInstructions, got %#v", github.ConsumptionInstructions)
	}
}

// TestProvisioningComponent_List_RegisteredWithoutPlane_SynthesizesEnvCells:
// consumption instructions on the RT are the durable Registered marker.
// After an aep-api restart the process-local value plane is empty; list must
// still emit envCells so the catalog and Build preflight treat the name as
// Registered instead of collecting project secrets again.
func TestProvisioningComponent_List_RegisteredWithoutPlane_SynthesizesEnvCells(t *testing.T) {
	t.Parallel()
	svc := provisioning.NewService(provisioning.Deps{
		RTCatalog: &cRTCatalog{defs: []openchoreo.ExternalResourceDefinition{{
			Name: "github",
			Config: []openchoreo.ExternalResourceConfigKey{
				{Key: "GITHUB_TOKEN", Secret: true},
			},
			ConsumptionInstructions: "Call api.github.com with Bearer GITHUB_TOKEN.",
		}}},
		Environments: &cEnvs{names: []string{"development"}},
		Design:       cDesign{},
		Projects:     cProjects{},
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
	if len(got) != 1 || got[0].Name != "github" {
		t.Fatalf("resources = %+v, want github", got)
	}
	if len(got[0].EnvCells) != 1 {
		t.Fatalf("envCells = %#v, want 1 synthesized cell (GITHUB_TOKEN × development)", got[0].EnvCells)
	}
	cell := got[0].EnvCells[0]
	if cell.Key != "GITHUB_TOKEN" || cell.Environment != "development" || cell.Status != gen.EnvValueCellDTOStatusConfigured {
		t.Fatalf("synthesized cell = %+v", cell)
	}
	if cell.Value != "" {
		t.Fatalf("secret cell must not carry value: %+v", cell)
	}
	if !svc.HasOrgEnvCells(context.Background(), "acme", "github") {
		t.Fatal("HasOrgEnvCells(github) = false, want true from RT consumption instructions")
	}
	if svc.HasOrgEnvCells(context.Background(), "acme", "not-registered") {
		t.Fatal("HasOrgEnvCells(unknown) = true, want false")
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

func TestProvisioningComponent_ListOrgEnvironments_Empty(t *testing.T) {
	t.Parallel()
	svc := provisioning.NewService(provisioning.Deps{
		Environments: &cEnvs{names: nil}, // implement ListNames → nil, nil
	})
	h := newProvHarness(t, svc)
	resp := h.AsOrg("acme").Get("/api/v1/dependencies/environments")
	if resp.Code != 200 {
		t.Fatalf("want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	body := strings.TrimSpace(resp.Body.String())
	if body != "[]" && body != "null" {
		// Contract: empty is []. Prefer "[]". Fail if the body is a hardcoded three-env list.
		t.Fatalf("empty environments = %s, want []", body)
	}
	var got []gen.EnvironmentDTO
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %#v, want empty", got)
	}
}

func TestProvisioningComponent_ListOrgEnvironments_NamesFromOC(t *testing.T) {
	t.Parallel()
	svc := provisioning.NewService(provisioning.Deps{
		Environments: &cEnvs{names: []string{"development", "staging-local"}},
	})
	h := newProvHarness(t, svc)
	resp := h.AsOrg("acme").Get("/api/v1/dependencies/environments")
	if resp.Code != 200 {
		t.Fatalf("want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	var got []gen.EnvironmentDTO
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 || got[0].Name != "development" || got[1].Name != "staging-local" {
		t.Fatalf("got %#v", got)
	}
}

func TestProvisioningComponent_ListOrgEnvironments_NoClaims401(t *testing.T) {
	t.Parallel()
	h := newProvHarness(t, provisioning.NewService(provisioning.Deps{}))
	if resp := h.NoAuth().Get("/api/v1/dependencies/environments"); resp.Code != 401 {
		t.Fatalf("claimless: want 401, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestProvisioningComponent_ListExternalResources_DTOGrowth(t *testing.T) {
	t.Parallel()
	raw := []byte(`[
	  {
	    "name": "stripe",
	    "description": "payments",
	    "consumptionInstructions": "Use the secret key as Bearer.",
	    "config": [{"key": "api_key", "secret": true}],
	    "consumers": [{"projectId": "shop", "componentName": "checkout"}],
	    "envCells": [
	      {"environment": "development", "key": "api_key", "status": "configured"},
	      {"environment": "production", "key": "api_key", "status": "unset"}
	    ],
	    "resourceDocs": [{"type": "openapi", "url": "https://example.com/openapi.yaml"}],
	    "instances": [{"project": "shop", "environment": "development", "status": "Ready"}]
	  },
	  {
	    "name": "github",
	    "config": [{"key": "token", "secret": true}],
	    "consumers": []
	  }
	]`)
	var got []gen.ExternalResourceDTO
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got[0].ConsumptionInstructions != "Use the secret key as Bearer." {
		t.Fatalf("consumptionInstructions = %#v", got[0].ConsumptionInstructions)
	}
	if len(got[0].EnvCells) != 2 {
		t.Fatalf("registered envCells = %#v", got[0].EnvCells)
	}
	if len(got[1].EnvCells) != 0 {
		t.Fatalf("project external envCells must be omitted or empty, got %#v", got[1].EnvCells)
	}
	for _, c := range got[0].EnvCells {
		if c.Value != "" {
			t.Fatalf("secret cell must not carry value: %+v", c)
		}
	}
}

func registerBody() gen.RegisterExternalResourceJSONRequestBody {
	return gen.RegisterExternalResourceJSONRequestBody{
		Name:                    "stripe",
		Description:             "Stripe payments",
		ConsumptionInstructions: "Use the secret as Bearer.",
		Config: []gen.ConfigKeyDTO{
			{Key: "api_key", Description: "Secret API key", Secret: true},
			{Key: "region", Description: "Account region", Secret: false},
		},
		EnvValues: []struct {
			Environment string `json:"environment"`
			Key         string `json:"key"`
			Value       string `json:"value"`
		}{
			{Environment: "development", Key: "api_key", Value: "sk_live"},
			{Environment: "development", Key: "region", Value: "us"},
			{Environment: "staging-local", Key: "api_key", Value: "sk_test"},
			{Environment: "staging-local", Key: "region", Value: "eu"},
		},
		ResourceDocs: []gen.ResourceDocWriteDTO{
			{Type: gen.ResourceDocWriteDTOTypeOpenapi, URL: "https://example.com/stripe/openapi.yaml"},
		},
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func newRegisterHarness(t *testing.T, catalog *cRTCatalog, plane *cValuePlane) *componenttest.Harness {
	t.Helper()
	return newRegisterHarnessWithDocs(t, catalog, plane, nil)
}

type recordingDocs struct {
	commits []struct{ orgID, logicalName, fileName, content string }
}

func (r *recordingDocs) CommitUTF8(_ context.Context, orgID, logicalName, fileName, content string) (string, error) {
	r.commits = append(r.commits, struct{ orgID, logicalName, fileName, content string }{orgID, logicalName, fileName, content})
	return logicalName + "/" + fileName, nil
}

func newRegisterHarnessWithDocs(t *testing.T, catalog *cRTCatalog, plane *cValuePlane, docs provisioning.OrgResourceDocs) *componenttest.Harness {
	t.Helper()
	if catalog == nil {
		catalog = &cRTCatalog{}
	}
	if plane == nil {
		plane = &cValuePlane{}
	}
	return newProvHarness(t, provisioning.NewService(provisioning.Deps{
		RTCatalog:         catalog,
		CatalogValuePlane: plane,
		Environments:      &cEnvs{names: []string{"development", "staging-local"}},
		OrgResourceDocs:   docs,
	}))
}

func TestProvisioningComponent_RegisterExternalResource_201(t *testing.T) {
	t.Parallel()
	plane := &cValuePlane{}
	h := newRegisterHarness(t, &cRTCatalog{}, plane)

	resp := h.AsOrg("acme").Post("/api/v1/dependencies/external-resources", mustJSON(t, registerBody()))
	if resp.Code != 201 {
		t.Fatalf("register: want 201, got %d body=%s", resp.Code, resp.Body.String())
	}
	var got gen.ExternalResourceDTO
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v\n%s", err, resp.Body.String())
	}
	if got.Name != "stripe" || got.Description != "Stripe payments" {
		t.Fatalf("registered resource = %+v", got)
	}
	if got.ConsumptionInstructions != "Use the secret as Bearer." {
		t.Fatalf("consumptionInstructions = %#v", got.ConsumptionInstructions)
	}
	if len(got.EnvCells) != 4 {
		t.Fatalf("envCells = %#v, want 2 keys × 2 envs", got.EnvCells)
	}
	var regionDev, apiKeyDev *gen.EnvValueCellDTO
	for i := range got.EnvCells {
		c := &got.EnvCells[i]
		if c.Status != gen.EnvValueCellDTOStatusConfigured {
			t.Errorf("cell status = %q, want configured", c.Status)
		}
		if c.Key == "api_key" && c.Value != "" {
			t.Fatalf("secret api_key cell must not carry value: %+v", c)
		}
		if c.Key == "region" && c.Environment == "development" {
			regionDev = c
		}
		if c.Key == "api_key" && c.Environment == "development" {
			apiKeyDev = c
		}
		if c.Key == "region" && c.Environment == "staging-local" && c.Value != "eu" {
			t.Errorf("staging-local region cell = %+v, want value eu", c)
		}
	}
	if regionDev == nil || regionDev.Value != "us" {
		t.Fatalf("plain region development cell = %+v, want value us", regionDev)
	}
	if apiKeyDev == nil {
		t.Fatal("want an api_key development cell")
	}
	if len(got.ResourceDocs) != 1 || got.ResourceDocs[0].Type != gen.ResourceDocPointerDTOTypeOpenapi || got.ResourceDocs[0].URL != "https://example.com/stripe/openapi.yaml" {
		t.Errorf("resourceDocs = %+v", got.ResourceDocs)
	}

	listed := h.AsOrg("acme").Get("/api/v1/dependencies/external-resources")
	if listed.Code != 200 {
		t.Fatalf("list after register: got %d body=%s", listed.Code, listed.Body.String())
	}
	var views []gen.ExternalResourceDTO
	if err := json.Unmarshal(listed.Body.Bytes(), &views); err != nil {
		t.Fatalf("list body: %v", err)
	}
	if len(views) != 1 || views[0].Name != "stripe" || len(views[0].EnvCells) != 4 {
		t.Fatalf("list after register = %+v, want Registered stripe with 4 envCells", views)
	}
}

func TestProvisioningComponent_RegisterExternalResource_409Duplicate(t *testing.T) {
	t.Parallel()
	h := newRegisterHarness(t, &cRTCatalog{defs: []openchoreo.ExternalResourceDefinition{
		{
			Name: "stripe",
			Config: []openchoreo.ExternalResourceConfigKey{
				{Key: "token", Secret: true},
			},
		},
	}}, &cValuePlane{})

	resp := h.AsOrg("acme").Post("/api/v1/dependencies/external-resources", mustJSON(t, registerBody()))
	if resp.Code != 409 {
		t.Fatalf("duplicate register: want 409, got %d body=%s", resp.Code, resp.Body.String())
	}
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if e.Code != "conflict" {
		t.Fatalf("409 envelope = %+v", e)
	}
}

func TestProvisioningComponent_RegisterExternalResource_400MissingEnvValue(t *testing.T) {
	t.Parallel()
	h := newRegisterHarness(t, &cRTCatalog{}, &cValuePlane{})
	body := registerBody()
	body.EnvValues = body.EnvValues[:3] // omit staging-local/region

	resp := h.AsOrg("acme").Post("/api/v1/dependencies/external-resources", mustJSON(t, body))
	if resp.Code != 400 {
		t.Fatalf("missing env value: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if e.Code != "bad_request" {
		t.Fatalf("400 envelope = %+v", e)
	}
	if strings.Contains(e.Message, "not implemented") {
		t.Fatalf("want a validation 400, got stub: %+v", e)
	}
}

func TestProvisioningComponent_RegisterExternalResource_EnsureFailRetryNot409(t *testing.T) {
	t.Parallel()
	plane := &cValuePlane{}
	catalog := &cRTCatalog{failOnce: errors.New("ensure: oc unavailable")}
	h := newRegisterHarness(t, catalog, plane)

	body := mustJSON(t, registerBody())
	first := h.AsOrg("acme").Post("/api/v1/dependencies/external-resources", body)
	if first.Code == 201 || first.Code == 409 {
		t.Fatalf("first register: want Ensure failure (not 201/409), got %d body=%s", first.Code, first.Body.String())
	}
	if len(catalog.defs) != 0 {
		t.Fatalf("failed Ensure must not list the name, got defs=%+v", catalog.defs)
	}
	if len(plane.EnvCells("acme", "stripe")) == 0 {
		t.Fatal("PutEnvCells must run before Ensure so a retry is not missing cells")
	}

	second := h.AsOrg("acme").Post("/api/v1/dependencies/external-resources", body)
	if second.Code == 409 {
		t.Fatalf("retry after failed Ensure must not 409, body=%s", second.Body.String())
	}
	if second.Code != 201 {
		t.Fatalf("retry: want 201, got %d body=%s", second.Code, second.Body.String())
	}
}

func TestProvisioningComponent_RegisterExternalResource_CellsAreOrgScoped(t *testing.T) {
	t.Parallel()
	plane := &cValuePlane{}
	h := newRegisterHarness(t, &cRTCatalog{}, plane)

	resp := h.AsOrg("acme").Post("/api/v1/dependencies/external-resources", mustJSON(t, registerBody()))
	if resp.Code != 201 {
		t.Fatalf("register: want 201, got %d body=%s", resp.Code, resp.Body.String())
	}
	if len(plane.EnvCells("acme", "stripe")) == 0 {
		t.Fatal("acme stripe cells missing after register")
	}
	if len(plane.EnvCells("other", "stripe")) != 0 {
		t.Fatalf("other org must not see acme stripe cells, got %#v", plane.EnvCells("other", "stripe"))
	}

	listed := h.AsOrg("other").Get("/api/v1/dependencies/external-resources")
	if listed.Code != 200 {
		t.Fatalf("list as other: got %d body=%s", listed.Code, listed.Body.String())
	}
	var views []gen.ExternalResourceDTO
	if err := json.Unmarshal(listed.Body.Bytes(), &views); err != nil {
		t.Fatalf("list body: %v", err)
	}
	for _, v := range views {
		if v.Name != "stripe" {
			continue
		}
		for _, c := range v.EnvCells {
			if c.Key == "region" && c.Value == "us" {
				t.Fatalf("listing as other leaked acme region value: %+v", v.EnvCells)
			}
		}
	}
}

const fileRowBody = "# Stripe\n"

func TestProvisioningComponent_RegisterExternalResource_FileRowReturnsPathPointer(t *testing.T) {
	t.Parallel()
	docs := &recordingDocs{}
	h := newRegisterHarnessWithDocs(t, &cRTCatalog{}, &cValuePlane{}, docs)

	body := registerBody()
	body.ResourceDocs = []gen.ResourceDocWriteDTO{
		{Type: gen.ResourceDocWriteDTOTypeDocumentation, FileName: "README.md", Content: fileRowBody},
	}
	resp := h.AsOrg("acme").Post("/api/v1/dependencies/external-resources", mustJSON(t, body))
	if resp.Code != 201 {
		t.Fatalf("register file row: want 201, got %d body=%s", resp.Code, resp.Body.String())
	}
	raw := resp.Body.String()
	if strings.Contains(raw, `"content"`) {
		t.Errorf("response JSON must not include content: %s", raw)
	}
	if strings.Contains(raw, fileRowBody) || strings.Contains(raw, strings.TrimSpace(fileRowBody)) {
		t.Errorf("response JSON must not include file body: %s", raw)
	}
	var got gen.ExternalResourceDTO
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v\n%s", err, raw)
	}
	if len(got.ResourceDocs) != 1 {
		t.Fatalf("resourceDocs = %+v, want 1 path pointer", got.ResourceDocs)
	}
	d := got.ResourceDocs[0]
	if d.Type != gen.ResourceDocPointerDTOTypeDocumentation || d.Path != "stripe/README.md" || d.URL != "" {
		t.Errorf("resourceDocs[0] = %+v, want type=documentation path=stripe/README.md empty URL", d)
	}
	if len(docs.commits) != 1 {
		t.Fatalf("CommitUTF8 calls = %d, want 1", len(docs.commits))
	}

	listed := h.AsOrg("acme").Get("/api/v1/dependencies/external-resources")
	if listed.Code != 200 {
		t.Fatalf("list after file register: got %d body=%s", listed.Code, listed.Body.String())
	}
	listRaw := listed.Body.String()
	if strings.Contains(listRaw, `"content"`) {
		t.Errorf("list JSON must not include content: %s", listRaw)
	}
	if strings.Contains(listRaw, fileRowBody) || strings.Contains(listRaw, strings.TrimSpace(fileRowBody)) {
		t.Errorf("list JSON must not include file body: %s", listRaw)
	}
	var views []gen.ExternalResourceDTO
	if err := json.Unmarshal(listed.Body.Bytes(), &views); err != nil {
		t.Fatalf("list body: %v", err)
	}
	if len(views) != 1 || len(views[0].ResourceDocs) != 1 || views[0].ResourceDocs[0].Path != "stripe/README.md" {
		t.Fatalf("list resourceDocs = %+v, want path stripe/README.md", views)
	}
}

func TestProvisioningComponent_RegisterExternalResource_URLOnlyDoesNotMintRepo(t *testing.T) {
	t.Parallel()
	docs := &recordingDocs{}
	h := newRegisterHarnessWithDocs(t, &cRTCatalog{}, &cValuePlane{}, docs)

	resp := h.AsOrg("acme").Post("/api/v1/dependencies/external-resources", mustJSON(t, registerBody()))
	if resp.Code != 201 {
		t.Fatalf("register URL-only: want 201, got %d body=%s", resp.Code, resp.Body.String())
	}
	if len(docs.commits) != 0 {
		t.Fatalf("URL-only must not call CommitUTF8, got %d commits: %+v", len(docs.commits), docs.commits)
	}
	var got gen.ExternalResourceDTO
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v\n%s", err, resp.Body.String())
	}
	if len(got.ResourceDocs) != 1 || got.ResourceDocs[0].URL != "https://example.com/stripe/openapi.yaml" || got.ResourceDocs[0].Path != "" {
		t.Fatalf("resourceDocs = %+v, want URL set and Path empty", got.ResourceDocs)
	}
}

func TestProvisioningComponent_RegisterExternalResource_KeepPathDoesNotMint(t *testing.T) {
	t.Parallel()
	docs := &recordingDocs{}
	h := newRegisterHarnessWithDocs(t, &cRTCatalog{}, &cValuePlane{}, docs)

	body := registerBody()
	body.ResourceDocs = []gen.ResourceDocWriteDTO{
		{Type: gen.ResourceDocWriteDTOTypeDocumentation, Path: "stripe/README.md"},
	}
	resp := h.AsOrg("acme").Post("/api/v1/dependencies/external-resources", mustJSON(t, body))
	if resp.Code != 201 {
		t.Fatalf("register keep-path: want 201, got %d body=%s", resp.Code, resp.Body.String())
	}
	if len(docs.commits) != 0 {
		t.Fatalf("keep-path must not call CommitUTF8, got %d commits: %+v", len(docs.commits), docs.commits)
	}
	var got gen.ExternalResourceDTO
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v\n%s", err, resp.Body.String())
	}
	if len(got.ResourceDocs) != 1 || got.ResourceDocs[0].Path != "stripe/README.md" || got.ResourceDocs[0].URL != "" {
		t.Fatalf("resourceDocs = %+v, want path stripe/README.md and empty URL", got.ResourceDocs)
	}
}

func TestProvisioningComponent_UpdateExternalResource_KeepPathDoesNotMint(t *testing.T) {
	t.Parallel()
	docs := &recordingDocs{}
	h := newRegisterHarnessWithDocs(t, &cRTCatalog{}, &cValuePlane{}, docs)

	reg := h.AsOrg("acme").Post("/api/v1/dependencies/external-resources", mustJSON(t, registerBody()))
	if reg.Code != 201 {
		t.Fatalf("register: want 201, got %d body=%s", reg.Code, reg.Body.String())
	}
	if len(docs.commits) != 0 {
		t.Fatalf("URL-only register must not mint, got %d commits", len(docs.commits))
	}

	body := registerBody()
	body.ResourceDocs = []gen.ResourceDocWriteDTO{
		{Type: gen.ResourceDocWriteDTOTypeDocumentation, Path: "stripe/README.md"},
	}
	resp := h.AsOrg("acme").Put("/api/v1/dependencies/external-resources/stripe", mustJSON(t, body))
	if resp.Code != 200 {
		t.Fatalf("update keep-path: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if len(docs.commits) != 0 {
		t.Fatalf("keep-path update must not call CommitUTF8, got %d commits: %+v", len(docs.commits), docs.commits)
	}
	var got gen.ExternalResourceDTO
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v\n%s", err, resp.Body.String())
	}
	if len(got.ResourceDocs) != 1 || got.ResourceDocs[0].Path != "stripe/README.md" || got.ResourceDocs[0].URL != "" {
		t.Fatalf("resourceDocs = %+v, want path stripe/README.md and empty URL", got.ResourceDocs)
	}
}

func TestProvisioningComponent_RegisterExternalResource_BothURLAndContent400(t *testing.T) {
	t.Parallel()
	h := newRegisterHarness(t, &cRTCatalog{}, &cValuePlane{})
	body := registerBody()
	body.ResourceDocs = []gen.ResourceDocWriteDTO{
		{Type: gen.ResourceDocWriteDTOTypeOpenapi, URL: "https://example.com/stripe/openapi.yaml", Content: fileRowBody},
	}

	resp := h.AsOrg("acme").Post("/api/v1/dependencies/external-resources", mustJSON(t, body))
	if resp.Code != 400 {
		t.Fatalf("url+content: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if e.Code != "bad_request" {
		t.Fatalf("400 envelope = %+v", e)
	}
}

func TestProvisioningComponent_RegisterExternalResource_DuplicateFileRowDoesNotMint(t *testing.T) {
	t.Parallel()
	docs := &recordingDocs{}
	h := newRegisterHarnessWithDocs(t, &cRTCatalog{defs: []openchoreo.ExternalResourceDefinition{
		{
			Name: "stripe",
			Config: []openchoreo.ExternalResourceConfigKey{
				{Key: "token", Secret: true},
			},
		},
	}}, &cValuePlane{}, docs)

	body := registerBody()
	body.ResourceDocs = []gen.ResourceDocWriteDTO{
		{Type: gen.ResourceDocWriteDTOTypeDocumentation, FileName: "README.md", Content: fileRowBody},
	}
	resp := h.AsOrg("acme").Post("/api/v1/dependencies/external-resources", mustJSON(t, body))
	if resp.Code != 409 {
		t.Fatalf("duplicate file register: want 409, got %d body=%s", resp.Code, resp.Body.String())
	}
	if len(docs.commits) != 0 {
		t.Fatalf("duplicate file row must not call CommitUTF8, got %d commits: %+v", len(docs.commits), docs.commits)
	}
}

func TestProvisioningComponent_RegisterExternalResource_FileRowMissingEnvValueDoesNotMint(t *testing.T) {
	t.Parallel()
	docs := &recordingDocs{}
	h := newRegisterHarnessWithDocs(t, &cRTCatalog{}, &cValuePlane{}, docs)
	body := registerBody()
	body.EnvValues = body.EnvValues[:3] // omit staging-local/region
	body.ResourceDocs = []gen.ResourceDocWriteDTO{
		{Type: gen.ResourceDocWriteDTOTypeDocumentation, FileName: "README.md", Content: fileRowBody},
	}

	resp := h.AsOrg("acme").Post("/api/v1/dependencies/external-resources", mustJSON(t, body))
	if resp.Code != 400 {
		t.Fatalf("file row missing env value: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if e.Code != "bad_request" {
		t.Fatalf("400 envelope = %+v", e)
	}
	if len(docs.commits) != 0 {
		t.Fatalf("failed file register must not call CommitUTF8, got %d commits: %+v", len(docs.commits), docs.commits)
	}
}

func TestProvisioningComponent_UpdateExternalResource_FileRowMissingEnvValueDoesNotMint(t *testing.T) {
	t.Parallel()
	docs := &recordingDocs{}
	h := newRegisterHarnessWithDocs(t, &cRTCatalog{}, &cValuePlane{}, docs)

	reg := h.AsOrg("acme").Post("/api/v1/dependencies/external-resources", mustJSON(t, registerBody()))
	if reg.Code != 201 {
		t.Fatalf("register: want 201, got %d body=%s", reg.Code, reg.Body.String())
	}
	if len(docs.commits) != 0 {
		t.Fatalf("URL-only register must not mint, got %d commits", len(docs.commits))
	}

	body := registerBody()
	body.EnvValues = body.EnvValues[:3] // omit staging-local/region
	body.ResourceDocs = []gen.ResourceDocWriteDTO{
		{Type: gen.ResourceDocWriteDTOTypeDocumentation, FileName: "README.md", Content: fileRowBody},
	}
	resp := h.AsOrg("acme").Put("/api/v1/dependencies/external-resources/stripe", mustJSON(t, body))
	if resp.Code != 400 {
		t.Fatalf("update file row missing env: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if len(docs.commits) != 0 {
		t.Fatalf("failed file update must not call CommitUTF8, got %d commits: %+v", len(docs.commits), docs.commits)
	}
}

func TestProvisioningComponent_UpdateExternalResource_AcceptsFileRow(t *testing.T) {
	t.Parallel()
	docs := &recordingDocs{}
	h := newRegisterHarnessWithDocs(t, &cRTCatalog{}, &cValuePlane{}, docs)

	reg := h.AsOrg("acme").Post("/api/v1/dependencies/external-resources", mustJSON(t, registerBody()))
	if reg.Code != 201 {
		t.Fatalf("register: want 201, got %d body=%s", reg.Code, reg.Body.String())
	}
	if len(docs.commits) != 0 {
		t.Fatalf("URL-only register must not mint, got %d commits", len(docs.commits))
	}

	body := registerBody()
	body.ResourceDocs = []gen.ResourceDocWriteDTO{
		{Type: gen.ResourceDocWriteDTOTypeDocumentation, FileName: "README.md", Content: fileRowBody},
	}
	resp := h.AsOrg("acme").Put("/api/v1/dependencies/external-resources/stripe", mustJSON(t, body))
	if resp.Code != 200 {
		t.Fatalf("update file row: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	var got gen.ExternalResourceDTO
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v\n%s", err, resp.Body.String())
	}
	if len(got.ResourceDocs) != 1 || got.ResourceDocs[0].Path != "stripe/README.md" || got.ResourceDocs[0].URL != "" {
		t.Fatalf("resourceDocs = %+v, want path pointer stripe/README.md", got.ResourceDocs)
	}
	if len(docs.commits) != 1 {
		t.Fatalf("CommitUTF8 calls = %d, want 1", len(docs.commits))
	}
}

func envValue(env, key, value string) struct {
	Environment string `json:"environment"`
	Key         string `json:"key"`
	Value       string `json:"value"`
} {
	return struct {
		Environment string `json:"environment"`
		Key         string `json:"key"`
		Value       string `json:"value"`
	}{Environment: env, Key: key, Value: value}
}

func keepIfEmptyUpdateBody() gen.RegisterExternalResourceJSONRequestBody {
	body := registerBody()
	body.Description = "Stripe payments (updated)"
	body.ConsumptionInstructions = "Use the secret as Bearer. Rotate quarterly."
	body.Config = []gen.ConfigKeyDTO{
		{Key: "api_key", Description: "Secret API key (rotated)", Secret: true},
		{Key: "region", Description: "Account region (primary)", Secret: false},
	}
	body.EnvValues = []struct {
		Environment string `json:"environment"`
		Key         string `json:"key"`
		Value       string `json:"value"`
	}{
		envValue("development", "api_key", ""),
		envValue("development", "region", "ap-southeast"),
		envValue("staging-local", "api_key", ""),
		envValue("staging-local", "region", "eu"),
	}
	body.ResourceDocs = []gen.ResourceDocWriteDTO{
		{Type: gen.ResourceDocWriteDTOTypeOpenapi, URL: "https://example.com/stripe/openapi-v2.yaml"},
	}
	return body
}

func findEnvCell(t *testing.T, cells []gen.EnvValueCellDTO, env, key string) gen.EnvValueCellDTO {
	t.Helper()
	for _, c := range cells {
		if c.Environment == env && c.Key == key {
			return c
		}
	}
	t.Fatalf("no cell environment=%q key=%q in %+v", env, key, cells)
	return gen.EnvValueCellDTO{}
}

func TestProvisioningComponent_UpdateExternalResource_KeepIfEmptySecret(t *testing.T) {
	t.Parallel()
	h := newRegisterHarness(t, &cRTCatalog{}, &cValuePlane{})

	reg := h.AsOrg("acme").Post("/api/v1/dependencies/external-resources", mustJSON(t, registerBody()))
	if reg.Code != 201 {
		t.Fatalf("register: want 201, got %d body=%s", reg.Code, reg.Body.String())
	}

	body := keepIfEmptyUpdateBody()
	body.Name = "other-name"
	resp := h.AsOrg("acme").Put("/api/v1/dependencies/external-resources/stripe", mustJSON(t, body))
	if resp.Code != 200 {
		t.Fatalf("update: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	var got gen.ExternalResourceDTO
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v\n%s", err, resp.Body.String())
	}
	if got.Name != "stripe" || got.Description != "Stripe payments (updated)" {
		t.Fatalf("updated resource = %+v", got)
	}
	if got.ConsumptionInstructions != "Use the secret as Bearer. Rotate quarterly." {
		t.Fatalf("consumptionInstructions = %#v", got.ConsumptionInstructions)
	}
	if len(got.Config) != 2 || got.Config[0].Key != "api_key" || got.Config[0].Description != "Secret API key (rotated)" {
		t.Fatalf("config = %+v", got.Config)
	}
	if len(got.ResourceDocs) != 1 || got.ResourceDocs[0].URL != "https://example.com/stripe/openapi-v2.yaml" {
		t.Fatalf("resourceDocs = %+v", got.ResourceDocs)
	}

	apiKeyDev := findEnvCell(t, got.EnvCells, "development", "api_key")
	if apiKeyDev.Status != gen.EnvValueCellDTOStatusConfigured {
		t.Errorf("secret api_key development status = %q, want configured", apiKeyDev.Status)
	}
	if apiKeyDev.Value != "" {
		t.Fatalf("secret api_key cell must not carry value: %+v", apiKeyDev)
	}
	regionDev := findEnvCell(t, got.EnvCells, "development", "region")
	if regionDev.Value != "ap-southeast" {
		t.Fatalf("plain region development cell = %+v, want value ap-southeast", regionDev)
	}

	listed := h.AsOrg("acme").Get("/api/v1/dependencies/external-resources")
	if listed.Code != 200 {
		t.Fatalf("list after update: got %d body=%s", listed.Code, listed.Body.String())
	}
	var views []gen.ExternalResourceDTO
	if err := json.Unmarshal(listed.Body.Bytes(), &views); err != nil {
		t.Fatalf("list body: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("list after update = %+v, want one stripe row", views)
	}
	listedStripe := views[0]
	if listedStripe.Description != "Stripe payments (updated)" || listedStripe.ConsumptionInstructions != "Use the secret as Bearer. Rotate quarterly." {
		t.Fatalf("list metadata = %+v", listedStripe)
	}
	if len(listedStripe.Config) != 2 || listedStripe.Config[0].Key != "api_key" || listedStripe.Config[0].Description != "Secret API key (rotated)" {
		t.Fatalf("list config = %+v, want rotated api_key description", listedStripe.Config)
	}
	if listedStripe.Config[1].Key != "region" || listedStripe.Config[1].Description != "Account region (primary)" {
		t.Fatalf("list region config = %+v", listedStripe.Config)
	}
	if len(listedStripe.ResourceDocs) != 1 || listedStripe.ResourceDocs[0].URL != "https://example.com/stripe/openapi-v2.yaml" {
		t.Fatalf("list resourceDocs = %+v, want updated URL", listedStripe.ResourceDocs)
	}
	if findEnvCell(t, listedStripe.EnvCells, "development", "api_key").Value != "" {
		t.Fatalf("list secret cell leaked value: %+v", listedStripe.EnvCells)
	}
	if findEnvCell(t, listedStripe.EnvCells, "development", "region").Value != "ap-southeast" {
		t.Fatalf("list region cell = %+v, want ap-southeast", listedStripe.EnvCells)
	}
}

func TestProvisioningComponent_UpdateExternalResource_400KeyMutation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		mut  func(*gen.RegisterExternalResourceJSONRequestBody)
	}{
		{
			name: "add key",
			mut: func(body *gen.RegisterExternalResourceJSONRequestBody) {
				body.Config = append(body.Config, gen.ConfigKeyDTO{Key: "mode", Description: "Charge mode", Secret: false})
				body.EnvValues = append(body.EnvValues,
					envValue("development", "mode", "live"),
					envValue("staging-local", "mode", "test"),
				)
			},
		},
		{
			name: "remove key",
			mut: func(body *gen.RegisterExternalResourceJSONRequestBody) {
				body.Config = body.Config[:1]
				body.EnvValues = []struct {
					Environment string `json:"environment"`
					Key         string `json:"key"`
					Value       string `json:"value"`
				}{
					envValue("development", "api_key", "sk_live"),
					envValue("staging-local", "api_key", "sk_test"),
				}
			},
		},
		{
			name: "rename key",
			mut: func(body *gen.RegisterExternalResourceJSONRequestBody) {
				body.Config[0].Key = "secret_key"
				body.EnvValues = []struct {
					Environment string `json:"environment"`
					Key         string `json:"key"`
					Value       string `json:"value"`
				}{
					envValue("development", "secret_key", "sk_live"),
					envValue("development", "region", "us"),
					envValue("staging-local", "secret_key", "sk_test"),
					envValue("staging-local", "region", "eu"),
				}
			},
		},
		{
			name: "flip secret",
			mut: func(body *gen.RegisterExternalResourceJSONRequestBody) {
				body.Config[1].Secret = true
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newRegisterHarness(t, &cRTCatalog{}, &cValuePlane{})
			reg := h.AsOrg("acme").Post("/api/v1/dependencies/external-resources", mustJSON(t, registerBody()))
			if reg.Code != 201 {
				t.Fatalf("register: want 201, got %d body=%s", reg.Code, reg.Body.String())
			}
			body := registerBody()
			tc.mut(&body)
			resp := h.AsOrg("acme").Put("/api/v1/dependencies/external-resources/stripe", mustJSON(t, body))
			if resp.Code != 400 {
				t.Fatalf("key mutation %q: want 400, got %d body=%s", tc.name, resp.Code, resp.Body.String())
			}
			e := componenttest.DecodeEnvelope(t, resp.Body.String())
			if e.Code != "bad_request" {
				t.Fatalf("400 envelope = %+v", e)
			}
			if strings.Contains(e.Message, "not implemented") {
				t.Fatalf("want a key-identity 400, got stub: %+v", e)
			}
		})
	}
}

func TestProvisioningComponent_UpdateExternalResource_409ProjectExternal(t *testing.T) {
	t.Parallel()
	h := newRegisterHarness(t, &cRTCatalog{defs: []openchoreo.ExternalResourceDefinition{
		{
			Name: "github",
			Config: []openchoreo.ExternalResourceConfigKey{
				{Key: "token", Secret: true},
			},
		},
	}}, &cValuePlane{})

	resp := h.AsOrg("acme").Put("/api/v1/dependencies/external-resources/github", mustJSON(t, registerBody()))
	if resp.Code != 409 {
		t.Fatalf("project external update: want 409, got %d body=%s", resp.Code, resp.Body.String())
	}
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if e.Code != "conflict" {
		t.Fatalf("409 envelope = %+v", e)
	}
}

func TestProvisioningComponent_UpdateExternalResource_404Unknown(t *testing.T) {
	t.Parallel()
	h := newRegisterHarness(t, &cRTCatalog{}, &cValuePlane{})

	resp := h.AsOrg("acme").Put("/api/v1/dependencies/external-resources/no-such", mustJSON(t, registerBody()))
	if resp.Code != 404 {
		t.Fatalf("unknown update: want 404, got %d body=%s", resp.Code, resp.Body.String())
	}
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if e.Code != "not_found" {
		t.Fatalf("404 envelope = %+v", e)
	}
}

func TestProvisioningComponent_UpdateExternalResource_NonSecretPrefillRoundTrip(t *testing.T) {
	t.Parallel()
	h := newRegisterHarness(t, &cRTCatalog{}, &cValuePlane{})

	reg := h.AsOrg("acme").Post("/api/v1/dependencies/external-resources", mustJSON(t, registerBody()))
	if reg.Code != 201 {
		t.Fatalf("register: want 201, got %d body=%s", reg.Code, reg.Body.String())
	}

	listed := h.AsOrg("acme").Get("/api/v1/dependencies/external-resources")
	if listed.Code != 200 {
		t.Fatalf("list after register: got %d body=%s", listed.Code, listed.Body.String())
	}
	var before []gen.ExternalResourceDTO
	if err := json.Unmarshal(listed.Body.Bytes(), &before); err != nil {
		t.Fatalf("list body: %v", err)
	}
	if len(before) != 1 {
		t.Fatalf("list after register = %+v, want stripe", before)
	}
	if findEnvCell(t, before[0].EnvCells, "development", "region").Value != "us" {
		t.Fatalf("prefill region = %+v, want us", before[0].EnvCells)
	}

	body := registerBody()
	for i := range body.EnvValues {
		if body.EnvValues[i].Key == "region" && body.EnvValues[i].Environment == "development" {
			body.EnvValues[i].Value = "ap"
		}
	}
	resp := h.AsOrg("acme").Put("/api/v1/dependencies/external-resources/stripe", mustJSON(t, body))
	if resp.Code != 200 {
		t.Fatalf("update: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}

	after := h.AsOrg("acme").Get("/api/v1/dependencies/external-resources")
	if after.Code != 200 {
		t.Fatalf("list after update: got %d body=%s", after.Code, after.Body.String())
	}
	var views []gen.ExternalResourceDTO
	if err := json.Unmarshal(after.Body.Bytes(), &views); err != nil {
		t.Fatalf("list body: %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("list after update = %+v, want stripe", views)
	}
	if findEnvCell(t, views[0].EnvCells, "development", "region").Value != "ap" {
		t.Fatalf("round-trip region = %+v, want ap", views[0].EnvCells)
	}
	for _, c := range views[0].EnvCells {
		if c.Key == "api_key" && c.Value != "" {
			t.Fatalf("secret cell must not carry value: %+v", c)
		}
	}
}

// CollectExternalResourceValues / SaveValues is the project value plane.
// POST .../values must not populate org catalog envCells.
func TestProvisioningComponent_CollectValues_DoesNotCreateOrgEnvCells(t *testing.T) {
	t.Parallel()
	plane := &cValuePlane{}
	svc := provisioning.NewService(provisioning.Deps{
		RTCatalog: &cRTCatalog{defs: []openchoreo.ExternalResourceDefinition{
			{
				Name: "stripe",
				Config: []openchoreo.ExternalResourceConfigKey{
					{Key: "api_key", Secret: true}, {Key: "region"},
				},
			},
		}},
		CatalogValuePlane: plane,
		Design:            cDesign{comps: stripeConsumerDesign()},
		Issues:            cIssues{},
		ExtProv:           &cExtProv{},
	})
	h := newProvHarness(t, svc)

	resp := h.AsOrg("acme").Post(
		"/api/v1/projects/proj/dependencies/external-resources/stripe/values",
		`{"environments":{"development":{"api_key":"sk_live","region":"us"}}}`,
	)
	if resp.Code != 200 {
		t.Fatalf("collect values: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}

	listed := h.AsOrg("acme").Get("/api/v1/dependencies/external-resources")
	if listed.Code != 200 {
		t.Fatalf("list: got %d body=%s", listed.Code, listed.Body.String())
	}
	var views []gen.ExternalResourceDTO
	if err := json.Unmarshal(listed.Body.Bytes(), &views); err != nil {
		t.Fatalf("list body: %v", err)
	}
	if len(views) != 1 || views[0].Name != "stripe" {
		t.Fatalf("list = %+v, want stripe", views)
	}
	if len(views[0].EnvCells) != 0 {
		t.Fatalf("POST .../values must not create org envCells, got %#v", views[0].EnvCells)
	}
}

func TestProvisioningComponent_CollectValues_RegisteredName_409(t *testing.T) {
	t.Parallel()
	plane := &cValuePlane{}
	plane.PutEnvCells("acme", "stripe", []provisioning.EnvCell{
		{Environment: "development", Key: "api_key", Status: "configured"},
		{Environment: "development", Key: "region", Status: "configured", Value: "us"},
	})
	prov := &cExtProv{}
	svc := provisioning.NewService(provisioning.Deps{
		RTCatalog: &cRTCatalog{defs: []openchoreo.ExternalResourceDefinition{
			{Name: "stripe", Config: []openchoreo.ExternalResourceConfigKey{
				{Key: "api_key", Secret: true}, {Key: "region"},
			}},
		}},
		CatalogValuePlane: plane,
		Design:            cDesign{comps: stripeConsumerDesign()},
		Issues:            cIssues{},
		ExtProv:           prov,
	})
	h := newProvHarness(t, svc)
	resp := h.AsOrg("acme").Post(
		"/api/v1/projects/proj/dependencies/external-resources/stripe/values",
		`{"environments":{"development":{"api_key":"sk_live","region":"us"}}}`,
	)
	if resp.Code != 409 {
		t.Fatalf("Registered values POST: want 409, got %d body=%s", resp.Code, resp.Body.String())
	}
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if e.Code != "conflict" {
		t.Fatalf("409 envelope = %+v", e)
	}
	if prov.provisionCalls != 0 {
		t.Fatalf("Registered values POST must not provision project OpenBao, calls=%d", prov.provisionCalls)
	}
}

// cExtProv is a no-op ExternalProvisioner so SaveValues can succeed in the
// collect-values guard without writing an org catalog record.
type cExtProv struct {
	provisionCalls int
}

func (c *cExtProv) Provision(context.Context, string, string, string, *dependencies.ExternalResource, map[string]dependencies.EnvValues) (*dependencies.ProvisionResult, error) {
	c.provisionCalls++
	return &dependencies.ProvisionResult{}, nil
}
func (c *cExtProv) AuthorPreparedValues(context.Context, string, string, *dependencies.ExternalResource, map[string]dependencies.PreparedEnvValues) (*dependencies.ProvisionResult, error) {
	return &dependencies.ProvisionResult{}, nil
}
func (*cExtProv) Deprovision(context.Context, string, string, string, []string) error { return nil }
func (*cExtProv) ResolveRunnerSecrets(context.Context, string, string, string, []string) ([]dependencies.ExternalResourceRunnerSecret, error) {
	return nil, nil
}
