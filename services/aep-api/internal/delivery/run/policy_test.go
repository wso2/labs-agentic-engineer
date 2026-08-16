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

package run

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// TestDispatchable pins the predicate that guards every cycle boundary. The
// cases that matter are the two where "the milestone has open issues" and "the
// run may dispatch" part company: a gate holding a non-empty working set, and a
// milestone whose only open issues are ledger entries.
func TestDispatchable(t *testing.T) {
	cases := []struct {
		name string
		snap MilestoneSnapshot
		want bool
	}{
		{"work, no gate", MilestoneSnapshot{Work: 2, Total: 2}, true},
		{"a gate holds the dispatch", MilestoneSnapshot{Work: 2, Gates: 1, Total: 3}, false},
		{"ledger only — open issues, nothing to work", MilestoneSnapshot{Work: 0, Total: 4}, false},
		{"empty milestone", MilestoneSnapshot{}, false},
		{"a gate with nothing to hold", MilestoneSnapshot{Work: 0, Gates: 1, Total: 1}, false},
	}
	for _, c := range cases {
		if got := Dispatchable(c.snap); got != c.want {
			t.Errorf("Dispatchable(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestNextCycleKind pins the mapping from a cycle's outcome to what the next
// cycle is FOR — which is the same thing as which budget it spends.
func TestNextCycleKind(t *testing.T) {
	cases := map[cycleResult]string{
		cycleNone:     delivery.CycleKindCoding,
		cycleGreen:    delivery.CycleKindCoding,
		cycleRed:      delivery.CycleKindFix,
		cycleConflict: delivery.CycleKindConflict,
	}
	for previous, want := range cases {
		if got := nextCycleKind(previous); got != want {
			t.Errorf("nextCycleKind(%v) = %q, want %q", previous, got, want)
		}
	}
}

// TestBudgetRefusal pins every budget that can refuse a cycle, and the ORDER
// they are consulted in: the chain budget names the immediate cause, so it wins
// over the ceiling when both are spent.
func TestBudgetRefusal(t *testing.T) {
	cases := []struct {
		name                           string
		kind                           string
		cycles, fix, conflict, ceiling int
		want                           string
	}{
		{"a first coding cycle is free", delivery.CycleKindCoding, 0, 0, 0, 8, ""},
		{"the fix chain runs out", delivery.CycleKindFix, 3, delivery.RunMaxFixCycles, 0, 8, delivery.RunReasonFixChainBudget},
		{"one fix left", delivery.CycleKindFix, 3, delivery.RunMaxFixCycles - 1, 0, 8, ""},
		{"the conflict chain runs out", delivery.CycleKindConflict, 3, 0, delivery.RunMaxConflictCycles, 8, delivery.RunReasonConflictBudget},
		{"the ceiling stops an ordinary cycle", delivery.CycleKindCoding, 8, 0, 0, 8, delivery.RunReasonCycleCeiling},
		{"the ceiling stops a validation cycle too", delivery.CycleKindValidation, 8, 0, 0, 8, delivery.RunReasonCycleCeiling},
		{
			"the chain budget names the cause before the ceiling does",
			delivery.CycleKindFix, 8, delivery.RunMaxFixCycles, 0, 8, delivery.RunReasonFixChainBudget,
		},
		{"no ceiling configured cannot refuse", delivery.CycleKindCoding, 99, 0, 0, 0, ""},
	}
	for _, c := range cases {
		if got := budgetRefusal(c.kind, c.cycles, c.fix, c.conflict, c.ceiling); got != c.want {
			t.Errorf("budgetRefusal(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestNoProgress pins the rule that stops a run looping on work it cannot
// finish — including the deliberate coarseness: only a GREEN cycle is judged,
// because a red or conflicted one mints its own work and has its own budget.
func TestNoProgress(t *testing.T) {
	cases := []struct {
		name     string
		previous cycleResult
		before   int
		after    int
		want     bool
	}{
		{"a green cycle that closed nothing", cycleGreen, 3, 3, true},
		{"a green cycle that grew the milestone", cycleGreen, 3, 4, true},
		{"a green cycle that closed one", cycleGreen, 3, 2, false},
		{"a red cycle is judged by its fix budget", cycleRed, 3, 3, false},
		{"a conflicted cycle is judged by its conflict budget", cycleConflict, 3, 3, false},
		{"the first boundary of a run", cycleNone, 0, 3, false},
	}
	for _, c := range cases {
		if got := noProgress(c.previous, c.before, c.after); got != c.want {
			t.Errorf("noProgress(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestClassifyComponentBuild pins how a component's verdict is read off the
// WorkflowRuns themselves — the same runs the event plane's automatic
// re-trigger budget is derived from, which is what keeps the two halves from
// disagreeing about when red means red.
func TestClassifyComponentBuild(t *testing.T) {
	prefix := delivery.BuildRunNamePrefix("proj1", "order-service", "abc123def4567890")
	other := delivery.BuildRunName("proj1", "order-service", "999999999999", 1)

	cases := []struct {
		name string
		runs []BuildRunInfo
		want componentBuildState
	}{
		{"nothing triggered yet", nil, buildPending},
		{
			"triggered, still running",
			[]BuildRunInfo{{Name: delivery.BuildRunName("proj1", "order-service", "abc123def4567890", 1)}},
			buildPending,
		},
		{
			"one green attempt",
			[]BuildRunInfo{{Name: delivery.BuildRunName("proj1", "order-service", "abc123def4567890", 1), Terminal: true, Succeeded: true}},
			buildGreen,
		},
		{
			"the first red is a flake until the re-trigger reports",
			[]BuildRunInfo{{Name: delivery.BuildRunName("proj1", "order-service", "abc123def4567890", 1), Terminal: true}},
			buildPending,
		},
		{
			"red through the whole allowance is a verdict",
			[]BuildRunInfo{
				{Name: delivery.BuildRunName("proj1", "order-service", "abc123def4567890", 1), Terminal: true},
				{Name: delivery.BuildRunName("proj1", "order-service", "abc123def4567890", 2), Terminal: true},
			},
			buildRed,
		},
		{
			"a re-trigger that passed wins over the first failure",
			[]BuildRunInfo{
				{Name: delivery.BuildRunName("proj1", "order-service", "abc123def4567890", 1), Terminal: true},
				{Name: delivery.BuildRunName("proj1", "order-service", "abc123def4567890", 2), Terminal: true, Succeeded: true},
			},
			buildGreen,
		},
		{
			"another commit's failures do not count",
			[]BuildRunInfo{{Name: other, Terminal: true}, {Name: other, Terminal: true}},
			buildPending,
		},
	}
	for _, c := range cases {
		if got := classifyComponentBuild(c.runs, prefix); got != c.want {
			t.Errorf("classifyComponentBuild(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestCycleBuildStateGreen pins the "have all this cycle's builds reported?"
// arithmetic, including the case that is green precisely BECAUSE nothing was
// built: a validation cycle's pull request carries only tests and a report.
func TestCycleBuildStateGreen(t *testing.T) {
	cases := []struct {
		name  string
		state CycleBuildState
		want  bool
	}{
		{"nothing to build", CycleBuildState{}, true},
		{"all reported", CycleBuildState{Expected: 2, Settled: 2}, true},
		{"one still building", CycleBuildState{Expected: 2, Settled: 1}, false},
		{"one red", CycleBuildState{Expected: 2, Settled: 2, Red: []string{"web"}}, false},
	}
	for _, c := range cases {
		if got := c.state.Green(); got != c.want {
			t.Errorf("CycleBuildState.Green(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

// A cycle can expire with SOME components serving and others still rolling out.
// The deadline must report only the ones that did not come up — filing fix work
// against a component that deployed fine sends an agent after nothing.
func TestClassifyCycleDeploys_PendingNamesOnlyTheUnsettled(t *testing.T) {
	t.Parallel()
	got := classifyCycleDeploys(3, []delivery.ComponentDeploy{
		{Component: "ready-one", Ready: true},
		{Component: "still-rolling"},
		{Component: "ready-two", Ready: true},
	})
	if got.Ready != 2 {
		t.Errorf("Ready = %d, want 2", got.Ready)
	}
	if len(got.Pending) != 1 || got.Pending[0] != "still-rolling" {
		t.Errorf("Pending = %v, want only the component that never settled", got.Pending)
	}
	if got.Green() {
		t.Error("a partially deployed cycle must not read as green")
	}
}

// A failed component is named AND carries OpenChoreo's reason, while a pending
// one is neither — the three-way split the supervisor branches on.
func TestClassifyCycleDeploys_SeparatesFailedFromPending(t *testing.T) {
	t.Parallel()
	got := classifyCycleDeploys(2, []delivery.ComponentDeploy{
		{Component: "broken", Failed: true, Reason: "RenderingFailed"},
		{Component: "rolling"},
	})
	if len(got.Failed) != 1 || got.Failed[0] != "broken" {
		t.Errorf("Failed = %v", got.Failed)
	}
	if got.Reasons["broken"] != "RenderingFailed" {
		t.Errorf("Reasons = %v, want the condition reason carried through", got.Reasons)
	}
	if len(got.Pending) != 1 || got.Pending[0] != "rolling" {
		t.Errorf("Pending = %v", got.Pending)
	}
}
