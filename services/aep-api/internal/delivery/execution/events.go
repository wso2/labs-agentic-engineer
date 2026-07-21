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

package execution

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"strconv"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// RegisterFunc is the webhook-router registration seam (the same closure shape
// app.go passes so this package imports nothing from feature/webhook).
type RegisterFunc func(event, action string, h func(ctx context.Context, event, action string, payload []byte) error)

// Events holds the pull_request webhook handlers — the platform-owned half of
// webhook handling (§9.2). PR-opened ends the coding Execution; PR-merged spawns
// a build Execution through the funnel's registered executor; PR-closed-unmerged
// records the coding attempt as rejected. The issues.* handlers are the Task
// feature's job (the §1 split is a package boundary).
//
// Echo suppression is deliberately NOT applied here: it is scoped to issues.*
// (where every platform label/comment/repair write fires an issues.* echo, §9.2)
// because the platform authors no pull requests. In App mode the coding-agent
// runner opens the PR as <slug>[bot] — the SAME login as the platform identity —
// so suppressing self-sender PR deliveries would drop the runner's own
// pull_request.opened and strand the coding Execution in_progress forever (its
// admission mutex held, its retry blocked). The PR is a human/runner gate, not a
// platform projection, so its deliveries are always acted on.
type Events struct {
	store    ExecutionStore
	funnel   *Funnel
	registry *Registry
	prs      PRReader // for the sweep's PR-state reconciliation (may be nil)
	// signaler feeds PR events to a waiting devflow TaskFlow workflow. Nil-safe:
	// a nil signaler (old-console flow, or Temporal disabled) is a no-op, so the
	// webhook handlers behave exactly as before when no workflow is driving.
	signaler *delivery.Signaler
	// notifier wakes any attached task-log SSE stream so the console sees a PR
	// transition (coding done / merged / rejected) instantly instead of on the
	// stream's slow re-derive tick. Nil-safe.
	notifier *delivery.TaskStreamHub
}

// NewEvents wires the pull_request handlers. prs may be nil (then the sweep
// healer no-ops).
func NewEvents(store ExecutionStore, funnel *Funnel, registry *Registry, prs PRReader) *Events {
	return &Events{store: store, funnel: funnel, registry: registry, prs: prs}
}

// WithWorkflowSignaler wires the devflow signaler so PR events reach a waiting
// TaskFlow workflow. Optional — unset leaves the old behavior unchanged.
func (e *Events) WithWorkflowSignaler(s *delivery.Signaler) *Events {
	e.signaler = s
	return e
}

// WithTaskNotifier wires the task-log stream hub so PR transitions wake attached
// console streams instantly. Optional — nil-safe.
func (e *Events) WithTaskNotifier(h *delivery.TaskStreamHub) *Events {
	e.notifier = h
	return e
}

// RegisterHandlers installs the pull_request handlers on the webhook router.
func (e *Events) RegisterHandlers(register RegisterFunc) {
	register("pull_request", "opened", e.PullRequestOpened)
	register("pull_request", "edited", e.PullRequestOpened)
	register("pull_request", "ready_for_review", e.PullRequestOpened)
	register("pull_request", "closed", e.PullRequestClosed)
	register("pull_request", "reopened", e.noop)
}

// closesIssueRefRE matches a "Closes #N" (and fixes/resolves variants) in a PR
// body — the same convention the coding agent writes so the platform can link
// the PR back to its Task. Same-repo only.
var closesIssueRefRE = regexp.MustCompile(`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)\s*:?\s*#(\d+)\b`)

type pullRequestPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		Number         int    `json:"number"`
		Merged         bool   `json:"merged"`
		Draft          bool   `json:"draft"`
		State          string `json:"state"`
		Body           string `json:"body"`
		MergeCommitSHA string `json:"merge_commit_sha"`
		Head           struct {
			Ref string `json:"ref"`
		} `json:"head"`
	} `json:"pull_request"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
}

func parseClosesIssueRef(body string) int {
	m := closesIssueRefRE.FindStringSubmatch(body)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func (e *Events) noop(context.Context, string, string, []byte) error { return nil }

// PullRequestOpened ends the coding Execution when a non-draft PR links a Task
// via "Closes #N" (§7: the coding Execution ends at PR-opened). Fires on
// opened/edited/ready_for_review; Finish is guarded so repeats are no-ops.
func (e *Events) PullRequestOpened(ctx context.Context, _, _ string, payload []byte) error {
	var p pullRequestPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil // malformed delivery — swallow (matches router no-op policy)
	}
	if p.PullRequest.Draft {
		return nil
	}
	issueNumber := parseClosesIssueRef(p.PullRequest.Body)
	if issueNumber == 0 || p.Repository.FullName == "" {
		return nil
	}
	execs, err := e.store.LatestPerKind(ctx, p.Repository.FullName, issueNumber)
	if err != nil {
		return err
	}
	coding := execs[string(taskmeta.KindCoding)]
	if coding == nil || !taskmeta.ExecutionStatus(coding.Status).IsActive() {
		return nil // no active coding attempt to end (already ended, or none)
	}
	// Stamp the PR number so the sweep can GET the live PR later (heals a missed
	// close/merge webhook — §5).
	if _, err := e.store.Finish(ctx, coding.ID, string(taskmeta.ExecSucceeded), reasonPROpenPrefix+strconv.Itoa(p.PullRequest.Number)); err != nil {
		return err
	}
	// The coding attempt is done (PR made) — tell any waiting TaskFlow workflow.
	e.signaler.SignalTask(ctx, p.Repository.FullName, issueNumber, delivery.SigPROpened, delivery.PRSignal{
		Repo:     p.Repository.FullName,
		Issue:    issueNumber,
		PRNumber: p.PullRequest.Number,
		HeadSHA:  p.PullRequest.MergeCommitSHA,
	})
	e.notifier.Notify(p.Repository.FullName, issueNumber)
	return nil
}

// PullRequestClosed spawns a build Execution on merge and records rejection on
// close-unmerged (§4/§7). Merging a PR is the human gate — the build proceeds
// regardless of issue state.
func (e *Events) PullRequestClosed(ctx context.Context, _, _ string, payload []byte) error {
	var p pullRequestPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil
	}
	issueNumber := parseClosesIssueRef(p.PullRequest.Body)
	if issueNumber == 0 || p.Repository.FullName == "" {
		return nil
	}

	if !p.PullRequest.Merged {
		// PR closed without merging → record the coding attempt as rejected by
		// appending a terminal coding row (never mutate a terminal row). The
		// derived status flips to rejected via prState (§4).
		e.signaler.SignalTask(ctx, p.Repository.FullName, issueNumber, delivery.SigPRRejected, delivery.PRSignal{
			Repo:     p.Repository.FullName,
			Issue:    issueNumber,
			PRNumber: p.PullRequest.Number,
		})
		e.notifier.Notify(p.Repository.FullName, issueNumber)
		return e.recordRejection(ctx, p.Repository.FullName, issueNumber)
	}

	// Merged → spawn a build Execution and dispatch it through the registered
	// executor (§7). Builds do not pass the deps gate — a merged PR proceeds.
	// Tell any waiting TaskFlow workflow the PR merged (it then awaits the build).
	e.signaler.SignalTask(ctx, p.Repository.FullName, issueNumber, delivery.SigPRMerged, delivery.PRSignal{
		Repo:     p.Repository.FullName,
		Issue:    issueNumber,
		PRNumber: p.PullRequest.Number,
		MergeSHA: p.PullRequest.MergeCommitSHA,
	})
	e.notifier.Notify(p.Repository.FullName, issueNumber)
	return e.spawnBuild(ctx, p.Repository.FullName, issueNumber, p.PullRequest.MergeCommitSHA)
}

// spawnBuild admits + dispatches a build Execution for a merged Task. Shared by
// the PR-closed(merged) webhook and the sweep's PR-state healer.
func (e *Events) spawnBuild(ctx context.Context, repoFullName string, issueNumber int, mergeSHA string) error {
	// Dedupe on MERGE IDENTITY (the merge commit SHA), NOT task lifetime: skip
	// only when a build row already exists FOR THIS MERGE (active OR terminal) —
	// i.e. a GitHub re-delivery of the same merge webhook. A genuinely new merged
	// PR carries a new SHA, so it admits a fresh build row through TryAdmit as
	// normal even when an older attempt's terminal build exists (§7 retry = a new
	// row; a task with a failed build for an OLD merge stays retryable). SHA is
	// globally unique per merge, so it is the robust dedupe key — PR numbers churn
	// across coding attempts and TryAdmit's mutex only blocks ACTIVE rows.
	if mergeSHA != "" {
		built, err := e.buildExistsForMerge(ctx, repoFullName, issueNumber, mergeSHA)
		if err != nil {
			return err
		}
		if built {
			return nil // re-delivery of this same merge — already built
		}
	}
	facts, ok, err := e.funnel.TaskFactsFor(ctx, repoFullName, issueNumber)
	if err != nil {
		return err
	}
	if !ok {
		slog.WarnContext(ctx, "pr merged for unknown Task", "repo", repoFullName, "issue", issueNumber)
		return nil
	}
	if facts.Class == taskmeta.ClassValidation {
		// A merged validation PR spawns no build — a validation Task is project-
		// scoped (no component to build) and rests at `merged` once its PR merges
		// (validation-phase). Deriving stays at merged with no build Execution.
		return nil
	}
	row := &delivery.Execution{
		OrgID:       facts.OrgID,
		ProjectID:   facts.ProjectID,
		Repo:        facts.Repo,
		IssueNumber: facts.IssueNumber,
		Kind:        string(taskmeta.KindBuild),
		Status:      string(taskmeta.ExecQueued),
		DesignTag:   facts.DesignTag,
		SpecTag:     facts.SpecTag,
		Component:   facts.Component,
		// Pin the build to the merge commit so a git-auth retry can re-trigger the
		// same build after re-minting the clone credential (§7).
		CommitSHA: mergeSHA,
	}
	admitted, adRow, err := e.store.TryAdmit(ctx, row)
	if err != nil {
		return err
	}
	if !admitted {
		return nil // a build for this merge is already in flight
	}
	executor, ok := e.registry.Lookup(facts.Class)
	if !ok {
		slog.WarnContext(ctx, "no executor for merged Task's class", "class", facts.Class)
		_, _ = e.store.Finish(ctx, adRow.ID, string(taskmeta.ExecCanceled), reasonNoExecutor+" "+string(facts.Class))
		return nil
	}
	if err := executor.Run(ctx, delivery.DispatchRequest{Execution: adRow, Task: facts, MergeSHA: mergeSHA}); err != nil {
		_, _ = e.store.Finish(ctx, adRow.ID, string(taskmeta.ExecFailed), err.Error())
		return err
	}
	return nil
}

// buildExistsForMerge reports whether any build Execution (active or terminal)
// already targets mergeSHA for this Task — the merge-identity dedupe for
// spawnBuild. It scans all rows (not just the latest) so a re-delivery of an
// OLDER merge is also caught, and a new merge is never blocked by a prior one.
func (e *Events) buildExistsForMerge(ctx context.Context, repoFullName string, issueNumber int, mergeSHA string) (bool, error) {
	rows, err := e.store.ListByIssue(ctx, repoFullName, issueNumber)
	if err != nil {
		return false, err
	}
	for i := range rows {
		if rows[i].Kind == string(taskmeta.KindBuild) && rows[i].CommitSHA == mergeSHA {
			return true, nil
		}
	}
	return false, nil
}

// ReconcileTaskPR is the sweep's PR-state healer (§5): for one Task whose latest
// coding Execution claims an open PR (and no build yet), it GETs the live PR
// state and applies the handler a missed webhook would have — merged spawns the
// build, closed-unmerged records the rejection. A no-divergence PR is a no-op.
// The Task facts and its latest-per-kind executions are passed in (the sweep
// already listed the issues and batch-loaded the rows for the whole repo).
func (e *Events) ReconcileTaskPR(ctx context.Context, orgID, projectID, repoFullName string, issueNumber int, execs map[string]*delivery.Execution) error {
	if e.prs == nil {
		return nil
	}
	coding := execs[string(taskmeta.KindCoding)]
	prNum := openPRNumber(coding)
	if prNum == 0 {
		return nil // latest coding is not claiming an open PR — nothing to heal
	}
	// Already healed for THIS coding attempt's merge? A build row NEWER than the
	// coding row claiming this PR means the current merge is built — nothing to do.
	// An OLDER build (from a previous coding attempt) must NOT block healing this
	// new PR (the round-2 defect: a second merged PR was skipped because an old
	// terminal build existed). spawnBuild's merge-SHA dedupe is the backstop.
	if b := execs[string(taskmeta.KindBuild)]; b != nil && coding != nil && !b.CreatedAt.Before(coding.CreatedAt) {
		return nil
	}
	pr, err := e.prs.GetPullRequestState(ctx, orgID, projectID, prNum)
	if err != nil || pr == nil {
		return err // transient — next sweep retries
	}
	switch {
	case pr.Merged:
		return e.spawnBuild(ctx, repoFullName, issueNumber, pr.MergeCommitSHA)
	case pr.State == "closed":
		return e.recordRejection(ctx, repoFullName, issueNumber)
	}
	return nil // PR still open — no divergence
}

// recordRejection appends a terminal coding row tagged
// taskmeta.ReasonPRClosedUnmerged so the derived status becomes rejected. If a
// coding Execution is still active (rare — PR closed while the agent was
// mid-run), it is Finished failed instead.
func (e *Events) recordRejection(ctx context.Context, repo string, issueNumber int) error {
	execs, err := e.store.LatestPerKind(ctx, repo, issueNumber)
	if err != nil {
		return err
	}
	if coding := execs[string(taskmeta.KindCoding)]; coding != nil && taskmeta.ExecutionStatus(coding.Status).IsActive() {
		_, ferr := e.store.Finish(ctx, coding.ID, string(taskmeta.ExecFailed), taskmeta.ReasonPRClosedUnmerged)
		return ferr
	}
	// No active coding row — the run already succeeded at PR-open. Look up the
	// org/project to append the rejection row.
	facts, ok, err := e.funnel.TaskFactsFor(ctx, repo, issueNumber)
	if err != nil || !ok {
		return err
	}
	row := &delivery.Execution{
		OrgID:       facts.OrgID,
		ProjectID:   facts.ProjectID,
		Repo:        facts.Repo,
		IssueNumber: facts.IssueNumber,
		Kind:        string(taskmeta.KindCoding),
		Status:      string(taskmeta.ExecQueued),
		DesignTag:   facts.DesignTag,
		SpecTag:     facts.SpecTag,
		Component:   facts.Component,
	}
	admitted, adRow, err := e.store.TryAdmit(ctx, row)
	if err != nil {
		return err
	}
	if !admitted {
		return nil
	}
	_, ferr := e.store.Finish(ctx, adRow.ID, string(taskmeta.ExecFailed), taskmeta.ReasonPRClosedUnmerged)
	return ferr
}
