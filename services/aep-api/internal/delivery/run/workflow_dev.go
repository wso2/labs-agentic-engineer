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
	if gerr := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).ProvisionGates, in).Get(ctx, nil); gerr != nil {
		res, err = l.settle(ctx, delivery.RunStateFailed, delivery.RunReasonPlanFailed)
		if err != nil {
			return true, l.result(), err
		}
		workflow.GetLogger(ctx).Error("provisioning the version's gates failed", "error", gerr)
		return true, res, nil
	}
	if l.in.Rebuild {
		// The milestone is already filled — the click reopened exactly the issues
		// the cancel closed, marker and all. Re-planning here would mint nothing
		// (every title slug is already in the milestone) and the run would then
		// settle an unbuilt version as delivered. See RunInput.Rebuild.
		workflow.GetLogger(ctx).Info("rebuilding an unchanged version — its milestone is already filled, not re-planning",
			"milestone", l.in.MilestoneNumber, "tag", l.in.Tag)
		return false, RunResult{}, nil
	}
	if perr := workflow.ExecuteActivity(planActivityCtx(ctx), (*Activities).PlanMilestone, in).Get(ctx, nil); perr != nil {
		res, err = l.settle(ctx, delivery.RunStateFailed, delivery.RunReasonPlanFailed)
		if err != nil {
			return true, l.result(), err
		}
		workflow.GetLogger(ctx).Error("planning the version's tasks failed", "error", perr)
		return true, res, nil
	}
	return false, RunResult{}, nil
}

// deliverVersion is the dev run's onEmpty bookend: the version is deployed-green,
// so file its validation task and settle.
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
func (l *loop) deliverVersion(ctx workflow.Context) (RunResult, error) {
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
