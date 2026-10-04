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
// sourcecontrol.IssueService at the org's pod (the in-memory
// aestudiotest.Fake). Nothing GitHub-facing is mocked — supersede, milestone
// creation and the same-tag rebuild are asserted on the issues and milestones
// they leave behind. GitHub's own wire (the 422 recovery of a milestone
// create) is the pod client's, pinned in ae-studio-tools' internal/github.
//
// White-box (package build) because the plan path's steps are internal to the
// build sequence; the HTTP-visible half lives in build_test.go's component tier.
package build

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// ---- real IssueService on the in-memory pod ----------------------------------

// widgets is the repository (acme, shop) resolves to.
var widgets = sourcecontrol.RepoRef{Org: "acme", Owner: "acme", Repo: "widgets"}

// planFakeRepoRepo resolves (acme, shop) → github.com/acme/widgets, so every
// pod call addresses acme/widgets.
type planFakeRepoRepo struct{}

func (planFakeRepoRepo) GetByOrgAndProjectID(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return &sourcecontrol.GitRepository{OrgID: "acme", ProjectID: "shop", RepoURL: "https://github.com/acme/widgets"}, nil
}
func (planFakeRepoRepo) FindInOrgByFullName(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return nil, nil
}

func (planFakeRepoRepo) GetByOrgAndSlug(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (planFakeRepoRepo) ListAllReady(context.Context) ([]sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (planFakeRepoRepo) SetWebhookIDIfReady(context.Context, string, string, int64) (bool, error) {
	panic("not used")
}
func (planFakeRepoRepo) ClearWebhookIDs(context.Context, string) error { panic("not used") }
func (planFakeRepoRepo) SetStatusIf(context.Context, string, string, string, string) (bool, error) {
	panic("not used")
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

func newIssueSvcOnPod(pod *aestudiotest.Fake) sourcecontrol.IssueService {
	return sourcecontrol.NewIssueService(planFakeRepoRepo{}, pod)
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

// planHarness wires a Service whose plan path talks to the pod.
type planHarness struct {
	svc     *Service
	pod     *aestudiotest.Fake
	runs    *fakeRunStore
	planner *fakePlanner
	gates   *fakeGates
	starter *fakeStarter
}

func newPlanHarness(t *testing.T) *planHarness {
	t.Helper()
	pod := aestudiotest.New()
	h := &planHarness{
		pod:     pod,
		runs:    &fakeRunStore{},
		planner: &fakePlanner{},
		gates:   &fakeGates{},
		starter: &fakeStarter{},
	}
	host := newIssueSvcOnPod(pod)
	h.svc = NewService(Deps{})
	h.svc.SetPlanPath(PlanPathDeps{
		Milestones: host,
		// The same host, seen through the domain's issue-write surface: the
		// supersede path closes issues through the writer and milestones through
		// the milestone client, and both land on this one pod.
		Issues:  delivery.NewIssueWriter(host),
		Runs:    h.runs,
		Planner: h.planner,
		Gates:   h.gates,
		Starter: h.starter,
	})
	return h
}

// seedMilestones mints milestones m1..mn and answers the last number.
func (h *planHarness) seedMilestones(t *testing.T, n int) int {
	t.Helper()
	last := 0
	for i := 1; i <= n; i++ {
		res, err := h.pod.CreateMilestone(context.Background(), widgets, sourcecontrol.CreateMilestoneRequest{Title: fmt.Sprintf("m%d", i)})
		if err != nil {
			t.Fatal(err)
		}
		last = res.Number
	}
	return last
}

// issue seeds one issue into milestone and answers its number.
func (h *planHarness) issue(title, state string, milestone int, labels ...string) int {
	return h.pod.SeedIssue(widgets, sourcecontrol.IssueInfo{Title: title, State: state, Labels: labels}, milestone)
}

// opCount counts the pod calls of op so far.
func (h *planHarness) opCount(op string) int {
	n := 0
	for _, c := range h.pod.Calls() {
		if c.Op == op {
			n++
		}
	}
	return n
}

// milestoneReads answers the recorded filters of every milestone-issues read.
func (h *planHarness) milestoneReads() []sourcecontrol.MilestoneIssuesFilter {
	var out []sourcecontrol.MilestoneIssuesFilter
	for _, c := range h.pod.Calls() {
		if c.Op == aestudiotest.OpListMilestoneIssues {
			out = append(out, c.Milestone)
		}
	}
	return out
}

// issueNo answers issue n as the pod holds it now.
func (h *planHarness) issueNo(t *testing.T, n int) sourcecontrol.IssueInfo {
	t.Helper()
	for _, is := range h.pod.Issues(widgets) {
		if is.Number == n {
			return is
		}
	}
	t.Fatalf("issue #%d not on the pod", n)
	return sourcecontrol.IssueInfo{}
}

// milestoneOf answers the milestone issue n is in now (0: none).
func (h *planHarness) milestoneOf(t *testing.T, n int) int {
	t.Helper()
	ms, err := h.pod.ListMilestones(context.Background(), widgets, "all")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		members, err := h.pod.ListMilestoneIssues(context.Background(), widgets, sourcecontrol.MilestoneIssuesFilter{Number: m.Number, State: "all"})
		if err != nil {
			t.Fatal(err)
		}
		if slices.ContainsFunc(members, func(is sourcecontrol.IssueInfo) bool { return is.Number == n }) {
			return m.Number
		}
	}
	return 0
}

// milestoneState answers milestone m's state now.
func (h *planHarness) milestoneState(t *testing.T, m int) string {
	t.Helper()
	ms, err := h.pod.ListMilestones(context.Background(), widgets, "all")
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range ms {
		if x.Number == m {
			return x.State
		}
	}
	t.Fatalf("milestone %d not on the pod", m)
	return ""
}

// commentsOn answers every comment on issue n.
func (h *planHarness) commentsOn(t *testing.T, n int) []sourcecontrol.IssueComment {
	t.Helper()
	cs, err := h.pod.ListIssueComments(context.Background(), widgets, n, 100)
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

// ---- claim: the milestone is minted and the run row admitted -----------------

func TestClaimVersion_MintsTheMilestoneAndAdmitsTheRun(t *testing.T) {
	h := newPlanHarness(t)

	run, err := h.svc.claimVersion(context.Background(), "acme", "shop", spec.BuildScope{Tag: "v3"})
	if err != nil {
		t.Fatalf("claimVersion: %v", err)
	}
	if run.MilestoneNumber != 1 || run.MilestoneTitle != "v3" {
		t.Errorf("run = %+v, want milestone 1 titled v3", run)
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
	// Exactly ONE milestone create — the "+1" of the plan's 1+N budget — and,
	// with no earlier run row, nothing superseded.
	if n := h.opCount(aestudiotest.OpCreateMilestone); n != 1 {
		t.Errorf("milestone creates ×%d, want exactly 1", n)
	}
	if n := h.opCount(aestudiotest.OpCloseMilestone); n != 0 {
		t.Errorf("a first version must close no milestone, got %d closes", n)
	}
}

// GitHub's milestone-title uniqueness is case-SENSITIVE at create while its
// title filters are not, so a duplicate must recover the EXISTING number rather
// than mint a case-twin.
func TestClaimVersion_DoubleCreate_RecoversTheNumber(t *testing.T) {
	h := newPlanHarness(t)
	h.seedMilestones(t, 3)
	twin, err := h.pod.CreateMilestone(context.Background(), widgets, sourcecontrol.CreateMilestoneRequest{Title: "V3"})
	if err != nil {
		t.Fatal(err)
	}

	run, err := h.svc.claimVersion(context.Background(), "acme", "shop", spec.BuildScope{Tag: "v3"})
	if err != nil {
		t.Fatalf("claimVersion: %v", err)
	}
	if run.MilestoneNumber != twin.Number {
		t.Errorf("milestone = %d, want the existing case-twin %d", run.MilestoneNumber, twin.Number)
	}
	if ms, _ := h.pod.ListMilestones(context.Background(), widgets, "all"); len(ms) != 4 {
		t.Errorf("milestones = %d, want 4 — a case-twin must not be minted", len(ms))
	}
}

// The DB index is the mutex's authority: when TryAdmit loses, the click gets
// the same conflict the pre-check would have given it.
func TestClaimVersion_AdmissionRaceLost_IsAConflict(t *testing.T) {
	h := newPlanHarness(t)
	h.runs.refuse = true

	_, err := h.svc.claimVersion(context.Background(), "acme", "shop", spec.BuildScope{Tag: "v3"})
	if err != ErrBuildAlreadyRunning {
		t.Fatalf("err = %v, want ErrBuildAlreadyRunning", err)
	}
}

// From 4.13: AE Studio failing the milestone mint is still a 502 to the build
// slice, but the sentinel rides it (EdgeError.Err) and survives the handler's
// mapping (apierr.WithCause), so the edge answers 503 / 409 rather than 502.
func TestClaimVersion_AEStudioFailureKeepsItsSentinel(t *testing.T) {
	for _, sentinel := range []error{sourcecontrol.ErrAEStudioAbsent, sourcecontrol.ErrAEStudioUnavailable} {
		h := newPlanHarness(t)
		h.pod.FailOrg("acme", sentinel)

		_, err := h.svc.claimVersion(context.Background(), "acme", "shop", spec.BuildScope{Tag: "v3"})
		var ee *EdgeError
		if !errors.As(err, &ee) || ee.Status != 502 || !errors.Is(err, sentinel) {
			t.Fatalf("err = %v, want a 502 EdgeError carrying %v", err, sentinel)
		}
		if mapped := mapBuildRunError(err); !errors.Is(mapped, sentinel) {
			t.Fatalf("mapBuildRunError lost the sentinel: %v", mapped)
		}
	}
}

// ---- supersede ---------------------------------------------------------------

// §6: before v<N+1> exists, v<N>'s still-open work is closed with a superseded
// comment, its gates too, and then the milestone itself. The previous milestone
// is found through the RUN ROWS — never by matching titles against GitHub.
func TestSupersede_ClosesOpenWorkThenGatesThenTheMilestone(t *testing.T) {
	h := newPlanHarness(t)
	prev := h.seedMilestones(t, 6)
	h.runs.rows = []delivery.MilestoneRun{
		// Newest first, as the repository returns them. An incident run on an
		// even older milestone must not be mistaken for the previous version.
		{MilestoneNumber: prev, MilestoneTitle: "v2", Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild, State: delivery.RunStateFailed},
		{MilestoneNumber: 2, MilestoneTitle: "v1", Kind: delivery.RunKindTask, Origin: delivery.RunOriginIncidentAdoption, State: delivery.RunStateSucceeded},
	}
	// Planned work states its KIND, which is what makes it closeable here: an
	// armed issue carrying no kind is read as a DEFECT by the working set and by
	// supersede alike (delivery.WorkKindOf), so it would be carried forward
	// instead — see TestSupersede_CarriesOpenBugsForwardAndClosesThePlan.
	open := []int{
		h.issue("Implement orders", "open", prev, "aep", "development"),
		h.issue("Provision orders-db", "open", prev, "provision"),
		h.issue("Flaky checkout", "open", prev),
	}
	// An issue of the older milestone is not this supersede's business.
	older := h.issue("Old incident", "open", 2, "aep", "development")

	run, err := h.svc.claimVersion(context.Background(), "acme", "shop", spec.BuildScope{Tag: "v3"})
	if err != nil {
		t.Fatalf("claimVersion: %v", err)
	}

	// The read is scoped to the previous milestone — the NUMBER off the run row —
	// and to its open issues.
	reads := h.milestoneReads()
	if len(reads) == 0 || reads[0].Number != prev || reads[0].State != "open" {
		t.Fatalf("supersede reads = %+v, want milestone %d, open", reads, prev)
	}

	// Every open issue — work, gate and ledger alike — is commented and closed.
	for _, n := range open {
		cs := h.commentsOn(t, n)
		if len(cs) != 1 || !strings.Contains(cs[0].Body, "Superseded by v3") {
			t.Fatalf("issue %d comments = %+v, want one Superseded by v3 note", n, cs)
		}
		if got := h.issueNo(t, n); got.State != "closed" {
			t.Errorf("issue %d state = %q, want closed", n, got.State)
		}
	}
	if got := h.issueNo(t, older); got.State != "open" {
		t.Errorf("an issue of an older milestone was closed")
	}
	// Then the milestone itself; the version being cut is untouched.
	if got := h.milestoneState(t, prev); got != "closed" {
		t.Errorf("previous milestone state = %q, want closed", got)
	}
	if got := h.milestoneState(t, run.MilestoneNumber); got != "open" {
		t.Errorf("the new milestone was closed by supersede (%q)", got)
	}
	if n := h.opCount(aestudiotest.OpCloseMilestone); n != 1 {
		t.Errorf("milestone closes ×%d, want exactly 1", n)
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
// The armed issue with no kind at all is the case that reads as a bug WITHOUT
// saying so. It is the common human hand-over — adoption stamps the arming
// switch and deliberately no kind — and every working-set predicate in the loop
// works it as a bug (delivery.WorkKindOf). So supersede must read the same kind
// they do, or the next version cut silently CLOSES a defect somebody had
// adopted, with the issue's own labels saying it was work.
func TestSupersede_CarriesOpenBugsForwardAndClosesThePlan(t *testing.T) {
	h := newPlanHarness(t)
	prev := h.seedMilestones(t, 6)
	h.runs.rows = []delivery.MilestoneRun{
		{MilestoneNumber: prev, MilestoneTitle: "v3", Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild, State: delivery.RunStateFailed},
	}
	plan := h.issue("Implement orders", "open", prev, "aep", "development")
	buildBug := h.issue("Fix the failing build for orders", "open", prev, "aep", "bug", "src/build")
	conflict := h.issue("Rebase aep/m6-1", "open", prev, "aep", "conflict")
	gate := h.issue("Provision orders-db", "open", prev, "provision")
	incident := h.issue("Main went red", "open", prev, "bug", "src/incident")
	halted := h.issue("Fix checkout", "open", prev, "aep", "bug", "aep:halted")
	adopted := h.issue("Checkout drops the cart", "open", prev, "aep")

	run, err := h.svc.claimVersion(context.Background(), "acme", "shop", spec.BuildScope{Tag: "v4"})
	if err != nil {
		t.Fatalf("claimVersion: %v", err)
	}
	next := run.MilestoneNumber

	// The bugs — armed or not, halted or not — are MOVED into v4's milestone, and
	// never closed.
	for _, n := range []int{buildBug, incident, halted, adopted} {
		if got := h.milestoneOf(t, n); got != next {
			t.Errorf("issue %d is in milestone %d, want a move into %d", n, got, next)
		}
		if got := h.issueNo(t, n); got.State != "open" {
			t.Errorf("issue %d was closed; a defect is not superseded by a new plan", n)
		}
	}
	// The unadopted incident arrives still unarmed.
	if slices.Contains(h.issueNo(t, incident).Labels, "aep") {
		t.Errorf("carrying a defect forward armed it")
	}
	// The plan, its gate and the conflict are CLOSED, and stay where they were.
	for _, n := range []int{plan, conflict, gate} {
		if got := h.issueNo(t, n); got.State != "closed" {
			t.Errorf("issue %d state = %q, want closed", n, got.State)
		}
		if got := h.milestoneOf(t, n); got != prev {
			t.Errorf("issue %d was carried forward into %d; only a bug is", n, got)
		}
	}
	// A rebuild is what CLEARS the halt: `aep:halted` says a run gave up and the
	// reconcile sweep must not restart it, so carrying it into the new version
	// would hide the bug from the sweep for the rest of the project's life.
	if slices.Contains(h.issueNo(t, halted).Labels, "aep:halted") {
		t.Errorf("the halt on the carried-forward bug was not cleared")
	}
	// And it is not spent on the bugs that never carried it: one removal only.
	if n := h.opCount(aestudiotest.OpRemoveIssueLabel); n != 1 {
		t.Errorf("label removals ×%d, want exactly 1 (the halt)", n)
	}
	// The new milestone exists BEFORE the move — that ordering is the whole
	// reason claimVersion mints it first — and supersede never closes it.
	if got := h.milestoneState(t, next); got != "open" {
		t.Errorf("supersede closed the milestone being cut (%q)", got)
	}
	if got := h.milestoneState(t, prev); got != "closed" {
		t.Errorf("previous milestone state = %q, want closed", got)
	}
}

// This is what keeps the reconcile sweep sound. The sweep starts a run for any
// known milestone holding open work and no live run, so a superseded milestone
// must hold none — and after the carry-forward it holds none for a DIFFERENT
// reason than before: the plan is closed and the bugs have LEFT.
func TestSupersede_LeavesTheOldMilestoneWithNothingToRestart(t *testing.T) {
	h := newPlanHarness(t)
	prev := h.seedMilestones(t, 6)
	h.runs.rows = []delivery.MilestoneRun{
		{MilestoneNumber: prev, MilestoneTitle: "v3", Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild, State: delivery.RunStateFailed},
	}
	h.issue("Implement orders", "open", prev, "aep", "development")
	h.issue("Fix orders", "open", prev, "aep", "bug", "src/build")

	if _, err := h.svc.claimVersion(context.Background(), "acme", "shop", spec.BuildScope{Tag: "v4"}); err != nil {
		t.Fatalf("claimVersion: %v", err)
	}

	// Every open issue was either closed or moved out. Nothing was left behind
	// for the sweep to find.
	left, err := h.pod.ListMilestoneIssues(context.Background(), widgets, sourcecontrol.MilestoneIssuesFilter{Number: prev, State: "open"})
	if err != nil || len(left) != 0 {
		t.Errorf("open issues left in the superseded milestone = %+v err=%v, want none", left, err)
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
	host := newIssueSvcOnPod(h.pod)
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

// TestClaimVersion_MilestoneIsTitledAfterTheVersion pins milestone identity:
// a claim names its milestone after the TAG, and the previous version's
// milestone is superseded.
func TestClaimVersion_MilestoneIsTitledAfterTheVersion(t *testing.T) {
	h := newPlanHarness(t)
	prev := h.seedMilestones(t, 2)
	// v3 is its own version and supersedes v2's milestone.
	h.runs.rows = []delivery.MilestoneRun{{
		OrgID: "acme", ProjectID: "shop", MilestoneNumber: prev,
		MilestoneTitle: "v2", Tag: "v2", Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild,
	}}

	run, err := h.svc.claimVersion(context.Background(), "acme", "shop", spec.BuildScope{Tag: "v3"})
	if err != nil {
		t.Fatalf("claimVersion: %v", err)
	}
	if run.MilestoneTitle != "v3" || run.Tag != "v3" || run.MilestoneNumber != 3 {
		t.Fatalf("run identity = %+v, want v3 / v3 / milestone 3", run)
	}
	if got := h.milestoneState(t, prev); got != "closed" {
		t.Errorf("the previous version's milestone must be closed, got %q", got)
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
	m := h.seedMilestones(t, 1)
	if err := h.pod.CloseMilestone(context.Background(), widgets, m); err != nil {
		t.Fatal(err)
	}
	marked := []int{
		h.issue("Implement orders", "closed", m, "aep", "development", "aep:cancelled"),
		h.issue("Provision orders-db", "closed", m, "provision", "aep:cancelled"),
	}
	delivered := h.issue("Implement checkout", "closed", m, "aep", "development")

	if filled := h.svc.reopenIncrement(context.Background(), "acme", "shop", m); !filled {
		t.Error("a milestone holding a `development` issue is FILLED — the run must skip its planning turn")
	}

	// The milestone itself comes back: a version being worked whose milestone reads
	// closed is a lie the console renders.
	if got := h.milestoneState(t, m); got != "open" {
		t.Fatalf("milestone state = %q, want open", got)
	}
	// The read is EVERY state, unfiltered by label. Unfiltered because which issues
	// this rebuild owns is decided from the labels, exactly as supersede decides
	// what it carries; every state because the same list answers a second question
	// — whether the milestone holds planned work at all — and a milestone whose
	// Tasks are still open is as filled as one whose Tasks a cancel closed.
	reads := h.milestoneReads()
	if len(reads) == 0 {
		t.Fatal("the rebuild never listed the milestone's issues")
	}
	if r := reads[0]; r.Number != m || r.State != "all" || len(r.Labels) != 0 {
		t.Errorf("the rebuild read %+v, want milestone %d, state all, no label filter", r, m)
	}
	// The marked set — the planned Task AND the gate the cancel closed with it —
	// is reopened with its mark cleared.
	for _, n := range marked {
		got := h.issueNo(t, n)
		if got.State != "open" {
			t.Errorf("issue %d state = %q, want reopened", n, got.State)
		}
		if slices.Contains(got.Labels, "aep:cancelled") {
			t.Errorf("issue %d kept its cancel mark", n)
		}
	}
	if n := h.opCount(aestudiotest.OpReopenIssue); n != len(marked) {
		t.Errorf("issue reopens ×%d, want %d", n, len(marked))
	}
	// And the Task the build had already DELIVERED stays closed and unmarked.
	if got := h.issueNo(t, delivered); got.State != "closed" {
		t.Errorf("issue %d was finished before the cancel and must not be reopened", delivered)
	}
}

// A version nobody cancelled takes the same branch and reopens nothing. The
// spec-save status is the only question asked — there is no "was it cancelled"
// read anywhere — and the marker being absent is what answers it.
func TestReopenIncrement_AVersionNobodyCancelledReopensNothing(t *testing.T) {
	h := newPlanHarness(t)
	m := h.seedMilestones(t, 1)
	n := h.issue("Implement orders", "closed", m, "aep", "development")

	h.svc.reopenIncrement(context.Background(), "acme", "shop", m)

	if got := h.issueNo(t, n); got.State != "closed" || h.opCount(aestudiotest.OpReopenIssue) != 0 {
		t.Errorf("an unmarked issue must stay closed (state %q)", got.State)
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
	m := h.seedMilestones(t, 1)
	h.issue("Provision orders-db", "open", m, "provision", "aep:dep/orders-db")
	h.issue("Provision roles and test users", "open", m, "provision")

	if filled := h.svc.reopenIncrement(context.Background(), "acme", "shop", m); filled {
		t.Error("gates are minted BEFORE the planning turn, so a milestone holding only gates was never planned")
	}
}

// An OPEN planned Task counts too. The two questions this one list answers want
// different states, and reading only the closed ones would call a milestone whose
// Tasks are all still open "unplanned" and plan it a second time.
func TestReopenIncrement_OpenPlannedWorkCountsAsFilled(t *testing.T) {
	h := newPlanHarness(t)
	m := h.seedMilestones(t, 1)
	h.issue("Implement orders", "open", m, "aep", "development")

	if filled := h.svc.reopenIncrement(context.Background(), "acme", "shop", m); !filled {
		t.Error("planned work that is still open is planned work")
	}
}

// A LIST FAILURE FAILS TOWARDS PLANNING, and the asymmetry is deliberate. The
// wrong "filled" settles an unbuilt version as delivered, silently; the wrong
// "not filled" spends one LLM turn that dedupes onto the milestone's own titles
// and mints nothing. Only one of those is recoverable by looking at the console.
func TestReopenIncrement_AnUnreadableMilestoneIsNotAssumedFilled(t *testing.T) {
	h := newPlanHarness(t)
	m := h.seedMilestones(t, 1)
	h.issue("Implement orders", "open", m, "aep", "development")
	h.pod.FailOp(aestudiotest.OpListMilestoneIssues, sourcecontrol.ErrAEStudioUnavailable)

	if filled := h.svc.reopenIncrement(context.Background(), "acme", "shop", m); filled {
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
	m := h.seedMilestones(t, 1)
	n := h.issue("Implement orders", "open", m, "aep", "development", "aep:cancelled")

	h.svc.reopenIncrement(context.Background(), "acme", "shop", m)

	if c := h.opCount(aestudiotest.OpReopenIssue); c != 0 {
		t.Errorf("an issue that is already open needs no reopen (%d reopens)", c)
	}
	if slices.Contains(h.issueNo(t, n).Labels, "aep:cancelled") || h.opCount(aestudiotest.OpRemoveIssueLabel) != 1 {
		t.Errorf("the cancel mark must be cleared exactly once")
	}
}
