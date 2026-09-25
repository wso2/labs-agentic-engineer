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

// The rename, on a reconstructed pre-phase17 state: the old table's rows move
// into org_agent_settings (which AutoMigrate created), an existing new row
// wins, the old table goes, and a re-run is a no-op.
func TestPhase17_MovesTheRowsAndDropsTheOldTable(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()

	for _, sql := range []string{
		`CREATE TABLE org_coding_agent_settings (
		   oc_org_id text PRIMARY KEY, runtime text NOT NULL, model text NOT NULL,
		   updated_by text NOT NULL, updated_at timestamptz NOT NULL)`,
		`INSERT INTO org_coding_agent_settings VALUES
		   ('acme', 'opencode', 'claude-haiku-4-5', 'ada', now()),
		   ('globex', 'claude-code', 'claude-haiku-4-5', 'bob', now())`,
		// Written by this build before the step ran: it must not be overwritten.
		`INSERT INTO org_agent_settings (oc_org_id, runtime, model, updated_by, updated_at)
		   VALUES ('globex', 'claude-code', 'claude-sonnet-5', 'carol', now())`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	for i := 0; i < 2; i++ {
		if err := migrate.RunPhase17OrgAgentSettings(ctx, db); err != nil {
			t.Fatalf("phase17 pass %d: %v", i+1, err)
		}
	}

	var exists bool
	if err := db.Raw(`SELECT to_regclass('public.org_coding_agent_settings') IS NOT NULL`).Scan(&exists).Error; err != nil {
		t.Fatalf("probe: %v", err)
	}
	if exists {
		t.Fatal("org_coding_agent_settings survived")
	}
	type row struct{ OcOrgID, Runtime, Model, UpdatedBy string }
	var rows []row
	if err := db.Raw(`SELECT oc_org_id, runtime, model, updated_by FROM org_agent_settings ORDER BY oc_org_id`).Scan(&rows).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	want := []row{
		{"acme", "opencode", "claude-haiku-4-5", "ada"},
		{"globex", "claude-code", "claude-sonnet-5", "carol"},
	}
	if len(rows) != len(want) || rows[0] != want[0] || rows[1] != want[1] {
		t.Fatalf("org_agent_settings = %+v, want %+v", rows, want)
	}
}
