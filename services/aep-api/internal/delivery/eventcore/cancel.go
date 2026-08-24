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

// CLOSING the work a cancelled run was in the middle of.
//
// This is the sibling of halt.go and exists for the same mechanical reason.
// Closing the issues is what makes a cancel STICK: the reconcile sweep starts a
// run over a milestone's open WORK when no run is live on it, so an issue
// left open by a cancel is indistinguishable from work nobody has started, and
// the run the user just abandoned is restarted within a tick. Suppression, not
// bookkeeping.
//
// Where it DIFFERS from the halt is the population and the way back, and both
// follow from what the two endings mean. A halt says "this run gave up, and
// another attempt at the same version may be worth making" — so the work stays
// open, the milestone stays open, and the gates are untouched because a retry
// still needs its dependencies resolved. A cancel says "this increment is
// abandoned" — so a dev run's cancel closes everything the increment was
// carrying, its gates included, and closes the milestone behind it
// (delivery.InCancelledWork and CancelClosesTheMilestone hold the per-kind
// rules). It stops at two populations even so: the version's validation task,
// which judges software still deployed, and the LEDGER, which is nobody's work
// and never the platform's to write to.
//
// Nothing is reverted. Commits a cycle already merged stay on `main` and
// components it already promoted keep serving, which is why closing the milestone
// is a statement about the INCREMENT and not about what is deployed.
//
// `aep:cancelled` is the rebuild's handle on what was in flight. Only issues that
// were OPEN at cancel time are touched and stamped, so work a cycle genuinely
// finished stays closed and unmarked and a rebuild of the same spec cannot
// resurrect it — which is the whole reason the marker exists rather than a
// rebuild simply reopening the milestone's issues wholesale.

// CloseCancelledWork comments on, stamps `aep:cancelled` on, and closes every
// OPEN issue a cancel of a run of this KIND abandons, and returns the issues it
// closed.
//
// EXPORTED for the same reason HaltUnfinishedWork is: the trigger is the
// supervisor's — only the run knows it is stopping and why — while the write
// belongs here, so the platform keeps one place that decides what a label means
// and one prose voice on an issue. The supervisor reaches it through the
// `run.WorkCanceller` port and writes no issue of its own.
//
// A `validation` run closes nothing here, and that is a decision rather than a
// gap. Its consequence IS the version's validation task, and that close already
// happens on every ending, scoped to the task the run ADOPTED — so reaching the
// milestone from here would close a task that a run cancelled before its first
// read never adopted, which is precisely the case the design leaves open for the
// next trigger.
//
// THE LABEL GOES ON BEFORE THE CLOSE, and the order is load-bearing. An issue
// that ended up closed with no marker is invisible to the rebuild and its work is
// silently lost; an issue that ended up marked but still open costs nothing — the
// sweep skips its milestone, and the rebuild reopens it (a no-op) and clears the
// mark either way.
//
// Per-issue best-effort, like the halt: one failed write leaves one issue behind,
// where failing the whole call would leave EVERY issue open and restart all of
// them. The errors are joined and returned so the supervisor's activity retries,
// and a retry is safe — the label add is a merge, a second comment is noise
// rather than a state change, and a close is idempotent.
func (e *Events) CloseCancelledWork(ctx context.Context, orgID, projectID string, milestoneNumber int,
	runKind string) ([]int, error) {
	if e.p.Issues == nil {
		return nil, nil
	}
	// The milestone's OPEN issues, UNFILTERED — the same fetch shape the halt, the
	// merge policy and the reconcile sweep use, and for the same reason: a dev
	// run's cancel spans several populations (its working set AND the gates, which
	// carry no arming label at all), and no label filter can express a union of
	// those and then exclude two more. The STATE is the only narrowing the host
	// does; delivery.InCancelledWork is the rest.
	issues, err := e.p.Issues.ListMilestoneIssues(ctx, orgID, projectID, milestoneOpenIssuesFilter(milestoneNumber))
	if err != nil {
		return nil, err
	}
	comment := cancelComment(runKind)
	var (
		closed []int
		errs   []error
	)
	for _, issue := range issues {
		if !delivery.InCancelledWork(runKind, issue.Labels) {
			continue
		}
		if lerr := e.p.Writer.Label(ctx, orgID, projectID, issue.Number, delivery.LabelCancelled); lerr != nil {
			errs = append(errs, fmt.Errorf("cancel: label issue #%d: %w", issue.Number, lerr))
			continue
		}
		// The comment rides the close (IssueWriter.Close posts it first), so an
		// issue never ends up closed with nothing beside it explaining why.
		if cerr := e.p.Writer.Close(ctx, orgID, projectID, issue.Number, comment); cerr != nil {
			errs = append(errs, fmt.Errorf("cancel: close issue #%d: %w", issue.Number, cerr))
			continue
		}
		closed = append(closed, issue.Number)
	}
	if len(closed) > 0 {
		slog.InfoContext(ctx, "eventcore: closed the work a cancelled run had in flight",
			"project", projectID, "milestone", milestoneNumber, "runKind", runKind, "issues", closed)
	}
	return closed, errors.Join(errs...)
}

// cancelComment is what the platform says on an issue a cancel closed. Prose,
// like every other body this package writes, and it says the two things a reader
// needs: nothing is working this any more, and what brings it back.
//
// The way back differs by kind, which is why the text does. A cancelled BUILD
// abandons the increment, so the way forward is the spec and another build — and
// a build of an UNCHANGED spec is what reopens exactly these issues, which is
// worth saying on the issue because it is the non-obvious half. A cancelled
// bug-fix run abandons nothing but itself: the version stands, and reopening the
// bug hands it straight back.
func cancelComment(runKind string) string {
	if runKind == delivery.RunKindDev {
		return "The build working this issue was cancelled, so this increment is abandoned and nothing is " +
			"working this now. Building again from the same spec REOPENS this issue exactly as it was; " +
			"editing the spec first cuts a new version and plans it fresh. Nothing was reverted — code " +
			"that already merged is still on `main`, and anything already deployed is still serving."
	}
	return "The run working this issue was cancelled, so nothing is working it now. The deployed version " +
		"is unchanged and still stands; reopen this issue to hand it back to the platform, or file new work."
}
