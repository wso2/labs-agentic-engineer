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
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
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
	// gateMints / plans record the planning phase's two activities, in the order
	// the loop drove them.
	gateMints []PlanMilestoneInput
	plans     []PlanMilestoneInput
	// deploys records every promote the loop asked for, and deployMints every
	// deploy-fix filing. wavePlans records every ordering request, so a test can
	// assert the stage planned before it wrote.
	deploys     []DeployCycleInput
	wavePlans   []DeployCycleInput
	deployMints []MintDeployFixIssuesInput
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
	h.env.RegisterActivity(acts.ProvisionGates)
	h.env.RegisterActivity(acts.PlanMilestone)
	h.env.RegisterActivity(acts.PlanDeployWaves)
	h.env.RegisterActivity(acts.DeployCycle)
	h.env.RegisterActivity(acts.PollCycleDeployments)
	h.env.RegisterActivity(acts.MintDeployFixIssues)

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

// planIs pins what the planning phase's two activities answer. Registered per
// test rather than as a constructor default for the reason dispatchIs gives:
// the harness consumes expectations in registration order.
func (h *harness) planIs(gatesErr, planErr error) {
	h.set["plan"] = true
	h.env.OnActivity(h.acts.ProvisionGates, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.gateMints = append(h.gateMints, args.Get(1).(PlanMilestoneInput))
		}).Return(gatesErr)
	h.env.OnActivity(h.acts.PlanMilestone, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.plans = append(h.plans, args.Get(1).(PlanMilestoneInput))
		}).Return(planErr)
}

// planCounts reports the two tallies, read safely.
func (h *harness) planCounts() (gates, plans int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.gateMints), len(h.plans)
}

// deployIs pins what promoting the cycle's components answers. Registered per
// test rather than as a constructor default for the reason dispatchIs gives:
// the harness consumes expectations in registration order.
func (h *harness) deployIs(err error) {
	h.set["deploy"] = true
	h.env.OnActivity(h.acts.DeployCycle, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.deploys = append(h.deploys, args.Get(1).(DeployCycleInput))
		}).Return([]delivery.ComponentDeploy(nil), err)
}

// wavesAre pins the deploy order the planner answers.
func (h *harness) wavesAre(waves [][]string, err error) {
	h.set["waves"] = true
	h.env.OnActivity(h.acts.PlanDeployWaves, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.wavePlans = append(h.wavePlans, args.Get(1).(DeployCycleInput))
		}).Return(waves, err)
}

// wavesAreOneWave is the default: whatever the cycle built, promoted together —
// what a project with no hard wiring edges gets.
//
// Derived from the REQUEST rather than hardcoded, so a test that changes which
// components its builds report cannot end up silently planning a different set
// than the one being deployed.
func (h *harness) wavesAreOneWave() {
	h.set["waves"] = true
	h.env.OnActivity(h.acts.PlanDeployWaves, mock.Anything, mock.Anything).
		Return(func(_ context.Context, in DeployCycleInput) ([][]string, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.wavePlans = append(h.wavePlans, in)
			if len(in.Components) == 0 {
				return nil, nil
			}
			return [][]string{in.Components}, nil
		})
}

// deploymentsAre queues the readiness polls, in order; the last repeats.
func (h *harness) deploymentsAre(states ...CycleDeployState) {
	h.set["deployments"] = true
	for i, st := range states {
		call := h.env.OnActivity(h.acts.PollCycleDeployments, mock.Anything, mock.Anything).Return(st, nil)
		if i < len(states)-1 {
			call.Once()
		}
	}
}

// deployMintsAre pins what filing deploy-fix work answers.
func (h *harness) deployMintsAre(filed []int) {
	h.set["deployMint"] = true
	h.env.OnActivity(h.acts.MintDeployFixIssues, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.deployMints = append(h.deployMints, args.Get(1).(MintDeployFixIssuesInput))
		}).Return(filed, nil)
}

// deployCount / deployMintCount are the tallies, read safely.
func (h *harness) deployCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.deploys)
}

func (h *harness) deployMintCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.deployMints)
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
		h.buildsAre(CycleBuildState{Expected: 1, Settled: 1, Components: []string{"order-service"}})
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
	if !h.set["plan"] {
		h.planIs(nil, nil)
	}
	if !h.set["waves"] {
		h.wavesAreOneWave()
	}
	if !h.set["deploy"] {
		h.deployIs(nil)
	}
	if !h.set["deployments"] {
		h.deploymentsAre(CycleDeployState{Expected: 1, Ready: 1})
	}
	if !h.set["deployMint"] {
		h.deployMintsAre([]int{testRepairIssue})
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

// TestBuildsGreen_DeploysBeforeSettling pins the stage this loop now owns. A
// version is not delivered when its builds are green — it is delivered when its
// components are SERVING — so the cycle promotes the release itself and waits
// for the binding to be Ready before the boundary can settle the run.
func TestBuildsGreen_DeploysBeforeSettling(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1}, MilestoneSnapshot{})
	h.merges(1)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, 2, h.deployCount(),
		"one green cycle = one wave promoted, then one converge that promotes nothing")
	require.Equal(t, []string{"order-service"}, h.deploys[0].Components,
		"the deploy promotes the components the BUILD poll reported, not a re-derived set")
	require.Equal(t, testMergeSHA, h.deploys[0].CommitSHA,
		"the promote is pinned to the cycle's own merge commit")
}

// TestRedBuild_DoesNotDeploy: a component that did not build has no image to
// promote, so the red cycle must not reach the deploy stage at all. The fix
// cycle that follows passes through the green path and promotes then.
func TestRedBuild_DoesNotDeploy(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 1, Total: 1},
		MilestoneSnapshot{Work: 1, Total: 1},
		MilestoneSnapshot{},
	)
	h.buildsAre(
		CycleBuildState{Expected: 1, Settled: 1, Red: []string{"order-service"}, Components: []string{"order-service"}},
		CycleBuildState{Expected: 1, Settled: 1, Components: []string{"order-service"}},
	)
	h.merges(2)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, 2, h.deployCount(),
		"only the GREEN cycle deploys — its one wave and its converge, and nothing from the red one")
}

// TestDeploy_PromotesWaveByWaveThenConverges pins the whole stage's shape.
//
// A web app reads its backend's address out of window._env_ at module load, and
// that address exists only once the backend has a rendered binding — so the
// backend's wave goes first and the SPA's config is right the first time it is
// written. What flows the other way (a protected API's CORS allowlist is the
// project's SPA origins) cannot be known on the way up and is written by ONE
// converge at the end.
//
// The converge carries no commit, and that is the load-bearing assertion: a
// second promote re-cuts a release that already exists, which OpenChoreo refuses
// with a bare 500 and Temporal then retries forever.
func TestDeploy_PromotesWaveByWaveThenConverges(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1}, MilestoneSnapshot{})
	h.wavesAre([][]string{{"todo-api"}, {"todo-webapp"}}, nil)
	h.buildsAre(CycleBuildState{Expected: 2, Settled: 2, Components: []string{"todo-api", "todo-webapp"}})
	h.merges(1)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Len(t, h.deploys, 3, "two waves promote, then one converge")
	require.Equal(t, []string{"todo-api"}, h.deploys[0].Components,
		"the provider goes first — its address is what the consumer's config carries")
	require.Equal(t, []string{"todo-webapp"}, h.deploys[1].Components)
	require.Equal(t, testMergeSHA, h.deploys[0].CommitSHA)
	require.Equal(t, testMergeSHA, h.deploys[1].CommitSHA)

	require.Equal(t, []string{"todo-api", "todo-webapp"}, h.deploys[2].Components,
		"the converge re-asserts the whole set, not just the last wave")
	require.Empty(t, h.deploys[2].CommitSHA,
		"the converge promotes nothing: an empty commit is what keeps it from re-cutting a release")

	require.Len(t, h.wavePlans, 1, "the order is planned once per cycle, before anything is written")
	require.Equal(t, []string{"todo-api", "todo-webapp"}, h.wavePlans[0].Components)
}

// A wave that cannot come up ends the stage there: the waves after it depend on
// its addresses, and converging a set that is not serving would write wiring
// nothing can use.
func TestDeploy_FailedWaveRunsNoFurtherWaveOrConverge(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1}, MilestoneSnapshot{})
	h.wavesAre([][]string{{"todo-api"}, {"todo-webapp"}}, nil)
	h.buildsAre(CycleBuildState{Expected: 2, Settled: 2, Components: []string{"todo-api", "todo-webapp"}})
	h.deploymentsAre(CycleDeployState{Expected: 1, Failed: []string{"todo-api"}})
	h.deployMintsAre(nil)
	h.merges(1)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonDeployBudget)
	require.Len(t, h.deploys, 1, "the first wave failed; nothing downstream of it runs")
	require.Equal(t, []string{"todo-api"}, h.deploys[0].Components)
}

// An order that cannot be satisfied has to arrive as a DEPLOY FAILURE, not as a
// workflow error.
//
// Returned raw it would fail the workflow before the boundary could mint the fix
// work or settle the row — and a non-terminal run row blocks every later build on
// the project, which is the wedge this whole stage exists to stop producing. So
// the permanent error converts to cycleDeployFailed and takes the ordinary
// recovery path: file the work, settle on the deploy budget when nothing fixes it.
func TestDeployOrderUnsatisfiable_SettlesAsADeployFailure(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 1, Total: 1}, // dispatch
		MilestoneSnapshot{},                  // nothing came back to fix it
	)
	h.buildsAre(CycleBuildState{Expected: 2, Settled: 2, Components: []string{"web-a", "web-b"}})
	h.wavesAre(nil, temporal.NewNonRetryableApplicationError(
		"deployment: hard dependency cycle among components web-a needs [web-b]; web-b needs [web-a]",
		errTypePermanentDeploy, nil))
	h.deployMintsAre(nil)
	h.merges(1)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonDeployBudget)
	require.Empty(t, h.deploys, "an unsatisfiable order promotes nothing")
	require.Equal(t, 1, h.deployMintCount(), "the fix work is filed, not swallowed by a failed workflow")
	require.ElementsMatch(t, []string{"web-a", "web-b"}, h.deployMints[0].Components,
		"a cycle is nobody's individual fault — every component in it is named")
	require.Contains(t, h.deployMints[0].Reasons["web-a"], "hard dependency cycle",
		"the cause reaches the issue body rather than only the workflow history")
}

// A plan that could not be READ is a blip, not an answer, and must keep the
// unbounded retry that is right for it — the opposite of the case above.
func TestDeployOrderUnreadable_IsRetriedNotSettled(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1}, MilestoneSnapshot{})
	h.buildsAre(CycleBuildState{Expected: 1, Settled: 1, Components: []string{"order-service"}})
	h.wavesAre(nil, errors.New("oc: design read timed out"))
	h.merges(1)

	h.run(delivery.RunOriginSpecBuild, 0)

	require.True(t, h.env.IsWorkflowCompleted())
	require.Error(t, h.env.GetWorkflowError(), "a transient planning failure must not settle the run")
	require.Zero(t, h.deployMintCount(), "no fix work is filed for a blip")
}

// The single-wave shape of the same rule: a set that never came up has nothing
// to converge onto, and the failure is already the cycle's answer.
func TestDeploy_FailedWaveRunsNoConverge(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1}, MilestoneSnapshot{})
	h.deploymentsAre(CycleDeployState{Expected: 1, Failed: []string{"order-service"}})
	h.deployMintsAre(nil)
	h.merges(1)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonDeployBudget)
	require.Equal(t, 1, h.deployCount(), "a failed pass is the answer; no converge follows it")
}

// TestDeployFailed_BecomesTheNextCyclesWork is the recovery shape a failed
// deployment shares with a red build: the supervisor files the fix work (nothing
// else can — a ReleaseBinding produces no webhook), and the next cycle picks it
// out of the working set like any other issue.
func TestDeployFailed_BecomesTheNextCyclesWork(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 1, Total: 1}, // dispatch
		MilestoneSnapshot{Work: 1, Total: 1}, // the deploy-fix issue
		MilestoneSnapshot{},                  // delivered
	)
	h.deploymentsAre(
		CycleDeployState{Expected: 1, Failed: []string{"order-service"}, Reasons: map[string]string{"order-service": "RenderingFailed"}},
		CycleDeployState{Expected: 1, Ready: 1},
	)
	h.merges(2)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []string{delivery.CycleKindCoding, delivery.CycleKindFix}, h.dispatchKinds(),
		"a failed deployment recovers through an ordinary fix cycle")
	require.Equal(t, 1, h.deployMintCount(), "the supervisor files the deploy-fix work itself")
	require.Equal(t, []string{"order-service"}, h.deployMints[0].Components)
	require.Equal(t, "RenderingFailed", h.deployMints[0].Reasons["order-service"],
		"OpenChoreo's own reason reaches the issue body")
}

// TestDeployFailed_WithNoRecovery_SettlesOnTheDeployBudget: the components built
// but never came up and nothing joined the milestone to fix them. The run must
// NOT settle delivered — a version that compiled and does not run is exactly the
// state that would otherwise be mistaken for success.
func TestDeployFailed_WithNoRecovery_SettlesOnTheDeployBudget(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 1, Total: 1}, // dispatch
		MilestoneSnapshot{},                  // nothing came back to fix it
	)
	h.deploymentsAre(CycleDeployState{Expected: 1, Failed: []string{"order-service"}})
	h.deployMintsAre(nil)
	h.merges(1)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonDeployBudget)
}

// TestValidationCycle_TouchesNoComponent_SkipsDeploy: a validation cycle's pull
// request carries tests and a report, so there is nothing to promote. The stage
// must be a no-op rather than waiting for a binding that will never change.
func TestValidationCycle_TouchesNoComponent_SkipsDeploy(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 1, Total: 1}, // the coding cycle
		MilestoneSnapshot{},                  // deployed-green: validation follows
	)
	h.buildsAre(
		CycleBuildState{Expected: 1, Settled: 1, Components: []string{"order-service"}},
		CycleBuildState{}, // the validation cycle's merge touched nothing
	)
	h.validationIs(77, delivery.ValidationVerdictPassed)
	h.merges(2)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, 2, h.deployCount(),
		"only the coding cycle deploys (its wave plus its converge); the validation cycle has nothing to promote")
}

// TestDeployNeverReady_ExpiresIntoAFixIssue pins the loop's SECOND deadline and
// why it exists.
//
// awaitBuilds can wait forever safely because a WorkflowRun always terminates.
// A ReleaseBinding never does: it is a level OpenChoreo reconciles continuously,
// so an image that will never pull and a rollout thirty seconds from Ready are
// indistinguishable from inside the loop. Without a deadline the run hangs on
// the first one; with it, a stuck deployment becomes ordinary fix work.
func TestDeployNeverReady_ExpiresIntoADeployFailure(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 1, Total: 1}, // dispatch
		MilestoneSnapshot{},                  // nothing recovered it
	)
	// Neither Ready nor Failed, ever: the rollout that never lands. Only the
	// deadline can end this — which is the whole point of having one here.
	h.deploymentsAre(CycleDeployState{Expected: 1, Pending: []string{"order-service"}})
	h.deployMintsAre(nil)
	h.merges(1)

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonDeployBudget)
	require.Positive(t, h.deployMintCount(),
		"the expiry files fix work rather than hanging the run")
	require.Equal(t, []string{"order-service"}, h.deployMints[0].Components,
		"the components that never came up are the ones named")
}

// The deadline belongs to the STAGE, not to a wave.
//
// It is created once in deployCycle and passed into every wait, so a design that
// happens to split into more levels does not buy its run more time to come up. A
// per-wave timer would be invisible in every other assertion — the run still
// fails, the same issues are filed — and would silently multiply what a version
// is allowed by however many waves it has.
func TestDeployDeadline_IsTheStagesNotEachWaves(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{Work: 1, Total: 1},
		MilestoneSnapshot{},
	)
	h.buildsAre(CycleBuildState{Expected: 2, Settled: 2, Components: []string{"api", "web"}})
	h.wavesAre([][]string{{"api"}, {"web"}}, nil)
	// The FIRST wave never lands, so the stage can only end on the deadline.
	h.deploymentsAre(CycleDeployState{Expected: 1, Pending: []string{"api"}})
	h.deployMintsAre(nil)
	h.merges(1)

	start := h.env.Now()
	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)
	elapsed := h.env.Now().Sub(start)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonDeployBudget)
	require.Less(t, elapsed, 2*deployReadyTimeout,
		"the stage spent more than one deployReadyTimeout: the waves are each running their own clock")
	require.Len(t, h.deploys, 1, "the second wave never starts — the first never served")
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

// ---- the planning phase ------------------------------------------------------

// errPermanentForTest is what planErr produces for an answer. The mocked
// activity bypasses planErr, so a test that wants "permanent" has to return the
// classified error itself — a plain one would sit under Temporal's default
// unbounded retry and never reach a verdict.
func errPermanentForTest() error {
	return temporal.NewNonRetryableApplicationError(
		"repository not found", errTypePermanentPlan, sourcecontrol.ErrRepoNotFound)
}

// A run that OWNS a version fills its milestone first: gates, then the planning
// turn. The order is the contract — an open gate is a dispatch hold, so minting
// the gates before the work is what makes the dispatch predicate honest from the
// moment the first Task lands.
func TestPlanningPhase_MintsGatesThenPlansBeforeWorking(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1}, MilestoneSnapshot{})
	h.merges(1)

	h.runWith(RunInput{Origin: delivery.RunOriginSpecBuild, Tag: "v3"})
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	gates, plans := h.planCounts()
	require.Equal(t, 1, gates, "the version's gates are minted exactly once")
	require.Equal(t, 1, plans, "the planning turn runs exactly once")
	require.Equal(t, "v3", h.plans[0].Tag)
	require.Equal(t, testMilepost, h.plans[0].MilestoneNumber)
	// The cycle still ran, so planning did not replace working the milestone.
	require.Equal(t, []string{delivery.CycleKindCoding}, h.dispatchKinds())
}

// Only a run that owns a version plans one. Every other origin adopts a
// milestone somebody already filled, and is recognised by carrying no Tag —
// re-planning there would re-derive a version from a run that was only meant to
// resume.
func TestPlanningPhase_SkippedWhenTheRunOwnsNoVersion(t *testing.T) {
	for _, origin := range []string{delivery.RunOriginIncidentAdoption, delivery.RunOriginRevalidate} {
		t.Run(origin, func(t *testing.T) {
			h := newHarness(t)
			h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1}, MilestoneSnapshot{})
			h.merges(1)

			h.runWith(RunInput{Origin: origin}) // no Tag
			h.result(t)

			gates, plans := h.planCounts()
			require.Equal(t, 0, gates, "an adopted milestone is already filled")
			require.Equal(t, 0, plans)
		})
	}
}

// A run that did NOT plan may not read an empty working set as "delivered".
//
// The immediate zero-cycle settle is justified by planning being the workflow's
// own first phase — by the time the loop polls, the plan has landed or the run has
// settled. That reasoning covers a run that plans and nothing else. An incident
// adoption fires on a label write, and GitHub's issue index lags a write: a run
// that polls before the labelled issue is indexed sees Work == 0 with no cycles
// behind it. Settling there closes the milestone for work nothing dispatched.
//
// So it must PARK and wait for the issue to appear, then dispatch it.
func TestZeroCycleAdoption_ParksForTheLaggingIndexInsteadOfSettling(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{},                  // the index has not caught up yet
		MilestoneSnapshot{Work: 1, Total: 1}, // the labelled issue appears
		MilestoneSnapshot{},                  // worked, and now genuinely empty
	)
	h.merges(1)
	// The webhook that wakes the park once the issue is indexed.
	h.signal(delivery.SigRunWorkable, 2*time.Second)

	h.runWith(RunInput{Origin: delivery.RunOriginIncidentAdoption}) // no Tag
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, 1, h.dispatchCount(),
		"the adopted issue was dispatched; an immediate settle would have closed the milestone over it")
}

// The other side of the same predicate: a run that DOES own its version settles
// immediately on an empty working set, because its plan has demonstrably run.
func TestZeroCycleSpecBuild_SettlesImmediately(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{}) // planning minted nothing to work
	h.runWith(RunInput{Origin: delivery.RunOriginSpecBuild, Tag: "v3"})
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Zero(t, h.dispatchCount(), "nothing to dispatch — the version is delivered")
}

// The whole point of moving planning here: a planning failure that repeating
// cannot change settles the version, exactly as the detached goroutine did —
// but a transient one is now Temporal's to retry, not a version-killer.
func TestPlanningPhase_PermanentFailureSettlesPlanFailed(t *testing.T) {
	for _, tc := range []struct {
		name             string
		gatesErr, planEr error
	}{
		{"gates", errPermanentForTest(), nil},
		{"planner", nil, errPermanentForTest()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.milestoneIs(MilestoneSnapshot{Work: 1, Total: 1})
			h.planIs(tc.gatesErr, tc.planEr)

			h.runWith(RunInput{Origin: delivery.RunOriginSpecBuild, Tag: "v3"})
			res := h.result(t)

			h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonPlanFailed)
			require.Equal(t, 0, h.dispatchCount(), "a version that could not be planned dispatches nothing")
		})
	}
}

// A plan that mints nothing settles the version DELIVERED rather than parking.
//
// This is the rule that replaced the zero-cycle wait. That wait existed only
// because the click admitted the run row before its planning turn, so a poll
// could land mid-plan and read "not planned yet" as "nothing to do". Planning is
// this workflow's own first phase now, so by the time anything is polled the
// plan has landed — and an empty milestone is exactly what a re-build of a
// version whose Tasks all already exist and are closed should produce.
func TestPlanningPhase_EmptyPlanSettlesDelivered(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{}) // planning minted nothing

	h.runWith(RunInput{Origin: delivery.RunOriginSpecBuild, Tag: "v3"})
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, 0, h.dispatchCount())
	require.Equal(t, 1, h.closed, "a delivered version closes its milestone")
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
