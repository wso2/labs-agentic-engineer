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

// defaultSweepInterval is the reconcile cadence. A backstop, not a driver:
// everything it heals is something a webhook should have done, so it is slow
// on purpose.
const defaultSweepInterval = 60 * time.Second

// Sweep is the reconcile backstop, and it has TWO trigger conditions:
//
//	a milestone with open work and no live run gets one, and
//	a live run row past its planning phase is re-offered to the supervisor.
//
// The first heals both failure modes the event plane can have. A delivery
// GitHub never made (or that failed past its retries) leaves a milestone with
// work and nobody working it. And the adoption-versus-settle race — an issue
// joining a milestone in the instant the supervisor decided it was empty —
// leaves exactly the same footprint.
//
// The second heals a failure mode the row model has: a live ROW is not a live
// WORKFLOW. Nothing else notices a row whose execution is gone, and because a
// non-terminal row answers LiveRunForMilestone forever, the first rule would
// skip it forever while the partial indexes refuse every later run on that
// project. Re-offering is idempotent — a running execution answers
// AlreadyStarted and the row is reused, not re-admitted — so the healthy case
// costs one Temporal call and changes nothing.
//
// The rule is safe because supersede makes it so: the plan path closes the
// previous version's open issues before minting the next milestone, so an
// abandoned milestone holds no open work and this sweep can never resurrect
// one. Cancel is likewise final — the increment is abandoned, and the only way
// forward is the next build.
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
		counts, cerr := e.p.Issues.MilestoneIssueCounts(ctx, repo.OrgID, repo.ProjectID, milestone.Number)
		if cerr != nil {
			errs = append(errs, cerr)
			continue
		}
		if !hasOpenWork(counts) {
			continue
		}
		slog.InfoContext(ctx, "eventcore: reconcile sweep found unworked open issues — starting a run",
			"project", repo.ProjectID, "milestone", milestone.Number, "openIssues", counts.OpenTotal)
		if serr := e.startRun(ctx, repo.OrgID, repo.ProjectID, milestone); serr != nil {
			errs = append(errs, serr)
		}
	}
	return errors.Join(errs...)
}

// hasOpenWork is the sweep's trigger condition, and it is deliberately WEAKER
// than the dispatch predicate: an open gate must not stop a run from being
// started, only from dispatching. A run whose milestone has a gate open starts
// and waits — which is precisely the state a missed delivery should be healed
// into.
func hasOpenWork(counts *sourcecontrol.MilestoneIssueCounts) bool {
	return counts != nil && counts.OpenTotal > 0
}
