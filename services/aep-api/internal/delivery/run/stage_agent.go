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

// The AGENT STAGE: open the cycle record, spend the cycle budgets, launch the
// agent, and wait for its pull request to land — re-dispatching within the
// cycle's own budget when it does not.
//
// It is the one stage every workflow in this package runs. A dev or task cycle
// continues into builds and a deploy (loop.runCycle); a validation run stops
// here, because its pull request carries only tests and a report.

// cycleResult is what one cycle produced. It is the only thing the boundary
// needs to know about a cycle: what to do next, and which budget was spent.
type cycleResult int

const (
	// cycleNone means no cycle has run yet (or the last one is deliberately not
	// counted).
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
	// cyclePublisherCredentials — dispatch cannot mount publisher CC. Not
	// agent death: repeating the Job create cannot stamp the SecretReference.
	cyclePublisherCredentials
	// cycleDeployFailed — merged and built, but a component's ReleaseBinding
	// never came up. Distinct from cycleRed because the failure is a different
	// class with a different terminal reason: the code compiled, the platform
	// could not run it.
	cycleDeployFailed
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

// noAnchorIssue is the anchor a cycle over a whole WORKING SET passes: none. A
// fix or conflict issue is ordinary work and the runner re-lists the milestone
// before picking each issue anyway, so only the validation cycle — one issue,
// one run — names one. Spelled rather than written as a bare 0 at the call
// sites, because whether a cycle is issue-anchored is the difference between
// judging one thing and working everything.
const noAnchorIssue = 0

// agentStage dispatches one agent run and waits for it to land a merged pull
// request, spending the cycle's budgets as it opens.
//
// It returns landed=false with the cycleResult that ended it — agent death, a
// conflict, a cancel, a quota refusal — having closed the cycle record with no
// merge SHA so the timeline shows a dispatch that produced nothing rather than
// an open cycle forever. On landed=true the cycle record is closed at its merge
// SHA and l.mergeSHA / l.prNumber / l.cycleID carry the landing.
//
// anchorIssue is set only for a validation cycle (see noAnchorIssue).
func (l *loop) agentStage(ctx workflow.Context, kind string, anchorIssue int) (landed bool, res cycleResult, err error) {
	cycleID, err := l.appendCycle(ctx, kind)
	if err != nil {
		return false, cycleNone, err
	}

	// Budgets are spent when the cycle OPENS, not when it ends: a cycle that
	// crashes mid-flight has still consumed the run's allowance to attempt it.
	l.st.CyclesTotal++
	if err := l.bump(ctx, delivery.RunBudgetCycles); err != nil {
		return false, cycleNone, err
	}
	switch kind {
	case delivery.CycleKindFix:
		l.st.FixCycles++
		if err := l.bump(ctx, delivery.RunBudgetFixCycles); err != nil {
			return false, cycleNone, err
		}
	case delivery.CycleKindConflict:
		l.st.ConflictCycles++
		if err := l.bump(ctx, delivery.RunBudgetConflictCycles); err != nil {
			return false, cycleNone, err
		}
	}

	l.st.CycleKind = kind
	l.st.CycleAttempt = 0
	l.st.CyclePR = 0
	l.prNumber, l.mergeSHA = 0, ""
	// Held on the loop because a validation verdict is written AFTER this
	// returns: it is derived from the report at the cycle's own merge commit,
	// which does not exist until the cycle has landed and closed.
	l.cycleID = cycleID
	if err := l.setState(ctx, delivery.RunStateRunning); err != nil {
		return false, cycleNone, err
	}
	l.st.Phase = phaseFor(kind)

	landed, res, err = l.dispatchUntilLanded(ctx, kind, anchorIssue, cycleID)
	if err != nil {
		return false, cycleNone, err
	}
	if !landed {
		if err := l.finishCycle(ctx, cycleID, ""); err != nil {
			return false, cycleNone, err
		}
		return false, res, nil
	}
	if err := l.finishCycle(ctx, cycleID, l.mergeSHA); err != nil {
		return false, cycleNone, err
	}
	return true, cycleNone, nil
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
		// Re-derive cancel from the run row before SPENDING an attempt. Reached
		// on the second pass and after, when the first attempt ended in the
		// landing deadline — which is precisely the shape a reaped pod leaves
		// behind. Without it a cancel whose signal was lost buys the re-dispatch
		// it was meant to stop.
		if l.st.CycleAttempt > 0 {
			facts, ferr := l.cycleFacts(ctx)
			if ferr != nil {
				return false, cycleNone, ferr
			}
			if facts.CancelRequested {
				return false, cycleCancelled, nil
			}
		}
		l.st.CycleAttempt++
		jobRef, derr := l.dispatch(ctx, kind, anchorIssue, cycleID)
		if derr != nil {
			// A quota refusal is the one launch failure that is NOT agent
			// death: re-attempting cannot free a slot, so the loop stops here
			// instead of burning the rest of the budget on the same answer.
			if isAgentQuotaBlocked(derr) {
				return false, cycleQuotaBlocked, nil
			}
			// Same shape as the quota refusal and for the same reason: the Job
			// could not be created at all, and re-attempting cannot stamp the
			// SecretReference it is missing.
			if isPublisherCredentialsMissing(derr) {
				return false, cyclePublisherCredentials, nil
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
			// Cancel before landing: a merge that raced the cancel still belongs
			// to a run the user stopped, and carrying on into the build stage
			// would be the loop doing work nobody asked for.
			if facts.CancelRequested {
				stopDeadline()
				return false, cycleCancelled, nil
			}
			if facts.MergeSHA != "" {
				l.mergeSHA, l.prNumber = facts.MergeSHA, facts.PRNumber
				l.st.CyclePR = facts.PRNumber
				stopDeadline()
				// Landed: the verdict is the next stage's, not this loop's.
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

// isPublisherCredentialsMissing reports whether a dispatch failed because the
// publisher client credentials could not be mounted. Matched on the
// ApplicationError TYPE for the same reason as the quota refusal above.
func isPublisherCredentialsMissing(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == delivery.ErrTypePublisherCredentialsMissing
	}
	return false
}
