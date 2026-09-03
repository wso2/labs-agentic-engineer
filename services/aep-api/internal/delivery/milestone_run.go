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
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// MilestoneRun kinds, origins, states, terminal reasons and validation verdicts
// (plain strings, matching the model convention — canonical values here, no
// separate enum package).
const (
	// RunKindDev is a run that DELIVERS A VERSION: it fills its own milestone
	// (gates, then the planning turn), works the version's development, bug and
	// conflict issues, and judges the result against the version's acceptance
	// criteria. It is the only kind that takes the per-project build mutex, and
	// the only kind whose version is "the version this project is on".
	RunKindDev = "dev"
	// RunKindTask is a run that works a DEFECT inside a version somebody else
	// already delivered — a bug or a merge conflict adopted into an
	// already-deployed milestone. Task runs are deliberately OUTSIDE the project
	// mutex: each works its own milestone, they execute concurrently with each
	// other and with a live dev run, and serialising them per project would turn
	// every incident into a queue behind the next build.
	RunKindTask = "task"
	// RunKindValidation is a run that asks a version's validation criteria again,
	// against the system already deployed. It has NO WORKING SET — an empty
	// milestone is its expected starting state — and it builds and deploys
	// nothing, so it enters the loop at validation rather than at the working set.
	//
	// Also outside the project mutex: it re-judges a version that already
	// shipped, so holding up the next build for its duration would be wrong. The
	// cost is that the database does not serialise two of them, which is why the
	// trigger checks LiveRunForMilestone before admitting one.
	RunKindValidation = "validation"

	// Run origins name WHERE a run came from. What a run DOES is its kind above,
	// and every predicate in the platform is written on the kind — an origin is
	// only ever a label on the trigger.
	//
	// RunOriginSpecBuild is a run started by the build click: the plan path cut a
	// v<N> tag, minted the milestone, and started the supervisor. Kind dev.
	RunOriginSpecBuild = "spec-build"
	// RunOriginIncidentAdoption is a run started by adopting an issue into an
	// already-deployed version's milestone. Kind task.
	RunOriginIncidentAdoption = "incident-adoption"
	// RunOriginRevalidate is a run that asks a version's validation criteria again,
	// against the system already deployed. Kind validation. Both triggers wear it:
	// a human clicking revalidate, and the reconcile sweep finding the version's
	// validation task open — an origin is a label on the trigger, and what the run
	// DOES is its kind.
	//
	// What follows the verdict is chosen by the version's attempt allowance. One
	// attempt reports and settles — the allowance is spent before the run reaches
	// the point where it would file repair work, so nothing is rebuilt. The default
	// gives the repair chain: one issue per failed criterion, which an ordinary run
	// then works.
	RunOriginRevalidate = "revalidate"

	// RunStatePlanning is the FILL WINDOW: the row has been admitted (so the
	// build mutex is armed) but the milestone it works is still being written —
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
	// Terminal, so the run row releases the build mutex and the user can
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
	// RunReasonVersionIncomplete — the milestone has no work left, and the
	// version is still not serving. Every component that is not serving is named
	// in the settle log and on the run's live status (ADR-0026).
	//
	// By the loop's own invariants it should be unreachable: a component that is
	// behind and whose providers are met is promoted by the cycle's reconcile,
	// and one whose deployment failed mints a fix issue that keeps the working
	// set non-empty. It exists because the alternative to being unreachable is
	// being SILENT — the shape it replaces settled such a version `succeeded`,
	// minted its validation task and closed the increment with an API nothing had
	// ever bound. A gate that should never fire is worth having when the thing it
	// catches is indistinguishable from success.
	RunReasonVersionIncomplete = "version-incomplete"
	RunReasonFixChainBudget    = "fix-chain-budget"
	RunReasonConflictBudget    = "conflict-budget"
	RunReasonNoProgress        = "no-progress"
	RunReasonCycleCeiling      = "cycle-ceiling"
	RunReasonValidationFailed  = "validation-failed"
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
	// admitted BEFORE the planning turn (so the build mutex is armed for the
	// whole of it), which means a planning turn that cannot finish must settle
	// the row it armed. Without it a failed plan would wedge the project behind
	// its own mutex until a human cancelled.
	RunReasonPlanFailed = "plan-failed"
	// RunReasonAgentQuotaBlocked explains RunStateBlocked: the entitlement gate
	// refused the cycle's component create (HTTP 402). The actionable text the
	// console shows is AgentQuotaBlockedMessage (agent_quota.go).
	RunReasonAgentQuotaBlocked = "agent-quota-blocked"
	// RunReasonPublisherCredentials explains RunStateBlocked: coding dispatch
	// had no publisher SecretReference to mount. Retrying the Job cannot stamp
	// it. The console text is PublisherCredentialsMissingMessage.
	RunReasonPublisherCredentials = "publisher-credentials-missing"

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
	//   skipped       no validation criteria authored, and incident runs, which
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
// "Fatal" is about the END of the chain, not the first occurrence. The two fatal
// verdicts are exactly the two that get another go: `failed` mints an issue per
// failed criterion for an ordinary run to work, and `unreported` re-dispatches
// inside the validation workflow. Which is also why the same predicate tells the
// read model when a live run should read `awaiting-fix` instead of its verdict.
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
	// RunMaxValidationAttempts bounds the repair-and-re-judge chain: how many times
	// a VERSION may be judged before the answer it keeps giving is accepted.
	//
	// Per version and not per run, because each attempt IS its own validation run:
	// the count is how many `kind = validation` runs the milestone has, read from
	// the ledger. Nothing carries it — the previous attempt's execution has ended —
	// and a per-run counter would let a version buy unlimited attempts by being
	// re-triggered.
	//
	// It counts ATTEMPTS rather than the coding cycles between them, because
	// attempts are the thing being repeated — the repair work is ordinary work and
	// is already bounded by the cycle ceiling. Alone among the budgets it names no
	// failure class: spending it settles the run on the verdict the attempt already
	// produced (see ValidationVerdictFailsRun), which is why `validation-failed`
	// means "still failing after every attempt".
	//
	// This is the DEFAULT. A run may pin its own (MilestoneRun.ValidationAttempts),
	// and one attempt is what turns a revalidation into a pure re-check: the
	// allowance is exhausted at the first fatal verdict, which settles the run
	// before it reaches the repair mint, so no work is filed and nothing is
	// rebuilt.
	RunMaxValidationAttempts = 2
	// RunDefaultCycleCeiling is the total-cycle ceiling a run starts with when
	// the caller does not pin one. The legitimate worst case uses 4–5 cycles.
	RunDefaultCycleCeiling = 8
)

// MilestoneRun is one run of the milestone loop: "work the open issues in
// milestone M until it settles". Every run of every kind is this one row, which
// carries the run's small state, its budgets and its validation verdict.
//
// Kind is what the platform decides on — which runs take the project mutex,
// which one owns the project's version, which fills its own milestone, and
// which reads an empty working set as evidence.
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
// The build mutex (§5's 409, in DB form) is a partial unique index on
// (org_id, project_id) WHERE kind = 'dev' AND state IN
// ('planning','waiting','running'), created by the milestone_run_kind migration
// — AutoMigrate cannot express a partial index. Task and validation runs are
// deliberately outside the index, so they run concurrently on their own
// milestones. A second partial index, on (org_id, project_id,
// milestone_number), makes "one live run per milestone, of ANY kind" true.
// DependencyNames is a jsonb-serialized list of dependency names. Named so the
// column's shape is declared once rather than spelled out at the field, matching
// IssueNumbers on RunCycle.
type DependencyNames []string

// Value/Scan make DependencyNames encode itself as jsonb.
//
// The gorm `serializer:json` tag that RunCycle.Resolves uses only covers the
// STRUCT write path. The run repository parks a run through updateNonTerminal,
// which writes a map[string]any — and a map update hands the value straight to
// the driver, serializer untouched. A bare []string is then encoded as a
// Postgres array literal (`{stripe}`), which jsonb rejects with SQLSTATE 22P02,
// so the deploy gate's park would fail the moment it had a dependency to name —
// which is every park it makes. Owning the encoding on the type makes both write
// paths agree.
func (d DependencyNames) Value() (driver.Value, error) {
	if d == nil {
		return nil, nil
	}
	return json.Marshal([]string(d))
}

func (d *DependencyNames) Scan(src any) error {
	if src == nil {
		*d = nil
		return nil
	}
	var raw []byte
	switch v := src.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("delivery: cannot scan %T into DependencyNames", src)
	}
	if len(raw) == 0 {
		*d = nil
		return nil
	}
	return json.Unmarshal(raw, (*[]string)(d))
}

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

	// Kind is what this run DOES: dev | task | validation. Every predicate in
	// the platform reads it, including the partial index above — which is why
	// TryAdmit validates it against IsRunKind rather than trusting the caller.
	//
	// The `dev` column default exists only so the AutoMigrate that ADDS this
	// column to a populated table can make it NOT NULL. It is never a semantic
	// default: the migration's backfill immediately re-derives every existing
	// row's kind from its origin, and no writer may insert without one.
	Kind string `gorm:"not null;index;default:dev" json:"kind"` // dev | task | validation
	// Origin is WHERE the run came from: spec-build | incident-adoption |
	// revalidate. It is a label on the trigger and nothing branches on it.
	Origin string `gorm:"not null;index" json:"origin"`
	// State is the run's position between admission and settling.
	State string `gorm:"not null;index;default:waiting" json:"state"` // planning | waiting | running | succeeded | failed | cancelled | blocked
	// TerminalReason is set exactly once, when the run settles into a non-success
	// terminal state. Empty while non-terminal and on a succeeded run.
	TerminalReason string `gorm:"type:text" json:"terminalReason,omitempty"`

	// WaitingReason / BlockingDependencies explain a `waiting` run, and are
	// persisted rather than read off the live workflow query because the console
	// reads this row. Set by the deploy gate's park (ADR-0023) and cleared on the
	// next move to running; empty for the ordinary between-cycles park, which
	// needs no explanation.
	//
	// Both columns are plain adds, so BaseModels' AutoMigrate creates them: they
	// are nullable with no default and no index, which is everything AutoMigrate
	// can express. Nothing goes in the ordered migration list, which exists for
	// the partial indexes and CHECK constraints it cannot.
	WaitingReason        string          `gorm:"type:text" json:"waitingReason,omitempty"`
	BlockingDependencies DependencyNames `gorm:"type:jsonb" json:"blockingDependencies,omitempty"`

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
	// ValidationCycles is the number of validation cycles this run has opened.
	// A validation run normally opens exactly one; more than one means its agent
	// merged without committing a report and was re-dispatched. The VERSION's
	// attempt allowance is counted from the milestone's validation runs, not from
	// this column.
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
	// Only a VALIDATION run sets it. A settled dev run leaves it empty, which reads
	// as "delivered, not yet judged" — the exception being a version with no
	// acceptance oracle, where nothing will ever judge it and `skipped` says so.
	// Readers take the newest VALIDATING run on a milestone (RunValidates), so a
	// task run's silence here cannot make a passed version read as unvalidated.
	ValidationVerdict string `gorm:"type:text" json:"validationVerdict,omitempty"`
	// ValidationIssue is the version's validation task, persisted alongside the
	// verdict so a SETTLED run stays navigable to its criteria. It is otherwise only
	// in live workflow state, which means that once Temporal retention lapses the
	// platform can no longer say which issue produced a run's verdict — leaving a
	// verdict with no way to reach the criteria, the PR, or the runner's own summary.
	// Written by the run that JUDGED, so it is zero on a dev run (which files the
	// task but records no judgement) and on a task run.
	ValidationIssue int `gorm:"not null;default:0" json:"validationIssue,omitempty"`

	// CancelRequestedAt is when a human asked for this run to stop — the DURABLE
	// half of cancel, written by the cancel surface before it signals.
	//
	// Cancel is the one fact the loop cannot re-derive from the world: a pod the
	// cancel reaped and a pod that died on its own look identical from inside the
	// workflow, so a loop that knew only "the agent is gone" would spend a
	// re-dispatch and open a fresh cycle over a run the user just stopped. Every
	// other fact the loop acts on is a wake-up it re-reads ground truth for, and
	// this column is what lets cancel work the same way: the signal becomes the
	// fast path, and this becomes the evidence.
	//
	// Set once — the FIRST request stands, so a second click cannot move the
	// stamp — and never cleared: a run is cancelled or it is not, and the row is
	// terminal soon after. Nil on every run nobody cancelled.
	CancelRequestedAt *time.Time `json:"cancelRequestedAt,omitempty"`

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

// RunKinds is the closed set of run kinds, in no significant order. It exists
// for the callers that must act on ALL of them rather than on one: a project
// delete has to terminate every workflow id a milestone could have, and a kind
// missing from that sweep leaves a supervisor retrying forever against a
// repository that is gone.
var RunKinds = []string{RunKindDev, RunKindTask, RunKindValidation}

// IsRunKind reports whether a string is one of the three run kinds. It is the
// admission guard's predicate, kept beside the constants because the reason
// kinds are validated at all is the build mutex: that index is a PARTIAL one
// keyed on the literal `dev`, so a typo'd kind would not fail — it would
// silently escape the one-active-build-per-project invariant, and two agents
// would end up on one branch with nothing having reported an error.
func IsRunKind(kind string) bool {
	switch kind {
	case RunKindDev, RunKindTask, RunKindValidation:
		return true
	default:
		return false
	}
}

// IsRunOrigin reports whether a string is one of the three run origins. Origin
// is a label on the trigger, but it is a NOT NULL closed enum on the wire and
// on the row, so admission validates it for the same reason it validates
// everything else it writes: a value the reader rejects and the writer accepted
// is a row nobody can render.
func IsRunOrigin(origin string) bool {
	switch origin {
	case RunOriginSpecBuild, RunOriginIncidentAdoption, RunOriginRevalidate:
		return true
	default:
		return false
	}
}

// RunKindForOrigin maps a run's origin onto the kind it implies. It is the Go
// twin of the migration's backfill, and the ONE place the mapping is written
// outside SQL.
//
// It exists for facts that were recorded before the kind column did: a run row
// backfilled from a deployment that predates it, and — the case that cannot be
// backfilled — a Temporal workflow input, which lives in history and replays
// with whatever fields existed when the execution started. A live dev run
// replaying with an empty kind would read as "not dev", skip its validation
// phase, and settle succeeded without ever asking the version's criteria.
//
// An unknown origin yields the empty string rather than a guess: nothing may
// silently become `dev` and take the project mutex.
func RunKindForOrigin(origin string) string {
	switch origin {
	case RunOriginSpecBuild:
		return RunKindDev
	case RunOriginIncidentAdoption:
		return RunKindTask
	case RunOriginRevalidate:
		return RunKindValidation
	default:
		return ""
	}
}

// RoutableRunKind resolves the kind a run row is ADDRESSED by — the workflow
// type a start executes, and the workflow id a signal is aimed at — and reports
// whether the row can be addressed at all.
//
// It is the ONE place that resolution is written, because its two callers are the
// two that fail SILENTLY rather than loudly: a start with the wrong type runs the
// wrong loop, and a signal with the wrong id is swallowed as NotFound.
//
// Two rows are routable, and only two:
//
//	a VALID kind        the ordinary case; the kind is what the row says it is.
//	an EMPTY kind whose ORIGIN implies one. That is the pre-column history and
//	                    nothing else: origin has always been NOT NULL and every
//	                    writer sets both from one decision, so an empty kind
//	                    beside a known origin is a fact recorded before the column
//	                    existed — a backfilled row, or a Temporal input replaying
//	                    out of history with only the fields that existed when the
//	                    execution started.
//
// Everything else is REFUSED, and the refusal is the point. A non-empty kind this
// package does not recognise is not history, it is corruption — admission
// validates against IsRunKind, so no writer can produce one — and neither can an
// empty kind beside an unknown origin. Reading either as `dev` is what the guard
// exists to stop: dev is the kind that takes the project's build mutex and plans
// a version, so guessing it starts a build nobody asked for and blocks every
// later one behind it. Refusing costs a visible error and a run that never
// starts, which is the direction a corrupt row must fail in.
func RoutableRunKind(kind, origin string) (string, bool) {
	if IsRunKind(kind) {
		return kind, true
	}
	if kind == "" {
		if implied := RunKindForOrigin(origin); implied != "" {
			return implied, true
		}
	}
	return "", false
}

// SettleClosesTheMilestone reports whether a run of this KIND settling into this
// STATE finishes the VERSION — and therefore closes its milestone.
//
// It is the whole of the rule, in one place, because the two halves of it are
// each other's failure mode: closing too late leaves a finished version's
// milestone open forever, and closing too EARLY breaks the hand-off the three
// workflows exist to create.
//
//	validation  succeeded → CLOSES. The version has its verdict, and a green
//	            ending is what a succeeded validation run IS: every fatal verdict
//	            settles the run `failed` (ValidationVerdictFailsRun), so there is
//	            no succeeded validation run over a version that did not stand.
//	dev         succeeded → closes ONLY if it filed no validation task. Filing one
//	            means the version is DEPLOYED AND UNJUDGED and the milestone is not
//	            finished; filing none (no acceptance oracle, or a plan that minted
//	            nothing) means nothing will ever judge it, and leaving the milestone
//	            open would strand the version in "any moment now" forever.
//	task        never. It fixes one defect inside a version somebody else
//	            delivered; finishing that says nothing about the version.
//	failed      never, of any kind. The way forward from a failed increment is
//	            more work in the same version.
//	cancelled   per CancelClosesTheMilestone — a dev run's cancel abandons the
//	            increment, and nothing else's does.
//	blocked     never. A quota block is a wait somebody else clears.
//
// Why a dev run must NOT close on the hand-off is concrete rather than tidy. The
// validation agent discovers its own work with `gh issue list --milestone`, which
// resolves the milestone BY TITLE and only sees OPEN milestones (skills/aep says
// so in as many words). A dev run that closed the milestone over the validation
// task it had just minted would leave that task unfindable by the only agent
// meant to work it — the version deployed, the task open, and the run that could
// judge it unable to see the milestone it lives in.
//
// Milestone state is display only and nothing branches on it, which is what makes
// getting it right a documentation problem rather than a correctness one — except
// through that one agent-side read, which is why the rule is stated here instead
// of being left to each settle site.
//
// awaitingVerdict says a validation task now stands open over this version,
// waiting for somebody to judge it. Only the dev arm reads it: a validation run
// closes the task on every ending, and a task run's reopen is the thing that
// hands the version BACK to validation, so for both of them the answer is already
// decided by the kind.
func SettleClosesTheMilestone(runKind, state string, awaitingVerdict bool) bool {
	switch state {
	case RunStateSucceeded:
		switch runKind {
		case RunKindValidation:
			return true
		case RunKindDev:
			return !awaitingVerdict
		default:
			return false
		}
	case RunStateCancelled:
		return CancelClosesTheMilestone(runKind)
	default:
		return false
	}
}

// RunValidates reports whether a run of this kind produces a VERDICT about the
// deployed system.
//
// Exactly one kind does. A validation run is the judgement — it is the whole
// reason the kind exists. A dev run delivers a version and MINTS the validation
// task at deployed-green, but it never asks the criteria itself: judging inside
// the delivery loop meant one workflow owned both "is the increment built" and
// "does it hold", and the two answers have different lifetimes (a version is
// re-judged long after it shipped) and different failure classes. A task run
// fixes one thing in an already-judged version, and re-validating the whole
// system for it would price every incident like a release.
//
// It lives here rather than inline in a read model so nothing has to re-derive
// which rows can carry a verdict: the newest VALIDATING run on a milestone owns
// that version's answer, and a dev run's empty verdict means "not judged yet",
// never "judged and fine".
func RunValidates(kind string) bool {
	return kind == RunKindValidation
}

// IsTerminalRunState reports whether a run state is settled. Terminal rows are
// never resurrected: every guarded transition in MilestoneRunRepository is
// fenced on the state NOT being terminal, and the build mutex only counts
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
// missing from one and present in the other would let a second dev run in.
var nonTerminalRunStates = []string{RunStatePlanning, RunStateWaiting, RunStateRunning}
