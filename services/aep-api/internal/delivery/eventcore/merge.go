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
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// milestoneOpenIssuesFilter reads a milestone's OPEN issues — the whole
// population the merge predicate is decided against. Closed issues are excluded
// by the state filter and pull requests by the host; NO label filter is applied,
// because which labels count as this run's work is decideAutoMerge's decision
// alone, made against the labels it can see.
//
// Narrowing here by label is what broke validation auto-merge: the fetch asked
// for `aep` only, so the milestone's validation task — which then carried no
// arming label at all — was gone before the policy could accept it, and every
// validation pull request was declined with "no resolved issue is this run's
// work". Two copies of one predicate, and the hidden copy won.
//
// Adding the validation label to this filter would not fix it either: REST
// `?labels=a,b` is AND (see githubhost.ListMilestoneIssues), so asking for both
// demands an issue carrying both and matches nothing. The fetch stays wide and
// the policy stays the only place labels are read.
func milestoneOpenIssuesFilter(milestoneNumber int) sourcecontrol.MilestoneIssuesFilter {
	return sourcecontrol.MilestoneIssuesFilter{
		Number: milestoneNumber,
		State:  "open",
	}
}

// merge squash-merges the cycle's pull request through the org's credential
// (in App mode that identity IS <slug>[bot] — there is no second credential).
//
// It reads the live pull request FIRST. That read is what makes a redelivered
// webhook harmless: GitHub redelivers any delivery whose handler failed, and
// the second pass must find the pull request already merged and do nothing
// rather than issue a second merge call.
func (e *Events) merge(ctx context.Context, orgID, projectID string, run *delivery.MilestoneRun,
	prNumber int, branch string, decision mergeDecision) error {
	if e.p.Merger == nil {
		return nil
	}
	settled, err := e.prSettled(ctx, orgID, projectID, prNumber)
	if err != nil {
		return err // transient read failure — let GitHub redeliver
	}
	if settled {
		return nil
	}
	if merr := e.p.Merger.MergePullRequest(ctx, orgID, projectID, prNumber); merr != nil {
		return e.onMergeRefused(ctx, orgID, projectID, run, prNumber, branch, merr)
	}
	slog.InfoContext(ctx, "eventcore: squash-merged the cycle's pull request",
		"pr", prNumber, "milestone", run.MilestoneNumber, "resolves", decision.Matched)
	return nil
}

// prSettled reports whether the pull request is already merged or closed. A
// nil PR reader means the check cannot be made, which is treated as "not
// settled" — the merge call itself is idempotent on an already-merged PR, so
// the read is a cost saver and a conflict discriminator, not the only guard.
func (e *Events) prSettled(ctx context.Context, orgID, projectID string, prNumber int) (bool, error) {
	if e.p.PRs == nil {
		return false, nil
	}
	st, err := e.p.PRs.GetPullRequestState(ctx, orgID, projectID, prNumber)
	if err != nil {
		return false, err
	}
	if st == nil {
		return false, nil
	}
	return st.Merged || strings.EqualFold(st.State, "closed"), nil
}

// onMergeRefused classifies an answered merge refusal and, when it is a
// conflict, mints the conflict issue that puts the rebase back into the
// working set.
//
// The classification is deliberately coarse, and can be because of what this
// model does NOT have: no required checks, no commit statuses, no review
// gates. The agent's in-pod build gate is the only quality gate, so the host
// has exactly one reason left to refuse a squash on a pull request that is
// still open — it does not merge cleanly.
//
// The live re-read before classifying covers the two ways an error can lie: a
// merge that actually landed and then failed to respond, and a pull request a
// human closed underneath us.
func (e *Events) onMergeRefused(ctx context.Context, orgID, projectID string, run *delivery.MilestoneRun,
	prNumber int, branch string, mergeErr error) error {
	settled, err := e.prSettled(ctx, orgID, projectID, prNumber)
	if err != nil {
		return err
	}
	if settled {
		return nil // it landed (or was closed) after all
	}
	slog.WarnContext(ctx, "eventcore: merge refused on an open pull request — treating as a conflict",
		"pr", prNumber, "milestone", run.MilestoneNumber, "error", mergeErr)
	e.noteCycleMergeRefused(ctx, run, mergeErr.Error())
	issueNumber, err := e.mintConflictIssue(ctx, orgID, projectID, run, prNumber, branch, mergeErr)
	if err != nil {
		return err
	}
	e.signal(ctx, run, delivery.SigRunConflict, delivery.RunSignal{
		PRNumber:    prNumber,
		Branch:      branch,
		IssueNumber: issueNumber,
		Message:     mergeErr.Error(),
	})
	return nil
}
