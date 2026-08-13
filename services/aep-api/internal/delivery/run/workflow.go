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

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// Cadences. Only two of them are load-bearing.
const (
	// activityTimeout bounds one activity call. Every activity is a single
	// GitHub, OpenChoreo or database round trip.
	activityTimeout = 2 * time.Minute

	// waitPollInterval re-reads the milestone while the run is WAITING. The wait
	// itself stays unbounded — this timer never ends it, it only re-derives the
	// predicate, so a lost `issues.closed` delivery costs ten minutes of latency
	// instead of stranding the run behind a gate that is already resolved.
	waitPollInterval = 10 * time.Minute

	// buildPollInterval re-reads the cycle's builds. Same role: the build
	// terminals arrive as signals, and this is what makes a lost one survivable.
	buildPollInterval = time.Minute

	// cycleLandingTimeout is how long ONE dispatch has to land a merged pull
	// request before the supervisor calls it agent death and spends a
	// re-dispatch. It is the only deadline in the loop, and it exists because
	// "the agent died" (including a Job that exited without opening a pull
	// request) is a named failure class with a named budget.
	cycleLandingTimeout = 2 * time.Hour

	// traitSyncTimeout bounds the WHOLE managed-API trait sync including its
	// retries. It is bounded rather than left to Temporal's unlimited default
	// because the sync is a convergence step, not a step the version depends on:
	// a cycle must not hang on it. See syncAPITraits for what happens when it
	// runs out.
	traitSyncTimeout = 5 * time.Minute
)

// RunInput starts a supervisor over one milestone. Everything in it is already
// decided by the caller: the run row exists, the milestone exists, and the
// ceiling is snapshotted so a config change cannot retroactively fail a live
// run.
type RunInput struct {
	RunID           string `json:"runId"`
	OrgID           string `json:"orgId"`
	ProjectID       string `json:"projectId"`
	MilestoneNumber int    `json:"milestoneNumber"`
	MilestoneTitle  string `json:"milestoneTitle"`
	Origin          string `json:"origin"`
	CycleCeiling    int    `json:"cycleCeiling,omitempty"`
	// ValidationAttempts pins how many times this run may validate. Zero means the
	// platform default, and it MUST: a workflow input lives in Temporal history,
	// so an execution started before this field existed replays with the zero
	// value. An int that falls back to the default replays identically; a bool
	// would have flipped behaviour under every live run.
	ValidationAttempts int `json:"validationAttempts,omitempty"`
}

// RunResult is the run's outcome, mirroring what was written to the run row.
type RunResult struct {
	State             string `json:"state"`
	TerminalReason    string `json:"terminalReason,omitempty"`
	ValidationVerdict string `json:"validationVerdict,omitempty"`
	Cycles            int    `json:"cycles"`
}

// MilestoneRunWorkflow is the run supervisor: work the open issues in one
// milestone until the milestone settles.
//
// It never returns an error for a run that reached a decision — a failed run is
// a SUCCEEDED workflow carrying a terminal reason, because "the increment could
// not be delivered" is an outcome the platform records, not a crash. A returned
// error means the supervisor itself could not function.
func MilestoneRunWorkflow(ctx workflow.Context, in RunInput) (RunResult, error) {
	l := newLoop(ctx, in)
	if err := workflow.SetQueryHandler(ctx, delivery.QueryRunStatus, func() (delivery.RunStatus, error) {
		return l.st, nil
	}); err != nil {
		return RunResult{}, err
	}
	return l.run(ctx)
}

// loop is the run's whole in-workflow state. It is the authority on the
// budgets: they are counted here, deterministically, and written outwards to
// the run row for the read model — never read back, because a replay must
// reproduce the same decisions without a database.
type loop struct {
	in RunInput
	st delivery.RunStatus

	cancel   workflow.ReceiveChannel
	workable workflow.ReceiveChannel
	merged   workflow.ReceiveChannel
	builds   workflow.ReceiveChannel
	conflict workflow.ReceiveChannel

	// lastResult is what the previous cycle produced — it selects the next
	// cycle's kind and feeds the no-progress rule.
	lastResult cycleResult
	// workBefore is the working-set size when the previous cycle was dispatched.
	workBefore int

	// prNumber / mergeSHA are the current cycle's landing, read from the cycle
	// record rather than from the signal that announced it.
	prNumber int
	mergeSHA string
	// cycleID is the current cycle's record id. Surfaced on the loop because the
	// verdict write lands after runCycle has returned.
	cycleID string

	// validationAttempts counts validation cycles this run has opened, bounded by
	// maxValidationAttempts. It replaced a `validationDone bool`: once a
	// failed validation is repairable, "has it validated yet" stopped being the
	// question and "how many times" started.
	validationAttempts int
	// maxValidationAttempts is this run's allowance, resolved once at start from
	// the input so both places that check it read the same number. One attempt is
	// what makes a revalidation a pure re-check: it is spent at the first fatal
	// verdict, which settles the run before the loop reaches the mint.
	maxValidationAttempts int
	// lastReportDigest fingerprints the previous attempt's report. A repeat whose
	// report digests the same learned nothing, so the loop stops instead of
	// spending the rest of the budget on the same answer.
	lastReportDigest string
}

func newLoop(ctx workflow.Context, in RunInput) *loop {
	ceiling := in.CycleCeiling
	if ceiling <= 0 {
		ceiling = delivery.RunDefaultCycleCeiling
	}
	// Same <=0 fallback as the ceiling, and load-bearing for the same reason: a
	// workflow started before the field existed replays with the zero value, so
	// zero has to mean the default rather than "no attempts".
	attempts := in.ValidationAttempts
	if attempts <= 0 {
		attempts = delivery.RunMaxValidationAttempts
	}
	return &loop{
		in:                    in,
		maxValidationAttempts: attempts,
		cancel:                workflow.GetSignalChannel(ctx, delivery.SigRunCancel),
		workable:              workflow.GetSignalChannel(ctx, delivery.SigRunWorkable),
		merged:                workflow.GetSignalChannel(ctx, delivery.SigRunPRMerged),
		builds:                workflow.GetSignalChannel(ctx, delivery.SigRunBuildTerminal),
		conflict:              workflow.GetSignalChannel(ctx, delivery.SigRunConflict),
		st: delivery.RunStatus{
			RunID:           in.RunID,
			MilestoneNumber: in.MilestoneNumber,
			Origin:          in.Origin,
			State:           delivery.RunStateWaiting,
			Phase:           delivery.RunPhaseWaiting,
			CycleCeiling:    ceiling,
		},
	}
}

// run is the cycle-boundary loop. Every pass begins by polling GROUND TRUTH —
// the milestone itself — and every decision below is made from that poll and
// the workflow's own counters. No branch here trusts a signal's payload.
func (l *loop) run(ctx workflow.Context) (RunResult, error) {
	for {
		if l.cancelRequested() {
			return l.settle(ctx, delivery.RunStateCancelled, "")
		}

		snap, err := l.pollMilestone(ctx)
		if err != nil {
			return l.result(), err
		}

		// SETTLE comes first, before the gate check: a stray gate holds dispatch,
		// and with an empty working set there is nothing to dispatch, so it holds
		// nothing.
		if snap.Work == 0 {
			settled, res, err := l.onEmptyWorkingSet(ctx)
			if settled || err != nil {
				return res, err
			}
			continue
		}

		if noProgress(l.lastResult, l.workBefore, snap.Work) {
			return l.settle(ctx, delivery.RunStateFailed, delivery.RunReasonNoProgress)
		}

		if !Dispatchable(snap) {
			// An open gate is a deliberate human brake. Park in the unbounded wait
			// — cancel is its only expiry — and re-derive when anything happens.
			cancelled, perr := l.park(ctx)
			if perr != nil {
				return l.result(), perr
			}
			if cancelled {
				return l.settle(ctx, delivery.RunStateCancelled, "")
			}
			continue
		}

		kind := nextCycleKind(l.lastResult)
		if reason := budgetRefusal(kind, l.st.CyclesTotal, l.st.FixCycles, l.st.ConflictCycles, l.st.CycleCeiling); reason != "" {
			return l.settle(ctx, delivery.RunStateFailed, reason)
		}

		l.workBefore = snap.Work
		res, err := l.runCycle(ctx, kind, 0)
		if err != nil {
			return l.result(), err
		}
		switch res {
		case cycleCancelled:
			return l.settle(ctx, delivery.RunStateCancelled, "")
		case cycleAgentDead:
			return l.settle(ctx, delivery.RunStateFailed, delivery.RunReasonRedispatchBudget)
		case cycleQuotaBlocked:
			return l.settle(ctx, delivery.RunStateBlocked, delivery.RunReasonAgentQuotaBlocked)
		}
		l.lastResult = res
	}
}

// onEmptyWorkingSet decides what an exhausted milestone means. Four different
// things, in this order:
//
//  1. Nothing has EVER been dispatched — there is no increment to call
//     delivered, so the run waits rather than settling. See below.
//  2. The last cycle ended badly and NOTHING came back to recover it. The
//     recovery issue the event plane should have minted is not there, so the
//     run cannot proceed and fails naming the budget that ran out.
//  3. A spec run at deployed-green with no validation yet — mint the validation
//     issue and work it with a fresh dispatch of the same loop.
//  4. Otherwise the version is delivered.
//
// Case 1 is the one that must NOT settle. "Empty working set" means delivered
// only in contrast to work this run actually did; with zero cycles behind it
// the same reading is indistinguishable from a milestone whose issues have not
// been minted yet — the plan path admits the run row BEFORE its planning turn
// (so the spec mutex is armed across it), so a poll can legitimately land in
// that window and see nothing. Settling there closes a version nobody built.
// §7's wait is unbounded and cancel is its only expiry, so the run parks and
// re-derives on every `issues` webhook and at the poll backstop.
//
// What ends such a run, then: work arriving (it dispatches), a human cancelling
// (§7's only expiry), or — when the planning turn itself failed and no issue is
// ever coming — the PLAN PATH settling the row it armed with
// RunReasonPlanFailed. Those two cannot race: the plan path starts the
// supervisor only after planning returns, so a run that failed to plan has no
// workflow behind it, and a workflow that exists is past planning. The
// repository's non-terminal guard on Settle is the backstop if that ordering
// ever changes — the first settle wins, and this loop never issues one here.
//
// It returns settled=false for the two cases that continue: the zero-cycle wait
// above, and a validation cycle that passed — after which the boundary is
// re-entered so anything adopted while validation ran is picked up.
//
// A REVALIDATION is the one run that must skip case 1. Its milestone is a
// version that already shipped, so an empty working set is not an ambiguous
// reading of a milestone mid-plan — it is the expected state, and the whole
// reason the run exists is to go straight to validation. Parking it would be a
// run that waits forever for work nobody is going to file.
func (l *loop) onEmptyWorkingSet(ctx workflow.Context) (settled bool, res RunResult, err error) {
	if l.st.CyclesTotal == 0 && l.in.Origin != delivery.RunOriginRevalidate {
		cancelled, perr := l.park(ctx)
		if perr != nil {
			return true, l.result(), perr
		}
		if cancelled {
			res, err = l.settle(ctx, delivery.RunStateCancelled, "")
			return true, res, err
		}
		return false, RunResult{}, nil
	}
	switch l.lastResult {
	case cycleRed:
		// The build is red, its one automatic re-trigger is spent, and no fix
		// issue joined the milestone. There is nothing left that could make it
		// green.
		res, err = l.settle(ctx, delivery.RunStateFailed, delivery.RunReasonBuildRetriggerBudget)
		return true, res, err
	case cycleConflict:
		// Same shape: the pull request would not merge and no conflict issue
		// arrived to rebase it.
		res, err = l.settle(ctx, delivery.RunStateFailed, delivery.RunReasonConflictBudget)
		return true, res, err
	}

	// Which origins validate is delivery's to say (RunValidates): the spec build
	// that delivers a version, and the revalidation that exists to ask its criteria
	// again. An incident run fixes one thing in an already-validated version, and
	// re-validating the whole system for it would price every incident like a
	// release.
	//
	// Re-entry is what makes the repeat loop possible: this boundary is re-entered
	// after every repair cycle, and validation runs again while the run has attempts
	// left. Spending them is not settled here — runValidation settles on the verdict
	// it is already holding, so the reason names the failure rather than the budget.
	if !delivery.RunValidates(l.in.Origin) {
		res, err = l.settle(ctx, delivery.RunStateSucceeded, "")
		return true, res, err
	}
	return l.runValidation(ctx)
}

// runValidation mints the validation issue at deployed-green and works it with
// a fresh dispatch of the same loop — then, when the attempt failed and the run
// has attempts left, files the repair work and hands the boundary back so an
// ordinary coding cycle can fix it.
//
// Minting HERE, and not at plan time, is what makes the coverage honest:
// mid-run adoption postpones deployed-green by construction, so by the time
// this runs the validation issue covers everything the run landed.
//
// The two recoverable verdicts take different routes to the same boundary, and
// neither needs a branch anywhere else in the loop:
//
//   - `failed` is a defect. One issue per failed criterion joins the working set,
//     so the next boundary poll dispatches an ordinary coding cycle, its builds go
//     green, the working set empties, and control arrives back here.
//   - `unreported` is an agent that merged without a report. Nothing is wrong with
//     the software, so nothing is minted — the working set stays empty and the
//     boundary bounces straight back into this function, which is exactly the
//     remedy: dispatch validation again. Note this is NOT RunMaxRedispatchPerCycle
//     territory; that budget answers an agent that never landed a pull request,
//     and this one did.
func (l *loop) runValidation(ctx workflow.Context) (settled bool, res RunResult, err error) {
	// Is validation still this run's business? Answered from the verdict already
	// held, which is what the boundary cannot decide for itself.
	//
	// This is the guard that replaced a `validationDone bool`. The flag carried two
	// meanings at once — "validation has run" and "validation is finished" — and
	// they came apart the moment a failure could be repaired. Only a FATAL verdict
	// leaves anything to do.
	if l.st.ValidationVerdict != "" {
		reason, fatal := delivery.ValidationVerdictFailsRun(l.st.ValidationVerdict)
		if !fatal {
			// Already answered, and the answer stands. Re-entering the boundary after a
			// non-fatal verdict exists to pick up work adopted while validation ran; an
			// empty working set on the way back means the version is delivered.
			res, err = l.settle(ctx, delivery.RunStateSucceeded, "")
			return true, res, err
		}
		if l.validationAttempts >= l.maxValidationAttempts {
			// Out of attempts: accept the answer this run keeps getting. Settling on the
			// HELD verdict rather than on a budget of its own is why `validation-failed`
			// now means "still failing after every attempt", and why the repeat loop
			// needed no new terminal reason.
			res, err = l.settle(ctx, delivery.RunStateFailed, reason)
			return true, res, err
		}
		// Fatal, with attempts left: repair landed (or there was nothing to repair),
		// so try again below.
	}

	issue, err := l.ensureValidationIssue(ctx)
	if err != nil {
		return true, l.result(), err
	}
	if issue == 0 {
		// No acceptance oracle — nothing to validate, which is itself a verdict. It
		// belongs to no cycle: none has been opened, and l.cycleID is still the last
		// coding cycle's.
		if err := l.setVerdict(ctx, noCycle, delivery.ValidationVerdictSkipped); err != nil {
			return true, l.result(), err
		}
		res, err = l.settle(ctx, delivery.RunStateSucceeded, "")
		return true, res, err
	}
	l.st.ValidationIssue = issue

	if reason := budgetRefusal(delivery.CycleKindValidation, l.st.CyclesTotal, l.st.FixCycles, l.st.ConflictCycles, l.st.CycleCeiling); reason != "" {
		res, err = l.settle(ctx, delivery.RunStateFailed, reason)
		return true, res, err
	}
	l.validationAttempts++
	if err := l.bump(ctx, delivery.RunBudgetValidationCycles); err != nil {
		return true, l.result(), err
	}
	outcome, err := l.runCycle(ctx, delivery.CycleKindValidation, issue)
	if err != nil {
		return true, l.result(), err
	}
	switch outcome {
	case cycleCancelled:
		res, err = l.settle(ctx, delivery.RunStateCancelled, "")
		return true, res, err
	case cycleAgentDead:
		res, err = l.settle(ctx, delivery.RunStateFailed, delivery.RunReasonRedispatchBudget)
		return true, res, err
	case cycleQuotaBlocked:
		res, err = l.settle(ctx, delivery.RunStateBlocked, delivery.RunReasonAgentQuotaBlocked)
		return true, res, err
	}

	out, err := l.readVerdict(ctx)
	if err != nil {
		return true, l.result(), err
	}
	// The one verdict a cycle owns: l.cycleID is the validation cycle runCycle just
	// closed, and out.Verdict was derived from the report at its own merge commit.
	if err := l.setVerdict(ctx, l.cycleID, out.Verdict); err != nil {
		return true, l.result(), err
	}
	// Which verdicts are fatal, and under which reason, is delivery's to say — one
	// definition so the supervisor and the read model cannot disagree about whether
	// a version settled green. `failed` is a real assertion loss; `unreported` is
	// an agent that merged a pull request and delivered no report at its own merge
	// commit. `partial` and `inconclusive` are honest reports of incomplete
	// evidence, not defects, so they fall through and settle the run green.
	reason, fatal := delivery.ValidationVerdictFailsRun(out.Verdict)
	if !fatal {
		return l.reenterAfterValidation()
	}

	// A repeat that reached the SAME answer learned nothing: the criteria, their
	// outcomes and their failure messages all digest identically, so the repair did
	// not move the system and another attempt would only produce this report a third
	// time. Stop here rather than spending the rest of the budget.
	if l.lastReportDigest != "" && out.Digest == l.lastReportDigest {
		res, err = l.settle(ctx, delivery.RunStateFailed, reason)
		return true, res, err
	}
	l.lastReportDigest = out.Digest

	// Out of attempts, so this verdict is the run's answer. It also settles BEFORE
	// the mint below, which is what makes a one-attempt run a pure re-check: the
	// allowance is spent by the first fatal verdict, so no repair work is filed and
	// no coding cycle follows.
	if l.validationAttempts >= l.maxValidationAttempts {
		res, err = l.settle(ctx, delivery.RunStateFailed, reason)
		return true, res, err
	}

	// Attempts remain, so the failure becomes work. `unreported` mints nothing on
	// purpose: the software is not what failed, and an empty working set is what
	// sends the boundary straight back here to validate again.
	if out.Verdict == delivery.ValidationVerdictFailed {
		filed, merr := l.mintRepairIssues(ctx, issue)
		if merr != nil {
			return true, l.result(), merr
		}
		if len(filed) == 0 {
			// `failed` with nothing to file means the report named a failure the
			// minter could not turn into work. Repairing is then impossible and
			// another attempt would be the same dispatch, so settle honestly.
			res, err = l.settle(ctx, delivery.RunStateFailed, reason)
			return true, res, err
		}
		// PARK before handing the boundary back. GitHub's issue index lags a write
		// (validation_issues.go records a read-back that answered "no validation
		// issue" for one this platform had just filed), so the very next
		// pollMilestone can legitimately see an empty working set — which would fall
		// into onEmptyWorkingSet and settle this run SUCCEEDED over a failure it just
		// filed repair work for. Signal channels buffer, so parking after the mint
		// cannot miss the `issues` webhook that wakes it.
		cancelled, perr := l.park(ctx)
		if perr != nil {
			return true, l.result(), perr
		}
		if cancelled {
			res, err = l.settle(ctx, delivery.RunStateCancelled, "")
			return true, res, err
		}
	}
	return l.reenterAfterValidation()
}

// reenterAfterValidation hands control back to the cycle boundary with the loop's
// progress state cleared.
//
// A validation cycle closes no working-set issue, so the no-progress rule must not
// see it: comparing the working set before and after would read a cycle that was
// never about the working set as a cycle that achieved nothing. Clearing also picks
// up anything adopted while validation was running.
func (l *loop) reenterAfterValidation() (bool, RunResult, error) {
	l.lastResult = cycleNone
	l.workBefore = 0
	return false, RunResult{}, nil
}

// settle ends the run. The milestone close is display only and happens on
// success alone: a failed or cancelled increment stays open, because the way
// forward from it is more work in the same version.
func (l *loop) settle(ctx workflow.Context, state, reason string) (RunResult, error) {
	l.st.Phase = delivery.RunPhaseSettling
	// Cancel stops the agent at the HTTP surface (runread.CycleReaper →
	// DeleteComponent), not here: a Temporal-durable stop was tried on main and
	// dropped in favour of that best-effort reap (phase-08 Cancel B1).
	if state == delivery.RunStateSucceeded {
		if l.st.ValidationVerdict == "" {
			// "The run finished and did not validate" is an honest verdict; an
			// empty one would read as "not yet". An empty verdict here means no
			// validation cycle ever produced one, so it belongs to no cycle.
			if err := l.setVerdict(ctx, noCycle, delivery.ValidationVerdictSkipped); err != nil {
				return l.result(), err
			}
		}
		if err := l.closeMilestone(ctx); err != nil {
			return l.result(), err
		}
	}
	if err := l.settleRun(ctx, state, reason); err != nil {
		return l.result(), err
	}
	l.st.State = state
	l.st.TerminalReason = reason
	l.st.CycleKind = ""
	l.st.CycleAttempt = 0
	return l.result(), nil
}

func (l *loop) result() RunResult {
	return RunResult{
		State:             l.st.State,
		TerminalReason:    l.st.TerminalReason,
		ValidationVerdict: l.st.ValidationVerdict,
		Cycles:            l.st.CyclesTotal,
	}
}

// park puts the run into the WAITING state — row and live status together —
// and blocks there until something worth re-deriving happens. It returns true
// only for cancel.
//
// Both of the loop's holds go through it, because they are the same state seen
// from two sides: a gate holding the next dispatch, and a milestone that has
// not produced any work yet. Neither is a run that is finished.
func (l *loop) park(ctx workflow.Context) (cancelled bool, err error) {
	if serr := l.setState(ctx, delivery.RunStateWaiting); serr != nil {
		return false, serr
	}
	l.st.Phase = delivery.RunPhaseWaiting
	l.st.CycleKind = ""
	return l.await(ctx), nil
}

// await parks the run in the unbounded wait. It returns true only for cancel —
// every other wake-up is a reason to re-derive the predicate from ground truth,
// which is why the signals are drained without being read.
func (l *loop) await(ctx workflow.Context) (cancelled bool) {
	timerCtx, stop := workflow.WithCancel(ctx)
	defer stop()

	sel := workflow.NewSelector(ctx)
	sel.AddReceive(l.cancel, func(c workflow.ReceiveChannel, _ bool) {
		c.Receive(ctx, nil)
		cancelled = true
	})
	for _, ch := range []workflow.ReceiveChannel{l.workable, l.merged, l.builds, l.conflict} {
		sel.AddReceive(ch, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil) })
	}
	sel.AddFuture(workflow.NewTimer(timerCtx, waitPollInterval), func(workflow.Future) {})
	sel.Select(ctx)
	return cancelled
}

// cancelRequested drains a pending cancel without blocking. Checked at every
// boundary so a cancel that arrived mid-cycle is honoured at the first safe
// point rather than after another dispatch.
func (l *loop) cancelRequested() bool { return l.cancel.ReceiveAsync(nil) }

// ---- activity calls --------------------------------------------------------

// activityCtx is the options every activity but the dispatch runs under.
//
// The retry policy is Temporal's default — unbounded, with backoff. That is
// deliberate: none of these activities has a "give up" answer that would be
// better than waiting. A supervisor that cannot reach GitHub should stall
// visibly, not settle a run on a network blip.
//
// Unbounded applies to the blips only. A failure that repeating cannot change —
// the project deleted underneath the run, a rejected credential — is marked
// non-retryable by the activity itself (errors.go) and fails on its first
// attempt, so the policy here never gets to spend a lifetime on it.
func activityCtx(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: activityTimeout,
	})
}

func (l *loop) pollMilestone(ctx workflow.Context) (MilestoneSnapshot, error) {
	var out MilestoneSnapshot
	err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).PollMilestone, l.milestoneRef()).Get(ctx, &out)
	return out, err
}

func (l *loop) milestoneRef() MilestoneRef {
	return MilestoneRef{OrgID: l.in.OrgID, ProjectID: l.in.ProjectID, MilestoneNumber: l.in.MilestoneNumber}
}

// setState moves the run row AND the live status together, so a query and the
// database can never disagree about whether the run is parked or working.
func (l *loop) setState(ctx workflow.Context, state string) error {
	if err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).SetRunState,
		SetRunStateInput{RunID: l.in.RunID, State: state}).Get(ctx, nil); err != nil {
		return err
	}
	l.st.State = state
	return nil
}

func (l *loop) settleRun(ctx workflow.Context, state, reason string) error {
	return workflow.ExecuteActivity(activityCtx(ctx), (*Activities).SettleRun,
		SettleRunInput{RunID: l.in.RunID, State: state, Reason: reason}).Get(ctx, nil)
}

func (l *loop) bump(ctx workflow.Context, counter delivery.RunBudget) error {
	return workflow.ExecuteActivity(activityCtx(ctx), (*Activities).BumpRunBudget,
		BumpRunBudgetInput{RunID: l.in.RunID, Counter: string(counter)}).Get(ctx, nil)
}

// noCycle is the cycle id for a verdict that belongs to no cycle. Named rather
// than written as a bare "" at the call sites, because which verdicts have a
// cycle behind them is the whole point of the argument.
const noCycle = ""

// setVerdict records the verdict and the issue that produced it in one write: on
// the run always, and on a cycle only when that cycle is the attempt the verdict
// came from.
//
// cycleID is a PARAMETER rather than a read of l.cycleID, because the loop's
// current cycle is not always the verdict's source. `skipped` is decided in two
// places that have no validation cycle at all — before one is opened, and in
// settle — and at both of them l.cycleID still holds the last CODING cycle's id.
// Writing there would contradict RunCycle.ValidationVerdict, documented as empty
// on every other kind, and the cycle write is write-once, so it would be permanent.
//
// The issue is persisted because it otherwise lives only here, in workflow state —
// so once Temporal retention lapses a settled run would carry a verdict with no way
// back to the criteria, the pull request, or the runner's own summary.
func (l *loop) setVerdict(ctx workflow.Context, cycleID, verdict string) error {
	if err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).SetValidationVerdict,
		SetValidationVerdictInput{
			RunID: l.in.RunID, CycleID: cycleID, Verdict: verdict, Issue: l.st.ValidationIssue,
		}).Get(ctx, nil); err != nil {
		return err
	}
	l.st.ValidationVerdict = verdict
	return nil
}

// mintRepairIssues files one issue per failed criterion from the attempt whose
// report was just read, at the same pinned commit.
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

func (l *loop) closeMilestone(ctx workflow.Context) error {
	return workflow.ExecuteActivity(activityCtx(ctx), (*Activities).CloseMilestone, l.milestoneRef()).Get(ctx, nil)
}

func (l *loop) ensureValidationIssue(ctx workflow.Context) (int, error) {
	var issue int
	err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).EnsureValidationIssue, l.milestoneRef()).Get(ctx, &issue)
	return issue, err
}

// readVerdict reads the report the validation cycle just merged, pinned to that
// cycle's own merge commit (l.mergeSHA, learned from the polled PR facts). Without
// the pin the read would follow the branch tip and a later run's report could
// answer for this one.
func (l *loop) readVerdict(ctx workflow.Context) (ValidationOutcome, error) {
	var out ValidationOutcome
	err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).ReadValidationVerdict,
		ValidationReportRef{OrgID: l.in.OrgID, ProjectID: l.in.ProjectID, At: l.mergeSHA}).Get(ctx, &out)
	return out, err
}

// dispatchActivityCtx runs the agent launch with retries OFF. A launch that did
// not happen is agent death, and the cycle's own re-dispatch budget is the
// answer to that — a Temporal retry on top would spend it invisibly.
func dispatchActivityCtx(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: activityTimeout,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
}

// traitSyncActivityCtx retries the managed-API trait sync under an overall
// deadline. Retries are wanted — the failures this sees are transient
// OpenChoreo round trips, and the previous owner of this write dropped them
// silently — but they are bounded, because no part of delivering the version
// depends on the answer.
func traitSyncActivityCtx(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    activityTimeout,
		ScheduleToCloseTimeout: traitSyncTimeout,
	})
}

// syncAPITraits converges the managed-API gateway policy for the project after
// a cycle's builds go green.
//
// It NEVER fails the cycle. The reason is not that the write is unimportant —
// an unset `jwtAuth` leaves a protected API's gateway passing every request
// through unauthenticated — but that failing here would not undo it: the
// component is already deployed and serving by the time this runs, so a red
// cycle would add noise without removing exposure. Only convergence removes it,
// which is why the outcome is logged loudly and left to be re-asserted.
//
// This is the interim trigger. It is coupled to THIS build rail, which is
// exactly how its predecessor died — the trait sync used to hang off the
// ExecWatcher's build terminal, and stopped firing the moment builds moved to
// this loop and stopped writing the execution rows that watcher reads. A
// rail-agnostic reconcile sweep is what makes the guarantee; this only makes it
// prompt.
func (l *loop) syncAPITraits(ctx workflow.Context) {
	err := workflow.ExecuteActivity(traitSyncActivityCtx(ctx), (*Activities).SyncAPITraits,
		ProjectRef{OrgID: l.in.OrgID, ProjectID: l.in.ProjectID}).Get(ctx, nil)
	if err != nil {
		workflow.GetLogger(ctx).Error(
			"managed-API trait sync did not converge; protected APIs in this project may be serving unauthenticated",
			"orgID", l.in.OrgID, "projectID", l.in.ProjectID, "error", err)
	}
}
