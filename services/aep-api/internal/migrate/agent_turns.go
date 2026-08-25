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

// RunAgentTurns creates the agent_turns indexes AutoMigrate cannot express
// from the model: a partial (WHERE-clause) unique index, and a composite one
// with a descending column.
//
//  1. The one-active-turn-per-project guard: at most one running turn per
//     (org_id, project_id), across every use case. Turn start is INSERT ...
//     ON CONFLICT DO NOTHING against this index, so racing POSTs resolve to
//     exactly one admitted turn and the loser reads the active row for its
//     409 {activeTurnId}.
//
//  2. The newest-turn lookup behind the status poll's spec.agent (#562) and
//     the kickoff's idempotence guard. Both run `WHERE org_id = ? AND
//     project_id = ? ORDER BY created_at DESC LIMIT 1`, and the status one
//     runs every 5s per viewer while an agent works. The model's single-column
//     indexes cannot serve it and the partial unique above covers only running
//     rows, so without this Postgres scans the project's whole turn history and
//     sorts it — on a poll whose entire budget is "cheap enough for 5s".
//     Descending so the index order IS the query order, making it a one-row
//     read rather than a sort of the matched set.
//
// Idempotent: CREATE INDEX IF NOT EXISTS is a no-op on re-run, and the step
// no-ops entirely if the table is not present yet.
func RunAgentTurns(ctx context.Context, db *gorm.DB) error {
	if !hasTable(db, "agent_turns") {
		return nil
	}
	if err := db.WithContext(ctx).Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS ux_agent_turns_active
		ON agent_turns (org_id, project_id)
		WHERE status = 'running'`).Error; err != nil {
		return fmt.Errorf("agent_turns active-guard index: %w", err)
	}
	if err := db.WithContext(ctx).Exec(`
		CREATE INDEX IF NOT EXISTS ix_agent_turns_project_newest
		ON agent_turns (org_id, project_id, created_at DESC)`).Error; err != nil {
		return fmt.Errorf("agent_turns newest-turn index: %w", err)
	}
	return nil
}
