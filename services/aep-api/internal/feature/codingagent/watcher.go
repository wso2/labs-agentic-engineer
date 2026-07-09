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
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/clients/clustergatewayproxy"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/models"
	"github.com/wso2/aep/aep-api/repositories"
)

// finalLogTailBytes caps the captured snapshot size (~3000 lines).
const finalLogTailBytes = 256 * 1024

// JobWatcher polls per-Execution coding-agent Jobs via cluster-gateway-proxy
// (the `ca-…` proxy dispatch path) and projects terminal phases onto the
// executions rows. It does NOT drive success transitions — a succeeded agent
// Job only means the pod exited zero; the PR (via the pull_request webhook) is
// the durable completion. It Finishes a coding Execution FAILED when the Job
// fails/vanishes, and captures the pod's final log into coding_agent_logs. State
// is written through the execution repository (one discipline).
type JobWatcher struct {
	db       *gorm.DB
	proxy    *clustergatewayproxy.Client
	execRows repositories.ExecutionRepository

	pollInterval time.Duration
	once         sync.Once
}

// NewJobWatcher constructs a watcher. db + proxy + execRows required.
func NewJobWatcher(db *gorm.DB, proxy *clustergatewayproxy.Client, execRows repositories.ExecutionRepository) *JobWatcher {
	if db == nil || proxy == nil || execRows == nil {
		panic("codingagent.JobWatcher: db + proxy + execRows are required")
	}
	return &JobWatcher{db: db, proxy: proxy, execRows: execRows, pollInterval: 30 * time.Second}
}

// Run blocks until ctx is canceled, ticking immediately then on pollInterval.
func (w *JobWatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	w.once.Do(func() { slog.Info("codingagent.JobWatcher: started", "interval", w.pollInterval) })
	w.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			slog.Info("codingagent.JobWatcher: stopping")
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *JobWatcher) tick(ctx context.Context) {
	// Claim only running, proxy-dispatched coding Executions (`ca-…` run names).
	active, err := w.execRows.ListActive(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "codingagent.JobWatcher: list active executions failed", "error", err)
		return
	}
	for i := range active {
		row := &active[i]
		if row.Kind != string(taskmeta.KindCoding) || row.Status != string(taskmeta.ExecRunning) {
			continue
		}
		if !isProxyJobRun(row.RunName) {
			continue // ClusterWorkflow runs (`wf-…`) are the ExecWatcher's job
		}
		w.checkOne(ctx, row)
	}
}

func (w *JobWatcher) checkOne(ctx context.Context, row *models.Execution) {
	ns, ok := w.resolveNS(ctx, row.OrgID)
	if !ok {
		slog.DebugContext(ctx, "codingagent.JobWatcher: NS resolve failed; skip", "execution", row.ID, "org", row.OrgID)
		return
	}
	status, err := w.proxy.GetJob(ctx, ns, row.RunName)
	if err != nil {
		if errors.Is(err, clustergatewayproxy.ErrNotFound) {
			w.finishFailed(ctx, row, "job_not_found_in_namespace")
			return
		}
		slog.WarnContext(ctx, "codingagent.JobWatcher: GetJob failed", "execution", row.ID, "ns", ns, "run", row.RunName, "error", err)
		return
	}
	switch {
	case status.Succeeded > 0:
		// Agent succeeded — the coding Execution ends via the PR webhook, not
		// here. Capture the log + clean up the per-run ExternalSecrets.
		w.captureFinalLog(ctx, row, ns, "Succeeded")
		w.cleanupPerRunExternalSecrets(ctx, row, ns)
	case status.Failed > 0:
		reason := "job_failed"
		for _, c := range status.Conditions {
			if c.Type == "Failed" && c.Status == "True" {
				reason = "job_failed:" + c.Reason
				break
			}
		}
		w.captureFinalLog(ctx, row, ns, "Failed")
		w.cleanupPerRunExternalSecrets(ctx, row, ns)
		w.finishFailed(ctx, row, reason)
	}
}

func (w *JobWatcher) finishFailed(ctx context.Context, row *models.Execution, reason string) {
	if _, err := w.execRows.Finish(ctx, row.ID, string(taskmeta.ExecFailed), reason); err != nil {
		slog.ErrorContext(ctx, "codingagent.JobWatcher: finish failed", "execution", row.ID, "reason", reason, "error", err)
		return
	}
	slog.InfoContext(ctx, "codingagent.JobWatcher: coding execution failed", "execution", row.ID, "reason", reason)
}

// cleanupPerRunExternalSecrets deletes the per-run ExternalSecrets the
// Dispatcher applied (anthropic + github + optional publisher). Best-effort.
func (w *JobWatcher) cleanupPerRunExternalSecrets(ctx context.Context, row *models.Execution, ns string) {
	if row.RunName == "" {
		return
	}
	for _, name := range []string{row.RunName + "-anthropic-es", row.RunName + "-github-es", row.RunName + "-publisher-es"} {
		if err := w.proxy.DeleteExternalSecret(ctx, ns, name); err != nil {
			slog.WarnContext(ctx, "codingagent.JobWatcher: cleanup ExternalSecret failed", "execution", row.ID, "ns", ns, "es", name, "error", err)
		}
	}
}

// captureFinalLog reads the agent pod's stdout/stderr once and persists it to
// coding_agent_logs, keyed by the execution id. Idempotent on (task_id,run_name).
func (w *JobWatcher) captureFinalLog(ctx context.Context, row *models.Execution, ns, phase string) {
	var exists int64
	if err := w.db.WithContext(ctx).Model(&models.CodingAgentLog{}).
		Where("task_id = ? AND run_name = ?", row.ID, row.RunName).
		Count(&exists).Error; err == nil && exists > 0 {
		return
	}
	podName, err := w.proxy.GetJobPodName(ctx, ns, row.RunName)
	if err != nil {
		slog.WarnContext(ctx, "codingagent.JobWatcher: captureFinalLog: pod lookup failed", "execution", row.ID, "ns", ns, "run", row.RunName, "error", err)
		return
	}
	body, err := w.proxy.TailPodLog(ctx, ns, podName, clustergatewayproxy.PodLogOptions{Timestamps: true, LimitBytes: finalLogTailBytes})
	if err != nil {
		slog.WarnContext(ctx, "codingagent.JobWatcher: captureFinalLog: tail failed", "execution", row.ID, "ns", ns, "pod", podName, "error", err)
		return
	}
	execUUID, err := uuid.Parse(row.ID)
	if err != nil {
		slog.WarnContext(ctx, "codingagent.JobWatcher: captureFinalLog: invalid execution id", "execution", row.ID, "error", err)
		return
	}
	if err := w.db.WithContext(ctx).Create(&models.CodingAgentLog{
		TaskID:     execUUID,
		RunName:    row.RunName,
		FinalPhase: phase,
		LogText:    string(body),
		SizeBytes:  int64(len(body)),
	}).Error; err != nil {
		slog.WarnContext(ctx, "codingagent.JobWatcher: captureFinalLog: persist failed", "execution", row.ID, "run", row.RunName, "error", err)
		return
	}
	slog.InfoContext(ctx, "codingagent.JobWatcher: captured final log", "execution", row.ID, "run", row.RunName, "phase", phase, "bytes", len(body))
}

func (w *JobWatcher) resolveNS(ctx context.Context, orgID string) (string, bool) {
	return resolveRemoteWorkerNS(ctx, w.db, orgID)
}
