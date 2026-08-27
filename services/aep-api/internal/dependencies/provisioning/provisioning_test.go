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

package provisioning

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// ---- fakes -----------------------------------------------------------------

type fakeIssues struct {
	list     []sourcecontrol.IssueInfo
	created  []sourcecontrol.CreateIssueRequest
	closed   map[int]string
	comments map[int][]string
	nextNum  int
	// project records the project each CREATED issue belongs to so ListIssues can
	// filter by project (production lists issues per project repo). Seeded issues
	// carry no project and match every project — backward compatible with the
	// single-project tests.
	project map[int]string
	// raceNewIssues simulates GitHub's eventually-consistent label-filtered issue
	// LIST: when true, an issue just returned by CreateIssue is hidden from
	// ListIssues (as if the list index has not caught up yet). CreateIssue still
	// returns the real number — this is exactly the read-after-write race #164 hits.
	raceNewIssues  bool
	hiddenFromList map[int]bool
}

func newFakeIssues(seed []sourcecontrol.IssueInfo) *fakeIssues {
	max := 0
	for _, i := range seed {
		if i.Number > max {
			max = i.Number
		}
	}
	return &fakeIssues{list: seed, closed: map[int]string{}, comments: map[int][]string{}, nextNum: max + 1, project: map[int]string{}, hiddenFromList: map[int]bool{}}
}

func (f *fakeIssues) ListIssues(_ context.Context, _, projectID string, _ []string) ([]sourcecontrol.IssueInfo, error) {
	var out []sourcecontrol.IssueInfo
	for _, i := range f.list {
		if f.hiddenFromList[i.Number] {
			continue // eventually-consistent list has not caught up to this create yet
		}
		if p := f.project[i.Number]; p == "" || p == projectID {
			out = append(out, i)
		}
	}
	return out, nil
}
func (f *fakeIssues) CreateIssue(_ context.Context, _, projectID string, req sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error) {
	f.created = append(f.created, req)
	n := f.nextNum
	f.nextNum++
	f.list = append(f.list, sourcecontrol.IssueInfo{Number: n, Title: req.Title, Body: req.Body, State: "open", Labels: req.Labels})
	f.project[n] = projectID
	if f.raceNewIssues {
		f.hiddenFromList[n] = true
	}
	return &sourcecontrol.IssueResult{Number: n}, nil
}
func (f *fakeIssues) CloseIssue(_ context.Context, _, _ string, number int, comment string) error {
	f.closed[number] = comment
	for i := range f.list {
		if f.list[i].Number == number {
			f.list[i].State = "closed"
		}
	}
	return nil
}
func (f *fakeIssues) CommentIssue(_ context.Context, _, _ string, number int, body string) error {
	f.comments[number] = append(f.comments[number], body)
	return nil
}
func (f *fakeIssues) AddLabels(_ context.Context, _, _ string, number int, labels []string) error {
	for i := range f.list {
		if f.list[i].Number == number {
			f.list[i].Labels = append(f.list[i].Labels, labels...)
		}
	}
	return nil
}

type fakeExecStore struct {
	rows   []*delivery.Execution
	nextID int
}

func (f *fakeExecStore) TryAdmit(_ context.Context, e *delivery.Execution) (bool, *delivery.Execution, error) {
	for _, r := range f.rows {
		if r.Repo == e.Repo && r.IssueNumber == e.IssueNumber && r.Kind == e.Kind &&
			taskmeta.ExecutionStatus(r.Status).IsActive() {
			return false, r, nil
		}
	}
	f.nextID++
	e.ID = fmt.Sprintf("exec-%d", f.nextID)
	f.rows = append(f.rows, e)
	return true, e, nil
}
func (f *fakeExecStore) StartWithRun(_ context.Context, id, runName string) (*delivery.Execution, error) {
	for _, r := range f.rows {
		if r.ID == id {
			r.Status = string(taskmeta.ExecRunning)
			r.RunName = runName
			now := time.Unix(1000, 0)
			r.StartedAt = &now
			return r, nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (f *fakeExecStore) Finish(_ context.Context, id, status, reason string) (*delivery.Execution, error) {
	for _, r := range f.rows {
		if r.ID == id {
			if !taskmeta.ExecutionStatus(r.Status).IsActive() {
				return nil, nil // loser — already terminal
			}
			r.Status = status
			r.Reason = reason
			return r, nil
		}
	}
	return nil, nil
}
func (f *fakeExecStore) ListActive(_ context.Context) ([]delivery.Execution, error) {
	var out []delivery.Execution
	for _, r := range f.rows {
		if taskmeta.ExecutionStatus(r.Status).IsActive() {
			out = append(out, *r)
		}
	}
	return out, nil
}

type fakeDesign struct {
	comps []spec.DesignComponent
	err   error
}

func (f fakeDesign) ReadDesignComponents(context.Context, string, string) ([]spec.DesignComponent, error) {
	return f.comps, f.err
}

type countingDesign struct {
	comps []spec.DesignComponent
	err   error
	calls int
}

func (f *countingDesign) ReadDesignComponents(context.Context, string, string) ([]spec.DesignComponent, error) {
	f.calls++
	return f.comps, f.err
}

type fakeRepos struct{}

func (fakeRepos) RepoFullName(context.Context, string, string) (string, error) { return "o/r", nil }
func (fakeRepos) ByFullName(context.Context, string) (string, string, error) {
	return "acme", "warehouse", nil
}

// fakeRTCatalog fakes ExternalRTCatalog — the OC-RT-backed org-settings
// list+delete surface (Task 5) — with RT fixtures in place of DB rows.
type fakeRTCatalog struct {
	defs    []openchoreo.ExternalResourceDefinition
	deleted []string
	listErr error
	delErr  error
}

func (f *fakeRTCatalog) List(_ context.Context, _ string) ([]openchoreo.ExternalResourceDefinition, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.defs, nil
}

func (f *fakeRTCatalog) Ensure(_ context.Context, _ string, rt *openchoreo.ResourceType) error {
	if rt == nil {
		return nil
	}
	def, ok := openchoreo.ExternalDefinitionFromRT(rt)
	if !ok {
		return nil
	}
	for i, existing := range f.defs {
		if strings.EqualFold(existing.Name, def.Name) {
			f.defs[i] = def
			return nil
		}
	}
	f.defs = append(f.defs, def)
	return nil
}

func (f *fakeRTCatalog) Update(_ context.Context, _ string, rt *openchoreo.ResourceType) error {
	if rt == nil {
		return fmt.Errorf("fakeRTCatalog.Update: nil ResourceType")
	}
	def, ok := openchoreo.ExternalDefinitionFromRT(rt)
	if !ok {
		return fmt.Errorf("fakeRTCatalog.Update: not an external ResourceType")
	}
	for i, existing := range f.defs {
		if strings.EqualFold(existing.Name, def.Name) {
			f.defs[i] = def
			return nil
		}
	}
	return fmt.Errorf("fakeRTCatalog.Update: %s not found", def.Name)
}

func (f *fakeRTCatalog) Delete(_ context.Context, _, name string) error {
	if f.delErr != nil {
		return f.delErr
	}
	f.deleted = append(f.deleted, name)
	return nil
}

type fakeExtProv struct {
	calls         int
	byEnv         map[string]dependencies.EnvValues
	result        *dependencies.ProvisionResult
	err           error
	deprovisioned []string
	// lastER is the *dependencies.ExternalResource the last Provision call received —
	// lets a test assert the RT-authoring definition was built from the design
	// (name/description/config schema) rather than fetched from the catalog.
	lastER *dependencies.ExternalResource

	// AuthorPreparedValues spies (the build path's no-SM-write author half).
	authorPreparedCalls int
	authorByEnv         map[string]dependencies.PreparedEnvValues
	authorResult        *dependencies.ProvisionResult
	authorErr           error
	// authorLastER is the *dependencies.ExternalResource the last AuthorPreparedValues
	// call received — same purpose as lastER, for the build author path.
	authorLastER *dependencies.ExternalResource
}

func (f *fakeExtProv) Provision(_ context.Context, _, _, _ string, er *dependencies.ExternalResource, byEnv map[string]dependencies.EnvValues) (*dependencies.ProvisionResult, error) {
	f.calls++
	f.byEnv = byEnv
	f.lastER = er
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &dependencies.ProvisionResult{ResourceName: "o-ext", BindingByEnv: map[string]string{"development": "o-ext-development"}}, nil
}
func (f *fakeExtProv) AuthorPreparedValues(_ context.Context, _, _ string, er *dependencies.ExternalResource, byEnv map[string]dependencies.PreparedEnvValues) (*dependencies.ProvisionResult, error) {
	f.authorPreparedCalls++
	f.authorByEnv = byEnv
	f.authorLastER = er
	if f.authorErr != nil {
		return nil, f.authorErr
	}
	if f.authorResult != nil {
		return f.authorResult, nil
	}
	return &dependencies.ProvisionResult{ResourceName: "o-ext", BindingByEnv: map[string]string{"development": "o-ext-development"}}, nil
}
func (f *fakeExtProv) Deprovision(_ context.Context, _, _, name string, _ []string) error {
	f.deprovisioned = append(f.deprovisioned, name)
	return nil
}
func (f *fakeExtProv) ResolveRunnerSecrets(_ context.Context, _, _, _ string, names []string) ([]dependencies.ExternalResourceRunnerSecret, error) {
	out := make([]dependencies.ExternalResourceRunnerSecret, 0, len(names))
	for _, n := range names {
		out = append(out, dependencies.ExternalResourceRunnerSecret{KVPath: "vault/" + n, Keys: []string{"API_KEY"}})
	}
	return out, nil
}

type fakePlatProv struct {
	calls         int
	params        map[string]any
	result        *dependencies.PlatformProvisionResult
	err           error
	deprovisioned []string
}

func (f *fakePlatProv) Provision(_ context.Context, _, _, depName, _ string, params map[string]any, _ []string) (*dependencies.PlatformProvisionResult, error) {
	f.calls++
	f.params = params
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &dependencies.PlatformProvisionResult{ResourceName: "o-" + depName, BindingByEnv: map[string]string{"development": "o-" + depName + "-development"}}, nil
}
func (f *fakePlatProv) Deprovision(_ context.Context, _, _, depName string, _ []string) error {
	f.deprovisioned = append(f.deprovisioned, depName)
	return nil
}

type fakeBindings struct {
	byName map[string]*openchoreo.ResourceReleaseBinding
}

func (f *fakeBindings) GetBinding(_ context.Context, _, name string) (*openchoreo.ResourceReleaseBinding, error) {
	return f.byName[name], nil
}

type fakeProjects struct{ refs []ProjectRef }

func (f fakeProjects) ListProjects(_ context.Context, orgID string) ([]ProjectRef, error) {
	var out []ProjectRef
	for _, r := range f.refs {
		if r.OrgID == orgID {
			out = append(out, r)
		}
	}
	return out, nil
}

type fakeProviders struct {
	byName map[string]openchoreo.WorkloadEndpointInfo
	// nsVisible / projectEP back the two visibility-scoped resolves the ADR-0004
	// wiring comment uses; byName is the any-visibility access-request lookup.
	nsVisible map[string]openchoreo.WorkloadEndpointInfo
	projectEP map[string]openchoreo.WorkloadEndpointInfo
}

func (f fakeProviders) FindByComponent(_ context.Context, _, name string) (openchoreo.WorkloadEndpointInfo, bool, error) {
	ep, ok := f.byName[name]
	return ep, ok, nil
}
func (f fakeProviders) ResolveNamespaceVisible(_ context.Context, _, name string) (openchoreo.WorkloadEndpointInfo, bool, error) {
	ep, ok := f.nsVisible[name]
	return ep, ok, nil
}
func (f fakeProviders) ResolveProjectEndpoint(_ context.Context, _, _, ocComponent string) (openchoreo.WorkloadEndpointInfo, bool, error) {
	ep, ok := f.projectEP[ocComponent]
	return ep, ok, nil
}

type fakeAccess struct {
	rows   []*dependencies.AccessRequest
	nextID int
}

func (f *fakeAccess) Create(_ context.Context, ar *dependencies.AccessRequest) error {
	f.nextID++
	ar.ID = fmt.Sprintf("ar-%d", f.nextID)
	f.rows = append(f.rows, ar)
	return nil
}
func (f *fakeAccess) ListByConsumerProject(_ context.Context, orgID, projectID string) ([]dependencies.AccessRequest, error) {
	var out []dependencies.AccessRequest
	for _, r := range f.rows {
		if r.OrgID == orgID && r.ConsumerProjectID == projectID {
			out = append(out, *r)
		}
	}
	return out, nil
}
func (f *fakeAccess) FindOpenForTarget(_ context.Context, orgID, providerProjectID, providerComponent string) (*dependencies.AccessRequest, error) {
	for _, r := range f.rows {
		if r.OrgID == orgID && r.ProviderProjectID == providerProjectID && r.ProviderComponentName == providerComponent &&
			r.Status != dependencies.AccessRequestStatusGranted && r.Status != dependencies.AccessRequestStatusRejected {
			return r, nil
		}
	}
	return nil, nil
}
func (f *fakeAccess) UpdateStatus(_ context.Context, id, status string) error {
	for _, r := range f.rows {
		if r.ID == id {
			r.Status = status
		}
	}
	return nil
}
func (f *fakeAccess) ListByProviderTask(_ context.Context, providerTaskID string) ([]dependencies.AccessRequest, error) {
	var out []dependencies.AccessRequest
	for _, r := range f.rows {
		if r.ProviderTaskID == providerTaskID {
			out = append(out, *r)
		}
	}
	return out, nil
}

func TestOnIssueClosed_RejectsUngrantedRidersOnDecline(t *testing.T) {
	access := &fakeAccess{}
	ctx := context.Background()
	// Two consumers rode the same warehouse#12 org-publish gate issue: one still
	// pending, one already granted (an earlier partial grant).
	_ = access.Create(ctx, &dependencies.AccessRequest{OrgID: "acme", ProviderProjectID: "warehouse",
		ProviderTaskID: providerTaskKey("warehouse", 12), Status: dependencies.AccessRequestStatusRequested})
	_ = access.Create(ctx, &dependencies.AccessRequest{OrgID: "acme", ProviderProjectID: "warehouse",
		ProviderTaskID: providerTaskKey("warehouse", 12), Status: dependencies.AccessRequestStatusGranted})
	svc := NewService(Deps{Access: access, Repos: fakeRepos{}})

	// Provider manually closes the gate issue → decline.
	payload := []byte(`{"issue":{"number":12},"repository":{"full_name":"asdlc-repos/warehouse"}}`)
	if err := svc.OnIssueClosed(ctx, "issues", "closed", payload); err != nil {
		t.Fatalf("OnIssueClosed: %v", err)
	}
	if access.rows[0].Status != dependencies.AccessRequestStatusRejected {
		t.Fatalf("pending rider must flip to rejected, got %q", access.rows[0].Status)
	}
	if access.rows[1].Status != dependencies.AccessRequestStatusGranted {
		t.Fatalf("already-granted rider must stay granted, got %q", access.rows[1].Status)
	}
}

func TestOnIssueClosed_NonOrgPublishIssueIsNoop(t *testing.T) {
	access := &fakeAccess{}
	svc := NewService(Deps{Access: access, Repos: fakeRepos{}})
	// No access request keys to issue #99 (a routine coding-issue close) → no-op.
	payload := []byte(`{"issue":{"number":99},"repository":{"full_name":"asdlc-repos/warehouse"}}`)
	if err := svc.OnIssueClosed(context.Background(), "issues", "closed", payload); err != nil {
		t.Fatalf("OnIssueClosed on a non-org-publish issue must be a silent no-op, got %v", err)
	}
}

func readyBinding(outputs ...string) *openchoreo.ResourceReleaseBinding {
	st := &openchoreo.ResourceReleaseBindingStatus{
		Conditions: []openchoreo.OCCondition{{Type: "Ready", Status: "True"}},
	}
	for _, o := range outputs {
		st.Outputs = append(st.Outputs, openchoreo.ResolvedOutput{Name: o})
	}
	return &openchoreo.ResourceReleaseBinding{Status: st}
}

// ---- fixtures --------------------------------------------------------------

func designWithDeps() []spec.DesignComponent {
	return []spec.DesignComponent{{
		Name: "orders",
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "stripe", Config: []spec.ConfigKey{
				{Key: "api_key", Secret: true}, {Key: "region"},
			}},
			{Kind: spec.DependencyKindPlatformResource, Name: "orders-db", ResourceType: "postgres-cnpg", Parameters: map[string]any{"size": "small"}},
		},
	}}
}

func newTestService(issues *fakeIssues, execs *fakeExecStore, design DesignReader, ext *fakeExtProv, plat *fakePlatProv, bindings *fakeBindings) *Service {
	return NewService(Deps{
		Issues:   issues,
		Execs:    execs,
		Design:   design,
		Repos:    fakeRepos{},
		ExtProv:  ext,
		PlatProv: plat,
		Bindings: bindings,
	})
}

// ---- tests -----------------------------------------------------------------

func TestEnsureProvisionIssues_MintsPerDepDeduped(t *testing.T) {
	issues := newFakeIssues(nil)
	svc := newTestService(issues, &fakeExecStore{}, fakeDesign{comps: designWithDeps()}, &fakeExtProv{}, &fakePlatProv{}, &fakeBindings{})

	gateByDep, err := svc.EnsureProvisionIssues(context.Background(), "org", "proj", "v1-1", 0)
	if err != nil {
		t.Fatalf("EnsureProvisionIssues: %v", err)
	}
	if len(issues.created) != 1 {
		t.Fatalf("want 1 platform-resource gate issue, got %d", len(issues.created))
	}
	// The returned map carries the minted gate number for each distinct dep — the
	// read-your-write the build path threads past the racy list.
	if gateByDep["stripe"] != 0 || gateByDep["orders-db"] == 0 {
		t.Fatalf("EnsureProvisionIssues must return the minted gate number per dep, got %+v", gateByDep)
	}
	// A gate is PROSE plus two labels: the `provision` kind and the
	// aep:dep/<slug> that keys it to its dependency. It never carries the `aep`
	// working-set label — it is a hold on dispatch, never agent work — and its
	// body carries no machine block, because nothing parses it.
	var deps []string
	for _, req := range issues.created {
		if !contains(req.Labels, delivery.KindProvision) {
			t.Errorf("gate issue missing the provision kind: %v", req.Labels)
		}
		if contains(req.Labels, delivery.LabelAgentWork) {
			t.Errorf("a gate must never be agent work: %v", req.Labels)
		}
		if strings.Contains(req.Body, "aep:task/v1") {
			t.Errorf("gate issue body still carries a machine block:\n%s", req.Body)
		}
		deps = append(deps, gateDepFromLabels(req.Labels))
	}
	sort.Strings(deps)
	if !reflect.DeepEqual(deps, []string{"orders-db"}) {
		t.Fatalf("gate dep labels = %v, want [orders-db]", deps)
	}

	// Idempotent: a second call mints nothing new (the deps already have open issues).
	issues.created = nil
	gateByDep2, err := svc.EnsureProvisionIssues(context.Background(), "org", "proj", "v1-1", 0)
	if err != nil {
		t.Fatalf("EnsureProvisionIssues #2: %v", err)
	}
	if len(issues.created) != 0 {
		t.Fatalf("second mint must be a no-op, created %d", len(issues.created))
	}
	// The map still resolves the pre-existing open gates (from openProvisionDeps).
	if gateByDep2["stripe"] != 0 || gateByDep2["orders-db"] == 0 {
		t.Fatalf("second call must still return the existing gate numbers, got %+v", gateByDep2)
	}
}

// A gate must land IN the version's milestone, or the run's dispatch predicate
// ("no open `provision` issue in this milestone") can never see the hold. The
// number rides the CREATE — one call, no follow-up PATCH. A gate deliberately
// does not carry the `aep` working-set label: it is a dispatch hold, never
// agent work.
func TestEnsureProvisionIssues_AssignsTheMilestoneAtCreation(t *testing.T) {
	issues := newFakeIssues(nil)
	svc := newTestService(issues, &fakeExecStore{}, fakeDesign{comps: designWithDeps()}, &fakeExtProv{}, &fakePlatProv{}, &fakeBindings{})

	if _, err := svc.EnsureProvisionIssues(context.Background(), "org", "proj", "v4", 7); err != nil {
		t.Fatalf("EnsureProvisionIssues: %v", err)
	}
	if len(issues.created) == 0 {
		t.Fatal("no gate issue was minted")
	}
	for _, req := range issues.created {
		if req.Milestone == nil || *req.Milestone != 7 {
			t.Errorf("gate %q: milestone = %v, want 7 assigned at creation", req.Title, req.Milestone)
		}
		if contains(req.Labels, "aep") {
			t.Errorf("gate %q carries the aep working-set label: %v — a gate is never agent work", req.Title, req.Labels)
		}
	}
}

func TestSaveValues_ProvisionsAndClosesGate(t *testing.T) {
	gate := provisionGateIssue(10, "stripe")
	issues := newFakeIssues([]sourcecontrol.IssueInfo{gate})
	execs := &fakeExecStore{}
	ext := &fakeExtProv{}
	// Secret-vs-plain classification comes from the project's committed design
	// (dep.Config), never a catalog: SaveValues reads the design to split
	// api_key → secret, region → plain (see designWithDeps).
	svc := newTestService(issues, execs, fakeDesign{comps: designWithDeps()}, ext, &fakePlatProv{}, &fakeBindings{})

	err := svc.SaveValues(context.Background(), "org", "org", "proj", "stripe", map[string]map[string]string{
		"development": {"api_key": "sk_live_x", "region": "us"},
	})
	if err != nil {
		t.Fatalf("SaveValues: %v", err)
	}
	if ext.calls != 1 {
		t.Fatalf("external provisioner must be called once, got %d", ext.calls)
	}
	// Values split by schema: api_key → secret, region → plain. No secret leaks
	// into the plain map.
	ev := ext.byEnv["development"]
	if ev.Secret["api_key"] != "sk_live_x" || ev.Plain["region"] != "us" {
		t.Fatalf("split-by-schema wrong: %+v", ev)
	}
	if _, leaked := ev.Plain["api_key"]; leaked {
		t.Fatalf("secret api_key leaked into the plain map")
	}
	// The gate issue is closed (consumers release via gate-close webhook + sweep).
	if _, closed := issues.closed[10]; !closed {
		t.Fatalf("gate issue must be closed after external provisioning")
	}
	// The provision Execution derives deployed (succeeded provision run).
	if r := latestProvisionRow(execs); r == nil || r.Status != string(taskmeta.ExecSucceeded) {
		t.Fatalf("want a succeeded provision run, got %+v", r)
	}
	// No secret value appears in the close comment.
	if strings.Contains(issues.closed[10], "sk_live_x") {
		t.Fatalf("secret leaked into the close comment: %q", issues.closed[10])
	}
}

func TestProvision_PlatformIsAsync_LeftRunning(t *testing.T) {
	gate := provisionGateIssue(11, "orders-db")
	issues := newFakeIssues([]sourcecontrol.IssueInfo{gate})
	execs := &fakeExecStore{}
	plat := &fakePlatProv{}
	svc := newTestService(issues, execs, fakeDesign{comps: designWithDeps()}, &fakeExtProv{}, plat, &fakeBindings{})

	if err := svc.Provision(context.Background(), "org", "proj", "orders-db", nil, nil); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if plat.calls != 1 {
		t.Fatalf("platform provisioner must be called once, got %d", plat.calls)
	}
	// Design parameters flow through (size=small).
	if plat.params["size"] != "small" {
		t.Fatalf("design params must reach the provisioner, got %+v", plat.params)
	}
	// Async: the run is RUNNING (not finished) and the gate stays OPEN — the
	// watcher completes it.
	r := latestProvisionRow(execs)
	if r == nil || r.Status != string(taskmeta.ExecRunning) {
		t.Fatalf("platform provision run must be left running, got %+v", r)
	}
	if r.RunName != "o-orders-db-development" {
		t.Fatalf("run must pin the development binding name, got %q", r.RunName)
	}
	if _, closed := issues.closed[11]; closed {
		t.Fatalf("gate issue must stay open until the binding is ready")
	}
}

func TestResourceWatcher_ReadyClosesGateAndReleases(t *testing.T) {
	gate := provisionGateIssue(11, "orders-db")
	issues := newFakeIssues([]sourcecontrol.IssueInfo{gate})
	execs := &fakeExecStore{}
	bindings := &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{}}
	plat := &fakePlatProv{}
	svc := newTestService(issues, execs, fakeDesign{comps: designWithDeps()}, &fakeExtProv{}, plat, bindings)

	// Provision (async) → running row pinned to the binding.
	if err := svc.Provision(context.Background(), "org", "proj", "orders-db", nil, nil); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	w := NewResourceWatcher(svc, nil, time.Second)
	// Pin the clock near StartedAt (Unix 1000) so the stale bound does not fire.
	w.now = func() time.Time { return time.Unix(1000, 0).Add(time.Minute) }

	// Binding not ready yet → watcher waits (run stays running, gate open).
	bindings.byName["o-orders-db-development"] = &openchoreo.ResourceReleaseBinding{}
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep (not ready): %v", err)
	}
	if r := latestProvisionRow(execs); r.Status != string(taskmeta.ExecRunning) {
		t.Fatalf("run must stay running while binding not ready, got %s", r.Status)
	}

	// Binding goes Ready → watcher finishes the run, closes the gate (webhook + sweep release consumers).
	bindings.byName["o-orders-db-development"] = readyBinding("host", "port")
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep (ready): %v", err)
	}
	if r := latestProvisionRow(execs); r == nil || r.Status != string(taskmeta.ExecSucceeded) {
		t.Fatalf("ready binding must finish the run succeeded, got %+v", r)
	}
	if _, closed := issues.closed[11]; !closed {
		t.Fatalf("gate issue must be closed once the binding is ready")
	}
}

func TestResourceWatcher_StaleFails(t *testing.T) {
	gate := provisionGateIssue(11, "orders-db")
	issues := newFakeIssues([]sourcecontrol.IssueInfo{gate})
	execs := &fakeExecStore{}
	bindings := &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{"o-orders-db-development": {}}}
	svc := newTestService(issues, execs, fakeDesign{comps: designWithDeps()}, &fakeExtProv{}, &fakePlatProv{}, bindings)
	if err := svc.Provision(context.Background(), "org", "proj", "orders-db", nil, nil); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	w := NewResourceWatcher(svc, nil, time.Second)
	// Jump the clock past the stale window (StartedAt was stamped at Unix 1000).
	w.now = func() time.Time { return time.Unix(1000, 0).Add(31 * time.Minute) }
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if r := latestProvisionRow(execs); r == nil || r.Status != string(taskmeta.ExecFailed) {
		t.Fatalf("a stale provision run must be failed, got %+v", r)
	}
}

func TestStatus_MasksOutputsToNames(t *testing.T) {
	bindings := &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{
		"proj-orders-db-development": readyBinding("host", "port"),
	}}
	svc := newTestService(newFakeIssues(nil), &fakeExecStore{}, fakeDesign{}, &fakeExtProv{}, &fakePlatProv{}, bindings)
	st, err := svc.Status(context.Background(), "org", "proj", "orders-db", "")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Ready || st.Status != "ready" {
		t.Fatalf("want ready, got %+v", st)
	}
	if len(st.Outputs) != 2 || st.Outputs[0] != "host" {
		t.Fatalf("outputs must be the masked names, got %+v", st.Outputs)
	}
}

func TestDeleteExternalResource_InUse409(t *testing.T) {
	// Consumers are scanned from committed designs (component_tasks is gone):
	// project "proj" component "orders" declares external dep "stripe".
	rtCatalog := &fakeRTCatalog{}
	svc := NewService(Deps{
		Issues:    newFakeIssues(nil),
		Execs:     &fakeExecStore{},
		Design:    fakeDesign{comps: designWithDeps()},
		Repos:     fakeRepos{},
		RTCatalog: rtCatalog,
		Projects:  fakeProjects{refs: []ProjectRef{{OrgID: "org", ProjectID: "proj"}}},
	})
	if err := svc.DeleteExternalResource(context.Background(), "org", "stripe"); err != ErrExternalResourceInUse {
		t.Fatalf("in-use delete must return ErrExternalResourceInUse, got %v", err)
	}
	if len(rtCatalog.deleted) != 0 {
		t.Fatalf("an in-use resource must not delete any ResourceType, got %v", rtCatalog.deleted)
	}
	// No consumers → delete proceeds — the OC ResourceType delete is called.
	if err := svc.DeleteExternalResource(context.Background(), "org", "unused"); err != nil {
		t.Fatalf("delete of unused resource: %v", err)
	}
	if len(rtCatalog.deleted) != 1 || rtCatalog.deleted[0] != "unused" {
		t.Fatalf("unused resource's ResourceType must be deleted, got %v", rtCatalog.deleted)
	}
}

// ListExternalResources sources each entry's name/description/config schema
// from the RT catalog (Task 5's ExternalRTCatalog — RT reconstruction, see
// resources.ExternalResourceCatalog.List) and merges consumers from the SAME
// design-sweep DeleteExternalResource's in-use guard uses.
func TestListExternalResources_FromRT(t *testing.T) {
	rtCatalog := &fakeRTCatalog{defs: []openchoreo.ExternalResourceDefinition{
		{Name: "stripe", Description: "payments", Config: []openchoreo.ExternalResourceConfigKey{
			{Key: "api_key", Secret: true}, {Key: "region"},
		}},
	}}
	svc := NewService(Deps{
		Design:    fakeDesign{comps: designWithDeps()},
		RTCatalog: rtCatalog,
		Projects:  fakeProjects{refs: []ProjectRef{{OrgID: "org", ProjectID: "proj"}}},
	})
	views, err := svc.ListExternalResources(context.Background(), "org")
	if err != nil {
		t.Fatalf("ListExternalResources: %v", err)
	}
	if len(views) != 1 || views[0].Name != "stripe" || views[0].Description != "payments" {
		t.Fatalf("views = %+v, want the RT-sourced stripe entry", views)
	}
	if len(views[0].Config) != 2 || views[0].Config[0].Key != "api_key" || !views[0].Config[0].Secret {
		t.Errorf("config schema = %+v, want the RT-reconstructed schema", views[0].Config)
	}
	if len(views[0].Consumers) != 1 || views[0].Consumers[0].ProjectID != "proj" || views[0].Consumers[0].ComponentName != "orders" {
		t.Errorf("consumers = %+v, want the design-scanned orders consumer", views[0].Consumers)
	}
}

func TestRequestAccess_CreatesRequestAndProviderIssue(t *testing.T) {
	consumer := spec.DesignComponent{Name: "web", Dependencies: []spec.Dependency{
		{Kind: spec.DependencyKindOrgService, Name: "inventory"},
	}}
	issues := newFakeIssues(nil)
	access := &fakeAccess{}
	providers := fakeProviders{byName: map[string]openchoreo.WorkloadEndpointInfo{
		"inventory": {Project: "warehouse", Component: "warehouse-inventory", Name: "http"},
	}}
	svc := NewService(Deps{
		Issues:    issues,
		Execs:     &fakeExecStore{},
		Design:    fakeDesign{comps: []spec.DesignComponent{consumer}},
		Repos:     fakeRepos{},
		Access:    access,
		Providers: providers,
	})

	ar, err := svc.RequestAccess(context.Background(), "org", "storefront", "web", "inventory")
	if err != nil {
		t.Fatalf("RequestAccess: %v", err)
	}
	if ar.Status != dependencies.AccessRequestStatusRequested {
		t.Fatalf("new access request must be 'requested', got %q", ar.Status)
	}
	if ar.ProviderProjectID != "warehouse" || ar.ProviderComponentName != "inventory" {
		t.Fatalf("provider resolution wrong: %+v", ar)
	}
	// A provider-side org-publish gate issue was created on the provider project.
	if len(issues.created) != 1 {
		t.Fatalf("want one provider org-publish issue, got %d", len(issues.created))
	}
	if got := gateDepFromLabels(issues.created[0].Labels); got != "inventory" {
		t.Fatalf("org-publish gate dep label = %q, want inventory", got)
	}
	if !contains(issues.created[0].Labels, delivery.KindProvision) {
		t.Fatalf("org-publish gate missing the provision kind: %v", issues.created[0].Labels)
	}
	if ar.ProviderIssueNumber == 0 {
		t.Fatalf("access request must link the provider issue number")
	}

	// A second consumer of the same provider rides the SAME issue (dedup).
	issues.created = nil
	ar2, err := svc.RequestAccess(context.Background(), "org", "another", "api", "inventory")
	if err != nil {
		t.Fatalf("RequestAccess #2: %v", err)
	}
	if len(issues.created) != 0 {
		t.Fatalf("second consumer must ride the existing issue, created %d", len(issues.created))
	}
	if ar2.ProviderIssueNumber != ar.ProviderIssueNumber {
		t.Fatalf("second request must reference the same provider issue")
	}
}

func TestGrant_OnProviderDeploy(t *testing.T) {
	issues := newFakeIssues([]sourcecontrol.IssueInfo{{Number: 20, State: "open", Title: "publish"}})
	access := &fakeAccess{}
	// Two riders on the same provider issue, both pending.
	pk := providerTaskKey("warehouse", 20)
	_ = access.Create(context.Background(), &dependencies.AccessRequest{
		OrgID: "org", ConsumerProjectID: "storefront", ProviderProjectID: "warehouse",
		ProviderComponentName: "inventory", ProviderTaskID: pk, ProviderIssueNumber: 20,
		Status: dependencies.AccessRequestStatusRequested,
	})
	_ = access.Create(context.Background(), &dependencies.AccessRequest{
		OrgID: "org", ConsumerProjectID: "another", ProviderProjectID: "warehouse",
		ProviderComponentName: "inventory", ProviderTaskID: pk, ProviderIssueNumber: 20,
		Status: dependencies.AccessRequestStatusRequested,
	})
	svc := NewService(Deps{Issues: issues, Execs: &fakeExecStore{}, Access: access, Repos: fakeRepos{}})

	// The provider component deploys → grant all riders + close the issue.
	if err := svc.OnComponentDeployed(context.Background(), "org", "warehouse", "inventory"); err != nil {
		t.Fatalf("OnComponentDeployed: %v", err)
	}
	for _, r := range access.rows {
		if r.Status != dependencies.AccessRequestStatusGranted {
			t.Fatalf("all riders must be granted, got %q", r.Status)
		}
	}
	if _, closed := issues.closed[20]; !closed {
		t.Fatalf("provider org-publish issue must be closed on grant")
	}
	// A deploy with no pending access is a no-op (does not error).
	if err := svc.OnComponentDeployed(context.Background(), "org", "warehouse", "other"); err != nil {
		t.Fatalf("no-op deploy grant: %v", err)
	}
}

func TestResolveComponentRunnerSecrets(t *testing.T) {
	ext := &fakeExtProv{}
	svc := NewService(Deps{
		Issues:  newFakeIssues(nil),
		Execs:   &fakeExecStore{},
		Design:  fakeDesign{comps: designWithDeps()}, // orders has external "stripe"
		Repos:   fakeRepos{},
		ExtProv: ext,
	})
	srs, err := svc.ResolveComponentRunnerSecrets(context.Background(), "org", "proj", "orders", "")
	if err != nil {
		t.Fatalf("ResolveComponentRunnerSecrets: %v", err)
	}
	// Only the external dep (stripe) yields a runner secret; the platform-resource
	// dep (orders-db) does not.
	if len(srs) != 1 || srs[0].KVPath != "vault/stripe" {
		t.Fatalf("want one runner secret for stripe, got %+v", srs)
	}
	// A component with no external deps yields none.
	none, err := svc.ResolveComponentRunnerSecrets(context.Background(), "org", "proj", "nonexistent", "")
	if err != nil || len(none) != 0 {
		t.Fatalf("unknown component: want no secrets, got %+v (err %v)", none, err)
	}
}

func TestDeprovisionProject_TearsDownResources(t *testing.T) {
	ext := &fakeExtProv{}
	plat := &fakePlatProv{}
	svc := NewService(Deps{
		Issues:   newFakeIssues(nil),
		Execs:    &fakeExecStore{},
		Design:   fakeDesign{comps: designWithDeps()},
		Repos:    fakeRepos{},
		ExtProv:  ext,
		PlatProv: plat,
	})
	if err := svc.DeprovisionProject(context.Background(), "org", "proj"); err != nil {
		t.Fatalf("DeprovisionProject: %v", err)
	}
	if len(ext.deprovisioned) != 1 || ext.deprovisioned[0] != "stripe" {
		t.Fatalf("external dep must be deprovisioned, got %v", ext.deprovisioned)
	}
	if len(plat.deprovisioned) != 1 || plat.deprovisioned[0] != "orders-db" {
		t.Fatalf("platform-resource dep must be deprovisioned, got %v", plat.deprovisioned)
	}
}

func TestSaveValues_WrongKind400(t *testing.T) {
	// stripe is external; asking to provision it as a platform resource is wrong-kind.
	svc := newTestService(newFakeIssues(nil), &fakeExecStore{}, fakeDesign{comps: designWithDeps()}, &fakeExtProv{}, &fakePlatProv{}, &fakeBindings{})
	err := svc.Provision(context.Background(), "org", "proj", "stripe", nil, nil)
	if err == nil || !strings.Contains(err.Error(), dependencies.ErrDepWrongKind.Error()) {
		t.Fatalf("provisioning an external dep as a resource must be wrong-kind, got %v", err)
	}
}

// ---- helpers ---------------------------------------------------------------

// provisionGateIssue builds a seeded gate issue exactly as the platform mints
// one: prose, the `provision` kind, and the aep:dep/<slug> label that keys
// it to its dependency. That label pair IS the index — nothing reads the body.
func provisionGateIssue(number int, depName string) sourcecontrol.IssueInfo {
	return sourcecontrol.IssueInfo{
		Number: number,
		Title:  "Provision " + depName,
		Body:   "Provide this dependency's values in the architecture drawer.",
		State:  "open",
		Labels: gateLabels(depName),
	}
}

func latestProvisionRow(execs *fakeExecStore) *delivery.Execution {
	var last *delivery.Execution
	for _, r := range execs.rows {
		if r.Kind == string(taskmeta.KindProvision) {
			last = r
		}
	}
	return last
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// TestSaveValues_DesignReadErrorFails: a design-read failure must fail the
// call, NOT fall through to classifying every submitted value as plain for
// lack of a schema (which would leak a secret into a ConfigMap). The external
// provisioner must never be called.
func TestSaveValues_DesignReadErrorFails(t *testing.T) {
	ext := &fakeExtProv{}
	svc := newTestService(newFakeIssues(nil), &fakeExecStore{},
		fakeDesign{err: fmt.Errorf("boom")}, ext, &fakePlatProv{}, &fakeBindings{})

	err := svc.SaveValues(context.Background(), "org", "org", "proj", "stripe",
		map[string]map[string]string{"development": {"api_key": "sk_live_x"}})
	if err == nil {
		t.Fatal("SaveValues must return an error when the design cannot be read")
	}
	if ext.calls != 0 {
		t.Fatalf("external provisioner must NOT be called on a design-read failure, got %d", ext.calls)
	}
}

// TestSaveValues_UnionSecretAcrossComponents: when two components declare the
// same external dependency and only ONE marks a key secret, the value must be
// classified secret (secret wins across the union) — regardless of which
// component the design scan lists first. The component that declares api_key
// PLAIN is listed FIRST here, so a first-component-wins classification would
// leak the value into the plain map.
func TestSaveValues_UnionSecretAcrossComponents(t *testing.T) {
	gate := provisionGateIssue(10, "stripe")
	ext := &fakeExtProv{}
	comps := []spec.DesignComponent{
		{Name: "webhook-worker", Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "stripe", Config: []spec.ConfigKey{{Key: "api_key"}}}, // plain, first
		}},
		{Name: "checkout", Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "stripe", Config: []spec.ConfigKey{{Key: "api_key", Secret: true}}}, // secret
		}},
	}
	svc := newTestService(newFakeIssues([]sourcecontrol.IssueInfo{gate}), &fakeExecStore{},
		fakeDesign{comps: comps}, ext, &fakePlatProv{}, &fakeBindings{})

	if err := svc.SaveValues(context.Background(), "org", "org", "proj", "stripe",
		map[string]map[string]string{"development": {"api_key": "sk_live_x"}}); err != nil {
		t.Fatalf("SaveValues: %v", err)
	}
	if ext.calls != 1 {
		t.Fatalf("external provisioner must be called once, got %d", ext.calls)
	}
	ev := ext.byEnv["development"]
	if ev.Secret["api_key"] != "sk_live_x" {
		t.Fatalf("api_key must be classified SECRET via the union (secret wins); got secret=%v plain=%v", ev.Secret, ev.Plain)
	}
	if _, leaked := ev.Plain["api_key"]; leaked {
		t.Fatal("api_key leaked into the plain map despite being secret in one component")
	}
}

// TestSaveValues_AuthorsDefinitionFromDesign proves the RT-authoring definition
// (name + description + config schema) is built off the DESIGN, not the
// external_resources table: with an EMPTY catalog (the old code returned
// ErrNotRegistered here), SaveValues still succeeds and the external
// provisioner receives a design-sourced ExternalResource.
func TestSaveValues_AuthorsDefinitionFromDesign(t *testing.T) {
	gate := provisionGateIssue(10, "stripe")
	ext := &fakeExtProv{}
	svc := newTestService(newFakeIssues([]sourcecontrol.IssueInfo{gate}), &fakeExecStore{},
		fakeDesign{comps: designWithDeps()}, ext, &fakePlatProv{}, &fakeBindings{}) // empty catalog

	if err := svc.SaveValues(context.Background(), "org", "org", "proj", "stripe",
		map[string]map[string]string{"development": {"api_key": "sk", "region": "us"}}); err != nil {
		t.Fatalf("SaveValues must author from the design even with an empty catalog: %v", err)
	}
	if ext.lastER == nil || ext.lastER.Name != "stripe" {
		t.Fatalf("Provision must receive a design-built ExternalResource, got %+v", ext.lastER)
	}
	// Config schema comes from the design union: api_key secret, region plain.
	secret := map[string]bool{}
	for _, k := range ext.lastER.ConfigKeys {
		secret[k.Key] = k.Secret
	}
	if !secret["api_key"] || secret["region"] {
		t.Fatalf("authored config must reflect the design schema; got %v", secret)
	}
}

// TestCompleteProvisionRow_FinishLoserSkipsCloseIssue: when Finish returns
// (nil, nil) because the row is already terminal, the loser must not CloseIssue
// — another replica already won.
func TestCompleteProvisionRow_FinishLoserSkipsCloseIssue(t *testing.T) {
	gate := provisionGateIssue(10, "stripe")
	issues := newFakeIssues([]sourcecontrol.IssueInfo{gate})
	row := &delivery.Execution{ID: "e1", Status: string(taskmeta.ExecSucceeded)} // already terminal
	execs := &fakeExecStore{rows: []*delivery.Execution{row}}
	svc := newTestService(issues, execs, fakeDesign{comps: designWithDeps()}, &fakeExtProv{}, &fakePlatProv{}, &fakeBindings{})

	svc.completeProvisionRow(context.Background(), "org", "proj", "stripe", 10, "e1", "ref")

	if _, closed := issues.closed[10]; closed {
		t.Fatalf("loser must not CloseIssue")
	}
}

// TestFailProvisionRow_FinishLoserSkipsComment: same race on the failure path —
// a pre-terminal row means Finish lost, so the gate must not get a failed comment.
func TestFailProvisionRow_FinishLoserSkipsComment(t *testing.T) {
	gate := provisionGateIssue(10, "stripe")
	issues := newFakeIssues([]sourcecontrol.IssueInfo{gate})
	row := &delivery.Execution{ID: "e1", Status: string(taskmeta.ExecFailed)} // already terminal
	execs := &fakeExecStore{rows: []*delivery.Execution{row}}
	svc := newTestService(issues, execs, fakeDesign{comps: designWithDeps()}, &fakeExtProv{}, &fakePlatProv{}, &fakeBindings{})

	svc.failProvisionRow(context.Background(), "org", "proj", 10, "e1", "boom")

	if comments := issues.comments[10]; len(comments) != 0 {
		t.Fatalf("loser must not CommentIssue, got %v", comments)
	}
}
