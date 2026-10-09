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

// The step adds startup_clock_at on its own — on a table built before it (the
// pre-migration shape, rebuilt by dropping it) — as a nullable timestamp; an
// existing row reads as "no clock yet" (its deadline is the apply cap). A
// second run changes nothing.
func TestPhase31RunCycleStartupClock_AddsTheColumnIdempotently(t *testing.T) {
	db := dbtest.New(t)
	bootMigrate(t, db)
	ctx := context.Background()

	if err := db.Exec(`ALTER TABLE run_cycles DROP COLUMN IF EXISTS startup_clock_at`).Error; err != nil {
		t.Fatalf("rebuild the pre-migration shape: %v", err)
	}
	if err := db.Exec(`INSERT INTO run_cycles (org_id, project_id, run_id, kind) VALUES ('acme', 'shop', 'run-0', 'coding')`).Error; err != nil {
		t.Fatalf("seed a pre-migration row: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := migrate.RunPhase31RunCycleStartupClock(ctx, db); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	dropIdleConnections(t, db)

	var col struct {
		DataType   string
		IsNullable string
	}
	if err := db.Raw(`SELECT data_type, is_nullable FROM information_schema.columns
		WHERE table_name = 'run_cycles' AND column_name = 'startup_clock_at'`).Scan(&col).Error; err != nil {
		t.Fatalf("read startup_clock_at: %v", err)
	}
	if col.DataType != "timestamp with time zone" || col.IsNullable != "YES" {
		t.Fatalf("startup_clock_at = %+v, want a nullable timestamptz", col)
	}
	var clocked int64
	if err := db.Raw(`SELECT count(*) FROM run_cycles WHERE startup_clock_at IS NOT NULL`).Scan(&clocked).Error; err != nil {
		t.Fatal(err)
	}
	if clocked != 0 {
		t.Fatalf("%d existing rows read as clocked; none should", clocked)
	}
}
