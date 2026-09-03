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
	"time"

	"go.temporal.io/sdk/workflow"
)

// The BUILD STAGE: wait for the merge's build fan-out to reach a verdict.

// awaitBuilds waits for the merge's build fan-out to reach a verdict.
//
// It is bounded only by cancel, on purpose. An OpenChoreo WorkflowRun always
// terminates (the platform gives every build an active deadline), so a poll
// eventually sees every expected component settle — and inventing a timeout
// here would create a failure class the terminal reasons do not name, which is
// exactly how terminal reasons stop being honest.
//
// That premise has a dependency worth stating, because it has been broken once:
// a WorkflowRun only inherits the platform's deadline once it RENDERS into an
// Argo Workflow. A run that OpenChoreo accepts but cannot render never gets one,
// and it is indistinguishable from a slow build from here — its status says
// WorkflowPending with no condition and no event naming the cause. The known way
// to produce that is a run name over the Kubernetes label-value budget, which is
// now refused at creation (see k8sname.MaxLabelValueLen and the guard in the
// OpenChoreo client's createWorkflowRun), so this loop's premise holds by
// construction rather than by luck. If another render failure is ever found, the
// fix belongs there too — at the point of creation, where the cause is still
// known — and not in a timeout here, which could only report "something took too
// long" about a run that was never going to start.
//
// It answers the CYCLE's verdict and nothing else. It used to hand the deploy
// stage the components its poll had seen, which is what made the deploy set the
// cycle's path diff — and a component whose build went green in a cycle a
// sibling failed was then promoted by nothing, ever. The deploy reads the
// version's own state instead (ADR-0026); this stage's answer is only "did the
// code this merge built compile".
func (l *loop) awaitBuilds(ctx workflow.Context) (cycleResult, error) {
	for {
		state, err := l.pollBuilds(ctx)
		if err != nil {
			return cycleNone, err
		}
		if len(state.Red) > 0 {
			return cycleRed, nil
		}
		if state.Green() {
			return cycleGreen, nil
		}

		if cancelled, _ := l.awaitWake(ctx, buildPollInterval, nil); cancelled {
			return cycleCancelled, nil
		}
	}
}

// awaitWake blocks until something worth re-polling happens: a signal, the poll
// timer, a cancel, or (when given) a stage deadline.
//
// Every signal is DRAINED without being read. That is the loop's standing rule —
// a signal is a wake-up, never evidence — so what a waiter does on waking is
// always the same: go and re-read ground truth. Both stages therefore need the
// identical select, and the only things that differ between them are how often
// to re-poll and whether the stage has an expiry at all.
func (l *loop) awaitWake(ctx workflow.Context, poll time.Duration, deadline workflow.Future) (cancelled, expired bool) {
	timerCtx, stop := workflow.WithCancel(ctx)
	defer stop()

	sel := workflow.NewSelector(ctx)
	sel.AddReceive(l.cancel, func(c workflow.ReceiveChannel, _ bool) {
		c.Receive(ctx, nil)
		cancelled = true
	})
	for _, ch := range []workflow.ReceiveChannel{l.builds, l.workable, l.merged, l.conflict, l.valuesSaved} {
		sel.AddReceive(ch, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil) })
	}
	sel.AddFuture(workflow.NewTimer(timerCtx, poll), func(workflow.Future) {})
	if deadline != nil {
		sel.AddFuture(deadline, func(workflow.Future) { expired = true })
	}
	sel.Select(ctx)
	return cancelled, expired
}

func (l *loop) pollBuilds(ctx workflow.Context) (CycleBuildState, error) {
	var state CycleBuildState
	err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).PollCycleBuilds, CycleBuildsInput{
		OrgID: l.in.OrgID, ProjectID: l.in.ProjectID, PRNumber: l.prNumber, MergeSHA: l.mergeSHA,
	}).Get(ctx, &state)
	return state, err
}
