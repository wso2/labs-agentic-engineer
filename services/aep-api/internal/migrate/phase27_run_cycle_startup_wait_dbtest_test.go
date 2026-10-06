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

// The step adds both startup-wait columns on its own — on a table built
// before them (the pre-migration shape, rebuilt by dropping them) — with the
// shape the model reads: reason text NOT NULL DEFAULT ”, since a nullable
// timestamp; an existing row reads as "no wait". A second run changes nothing.
func TestPhase27RunCycleStartupWait_AddsTheColumnsIdempotently(t *testing.T) {
	db := dbtest.New(t)
	bootMigrate(t, db)
	ctx := context.Background()

	if err := db.Exec(`ALTER TABLE run_cycles DROP COLUMN IF EXISTS startup_wait_reason, DROP COLUMN IF EXISTS startup_wait_since`).Error; err != nil {
		t.Fatalf("rebuild the pre-migration shape: %v", err)
	}
	if err := db.Exec(`INSERT INTO run_cycles (org_id, project_id, run_id, kind) VALUES ('acme', 'shop', 'run-0', 'coding')`).Error; err != nil {
		t.Fatalf("seed a pre-migration row: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := migrate.RunPhase27RunCycleStartupWait(ctx, db); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	dropIdleConnections(t, db)

	type column struct {
		DataType   string
		IsNullable string
		Default    *string
	}
	read := func(name string) column {
		t.Helper()
		var c column
		if err := db.Raw(`SELECT data_type, is_nullable, column_default AS default
			FROM information_schema.columns WHERE table_name = 'run_cycles' AND column_name = ?`, name).Scan(&c).Error; err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return c
	}
	if c := read("startup_wait_reason"); c.DataType != "text" || c.IsNullable != "NO" || c.Default == nil || *c.Default != "''::text" {
		t.Fatalf("startup_wait_reason = %+v, want text NOT NULL DEFAULT ''", c)
	}
	if c := read("startup_wait_since"); c.DataType != "timestamp with time zone" || c.IsNullable != "YES" {
		t.Fatalf("startup_wait_since = %+v, want a nullable timestamptz", c)
	}
	var waiting int64
	if err := db.Raw(`SELECT count(*) FROM run_cycles WHERE startup_wait_reason <> '' OR startup_wait_since IS NOT NULL`).Scan(&waiting).Error; err != nil {
		t.Fatal(err)
	}
	if waiting != 0 {
		t.Fatalf("%d existing rows read as waiting; none should", waiting)
	}
}
