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

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/gen"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
)

// BuildRetrier re-mints the build clone credential and re-triggers a build at
// the row's pinned commit under a fresh run name, returning that name. The
// coding executor satisfies it — wired in only when build-secret staging is
// configured, so the retry path is inert on public-repo-only deployments.
type BuildRetrier interface {
	RetryAuthFailedBuild(ctx context.Context, row *delivery.Execution) (newRunName string, err error)
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
	execRows  delivery.ExecutionRepository
	asService func(ctx context.Context) context.Context
	tick      time.Duration

	// buildRetrier + authBudget bound the git-clone-auth build retry. nil retrier
	// → a git-auth build failure is Finished failed like any other failure.
	buildRetrier BuildRetrier
	authBudget   int

	// deployObserver is notified when a component deploys (build success), so the
	// provisioning feature can grant pending cross-project access (nil → skipped).
	deployObserver DeployObserver

	// buildObserver receives every BUILD terminal this watcher settles, through
	// the root port. It is how the event plane learns a build finished without
	// either package importing the other: this watcher stays here (it shares
	// the run-classification helpers with the executor next to it) and reports
	// outwards. Nil-safe.
	buildObserver delivery.BuildTerminalObserver

	// notifier wakes any attached task-log stream on a build/deploy terminal.
	// Nil-safe.
	notifier *delivery.TaskStreamHub
}

// WithBuildObserver wires the build-terminal observer (the event plane) so a
// component's build outcome reaches the milestone-run loop. Optional —
// nil-safe. Returns the receiver.
func (w *ExecWatcher) WithBuildObserver(o delivery.BuildTerminalObserver) *ExecWatcher {
	w.buildObserver = o
	return w
}

// WithTaskNotifier wires the task-log stream hub so build/deploy terminals wake
// attached console streams instantly. Optional — nil-safe.
func (w *ExecWatcher) WithTaskNotifier(h *delivery.TaskStreamHub) *ExecWatcher {
	w.notifier = h
	return w
}

// NewExecWatcher wires the watcher. asService may be nil (tests); tick defaults
// to 10s.
func NewExecWatcher(oc openchoreo.ComponentClient, execRows delivery.ExecutionRepository, asService func(ctx context.Context) context.Context, tick time.Duration) *ExecWatcher {
	if tick <= 0 {
		tick = 10 * time.Second
	}
	return &ExecWatcher{oc: oc, execRows: execRows, asService: asService, tick: tick}
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

// Sweep runs one reconciliation pass over running executions. Exported for tests.
func (w *ExecWatcher) Sweep(ctx context.Context) error {
	if w.asService != nil {
		ctx = w.asService(ctx)
	}
	active, err := w.execRows.ListActive(ctx)
	if err != nil {
		return err
	}
	for i := range active {
		row := &active[i]
		if row.Status != string(taskmeta.ExecRunning) || row.RunName == "" {
			continue
		}
		// Coding-agent Job Components (`ca-…`) are owned by the cycle
		// JobWatcher, not by WorkflowRuns. Any still-running KindCoding
		// execution row with a ca- run name is a pre-migration leftover: no
		// watcher will ever Finish it via GetWorkflowRun. Close it here so it
		// cannot sit `running` forever and hold the issue mutex.
		if isCodingAgentRun(row.RunName) {
			w.finishLegacyCodingExecution(ctx, row)
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

// legacyCodingExecutionReason is stamped onto KindCoding execution rows whose
// RunName is a coding-agent JobRef (`ca-…`). Those Jobs are owned by the cycle
// JobWatcher; the ExecWatcher must not poll them as WorkflowRuns, and must not
// leave them `running` forever after the migration.
const legacyCodingExecutionReason = "legacy-coding-execution-retired"

func (w *ExecWatcher) finishLegacyCodingExecution(ctx context.Context, row *delivery.Execution) {
	exec, err := w.execRows.Finish(ctx, row.ID, string(taskmeta.ExecFailed), legacyCodingExecutionReason)
	if err != nil {
		slog.WarnContext(ctx, "exec watcher: finish legacy coding execution failed",
			"execution", row.ID, "run", row.RunName, "error", err)
		return
	}
	if exec == nil {
		return // lost the race — another replica already finished
	}
	slog.InfoContext(ctx, "exec watcher: closed legacy ca- coding execution",
		"execution", row.ID, "run", row.RunName)
	w.notifier.Notify(row.Repo, row.IssueNumber)
}

func (w *ExecWatcher) reconcile(ctx context.Context, row *delivery.Execution, run *gen.WorkflowRun) {
	succeeded := run.Status == openchoreo.ReasonWorkflowSucceeded
	switch row.Kind {
	case string(taskmeta.KindCoding):
		if !succeeded {
			// A failed coding run — the PR will never open; Finish failed.
			exec, err := w.execRows.Finish(ctx, row.ID, string(taskmeta.ExecFailed), workflowReason(run))
			if err != nil {
				slog.WarnContext(ctx, "exec watcher: finish coding failed", "execution", row.ID, "error", err)
			} else if exec == nil {
				return // lost the race — another replica already finished
			}
			w.notifier.Notify(row.Repo, row.IssueNumber)
		}
		// A succeeded coding run rides the pull_request-opened webhook — no action.
	case string(taskmeta.KindBuild):
		if succeeded {
			exec, err := w.execRows.Finish(ctx, row.ID, string(taskmeta.ExecSucceeded), "")
			if err != nil {
				slog.WarnContext(ctx, "exec watcher: finish build succeeded", "execution", row.ID, "error", err)
				return
			}
			if exec == nil {
				return // lost the race — another replica already finished
			}
			if w.deployObserver != nil {
				// The component deployed — grant any pending cross-project access
				// request targeting it (best-effort; the grant read no-ops otherwise).
				if derr := w.deployObserver.OnComponentDeployed(ctx, row.OrgID, row.ProjectID, row.Component); derr != nil {
					slog.WarnContext(ctx, "exec watcher: deploy observer failed", "component", row.Component, "error", derr)
				}
			}
			w.notifier.Notify(row.Repo, row.IssueNumber)
			w.notifyBuildTerminal(ctx, row, true, "")
			return
		}
		w.reconcileBuildFailure(ctx, row, run)
	}
}

// reconcileBuildFailure handles a completed-failed build run. A git-clone-auth
// failure within budget is re-minted + re-triggered (the row stays running, its
// attempt count threaded through the reason); budget exhaustion or a non-auth
// failure Finishes the row failed. Mirrors the legacy build watcher's §9.3 loop,
// re-keyed to the execution row's reason instead of a BuildAuthRetryCount column.
func (w *ExecWatcher) reconcileBuildFailure(ctx context.Context, row *delivery.Execution, run *gen.WorkflowRun) {
	_, authFailure := classifyBuildRun(run)
	if !authFailure || w.buildRetrier == nil {
		exec, err := w.execRows.Finish(ctx, row.ID, string(taskmeta.ExecFailed), workflowReason(run))
		if err != nil {
			// Notify is cheap/idempotent; do NOT notifyBuildTerminal — the row
			// stays running and the next tick retries Finish. Reporting terminal
			// on a DB blip spends budget / mints a fix issue prematurely.
			slog.WarnContext(ctx, "exec watcher: finish build failed", "execution", row.ID, "error", err)
			w.notifier.Notify(row.Repo, row.IssueNumber)
			return
		}
		if exec == nil {
			return // lost the race — another replica already finished
		}
		w.notifier.Notify(row.Repo, row.IssueNumber)
		w.notifyBuildTerminal(ctx, row, false, workflowReason(run))
		return
	}
	attempt := parseBuildAuthRetryAttempt(row.Reason)
	if attempt >= w.authBudget {
		exec, err := w.execRows.Finish(ctx, row.ID, string(taskmeta.ExecFailed), buildAuthRetryExceededReason)
		if err != nil {
			slog.WarnContext(ctx, "exec watcher: finish build auth-exhausted", "execution", row.ID, "error", err)
			w.notifier.Notify(row.Repo, row.IssueNumber)
			return
		}
		if exec == nil {
			return // lost the race — another replica already finished
		}
		slog.WarnContext(ctx, "exec watcher: build git-auth retry budget exhausted", "execution", row.ID, "attempts", attempt, "budget", w.authBudget)
		w.notifier.Notify(row.Repo, row.IssueNumber)
		w.notifyBuildTerminal(ctx, row, false, buildAuthRetryExceededReason)
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

// notifyBuildTerminal reports a settled BUILD to the event plane through the
// root port. Best-effort: the observer's job is to advance a milestone run,
// and a run that misses one terminal re-derives from OpenChoreo at its next
// cycle boundary — whereas an error propagated here would abort the rest of
// the watcher's sweep over unrelated executions.
//
// Only builds are reported. A coding terminal is the runner's business and
// reaches the loop as a pull request.
func (w *ExecWatcher) notifyBuildTerminal(ctx context.Context, row *delivery.Execution, succeeded bool, reason string) {
	if w.buildObserver == nil {
		return
	}
	err := w.buildObserver.OnBuildTerminal(ctx, delivery.BuildTerminal{
		OrgID:     row.OrgID,
		ProjectID: row.ProjectID,
		Component: row.Component,
		CommitSHA: row.CommitSHA,
		RunName:   row.RunName,
		Succeeded: succeeded,
		Reason:    reason,
	})
	if err != nil {
		slog.WarnContext(ctx, "exec watcher: build-terminal observer failed",
			"execution", row.ID, "component", row.Component, "error", err)
	}
}

// workflowReason returns a short reason string for a terminal WorkflowRun.
func workflowReason(run *gen.WorkflowRun) string {
	if run.Status == openchoreo.ReasonWorkflowSucceeded {
		return ""
	}
	return run.Status
}
