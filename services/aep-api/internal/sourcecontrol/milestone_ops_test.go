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

package sourcecontrol_test

// The milestone surface at the service tier: the REAL issueService over the
// in-memory pod (newIssueSvcOnFake). The GitHub wire it rides — the
// case-sensitivity split, the 422 recovery, number-not-title addressing,
// pagination, PR exclusion, the one GraphQL round trip of the counts — is the
// pod's client's, pinned in ae-studio-tools' internal/github tests.

import (
	"errors"
	"slices"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

func TestCreateMilestone_CreatesWhenAbsent(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)

	res, err := svc.CreateMilestone(testContext(), "org1", "proj1", sourcecontrol.CreateMilestoneRequest{
		Title: "  v3 ", Description: "spec tag v3",
	})
	if err != nil {
		t.Fatalf("CreateMilestone: %v", err)
	}
	if res.Number != 1 || !res.Created {
		t.Fatalf("result = %+v, want {1 true}", res)
	}
	ms, err := f.ListMilestones(testContext(), widgets, "all")
	if err != nil || len(ms) != 1 || ms[0].Title != "v3" || ms[0].Description != "spec tag v3" {
		t.Fatalf("milestones = %+v err=%v, want one trimmed v3", ms, err)
	}
}

// A case-twin is adopted, never minted beside the original: the issues-list
// title filter is case-insensitive, so a twin pair would merge on every read.
func TestCreateMilestone_AdoptsACaseTwin(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	twin, err := f.CreateMilestone(testContext(), widgets, sourcecontrol.CreateMilestoneRequest{Title: "V3"})
	if err != nil {
		t.Fatal(err)
	}

	res, err := svc.CreateMilestone(testContext(), "org1", "proj1", sourcecontrol.CreateMilestoneRequest{Title: "v3"})
	if err != nil {
		t.Fatalf("CreateMilestone: %v", err)
	}
	if res.Number != twin.Number || res.Created {
		t.Fatalf("result = %+v, want {%d false}", res, twin.Number)
	}
}

func TestCreateMilestone_TitleRequired(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	if _, err := svc.CreateMilestone(testContext(), "org1", "proj1", sourcecontrol.CreateMilestoneRequest{Title: "  "}); err == nil {
		t.Fatal("want error for blank title, got nil")
	}
	if len(f.Calls()) != 0 {
		t.Fatalf("nothing must reach the pod, got %v", ops(f))
	}
}

func TestCloseAndReopenMilestone(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	m, err := f.CreateMilestone(testContext(), widgets, sourcecontrol.CreateMilestoneRequest{Title: "v1"})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.CloseMilestone(testContext(), "org1", "proj1", m.Number); err != nil {
		t.Fatalf("CloseMilestone: %v", err)
	}
	if closed, _ := svc.ListMilestones(testContext(), "org1", "proj1", "closed"); len(closed) != 1 || closed[0].Number != m.Number {
		t.Fatalf("closed milestones = %+v", closed)
	}
	if err := svc.ReopenMilestone(testContext(), "org1", "proj1", m.Number); err != nil {
		t.Fatalf("ReopenMilestone: %v", err)
	}
	if open, _ := svc.ListMilestones(testContext(), "org1", "proj1", "open"); len(open) != 1 {
		t.Fatalf("open milestones = %+v", open)
	}
}

func TestListMilestones_EveryState(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	for _, title := range []string{"v1", "v2"} {
		if _, err := f.CreateMilestone(testContext(), widgets, sourcecontrol.CreateMilestoneRequest{Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.CloseMilestone(testContext(), widgets, 1); err != nil {
		t.Fatal(err)
	}

	got, err := svc.ListMilestones(testContext(), "org1", "proj1", "all")
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	if len(got) != 2 || got[0].State != "closed" || got[1].State != "open" {
		t.Fatalf("milestones = %+v, want v1 closed and v2 open", got)
	}
}

// The milestone is addressed by NUMBER, labels AND-filter, and member pull
// requests never count as issues.
func TestListMilestoneIssues_FiltersByNumberStateAndLabels(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	m, err := f.CreateMilestone(testContext(), widgets, sourcecontrol.CreateMilestoneRequest{Title: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	both := f.SeedIssue(widgets, sourcecontrol.IssueInfo{Title: "planned", Labels: []string{"aep", "development"}}, m.Number)
	f.SeedIssue(widgets, sourcecontrol.IssueInfo{Title: "only armed", Labels: []string{"aep"}}, m.Number)
	f.SeedIssue(widgets, sourcecontrol.IssueInfo{Title: "closed", State: "closed", StateReason: "completed", Labels: []string{"aep", "development"}}, m.Number)
	f.SeedIssue(widgets, sourcecontrol.IssueInfo{Title: "elsewhere", Labels: []string{"aep", "development"}}, 0)
	f.SeedPullRequest(widgets, []string{"a.go"})

	got, err := svc.ListMilestoneIssues(testContext(), "org1", "proj1", sourcecontrol.MilestoneIssuesFilter{
		Number: m.Number, State: "open", Labels: []string{"aep", "development"},
	})
	if err != nil {
		t.Fatalf("ListMilestoneIssues: %v", err)
	}
	if len(got) != 1 || got[0].Number != both {
		t.Fatalf("issues = %+v, want only #%d", got, both)
	}
}

func TestListMilestoneIssues_NumberRequired(t *testing.T) {
	t.Parallel()
	svc, _ := newIssueSvcOnFake(t)
	if _, err := svc.ListMilestoneIssues(testContext(), "org1", "proj1", sourcecontrol.MilestoneIssuesFilter{}); err == nil {
		t.Fatal("want error for a zero milestone number, got nil")
	}
}

// The dispatch predicate input: the open populations of one milestone, read
// through the pod in one call.
func TestMilestoneIssueCounts_CountsTheMilestonesOpenPopulations(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	m, err := f.CreateMilestone(testContext(), widgets, sourcecontrol.CreateMilestoneRequest{Title: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, labels := range [][]string{
		{labelGate},
		{labelWork, labelDev}, {labelWork, labelDev},
		{labelWork, labelBug, labelSrcValid},
		{labelWork, labelValid},
		nil,
	} {
		f.SeedIssue(widgets, sourcecontrol.IssueInfo{Title: "i", Labels: labels}, m.Number)
	}
	f.SeedIssue(widgets, sourcecontrol.IssueInfo{Title: "done", State: "closed", Labels: []string{labelWork, labelDev}}, m.Number)

	counts, err := svc.MilestoneIssueCounts(testContext(), "org1", "proj1", m.Number)
	if err != nil {
		t.Fatalf("MilestoneIssueCounts: %v", err)
	}
	want := sourcecontrol.MilestoneIssueCounts{
		OpenProvision: 1, OpenTotal: 6,
		OpenAgentWork: 4, OpenDevelopment: 2, OpenValidation: 1,
		OpenValidationRepairs: 1,
	}
	if *counts != want {
		t.Fatalf("counts = %+v, want %+v", *counts, want)
	}
	// 4 armed issues, one of which is the validation task: 3 for the dev loop,
	// and 1 once the milestone's planned work is taken out as well.
	if got := counts.OpenDevWork(); got != 3 {
		t.Fatalf("OpenDevWork = %d, want 3", got)
	}
	if got := counts.OpenTaskWork(); got != 1 {
		t.Fatalf("OpenTaskWork = %d, want 1", got)
	}
}

// The label vocabulary, spelled here rather than imported: `sourcecontrol` is
// below `delivery` and must not depend on it. The literals are the same ones
// milestoneIssueCountsQuery embeds, which is the coupling under test.
const (
	labelWork  = "aep"
	labelDev   = "development"
	labelBug   = "bug"
	labelGate  = "provision"
	labelValid = "validation"
	// srcValidation is the SOURCE a failed verdict stamps on the repair work it
	// files. It is counted like a kind and subtracted like nothing: the bug-fix
	// loop reads it to decide whether draining its working set reopens the
	// version's validation task, and no working set is defined by it.
	labelSrcValid = "src/validation"
)

// hostCounts answers the populations the REAL host would report for a milestone
// holding these open issues.
//
// It exists because the shipped arithmetic was once wrong for exactly one
// reason: a belief that GraphQL's `labels:` argument intersects. It UNIONS — an
// issue matches when it carries ANY listed label. (The AND-semantics filter is
// the REST `?labels=a,b` parameter, a different API over the same resource.)
// Every case below is therefore stated as issues-and-labels and counted through
// this function, so no case can assert against a population GitHub cannot
// produce.
//
// Each field counts ONE label, exactly as each alias in the query filters on
// one — so this fake would still be honest if the argument were an intersection.
// That is deliberate: the query no longer depends on which of the two it is.
func hostCounts(issues ...[]string) *sourcecontrol.MilestoneIssueCounts {
	carrying := func(want string) int {
		n := 0
		for _, have := range issues {
			if slices.Contains(have, want) {
				n++
			}
		}
		return n
	}
	return &sourcecontrol.MilestoneIssueCounts{
		OpenProvision:         carrying(labelGate),
		OpenAgentWork:         carrying(labelWork),
		OpenDevelopment:       carrying(labelDev),
		OpenValidation:        carrying(labelValid),
		OpenValidationRepairs: carrying(labelSrcValid),
		OpenTotal:             len(issues),
	}
}

// TestMilestoneIssueCounts_WorkingSetArithmetic pins the ONE place each working
// set is computed, over the populations the host actually returns.
//
// Subtraction is exact here where inclusion-exclusion was not, because every
// subtracted kind is a strict SUBSET of the armed population: an armed issue
// carries exactly one kind, so it is counted once and removed at most once.
func TestMilestoneIssueCounts_WorkingSetArithmetic(t *testing.T) {
	t.Parallel()
	var (
		planned = []string{labelWork, labelDev}
		bug     = []string{labelWork, labelBug}
		gate    = []string{labelGate}
		valid   = []string{labelWork, labelValid}
		armed   = []string{labelWork} // armed by a human, not yet classified
		ledger  = []string(nil)
		// A failed verdict's repair work: an ordinary armed bug whose SOURCE
		// records where it came from.
		repair = []string{labelWork, labelBug, labelSrcValid}
	)
	cases := []struct {
		name     string
		counts   *sourcecontrol.MilestoneIssueCounts
		wantDev  int
		wantTask int
	}{
		{"a milestone of planned work", hostCounts(planned, planned, planned), 3, 0},
		{
			// Human-filed issues carrying no arming label are the milestone's
			// LEDGER: they inflate the total and are never work.
			"ledger issues are not work",
			hostCounts(ledger, ledger, ledger, ledger), 0, 0,
		},
		{
			// THE LIVE FAILURE, exactly as it stood: one task alongside one
			// provision gate. Read as an empty working set, the run settles a
			// version nobody built. A gate carries no arming label at all now, so
			// there is nothing left for it to be subtracted from.
			"a gate alongside real work leaves the work visible",
			hostCounts(planned, gate), 1, 0,
		},
		{
			"the validation task is nobody's working set",
			hostCounts(planned, planned, planned, gate, valid, ledger, ledger), 3, 0,
		},
		{
			// The whole reason the task working set exists: a bug-fix run works the
			// deployed version and must not pick up the build's planned work.
			"a bug is in both working sets, planned work only in dev",
			hostCounts(planned, planned, bug), 3, 1,
		},
		{
			// An armed issue with no kind is what a human's adoption produces. It
			// counts as work in BOTH sets — which is what delivery.InDevWorkingSet
			// answers for the same issue, and the safe direction besides: a stall
			// is visible where a silent settle is not.
			"an armed issue with no kind is work",
			hostCounts(armed, valid), 1, 1,
		},
		{
			// Not producible by hostCounts, and that is the point: the clamp guards
			// against a host that answers inconsistently, not against a milestone.
			"an inconsistent host cannot produce negative work",
			&sourcecontrol.MilestoneIssueCounts{OpenAgentWork: 0, OpenValidation: 2, OpenDevelopment: 1},
			0, 0,
		},
		{
			// A source is not a kind and NOTHING subtracts it: a repair bug is an
			// ordinary bug in both working sets, counted once. Reading the source as
			// an exclusion would empty the very working set the repair created.
			"verdict-sourced repair work is ordinary work",
			hostCounts(repair, repair, planned), 3, 2,
		},
		{"an unknown milestone has no work", nil, 0, 0},
	}
	for _, c := range cases {
		if got := c.counts.OpenDevWork(); got != c.wantDev {
			t.Errorf("OpenDevWork(%s) = %d, want %d (counts %+v)", c.name, got, c.wantDev, c.counts)
		}
		if got := c.counts.OpenTaskWork(); got != c.wantTask {
			t.Errorf("OpenTaskWork(%s) = %d, want %d (counts %+v)", c.name, got, c.wantTask, c.counts)
		}
	}
}

// TestMilestoneIssueCounts_AGateHoldsWorkItDoesNotErase is the live regression
// in one assertion pair: the working set of a freshly planned milestone is ONE
// task whether or not its provision gate is still open. The gate holds the
// dispatch (the predicate's other clause, in `delivery`); it must never make
// the milestone read as empty, because empty is what closes the version.
//
// It also pins the case a naive "just arm everything" would break: a gate
// carries NO arming label, so the gate count is the ONLY read that can see it.
// Counting gates through the armed population would make an open gate invisible
// and the run would dispatch straight past its own hold.
func TestMilestoneIssueCounts_AGateHoldsWorkItDoesNotErase(t *testing.T) {
	t.Parallel()
	gated := hostCounts([]string{labelWork, labelDev}, []string{labelGate})
	if got := gated.OpenDevWork(); got != 1 {
		t.Fatalf("working set behind an open gate = %d, want 1 (counts %+v)", got, gated)
	}
	if gated.OpenProvision != 1 {
		t.Fatalf("gates = %d, want 1", gated.OpenProvision)
	}
	if gated.OpenAgentWork != 1 {
		t.Fatalf("armed issues = %d, want 1 — a gate is not armed", gated.OpenAgentWork)
	}
	released := hostCounts([]string{labelWork, labelDev})
	if got := released.OpenDevWork(); got != 1 {
		t.Fatalf("working set after the gate closed = %d, want 1 (counts %+v)", got, released)
	}
	if released.OpenProvision != 0 {
		t.Fatalf("gates after the gate closed = %d, want 0", released.OpenProvision)
	}
}

// A milestone number that no longer resolves (deleted on GitHub) is a
// recoverable state, distinguishable from a transport failure.
func TestMilestoneIssueCounts_MissingMilestone(t *testing.T) {
	t.Parallel()
	svc, _ := newIssueSvcOnFake(t)

	_, err := svc.MilestoneIssueCounts(testContext(), "org1", "proj1", 404)
	if !errors.Is(err, sourcecontrol.ErrMilestoneNotFound) {
		t.Fatalf("err = %v, want ErrMilestoneNotFound", err)
	}
}

// GitHub's rate limit reaches the caller typed, so the run supervisor can
// wait it out rather than read it as a missing milestone.
func TestMilestoneIssueCounts_RateLimitIsTyped(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	f.FailOp(aestudiotest.OpMilestoneIssueCounts, &sourcecontrol.RateLimitedError{})

	_, err := svc.MilestoneIssueCounts(testContext(), "org1", "proj1", 9)
	var rl *sourcecontrol.RateLimitedError
	if !errors.As(err, &rl) || errors.Is(err, sourcecontrol.ErrMilestoneNotFound) {
		t.Fatalf("err = %v, want a RateLimitedError", err)
	}
}

// Assignment rides issue creation (one call, not create-then-patch).
func TestCreateIssue_AssignsMilestoneAtCreation(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	m, err := f.CreateMilestone(testContext(), widgets, sourcecontrol.CreateMilestoneRequest{Title: "v1"})
	if err != nil {
		t.Fatal(err)
	}

	res, err := svc.CreateIssue(testContext(), "org1", "proj1", sourcecontrol.CreateIssueRequest{
		Title: "Implement auth", Body: "do the thing", Milestone: &m.Number,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	members, _ := f.ListMilestoneIssues(testContext(), widgets, sourcecontrol.MilestoneIssuesFilter{Number: m.Number})
	if len(members) != 1 || members[0].Number != res.Number {
		t.Fatalf("milestone members = %+v, want #%d", members, res.Number)
	}
	if slices.Contains(ops(f), aestudiotest.OpSetIssueMilestone) {
		t.Fatalf("milestone set after creation: %v", ops(f))
	}
}

func TestCreateIssue_LeavesMilestoneUnsetWhenUnset(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	m, err := f.CreateMilestone(testContext(), widgets, sourcecontrol.CreateMilestoneRequest{Title: "v1"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CreateIssue(testContext(), "org1", "proj1", sourcecontrol.CreateIssueRequest{Title: "unassigned"}); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if members, _ := f.ListMilestoneIssues(testContext(), widgets, sourcecontrol.MilestoneIssuesFilter{Number: m.Number, State: "all"}); len(members) != 0 {
		t.Fatalf("an unassigned issue joined a milestone: %+v", members)
	}
}
