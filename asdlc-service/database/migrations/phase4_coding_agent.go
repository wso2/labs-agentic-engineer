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

package migrations

import (
	"fmt"
	"log/slog"

	"gorm.io/gorm"
)

// RunPhase4CodingAgent adds the `last_coding_agent_run_name` column to
// component_tasks plus the partial indices that back the watchers'
// 10s-cadence sweep queries. Mirrors the existing LastBuildRunName column
// but tracks the per-task WorkflowRun of ClusterWorkflow
// `app-factory-coding-agent` (the new ephemeral-pod path that replaces
// the long-lived remote-worker).
//
// Both watcher queries are non-sargable on the empty-string predicate:
//
//	WHERE status = 'in_progress' AND last_coding_agent_run_name <> ''
//	WHERE status = 'building'    AND last_build_run_name        <> ''
//
// The partial indices below exclude the empty rows and order by
// last_event_at NULLS FIRST so the watcher's `ORDER BY` is index-driven.
//
// Idempotent — re-running is a no-op once the column / indices exist.
func RunPhase4CodingAgent(db *gorm.DB) error {
	const check = `SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema='public' AND table_name='component_tasks' AND column_name='last_coding_agent_run_name'
	)`
	var exists struct{ Exists bool }
	if err := db.Raw(check).Scan(&exists).Error; err != nil {
		return fmt.Errorf("phase4_coding_agent: detect column: %w", err)
	}
	if !exists.Exists {
		if err := db.Exec(`ALTER TABLE component_tasks ADD COLUMN last_coding_agent_run_name TEXT NOT NULL DEFAULT ''`).Error; err != nil {
			return fmt.Errorf("phase4_coding_agent: add column: %w", err)
		}
		slog.Info("phase4_coding_agent migration: added last_coding_agent_run_name column")
	}

	statements := []string{
		`CREATE INDEX IF NOT EXISTS idx_component_tasks_in_progress_run
			ON component_tasks (last_event_at NULLS FIRST)
			WHERE status = 'in_progress' AND last_coding_agent_run_name <> ''`,
		`CREATE INDEX IF NOT EXISTS idx_component_tasks_building_run
			ON component_tasks (last_event_at NULLS FIRST)
			WHERE status = 'building' AND last_build_run_name <> ''`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("phase4_coding_agent: create index: %w", err)
		}
	}
	return nil
}
