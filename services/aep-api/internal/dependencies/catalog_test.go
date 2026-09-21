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

package dependencies

import (
	"context"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// Structural compile-time check (dependency-management Phase 5): *Catalog is the
// concrete provider the composition root wires as the read-time org-service
// resolver (spec.SetOrgServiceResolver), so it must satisfy the artifacts
// consumer-side port.
var _ spec.OrgServiceResolver = (*Catalog)(nil)

func sampleEndpoints() []openchoreo.WorkloadEndpointInfo {
	return []openchoreo.WorkloadEndpointInfo{
		// org-published: external + namespace → an org-service target.
		{Project: "hr", Component: "employee-api", Workload: "hr-employee-api-workload",
			Name: "http", Type: "HTTP", Port: 8080, Visibility: []string{"external", "namespace"}},
		// project-only: no namespace visibility → NOT an org-service target.
		{Project: "hr", Component: "payroll-internal", Workload: "hr-payroll-internal-workload",
			Name: "http", Type: "HTTP", Port: 8081, Visibility: []string{"external"}},
		// same-project sibling with ONLY implicit project visibility (no
		// namespace/external) → resolvable by ResolveProjectEndpoint, but NOT
		// namespace-visible.
		{Project: "org-roster", Component: "org-roster-todo-api", Workload: "org-roster-todo-api-workload",
			Name: "http", Type: "HTTP", Port: 8082, Visibility: nil},
	}
}

func fakeRC(endpoints []openchoreo.WorkloadEndpointInfo) *ocmocks.ResourceClientMock {
	return &ocmocks.ResourceClientMock{
		ListWorkloadEndpointsFunc: func(_ context.Context, _ string) ([]openchoreo.WorkloadEndpointInfo, error) {
			return endpoints, nil
		},
	}
}

func TestCatalog_List(t *testing.T) {
	t.Parallel()
	cat := NewCatalog(fakeRC(sampleEndpoints()))

	got, err := cat.List(context.Background(), "ns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 endpoints, got %d: %+v", len(got), got)
	}
}

func TestCatalog_List_PropagatesError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("boom")
	rc := &ocmocks.ResourceClientMock{
		ListWorkloadEndpointsFunc: func(_ context.Context, _ string) ([]openchoreo.WorkloadEndpointInfo, error) {
			return nil, wantErr
		},
	}
	cat := NewCatalog(rc)

	if _, err := cat.List(context.Background(), "ns"); !errors.Is(err, wantErr) {
		t.Fatalf("want wrapped %v, got %v", wantErr, err)
	}
}

func TestCatalog_NilSafety(t *testing.T) {
	t.Parallel()

	var nilCatalog *Catalog
	got, err := nilCatalog.List(context.Background(), "ns")
	if got != nil || err != nil {
		t.Fatalf("nil receiver: want (nil, nil), got (%v, %v)", got, err)
	}

	unwired := NewCatalog(nil)
	got, err = unwired.List(context.Background(), "ns")
	if got != nil || err != nil {
		t.Fatalf("nil client: want (nil, nil), got (%v, %v)", got, err)
	}
}

func TestCatalog_ResolveProjectEndpoint(t *testing.T) {
	t.Parallel()
	cat := NewCatalog(fakeRC(sampleEndpoints()))

	// A project-only sibling (NOT namespace-visible) resolves by project+component.
	got, ok, err := cat.ResolveProjectEndpoint(context.Background(), "ns", "org-roster", "org-roster-todo-api")
	if err != nil || !ok {
		t.Fatalf("org-roster-todo-api: want resolved, got ok=%v err=%v", ok, err)
	}
	if got.Name != "http" || got.Component != "org-roster-todo-api" {
		t.Fatalf("resolved wrong target: %+v", got)
	}
	// Sanity: this endpoint is project-only — it must NOT be namespace-visible.
	if got.NamespaceVisible() {
		t.Fatalf("expected project-only endpoint, got namespace-visible: %+v", got)
	}

	// Unknown component must not resolve.
	if _, ok, _ := cat.ResolveProjectEndpoint(context.Background(), "ns", "org-roster", "org-roster-nope"); ok {
		t.Fatalf("unknown component must not resolve")
	}
}

func TestCatalog_ResolveNamespaceVisible(t *testing.T) {
	t.Parallel()
	cat := NewCatalog(fakeRC(sampleEndpoints()))

	got, ok, err := cat.ResolveNamespaceVisible(context.Background(), "ns", "employee-api")
	if err != nil || !ok {
		t.Fatalf("employee-api: want resolved, got ok=%v err=%v", ok, err)
	}
	if got.Project != "hr" || got.Name != "http" {
		t.Fatalf("resolved wrong target: %+v", got)
	}

	// project-only target must not resolve as an org-service.
	if _, ok, _ := cat.ResolveNamespaceVisible(context.Background(), "ns", "payroll-internal"); ok {
		t.Fatalf("payroll-internal is project-only — must not resolve")
	}
	// unknown name.
	if _, ok, _ := cat.ResolveNamespaceVisible(context.Background(), "ns", "nope"); ok {
		t.Fatalf("unknown org-service must not resolve")
	}
}

func TestCatalog_IsNamespaceVisible(t *testing.T) {
	t.Parallel()
	cat := NewCatalog(fakeRC(sampleEndpoints()))

	ok, err := cat.IsNamespaceVisible(context.Background(), "ns", "employee-api")
	if err != nil || !ok {
		t.Fatalf("employee-api: want visible, got ok=%v err=%v", ok, err)
	}
	ok, err = cat.IsNamespaceVisible(context.Background(), "ns", "payroll-internal")
	if err != nil || ok {
		t.Fatalf("payroll-internal: want not visible, got ok=%v err=%v", ok, err)
	}
}

func TestCatalog_ExistsAnyVisibility(t *testing.T) {
	t.Parallel()
	cat := NewCatalog(fakeRC(sampleEndpoints()))

	// A project-only component (NOT namespace-visible) still exists in the
	// catalog → ExistsAnyVisibility true (the blocked/access-required case).
	got, err := cat.ExistsAnyVisibility(context.Background(), "ns", "org-roster-todo-api")
	if err != nil {
		t.Fatalf("org-roster-todo-api: unexpected error: %v", err)
	}
	if !got {
		t.Fatalf("org-roster-todo-api: want exists=true (project-only but present)")
	}

	// An unknown component does not exist at any visibility → the not-found case.
	got, err = cat.ExistsAnyVisibility(context.Background(), "ns", "nope")
	if err != nil {
		t.Fatalf("nope: unexpected error: %v", err)
	}
	if got {
		t.Fatalf("nope: want exists=false")
	}
}

func TestCatalog_FindByComponent(t *testing.T) {
	t.Parallel()
	cat := NewCatalog(fakeRC(sampleEndpoints()))

	// A project-only component (NOT namespace-visible) is still found — the
	// provider lookup resolves the row regardless of visibility.
	got, ok, err := cat.FindByComponent(context.Background(), "ns", "payroll-internal")
	if err != nil || !ok {
		t.Fatalf("payroll-internal: want found, got ok=%v err=%v", ok, err)
	}
	if got.Project != "hr" || got.Name != "http" || got.Type != "HTTP" {
		t.Fatalf("resolved wrong row: %+v", got)
	}

	// An org-published component is also found.
	if _, ok, _ := cat.FindByComponent(context.Background(), "ns", "employee-api"); !ok {
		t.Fatalf("employee-api: want found")
	}

	// Unknown component must not resolve (the not-found case).
	if _, ok, _ := cat.FindByComponent(context.Background(), "ns", "nope"); ok {
		t.Fatalf("unknown component must not resolve")
	}
}

// ---- Resolve / ListResolved (Task A2: per-endpoint spec discovery) ---------

// fakeRepoLocator resolves an app-factory provider's git repo row by project id.
type fakeRepoLocator struct {
	byProject map[string]*sourcecontrol.GitRepository
}

func (f fakeRepoLocator) GetByOrgAndProjectID(_ context.Context, _, projectID string) (*sourcecontrol.GitRepository, error) {
	return f.byProject[projectID], nil
}

// fakeDesignReader returns a provider project's assembled design bundle.
type fakeDesignReader struct {
	byProject map[string]*spec.DesignFile
}

func (f fakeDesignReader) ReadDesign(_ context.Context, _, projectID string) (*spec.DesignFile, error) {
	return f.byProject[projectID], nil
}

func designWith(comp spec.DesignComponent) *spec.DesignFile {
	return &spec.DesignFile{Components: []spec.DesignComponent{comp}}
}

// (a) An endpoint whose deployed Workload CR already carries an inline schema
// resolves to availability=inline straight from the CR — no repo/design probe.
func TestResolve_InlineFromSchema(t *testing.T) {
	t.Parallel()
	e := openchoreo.WorkloadEndpointInfo{
		Project: "hr", Component: "employee-api", Name: "http", Type: "HTTP", Port: 8080,
		Visibility: []string{"namespace"}, SchemaType: "openapi", SchemaContent: "openapi: 3.0.3\ninfo: {}\n",
	}
	cat := NewCatalog(fakeRC([]openchoreo.WorkloadEndpointInfo{e}),
		WithRepoLocator(fakeRepoLocator{byProject: map[string]*sourcecontrol.GitRepository{
			"hr": {RepoURL: "https://github.com/acme/hr.git", DefaultBranch: "main"},
		}}),
	)

	got := cat.Resolve(context.Background(), "org", e)
	if got.Spec.Availability != "inline" {
		t.Fatalf("availability: want inline, got %q", got.Spec.Availability)
	}
	if got.Spec.InlineContent != e.SchemaContent {
		t.Fatalf("inline content: want CR schema, got %q", got.Spec.InlineContent)
	}
	if !got.NamespaceVisible {
		t.Fatalf("expected NamespaceVisible=true")
	}
	// Repo coords resolve independently of the CR-schema inline spec — the two
	// sources are orthogonal, so availability=inline must not suppress coords.
	if got.Owner != "acme" || got.Repo != "hr" || got.Branch != "main" {
		t.Fatalf("repo coords: want acme/hr@main, got %s/%s@%s", got.Owner, got.Repo, got.Branch)
	}
}

// (b) No CR schema, but the app-factory provider committed a design-bundle
// openapi.yaml → read it server-side → inline, with Path set for provenance.
// Repo coords are also resolved but step 2 wins over step 3.
func TestResolve_InlineFromDesignBundle(t *testing.T) {
	t.Parallel()
	e := openchoreo.WorkloadEndpointInfo{
		Project: "hr", Component: "employee-api", Name: "http", Type: "HTTP", Port: 8080,
		Visibility: []string{"namespace"},
	}
	openapiSpec := "openapi: 3.0.3\ninfo:\n  title: Employee API\n"
	cat := NewCatalog(fakeRC([]openchoreo.WorkloadEndpointInfo{e}),
		WithDesignReader(fakeDesignReader{byProject: map[string]*spec.DesignFile{
			"hr": designWith(spec.DesignComponent{Name: "employee-api", AppPath: "svc", OpenAPISpec: openapiSpec}),
		}}),
		WithRepoLocator(fakeRepoLocator{byProject: map[string]*sourcecontrol.GitRepository{
			"hr": {RepoURL: "https://github.com/acme/hr.git", DefaultBranch: "main"},
		}}),
	)

	got := cat.Resolve(context.Background(), "org", e)
	if got.Spec.Availability != "inline" {
		t.Fatalf("availability: want inline, got %q", got.Spec.Availability)
	}
	if got.Spec.InlineContent != openapiSpec {
		t.Fatalf("inline content: want design-bundle openapiSpec, got %q", got.Spec.InlineContent)
	}
	if got.Spec.Path != "specs/design/components/employee-api/openapi.yaml" {
		t.Fatalf("provenance path: got %q", got.Spec.Path)
	}
	// repo coords still resolved for provenance even though availability=inline.
	if got.Owner != "acme" || got.Repo != "hr" {
		t.Fatalf("repo coords: want acme/hr, got %s/%s", got.Owner, got.Repo)
	}
}

// (b2) The design-bundle component is matched case-insensitively against the
// endpoint's OC component name — the provenance Path's folder segment must
// use the MATCHED component's actual committed Name/casing, not the
// endpoint's (possibly differently-cased) Component.
func TestResolve_InlineFromDesignBundle_ProvenancePathUsesMatchedComponentCasing(t *testing.T) {
	t.Parallel()
	e := openchoreo.WorkloadEndpointInfo{
		Project: "hr", Component: "Employee-API", Name: "http", Type: "HTTP", Port: 8080,
		Visibility: []string{"namespace"},
	}
	openapiSpec := "openapi: 3.0.3\ninfo:\n  title: Employee API\n"
	cat := NewCatalog(fakeRC([]openchoreo.WorkloadEndpointInfo{e}),
		WithDesignReader(fakeDesignReader{byProject: map[string]*spec.DesignFile{
			// Committed folder casing differs from the endpoint's Component field.
			"hr": designWith(spec.DesignComponent{Name: "employee-api", AppPath: "svc", OpenAPISpec: openapiSpec}),
		}}),
	)

	got := cat.Resolve(context.Background(), "org", e)
	if got.Spec.Availability != "inline" {
		t.Fatalf("availability: want inline, got %q", got.Spec.Availability)
	}
	if got.Spec.Path != "specs/design/components/employee-api/openapi.yaml" {
		t.Fatalf("provenance path: want matched design component's casing (employee-api), got %q", got.Spec.Path)
	}
}

// (b3) app-factory OC components are project-prefixed
// (spec.owner.componentName = "<project>-<design-component-name>"), but the
// design bundle stores components under their unprefixed authored name. The
// design-component lookup must retry with the project prefix stripped so the
// inline-from-design-bundle path still fires for app-factory providers.
func TestResolve_InlineFromDesignBundle_ProjectPrefixedComponentName(t *testing.T) {
	t.Parallel()
	e := openchoreo.WorkloadEndpointInfo{
		Project: "myproj", Component: "myproj-svc", Name: "http", Type: "HTTP", Port: 8080,
		Visibility: []string{"namespace"},
	}
	openapiSpec := "openapi: 3.0.3\ninfo:\n  title: Svc\n"
	cat := NewCatalog(fakeRC([]openchoreo.WorkloadEndpointInfo{e}),
		WithDesignReader(fakeDesignReader{byProject: map[string]*spec.DesignFile{
			// Design bundle stores the component under its UNPREFIXED authored name.
			"myproj": designWith(spec.DesignComponent{Name: "svc", OpenAPISpec: openapiSpec}),
		}}),
	)

	got := cat.Resolve(context.Background(), "org", e)
	if got.Spec.Availability != "inline" {
		t.Fatalf("availability: want inline, got %q", got.Spec.Availability)
	}
	if got.Spec.InlineContent != openapiSpec {
		t.Fatalf("inline content: want design-bundle openapiSpec, got %q", got.Spec.InlineContent)
	}
	if got.Spec.Path != "specs/design/components/svc/openapi.yaml" {
		t.Fatalf("provenance path: want unprefixed design component name (svc), got %q", got.Spec.Path)
	}
}

// (c) No schema anywhere, but repo coords resolve (git_repositories row) →
// availability=repo with the component subdir as the hint.
func TestResolve_RepoCoords(t *testing.T) {
	t.Parallel()
	e := openchoreo.WorkloadEndpointInfo{
		Project: "hr", Component: "employee-api", Name: "http", Type: "HTTP", Port: 8080,
		Visibility: []string{"namespace"},
	}
	cat := NewCatalog(fakeRC([]openchoreo.WorkloadEndpointInfo{e}),
		// design bundle present but component has NO openapi.yaml → not inline.
		WithDesignReader(fakeDesignReader{byProject: map[string]*spec.DesignFile{
			"hr": designWith(spec.DesignComponent{Name: "employee-api", AppPath: "services/employee"}),
		}}),
		WithRepoLocator(fakeRepoLocator{byProject: map[string]*sourcecontrol.GitRepository{
			"hr": {RepoURL: "https://github.com/acme/hr", DefaultBranch: "main"},
		}}),
	)

	got := cat.Resolve(context.Background(), "org", e)
	if got.Spec.Availability != "repo" {
		t.Fatalf("availability: want repo, got %q", got.Spec.Availability)
	}
	if got.Spec.InlineContent != "" {
		t.Fatalf("repo availability must not carry inline content, got %q", got.Spec.InlineContent)
	}
	if got.Owner != "acme" || got.Repo != "hr" || got.Branch != "main" {
		t.Fatalf("repo coords: want acme/hr@main, got %s/%s@%s", got.Owner, got.Repo, got.Branch)
	}
	if got.Subdir != "services/employee" || got.Spec.Path != "services/employee" {
		t.Fatalf("subdir hint: got Subdir=%q Path=%q", got.Subdir, got.Spec.Path)
	}
}

// (d) BYO-image provider: no CR schema, no design bundle, no repo coords →
// availability=none.
func TestResolve_None(t *testing.T) {
	t.Parallel()
	e := openchoreo.WorkloadEndpointInfo{
		Project: "hr", Component: "byo", Name: "http", Type: "HTTP", Port: 8080,
		Visibility: []string{"namespace"},
	}
	cat := NewCatalog(fakeRC([]openchoreo.WorkloadEndpointInfo{e}))

	got := cat.Resolve(context.Background(), "org", e)
	if got.Spec.Availability != "none" {
		t.Fatalf("availability: want none, got %q", got.Spec.Availability)
	}
	if got.Owner != "" || got.Repo != "" || got.Spec.InlineContent != "" {
		t.Fatalf("none case must carry no coords/content, got %+v", got)
	}
}

func TestListResolved(t *testing.T) {
	t.Parallel()
	eps := []openchoreo.WorkloadEndpointInfo{
		{Project: "hr", Component: "employee-api", Name: "http", Type: "HTTP", Port: 8080,
			Visibility: []string{"namespace"}, SchemaContent: "openapi: 3.0.3\n"},
		{Project: "hr", Component: "byo", Name: "http", Type: "HTTP", Port: 8081},
	}
	cat := NewCatalog(fakeRC(eps))

	got, err := cat.ListResolved(context.Background(), "org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 resolved endpoints, got %d", len(got))
	}
	byComp := map[string]OrgComponentEndpoint{}
	for _, r := range got {
		byComp[r.Component] = r
	}
	if byComp["employee-api"].Spec.Availability != "inline" {
		t.Fatalf("employee-api: want inline, got %q", byComp["employee-api"].Spec.Availability)
	}
	if byComp["byo"].Spec.Availability != "none" {
		t.Fatalf("byo: want none, got %q", byComp["byo"].Spec.Availability)
	}
}

func TestListResolved_NilSafety(t *testing.T) {
	t.Parallel()
	var nilCatalog *Catalog
	got, err := nilCatalog.ListResolved(context.Background(), "org")
	if got != nil || err != nil {
		t.Fatalf("nil receiver: want (nil, nil), got (%v, %v)", got, err)
	}
}

// countingDesignReader tracks how many times ReadDesign was called per
// project — used to assert ListResolved's per-pass design-bundle memoization
// (a project publishing several endpoints must trigger at most one remote
// read per project for the whole pass).
type countingDesignReader struct {
	byProject map[string]*spec.DesignFile
	calls     map[string]int
}

func (f *countingDesignReader) ReadDesign(_ context.Context, _, projectID string) (*spec.DesignFile, error) {
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[projectID]++
	return f.byProject[projectID], nil
}

func TestListResolved_MemoizesDesignReadPerProject(t *testing.T) {
	t.Parallel()
	// Three endpoints owned by the same provider project ("hr") — a naive
	// per-endpoint resolve would read the design bundle 3 times in this pass.
	eps := []openchoreo.WorkloadEndpointInfo{
		{Project: "hr", Component: "employee-api", Name: "http", Type: "HTTP", Port: 8080, Visibility: []string{"namespace"}},
		{Project: "hr", Component: "payroll-api", Name: "http", Type: "HTTP", Port: 8081, Visibility: []string{"namespace"}},
		{Project: "hr", Component: "benefits-api", Name: "http", Type: "HTTP", Port: 8082, Visibility: []string{"namespace"}},
	}
	reader := &countingDesignReader{byProject: map[string]*spec.DesignFile{
		"hr": designWith(spec.DesignComponent{Name: "employee-api", OpenAPISpec: "openapi: 3.0.3\n"}),
	}}
	cat := NewCatalog(fakeRC(eps), WithDesignReader(reader))

	if _, err := cat.ListResolved(context.Background(), "org"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := reader.calls["hr"]; got != 1 {
		t.Fatalf("ReadDesign(hr): want 1 call across 3 endpoints in one ListResolved pass, got %d", got)
	}
}
