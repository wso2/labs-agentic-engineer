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

// TaskRunWorkflow works a DEFECT inside a version somebody already delivered: a
// bug or a merge conflict adopted into an already-deployed milestone.
//
// It is the SAME cycle loop the dev run is, over a NARROWER working set and with
// no planning bookend. Everything that makes the loop safe — the four budgets,
// the no-progress rule, the gate park, cancel re-derived from the row — is
// therefore the same code and not a second copy of it.
//
// Its working set is bugs and conflicts ONLY. Planned work is dev-workflow's
// alone: a dev run owns the version and holds the project's build mutex, so
// planned issues left open by a build that gave up must wait for another build
// rather than be continued by a run that never planned them and carries
// different budgets.
//
// Its milestone is somebody else's, which is what makes an empty working set
// meaningless to it: adoption fires on a label write and GitHub's issue index
// lags a write, so a first poll can legitimately precede the issue the run was
// started for. It parks instead of settling (see onEmptyWorkingSet).
func TaskRunWorkflow(ctx workflow.Context, in RunInput) (RunResult, error) {
	l := newLoop(ctx, in)
	if err := workflow.SetQueryHandler(ctx, delivery.QueryRunStatus, func() (delivery.RunStatus, error) {
		return l.st, nil
	}); err != nil {
		return RunResult{}, err
	}
	return l.work(ctx, bookends{
		work:    taskWorkingSet,
		onEmpty: l.reopenValidationTask,
	})
}

// reopenValidationTask is the task run's onEmpty bookend: the defects are fixed
// and their components are serving, so if any of them came from a VERDICT, ask
// the version's oracle again.
//
// This is the edge that CLOSES THE REPAIR CHAIN, and without it the chain is a
// dead end: a failed verdict files one bug per failed criterion and closes the
// task, so the bugs get fixed and deployed while the version's verdict stands at
// the failure until a human clicks revalidate. The reopen is what turns the fix
// into the next question — the reconcile sweep starts a validation run off the
// reopened task, and the SAME oracle judges the repair. The chain is bounded by
// the version's attempt allowance and by the identical-digest rule, both of which
// live in the validation workflow.
//
// It reopens ONLY for `src/validation` work, and that condition is the single
// place in the platform where a source label routes anything — everywhere else a
// `src/*` is provenance. An SRE's incident fix or a user's bug fix deploys and the
// standing verdict holds: an incident is not priced like a release, and
// re-judging the whole system for one defect would spend a validation agent per
// bug fix. That is also the accepted cost stated in the design — a verdict is a
// statement about a VERSION, not a commit, so `v3 passed` can describe code that
// shipped after the verdict was recorded.
//
// The attribution is the LATCHED flag and deliberately not a settle-time read of
// the milestone (see loop.workedValidationRepair): which issues this run closed
// is not knowable from the milestone, and "does a closed src/validation issue
// exist" would be true forever after the first repair — reopening the task after
// every subsequent run, which validation then closes, without end.
//
// The reopen goes through EnsureValidationIssue because that is already the
// version's adopt-or-reopen-or-mint: it finds the task in ANY state and reopens a
// closed one rather than filing a second issue carrying a second snapshot of the
// oracle. A project whose oracle has since disappeared answers 0 and nothing is
// reopened, which is the honest reading of "there is nothing to ask".
func (l *loop) reopenValidationTask(ctx workflow.Context) (RunResult, error) {
	if !l.workedValidationRepair {
		return l.settle(ctx, delivery.RunStateSucceeded, "")
	}
	issue, err := l.ensureValidationIssue(ctx)
	if err != nil {
		return l.result(), err
	}
	if issue > 0 {
		// Live status only. The task run reaches no verdict, so its row's
		// validation columns stay empty — the validation run that works the
		// reopened task records the issue alongside the answer it produced, which
		// is the only pairing a reader can act on.
		l.st.ValidationIssue = issue
		workflow.GetLogger(ctx).Info("verdict-sourced repair delivered; reopened the version's validation task",
			"milestone", l.in.MilestoneNumber, "validationIssue", issue)
	}
	return l.settle(ctx, delivery.RunStateSucceeded, "")
}
