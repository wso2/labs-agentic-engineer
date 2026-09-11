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

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/migrate"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// primaryKeyColumns reads a table's primary key in key order.
func primaryKeyColumns(t *testing.T, db *gorm.DB, table string) string {
	t.Helper()
	var cols []string
	err := db.Raw(`
		SELECT a.attname
		  FROM pg_constraint c
		  JOIN unnest(c.conkey) WITH ORDINALITY AS k(attnum, ord) ON TRUE
		  JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.attnum
		 WHERE c.conrelid = ?::regclass
		   AND c.contype = 'p'
		 ORDER BY k.ord`, table).Scan(&cols).Error
	if err != nil {
		t.Fatalf("read primary key of %s: %v", table, err)
	}
	return strings.Join(cols, ",")
}

// The shape everything else assumes: an identity object is addressed by the
// directory it lives on, and the directory is (org, environment).
//
// dbtest.New has already applied phase15 through the production migrate.RunAll,
// so this reads the resulting schema rather than re-running it.
func TestPhase15IdentityPerEnvironment_RekeysToCompositePKs(t *testing.T) {
	db := dbtest.New(t)

	for table, want := range map[string]string{
		"idp_roles":      "org_id,environment,name",
		"test_users":     "org_id,environment,username",
		"test_user_refs": "org_id,environment,project_id,username",
	} {
		if got := primaryKeyColumns(t, db, table); got != want {
			t.Errorf("%s primary key = (%s); want (%s)", table, got, want)
		}
	}
}

// The same role name and the same username exist independently on two
// environments — two groups and two accounts on two directories that share
// nothing. Under the old single-column keys the second insert here was a
// conflict, which is exactly why the key had to move.
func TestPhase15IdentityPerEnvironment_SameNameOnTwoEnvironments(t *testing.T) {
	db := dbtest.New(t)

	for _, env := range []string{"default", "staging"} {
		if err := db.Exec(`
			INSERT INTO idp_roles (org_id, environment, name, thunder_group_id)
			VALUES ('acme', ?, 'Viewer', ?)`, env, "grp-"+env).Error; err != nil {
			t.Fatalf("insert role on %s: %v", env, err)
		}
		if err := db.Exec(`
			INSERT INTO test_users (org_id, environment, username, thunder_user_id, role_name)
			VALUES ('acme', ?, 'test-viewer', ?, 'Viewer')`, env, "usr-"+env).Error; err != nil {
			t.Fatalf("insert test user on %s: %v", env, err)
		}
	}

	var roles, users int64
	if err := db.Raw(`SELECT count(*) FROM idp_roles WHERE org_id='acme' AND name='Viewer'`).
		Scan(&roles).Error; err != nil {
		t.Fatalf("count roles: %v", err)
	}
	if err := db.Raw(`SELECT count(*) FROM test_users WHERE org_id='acme' AND username='test-viewer'`).
		Scan(&users).Error; err != nil {
		t.Fatalf("count test users: %v", err)
	}
	if roles != 2 || users != 2 {
		t.Fatalf("roles=%d testUsers=%d; both environments' rows must coexist", roles, users)
	}
}

// A row that predates the re-key names a group or an account on the PLATFORM
// identity provider, which is not where a build provisions any more. It is
// DELETED rather than backfilled: keeping it would claim the platform owns an
// object on a directory that has never held it, and the ensure treats such a row
// as its licence to enrol test accounts.
//
// This is the one-way door in this migration, so it is reconstructed from the
// pre-migration shape rather than asserted on an empty table.
func TestPhase15IdentityPerEnvironment_DiscardsPlatformIdPRows(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()

	// Rebuild the pre-migration shape: drop the key columns (which cascades the
	// composite PKs away) and restore the single-column keys.
	for _, stmt := range []string{
		`ALTER TABLE test_user_refs DROP COLUMN environment`,
		`ALTER TABLE test_users DROP COLUMN environment, DROP COLUMN org_id`,
		`ALTER TABLE idp_roles DROP COLUMN environment, DROP COLUMN org_id`,
		`ALTER TABLE idp_roles ADD PRIMARY KEY (name)`,
		`ALTER TABLE test_users ADD PRIMARY KEY (username)`,
		`ALTER TABLE test_user_refs ADD PRIMARY KEY (org_id, project_id, username)`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("reconstruct pre-migration shape (%s): %v", stmt, err)
		}
	}
	if err := db.Exec(`
		INSERT INTO idp_roles (name, thunder_group_id, created_by_org, created_by_project)
		VALUES ('Viewer', 'grp-on-the-platform-idp', 'acme', 'expenses')`).Error; err != nil {
		t.Fatalf("seed legacy role: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO test_users (username, thunder_user_id, role_name, password_sealed)
		VALUES ('test-viewer', 'usr-on-the-platform-idp', 'Viewer', 'sealed')`).Error; err != nil {
		t.Fatalf("seed legacy test user: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO test_user_refs (org_id, project_id, username, role_name)
		VALUES ('acme', 'expenses', 'test-viewer', 'Viewer')`).Error; err != nil {
		t.Fatalf("seed legacy ref: %v", err)
	}

	if err := migrate.RunPhase15IdentityPerEnvironment(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, table := range []string{"idp_roles", "test_users", "test_user_refs"} {
		var n int64
		if err := db.Raw(`SELECT count(*) FROM ` + table).Scan(&n).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Fatalf("%s kept %d row(s) naming the platform identity provider; they must be discarded", table, n)
		}
	}
	// …and the keys moved on the way.
	if got := primaryKeyColumns(t, db, "idp_roles"); got != "org_id,environment,name" {
		t.Fatalf("idp_roles primary key = (%s) after the migration", got)
	}
	if got := primaryKeyColumns(t, db, "test_user_refs"); got != "org_id,environment,project_id,username" {
		t.Fatalf("test_user_refs primary key = (%s) after the migration", got)
	}
}

// Forward-only migrations are re-applied on every boot, so a second run must be
// a no-op rather than an error — which is what the DO-block guards on the
// current key's shape are for.
func TestPhase15IdentityPerEnvironment_Idempotent(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()

	if err := db.Exec(`
		INSERT INTO idp_roles (org_id, environment, name, thunder_group_id)
		VALUES ('acme', 'default', 'Viewer', 'grp-1')`).Error; err != nil {
		t.Fatalf("seed migrated row: %v", err)
	}

	for i := range 2 {
		if err := migrate.RunPhase15IdentityPerEnvironment(ctx, db); err != nil {
			t.Fatalf("re-run %d: %v", i+1, err)
		}
	}

	// A row that already carries its environment is NOT a platform-IdP-era row
	// and must survive every re-run.
	var n int64
	if err := db.Raw(`SELECT count(*) FROM idp_roles WHERE org_id='acme' AND environment='default'`).
		Scan(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("a migrated row was discarded by a re-run (%d rows left)", n)
	}
	if got := primaryKeyColumns(t, db, "test_users"); got != "org_id,environment,username" {
		t.Fatalf("test_users primary key after re-runs = (%s)", got)
	}
}
