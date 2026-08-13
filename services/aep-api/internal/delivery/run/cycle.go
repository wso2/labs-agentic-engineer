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

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// cycleResult is what one cycle produced. It is the only thing the boundary
// needs to know about a cycle: what to do next, and which budget was spent.
type cycleResult int

const (
	// cycleNone means no cycle has run yet (or the last one is deliberately not
	// counted, as after validation).
	cycleNone cycleResult = iota
	// cycleGreen — merged and every touched component built.
	cycleGreen
	// cycleRed — merged, but a component's build stayed red through its one
	// automatic re-trigger. The event plane minted the fix issue.
	cycleRed
	// cycleConflict — the pull request would not merge. The event plane minted
	// the conflict issue naming it.
	cycleConflict
	// cycleAgentDead — the dispatch never landed a pull request, through the
	// whole per-cycle re-dispatch budget.
	cycleAgentDead
	// cycleCancelled — a human abandoned the increment mid-cycle.
	cycleCancelled
	// cycleQuotaBlocked — the org has no agent-concurrency slot, so the cycle
	// never launched. Not a failure and not a spent budget: the run settles
	// blocked with an actionable message.
	cycleQuotaBlocked
)

// landing is how one dispatch attempt ended.
type landing int

const (
	landingMergeSignalled landing = iota
	landingConflict
	landingCancelled
	landingTimeout
)

// runCycle is ONE dispatch of the agent at the milestone, through to a verdict.
//
//	append the cycle record ─► dispatch ─► wait for the pull request to land
//	                                    ├─ conflict  ─► cycleConflict
//	                                    ├─ no landing within the deadline ─► re-dispatch
//	                                    └─ merged ─► wait for the fan-out's builds
//	                                                 ├─ all green ─► cycleGreen
//	                                                 └─ a component red ─► cycleRed
//
// anchorIssue is set only for the validation cycle: every other kind works the
// milestone's whole working set, because a fix or conflict issue is ordinary
// work and the runner re-lists the milestone before picking each issue anyway.
func (l *loop) runCycle(ctx workflow.Context, kind string, anchorIssue int) (cycleResult, error) {
	cycleID, err := l.appendCycle(ctx, kind)
	if err != nil {
		return cycleNone, err
	}

	// Budgets are spent when the cycle OPENS, not when it ends: a cycle that
	// crashes mid-flight has still consumed the run's allowance to attempt it.
	l.st.CyclesTotal++
	if err := l.bump(ctx, delivery.RunBudgetCycles); err != nil {
		return cycleNone, err
	}
	switch kind {
	case delivery.CycleKindFix:
		l.st.FixCycles++
		if err := l.bump(ctx, delivery.RunBudgetFixCycles); err != nil {
			return cycleNone, err
		}
	case delivery.CycleKindConflict:
		l.st.ConflictCycles++
		if err := l.bump(ctx, delivery.RunBudgetConflictCycles); err != nil {
			return cycleNone, err
		}
	}

	l.st.CycleKind = kind
	l.st.CycleAttempt = 0
	l.st.CyclePR = 0
	l.prNumber, l.mergeSHA = 0, ""
	// Held on the loop because the validation verdict is written AFTER this returns:
	// it is derived from the report at the cycle's own merge commit, which does not
	// exist until the cycle has landed and closed.
	l.cycleID = cycleID
	if err := l.setState(ctx, delivery.RunStateRunning); err != nil {
		return cycleNone, err
	}
	l.st.Phase = phaseFor(kind)

	landed, res, err := l.dispatchUntilLanded(ctx, kind, anchorIssue, cycleID)
	if err != nil {
		return cycleNone, err
	}
	if !landed {
		// Nothing merged: close the cycle with no merge SHA so the timeline shows
		// a dispatch that produced nothing rather than an open cycle forever.
		if err := l.finishCycle(ctx, cycleID, ""); err != nil {
			return cycleNone, err
		}
		return res, nil
	}

	if err := l.finishCycle(ctx, cycleID, l.mergeSHA); err != nil {
		return cycleNone, err
	}
	l.st.Phase = delivery.RunPhaseBuilding
	res, err = l.awaitBuilds(ctx)
	if err != nil {
		return cycleNone, err
	}
	if res == cycleGreen {
		// Green is the first moment the managed-API trait config has somewhere to
		// land: OpenChoreo builds the ReleaseBinding that carries it out of the
		// workload the build's last step generates. Deliberately not on the red
		// path — a component that did not build has no new binding to converge,
		// and the fix cycle that follows will pass through here again.
		l.syncAPITraits(ctx)
	}
	return res, nil
}

// dispatchUntilLanded spends the cycle's re-dispatch budget trying to land a
// merged pull request.
//
// A dispatch that fails to LAUNCH counts as an attempt: a Job that could not be
// created is agent death arriving early, and the budget that names that failure
// class is exactly this one. (Temporal does not retry the launch either — see
// dispatchActivityCtx.)
func (l *loop) dispatchUntilLanded(ctx workflow.Context, kind string, anchorIssue int, cycleID string) (bool, cycleResult, error) {
	for l.st.CycleAttempt < delivery.RunMaxRedispatchPerCycle {
		l.st.CycleAttempt++
		jobRef, derr := l.dispatch(ctx, kind, anchorIssue, cycleID)
		if derr != nil {
			// A quota refusal is the one launch failure that is NOT agent
			// death: re-attempting cannot free a slot, so the loop stops here
			// instead of burning the rest of the budget on the same answer.
			if isAgentQuotaBlocked(derr) {
				return false, cycleQuotaBlocked, nil
			}
			continue
		}
		if err := l.noteDispatch(ctx, cycleID, jobRef); err != nil {
			return false, cycleNone, err
		}

		attemptCtx, stopDeadline := workflow.WithCancel(ctx)
		deadline := workflow.NewTimer(attemptCtx, cycleLandingTimeout)
		expired := false
		for !expired {
			switch l.awaitLanding(ctx, deadline) {
			case landingCancelled:
				stopDeadline()
				return false, cycleCancelled, nil
			case landingConflict:
				stopDeadline()
				return false, cycleConflict, nil
			case landingTimeout:
				expired = true
			case landingMergeSignalled:
				// Never act on the payload: a human's pull request merging during
				// the cycle raises the same signal, and only the CYCLE RECORD says
				// whether the agent's own work landed.
			}
			facts, ferr := l.cycleFacts(ctx)
			if ferr != nil {
				stopDeadline()
				return false, cycleNone, ferr
			}
			if facts.MergeSHA != "" {
				l.mergeSHA, l.prNumber = facts.MergeSHA, facts.PRNumber
				l.st.CyclePR = facts.PRNumber
				stopDeadline()
				// Landed: the verdict is the build phase's, not this loop's.
				return true, cycleNone, nil
			}
		}
		stopDeadline()
	}
	return false, cycleAgentDead, nil
}

// awaitLanding blocks for one event of the cycle's coding phase. The deadline
// future is created once per ATTEMPT, so a spurious wake-up re-enters the wait
// without extending the agent's allowance.
func (l *loop) awaitLanding(ctx workflow.Context, deadline workflow.Future) landing {
	out := landingMergeSignalled
	sel := workflow.NewSelector(ctx)
	sel.AddReceive(l.cancel, func(c workflow.ReceiveChannel, _ bool) {
		c.Receive(ctx, nil)
		out = landingCancelled
	})
	sel.AddReceive(l.conflict, func(c workflow.ReceiveChannel, _ bool) {
		c.Receive(ctx, nil)
		out = landingConflict
	})
	sel.AddReceive(l.merged, func(c workflow.ReceiveChannel, _ bool) {
		c.Receive(ctx, nil)
		out = landingMergeSignalled
	})
	// A workable or build signal during the coding phase is noise (an issue
	// joined the milestone, a stale build reported). Drained so it cannot wake
	// the next wait spuriously.
	for _, ch := range []workflow.ReceiveChannel{l.workable, l.builds} {
		sel.AddReceive(ch, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil) })
	}
	sel.AddFuture(deadline, func(workflow.Future) { out = landingTimeout })
	sel.Select(ctx)
	return out
}

// awaitBuilds waits for the merge's build fan-out to reach a verdict.
//
// It is bounded only by cancel, on purpose. An OpenChoreo WorkflowRun always
// terminates (the platform gives every build an active deadline), so a poll
// eventually sees every expected component settle — and inventing a timeout
// here would create a failure class §7 does not name, which is exactly how
// terminal reasons stop being honest.
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

		timerCtx, stop := workflow.WithCancel(ctx)
		cancelled := false
		sel := workflow.NewSelector(ctx)
		sel.AddReceive(l.cancel, func(c workflow.ReceiveChannel, _ bool) {
			c.Receive(ctx, nil)
			cancelled = true
		})
		for _, ch := range []workflow.ReceiveChannel{l.builds, l.workable, l.merged, l.conflict} {
			sel.AddReceive(ch, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil) })
		}
		sel.AddFuture(workflow.NewTimer(timerCtx, buildPollInterval), func(workflow.Future) {})
		sel.Select(ctx)
		stop()
		if cancelled {
			return cycleCancelled, nil
		}
	}
}

// phaseFor names the read-model phase a cycle of this kind starts in.
func phaseFor(kind string) string {
	if kind == delivery.CycleKindValidation {
		return delivery.RunPhaseValidating
	}
	return delivery.RunPhaseCoding
}

// ---- cycle activity calls --------------------------------------------------

func (l *loop) appendCycle(ctx workflow.Context, kind string) (string, error) {
	var cycleID string
	err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).AppendCycle, AppendCycleInput{
		RunID: l.in.RunID, OrgID: l.in.OrgID, ProjectID: l.in.ProjectID, Kind: kind,
	}).Get(ctx, &cycleID)
	return cycleID, err
}

func (l *loop) noteDispatch(ctx workflow.Context, cycleID, jobRef string) error {
	return workflow.ExecuteActivity(activityCtx(ctx), (*Activities).NoteCycleDispatch,
		NoteCycleDispatchInput{CycleID: cycleID, JobRef: jobRef}).Get(ctx, nil)
}

func (l *loop) finishCycle(ctx workflow.Context, cycleID, mergeSHA string) error {
	return workflow.ExecuteActivity(activityCtx(ctx), (*Activities).FinishCycle,
		FinishCycleInput{CycleID: cycleID, MergeSHA: mergeSHA}).Get(ctx, nil)
}

func (l *loop) cycleFacts(ctx workflow.Context) (CycleFacts, error) {
	var facts CycleFacts
	err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).ReadCycleFacts,
		CycleFactsInput{OrgID: l.in.OrgID, RunID: l.in.RunID}).Get(ctx, &facts)
	return facts, err
}

func (l *loop) dispatch(ctx workflow.Context, kind string, anchorIssue int, cycleID string) (string, error) {
	var jobRef string
	err := workflow.ExecuteActivity(dispatchActivityCtx(ctx), (*Activities).DispatchAgent, delivery.MilestoneDispatch{
		OrgID:           l.in.OrgID,
		ProjectID:       l.in.ProjectID,
		MilestoneNumber: l.in.MilestoneNumber,
		MilestoneTitle:  l.in.MilestoneTitle,
		Kind:            kind,
		IssueNumber:     anchorIssue,
		RunID:           l.in.RunID,
		CycleID:         cycleID,
	}).Get(ctx, &jobRef)
	return jobRef, err
}

func (l *loop) pollBuilds(ctx workflow.Context) (CycleBuildState, error) {
	var state CycleBuildState
	err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).PollCycleBuilds, CycleBuildsInput{
		OrgID: l.in.OrgID, ProjectID: l.in.ProjectID, PRNumber: l.prNumber, MergeSHA: l.mergeSHA,
	}).Get(ctx, &state)
	return state, err
}

// isAgentQuotaBlocked reports whether a dispatch failed because the org is at
// its agent-concurrency cap. It matches on the ApplicationError TYPE the
// dispatch activity stamps, because Temporal round-trips errors as data and a
// sentinel does not survive the boundary.
func isAgentQuotaBlocked(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == delivery.ErrTypeAgentQuotaBlocked
	}
	return false
}
