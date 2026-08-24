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
	"log/slog"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// Why the sweep reads ISSUES where the cycle-boundary poll reads COUNTS.
//
// It makes two decisions a count cannot express, and both are intersections the
// host's union-valued GraphQL `labels:` argument cannot ask for. It must ROUTE by
// kind ("carries `aep` AND is of kind `validation`"), and it must SKIP the work a
// failed run gave up on ("carries `aep` AND `aep:halted`"). Both are therefore
// decided in Go over an UNFILTERED fetch, exactly as the auto-merge policy does
// and for the same stated reason: the fetch stays wide and the policy is the only
// place labels are read. Neither costs a round trip, and neither reintroduces a
// negative label query.
//
// It costs one REST call per known milestone per pass, replacing one GraphQL
// call. The boundary poll keeps its counts — that read runs at every cycle
// boundary and is the loop's hottest, and `aep:halted` deliberately does not
// reach it: a halted issue inside a LIVE run's milestone is a contradiction (the
// run that halted them is terminal by construction), so the hot poll is not
// complicated for a state it cannot see.

// defaultSweepInterval is the reconcile cadence. A backstop, not a driver:
// everything it heals is something a webhook should have done, so it is slow
// on purpose.
const defaultSweepInterval = 60 * time.Second

// Sweep is the reconcile backstop AND the trigger router, and it has TWO
// trigger conditions:
//
//	a milestone with open WORK, no live run and no cancelled increment gets a
//	run OF THE RIGHT KIND, and
//	a live run row past its planning phase is re-offered to the supervisor.
//
// Open WORK, never "an open issue": what starts a run is the trigger predicate of
// a species (offerRun), so a milestone holding only a ledger note, only a gate or
// only planned work starts nothing at all.
//
// The first heals both failure modes the event plane can have. A delivery
// GitHub never made (or that failed past its retries) leaves a milestone with
// work and nobody working it. And the adoption-versus-settle race — an issue
// joining a milestone in the instant the supervisor decided it was empty —
// leaves exactly the same footprint. It is also the ONLY thing that starts a
// validation run without a human asking: a dev run settles having filed the
// version's validation task, and this is what picks that task up.
//
// The second heals a failure mode the row model has: a live ROW is not a live
// WORKFLOW. Nothing else notices a row whose execution is gone, and because a
// non-terminal row answers LiveRunForMilestone forever, the first rule would
// skip it forever while the partial indexes refuse every later run on that
// project. Re-offering is idempotent — a running execution answers
// AlreadyStarted and the row is reused, not re-admitted — so the healthy case
// costs one Temporal call and changes nothing.
//
// Three things keep the first rule from resurrecting work nobody wants, and all
// three are somebody else's write:
//
//   - SUPERSEDE empties the previous version's milestone. It closes the planned
//     work and the gates (a plan is replaced by a plan) and MOVES the open bugs
//     into the new version's milestone (a defect is not superseded — it is still
//     broken, and the new version is what ships the fix). Either way the
//     superseded milestone holds no workable issue, so the trigger never fires on
//     it. A move that failed leaves one bug behind and a task run picks it up —
//     which is the right outcome for a defect, and the same best-effort posture a
//     failed close has always had.
//   - CANCEL is final, and it is enforced TWICE over. The cancelled run closes
//     every issue it had in flight (cancel.go), and this rule skips the milestone
//     outright while its newest run reads `cancelled` — because a closed milestone
//     still accepts issues, so a reopened or newly filed one inside an abandoned
//     increment would otherwise start a run that builds and deploys a version
//     nobody is shipping. The skip clears itself: a rebuild admits a new row on
//     the same milestone, so the newest run is no longer the cancelled one.
//   - HALT marks what a FAILED run could not finish (`aep:halted`, see halt.go).
//     Without it this rule is a budget defeater: a run settles `failed` with its
//     working set still open, so the sweep starts a fresh run on the same issues
//     with fresh budgets, forever.
//
// It walks the milestones the PLATFORM knows (from its own run rows), not
// GitHub's milestone list: a milestone the platform never ran is not a missed
// delivery, it is somebody else's milestone. That is also what keeps the sweep
// inert on a project the platform has never run.
type Sweep struct {
	events   *Events
	repos    RepoLister
	interval time.Duration
}

// NewSweep wires the sweep. interval ≤ 0 uses the default.
func NewSweep(events *Events, repos RepoLister, interval time.Duration) *Sweep {
	if interval <= 0 {
		interval = defaultSweepInterval
	}
	return &Sweep{events: events, repos: repos, interval: interval}
}

// Run ticks until ctx is cancelled (the app.Watcher shape).
func (s *Sweep) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Once(ctx); err != nil {
				slog.WarnContext(ctx, "eventcore: reconcile sweep failed", "error", err)
			}
		}
	}
}

// Once runs a single reconcile pass. Exported so the pass can be driven
// directly — by a test, and by anything that wants to reconcile now.
//
// One repository's failure never stops the others: the sweep's whole purpose
// is to be the thing that still runs when something else is broken.
func (s *Sweep) Once(ctx context.Context) error {
	if s.repos == nil || s.events == nil {
		return nil
	}
	repos, err := s.repos.ListAll(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, repo := range repos {
		if rerr := s.reconcileRepo(ctx, repo); rerr != nil {
			errs = append(errs, rerr)
		}
	}
	return errors.Join(errs...)
}

func (s *Sweep) reconcileRepo(ctx context.Context, repo RepoRef) error {
	e := s.events
	if e.p.Runs == nil || e.p.Issues == nil {
		return nil
	}
	milestones, err := e.p.Runs.KnownMilestones(ctx, repo.OrgID, repo.ProjectID)
	if err != nil {
		return err
	}
	var errs []error
	for _, milestone := range milestones {
		live, lerr := e.p.Runs.LiveRunForMilestone(ctx, repo.OrgID, repo.ProjectID, milestone.Number)
		if lerr != nil {
			errs = append(errs, lerr)
			continue
		}
		if live != nil {
			// A live ROW is not a live WORKFLOW. Re-offer it: StartRun is
			// idempotent — an execution that is running answers AlreadyStarted,
			// and the row is reused rather than re-admitted — so this costs one
			// Temporal call and heals a row whose workflow is gone. Without it a
			// non-terminal row answers LiveRunForMilestone forever, the sweep
			// skips it forever, and the partial indexes refuse every later run on
			// that project (the wedge migrate/milestone_runs.go:75-85 documents).
			//
			// EXCEPT a run still in its planning phase. Re-offering that one would
			// start a fresh workflow with no Tag and no provision inputs — the
			// caller's, not the row's — so it would skip planning entirely and
			// settle an unplanned version as delivered. A planning row is the
			// click's to resolve: it starts the workflow synchronously and settles
			// the row when it cannot.
			if live.State != delivery.RunStatePlanning {
				if serr := e.startRun(ctx, repo.OrgID, repo.ProjectID, milestone); serr != nil {
					errs = append(errs, serr)
				}
			}
			continue
		}
		// A CANCELLED increment is abandoned, and the milestone is skipped whole —
		// before the issue fetch, because there is no decision left for the issues
		// to inform and this saves the round trip.
		//
		// It has to be a decision about the MILESTONE rather than about the issues,
		// which is the difference from the halt: a closed milestone still accepts
		// new ones, so an issue reopened (or freshly filed) inside a cancelled
		// version carries no mark and would otherwise start a task run that builds
		// and deploys against a version nobody is shipping.
		//
		// The rule clears itself: a rebuild admits a new row on the same milestone,
		// so the newest run stops being the cancelled one the moment somebody
		// decides to work the version again. That is why it reads the NEWEST run of
		// any kind rather than looking for a cancel anywhere in the history.
		cancelled, cerr := e.milestoneCancelled(ctx, repo.OrgID, repo.ProjectID, milestone.Number)
		if cerr != nil {
			errs = append(errs, cerr)
			continue
		}
		if cancelled {
			continue
		}
		issues, ierr := e.p.Issues.ListMilestoneIssues(ctx, repo.OrgID, repo.ProjectID,
			milestoneOpenIssuesFilter(milestone.Number))
		if ierr != nil {
			errs = append(errs, ierr)
			continue
		}
		if serr := e.offerRun(ctx, repo.OrgID, repo.ProjectID, milestone, issues); serr != nil {
			errs = append(errs, serr)
		}
	}
	return errors.Join(errs...)
}

// milestoneCancelled reports whether this milestone's NEWEST run settled
// `cancelled` — that is, whether its increment stands abandoned.
//
// Newest-of-any-kind, and nothing older, is the whole of the rule. A cancel
// somewhere in a milestone's history says nothing about now: the version may have
// been rebuilt and delivered since, and a run that is live is caught by the
// caller's own live-run branch before this is ever asked.
//
// A milestone with no rows at all answers false. The sweep only walks milestones
// the platform has run, so that is a concurrent purge rather than a state, and
// "not cancelled" is the answer that heals rather than the one that hides work.
func (e *Events) milestoneCancelled(ctx context.Context, orgID, projectID string, milestoneNumber int) (bool, error) {
	newest, err := e.p.Runs.NewestRunForMilestone(ctx, orgID, projectID, milestoneNumber)
	if err != nil {
		return false, err
	}
	if newest == nil {
		return false, nil
	}
	if newest.State != delivery.RunStateCancelled {
		return false, nil
	}
	// Only a cancelled run that ABANDONS THE INCREMENT suppresses the milestone,
	// which is the dev species alone — the same predicate that decides whether the
	// cancel closes the milestone at all.
	//
	// A cancelled task or validation run leaves the version exactly as deployed as
	// it was and its milestone open, so the work still in it is still somebody's.
	// Treating those as an abandoned increment is what stranded a version: cancel a
	// bug-fix run and the open validation task beside it became invisible to the
	// sweep, so nothing ever judged the version and no report was ever produced.
	kind, _ := delivery.RoutableRunKind(newest.Kind, newest.Origin)
	if !delivery.CancelClosesTheMilestone(kind) {
		return false, nil
	}
	slog.DebugContext(ctx, "eventcore: reconcile sweep skipping a cancelled increment",
		"project", projectID, "milestone", milestoneNumber, "run", newest.ID)
	return true, nil
}

// offerRun routes ONE unworked milestone by the TRIGGER PREDICATES themselves: a
// validation run when the version's validation task is open, a task run when the
// milestone holds task working-set work, and NOTHING otherwise.
//
// "Otherwise" is a real population and it is why the routing is written on the
// predicates rather than on "something is open". A milestone can sit with only a
// human's ledger note in it, or only an open `provision` gate, or only planned
// work a build gave up on — and each of those started a paid agent run that then
// had nothing in its working set and parked, on a milestone nobody was building,
// every 60 seconds forever. What each of those states actually needs:
//
//	a ledger note   nothing. Unarmed is the whole meaning of ledger: the platform
//	                does not work it until a human arms it, and arming raises the
//	                adoption path directly.
//	a gate alone    nothing. A gate holds the next DISPATCH; with no work behind
//	                it there is nothing to hold, and a run started to wait on it
//	                would wait on an empty milestone.
//	`development`   nothing this pass can offer. Planned work is dev-workflow's
//	                alone (InTaskWorkingSet excludes it deliberately), and only
//	                the build click may start a dev run — it carries the version
//	                mutex and the tag. Left open by a build that gave up, planned
//	                work is normally `aep:halted` anyway, and the way forward is
//	                another build.
//
// Validation wins when both populations are open, and that ordering costs nothing
// in practice: a dev run files the validation task only at deployed-green, with
// the working set already empty, and a failed attempt's repair issues are filed
// after the task has been closed. The two coexist only when a human files work
// into a version awaiting its verdict, and judging first is the safe order there
// — the verdict is about what is deployed, which the new work has not changed yet.
//
// HALTED issues are dropped before either decision, and dropped rather than
// merely ignored: a milestone holding nothing but halted work is QUIET, and
// starting a run on it would park a supervisor on a milestone whose work nobody
// intends to finish. A newly filed issue in the same milestone is untouched by
// the filter and starts a run normally — halting marks the issues a run gave up
// on, never the milestone.
func (e *Events) offerRun(ctx context.Context, orgID, projectID string, milestone MilestoneRef,
	issues []sourcecontrol.IssueInfo) error {
	issues = notHalted(issues)
	for _, iss := range issues {
		if delivery.HasLabel(iss.Labels, delivery.LabelAgentWork) && delivery.IsValidationWork(iss.Labels) {
			slog.InfoContext(ctx, "eventcore: reconcile sweep found an open validation task — judging the version",
				"project", projectID, "milestone", milestone.Number, "validationIssue", iss.Number)
			return e.startValidationRun(ctx, orgID, projectID, milestone)
		}
	}
	for _, iss := range issues {
		if delivery.InTaskWorkingSet(iss.Labels) {
			slog.InfoContext(ctx, "eventcore: reconcile sweep found unworked defects — starting a run",
				"project", projectID, "milestone", milestone.Number, "issue", iss.Number)
			return e.startRun(ctx, orgID, projectID, milestone)
		}
	}
	return nil
}

// notHalted drops the issues a failed run marked `aep:halted`.
//
// It is a DECISION over issues the sweep already fetched, never a query filter,
// and that is the whole reason the fetch is unfiltered: "carries `aep` AND
// `aep:halted`" is an intersection the host cannot count, and its complement is a
// negative label query the host cannot express at all. Deciding here costs
// nothing and keeps every label rule in Go.
func notHalted(issues []sourcecontrol.IssueInfo) []sourcecontrol.IssueInfo {
	out := make([]sourcecontrol.IssueInfo, 0, len(issues))
	for _, iss := range issues {
		if delivery.HasLabel(iss.Labels, delivery.LabelHalted) {
			continue
		}
		out = append(out, iss)
	}
	return out
}
