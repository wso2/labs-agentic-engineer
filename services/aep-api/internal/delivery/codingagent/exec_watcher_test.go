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

package codingagent

import (
	"context"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// runningBuild seeds a running build row with a given run name + reason.
func runningBuild(id, runName, reason string) *delivery.Execution {
	return &delivery.Execution{
		ID: id, OrgID: "acme", ProjectID: "widgets", Repo: "acme/widgets", IssueNumber: 7,
		Kind: string(taskmeta.KindBuild), Status: string(taskmeta.ExecRunning),
		Component: "order-service", CommitSHA: "deadbeef", RunName: runName, Reason: reason,
	}
}

// ocRuns serves a fixed WorkflowRun for any GetWorkflowRun by run name.
func ocRuns(byRun map[string]*gen.WorkflowRun) *ocmocks.ComponentClientMock {
	return &ocmocks.ComponentClientMock{
		GetWorkflowRunFunc: func(_ context.Context, _, runName string) (*gen.WorkflowRun, error) {
			return byRun[runName], nil
		},
	}
}

func authFailedRun(name string) *gen.WorkflowRun {
	return &gen.WorkflowRun{
		Name: name, Completed: true, Status: openchoreo.ReasonWorkflowFailed,
		Tasks: []gen.WorkflowRunTask{{Name: "checkout-source", Phase: "Failed", Message: "fatal: Authentication failed for 'https://github.com/acme/widgets'"}},
	}
}

func plainFailedRun(name string) *gen.WorkflowRun {
	return &gen.WorkflowRun{
		Name: name, Completed: true, Status: openchoreo.ReasonWorkflowFailed,
		Tasks: []gen.WorkflowRunTask{{Name: "build", Phase: "Failed", Message: "compilation error"}},
	}
}

func TestExecWatcher_BuildGitAuthFailure_WithinBudget_ReMintsAndRetries(t *testing.T) {
	row := runningBuild("b1", "run-1", "") // first failure — no retry marker yet
	repo := newFakeExecRepo(row)
	retrier := &fakeRetrier{newRun: "run-2"}
	w := NewExecWatcher(ocRuns(map[string]*gen.WorkflowRun{"run-1": authFailedRun("run-1")}), repo, nil, 0).
		WithBuildRetrier(retrier, 3)

	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if retrier.count() != 1 {
		t.Fatalf("expected exactly one re-mint+retry, got %d", retrier.count())
	}
	got := repo.get("b1")
	if got.Status != string(taskmeta.ExecRunning) {
		t.Errorf("row must stay running across a retry, got %q", got.Status)
	}
	if got.RunName != "run-2" {
		t.Errorf("row must track the re-triggered run, got %q", got.RunName)
	}
	if got.Reason != "build_auth_retry:1" {
		t.Errorf("retry attempt must be recorded on reason, got %q", got.Reason)
	}
}

func TestExecWatcher_BuildGitAuthFailure_BudgetExhausted_FinishesFailed(t *testing.T) {
	row := runningBuild("b1", "run-3", "build_auth_retry:3") // already at budget
	repo := newFakeExecRepo(row)
	retrier := &fakeRetrier{newRun: "run-4"}
	w := NewExecWatcher(ocRuns(map[string]*gen.WorkflowRun{"run-3": authFailedRun("run-3")}), repo, nil, 0).
		WithBuildRetrier(retrier, 3)

	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if retrier.count() != 0 {
		t.Errorf("budget exhausted → no further retry, got %d", retrier.count())
	}
	got := repo.get("b1")
	if got.Status != string(taskmeta.ExecFailed) || got.Reason != buildAuthRetryExceededReason {
		t.Errorf("exhausted budget must Finish failed with %q, got status=%q reason=%q", buildAuthRetryExceededReason, got.Status, got.Reason)
	}
}

func TestExecWatcher_BuildGitAuthFailure_ReMintErrorStillMarchesToBudget(t *testing.T) {
	row := runningBuild("b1", "run-1", "")
	repo := newFakeExecRepo(row)
	retrier := &fakeRetrier{err: errors.New("org disconnected")}
	w := NewExecWatcher(ocRuns(map[string]*gen.WorkflowRun{"run-1": authFailedRun("run-1")}), repo, nil, 0).
		WithBuildRetrier(retrier, 3)

	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	got := repo.get("b1")
	// The attempt counter still advances (so a stuck re-mint eventually exhausts),
	// but the run name is unchanged (the old failed run).
	if got.Reason != "build_auth_retry:1" || got.RunName != "run-1" {
		t.Errorf("failed re-mint must advance the counter on the same run, got reason=%q run=%q", got.Reason, got.RunName)
	}
	if got.Status != string(taskmeta.ExecRunning) {
		t.Errorf("row must stay running, got %q", got.Status)
	}
}

func TestExecWatcher_BuildPlainFailure_FinishesFailed(t *testing.T) {
	row := runningBuild("b1", "run-1", "")
	repo := newFakeExecRepo(row)
	retrier := &fakeRetrier{}
	w := NewExecWatcher(ocRuns(map[string]*gen.WorkflowRun{"run-1": plainFailedRun("run-1")}), repo, nil, 0).
		WithBuildRetrier(retrier, 3)

	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if retrier.count() != 0 {
		t.Errorf("a non-auth failure must not retry, got %d", retrier.count())
	}
	if got := repo.get("b1"); got.Status != string(taskmeta.ExecFailed) {
		t.Errorf("plain failure must Finish failed, got %q", got.Status)
	}
}

func TestExecWatcher_BuildSuccess_FinishesSucceeded(t *testing.T) {
	row := runningBuild("b1", "run-1", "")
	repo := newFakeExecRepo(row)
	ok := &gen.WorkflowRun{Name: "run-1", Completed: true, Status: openchoreo.ReasonWorkflowSucceeded}
	w := NewExecWatcher(ocRuns(map[string]*gen.WorkflowRun{"run-1": ok}), repo, nil, 0)

	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := repo.get("b1"); got.Status != string(taskmeta.ExecSucceeded) {
		t.Errorf("build success must Finish succeeded, got %q", got.Status)
	}
}

// TestExecWatcher_ClosesLegacyCodingAgentExecutions: ca- KindCoding rows are
// not WorkflowRuns. Skipping them forever left pre-migration executions
// `running`; the watcher now Finishes them failed so the issue mutex releases.
func TestExecWatcher_ClosesLegacyCodingAgentExecutions(t *testing.T) {
	caRow := &delivery.Execution{ID: "j1", OrgID: "acme", Repo: "acme/widgets", IssueNumber: 7,
		Kind: string(taskmeta.KindCoding), Status: string(taskmeta.ExecRunning), RunName: "ca-abc12345-2601011200"}
	wfRow := &delivery.Execution{ID: "w1", OrgID: "acme", Repo: "acme/widgets", IssueNumber: 8,
		Kind: string(taskmeta.KindCoding), Status: string(taskmeta.ExecRunning), RunName: "wf-xyz"}
	repo := newFakeExecRepo(caRow, wfRow)

	var polled []string
	oc := &ocmocks.ComponentClientMock{
		GetWorkflowRunFunc: func(_ context.Context, _, runName string) (*gen.WorkflowRun, error) {
			polled = append(polled, runName)
			return &gen.WorkflowRun{Name: runName, Completed: false}, nil // still running
		},
	}
	w := NewExecWatcher(oc, repo, nil, 0)

	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(polled) != 1 || polled[0] != "wf-xyz" {
		t.Fatalf("ExecWatcher must not poll ca- as WorkflowRuns, polled=%v", polled)
	}
	if got := repo.get("j1"); got.Status != string(taskmeta.ExecFailed) || got.Reason != legacyCodingExecutionReason {
		t.Fatalf("legacy ca- row = status %q reason %q, want failed/%s", got.Status, got.Reason, legacyCodingExecutionReason)
	}
	if got := repo.get("w1"); got.Status != string(taskmeta.ExecRunning) {
		t.Fatalf("wf- coding row must stay running until WorkflowRun completes, got %q", got.Status)
	}
}

func TestExecWatcher_CodingFailure_FinishesFailed_SuccessRidesPRWebhook(t *testing.T) {
	// A ClusterWorkflow (`wf-…`) coding run — the ExecWatcher's domain;
	// `ca-…` coding-agent runs are the JobWatcher's and are skipped here.
	coding := &delivery.Execution{ID: "c1", OrgID: "acme", Repo: "acme/widgets", IssueNumber: 7,
		Kind: string(taskmeta.KindCoding), Status: string(taskmeta.ExecRunning), RunName: "wf-1"}
	repo := newFakeExecRepo(coding)
	failed := &gen.WorkflowRun{Name: "wf-1", Completed: true, Status: openchoreo.ReasonWorkflowFailed}
	w := NewExecWatcher(ocRuns(map[string]*gen.WorkflowRun{"wf-1": failed}), repo, nil, 0)

	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := repo.get("c1"); got.Status != string(taskmeta.ExecFailed) {
		t.Errorf("failed coding run must Finish failed (no PR will open), got %q", got.Status)
	}
}

// countingBuildObserver counts OnBuildTerminal deliveries so a Finish loser can
// be shown not to re-fire terminal side effects.
type countingBuildObserver struct{ n int }

func (c *countingBuildObserver) OnBuildTerminal(context.Context, delivery.BuildTerminal) error {
	c.n++
	return nil
}

// TestExecWatcher_BuildSuccess_FinishLoserSkipsObserver: reconcile twice on the
// same completed build — the second Finish is a loser ((nil, nil)) and must not
// call OnBuildTerminal again.
func TestExecWatcher_BuildSuccess_FinishLoserSkipsObserver(t *testing.T) {
	row := runningBuild("b1", "run-1", "")
	repo := newFakeExecRepo(row)
	ok := &gen.WorkflowRun{Name: "run-1", Completed: true, Status: openchoreo.ReasonWorkflowSucceeded}
	obs := &countingBuildObserver{}
	w := NewExecWatcher(ocRuns(map[string]*gen.WorkflowRun{"run-1": ok}), repo, nil, 0).
		WithBuildObserver(obs)

	ctx := context.Background()
	if err := w.Sweep(ctx); err != nil {
		t.Fatalf("first Sweep: %v", err)
	}
	if obs.n != 1 {
		t.Fatalf("winner must notify observer once, got %d", obs.n)
	}
	// Row is terminal; Sweep no longer lists it. Drive reconcile directly for the loser path.
	w.reconcile(ctx, row, ok)
	if obs.n != 1 {
		t.Fatalf("Finish loser must not re-fire OnBuildTerminal, got %d", obs.n)
	}
}

// TestExecWatcher_CodingFailure_FinishLoserSkipsNotify: reconcile twice on the
// same failed coding run — the second Finish is a loser and must not Notify.
func TestExecWatcher_CodingFailure_FinishLoserSkipsNotify(t *testing.T) {
	coding := &delivery.Execution{ID: "c1", OrgID: "acme", Repo: "acme/widgets", IssueNumber: 7,
		Kind: string(taskmeta.KindCoding), Status: string(taskmeta.ExecRunning), RunName: "wf-1"}
	repo := newFakeExecRepo(coding)
	failed := &gen.WorkflowRun{Name: "wf-1", Completed: true, Status: openchoreo.ReasonWorkflowFailed}
	hub := delivery.NewTaskStreamHub()
	ch, cancel := hub.Subscribe(coding.Repo, coding.IssueNumber)
	defer cancel()
	w := NewExecWatcher(ocRuns(map[string]*gen.WorkflowRun{"wf-1": failed}), repo, nil, 0).
		WithTaskNotifier(hub)

	ctx := context.Background()
	if err := w.Sweep(ctx); err != nil {
		t.Fatalf("first Sweep: %v", err)
	}
	select {
	case <-ch:
	default:
		t.Fatal("winner must Notify")
	}
	w.reconcile(ctx, coding, failed)
	select {
	case <-ch:
		t.Fatal("Finish loser must not Notify")
	default:
	}
}

// TestExecWatcher_BuildPlainFailure_FinishLoserSkipsNotifyAndObserver: reconcile
// twice on the same plain-failed build — the second Finish is a loser and must
// not Notify or re-fire OnBuildTerminal.
func TestExecWatcher_BuildPlainFailure_FinishLoserSkipsNotifyAndObserver(t *testing.T) {
	row := runningBuild("b1", "run-1", "")
	repo := newFakeExecRepo(row)
	failed := plainFailedRun("run-1")
	hub := delivery.NewTaskStreamHub()
	ch, cancel := hub.Subscribe(row.Repo, row.IssueNumber)
	defer cancel()
	obs := &countingBuildObserver{}
	w := NewExecWatcher(ocRuns(map[string]*gen.WorkflowRun{"run-1": failed}), repo, nil, 0).
		WithBuildRetrier(&fakeRetrier{}, 3).
		WithTaskNotifier(hub).
		WithBuildObserver(obs)

	ctx := context.Background()
	if err := w.Sweep(ctx); err != nil {
		t.Fatalf("first Sweep: %v", err)
	}
	select {
	case <-ch:
	default:
		t.Fatal("winner must Notify")
	}
	if obs.n != 1 {
		t.Fatalf("winner must notify observer once, got %d", obs.n)
	}
	w.reconcile(ctx, row, failed)
	select {
	case <-ch:
		t.Fatal("Finish loser must not Notify")
	default:
	}
	if obs.n != 1 {
		t.Fatalf("Finish loser must not re-fire OnBuildTerminal, got %d", obs.n)
	}
}

// TestExecWatcher_BuildAuthBudgetExhausted_FinishLoserSkipsNotifyAndObserver:
// reconcile twice after auth-retry budget is spent — the second Finish is a
// loser and must not Notify or re-fire OnBuildTerminal.
func TestExecWatcher_BuildAuthBudgetExhausted_FinishLoserSkipsNotifyAndObserver(t *testing.T) {
	row := runningBuild("b1", "run-3", "build_auth_retry:3")
	repo := newFakeExecRepo(row)
	failed := authFailedRun("run-3")
	hub := delivery.NewTaskStreamHub()
	ch, cancel := hub.Subscribe(row.Repo, row.IssueNumber)
	defer cancel()
	obs := &countingBuildObserver{}
	w := NewExecWatcher(ocRuns(map[string]*gen.WorkflowRun{"run-3": failed}), repo, nil, 0).
		WithBuildRetrier(&fakeRetrier{newRun: "run-4"}, 3).
		WithTaskNotifier(hub).
		WithBuildObserver(obs)

	ctx := context.Background()
	if err := w.Sweep(ctx); err != nil {
		t.Fatalf("first Sweep: %v", err)
	}
	select {
	case <-ch:
	default:
		t.Fatal("winner must Notify")
	}
	if obs.n != 1 {
		t.Fatalf("winner must notify observer once, got %d", obs.n)
	}
	w.reconcile(ctx, row, failed)
	select {
	case <-ch:
		t.Fatal("Finish loser must not Notify")
	default:
	}
	if obs.n != 1 {
		t.Fatalf("Finish loser must not re-fire OnBuildTerminal, got %d", obs.n)
	}
}

// TestExecWatcher_BuildPlainFailure_FinishErrorSkipsObserver: a Finish DB
// error must Notify (idempotent) but must NOT call OnBuildTerminal — the row
// stays running for the next tick.
func TestExecWatcher_BuildPlainFailure_FinishErrorSkipsObserver(t *testing.T) {
	row := runningBuild("b1", "run-1", "")
	repo := newFakeExecRepo(row)
	repo.finishErr = errors.New("db blip")
	failed := plainFailedRun("run-1")
	hub := delivery.NewTaskStreamHub()
	ch, cancel := hub.Subscribe(row.Repo, row.IssueNumber)
	defer cancel()
	obs := &countingBuildObserver{}
	w := NewExecWatcher(ocRuns(map[string]*gen.WorkflowRun{"run-1": failed}), repo, nil, 0).
		WithBuildRetrier(&fakeRetrier{}, 3).
		WithTaskNotifier(hub).
		WithBuildObserver(obs)

	w.reconcile(context.Background(), row, failed)
	select {
	case <-ch:
	default:
		t.Fatal("Finish DB error must still Notify")
	}
	if obs.n != 0 {
		t.Fatalf("Finish DB error must not OnBuildTerminal, got %d", obs.n)
	}
	if got := repo.get("b1"); got.Status != string(taskmeta.ExecRunning) {
		t.Fatalf("row must stay running after Finish error, got %q", got.Status)
	}
}

// TestExecWatcher_BuildAuthBudgetExhausted_FinishErrorSkipsObserver: same
// Finish-error contract on the auth-budget-exhausted branch.
func TestExecWatcher_BuildAuthBudgetExhausted_FinishErrorSkipsObserver(t *testing.T) {
	row := runningBuild("b1", "run-3", "build_auth_retry:3")
	repo := newFakeExecRepo(row)
	repo.finishErr = errors.New("db blip")
	failed := authFailedRun("run-3")
	obs := &countingBuildObserver{}
	w := NewExecWatcher(ocRuns(map[string]*gen.WorkflowRun{"run-3": failed}), repo, nil, 0).
		WithBuildRetrier(&fakeRetrier{newRun: "run-4"}, 3).
		WithBuildObserver(obs)

	w.reconcile(context.Background(), row, failed)
	if obs.n != 0 {
		t.Fatalf("Finish DB error must not OnBuildTerminal, got %d", obs.n)
	}
	if got := repo.get("b1"); got.Status != string(taskmeta.ExecRunning) {
		t.Fatalf("row must stay running after Finish error, got %q", got.Status)
	}
}
