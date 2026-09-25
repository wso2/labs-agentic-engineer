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
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/migrate"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// TestPhase13AnthropicCredentialRole_RekeysToCompositePK proves the migration
// landed on the shape everything else assumes: a `role` column defaulting to
// 'default', a CHECK bounding it, and a (oc_org_id, role) primary key.
//
// dbtest.New has already applied phase13 through the production
// migrate.RunAll, so this reads the resulting schema rather than re-running it.
func TestPhase13AnthropicCredentialRole_RekeysToCompositePK(t *testing.T) {
	db := dbtest.New(t)

	var pkCols []string
	if err := db.Raw(`
		SELECT a.attname
		  FROM pg_constraint c
		  JOIN unnest(c.conkey) WITH ORDINALITY AS k(attnum, ord) ON TRUE
		  JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.attnum
		 WHERE c.conrelid = 'org_anthropic_credentials'::regclass
		   AND c.contype = 'p'
		 ORDER BY k.ord`).Scan(&pkCols).Error; err != nil {
		t.Fatalf("read primary key: %v", err)
	}
	if got := strings.Join(pkCols, ","); got != "oc_org_id,role" {
		t.Fatalf("primary key = (%s); want (oc_org_id, role)", got)
	}

	var notNull bool
	var defaultExpr string
	if err := db.Raw(`
		SELECT attnotnull, COALESCE(pg_get_expr(d.adbin, d.adrelid), '')
		  FROM pg_attribute a
		  LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		 WHERE a.attrelid = 'org_anthropic_credentials'::regclass
		   AND a.attname = 'role'`).Row().Scan(&notNull, &defaultExpr); err != nil {
		t.Fatalf("read role column: %v", err)
	}
	if !notNull {
		t.Fatal("role must be NOT NULL — a null role would be a row no reader can address")
	}
	if !strings.Contains(defaultExpr, "'default'") {
		t.Fatalf("role default = %q; want 'default' so existing rows backfill to the org's default key", defaultExpr)
	}
}

// The CHECK is what keeps a typo'd role from becoming a row that every reader
// silently skips (GetByOrg filters by role, so it would simply never be found).
func TestPhase13AnthropicCredentialRole_CheckRejectsUnknownRole(t *testing.T) {
	db := dbtest.New(t)

	err := db.Exec(`
		INSERT INTO org_anthropic_credentials (oc_org_id, role, key_prefix, key_last4, status)
		VALUES ('acme', 'codign', 'sk-ant-ap03-x', 'wxyz', 'active')`).Error
	if err == nil {
		t.Fatal("a role outside ('default','coding') must be rejected by the CHECK")
	}
	if !strings.Contains(err.Error(), "role_check") {
		t.Fatalf("want the role CHECK to reject it, got: %v", err)
	}
}

// Both roles must be insertable for the SAME org — the whole point of the
// re-key. Under the old single-column PK this second insert was a conflict.
func TestPhase13AnthropicCredentialRole_BothRolesCoexist(t *testing.T) {
	db := dbtest.New(t)

	// Each role with the only kind phase16's CHECK lets it hold.
	for role, kind := range map[string]string{"default": "api_key", "coding": "oauth_token"} {
		if err := db.Exec(`
			INSERT INTO org_anthropic_credentials (oc_org_id, role, credential_kind, key_prefix, key_last4, status)
			VALUES ('acme', ?, ?, 'sk-ant-ap03-x', 'wxyz', 'active')`, role, kind).Error; err != nil {
			t.Fatalf("insert role %s: %v", role, err)
		}
	}

	var n int64
	if err := db.Raw(`SELECT count(*) FROM org_anthropic_credentials WHERE oc_org_id='acme'`).Scan(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("both roles must coexist for one org; got %d row(s)", n)
	}
}

// Forward-only migrations are re-applied on every boot, so a second run must be
// a no-op rather than an error — ADD CONSTRAINT has no IF NOT EXISTS, which is
// exactly what the DO blocks in the migration are guarding.
func TestPhase13AnthropicCredentialRole_Idempotent(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()

	for i := range 2 {
		if err := migrate.RunPhase13AnthropicCredentialRole(ctx, db); err != nil {
			t.Fatalf("re-run %d: %v", i+1, err)
		}
	}

	// …and the PK survived the re-runs.
	var pkCols []string
	if err := db.Raw(`
		SELECT a.attname
		  FROM pg_constraint c
		  JOIN unnest(c.conkey) WITH ORDINALITY AS k(attnum, ord) ON TRUE
		  JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.attnum
		 WHERE c.conrelid = 'org_anthropic_credentials'::regclass
		   AND c.contype = 'p'
		 ORDER BY k.ord`).Scan(&pkCols).Error; err != nil {
		t.Fatalf("read primary key: %v", err)
	}
	if got := strings.Join(pkCols, ","); got != "oc_org_id,role" {
		t.Fatalf("primary key after re-runs = (%s); want (oc_org_id, role)", got)
	}
}

// A row that predates the re-key must come out as the org's DEFAULT key. This
// is the one-way door: get it wrong and every existing org loses its key.
func TestPhase13AnthropicCredentialRole_BackfillsExistingRowsAsDefault(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()

	// Reconstruct the pre-migration shape: drop role (which cascades the
	// composite PK away), restore the single-column PK, then seed a legacy row.
	if err := db.Exec(`ALTER TABLE org_anthropic_credentials DROP COLUMN role`).Error; err != nil {
		t.Fatalf("drop role: %v", err)
	}
	if err := db.Exec(`ALTER TABLE org_anthropic_credentials ADD PRIMARY KEY (oc_org_id)`).Error; err != nil {
		t.Fatalf("restore legacy PK: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO org_anthropic_credentials (oc_org_id, key_prefix, key_last4, status)
		VALUES ('legacy-org', 'sk-ant-ap03-L', 'egcy', 'active')`).Error; err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}

	if err := migrate.RunPhase13AnthropicCredentialRole(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var role string
	if err := db.Raw(
		`SELECT role FROM org_anthropic_credentials WHERE oc_org_id='legacy-org'`).Scan(&role).Error; err != nil {
		t.Fatalf("read migrated row: %v", err)
	}
	if role != "default" {
		t.Fatalf("a pre-migration row must become the org's DEFAULT key; got role=%q", role)
	}
}
