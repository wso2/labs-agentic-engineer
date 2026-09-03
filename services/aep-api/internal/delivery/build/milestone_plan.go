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
// not after. The row IS the build mutex (§5's 409 in DB form); a planning
// turn is minutes of LLM time, so admitting afterwards would leave the mutex
// unarmed for exactly the window a double-click lands in. Planning then runs
// detached from the request — the POST answers with its tag as soon as the
// version is claimed, as it always has.

// planPath carries the collaborators of the milestone plan path. It is a
// separate value from Service's build-sequence ports so an unwired plan path is
// obviously unwired (nil) rather than a Service with half its fields empty.
type planPath struct {
	milestones MilestoneClient
	// issues is the domain's issue-write surface, used here for exactly one
	// write: closing the previous version's leftovers as superseded.
	issues  *delivery.IssueWriter
	runs    MilestoneRunStore
	planner SpecPlanner
	gates   GateResolver
	starter RunStarter
}

// PlanPathDeps wires the milestone plan path. It is set separately from
// NewService (SetPlanPath) because the gate resolver is the provisioning
// service, which the composition root builds AFTER the build service — the
// same ordering knot SetProviderBuildTrigger unties in the other direction.
type PlanPathDeps struct {
	Milestones MilestoneClient
	// Issues closes the superseded version's still-open work. The domain's one
	// issue-write surface, so a supersede comment and a mint share a vocabulary.
	Issues  *delivery.IssueWriter
	Runs    MilestoneRunStore
	Planner SpecPlanner
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
		issues:     d.Issues,
		runs:       d.Runs,
		planner:    d.Planner,
		gates:      d.Gates,
		starter:    d.Starter,
	}
}

// activeDevRun is the endpoint's 409 pre-check: one live dev run per project.
// The DB's partial unique index is the authority (TryAdmit's ON CONFLICT DO
// NOTHING is the race backstop); this read exists so a user who clicks build
// twice gets a conflict that names itself instead of a bare insert failure.
func (s *Service) activeDevRun(ctx context.Context, orgID, projectID string) error {
	if s.plan == nil || s.plan.runs == nil {
		return nil
	}
	run, err := s.plan.runs.ActiveDevRunByProject(ctx, orgID, projectID)
	if err != nil {
		return &EdgeError{Status: 500, Message: "lookup active dev run"}
	}
	if run != nil {
		return ErrBuildAlreadyRunning
	}
	return nil
}

// activeValidationRun is the endpoint's other 409 pre-check: no live validation
// run anywhere in the project.
//
// Unlike activeDevRun there is no index behind this one — a validation run sits
// outside the build mutex on purpose — so this read IS the rule and not merely a
// nicer error for it. Two concurrent clicks can both pass it, and that is
// accepted: the loss is a build that starts while a verdict is being reached,
// which the per-milestone index still keeps from putting two agents on one
// branch.
func (s *Service) activeValidationRun(ctx context.Context, orgID, projectID string) error {
	if s.plan == nil || s.plan.runs == nil {
		return nil
	}
	run, err := s.plan.runs.ActiveValidationRunByProject(ctx, orgID, projectID)
	if err != nil {
		return &EdgeError{Status: 500, Message: "lookup active validation run"}
	}
	if run != nil {
		return ErrValidationRunLive
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
	// The new milestone FIRST, because supersede now MOVES the previous version's
	// open bugs and needs somewhere to move them to. Nothing else about the order
	// changed: CreateMilestone is get-or-create by title, so a claim that resolves
	// to a title this project already has reuses it (res.Created=false) and the
	// call is safe to make before anything is superseded.
	res, err := p.milestones.CreateMilestone(ctx, orgID, projectID, sourcecontrol.CreateMilestoneRequest{
		Title:       milestoneTitle,
		Description: "Delivery increment and ledger for spec " + tag + ".",
	})
	if err != nil {
		return nil, &EdgeError{Status: 502, Message: "create milestone: " + err.Error()}
	}
	// Supersede BEFORE the run row is admitted, and never a milestone of the title
	// being claimed: a re-tag that lands on the SAME milestone tops it up, and
	// closing its open work would abandon the very tasks the delta plan is about
	// to extend. previousDevMilestone enforces that by title, so it holds whatever
	// order the milestone is minted in.
	s.supersedePreviousMilestone(ctx, orgID, projectID, milestoneTitle, res.Number)

	admitted, row, err := p.runs.TryAdmit(ctx, &delivery.MilestoneRun{
		OrgID:           orgID,
		ProjectID:       projectID,
		MilestoneNumber: res.Number,
		MilestoneTitle:  milestoneTitle,
		Tag:             tag,
		Kind:            delivery.RunKindDev,
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

// supersedePreviousMilestone empties the previous version's milestone and then
// closes it (§6). A PLAN is replaced by a plan; a DEFECT is not replaced by
// anything.
//
//	development, provision  CLOSED — the new plan supersedes the old one, and a
//	                        gate names a dependency the new plan re-mints.
//	bug                     MOVED into the new milestone. It is still broken, and
//	                        the new version is what will ship the fix.
//	conflict                CLOSED, not moved: it names a branch of the version
//	                        being superseded, which is about to be irrelevant.
//	everything else         CLOSED — the validation task (a verdict about a
//	                        version nobody is shipping any more) and ledger-only
//	                        human notes, whose carrying forward would make them
//	                        part of a version they were never about.
//
// Moving a bug is not ARMING it: an unadopted incident arrives in the new
// milestone still unarmed and still ledger-only, so carrying a defect forward can
// never turn it into agent work nobody asked for. It DOES clear `aep:halted`,
// because a new version is a fresh attempt at the defect and the run that gave up
// on it is long gone.
//
// The destination milestone must therefore already exist, which is why
// claimVersion mints it BEFORE calling this. Superseding first was the older
// order, and it had nowhere to move a bug to.
//
// It is deliberately best-effort per issue: a single close or move failure leaves
// one stale issue behind, which the human can close, whereas failing the build
// would strand the whole next version. A bug that failed to move stays open in the
// superseded milestone and the reconcile sweep will offer it to a task run — the
// right outcome for a defect, just recorded under the older version.
//
// The previous milestone is found through the RUN ROWS, never by title match:
// GitHub milestone titles are freely renamable and its title filters are
// case-insensitive while create-uniqueness is not, so the number recorded on the
// row is the only sound index. Any milestone number this project has ever run a
// SPEC BUILD on, other than the one being cut now, is a previous version.
func (s *Service) supersedePreviousMilestone(ctx context.Context, orgID, projectID, milestoneTitle string, into int) {
	p := s.plan
	rows, err := p.runs.ListByProject(ctx, orgID, projectID)
	if err != nil {
		slog.WarnContext(ctx, "build: list runs for supersede failed — skipping",
			"project", projectID, "error", err)
		return
	}
	prev, ok := previousDevMilestone(rows, milestoneTitle)
	if !ok {
		return // first version of this project: nothing to supersede
	}

	comment := fmt.Sprintf("Superseded by %s.", milestoneTitle)
	carried := fmt.Sprintf("Still open when %s superseded this version — carried forward, because a defect is not superseded by a new plan.", milestoneTitle)
	// Every population, in the order §6 names them: the agent work first, then
	// the gates that were holding it. `state: open` is the filter; no label
	// filter, because a milestone's leftovers are exactly "everything still open
	// in it" and which of them are carried forward is this function's decision to
	// make from the labels it can see.
	issues, err := p.milestones.ListMilestoneIssues(ctx, orgID, projectID, sourcecontrol.MilestoneIssuesFilter{
		Number: prev.MilestoneNumber,
		State:  "open",
	})
	if err != nil {
		slog.WarnContext(ctx, "build: list previous milestone issues failed — closing the milestone only",
			"project", projectID, "milestone", prev.MilestoneNumber, "error", err)
	}
	closed, moved := 0, 0
	for _, issue := range gatesLast(issues) {
		// delivery.WorkKindOf, never KindOf: the kind a carry-forward decision reads
		// has to be the kind the WORKING SET reads, or the two disagree about what a
		// defect is. An ARMED issue carrying no kind is the common human hand-over —
		// adoption stamps no kind on purpose — and every predicate in the loop works
		// it as a bug, so KindOf's honest "" put it in the CLOSE arm and the next
		// version cut silently closed a defect somebody had adopted.
		if delivery.WorkKindOf(issue.Labels) == delivery.KindBug {
			// The move FIRST, then the note. A note on an issue that stayed behind
			// would claim a carry-forward that did not happen.
			if merr := p.issues.SetMilestone(ctx, orgID, projectID, issue.Number, into); merr != nil {
				slog.WarnContext(ctx, "build: carry a superseded bug forward failed",
					"project", projectID, "issue", issue.Number, "into", into, "error", merr)
				continue
			}
			moved++
			// A new version is a fresh attempt at the defect, so the halt goes with
			// the old one. `aep:halted` says "a run gave up on this and the
			// reconcile sweep must not restart it"; carrying it forward would hide
			// the bug from the sweep for the rest of the project's life, which is
			// the opposite of what carrying it forward is for. This is the
			// "cleared by a rebuild" half of the marker's contract.
			if delivery.HasLabel(issue.Labels, delivery.LabelHalted) {
				if uerr := p.issues.Unlabel(ctx, orgID, projectID, issue.Number, delivery.LabelHalted); uerr != nil {
					slog.WarnContext(ctx, "build: clearing the halt on a carried-forward bug failed",
						"project", projectID, "issue", issue.Number, "error", uerr)
				}
			}
			if cerr := p.issues.Comment(ctx, orgID, projectID, issue.Number, carried); cerr != nil {
				slog.WarnContext(ctx, "build: comment on a carried-forward bug failed",
					"project", projectID, "issue", issue.Number, "error", cerr)
			}
			continue
		}
		if cerr := p.issues.Close(ctx, orgID, projectID, issue.Number, comment); cerr != nil {
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
		"issuesClosed", closed, "bugsCarriedForward", moved, "supersededBy", milestoneTitle)
}

// reopenIncrement puts the version's milestone — and exactly the issues a CANCEL
// closed inside it — back into the state the cancel found them in, so the run
// admitted over it has a working set to work.
//
// It runs on the SAME-TAG branch of the build click and only there. The spec-save
// status is the whole decision: an unchanged spec resolves to the version already
// on disk, so this build is that version being worked AGAIN rather than a new one
// being cut. There is no "was it cancelled" question anywhere — the marker on the
// issues answers it, and a version nobody cancelled simply has none.
//
// `aep:cancelled` is the handle, and the reason it exists rather than "reopen
// everything in the milestone": only issues that were OPEN when the cancel landed
// carry it, so work a cycle genuinely FINISHED stays closed. Reopening wholesale
// would resurrect every Task the build had already delivered and dispatch an agent
// at work that is already merged and serving.
//
// The label is CLEARED as it is reopened. It is a record of one abandoned attempt,
// not a property of the issue: leaving it on would make the NEXT cancel's marked
// set the union of two attempts, and this reopen would then restore work the
// second cancel had deliberately left closed.
//
// Reopen FIRST, then unlabel. An issue that ended up open with the mark still on
// is reopened again by the next rebuild (a no-op) and is otherwise ordinary work;
// an issue unlabelled but still closed is invisible to every future rebuild and
// its work is silently lost.
//
// Best-effort per issue and per step, like supersede, and for the same reason: one
// issue left behind is a human's one click, where failing the click would strand
// the whole rebuild. The milestone reopen is unconditional on this branch — a
// version being worked whose milestone reads closed is a lie the console renders,
// whether it was a cancel or a delivered run that closed it.
//
// IT ALSO ANSWERS THE PLANNING QUESTION, and that second job is why it returns
// something. `filled` reports whether the milestone holds planned work at all —
// any `development` issue, in any state — which is the fact the run needs to know
// whether re-planning it would be a no-op. It is answered HERE because this
// function already holds the milestone's whole issue list, and a second caller
// asking the host the same question is a second round trip and a second reading
// of the same labels. See Service.Run for what the answer decides.
func (s *Service) reopenIncrement(ctx context.Context, orgID, projectID string, milestoneNumber int) (filled bool) {
	p := s.plan
	if rerr := p.milestones.ReopenMilestone(ctx, orgID, projectID, milestoneNumber); rerr != nil {
		slog.WarnContext(ctx, "build: reopening the rebuilt version's milestone failed",
			"project", projectID, "milestone", milestoneNumber, "error", rerr)
	}
	// The milestone's issues in EVERY state, unfiltered by label: which of them this
	// rebuild owns is decided here, from the labels, exactly as supersede decides
	// what it carries forward.
	//
	// `all` rather than `closed`, because the two questions this list answers want
	// different states and the wider one is a superset. The reopen half still acts
	// only on what is closed; the planning half has to see the milestone WHOLE — a
	// version whose Tasks are all still open is as filled as one whose Tasks a
	// cancel closed, and asking only for the closed ones would read it as unplanned
	// and plan it twice.
	issues, err := p.milestones.ListMilestoneIssues(ctx, orgID, projectID, sourcecontrol.MilestoneIssuesFilter{
		Number: milestoneNumber,
		State:  "all",
	})
	if err != nil {
		// NOT filled, and the caller re-plans. A read failure must fail towards the
		// LLM turn that mints nothing over a milestone already holding its work,
		// never towards the skip that would settle an unbuilt version as delivered.
		slog.WarnContext(ctx, "build: list the rebuilt milestone's issues failed — nothing reopened, re-planning",
			"project", projectID, "milestone", milestoneNumber, "error", err)
		return false
	}
	reopened := 0
	for _, issue := range issues {
		if delivery.HasLabel(issue.Labels, delivery.KindDevelopment) {
			filled = true
		}
		if !delivery.HasLabel(issue.Labels, delivery.LabelCancelled) {
			continue
		}
		// An issue the cancel marked but never managed to close needs no reopen —
		// cancel.go tolerates that state deliberately — but it does need the mark
		// cleared, or the NEXT cancel's marked set is the union of two attempts.
		if issue.State == "closed" {
			if rerr := p.issues.Reopen(ctx, orgID, projectID, issue.Number); rerr != nil {
				slog.WarnContext(ctx, "build: reopening a cancelled issue failed",
					"project", projectID, "issue", issue.Number, "error", rerr)
				continue
			}
			reopened++
		}
		if uerr := p.issues.Unlabel(ctx, orgID, projectID, issue.Number, delivery.LabelCancelled); uerr != nil {
			slog.WarnContext(ctx, "build: clearing the cancel mark on a reopened issue failed",
				"project", projectID, "issue", issue.Number, "error", uerr)
		}
	}
	slog.InfoContext(ctx, "build: rebuilding an unchanged version — reopened what the cancel closed",
		"project", projectID, "milestone", milestoneNumber, "issuesReopened", reopened, "milestoneFilled", filled)
	return filled
}

// previousDevMilestone picks the newest DEV run's milestone whose title is not
// the one being claimed. rows arrive newest-first from the repository.
//
// Dev runs only, because only a dev run delivers a version: task and validation
// runs work an EXISTING milestone that may be any version's, so superseding on
// the newest row outright would close a milestone the project is still on.
//
// Comparing on TITLE here is not a GitHub title match: it compares the
// platform-recorded milestone title against the title being claimed, both
// platform-side values. A claim that resolves to a milestone this project
// already has matches and is skipped — a milestone must never supersede itself.
func previousDevMilestone(rows []delivery.MilestoneRun, milestoneTitle string) (delivery.MilestoneRun, bool) {
	for i := range rows {
		if rows[i].Kind != delivery.RunKindDev {
			continue // task and validation runs work their own (older) milestones
		}
		if rows[i].MilestoneTitle == milestoneTitle {
			continue
		}
		return rows[i], true
	}
	return delivery.MilestoneRun{}, false
}

// gatesLast orders a superseded milestone's issues so the work is dealt with —
// closed, or carried forward — before the gates that were holding it, matching
// §6's sequence. It matters only for what an observer sees in the issue timeline
// — the end state is the same — but a gate closing before the work it gated reads
// as a resolution rather than an abandonment.
func gatesLast(issues []sourcecontrol.IssueInfo) []sourcecontrol.IssueInfo {
	out := make([]sourcecontrol.IssueInfo, 0, len(issues))
	for _, i := range issues {
		if !delivery.IsDispatchGate(i.Labels) {
			out = append(out, i)
		}
	}
	for _, i := range issues {
		if delivery.IsDispatchGate(i.Labels) {
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
// `rebuild` rides the same request for the same reason, and narrows the planning
// phase to its gates: only the click knows the milestone was already filled and
// that it has just reopened it.
//
// A supervisor that cannot start is the one failure this path still settles
// itself. Everything the run does after this point is the workflow's to fail,
// with Temporal's retries behind it.
func (s *Service) startRun(ctx context.Context, orgID, projectID, tag string,
	run *delivery.MilestoneRun, inputs []delivery.ProvisionInput, rebuild bool) error {
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
		Kind:            delivery.RunKindDev,
		Origin:          delivery.RunOriginSpecBuild,
		RunID:           run.ID,
		Tag:             tag,
		ProvisionInputs: inputs,
		Rebuild:         rebuild,
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
// project's build mutex would stay held: no later build could be admitted, and
// the reconcile sweep counts `planning` as live so nothing would heal it. A
// cancelled client must not be able to wedge a project.
func (s *Service) failRun(ctx context.Context, run *delivery.MilestoneRun, cause error) {
	slog.ErrorContext(ctx, "build: milestone plan path failed — settling the run",
		"project", run.ProjectID, "run", run.ID, "milestone", run.MilestoneNumber, "error", cause)
	settleCtx := context.WithoutCancel(ctx)
	if _, err := s.plan.runs.Settle(settleCtx, run.ID, delivery.RunStateFailed, delivery.RunReasonPlanFailed); err != nil {
		slog.ErrorContext(ctx, "build: settling the failed run ALSO failed — the project's build mutex is held",
			"project", run.ProjectID, "run", run.ID, "error", err)
	}
}
