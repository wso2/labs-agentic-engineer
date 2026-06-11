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

// Phase 3 — coding_agent_logs sidecar table.
//
// Captures the final pod-log tail for new-path (cluster-gateway-proxy)
// coding-agent dispatches when the Job hits terminal state. Read by
// `progress_service.GetAgentProgress` once the Job is past TTL so the
// console can still surface diagnostics (the legacy path used
// OpenChoreo's Observer + OpenSearch; the new dispatch NS
// (`wc-…-remote-worker`) doesn't match Observer's hardcoded
// `workflows-<…>` filter, so the BFF tails `pods/log` itself).
//
// Sidecar rather than a column on `component_tasks`: the parent table
// holds only small hot fields and is read on every list / status /
// dispatch path; appending a TEXT TOAST'd blob there would force
// detoasting whenever ORM SELECT-* paths run. Mirrors the existing
// `webhook_payloads` ↔ `webhook_deliveries` split.
package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

func RunPhase3CodingAgentLogs(ctx context.Context, db *gorm.DB) error {
	stmt := `DO $$ BEGIN
		   IF EXISTS (SELECT FROM information_schema.tables
		              WHERE table_schema='public' AND table_name='component_tasks') THEN
		     CREATE TABLE IF NOT EXISTS coding_agent_logs (
		       task_id      UUID         NOT NULL REFERENCES component_tasks(id) ON DELETE CASCADE,
		       run_name     TEXT         NOT NULL,
		       final_phase  TEXT         NOT NULL,
		       captured_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
		       log_text     TEXT         NOT NULL,
		       size_bytes   BIGINT       NOT NULL,
		       PRIMARY KEY (task_id, run_name)
		     );
		     CREATE INDEX IF NOT EXISTS idx_coding_agent_logs_task_id
		       ON coding_agent_logs(task_id);
		   END IF;
		 END $$`
	if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
		return fmt.Errorf("phase3_coding_agent_logs: %w", err)
	}
	return nil
}
