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

// RunProjectConversations creates the one-current-thread-per-scope partial
// unique index on project_conversations (#430): at most one current row per
// (org_id, project_id, use_case). Lazy create is INSERT ... ON CONFLICT DO
// NOTHING against this index, so teammates racing a project's first resolve
// converge on exactly one thread — the same admission pattern as
// ux_agent_turns_active and the milestone-run mutex. AutoMigrate creates the
// table from the model but cannot express a partial (WHERE-clause) index.
//
// Idempotent: CREATE UNIQUE INDEX IF NOT EXISTS is a no-op on re-run, and the
// step no-ops entirely if the table is not present yet.
func RunProjectConversations(ctx context.Context, db *gorm.DB) error {
	if !hasTable(db, "project_conversations") {
		return nil
	}
	if err := db.WithContext(ctx).Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS ux_project_conversations_current
		ON project_conversations (org_id, project_id, use_case)
		WHERE current`).Error; err != nil {
		return fmt.Errorf("project_conversations current-guard index: %w", err)
	}
	return nil
}
