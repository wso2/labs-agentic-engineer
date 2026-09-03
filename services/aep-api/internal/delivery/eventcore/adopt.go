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

package eventcore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// ErrNoDeployedMilestone is adoption's honest refusal: a bare issue joins the
// DEPLOYED version's milestone, and a project that has never completed a build
// has no such version. The message is written for a human because the console
// dispatch path returns it to one verbatim.
var ErrNoDeployedMilestone = errors.New("no milestone for the deployed version — trigger a build")

// AdoptTarget is the issue being handed to the coding agent, plus the
// milestone it already belongs to (0 when it is a bare issue). The webhook
// path reads both out of the payload — every issues delivery embeds the full
// issue — so adoption costs no extra GitHub read.
type AdoptTarget struct {
	Number          int
	MilestoneNumber int
	MilestoneTitle  string
	// Labels is the issue's label set, when the caller has it. Adoption starts a
	// TASK run, so it routes on the KIND these carry and refuses an issue that
	// belongs to another species (delivery.AdoptableByATaskRun).
	//
	// Empty means "unclassified", which adopts — the console's dispatch button
	// hands over a bare issue a human has not labelled at all, and that is the
	// ordinary path rather than a missing check.
	Labels []string
}

// AdoptIssue hands one issue to the coding agent, from either of the two
// adoption routes: the `aep` arming label arriving by webhook, and the console's
// dispatch button (which calls this directly, because a label the platform
// stamps itself comes back as an echo and is dropped).
//
// The rules, in order:
//
//   - An issue that already has a milestone keeps it. The human put it there.
//   - A bare issue joins the deployed version's milestone — the version it is
//     an incident against. With no deployed version there is nothing to attach
//     it to, and the caller gets ErrNoDeployedMilestone rather than a guess.
//   - If a run is already live on that milestone, this is a no-op: the run
//     re-reads its milestone at the next cycle boundary and picks the issue up
//     there. Starting a second run on one milestone would put two agents on
//     one branch.
//   - Otherwise an incident run starts over that milestone.
//
// Adoption does NOT stamp the arming label. The working set is read from the
// milestone, and arming IS the human's act of adoption — inventing a second,
// platform-authored path to the same state would make "who adopted this"
// unanswerable.
//
// Nor does it stamp a KIND. An armed issue carrying none reads as a bug to every
// working-set predicate (delivery.InDevWorkingSet), which is what a human
// handing over an unclassified issue means, and it is the same answer the host's
// counts give — the two must not disagree about one issue.
func (e *Events) AdoptIssue(ctx context.Context, orgID, projectID string, target AdoptTarget) error {
	if target.Number == 0 || e.p.Runs == nil {
		return nil
	}
	// Route on the kind BEFORE anything is written. An issue that belongs to
	// another species must not be pulled into a bug-fix run, and it must not be
	// moved into the deployed version's milestone on the way there either.
	if !delivery.AdoptableByATaskRun(target.Labels) {
		slog.DebugContext(ctx, "eventcore: not adopting — this issue is another run species' work",
			"issue", target.Number, "kind", delivery.KindOf(target.Labels))
		return nil
	}
	milestone := MilestoneRef{Number: target.MilestoneNumber, Title: target.MilestoneTitle}
	if milestone.Number == 0 {
		deployed, err := e.p.Runs.DeployedMilestoneRun(ctx, orgID, projectID)
		if err != nil {
			return err
		}
		if deployed == nil {
			return ErrNoDeployedMilestone
		}
		milestone = MilestoneRef{Number: deployed.MilestoneNumber, Title: deployed.MilestoneTitle}
		if e.p.Issues != nil {
			if err := e.p.Issues.SetIssueMilestone(ctx, orgID, projectID, target.Number, milestone.Number); err != nil {
				return err
			}
		}
		slog.InfoContext(ctx, "eventcore: adopted a bare issue into the deployed version's milestone",
			"issue", target.Number, "milestone", milestone.Number, "version", milestone.Title)
	}

	live, err := e.p.Runs.LiveRunForMilestone(ctx, orgID, projectID, milestone.Number)
	if err != nil {
		return err
	}
	if live != nil {
		// A no-op, and what makes it safe is the run's next CYCLE BOUNDARY: a dev
		// or task run re-reads its milestone there and picks the issue up.
		//
		// A live VALIDATION run has no such boundary — it polls no working set —
		// so an issue adopted while one is judging is picked up by the reconcile
		// sweep instead, once that run settles. Starting a second run here would be
		// worse than the wait: the per-milestone index refuses it, and two agents on
		// one branch is what the index exists to prevent.
		slog.DebugContext(ctx, "eventcore: adoption into a milestone with a live run — picked up at its next boundary",
			"issue", target.Number, "milestone", milestone.Number, "run", live.ID, "kind", live.Kind)
		return nil
	}
	return e.startRun(ctx, orgID, projectID, milestone)
}

// startRun asks the supervisor for a TASK run over a milestone: work the
// milestone's open defects. A dev run belongs to the plan path alone, where the
// version mutex lives.
func (e *Events) startRun(ctx context.Context, orgID, projectID string, milestone MilestoneRef) error {
	return e.start(ctx, projectID, delivery.StartRunRequest{
		OrgID:           orgID,
		ProjectID:       projectID,
		MilestoneNumber: milestone.Number,
		MilestoneTitle:  milestone.Title,
		Kind:            delivery.RunKindTask,
		Origin:          delivery.RunOriginIncidentAdoption,
	})
}

// startValidationRun asks the supervisor to JUDGE a version, because its
// validation task is open.
//
// The reconcile sweep is its caller, which makes this the platform's own trigger
// rather than a human's: a dev run settles having filed the task, and this is
// what turns that task into a run. The `revalidate` origin is honest either way —
// an origin is a label on the trigger, and what the run DOES is its kind.
//
// It carries no attempt allowance, so the run resolves the platform default. The
// per-version allowance is spent by the milestone's validation runs, counted from
// the ledger, so a sweep-started attempt cannot widen what a version is allowed.
func (e *Events) startValidationRun(ctx context.Context, orgID, projectID string, milestone MilestoneRef) error {
	return e.start(ctx, projectID, delivery.StartRunRequest{
		OrgID:           orgID,
		ProjectID:       projectID,
		MilestoneNumber: milestone.Number,
		MilestoneTitle:  milestone.Title,
		Kind:            delivery.RunKindValidation,
		Origin:          delivery.RunOriginRevalidate,
	})
}

// start is the shared ask, and the shared reading of a degraded boot.
func (e *Events) start(ctx context.Context, projectID string, req delivery.StartRunRequest) error {
	if e.p.Starter == nil {
		slog.DebugContext(ctx, "eventcore: no run starter wired — nothing to start",
			"project", projectID, "milestone", req.MilestoneNumber)
		return nil
	}
	err := e.p.Starter.StartRun(ctx, req)
	if errors.Is(err, delivery.ErrRunNotStarted) {
		// A degraded boot. This package re-offers on a timer — the reconcile
		// sweep runs every pass — so "not started yet" is nothing to report. The
		// callers that have no timer are the ones the sentinel exists for.
		slog.DebugContext(ctx, "eventcore: platform not ready to start a run — the sweep will re-offer",
			"project", projectID, "milestone", req.MilestoneNumber)
		return nil
	}
	return err
}

// Revalidate asks a version's validation criteria again, against the system
// already deployed.
//
// It is AdoptIssue's sibling and deliberately so: both hand a milestone to the
// run loop, both refuse to start a second run on one milestone, and both are
// reached from a human's request rather than from a delivery. The difference is
// what the run does first — adoption files work and the loop picks it up, while
// this files nothing and the loop enters at validation, because the milestone's
// working set is already empty.
//
// The three guards all run BEFORE the supervisor is asked, and the order is the
// cheap-and-certain first:
//
//  1. A live run on the milestone. This one ANSWERS the caller; it does not make
//     the invariant true. Two concurrent requests can both pass it, so the rule
//     itself lives in the database — a partial unique index admitting one
//     non-terminal run per milestone, which the insert's ON CONFLICT DO NOTHING
//     catches. Without that index the loser's row was admitted with no workflow
//     behind it (Temporal answers AlreadyStarted on the reused id) and, being
//     non-terminal, refused every later revalidation of that version forever.
//  2. Open work in the milestone.
//  3. An acceptance oracle to validate against.
//
// attempts and ceiling ride through untouched; zero on either means the
// platform default, resolved at the run row and again in the workflow.
func (e *Events) Revalidate(ctx context.Context, orgID, projectID string, milestone MilestoneRef, attempts, ceiling int) (runID string, err error) {
	// Criteria is required, not optional. Without it the oracle guard is skipped,
	// and a version with no criteria would run a revalidation that can only
	// conclude `skipped` — overwriting a real verdict, since the newest run owns
	// the version's answer. A misconfigured boot must refuse, not silently widen
	// what a revalidation may do.
	if e.p.Runs == nil || e.p.Issues == nil || e.p.Starter == nil || e.p.Criteria == nil {
		return "", fmt.Errorf("eventcore: revalidate not configured")
	}
	live, err := e.p.Runs.LiveRunForMilestone(ctx, orgID, projectID, milestone.Number)
	if err != nil {
		return "", err
	}
	if live != nil {
		return "", delivery.ErrRunAlreadyLive
	}
	counts, err := e.p.Issues.MilestoneIssueCounts(ctx, orgID, projectID, milestone.Number)
	if err != nil {
		return "", err
	}
	// The WORKING SET, not every open issue: a stray gate or the version's own
	// validation task must not read as unfinished work, and neither is something
	// a coding cycle would pick up.
	if counts != nil && counts.OpenDevWork() > 0 {
		return "", delivery.ErrMilestoneHasOpenWork
	}
	hasCriteria, cerr := e.p.Criteria.HasValidationCriteria(ctx, orgID, projectID)
	if cerr != nil {
		return "", cerr
	}
	if !hasCriteria {
		return "", delivery.ErrNoValidationCriteria
	}

	slog.InfoContext(ctx, "eventcore: revalidating a deployed version",
		"project", projectID, "milestone", milestone.Number, "version", milestone.Title,
		"validationAttempts", attempts, "cycleCeiling", ceiling)
	if serr := e.p.Starter.StartRun(ctx, delivery.StartRunRequest{
		OrgID:              orgID,
		ProjectID:          projectID,
		MilestoneNumber:    milestone.Number,
		MilestoneTitle:     milestone.Title,
		Kind:               delivery.RunKindValidation,
		Origin:             delivery.RunOriginRevalidate,
		ValidationAttempts: attempts,
		CycleCeiling:       ceiling,
	}); serr != nil {
		return "", serr
	}
	// The row the supervisor just admitted. Read back rather than returned by
	// StartRun, which is shared with the detection paths and has no caller waiting
	// on an id — a revalidation's caller does, since the run's progress stream is
	// keyed by it.
	//
	// Its ABSENCE is the more important answer. StartRun reports success for
	// several states in which nothing was actually started — no agent dispatcher
	// wired, the workflow engine unreachable, the admission losing a race — because
	// its other callers re-offer on a timer and a degraded boot must not fail them.
	// A human waiting on a verdict has no such loop, so an empty read here is
	// reported rather than dressed up as a 202 over a run that does not exist.
	started, err := e.p.Runs.LiveRunForMilestone(ctx, orgID, projectID, milestone.Number)
	if err != nil {
		return "", err
	}
	if started == nil {
		return "", delivery.ErrRunNotStarted
	}
	return started.ID, nil
}
