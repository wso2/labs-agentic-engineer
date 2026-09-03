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
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// The DEPLOY GATE (ADR-0023), driven through the whole loop. The unit tests in
// activities_gate_test.go pin the activity; these pin what the STAGE does with
// its two answers, which is where the decision actually lives.

// TestDeployGate_UnconfiguredValuesParkTheRunAndNameTheBlockers is the gate's
// reason for existing: a project short a credential must not deploy, and the
// park must say WHICH credential — the park is unbounded and the only thing that
// ends it is a person, so a park that does not name its blockers is a hang.
func TestDeployGate_UnconfiguredValuesParkTheRunAndNameTheBlockers(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.merges(1)
	// Blocked on two, then on one as a value arrives, then open.
	h.gateVerdictsAre(
		DeployGateVerdict{Unconfigured: []string{"stripe", "twilio"}},
		DeployGateVerdict{Unconfigured: []string{"twilio"}},
		DeployGateVerdict{},
	)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	parks, checks := h.parksOnValues()
	require.Len(t, parks, 2, "the gate re-asserts the park on every blocked pass")
	require.Equal(t, delivery.RunStateWaiting, parks[0].State)
	require.Equal(t, []string{"stripe", "twilio"}, parks[0].BlockingDependencies,
		"the park names what it is waiting for, or `waiting` reads as a hang")
	// RE-ASSERTED, not written once: the second pass names only what is still
	// missing, so the console never asks for a value the developer already saved.
	require.Equal(t, []string{"twilio"}, parks[1].BlockingDependencies)
	require.Equal(t, 3, checks, "the gate is re-read on every pass, not cached")

	// And it deployed only once the gate opened — the whole point.
	require.Equal(t, 2, h.deployCount(), "one wave then one converge, after the gate opened")
}

// TestDeployGate_ResumesRunningAfterAPark: the run has to come OUT of `waiting`
// before it promotes, or the console shows a deploying run as parked on
// credentials that already arrived.
func TestDeployGate_ResumesRunningAfterAPark(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.merges(1)
	h.gateVerdictsAre(
		DeployGateVerdict{Unconfigured: []string{"stripe"}},
		DeployGateVerdict{},
	)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	// The last state write before the first promote is `running`.
	var lastBeforeDeploy string
	for _, in := range h.stateWrites {
		if in.WaitingReason != "" {
			lastBeforeDeploy = delivery.RunStateWaiting
			continue
		}
		lastBeforeDeploy = in.State
	}
	require.Equal(t, delivery.RunStateRunning, lastBeforeDeploy,
		"a run that parked must be put back into running before it deploys")
}

// TestDeployGate_OpenGateWritesNoStateTransition: the ordinary case is a
// configured project, and it must cost nothing. A redundant `running` write on
// every deploy would show the console a transition that did not happen.
func TestDeployGate_OpenGateWritesNoStateTransition(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.merges(1)
	h.gateVerdictsAre(DeployGateVerdict{})

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	parks, _ := h.parksOnValues()
	require.Empty(t, parks, "a configured project never parks")
	require.Equal(t, 2, h.deployCount())
}

// TestDeployGate_ProvisioningPollsRatherThanParks is the OTHER half of the
// decision, and the reason the verdict keeps its two lists apart. A platform
// resource that is still provisioning is the platform working: parking on it
// would hang the run on something that resolves itself, and would tell a
// developer to go and supply a value that is not theirs to supply.
func TestDeployGate_ProvisioningPollsRatherThanParks(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.merges(1)
	h.gateVerdictsAre(
		DeployGateVerdict{Provisioning: []string{"postgres"}},
		DeployGateVerdict{Provisioning: []string{"postgres"}},
		DeployGateVerdict{},
	)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	parks, checks := h.parksOnValues()
	require.Empty(t, parks, "the platform still working is not a reason to tell a human to act")
	require.Equal(t, 3, checks, "it polls the gate instead")
	require.Equal(t, 2, h.deployCount())
}

// TestDeployGate_CancelExitsAnUnboundedPark. The park has no deadline by design
// — a credential arrives when somebody gets round to it — so cancellation is its
// ONLY expiry. A park that could not be cancelled would be a run nobody can stop.
func TestDeployGate_CancelExitsAnUnboundedPark(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.merges(1)
	// Never opens.
	h.gateVerdictsAre(DeployGateVerdict{Unconfigured: []string{"stripe"}})
	h.signal(delivery.SigRunCancel, 2*time.Second)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateCancelled, "")
	require.Equal(t, 0, h.deployCount(), "a run cancelled at the gate never promoted anything")
}

// TestDeployGate_AValuesSavedSignalUnparksTheRun. A value save produces no
// webhook, so the signal is the only prompt wake there is — everything else
// waits out the ten-minute wait-poll. The signal is a WAKE-UP, not evidence: the
// stage re-reads the gate and parks straight back if another value is still
// missing.
func TestDeployGate_AValuesSavedSignalUnparksTheRun(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.merges(1)
	h.gateVerdictsAre(
		DeployGateVerdict{Unconfigured: []string{"stripe"}},
		DeployGateVerdict{},
	)
	h.signal(delivery.SigRunValuesSaved, 2*time.Second)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, 2, h.deployCount())
}

// TestDeployGate_ACycleThatPromotesNothingIsNeverParked. A converge or
// validation cycle touches no component, so gating it would park a run on a
// credential it is not going to use — and, since the park is unbounded, would
// hold a run that had nothing left to deploy.
func TestDeployGate_ACycleThatPromotesNothingIsNeverParked(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.merges(1)
	// A merge that built nothing: the deploy stage returns before the gate.
	h.buildsAre(CycleBuildState{Expected: 0, Settled: 0})
	h.gateVerdictsAre(DeployGateVerdict{Unconfigured: []string{"stripe"}})

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	parks, checks := h.parksOnValues()
	require.Empty(t, parks, "a cycle that promotes nothing must never park on a credential")
	require.Zero(t, checks, "and must not even ask the gate")
}

// TestDeployGate_ACancelStampedOnTheRowExitsTheValuesPark. Cancellation is
// delivered twice — a signal and a stamp on the run row — because the cancel
// surface swallows a failed signal delivery so that a dead engine cannot wedge
// the console. The park is unbounded, so a gate that believed only the signal
// would hold a run the user cancelled forever, on a credential nobody is going
// to supply. No signal is sent here: the row is the only evidence there is.
func TestDeployGate_ACancelStampedOnTheRowExitsTheValuesPark(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.merges(1)
	h.gateVerdictsAre(DeployGateVerdict{Unconfigured: []string{"stripe"}}) // never opens
	h.cancelStampedOnceAtTheGate()

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateCancelled, "")
	require.Equal(t, 0, h.deployCount(), "a run cancelled at the gate never promoted anything")
}

// TestDeployGate_AResourceThatNeverProvisionsFailsTheCycle. The provisioning
// POLL is the gate's other branch, and "the platform is still working" is an
// assumption: a binding with a bad ResourceType, or a provisioner that fails
// forever, never reaches Ready. Polling it without an expiry would leave the run
// running/deploying for good — no waiting reason, no blocking names, no terminal
// reason — which is precisely the silent hang the values park was designed to
// avoid. It is bounded by the stage's own budget, and expiry is a deploy failure
// that names the resource in the fix work it files.
func TestDeployGate_AResourceThatNeverProvisionsFailsTheCycle(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.merges(1)
	h.gateVerdictsAre(DeployGateVerdict{Provisioning: []string{"orders-db"}}) // never becomes Ready

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonDeployBudget)
	require.Equal(t, 0, h.deployCount(), "the gate never opened, so nothing was promoted")
	parks, _ := h.parksOnValues()
	require.Empty(t, parks, "the platform still working is never a human's park")

	mints := h.deployMintInputs()
	require.Len(t, mints, 1, "a wedged deploy has to become work, or the next boundary poll settles a version that is not running")
	require.Equal(t, []string{"order-service"}, delivery.TargetNames(mints[0].Failed),
		"what this pass would have promoted is what could not be delivered; the resource is nobody's individual fault")
	require.Contains(t, mints[0].Reasons["order-service"], "orders-db",
		"the cause names the resource, which is the one fact the reader cannot derive")
}

// TestDeployGate_TheValuesParkDoesNotSpendTheProvisioningBudget. The two waits
// are charged differently on purpose: deployReadyTimeout bounds how long a
// BINDING may take to become usable, and a developer taking a day to find an API
// key must not spend a second of it. A budget started when the stage was entered
// would settle `deploy-budget` on a run behaving exactly as ADR-0023 designed.
//
// The run below is parked on a value for twenty minutes — longer than the whole
// budget — before the platform is asked to provision anything.
func TestDeployGate_TheValuesParkDoesNotSpendTheProvisioningBudget(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.merges(1)
	// Two park passes at waitPollInterval each, then the platform's turn — which
	// takes more than one pass, so a budget that had already run down while the
	// run was parked would be latched on the first poll and fail the second.
	h.gateVerdictsAre(
		DeployGateVerdict{Unconfigured: []string{"stripe"}},
		DeployGateVerdict{Unconfigured: []string{"stripe"}},
		DeployGateVerdict{Provisioning: []string{"orders-db"}},
		DeployGateVerdict{Provisioning: []string{"orders-db"}},
		DeployGateVerdict{},
	)
	require.Greater(t, 2*waitPollInterval, deployReadyTimeout, "the park has to outlast the budget for this to prove anything")

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, 2, h.deployCount(), "one wave then one converge: the budget was never spent on the park")
}

// TestDeployGate_LeavingTheValuesParkClearsIt. The park stamps a reason and the
// names it is blocked on, and those are what the console renders. A pass that
// moved on to platform provisioning while leaving them there would tell the
// developer to supply a credential they already supplied, and name a dependency
// that is no longer the blocker — while the real one goes unmentioned.
func TestDeployGate_LeavingTheValuesParkClearsIt(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1), MilestoneSnapshot{})
	h.merges(1)
	// The value arrives, and the platform becomes the blocker for a while.
	verdicts := []DeployGateVerdict{{Unconfigured: []string{"stripe"}}}
	for i := 0; i < 8; i++ {
		verdicts = append(verdicts, DeployGateVerdict{Provisioning: []string{"orders-db"}})
	}
	h.gateVerdictsAre(append(verdicts, DeployGateVerdict{})...)
	h.signal(delivery.SigRunValuesSaved, 2*time.Second)

	// Mid-run, while the gate is polling the platform: the live status a console
	// query answers must no longer claim the run is waiting on a value.
	var whileProvisioning delivery.RunStatus
	h.env.RegisterDelayedCallback(func() {
		resp, err := h.env.QueryWorkflow(delivery.QueryRunStatus)
		require.NoError(t, err)
		require.NoError(t, resp.Get(&whileProvisioning))
	}, time.Minute)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateSucceeded, "")
	require.Equal(t, delivery.RunStateRunning, whileProvisioning.State,
		"the run is no longer parked: the platform is working, not a person")
	require.Empty(t, whileProvisioning.WaitingReason, "a stale park reason reads as a hang that is not happening")
	require.Empty(t, whileProvisioning.BlockingDependencies,
		"and a stale name asks for a credential the developer already saved")

	// The row was cleared too, and by the write that immediately follows the park.
	parks, _ := h.parksOnValues()
	require.Len(t, parks, 1, "one park: the value arrived on the second pass")
	writes := h.stateWriteLog()
	idx := -1
	for i, in := range writes {
		if in.WaitingReason == delivery.RunWaitingOnExternalValues {
			idx = i
		}
	}
	require.GreaterOrEqual(t, idx, 0)
	require.Greater(t, len(writes), idx+1, "leaving the park has to write something")
	after := writes[idx+1]
	require.Equal(t, delivery.RunStateRunning, after.State)
	require.Empty(t, after.WaitingReason)
	require.Empty(t, after.BlockingDependencies)
	require.Equal(t, 2, h.deployCount())
}

// ---- harness helpers this file needs ---------------------------------------

// cancelStampedOnceAtTheGate makes the run ROW report a cancellation from the
// moment the deploy gate is first consulted, and never delivers the signal. It
// is the shape of a cancel whose signal delivery failed: the row is stamped, the
// runtime never heard about it.
func (h *harness) cancelStampedOnceAtTheGate() {
	h.set["facts"] = true
	h.env.OnActivity(h.acts.ReadCycleFacts, mock.Anything, mock.Anything).
		Return(func(context.Context, CycleFactsInput) (CycleFacts, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			return CycleFacts{
				CycleID:         testCycleID,
				MergeSHA:        testMergeSHA,
				PRNumber:        testPRNumber,
				Ended:           true,
				CancelRequested: h.gateChecks > 0,
			}, nil
		})
}

// deployMintInputs is the filed deploy-fix work, read safely.
func (h *harness) deployMintInputs() []MintDeployFixIssuesInput {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]MintDeployFixIssuesInput(nil), h.deployMints...)
}

// stateWriteLog is the run row's state writes in order, read safely.
func (h *harness) stateWriteLog() []SetRunStateInput {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]SetRunStateInput(nil), h.stateWrites...)
}
