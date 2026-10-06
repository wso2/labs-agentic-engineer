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

package migrate_test

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/migrate"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// The one-active-turn guard moved from per project to per (project, use case)
// so the Issues chat runs beside the spec chat. A database that booted before
// the move still carries the project-wide index, which would keep refusing the
// second chat's turn: the step creates the per-use-case guard and drops the
// old one, and a re-run (every boot) is a no-op.
func TestRunAgentTurns_ActiveGuardPerUseCase(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()

	// Put the table back in its pre-move shape: only the project-wide guard.
	if err := db.Exec(`DROP INDEX IF EXISTS ux_agent_turns_active_use_case`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS ux_agent_turns_active
		ON agent_turns (org_id, project_id)
		WHERE status = 'running'`).Error; err != nil {
		t.Fatal(err)
	}

	for run := 0; run < 2; run++ {
		if err := migrate.RunAgentTurns(ctx, db); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
	}

	if !hasIndex(t, db, "ux_agent_turns_active_use_case") {
		t.Error("the per-use-case guard is missing — nothing stops two running turns in one chat")
	}
	if hasIndex(t, db, "ux_agent_turns_active") {
		t.Error("the project-wide guard survived — an Issues turn would still block the spec chat")
	}
}
