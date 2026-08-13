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

package migrate

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// coding_agent_logs.go — legacy CREATE TABLE for execution-keyed agent-log rows.
//
// SUPERSEDES phase3_coding_agent_logs (which keyed the table to component_tasks
// and guarded on that table existing). In the GitHub-native model a coding run
// is an Execution, and component_tasks is DROPPED (tasks_github_native) — so the
// legacy migration never creates the table. This creates the table keyed to the
// Execution id (delivery.CodingAgentLog.TaskID = executions.id — the column
// keeps the historic `task_id` name), FK-cascading on the execution. Nothing
// writes the table any more; GetByRun still serves pre-existing rows for old
// execution progress. Runs AFTER `executions` (the FK target) and
// `tasks_github_native` (which cascade-drops any legacy table).
func RunCodingAgentLogs(ctx context.Context, db *gorm.DB) error {
	stmt := `
		CREATE TABLE IF NOT EXISTS coding_agent_logs (
		  task_id      UUID         NOT NULL REFERENCES executions(id) ON DELETE CASCADE,
		  run_name     TEXT         NOT NULL,
		  final_phase  TEXT         NOT NULL,
		  captured_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
		  log_text     TEXT         NOT NULL,
		  size_bytes   BIGINT       NOT NULL,
		  PRIMARY KEY (task_id, run_name)
		);
		CREATE INDEX IF NOT EXISTS idx_coding_agent_logs_task_id ON coding_agent_logs(task_id);`
	if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
		return fmt.Errorf("coding_agent_logs: %w", err)
	}
	return nil
}
