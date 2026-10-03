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
	"strings"

	"gorm.io/gorm"
)

// phase24EngineColumns are the agent_turns columns only aep-api's in-process
// turn engine wrote (the guard's heartbeat, the D19 tag stamp, the fold's
// commit and conflict paths, the use case its conversations were keyed by).
var phase24EngineColumns = []string{"heartbeat_at", "spec_tag", "paths", "commit_sha", "no_changes", "use_case"}

// phase24Before is the bound under which a started_at is no real start: the
// engine left it NULL, or wrote Go's zero time (0001-01-01).
const phase24Before = "0002-01-01"

// RunPhase24AgentTurnsLedger makes agent_turns the finished-turn ledger
// (07 §12). Turns run in the org's AE Studio pod now, which records each one
// once it has finished; aep-api's in-process engine and its conversation store
// are gone. In one transaction:
//
//  1. The engine's rows get what the ledger reads: a kind from their use case
//     (task-plan → plan; a `/start` turn, by flow or by the transcript line,
//     → kickoff; anything else → browser) and a start time from created_at.
//     Rows the ledger itself wrote already carry the pod's kind and start and
//     are left alone. The kind is backfilled BEFORE use_case is dropped.
//  2. Marketplace turns move from the engine's synthetic project
//     __marketplace_register__ to the empty project (""), where the ledger keeps them.
//  3. Running rows are deleted: nothing will ever finish them.
//  4. The one-active guard ux_agent_turns_active and the engine's columns go.
//  5. The primary key widens from id to (org_id, id): the pod chooses turn ids
//     (the kickoff's is deterministic), so another org's row with the same id
//     must never stand in for this org's record.
//  6. project_conversations is dropped.
//
// Idempotent: each part is guarded on the shape it changes, so a re-run (and a
// fresh schema, which AutoMigrate builds in the final shape) changes nothing.
func RunPhase24AgentTurnsLedger(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := phase24AgentTurns(tx); err != nil {
			return fmt.Errorf("phase24_agent_turns_ledger: %w", err)
		}
		if err := tx.Exec(`DROP TABLE IF EXISTS project_conversations`).Error; err != nil {
			return fmt.Errorf("phase24_agent_turns_ledger: drop project_conversations: %w", err)
		}
		return nil
	})
}

func phase24AgentTurns(tx *gorm.DB) error {
	if !hasTable(tx, "agent_turns") {
		return nil
	}
	engineRows := `(started_at IS NULL OR started_at < '` + phase24Before + `')`
	if hasColumn(tx, "agent_turns", "use_case") {
		if err := tx.Exec(`UPDATE agent_turns SET kind = CASE
				WHEN use_case = 'task-plan' THEN 'plan'
				WHEN flow = 'start' OR summary LIKE '/start%' THEN 'kickoff'
				ELSE 'browser' END
			WHERE ` + engineRows).Error; err != nil {
			return fmt.Errorf("backfill kind: %w", err)
		}
	}
	if err := tx.Exec(`UPDATE agent_turns SET started_at = created_at WHERE ` + engineRows).Error; err != nil {
		return fmt.Errorf("backfill started_at: %w", err)
	}
	if err := tx.Exec(`UPDATE agent_turns SET project_id = '' WHERE project_id = '__marketplace_register__'`).Error; err != nil {
		return fmt.Errorf("move marketplace turns: %w", err)
	}
	if err := tx.Exec(`DELETE FROM agent_turns WHERE status = 'running'`).Error; err != nil {
		return fmt.Errorf("delete running turns: %w", err)
	}
	if err := tx.Exec(`DROP INDEX IF EXISTS ux_agent_turns_active`).Error; err != nil {
		return fmt.Errorf("drop the active-turn guard: %w", err)
	}
	for _, col := range phase24EngineColumns {
		if err := tx.Exec(`ALTER TABLE agent_turns DROP COLUMN IF EXISTS ` + col).Error; err != nil {
			return fmt.Errorf("drop %s: %w", col, err)
		}
	}
	return phase24WidenKey(tx)
}

// phase24WidenKey moves the primary key to (org_id, id) unless it is there.
func phase24WidenKey(tx *gorm.DB) error {
	var key []struct {
		Name   string
		Column string
	}
	if err := tx.Raw(`
		SELECT c.conname AS name, a.attname AS "column"
		  FROM pg_constraint c
		  JOIN unnest(c.conkey) WITH ORDINALITY AS k(attnum, ord) ON TRUE
		  JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.attnum
		 WHERE c.conrelid = 'agent_turns'::regclass AND c.contype = 'p'
		 ORDER BY k.ord`).Scan(&key).Error; err != nil {
		return fmt.Errorf("read the primary key: %w", err)
	}
	cols := make([]string, 0, len(key))
	for _, k := range key {
		cols = append(cols, k.Column)
	}
	if strings.Join(cols, ",") == "org_id,id" {
		return nil
	}
	if len(key) > 0 {
		if err := tx.Exec(`ALTER TABLE agent_turns DROP CONSTRAINT "` + key[0].Name + `"`).Error; err != nil {
			return fmt.Errorf("drop the primary key %s: %w", key[0].Name, err)
		}
	}
	if err := tx.Exec(`ALTER TABLE agent_turns ADD PRIMARY KEY (org_id, id)`).Error; err != nil {
		return fmt.Errorf("add the (org_id, id) primary key: %w", err)
	}
	return nil
}
