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

// Phase24 drops the table phase22 created and the SRE org_secrets rows, keeps
// every other secret, and a re-run is a no-op.
func TestPhase24DropSreModelConnections(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	if err := migrate.RunOrgSecretsMigration(ctx, db); err != nil {
		t.Fatal(err)
	}
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

	var tables int64
	if err := db.Raw(`SELECT count(*) FROM information_schema.tables WHERE table_name = 'org_sre_model_connections'`).Scan(&tables).Error; err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatal("org_sre_model_connections still exists")
	}
	var keys []string
	if err := db.Raw(`SELECT key FROM org_secrets WHERE oc_org_id = 'acme' ORDER BY key`).Scan(&keys).Error; err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "github/pat" {
		t.Fatalf("org_secrets keys = %v, want only github/pat", keys)
	}
}
