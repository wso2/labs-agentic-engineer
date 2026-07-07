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
	"fmt"
	"strconv"
	"testing"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/models"
	"github.com/wso2/aep/aep-api/repositories"
)

// reconcile loads the Task's latest-per-kind rows and runs the PR-state healer.
func reconcile(t *testing.T, e *Events, repo string, issue int) error {
	t.Helper()
	execs, err := e.store.LatestPerKind(context.Background(), repo, issue)
	if err != nil {
		t.Fatalf("LatestPerKind: %v", err)
	}
	return e.ReconcileTaskPR(context.Background(), "org1", "proj1", repo, issue, execs)
}

func prPayload(action string, number int, merged bool, mergeSHA, body, sender string) []byte {
	return []byte(fmt.Sprintf(`{
		"action": %q,
		"pull_request": {"number": %d, "merged": %t, "draft": false, "state": "closed", "body": %q, "merge_commit_sha": %q, "head": {"ref": "feature"}},
		"repository": {"full_name": "o/r"},
		"sender": {"login": %q}
	}`, action, number, merged, body, mergeSHA, sender))
}

func newEvents(store *fakeStore, issues *fakeIssues, exec Executor) *Events {
	return newEventsWithPR(store, issues, exec, nil)
}

func newEventsWithPR(store *fakeStore, issues *fakeIssues, exec Executor, prs PRReader) *Events {
	reg := NewRegistry()
	if exec != nil {
		reg.Register(taskmeta.ClassCoding, exec)
	}
	resolver := NewTaskResolver(issues, fakeRepos{orgID: "org1", projectID: "proj1"})
	return NewEvents(store, resolver, reg, prs)
}

func TestEvents_PROpened_EndsCodingExecution(t *testing.T) {
	store := newFakeStore()
	// A running coding execution exists for issue 2.
	_, row, _ := store.TryAdmit(context.Background(), &models.Execution{Repo: "o/r", IssueNumber: 2, Kind: string(taskmeta.KindCoding)})
	store.markRunning(row.ID)

	issues := newFakeIssues([]gitrepo.IssueInfo{taskIssue(2, "order-service", nil, nil, "open")})
	e := newEvents(store, issues, &fakeExecutor{store: store, startOK: true})

	if err := e.PullRequestOpened(context.Background(), "pull_request", "opened",
		prPayload("opened", 7, false, "", "Closes #2", "dev")); err != nil {
		t.Fatalf("PullRequestOpened: %v", err)
	}
	execs, _ := store.LatestPerKind(context.Background(), "o/r", 2)
	if c := execs[string(taskmeta.KindCoding)]; c == nil || c.Status != string(taskmeta.ExecSucceeded) {
		t.Errorf("expected coding execution succeeded after PR open, got %+v", c)
	}
}

func TestEvents_PRMerged_SpawnsBuild(t *testing.T) {
	store := newFakeStore()
	issues := newFakeIssues([]gitrepo.IssueInfo{taskIssue(2, "order-service", nil, nil, "open")})
	exec := &fakeExecutor{store: store, startOK: true}
	e := newEvents(store, issues, exec)

	if err := e.PullRequestClosed(context.Background(), "pull_request", "closed",
		prPayload("closed", 7, true, "abc123", "Closes #2", "dev")); err != nil {
		t.Fatalf("PullRequestClosed(merged): %v", err)
	}
	if len(exec.got) != 1 || exec.got[0].Execution.Kind != string(taskmeta.KindBuild) {
		t.Fatalf("expected one build dispatch, got %+v", exec.got)
	}
	if exec.got[0].MergeSHA != "abc123" {
		t.Errorf("expected merge SHA propagated, got %q", exec.got[0].MergeSHA)
	}
}

func TestEvents_PRClosedUnmerged_RecordsRejection(t *testing.T) {
	store := newFakeStore()
	// A coding execution already succeeded (PR was opened earlier).
	_, row, _ := store.TryAdmit(context.Background(), &models.Execution{Repo: "o/r", IssueNumber: 2, Kind: string(taskmeta.KindCoding)})
	_, _ = store.Finish(context.Background(), row.ID, string(taskmeta.ExecSucceeded), "pull request opened")

	issues := newFakeIssues([]gitrepo.IssueInfo{taskIssue(2, "order-service", nil, nil, "open")})
	e := newEvents(store, issues, &fakeExecutor{store: store, startOK: true})

	if err := e.PullRequestClosed(context.Background(), "pull_request", "closed",
		prPayload("closed", 7, false, "", "Closes #2", "dev")); err != nil {
		t.Fatalf("PullRequestClosed(unmerged): %v", err)
	}
	execs, _ := store.LatestPerKind(context.Background(), "o/r", 2)
	c := execs[string(taskmeta.KindCoding)]
	if c == nil || c.Status != string(taskmeta.ExecFailed) || c.Reason != taskmeta.ReasonPRClosedUnmerged {
		t.Errorf("expected a rejected coding row, got %+v", c)
	}
	// Derived PR state must be closed-unmerged → rejected.
	if got := taskmeta.PRStateFromFacts(repositories.ExecutionFacts(execs)); got != taskmeta.PRClosedUnmerged {
		t.Errorf("prState = %q, want closed_unmerged", got)
	}
}

// --- PR-state healer (§5 reconciliation, amended decision #1) ----------------

func seedOpenPRTask(store *fakeStore, prNumber int) {
	// A coding execution that succeeded at PR-open, claiming PR #prNumber open.
	_, row, _ := store.TryAdmit(context.Background(), &models.Execution{Repo: "o/r", IssueNumber: 2, Kind: string(taskmeta.KindCoding)})
	_, _ = store.Finish(context.Background(), row.ID, string(taskmeta.ExecSucceeded), reasonPROpenPrefix+strconv.Itoa(prNumber))
}

func TestEvents_ReconcileTaskPR_MissedCloseUnmerged_HealsToRejected(t *testing.T) {
	store := newFakeStore()
	seedOpenPRTask(store, 7)
	issues := newFakeIssues([]gitrepo.IssueInfo{taskIssue(2, "order-service", nil, nil, "open")})
	prs := fakePRReader{states: map[int]*gitrepo.PullRequestState{7: {State: "closed", Merged: false}}}
	e := newEventsWithPR(store, issues, &fakeExecutor{store: store, startOK: true}, prs)

	if err := reconcile(t, e, "o/r", 2); err != nil {
		t.Fatalf("ReconcileTaskPR: %v", err)
	}
	execs, _ := store.LatestPerKind(context.Background(), "o/r", 2)
	if got := taskmeta.PRStateFromFacts(repositories.ExecutionFacts(execs)); got != taskmeta.PRClosedUnmerged {
		t.Errorf("missed close-unmerged must heal to rejected, prState=%q", got)
	}
}

func TestEvents_ReconcileTaskPR_MissedMerge_SpawnsBuild(t *testing.T) {
	store := newFakeStore()
	seedOpenPRTask(store, 7)
	issues := newFakeIssues([]gitrepo.IssueInfo{taskIssue(2, "order-service", nil, nil, "open")})
	prs := fakePRReader{states: map[int]*gitrepo.PullRequestState{7: {State: "closed", Merged: true, MergeCommitSHA: "abc123"}}}
	exec := &fakeExecutor{store: store, startOK: true}
	e := newEventsWithPR(store, issues, exec, prs)

	if err := reconcile(t, e, "o/r", 2); err != nil {
		t.Fatalf("ReconcileTaskPR: %v", err)
	}
	if len(exec.got) != 1 || exec.got[0].Execution.Kind != string(taskmeta.KindBuild) || exec.got[0].MergeSHA != "abc123" {
		t.Fatalf("missed merge must spawn a build with the merge SHA, got %+v", exec.got)
	}
}

// TestEvents_ReconcileTaskPR_NewMergeWithOlderBuild_Heals proves the healer
// heals a merged-without-build-for-that-SHA coding row EVEN WHEN an older attempt
// left a terminal build (round-2 fix): the older build (created before the latest
// coding row) does not count as "this PR is built".
func TestEvents_ReconcileTaskPR_NewMergeWithOlderBuild_Heals(t *testing.T) {
	store := newFakeStore()
	// Older failed build from a previous attempt (SHA sha2), created FIRST.
	_, b1, _ := store.TryAdmit(context.Background(), &models.Execution{Repo: "o/r", IssueNumber: 2, Kind: string(taskmeta.KindBuild), CommitSHA: "sha2"})
	_, _ = store.Finish(context.Background(), b1.ID, string(taskmeta.ExecFailed), "boom")
	// The latest coding succeeded claiming open PR #3 — newer than the old build.
	_, c, _ := store.TryAdmit(context.Background(), &models.Execution{Repo: "o/r", IssueNumber: 2, Kind: string(taskmeta.KindCoding)})
	_, _ = store.Finish(context.Background(), c.ID, string(taskmeta.ExecSucceeded), reasonPROpenPrefix+"3")

	issues := newFakeIssues([]gitrepo.IssueInfo{taskIssue(2, "order-service", nil, nil, "open")})
	prs := fakePRReader{states: map[int]*gitrepo.PullRequestState{3: {State: "closed", Merged: true, MergeCommitSHA: "sha3"}}}
	exec := &fakeExecutor{store: store, startOK: true}
	e := newEventsWithPR(store, issues, exec, prs)

	if err := reconcile(t, e, "o/r", 2); err != nil {
		t.Fatalf("ReconcileTaskPR: %v", err)
	}
	if len(exec.got) != 1 || exec.got[0].Execution.Kind != string(taskmeta.KindBuild) || exec.got[0].MergeSHA != "sha3" {
		t.Fatalf("healer must spawn a build for the NEW merge despite an older build, got %+v", exec.got)
	}
}

func TestEvents_ReconcileTaskPR_NoDivergence_NoWrites(t *testing.T) {
	store := newFakeStore()
	seedOpenPRTask(store, 7)
	issues := newFakeIssues([]gitrepo.IssueInfo{taskIssue(2, "order-service", nil, nil, "open")})
	prs := fakePRReader{states: map[int]*gitrepo.PullRequestState{7: {State: "open", Merged: false}}}
	exec := &fakeExecutor{store: store, startOK: true}
	e := newEventsWithPR(store, issues, exec, prs)

	before, _ := store.ListByIssue(context.Background(), "o/r", 2)
	if err := reconcile(t, e, "o/r", 2); err != nil {
		t.Fatalf("ReconcileTaskPR: %v", err)
	}
	after, _ := store.ListByIssue(context.Background(), "o/r", 2)
	if len(after) != len(before) || len(exec.got) != 0 {
		t.Errorf("PR still open must be a no-op: rows %d→%d, dispatches %d", len(before), len(after), len(exec.got))
	}
}

// TestEvents_PROpened_PlatformSenderStillEnds is the gate-review regression:
// pull_request.* handlers apply NO echo suppression. In App mode the coding
// runner opens its PR as <slug>[bot] — the same login as the platform identity —
// so a self-sender pull_request.opened MUST still end the coding Execution
// (suppressing it would strand the row in_progress forever, mutex held).
func TestEvents_PROpened_PlatformSenderStillEnds(t *testing.T) {
	store := newFakeStore()
	_, row, _ := store.TryAdmit(context.Background(), &models.Execution{Repo: "o/r", IssueNumber: 2, Kind: string(taskmeta.KindCoding)})
	store.markRunning(row.ID)

	issues := newFakeIssues([]gitrepo.IssueInfo{taskIssue(2, "order-service", nil, nil, "open")})
	e := newEvents(store, issues, &fakeExecutor{store: store, startOK: true})

	// Sender is the platform/App-bot login — must NOT be dropped.
	if err := e.PullRequestOpened(context.Background(), "pull_request", "opened",
		prPayload("opened", 7, false, "", "Closes #2", "aep-platform[bot]")); err != nil {
		t.Fatalf("PullRequestOpened: %v", err)
	}
	execs, _ := store.LatestPerKind(context.Background(), "o/r", 2)
	if c := execs[string(taskmeta.KindCoding)]; c == nil || c.Status != string(taskmeta.ExecSucceeded) {
		t.Fatalf("a self-sender PR-opened must still end the coding execution, got %+v", c)
	}
}

// TestEvents_PRMerged_PlatformSenderStillSpawnsBuild is the same regression on
// the merge path: a self-sender merge delivery must still spawn the build.
func TestEvents_PRMerged_PlatformSenderStillSpawnsBuild(t *testing.T) {
	store := newFakeStore()
	issues := newFakeIssues([]gitrepo.IssueInfo{taskIssue(2, "order-service", nil, nil, "open")})
	exec := &fakeExecutor{store: store, startOK: true}
	e := newEvents(store, issues, exec)

	if err := e.PullRequestClosed(context.Background(), "pull_request", "closed",
		prPayload("closed", 7, true, "abc123", "Closes #2", "aep-platform[bot]")); err != nil {
		t.Fatalf("PullRequestClosed(merged): %v", err)
	}
	if len(exec.got) != 1 || exec.got[0].Execution.Kind != string(taskmeta.KindBuild) {
		t.Fatalf("a self-sender merge must still spawn a build, got %+v", exec.got)
	}
}

// TestEvents_PRMerged_ReDeliveryAfterBuild_NoDuplicate pins the merge-identity
// dedupe: a GitHub re-delivery of the SAME merge (same commit SHA) arriving AFTER
// the build terminated must not admit a second build row (TryAdmit's mutex only
// blocks ACTIVE rows; the SHA guard is what stops a terminal-row duplicate).
func TestEvents_PRMerged_ReDeliveryAfterBuild_NoDuplicate(t *testing.T) {
	store := newFakeStore()
	// A build already ran to completion for THIS merge (SHA abc123).
	_, b, _ := store.TryAdmit(context.Background(), &models.Execution{Repo: "o/r", IssueNumber: 2, Kind: string(taskmeta.KindBuild), CommitSHA: "abc123"})
	_, _ = store.Finish(context.Background(), b.ID, string(taskmeta.ExecSucceeded), "")

	issues := newFakeIssues([]gitrepo.IssueInfo{taskIssue(2, "order-service", nil, nil, "open")})
	exec := &fakeExecutor{store: store, startOK: true}
	e := newEvents(store, issues, exec)

	if err := e.PullRequestClosed(context.Background(), "pull_request", "closed",
		prPayload("closed", 7, true, "abc123", "Closes #2", "dev")); err != nil {
		t.Fatalf("PullRequestClosed(merged re-delivery): %v", err)
	}
	if len(exec.got) != 0 {
		t.Fatalf("re-delivery of the same merge must not dispatch again, got %d", len(exec.got))
	}
	if n := countBuilds(store, 2); n != 1 {
		t.Fatalf("expected exactly one build row after same-merge re-delivery, got %d", n)
	}
}

// TestEvents_PRMerged_NewMergeAfterFailedBuild_Spawns pins the round-2 fix: a
// genuinely NEW merged PR (new SHA) must spawn a build even when an OLDER
// attempt's terminal (failed) build exists — the task stays retryable.
func TestEvents_PRMerged_NewMergeAfterFailedBuild_Spawns(t *testing.T) {
	store := newFakeStore()
	// Round 1: PR #2 merged at SHA_2, its build FAILED.
	_, b1, _ := store.TryAdmit(context.Background(), &models.Execution{Repo: "o/r", IssueNumber: 2, Kind: string(taskmeta.KindBuild), CommitSHA: "sha2"})
	_, _ = store.Finish(context.Background(), b1.ID, string(taskmeta.ExecFailed), "boom")

	issues := newFakeIssues([]gitrepo.IssueInfo{taskIssue(2, "order-service", nil, nil, "open")})
	exec := &fakeExecutor{store: store, startOK: true}
	e := newEvents(store, issues, exec)

	// Round 2: PR #3 merged at a NEW SHA_3.
	if err := e.PullRequestClosed(context.Background(), "pull_request", "closed",
		prPayload("closed", 3, true, "sha3", "Closes #2", "dev")); err != nil {
		t.Fatalf("PullRequestClosed(new merge): %v", err)
	}
	if len(exec.got) != 1 || exec.got[0].Execution.Kind != string(taskmeta.KindBuild) || exec.got[0].MergeSHA != "sha3" {
		t.Fatalf("a new merged PR must spawn a build at the new SHA, got %+v", exec.got)
	}
	if n := countBuilds(store, 2); n != 2 {
		t.Fatalf("expected two build rows (old failed + new), got %d", n)
	}
}

func countBuilds(store *fakeStore, issue int) int {
	rows, _ := store.ListByIssue(context.Background(), "o/r", issue)
	n := 0
	for _, r := range rows {
		if r.Kind == string(taskmeta.KindBuild) {
			n++
		}
	}
	return n
}
