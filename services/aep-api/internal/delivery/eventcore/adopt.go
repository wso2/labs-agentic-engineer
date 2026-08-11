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
	"fmt"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// AdoptTarget is the issue being handed to the coding agent, plus the
// milestone it already belongs to (0 when it is a bare issue). The webhook
// path reads both out of the payload — every issues delivery embeds the full
// issue — so adoption costs no extra GitHub read.
type AdoptTarget struct {
	Number          int
	MilestoneNumber int
	MilestoneTitle  string
}

// AdoptIssue hands one issue to the coding agent, from either of the two
// adoption routes: the `aep:codingagent` label arriving by webhook, and the
// console's dispatch button (which calls this directly, because a label the
// platform stamps itself comes back as an echo and is dropped).
//
// The rules, in order:
//
//   - An issue that already has a milestone keeps it. The human put it there.
//   - A bare issue joins the milestone of the version it is an incident
//     against: the deployed one, or — when nothing has been deployed yet — the
//     spec build currently in flight. With neither, the caller gets
//     delivery.ErrNoAdoptableMilestone rather than a guess.
//   - Adoption STAMPS the agent-work label. See below.
//   - If a run is already live on that milestone, no second run starts —
//     that would put two agents on one branch. The live run is woken instead,
//     because a run parked on an empty working set has no other way to learn
//     that work arrived.
//   - Otherwise an incident run starts over that milestone.
//
// Adoption stamps delivery.LabelAgentWork because the working set is the
// milestone's `aep`-labelled issues (MilestoneIssueCounts.OpenNonGateWork), so
// an adopted issue without it is invisible to the dispatch predicate: the run
// starts, finds nothing to work, and parks forever. Membership of the milestone
// is NOT sufficient on its own — ledger issues live there too, which is the
// distinction the label exists to draw.
//
// This does not blur who adopted the issue. `aep:codingagent` records the act
// of adoption and survives untouched; `aep` records the consequence — that the
// issue is now agent work. Two facts, two labels, and the second is the
// platform's to write precisely because it is derived from the first.
func (e *Events) AdoptIssue(ctx context.Context, orgID, projectID string, target AdoptTarget) error {
	if target.Number == 0 || e.p.Runs == nil {
		return nil
	}
	milestone := MilestoneRef{Number: target.MilestoneNumber, Title: target.MilestoneTitle}
	if milestone.Number == 0 {
		resolved, err := e.adoptableMilestone(ctx, orgID, projectID)
		if err != nil {
			return err
		}
		milestone = *resolved
		if e.p.Issues != nil {
			if err := e.p.Issues.SetIssueMilestone(ctx, orgID, projectID, target.Number, milestone.Number); err != nil {
				return err
			}
		}
		slog.InfoContext(ctx, "eventcore: adopted a bare issue into a version's milestone",
			"issue", target.Number, "milestone", milestone.Number, "version", milestone.Title)
	}

	// Before any run is started or woken: an issue that is not agent work would
	// send it straight back to an empty working set.
	if e.p.Issues != nil {
		if err := e.p.Issues.AddLabels(ctx, orgID, projectID, target.Number, []string{delivery.LabelAgentWork}); err != nil {
			return fmt.Errorf("adopt issue %d: mark as agent work: %w", target.Number, err)
		}
	}

	live, err := e.p.Runs.LiveRunForMilestone(ctx, orgID, projectID, milestone.Number)
	if err != nil {
		return err
	}
	if live != nil {
		slog.DebugContext(ctx, "eventcore: adoption into a milestone with a live run",
			"issue", target.Number, "milestone", milestone.Number, "run", live.ID)
		// A RUNNING run re-reads its milestone at the next cycle boundary. A
		// WAITING one is parked and re-derives only when told to — and the label
		// this call just wrote comes back as a suppressed echo, so the webhook
		// path will not tell it.
		return e.wakeIfWorkable(ctx, orgID, projectID, milestone.Number)
	}
	return e.startRun(ctx, orgID, projectID, milestone)
}

// adoptableMilestone is the version a bare issue belongs to: the deployed one
// first, then a spec build still in flight.
//
// The in-flight fallback is what makes an incident filed DURING a project's
// first build adoptable. That is not an edge case — it is the common one: the
// SRE/RCA agent fires on an alert raised by the very deployment the build is
// performing, so its handoff routinely lands minutes before the run that caused
// it reaches `succeeded`. Refusing there dropped the handoff permanently, since
// nothing retries it.
//
// Attaching to the in-flight run is also the same shape the platform already
// uses for a red build inside a run: the fix issue joins that run's milestone
// and the run works it in a later cycle.
func (e *Events) adoptableMilestone(ctx context.Context, orgID, projectID string) (*MilestoneRef, error) {
	deployed, err := e.p.Runs.DeployedMilestoneRun(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if deployed != nil {
		return &MilestoneRef{Number: deployed.MilestoneNumber, Title: deployed.MilestoneTitle}, nil
	}

	live, err := e.p.Runs.LiveRunsForProject(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	for i := range live {
		if live[i].Origin == delivery.RunOriginSpecBuild {
			slog.InfoContext(ctx, "eventcore: no deployed version — adopting into the spec build in flight",
				"project", projectID, "milestone", live[i].MilestoneNumber, "run", live[i].ID)
			return &MilestoneRef{Number: live[i].MilestoneNumber, Title: live[i].MilestoneTitle}, nil
		}
	}
	return nil, delivery.ErrNoAdoptableMilestone
}

// startRun asks the supervisor for an incident run over a milestone. Every run
// this package starts BY DETECTION is an incident adoption — the spec-build
// origin belongs to the plan path alone, where the version mutex lives, and the
// revalidate origin is only ever asked for by a human (Revalidate below).
func (e *Events) startRun(ctx context.Context, orgID, projectID string, milestone MilestoneRef) error {
	if e.p.Starter == nil {
		slog.DebugContext(ctx, "eventcore: no run starter wired — nothing to start",
			"project", projectID, "milestone", milestone.Number)
		return nil
	}
	return e.p.Starter.StartRun(ctx, delivery.StartRunRequest{
		OrgID:           orgID,
		ProjectID:       projectID,
		MilestoneNumber: milestone.Number,
		MilestoneTitle:  milestone.Title,
		Origin:          delivery.RunOriginIncidentAdoption,
	})
}

// Revalidate asks a version's acceptance criteria again, against the system
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
	// validation issue must not read as unfinished work, and neither is something
	// a coding cycle would pick up.
	if counts != nil && counts.OpenNonGateWork() > 0 {
		return "", delivery.ErrMilestoneHasOpenWork
	}
	hasCriteria, cerr := e.p.Criteria.HasValidationCriteria(ctx, orgID, projectID)
	if cerr != nil {
		return "", cerr
	}
	if !hasCriteria {
		return "", delivery.ErrNoAcceptanceCriteria
	}

	slog.InfoContext(ctx, "eventcore: revalidating a deployed version",
		"project", projectID, "milestone", milestone.Number, "version", milestone.Title,
		"validationAttempts", attempts, "cycleCeiling", ceiling)
	if serr := e.p.Starter.StartRun(ctx, delivery.StartRunRequest{
		OrgID:              orgID,
		ProjectID:          projectID,
		MilestoneNumber:    milestone.Number,
		MilestoneTitle:     milestone.Title,
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
