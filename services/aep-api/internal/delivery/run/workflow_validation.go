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
	"go.temporal.io/sdk/workflow"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// ValidationRunWorkflow JUDGES a deployed version against its acceptance
// criteria, and is the only workflow that produces a verdict.
//
// It does NOT share the cycle-boundary loop, and the reason is not economy of
// code: it has no working set to poll and it builds and deploys nothing. Its
// pull request touches only `tests/`, so the merge's path diff yields no
// components and both later stages were already silent no-ops for it — skipping
// them outright is the honest form of what was already true, and it removes two
// stages' worth of failure modes from a run that could never reach them.
//
//	adopt-or-mint the validation task
//	  └─► agent stage (AEP_TASK_KIND=validation, anchored at that issue)
//	        └─► read the verdict at the cycle's OWN merge SHA
//	              ├─ not fatal ──────────────► close the task · succeeded
//	              ├─ unreported, budget left ► re-dispatch (nothing else can fix it)
//	              └─ failed ────────────────► one repair issue per failed criterion
//	                                          · close the task · failed
//
// Every exit closes the task. See settleJudged for why that is not optional.
func ValidationRunWorkflow(ctx workflow.Context, in RunInput) (RunResult, error) {
	l := newLoop(ctx, in)
	if err := workflow.SetQueryHandler(ctx, delivery.QueryRunStatus, func() (delivery.RunStatus, error) {
		return l.st, nil
	}); err != nil {
		return RunResult{}, err
	}
	return l.judgeVersion(ctx)
}

// judgeVersion is the whole workflow.
//
// The two numbers it needs SPAN RUNS — how many times this version has already
// been judged, and what the previous judgement concluded — and both are DERIVED
// from the milestone's own validation runs rather than handed forward. Nothing
// carries them because nothing could: each attempt is its own execution, so the
// previous one's workflow state is gone, and a carrier passed through the run row
// would have to be written by the run that is about to end and read by one that
// does not exist yet. The ledger already holds both facts.
func (l *loop) judgeVersion(ctx workflow.Context) (RunResult, error) {
	cancelled, err := l.cancelled(ctx)
	if err != nil {
		return l.result(), err
	}
	if cancelled {
		// Nothing has been adopted yet, so there is no task to close: cancelling
		// before the first read leaves the version's task exactly as it was, which
		// is what "trigger validation again" needs.
		return l.settle(ctx, delivery.RunStateCancelled, "")
	}

	hist, err := l.readValidationHistory(ctx)
	if err != nil {
		return l.result(), err
	}
	l.validationAttempts, l.lastReportDigest = hist.Attempts, hist.Digest

	issue, err := l.ensureValidationIssue(ctx)
	if err != nil {
		return l.result(), err
	}
	if issue == 0 {
		// No acceptance oracle — nothing to validate, which is itself a verdict. It
		// belongs to no cycle: none was opened.
		if verr := l.setVerdict(ctx, noCycle, delivery.ValidationVerdictSkipped, ""); verr != nil {
			return l.result(), verr
		}
		return l.settle(ctx, delivery.RunStateSucceeded, "")
	}
	l.st.ValidationIssue = issue

	for dispatches := 0; ; dispatches++ {
		if reason := budgetRefusal(delivery.CycleKindValidation,
			l.st.CyclesTotal, l.st.FixCycles, l.st.ConflictCycles, l.st.CycleCeiling); reason != "" {
			return l.settleJudged(ctx, delivery.RunStateFailed, reason)
		}
		if err := l.bump(ctx, delivery.RunBudgetValidationCycles); err != nil {
			return l.result(), err
		}
		landed, res, err := l.agentStage(ctx, delivery.CycleKindValidation, issue)
		if err != nil {
			return l.result(), err
		}
		if !landed {
			state, reason := stateForUnlandedValidation(res)
			return l.settleJudged(ctx, state, reason)
		}

		out, err := l.readVerdict(ctx)
		if err != nil {
			return l.result(), err
		}
		// The verdict and its digest are ONE write, against the cycle that produced
		// them — see SetValidationVerdict, whose write-once fence is what makes the
		// pairing mandatory rather than tidy.
		if verr := l.setVerdict(ctx, l.cycleID, out.Verdict, out.Digest); verr != nil {
			return l.result(), verr
		}

		// Which verdicts are fatal, and under which reason, is delivery's to say —
		// one definition so the supervisor and the read model cannot disagree about
		// whether a version stood. `failed` is a real assertion loss; `unreported`
		// is an agent that merged a pull request and delivered no report at its own
		// merge commit. `partial` and `inconclusive` are honest reports of
		// incomplete evidence, not defects, so they settle the run green.
		reason, fatal := delivery.ValidationVerdictFailsRun(out.Verdict)
		if !fatal {
			return l.settleJudged(ctx, delivery.RunStateSucceeded, "")
		}

		// `unreported` is the one failure this workflow can remedy itself, and the
		// only one it must: nothing was deployed, no criterion asserted, and no
		// issue anybody could file would change the answer — the agent simply did
		// not commit a report. Another dispatch is the whole remedy, so it happens
		// HERE rather than by settling and hoping something restarts the run.
		// Bounded, because an agent that ignored the report contract twice will
		// ignore it a third time.
		if out.Verdict == delivery.ValidationVerdictUnreported && dispatches+1 < maxUnreportedDispatches {
			workflow.GetLogger(ctx).Warn("validation agent merged without a report — re-dispatching",
				"validationIssue", issue, "dispatch", dispatches+1)
			continue
		}

		// A repeat that reached the SAME answer learned nothing: the criteria, their
		// outcomes and their failure messages all digest identically, so the repair
		// did not move the system and another attempt would only produce this report
		// a third time. Stop here rather than filing repair work nobody can act on.
		if l.lastReportDigest != "" && out.Digest == l.lastReportDigest {
			workflow.GetLogger(ctx).Info("validation reached the same answer as the last attempt — stopping",
				"validationIssue", issue, "attempt", l.validationAttempts)
			return l.settleJudged(ctx, delivery.RunStateFailed, reason)
		}

		// Out of attempts, so this verdict is the version's answer. Settling BEFORE
		// the mint is what makes a one-attempt allowance a pure re-check: no repair
		// work is filed and nothing is rebuilt.
		if l.validationAttempts >= l.maxValidationAttempts {
			return l.settleJudged(ctx, delivery.RunStateFailed, reason)
		}

		if out.Verdict == delivery.ValidationVerdictFailed {
			if _, merr := l.mintRepairIssues(ctx, issue); merr != nil {
				return l.result(), merr
			}
		}
		return l.settleJudged(ctx, delivery.RunStateFailed, reason)
	}
}

// stateForUnlandedValidation maps an agent stage that never landed onto the run's
// terminal state and reason. Every one of the four is an existing failure class,
// which is the point: a validation run invents no new way to end.
//
// A conflict is the odd one and is reported as the conflict chain running out,
// because from this workflow's position it has: the event plane has already minted
// the conflict issue naming the branch, and that issue is ordinary work in the
// milestone for a task run to pick up — but nothing here can rebase a branch, so
// there is no second attempt to make.
func stateForUnlandedValidation(res cycleResult) (state, reason string) {
	switch res {
	case cycleCancelled:
		return delivery.RunStateCancelled, ""
	case cycleQuotaBlocked:
		return delivery.RunStateBlocked, delivery.RunReasonAgentQuotaBlocked
	case cyclePublisherCredentials:
		return delivery.RunStateBlocked, delivery.RunReasonPublisherCredentials
	case cycleConflict:
		return delivery.RunStateFailed, delivery.RunReasonConflictBudget
	default:
		return delivery.RunStateFailed, delivery.RunReasonRedispatchBudget
	}
}

// settleJudged closes the version's validation task and then settles the run.
//
// The close happens on EVERY ending, verdict or no verdict — including the agent
// dying through the whole re-dispatch budget, and including a cancel that arrived
// after the task was adopted. That is not tidiness, it is the loop's only
// termination guarantee: the reconcile sweep starts a validation run BECAUSE an
// open `validation`-kind issue exists, so a run that gave up and left the task
// open would be restarted within a tick, give up again, and keep doing that
// forever. Nothing outside the workflow can repair a dead dispatch, so nothing
// outside it can break that cycle.
//
// What the platform is left with is a version that is deployed and unjudged. That
// is honest — no verdict was reached, and none is claimed — and a person
// re-triggers validation when they want the answer.
//
// It is also why the pull-request body says `Validates #N` rather than a GitHub
// closing keyword (see eventcore/resolves.go): the platform owns this issue's
// lifecycle, and two owners would race on every attempt.
func (l *loop) settleJudged(ctx workflow.Context, state, reason string) (RunResult, error) {
	if err := l.closeValidationIssue(ctx); err != nil {
		return l.result(), err
	}
	return l.settle(ctx, state, reason)
}

// ---- validation activity calls ---------------------------------------------

// noCycle is the cycle id for a verdict that belongs to no cycle. Named rather
// than written as a bare "" at the call sites, because which verdicts have a
// cycle behind them is the whole point of the argument.
const noCycle = ""

func (l *loop) readValidationHistory(ctx workflow.Context) (ValidationHistory, error) {
	var out ValidationHistory
	err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).ReadValidationHistory,
		ValidationHistoryInput{
			OrgID:           l.in.OrgID,
			ProjectID:       l.in.ProjectID,
			MilestoneNumber: l.in.MilestoneNumber,
			RunID:           l.in.RunID,
		}).Get(ctx, &out)
	return out, err
}

func (l *loop) ensureValidationIssue(ctx workflow.Context) (int, error) {
	var issue int
	err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).EnsureValidationIssue, l.milestoneRef()).Get(ctx, &issue)
	return issue, err
}

func (l *loop) closeValidationIssue(ctx workflow.Context) error {
	if l.st.ValidationIssue == 0 {
		return nil
	}
	return workflow.ExecuteActivity(activityCtx(ctx), (*Activities).CloseValidationIssue,
		CloseValidationIssueInput{
			OrgID:     l.in.OrgID,
			ProjectID: l.in.ProjectID,
			Issue:     l.st.ValidationIssue,
			Verdict:   l.st.ValidationVerdict,
		}).Get(ctx, nil)
}

// setVerdict records the verdict, its digest and the issue that produced it in
// one write: on the run always, and on a cycle only when that cycle is the
// attempt the verdict came from.
//
// cycleID is a PARAMETER rather than a read of l.cycleID, because the loop's
// current cycle is not always the verdict's source. `skipped` is decided where
// there is no validation cycle at all, and writing it against whatever cycle
// happened to be last would contradict RunCycle.ValidationVerdict — documented as
// empty on every other kind — permanently, because the cycle write is write-once.
//
// The issue is persisted because it otherwise lives only here, in workflow state —
// so once Temporal retention lapses a settled run would carry a verdict with no
// way back to the criteria, the pull request, or the runner's own summary.
func (l *loop) setVerdict(ctx workflow.Context, cycleID, verdict, digest string) error {
	if err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).SetValidationVerdict,
		SetValidationVerdictInput{
			RunID: l.in.RunID, CycleID: cycleID, Verdict: verdict, Digest: digest, Issue: l.st.ValidationIssue,
		}).Get(ctx, nil); err != nil {
		return err
	}
	l.st.ValidationVerdict = verdict
	return nil
}

// readVerdict reads the report the validation cycle just merged, pinned to that
// cycle's own merge commit (l.mergeSHA, learned from the polled cycle facts).
//
// The pin is load-bearing, not defensive: the report lives at ONE fixed path that
// every run overwrites, so a read of the branch tip returns the newest run's
// results regardless of which run is asking — and a run whose agent shipped no
// report at all would inherit its predecessor's and be handed a confidently wrong
// verdict.
func (l *loop) readVerdict(ctx workflow.Context) (ValidationOutcome, error) {
	var out ValidationOutcome
	err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).ReadValidationVerdict,
		ValidationReportRef{OrgID: l.in.OrgID, ProjectID: l.in.ProjectID, At: l.mergeSHA}).Get(ctx, &out)
	return out, err
}

// mintRepairIssues files ONE issue per failed criterion from the attempt whose
// report was just read, at the same pinned commit.
//
// One per criterion and never one omnibus issue: the no-progress rule compares
// working-set SIZES, so repairing two of three failures has to read as progress.
// A single issue holding three failures could only be open or closed.
func (l *loop) mintRepairIssues(ctx workflow.Context, issue int) ([]int, error) {
	var filed []int
	err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).MintValidationRepairIssues,
		MintValidationRepairIssuesInput{
			OrgID:           l.in.OrgID,
			ProjectID:       l.in.ProjectID,
			MilestoneNumber: l.in.MilestoneNumber,
			At:              l.mergeSHA,
			CycleID:         l.cycleID,
		}).Get(ctx, &filed)
	if err != nil {
		return nil, err
	}
	workflow.GetLogger(ctx).Info("validation attempt failed; filed repair work",
		"validationIssue", issue, "repairIssues", filed, "attempt", l.validationAttempts)
	return filed, nil
}
