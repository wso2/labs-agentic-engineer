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

func TestPhase23OrgSecretRefs(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	// Rebuild the pre-phase23 shape on the migrated test database.
	prePhase26Shape(t, db)
	for _, stmt := range []string{
		`ALTER TABLE org_secrets DROP COLUMN secret_ref_name`,
		`ALTER TABLE org_secrets DROP COLUMN written_at`,
		`ALTER TABLE org_secrets ALTER COLUMN value SET NOT NULL`,
		`ALTER TABLE organization_idp_profiles DROP COLUMN publisher_thunder_app_id`,
		`ALTER TABLE organization_idp_profiles DROP COLUMN studio_client_id`,
		`ALTER TABLE organization_idp_profiles DROP COLUMN studio_thunder_app_id`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("rebuild the pre-phase23 shape (%s): %v", stmt, err)
		}
	}
	if err := db.Exec(`INSERT INTO org_secrets (oc_org_id, key, value) VALUES ('default', 'github/pat', 'sealed-bytes')`).Error; err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	for i := 0; i < 2; i++ { // the second run proves idempotence
		if err := migrate.RunPhase23OrgSecretRefs(ctx, db); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}

	columns := []struct{ table, column, nullable string }{
		{"org_secrets", "secret_ref_name", "YES"},
		{"org_secrets", "written_at", "YES"},
		{"org_secrets", "value", "YES"},
		{"organization_idp_profiles", "publisher_thunder_app_id", "YES"},
		{"organization_idp_profiles", "studio_client_id", "YES"},
		{"organization_idp_profiles", "studio_thunder_app_id", "YES"},
	}
	for _, c := range columns {
		var nullable string
		if err := db.Raw(`SELECT is_nullable FROM information_schema.columns
		   WHERE table_schema = 'public' AND table_name = ? AND column_name = ?`, c.table, c.column).
			Scan(&nullable).Error; err != nil {
			t.Fatalf("probe %s.%s: %v", c.table, c.column, err)
		}
		if nullable != c.nullable {
			t.Errorf("%s.%s is_nullable = %q, want %q (missing column reads empty)", c.table, c.column, nullable, c.nullable)
		}
	}

	var legacy struct {
		Value         string
		SecretRefName *string
	}
	if err := db.Raw(`SELECT value, secret_ref_name FROM org_secrets WHERE oc_org_id = 'default' AND key = 'github/pat'`).
		Scan(&legacy).Error; err != nil {
		t.Fatalf("read legacy row: %v", err)
	}
	if legacy.Value != "sealed-bytes" || legacy.SecretRefName != nil {
		t.Fatalf("legacy row = %+v, want it untouched", legacy)
	}

	if err := db.Exec(`INSERT INTO org_secrets (oc_org_id, key, secret_ref_name) VALUES ('default', 'github-pat', 'default-github-pat-0a1b2c3d')`).Error; err != nil {
		t.Fatalf("a reference row with no value must be legal: %v", err)
	}
}
