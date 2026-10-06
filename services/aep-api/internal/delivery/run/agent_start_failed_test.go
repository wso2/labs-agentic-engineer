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
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// agent_start_failed_test.go — a cycle whose agent never started (the watcher
// closed it startup_failed past the grace and suspended its Job) settles the
// run failed under its own reason, agent-start-failed, after the ONE dispatch
// that was made: not redispatch-budget, which names an agent that ran and died
// through the whole budget.

const unschedulableReason = "startup_failed:Unschedulable: 0/1 nodes are available: 1 Insufficient memory."

func TestAgentStartFailed_FailsADevRunUnderItsOwnReason(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1))
	h.factsAre(CycleFacts{CycleID: testCycleID, Ended: true, AgentReason: unschedulableReason})
	h.signal(delivery.SigRunAgentDied, 10*time.Minute)

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonAgentStartFailed)
	require.Equal(t, 1, h.dispatchCount(), "a closed cycle buys no re-dispatch")
	require.Equal(t, []FinishCycleInput{{CycleID: testCycleID}}, h.finishes,
		"the cycle closes with no merge SHA, like any dispatch that landed nothing")
	require.Equal(t, 0, h.deployMintCount(), "nothing ran, so there is nothing to fix")
}

// Without the signal the landing deadline wakes the loop into the same read.
func TestAgentStartFailed_WithoutTheSignalTheDeadlineSettlesTheSame(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(workable(1, 1))
	h.factsAre(CycleFacts{CycleID: testCycleID, Ended: true, AgentReason: unschedulableReason})

	h.run(delivery.RunKindDev, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonAgentStartFailed)
}

// F2 option A: a validation agent that never started is a FAILED validation
// run under the same reason, and its task still closes (with no verdict), so
// the reconcile sweep does not restart it forever; "Run validation" is the way
// forward.
func TestAgentStartFailed_FailsAValidationRunAndClosesItsTask(t *testing.T) {
	h := newHarness(t)
	h.validationIs(77, delivery.ValidationVerdictPassed) // never read — nothing lands
	h.factsAre(CycleFacts{CycleID: testCycleID, Ended: true, AgentReason: unschedulableReason})
	h.signal(delivery.SigRunAgentDied, 10*time.Minute)

	h.run(delivery.RunKindValidation, 0)
	res := h.result(t)

	h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonAgentStartFailed)
	require.Equal(t, 1, h.dispatchCount())
	h.mu.Lock()
	defer h.mu.Unlock()
	require.Len(t, h.taskCloses, 1, "the task closes on every ending")
	require.Empty(t, h.taskCloses[0].Verdict, "no verdict is claimed for an agent that never ran")
	require.Empty(t, h.verdictWrites)
}

func TestAgentStartFailed_ValidationSwitchMapsIt(t *testing.T) {
	state, reason := stateForUnlandedValidation(cycleAgentStartFailed)
	require.Equal(t, delivery.RunStateFailed, state)
	require.Equal(t, delivery.RunReasonAgentStartFailed, reason)
}

// Any other closing reason is still agent death through the budget: the rule
// keys on the startup_failed prefix only.
func TestAgentStartFailed_OtherAgentDeathsStayRedispatchBudget(t *testing.T) {
	for _, reason := range []string{"agent_failed:OOMKilled", "timed_out", "job_not_found", ""} {
		t.Run(reason, func(t *testing.T) {
			h := newHarness(t)
			h.milestoneIs(workable(1, 1))
			h.factsAre(CycleFacts{CycleID: testCycleID, Ended: true, AgentReason: reason})
			h.signal(delivery.SigRunAgentDied, 10*time.Minute)

			h.run(delivery.RunKindDev, 0)
			res := h.result(t)

			h.assertSettled(t, res, delivery.RunStateFailed, delivery.RunReasonRedispatchBudget)
		})
	}
}
