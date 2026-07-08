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
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/models"
)

// runningBuild seeds a running build row with a given run name + reason.
func runningBuild(id, runName, reason string) *models.Execution {
	return &models.Execution{
		ID: id, OrgID: "acme", ProjectID: "widgets", Repo: "acme/widgets", IssueNumber: 7,
		Kind: string(taskmeta.KindBuild), Status: string(taskmeta.ExecRunning),
		Component: "order-service", CommitSHA: "deadbeef", RunName: runName, Reason: reason,
	}
}

// ocRuns serves a fixed WorkflowRun for any GetWorkflowRun by run name.
func ocRuns(byRun map[string]*models.WorkflowRun) *ocmocks.ComponentClientMock {
	return &ocmocks.ComponentClientMock{
		GetWorkflowRunFunc: func(_ context.Context, _, runName string) (*models.WorkflowRun, error) {
			return byRun[runName], nil
		},
	}
}

func authFailedRun(name string) *models.WorkflowRun {
	return &models.WorkflowRun{
		Name: name, Completed: true, Status: openchoreo.ReasonWorkflowFailed,
		Tasks: []models.WorkflowRunTask{{Name: "checkout-source", Phase: "Failed", Message: "fatal: Authentication failed for 'https://github.com/acme/widgets'"}},
	}
}

func plainFailedRun(name string) *models.WorkflowRun {
	return &models.WorkflowRun{
		Name: name, Completed: true, Status: openchoreo.ReasonWorkflowFailed,
		Tasks: []models.WorkflowRunTask{{Name: "build", Phase: "Failed", Message: "compilation error"}},
	}
}

func TestExecWatcher_BuildGitAuthFailure_WithinBudget_ReMintsAndRetries(t *testing.T) {
	row := runningBuild("b1", "run-1", "") // first failure — no retry marker yet
	repo := newFakeExecRepo(row)
	retrier := &fakeRetrier{newRun: "run-2"}
	w := NewExecWatcher(ocRuns(map[string]*models.WorkflowRun{"run-1": authFailedRun("run-1")}), repo, nil, nil, 0).
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
	w := NewExecWatcher(ocRuns(map[string]*models.WorkflowRun{"run-3": authFailedRun("run-3")}), repo, nil, nil, 0).
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
	w := NewExecWatcher(ocRuns(map[string]*models.WorkflowRun{"run-1": authFailedRun("run-1")}), repo, nil, nil, 0).
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
	w := NewExecWatcher(ocRuns(map[string]*models.WorkflowRun{"run-1": plainFailedRun("run-1")}), repo, nil, nil, 0).
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

func TestExecWatcher_BuildSuccess_FinishesAndReevaluates(t *testing.T) {
	row := runningBuild("b1", "run-1", "")
	repo := newFakeExecRepo(row)
	reeval := &fakeReeval{}
	ok := &models.WorkflowRun{Name: "run-1", Completed: true, Status: openchoreo.ReasonWorkflowSucceeded}
	w := NewExecWatcher(ocRuns(map[string]*models.WorkflowRun{"run-1": ok}), repo, reeval, nil, 0)

	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := repo.get("b1"); got.Status != string(taskmeta.ExecSucceeded) {
		t.Errorf("build success must Finish succeeded, got %q", got.Status)
	}
	if reeval.count() != 1 {
		t.Errorf("build success must reevaluate the funnel once, got %d", reeval.count())
	}
}

// TestExecWatcher_SkipsProxyJobRuns proves the ExecWatcher ignores
// proxy-dispatched coding-agent Jobs (`ca-…`, owned by the JobWatcher) and polls
// only OpenChoreo WorkflowRuns (`wf-…`) — otherwise it spams "WorkflowRun not
// found" every tick for the Job rows.
func TestExecWatcher_SkipsProxyJobRuns(t *testing.T) {
	caRow := &models.Execution{ID: "j1", OrgID: "acme", Repo: "acme/widgets", IssueNumber: 7,
		Kind: string(taskmeta.KindCoding), Status: string(taskmeta.ExecRunning), RunName: "ca-abc12345-2601011200"}
	wfRow := &models.Execution{ID: "w1", OrgID: "acme", Repo: "acme/widgets", IssueNumber: 8,
		Kind: string(taskmeta.KindCoding), Status: string(taskmeta.ExecRunning), RunName: "wf-xyz"}
	repo := newFakeExecRepo(caRow, wfRow)

	var polled []string
	oc := &ocmocks.ComponentClientMock{
		GetWorkflowRunFunc: func(_ context.Context, _, runName string) (*models.WorkflowRun, error) {
			polled = append(polled, runName)
			return &models.WorkflowRun{Name: runName, Completed: false}, nil // still running
		},
	}
	w := NewExecWatcher(oc, repo, nil, nil, 0)

	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(polled) != 1 || polled[0] != "wf-xyz" {
		t.Fatalf("ExecWatcher must skip ca- rows and poll only wf-, polled=%v", polled)
	}
}

// fakeSignaler records every Signal call.
type fakeSignaler struct {
	mu    sync.Mutex
	calls []signalCall
}

type signalCall struct {
	workflowID, name string
}

func (f *fakeSignaler) Signal(_ context.Context, workflowID, name string, _ any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, signalCall{workflowID, name})
	return nil
}

func (f *fakeSignaler) names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.name
	}
	return out
}

// TestExecWatcher_BuildSuccess_StartsDeployPollInsteadOfSynthesizing covers
// the §R3.2 fix: build success signals BuildSucceeded + DeployStarted and
// records a "deploying" read-model checkpoint, but does NOT fire
// DeploySucceeded synthetically in the same tick — that now waits for
// pollDeploys to observe a real Ready ReleaseBinding.
func TestExecWatcher_BuildSuccess_StartsDeployPollInsteadOfSynthesizing(t *testing.T) {
	row := runningBuild("b1", "run-1", "")
	repo := newFakeExecRepo(row)
	signals := &fakeSignaler{}
	ok := &models.WorkflowRun{Name: "run-1", Completed: true, Status: openchoreo.ReasonWorkflowSucceeded}
	w := NewExecWatcher(ocRuns(map[string]*models.WorkflowRun{"run-1": ok}), repo, nil, nil, 0).
		WithTaskSignaler(signals)

	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	got := signals.names()
	if len(got) != 2 || got[0] != "BuildSucceeded" || got[1] != "DeployStarted" {
		t.Fatalf("signals = %v, want exactly [BuildSucceeded, DeployStarted] (no synthetic DeploySucceeded)", got)
	}

	wfID := "task:acme:widgets:order-service"
	rm, err := repo.GetByWorkflowID(context.Background(), wfID)
	if err != nil || rm == nil {
		t.Fatalf("GetByWorkflowID: row=%v err=%v", rm, err)
	}
	if rm.Status != readModelStatusDeploying {
		t.Errorf("read-model status = %q, want %q", rm.Status, readModelStatusDeploying)
	}
}

// TestExecWatcher_PollDeploys_ReadyFiresDeploySucceeded covers the real
// completion path: once the component's ReleaseBinding reports Ready, the
// next Sweep fires DeploySucceeded and marks the read-model row terminal.
func TestExecWatcher_PollDeploys_ReadyFiresDeploySucceeded(t *testing.T) {
	repo := newFakeExecRepo()
	signals := &fakeSignaler{}
	oc := &ocmocks.ComponentClientMock{
		IsComponentReadyFunc: func(context.Context, string, string, string) (bool, error) { return true, nil },
	}
	w := NewExecWatcher(oc, repo, nil, nil, 0).WithTaskSignaler(signals)

	wfID := "task:acme:widgets:order-service"
	if _, err := repo.UpsertReadModel(context.Background(), &models.Execution{
		WorkflowID: wfID, OrgID: "acme", ProjectID: "widgets", Component: "order-service",
		Kind: string(taskmeta.KindBuild), Status: readModelStatusDeploying,
	}); err != nil {
		t.Fatalf("seed UpsertReadModel: %v", err)
	}

	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := signals.names(); len(got) != 1 || got[0] != "DeploySucceeded" {
		t.Fatalf("signals = %v, want [DeploySucceeded]", got)
	}
	rm, err := repo.GetByWorkflowID(context.Background(), wfID)
	if err != nil || rm == nil || rm.Status != readModelStatusDeployed {
		t.Fatalf("read-model row not marked deployed: row=%v err=%v", rm, err)
	}
}

// TestExecWatcher_PollDeploys_NotReadyKeepsPolling covers the in-progress
// case: not-yet-ready leaves the row at "deploying" and fires no signal.
func TestExecWatcher_PollDeploys_NotReadyKeepsPolling(t *testing.T) {
	repo := newFakeExecRepo()
	signals := &fakeSignaler{}
	oc := &ocmocks.ComponentClientMock{
		IsComponentReadyFunc: func(context.Context, string, string, string) (bool, error) { return false, nil },
	}
	w := NewExecWatcher(oc, repo, nil, nil, 0).WithTaskSignaler(signals)

	wfID := "task:acme:widgets:order-service"
	if _, err := repo.UpsertReadModel(context.Background(), &models.Execution{
		WorkflowID: wfID, OrgID: "acme", ProjectID: "widgets", Component: "order-service",
		Kind: string(taskmeta.KindBuild), Status: readModelStatusDeploying,
	}); err != nil {
		t.Fatalf("seed UpsertReadModel: %v", err)
	}

	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := signals.names(); len(got) != 0 {
		t.Fatalf("signals = %v, want none (still deploying)", got)
	}
	rm, err := repo.GetByWorkflowID(context.Background(), wfID)
	if err != nil || rm == nil || rm.Status != readModelStatusDeploying {
		t.Fatalf("read-model row should stay deploying: row=%v err=%v", rm, err)
	}
}

// TestExecWatcher_PollDeploys_StaleGivesUpAndFiresDeployFailed covers the
// bound: a row stuck "deploying" past deployPollStaleAfter fires
// DeployFailed rather than polling forever.
func TestExecWatcher_PollDeploys_StaleGivesUpAndFiresDeployFailed(t *testing.T) {
	repo := newFakeExecRepo()
	signals := &fakeSignaler{}
	oc := &ocmocks.ComponentClientMock{
		IsComponentReadyFunc: func(context.Context, string, string, string) (bool, error) { return false, nil },
	}
	w := NewExecWatcher(oc, repo, nil, nil, 0).WithTaskSignaler(signals)

	wfID := "task:acme:widgets:order-service"
	stale, err := repo.UpsertReadModel(context.Background(), &models.Execution{
		WorkflowID: wfID, OrgID: "acme", ProjectID: "widgets", Component: "order-service",
		Kind: string(taskmeta.KindBuild), Status: readModelStatusDeploying,
	})
	if err != nil {
		t.Fatalf("seed UpsertReadModel: %v", err)
	}
	stale.CreatedAt = time.Now().Add(-2 * deployPollStaleAfter)
	repo.rows[wfID] = stale

	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := signals.names(); len(got) != 1 || got[0] != "DeployFailed" {
		t.Fatalf("signals = %v, want [DeployFailed]", got)
	}
	rm, err := repo.GetByWorkflowID(context.Background(), wfID)
	if err != nil || rm == nil || rm.Status != readModelStatusDeployFailed {
		t.Fatalf("read-model row should be marked deploy_failed: row=%v err=%v", rm, err)
	}
}

func TestExecWatcher_CodingFailure_FinishesFailed_SuccessRidesPRWebhook(t *testing.T) {
	// A ClusterWorkflow (`wf-…`) coding run — the ExecWatcher's domain; proxy
	// `ca-…` runs are the JobWatcher's and are skipped here.
	coding := &models.Execution{ID: "c1", OrgID: "acme", Repo: "acme/widgets", IssueNumber: 7,
		Kind: string(taskmeta.KindCoding), Status: string(taskmeta.ExecRunning), RunName: "wf-1"}
	repo := newFakeExecRepo(coding)
	failed := &models.WorkflowRun{Name: "wf-1", Completed: true, Status: openchoreo.ReasonWorkflowFailed}
	w := NewExecWatcher(ocRuns(map[string]*models.WorkflowRun{"wf-1": failed}), repo, nil, nil, 0)

	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := repo.get("c1"); got.Status != string(taskmeta.ExecFailed) {
		t.Errorf("failed coding run must Finish failed (no PR will open), got %q", got.Status)
	}
}
