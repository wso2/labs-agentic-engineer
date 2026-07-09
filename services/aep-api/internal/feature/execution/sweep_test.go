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
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/models"
)

type fakeRepoLister struct{ repos []RepoRef }

func (f fakeRepoLister) ListAll(context.Context) ([]RepoRef, error) { return f.repos, nil }

// countingPRReader records how many live PR fetches the healer makes.
type countingPRReader struct {
	states map[int]*gitrepo.PullRequestState
	calls  int
}

func (f *countingPRReader) GetPullRequestState(_ context.Context, _, _ string, number int) (*gitrepo.PullRequestState, error) {
	f.calls++
	return f.states[number], nil
}

func oneRepo() fakeRepoLister {
	return fakeRepoLister{repos: []RepoRef{{OrgID: "org1", ProjectID: "proj1", FullName: "o/r"}}}
}

// TestSweep_PRReconcile_HealsClosedIssue proves the sweep reconciles a CLOSED
// Task issue: a merged PR auto-closes its issue, but the merged chain still
// completes (§4). Before the fix the open-only filter left it dark forever.
func TestSweep_PRReconcile_HealsClosedIssue(t *testing.T) {
	store := newFakeStore()
	// Latest coding succeeded claiming open PR #3; no build yet.
	_, c, _ := store.TryAdmit(context.Background(), &models.Execution{Repo: "o/r", IssueNumber: 2, Kind: string(taskmeta.KindCoding), Component: "order-service"})
	_, _ = store.Finish(context.Background(), c.ID, string(taskmeta.ExecSucceeded), reasonPROpenPrefix+"3")

	issues := newFakeIssues([]gitrepo.IssueInfo{taskIssue(2, "order-service", nil, nil, "closed")})
	prs := &countingPRReader{states: map[int]*gitrepo.PullRequestState{3: {State: "closed", Merged: true, MergeCommitSHA: "sha3"}}}
	exec := &fakeExecutor{store: store, startOK: true}
	events := newEventsWithPR(store, issues, exec, prs)
	sweep := NewSweep(events.funnel, events, store, oneRepo(), issues, time.Minute)

	if err := sweep.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(exec.got) != 1 || exec.got[0].Execution.Kind != string(taskmeta.KindBuild) || exec.got[0].MergeSHA != "sha3" {
		t.Fatalf("a closed merged Task must be healed (build spawned), got %+v", exec.got)
	}
	if prs.calls != 1 {
		t.Errorf("expected exactly one live PR fetch, got %d", prs.calls)
	}
}

// TestSweep_PRReconcile_SettledTaskNoAPICall proves the cheap local pre-check:
// a Task whose current merge is already built (build newer than the coding row)
// costs one DB query and NO GitHub PR fetch, even though it is now visited.
func TestSweep_PRReconcile_SettledTaskNoAPICall(t *testing.T) {
	store := newFakeStore()
	// Coding succeeded (pr#3), then its build ran (newer row) → already settled.
	_, c, _ := store.TryAdmit(context.Background(), &models.Execution{Repo: "o/r", IssueNumber: 2, Kind: string(taskmeta.KindCoding)})
	_, _ = store.Finish(context.Background(), c.ID, string(taskmeta.ExecSucceeded), reasonPROpenPrefix+"3")
	_, b, _ := store.TryAdmit(context.Background(), &models.Execution{Repo: "o/r", IssueNumber: 2, Kind: string(taskmeta.KindBuild), CommitSHA: "sha3"})
	_, _ = store.Finish(context.Background(), b.ID, string(taskmeta.ExecSucceeded), "")

	issues := newFakeIssues([]gitrepo.IssueInfo{taskIssue(2, "order-service", nil, nil, "closed")})
	prs := &countingPRReader{states: map[int]*gitrepo.PullRequestState{}}
	exec := &fakeExecutor{store: store, startOK: true}
	events := newEventsWithPR(store, issues, exec, prs)
	sweep := NewSweep(events.funnel, events, store, oneRepo(), issues, time.Minute)

	if err := sweep.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if prs.calls != 0 {
		t.Errorf("a settled Task must cost no live PR fetch, got %d", prs.calls)
	}
	if len(exec.got) != 0 {
		t.Errorf("a settled Task must not re-dispatch, got %d", len(exec.got))
	}
}

// --- aep:attention clearing authority ----------------------------------------
// (containsLabel now lives in funnel.go — the sweep tests share it.)

func attnSweep(t *testing.T, store *fakeStore, issues *fakeIssues) *Sweep {
	t.Helper()
	exec := &fakeExecutor{store: store, startOK: true}
	events := newEventsWithPR(store, issues, exec, &countingPRReader{states: map[int]*gitrepo.PullRequestState{}})
	return NewSweep(events.funnel, events, store, oneRepo(), issues, time.Minute)
}

func TestSweep_ClearsStaleAttention_WhenHealthy(t *testing.T) {
	store := newFakeStore()
	// Deployed: coding succeeded + build succeeded (the earlier failed attempt
	// that raised the flag is gone — latest per kind is healthy).
	_, c, _ := store.TryAdmit(context.Background(), &models.Execution{Repo: "o/r", IssueNumber: 2, Kind: string(taskmeta.KindCoding)})
	_, _ = store.Finish(context.Background(), c.ID, string(taskmeta.ExecSucceeded), "")
	_, b, _ := store.TryAdmit(context.Background(), &models.Execution{Repo: "o/r", IssueNumber: 2, Kind: string(taskmeta.KindBuild), CommitSHA: "sha"})
	_, _ = store.Finish(context.Background(), b.ID, string(taskmeta.ExecSucceeded), "")

	issues := newFakeIssues([]gitrepo.IssueInfo{taskIssue(2, "order-service", nil, []string{taskmeta.LabelAttention}, "open")})
	if err := attnSweep(t, store, issues).Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if !containsLabel(issues.removed[2], taskmeta.LabelAttention) {
		t.Fatalf("a healthy deployed Task must have aep:attention cleared, removed=%v", issues.removed[2])
	}
}

func TestSweep_KeepsAttention_WhenLatestExecutionFailed(t *testing.T) {
	store := newFakeStore()
	_, c, _ := store.TryAdmit(context.Background(), &models.Execution{Repo: "o/r", IssueNumber: 2, Kind: string(taskmeta.KindCoding)})
	_, _ = store.Finish(context.Background(), c.ID, string(taskmeta.ExecSucceeded), "")
	_, b, _ := store.TryAdmit(context.Background(), &models.Execution{Repo: "o/r", IssueNumber: 2, Kind: string(taskmeta.KindBuild), CommitSHA: "sha"})
	_, _ = store.Finish(context.Background(), b.ID, string(taskmeta.ExecFailed), "boom")

	issues := newFakeIssues([]gitrepo.IssueInfo{taskIssue(2, "order-service", nil, []string{taskmeta.LabelAttention}, "open")})
	if err := attnSweep(t, store, issues).Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if containsLabel(issues.removed[2], taskmeta.LabelAttention) {
		t.Fatal("a Task with a failed latest execution must KEEP aep:attention")
	}
}

func TestSweep_SkipsAttentionClear_WhenBlockInvalid(t *testing.T) {
	store := newFakeStore()
	_, c, _ := store.TryAdmit(context.Background(), &models.Execution{Repo: "o/r", IssueNumber: 2, Kind: string(taskmeta.KindCoding)})
	_, _ = store.Finish(context.Background(), c.ID, string(taskmeta.ExecSucceeded), "")

	// Task labels present (marker + class + attention) but the body has no valid
	// machine block — a live attention reason, so clearing is skipped.
	labels := append(taskmeta.NewTaskLabels(taskmeta.ClassCoding, taskmeta.OriginSpecPlan), taskmeta.LabelAttention)
	issues := newFakeIssues([]gitrepo.IssueInfo{{Number: 2, Title: "T", Body: "no machine block here", State: "open", Labels: labels}})
	if err := attnSweep(t, store, issues).Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if containsLabel(issues.removed[2], taskmeta.LabelAttention) {
		t.Fatal("a mangled-block Task must stay flagged (block is a live reason)")
	}
}

func TestSweep_AttentionAbsent_NoGitHubCall(t *testing.T) {
	store := newFakeStore()
	_, c, _ := store.TryAdmit(context.Background(), &models.Execution{Repo: "o/r", IssueNumber: 2, Kind: string(taskmeta.KindCoding)})
	_, _ = store.Finish(context.Background(), c.ID, string(taskmeta.ExecSucceeded), "")

	issues := newFakeIssues([]gitrepo.IssueInfo{taskIssue(2, "order-service", nil, nil, "open")}) // no attention label
	if err := attnSweep(t, store, issues).Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(issues.removed[2]) != 0 {
		t.Fatalf("no attention label → no RemoveLabel call, got %v", issues.removed[2])
	}
}
