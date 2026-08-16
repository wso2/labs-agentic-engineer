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

package build

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// The MILESTONE PLAN PATH: everything the build click does once the whole-spec
// gate has passed and the `v<N>` tag is cut.
//
//	supersede v<N-1>  →  create milestone "v<N>"  →  admit the run row
//	                                              └→ (detached) plan Tasks into
//	                                                 it, mint its gates, start
//	                                                 the supervisor
//
// It lives in `build` because the ORDER is a property of the build click, and
// `Service.Run` already is that click's ordered sequence: every step before it
// (the Temporal probe, the repo lookup, the drawer's pre-tag work, the
// dependency hard gate, the tag cut) is an input to it, and the 409 that
// protects it is the same endpoint's. The two halves it cannot own itself — the
// planning turn (a `task` concern; the milestone number has to ride each issue
// CREATE, which happens inside the planner's streaming tap) and the gate
// resolvers (a `dependencies/provisioning` concern) — are reached through root
// ports, the same way the build already reaches the task reads.
//
// WHERE THE SEQUENCE SPLITS, and why: the run row is admitted BEFORE planning,
// not after. The row IS the spec-run mutex (§5's 409 in DB form); a planning
// turn is minutes of LLM time, so admitting afterwards would leave the mutex
// unarmed for exactly the window a double-click lands in. Planning then runs
// detached from the request — the POST answers with its tag as soon as the
// version is claimed, as it always has.

// planPath carries the collaborators of the milestone plan path. It is a
// separate value from Service's build-sequence ports so an unwired plan path is
// obviously unwired (nil) rather than a Service with half its fields empty.
type planPath struct {
	milestones MilestoneClient
	runs       MilestoneRunStore
	planner    SpecPlanner
	gates      GateResolver
	starter    RunStarter
}

// PlanPathDeps wires the milestone plan path. It is set separately from
// NewService (SetPlanPath) because the gate resolver is the provisioning
// service, which the composition root builds AFTER the build service — the
// same ordering knot SetProviderBuildTrigger unties in the other direction.
type PlanPathDeps struct {
	Milestones MilestoneClient
	Runs       MilestoneRunStore
	Planner    SpecPlanner
	// Gates is optional: a project with no drawer inputs and no design
	// dependencies mints no gate, and an unwired resolver simply mints none.
	Gates GateResolver
	// Starter is the run supervisor. Optional in the same sense the event
	// plane's is: without one the run row exists and waits, and the reconcile
	// sweep re-offers the milestone once a supervisor is wired.
	Starter RunStarter
}

// SetPlanPath wires the plan path. Until it is called the build sequence has no
// milestone half — Run cuts its tag and returns, which is what every test that
// only exercises the gate/tag half relies on.
func (s *Service) SetPlanPath(d PlanPathDeps) {
	s.plan = &planPath{
		milestones: d.Milestones,
		runs:       d.Runs,
		planner:    d.Planner,
		gates:      d.Gates,
		starter:    d.Starter,
	}
}

// activeSpecRun is the endpoint's 409 pre-check: one live spec run per project.
// The DB's partial unique index is the authority (TryAdmit's ON CONFLICT DO
// NOTHING is the race backstop); this read exists so a user who clicks build
// twice gets a conflict that names itself instead of a bare insert failure.
func (s *Service) activeSpecRun(ctx context.Context, orgID, projectID string) error {
	if s.plan == nil || s.plan.runs == nil {
		return nil
	}
	run, err := s.plan.runs.ActiveSpecRunByProject(ctx, orgID, projectID)
	if err != nil {
		return &EdgeError{Status: 500, Message: "lookup active spec run"}
	}
	if run != nil {
		return ErrBuildAlreadyRunning
	}
	return nil
}

// claimVersion is the synchronous half of the plan path: supersede the previous
// milestone, mint this version's, and admit the run row that arms the mutex. It
// returns the admitted run, or ErrBuildAlreadyRunning when another entrant won
// the admission race.
//
// Everything here is a GitHub round trip or a single INSERT — bounded work the
// POST can afford, unlike the planning turn that follows it.
func (s *Service) claimVersion(ctx context.Context, orgID, projectID string, scope spec.BuildScope) (*delivery.MilestoneRun, error) {
	p := s.plan
	tag, milestoneTitle := scope.Tag, scope.MilestoneTitle()
	// Supersede FIRST — but never a milestone of the title being claimed: a
	// re-tag that lands on the SAME milestone tops it up, and closing its open
	// work would abandon the very tasks the delta plan is about to extend.
	s.supersedePreviousMilestone(ctx, orgID, projectID, milestoneTitle)

	// CreateMilestone is get-or-create by title, so a claim that resolves to a
	// title this project already has reuses it (res.Created=false).
	res, err := p.milestones.CreateMilestone(ctx, orgID, projectID, sourcecontrol.CreateMilestoneRequest{
		Title:       milestoneTitle,
		Description: "Delivery increment and ledger for spec " + tag + ".",
	})
	if err != nil {
		return nil, &EdgeError{Status: 502, Message: "create milestone: " + err.Error()}
	}

	admitted, row, err := p.runs.TryAdmit(ctx, &delivery.MilestoneRun{
		OrgID:           orgID,
		ProjectID:       projectID,
		MilestoneNumber: res.Number,
		MilestoneTitle:  milestoneTitle,
		Tag:             tag,
		Origin:          delivery.RunOriginSpecBuild,
		// PLANNING, not waiting: the run has not filled its milestone yet, so for the next
		// minutes this row is a version being written, not a run parked on
		// something a human has to do. Admitting as waiting is what made the
		// console tell users their build was held while it was busy.
		State: delivery.RunStatePlanning,
	})
	if err != nil {
		return nil, &EdgeError{Status: 500, Message: "admit milestone run: " + err.Error()}
	}
	if !admitted {
		// The partial unique index refused the insert: a concurrent click got
		// there between the pre-check and here. Same answer as the pre-check.
		return nil, ErrBuildAlreadyRunning
	}
	slog.InfoContext(ctx, "build: version claimed",
		"org", orgID, "project", projectID, "tag", tag,
		"milestone", res.Number, "milestoneCreated", res.Created, "run", row.ID)
	return row, nil
}

// supersedePreviousMilestone closes the previous version's still-open work and
// then the milestone itself (§6). It is deliberately best-effort per issue: a
// single close failure leaves one stale issue behind, which the human can close,
// whereas failing the build would strand the whole next version.
//
// The previous milestone is found through the RUN ROWS, never by title match:
// GitHub milestone titles are freely renamable and its title filters are
// case-insensitive while create-uniqueness is not, so the number recorded on the
// row is the only sound index. Any milestone number this project has ever run a
// SPEC BUILD on, other than the one being cut now, is a previous version.
func (s *Service) supersedePreviousMilestone(ctx context.Context, orgID, projectID, milestoneTitle string) {
	p := s.plan
	rows, err := p.runs.ListByProject(ctx, orgID, projectID)
	if err != nil {
		slog.WarnContext(ctx, "build: list runs for supersede failed — skipping",
			"project", projectID, "error", err)
		return
	}
	prev, ok := previousSpecMilestone(rows, milestoneTitle)
	if !ok {
		return // first version of this project: nothing to supersede
	}

	comment := fmt.Sprintf("Superseded by %s.", milestoneTitle)
	// Both populations, in the order §6 names them: the agent work first, then
	// the gates that were holding it. `state: open` is the filter; no label
	// filter, because a milestone's leftovers are exactly "everything still open
	// in it" — a ledger-only human issue included, since carrying it into the
	// next version would make it agent work nobody asked for.
	issues, err := p.milestones.ListMilestoneIssues(ctx, orgID, projectID, sourcecontrol.MilestoneIssuesFilter{
		Number: prev.MilestoneNumber,
		State:  "open",
	})
	if err != nil {
		slog.WarnContext(ctx, "build: list previous milestone issues failed — closing the milestone only",
			"project", projectID, "milestone", prev.MilestoneNumber, "error", err)
	}
	closed := 0
	for _, issue := range gatesLast(issues) {
		if cerr := p.milestones.CloseIssue(ctx, orgID, projectID, issue.Number, comment); cerr != nil {
			slog.WarnContext(ctx, "build: close superseded issue failed",
				"project", projectID, "issue", issue.Number, "error", cerr)
			continue
		}
		closed++
	}
	if cerr := p.milestones.CloseMilestone(ctx, orgID, projectID, prev.MilestoneNumber); cerr != nil {
		slog.WarnContext(ctx, "build: close superseded milestone failed",
			"project", projectID, "milestone", prev.MilestoneNumber, "error", cerr)
	}
	slog.InfoContext(ctx, "build: superseded previous milestone",
		"project", projectID, "milestone", prev.MilestoneNumber, "title", prev.MilestoneTitle,
		"issuesClosed", closed, "supersededBy", milestoneTitle)
}

// previousSpecMilestone picks the newest spec-build milestone whose title is
// not the one being claimed. rows arrive newest-first from the repository.
//
// Comparing on TITLE here is not a GitHub title match: it compares the
// platform-recorded milestone title against the title being claimed, both
// platform-side values. A claim that resolves to a milestone this project
// already has matches and is skipped — a milestone must never supersede itself.
func previousSpecMilestone(rows []delivery.MilestoneRun, milestoneTitle string) (delivery.MilestoneRun, bool) {
	for i := range rows {
		if rows[i].Origin != delivery.RunOriginSpecBuild {
			continue // incident runs work their own (older) milestones
		}
		if rows[i].MilestoneTitle == milestoneTitle {
			continue
		}
		return rows[i], true
	}
	return delivery.MilestoneRun{}, false
}

// gatesLast orders a superseded milestone's issues so the agent work closes
// before the gates that were holding it, matching §6's sequence. It matters
// only for what an observer sees in the issue timeline — the end state is the
// same — but a gate closing before the work it gated reads as a resolution
// rather than an abandonment.
func gatesLast(issues []sourcecontrol.IssueInfo) []sourcecontrol.IssueInfo {
	out := make([]sourcecontrol.IssueInfo, 0, len(issues))
	for _, i := range issues {
		if !delivery.HasLabel(i.Labels, delivery.LabelProvisionGate) {
			out = append(out, i)
		}
	}
	for _, i := range issues {
		if delivery.HasLabel(i.Labels, delivery.LabelProvisionGate) {
			out = append(out, i)
		}
	}
	return out
}

// startRun hands the claimed version to the supervisor, which fills its
// milestone — gates, then the planning turn — as the run's first phase.
//
// The click carries the Tag and the provision inputs into the request because
// only the caller knows this is a version being FILLED rather than a run being
// resumed; the sweep and the adoption paths leave both empty and the workflow
// skips planning. See delivery.StartRunRequest.
//
// A supervisor that cannot start is the one failure this path still settles
// itself. Everything the run does after this point is the workflow's to fail,
// with Temporal's retries behind it.
func (s *Service) startRun(ctx context.Context, orgID, projectID, tag string,
	run *delivery.MilestoneRun, inputs []delivery.ProvisionInput) error {
	p := s.plan
	if p.starter == nil {
		slog.InfoContext(ctx, "build: no run supervisor wired — run row waits",
			"project", projectID, "run", run.ID, "milestone", run.MilestoneNumber)
		return nil
	}
	err := p.starter.StartRun(ctx, delivery.StartRunRequest{
		OrgID:           orgID,
		ProjectID:       projectID,
		MilestoneNumber: run.MilestoneNumber,
		MilestoneTitle:  run.MilestoneTitle,
		Origin:          delivery.RunOriginSpecBuild,
		RunID:           run.ID,
		Tag:             tag,
		ProvisionInputs: inputs,
	})
	if err == nil {
		return nil
	}
	s.failRun(ctx, run, fmt.Errorf("start run: %w", err))
	if errors.Is(err, delivery.ErrRunNotStarted) {
		// The platform is not ready to work this version — no workflow engine, no
		// agent dispatcher. A 503 says so, and because the row above is now
		// terminal the user's next click is admitted rather than refused by a run
		// that never ran.
		return &EdgeError{Status: 503, Message: "the platform is not ready to work this version — try again shortly"}
	}
	return &EdgeError{Status: 502, Message: "start run: " + err.Error()}
}

// failRun settles a run the plan path could not fill, so the mutex it armed is
// released. The reason names exactly this failure class, keeping the terminal
// reasons honest.
//
// The settle write DELIBERATELY outlives the request. This runs on the click's
// own context, and a user who navigates away — or a proxy that times out — while
// StartRun is in flight would otherwise cancel the one write that makes the row
// terminal. The row would stay `planning`, which is non-terminal, and the
// project's spec mutex would stay held: no later build could be admitted, and
// the reconcile sweep counts `planning` as live so nothing would heal it. A
// cancelled client must not be able to wedge a project.
func (s *Service) failRun(ctx context.Context, run *delivery.MilestoneRun, cause error) {
	slog.ErrorContext(ctx, "build: milestone plan path failed — settling the run",
		"project", run.ProjectID, "run", run.ID, "milestone", run.MilestoneNumber, "error", cause)
	settleCtx := context.WithoutCancel(ctx)
	if _, err := s.plan.runs.Settle(settleCtx, run.ID, delivery.RunStateFailed, delivery.RunReasonPlanFailed); err != nil {
		slog.ErrorContext(ctx, "build: settling the failed run ALSO failed — the project's spec mutex is held",
			"project", run.ProjectID, "run", run.ID, "error", err)
	}
}
