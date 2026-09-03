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
	deploys     []PromoteInput
	wavePlans   []PlanDeployInput
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
	// taskCloses records every close of the version's validation task. It is the
	// infinite-loop guard's assertion surface: the reconcile sweep starts a
	// validation run BECAUSE that task is open, so an ending that leaves it open is
	// an ending that repeats forever.
	taskCloses []CloseValidationIssueInput
	// halts records every halt of a failed run's unfinished work. It is the
	// budget's assertion surface: the reconcile sweep restarts open work of a
	// kind on a milestone with no live run, so a failed settle that halted
	// nothing is a budget the platform no longer enforces.
	halts []HaltWorkInput
	// cancels records every close of a cancelled run's in-flight work. It is the
	// cancel's assertion surface for the same reason halts is the halt's: the
	// reconcile sweep restarts open work of a kind on a milestone with no live run,
	// so a cancelled settle that closed nothing is a cancel button that stops the
	// run and then pays for its replacement a minute later.
	cancels []CloseCancelledWorkInput
	closed  int
	// stateWrites keeps the full SetRunState payloads, so a test can assert on
	// the PARK's explanation — the reason and the dependency names — and not
	// merely that the run went to `waiting`. A park that does not say what it is
	// waiting for is the failure this slice exists to prevent.
	stateWrites []SetRunStateInput
	// gateChecks counts the deploy gate's reads, so a test can assert the gate
	// was consulted (or deliberately was not, on a cycle that promotes nothing).
	gateChecks int

	// The VERSION STATE model: what the cluster would answer ReadVersionState.
	//
	// Derived rather than pinned, because the loop now reads the same world
	// twice — once to decide what to promote, once at the boundary to decide
	// whether the version may be delivered — and a static answer could only be
	// right for one of them. buildStates is what the test already pinned
	// (a component green at the merge SHA is BEHIND until something promotes it,
	// a red one is UNBUILT), and promoted is what the loop actually wrote. So a
	// test describes builds and deployments as before, and the version state
	// follows from them the way it follows from the cluster.
	seenBuilds []CycleBuildState
	promoted   map[string]string
	// versionOverride, when set, replaces the derived model — for the tests that
	// are ABOUT a state the loop's own actions cannot produce, such as a
	// component nothing ever bound.
	versionOverride *delivery.VersionState
}

// newHarness registers the activities whose behaviour never varies — the
// writers the loop records its progress through. The facts a run turns on are
// pinned per test, or defaulted at run() time.
func newHarness(t *testing.T) *harness {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	h := &harness{env: ts.NewTestWorkflowEnvironment(), set: map[string]bool{}, promoted: map[string]string{}}
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
	h.env.RegisterActivity(acts.ReadValidationHistory)
	h.env.RegisterActivity(acts.CloseValidationIssue)
	h.env.RegisterActivity(acts.DispatchAgent)
	h.env.RegisterActivity(acts.ProvisionGates)
	h.env.RegisterActivity(acts.PlanMilestone)
	h.env.RegisterActivity(acts.PlanDeployWaves)
	h.env.RegisterActivity(acts.PromoteWave)
	h.env.RegisterActivity(acts.PollDeployments)
	h.env.RegisterActivity(acts.MintDeployFixIssues)
	h.env.RegisterActivity(acts.HaltUnfinishedWork)
	h.env.RegisterActivity(acts.CloseCancelledWork)
	h.env.RegisterActivity(acts.CheckDeployReadiness)
	h.env.RegisterActivity(acts.ReadVersionState)

	h.env.OnActivity(acts.SetRunState, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			in := args.Get(1).(SetRunStateInput)
			h.mu.Lock()
			defer h.mu.Unlock()
			h.states = append(h.states, in.State)
			h.stateWrites = append(h.stateWrites, in)
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
	// The halt never varies either: every FAILED settle marks the work it could
	// not finish, so a test asserts on WHAT was halted rather than on whether the
	// activity was pinned.
	h.env.OnActivity(acts.HaltUnfinishedWork, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.halts = append(h.halts, args.Get(1).(HaltWorkInput))
		}).Return([]int{}, nil)
	// Neither does the cancel's close: every CANCELLED settle closes the work the
	// run had in flight, so a test asserts on WHAT was closed rather than on
	// whether the activity was pinned.
	h.env.OnActivity(acts.CloseCancelledWork, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.cancels = append(h.cancels, args.Get(1).(CloseCancelledWorkInput))
		}).Return([]int{}, nil)
	// The validation task's close never varies — it is the platform's write, made
	// on every ending — so it is a constructor default like the other writers.
	h.env.OnActivity(acts.CloseValidationIssue, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.taskCloses = append(h.taskCloses, args.Get(1).(CloseValidationIssueInput))
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

// planStep names one of the two activities the planning phase drives, for
// planIsCancelledDuring.
type planStep int

const (
	gatesActivity planStep = iota
	planActivity
)

// planIsCancelledDuring pins the planning phase's activities and makes a cancel
// arrive WHILE the named one is running.
//
// The signal is sent from inside the activity mock, and that is a necessity
// rather than a shortcut: the environment auto-advances its clock only when
// nothing is executing, so a RegisterDelayedCallback scheduled for the window an
// activity is in flight would never fire. Signalling from the activity puts the
// cancel in the workflow's channel before the workflow task that reads that
// activity's result — which is exactly the race the phase has to resolve.
//
// failWith, when supplied, is what the named activity ALSO answers, so a test can
// pin what the run reports when a cancel and a failure land together.
func (h *harness) planIsCancelledDuring(step planStep, failWith ...error) {
	h.set["plan"] = true
	var stepErr error
	if len(failWith) > 0 {
		stepErr = failWith[0]
	}
	cancel := func() {
		h.env.SignalWorkflow(delivery.SigRunCancel, delivery.RunSignal{
			Signal: delivery.SigRunCancel, MilestoneNumber: testMilepost,
		})
	}
	var gatesErr, planTurnErr error
	if step == gatesActivity {
		gatesErr = stepErr
	} else {
		planTurnErr = stepErr
	}
	h.env.OnActivity(h.acts.ProvisionGates, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			h.mu.Lock()
			h.gateMints = append(h.gateMints, args.Get(1).(PlanMilestoneInput))
			h.mu.Unlock()
			if step == gatesActivity {
				cancel()
			}
		}).Return(gatesErr)
	h.env.OnActivity(h.acts.PlanMilestone, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			h.mu.Lock()
			h.plans = append(h.plans, args.Get(1).(PlanMilestoneInput))
			h.mu.Unlock()
			if step == planActivity {
				cancel()
			}
		}).Return(planTurnErr)
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
	h.env.OnActivity(h.acts.PromoteWave, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			in := args.Get(1).(PromoteInput)
			h.mu.Lock()
			defer h.mu.Unlock()
			h.deploys = append(h.deploys, in)
			if err != nil {
				return // a failed promote wrote nothing
			}
			for _, t := range in.Targets {
				if t.CommitSHA == "" {
					continue // a converge moves no pin
				}
				h.promoted[t.Component] = t.CommitSHA
			}
		}).Return([]delivery.ComponentDeploy(nil), err)
}

// versionIs pins the version state outright, replacing the derived model. For
// the cases the loop's own actions cannot produce — a component whose binding
// nothing ever wrote, above all, which is the failure the delivery gate exists
// to catch.
func (h *harness) versionIs(components ...delivery.ComponentState) {
	h.set["version"] = true
	st := delivery.VersionState{Components: components}
	h.mu.Lock()
	h.versionOverride = &st
	h.mu.Unlock()
}

// versionState is what ReadVersionState answers: the override if a test pinned
// one, otherwise the model derived from the builds it pinned and the promotes
// the loop made.
//
// A component is SERVING once the loop promoted it at the commit its newest
// green build was cut from — which is exactly when the real read would say so,
// because the stage does not leave the promote until the binding is Ready.
func (h *harness) versionState() delivery.VersionState {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.versionOverride != nil {
		return *h.versionOverride
	}
	desired := map[string]string{}
	var order []string
	for _, st := range h.seenBuilds {
		for _, name := range st.Components {
			if _, seen := desired[name]; !seen {
				order = append(order, name)
			}
			desired[name] = testMergeSHA
		}
		for _, name := range st.Red {
			if _, seen := desired[name]; !seen {
				order = append(order, name)
			}
			// A red build is a component with no release to pin. The next state
			// in the queue may report it green, which overwrites this.
			desired[name] = ""
		}
	}
	out := delivery.VersionState{}
	for _, name := range order {
		st := delivery.ComponentState{Component: name, DesiredSHA: desired[name]}
		switch {
		case desired[name] == "":
			st.State = delivery.ComponentStateUnbuilt
		case h.promoted[name] == desired[name]:
			st.State, st.Ready = delivery.ComponentStateServing, true
		default:
			st.State = delivery.ComponentStateBehind
		}
		out.Components = append(out.Components, st)
	}
	return out
}

// wavesAre pins the levels the planner answers, by name. Each named component
// is promoted at the commit the version state says it is behind by, so a test
// describes the ORDER and the model supplies the commits.
func (h *harness) wavesAre(waves [][]string, err error) {
	h.set["waves"] = true
	h.env.OnActivity(h.acts.PlanDeployWaves, mock.Anything, mock.Anything).
		Return(func(_ context.Context, in PlanDeployInput) (delivery.DeployPlan, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.wavePlans = append(h.wavePlans, in)
			if err != nil {
				return delivery.DeployPlan{}, err
			}
			return planFor(in.Version, waves), nil
		})
}

// wavesAreOneWave is the default: everything the version is behind by,
// promoted together — what a project with no hard wiring edges gets.
//
// Derived from the STATE rather than hardcoded, so a test that changes which
// components its builds report cannot end up planning a different set than the
// one the loop would promote.
func (h *harness) wavesAreOneWave() {
	h.set["waves"] = true
	h.env.OnActivity(h.acts.PlanDeployWaves, mock.Anything, mock.Anything).
		Return(func(_ context.Context, in PlanDeployInput) (delivery.DeployPlan, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.wavePlans = append(h.wavePlans, in)
			return planFor(in.Version, nil), nil
		})
}

// planIsHolding pins a planner that refuses to promote the named components,
// however many cycles the run takes, and promotes everything else that is
// behind. It is the one decision the derived plan cannot make on its own,
// because it does not read a design.
func (h *harness) planIsHolding(held ...string) {
	h.set["waves"] = true
	holds := make(map[string]bool, len(held))
	for _, name := range held {
		holds[name] = true
	}
	h.env.OnActivity(h.acts.PlanDeployWaves, mock.Anything, mock.Anything).
		Return(func(_ context.Context, in PlanDeployInput) (delivery.DeployPlan, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.wavePlans = append(h.wavePlans, in)
			var promote []string
			for _, c := range in.Version.Components {
				if c.State == delivery.ComponentStateBehind && !holds[c.Component] {
					promote = append(promote, c.Component)
				}
			}
			plan := planFor(in.Version, [][]string{promote})
			for i := range plan.Held {
				plan.Held[i].WaitingOn = []string{"a provider that is not serving"}
			}
			return plan, nil
		})
}

// planFor is the harness's stand-in for the real planner: it levels the behind
// components the way the test asked (or all of them in one wave), pairs each
// with the commit the state says it is behind by, and waits on everything it
// promoted plus anything already converging.
//
// It deliberately does NOT hold anything. What a plan holds is the planner's
// own decision, tested against real designs in projects/wiring_graph_test.go;
// what these tests exercise is what the STAGE does with a plan.
func planFor(version delivery.VersionState, levels [][]string) delivery.DeployPlan {
	commits := map[string]string{}
	var behind []string
	plan := delivery.DeployPlan{}
	for _, c := range version.Components {
		switch c.State {
		case delivery.ComponentStateBehind:
			commits[c.Component] = c.DesiredSHA
			behind = append(behind, c.Component)
		case delivery.ComponentStateConverging:
			plan.Waited = append(plan.Waited, c.Component)
		}
	}
	if levels == nil && len(behind) > 0 {
		levels = [][]string{behind}
	}
	placed := map[string]bool{}
	for _, level := range levels {
		var wave []delivery.DeployTarget
		for _, name := range level {
			if _, isBehind := commits[name]; !isBehind {
				continue // already serving, or never built: not this pass's work
			}
			wave = append(wave, delivery.DeployTarget{Component: name, CommitSHA: commits[name]})
			plan.Waited = append(plan.Waited, name)
			placed[name] = true
		}
		if len(wave) > 0 {
			plan.Waves = append(plan.Waves, wave)
		}
	}
	// A behind component the levels did not name is HELD — which is how a test
	// describes a plan that refuses to promote something.
	for _, name := range behind {
		if !placed[name] {
			plan.Held = append(plan.Held, delivery.ComponentState{
				Component: name, State: delivery.ComponentStateHeld, DesiredSHA: commits[name],
			})
		}
	}
	return plan
}

// deploymentsAre queues the readiness polls, in order; the last repeats.
func (h *harness) deploymentsAre(states ...CycleDeployState) {
	h.set["deployments"] = true
	for i, st := range states {
		call := h.env.OnActivity(h.acts.PollDeployments, mock.Anything, mock.Anything).Return(st, nil)
		if i < len(states)-1 {
			call.Once()
		}
	}
}

// gateVerdictsAre queues the deploy gate's answers, in order; the last repeats.
// The gate is polled on every pass of the park, so a test that wants to see the
// run resume gives a blocked verdict first and an open one after.
func (h *harness) gateVerdictsAre(verdicts ...DeployGateVerdict) {
	h.set["gate"] = true
	for i, v := range verdicts {
		call := h.env.OnActivity(h.acts.CheckDeployReadiness, mock.Anything, mock.Anything).
			Run(func(mock.Arguments) {
				h.mu.Lock()
				defer h.mu.Unlock()
				h.gateChecks++
			}).Return(v, nil)
		if i < len(verdicts)-1 {
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

// workable is a milestone holding n workable issues out of total open ones, with
// no gate — and it counts them in BOTH working sets.
//
// Both, because most of what these tests exercise is the LOOP, which is one
// implementation shared by the dev and task species: a case that only counted
// the dev set would silently become a park when driven as a task run, and the
// test would time out rather than fail with a reason. The cases that are ABOUT
// the difference between the two populations spell the fields out (see
// TestTaskRun_NeverPicksUpAFailedDevRunsPlannedWork).
func workable(n, total int) MilestoneSnapshot {
	return MilestoneSnapshot{DevWork: n, TaskWork: n, Total: total}
}

// gated is workable with gates open — the dispatch hold.
func gated(n, gates, total int) MilestoneSnapshot {
	s := workable(n, total)
	s.Gates = gates
	return s
}

// repairing is workable whose defects came from a FAILED VERDICT: n open issues
// carrying `src/validation`. It is what makes a task run's bookend reopen the
// version's validation task.
func repairing(n, total int) MilestoneSnapshot {
	s := workable(n, total)
	s.ValidationRepairs = n
	return s
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
	for i, st := range states {
		// Each answer is RECORDED as the loop consumes it, which is what makes the
		// version model advance with the run: a component reported red in cycle
		// one and green in cycle two is unbuilt while cycle one is deciding and
		// behind once cycle two has seen its build. Deriving from the queue
		// instead would let the first cycle see the second cycle's builds.
		call := h.env.OnActivity(h.acts.PollCycleBuilds, mock.Anything, mock.Anything).
			Run(func(mock.Arguments) {
				h.mu.Lock()
				defer h.mu.Unlock()
				h.seenBuilds = append(h.seenBuilds, st)
			}).Return(st, nil)
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

// factsAre queues the loop's GROUND-TRUTH reads, in order; the last repeats.
//
// The ordered form exists because cancel and a landing are read from the same
// activity: a test that wants a run cancelled MID-CYCLE has to answer the
// boundary's question ("was this cancelled?") differently from the one the
// landing wait asks a moment later.
func (h *harness) factsAre(facts ...CycleFacts) {
	h.set["facts"] = true
	for i, f := range facts {
		call := h.env.OnActivity(h.acts.ReadCycleFacts, mock.Anything, mock.Anything).Return(f, nil)
		if i < len(facts)-1 {
			call.Once()
		}
	}
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

// haltedWork is what the run marked as work it could not finish, read safely.
func (h *harness) haltedWork() []HaltWorkInput {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]HaltWorkInput(nil), h.halts...)
}

// cancelledWork is what the run closed as work it had in flight, read safely.
func (h *harness) cancelledWork() []CloseCancelledWorkInput {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]CloseCancelledWorkInput(nil), h.cancels...)
}

// taskCloseCount is the validation-task close tally, read safely.
func (h *harness) taskCloseCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.taskCloses)
}

// validationHistoryIs pins what the LEDGER says about this version's earlier
// judgements: how many `validation` runs it has (including the one under test),
// and what the previous one concluded.
//
// Both span runs, which is why they are read rather than carried, and why a test
// that wants a SECOND attempt pins attempts=2 rather than driving two workflows.
func (h *harness) validationHistoryIs(attempts int, digest string) {
	h.set["history"] = true
	h.env.OnActivity(h.acts.ReadValidationHistory, mock.Anything, mock.Anything).
		Return(ValidationHistory{Attempts: attempts, Digest: digest}, nil)
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
	if !h.set["history"] {
		// A version's FIRST judgement: this attempt, and nothing to compare it to.
		h.validationHistoryIs(1, "")
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
	if !h.set["version"] {
		h.set["version"] = true
	}
	// Always registered, override or not: the read is on the path of every dev
	// run's delivery, and a mock the loop calls without an expectation fails the
	// workflow rather than the assertion.
	h.env.OnActivity(h.acts.ReadVersionState, mock.Anything, mock.Anything).
		Return(func(context.Context, ProjectRef) (delivery.VersionState, error) {
			return h.versionState(), nil
		})
	if !h.set["gate"] {
		// An OPEN gate is the default: the project is configured, so every test
		// that is not about the gate deploys exactly as it did before ADR-0023.
		h.gateVerdictsAre(DeployGateVerdict{})
	}
}

// parksOnValues returns the park writes the deploy gate made, and the tally of
// gate reads, read safely.
func (h *harness) parksOnValues() (parks []SetRunStateInput, checks int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, in := range h.stateWrites {
		if in.WaitingReason == delivery.RunWaitingOnExternalValues {
			parks = append(parks, in)
		}
	}
	return parks, h.gateChecks
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

func (h *harness) run(kind string, ceiling int) {
	h.runWith(RunInput{Kind: kind, CycleCeiling: ceiling})
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
	h.env.ExecuteWorkflow(workflowFor(in), in)
}

// workflowFor picks the entry point a run of this input's kind executes — the
// same routing Supervisor.StartRun performs off the run ROW.
//
// It is written here rather than passed per test so a test that changes a kind
// cannot keep driving the wrong loop: the kind IS the workflow now, not a branch
// inside one.
func workflowFor(in RunInput) any {
	switch runKind(in) {
	case delivery.RunKindValidation:
		return ValidationRunWorkflow
	case delivery.RunKindTask:
		return TaskRunWorkflow
	default:
		return DevRunWorkflow
	}
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
	h.milestoneIs(workable(2, 2), MilestoneSnapshot{})
	h.merges(1)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []string{delivery.CycleKindCoding}, h.dispatchKinds())
	require.Equal(t, 1, res.Cycles)
	require.Equal(t, 1, h.closed,
		"no acceptance oracle, so no validation task: nothing is coming and the milestone closes")
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
		workable(2, 2),      // dispatch
		workable(1, 1),      // the fix issue eventcore minted
		MilestoneSnapshot{}, // delivered
	)
	h.buildsAre(
		CycleBuildState{Expected: 1, Settled: 1, Red: []string{"order-service"}},
		CycleBuildState{Expected: 1, Settled: 1},
	)
	h.merges(2)

	h.run(delivery.RunKindTask, 0)
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
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.merges(1)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, 2, h.deployCount(),
		"one green cycle = one wave promoted, then one converge that promotes nothing")
	require.Equal(t, []delivery.DeployTarget{{Component: "order-service", CommitSHA: testMergeSHA}},
		h.deploys[0].Targets,
		"the deploy promotes what the VERSION is behind by, each at its own newest green build")
}

// TestDeliverVersion_RefusesAVersionThatIsNotServing reconstructs the incident
// this gate was written for.
//
// A build's webapp went red while its api went green. The api was never
// promoted — the deploy set was the cycle's path diff, and the fix cycle's diff
// touched only the webapp — so when the milestone emptied the run settled
// SUCCEEDED, minted the version's validation task and closed the increment with
// an api nothing had ever bound. Every cycle had been green; nothing in the
// sequence was a lie. The predicate was.
//
// The version is now READ at the boundary, and a component that is not serving
// fails the run with a reason that names the class rather than delivering
// software that is not running.
func TestDeliverVersion_RefusesAVersionThatIsNotServing(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.merges(1)
	// The api built and is serving; the webapp has a green build and no binding.
	// That is exactly what the cluster answered on 2026-09-01.
	h.versionIs(
		delivery.ComponentState{
			Component: "onboarding-api", State: delivery.ComponentStateServing, Ready: true,
		},
		delivery.ComponentState{
			Component: "onboarding-webapp", State: delivery.ComponentStateBehind,
			DesiredSHA: testMergeSHA,
		},
	)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonVersionIncomplete)
	h.env.AssertNotCalled(t, "EnsureValidationIssue", mock.Anything, mock.Anything)
	require.Equal(t, 0, h.closed,
		"and the milestone stays open: the way forward is more work in the same version")
	require.Len(t, h.haltedWork(), 1,
		"the failed settle halts what it could not finish, like every other failure")
}

// TestDeliverVersion_DeliversWhenEveryComponentServes is the other half of the
// same predicate, and it asserts the READ rather than the outcome: the boundary
// re-derives the version state instead of trusting the cycle that just ended,
// because between the two the world may have moved.
func TestDeliverVersion_DeliversWhenEveryComponentServes(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.merges(1)
	h.versionIs(delivery.ComponentState{
		Component: "order-service", State: delivery.ComponentStateServing, Ready: true,
	})
	h.validationIs(77, delivery.ValidationVerdictPassed)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	h.env.AssertCalled(t, "EnsureValidationIssue", mock.Anything, mock.Anything)
	require.Empty(t, res.ValidationVerdict,
		"a dev run delivers and never judges — the validation run owns the verdict")
}

// TestRedBuild_PromotesItsGreenSiblings is the incident, run forwards.
//
// Cycle one built two components: the api went green, the webapp went red. The
// cycle is RED — the webapp's fix issue is the next cycle's work — and the api
// is promoted anyway, in that same cycle. What this replaced returned at the red
// verdict before the deploy stage, so the api's release was never cut; and
// because the deploy set was the cycle's path diff, the fix cycle that followed
// touched only the webapp and never mentioned the api again. The api was
// stranded for the rest of the version's life.
//
// A red build cannot be helped by holding its siblings back, and the promotable
// rule is what makes promoting them safe: only components whose hard providers
// are serving or promoted ahead of them are written.
func TestRedBuild_PromotesItsGreenSiblings(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		workable(1, 1),      // dispatch
		workable(1, 1),      // the webapp's fix issue
		MilestoneSnapshot{}, // delivered
	)
	h.buildsAre(
		CycleBuildState{Expected: 2, Settled: 2,
			Components: []string{"onboarding-api", "onboarding-webapp"},
			Red:        []string{"onboarding-webapp"}},
		CycleBuildState{Expected: 1, Settled: 1, Components: []string{"onboarding-webapp"}},
	)
	h.merges(2)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []string{delivery.CycleKindCoding, delivery.CycleKindFix}, h.dispatchKinds(),
		"the red build still makes the next cycle a FIX cycle — the deploy does not launder the verdict")

	first := h.deploys[0]
	require.Equal(t, []string{"onboarding-api"}, delivery.TargetNames(first.Targets),
		"the RED cycle promotes its green sibling; the webapp has no release to pin")
	require.Equal(t, 0, h.deployMintCount(),
		"a component with no image is not a deployment failure — its red build already filed its issue")

	// And the fix cycle picks the webapp up, so the version ends serving both.
	var promoted []string
	for _, in := range h.deploys {
		for _, target := range in.Targets {
			if target.CommitSHA != "" {
				promoted = append(promoted, target.Component)
			}
		}
	}
	require.ElementsMatch(t, []string{"onboarding-api", "onboarding-webapp"}, promoted,
		"both components are serving by the time the version is delivered")
}

// TestHeldComponent_IsNeitherPromotedNorBlamed is the objection, answered: A
// needs B, A is green, B is red.
//
// A is HELD. Not promoted — a SPA published against an api with no address is a
// blank page — and not waited on and not filed against either, which is the part
// that is easy to get wrong: a deploy-fix issue would send an agent to debug a
// deployment that is perfectly healthy and simply has not happened. B's red
// build has its own issue, and the pass that finds B serving promotes A.
//
// Which components a plan holds is the planner's decision, tested against real
// designs in projects. What this pins is the STAGE's treatment of one.
func TestHeldComponent_IsNeitherPromotedNorBlamed(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), workable(1, 1), MilestoneSnapshot{})
	h.buildsAre(
		CycleBuildState{Expected: 2, Settled: 2,
			Components: []string{"api", "web"}, Red: []string{"api"}},
		CycleBuildState{Expected: 1, Settled: 1, Components: []string{"api"}},
	)
	// The planner refuses to promote the web app while its api is unbuilt.
	h.planIsHolding("web")
	h.merges(2)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	// The version never serves the web app, so the run refuses to deliver it —
	// loudly, rather than shipping a SPA with no backend.
	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonVersionIncomplete)
	for _, in := range h.deploys {
		require.NotContains(t, delivery.TargetNames(in.Targets), "web",
			"a held component is never written — not in a wave, and not in the converge either")
	}
	require.Equal(t, 0, h.deployMintCount(),
		"held is not a deployment failure: filing one would send an agent after nothing")
	require.Len(t, h.deploys, 2,
		"cycle one wrote NOTHING — not a wave and not a converge either, because a pass that "+
			"promotes nothing never consulted the deploy gate; cycle two promoted the api and converged it")
}

// TestReconcile_AServingVersionWritesNothing is the property that lets the
// reconcile run on every cycle, red or green: re-running it against a version
// that already serves what has been built promotes nothing, waits on nothing,
// and never consults the deploy gate.
//
// It is also what keeps a converge/validation-shaped cycle — a merge that
// rebuilt no component — from parking on a credential it will not use.
func TestReconcile_AServingVersionWritesNothing(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.merges(1)
	h.versionIs(delivery.ComponentState{
		Component: "order-service", State: delivery.ComponentStateServing, Ready: true,
		Pinned: "release-order-service",
	})

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Empty(t, h.deploys, "nothing is behind, so nothing is written — not even a converge")
	require.Empty(t, h.wavePlans, "and nothing is planned: the state alone answers it")
	_, checks := h.parksOnValues()
	require.Equal(t, 0, checks,
		"a pass that writes nothing must not consult the deploy gate — a credential it will not use cannot park the run")
	h.env.AssertNotCalled(t, "PollDeployments", mock.Anything, mock.Anything)
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
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.wavesAre([][]string{{"todo-api"}, {"todo-webapp"}}, nil)
	h.buildsAre(CycleBuildState{Expected: 2, Settled: 2, Components: []string{"todo-api", "todo-webapp"}})
	h.merges(1)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Len(t, h.deploys, 3, "two waves promote, then one converge")
	require.Equal(t, []delivery.DeployTarget{{Component: "todo-api", CommitSHA: testMergeSHA}},
		h.deploys[0].Targets,
		"the provider goes first — its address is what the consumer's config carries")
	require.Equal(t, []delivery.DeployTarget{{Component: "todo-webapp", CommitSHA: testMergeSHA}},
		h.deploys[1].Targets)

	require.Equal(t, []string{"todo-api", "todo-webapp"}, delivery.TargetNames(h.deploys[2].Targets),
		"the converge re-asserts the whole set, not just the last wave")
	for _, target := range h.deploys[2].Targets {
		require.Empty(t, target.CommitSHA,
			"the converge promotes nothing: an empty commit is what keeps it from re-cutting a release")
	}

	require.Len(t, h.wavePlans, 1, "the pass is planned once per cycle, before anything is written")
	require.Equal(t, []string{"todo-api", "todo-webapp"},
		behindNames(h.wavePlans[0].Version),
		"the planner is handed the version's own state, not the cycle's path diff")
}

// A wave that cannot come up ends the stage there: the waves after it depend on
// its addresses, and converging a set that is not serving would write wiring
// nothing can use.
func TestDeploy_FailedWaveRunsNoFurtherWaveOrConverge(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.wavesAre([][]string{{"todo-api"}, {"todo-webapp"}}, nil)
	h.buildsAre(CycleBuildState{Expected: 2, Settled: 2, Components: []string{"todo-api", "todo-webapp"}})
	h.deploymentsAre(CycleDeployState{Expected: 1, Failed: []string{"todo-api"}})
	h.deployMintsAre(nil)
	h.merges(1)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonDeployBudget)
	require.Len(t, h.deploys, 1, "the first wave failed; nothing downstream of it runs")
	require.Equal(t, []string{"todo-api"}, delivery.TargetNames(h.deploys[0].Targets))
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
		workable(1, 1),      // dispatch
		MilestoneSnapshot{}, // nothing came back to fix it
	)
	h.buildsAre(CycleBuildState{Expected: 2, Settled: 2, Components: []string{"web-a", "web-b"}})
	h.wavesAre(nil, temporal.NewNonRetryableApplicationError(
		"deployment: hard dependency cycle among components web-a needs [web-b]; web-b needs [web-a]",
		errTypePermanentDeploy, nil))
	h.deployMintsAre(nil)
	h.merges(1)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonDeployBudget)
	require.Empty(t, h.deploys, "an unsatisfiable order promotes nothing")
	require.Equal(t, 1, h.deployMintCount(), "the fix work is filed, not swallowed by a failed workflow")
	require.ElementsMatch(t, []string{"web-a", "web-b"}, delivery.TargetNames(h.deployMints[0].Failed),
		"a cycle is nobody's individual fault — every component in it is named")
	require.Contains(t, h.deployMints[0].Reasons["web-a"], "hard dependency cycle",
		"the cause reaches the issue body rather than only the workflow history")
}

// A permanent plan failure must always NAME somebody, because the fix issue is
// what stops the next boundary settling a version that is not running.
//
// The reachable hole this closes: a pass is entered when anything is behind OR
// converging, so a version whose components are all converging can hit an
// unsatisfiable order with an empty behind set. Attributed to the behind set
// alone, the failure would name nobody, mint nothing, and settle the run on the
// deploy budget with an empty milestone behind it — the exact wedge the stage
// exists to stop producing.
func TestDeployOrderPermanentlyUnsatisfiable_NamesEveryNonServingComponent(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	// Nothing behind: both components are converging towards the release they
	// already pin, which is what makes the behind set empty.
	h.versionIs(
		delivery.ComponentState{
			Component: "todo-api", State: delivery.ComponentStateConverging,
			DesiredSHA: testMergeSHA, Pinned: "release-todo-api",
		},
		delivery.ComponentState{
			Component: "todo-webapp", State: delivery.ComponentStateConverging,
			DesiredSHA: testMergeSHA, Pinned: "release-todo-webapp",
		},
	)
	h.wavesAre(nil, temporal.NewNonRetryableApplicationError(
		"deployment: hard dependency cycle among components todo-api needs [todo-webapp]; todo-webapp needs [todo-api]",
		errTypePermanentDeploy, nil))
	h.deployMintsAre([]int{testRepairIssue})
	h.merges(1)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonDeployBudget)
	require.Equal(t, 1, h.deployMintCount(), "a failure that names nobody files nothing and wedges the run")
	require.ElementsMatch(t, []string{"todo-api", "todo-webapp"},
		delivery.TargetNames(h.deployMints[0].Failed),
		"with nothing behind, the attribution falls back to every component that is not serving")
}

// A plan that could not be READ is a blip, not an answer, and must keep the
// unbounded retry that is right for it — the opposite of the case above.
func TestDeployOrderUnreadable_IsRetriedNotSettled(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.buildsAre(CycleBuildState{Expected: 1, Settled: 1, Components: []string{"order-service"}})
	h.wavesAre(nil, errors.New("oc: design read timed out"))
	h.merges(1)

	h.run(delivery.RunKindDev, 0)

	require.True(t, h.env.IsWorkflowCompleted())
	require.Error(t, h.env.GetWorkflowError(), "a transient planning failure must not settle the run")
	require.Zero(t, h.deployMintCount(), "no fix work is filed for a blip")
}

// The single-wave shape of the same rule: a set that never came up has nothing
// to converge onto, and the failure is already the cycle's answer.
func TestDeploy_FailedWaveRunsNoConverge(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.deploymentsAre(CycleDeployState{Expected: 1, Failed: []string{"order-service"}})
	h.deployMintsAre(nil)
	h.merges(1)

	h.run(delivery.RunKindDev, 0)
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
		workable(1, 1),      // dispatch
		workable(1, 1),      // the deploy-fix issue
		MilestoneSnapshot{}, // delivered
	)
	h.deploymentsAre(
		CycleDeployState{Expected: 1, Failed: []string{"order-service"}, Reasons: map[string]string{"order-service": "RenderingFailed"}},
		CycleDeployState{Expected: 1, Ready: 1},
	)
	h.merges(2)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []string{delivery.CycleKindCoding, delivery.CycleKindFix}, h.dispatchKinds(),
		"a failed deployment recovers through an ordinary fix cycle")
	require.Equal(t, 1, h.deployMintCount(), "the supervisor files the deploy-fix work itself")
	require.Equal(t, []string{"order-service"}, delivery.TargetNames(h.deployMints[0].Failed))
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
		workable(1, 1),      // dispatch
		MilestoneSnapshot{}, // nothing came back to fix it
	)
	h.deploymentsAre(CycleDeployState{Expected: 1, Failed: []string{"order-service"}})
	h.deployMintsAre(nil)
	h.merges(1)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonDeployBudget)
}

// TestValidationRun_BuildsAndDeploysNothing: a validation run's pull request
// carries tests and a report, so the path diff yields no components. Both later
// stages were already silent no-ops for it, and the workflow now SKIPS them
// outright — which is the honest form of what was already true, and removes two
// stages' worth of failure modes from a run that could never reach them.
func TestValidationRun_BuildsAndDeploysNothing(t *testing.T) {
	h := newHarness(t)
	h.validationIs(77, delivery.ValidationVerdictPassed)
	h.merges(1)

	h.run(delivery.RunKindValidation, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, 0, h.deployCount(), "a validation run promotes nothing")
	h.env.AssertNotCalled(t, "PollCycleBuilds", mock.Anything, mock.Anything)
	h.env.AssertNotCalled(t, "PlanDeployWaves", mock.Anything, mock.Anything)
	h.env.AssertNotCalled(t, "PollDeployments", mock.Anything, mock.Anything)
	// And it never polls a working set: it has none, which is why it does not
	// share the cycle-boundary loop at all.
	h.env.AssertNotCalled(t, "PollMilestone", mock.Anything, mock.Anything)
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
		workable(1, 1),      // dispatch
		MilestoneSnapshot{}, // nothing recovered it
	)
	// Neither Ready nor Failed, ever: the rollout that never lands. Only the
	// deadline can end this — which is the whole point of having one here.
	h.deploymentsAre(CycleDeployState{Expected: 1, Pending: []string{"order-service"}})
	h.deployMintsAre(nil)
	h.merges(1)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonDeployBudget)
	require.Positive(t, h.deployMintCount(),
		"the expiry files fix work rather than hanging the run")
	require.Equal(t, []string{"order-service"}, delivery.TargetNames(h.deployMints[0].Failed),
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
		workable(1, 1),
		MilestoneSnapshot{},
	)
	h.buildsAre(CycleBuildState{Expected: 2, Settled: 2, Components: []string{"api", "web"}})
	h.wavesAre([][]string{{"api"}, {"web"}}, nil)
	// The FIRST wave never lands, so the stage can only end on the deadline.
	h.deploymentsAre(CycleDeployState{Expected: 1, Pending: []string{"api"}})
	h.deployMintsAre(nil)
	h.merges(1)

	start := h.env.Now()
	h.run(delivery.RunKindDev, 0)
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
		workable(2, 2),
		workable(1, 1), // the conflict issue
		MilestoneSnapshot{},
	)
	h.signal(delivery.SigRunConflict, time.Second)
	h.signal(delivery.SigRunPRMerged, 2*time.Second)

	h.run(delivery.RunKindTask, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []string{delivery.CycleKindCoding, delivery.CycleKindConflict}, h.dispatchKinds())
	require.Equal(t, "", h.finishes[0].MergeSHA, "a conflicted cycle landed nothing")
	require.Equal(t, testMergeSHA, h.finishes[1].MergeSHA)
}

// ---- the dev run's hand-off ------------------------------------------------

// TestDevRun_MintsTheValidationTaskAtDeployedGreen is the split's whole point on
// the dev side: the version is delivered, the validation TASK is filed, and the
// run settles without ever asking the criteria.
//
// The verdict it leaves is EMPTY, and that is deliberate — "delivered, not yet
// judged". The validation run the sweep starts off this task owns the version's
// answer, and the read model reads the newest VALIDATING run on the milestone for
// exactly that reason.
//
// And the MILESTONE STAYS OPEN. That is the hand-off, not bookkeeping: the
// validation agent finds its work with `gh issue list --milestone`, which
// resolves by title and sees only OPEN milestones, so a dev run that closed the
// milestone over the task it had just minted would leave that task undiscoverable
// by the only agent meant to work it.
func TestDevRun_MintsTheValidationTaskAtDeployedGreen(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.validationIs(77, delivery.ValidationVerdictPassed) // the verdict is never read
	h.merges(1)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []string{delivery.CycleKindCoding}, h.dispatchKinds(),
		"a dev run never dispatches a validation cycle")
	require.Equal(t, 0, h.closed,
		"the version is deployed and UNJUDGED — closing its milestone hides the validation task from the agent that must work it")
	require.Empty(t, res.ValidationVerdict,
		"an empty verdict is 'awaiting judgement'; the validation run records the answer")
	h.env.AssertNotCalled(t, "ReadValidationVerdict", mock.Anything, mock.Anything)
	require.Equal(t, 0, h.taskCloseCount(),
		"the dev run FILES the task; closing it belongs to the run that works it")
}

// A project with no acceptance oracle gets no validation task, so nothing will
// ever judge that version — and an empty verdict would read as "any moment now"
// forever. `skipped` says what is true, and it belongs to no cycle.
//
// This is also the ONE dev ending that closes the milestone, and for the reason
// the hand-off case does not: with no task filed, nothing is coming. Leaving the
// milestone open would strand the version between "unfinished" and "unjudged"
// with nothing able to move it either way.
func TestDevRun_NoAcceptanceOracleRecordsSkipped(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.validationIs(0, delivery.ValidationVerdictSkipped) // EnsureValidationIssue answers 0
	h.merges(1)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, delivery.ValidationVerdictSkipped, res.ValidationVerdict)
	require.Equal(t, 1, h.closed,
		"nothing will ever judge this version, so the milestone has nothing left to wait for")
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Len(t, h.verdictWrites, 1)
	require.Empty(t, h.verdictWrites[0].CycleID,
		"skipped belongs to no cycle — none was opened, and the cycle write is write-once")
}

// A TASK run judges nothing and claims nothing. It must not stamp a verdict on
// its own row either: the read model picks the newest VALIDATING run on the
// milestone, and a task run writing `skipped` used to be why a single adopted
// issue made a genuinely passed version read as unvalidated.
func TestTaskRun_JudgesNothingAndRecordsNoVerdict(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.merges(1)

	h.run(delivery.RunKindTask, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []string{delivery.CycleKindCoding}, h.dispatchKinds())
	require.Empty(t, res.ValidationVerdict)
	h.env.AssertNotCalled(t, "EnsureValidationIssue", mock.Anything, mock.Anything)
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Empty(t, h.verdictWrites, "a task run writes no verdict at all")
}

// ---- the task run closes the chain -----------------------------------------

// TestTaskRun_AVerdictSourcedFixReopensTheValidationTask is the edge that closes
// the repair chain.
//
// Without it the chain is a dead end: a failed validation files one bug per
// failed criterion and closes the task, so the bugs get fixed, built and deployed
// while the version's verdict stands at the failure until a human clicks
// revalidate. The fix run reopens the task, the reconcile sweep starts a
// validation run off it, and the SAME oracle judges the repair.
func TestTaskRun_AVerdictSourcedFixReopensTheValidationTask(t *testing.T) {
	h := newHarness(t)
	// One open bug carrying `src/validation` — a failed verdict's repair work —
	// then a milestone drained by working it.
	h.milestoneIs(repairing(1, 1), MilestoneSnapshot{})
	h.validationIs(77, delivery.ValidationVerdictFailed) // the verdict is never read here
	h.merges(1)

	h.run(delivery.RunKindTask, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	h.env.AssertCalled(t, "EnsureValidationIssue", mock.Anything, mock.Anything)
	// The reopen is not a judgement. The task run reaches no verdict, so its row
	// records none — the validation run that works the reopened task owns the
	// version's answer.
	require.Empty(t, res.ValidationVerdict)
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Empty(t, h.verdictWrites, "a task run writes no verdict, not even when it reopens the task")
}

// An INCIDENT fix does not reopen it, and neither does a user's bug fix. An
// incident is not priced like a release: re-judging the whole system for one
// defect would spend a validation agent per bug fix, and the standing verdict is
// a statement about a VERSION rather than about a commit — so `v3 passed` may
// describe code that shipped after the verdict was recorded.
//
// This is also the only conditional in the platform where a `src/*` label routes
// anything. Everywhere else a source is provenance.
func TestTaskRun_AnIncidentFixLeavesTheVerdictStanding(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{}) // no src/validation work
	h.validationIs(77, delivery.ValidationVerdictFailed)
	h.merges(1)

	h.run(delivery.RunKindTask, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	h.env.AssertNotCalled(t, "EnsureValidationIssue", mock.Anything, mock.Anything)
}

// The attribution LATCHES, and it has to. By the time the working set is empty
// the repair issues are closed, so the poll that finds the milestone drained can
// no longer see what the run just fixed — a settle-time read would always answer
// "no verdict work here" and the chain would never close.
//
// (The alternative, asking whether a CLOSED `src/validation` issue exists, is
// true forever after the first repair: it would reopen the task after every later
// run, which validation then closes, without end.)
func TestTaskRun_TheVerdictSourceIsRememberedFromTheDispatchingPoll(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		// A repair bug and an ordinary one. Only the first poll can see that either
		// of them came from a verdict.
		MilestoneSnapshot{DevWork: 2, TaskWork: 2, Total: 2, ValidationRepairs: 1},
		// The repair is fixed and closed; the ordinary bug is still there.
		MilestoneSnapshot{DevWork: 1, TaskWork: 1, Total: 1},
		MilestoneSnapshot{}, // drained: nothing left to see
	)
	h.validationIs(77, delivery.ValidationVerdictFailed)
	h.merges(2)

	h.run(delivery.RunKindTask, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	h.env.AssertCalled(t, "EnsureValidationIssue", mock.Anything, mock.Anything)
}

// TestTaskRun_NeverPicksUpAFailedDevRunsPlannedWork is the working-set narrowing,
// stated as the failure it prevents.
//
// A dev run that exhausts a budget settles `failed` with its planned work still
// OPEN. Planned work is dev-workflow's alone: the dev run owns the version and
// holds the project's build mutex, so those issues must wait for another build —
// never be continued by a run that never planned them, works the DEPLOYED version
// instead of the one being built, and carries different budgets.
func TestTaskRun_NeverPicksUpAFailedDevRunsPlannedWork(t *testing.T) {
	// The milestone a dev run left behind when it gave up: planned work still
	// open, counted in the dev working set and in no task run's.
	leftover := MilestoneSnapshot{DevWork: 2, TaskWork: 0, Total: 2}

	t.Run("a task run dispatches nothing at it", func(t *testing.T) {
		h := newHarness(t)
		h.milestoneIs(leftover)
		// Its working set is empty and it planned no milestone, so it parks — and
		// cancel is the unbounded wait's only expiry.
		h.signal(delivery.SigRunCancel, 2*time.Second)

		h.run(delivery.RunKindTask, 0)
		res := h.result(t)

		h.assertSettled(t, res, delivery.RunStateCancelled, "")
		require.Equal(t, 0, h.dispatchCount(),
			"planned work belongs to the build that planned it; a bug-fix run must not spend a dispatch on it")
	})

	t.Run("the build that planned it does", func(t *testing.T) {
		h := newHarness(t)
		h.milestoneIs(leftover, MilestoneSnapshot{})
		h.merges(1)

		h.run(delivery.RunKindDev, 0)
		res := h.result(t)

		h.assertSettled(t, res, delivery.RunStateSucceeded, "")
		require.Equal(t, 1, h.dispatchCount(), "the same milestone IS a dev run's work")
	})
}

// ---- halting what a failed run could not finish ----------------------------

// TestFailedSettle_HaltsTheWorkItCouldNotFinish is what keeps every budget in the
// platform meaning something.
//
// A failed run leaves its working set OPEN — the milestone stays open too,
// because the way forward is more work in the same version — and the reconcile
// sweep's trigger is "open work of a species on a milestone with no live run". So
// without the halt the run that just exhausted its deploy budget is replaced
// within a tick by a fresh run with a fresh budget, on the same issues, forever.
// The symptom is an unexplained cloud bill rather than a failing test.
//
// The halt names the run's KIND, because the working set is per species and the
// mark must not reach outside the population this run was responsible for.
func TestFailedSettle_HaltsTheWorkItCouldNotFinish(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		workable(1, 1),      // dispatch
		MilestoneSnapshot{}, // the deploy failed and nothing came back to fix it
	)
	h.deploymentsAre(CycleDeployState{Expected: 1, Failed: []string{"order-service"}})
	h.merges(1)

	h.runWith(RunInput{Kind: delivery.RunKindDev, Tag: "v3"})
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonDeployBudget)
	halts := h.haltedWork()
	require.Len(t, halts, 1, "a failed settle halts the work it could not finish, exactly once")
	require.Equal(t, delivery.RunKindDev, halts[0].Kind)
	require.Equal(t, delivery.RunReasonDeployBudget, halts[0].Reason,
		"the terminal reason is quoted onto the issues, so a human reads why without opening the run")
	require.Equal(t, testMilepost, halts[0].MilestoneNumber)
	// The deploy-fix issue this very run filed is inside that milestone's working
	// set, which is the point: it is the newest thing there and therefore the first
	// thing a restarted run would pick up.
	require.Equal(t, 1, h.deployMintCount())
}

// A SUCCEEDED run halts nothing. There is nothing left to halt — an empty working
// set is what settled it — and marking anything here would put `aep:halted` on
// work somebody filed in the instant the version was delivered.
func TestSucceededSettle_HaltsNothing(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.merges(1)

	h.runWith(RunInput{Kind: delivery.RunKindDev, Tag: "v3"})
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Empty(t, h.haltedWork())
}

// A CANCELLED run halts nothing either, and the reason is a different one: cancel
// has its own vocabulary and its own way forward. It CLOSES the work it had in
// flight and stamps `aep:cancelled` on it, which is the rebuild's handle on what
// was in flight; stamping `aep:halted` over that would say the sweep must leave
// alone what the rebuild is about to reopen.
func TestCancelledSettle_HaltsNothing(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1))
	h.signal(delivery.SigRunCancel, time.Second)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateCancelled, "")
	require.Empty(t, h.haltedWork())
}

// A VALIDATION run's failure halts nothing, and that is a decision. Its own work
// is the version's validation task, which it closes on EVERY ending; the repair
// issues it files and the conflict issue a stuck validation pull request produces
// are deliberately an ordinary task run's work. Halting those would break the
// repair chain rather than protect a budget.
func TestValidationRun_FailureHaltsNoWork(t *testing.T) {
	h := newHarness(t)
	h.validationIs(77, delivery.ValidationVerdictFailed)
	h.validationHistoryIs(1, "")
	h.merges(1)

	h.runWith(RunInput{Kind: delivery.RunKindValidation, ValidationAttempts: 2})
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonValidationFailed)
	require.Equal(t, 1, h.repairMintCount(), "the failure became work for an ordinary run")
	require.Empty(t, h.haltedWork(), "and that work must not be halted the moment it is filed")
}

// ---- the validation run ----------------------------------------------------

// TestValidationRun_Passes is the workflow's happy path: adopt the version's
// task, dispatch one agent stage anchored at it, read the verdict at that cycle's
// own merge commit, close the task, settle.
func TestValidationRun_Passes(t *testing.T) {
	h := newHarness(t)
	h.validationIs(77, delivery.ValidationVerdictPassed)
	h.merges(1)

	h.run(delivery.RunKindValidation, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []string{delivery.CycleKindValidation}, h.dispatchKinds())
	require.Equal(t, delivery.ValidationVerdictPassed, res.ValidationVerdict)

	h.mu.Lock()
	defer h.mu.Unlock()
	require.Equal(t, 77, h.dispatches[0].IssueNumber,
		"the validation dispatch is anchored to one issue, not to a working set")
	require.Equal(t, delivery.CycleKindValidation, h.dispatches[0].Kind,
		"AEP_TASK_KIND=validation rides the dispatch kind")
	require.Len(t, h.taskCloses, 1, "the platform closes the task it adopted")
	require.Equal(t, 77, h.taskCloses[0].Issue)
	require.Equal(t, delivery.ValidationVerdictPassed, h.taskCloses[0].Verdict)
	// The GREEN ENDING is where the version's milestone closes — zero open
	// working-set issues and a terminal verdict on the newest validation run. A
	// succeeded validation run is a green ending by construction: every fatal
	// verdict settles the run `failed`.
	require.Equal(t, 1, h.closed, "a judged version is finished, so its milestone closes")
}

// TestValidationRun_FailedFilesOneIssuePerCriterion is the repair hand-off. The
// failure becomes ORDINARY WORK in the milestone — one issue per failed criterion
// — and the run then settles on the verdict it reached.
//
// One per criterion and never one omnibus issue: the no-progress rule compares
// working-set SIZES, so repairing two of three failures has to read as progress.
func TestValidationRun_FailedFilesOneIssuePerCriterion(t *testing.T) {
	h := newHarness(t)
	h.validationIs(77, delivery.ValidationVerdictFailed)
	h.repairMintsAre([]int{testRepairIssue, testRepairIssue + 1})
	h.merges(1)

	h.run(delivery.RunKindValidation, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonValidationFailed)
	require.Equal(t, delivery.ValidationVerdictFailed, res.ValidationVerdict)
	require.Equal(t, 0, h.closed, "a version that failed its criteria keeps its milestone open")

	h.mu.Lock()
	defer h.mu.Unlock()
	require.Len(t, h.repairMints, 1, "the mint is asked once")
	require.Equal(t, testMergeSHA, h.repairMints[0].At,
		"repair issues come from the report at the attempt's OWN merge commit")
	require.Equal(t, testCycleID, h.repairMints[0].CycleID,
		"THIS attempt's cycle id is the issues' dedupe key, so the next attempt files fresh work")
	require.Len(t, h.taskCloses, 1, "the task closes even on a failing verdict")
}

// TestValidationRun_UnreportedRedispatchesInsideTheWorkflow covers the one
// failure this workflow can remedy itself.
//
// An agent that merged a pull request and committed no report has not broken the
// software: no criterion asserted, nothing was deployed, and no issue anybody
// could file would change the answer. Another dispatch is the WHOLE remedy, so it
// happens inside this workflow — settling and hoping something restarts the run
// would be hoping for the sweep, which cannot see a closed task.
func TestValidationRun_UnreportedRedispatchesInsideTheWorkflow(t *testing.T) {
	h := newHarness(t)
	h.validationAttemptsAre(77,
		ValidationOutcome{Verdict: delivery.ValidationVerdictUnreported},
		ValidationOutcome{Verdict: delivery.ValidationVerdictPassed, Digest: "green"},
	)
	h.merges(2)

	h.run(delivery.RunKindValidation, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, []string{delivery.CycleKindValidation, delivery.CycleKindValidation},
		h.dispatchKinds(), "two validation cycles inside ONE run")
	require.Equal(t, 0, h.repairMintCount(),
		"nothing is wrong with the software, so nothing is filed")
	require.Equal(t, 1, h.taskCloseCount(), "one run, one close")
}

// The same remedy, bounded. An agent that ignored the report contract through the
// whole allowance will ignore it again, and every dispatch is a paid agent run.
// The reason is its own class: "the suite went red" and "nothing was reported"
// are different explanations, and a terminal reason exists to explain.
func TestValidationRun_UnreportedThroughTheBudgetFails(t *testing.T) {
	h := newHarness(t)
	h.validationIs(77, delivery.ValidationVerdictUnreported)
	h.merges(maxUnreportedDispatches)

	h.run(delivery.RunKindValidation, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonValidationUnreported)
	require.Equal(t, maxUnreportedDispatches, h.dispatchCount(),
		"the re-dispatch allowance is spent, and not exceeded")
	require.Equal(t, 0, h.repairMintCount())
	require.Equal(t, 1, h.taskCloseCount())
}

// TestValidationRun_LastAttemptFilesNoRepairWork: the allowance is per VERSION and
// is spent by the milestone's validation runs, so an attempt that is the last one
// settles BEFORE the mint.
//
// That ordering is what makes a one-attempt allowance a pure re-check, and why the
// revalidate endpoint needs no separate "should it fix things" switch.
func TestValidationRun_LastAttemptFilesNoRepairWork(t *testing.T) {
	for _, c := range []struct {
		name     string
		attempts int
		allow    int
	}{
		{"a one-attempt allowance is spent by the first fatal verdict", 1, 1},
		{"the version's default allowance, already spent", delivery.RunMaxValidationAttempts, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			h.validationIs(77, delivery.ValidationVerdictFailed)
			h.validationHistoryIs(c.attempts, "")
			h.merges(1)

			h.runWith(RunInput{Kind: delivery.RunKindValidation, ValidationAttempts: c.allow})
			res := h.result(t)

			h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonValidationFailed)
			require.Equal(t, delivery.ValidationVerdictFailed, res.ValidationVerdict,
				"the answer is recorded even though nothing was repaired")
			require.Equal(t, 0, h.repairMintCount(),
				"the allowance is spent, so the mint is never reached")
			require.Equal(t, 1, h.taskCloseCount())
		})
	}
}

// An attempt with allowance LEFT files the repair work. The contrast with the
// test above is the whole point: same verdict, same harness, one fewer attempt
// already spent.
func TestValidationRun_AttemptsRemainingFileRepairWork(t *testing.T) {
	h := newHarness(t)
	h.validationIs(77, delivery.ValidationVerdictFailed)
	h.validationHistoryIs(1, "") // first of the default two
	h.merges(1)

	h.run(delivery.RunKindValidation, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonValidationFailed)
	require.Equal(t, 1, h.repairMintCount(), "attempts remain, so the failure becomes work")
	require.Equal(t, 2, delivery.RunMaxValidationAttempts,
		"this test's shape assumes the allowance it is proving")
}

// TestValidationRun_IdenticalDigestStopsTheChain is the digest guard, and it needs
// its own test because it is the ONLY thing between a repeat attempt and a
// deployment that never picked up the repair.
//
// Two consecutive reports that agree on every criterion, every status and every
// failure message learned the same nothing: the repair did not move the system, so
// another round could only produce that report a third time. The chain stops even
// though the allowance is NOT spent — which is what makes it a guard rather than a
// budget.
//
// The comparison spans runs (each attempt is its own run), which is why the
// previous digest is READ from the ledger rather than carried.
func TestValidationRun_IdenticalDigestStopsTheChain(t *testing.T) {
	h := newHarness(t)
	h.validationAttemptsAre(77, ValidationOutcome{
		Verdict: delivery.ValidationVerdictFailed, Digest: "unchanged",
	})
	// Attempt 2 of a default allowance of 2 would file repair work on its own
	// merits — the digest is the only thing stopping it. Pin attempt 1 of a wider
	// allowance so the guard is unambiguously what fired.
	h.validationHistoryIs(1, "unchanged")
	h.merges(1)

	h.runWith(RunInput{Kind: delivery.RunKindValidation, ValidationAttempts: 5})
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonValidationFailed)
	require.Equal(t, 0, h.repairMintCount(),
		"the repair moved nothing, so no further work is filed")
	require.Equal(t, 1, h.taskCloseCount())
}

// The other half of the guard: a digest that MOVED is a repair that changed
// something, so the chain carries on and files the next round of work. Without
// this the guard could be satisfied by never firing at all.
func TestValidationRun_MovedDigestKeepsTheChainGoing(t *testing.T) {
	h := newHarness(t)
	h.validationAttemptsAre(77, ValidationOutcome{
		Verdict: delivery.ValidationVerdictFailed, Digest: "red-2",
	})
	h.validationHistoryIs(1, "red-1")
	h.merges(1)

	h.runWith(RunInput{Kind: delivery.RunKindValidation, ValidationAttempts: 5})
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonValidationFailed)
	require.Equal(t, 1, h.repairMintCount(), "the answer changed, so the chain files more work")
}

// A `failed` attempt whose report names failures the minter cannot turn into work
// still settles on its verdict — the mint is asked, and an empty answer changes
// nothing about what the version was judged to be.
func TestValidationRun_FailedWithNothingToRepairStillSettles(t *testing.T) {
	h := newHarness(t)
	h.validationIs(77, delivery.ValidationVerdictFailed)
	h.repairMintsAre(nil)
	h.validationHistoryIs(1, "")
	h.merges(1)

	h.run(delivery.RunKindValidation, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonValidationFailed)
	require.Equal(t, 1, h.repairMintCount(), "it tried")
	require.Equal(t, 1, h.taskCloseCount())
}

// TestValidationRun_IncompleteEvidenceStillSucceeds pins the pair that must NOT
// fail a run. Both are honest reports about the test harness rather than evidence
// the increment is broken:
//
//   - partial: something passed, nothing failed, and some criteria were never
//     covered — which is why it is not reported as `passed`;
//   - inconclusive: no test results at all.
func TestValidationRun_IncompleteEvidenceStillSucceeds(t *testing.T) {
	for _, verdict := range []string{
		delivery.ValidationVerdictPartial,
		delivery.ValidationVerdictInconclusive,
	} {
		t.Run(verdict, func(t *testing.T) {
			h := newHarness(t)
			h.validationIs(77, verdict)
			h.merges(1)

			h.run(delivery.RunKindValidation, 0)
			res := h.result(t)

			h.assertSettled(t, res, delivery.RunStateSucceeded, "")
			require.Equal(t, verdict, res.ValidationVerdict)
			require.Equal(t, 1, h.taskCloseCount())
		})
	}
}

// The verdict, its DIGEST and the issue that produced it are ONE write, against
// the attempt's own cycle.
//
// The digest has to ride that write and not a later one: the cycle write is fenced
// write-once on an empty verdict, so a digest recorded afterwards could never land
// on the cycle it belongs to — and the next attempt would have nothing to compare
// against, silently disabling the guard above.
func TestValidationRun_WritesVerdictDigestAndIssueTogether(t *testing.T) {
	h := newHarness(t)
	h.validationAttemptsAre(77, ValidationOutcome{
		Verdict: delivery.ValidationVerdictPassed, Digest: "green-1",
	})
	h.merges(1)

	h.run(delivery.RunKindValidation, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Len(t, h.verdictWrites, 1, "the verdict is written once")
	w := h.verdictWrites[0]
	require.Equal(t, delivery.ValidationVerdictPassed, w.Verdict)
	require.Equal(t, "green-1", w.Digest, "the digest rides the verdict's own write")
	require.Equal(t, 77, w.Issue, "the issue is persisted, not left in workflow state")
	require.Equal(t, testCycleID, w.CycleID,
		"the verdict is written against the attempt's own cycle, not just the run")
}

// TestValidationRun_NoAcceptanceOracleSettlesSkipped: EnsureValidationIssue
// answers 0, so there is nothing to judge. That is itself a verdict, it belongs
// to no cycle, and there is no task to close — a run that adopted nothing leaves
// the version exactly as it found it.
func TestValidationRun_NoAcceptanceOracleSettlesSkipped(t *testing.T) {
	h := newHarness(t)
	h.validationIs(0, delivery.ValidationVerdictSkipped)

	h.run(delivery.RunKindValidation, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, delivery.ValidationVerdictSkipped, res.ValidationVerdict)
	require.Equal(t, 0, h.dispatchCount(), "nothing was dispatched")
	require.Equal(t, 0, h.taskCloseCount(), "there was no task to close")
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Empty(t, h.verdictWrites[0].CycleID, "skipped belongs to no cycle")
}

// TestValidationRun_AgentDeathStillClosesTheTask is THE INFINITE-LOOP GUARD, and
// the reason the close is unconditional.
//
// The reconcile sweep starts a validation run BECAUSE an open `validation`-kind
// issue exists. An agent that dies through the whole re-dispatch budget produces
// no verdict and nothing outside the workflow can repair it — so a run that gave
// up and left the task open would be restarted within a tick, give up again, and
// keep doing that forever, paying for two agent dispatches each time.
//
// Closing it leaves the version deployed and UNJUDGED, which is honest: no verdict
// was reached and none is claimed, and a person re-triggers.
func TestValidationRun_AgentDeathStillClosesTheTask(t *testing.T) {
	h := newHarness(t)
	h.validationIs(77, delivery.ValidationVerdictPassed) // never read — nothing lands
	h.mergesAt("")                                       // the cycle record never learns a merge

	h.run(delivery.RunKindValidation, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonRedispatchBudget)
	require.Equal(t, delivery.RunMaxRedispatchPerCycle, h.dispatchCount())
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Len(t, h.taskCloses, 1,
		"the task must close on an ending with no verdict, or the sweep restarts this run forever")
	require.Empty(t, h.taskCloses[0].Verdict,
		"the close says no verdict was reached rather than inventing one")
	require.Empty(t, h.verdictWrites, "no verdict is claimed for an attempt that produced none")
}

// TestValidationRun_ClosesTheTaskOnEveryEnding generalises the guard above across
// every way this workflow can end after adopting the task. Any ending that leaves
// it open is an ending the sweep repeats.
func TestValidationRun_ClosesTheTaskOnEveryEnding(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(h *harness)
		state   string
		reason  string
	}{
		{
			name:    "a verdict was reached",
			arrange: func(h *harness) { h.validationIs(77, delivery.ValidationVerdictPassed); h.merges(1) },
			state:   delivery.RunStateSucceeded,
		},
		{
			name: "the agent died through its whole budget",
			arrange: func(h *harness) {
				h.validationIs(77, delivery.ValidationVerdictPassed)
				h.mergesAt("")
			},
			state:  delivery.RunStateFailed,
			reason: delivery.RunReasonRedispatchBudget,
		},
		{
			name: "the org has no agent slot",
			arrange: func(h *harness) {
				h.validationIs(77, delivery.ValidationVerdictPassed)
				h.dispatchIs("", temporal.NewNonRetryableApplicationError(
					delivery.AgentQuotaBlockedMessage, delivery.ErrTypeAgentQuotaBlocked,
					delivery.ErrAgentQuotaExceeded))
			},
			state:  delivery.RunStateBlocked,
			reason: delivery.RunReasonAgentQuotaBlocked,
		},
		{
			name: "the pull request would not merge",
			arrange: func(h *harness) {
				h.validationIs(77, delivery.ValidationVerdictPassed)
				h.signal(delivery.SigRunConflict, time.Second)
			},
			state:  delivery.RunStateFailed,
			reason: delivery.RunReasonConflictBudget,
		},
		{
			name: "a human cancelled it mid-cycle",
			arrange: func(h *harness) {
				h.validationIs(77, delivery.ValidationVerdictPassed)
				h.factsAre(CycleFacts{}, CycleFacts{CycleID: testCycleID, CancelRequested: true})
				h.signal(delivery.SigRunPRMerged, time.Second)
			},
			state: delivery.RunStateCancelled,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			c.arrange(h)
			h.run(delivery.RunKindValidation, 0)
			res := h.result(t)

			h.assertSettled(t, res, c.state, c.reason)
			require.Equal(t, 1, h.taskCloseCount(),
				"every ending after adopting the task must close it")
		})
	}
}

// A cancel that arrives BEFORE the task is adopted leaves it open, and that is the
// one exception the design wants: cancelling a validation leaves the task for the
// next trigger, because the way forward from a cancelled validation is to validate
// again.
func TestValidationRun_CancelBeforeAdoptionLeavesTheTaskOpen(t *testing.T) {
	h := newHarness(t)
	h.validationIs(77, delivery.ValidationVerdictPassed)
	h.factsAre(CycleFacts{CancelRequested: true})

	h.run(delivery.RunKindValidation, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateCancelled, "")
	require.Equal(t, 0, h.dispatchCount())
	require.Equal(t, 0, h.taskCloseCount(), "nothing had been adopted, so nothing is closed")
	h.env.AssertNotCalled(t, "EnsureValidationIssue", mock.Anything, mock.Anything)
}

// TestValidationRun_AnchorsAtTheAdoptedIssue: an issue already OPEN is ADOPTED as
// this attempt's, never re-filed. The task is the version's persistent handle —
// its body embeds the oracle as it stood at mint time — so a second one would
// split the thread the attempts comment on and double what the sweep sees as
// unworked.
func TestValidationRun_AnchorsAtTheAdoptedIssue(t *testing.T) {
	h := newHarness(t)
	h.validationIs(77, delivery.ValidationVerdictPassed)
	h.merges(1)

	h.run(delivery.RunKindValidation, 0)
	_ = h.result(t)

	h.mu.Lock()
	defer h.mu.Unlock()
	require.Equal(t, 77, h.dispatches[0].IssueNumber)
	require.Equal(t, 77, h.taskCloses[0].Issue,
		"the run closes the issue it adopted, not one it invented")
}

// TestRunInput_WithoutAKindFallsBackToItsOrigin is the REPLAY case, and the one
// that cannot be backfilled.
//
// A workflow input lives in Temporal history: an execution started before the
// kind field existed replays with the empty string, forever. Kind now selects the
// workflow TYPE, so the fallback's remaining job is inside the loop — above all
// plansItsOwnMilestone, which decides whether the run fills its own milestone and
// whether it may read an empty working set as "delivered". A dev run replaying as
// "not dev" would skip planning and then park forever waiting for work nobody was
// going to file.
func TestRunInput_WithoutAKindFallsBackToItsOrigin(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{}) // planning mints nothing → delivered

	h.runWith(RunInput{Origin: delivery.RunOriginSpecBuild, Tag: "v3"}) // no Kind
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	gates, plans := h.planCounts()
	require.Equal(t, 1, gates, "a replayed spec build must still fill its own milestone")
	require.Equal(t, 1, plans)
}

// TestRedispatchBudget_AgentDeathEndsTheRun: the dispatch never lands a pull
// request, so the cycle spends its whole per-cycle allowance and the run fails
// naming that budget. Nothing here needs a real two hours — the environment
// fast-forwards both deadlines.
func TestRedispatchBudget_AgentDeathEndsTheRun(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1))
	h.mergesAt("") // the cycle record never learns a merge

	h.run(delivery.RunKindDev, 0)
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
	h.milestoneIs(workable(1, 1))
	h.dispatchIs("", temporal.NewNonRetryableApplicationError(
		delivery.AgentQuotaBlockedMessage, delivery.ErrTypeAgentQuotaBlocked, delivery.ErrAgentQuotaExceeded))

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateBlocked, delivery.RunReasonAgentQuotaBlocked)
	require.Equal(t, 1, h.dispatchCount(),
		"a quota refusal must not be re-attempted — the answer cannot change without a human")
	require.Equal(t, 0, h.closed, "a blocked increment keeps its milestone open")
	require.True(t, delivery.IsTerminalRunState(delivery.RunStateBlocked),
		"blocked must be terminal, or the build mutex stays armed forever")
}

func TestPublisherCredentialsMissing_SettlesBlockedWithoutSpendingTheBudget(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1))
	h.dispatchIs("", temporal.NewNonRetryableApplicationError(
		delivery.PublisherCredentialsMissingMessage, delivery.ErrTypePublisherCredentialsMissing, delivery.ErrPublisherCredentialsMissing))

	h.run(delivery.RunOriginSpecBuild, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateBlocked, delivery.RunReasonPublisherCredentials)
	require.Equal(t, 1, h.dispatchCount(),
		"a missing publisher SecretReference must not be re-attempted — Job create cannot stamp it")
	require.Equal(t, 0, h.closed, "a blocked increment keeps its milestone open")
}

// TestBuildRetriggerBudget_RedWithNothingToFix is the exit for a build that
// stayed red through its one automatic re-trigger and produced no fix issue:
// the allowance is spent and nothing came back that could make it green.
func TestBuildRetriggerBudget_RedWithNothingToFix(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.buildsAre(CycleBuildState{Expected: 1, Settled: 1, Red: []string{"order-service"}})
	h.merges(1)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonBuildRetriggerBudget)
	require.Equal(t, 1, h.dispatchCount())
}

// TestFixChainBudget_TwoFixCyclesIsTheLimit walks the fix chain to exhaustion.
func TestFixChainBudget_TwoFixCyclesIsTheLimit(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1))
	h.buildsAre(CycleBuildState{Expected: 1, Settled: 1, Red: []string{"order-service"}})
	h.merges(3)

	h.run(delivery.RunKindDev, 0)
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
	h.milestoneIs(workable(1, 1))
	for i := 1; i <= 3; i++ {
		h.signal(delivery.SigRunConflict, time.Duration(i)*time.Second)
	}

	h.run(delivery.RunKindDev, 0)
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
	h.milestoneIs(workable(2, 2))
	h.merges(1)

	h.run(delivery.RunKindDev, 0)
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
		workable(3, 3),
		workable(2, 2),
		workable(1, 1),
	)
	h.merges(2)

	h.run(delivery.RunKindDev, 2)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonCycleCeiling)
	require.Equal(t, 2, h.dispatchCount())
}

// TestCancel_FromWaiting: cancel is the ONLY expiry the unbounded wait has.
// The run is parked behind a gate and never dispatches.
func TestCancel_FromWaiting(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(gated(1, 1, 2))
	h.signal(delivery.SigRunCancel, time.Second)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateCancelled, "")
	require.Equal(t, 0, h.dispatchCount(), "a cancelled wait never dispatched")
	// The increment is abandoned, so the milestone is closed behind it — and its
	// gates go with the rest of the work (see TestCancel_DevClosesTheWholeMilestone).
	require.Equal(t, 1, h.closed, "a cancelled build abandons the increment and closes its milestone")
}

// TestCancel_FromRunning: cancel mid-cycle settles the run and closes the cycle
// with no merge, so the timeline shows a dispatch that was abandoned rather than
// one still in flight.
func TestCancel_FromRunning(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1))
	h.signal(delivery.SigRunCancel, time.Second)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateCancelled, "")
	require.Equal(t, 1, h.dispatchCount())
	require.Equal(t, []FinishCycleInput{{CycleID: testCycleID}}, h.finishes)
}

// ---- what a cancel COSTS, per species ---------------------------------------
//
// Closing the issues is what makes a cancel STICK. The reconcile sweep starts a
// run over a milestone's open WORK when no run is live on it, so a cancel that
// only recorded itself would be undone within a tick — the cancel button would
// stop the run and pay for its replacement a minute later. These three pin what
// each species abandons, because the answer differs and the differences are the
// design.

// A cancelled BUILD abandons the whole increment: every open issue in its
// milestone is closed and stamped, gates included, and the milestone is closed
// behind them.
//
// The gates going is the deliberate asymmetry with the halt. A halted run may be
// retried in the same version, so its gates still name dependencies somebody has
// to resolve; a cancelled one will not be, and the way forward is the spec and
// another build.
func TestCancel_DevAbandonsTheIncrementAndClosesItsMilestone(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1))
	h.signal(delivery.SigRunCancel, time.Second)

	h.runWith(RunInput{Kind: delivery.RunKindDev, Tag: "v3"})
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateCancelled, "")
	cancels := h.cancelledWork()
	require.Len(t, cancels, 1, "a cancelled settle closes the work it had in flight, exactly once")
	require.Equal(t, delivery.RunKindDev, cancels[0].Kind,
		"the kind selects the population, and a build's is the whole milestone")
	require.Equal(t, testMilepost, cancels[0].MilestoneNumber)
	require.Equal(t, 1, h.closedCount(), "the increment is abandoned, so its milestone closes")
	require.Empty(t, h.haltedWork(), "cancel has its own vocabulary; `aep:halted` would contradict it")
}

// A cancelled TASK run abandons only itself. The version it works is the DEPLOYED
// one — it is not being withdrawn — so its milestone stays OPEN and the way
// forward is to reopen the bugs or file new ones.
func TestCancel_TaskLeavesTheDeployedVersionStanding(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1))
	h.signal(delivery.SigRunCancel, time.Second)

	h.run(delivery.RunKindTask, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateCancelled, "")
	cancels := h.cancelledWork()
	require.Len(t, cancels, 1)
	require.Equal(t, delivery.RunKindTask, cancels[0].Kind,
		"a bug-fix run's cancel reaches its own defects and nothing of the version's")
	require.Equal(t, 0, h.closedCount(),
		"a cancelled bug-fix run withdraws no release, so the milestone stays open")
}

// A cancelled VALIDATION run closes ONE thing — the task it adopted — through the
// close every ending performs, and it reaches the milestone not at all.
//
// That narrowing is what keeps TestValidationRun_CancelBeforeAdoptionLeavesTheTaskOpen
// true: the close is scoped to what this run adopted, so a cancel before the first
// read leaves the version's task for the next trigger. Reaching the milestone would
// close it anyway and turn "validate again" into "file the task again".
func TestCancel_ValidationClosesOnlyTheTaskItAdopted(t *testing.T) {
	h := newHarness(t)
	h.validationIs(77, delivery.ValidationVerdictPassed)
	h.factsAre(CycleFacts{}, CycleFacts{CycleID: testCycleID, CancelRequested: true})
	h.signal(delivery.SigRunPRMerged, time.Second)

	h.run(delivery.RunKindValidation, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateCancelled, "")
	require.Equal(t, 1, h.taskCloseCount(), "the adopted task is closed, as on every ending")
	require.Empty(t, h.cancelledWork(),
		"a validation run's cancel must not reach the milestone — the repair work there is a task run's")
	require.Equal(t, 0, h.closedCount(), "the version stands, merely unjudged")
}

// TestMidRunGate_HoldsTheNextDispatch is the human brake: a gate filed while the
// run is live stops the NEXT cycle, and only the next cycle. The assertion that
// matters is inside the callback — at the moment the gate was open, nothing new
// had been dispatched.
func TestMidRunGate_HoldsTheNextDispatch(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		workable(2, 2),      // cycle 1 dispatches
		gated(1, 1, 2),      // a gate appears → hold
		workable(1, 1),      // the gate closed → cycle 2
		MilestoneSnapshot{}, // delivered
	)
	h.merges(1)

	heldAt := -1
	h.env.RegisterDelayedCallback(func() {
		heldAt = h.dispatchCount()
		h.env.SignalWorkflow(delivery.SigRunWorkable, delivery.RunSignal{Signal: delivery.SigRunWorkable})
	}, 2*time.Second)
	h.signal(delivery.SigRunPRMerged, 3*time.Second)

	h.run(delivery.RunKindTask, 0)
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
		workable(1, 1),
		gated(0, 1, 1),
	)
	h.merges(1)

	h.run(delivery.RunKindTask, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, 0, h.closed,
		"a task run fixes one defect inside a version somebody else delivered — it finishes no version")
}

// ---- ground truth and liveness --------------------------------------------

// TestMergeSignalIsNotEvidence pins the rule that a signal is a wake-up, never
// evidence: a merge signal whose cycle record shows no merge (a HUMAN's pull
// request landing during the cycle raises the very same signal) must not end
// the agent's cycle.
func TestMergeSignalIsNotEvidence(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1))
	h.mergesAt("") // ground truth: this cycle landed nothing
	h.merges(3)    // three merge signals arrive anyway

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	// The run ends on the re-dispatch budget, not on a phantom green cycle.
	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonRedispatchBudget)
}

// TestBuildTerminalSignalWakesTheBuildWait exercises the build phase's wait: the
// first poll finds a component still building, the signal wakes the loop, and
// the re-poll settles it.
func TestBuildTerminalSignalWakesTheBuildWait(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.buildsAre(
		CycleBuildState{Expected: 2, Settled: 1},
		CycleBuildState{Expected: 2, Settled: 2},
	)
	h.merges(1)
	h.signal(delivery.SigRunBuildTerminal, 2*time.Second)

	h.run(delivery.RunKindTask, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
}

// TestQueryRunStatus proves the loop reports its POSITION live — the thing no
// database column holds, because fix and conflict cycles re-enter earlier phases
// and a stored phase enum would lie mid-loop.
func TestQueryRunStatus(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(2, 2), MilestoneSnapshot{})

	var midRun delivery.RunStatus
	h.env.RegisterDelayedCallback(func() {
		resp, err := h.env.QueryWorkflow(delivery.QueryRunStatus)
		require.NoError(t, err)
		require.NoError(t, resp.Get(&midRun))
		h.env.SignalWorkflow(delivery.SigRunPRMerged, delivery.RunSignal{Signal: delivery.SigRunPRMerged})
	}, time.Second)

	h.run(delivery.RunKindDev, 0)
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
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.merges(1)

	h.runWith(RunInput{Kind: delivery.RunKindDev, Tag: "v3"})
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

// Only a run that owns a version plans one. Every other kind adopts a
// milestone somebody already filled, and is recognised by carrying no Tag —
// re-planning there would re-derive a version from a run that was only meant to
// resume.
func TestPlanningPhase_SkippedWhenTheRunOwnsNoVersion(t *testing.T) {
	for _, kind := range []string{delivery.RunKindTask, delivery.RunKindValidation} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
			h.merges(1)

			h.runWith(RunInput{Kind: kind}) // no Tag
			h.result(t)

			gates, plans := h.planCounts()
			require.Equal(t, 0, gates, "an adopted milestone is already filled")
			require.Equal(t, 0, plans)
		})
	}
}

// A REBUILD of an UNCHANGED spec mints the gates and does NOT plan.
//
// This is the subtle one. Plan dedupe is the title slug against the milestone's
// issues in ANY state, which is what makes re-planning additive-only and a crash
// re-run a no-op — and the cancel this rebuild is recovering from CLOSED every
// open issue. So a re-plan would recognise every slug, mint NOTHING, and the loop
// would then read the empty working set as "delivered" and settle a version it
// never built. Reopening the marked issues is what restores the working set
// instead, and the click has already done it by the time this run starts.
//
// The GATES still run, and must: they dedupe onto the reopened gate issues, so a
// dependency resolved since the cancel is re-read rather than assumed.
func TestPlanningPhase_ARebuildMintsGatesButDoesNotPlan(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.merges(1)

	h.runWith(RunInput{Kind: delivery.RunKindDev, Tag: "v3", Rebuild: true})
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	gates, plans := h.planCounts()
	require.Equal(t, 1, gates, "the version's gates are re-derived and dedupe onto the reopened ones")
	require.Equal(t, 0, plans, "a re-plan over reopened work would mint nothing and settle an unbuilt version")
	// The reopened work was still worked: skipping the plan is not skipping the run.
	require.Equal(t, []string{delivery.CycleKindCoding}, h.dispatchKinds())
}

// ---- cancel reaches the PLANNING phase --------------------------------------
//
// The cycle loop asks about cancel at every boundary, but the planning bookend
// runs BEFORE the first boundary — so for as long as it lasted, a run was blind
// to a cancel that had already been delivered. It is the phase a person is most
// likely to be looking at when they press the button, and the one where a stuck
// activity can hold a run for hours: a version whose gates could not be authored
// sat re-minting them with six delivered cancel signals unread in the channel.
//
// These four pin the four ways the phase can now hear it.

// The row is asked FIRST, before a single gate is minted. It is the only reading
// that survives a cancel whose SIGNAL never arrived — the cancel surface swallows
// a failed delivery so a dead engine cannot wedge the console — and planning is
// the longest stretch a run has to be wrong about.
func TestPlanningPhase_CancelAlreadyOnTheRowSettlesBeforeMintingGates(t *testing.T) {
	h := newHarness(t)
	h.factsAre(CycleFacts{CancelRequested: true})
	h.milestoneIs(workable(1, 1))

	h.runWith(RunInput{Kind: delivery.RunKindDev, Tag: "v3"})
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateCancelled, "")
	gates, plans := h.planCounts()
	require.Equal(t, 0, gates, "a run cancelled before it started mints no gates")
	require.Equal(t, 0, plans)
	require.Equal(t, 0, h.dispatchCount())
}

// A cancel delivered WHILE the gates are being authored settles the run at once,
// and the planning turn never runs. This is the case the incident was: the gate
// activity holding the phase open, cancel unread behind it.
func TestPlanningPhase_CancelDuringTheGatesSettlesWithoutPlanning(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1))
	// Signalled from inside the activity, which is the only way to place a cancel
	// in the window this test is about: the environment's clock does not advance
	// while an activity is in flight, so a delayed callback could not fire there.
	h.planIsCancelledDuring(gatesActivity)

	h.runWith(RunInput{Kind: delivery.RunKindDev, Tag: "v3"})
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateCancelled, "")
	gates, plans := h.planCounts()
	require.Equal(t, 1, gates, "the attempt that was interrupted still happened")
	require.Equal(t, 0, plans, "the phase ended at the cancel; the planning turn never ran")
	require.Equal(t, 0, h.dispatchCount(), "a cancelled planning phase dispatches nothing")
	require.Equal(t, 1, h.closedCount(), "the increment is abandoned, so its milestone closes")
}

// The same, one activity later: the gates landed, and the cancel arrives during
// the planning TURN — the 30-minute one, where a person waiting on a spec they
// have changed their mind about is most likely to press the button.
func TestPlanningPhase_CancelDuringThePlanningTurnSettles(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1))
	h.planIsCancelledDuring(planActivity)

	h.runWith(RunInput{Kind: delivery.RunKindDev, Tag: "v3"})
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateCancelled, "")
	gates, plans := h.planCounts()
	require.Equal(t, 1, gates)
	require.Equal(t, 1, plans, "the turn that was interrupted still happened")
	require.Equal(t, 0, h.dispatchCount())
}

// A cancel pending in the same workflow task as a FAILED activity is read as a
// cancel, not as the failure.
//
// The selector's registration order is what decides this, and the alternative is
// actively misleading: the version would settle `plan-failed` — a story about the
// platform being unable to author the version — for a run a person had
// deliberately stopped, with their cancel still sitting unread.
func TestPlanningPhase_CancelWinsOverASimultaneousGateFailure(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1))
	h.planIsCancelledDuring(gatesActivity, errors.New("openchoreo unreachable"))

	h.runWith(RunInput{Kind: delivery.RunKindDev, Tag: "v3"})
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateCancelled, "")
	require.NotEqual(t, delivery.RunReasonPlanFailed, res.TerminalReason,
		"a cancelled run reports the cancel, never the failure it raced")
}

// ---- the gate activity's retries are BOUNDED --------------------------------

// Provisioning has answers that repeating cannot change — a dependency naming a
// ClusterResourceType nobody installed is the one that taught us — and under
// Temporal's default unbounded policy such an answer is a permanent, invisible
// loop rather than a failed build. Worse than invisible: the activity re-mints
// the version's gate issues on every attempt, so the symptom is a milestone
// filling with duplicate gates forever.
//
// So it gives up, and the run settles `plan-failed` — the reason the phase has
// always written for a version it could not author.
func TestPlanningPhase_AnUnsatisfiableGateGivesUpAfterItsAttempts(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1))
	h.planIs(errors.New(`resource "x" produced no new ResourceRelease within 1m0s`), nil)

	h.runWith(RunInput{Kind: delivery.RunKindDev, Tag: "v3"})
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonPlanFailed)
	gates, plans := h.planCounts()
	require.Equal(t, gateActivityAttempts, gates,
		"the gate activity is tried a bounded number of times and then the version fails")
	require.Equal(t, 0, plans, "a version whose gates could not be authored is never planned")
	require.Equal(t, 0, h.dispatchCount())
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
		MilestoneSnapshot{}, // the index has not caught up yet
		workable(1, 1),      // the labelled issue appears
		MilestoneSnapshot{}, // worked, and now genuinely empty
	)
	h.merges(1)
	// The webhook that wakes the park once the issue is indexed.
	h.signal(delivery.SigRunWorkable, 2*time.Second)

	h.runWith(RunInput{Kind: delivery.RunKindTask}) // no Tag
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
	h.runWith(RunInput{Kind: delivery.RunKindDev, Tag: "v3"})
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
			h.milestoneIs(workable(1, 1))
			h.planIs(tc.gatesErr, tc.planEr)

			h.runWith(RunInput{Kind: delivery.RunKindDev, Tag: "v3"})
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

	h.runWith(RunInput{Kind: delivery.RunKindDev, Tag: "v3"})
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, 0, h.dispatchCount())
	require.Equal(t, 1, h.closed,
		"a version nothing will ever judge — no task was filed — has nothing left to wait for")
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

// ---- cancel is durable ------------------------------------------------------

// TestCancel_RecordedOnTheRunRowStopsTheRedispatch is what making cancel durable
// buys. The cancel surface reaps the agent's Component, and from inside the loop
// a reaped pod and a dead agent are indistinguishable: the landing deadline
// expires with nothing merged. Without the stamp the loop calls that agent
// death, spends a re-dispatch, and opens a fresh cycle over a run the user just
// stopped — which is the bug.
//
// NO signal is sent here, deliberately. Signal delivery is best-effort by
// construction, so this is also the proof that losing one now costs latency
// rather than a cycle.
func TestCancel_RecordedOnTheRunRowStopsTheRedispatch(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1))
	h.factsAre(
		// The boundary asks first, before anything is dispatched.
		CycleFacts{CycleID: testCycleID},
		// Then the landing wait, after the pod was reaped and the deadline blew.
		CycleFacts{CycleID: testCycleID, CancelRequested: true},
	)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateCancelled, "")
	require.Equal(t, 1, h.dispatchCount(), "a cancelled cycle must not buy a re-dispatch")
}

// TestCancel_RecordedBeforeTheFirstDispatchNeverDispatches: a run parked in the
// unbounded wait has no cycle record at all, so the cancel read has to be
// independent of one. Without that, a cancel on a parked run would be invisible
// until it dispatched — which is precisely what it must never do.
func TestCancel_RecordedBeforeTheFirstDispatchNeverDispatches(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1))
	h.factsAre(CycleFacts{CancelRequested: true})

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateCancelled, "")
	require.Equal(t, 0, h.dispatchCount(), "a run cancelled before its first cycle never dispatches")
}

// TestAgentDeath_WithNoCancelRecordedStillSpendsTheRedispatch is the other half,
// and the one that would catch an over-eager fix. Making cancel durable must not
// turn every unlanded cycle into a cancellation: an agent that genuinely died
// still costs its re-dispatch budget and still settles on redispatch-budget.
func TestAgentDeath_WithNoCancelRecordedStillSpendsTheRedispatch(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1))
	h.factsAre(CycleFacts{CycleID: testCycleID}) // never lands, never cancelled

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonRedispatchBudget)
	require.Equal(t, delivery.RunMaxRedispatchPerCycle, h.dispatchCount(),
		"genuine agent death must still spend the whole re-dispatch budget")
}
