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
	"log/slog"

	"gorm.io/gorm"
)

// RunPhase15IdentityPerEnvironment re-keys the identity tables from one
// cluster-wide directory to one per (org, environment).
//
// Roles and test users used to live on the single platform identity provider,
// so a role name and a username were each unique cluster-wide and the primary
// keys said so: `idp_roles(name)`, `test_users(username)`. They now live on the
// ENVIRONMENT's own identity provider — one per (org, environment) — where the
// same name on two environments is two groups, on two directories, that share
// nothing. The keys become (org_id, environment, name) and (org_id,
// environment, username), and `test_user_refs` gains the environment beside the
// org it already carried.
//
// ## Existing rows are DELETED, not backfilled
//
// Every surviving row names a Thunder group id or user id on the PLATFORM
// identity provider. Those objects are not on any environment's provider and
// never will be: nothing migrates a directory object between instances. A
// backfill would therefore write rows claiming "the platform created role X on
// acme/development" for a group that does not exist there — and the ensure's
// whole safety story is that a row IS the ownership marker. It would enrol test
// accounts into groups on the strength of a row about a different directory, and
// the panel would offer reveal and rotate for accounts that are not there.
//
// Deleting is cheap and self-healing: the next build's ensure is idempotent and
// recreates every role and test user the design declares, on the right
// directory, sealing fresh passwords and republishing them on that version's
// gate ticket. What is lost is the OLD passwords for the old platform-IdP
// accounts, which no longer authenticate anything the platform deploys — those
// accounts are left standing on the platform IdP (this package never deletes a
// directory object) for an operator to clear.
//
// The count is logged rather than silently dropped, because "how many
// credentials did that boot discard" is the one question a reader of this
// migration will have.
//
// Forward-only, and idempotent: the deletes are no-ops once the columns are
// keyed, and the primary-key swap is guarded on the CURRENT key's shape.
func RunPhase15IdentityPerEnvironment(ctx context.Context, db *gorm.DB) error {
	// 0. Nothing to do before the tables exist. AutoMigrate (BaseModels) creates
	//    them from the models, which already carry the new columns — so on a
	//    FRESH database this step finds a composite key and does nothing at all.
	for _, table := range []string{"idp_roles", "test_users", "test_user_refs"} {
		var exists bool
		if err := db.WithContext(ctx).Raw(
			`SELECT EXISTS (SELECT FROM information_schema.tables
			                 WHERE table_schema='public' AND table_name=?)`, table).
			Scan(&exists).Error; err != nil {
			return fmt.Errorf("phase15 probe %s: %w", table, err)
		}
		if !exists {
			return nil
		}
	}

	// 1. expand — the columns. AutoMigrate has already added them on the normal
	//    boot path; doing it here as well keeps this step true on its own, which
	//    is what makes the dbtest able to reconstruct the pre-migration shape.
	//    DEFAULT '' so the ALTER itself backfills and the column can be NOT NULL
	//    from the start; every such row is deleted two statements later anyway.
	for _, stmt := range []string{
		`ALTER TABLE idp_roles      ADD COLUMN IF NOT EXISTS org_id      TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE idp_roles      ADD COLUMN IF NOT EXISTS environment TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE test_users     ADD COLUMN IF NOT EXISTS org_id      TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE test_users     ADD COLUMN IF NOT EXISTS environment TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE test_user_refs ADD COLUMN IF NOT EXISTS environment TEXT NOT NULL DEFAULT ''`,
	} {
		if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
			return fmt.Errorf("phase15 expand: %w", err)
		}
	}

	// 2. discard — every row that names an object on the platform identity
	//    provider. They are exactly the rows with no environment.
	var roles, users, refs int64
	if err := db.WithContext(ctx).Raw(
		`SELECT count(*) FROM idp_roles WHERE environment = ''`).Scan(&roles).Error; err != nil {
		return fmt.Errorf("phase15 count roles: %w", err)
	}
	if err := db.WithContext(ctx).Raw(
		`SELECT count(*) FROM test_users WHERE environment = ''`).Scan(&users).Error; err != nil {
		return fmt.Errorf("phase15 count test users: %w", err)
	}
	if err := db.WithContext(ctx).Raw(
		`SELECT count(*) FROM test_user_refs WHERE environment = ''`).Scan(&refs).Error; err != nil {
		return fmt.Errorf("phase15 count refs: %w", err)
	}
	if roles+users+refs > 0 {
		slog.WarnContext(ctx, "phase15: discarding the platform-IdP era's roles and test users — "+
			"they name directory objects on the platform identity provider, which is no longer where "+
			"a build provisions; the next build's ensure recreates them on the environment's own "+
			"identity provider and republishes the logins on its gate ticket",
			"roles", roles, "testUsers", users, "projectRefs", refs)
	}
	// Refs first: they reference the accounts.
	for _, stmt := range []string{
		`DELETE FROM test_user_refs WHERE environment = ''`,
		`DELETE FROM test_users     WHERE environment = ''`,
		`DELETE FROM idp_roles      WHERE environment = ''`,
	} {
		if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
			return fmt.Errorf("phase15 discard: %w", err)
		}
	}

	// 3. contract — swap each primary key to the composite one, in ONE statement
	//    per table so a boot interrupted between them never leaves a table
	//    without a primary key. Each guard reads the CURRENT key's column list,
	//    so it fires only while the old key is still in place and a re-run is a
	//    no-op (ADD CONSTRAINT has no IF NOT EXISTS, and this list is re-applied
	//    on every boot).
	for _, swap := range []struct{ table, oldKey, newKey string }{
		{"idp_roles", `'name'`, "org_id, environment, name"},
		{"test_users", `'username'`, "org_id, environment, username"},
		{"test_user_refs", `'org_id','project_id','username'`, "org_id, environment, project_id, username"},
	} {
		sql := fmt.Sprintf(`DO $$
		 BEGIN
		   IF EXISTS (
		     SELECT 1 FROM pg_constraint c
		      WHERE c.conrelid = '%[1]s'::regclass
		        AND c.contype  = 'p'
		        AND (SELECT array_agg(a.attname::text ORDER BY a.attname)
		               FROM pg_attribute a
		              WHERE a.attrelid = c.conrelid
		                AND a.attnum = ANY(c.conkey)) = ARRAY[%[2]s]::text[]
		   ) THEN
		     ALTER TABLE %[1]s
		       DROP CONSTRAINT %[1]s_pkey,
		       ADD  PRIMARY KEY (%[3]s);
		   END IF;
		 END $$`, swap.table, swap.oldKey, swap.newKey)
		if err := db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("phase15 contract %s: %w", swap.table, err)
		}
	}
	return nil
}
