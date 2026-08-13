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
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// The run loop is tested end-to-end with the Temporal Go SDK test suite: every
// activity is mocked, so there is no Temporal server, no database, no GitHub
// and no cluster. What is exercised is the only thing this package owns — the
// DECISIONS: when to dispatch, which budget a failure spends, and which of §7's
// exits a run takes.
//
// Time is free here. The environment fast-forwards its clock whenever the
// workflow is blocked on nothing but timers, so an unbounded wait, a two-hour
// dispatch deadline and a ten-minute re-poll all cost the same as an assertion.

const (
	testOrg      = "org1"
	testProject  = "proj1"
	testRunID    = "run-1"
	testCycleID  = "cycle-1"
	testMilepost = 7
	testMergeSHA = "abc123def4567890"
	testPRNumber = 42
	// testRepairIssue is the issue a failed validation attempt files for a failed
	// criterion — the work that makes the next boundary dispatch a coding cycle.
	testRepairIssue = 91
)

// harness records what the loop DID — the sequence of dispatches, how each
// cycle was closed, and the run's final write — so a test can assert on
// behaviour rather than on mock call counts.
type harness struct {
	env  *testsuite.TestWorkflowEnvironment
	acts *Activities

	// set records which facts a test pinned. Defaults are applied at run() time
	// rather than in the constructor because testify consumes expectations in
	// REGISTRATION order: an unlimited default registered first would swallow
	// every call and silently mask the test's own sequence.
	set map[string]bool

	mu         sync.Mutex
	dispatches []delivery.MilestoneDispatch
	finishes   []FinishCycleInput
	states     []string
	settle     SettleRunInput
	verdicts   []string
	// traitSyncs records every managed-API trait convergence the loop asked for,
	// so a test can assert WHEN it fires rather than merely that it was wired.
	traitSyncs []ProjectRef
	// verdictWrites keeps the full payload so a test can assert on what was
	// PERSISTED (verdict + issue), not merely on the verdict the run returned.
	verdictWrites []SetValidationVerdictInput
	// repairMints records every repair-issue filing a failed validation attempt
	// asked for, so a test can assert the loop turned the failure into work rather
	// than merely that it did not settle.
	repairMints []MintValidationRepairIssuesInput
	closed      int
}

// newHarness registers the activities whose behaviour never varies — the
// writers the loop records its progress through. The facts a run turns on are
// pinned per test, or defaulted at run() time.
func newHarness(t *testing.T) *harness {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	h := &harness{env: ts.NewTestWorkflowEnvironment(), set: map[string]bool{}}
	var acts *Activities
	h.acts = acts

	h.env.RegisterActivity(acts.PollMilestone)
	h.env.RegisterActivity(acts.SetRunState)
	h.env.RegisterActivity(acts.SettleRun)
	h.env.RegisterActivity(acts.BumpRunBudget)
	h.env.RegisterActivity(acts.SetValidationVerdict)
	h.env.RegisterActivity(acts.AppendCycle)
	h.env.RegisterActivity(acts.NoteCycleDispatch)
	h.env.RegisterActivity(acts.FinishCycle)
	h.env.RegisterActivity(acts.ReadCycleFacts)
	h.env.RegisterActivity(acts.CloseMilestone)
	h.env.RegisterActivity(acts.PollCycleBuilds)
	h.env.RegisterActivity(acts.EnsureValidationIssue)
	h.env.RegisterActivity(acts.ReadValidationVerdict)
	h.env.RegisterActivity(acts.MintValidationRepairIssues)
	h.env.RegisterActivity(acts.DispatchAgent)
	h.env.RegisterActivity(acts.SyncAPITraits)

	h.env.OnActivity(acts.SetRunState, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			in := args.Get(1).(SetRunStateInput)
			h.mu.Lock()
			defer h.mu.Unlock()
			h.states = append(h.states, in.State)
		}).Return(nil)
	h.env.OnActivity(acts.SettleRun, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.settle = args.Get(1).(SettleRunInput)
		}).Return(nil)
	h.env.OnActivity(acts.BumpRunBudget, mock.Anything, mock.Anything).Return(nil)
	h.env.OnActivity(acts.SetValidationVerdict, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			in := args.Get(1).(SetValidationVerdictInput)
			h.mu.Lock()
			defer h.mu.Unlock()
			h.verdicts = append(h.verdicts, in.Verdict)
			h.verdictWrites = append(h.verdictWrites, in)
		}).Return(nil)
	h.env.OnActivity(acts.AppendCycle, mock.Anything, mock.Anything).Return(testCycleID, nil)
	h.env.OnActivity(acts.NoteCycleDispatch, mock.Anything, mock.Anything).Return(nil)
	h.env.OnActivity(acts.FinishCycle, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.finishes = append(h.finishes, args.Get(1).(FinishCycleInput))
		}).Return(nil)
	h.env.OnActivity(acts.CloseMilestone, mock.Anything, mock.Anything).
		Run(func(mock.Arguments) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.closed++
		}).Return(nil)
	return h
}

// dispatchIs pins what launching the cycle's agent answers. Registered per test
// rather than as a constructor default for the reason traitSyncIs gives: the
// harness consumes expectations in registration order, so an unlimited default
// set in newHarness would swallow the override. Every dispatch is recorded
// either way, so the budget assertions hold for a failing dispatch too.
func (h *harness) dispatchIs(jobRef string, err error) {
	h.set["dispatch"] = true
	h.env.OnActivity(h.acts.DispatchAgent, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.dispatches = append(h.dispatches, args.Get(1).(delivery.MilestoneDispatch))
		}).Return(jobRef, err)
}

// traitSyncIs pins what the managed-API convergence answers. Registered per
// test rather than as a constructor default so a test can make it fail — the
// harness consumes expectations in registration order, so an unlimited default
// set in newHarness would swallow the override.
func (h *harness) traitSyncIs(err error) {
	h.set["traits"] = true
	h.env.OnActivity(h.acts.SyncAPITraits, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.traitSyncs = append(h.traitSyncs, args.Get(1).(ProjectRef))
		}).Return(err)
}

// traitSyncCount is the convergence tally, read safely.
func (h *harness) traitSyncCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.traitSyncs)
}

// milestoneIs queues the cycle-boundary polls, in order. The last one repeats
// for as long as the run keeps asking.
func (h *harness) milestoneIs(snaps ...MilestoneSnapshot) {
	h.set["milestone"] = true
	for i, s := range snaps {
		call := h.env.OnActivity(h.acts.PollMilestone, mock.Anything, mock.Anything).Return(s, nil)
		if i < len(snaps)-1 {
			call.Once()
		}
	}
}

// buildsAre queues the cycle-build polls, in order; the last repeats.
func (h *harness) buildsAre(states ...CycleBuildState) {
	h.set["builds"] = true
	for i, s := range states {
		call := h.env.OnActivity(h.acts.PollCycleBuilds, mock.Anything, mock.Anything).Return(s, nil)
		if i < len(states)-1 {
			call.Once()
		}
	}
}

// mergesAt makes the cycle record report a merge — the GROUND TRUTH the loop
// consults instead of believing the signal that woke it. An empty SHA means the
// agent never landed anything.
func (h *harness) mergesAt(sha string) {
	h.set["facts"] = true
	facts := CycleFacts{CycleID: testCycleID}
	if sha != "" {
		facts.MergeSHA, facts.PRNumber, facts.Ended = sha, testPRNumber, true
	}
	h.env.OnActivity(h.acts.ReadCycleFacts, mock.Anything, mock.Anything).Return(facts, nil)
}

// validationIs pins the acceptance oracle's issue number (0 = no criteria) and
// the verdict the runner's report yields, for every attempt.
//
// The digest is derived from the verdict, so a test that pins ONE verdict and gets
// two attempts is describing two identical reports — which is what the
// identical-report rule exists to stop. Tests that want the repair to have changed
// something use validationAttemptsAre.
func (h *harness) validationIs(issue int, verdict string) {
	h.validationAttemptsAre(issue, ValidationOutcome{Verdict: verdict, Digest: "digest-" + verdict})
}

// validationAttemptsAre queues one outcome per validation ATTEMPT, in order; the
// last repeats. The digest is explicit because it is what tells a repeat that
// learned something from one that reached the same answer again.
func (h *harness) validationAttemptsAre(issue int, outcomes ...ValidationOutcome) {
	h.set["validation"] = true
	h.env.OnActivity(h.acts.EnsureValidationIssue, mock.Anything, mock.Anything).Return(issue, nil)
	for i, out := range outcomes {
		call := h.env.OnActivity(h.acts.ReadValidationVerdict, mock.Anything, mock.Anything).Return(out, nil)
		if i < len(outcomes)-1 {
			call.Once()
		}
	}
}

// repairMintsAre pins what filing repair issues answers. Registered per test, like
// the other overridable facts, so a test can make a failed attempt file NOTHING —
// the case where the report named a failure the minter could not turn into work.
func (h *harness) repairMintsAre(filed []int) {
	h.set["repair"] = true
	h.env.OnActivity(h.acts.MintValidationRepairIssues, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.repairMints = append(h.repairMints, args.Get(1).(MintValidationRepairIssuesInput))
		}).Return(filed, nil)
}

// repairMintCount is the repair-filing tally, read safely.
func (h *harness) repairMintCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.repairMints)
}

// applyDefaults fills in the facts a test did not pin: every cycle lands, every
// build is green, and the project has no acceptance oracle.
func (h *harness) applyDefaults() {
	if !h.set["dispatch"] {
		h.dispatchIs("job-1", nil)
	}
	if !h.set["facts"] {
		h.mergesAt(testMergeSHA)
	}
	if !h.set["builds"] {
		h.buildsAre(CycleBuildState{Expected: 1, Settled: 1})
	}
	if !h.set["validation"] {
		h.validationIs(0, delivery.ValidationVerdictSkipped)
	}
	if !h.set["repair"] {
		h.repairMintsAre([]int{testRepairIssue})
	}
	if !h.set["milestone"] {
		h.milestoneIs(MilestoneSnapshot{})
	}
	if !h.set["traits"] {
		h.traitSyncIs(nil)
	}
}

// signal schedules one inbound run signal at a virtual offset from start.
func (h *harness) signal(name string, after time.Duration) {
	h.env.RegisterDelayedCallback(func() {
		h.env.SignalWorkflow(name, delivery.RunSignal{Signal: name, MilestoneNumber: testMilepost})
	}, after)
}

// merges schedules n merge signals, one per cycle, a second apart.
func (h *harness) merges(n int) {
	for i := 1; i <= n; i++ {
		h.signal(delivery.SigRunPRMerged, time.Duration(i)*time.Second)
	}
}

func (h *harness) run(origin string, ceiling int) {
	h.runWith(RunInput{Origin: origin, CycleCeiling: ceiling})
}

// runWith starts the workflow with the caller's budgets, filling in the identity
// every test shares. It exists for the inputs that only some runs pin —
// ValidationAttempts above all — so the common `run` stays two arguments.
func (h *harness) runWith(in RunInput) {
	h.applyDefaults()
	in.RunID = testRunID
	in.OrgID = testOrg
	in.ProjectID = testProject
	in.MilestoneNumber = testMilepost
	in.MilestoneTitle = "v3"
	h.env.ExecuteWorkflow(MilestoneRunWorkflow, in)
}

func (h *harness) result(t *testing.T) RunResult {
	t.Helper()
	require.True(t, h.env.IsWorkflowCompleted())
	require.NoError(t, h.env.GetWorkflowError())
	var res RunResult
	require.NoError(t, h.env.GetWorkflowResult(&res))
	return res
}

func (h *harness) dispatchKinds() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.dispatches))
	for _, d := range h.dispatches {
		out = append(out, d.Kind)
	}
	return out
}

func (h *harness) dispatchCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.dispatches)
}

// closedCount is the milestone-close tally, read safely. Tests that assert
// after the workflow completed read h.closed directly; a test that asserts
// MID-RUN, from a delayed callback, races the activity goroutine and must not.
func (h *harness) closedCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closed
}

// settledState is the run row's terminal state so far, read safely, for the
// same mid-run reason. Empty means nothing has settled the run.
func (h *harness) settledState() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.settle.State
}

// assertSettled checks the run's terminal write and the workflow result agree —
// the row is what the console reads, the result is what Temporal records, and a
// run that disagreed with itself would be unexplainable.
func (h *harness) assertSettled(t *testing.T, res RunResult, state, reason string) {
	t.Helper()
	require.Equal(t, state, res.State, "workflow result state")
	require.Equal(t, reason, res.TerminalReason, "workflow result reason")
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Equal(t, testRunID, h.settle.RunID)
	require.Equal(t, state, h.settle.State, "run row state")
	require.Equal(t, reason, h.settle.Reason, "run row reason")
}

// ---- the §7 exits ----------------------------------------------------------

// TestHappyPath_OneCycleDeliversTheVersion is the loop's whole point: work the
// milestone, land the pull request, watch the builds go green, find nothing
// left, close the milestone.
func TestHappyPath_OneCycleDeliversTheVersion(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 2, Total: 2}, MilestoneSnapshot{})
	h.merges(1)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []string{delivery.CycleKindCoding}, h.dispatchKinds())
	require.Equal(t, 1, res.Cycles)
	require.Equal(t, 1, h.closed, "a settled version closes its milestone")
	require.Equal(t, []FinishCycleInput{{CycleID: testCycleID, MergeSHA: testMergeSHA}}, h.finishes)
	// The run row never says "waiting" here: it parks only when something
	// actually holds it, and a boundary the loop passes straight through is not
	// a wait a human could act on.
	require.Equal(t, []string{delivery.RunStateRunning}, dedupeStates(h.states))
	// No acceptance oracle: skip-if-no-criteria is a verdict, not a silence.
	require.Equal(t, delivery.ValidationVerdictSkipped, res.ValidationVerdict)
}

// TestFixCycle_RedBuildBecomesTheNextCyclesWork proves recovery is
// indistinguishable from normal work: the red build's fix issue joins the
// working set and the next cycle picks it up.
func TestFixCycle_RedBuildBecomesTheNextCyclesWork(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 2, Total: 2}, // dispatch
		MilestoneSnapshot{Work: 1, Total: 1}, // the fix issue eventcore minted
		MilestoneSnapshot{},                  // delivered
	)
	h.buildsAre(
		CycleBuildState{Expected: 1, Settled: 1, Red: []string{"order-service"}},
		CycleBuildState{Expected: 1, Settled: 1},
	)
	h.merges(2)

	h.run(delivery.RunOriginIncidentAdoption, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []string{delivery.CycleKindCoding, delivery.CycleKindFix}, h.dispatchKinds())
}

// TestBuildsGreen_ConvergesTheManagedAPIGatewayPolicy pins the trigger this
// loop now owns. The `api-configuration` trait's per-environment half — the
// `jwtAuth` policy the gateway enforces — is written to the ReleaseBinding,
// which OpenChoreo creates from the workload the build's last step generates.
// Builds going green is therefore the first moment in a run where the write has
// a target, and the loop must take it: nothing else on this rail does, which is
// how protected APIs came to serve unauthenticated.
func TestBuildsGreen_ConvergesTheManagedAPIGatewayPolicy(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1}, MilestoneSnapshot{})
	h.merges(1)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []ProjectRef{{OrgID: testOrg, ProjectID: testProject}}, h.traitSyncs,
		"a green cycle converges the project's managed-API policy exactly once")
}

// TestRedBuild_ConvergesOnlyOnceItGoesGreen: a red cycle produced no new
// ReleaseBinding, so there is nothing to converge and the loop must not spend a
// round trip pretending otherwise. The fix cycle that follows passes through the
// same green path and does the write then.
func TestRedBuild_ConvergesOnlyOnceItGoesGreen(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 1, Total: 1},
		MilestoneSnapshot{Work: 1, Total: 1},
		MilestoneSnapshot{},
	)
	h.buildsAre(
		CycleBuildState{Expected: 1, Settled: 1, Red: []string{"order-service"}},
		CycleBuildState{Expected: 1, Settled: 1},
	)
	h.merges(2)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, 1, h.traitSyncCount(),
		"only the green cycle converges; the red one had no binding to write to")
}

// TestTraitSyncFailure_DoesNotFailTheRun pins the deliberate asymmetry: the
// convergence is retried under its own deadline, but its exhaustion is logged,
// not fatal. Failing the cycle would not undo the exposure — the component is
// already deployed and serving by the time this runs — so a red run would add
// noise without removing it. Only a later convergence removes it.
func TestTraitSyncFailure_DoesNotFailTheRun(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1}, MilestoneSnapshot{})
	h.traitSyncIs(errors.New("openchoreo unreachable"))
	h.merges(1)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Positive(t, h.traitSyncCount(), "the convergence was attempted")
}

// TestConflictCycle_AnUnmergeablePRBecomesTheNextCyclesWork is the same shape
// for the other recovery class — and proves the conflicted cycle is closed with
// NO merge SHA, because nothing landed.
func TestConflictCycle_AnUnmergeablePRBecomesTheNextCyclesWork(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 2, Total: 2},
		MilestoneSnapshot{Work: 1, Total: 1}, // the conflict issue
		MilestoneSnapshot{},
	)
	h.signal(delivery.SigRunConflict, time.Second)
	h.signal(delivery.SigRunPRMerged, 2*time.Second)

	h.run(delivery.RunOriginIncidentAdoption, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []string{delivery.CycleKindCoding, delivery.CycleKindConflict}, h.dispatchKinds())
	require.Equal(t, "", h.finishes[0].MergeSHA, "a conflicted cycle landed nothing")
	require.Equal(t, testMergeSHA, h.finishes[1].MergeSHA)
}

// TestValidationCycle_Passes covers §7's validation arm: at deployed-green with
// an empty working set, a SPEC run mints the validation issue and works it with
// a fresh dispatch anchored to that issue alone.
func TestValidationCycle_Passes(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 1, Total: 1},
		MilestoneSnapshot{}, // deployed-green, nothing left → validation
		MilestoneSnapshot{}, // after validation → settle
	)
	h.validationIs(77, delivery.ValidationVerdictPassed)
	h.merges(2)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []string{delivery.CycleKindCoding, delivery.CycleKindValidation}, h.dispatchKinds())
	require.Equal(t, delivery.ValidationVerdictPassed, res.ValidationVerdict)
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Equal(t, []string{delivery.ValidationVerdictPassed}, h.verdicts,
		"the verdict is written once, as a run property")
	require.Equal(t, 77, h.dispatches[1].IssueNumber, "the validation dispatch is anchored to one issue")
	require.Equal(t, 0, h.dispatches[0].IssueNumber, "a coding dispatch is a milestone reference only")
}

// TestValidationCycle_RepairsAFailureAndRevalidates is the self-healing loop.
//
// A `failed` attempt is a DEFECT, and the platform already knows what to do with a
// detected defect: file it as an issue, let it join the working set, and let the
// next cycle work it like any other. So the run does not settle — it files one
// issue per failed criterion, the boundary dispatches an ordinary CODING cycle, and
// validation runs again against the repaired system.
//
// Note the dispatch sequence: coding, validation, coding, validation. Nothing in
// the middle is a special "repair" kind, because a repair is ordinary work.
func TestValidationCycle_RepairsAFailureAndRevalidates(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 1, Total: 1}, // coding cycle
		MilestoneSnapshot{},                  // deployed-green → validation attempt 1
		MilestoneSnapshot{Work: 1, Total: 1}, // the repair issue → coding cycle
		MilestoneSnapshot{},                  // green again → validation attempt 2
		MilestoneSnapshot{},                  // passed → settle
	)
	// Different digests: the repair changed the answer, which is what stops the
	// identical-report rule from cutting the loop short.
	h.validationAttemptsAre(77,
		ValidationOutcome{Verdict: delivery.ValidationVerdictFailed, Digest: "red-1"},
		ValidationOutcome{Verdict: delivery.ValidationVerdictPassed, Digest: "green"},
	)
	h.merges(4)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []string{
		delivery.CycleKindCoding, delivery.CycleKindValidation,
		delivery.CycleKindCoding, delivery.CycleKindValidation,
	}, h.dispatchKinds())
	require.Equal(t, delivery.ValidationVerdictPassed, res.ValidationVerdict,
		"the run's verdict is its LATEST attempt's")
	require.Equal(t, 1, h.closed, "a self-healed increment is still a delivered increment")

	h.mu.Lock()
	defer h.mu.Unlock()
	require.Equal(t, []string{delivery.ValidationVerdictFailed, delivery.ValidationVerdictPassed},
		h.verdicts, "both attempts recorded their own verdict")
	require.Len(t, h.repairMints, 1, "the failed attempt filed repair work exactly once")
	require.Equal(t, testMergeSHA, h.repairMints[0].At,
		"repair issues come from the report at the attempt's OWN merge commit")
	require.Equal(t, testCycleID, h.repairMints[0].CycleID,
		"the attempt's cycle id is the issues' dedupe key")
	// The repair dispatch is an ordinary milestone reference — the loop does not
	// anchor it to the repair issue, because the runner re-lists the working set.
	require.Equal(t, 0, h.dispatches[2].IssueNumber)
}

// TestValidationCycle_UnreportedRedispatchesValidation covers the other
// recoverable verdict, and it takes a different route to the same boundary.
//
// An agent that merged a pull request and committed no report has not broken the
// SOFTWARE, so there is nothing to repair: the run mints nothing, the working set
// stays empty, and the boundary bounces straight back into validation. Two
// validation cycles in a row, with no coding cycle between them, is the proof.
func TestValidationCycle_UnreportedRedispatchesValidation(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 1, Total: 1}, // coding cycle
		MilestoneSnapshot{},                  // → validation attempt 1
		MilestoneSnapshot{},                  // still empty → validation attempt 2
		MilestoneSnapshot{},                  // passed → settle
	)
	h.validationAttemptsAre(77,
		ValidationOutcome{Verdict: delivery.ValidationVerdictUnreported},
		ValidationOutcome{Verdict: delivery.ValidationVerdictPassed, Digest: "green"},
	)
	h.merges(3)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []string{
		delivery.CycleKindCoding, delivery.CycleKindValidation, delivery.CycleKindValidation,
	}, h.dispatchKinds())
	require.Equal(t, 0, h.repairMintCount(),
		"nothing is wrong with the software, so nothing is filed")
}

// TestValidationCycle_FailsAfterEveryAttempt proves the loop is bounded, and that
// exhausting it needs no vocabulary of its own: the run settles on the verdict it
// keeps getting, so `validation-failed` now means "still failing after every
// attempt".
func TestValidationCycle_FailsAfterEveryAttempt(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 1, Total: 1}, // coding cycle
		MilestoneSnapshot{},                  // → validation attempt 1
		MilestoneSnapshot{Work: 1, Total: 1}, // repair issue → coding cycle
		MilestoneSnapshot{},                  // → validation attempt 2 (the last)
	)
	// Distinct digests, so it is the ATTEMPT BUDGET that ends this run and not the
	// identical-report short-circuit.
	h.validationAttemptsAre(77,
		ValidationOutcome{Verdict: delivery.ValidationVerdictFailed, Digest: "red-1"},
		ValidationOutcome{Verdict: delivery.ValidationVerdictFailed, Digest: "red-2"},
	)
	h.merges(4)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonValidationFailed)
	require.Equal(t, delivery.ValidationVerdictFailed, res.ValidationVerdict)
	require.Equal(t, 0, h.closed, "a failed increment keeps its milestone open")
	require.Equal(t, 2, delivery.RunMaxValidationAttempts,
		"this test's shape assumes the allowance it is proving")
	require.Equal(t, 1, h.repairMintCount(),
		"the LAST attempt files nothing — there is no attempt left to work it")
}

// TestValidationCycle_UnreportedTwiceStillFails pins the same bound for the other
// recoverable verdict, under its own reason. "The suite went red" and "nothing was
// reported" are different explanations, and a terminal reason exists to explain.
func TestValidationCycle_UnreportedTwiceStillFails(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 1, Total: 1},
		MilestoneSnapshot{},
		MilestoneSnapshot{},
	)
	h.validationIs(77, delivery.ValidationVerdictUnreported)
	h.merges(3)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonValidationUnreported)
	require.Equal(t, delivery.ValidationVerdictUnreported, res.ValidationVerdict)
	require.Equal(t, 0, h.closed, "a run that reported nothing keeps its milestone open")
}

// TestValidationCycle_IdenticalReportStopsEarly is the identical-report rule, and
// the reason it needs its own test is that it is the ONLY thing standing between a
// repeat attempt and a deployment that never picked up the repair.
//
// Two attempts whose reports agree on every criterion, every status and every
// failure message learned the same nothing. Another attempt could only produce that
// report a third time, so the run stops rather than spending the rest of its
// allowance — even though the allowance is not spent.
func TestValidationCycle_IdenticalReportStopsEarly(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 1, Total: 1},
		MilestoneSnapshot{},
		MilestoneSnapshot{Work: 1, Total: 1},
		MilestoneSnapshot{},
	)
	// The SAME digest twice: the repair did not move the system.
	h.validationAttemptsAre(77,
		ValidationOutcome{Verdict: delivery.ValidationVerdictFailed, Digest: "unchanged"},
		ValidationOutcome{Verdict: delivery.ValidationVerdictFailed, Digest: "unchanged"},
	)
	h.merges(4)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonValidationFailed)
	require.Equal(t, 1, h.repairMintCount(),
		"the second attempt learned nothing, so it files no further repair work")
}

// A `failed` attempt whose report names failures the minter cannot turn into work
// leaves the run with nothing to dispatch. Repeating would be the same validation
// cycle against the same code, so the run settles honestly rather than looping on
// an empty working set.
func TestValidationCycle_FailedWithNothingToRepairSettles(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1}, MilestoneSnapshot{})
	h.validationIs(77, delivery.ValidationVerdictFailed)
	h.repairMintsAre(nil)
	h.merges(2)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonValidationFailed)
	require.Equal(t, 1, h.repairMintCount(), "it tried")
}

// TestValidationCycle_IncompleteEvidenceStillSucceeds pins the pair that must NOT
// fail a run. Both are honest reports about the test harness rather than evidence
// the increment is broken:
//
//   - partial: something passed, nothing failed, and some criteria were never
//     covered — which is why it is not reported as `passed`;
//   - inconclusive: no test results at all.
//
// Telling "the oracle had nothing automatable" apart from "the agent ran nothing"
// is deferred with the rest of internal-agent-error handling, so inconclusive
// succeeds for now.
func TestValidationCycle_IncompleteEvidenceStillSucceeds(t *testing.T) {
	for _, verdict := range []string{
		delivery.ValidationVerdictPartial,
		delivery.ValidationVerdictInconclusive,
	} {
		t.Run(verdict, func(t *testing.T) {
			h := newHarness(t)
			h.milestoneIs(
				MilestoneSnapshot{Work: 1, Total: 1},
				MilestoneSnapshot{}, // deployed-green → validation
				MilestoneSnapshot{}, // after validation → settle
			)
			h.validationIs(77, verdict)
			h.merges(2)

			h.run(delivery.RunOriginSpecBuild, 0)
			res := h.result(t)

			h.assertSettled(t, res, delivery.RunStateSucceeded, "")
			require.Equal(t, verdict, res.ValidationVerdict)
			require.Equal(t, 1, h.closed,
				"a delivered increment closes its milestone even with incomplete evidence")
		})
	}
}

// The validation issue is persisted WITH the verdict: it otherwise lives only in
// workflow state, so a settled run would carry a verdict with no way back to the
// criteria that produced it once Temporal retention lapses.
func TestValidationCycle_PersistsTheIssueWithTheVerdict(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 1, Total: 1},
		MilestoneSnapshot{},
		MilestoneSnapshot{},
	)
	h.validationIs(77, delivery.ValidationVerdictPassed)
	h.merges(2)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Len(t, h.verdictWrites, 1, "the verdict is written once")
	require.Equal(t, 77, h.verdictWrites[0].Issue,
		"the issue is persisted alongside the verdict, not left in workflow state")
	// The CYCLE is named too, so the attempt keeps its own answer. Without it the run
	// row's single column would be the only record, and a run that validated twice
	// would remember nothing but the last attempt.
	require.Equal(t, testCycleID, h.verdictWrites[0].CycleID,
		"the verdict is written against the attempt's own cycle, not just the run")
}

// Each ATTEMPT names its own cycle when it records its verdict. That is what lets a
// self-healed run show attempt 1's failure beside attempt 2's pass: the run row can
// only hold the latest.
func TestValidationCycle_EachAttemptRecordsAgainstItsOwnCycle(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 1, Total: 1},
		MilestoneSnapshot{},
		MilestoneSnapshot{Work: 1, Total: 1},
		MilestoneSnapshot{},
		MilestoneSnapshot{},
	)
	h.validationAttemptsAre(77,
		ValidationOutcome{Verdict: delivery.ValidationVerdictFailed, Digest: "red"},
		ValidationOutcome{Verdict: delivery.ValidationVerdictPassed, Digest: "green"},
	)
	h.merges(4)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Len(t, h.verdictWrites, 2, "one write per attempt")
	for i, w := range h.verdictWrites {
		require.NotEmpty(t, w.CycleID, "attempt %d wrote no cycle id", i+1)
		require.Equal(t, 77, w.Issue, "every attempt shares the version's one validation issue")
	}
	require.Equal(t, delivery.ValidationVerdictFailed, h.verdictWrites[0].Verdict)
	require.Equal(t, delivery.ValidationVerdictPassed, h.verdictWrites[1].Verdict)
}

// A `skipped` verdict belongs to NO cycle, and both places that write it are
// reached with l.cycleID still holding the last CODING cycle's id — there is no
// validation cycle to name, because none was opened.
//
// The cycle write is write-once, so an id sent here would put a validation verdict
// on a coding row permanently, contradicting RunCycle.ValidationVerdict (documented
// empty on every other kind) and CycleView, which publishes the field as a
// validation-cycle property.
func TestSkippedVerdict_BelongsToNoCycle(t *testing.T) {
	for name, origin := range map[string]string{
		// Decided before a validation cycle opens: the project has no oracle.
		"no acceptance oracle": delivery.RunOriginSpecBuild,
		// Decided in settle: an incident run never reaches validation at all, so it
		// arrives at the end with an empty verdict.
		"validation never reached": delivery.RunOriginIncidentAdoption,
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1}, MilestoneSnapshot{})
			h.merges(1)

			h.run(origin, 0)
			res := h.result(t)

			h.assertSettled(t, res, delivery.RunStateSucceeded, "")
			require.Equal(t, delivery.ValidationVerdictSkipped, res.ValidationVerdict)
			h.mu.Lock()
			defer h.mu.Unlock()
			require.Len(t, h.verdictWrites, 1, "the verdict is written once")
			require.Empty(t, h.verdictWrites[0].CycleID,
				"skipped was stamped onto the last coding cycle's row")
		})
	}
}

// TestIncidentRun_GetsNoValidationCycle pins the origin split: an incident fixes
// one thing in an already-validated version, and re-validating the whole system
// for it would price every incident like a release.
func TestIncidentRun_GetsNoValidationCycle(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1}, MilestoneSnapshot{})
	h.merges(1)

	h.run(delivery.RunOriginIncidentAdoption, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []string{delivery.CycleKindCoding}, h.dispatchKinds())
	h.env.AssertNotCalled(t, "EnsureValidationIssue", mock.Anything, mock.Anything)
}

// TestRevalidateRun_EntersAtValidation is the origin's whole point: the milestone
// is a version that already shipped, so the very first poll returns an empty
// working set — the shape that parks every other run forever, because with no
// cycles behind it an empty milestone is indistinguishable from one mid-plan.
//
// A revalidation is the case where that ambiguity does not exist, and the proof is
// the dispatch list: one validation cycle, no coding cycle, nothing built.
func TestRevalidateRun_EntersAtValidation(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{}, // already delivered → straight to validation
		MilestoneSnapshot{}, // passed → settle
	)
	h.validationIs(77, delivery.ValidationVerdictPassed)
	h.merges(1)

	h.run(delivery.RunOriginRevalidate, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []string{delivery.CycleKindValidation}, h.dispatchKinds(),
		"a revalidation validates and does nothing else")
	require.Equal(t, delivery.ValidationVerdictPassed, res.ValidationVerdict)
	require.Equal(t, 77, h.dispatches[0].IssueNumber,
		"the version's existing validation issue is reopened, not re-minted")
}

// TestRevalidateRun_OneAttemptFilesNoRepairWork is the dev-loop shape, and it
// rests on an ORDERING rather than on a flag: runValidation settles on an
// exhausted attempt allowance BEFORE it reaches the mint, so an allowance of one
// makes repair work unreachable.
//
// That is why the endpoint needs no separate "should it fix things" switch. The
// contrast with TestValidationCycle_RepairsAFailureAndRevalidates is the whole
// test: same verdict, same harness, one fewer attempt — and no repair issues, no
// coding cycle, nothing rebuilt.
func TestRevalidateRun_OneAttemptFilesNoRepairWork(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{})
	h.validationIs(77, delivery.ValidationVerdictFailed)
	h.merges(1)

	h.runWith(RunInput{Origin: delivery.RunOriginRevalidate, ValidationAttempts: 1})
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonValidationFailed)
	require.Equal(t, []string{delivery.CycleKindValidation}, h.dispatchKinds())
	require.Equal(t, delivery.ValidationVerdictFailed, res.ValidationVerdict,
		"the answer is recorded even though nothing was repaired")
	require.Equal(t, 0, h.closed, "a failed version keeps its milestone open")

	h.mu.Lock()
	defer h.mu.Unlock()
	require.Empty(t, h.repairMints,
		"one attempt is spent by the first fatal verdict, so the mint is never reached")
}

// TestRevalidateRun_DefaultAttemptsRepairAndRebuild is the same origin taking the
// other route — the one that exists to fix what it finds on an already-deployed
// system.
//
// Left at the default allowance, a revalidation is an ordinary run that happened
// to start at validation: the failure becomes issues, the boundary dispatches a
// CODING cycle which merges and builds, and validation runs again. The dispatch
// sequence is the assertion — validation, coding, validation — and it is the
// repair loop the spec build already had, reached from a different door.
func TestRevalidateRun_DefaultAttemptsRepairAndRebuild(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{},                  // deployed → validation attempt 1
		MilestoneSnapshot{Work: 1, Total: 1}, // the repair issue → coding cycle
		MilestoneSnapshot{},                  // green again → validation attempt 2
		MilestoneSnapshot{},                  // passed → settle
	)
	h.validationAttemptsAre(77,
		ValidationOutcome{Verdict: delivery.ValidationVerdictFailed, Digest: "red-1"},
		ValidationOutcome{Verdict: delivery.ValidationVerdictPassed, Digest: "green"},
	)
	h.merges(3)

	h.run(delivery.RunOriginRevalidate, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []string{
		delivery.CycleKindValidation, delivery.CycleKindCoding, delivery.CycleKindValidation,
	}, h.dispatchKinds(), "it repairs on the ordinary path — the repair is not a special kind")
	require.Equal(t, delivery.ValidationVerdictPassed, res.ValidationVerdict)

	h.mu.Lock()
	defer h.mu.Unlock()
	require.Len(t, h.repairMints, 1, "the failed attempt filed repair work")
}

// TestRevalidateRun_ZeroAttemptsMeansTheDefault is the replay guard, written as a
// behaviour rather than as a unit test of newLoop.
//
// A workflow input lives in Temporal history, so an execution started before the
// field existed replays with the zero value. Zero therefore has to mean the
// platform default — if it meant "no attempts", every run in flight across the
// deploy would settle without validating. The proof is that a zero-attempt input
// still repairs and re-validates, which only the default allowance permits.
func TestRevalidateRun_ZeroAttemptsMeansTheDefault(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{},
		MilestoneSnapshot{Work: 1, Total: 1},
		MilestoneSnapshot{},
		MilestoneSnapshot{},
	)
	h.validationAttemptsAre(77,
		ValidationOutcome{Verdict: delivery.ValidationVerdictFailed, Digest: "red-1"},
		ValidationOutcome{Verdict: delivery.ValidationVerdictPassed, Digest: "green"},
	)
	h.merges(3)

	h.runWith(RunInput{Origin: delivery.RunOriginRevalidate, ValidationAttempts: 0})
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Len(t, h.dispatchKinds(), 3,
		"zero fell back to the default allowance, so the run got its second attempt")
}

// TestRedispatchBudget_AgentDeathEndsTheRun: the dispatch never lands a pull
// request, so the cycle spends its whole per-cycle allowance and the run fails
// naming that budget. Nothing here needs a real two hours — the environment
// fast-forwards both deadlines.
func TestRedispatchBudget_AgentDeathEndsTheRun(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1})
	h.mergesAt("") // the cycle record never learns a merge

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonRedispatchBudget)
	require.Equal(t, delivery.RunMaxRedispatchPerCycle, h.dispatchCount())
	require.Equal(t, "", h.finishes[0].MergeSHA)
}

// TestAgentQuotaBlocked_SettlesBlockedWithoutSpendingTheBudget is the
// billing-cap exit, and the reason it is not a failure: nothing was launched,
// nothing is broken, and the only thing that changes the answer is a human
// freeing a slot. So the run settles BLOCKED under its own reason, on the FIRST
// refusal — spending the re-dispatch budget on two more identical refusals
// would only delay the message the user needs.
func TestAgentQuotaBlocked_SettlesBlockedWithoutSpendingTheBudget(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1})
	h.dispatchIs("", temporal.NewNonRetryableApplicationError(
		delivery.AgentQuotaBlockedMessage, delivery.ErrTypeAgentQuotaBlocked, delivery.ErrAgentQuotaExceeded))

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateBlocked, delivery.RunReasonAgentQuotaBlocked)
	require.Equal(t, 1, h.dispatchCount(),
		"a quota refusal must not be re-attempted — the answer cannot change without a human")
	require.Equal(t, 0, h.closed, "a blocked increment keeps its milestone open")
	require.True(t, delivery.IsTerminalRunState(delivery.RunStateBlocked),
		"blocked must be terminal, or the spec-run mutex stays armed forever")
}

// TestBuildRetriggerBudget_RedWithNothingToFix is the exit for a build that
// stayed red through its one automatic re-trigger and produced no fix issue:
// the allowance is spent and nothing came back that could make it green.
func TestBuildRetriggerBudget_RedWithNothingToFix(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1}, MilestoneSnapshot{})
	h.buildsAre(CycleBuildState{Expected: 1, Settled: 1, Red: []string{"order-service"}})
	h.merges(1)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonBuildRetriggerBudget)
	require.Equal(t, 1, h.dispatchCount())
}

// TestFixChainBudget_TwoFixCyclesIsTheLimit walks the fix chain to exhaustion.
func TestFixChainBudget_TwoFixCyclesIsTheLimit(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1})
	h.buildsAre(CycleBuildState{Expected: 1, Settled: 1, Red: []string{"order-service"}})
	h.merges(3)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonFixChainBudget)
	require.Equal(t, []string{
		delivery.CycleKindCoding, delivery.CycleKindFix, delivery.CycleKindFix,
	}, h.dispatchKinds())
}

// TestConflictBudget_TwoConflictCyclesIsTheLimit does the same for the other
// chain, and is what keeps the two reasons apart: a run that could not merge is
// never reported as a run that could not build.
func TestConflictBudget_TwoConflictCyclesIsTheLimit(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1})
	for i := 1; i <= 3; i++ {
		h.signal(delivery.SigRunConflict, time.Duration(i)*time.Second)
	}

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonConflictBudget)
	require.Equal(t, []string{
		delivery.CycleKindCoding, delivery.CycleKindConflict, delivery.CycleKindConflict,
	}, h.dispatchKinds())
}

// TestNoProgress_AGreenCycleThatChangedNothing: the agent merged, everything
// built, and the milestone is exactly as it was. Another cycle would be the
// same dispatch against the same working set.
func TestNoProgress_AGreenCycleThatChangedNothing(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 2, Total: 2})
	h.merges(1)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonNoProgress)
	require.Equal(t, 1, h.dispatchCount())
}

// TestCycleCeiling_StopsARunThatIsStillMakingProgress proves the ceiling is a
// backstop over and above the per-class budgets: every cycle here closes an
// issue, so no other budget would ever fire.
func TestCycleCeiling_StopsARunThatIsStillMakingProgress(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 3, Total: 3},
		MilestoneSnapshot{Work: 2, Total: 2},
		MilestoneSnapshot{Work: 1, Total: 1},
	)
	h.merges(2)

	h.run(delivery.RunOriginSpecBuild, 2)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonCycleCeiling)
	require.Equal(t, 2, h.dispatchCount())
}

// TestCancel_FromWaiting: cancel is the ONLY expiry the unbounded wait has.
// The run is parked behind a gate and never dispatches.
func TestCancel_FromWaiting(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 1, Gates: 1, Total: 2})
	h.signal(delivery.SigRunCancel, time.Second)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateCancelled, "")
	require.Equal(t, 0, h.dispatchCount(), "a cancelled wait never dispatched")
	require.Equal(t, 0, h.closed, "an abandoned increment keeps its milestone open")
}

// TestCancel_FromRunning: cancel mid-cycle settles the run and closes the cycle
// with no merge, so the timeline shows a dispatch that was abandoned rather than
// one still in flight.
func TestCancel_FromRunning(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1})
	h.signal(delivery.SigRunCancel, time.Second)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateCancelled, "")
	require.Equal(t, 1, h.dispatchCount())
	require.Equal(t, []FinishCycleInput{{CycleID: testCycleID}}, h.finishes)
}

// TestMidRunGate_HoldsTheNextDispatch is the human brake: a gate filed while the
// run is live stops the NEXT cycle, and only the next cycle. The assertion that
// matters is inside the callback — at the moment the gate was open, nothing new
// had been dispatched.
func TestMidRunGate_HoldsTheNextDispatch(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 2, Total: 2},           // cycle 1 dispatches
		MilestoneSnapshot{Work: 1, Gates: 1, Total: 2}, // a gate appears → hold
		MilestoneSnapshot{Work: 1, Total: 1},           // the gate closed → cycle 2
		MilestoneSnapshot{},                            // delivered
	)
	h.merges(1)

	heldAt := -1
	h.env.RegisterDelayedCallback(func() {
		heldAt = h.dispatchCount()
		h.env.SignalWorkflow(delivery.SigRunWorkable, delivery.RunSignal{Signal: delivery.SigRunWorkable})
	}, 2*time.Second)
	h.signal(delivery.SigRunPRMerged, 3*time.Second)

	h.run(delivery.RunOriginIncidentAdoption, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, 1, heldAt, "the gate must hold the second dispatch")
	require.Equal(t, 2, h.dispatchCount(), "the closed gate releases it")
}

// TestSettle_WithAStrayGateStillOpen: gates hold DISPATCH. With an empty working
// set there is nothing to dispatch, so an open gate holds nothing and the
// version still settles.
func TestSettle_WithAStrayGateStillOpen(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 1, Total: 1},
		MilestoneSnapshot{Work: 0, Gates: 1, Total: 1},
	)
	h.merges(1)

	h.run(delivery.RunOriginIncidentAdoption, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, 1, h.closed)
}

// ---- ground truth and liveness --------------------------------------------

// TestMergeSignalIsNotEvidence pins the rule that a signal is a wake-up, never
// evidence: a merge signal whose cycle record shows no merge (a HUMAN's pull
// request landing during the cycle raises the very same signal) must not end
// the agent's cycle.
func TestMergeSignalIsNotEvidence(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1})
	h.mergesAt("") // ground truth: this cycle landed nothing
	h.merges(3)    // three merge signals arrive anyway

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	// The run ends on the re-dispatch budget, not on a phantom green cycle.
	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonRedispatchBudget)
}

// TestBuildTerminalSignalWakesTheBuildWait exercises the build phase's wait: the
// first poll finds a component still building, the signal wakes the loop, and
// the re-poll settles it.
func TestBuildTerminalSignalWakesTheBuildWait(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1}, MilestoneSnapshot{})
	h.buildsAre(
		CycleBuildState{Expected: 2, Settled: 1},
		CycleBuildState{Expected: 2, Settled: 2},
	)
	h.merges(1)
	h.signal(delivery.SigRunBuildTerminal, 2*time.Second)

	h.run(delivery.RunOriginIncidentAdoption, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
}

// TestQueryRunStatus proves the loop reports its POSITION live — the thing no
// database column holds, because fix and conflict cycles re-enter earlier phases
// and a stored phase enum would lie mid-loop.
func TestQueryRunStatus(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 2, Total: 2}, MilestoneSnapshot{})

	var midRun delivery.RunStatus
	h.env.RegisterDelayedCallback(func() {
		resp, err := h.env.QueryWorkflow(delivery.QueryRunStatus)
		require.NoError(t, err)
		require.NoError(t, resp.Get(&midRun))
		h.env.SignalWorkflow(delivery.SigRunPRMerged, delivery.RunSignal{Signal: delivery.SigRunPRMerged})
	}, time.Second)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	require.Equal(t, delivery.RunStateRunning, midRun.State)
	require.Equal(t, delivery.RunPhaseCoding, midRun.Phase)
	require.Equal(t, delivery.CycleKindCoding, midRun.CycleKind)
	require.Equal(t, 1, midRun.CycleAttempt)
	require.Equal(t, 1, midRun.CyclesTotal)
	require.Equal(t, delivery.RunDefaultCycleCeiling, midRun.CycleCeiling)
	require.Equal(t, testMilepost, midRun.MilestoneNumber)

	var settled delivery.RunStatus
	resp, err := h.env.QueryWorkflow(delivery.QueryRunStatus)
	require.NoError(t, err)
	require.NoError(t, resp.Get(&settled))
	require.Equal(t, delivery.RunStateSucceeded, settled.State)
	require.Equal(t, delivery.RunPhaseSettling, settled.Phase)
	require.Equal(t, delivery.RunStateSucceeded, res.State)
}

// TestZeroCycleRun_WaitsForWorkInsteadOfSettling is the regression for a run
// that closed its version having never dispatched anything.
//
// The plan path admits the run row BEFORE its planning turn, so the supervisor
// can legitimately poll a milestone whose issues have not been minted yet. An
// empty working set at that moment means "not planned yet", not "delivered" —
// the run must park in §7's unbounded wait, and the work that arrives
// afterwards must still be worked.
func TestZeroCycleRun_WaitsForWorkInsteadOfSettling(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{},                  // the planning turn has not minted its issues yet
		MilestoneSnapshot{Work: 1, Total: 1}, // they land
		MilestoneSnapshot{},                  // and the cycle delivers them
	)

	var waiting delivery.RunStatus
	var closedWhileWaiting, dispatchedWhileWaiting int
	var settledWhileWaiting string
	h.env.RegisterDelayedCallback(func() {
		resp, err := h.env.QueryWorkflow(delivery.QueryRunStatus)
		require.NoError(t, err)
		require.NoError(t, resp.Get(&waiting))
		closedWhileWaiting, dispatchedWhileWaiting = h.closedCount(), h.dispatchCount()
		settledWhileWaiting = h.settledState()
		h.env.SignalWorkflow(delivery.SigRunWorkable, delivery.RunSignal{Signal: delivery.SigRunWorkable})
	}, time.Second)
	h.signal(delivery.SigRunPRMerged, 2*time.Second)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	require.Equal(t, delivery.RunStateWaiting, waiting.State,
		"a run that has never dispatched must park on an empty working set, not settle")
	require.Equal(t, delivery.RunPhaseWaiting, waiting.Phase)
	require.Equal(t, "", settledWhileWaiting, "nothing may write the run's outcome while it waits")
	require.Equal(t, 0, closedWhileWaiting, "the version's milestone must still be open")
	require.Equal(t, 0, dispatchedWhileWaiting)

	// The work that arrived later is picked up by the very next boundary.
	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []string{delivery.CycleKindCoding}, h.dispatchKinds())
	require.Equal(t, 1, res.Cycles)
	require.Equal(t, 1, h.closed, "the milestone closes only once the run delivered something")
}

// TestZeroCycleRun_WaitsUntilCancelled is the other half of the same rule: the
// wait is UNBOUNDED. A milestone that never receives work is not a delivered
// version, however many poll backstops pass — only a human cancelling ends it,
// and a cancelled increment keeps its milestone open.
func TestZeroCycleRun_WaitsUntilCancelled(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{})
	h.signal(delivery.SigRunCancel, time.Hour) // several poll backstops later

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateCancelled, "")
	require.Equal(t, 0, h.dispatchCount())
	require.Equal(t, 0, h.closed, "a run that delivered nothing must not close the version")
}

// dedupeStates collapses repeated run-state writes so a test can assert the
// oscillation rather than the write count.
func dedupeStates(states []string) []string {
	var out []string
	for _, s := range states {
		if len(out) == 0 || out[len(out)-1] != s {
			out = append(out, s)
		}
	}
	return out
}
