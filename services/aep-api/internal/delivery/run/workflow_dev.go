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

// DevRunWorkflow DELIVERS A VERSION: fill the milestone, work it until every
// touched component is serving, mint the version's validation task, settle.
//
// It never validates. Judging used to be this loop's last cycle, and pulling it
// out is what the split is for: "is the increment built" and "does the deployed
// system hold" have different lifetimes (a version is re-judged long after it
// shipped), different failure classes, and — the part that decided it — different
// answers to "what happens when the agent dies". A dev run whose validation
// agent died had to fail the whole version; a validation run that dies leaves the
// version deployed and unjudged, which is honest and recoverable by one click.
//
// It never returns an error for a run that reached a decision — a failed run is a
// SUCCEEDED workflow carrying a terminal reason, because "the increment could not
// be delivered" is an outcome the platform records, not a crash. A returned error
// means the supervisor itself could not function.
func DevRunWorkflow(ctx workflow.Context, in RunInput) (RunResult, error) {
	l := newLoop(ctx, in)
	if err := workflow.SetQueryHandler(ctx, delivery.QueryRunStatus, func() (delivery.RunStatus, error) {
		return l.st, nil
	}); err != nil {
		return RunResult{}, err
	}
	return l.work(ctx, bookends{
		work:    devWorkingSet,
		before:  l.fillMilestone,
		onEmpty: l.deliverVersion,
	})
}

// fillMilestone is the PLANNING phase: mint the version's dependency gates, then
// plan its Tasks into the milestone. It runs once, before the cycle loop.
//
// It lives here rather than in the build click because planning is the longest
// and most failure-prone step in a version's life — an LLM turn wrapped around
// git and GitHub — and the click had nowhere to put it but a detached goroutine.
// As a pair of activities it is durable across a worker restart, retried on a
// blip, and failed fast on an answer (planErr). None of that was true of the
// goroutine, where a seven-second connect timeout settled the whole version.
//
// Only a run that OWNS a version plans one: a dev run the reconcile sweep
// re-offers carries no Tag, and re-planning a milestone somebody already filled
// is what plansItsOwnMilestone exists to refuse.
//
// A REBUILD of an unchanged spec owns its version and still mints its gates, but
// its milestone was refilled by the click reopening what the cancel closed — so
// the planning TURN is skipped. It has to be: plan dedupe is the title slug
// against the milestone's issues in ANY state, so a re-plan over reopened work
// would recognise every slug, mint nothing, and hand the loop an empty working
// set to read as "delivered".
//
// A permanent failure settles the row `plan-failed` — the same terminal reason
// the click used to write, so the read model is unchanged.
//
// The predicate is shared with onEmptyWorkingSet on purpose: whether a run may
// read an empty working set as "delivered" is exactly the question of whether it
// planned that milestone itself, and two spellings of that could drift into a run
// settling a version it never filled.
func (l *loop) fillMilestone(ctx workflow.Context) (settled bool, res RunResult, err error) {
	if !l.plansItsOwnMilestone() {
		return false, RunResult{}, nil
	}
	l.st.Phase = delivery.RunPhasePlanning

	// CANCEL IS ASKED THREE TIMES IN THIS FUNCTION and the first is the row, not
	// the signal. It covers the one case the other two cannot: a cancel whose
	// signal never arrived. The cancel surface swallows a failed delivery so a
	// dead engine cannot wedge the console, and planning is the longest stretch a
	// run has — so without this read a lost signal costs the whole phase rather
	// than the moment. It is one database round trip per build.
	if cancelled, cerr := l.cancelled(ctx); cerr != nil {
		return true, l.result(), cerr
	} else if cancelled {
		res, err = l.settle(ctx, delivery.RunStateCancelled, "")
		return true, res, err
	}

	in := PlanMilestoneInput{
		OrgID:           l.in.OrgID,
		ProjectID:       l.in.ProjectID,
		MilestoneNumber: l.in.MilestoneNumber,
		Tag:             l.in.Tag,
		ProvisionInputs: l.in.ProvisionInputs,
	}
	// Gates FIRST. An open gate is a dispatch hold, so minting the gates before
	// the work is what makes the dispatch predicate honest from the moment the
	// first Task lands — the same order the click ran them in.
	//
	// gateActivityCtx, not activityCtx: this is the one activity here whose
	// retries are BOUNDED, because provisioning has answers repeating cannot
	// change and the unbounded default turned one of them into a permanent loop
	// that re-minted the version's gates on every attempt. See
	// gateActivityAttempts.
	cancelled, gerr := l.awaitInterruptibly(ctx, gateActivityCtx(ctx), (*Activities).ProvisionGates, in)
	if cancelled {
		workflow.GetLogger(ctx).Info("cancelled while provisioning the version's gates",
			"milestone", l.in.MilestoneNumber, "tag", l.in.Tag)
		res, err = l.settle(ctx, delivery.RunStateCancelled, "")
		return true, res, err
	}
	if gerr != nil {
		res, err = l.settle(ctx, delivery.RunStateFailed, delivery.RunReasonPlanFailed)
		if err != nil {
			return true, l.result(), err
		}
		workflow.GetLogger(ctx).Error("provisioning the version's gates failed", "error", gerr)
		return true, res, nil
	}
	if l.in.Rebuild {
		// The milestone is already filled — the click reopened exactly the issues
		// the cancel closed, marker and all, and CONFIRMED against the milestone
		// that it holds planned work at all. Re-planning here would mint nothing
		// (every title slug is already in the milestone) and the run would then
		// settle an unbuilt version as delivered. See RunInput.Rebuild, and the
		// cancel branch below for the one case this reads slightly generously.
		workflow.GetLogger(ctx).Info("rebuilding an unchanged version — its milestone is already filled, not re-planning",
			"milestone", l.in.MilestoneNumber, "tag", l.in.Tag)
		return false, RunResult{}, nil
	}
	cancelled, perr := l.awaitInterruptibly(ctx, planActivityCtx(ctx), (*Activities).PlanMilestone, in)
	if cancelled {
		// The turn is cut short mid-plan, so the milestone may hold SOME of the
		// version's work — the cancel closes and marks whatever landed, and a
		// rebuild reopens exactly that.
		//
		// KNOWN LIMITATION, and it is the milestone's shape that causes it: a
		// partial plan is indistinguishable from a complete one. Both hold
		// `development` issues, so build.reopenIncrement answers "filled", the
		// rebuild skips its planning turn, and the version ships the Tasks that
		// landed rather than the Tasks it declared. The way out is to change the
		// spec, which cuts a new tag and plans it fresh.
		//
		// It is not NEW here — a planning turn that failed permanently half-way
		// leaves the same shape — but a cancel makes it reachable on purpose, so
		// it is written down rather than left to be rediscovered. Fixing it means
		// either re-planning on every rebuild (additive and idempotent, but an LLM
		// turn the skip exists to save) or recording how far the turn got, which
		// is a fact the milestone cannot carry.
		workflow.GetLogger(ctx).Info("cancelled while planning the version's tasks",
			"milestone", l.in.MilestoneNumber, "tag", l.in.Tag)
		res, err = l.settle(ctx, delivery.RunStateCancelled, "")
		return true, res, err
	}
	if perr != nil {
		res, err = l.settle(ctx, delivery.RunStateFailed, delivery.RunReasonPlanFailed)
		if err != nil {
			return true, l.result(), err
		}
		workflow.GetLogger(ctx).Error("planning the version's tasks failed", "error", perr)
		return true, res, nil
	}
	return false, RunResult{}, nil
}

// deliverVersion is the dev run's onEmpty bookend: the milestone has no work
// left, so — IF the version is serving — file its validation task and settle.
//
// Minting HERE, and not at plan time, is load-bearing twice over. An issue
// nothing can work until every component deploys would sit in the working set and
// hold every cycle boundary open — a version that could never settle. And the
// coverage would be wrong: mid-run adoption postpones deployed-green by
// construction, so only a task minted at the end covers everything the run
// actually landed.
//
// The run then settles SUCCEEDED with an EMPTY verdict, which is the honest
// reading of "delivered, not yet judged": the validation run the sweep starts off
// this task owns the version's answer, and the read model reads the newest
// VALIDATING run on the milestone for exactly that reason
// (delivery.RunValidates).
//
// The one case that does record a verdict is a project with no acceptance oracle.
// EnsureValidationIssue answers 0, so no validation task exists, so nothing will
// ever judge this version — and an empty verdict would read as "any moment now"
// forever. `skipped` says what is true.
//
// # The gate: serving(V), asserted rather than inferred
//
// "Deployed-green" used to be INFERRED from a sequence of correct cycles — the
// working set emptied and the last cycle ended green, so everything must be up.
// That is the same class of predicate ADR-0017 removed from validation, and it
// failed the same way: a component the cycles never promoted was never
// contradicted by any of them, so a version shipped with an API nothing had
// bound, its validation task filed against software that was not running.
//
// So the version is READ here (ADR-0026) and every component in the DESIGN must
// be serving. Anything else settles `version-incomplete` naming what was not,
// which by the loop's invariants should be unreachable — a behind component
// whose providers are met is promoted by the cycle's reconcile, and a failed
// deployment mints a fix issue that keeps the working set non-empty. It is the
// case that survives being unreachable that this exists for.
func (l *loop) deliverVersion(ctx workflow.Context) (RunResult, error) {
	version, err := l.readVersionState(ctx)
	if err != nil {
		return l.result(), err
	}
	if !version.Serving() {
		workflow.GetLogger(ctx).Error("the milestone has no work left and the version is not serving",
			"milestone", l.in.MilestoneNumber, "tag", l.in.Tag, "notServing", version.Describe())
		return l.settle(ctx, delivery.RunStateFailed, delivery.RunReasonVersionIncomplete)
	}
	issue, err := l.ensureValidationIssue(ctx)
	if err != nil {
		return l.result(), err
	}
	if issue == 0 {
		if verr := l.setVerdict(ctx, noCycle, delivery.ValidationVerdictSkipped, ""); verr != nil {
			return l.result(), verr
		}
		return l.settle(ctx, delivery.RunStateSucceeded, "")
	}
	// Held in live status only. The issue is NOT written to this run's row: the
	// row's validation columns are the record of a JUDGEMENT, and this run makes
	// none — the validation run that works the task records the issue alongside
	// the verdict it produced, which is the only pairing a reader can act on.
	l.st.ValidationIssue = issue
	workflow.GetLogger(ctx).Info("version deployed-green; filed its validation task",
		"milestone", l.in.MilestoneNumber, "validationIssue", issue)
	return l.settle(ctx, delivery.RunStateSucceeded, "")
}
