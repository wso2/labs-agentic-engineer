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

package eventcore

import (
	"reflect"
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// UNIT TIER — the four decisions, as pure functions.

func TestParseResolvesRefs(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []int
	}{
		{"one per line, every keyword", "Resolves #12\nCloses #13\nFixes #14", []int{12, 13, 14}},
		{"case and colon variants", "resolved: #7\nCLOSED #8\nfix #9", []int{7, 8, 9}},
		{"deduplicated, first-seen order", "Resolves #5\nCloses #5\nFixes #2", []int{5, 2}},
		{"a bare mention is not a claim", "See #12 for context", nil},
		{"cross-repo references are not milestone members", "Closes other/repo#12", nil},
		{"prose around the reference", "This resolves #12 in full.", []int{12}},
		{"nothing at all", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseResolvesRefs(c.body); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("parseResolvesRefs(%q) = %v, want %v", c.body, got, c.want)
			}
		})
	}
}

func TestMilestoneFromBranch(t *testing.T) {
	cases := []struct {
		ref  string
		want int
		ok   bool
	}{
		{"aep/m7-c1", 7, true},
		{"aep/m12-c3", 12, true},
		{"aep/m7", 7, true},
		{"feature/whatever", 0, false},
		{"aep/mx-c1", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := milestoneFromBranch(c.ref)
		if got != c.want || ok != c.ok {
			t.Errorf("milestoneFromBranch(%q) = (%d, %v), want (%d, %v)", c.ref, got, ok, c.want, c.ok)
		}
	}
}

func TestDecideAutoMerge(t *testing.T) {
	work := []sourcecontrol.IssueInfo{
		{Number: 12, State: "open", Labels: []string{delivery.LabelAgentWork, delivery.KindDevelopment}},
		{Number: 13, State: "open", Labels: []string{delivery.LabelAgentWork, delivery.KindBug, delivery.SrcBuild}},
		// A ledger issue: in the milestone, but unarmed — nobody works it.
		{Number: 14, State: "open", Labels: []string{"question"}},
		// The validation task: armed like any other agent work, and of a kind no
		// working set includes.
		{Number: 15, State: "open", Labels: []string{delivery.LabelAgentWork, delivery.KindValidation}},
		// A dispatch gate: the platform's, never claimable by a pull request.
		{Number: 16, State: "open", Labels: []string{delivery.KindProvision}},
		// A red-main incident: classified as a bug, but NOT armed. Classification
		// is not permission — nobody may claim it until a human arms it.
		{Number: 17, State: "open", Labels: []string{delivery.KindBug, delivery.SrcIncident}},
	}
	cases := []struct {
		name string
		// resolves is what a CLOSING keyword claimed; validates is what the
		// non-closing `Validates #N` claimed. They are separate arguments because
		// they are separate lists all the way through the policy — see
		// decideAutoMerge.
		resolves  []int
		validates []int
		want      bool
		matched   []int
		validated []int
	}{
		{"one armed issue is enough", []int{12}, nil, true, []int{12}, nil},
		{"several", []int{12, 13}, nil, true, []int{12, 13}, nil},
		{"a claim outside the milestone decides nothing", []int{99}, nil, false, nil, nil},
		{"a ledger issue is not agent work", []int{14}, nil, false, nil, nil},
		{"partial match still merges", []int{99, 12}, nil, true, []int{12}, nil},
		{"claiming nothing never merges", nil, nil, false, nil, nil},
		// The validation cycle's pull request IS this run's work. It reaches the
		// policy through the NON-closing reference, because the platform owns the
		// task's close — and declining it strands the tests and the report
		// unmerged, so no run could ever read a verdict from them.
		{"a validation PR carrying only Validates #N merges", nil, []int{15}, true, nil, []int{15}},
		// The scope is what stops `Validates` becoming a general-purpose way to get
		// a pull request merged while closing nothing: the working set would never
		// empty, because the issues it claimed would still be open.
		{"a coding PR carrying only Validates #N is declined", nil, []int{12}, false, nil, nil},
		{"Validates on a ledger issue is declined", nil, []int{14}, false, nil, nil},
		{"Validates on a gate is declined", nil, []int{16}, false, nil, nil},
		// A closing keyword aimed at the validation task still merges — the arming
		// label admits it — but the platform's close is what actually ends the task.
		{"the validation task is still claimable by a closing keyword", []int{15}, nil, true, []int{15}, nil},
		{"a dispatch gate is not claimable", []int{16}, nil, false, nil, nil},
		{"an unarmed bug is not this run's work", []int{17}, nil, false, nil, nil},
		// Both lists populated: each is matched against its own population, and
		// neither is folded into the other.
		{"both references are kept apart", []int{12}, []int{15}, true, []int{12}, []int{15}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := decideAutoMerge(c.resolves, c.validates, work)
			if got.Merge != c.want {
				t.Fatalf("Merge = %v (%s), want %v", got.Merge, got.Reason, c.want)
			}
			if !reflect.DeepEqual(got.Matched, c.matched) {
				t.Fatalf("Matched = %v, want %v", got.Matched, c.matched)
			}
			if !reflect.DeepEqual(got.Validated, c.validated) {
				t.Fatalf("Validated = %v, want %v", got.Validated, c.validated)
			}
		})
	}
}

// TestParseValidatesRefs pins the non-closing reference the validation cycle
// carries, and — the half that matters — that it stays OUT of the Resolves list.
//
// The two parses must never merge: a coding pull request's Resolves list is the
// durable record of what that cycle finished (RunCycle.Resolves), so folding a
// reference nothing closed into it would claim work that is still open.
func TestParseValidatesRefs(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		validates []int
		resolves  []int
	}{
		{"the canonical form", "Validates #15", []int{15}, nil},
		{"present tense", "validate #15", []int{15}, nil},
		{"past tense", "Validated #15", []int{15}, nil},
		{"colon form", "Validates: #15", []int{15}, nil},
		{"deduplicated, first-seen order", "Validates #15\nValidates #9\nValidates #15", []int{15, 9}, nil},
		{"a cross-repo reference is not a milestone member", "Validates owner/repo#15", nil, nil},
		{"no reference at all", "just some prose", nil, nil},
		// The load-bearing separation, both ways.
		{"Validates never enters the Resolves list", "Validates #15", []int{15}, nil},
		{"Closes never enters the Validates list", "Closes #15", nil, []int{15}},
		{"a body carrying both keeps them apart", "Closes #12\nValidates #15", []int{15}, []int{12}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseValidatesRefs(c.body); !reflect.DeepEqual(got, c.validates) {
				t.Errorf("parseValidatesRefs = %v, want %v", got, c.validates)
			}
			if got := parseResolvesRefs(c.body); !reflect.DeepEqual(got, c.resolves) {
				t.Errorf("parseResolvesRefs = %v, want %v", got, c.resolves)
			}
		})
	}
}

// TestDispatchable pins the predicate against the populations a milestone can
// actually hold. Every case is stated as the milestone's OPEN ISSUES and their
// labels, and turned into counts by hostCounts — the union filter the real host
// applies. Stating counts directly is how a predicate that reads every real
// milestone as empty passes its own tests.
//
// The cases that matter are the ones where "some issue is open" and "there is
// work to do" part company: a ledger-only milestone, a milestone whose only
// open work is the validation task, and an open gate.
func TestDispatchable(t *testing.T) {
	var (
		task   = []string{delivery.LabelAgentWork, delivery.KindDevelopment}
		bug    = []string{delivery.LabelAgentWork, delivery.KindBug, delivery.SrcBuild}
		gate   = []string{delivery.KindProvision}
		valid  = []string{delivery.LabelAgentWork, delivery.KindValidation}
		ledger = []string(nil)
		armed  = []string{delivery.LabelAgentWork} // armed by a human, unclassified
	)
	cases := []struct {
		name   string
		counts *sourcecontrol.MilestoneIssueCounts
		want   bool
	}{
		{"one open task, no gate", hostCounts(task), true},
		{"one open bug, no gate", hostCounts(bug), true},
		{
			// The regression this predicate exists to prevent: unarmed human-filed
			// issues are the milestone's LEDGER. They are never worked, so a
			// milestone holding nothing else has an empty working set.
			"only ledger issues are open",
			hostCounts(ledger, ledger, ledger, ledger), false,
		},
		{
			// The validation task belongs to the run's validation cycle, not to a
			// coding cycle, so it is not work a dispatch can pick up — even though
			// it is armed.
			"only the validation task is open", hostCounts(valid), false,
		},
		{
			// A human armed a bare issue and classified nothing. It IS work: the
			// counts and delivery.InDevWorkingSet must give the same answer, and a
			// stall is visible where a silent settle is not.
			"an armed issue with no kind is dispatchable", hostCounts(armed), true,
		},
		{
			// THE LIVE FAILURE. One planned task alongside one provision gate: the
			// gate holds dispatch, but the task is real work and the milestone is
			// NOT empty. Read as empty, the run settles a version nobody built.
			"a gate alongside one real task holds dispatch",
			hostCounts(task, gate), false,
		},
		{
			// …and the moment the gate closes, that same task is dispatchable. This
			// is the pair that catches an arithmetic which always answers zero.
			"the gate closes and the task is dispatchable",
			hostCounts(task), true,
		},
		{
			"an open gate holds dispatch even with work waiting",
			hostCounts(task, task, task, gate), false,
		},
		{
			// A gate alone: it holds dispatch AND is not work, so neither clause of
			// the predicate can be satisfied by it.
			"only a gate is open", hostCounts(gate), false,
		},
		{"everything closed", hostCounts(), false},
		{"unknown milestone", nil, false},
	}
	for _, c := range cases {
		if got := dispatchable(c.counts); got != c.want {
			t.Errorf("dispatchable(%s) = %v, want %v (counts %+v)", c.name, got, c.want, c.counts)
		}
	}
}

// TestDispatchable_TheGateIsTheOnlyThingHolding is the live failure told as the
// sequence it actually was: a freshly planned milestone holds one coding task
// and one provision gate, and the ONLY reason it may not dispatch is the gate.
// Its working set is one issue throughout — before the gate resolves and after.
func TestDispatchable_TheGateIsTheOnlyThingHolding(t *testing.T) {
	gated := hostCounts(
		[]string{delivery.LabelAgentWork, delivery.KindDevelopment}, // the planned task
		[]string{delivery.KindProvision},                            // the dependency gate
	)
	if got, want := gated.OpenDevWork(), 1; got != want {
		t.Fatalf("working set behind the gate = %d, want %d (counts %+v)", got, want, gated)
	}
	if gated.OpenProvision != 1 {
		t.Fatalf("gates = %d, want 1", gated.OpenProvision)
	}
	if dispatchable(gated) {
		t.Fatal("an open gate must hold the dispatch")
	}

	released := hostCounts([]string{delivery.LabelAgentWork, delivery.KindDevelopment})
	if got, want := released.OpenDevWork(), 1; got != want {
		t.Fatalf("working set after the gate closed = %d, want %d", got, want)
	}
	if !dispatchable(released) {
		t.Fatal("the closed gate must release the task the milestone was holding")
	}
}

// TestAttemptsFor pins the budget's counting rule, including the two ways it
// could over-count: another component's runs, and the same component at a
// different commit.
func TestAttemptsFor(t *testing.T) {
	runs := []BuildRun{
		{Name: delivery.BuildRunName("proj1", "order-service", "abc123def456789", 1)},
		{Name: delivery.BuildRunName("proj1", "order-service", "abc123def456789", 2)},
		{Name: delivery.BuildRunName("proj1", "order-service", "999999999999999", 1)},
		{Name: delivery.BuildRunName("proj1", "web", "abc123def456789", 1)},
	}
	if got := attemptsFor(runs, delivery.BuildRunNamePrefix("proj1", "order-service", "abc123def456789")); got != 2 {
		t.Fatalf("attempts for (order-service, abc123def456) = %d, want 2", got)
	}
	if got := attemptsFor(runs, delivery.BuildRunNamePrefix("proj1", "web", "abc123def456789")); got != 1 {
		t.Fatalf("attempts for (web, abc123def456) = %d, want 1", got)
	}
	if got := attemptsFor(nil, delivery.BuildRunNamePrefix("proj1", "web", "abc")); got != 0 {
		t.Fatalf("attempts with no runs = %d, want 0", got)
	}
}

// TestBudgetIsOnePerComponentPerSHA drives the rule through the function that
// enforces it, which is also the function that makes the fan-out idempotent —
// they are the same counting, so they cannot drift apart.
func TestBudgetIsOnePerComponentPerSHA(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	sha := "abc123def456789"

	// The merge fan-out: one run, and a redelivery adds none.
	for i := 0; i < 3; i++ {
		if _, err := h.events.ensureBuildRun(ctx, testOrg, testProject, "order-service", sha, staged("org-git-secret"), mergeBuildLimit); err != nil {
			t.Fatalf("fan-out %d: %v", i, err)
		}
	}
	if got := h.builds.triggeredFor("order-service"); len(got) != 1 {
		t.Fatalf("the merge fan-out allows exactly one run per (component, SHA), got %v", got)
	}

	// The red path allows one more, and never a third.
	attempt, err := h.events.ensureBuildRun(ctx, testOrg, testProject, "order-service", sha, staged("org-git-secret"), redBuildLimit)
	if err != nil || attempt != 2 {
		t.Fatalf("the first red must re-trigger attempt 2, got (%d, %v)", attempt, err)
	}
	attempt, err = h.events.ensureBuildRun(ctx, testOrg, testProject, "order-service", sha, staged("org-git-secret"), redBuildLimit)
	if err != nil || attempt != 0 {
		t.Fatalf("the budget is spent after one re-trigger, got (%d, %v)", attempt, err)
	}
	if got := h.builds.triggeredFor("order-service"); len(got) != 2 {
		t.Fatalf("exactly two runs total for one (component, SHA), got %v", got)
	}

	// A NEW commit starts a fresh allowance — the budget is per SHA.
	attempt, err = h.events.ensureBuildRun(ctx, testOrg, testProject, "order-service", "feedfacefeed0", staged("org-git-secret"), mergeBuildLimit)
	if err != nil || attempt != 1 {
		t.Fatalf("a new merge SHA gets its own attempt 1, got (%d, %v)", attempt, err)
	}
}
