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

package delivery

// Where a version's validation stands — the ValidationState contract enum.
//
// Only the four LIFECYCLE values live here; the rest of the enum is the run's
// verdict verbatim (ValidationVerdict*), because a fold would have to discard
// `partial`, `inconclusive` and `unreported` at the one surface that needs
// them, and `completed` never said whether anything passed.
const (
	ValidationStageNone    = "none"
	ValidationStageRunning = "running"
	// ValidationStageAwaitingFix is a live run REPAIRING a failed validation: the
	// run holds a fatal verdict but has attempts left, and the work in flight is a
	// CODING cycle. It names the implementation rather than validation because
	// that is what is being fixed — rendering the bare `failed` verdict here would
	// read as terminal while the platform is actively resolving it.
	ValidationStageAwaitingFix = "awaiting-fix"
	// ValidationStageCancelled is a person STOPPING the judging: a validation run
	// settled cancelled before it recorded a verdict, so nothing will answer for
	// this version unless somebody re-asks. It is the one no-verdict state that
	// does NOT hold promotion, and the distinction is the whole reason it exists:
	// every other way to reach no verdict is an accident — a failed increment, an
	// agent that died — where refusing to promote an unjudged version is the safe
	// answer, and this one is a decision somebody already made.
	ValidationStageCancelled = "cancelled"
)

// ValidationStageFromRun answers a version's validation state from the run row
// alone, reporting decided=false when only the run's latest cycle can settle it.
//
// It lives HERE, beside the verdict vocabulary and RunValidates, because two
// surfaces ask it: the project status aggregate (deploy.validation) and the
// validation read model. Two copies would be two ideas of when a version counts
// as judged, and the pair would drift the first time the loop changed.
//
// The verdict is MIRRORED, not folded: it is the thing a reader wants to know,
// and folding it into a coarser word is how "completed" came to mean "passed"
// without saying so — it would discard partial, inconclusive and unreported
// entirely.
//
// A TERMINAL run with no verdict splits in two, and the split is the difference
// between a state that resolves and one that never will: judging that was
// CANCELLED is settled (`cancelled`), and everything else is a run that simply
// never got there (`none`, which promises a verdict is still coming).
//
// A verdict is final on a TERMINAL run, and on a live run when it is not one the
// loop repairs. A live run holding a repairable verdict is undecided: the
// verdict is mid-loop, so rendering it would tell a reader the version failed
// validation while the platform is repairing it and about to validate again.
// Which verdicts the loop repairs is ValidationVerdictFailsRun's to say, so this
// cannot drift from what the supervisor actually does.
//
// Undecided therefore means: a live run with no verdict yet, or one whose
// verdict is still repairable. Whether a validation cycle is in flight is not on
// this row — loop position is never a stored enum, because a fix or conflict
// cycle re-enters an earlier phase and a flat enum would lie mid-loop.
func ValidationStageFromRun(run *MilestoneRun) (state string, decided bool) {
	if run == nil {
		return ValidationStageNone, true
	}
	if IsTerminalRunState(run.State) {
		if run.ValidationVerdict != "" {
			return run.ValidationVerdict, true
		}
		// A cancelled VALIDATION run: somebody stopped the judging, so no verdict is
		// coming for this version and `none` — which promises one — would be a lie
		// that never resolves.
		//
		// The KIND guard is load-bearing rather than defensive. This function is
		// handed whatever the caller's selector returns, which is a validation-kind
		// run OR a fall-back to the dev run, so testing the state alone would also
		// catch a cancelled DEV run. That is an ABANDONED INCREMENT — the reconcile
		// sweep suppresses its whole milestone for exactly that reason — and
		// reporting it as "nothing left to wait for" would offer the version for
		// promotion, which is worse than the confusion this state exists to remove.
		if run.State == RunStateCancelled && RunValidates(run.Kind) {
			return ValidationStageCancelled, true
		}
		// Settled without ever recording a verdict: the run never reached validation.
		return ValidationStageNone, true
	}
	if run.ValidationVerdict != "" {
		if _, fatal := ValidationVerdictFailsRun(run.ValidationVerdict); !fatal {
			return run.ValidationVerdict, true
		}
	}
	return "", false
}

// ValidationStageWithCycle settles the two cases the run row alone cannot, from
// that run's LATEST cycle.
//
// Taking the cycle as an argument rather than reading it is what lets both
// callers share this: the status aggregate reads it only when it must (the
// conditional query is the whole point of the split), while the validation read
// model already holds every cycle it loaded. A function that read for itself
// would make the cheap caller pay the expensive caller's query.
//
// A nil cycle is a live run that has dispatched nothing yet, which has nothing
// to say about validation.
func ValidationStageWithCycle(run *MilestoneRun, latest *RunCycle) string {
	if latest != nil && latest.Kind == CycleKindValidation && latest.EndedAt == nil {
		return ValidationStageRunning
	}
	// A live run carrying a repairable verdict, whose current cycle is ordinary
	// work: this is the self-healing loop mid-flight. The verdict is real but not
	// final, and the cycle in flight is what will make it stale.
	if run != nil {
		if _, fatal := ValidationVerdictFailsRun(run.ValidationVerdict); fatal {
			return ValidationStageAwaitingFix
		}
	}
	// A live run whose current cycle is coding, fixing or resolving a conflict has
	// nothing to say about validation yet. Saying "validating" here was wrong for
	// most of every run's life.
	return ValidationStageNone
}

// NewestValidatingOnMilestone returns the newest run on ref's milestone that
// could have produced a verdict — which is ref itself unless something later
// re-judged that version.
//
// It exists because a version's answer and the version's BUILD can come from
// different rows: the dev run delivers it, and a revalidation started afterwards
// may hold a newer verdict for the very same milestone.
//
// The KIND filter is the load-bearing half, and it predates revalidation. A task
// run never validates, and `settle` stamps `skipped` on any succeeded run that
// never did — so the newest run on a milestone is routinely one whose verdict
// means "I was never asked". Returning it made a single adopted issue report a
// genuinely passed version as unvalidated (#423).
//
// rows must be newest-first, which is what the repository's list reads
// guarantee, so this is a scan and not a sort.
func NewestValidatingOnMilestone(rows []MilestoneRun, ref *MilestoneRun) *MilestoneRun {
	if ref == nil {
		return nil
	}
	for i := range rows {
		if rows[i].MilestoneNumber == ref.MilestoneNumber && RunValidates(rows[i].Kind) {
			return &rows[i]
		}
	}
	return ref
}

// NewestRunOfKindOnMilestone returns the newest run of one kind on a milestone,
// or nil. rows must be newest-first.
//
// It is the other half of picking the row that answers for a version. A verdict
// can only sit on a run that could have produced one, and a milestone
// accumulates rows that could not: a task run fixing one defect never judges the
// system, and `settle` stamps `skipped` on any succeeded run that never asked.
// Anchoring on the DEV run — the row that delivered the version — and then
// letting NewestValidatingOnMilestone prefer a later judgement is what keeps an
// adopted incident from answering for a version it never judged (#423).
func NewestRunOfKindOnMilestone(rows []MilestoneRun, milestoneNumber int, kind string) *MilestoneRun {
	for i := range rows {
		if rows[i].MilestoneNumber == milestoneNumber && rows[i].Kind == kind {
			return &rows[i]
		}
	}
	return nil
}

// AnsweringRunOnMilestone is the run whose verdict is a version's answer: the
// newest run that judged it, or the dev run that delivered it when nothing has.
//
// The two-step is the rule, and a surface that spelled it out for itself read
// the wrong row and hid a real verdict (#423). Callers that already hold the
// dev run compose the halves directly — the status aggregate does, since it
// reads that row for the build stage anyway — so this is the shorthand for
// everyone else, not a funnel.
func AnsweringRunOnMilestone(rows []MilestoneRun, milestoneNumber int) *MilestoneRun {
	dev := NewestRunOfKindOnMilestone(rows, milestoneNumber, RunKindDev)
	return NewestValidatingOnMilestone(rows, dev)
}

// DeployedRun returns the run that last finished delivering a version — the
// newest SUCCEEDED dev run, or nil when the project has never completed one.
//
// "Deployed" is a fact about runs rather than about the cluster: a running v2
// does not unseat a live v1, so the newest run is the wrong answer and only a
// succeeded one counts. It is the same rule `deploy.version` reports, and what
// a revalidation is allowed to judge — the runner resolves its endpoints from the
// cluster at request time, so any other version would be judged against code it
// never shipped.
//
// rows must be newest-first, which the repository's list reads guarantee.
func DeployedRun(rows []MilestoneRun) *MilestoneRun {
	for i := range rows {
		if rows[i].Kind == RunKindDev && rows[i].State == RunStateSucceeded {
			return &rows[i]
		}
	}
	return nil
}
