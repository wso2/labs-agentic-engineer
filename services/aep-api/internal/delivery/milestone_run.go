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

import (
	"errors"
	"time"
)

// ErrNoAdoptableMilestone is adoption's honest refusal: a bare issue joins the
// milestone of the version it is an incident against, and a project that has
// never started a spec build has no such version — there is nothing to attach
// it to, and a guess would file the incident against a version that does not
// exist.
//
// It lives in the kernel rather than in `eventcore` because the HTTP edge has
// to recognise it: the promote-from-issue leg of the SRE/RCA handoff surfaces
// it to a caller verbatim, and `task` may not import `eventcore` (the task ⊥
// run arch lock). A sentinel both packages already import is the only place a
// shared `errors.Is` can live.
//
// The message is written for a human because that is where it ends up.
var ErrNoAdoptableMilestone = errors.New("no version to adopt this issue into — build the project first")

// MilestoneRun origins, states, terminal reasons and validation verdicts
// (plain strings, matching the model convention — canonical values here, no
// separate enum package).
const (
	// RunOriginSpecBuild is a run started by the build click: the plan path cut
	// a v<N> tag, minted the milestone, and started the supervisor. At most one
	// non-terminal spec-build run exists per project (the mutex below).
	RunOriginSpecBuild = "spec-build"
	// RunOriginIncidentAdoption is a run started by adopting an issue into an
	// already-deployed version's milestone. Incident runs on other milestones
	// execute concurrently with each other and with a live spec run.
	RunOriginIncidentAdoption = "incident-adoption"
	// RunOriginRevalidate is a run started to ask a version's acceptance criteria
	// again, against the system already deployed. It is the only origin that
	// ENTERS THE LOOP AT VALIDATION rather than at the working set: its milestone
	// has no work left, so the boundary would otherwise park it forever.
	//
	// What it does after the verdict is the loop's ordinary behaviour, chosen by
	// the run's ValidationAttempts. One attempt reports and settles — the
	// allowance is spent before the loop reaches the point where it would file
	// repair work, so nothing is rebuilt. The default attempts give the full
	// repair chain: issues per failed criterion, a coding cycle, builds, and a
	// second validation.
	//
	// Deliberately OUTSIDE the spec-run mutex (its partial index names
	// `spec-build` alone): a revalidate re-judges a version that already shipped,
	// so holding up the next build for its duration would be wrong. The cost is
	// that the database does not serialise two of them, which is why the trigger
	// checks LiveRunForMilestone before admitting one.
	RunOriginRevalidate = "revalidate"

	// RunStatePlanning is the FILL WINDOW: the row has been admitted (so the
	// spec mutex is armed) but the milestone it works is still being written —
	// gates minted, then the planning turn streaming issues into GitHub. It is
	// bounded work the platform is actively doing, which is exactly what
	// separates it from RunStateWaiting: nothing is held, nobody is needed.
	//
	// Only the plan path writes it, and only at admission. The supervisor's
	// first pass leaves it — for running if it dispatches, for waiting if a gate
	// holds it — so no transition ever moves INTO planning.
	RunStatePlanning = "planning"
	// RunStateWaiting is the unbounded wait between cycles; cancel is its only
	// expiry. RunStateRunning covers a dispatched cycle. The loop oscillates
	// waiting ⇄ running until it settles.
	RunStateWaiting   = "waiting"
	RunStateRunning   = "running"
	RunStateSucceeded = "succeeded"
	RunStateFailed    = "failed"
	RunStateCancelled = "cancelled"
	// RunStateBlocked is terminal and is NOT a failure: the org has no agent
	// concurrency slot left, so the cycle was never launched. It is its own
	// state rather than a failure reason because the distinction is what the
	// user acts on — "wait or stop a run" versus "something went wrong" — and
	// because a failed run reads as the platform's fault.
	//
	// Terminal, so the run row releases the spec-run mutex and the user can
	// start the version again once a slot frees; the run is never resurrected
	// in place.
	RunStateBlocked = "blocked"

	// Terminal reasons. Each names exactly ONE failure class so the reason a run
	// stopped is never ambiguous. Empty while the run is non-terminal, and empty
	// on a succeeded run.
	RunReasonRedispatchBudget     = "redispatch-budget"
	RunReasonBuildRetriggerBudget = "build-retrigger-budget"
	// RunReasonDeployBudget — the cycle's components built, but a deployment
	// never reached Ready and no fix issue arrived to recover it.
	//
	// Its own class rather than a shade of build-retrigger-budget, because the
	// two send a human to different places: a red build is code that did not
	// compile, while this is code that compiled and would not run — a bad image,
	// an unrenderable trait, a missing dependency at runtime.
	RunReasonDeployBudget = "deploy-budget"
	RunReasonFixChainBudget       = "fix-chain-budget"
	RunReasonConflictBudget       = "conflict-budget"
	RunReasonNoProgress           = "no-progress"
	RunReasonCycleCeiling         = "cycle-ceiling"
	RunReasonValidationFailed     = "validation-failed"
	// RunReasonValidationUnreported is its own failure class, distinct from
	// validation-failed: the suite going red and the agent delivering no report at
	// all are different explanations, and a terminal reason exists to explain.
	//
	// It is a failure because the report is read at the validation cycle's OWN
	// merge commit, so an absent report is a hard fact about this run rather than
	// a propagation artifact — the agent merged a pull request and reported
	// nothing.
	RunReasonValidationUnreported = "validation-unreported"
	// RunReasonPlanFailed is the plan path's own failure class: the run row is
	// admitted BEFORE the planning turn (so the spec-run mutex is armed for the
	// whole of it), which means a planning turn that cannot finish must settle
	// the row it armed. Without it a failed plan would wedge the project behind
	// its own mutex until a human cancelled.
	RunReasonPlanFailed = "plan-failed"
	// RunReasonAgentQuotaBlocked explains RunStateBlocked: the entitlement gate
	// refused the cycle's component create (HTTP 402). The actionable text the
	// console shows is AgentQuotaBlockedMessage (agent_quota.go).
	RunReasonAgentQuotaBlocked = "agent-quota-blocked"

	// Validation verdicts — what the run learned about the deployed system. Empty
	// until the validation cycle settles.
	//
	// Six values because each names a distinct situation with a distinct action.
	// The vocabulary is deliberately about EVIDENCE, not about blame: a verdict
	// says what we know, and the run's State + TerminalReason say whether the
	// increment stood.
	//
	//   passed        every criterion was automated and every one passed
	//   partial       some passed, none failed, and some were never covered —
	//                 so `passed` would be a claim about criteria nobody checked
	//   failed        a criterion asserted and lost (fails the run)
	//   inconclusive  no test results at all; nothing to conclude from
	//   unreported    no usable report at the cycle's merge commit (fails the run)
	//   skipped       no acceptance criteria authored, and incident runs, which
	//                 get no validation cycle at all
	//
	// `passed` requiring FULL coverage is the point: it previously held whenever
	// one criterion passed and none failed, so a project could read "passed" over
	// twenty manual criteria nobody had looked at.
	ValidationVerdictPassed       = "passed"
	ValidationVerdictPartial      = "partial"
	ValidationVerdictFailed       = "failed"
	ValidationVerdictInconclusive = "inconclusive"
	ValidationVerdictUnreported   = "unreported"
	ValidationVerdictSkipped      = "skipped"
)

// ValidationVerdicts is the closed set of verdicts the store accepts. Kept beside
// the constants rather than in the repository so growing the vocabulary is one
// edit: a seventh verdict that the writer rejected but the reader accepted would
// be a silent write failure at the only moment a run records what it learned.
var ValidationVerdicts = map[string]bool{
	ValidationVerdictPassed:       true,
	ValidationVerdictPartial:      true,
	ValidationVerdictFailed:       true,
	ValidationVerdictInconclusive: true,
	ValidationVerdictUnreported:   true,
	ValidationVerdictSkipped:      true,
}

// IsValidationTerminalReason reports whether a run's terminal reason came from the
// validating phase. The overview needs it: a validation failure is not a BUILD
// failure — every coding cycle landed — so the build stage must not render red
// while the validation chip explains the real cause.
//
// Kept beside ValidationVerdictFailsRun so the two can never disagree about which
// reasons the phase can produce: a reason added to one and missed by the other
// would make the overview contradict itself.
func IsValidationTerminalReason(reason string) bool {
	return reason == RunReasonValidationFailed || reason == RunReasonValidationUnreported
}

// ValidationVerdictFailsRun reports whether a verdict ends the run unsuccessfully,
// and names the terminal reason it settles under. It is the single place the
// verdict→outcome mapping lives, so the supervisor and any future consumer cannot
// disagree about which verdicts are fatal.
//
// Only a real assertion failure and a missing report are fatal. `partial` and
// `inconclusive` are honest reports of incomplete evidence, not defects in the
// increment — telling "the oracle had nothing automatable" apart from "the agent
// ran nothing" is deferred to internal-agent-error handling.
//
// "Fatal" is now about the END of the loop, not the first occurrence. The two
// fatal verdicts are exactly the two the run REPAIRS: `failed` mints an issue per
// failed criterion and re-validates, `unreported` re-dispatches validation. This
// only settles a run once RunMaxValidationAttempts is spent — which is also why
// the same predicate tells the read model when a live run should read
// `awaiting-fix` instead of its verdict.
func ValidationVerdictFailsRun(verdict string) (reason string, fatal bool) {
	switch verdict {
	case ValidationVerdictFailed:
		return RunReasonValidationFailed, true
	case ValidationVerdictUnreported:
		return RunReasonValidationUnreported, true
	default:
		return "", false
	}
}

// Budget limits. Each budget names exactly one failure class, which is what
// keeps the terminal reasons honest.
const (
	// RunMaxRedispatchPerCycle bounds agent death (including
	// Job-succeeded-no-PR) within ONE cycle. It is counted on the cycle record's
	// Attempts, not on the run row, because it resets at every cycle boundary.
	RunMaxRedispatchPerCycle = 2
	// RunMaxBuildRetriggersPerComponentSHA is the automatic build re-trigger
	// allowance: one per component per merge SHA. The authoritative guard is
	// keyed by (component, SHA) at the trigger site; MilestoneRun.BuildRetriggers
	// is only the run-wide tally behind RunReasonBuildRetriggerBudget.
	RunMaxBuildRetriggersPerComponentSHA = 1
	// RunMaxFixCycles / RunMaxConflictCycles bound the two recovery chains.
	RunMaxFixCycles      = 2
	RunMaxConflictCycles = 2
	// RunMaxValidationAttempts bounds the repair-and-re-validate loop: how many
	// times ONE run may validate before it accepts the answer it keeps getting.
	//
	// It counts validation ATTEMPTS rather than the coding cycles between them,
	// because attempts are the thing being repeated — the repair cycles are
	// ordinary work and are already bounded by the cycle ceiling. Alone among the
	// budgets it names no failure class: spending it settles the run on the verdict
	// the last attempt produced (see ValidationVerdictFailsRun), which is why
	// `validation-failed` now means "still failing after every attempt".
	//
	// This is the DEFAULT. A run may pin its own (MilestoneRun.ValidationAttempts),
	// and one attempt is what turns a revalidation into a pure re-check: the
	// allowance is exhausted at the first fatal verdict, which settles the run
	// before the loop reaches the mint, so no repair work is filed and nothing is
	// rebuilt.
	RunMaxValidationAttempts = 2
	// RunDefaultCycleCeiling is the total-cycle ceiling a run starts with when
	// the caller does not pin one. The legitimate worst case uses 4–5 cycles.
	RunDefaultCycleCeiling = 8
)

// MilestoneRun is one run of the milestone loop: "work the open issues in
// milestone M until it settles". There is exactly ONE run species — a spec
// build and an incident adoption differ only in Origin (and in whether the run
// gets a validation cycle) — so this single row carries every run's small
// state, its budgets and its validation verdict.
//
// Identity is (OrgID, ProjectID, MilestoneNumber). **The milestone NUMBER is
// the platform key, never the title**: GitHub milestone titles are freely
// renamable and its title filters are case-insensitive while create-uniqueness
// is case-sensitive. MilestoneTitle is the title AT CREATION, kept for display
// and for the runner's milestone discovery call. A `?tag=` query resolves to a
// milestone number through the Tag on these rows — which is why the read model
// never title-matches against GitHub.
//
// Loop POSITION is deliberately absent: it renders from the latest RunCycle
// joined live, because fix and conflict cycles re-enter earlier phases and a
// flat phase enum would lie mid-loop. Per-component build/deploy status is
// likewise absent — it is derived from OpenChoreo on read, never stored.
//
// The spec-run mutex (§5's 409, in DB form) is a partial unique index on
// (org_id, project_id) WHERE origin = 'spec-build' AND state IN
// ('planning','waiting','running'), created by the milestone_runs migration —
// AutoMigrate cannot express a partial index. Incident-adoption runs are
// deliberately outside the index, so they run concurrently on their own
// milestones.
type MilestoneRun struct {
	ID        string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	OrgID     string `gorm:"index;not null" json:"-"`
	ProjectID string `gorm:"index;not null" json:"projectId"`

	// MilestoneNumber is the GitHub milestone this run works — the platform key.
	MilestoneNumber int `gorm:"index;not null" json:"milestoneNumber"`
	// MilestoneTitle is the milestone's GitHub title at creation, whatever
	// spec.BuildScope.MilestoneTitle resolved to. The runner's `gh issue list
	// --milestone` needs the real GitHub title, so this stays the created title;
	// never a lookup key against GitHub, and never the version — read SpecTag
	// for that.
	MilestoneTitle string `gorm:"index;not null" json:"milestoneTitle"`
	// Tag is the spec version (`v<N>`) this run builds — what `?tag=` resolves
	// against. Empty on the populations SpecTag documents; read it through
	// SpecTag, never directly.
	Tag string `gorm:"index" json:"tag,omitempty"`

	Origin string `gorm:"not null;index" json:"origin"`                // spec-build | incident-adoption
	State  string `gorm:"not null;index;default:waiting" json:"state"` // planning | waiting | running | succeeded | failed | cancelled
	// TerminalReason is set exactly once, when the run settles into a non-success
	// terminal state. Empty while non-terminal and on a succeeded run.
	TerminalReason string `gorm:"type:text" json:"terminalReason,omitempty"`

	// Budget counters. CyclesTotal is checked against CycleCeiling; FixCycles and
	// ConflictCycles bound the two recovery chains; BuildRetriggers is the
	// run-wide tally of automatic build re-triggers (the authoritative
	// one-per-component-per-SHA guard lives at the trigger site, which is keyed
	// by facts this row does not hold). The per-cycle re-dispatch budget is NOT
	// here — it is RunCycle.Attempts, because it resets each cycle.
	CyclesTotal     int `gorm:"not null;default:0" json:"cyclesTotal"`
	FixCycles       int `gorm:"not null;default:0" json:"fixCycles"`
	ConflictCycles  int `gorm:"not null;default:0" json:"conflictCycles"`
	BuildRetriggers int `gorm:"not null;default:0" json:"buildRetriggers"`
	// ValidationCycles is the number of validation ATTEMPTS this run has opened,
	// bounded by RunMaxValidationAttempts. More than one means the run repaired a
	// failed validation and tried again.
	ValidationCycles int `gorm:"not null;default:0" json:"validationCycles"`
	// CycleCeiling is the run's total-cycle ceiling, snapshotted at start so a
	// config change cannot retroactively fail (or rescue) a live run.
	CycleCeiling int `gorm:"not null" json:"cycleCeiling"`
	// ValidationAttempts is how many times THIS run may validate, snapshotted for
	// the same reason as CycleCeiling. Zero means the platform default
	// (RunMaxValidationAttempts) — every run admitted before the column existed
	// reads zero, so the default has to be what zero means.
	//
	// It is stored rather than left to the workflow's input alone because the
	// supervisor is restartable: the reconcile sweep re-offers a row whose
	// workflow never started, and Supervisor.admit hands that EXISTING row back.
	// The run then starts from the row, so a value living only in the original
	// request would be silently replaced by the sweep's default.
	ValidationAttempts int `gorm:"not null;default:0" json:"validationAttempts,omitempty"`

	// ValidationVerdict is a run property, not a per-issue one: the LATEST
	// validation attempt's outcome. Empty until the first attempt settles.
	//
	// A run may validate more than once (see RunMaxValidationAttempts), and each
	// attempt records its own verdict on its RunCycle. This column is the run's
	// answer — the last thing validation concluded — so a self-healed run reads
	// `passed` here while its cycle ledger still shows the attempt that failed.
	ValidationVerdict string `gorm:"type:text" json:"validationVerdict,omitempty"`
	// ValidationIssue is the validation issue this run minted, persisted so a
	// SETTLED run stays navigable to its criteria. It is otherwise only in live
	// workflow state, which means that once Temporal retention lapses the platform
	// can no longer say which issue produced a run's verdict — leaving a verdict
	// with no way to reach the criteria, the PR, or the runner's own summary.
	// Zero until the validation cycle mints it, and on incident runs.
	ValidationIssue int `gorm:"not null;default:0" json:"validationIssue,omitempty"`

	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
}

// TableName pins the table name so a struct rename cannot silently move the
// table.
func (MilestoneRun) TableName() string { return "milestone_runs" }

// SpecTag is the `v<N>` version this run belongs to — the ONE place the
// Tag-with-legacy-fallback rule is written, so a read model can never disagree
// with the store about what version a row is.
//
// Tag is empty on two populations: pre-phase rows, whose MilestoneTitle IS the
// tag, and incident runs admitted before the tag rode StartRunRequest. Both
// fall back to the title, which is why the fallback is a read-side rule rather
// than a backfill.
func (r MilestoneRun) SpecTag() string {
	if r.Tag != "" {
		return r.Tag
	}
	return r.MilestoneTitle
}

// IsRunOrigin reports whether a string is one of the three run origins. It is
// the admission guard's predicate, kept beside the constants because the reason
// origins are validated at all is the spec-run mutex: that index is a partial
// one keyed on the literal `spec-build`, so a typo'd origin would not fail — it
// would silently escape the one-active-spec-run-per-project invariant.
func IsRunOrigin(origin string) bool {
	switch origin {
	case RunOriginSpecBuild, RunOriginIncidentAdoption, RunOriginRevalidate:
		return true
	default:
		return false
	}
}

// RunValidates reports whether a run of this origin asks the version's
// acceptance criteria at all.
//
// Validation is a SPEC-run property plus the revalidation that exists to repeat
// it. An incident run fixes one thing in an already-validated version, and
// re-validating the whole system for it would price every incident like a
// release.
//
// It lives here rather than inline in the loop so the supervisor and anything
// that later needs to explain a `skipped` verdict cannot disagree about which
// runs were ever going to produce one.
func RunValidates(origin string) bool {
	return origin == RunOriginSpecBuild || origin == RunOriginRevalidate
}

// IsTerminalRunState reports whether a run state is settled. Terminal rows are
// never resurrected: every guarded transition in MilestoneRunRepository is
// fenced on the state NOT being terminal, and the spec-run mutex only counts
// non-terminal rows.
func IsTerminalRunState(state string) bool {
	switch state {
	case RunStateSucceeded, RunStateFailed, RunStateCancelled, RunStateBlocked:
		return true
	default:
		return false
	}
}

// nonTerminalRunStates is the WHERE-clause form of !IsTerminalRunState, shared
// by the guarded transitions and the mutex lookup so the two can never disagree.
// It must stay in step with the migration's partial index predicate — a state
// missing from one and present in the other would let a second spec run in.
var nonTerminalRunStates = []string{RunStatePlanning, RunStateWaiting, RunStateRunning}
