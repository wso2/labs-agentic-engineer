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

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/migrate"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// Phase24 drops the table phase22 created and, on a database that still has
// org_secrets' legacy (key, value) shape, the SRE rows, keeping every other
// secret; a re-run is a no-op.
func TestPhase24DropSreModelConnections(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	prePhase29Shape(t, db)
	if err := migrate.RunPhase22OrgSreModelConnections(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"sre-model/key", "sre-model/seed-applied", "sre/handoff-token", "github/pat"} {
		if err := db.Exec(`INSERT INTO org_secrets (oc_org_id, key, value) VALUES ('acme', ?, 'x')`, key).Error; err != nil {
			t.Fatal(err)
		}
	}

	for run := 0; run < 2; run++ {
		if err := migrate.RunPhase24DropSreModelConnections(ctx, db); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
	}

	assertNoSreModelConnections(t, db)
	var keys []string
	if err := db.Raw(`SELECT key FROM org_secrets WHERE oc_org_id = 'acme' ORDER BY key`).Scan(&keys).Error; err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "github/pat" {
		t.Fatalf("org_secrets keys = %v, want only github/pat", keys)
	}
}

// Every step reruns on every boot, so phase24 must tolerate the schema phase29
// leaves behind (org_secrets.key renamed to secret): it still drops the table
// and touches no secret row.
func TestPhase24DropSreModelConnections_ConvergedShape(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	if err := migrate.RunPhase22OrgSreModelConnections(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO org_secrets (oc_org_id, secret, secret_ref_name) VALUES ('acme', 'github/pat', 'aep-acme-github-pat')`).Error; err != nil {
		t.Fatal(err)
	}

	for run := 0; run < 2; run++ {
		if err := migrate.RunPhase24DropSreModelConnections(ctx, db); err != nil {
			t.Fatalf("run %d on the converged org_secrets: %v", run, err)
		}
	}

	assertNoSreModelConnections(t, db)
	var rows int64
	if err := db.Raw(`SELECT count(*) FROM org_secrets WHERE oc_org_id = 'acme'`).Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("org_secrets rows = %d, want the one reference row untouched", rows)
	}
}

func assertNoSreModelConnections(t *testing.T, db *gorm.DB) {
	t.Helper()
	var tables int64
	if err := db.Raw(`SELECT count(*) FROM information_schema.tables WHERE table_name = 'org_sre_model_connections'`).Scan(&tables).Error; err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatal("org_sre_model_connections still exists")
	}
}
