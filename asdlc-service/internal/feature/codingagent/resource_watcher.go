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

	"gorm.io/gorm"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// ResourceWatcher polls OpenChoreo for the status of in-flight resource-provisioning
// tasks. When the backing ResourceReleaseBinding reaches Ready=True, the task is
// transitioned to `deployed` and the cascade redispatch fires so any gated component
// tasks are unblocked.
//
// Mirrors BuildWatcher's structure (ticker Run, sweep with FOR UPDATE SKIP LOCKED,
// asServiceIdentity) — the template is codingagent/build_watcher.go.
//
// A real database (postgres-cnpg, Redis, …) can take several minutes to stand up.
// The watcher MUST NOT block the provision request path on readiness. It polls at
// the same 10s cadence as the build watcher and uses a bounded-age guard (10 min)
// to log + leave a stuck task rather than failing it spuriously.
//
// Multi-replica safe: FOR UPDATE SKIP LOCKED prevents two BFF replicas from
// double-processing the same task.
type ResourceWatcher struct {
	db                *gorm.DB
	rc                openchoreo.ResourceClient
	redispatch        OnHoldDispatcher
	asServiceIdentity func(ctx context.Context) context.Context
	tick              time.Duration
	staleAfter        time.Duration
}

// NewResourceWatcher constructs a ResourceWatcher.
// rc: the OC resource client (GetBinding); redispatch: the cascade adapter
// (same signature as OnHoldDispatcher); asServiceIdentity: ADR-0005 dual-mode
// OC auth (async OC-reading paths run under service-identity).
func NewResourceWatcher(
	db *gorm.DB,
	rc openchoreo.ResourceClient,
	redispatch OnHoldDispatcher,
	asServiceIdentity func(ctx context.Context) context.Context,
) *ResourceWatcher {
	return &ResourceWatcher{
		db:                db,
		rc:                rc,
		redispatch:        redispatch,
		asServiceIdentity: asServiceIdentity,
		tick:              10 * time.Second,
		staleAfter:        10 * time.Minute,
	}
}

// Run blocks until ctx is cancelled, sweeping at the configured cadence.
// Spawned as a goroutine from main beside buildWatcher.Run and onHoldWatcher.Run.
func (w *ResourceWatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(w.tick)
	defer ticker.Stop()
	slog.InfoContext(ctx, "resource watcher started", "tick", w.tick, "staleAfter", w.staleAfter)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.sweep(ctx)
		}
	}
}

func (w *ResourceWatcher) sweep(ctx context.Context) {
	if w.asServiceIdentity != nil {
		ctx = w.asServiceIdentity(ctx)
	}

	// Acquire a batch of `building` resource-provisioning tasks under
	// FOR UPDATE SKIP LOCKED so concurrent BFF replicas split the work.
	var batch []models.ComponentTask
	err := w.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Raw(`
			SELECT * FROM component_tasks
			WHERE type = ? AND status = ?
			ORDER BY last_event_at NULLS FIRST
			LIMIT 50
			FOR UPDATE SKIP LOCKED
		`, models.TaskTypeResourceProvisioning, string(models.TaskStatusBuilding)).
			Scan(&batch).Error
	})
	if err != nil {
		slog.ErrorContext(ctx, "resource watcher: select batch failed", "error", err)
		return
	}
	if len(batch) == 0 {
		return
	}

	for i := range batch {
		t := &batch[i]
		w.handleTask(ctx, t)
	}
}

func (w *ResourceWatcher) handleTask(ctx context.Context, t *models.ComponentTask) {
	// Bounded-age guard: a stuck provision logs + leaves rather than failing
	// spuriously. A real DB should not take more than staleAfter to reach Ready.
	if t.LastEventAt != nil && time.Since(*t.LastEventAt) > w.staleAfter {
		slog.WarnContext(ctx, "resource watcher: task stale, skipping",
			"task", t.ID, "resource", t.ResourceName,
			"age", time.Since(*t.LastEventAt).Round(time.Second))
		return
	}

	// Derive the binding name (mirrors BuildPlatformBinding: project-resource-env).
	// The single-env assumption (dispatch_service.go:807) uses "development".
	bindingName := t.ProjectID + "-" + t.ResourceName + "-development"

	binding, err := w.rc.GetBinding(ctx, t.OrgID, bindingName)
	if err != nil {
		// Transient — try again on the next tick.
		slog.WarnContext(ctx, "resource watcher: GetBinding failed",
			"task", t.ID, "binding", bindingName, "error", err)
		return
	}
	// GetBinding returns (nil, nil) on 404 — the binding may not exist yet.
	// Treat a nil binding as not-ready; leave for the next tick.

	done, failed := classifyBinding(binding)
	switch {
	case done:
		if err := w.db.WithContext(ctx).Model(&models.ComponentTask{}).
			Where("id = ?", t.ID).
			Update("status", string(models.TaskStatusDeployed)).Error; err != nil {
			slog.ErrorContext(ctx, "resource watcher: mark deployed failed",
				"task", t.ID, "error", err)
			return
		}
		slog.InfoContext(ctx, "resource watcher: resource ready → deployed",
			"task", t.ID, "resource", t.ResourceName)
		// Cascade: unblock component tasks gated on this resource.
		if w.redispatch != nil {
			count, rerr := w.redispatch(ctx, t.OrgID, t.ProjectID)
			if rerr != nil {
				slog.WarnContext(ctx, "resource watcher: redispatch failed",
					"task", t.ID, "org", t.OrgID, "project", t.ProjectID, "error", rerr)
			} else if count > 0 {
				slog.InfoContext(ctx, "resource watcher: cascaded dispatch after resource ready",
					"task", t.ID, "org", t.OrgID, "project", t.ProjectID, "dispatched", count)
			}
		}
	case failed:
		if err := w.db.WithContext(ctx).Model(&models.ComponentTask{}).
			Where("id = ?", t.ID).
			Update("status", string(models.TaskStatusFailed)).Error; err != nil {
			slog.ErrorContext(ctx, "resource watcher: mark failed failed",
				"task", t.ID, "error", err)
			return
		}
		slog.WarnContext(ctx, "resource watcher: binding in terminal failed state",
			"task", t.ID, "resource", t.ResourceName)
	default:
		// Still pending / unknown — leave for next tick.
	}
}

// classifyBinding maps a ResourceReleaseBinding to a (done, failed) pair.
// Pure function — no side effects, directly unit-testable.
//
//   - Ready=True  → (true, false)  — provisioning complete, cascade
//   - Terminal False condition (non-progressing reason) → (false, true)  — hard failure
//   - Pending / Unknown / nil → (false, false) — leave for next tick
func classifyBinding(b *openchoreo.ResourceReleaseBinding) (done bool, failed bool) {
	if b == nil {
		return false, false
	}
	if b.IsReady() {
		return true, false
	}
	if b.Status == nil {
		return false, false
	}
	// A False condition with a non-progressing reason is terminal.
	// "Progressing" reasons (e.g. "Creating", "Initializing", "") mean we should wait.
	for _, c := range b.Status.Conditions {
		if c.Status != "False" {
			continue
		}
		if isTerminalReason(c.Reason) {
			return false, true
		}
	}
	return false, false
}

// progressingReasons are non-terminal False reasons — the controller is still
// working. Any False condition with a reason NOT in this set is considered terminal.
var progressingReasons = map[string]bool{
	"":             true, // empty reason → still initializing
	"Creating":     true,
	"Initializing": true,
	"Progressing":  true,
	"Pending":      true,
	"Waiting":      true,
}

func isTerminalReason(reason string) bool {
	return !progressingReasons[reason]
}
