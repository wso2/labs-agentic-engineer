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

// The CYCLE BOUNDARY: the shared state every workflow in this package carries,
// and the loop two of them are. Poll the milestone, decide whether to dispatch,
// spend the budgets, park when something holds it.

// Cadences. Only two of them are load-bearing.
const (
	// activityTimeout bounds one activity call. Almost every activity is a
	// single GitHub, OpenChoreo or database round trip — planActivityTimeout
	// covers the one that is not.
	activityTimeout = 2 * time.Minute

	// gateActivityTimeout bounds ProvisionGates. Wider than activityTimeout
	// because that activity is not one round trip: it authors every one of the
	// version's platform resources in sequence, and each one WAITS up to a minute
	// for OpenChoreo to cut its ResourceRelease
	// (openchoreo.WaitForReleaseChange). Four dependencies, one of them
	// unsatisfiable, already exceeds two minutes — and a StartToClose expiry
	// there reports "timeout" as the run's terminal reason instead of the
	// provisioning failure that actually happened, which is the one thing a
	// reader of a failed version needs.
	gateActivityTimeout = 5 * time.Minute

	// gateActivityAttempts is how many times ProvisionGates is tried before the
	// run gives up on the version.
	//
	// BOUNDED, unlike almost everything else here, and the bound is the point.
	// Provisioning has answers that repeating cannot change — a dependency naming
	// a ClusterResourceType nobody installed is the case that taught us — and
	// under Temporal's default unbounded policy such an answer becomes a
	// permanent, invisible loop: the activity re-mints the version's gate issues
	// on every attempt (they are deduped against OPEN gates, and it closes the
	// ready ones itself), so the symptom is a milestone filling with duplicate
	// gates forever rather than a failed build.
	//
	// It is the BACKSTOP, not the front line, and that split is deliberate. The
	// case above is now caught twice before it reaches here: the build click
	// refuses a design naming a type the cluster does not have, and a permanent
	// provision fault that does reach the activity comes back non-retryable and
	// fails on attempt one with the provisioner's own message (provisionErr). Both
	// only cover the modes we can NAME. This bound covers the rest — including the
	// next one nobody has met — which is why it stays even though the incident
	// that motivated it now fails earlier and reads better.
	//
	// Three rather than one, because the same call is also how a genuine GitHub or
	// OpenChoreo blip shows up, and failing a version on the first hiccup would
	// trade a rare runaway for a common false failure.
	gateActivityAttempts = 3

	// gateActivityRetryInterval is the FIRST gap between those attempts, and it is
	// set explicitly because Temporal's default (1s, doubling) makes the bound
	// above far tighter than it looks. A fault that fails FAST — connection
	// refused, DNS, a 4xx — burns all three attempts in about three seconds, so a
	// blip lasting longer than a blink would fail the version. That is the false
	// failure the bound was supposed to avoid, arrived at by a different road.
	//
	// At 10s doubling, the three attempts span ~30 seconds: long enough to sit out
	// an ordinary hiccup, short enough that a version nobody can provision still
	// gives up while its author is watching. The named permanent faults do not
	// wait for any of this — provisionErr fails them on attempt one.
	gateActivityRetryInterval = 10 * time.Second

	// planActivityTimeout bounds PlanMilestone, the only activity that waits on
	// an agent turn rather than a round trip. A healthy plan turn has no upper
	// bound on its total length — only on its SILENCE, and the plan tap's own
	// idle watchdog already enforces that (task.planDrainIdleTimeout, 90s
	// against ~15s keep-alives). So a short bound here never caught a hung
	// turn, it only killed a slow healthy one: the turn kept draining past the
	// timeout, holding the per-project plan lock, and every retry Temporal
	// scheduled meanwhile failed with ErrPlanInProgress until the original
	// finished — then re-ran the whole turn to get an answer it had already
	// thrown away. Generous enough that only a dead worker reaches it.
	planActivityTimeout = 30 * time.Minute

	// waitPollInterval re-reads the milestone while the run is WAITING. The wait
	// itself stays unbounded — this timer never ends it, it only re-derives the
	// predicate, so a lost `issues.closed` delivery costs ten minutes of latency
	// instead of stranding the run behind a gate that is already resolved.
	waitPollInterval = 10 * time.Minute

	// buildPollInterval re-reads the cycle's builds. Same role: the build
	// terminals arrive as signals, and this is what makes a lost one survivable.
	buildPollInterval = time.Minute

	// deployPollInterval re-reads the cycle's ReleaseBindings. Nothing signals a
	// deployment — it is a level OpenChoreo reconciles continuously, with no
	// event to deliver — so unlike the build poll this is the ONLY way the stage
	// learns anything, not a backstop for a lost delivery.
	deployPollInterval = 15 * time.Second

	// deployReadyTimeout bounds the wait for a cycle's components to serve.
	//
	// The loop's second real deadline, and the only other one besides
	// cycleLandingTimeout. It exists because a ReleaseBinding never terminates:
	// a build always finishes, so awaitBuilds can wait forever safely, but an
	// image that will never pull and a rollout thirty seconds from Ready are
	// indistinguishable from outside. Generous enough to cover a cold image pull
	// on a laptop-sized cluster; short enough that a broken deployment becomes a
	// fix issue inside one coffee break rather than hanging the version.
	deployReadyTimeout = 15 * time.Minute

	// cycleLandingTimeout is how long ONE dispatch has to land a merged pull
	// request before the supervisor calls it agent death and spends a
	// re-dispatch. It is the only deadline in the loop, and it exists because
	// "the agent died" (including a Job that exited without opening a pull
	// request) is a named failure class with a named budget.
	cycleLandingTimeout = 2 * time.Hour
)

// RunInput starts a supervisor over one milestone. Everything in it is already
// decided by the caller: the run row exists, the milestone exists, and the
// ceiling is snapshotted so a config change cannot retroactively fail a live
// run.
//
// It is ONE input type for all three workflows. They differ in what they do with
// a milestone, not in how they are addressed, and a per-workflow input would put
// the same six identity fields in three places.
type RunInput struct {
	RunID           string `json:"runId"`
	OrgID           string `json:"orgId"`
	ProjectID       string `json:"projectId"`
	MilestoneNumber int    `json:"milestoneNumber"`
	MilestoneTitle  string `json:"milestoneTitle"`
	// Kind is what this run does (dev | task | validation). It now selects the
	// WORKFLOW TYPE rather than a branch inside one, so the loop reads it only
	// for the read model and for the one predicate a dev run still shares with a
	// task run — whether it owns the version it is working.
	//
	// Read through loop.kind() and never directly, because an execution started
	// before this field existed replays with the empty string.
	Kind         string `json:"kind,omitempty"`
	Origin       string `json:"origin"`
	CycleCeiling int    `json:"cycleCeiling,omitempty"`
	// ValidationAttempts pins how many times this VERSION may be judged. Zero
	// means the platform default, and it MUST: a workflow input lives in Temporal
	// history, so an execution started before this field existed replays with the
	// zero value. An int that falls back to the default replays identically; a
	// bool would have flipped behaviour under every live run.
	//
	// The allowance is per version rather than per run, and it is spent by the
	// milestone's validation RUNS (workflow_validation.go): one attempt is what
	// turns a revalidation into a pure re-check.
	ValidationAttempts int `json:"validationAttempts,omitempty"`

	// Tag and ProvisionInputs are what the PLANNING phase needs: the version
	// being filled, and the dependency inputs its gates are minted from.
	//
	// Both empty means "this run does not plan" — which is the correct reading
	// for every origin that adopts an already-filled milestone, AND for an
	// execution started before planning moved into this workflow. Same
	// replay-safety rule as ValidationAttempts above: a zero value has to mean
	// the pre-existing behaviour, or live runs change shape mid-flight.
	//
	// ProvisionInput carries SM-API references and non-secret config, never a
	// secret value (see its doc), so it is safe in workflow history.
	Tag             string                    `json:"tag,omitempty"`
	ProvisionInputs []delivery.ProvisionInput `json:"provisionInputs,omitempty"`

	// Rebuild narrows the planning phase to its GATES: this run owns the version
	// (it carries a Tag) but its milestone is ALREADY FILLED, because the click
	// reopened it. It rides the request beside Tag, for the same reason and with
	// the same replay rule — false is the pre-existing behaviour, so an execution
	// started before the field existed replays as an ordinary planning run.
	//
	// Skipping the plan is not an optimisation, it is the only correct answer.
	// Plan dedupe is the title slug against the milestone's issues in ANY state,
	// which is what makes re-planning additive-only and a crash re-run a no-op —
	// and a cancel CLOSED every open issue, so a re-plan would recognise every
	// slug, mint NOTHING, and the run would then read the empty working set as
	// "delivered" and settle a version it never built. Reopening the issues is
	// what restores the working set without breaking that dedupe rule, and it is
	// also cheaper: no LLM turn.
	//
	// The gates still run, and must: they are idempotent by dedupe key and land
	// on the reopened gate issues, so a dependency resolved since the cancel is
	// re-read rather than assumed.
	Rebuild bool `json:"rebuild,omitempty"`
}

// RunResult is the run's outcome, mirroring what was written to the run row.
type RunResult struct {
	State             string `json:"state"`
	TerminalReason    string `json:"terminalReason,omitempty"`
	ValidationVerdict string `json:"validationVerdict,omitempty"`
	Cycles            int    `json:"cycles"`
}

// loop is a run's whole in-workflow state, SHARED by all three workflows: it
// owns the signal channels, the budgets and the cycle state, and every workflow
// wants all three.
//
// It is the authority on the budgets: they are counted here, deterministically,
// and written outwards to the run row for the read model — never read back,
// because a replay must reproduce the same decisions without a database.
type loop struct {
	in RunInput
	st delivery.RunStatus

	cancel   workflow.ReceiveChannel
	workable workflow.ReceiveChannel
	merged   workflow.ReceiveChannel
	builds   workflow.ReceiveChannel
	conflict workflow.ReceiveChannel
	// valuesSaved wakes a run parked on the DEPLOY GATE. Separate from workable
	// because the two park for different reasons and re-derive different
	// predicates: workable is "the dispatch gate opened", this is "a credential
	// arrived".
	valuesSaved workflow.ReceiveChannel

	// lastResult is what the previous cycle produced — it selects the next
	// cycle's kind and feeds the no-progress rule.
	lastResult cycleResult
	// workBefore is the working-set size when the previous cycle was dispatched.
	workBefore int

	// prNumber / mergeSHA are the current cycle's landing, read from the cycle
	// record rather than from the signal that announced it.
	prNumber int
	mergeSHA string

	// deployFailed / deployFailures carry the last deploy stage's verdict from
	// the cycle into the issue the boundary mints for it. Held on the loop rather
	// than returned through cycleResult because every other failure class already
	// has its issue minted for it by the EVENT PLANE, which sees the failure
	// first-hand; a deployment has no webhook, so the supervisor is the only
	// thing that ever knows which component did not come up.
	//
	// Each failure carries the COMMIT its release was being cut from, because the
	// issue's dedupe key is (component, commit) and one reconcile pass can promote
	// two components at two different commits.
	deployFailed   []delivery.DeployTarget
	deployFailures map[string]string
	// cycleID is the current cycle's record id. Surfaced on the loop because the
	// verdict write lands after the agent stage has returned.
	cycleID string

	// validationAttempts is how many times this VERSION has been judged,
	// including the run holding this loop. It is DERIVED from the milestone's own
	// validation runs rather than carried, because attempts span workflows now
	// (see workflow_validation.go).
	validationAttempts int
	// maxValidationAttempts is the version's allowance, resolved once at start
	// from the input. One attempt is what makes a revalidation a pure re-check: it
	// is spent by the first fatal verdict, which settles the run before the loop
	// reaches the repair mint.
	maxValidationAttempts int
	// lastReportDigest fingerprints the PREVIOUS attempt's report, read off its
	// cycle row. A repeat whose report digests the same learned nothing, so the
	// chain stops instead of spending the rest of the allowance on the same
	// answer.
	lastReportDigest string

	// workedValidationRepair records that some boundary poll saw open
	// `src/validation` work in this milestone. It LATCHES: set on the first poll
	// that sees any, never cleared.
	//
	// It is how a task run knows, at the moment its working set empties, that
	// what it just finished came from a failed verdict — and therefore that the
	// version's validation task must be reopened so the same oracle judges the
	// repair. Everything else about a `src/*` source is provenance; this is the
	// one place one routes anything.
	//
	// Latching is not laziness, it is the only form that can work. By the time the
	// working set is empty the repair issues are CLOSED, so a poll taken then can
	// no longer see them; and asking GitHub "does a closed src/validation issue
	// exist" instead would be true forever after the first repair, which reopens
	// the task after every later run — validation closes it, the next task run
	// reopens it, without end. A flag over the polls this run actually took is
	// deterministic workflow state, replays identically, and costs no round trip
	// because the count rides the poll that was already happening.
	workedValidationRepair bool
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
		valuesSaved:           workflow.GetSignalChannel(ctx, delivery.SigRunValuesSaved),
		st: delivery.RunStatus{
			RunID:           in.RunID,
			MilestoneNumber: in.MilestoneNumber,
			Kind:            runKind(in),
			Origin:          in.Origin,
			State:           delivery.RunStateWaiting,
			Phase:           delivery.RunPhaseWaiting,
			CycleCeiling:    ceiling,
		},
	}
}

// bookends are what one workflow contributes to the shared cycle loop: what runs
// BEFORE the first boundary poll, and what runs when the working set EMPTIES.
//
// The dev run and the task run are the SAME loop with different bookends — a dev
// run fills its milestone first and mints the validation task at deployed-green,
// a task run does neither — so the difference is two function values rather than
// two copies of the loop. Two copies is the shape this replaced: every rule the
// loop enforces (no-progress, four budgets, the gate park, cancel re-derivation)
// would have had to be maintained twice, and a fix applied to one of them is a
// silent divergence in how the platform treats a defect versus a release.
//
// A nil before is "start working immediately". onEmpty is required: a loop with
// nothing to do at an empty working set could only spin.
type bookends struct {
	// work selects WHICH working set this run polls out of the snapshot, and it is
	// the most consequential of the three: it decides what a dispatch is spent on
	// and what an empty milestone means. A dev run takes DevWork, a task run takes
	// TaskWork — which excludes planned work, because a build that gave up leaves
	// its plan open and a bug-fix run must not continue it with different budgets.
	//
	// Nil reads as the DEV working set, which is the wider of the two: a run that
	// sees work it need not do stalls visibly, where a run blind to work settles a
	// version nobody built.
	work func(MilestoneSnapshot) int
	// before runs once, before the first boundary poll. settled=true means it
	// ended the run itself and res is the outcome.
	before func(ctx workflow.Context) (settled bool, res RunResult, err error)
	// onEmpty runs when the working set is empty and no recovery is outstanding.
	// It always ends the run — neither surviving workflow has anything to do
	// after it.
	onEmpty func(ctx workflow.Context) (RunResult, error)
}

// work is the cycle-boundary loop. Every pass begins by polling GROUND TRUTH —
// the milestone itself — and every decision below is made from that poll and
// the workflow's own counters. No branch here trusts a signal's payload.
func (l *loop) work(ctx workflow.Context, ends bookends) (RunResult, error) {
	if ends.work == nil {
		ends.work = devWorkingSet
	}
	if ends.before != nil {
		if settled, res, err := ends.before(ctx); settled || err != nil {
			return res, err
		}
	}
	for {
		cancelled, cerr := l.cancelled(ctx)
		if cerr != nil {
			return l.result(), cerr
		}
		if cancelled {
			return l.settle(ctx, delivery.RunStateCancelled, "")
		}

		snap, err := l.pollMilestone(ctx)
		if err != nil {
			return l.result(), err
		}
		// Latch BEFORE the settle branch: the poll that finds the working set
		// empty is by definition the one that can no longer see the repair issues
		// this run closed.
		if snap.ValidationRepairs > 0 {
			l.workedValidationRepair = true
		}
		work := ends.work(snap)

		// SETTLE comes first, before the gate check: a stray gate holds dispatch,
		// and with an empty working set there is nothing to dispatch, so it holds
		// nothing.
		if work == 0 {
			settled, res, err := l.onEmptyWorkingSet(ctx, ends)
			if settled || err != nil {
				return res, err
			}
			continue
		}

		if noProgress(l.lastResult, l.workBefore, work) {
			return l.settle(ctx, delivery.RunStateFailed, delivery.RunReasonNoProgress)
		}

		if !Dispatchable(snap, work) {
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

		l.workBefore = work
		res, err := l.runCycle(ctx, kind, noAnchorIssue)
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
		case cyclePublisherCredentials:
			return l.settle(ctx, delivery.RunStateBlocked, delivery.RunReasonPublisherCredentials)
		default:
			// File the deploy's work before looping, on whatever the cycle's
			// RESULT was: a red cycle can also have a failed deployment now that
			// the reconcile runs on both verdicts, and the result carries the
			// build's verdict. Keying on the failures themselves is what keeps
			// the two independent.
			//
			// Unlike a red build or a conflict — where the event plane has already
			// minted the issue by the time the supervisor hears about it — nothing
			// else observes a deployment, so if this does not mint, the next
			// boundary finds an empty working set and settles a run whose version
			// is not running.
			if err := l.mintDeployFixIssues(ctx); err != nil {
				return l.result(), err
			}
		}
		l.lastResult = res
	}
}

// onEmptyWorkingSet decides what an exhausted milestone means. Four different
// things, in this order:
//
//  1. Nothing was ever dispatched by a run that FILLED this milestone — planning
//     produced no work, so the version is delivered without ever being judged.
//  2. Nothing was ever dispatched by a run that adopted somebody else's
//     milestone — the emptiness is not evidence, so park.
//  3. The last cycle ended badly and NOTHING came back to recover it. The
//     recovery issue the event plane should have minted is not there, so the
//     run cannot proceed and fails naming the budget that ran out.
//  4. Otherwise the workflow's own onEmpty bookend ends the run.
//
// Case 1 settles rather than falling through to the bookend, and that is the
// whole difference between it and the same milestone emptying after a cycle: a
// dev run's bookend files the version's validation task, and validation asserts
// against what a run LANDED. This one landed nothing, so there is nothing to
// judge and `skipped` is the honest verdict. It is also the right answer for a
// re-build of a version whose Tasks all already exist and are closed, where
// planning legitimately mints nothing. With no task filed, nothing will ever
// judge this version — which is exactly the dev ending that DOES close the
// milestone (delivery.SettleClosesTheMilestone).
//
// Case 2 is why case 1 is gated on plansItsOwnMilestone rather than on the cycle
// count alone. A task run fires on a label write, and GitHub's issue index lags a
// write (see validation_issues.go, which records a read-back that answered "no
// validation issue" for one this platform had just filed); a run that polled
// before the labelled issue was indexed would read an empty working set with no
// cycles behind it, settle SUCCEEDED, and — filing no validation task on the way
// — close the milestone over work nothing had dispatched.
//
// It returns settled=false only for that park, after which the boundary is
// re-entered.
func (l *loop) onEmptyWorkingSet(ctx workflow.Context, ends bookends) (settled bool, res RunResult, err error) {
	if l.st.CyclesTotal == 0 {
		if !l.plansItsOwnMilestone() {
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
		// Planning landed and produced nothing to work. Delivered, and unjudged.
		if verr := l.setVerdict(ctx, noCycle, delivery.ValidationVerdictSkipped, ""); verr != nil {
			return true, l.result(), verr
		}
		res, err = l.settle(ctx, delivery.RunStateSucceeded, "")
		return true, res, err
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
	case cycleDeployFailed:
		// The components built but never came up, and nothing joined the
		// milestone to fix them. Deliberately NOT settled as delivered: the
		// version compiled, which is exactly the state that would otherwise be
		// mistaken for success.
		res, err = l.settle(ctx, delivery.RunStateFailed, delivery.RunReasonDeployBudget)
		return true, res, err
	}
	res, err = ends.onEmpty(ctx)
	return true, res, err
}

// runCycle is ONE dispatch of the agent at the milestone, through to a verdict:
// the agent stage, then the merge's builds, then the deploy.
//
//	append the cycle record ─► dispatch ─► wait for the pull request to land
//	                                    ├─ conflict  ─► cycleConflict
//	                                    ├─ no landing within the deadline ─► re-dispatch
//	                                    └─ merged ─► wait for the fan-out's builds
//	                                                 ├─ a component red ─► build verdict red
//	                                                 └─ all green ─► build verdict green
//	                                                 then, on EITHER verdict:
//	                                                 RECONCILE the version, wait for Ready
//	                                                 ├─ all ready ─► deploy verdict green
//	                                                 └─ a component failed ─► cycleDeployFailed
//
// It composes the three stages, which is why it lives with the boundary that
// drives it rather than in any one of them: what a cycle IS — code that merges,
// builds and then serves — is the boundary's definition of a delivered
// increment. A validation run has no such cycle: its pull request touches only
// `tests/`, so it runs the agent stage alone (workflow_validation.go).
//
// The deploy runs on a RED build too (ADR-0026). A red build has already minted
// its fix issue and cannot be helped by holding its siblings back; the version's
// other components are built, and promoting them is what keeps "what is serving"
// a function of what has been built rather than of which files a fix touched.
// What makes that safe is the promotable rule, not this call site: the reconcile
// writes only components whose hard providers are serving or promoted ahead of
// them.
//
// A cycle can therefore be red AND have a deploy failure. Both mint their work —
// the build's issue by the event plane, the deploy's by the boundary — and the
// cycle's RESULT is the earlier stage's verdict, because the deploy verdict is
// downstream of it and the result only decides the next cycle's kind and the
// terminal reason at an empty boundary, where both would name a budget that has
// already produced its issue.
func (l *loop) runCycle(ctx workflow.Context, kind string, anchorIssue int) (cycleResult, error) {
	landed, res, err := l.agentStage(ctx, kind, anchorIssue)
	if err != nil || !landed {
		return res, err
	}

	l.st.Phase = delivery.RunPhaseBuilding
	buildRes, err := l.awaitBuilds(ctx)
	if err != nil {
		return cycleNone, err
	}
	if buildRes != cycleGreen && buildRes != cycleRed {
		// Cancelled mid-build. Nothing to reconcile — the run is over.
		return buildRes, nil
	}

	// Built, not yet running. The deploy is the platform's own act — nothing
	// promotes a release on its own — so the cycle is not over until the version
	// is serving what has been built. Everything downstream of a green cycle
	// depends on that being true rather than merely requested: validation asserts
	// against the deployment, and the version is called delivered on the strength
	// of it.
	l.st.Phase = delivery.RunPhaseDeploying
	version, err := l.readVersionState(ctx)
	if err != nil {
		return cycleNone, err
	}
	deployRes, err := l.reconcileVersion(ctx, version)
	if err != nil {
		return cycleNone, err
	}
	switch {
	case deployRes == cycleCancelled:
		return cycleCancelled, nil
	case buildRes == cycleRed:
		return cycleRed, nil
	default:
		return deployRes, nil
	}
}

// settle ends the run, and each terminal state carries its own consequence for
// the milestone's issues — because each says something different about whether
// the work is still somebody's.
//
//	succeeded  the work is done. Whether the VERSION is finished with it is a
//	           separate question, and delivery.SettleClosesTheMilestone answers it.
//	failed     the work stays OPEN and is HALTED, because the way forward from a
//	           failed increment is more work in the same version.
//	cancelled  the in-flight work is CLOSED and stamped `aep:cancelled`, and a
//	           DEV run's milestone is closed with it: the increment is abandoned.
//	blocked    nothing. A quota block is a wait somebody else clears.
//
// The MILESTONE close is decided in delivery, not here, and deliberately: this
// function is shared by all three workflows, so a plain `state == succeeded`
// closed the milestone at a dev run's hand-off — over the validation task it had
// just minted, which the validation agent then could not find (it discovers its
// work through `gh issue list --milestone`, which resolves by title and sees only
// OPEN milestones). One predicate, reading the run's KIND and whether a verdict
// is still owed, is what keeps that rule impossible to state differently in three
// places.
//
// The ISSUE consequences run BEFORE the row is settled, and before the milestone
// close. That order is what makes them mandatory rather than best-effort at the
// loop's level: a write that fails stalls under Temporal's retries with the run
// still non-terminal, where settling first and then writing would leave a
// terminal row whose issues never got the treatment — and nothing afterwards
// would notice. The container closes last for the same reason supersede closes it
// last: a milestone closing before the work inside it reads as a resolution
// rather than an abandonment.
func (l *loop) settle(ctx workflow.Context, state, reason string) (RunResult, error) {
	l.st.Phase = delivery.RunPhaseSettling
	// Cancel stops the agent at the HTTP surface (runread.CycleReaper →
	// DeleteComponent), not here: a Temporal-durable stop was tried on main and
	// dropped in favour of that best-effort reap.
	if state == delivery.RunStateFailed {
		if err := l.haltUnfinishedWork(ctx, reason); err != nil {
			return l.result(), err
		}
	}
	if state == delivery.RunStateCancelled {
		if err := l.closeCancelledWork(ctx); err != nil {
			return l.result(), err
		}
	}
	// A validation task standing open over this version is what says the version
	// is deployed and UNJUDGED. A dev run reaches this having just filed one; a
	// task run that reopened one has too.
	awaitingVerdict := l.st.ValidationIssue != 0
	if delivery.SettleClosesTheMilestone(l.kind(), state, awaitingVerdict) {
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

// plansItsOwnMilestone reports whether this run OWNS the version it is working —
// and therefore whether it fills the milestone itself and may read an empty
// working set as "delivered".
//
// Two INDEPENDENT clauses, and both are needed. Only a dev run plans at all. And
// only the build CLICK supplies a Tag: a run the reconcile sweep or an adoption
// re-offers carries none, which is what stops a re-offer from re-planning a
// version somebody already filled. The tag rides the REQUEST, not the row, for
// exactly that reason — the row cannot tell "start me" from "fill me".
//
// A task run adopts a milestone somebody else filled, so for it an empty working
// set is evidence of nothing at all.
func (l *loop) plansItsOwnMilestone() bool {
	return l.kind() == delivery.RunKindDev && l.in.Tag != ""
}

// kind is the run's species, replay-safe. See RunInput.Kind.
func (l *loop) kind() string { return runKind(l.in) }

// runKind resolves a RunInput's kind, falling back to the origin it was started
// with when the input predates the field. Deterministic — a pure function of the
// input — so it is safe on the workflow's replay path.
func runKind(in RunInput) string {
	if in.Kind != "" {
		return in.Kind
	}
	return delivery.RunKindForOrigin(in.Origin)
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
	for _, ch := range []workflow.ReceiveChannel{l.workable, l.merged, l.builds, l.conflict, l.valuesSaved} {
		sel.AddReceive(ch, func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, nil) })
	}
	sel.AddFuture(workflow.NewTimer(timerCtx, waitPollInterval), func(workflow.Future) {})
	sel.Select(ctx)
	return cancelled
}

// cancelRequested drains a pending cancel signal without blocking. The FAST
// path: a cancel that arrived mid-cycle is honoured at the first safe point
// rather than after another dispatch.
func (l *loop) cancelRequested() bool { return l.cancel.ReceiveAsync(nil) }

// cancelled is the boundary's cancel question, asked both ways.
//
// The signal first, because it costs nothing and answers immediately. Then the
// run row, because the signal is not evidence: the cancel surface swallows a
// failed delivery so a dead engine cannot wedge the console, and a run parked in
// the unbounded wait would otherwise sit there forever on a signal that never
// arrived. Reading the row is what turns that from a wedge into ten minutes of
// latency.
func (l *loop) cancelled(ctx workflow.Context) (bool, error) {
	if l.cancelRequested() {
		return true, nil
	}
	facts, err := l.cycleFacts(ctx)
	if err != nil {
		return false, err
	}
	return facts.CancelRequested, nil
}

// awaitInterruptibly executes one activity and STOPS WAITING the moment a cancel
// arrives, answering which of the two happened.
//
// It exists for the planning bookend, and the bug it closes is worth naming: the
// cycle loop asks l.cancelled at every boundary, but the bookend runs BEFORE the
// first boundary, so for as long as it lasted a run was blind to cancel. Two
// activities is not much of a window until one of them retries — a version whose
// gates could not be authored sat there re-minting them, with six delivered
// cancel signals unread in the channel and nothing but a Temporal terminate to
// end it.
//
// The activity's context is a CHILD of the caller's, so cancelling it leaves the
// workflow's own context live and the run settles on the ordinary path —
// closing its issues, closing the milestone, writing its row. That is the whole
// reason cancel is a signal here and not a Temporal workflow cancellation
// (delivery.SigRunCancel), and this helper is how the planning phase joins that
// design rather than working around it.
//
// The future resolves at once because WaitForCancellation is left off: the
// workflow does not wait for the worker to acknowledge. What makes the ATTEMPT
// stop rather than merely be abandoned is the heartbeat on its activity options
// — see heartbeating — and the two are a pair. Without the heartbeat this still
// returns instantly and the run still settles; the orphaned attempt just runs out
// its remaining wait, unaware, before finding the workflow gone.
//
// The cancel channel is DRAINED on the way out, exactly as cancelRequested
// drains it, so the signal is consumed by whoever acts on it and never acted on
// twice.
func (l *loop) awaitInterruptibly(ctx, actCtx workflow.Context, act, arg any) (cancelled bool, err error) {
	actCtx, stopActivity := workflow.WithCancel(actCtx)
	// Deferred rather than called on the cancel branch alone: on the ordinary
	// branch the activity has already finished and this is a no-op, and one exit
	// path is easier to keep correct than two.
	defer stopActivity()

	future := workflow.ExecuteActivity(actCtx, act, arg)
	sel := workflow.NewSelector(ctx)
	// CANCEL IS ADDED FIRST, and the order is load-bearing rather than tidy.
	// Selector.Select walks its cases in registration order and takes the first
	// that is ready, so when the activity finished in the same workflow task the
	// cancel landed in, this is what decides which of the two the run acts on.
	//
	// The cancel has to win. The alternative reports the activity's own outcome
	// for a run a person had already stopped — and when that outcome is a failure,
	// the version settles `plan-failed` with a cancel sitting unread, which is
	// both the wrong terminal reason and the wrong story on the issues. Discarding
	// a successful attempt costs nothing either way: gates dedupe, and a plan the
	// cancel is about to close was work nobody wanted.
	sel.AddReceive(l.cancel, func(c workflow.ReceiveChannel, _ bool) {
		c.Receive(ctx, nil)
		cancelled = true
	})
	sel.AddFuture(future, func(f workflow.Future) {
		err = f.Get(ctx, nil)
	})
	sel.Select(ctx)
	return cancelled, err
}

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

// planActivityCtx is activityCtx for the planning turn: same unbounded retry
// policy, a timeout sized to an agent turn instead of a round trip.
func planActivityCtx(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: planActivityTimeout,
		// Heartbeat so a cancel can reach the turn — a 30-minute agent turn is the
		// longest a run is ever blind, and the one a person is most likely to change
		// their mind during. The timeout is deliberately generous rather than tight;
		// activityHeartbeatTimeout says why, and it matters most here.
		HeartbeatTimeout: activityHeartbeatTimeout,
	})
}

// gateActivityCtx runs ProvisionGates with a BOUNDED retry policy — see
// gateActivityAttempts for why this one activity does not get the unbounded
// default, and gateActivityTimeout for why it gets longer than a round trip.
//
// The heartbeat timeout is what makes a cancel reach the work rather than only
// the waiting: without one, cancelling the activity's context frees the WORKFLOW
// immediately but the attempt in the worker runs to completion unaware, so the
// resources it was mid-way through authoring keep being authored. See
// heartbeating, which supplies the beats these options expect.
func gateActivityCtx(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: gateActivityTimeout,
		HeartbeatTimeout:    activityHeartbeatTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: gateActivityAttempts,
			InitialInterval: gateActivityRetryInterval,
		},
	})
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

func (l *loop) pollMilestone(ctx workflow.Context) (MilestoneSnapshot, error) {
	var out MilestoneSnapshot
	err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).PollMilestone, l.milestoneRef()).Get(ctx, &out)
	return out, err
}

// readVersionState re-derives what the version is serving, and mirrors it onto
// the live status so a console reading a run mid-deploy sees the same five
// states the loop decided on.
//
// It is READ, never carried. The reconcile's own classification is a moment old
// by the time the boundary asks — a converge may have landed, a binding may
// have been hand-edited — and the whole value of the delivery gate is that it
// asserts what is true NOW rather than what the stage believed when it wrote.
// Same rule the working-set poll follows.
func (l *loop) readVersionState(ctx workflow.Context) (delivery.VersionState, error) {
	var out delivery.VersionState
	if err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).ReadVersionState,
		ProjectRef{OrgID: l.in.OrgID, ProjectID: l.in.ProjectID}).Get(ctx, &out); err != nil {
		return delivery.VersionState{}, err
	}
	l.st.Version = out
	return out, nil
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
	if state == delivery.RunStateRunning {
		// Mirror what the repository's SetState does to the row: a resumed run is
		// not waiting on anything. The row alone is not enough — QueryRunStatus
		// answers out of l.st, so leaving these set would have a query report a
		// deploying run as still parked on credentials that already arrived.
		l.st.WaitingReason = ""
		l.st.BlockingDependencies = nil
	}
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

func (l *loop) closeMilestone(ctx workflow.Context) error {
	return workflow.ExecuteActivity(activityCtx(ctx), (*Activities).CloseMilestone, l.milestoneRef()).Get(ctx, nil)
}

// haltUnfinishedWork marks the issues THIS run was working and could not finish,
// so the reconcile sweep does not immediately restart them.
//
// It runs on every FAILED settle, in every workflow, and it is what makes a
// budget mean anything at all. A failed run leaves its working set OPEN — the
// milestone stays open too, because the way forward is more work in the same
// version — and the sweep's rule is "open work of a species, no live run, start
// one". So without this the run that just exhausted `fix-chain-budget` is
// replaced within a tick by a fresh run with a fresh budget, on the same issues,
// forever. Every budget in the system is defeated at once, and the symptom is an
// unexplained cloud bill rather than a test failure.
//
// It covers the recovery issues the run filed ITSELF — a deploy fix minted at the
// last boundary is in the working set like any other — for the same reason: those
// are precisely the issues a restarted run would pick up first.
//
// A VALIDATION run halts nothing, and that is a decision rather than an omission
// (delivery.InWorkingSet answers the empty set for its kind). It polls no working
// set: its own work is the version's validation task, which it closes on every
// ending, and the repair issues a failed verdict files are deliberately somebody
// else's work — an ordinary task run's — as is the conflict issue the event plane
// mints for a validation pull request that will not rebase. Halting those would
// break the repair chain rather than protect a budget, so the activity is skipped
// outright rather than called and asked to do nothing.
//
// Best-effort in spirit but NOT in error handling: an activity failure here
// propagates, because the halt is under the same unbounded retry policy as every
// other write and a stall is preferable to a settle that silently re-arms the
// sweep. The label is cleared by a rebuild, or by a person removing it.
func (l *loop) haltUnfinishedWork(ctx workflow.Context, reason string) error {
	if l.kind() == delivery.RunKindValidation {
		return nil
	}
	var halted []int
	err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).HaltUnfinishedWork,
		HaltWorkInput{
			OrgID:           l.in.OrgID,
			ProjectID:       l.in.ProjectID,
			MilestoneNumber: l.in.MilestoneNumber,
			Kind:            l.kind(),
			Reason:          reason,
		}).Get(ctx, &halted)
	if err != nil {
		return err
	}
	if len(halted) > 0 {
		workflow.GetLogger(ctx).Info("run failed; halted the work it could not finish",
			"milestone", l.in.MilestoneNumber, "reason", reason, "issues", halted)
	}
	return nil
}

// closeCancelledWork closes the issues THIS run had in flight when a person
// abandoned it, stamping `aep:cancelled` on each.
//
// It is the halt's sibling and it defeats the same mechanism from the other
// ending. A cancelled run leaves its issues OPEN, and the sweep's rule is "open
// work of a species, no live run, start one" — so without this the run the user
// just cancelled is restarted within a tick, dispatches an agent, and the cancel
// button reads as having done nothing but cost money. Closing the issues is the
// suppression; the label is the way back.
//
// WHAT it closes is per KIND (delivery.InCancelledWork), and the dev case is
// deliberately wider than any working set: a cancelled BUILD abandons the whole
// increment, so the dispatch GATES go with the working set and the milestone is
// closed behind them. That is the asymmetry with the halt: a halted run may be
// retried in the same version, so its gates still name dependencies somebody has
// to resolve, and closing them would erase the record of what the version was
// waiting on.
//
// Two populations survive even a build's cancel. The version's VALIDATION TASK,
// because it is a handle on software still deployed. And the LEDGER — a human's
// unarmed note is not the platform's to close, and closing it would put a machine
// comment on somebody's own record.
//
// A task run's cancel reaches only the bugs and conflicts it was working. The
// version it works is the DEPLOYED one and is not being abandoned, so its plan,
// its validation task and its milestone are untouched, and the way forward is to
// reopen the bugs or file new ones.
//
// A VALIDATION run closes nothing here, and that is a decision rather than an
// omission. Its consequence is the version's validation task, and settleJudged
// already closes that on EVERY ending — scoped to the task this run ADOPTED,
// which is the narrowing that matters: a run cancelled before its first read
// adopted nothing, and the task stays open for the next trigger. Reaching the
// milestone from here would close it anyway and turn "validate again" into
// "file the task again".
//
// Nothing is REVERTED. Commits a cycle merged stay on `main` and components it
// promoted keep serving, so closing the milestone says the increment was
// abandoned — never that the release was withdrawn.
//
// Best-effort in spirit but NOT in error handling, exactly like the halt: an
// activity failure propagates, because a cancel that silently left the sweep
// armed is the bug this exists to prevent, and a visible stall is preferable.
func (l *loop) closeCancelledWork(ctx workflow.Context) error {
	if l.kind() == delivery.RunKindValidation {
		return nil
	}
	var closed []int
	err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).CloseCancelledWork,
		CloseCancelledWorkInput{
			OrgID:           l.in.OrgID,
			ProjectID:       l.in.ProjectID,
			MilestoneNumber: l.in.MilestoneNumber,
			Kind:            l.kind(),
		}).Get(ctx, &closed)
	if err != nil {
		return err
	}
	if len(closed) > 0 {
		workflow.GetLogger(ctx).Info("run cancelled; closed the work it had in flight",
			"milestone", l.in.MilestoneNumber, "kind", l.kind(), "issues", closed)
	}
	return nil
}

// mintDeployFixIssues files one issue per component that did not come up, so the
// next cycle can work it like any other fix.
//
// Only components this pass WROTE, or was already waiting on, can appear here —
// that is a property of the wait set, not a filter applied at this call. Held and
// unbuilt components are never waited on, so a healthy web app whose api is red
// cannot be handed a deployment bug it does not have.
//
// This is the supervisor minting an issue, which it does nowhere else — every
// other recovery issue belongs to the event plane, which observes the failure
// through a webhook. A deployment produces no webhook, so there is no event
// plane to route this through, and a failure nobody files is a failure the loop
// forgets on its next boundary poll.
func (l *loop) mintDeployFixIssues(ctx workflow.Context) error {
	if len(l.deployFailed) == 0 {
		return nil
	}
	err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).MintDeployFixIssues,
		MintDeployFixIssuesInput{
			OrgID:           l.in.OrgID,
			ProjectID:       l.in.ProjectID,
			MilestoneNumber: l.in.MilestoneNumber,
			Failed:          l.deployFailed,
			Reasons:         l.deployFailures,
		}).Get(ctx, nil)
	if err != nil {
		return err
	}
	workflow.GetLogger(ctx).Info("deployment failed; filed fix work",
		"components", delivery.TargetNames(l.deployFailed))
	l.deployFailed, l.deployFailures = nil, nil
	return nil
}
