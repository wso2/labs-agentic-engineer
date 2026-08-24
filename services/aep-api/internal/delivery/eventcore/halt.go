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

// HALTING the work a failed run gave up on.
//
// A run that exhausts a budget settles `failed` and leaves its working set OPEN,
// because the milestone stays open too — the way forward from a failed increment
// is more work in the same version. But the reconcile sweep's trigger is exactly
// "open work of a species on a milestone with no live run", so those leftovers
// are indistinguishable from work nobody has started, and the sweep starts a
// fresh run on them within a tick. With a fresh budget. Which it exhausts. And
// so on: every budget in the platform is defeated at once, and the symptom is an
// unexplained cloud bill rather than a failing test.
//
// `aep:halted` is the difference between "not started" and "given up on". The
// sweep skips a halted issue (see offerRun), and the mark is cleared by a
// rebuild or by a person removing the label — both of which are somebody
// deciding the work is worth trying again, which is exactly the decision the
// sweep must not make on its own.

// HaltUnfinishedWork stamps `aep:halted` and a comment naming the terminal
// reason on every open issue in the run's WORKING SET, and returns the issues it
// marked.
//
// EXPORTED, like MintDeployFixIssues and for the same reason: the trigger is the
// supervisor's — only the run knows it is giving up and why — while the write
// belongs here, so the platform keeps one place that decides what a label means
// and one prose voice on an issue. The supervisor reaches it through the
// `run.WorkHalter` port and writes no issue of its own.
//
// The working set is selected by the RUN's kind (delivery.InWorkingSet), and the
// narrowing is load-bearing in both directions. A dev run must not halt a bug a
// concurrent task run is working on the deployed version. A task run must not
// halt the planned work it was never allowed to touch — that work belongs to a
// build, and a build is what will resume it.
//
// A `validation` run's working set is empty by definition, so this halts nothing
// for it: its own work is the version's validation task, which it closes on every
// ending, and the repair issues a failed verdict files are deliberately an
// ordinary task run's work — as is the conflict issue a validation pull request
// that will not rebase produces. Halting those would break the repair chain.
//
// Per-issue best-effort: one label or comment that fails leaves one issue
// unmarked, which costs a single restarted run, where failing the whole call
// would leave EVERY issue unmarked and restart all of them. The errors are joined
// and returned so the supervisor's activity retries, and a retry is safe — the
// label add is a merge, and a second comment is noise rather than a state change.
func (e *Events) HaltUnfinishedWork(ctx context.Context, orgID, projectID string, milestoneNumber int,
	runKind, reason string) ([]int, error) {
	if e.p.Issues == nil {
		return nil, nil
	}
	// The milestone's OPEN issues, UNFILTERED — the same fetch shape the merge
	// policy and the reconcile sweep use, and for the same reason: "carries `aep`
	// AND is of kind X" is an intersection the host's union-valued GraphQL
	// `labels:` argument cannot express, and a REST label filter here would
	// narrow the fetch by a rule that belongs in the decision.
	issues, err := e.p.Issues.ListMilestoneIssues(ctx, orgID, projectID, milestoneOpenIssuesFilter(milestoneNumber))
	if err != nil {
		return nil, err
	}
	comment := haltComment(reason)
	var (
		halted []int
		errs   []error
	)
	for _, issue := range issues {
		if !delivery.InWorkingSet(runKind, issue.Labels) {
			continue
		}
		if delivery.HasLabel(issue.Labels, delivery.LabelHalted) {
			// Already halted by an earlier run that gave up on the same issue. Say
			// nothing a second time: the comment names a terminal reason, and two
			// of them on one issue read as two abandonments.
			continue
		}
		// The comment FIRST. It is the only explanation a human gets, and an issue
		// that ended up labelled with no reason beside it is a dead end.
		if cerr := e.p.Writer.Comment(ctx, orgID, projectID, issue.Number, comment); cerr != nil {
			errs = append(errs, fmt.Errorf("halt: comment issue #%d: %w", issue.Number, cerr))
			continue
		}
		if lerr := e.p.Writer.Label(ctx, orgID, projectID, issue.Number, delivery.LabelHalted); lerr != nil {
			errs = append(errs, fmt.Errorf("halt: label issue #%d: %w", issue.Number, lerr))
			continue
		}
		halted = append(halted, issue.Number)
	}
	if len(halted) > 0 {
		slog.InfoContext(ctx, "eventcore: halted the work a failed run could not finish",
			"project", projectID, "milestone", milestoneNumber, "runKind", runKind,
			"reason", reason, "issues", halted)
	}
	return halted, errors.Join(errs...)
}

// haltComment is what the platform says on an issue it is giving up on. Prose,
// like every other body this package writes, and it says the two things a reader
// needs: nothing is working this any more, and how to make something work it.
//
// An empty reason is its own sentence rather than a blank. A failed run without a
// terminal reason should not happen — every failure names a class — so a reader
// seeing this has found a loop bug and the wording should not hide it.
func haltComment(reason string) string {
	if reason == "" {
		return "The run working this issue failed without naming a reason, and stopped. " +
			"Nothing is working it now: the platform will not restart it on its own, because a run " +
			"that already gave up would be restarted with fresh budgets and give up again. " +
			"Remove the `aep:halted` label to hand it back to the platform, or build again."
	}
	return fmt.Sprintf("The run working this issue stopped: `%s`. "+
		"Nothing is working it now: the platform will not restart it on its own, because a run "+
		"that already gave up would be restarted with fresh budgets and give up again. "+
		"Remove the `aep:halted` label to hand it back to the platform, or build again.", reason)
}
