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
	"log/slog"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/models"
	"github.com/wso2/aep/aep-api/repositories"
	contract "github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"
)

// BuildRetrier re-mints the build clone credential and re-triggers a build at
// the row's pinned commit under a fresh run name, returning that name. The
// coding executor satisfies it — wired in only when build-secret staging is
// configured, so the retry path is inert on public-repo-only deployments.
type BuildRetrier interface {
	RetryAuthFailedBuild(ctx context.Context, row *models.Execution) (newRunName string, err error)
}

// TaskSignaler sends task lifecycle events to Temporal.
type TaskSignaler interface {
	Signal(ctx context.Context, workflowID, signalName string, arg any) error
}

// ExecWatcher reconciles running execution rows against their OpenChoreo
// WorkflowRun status — the complement to the GitHub webhook path. It writes
// terminal outcomes through the execution repository (one discipline):
//
//   - a coding run's SUCCESS is not acted on here — the coding Execution ends
//     via the pull_request-opened webhook (the PR is the real completion); only
//     a terminal-FAILED coding run is Finished failed;
//   - a build run's success/failure IS terminal here; on success it also
//     re-evaluates the funnel so Tasks whose dependency just deployed dispatch.
//     A build that FAILED at git-clone-auth within budget is re-minted and
//     re-triggered (the retrier) rather than Finished — the legacy build
//     watcher's auth-retry loop, re-keyed to the execution row's reason (§7).
type ExecWatcher struct {
	oc        openchoreo.ComponentClient
	execRows  repositories.ExecutionRepository
	reeval    Reevaluator
	asService func(ctx context.Context) context.Context
	tick      time.Duration

	// buildRetrier + authBudget bound the git-clone-auth build retry. nil retrier
	// → a git-auth build failure is Finished failed like any other failure.
	buildRetrier BuildRetrier
	authBudget   int

	// deployObserver is notified when a component deploys (build success), so the
	// provisioning feature can grant pending cross-project access (nil → skipped).
	deployObserver DeployObserver

	signals TaskSignaler
}

// NewExecWatcher wires the watcher. asService may be nil (tests); tick defaults
// to 10s.
func NewExecWatcher(oc openchoreo.ComponentClient, execRows repositories.ExecutionRepository, reeval Reevaluator, asService func(ctx context.Context) context.Context, tick time.Duration) *ExecWatcher {
	if tick <= 0 {
		tick = 10 * time.Second
	}
	return &ExecWatcher{oc: oc, execRows: execRows, reeval: reeval, asService: asService, tick: tick}
}

// WithBuildRetrier enables the git-clone-auth build retry loop. budget ≤0 uses
// the default. Returns the receiver for chained construction.
func (w *ExecWatcher) WithBuildRetrier(retrier BuildRetrier, budget int) *ExecWatcher {
	w.buildRetrier = retrier
	if budget <= 0 {
		budget = defaultBuildAuthRetryBudget
	}
	w.authBudget = budget
	return w
}

// WithDeployObserver enables the deploy-cascade notification on build success
// (nil → skipped). Returns the receiver for chained construction.
func (w *ExecWatcher) WithDeployObserver(o DeployObserver) *ExecWatcher {
	w.deployObserver = o
	return w
}

// WithTaskSignaler enables watcher -> TaskLifecycleWorkflow signaling.
func (w *ExecWatcher) WithTaskSignaler(signals TaskSignaler) *ExecWatcher {
	w.signals = signals
	return w
}

// Run sweeps on its interval until ctx is canceled.
func (w *ExecWatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(w.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.Sweep(ctx); err != nil {
				slog.WarnContext(ctx, "exec watcher sweep failed", "error", err)
			}
		}
	}
}

// Sweep runs one reconciliation pass over running executions. Exported for
// tests. pollDeploys runs FIRST, over rows a PRIOR tick's build-success
// started — a row this SAME tick's reconcile starts is deliberately left for
// the next tick, so a fresh "deploying" checkpoint is never polled before the
// component has had any chance to materialize.
func (w *ExecWatcher) Sweep(ctx context.Context) error {
	if w.asService != nil {
		ctx = w.asService(ctx)
	}
	w.pollDeploys(ctx)
	active, err := w.execRows.ListActive(ctx)
	if err != nil {
		return err
	}
	for i := range active {
		row := &active[i]
		if row.Status != string(taskmeta.ExecRunning) || row.RunName == "" {
			continue
		}
		// Proxy-dispatched coding-agent Jobs (`ca-…`) are K8s Jobs owned by the
		// JobWatcher, NOT OpenChoreo WorkflowRuns. Skip them or GetWorkflowRun
		// spams "WorkflowRun not found" every tick until the row terminates.
		if isProxyJobRun(row.RunName) {
			continue
		}
		run, gerr := w.oc.GetWorkflowRun(ctx, row.OrgID, row.RunName)
		if gerr != nil {
			slog.WarnContext(ctx, "exec watcher: get workflow run failed", "run", row.RunName, "error", gerr)
			continue
		}
		if run == nil || !run.Completed {
			continue // still running
		}
		w.reconcile(ctx, row, run)
	}
	return nil
}

func (w *ExecWatcher) reconcile(ctx context.Context, row *models.Execution, run *models.WorkflowRun) {
	succeeded := run.Status == openchoreo.ReasonWorkflowSucceeded
	switch row.Kind {
	case string(taskmeta.KindCoding):
		if !succeeded {
			// A failed coding run — the PR will never open; Finish failed.
			if _, err := w.execRows.Finish(ctx, row.ID, string(taskmeta.ExecFailed), workflowReason(run)); err != nil {
				slog.WarnContext(ctx, "exec watcher: finish coding failed", "execution", row.ID, "error", err)
			}
			w.signal(ctx, row, contract.SignalCodingAgentFailed)
		}
		// A succeeded coding run rides the pull_request-opened webhook — no action.
	case string(taskmeta.KindBuild):
		if succeeded {
			if _, err := w.execRows.Finish(ctx, row.ID, string(taskmeta.ExecSucceeded), ""); err != nil {
				slog.WarnContext(ctx, "exec watcher: finish build succeeded", "execution", row.ID, "error", err)
				return
			}
			if w.reeval != nil {
				// A dependency just deployed — release any Task queued on it (§5).
				if rerr := w.reeval.Reevaluate(ctx); rerr != nil {
					slog.WarnContext(ctx, "exec watcher: reevaluate after build success failed", "error", rerr)
				}
			}
			if w.deployObserver != nil {
				// The component deployed — grant any pending cross-project access
				// request targeting it (best-effort; the grant read no-ops otherwise).
				if derr := w.deployObserver.OnComponentDeployed(ctx, row.OrgID, row.ProjectID, row.Component); derr != nil {
					slog.WarnContext(ctx, "exec watcher: deploy observer failed", "component", row.Component, "error", derr)
				}
			}
			w.signal(ctx, row, contract.SignalBuildSucceeded)
			w.signal(ctx, row, contract.SignalDeployStarted)
			// Deploy completion is NOT synthesized here (§R3.2 fix) — record a
			// "deploying" read-model checkpoint; pollDeploys (called from Sweep)
			// polls the component's real ReleaseBinding Ready condition on
			// subsequent ticks and only then fires DeploySucceeded/DeployFailed.
			w.startDeployPoll(ctx, row)
			return
		}
		w.reconcileBuildFailure(ctx, row, run)
	}
}

// deployPollStaleAfter bounds how long a "deploying" read-model row waits for
// the component's ReleaseBinding to report Ready before pollDeploys gives up
// and fires DeployFailed — mirrors provisioning.ResourceWatcher's
// resourceWatchStaleAfter bound for the analogous OC-materialization wait.
const deployPollStaleAfter = 30 * time.Minute

// startDeployPoll records the "deploying" read-model checkpoint pollDeploys
// consumes. Best-effort: a write failure is logged, never propagated — the
// build success itself already succeeded.
func (w *ExecWatcher) startDeployPoll(ctx context.Context, row *models.Execution) {
	if row.Component == "" {
		return
	}
	wfID := contract.TaskWorkflowID(row.OrgID, row.ProjectID, row.Component)
	if _, err := w.execRows.UpsertReadModel(ctx, &models.Execution{
		WorkflowID: wfID,
		OrgID:      row.OrgID,
		ProjectID:  row.ProjectID,
		Repo:       row.Repo,
		Kind:       string(taskmeta.KindBuild),
		Status:     readModelStatusDeploying,
		Component:  row.Component,
	}); err != nil {
		slog.WarnContext(ctx, "exec watcher: start deploy poll failed", "workflow", wfID, "error", err)
	}
}

// readModelStatusDeploying / readModelStatusDeployed are read-model-only
// status values (§R3.2) — never taskmeta.ExecutionStatus, since read-model
// rows track workflow-position checkpoints, not a dispatch attempt's own
// lifecycle.
const (
	readModelStatusDeploying    = "deploying"
	readModelStatusDeployed     = "deployed"
	readModelStatusDeployFailed = "deploy_failed"
)

// pollDeploys checks every pending "deploying" read-model row's component for
// a real Ready ReleaseBinding, firing DeploySucceeded on success or
// DeployFailed once deployPollStaleAfter elapses with no Ready condition.
func (w *ExecWatcher) pollDeploys(ctx context.Context) {
	rows, err := w.execRows.ListReadModelByStatus(ctx, readModelStatusDeploying)
	if err != nil {
		slog.WarnContext(ctx, "exec watcher: list deploying read-model rows failed", "error", err)
		return
	}
	for i := range rows {
		row := &rows[i]
		ready, rerr := w.oc.IsComponentReady(ctx, row.OrgID, row.ProjectID, row.Component)
		if rerr != nil {
			slog.WarnContext(ctx, "exec watcher: is component ready failed", "component", row.Component, "error", rerr)
			continue
		}
		if ready {
			w.finishDeployPoll(ctx, row, readModelStatusDeployed, contract.SignalDeploySucceeded)
			continue
		}
		if time.Since(row.CreatedAt) > deployPollStaleAfter {
			slog.WarnContext(ctx, "exec watcher: deploy poll stale — giving up", "component", row.Component, "since", row.CreatedAt)
			w.finishDeployPoll(ctx, row, readModelStatusDeployFailed, contract.SignalDeployFailed)
		}
	}
}

func (w *ExecWatcher) finishDeployPoll(ctx context.Context, row *models.Execution, terminalStatus, signal string) {
	row.Status = terminalStatus
	if _, err := w.execRows.UpsertReadModel(ctx, row); err != nil {
		slog.WarnContext(ctx, "exec watcher: finish deploy poll failed", "workflow", row.WorkflowID, "error", err)
	}
	w.signal(ctx, row, signal)
}

// reconcileBuildFailure handles a completed-failed build run. A git-clone-auth
// failure within budget is re-minted + re-triggered (the row stays running, its
// attempt count threaded through the reason); budget exhaustion or a non-auth
// failure Finishes the row failed. Mirrors the legacy build watcher's §9.3 loop,
// re-keyed to the execution row's reason instead of a BuildAuthRetryCount column.
func (w *ExecWatcher) reconcileBuildFailure(ctx context.Context, row *models.Execution, run *models.WorkflowRun) {
	_, authFailure := classifyBuildRun(run)
	if !authFailure || w.buildRetrier == nil {
		if _, err := w.execRows.Finish(ctx, row.ID, string(taskmeta.ExecFailed), workflowReason(run)); err != nil {
			slog.WarnContext(ctx, "exec watcher: finish build failed", "execution", row.ID, "error", err)
		}
		w.signal(ctx, row, contract.SignalBuildFailed)
		return
	}
	attempt := parseBuildAuthRetryAttempt(row.Reason)
	if attempt >= w.authBudget {
		if _, err := w.execRows.Finish(ctx, row.ID, string(taskmeta.ExecFailed), buildAuthRetryExceededReason); err != nil {
			slog.WarnContext(ctx, "exec watcher: finish build auth-exhausted", "execution", row.ID, "error", err)
		} else {
			slog.WarnContext(ctx, "exec watcher: build git-auth retry budget exhausted", "execution", row.ID, "attempts", attempt, "budget", w.authBudget)
		}
		return
	}
	newRun, err := w.buildRetrier.RetryAuthFailedBuild(ctx, row)
	if err != nil {
		// A stuck re-mint (org disconnected / repo not in org) must still march
		// toward budget exhaustion, so record the attempt against the SAME run.
		slog.WarnContext(ctx, "exec watcher: build auth re-mint failed", "execution", row.ID, "attempt", attempt+1, "error", err)
		if _, nerr := w.execRows.NoteBuildRetry(ctx, row.ID, row.RunName, buildAuthRetryReason(attempt+1)); nerr != nil {
			slog.WarnContext(ctx, "exec watcher: note build retry (failed re-mint) failed", "execution", row.ID, "error", nerr)
		}
		return
	}
	if _, err := w.execRows.NoteBuildRetry(ctx, row.ID, newRun, buildAuthRetryReason(attempt+1)); err != nil {
		slog.WarnContext(ctx, "exec watcher: note build retry failed", "execution", row.ID, "newRun", newRun, "error", err)
		return
	}
	slog.InfoContext(ctx, "exec watcher: re-minted + re-triggered build after git-auth failure", "execution", row.ID, "newRun", newRun, "attempt", attempt+1)
}

func (w *ExecWatcher) signal(ctx context.Context, row *models.Execution, signal string) {
	if w.signals == nil || row == nil || row.Component == "" {
		return
	}
	wfID := contract.TaskWorkflowID(row.OrgID, row.ProjectID, row.Component)
	if err := w.signals.Signal(ctx, wfID, signal, nil); err != nil {
		slog.WarnContext(ctx, "exec watcher: task workflow signal failed", "workflow", wfID, "signal", signal, "error", err)
	}
}

// workflowReason returns a short reason string for a terminal WorkflowRun.
func workflowReason(run *models.WorkflowRun) string {
	if run.Status == openchoreo.ReasonWorkflowSucceeded {
		return ""
	}
	return run.Status
}
