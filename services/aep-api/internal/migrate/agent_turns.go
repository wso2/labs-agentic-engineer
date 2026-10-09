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

// RunAgentTurns creates the agent_turns index AutoMigrate cannot express
// from the model: a composite one with a descending column.
//
// The newest-turn lookup behind the status poll's spec.agent (#562), the
// kickoff's idempotence guard and the build gate's design baseline runs
// `WHERE org_id = ? AND project_id = ? ORDER BY created_at DESC LIMIT 1`.
// The model's single-column indexes cannot serve it, so without this Postgres
// scans the project's whole turn history and sorts it on every status poll.
// Descending so the index order IS the query order, making it a one-row read
// rather than a sort of the matched set.
//
// The in-process engine's one-active-turn guard (ux_agent_turns_active) is
// no longer created here: turns run in the org's AE Studio pod, and phase27
// drops the index.
//
// Idempotent: CREATE INDEX IF NOT EXISTS is a no-op on re-run, and the step
// no-ops entirely if the table is not present yet.
func RunAgentTurns(ctx context.Context, db *gorm.DB) error {
	if !hasTable(db, "agent_turns") {
		return nil
	}
	if err := db.WithContext(ctx).Exec(`
		CREATE INDEX IF NOT EXISTS ix_agent_turns_project_newest
		ON agent_turns (org_id, project_id, created_at DESC)`).Error; err != nil {
		return fmt.Errorf("agent_turns newest-turn index: %w", err)
	}
	return nil
}
