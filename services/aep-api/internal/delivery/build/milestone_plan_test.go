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

// SERVICE tier for the milestone plan path: the REAL plan path driving the REAL
// sourcecontrol.IssueService through the REAL githubhost client at a
// gittest.Stub. Nothing GitHub-facing is mocked — supersede, milestone
// creation, and the 422 recovery are asserted on the wire traffic they produce.
//
// White-box (package build) because the plan path's steps are internal to the
// build sequence; the HTTP-visible half lives in build_test.go's component tier.
package build

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/gittest"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	githubclient "github.com/wso2/aep/aep-api/internal/sourcecontrol/githubhost"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// ---- real IssueService on a stub --------------------------------------------

type planFakeCred struct{}

func (planFakeCred) Token(context.Context) (string, time.Time, error) {
	return "test-token", time.Time{}, nil
}
func (planFakeCred) Identity() secrets.Identity { return secrets.Identity{} }
func (planFakeCred) RepoOwner() string          { return "acme" }
func (planFakeCred) WebhookStrategy() secrets.WebhookStrategy {
	return secrets.WebhookPerRepo
}

type planFakeResolver struct{}

func (planFakeResolver) Resolve(context.Context, string) (secrets.Credential, error) {
	return planFakeCred{}, nil
}

// planFakeRepoRepo resolves (acme, shop) → github.com/acme/widgets so every
// REST call lands under /repos/acme/widgets on the stub.
type planFakeRepoRepo struct{}

func (planFakeRepoRepo) GetByOrgAndProjectID(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return &sourcecontrol.GitRepository{OrgID: "acme", ProjectID: "shop", RepoURL: "https://github.com/acme/widgets"}, nil
}
func (planFakeRepoRepo) GetByOrgAndSlug(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (planFakeRepoRepo) ListAllReady(context.Context) ([]sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (planFakeRepoRepo) ListAll(context.Context) ([]sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (planFakeRepoRepo) ListByOrg(context.Context, string) ([]sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (planFakeRepoRepo) Create(context.Context, *sourcecontrol.GitRepository) error { return nil }
func (planFakeRepoRepo) Update(context.Context, *sourcecontrol.GitRepository) error { return nil }
func (planFakeRepoRepo) DeleteByOrgAndProjectID(context.Context, string, string) error {
	return nil
}

func newIssueSvcOnStub(t *testing.T, stub *gittest.Stub) sourcecontrol.IssueService {
	t.Helper()
	return sourcecontrol.NewIssueService(
		planFakeRepoRepo{},
		githubclient.NewClient(githubclient.WithAPIBase(stub.URL)),
		planFakeResolver{},
	)
}

// jsonPage serves a paginated list: page 1 gets body, every later page gets [].
func jsonPage(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.URL.Query().Get("page") == "1" || r.URL.Query().Get("page") == "" {
			_, _ = w.Write([]byte(body))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}
}

func countRequests(t *testing.T, stub *gittest.Stub, method, path string) int {
	t.Helper()
	n := 0
	for _, r := range stub.Requests() {
		if r.Method == method && r.Path == path {
			n++
		}
	}
	return n
}

// ---- run store fake ----------------------------------------------------------

// fakeRunStore is the milestone_runs surface. It is a fake rather than a real
// repository because the DB half of the mutex (the partial unique index behind
// TryAdmit) has its own dbtest tier; what this tier proves is the plan path's
// USE of it.
type fakeRunStore struct {
	mu       sync.Mutex
	rows     []delivery.MilestoneRun
	active   *delivery.MilestoneRun
	judging  *delivery.MilestoneRun // a live validation run refuses the build
	refuse   bool                   // TryAdmit loses the admission race
	admitted []delivery.MilestoneRun
	settled  []string
	listErr  error
	nextID   int
}

func (f *fakeRunStore) ActiveDevRunByProject(context.Context, string, string) (*delivery.MilestoneRun, error) {
	return f.active, nil
}

func (f *fakeRunStore) ActiveValidationRunByProject(context.Context, string, string) (*delivery.MilestoneRun, error) {
	return f.judging, nil
}

func (f *fakeRunStore) TryAdmit(_ context.Context, run *delivery.MilestoneRun) (bool, *delivery.MilestoneRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse {
		return false, nil, nil
	}
	f.nextID++
	run.ID = fmt.Sprintf("run-%d", f.nextID)
	f.admitted = append(f.admitted, *run)
	return true, run, nil
}

func (f *fakeRunStore) Settle(_ context.Context, id, state, reason string) (*delivery.MilestoneRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settled = append(f.settled, id+":"+state+":"+reason)
	return nil, nil
}

func (f *fakeRunStore) ListByProject(context.Context, string, string) ([]delivery.MilestoneRun, error) {
	return f.rows, f.listErr
}

// ---- planner / gates / supervisor fakes ---------------------------------------

type fakePlanner struct {
	milestones []int
	err        error
}

func (f *fakePlanner) PlanIntoMilestone(_ context.Context, _, _ string, milestoneNumber int) error {
	f.milestones = append(f.milestones, milestoneNumber)
	return f.err
}

type fakeGates struct {
	milestones []int
	err        error
}

func (f *fakeGates) ProvisionForBuild(_ context.Context, _, _, _ string, milestoneNumber int, _ []delivery.ProvisionInput) error {
	f.milestones = append(f.milestones, milestoneNumber)
	return f.err
}

type fakeStarter struct {
	started []delivery.StartRunRequest
	err     error
}

func (f *fakeStarter) StartRun(_ context.Context, req delivery.StartRunRequest) error {
	f.started = append(f.started, req)
	return f.err
}

// planHarness wires a Service whose plan path talks to the stub.
type planHarness struct {
	svc     *Service
	stub    *gittest.Stub
	runs    *fakeRunStore
	planner *fakePlanner
	gates   *fakeGates
	starter *fakeStarter
}

func newPlanHarness(t *testing.T) *planHarness {
	t.Helper()
	stub := gittest.NewStub(t)
	h := &planHarness{
		stub:    stub,
		runs:    &fakeRunStore{},
		planner: &fakePlanner{},
		gates:   &fakeGates{},
		starter: &fakeStarter{},
	}
	host := newIssueSvcOnStub(t, stub)
	h.svc = NewService(Deps{})
	h.svc.SetPlanPath(PlanPathDeps{
		Milestones: host,
		// The same host, seen through the domain's issue-write surface: the
		// supersede path closes issues through the writer and milestones through
		// the milestone client, and both land on this one stub.
		Issues:  delivery.NewIssueWriter(host),
		Runs:    h.runs,
		Planner: h.planner,
		Gates:   h.gates,
		Starter: h.starter,
	})
	return h
}

// ---- claim: the milestone is minted and the run row admitted -----------------

func TestClaimVersion_MintsTheMilestoneAndAdmitsTheRun(t *testing.T) {
	h := newPlanHarness(t)
	h.stub.OnFunc(http.MethodGet, "/repos/acme/widgets/milestones", jsonPage(`[]`))
	h.stub.On(http.MethodPost, "/repos/acme/widgets/milestones", http.StatusCreated, `{"number":9,"title":"v3"}`)

	run, err := h.svc.claimVersion(context.Background(), "acme", "shop", spec.BuildScope{Tag: "v3"})
	if err != nil {
		t.Fatalf("claimVersion: %v", err)
	}
	if run.MilestoneNumber != 9 || run.MilestoneTitle != "v3" {
		t.Errorf("run = %+v, want milestone 9 titled v3", run)
	}
	// PLANNING, not waiting: the row is admitted before fillMilestone, so it
	// must not claim to be parked on a human while the platform is writing the
	// milestone.
	if run.Origin != delivery.RunOriginSpecBuild || run.State != delivery.RunStatePlanning {
		t.Errorf("run = %+v, want a planning dev run", run)
	}
	if len(h.runs.admitted) != 1 {
		t.Fatalf("admitted %d runs, want 1", len(h.runs.admitted))
	}
	// Exactly ONE milestone create — the "+1" of the plan's 1+N budget.
	if n := countRequests(t, h.stub, http.MethodPost, "/repos/acme/widgets/milestones"); n != 1 {
		t.Errorf("POST /milestones ×%d, want exactly 1", n)
	}
	// Nothing was superseded: this project has no earlier run row.
	if n := countRequests(t, h.stub, http.MethodPatch, "/repos/acme/widgets/milestones/9"); n != 0 {
		t.Errorf("a first version must close no milestone, got %d PATCHes", n)
	}
}

// GitHub's milestone-title uniqueness is case-SENSITIVE at create while its
// title filters are not, so a duplicate must recover the EXISTING number rather
// than mint a case-twin. Both recovery layers are exercised: the pre-check that
// avoids the POST, and the 422 already_exists that follows a lost race.
func TestClaimVersion_DoubleCreate_RecoversTheNumber(t *testing.T) {
	t.Run("pre-check adopts an existing case-twin", func(t *testing.T) {
		h := newPlanHarness(t)
		h.stub.OnFunc(http.MethodGet, "/repos/acme/widgets/milestones", jsonPage(`[{"number":4,"title":"V3","state":"open"}]`))

		run, err := h.svc.claimVersion(context.Background(), "acme", "shop", spec.BuildScope{Tag: "v3"})
		if err != nil {
			t.Fatalf("claimVersion: %v", err)
		}
		if run.MilestoneNumber != 4 {
			t.Errorf("milestone = %d, want the existing case-twin 4", run.MilestoneNumber)
		}
		if n := countRequests(t, h.stub, http.MethodPost, "/repos/acme/widgets/milestones"); n != 0 {
			t.Errorf("POST /milestones ×%d — the pre-check must prevent the create", n)
		}
	})

	t.Run("422 already_exists recovers by re-listing", func(t *testing.T) {
		h := newPlanHarness(t)
		// Empty at pre-check, populated on the recovery list: a concurrent
		// create landed in between.
		var calls int
		h.stub.OnFunc(http.MethodGet, "/repos/acme/widgets/milestones", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if r.URL.Query().Get("page") != "1" {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			calls++
			if calls == 1 {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_, _ = w.Write([]byte(`[{"number":11,"title":"v3","state":"open"}]`))
		})
		h.stub.On(http.MethodPost, "/repos/acme/widgets/milestones", http.StatusUnprocessableEntity,
			`{"message":"Validation Failed","errors":[{"resource":"Milestone","code":"already_exists","field":"title"}]}`)

		run, err := h.svc.claimVersion(context.Background(), "acme", "shop", spec.BuildScope{Tag: "v3"})
		if err != nil {
			t.Fatalf("claimVersion: %v", err)
		}
		if run.MilestoneNumber != 11 {
			t.Errorf("milestone = %d, want 11 recovered after the 422", run.MilestoneNumber)
		}
	})
}

// The DB index is the mutex's authority: when TryAdmit loses, the click gets
// the same conflict the pre-check would have given it.
func TestClaimVersion_AdmissionRaceLost_IsAConflict(t *testing.T) {
	h := newPlanHarness(t)
	h.runs.refuse = true
	h.stub.OnFunc(http.MethodGet, "/repos/acme/widgets/milestones", jsonPage(`[]`))
	h.stub.On(http.MethodPost, "/repos/acme/widgets/milestones", http.StatusCreated, `{"number":9,"title":"v3"}`)

	_, err := h.svc.claimVersion(context.Background(), "acme", "shop", spec.BuildScope{Tag: "v3"})
	if err != ErrBuildAlreadyRunning {
		t.Fatalf("err = %v, want ErrBuildAlreadyRunning", err)
	}
}

// ---- supersede ---------------------------------------------------------------

// §6: before v<N+1> exists, v<N>'s still-open work is closed with a superseded
// comment, its gates too, and then the milestone itself. The previous milestone
// is found through the RUN ROWS — never by matching titles against GitHub.
func TestSupersede_ClosesOpenWorkThenGatesThenTheMilestone(t *testing.T) {
	h := newPlanHarness(t)
	h.runs.rows = []delivery.MilestoneRun{
		// Newest first, as the repository returns them. An incident run on an
		// even older milestone must not be mistaken for the previous version.
		{MilestoneNumber: 6, MilestoneTitle: "v2", Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild, State: delivery.RunStateFailed},
		{MilestoneNumber: 2, MilestoneTitle: "v1", Kind: delivery.RunKindTask, Origin: delivery.RunOriginIncidentAdoption, State: delivery.RunStateSucceeded},
	}
	// Planned work states its KIND, which is what makes it closeable here: an
	// armed issue carrying no kind is read as a DEFECT by the working set and by
	// supersede alike (delivery.WorkKindOf), so it would be carried forward
	// instead — see TestSupersede_CarriesOpenBugsForwardAndClosesThePlan.
	h.stub.OnFunc(http.MethodGet, "/repos/acme/widgets/issues", jsonPage(`[
		{"number":31,"title":"Implement orders","state":"open","labels":[{"name":"aep"},{"name":"development"}]},
		{"number":32,"title":"Provision orders-db","state":"open","labels":[{"name":"provision"}]},
		{"number":33,"title":"Flaky checkout","state":"open","labels":[]}
	]`))
	for _, n := range []int{31, 32, 33} {
		h.stub.On(http.MethodPost, fmt.Sprintf("/repos/acme/widgets/issues/%d/comments", n), http.StatusCreated, `{}`)
		h.stub.On(http.MethodPatch, fmt.Sprintf("/repos/acme/widgets/issues/%d", n), http.StatusOK, `{}`)
	}
	h.stub.On(http.MethodPatch, "/repos/acme/widgets/milestones/6", http.StatusOK, `{"number":6,"state":"closed"}`)
	h.stub.OnFunc(http.MethodGet, "/repos/acme/widgets/milestones", jsonPage(`[]`))
	h.stub.On(http.MethodPost, "/repos/acme/widgets/milestones", http.StatusCreated, `{"number":9,"title":"v3"}`)

	if _, err := h.svc.claimVersion(context.Background(), "acme", "shop", spec.BuildScope{Tag: "v3"}); err != nil {
		t.Fatalf("claimVersion: %v", err)
	}

	// The read is scoped to milestone 6 — the NUMBER off the run row.
	var listed bool
	for _, r := range h.stub.Requests() {
		if r.Method == http.MethodGet && r.Path == "/repos/acme/widgets/issues" && strings.Contains(r.Query, "milestone=6") {
			listed = true
			if !strings.Contains(r.Query, "state=open") {
				t.Errorf("supersede listed %s, want state=open", r.Query)
			}
		}
	}
	if !listed {
		t.Fatal("supersede never listed milestone 6's issues")
	}

	// Every open issue — work, gate and ledger alike — is commented and closed.
	for _, n := range []int{31, 32, 33} {
		comments := requestsTo(h.stub, http.MethodPost, fmt.Sprintf("/repos/acme/widgets/issues/%d/comments", n))
		if len(comments) != 1 {
			t.Fatalf("issue %d: %d superseded comments, want 1", n, len(comments))
		}
		if !strings.Contains(comments[0].Body, "Superseded by v3") {
			t.Errorf("issue %d comment = %s, want the Superseded by v3 note", n, comments[0].Body)
		}
		closes := requestsTo(h.stub, http.MethodPatch, fmt.Sprintf("/repos/acme/widgets/issues/%d", n))
		if len(closes) != 1 || !strings.Contains(closes[0].Body, `"state":"closed"`) {
			t.Errorf("issue %d: closes = %+v, want one close", n, closes)
		}
	}
	// Then the milestone itself.
	if n := countRequests(t, h.stub, http.MethodPatch, "/repos/acme/widgets/milestones/6"); n != 1 {
		t.Errorf("PATCH /milestones/6 ×%d, want exactly 1 (the close)", n)
	}
	// And the version being cut is untouched by supersede.
	if n := countRequests(t, h.stub, http.MethodPatch, "/repos/acme/widgets/milestones/9"); n != 0 {
		t.Errorf("the new milestone was closed by supersede (%d PATCHes)", n)
	}
}

// TestSupersede_CarriesOpenBugsForwardAndClosesThePlan is the rule a version
// change does NOT get to override: a plan is replaced by a plan, but a defect is
// not superseded by anything. It is still broken, and the new version is what
// will ship the fix.
//
// A conflict issue is closed rather than moved even though it is recovery work,
// because it names a BRANCH of the version being superseded — a branch that is
// about to be irrelevant, and that nothing in the new version will rebase.
//
// Moving is not ARMING: the unadopted incident below arrives in the new milestone
// still unarmed and still ledger-only, so carrying a human's defect forward can
// never turn it into agent work nobody asked for.
//
// Issue #47 is the case that reads as a bug WITHOUT saying so: armed, no kind at
// all. It is the common human hand-over — adoption stamps the arming switch and
// deliberately no kind — and every working-set predicate in the loop works it as a
// bug (delivery.WorkKindOf). So supersede must read the same kind they do, or the
// next version cut silently CLOSES a defect somebody had adopted, with the issue's
// own labels saying it was work.
func TestSupersede_CarriesOpenBugsForwardAndClosesThePlan(t *testing.T) {
	h := newPlanHarness(t)
	h.runs.rows = []delivery.MilestoneRun{
		{MilestoneNumber: 6, MilestoneTitle: "v3", Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild, State: delivery.RunStateFailed},
	}
	h.stub.OnFunc(http.MethodGet, "/repos/acme/widgets/issues", jsonPage(`[
		{"number":41,"title":"Implement orders","state":"open","labels":[{"name":"aep"},{"name":"development"}]},
		{"number":42,"title":"Fix the failing build for orders","state":"open","labels":[{"name":"aep"},{"name":"bug"},{"name":"src/build"}]},
		{"number":43,"title":"Rebase aep/m6-1","state":"open","labels":[{"name":"aep"},{"name":"conflict"}]},
		{"number":44,"title":"Provision orders-db","state":"open","labels":[{"name":"provision"}]},
		{"number":45,"title":"Main went red","state":"open","labels":[{"name":"bug"},{"name":"src/incident"}]},
		{"number":46,"title":"Fix checkout","state":"open","labels":[{"name":"aep"},{"name":"bug"},{"name":"aep:halted"}]},
		{"number":47,"title":"Checkout drops the cart","state":"open","labels":[{"name":"aep"}]}
	]`))
	for _, n := range []int{41, 42, 43, 44, 45, 46, 47} {
		h.stub.On(http.MethodPost, fmt.Sprintf("/repos/acme/widgets/issues/%d/comments", n), http.StatusCreated, `{}`)
		h.stub.On(http.MethodPatch, fmt.Sprintf("/repos/acme/widgets/issues/%d", n), http.StatusOK, `{}`)
	}
	h.stub.On(http.MethodDelete, "/repos/acme/widgets/issues/46/labels/aep:halted", http.StatusOK, `[]`)
	h.stub.On(http.MethodPatch, "/repos/acme/widgets/milestones/6", http.StatusOK, `{"number":6,"state":"closed"}`)
	h.stub.OnFunc(http.MethodGet, "/repos/acme/widgets/milestones", jsonPage(`[]`))
	h.stub.On(http.MethodPost, "/repos/acme/widgets/milestones", http.StatusCreated, `{"number":9,"title":"v4"}`)

	if _, err := h.svc.claimVersion(context.Background(), "acme", "shop", spec.BuildScope{Tag: "v4"}); err != nil {
		t.Fatalf("claimVersion: %v", err)
	}

	// The bugs — armed or not, halted or not — are MOVED into v4's milestone, and
	// never closed.
	for _, n := range []int{42, 45, 46, 47} {
		writes := requestsTo(h.stub, http.MethodPatch, fmt.Sprintf("/repos/acme/widgets/issues/%d", n))
		if len(writes) != 1 {
			t.Fatalf("issue %d: %d PATCHes, want exactly the move", n, len(writes))
		}
		if !strings.Contains(writes[0].Body, `"milestone":9`) {
			t.Errorf("issue %d PATCH = %s, want a move into milestone 9", n, writes[0].Body)
		}
		if strings.Contains(writes[0].Body, `"state":"closed"`) {
			t.Errorf("issue %d was closed; a defect is not superseded by a new plan", n)
		}
	}
	// The plan, its gate and the conflict are CLOSED.
	for _, n := range []int{41, 43, 44} {
		writes := requestsTo(h.stub, http.MethodPatch, fmt.Sprintf("/repos/acme/widgets/issues/%d", n))
		if len(writes) != 1 || !strings.Contains(writes[0].Body, `"state":"closed"`) {
			t.Errorf("issue %d: PATCHes = %+v, want exactly one close", n, writes)
		}
		if strings.Contains(writes[0].Body, `"milestone"`) {
			t.Errorf("issue %d was carried forward; only a bug is", n)
		}
	}
	// A rebuild is what CLEARS the halt: `aep:halted` says a run gave up and the
	// reconcile sweep must not restart it, so carrying it into the new version
	// would hide the bug from the sweep for the rest of the project's life.
	if n := countRequests(t, h.stub, http.MethodDelete, "/repos/acme/widgets/issues/46/labels/aep:halted"); n != 1 {
		t.Errorf("the halt on the carried-forward bug was cleared %d times, want 1", n)
	}
	// And it is not spent on the bugs that never carried it.
	for _, n := range []int{42, 45} {
		if len(requestsTo(h.stub, http.MethodDelete, fmt.Sprintf("/repos/acme/widgets/issues/%d/labels/aep:halted", n))) != 0 {
			t.Errorf("issue %d was never halted; clearing it costs a request for nothing", n)
		}
	}
	// The new milestone exists BEFORE the move — that ordering is the whole reason
	// claimVersion mints it first — and supersede never touches it.
	if n := countRequests(t, h.stub, http.MethodPatch, "/repos/acme/widgets/milestones/9"); n != 0 {
		t.Errorf("supersede wrote to the milestone being cut (%d PATCHes)", n)
	}
	if n := countRequests(t, h.stub, http.MethodPatch, "/repos/acme/widgets/milestones/6"); n != 1 {
		t.Errorf("PATCH /milestones/6 ×%d, want exactly 1 (the close)", n)
	}
}

// This is what keeps the reconcile sweep sound. The sweep starts a run for any
// known milestone holding open work and no live run, so a superseded milestone
// must hold none — and after the carry-forward it holds none for a DIFFERENT
// reason than before: the plan is closed and the bugs have LEFT.
func TestSupersede_LeavesTheOldMilestoneWithNothingToRestart(t *testing.T) {
	h := newPlanHarness(t)
	h.runs.rows = []delivery.MilestoneRun{
		{MilestoneNumber: 6, MilestoneTitle: "v3", Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild, State: delivery.RunStateFailed},
	}
	h.stub.OnFunc(http.MethodGet, "/repos/acme/widgets/issues", jsonPage(`[
		{"number":41,"title":"Implement orders","state":"open","labels":[{"name":"aep"},{"name":"development"}]},
		{"number":42,"title":"Fix orders","state":"open","labels":[{"name":"aep"},{"name":"bug"},{"name":"src/build"}]}
	]`))
	for _, n := range []int{41, 42} {
		h.stub.On(http.MethodPost, fmt.Sprintf("/repos/acme/widgets/issues/%d/comments", n), http.StatusCreated, `{}`)
		h.stub.On(http.MethodPatch, fmt.Sprintf("/repos/acme/widgets/issues/%d", n), http.StatusOK, `{}`)
	}
	h.stub.On(http.MethodPatch, "/repos/acme/widgets/milestones/6", http.StatusOK, `{}`)
	h.stub.OnFunc(http.MethodGet, "/repos/acme/widgets/milestones", jsonPage(`[]`))
	h.stub.On(http.MethodPost, "/repos/acme/widgets/milestones", http.StatusCreated, `{"number":9,"title":"v4"}`)

	if _, err := h.svc.claimVersion(context.Background(), "acme", "shop", spec.BuildScope{Tag: "v4"}); err != nil {
		t.Fatalf("claimVersion: %v", err)
	}

	// Every open issue was either closed or moved out. Nothing was left behind for
	// the sweep to find.
	for _, n := range []int{41, 42} {
		if len(requestsTo(h.stub, http.MethodPatch, fmt.Sprintf("/repos/acme/widgets/issues/%d", n))) != 1 {
			t.Errorf("issue %d was neither closed nor carried forward", n)
		}
	}
}

// An unchanged spec re-build returns the SAME tag. Superseding then would close
// the version being rebuilt — the run rows are keyed by number but the guard is
// the recorded title, which is a platform-side value, not a GitHub read.
func TestSupersede_NeverSupersedesTheVersionBeingCut(t *testing.T) {
	rows := []delivery.MilestoneRun{
		{MilestoneNumber: 9, MilestoneTitle: "v3", Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild, State: delivery.RunStateSucceeded},
	}
	if _, ok := previousDevMilestone(rows, "v3"); ok {
		t.Fatal("a re-build of the same tag must supersede nothing")
	}
	rows = append([]delivery.MilestoneRun{
		{MilestoneNumber: 12, MilestoneTitle: "v4", Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild, State: delivery.RunStateWaiting},
	}, rows...)
	prev, ok := previousDevMilestone(rows, "v5")
	if !ok || prev.MilestoneNumber != 12 {
		t.Fatalf("previous = %+v (%v), want the newest spec milestone 12", prev, ok)
	}
}

// ---- start: handing the claimed version to the supervisor --------------------
//
// Gates and planning moved INTO the run workflow, so what the click owns here is
// exactly one step: start the supervisor, and settle the row when it cannot.

func TestStartRun_CarriesThePlanningInputsToTheSupervisor(t *testing.T) {
	h := newPlanHarness(t)
	run := &delivery.MilestoneRun{ID: "run-1", ProjectID: "shop", MilestoneNumber: 9, MilestoneTitle: "v3"}
	inputs := []delivery.ProvisionInput{{Component: "api", Dependency: "db", Kind: "platform-resource"}}

	if err := h.svc.startRun(context.Background(), "acme", "shop", "v3", run, inputs, false); err != nil {
		t.Fatalf("startRun: %v", err)
	}

	if len(h.starter.started) != 1 {
		t.Fatalf("started %d runs, want 1", len(h.starter.started))
	}
	got := h.starter.started[0]
	if got.RunID != "run-1" || got.MilestoneNumber != 9 || got.MilestoneTitle != "v3" ||
		got.Origin != delivery.RunOriginSpecBuild {
		t.Errorf("start request = %+v", got)
	}
	// The Tag is what tells the workflow this is a version to FILL rather than a
	// run to resume — without it the supervisor would skip planning and settle an
	// unplanned version as delivered.
	if got.Tag != "v3" {
		t.Errorf("start request Tag = %q, want the version being filled", got.Tag)
	}
	if len(got.ProvisionInputs) != 1 || got.ProvisionInputs[0].Dependency != "db" {
		t.Errorf("provision inputs not carried: %+v", got.ProvisionInputs)
	}
	// The click no longer plans; nothing here should have run gates or a turn.
	if len(h.gates.milestones) != 0 || len(h.planner.milestones) != 0 {
		t.Errorf("the click must not plan: gates=%v planner=%v", h.gates.milestones, h.planner.milestones)
	}
	if len(h.runs.settled) != 0 {
		t.Errorf("a clean start must settle nothing, got %v", h.runs.settled)
	}
}

// A supervisor that cannot start must not leave the project wedged behind the
// mutex the click armed. The row settles, so the user's next click is admitted
// rather than refused by a run that never ran.
func TestStartRun_NotStarted_SettlesTheRunAnd503s(t *testing.T) {
	h := newPlanHarness(t)
	h.starter.err = delivery.ErrRunNotStarted
	run := &delivery.MilestoneRun{ID: "run-1", ProjectID: "shop", MilestoneNumber: 9, MilestoneTitle: "v3"}

	err := h.svc.startRun(context.Background(), "acme", "shop", "v3", run, nil, false)

	var edge *EdgeError
	if !errors.As(err, &edge) || edge.Status != 503 {
		t.Fatalf("want a 503 EdgeError, got %v", err)
	}
	want := "run-1:" + delivery.RunStateFailed + ":" + delivery.RunReasonPlanFailed
	if len(h.runs.settled) != 1 || h.runs.settled[0] != want {
		t.Fatalf("settled = %v, want [%s]", h.runs.settled, want)
	}
}

// Any other start failure settles the same way — the row must never survive a
// start that did not happen — but reads as a bad gateway rather than "not ready".
func TestStartRun_OtherFailure_SettlesTheRunAnd502s(t *testing.T) {
	h := newPlanHarness(t)
	h.starter.err = fmt.Errorf("temporal refused")
	run := &delivery.MilestoneRun{ID: "run-1", ProjectID: "shop", MilestoneNumber: 9, MilestoneTitle: "v3"}

	err := h.svc.startRun(context.Background(), "acme", "shop", "v3", run, nil, false)

	var edge *EdgeError
	if !errors.As(err, &edge) || edge.Status != 502 {
		t.Fatalf("want a 502 EdgeError, got %v", err)
	}
	if len(h.runs.settled) != 1 {
		t.Fatalf("settled = %v, want exactly one", h.runs.settled)
	}
}

// An unwired supervisor is not a failure: the run row waits, exactly as the
// event plane's own no-op starter leaves an adopted milestone waiting.
func TestStartRun_NoSupervisor_LeavesTheRunWaiting(t *testing.T) {
	h := newPlanHarness(t)
	host := newIssueSvcOnStub(t, h.stub)
	h.svc.SetPlanPath(PlanPathDeps{
		Milestones: host,
		Issues:     delivery.NewIssueWriter(host),
		Runs:       h.runs,
		Planner:    h.planner,
	})
	run := &delivery.MilestoneRun{ID: "run-1", ProjectID: "shop", MilestoneNumber: 9, MilestoneTitle: "v3"}

	if err := h.svc.startRun(context.Background(), "acme", "shop", "v3", run, nil, false); err != nil {
		t.Fatalf("an unwired supervisor is not an error: %v", err)
	}
	if len(h.runs.settled) != 0 {
		t.Errorf("an unwired supervisor must not settle the run, got %v", h.runs.settled)
	}
}

// requestsTo returns the stub's recorded requests for one route.
func requestsTo(stub *gittest.Stub, method, path string) []gittest.RecordedRequest {
	var out []gittest.RecordedRequest
	for _, r := range stub.Requests() {
		if r.Method == method && r.Path == path {
			out = append(out, r)
		}
	}
	return out
}

// TestClaimVersion_MilestoneIsTitledAfterTheVersion pins milestone identity:
// a claim names its milestone after the TAG, and the previous version's
// milestone is superseded.
func TestClaimVersion_MilestoneIsTitledAfterTheVersion(t *testing.T) {
	h := newPlanHarness(t)
	h.stub.OnFunc(http.MethodGet, "/repos/acme/widgets/milestones", jsonPage(`[]`))
	h.stub.On(http.MethodPost, "/repos/acme/widgets/milestones", http.StatusCreated, `{"number":4,"title":"v3"}`)
	h.stub.OnFunc(http.MethodGet, "/repos/acme/widgets/issues", jsonPage(`[]`))
	h.stub.On(http.MethodPatch, "/repos/acme/widgets/milestones/2", http.StatusOK, `{}`)
	// v3 is its own version and supersedes v2's milestone.
	h.runs.rows = []delivery.MilestoneRun{{
		OrgID: "acme", ProjectID: "shop", MilestoneNumber: 2,
		MilestoneTitle: "v2", Tag: "v2", Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild,
	}}

	run, err := h.svc.claimVersion(context.Background(), "acme", "shop", spec.BuildScope{Tag: "v3"})
	if err != nil {
		t.Fatalf("claimVersion: %v", err)
	}
	if run.MilestoneTitle != "v3" || run.Tag != "v3" || run.MilestoneNumber != 4 {
		t.Fatalf("run identity = %+v, want v3 / v3 / milestone 4", run)
	}
	if n := countRequests(t, h.stub, http.MethodPatch, "/repos/acme/widgets/milestones/2"); n != 1 {
		t.Errorf("the previous version's milestone must be closed, got %d PATCHes", n)
	}
}

// ---- the same-tag rebuild -----------------------------------------------------

// TestReopenIncrement_ReopensExactlyTheMarkedSetAndClearsTheMark is the way back
// from a cancelled build.
//
// The click reached this because the spec-save status was `unchanged`: the same
// tag, so the same milestone, so this build is that version being worked AGAIN.
// `aep:cancelled` is the handle on what was in flight, and it is the whole reason
// the marker exists rather than "reopen everything in the milestone" — work a
// cycle GENUINELY FINISHED is closed and unmarked, and reopening it would dispatch
// an agent at code that is already merged and serving.
//
// The mark is CLEARED as each issue is reopened. It records ONE abandoned attempt,
// so leaving it on would make the next cancel's marked set the union of two
// attempts and this reopen would restore work that cancel deliberately left closed.
func TestReopenIncrement_ReopensExactlyTheMarkedSetAndClearsTheMark(t *testing.T) {
	h := newPlanHarness(t)
	h.stub.OnFunc(http.MethodGet, "/repos/acme/widgets/issues", jsonPage(`[
		{"number":31,"title":"Implement orders","state":"closed","labels":[{"name":"aep"},{"name":"development"},{"name":"aep:cancelled"}]},
		{"number":32,"title":"Provision orders-db","state":"closed","labels":[{"name":"provision"},{"name":"aep:cancelled"}]},
		{"number":33,"title":"Implement checkout","state":"closed","labels":[{"name":"aep"},{"name":"development"}]}
	]`))
	for _, n := range []int{31, 32} {
		h.stub.On(http.MethodPatch, fmt.Sprintf("/repos/acme/widgets/issues/%d", n), http.StatusOK, `{}`)
		h.stub.On(http.MethodDelete, fmt.Sprintf("/repos/acme/widgets/issues/%d/labels/aep:cancelled", n),
			http.StatusOK, `[]`)
	}
	h.stub.On(http.MethodPatch, "/repos/acme/widgets/milestones/9", http.StatusOK, `{"number":9,"state":"open"}`)

	if filled := h.svc.reopenIncrement(context.Background(), "acme", "shop", 9); !filled {
		t.Error("a milestone holding a `development` issue is FILLED — the run must skip its planning turn")
	}

	// The milestone itself comes back: a version being worked whose milestone reads
	// closed is a lie the console renders.
	reopens := requestsTo(h.stub, http.MethodPatch, "/repos/acme/widgets/milestones/9")
	if len(reopens) != 1 || !strings.Contains(reopens[0].Body, `"state":"open"`) {
		t.Fatalf("milestone reopens = %+v, want exactly one state:open", reopens)
	}
	// The read is EVERY state, unfiltered by label. Unfiltered because which issues
	// this rebuild owns is decided from the labels, exactly as supersede decides
	// what it carries; every state because the same list answers a second question
	// — whether the milestone holds planned work at all — and a milestone whose
	// Tasks are still open is as filled as one whose Tasks a cancel closed.
	var listed bool
	for _, r := range h.stub.Requests() {
		if r.Method == http.MethodGet && r.Path == "/repos/acme/widgets/issues" && strings.Contains(r.Query, "milestone=9") {
			listed = true
			if !strings.Contains(r.Query, "state=all") {
				t.Errorf("the rebuild listed %s, want state=all", r.Query)
			}
			if strings.Contains(r.Query, "labels=") {
				t.Errorf("the rebuild narrowed its fetch by label (%s) — the decision belongs in Go", r.Query)
			}
		}
	}
	if !listed {
		t.Fatal("the rebuild never listed milestone 9's issues")
	}
	// The marked set — the planned Task AND the gate the cancel closed with it.
	for _, n := range []int{31, 32} {
		patches := requestsTo(h.stub, http.MethodPatch, fmt.Sprintf("/repos/acme/widgets/issues/%d", n))
		if len(patches) != 1 || !strings.Contains(patches[0].Body, `"state":"open"`) {
			t.Errorf("issue %d: patches = %+v, want one reopen", n, patches)
		}
		cleared := countRequests(t, h.stub, http.MethodDelete,
			fmt.Sprintf("/repos/acme/widgets/issues/%d/labels/aep:cancelled", n))
		if cleared != 1 {
			t.Errorf("issue %d: the cancel mark was cleared %d times, want exactly 1", n, cleared)
		}
	}
	// And the Task the build had already DELIVERED stays closed and unmarked.
	if n := countRequests(t, h.stub, http.MethodPatch, "/repos/acme/widgets/issues/33"); n != 0 {
		t.Errorf("issue 33 was finished before the cancel and must not be reopened (%d patches)", n)
	}
}

// A version nobody cancelled takes the same branch and reopens nothing. The
// spec-save status is the only question asked — there is no "was it cancelled"
// read anywhere — and the marker being absent is what answers it.
func TestReopenIncrement_AVersionNobodyCancelledReopensNothing(t *testing.T) {
	h := newPlanHarness(t)
	h.stub.OnFunc(http.MethodGet, "/repos/acme/widgets/issues", jsonPage(`[
		{"number":31,"title":"Implement orders","state":"closed","labels":[{"name":"aep"},{"name":"development"}]}
	]`))
	h.stub.On(http.MethodPatch, "/repos/acme/widgets/milestones/9", http.StatusOK, `{"number":9,"state":"open"}`)

	h.svc.reopenIncrement(context.Background(), "acme", "shop", 9)

	if n := countRequests(t, h.stub, http.MethodPatch, "/repos/acme/widgets/issues/31"); n != 0 {
		t.Errorf("an unmarked issue must stay closed (%d patches)", n)
	}
}

// reopenIncrement also answers whether the milestone was ever PLANNED, and this
// is the answer that keeps a rebuild honest.
//
// A run that died in its planning phase leaves the milestone holding its gates
// and nothing else. `unchanged` is true for the next click all the same, so
// without this answer it would tell the run to skip planning — and a run that
// skips planning over a milestone with no work reads the empty working set as
// "planning produced nothing" and settles the version SUCCEEDED having built none
// of it.
//
// A GATE IS NOT WORK. That is the whole discrimination: gates are minted before
// the planning turn, so a milestone holding only gates is one whose planning turn
// never landed.
func TestReopenIncrement_AMilestoneHoldingOnlyGatesIsNotFilled(t *testing.T) {
	h := newPlanHarness(t)
	h.stub.OnFunc(http.MethodGet, "/repos/acme/widgets/issues", jsonPage(`[
		{"number":12,"title":"Provision orders-db","state":"open","labels":[{"name":"provision"},{"name":"aep:dep/orders-db"}]},
		{"number":13,"title":"Provision roles and test users","state":"open","labels":[{"name":"provision"}]}
	]`))
	h.stub.On(http.MethodPatch, "/repos/acme/widgets/milestones/9", http.StatusOK, `{"number":9,"state":"open"}`)

	if filled := h.svc.reopenIncrement(context.Background(), "acme", "shop", 9); filled {
		t.Error("gates are minted BEFORE the planning turn, so a milestone holding only gates was never planned")
	}
}

// An OPEN planned Task counts too. The two questions this one list answers want
// different states, and reading only the closed ones would call a milestone whose
// Tasks are all still open "unplanned" and plan it a second time.
func TestReopenIncrement_OpenPlannedWorkCountsAsFilled(t *testing.T) {
	h := newPlanHarness(t)
	h.stub.OnFunc(http.MethodGet, "/repos/acme/widgets/issues", jsonPage(`[
		{"number":31,"title":"Implement orders","state":"open","labels":[{"name":"aep"},{"name":"development"}]}
	]`))
	h.stub.On(http.MethodPatch, "/repos/acme/widgets/milestones/9", http.StatusOK, `{"number":9,"state":"open"}`)

	if filled := h.svc.reopenIncrement(context.Background(), "acme", "shop", 9); !filled {
		t.Error("planned work that is still open is planned work")
	}
}

// A LIST FAILURE FAILS TOWARDS PLANNING, and the asymmetry is deliberate. The
// wrong "filled" settles an unbuilt version as delivered, silently; the wrong
// "not filled" spends one LLM turn that dedupes onto the milestone's own titles
// and mints nothing. Only one of those is recoverable by looking at the console.
func TestReopenIncrement_AnUnreadableMilestoneIsNotAssumedFilled(t *testing.T) {
	h := newPlanHarness(t)
	h.stub.On(http.MethodGet, "/repos/acme/widgets/issues", http.StatusInternalServerError, `{}`)
	h.stub.On(http.MethodPatch, "/repos/acme/widgets/milestones/9", http.StatusOK, `{"number":9,"state":"open"}`)

	if filled := h.svc.reopenIncrement(context.Background(), "acme", "shop", 9); filled {
		t.Error("a milestone this build could not read must be re-planned, never assumed filled")
	}
}

// The mark is cleared on an issue the cancel marked but never managed to CLOSE.
//
// cancel.go tolerates that state on purpose — the label goes on before the close,
// so a failure between them leaves the issue open and marked, which costs nothing
// at the time. It costs something later: the mark records ONE abandoned attempt,
// and leaving it on makes the next cancel's marked set the union of two.
func TestReopenIncrement_ClearsTheMarkOnAnIssueTheCancelLeftOpen(t *testing.T) {
	h := newPlanHarness(t)
	h.stub.OnFunc(http.MethodGet, "/repos/acme/widgets/issues", jsonPage(`[
		{"number":31,"title":"Implement orders","state":"open","labels":[{"name":"aep"},{"name":"development"},{"name":"aep:cancelled"}]}
	]`))
	h.stub.On(http.MethodDelete, "/repos/acme/widgets/issues/31/labels/aep:cancelled", http.StatusOK, `[]`)
	h.stub.On(http.MethodPatch, "/repos/acme/widgets/milestones/9", http.StatusOK, `{"number":9,"state":"open"}`)

	h.svc.reopenIncrement(context.Background(), "acme", "shop", 9)

	if n := countRequests(t, h.stub, http.MethodPatch, "/repos/acme/widgets/issues/31"); n != 0 {
		t.Errorf("an issue that is already open needs no reopen (%d patches)", n)
	}
	if n := countRequests(t, h.stub, http.MethodDelete,
		"/repos/acme/widgets/issues/31/labels/aep:cancelled"); n != 1 {
		t.Errorf("the cancel mark was cleared %d times, want exactly 1", n)
	}
}
