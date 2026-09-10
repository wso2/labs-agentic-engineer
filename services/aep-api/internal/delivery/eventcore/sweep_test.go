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
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

func sweepOver(h *harness) *Sweep {
	return NewSweep(h.events, fakeRepoLister{repos: []RepoRef{
		{OrgID: testOrg, ProjectID: testProject, FullName: testRepo},
	}}, 0)
}

// TestSweep_StartsARunForUnworkedOpenIssues is the backstop's whole job: a
// milestone with open work and nobody on it. It heals a delivery GitHub never
// made and the adoption-versus-settle race, which leave the same footprint.
func TestSweep_StartsARunForUnworkedOpenIssues(t *testing.T) {
	h := newHarness(t, aRun("run-old", 7, delivery.RunStateSucceeded))
	h.issues.withDefects(7, 21, 22)

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 1 || h.sup.started[0].MilestoneNumber != 7 ||
		h.sup.started[0].Kind != delivery.RunKindTask {
		t.Fatalf("the sweep must start a run over the unworked milestone, got %+v", h.sup.started)
	}
}

// TestSweep_ReOffersALiveRow is the wedge test.
//
// A live ROW is not a live WORKFLOW. Nothing else in the platform notices a row
// whose execution is gone, and because a non-terminal row makes
// LiveRunForMilestone answer forever, the sweep's open-work rule would skip it
// forever — while the partial unique indexes refuse every later run on that
// project. Re-offering is what heals it, and it is safe because StartRun is
// idempotent: a running execution answers AlreadyStarted and the row is reused
// rather than re-admitted.
//
// ZERO open issues is the case that matters. That is exactly what hasOpenWork
// skips, so a row stranded before its milestone was filled is the one the old
// rule could never reach.
func TestSweep_ReOffersALiveRow(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.issues.withCounts(7, 0, 0, 0)

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 1 || h.sup.started[0].MilestoneNumber != 7 {
		t.Fatalf("a live row must be re-offered so a lost workflow is restarted, got %+v", h.sup.started)
	}
	// A re-offer resumes a run; it must never re-derive the version.
	if h.sup.started[0].Tag != "" || len(h.sup.started[0].ProvisionInputs) != 0 {
		t.Errorf("a re-offer must carry no planning inputs, got %+v", h.sup.started[0])
	}
}

// TestSweep_NeverReOffersARunStillPlanning — the one live row the sweep must
// leave alone.
//
// Re-offering it would start a fresh workflow with no Tag and no provision
// inputs (those ride the request, not the row), so the run would skip its
// planning phase entirely and settle an UNPLANNED version as delivered. A
// planning row belongs to the click, which starts the workflow synchronously
// and settles the row when it cannot.
func TestSweep_NeverReOffersARunStillPlanning(t *testing.T) {
	h := newHarness(t, aRun("run-1", 7, delivery.RunStatePlanning))
	h.issues.withCounts(7, 0, 0, 0)

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("a run still planning must not be re-offered, got %+v", h.sup.started)
	}
}

// TestSweep_NeverResurrectsASupersededMilestone is the property that makes the
// backstop safe to run forever: supersede closes the previous version's open
// issues before the next milestone is minted, so an abandoned milestone has no
// open work for the sweep to find.
func TestSweep_NeverResurrectsASupersededMilestone(t *testing.T) {
	// FAILED rather than cancelled on purpose: a cancelled increment is skipped by
	// its own rule (see below), which would let this one pass even if supersede
	// stopped emptying the milestone.
	h := newHarness(t, aRun("run-old", 6, delivery.RunStateFailed))
	h.issues.withCounts(6, 0, 0, 0) // superseded: everything closed

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("an abandoned milestone holds no open work and must stay abandoned, got %+v", h.sup.started)
	}
}

// TestSweep_AnOpenGateDoesNotStopARunFromStarting — a gate holds DISPATCH, not
// the run. Healing into "started and waiting" is the correct repair.
func TestSweep_AnOpenGateDoesNotStopARunFromStarting(t *testing.T) {
	h := newHarness(t, aRun("run-old", 7, delivery.RunStateFailed))
	h.issues.withDefects(7, 21).withIssue(7, 60, delivery.KindProvision)

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 1 {
		t.Fatalf("a gated milestone with work still needs a run to wait on it, got %+v", h.sup.started)
	}
}

// TestSweep_IsInertWithoutRunRows — the same gate as every handler: the sweep
// walks the milestones the PLATFORM has run, so a project it has never run for
// is invisible to it.
func TestSweep_IsInertWithoutRunRows(t *testing.T) {
	h := newHarness(t)
	h.issues.withCounts(7, 0, 5, 5) // GitHub is full of open issues

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("a milestone the platform never ran is somebody else's, got %+v", h.sup.started)
	}
}

// TestSweep_StartsAValidationRunForAnOpenValidationTask is the trigger the split
// created, and the only thing that turns a filed validation task into a verdict.
//
// A dev run settles at deployed-green having minted that task and never judges
// the version itself. Nothing else is watching: the task produces no webhook the
// platform reacts to, so if this pass did not route by KIND the version would sit
// deployed and unjudged forever with an open issue in its milestone.
func TestSweep_StartsAValidationRunForAnOpenValidationTask(t *testing.T) {
	h := newHarness(t, aRun("run-dev", 7, delivery.RunStateSucceeded))
	h.issues.withValidationIssue(7, 55)

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 1 {
		t.Fatalf("want one run started, got %+v", h.sup.started)
	}
	got := h.sup.started[0]
	if got.Kind != delivery.RunKindValidation {
		t.Fatalf("kind = %q, want %q — an open validation task is judged, not worked",
			got.Kind, delivery.RunKindValidation)
	}
	if got.MilestoneNumber != 7 {
		t.Fatalf("milestone = %d, want 7", got.MilestoneNumber)
	}
	// No attempt allowance rides the trigger: the per-version allowance is counted
	// from the ledger, so a sweep-started attempt cannot widen what a version is
	// allowed.
	if got.ValidationAttempts != 0 {
		t.Errorf("ValidationAttempts = %d, want 0 (the platform default)", got.ValidationAttempts)
	}
}

// The routing is by KIND, not by "something is open". Ordinary work still gets an
// ordinary run — the two must not collapse into one another, because a validation
// run has no working set and would judge a version whose work is unfinished.
func TestSweep_RoutesOrdinaryWorkToATaskRun(t *testing.T) {
	h := newHarness(t, aRun("run-old", 7, delivery.RunStateSucceeded))
	h.issues.withDefects(7, 21, 22)

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 1 || h.sup.started[0].Kind != delivery.RunKindTask {
		t.Fatalf("open work must start a task run, got %+v", h.sup.started)
	}
}

// Validation wins when both populations are open, and the cost is nothing in
// practice: a dev run files the task only at deployed-green with the working set
// already empty, and a failed attempt's repair issues are filed after the task has
// been closed. They coexist only when a human files work into a version awaiting
// its verdict, and judging first is the safe order there — the verdict is about
// what is DEPLOYED, which the new work has not changed yet.
func TestSweep_ValidationTaskWinsOverOrdinaryWork(t *testing.T) {
	h := newHarness(t, aRun("run-old", 7, delivery.RunStateSucceeded))
	h.issues.withDefects(7, 21).withValidationIssue(7, 55)

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 1 || h.sup.started[0].Kind != delivery.RunKindValidation {
		t.Fatalf("an open validation task must be judged first, got %+v", h.sup.started)
	}
}

// An UNARMED issue of kind `validation` is not a validation task — the arming
// switch is what says a loop may work it at all. Routing on the kind alone would
// let a human's stray label start a paid agent run.
//
// Nor is it work for anything else. It is in no working set, so nothing starts:
// "an issue is open" was never a reason to spend an agent.
func TestSweep_AnUnarmedValidationLabelStartsNothing(t *testing.T) {
	h := newHarness(t, aRun("run-old", 7, delivery.RunStateSucceeded))
	h.issues.withOpenIssues(7, []string{delivery.KindValidation})

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("an unarmed label is nobody's work — nothing may start, got %+v", h.sup.started)
	}
}

// A dispatch GATE alone starts NOTHING. A gate holds the next DISPATCH, so with
// no work behind it there is nothing for it to hold — and a run started to "wait
// on it" would park on an empty working set, on a milestone nobody is building,
// and be offered again on every 60-second pass.
func TestSweep_AGateAloneStartsNothing(t *testing.T) {
	h := newHarness(t, aRun("run-old", 7, delivery.RunStateFailed))
	h.issues.withOpenIssues(7, []string{delivery.KindProvision})

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("a gate is a hold, not work — nothing may start, got %+v", h.sup.started)
	}
}

// A human's LEDGER NOTE starts nothing. Unarmed is the whole meaning of ledger:
// the platform does not work it until somebody arms it, and arming raises the
// adoption path directly rather than through this pass.
func TestSweep_ALedgerNoteAloneStartsNothing(t *testing.T) {
	h := newHarness(t, aRun("run-old", 7, delivery.RunStateSucceeded))
	h.issues.withIssue(7, 61) // no labels at all: a note in the version's record

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("a human's note is not work — nothing may start, got %+v", h.sup.started)
	}
}

// PLANNED WORK left open starts nothing either, and this is the population the
// routing must be most careful about. `development` is dev-workflow's alone — a
// task run's working set excludes it deliberately — and only the build click may
// start a dev run, because it carries the version mutex and the tag. Offering a
// task run here dispatched an agent whose working set was empty by construction.
func TestSweep_PlannedWorkAloneStartsNothing(t *testing.T) {
	h := newHarness(t, aRun("run-old", 7, delivery.RunStateFailed))
	h.issues.withWork(7, 21, 22) // armed `development`, left open by a build that gave up

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("planned work waits for another BUILD, not for a task run, got %+v", h.sup.started)
	}
}

// TestSweep_SkipsWorkAFailedRunHalted is what makes a budget mean something.
//
// A run that exhausts one settles `failed` and leaves its working set OPEN,
// because the milestone stays open too — the way forward from a failed increment
// is more work in the same version. To this pass those leftovers look exactly
// like work nobody started, so without the halted marker it would start a fresh
// run on them, with fresh budgets, which would exhaust them and be replaced
// again. Every budget in the platform defeated at once, and the symptom is a
// cloud bill rather than a failing test.
func TestSweep_SkipsWorkAFailedRunHalted(t *testing.T) {
	h := newHarness(t, aRun("run-old", 7, delivery.RunStateFailed))
	h.issues.withIssue(7, 21, delivery.LabelAgentWork, delivery.KindBug, delivery.LabelHalted)

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("halted work must not be restarted with a fresh budget, got %+v", h.sup.started)
	}
}

// The mark is on the ISSUES a run gave up on, never on the milestone. So work
// filed afterwards — by a human, by an incident, by the next validation attempt —
// is ordinary unworked work and starts a run, and the halted issue beside it is
// simply not part of what that run may pick up (the working-set predicates are
// what enforce the second half; this pass only decides whether to start at all).
func TestSweep_ANewIssueBesideHaltedWorkStillStartsARun(t *testing.T) {
	h := newHarness(t, aRun("run-old", 7, delivery.RunStateFailed))
	h.issues.
		withIssue(7, 21, delivery.LabelAgentWork, delivery.KindBug, delivery.LabelHalted).
		withIssue(7, 22, delivery.LabelAgentWork, delivery.KindBug, delivery.SrcUser)

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 1 || h.sup.started[0].Kind != delivery.RunKindTask {
		t.Fatalf("a freshly filed issue is unworked work and must start a run, got %+v", h.sup.started)
	}
}

// A HALTED validation task would still be judged. It is not a state the platform
// produces — a validation run closes its task on every ending, and a halt only
// ever reaches a working set the run polled — but the routing must not depend on
// that: the marker is read before the kind, so an issue somebody hand-stamped
// stays out of the trigger whatever its kind.
func TestSweep_AHaltedValidationTaskIsNotJudged(t *testing.T) {
	h := newHarness(t, aRun("run-dev", 7, delivery.RunStateSucceeded))
	h.issues.withIssue(7, 55, delivery.LabelAgentWork, delivery.KindValidation, delivery.LabelHalted)

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("a halted issue is invisible to the trigger router, got %+v", h.sup.started)
	}
}

// ---- the cancelled increment -------------------------------------------------

// A cancelled increment is ABANDONED, and the milestone is skipped whole.
//
// The issues being closed is not enough on its own, which is what this pins. A
// closed milestone still accepts issues, so a person reopening one — or filing a
// new bug into the version they just cancelled — would otherwise start a task run
// that builds and deploys against a version nobody is shipping.
func TestSweep_SkipsAMilestoneWhoseIncrementWasCancelled(t *testing.T) {
	h := newHarness(t, aRun("run-cancelled", 7, delivery.RunStateCancelled))
	// Open work, deliberately: the point is that the milestone's STATE decides,
	// not its issues.
	h.issues.withDefects(7, 21, 22)

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("a cancelled increment must stay abandoned, got %+v", h.sup.started)
	}
}

// TestSweep_ACancelledBUGFIXRunDoesNotAbandonTheVersion is the other side of the
// rule, and it is the live failure this scoping exists for.
//
// Only a cancelled DEV run abandons an increment — the same predicate that
// decides whether the cancel closes the milestone. A cancelled task or validation
// run leaves the version exactly as deployed as it was, and its milestone open,
// so the work still in it is still somebody's.
//
// Skipping on ANY cancelled run stranded a delivered version: a bug-fix run was
// cancelled, and the open validation task beside it went invisible to the sweep,
// so nothing ever judged the version and no report was ever produced.
func TestSweep_ACancelledBUGFIXRunDoesNotAbandonTheVersion(t *testing.T) {
	cancelledTask := aRun("run-task-cancelled", 7, delivery.RunStateCancelled)
	cancelledTask.Kind, cancelledTask.Origin = delivery.RunKindTask, delivery.RunOriginIncidentAdoption
	h := newHarness(t, cancelledTask)
	h.issues.withValidationIssue(7, 6)

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 1 || h.sup.started[0].Kind != delivery.RunKindValidation {
		t.Fatalf("an open validation task beside a cancelled BUG-FIX run must still be "+
			"judged, got %+v", h.sup.started)
	}
}

// TestSweep_ACancelledMilestoneSurvivesRepeatedTicks is the SUPPRESSION PROOF, and
// the reason the cancel closes its issues at all.
//
// The sweep runs every 60s forever. If a cancelled run's leftovers were merely
// recorded rather than suppressed, the very next tick would start a fresh run over
// them — dispatching an agent, merging, promoting — and the cancel button would
// read as having stopped nothing while paying for its own replacement a minute
// later. Two passes is what makes it a claim about the steady state rather than
// about one pass.
func TestSweep_ACancelledMilestoneSurvivesRepeatedTicks(t *testing.T) {
	h := newHarness(t, aRun("run-cancelled", 7, delivery.RunStateCancelled))
	// The state the cancel leaves behind: every issue closed, and — because a fake
	// close does not un-list an issue and reality is not always tidier — an open
	// leftover the platform failed to close.
	h.issues.
		withClosedIssue(7, 21, delivery.LabelAgentWork, delivery.KindDevelopment, delivery.LabelCancelled).
		withIssue(7, 22, delivery.LabelAgentWork, delivery.KindBug, delivery.LabelCancelled)

	sweep := sweepOver(h)
	for tick := 1; tick <= 2; tick++ {
		if err := sweep.Once(t.Context()); err != nil {
			t.Fatalf("sweep tick %d: %v", tick, err)
		}
		if len(h.sup.started) != 0 {
			t.Fatalf("tick %d restarted the run the user cancelled: %+v", tick, h.sup.started)
		}
	}
}

// The skip CLEARS ITSELF, and nothing has to clear it. A rebuild admits a new row
// on the SAME milestone, so the newest run is no longer the cancelled one — which
// is why the rule reads the newest run of any kind rather than looking for a cancel
// anywhere in the history.
//
// Here that rebuild's run has since settled, leaving work still open: exactly the
// footprint the sweep exists to heal, and it must heal it.
func TestSweep_ARebuildEndsTheCancelSkip(t *testing.T) {
	h := newHarness(t,
		// Newest first, as the repository returns them.
		aRun("run-rebuild", 7, delivery.RunStateFailed),
		aRun("run-cancelled", 7, delivery.RunStateCancelled),
	)
	h.issues.withDefects(7, 21)

	if err := sweepOver(h).Once(t.Context()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(h.sup.started) != 1 || h.sup.started[0].MilestoneNumber != 7 {
		t.Fatalf("a rebuilt milestone is ordinary again and must be healed, got %+v", h.sup.started)
	}
}

// ---- ReconcileMilestone: the sweep's pass, driven for ONE milestone ---------
//
// The pass itself is the sweep's, verbatim — every test above drives it through
// `Once`, which is what proves the two cannot diverge. What these pin is the
// scoped DOOR: that a caller who knows which milestone changed gets the same
// answers, and reaches no other milestone doing it. Its caller is the run
// supervisor's settle (delivery.SettleHandsWorkOnward).

// H1 and H3: the version's validation task is open and nothing is judging it.
// This is the dev run's hand-off, and the repair chain's second half, which are
// the same footprint.
func TestReconcileMilestone_JudgesAVersionWhoseTaskIsOpen(t *testing.T) {
	h := newHarness(t, aRun("run-dev", 7, delivery.RunStateSucceeded))
	h.issues.withValidationIssue(7, 55)

	if err := h.events.ReconcileMilestone(t.Context(), testOrg, testProject, 7, "v7"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(h.sup.started) != 1 {
		t.Fatalf("want one run started, got %+v", h.sup.started)
	}
	got := h.sup.started[0]
	if got.Kind != delivery.RunKindValidation || got.MilestoneNumber != 7 {
		t.Fatalf("an open validation task must be judged, got %+v", got)
	}
	// The title rides through onto the row. MilestoneRun.SpecTag falls back to it
	// when the row carries no tag, so dropping it would put a run in the ledger
	// with no version to show for itself.
	if got.MilestoneTitle != "v7" {
		t.Errorf("MilestoneTitle = %q, want %q", got.MilestoneTitle, "v7")
	}
}

// H2: a failing verdict files one repair issue per failed criterion and settles.
// Those are an ordinary task run's work — a validation run halts nothing,
// precisely so this chain stays open — and nothing but this starts it.
func TestReconcileMilestone_WorksTheRepairIssuesAFailedVerdictFiled(t *testing.T) {
	judged := aRun("run-validation", 7, delivery.RunStateFailed)
	judged.Kind, judged.Origin = delivery.RunKindValidation, delivery.RunOriginRevalidate
	h := newHarness(t, judged)
	h.issues.withIssue(7, 31, delivery.LabelAgentWork, delivery.KindBug, delivery.SrcValidation)

	if err := h.events.ReconcileMilestone(t.Context(), testOrg, testProject, 7, "v7"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(h.sup.started) != 1 || h.sup.started[0].Kind != delivery.RunKindTask {
		t.Fatalf("verdict-sourced repair work must start a TASK run, got %+v", h.sup.started)
	}
}

// The WRONG MOMENT, and the reason the nudge is at the settle rather than at the
// write. Every one of those three hand-offs is written inside a run that is still
// live, moments before it ends — so a trigger fired at the write finds this run,
// re-offers it, and starts nothing. Only the row going terminal makes the
// milestone reconcilable, and that is a fact no webhook carries.
func TestReconcileMilestone_StartsNothingNewWhileTheRunIsStillLive(t *testing.T) {
	h := newHarness(t, aRun("run-dev", 7, delivery.RunStateRunning))
	h.issues.withValidationIssue(7, 55)

	if err := h.events.ReconcileMilestone(t.Context(), testOrg, testProject, 7, "v7"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// The live row is re-offered — that is the sweep's healing rule, and it is
	// idempotent — but nothing of a NEW kind is admitted over it.
	for _, s := range h.sup.started {
		if s.Kind == delivery.RunKindValidation {
			t.Fatalf("a validation run must not start beside a live run, got %+v", h.sup.started)
		}
	}
}

// Every suppressor the sweep grew is one this door inherits, because it is the
// same function. A cancelled increment is abandoned whole.
func TestReconcileMilestone_SkipsACancelledIncrement(t *testing.T) {
	h := newHarness(t, aRun("run-cancelled", 7, delivery.RunStateCancelled))
	h.issues.withDefects(7, 21)

	if err := h.events.ReconcileMilestone(t.Context(), testOrg, testProject, 7, "v7"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("a cancelled increment must stay abandoned, got %+v", h.sup.started)
	}
}

// The other inherited suppressor, and the one that matters most at settle time: a
// FAILED run halts what it could not finish BEFORE its row goes terminal, so by
// the time the nudge fires there is nothing left to restart. Without that
// ordering this call would hand a failed run's work straight back to a fresh run
// with fresh budgets — every budget in the platform defeated at once.
func TestReconcileMilestone_LeavesHaltedWorkAlone(t *testing.T) {
	h := newHarness(t, aRun("run-failed", 7, delivery.RunStateFailed))
	h.issues.withIssue(7, 21, delivery.LabelAgentWork, delivery.KindBug, delivery.LabelHalted)

	if err := h.events.ReconcileMilestone(t.Context(), testOrg, testProject, 7, "v7"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("halted work must not be restarted, got %+v", h.sup.started)
	}
}

// SCOPED, which is the whole economy of the door: the sweep costs one GitHub read
// per known milestone across every project, and this costs one. A caller naming
// its own milestone must not reach anybody else's.
func TestReconcileMilestone_TouchesNoOtherMilestone(t *testing.T) {
	h := newHarness(t,
		aRun("run-v7", 7, delivery.RunStateSucceeded),
		aRun("run-v8", 8, delivery.RunStateSucceeded))
	h.issues.withValidationIssue(8, 55)

	if err := h.events.ReconcileMilestone(t.Context(), testOrg, testProject, 7, "v7"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(h.sup.started) != 0 {
		t.Fatalf("reconciling v7 must not judge v8, got %+v", h.sup.started)
	}
}
