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

// leftoverBackfillWhere copies leftover sm_api_* onto secret_ref_* when the
// leftover name is present and secret_ref_name is missing or blank. A
// populated secret_ref_name wins even if sm_api_* differs — writers stamp
// the triplet atomically, so a name without written_at is not a leftover
// shape this step recovers per-column.
const leftoverBackfillWhere = `NULLIF(sm_api_secret_ref_name, '') IS NOT NULL
		          AND NULLIF(secret_ref_name, '') IS NULL`

// RunPhase11SecretRefColumns adds secret_ref_* columns and copies from
// leftover sm_api_* columns when those still exist and secret_ref_name is
// empty. A non-empty secret_ref_name is left untouched (secret_ref_* wins).
// RunPhase14DropSMAPIColumns drops the leftover columns after this backfill.
//
// Idempotent — ADD COLUMN IF NOT EXISTS + conditional UPDATE.
func RunPhase11SecretRefColumns(ctx context.Context, db *gorm.DB) error {
	stmts := []string{
		`DO $$ BEGIN
		   IF EXISTS (SELECT FROM information_schema.tables
		              WHERE table_schema='public' AND table_name='org_anthropic_credentials') THEN
		     ALTER TABLE org_anthropic_credentials
		       ADD COLUMN IF NOT EXISTS secret_ref_name     TEXT,
		       ADD COLUMN IF NOT EXISTS secret_ref_kv_path  TEXT,
		       ADD COLUMN IF NOT EXISTS secret_ref_property TEXT;
		     IF EXISTS (SELECT FROM information_schema.columns
		                WHERE table_schema='public' AND table_name='org_anthropic_credentials'
		                  AND column_name='sm_api_secret_ref_name') THEN
		       UPDATE org_anthropic_credentials
		          SET secret_ref_name     = sm_api_secret_ref_name,
		              secret_ref_kv_path  = sm_api_kv_path,
		              secret_ref_property = sm_api_property
		        WHERE ` + leftoverBackfillWhere + `;
		     END IF;
		   END IF;
		 END $$`,

		`DO $$ BEGIN
		   IF EXISTS (SELECT FROM information_schema.tables
		              WHERE table_schema='public' AND table_name='org_credentials') THEN
		     ALTER TABLE org_credentials
		       ADD COLUMN IF NOT EXISTS secret_ref_name       TEXT,
		       ADD COLUMN IF NOT EXISTS secret_ref_kv_path    TEXT,
		       ADD COLUMN IF NOT EXISTS secret_ref_property   TEXT,
		       ADD COLUMN IF NOT EXISTS secret_ref_written_at TIMESTAMPTZ;
		     IF EXISTS (SELECT FROM information_schema.columns
		                WHERE table_schema='public' AND table_name='org_credentials'
		                  AND column_name='sm_api_secret_ref_name') THEN
		       UPDATE org_credentials
		          SET secret_ref_name       = sm_api_secret_ref_name,
		              secret_ref_kv_path    = sm_api_kv_path,
		              secret_ref_property   = sm_api_property,
		              secret_ref_written_at = sm_api_written_at
		        WHERE ` + leftoverBackfillWhere + `;
		     END IF;
		   END IF;
		 END $$`,

		`DO $$ BEGIN
		   IF EXISTS (SELECT FROM information_schema.tables
		              WHERE table_schema='public' AND table_name='organization_idp_profiles') THEN
		     ALTER TABLE organization_idp_profiles
		       ADD COLUMN IF NOT EXISTS secret_ref_name       TEXT,
		       ADD COLUMN IF NOT EXISTS secret_ref_kv_path    TEXT,
		       ADD COLUMN IF NOT EXISTS secret_ref_property   TEXT,
		       ADD COLUMN IF NOT EXISTS secret_ref_written_at TIMESTAMPTZ;
		     IF EXISTS (SELECT FROM information_schema.columns
		                WHERE table_schema='public' AND table_name='organization_idp_profiles'
		                  AND column_name='sm_api_secret_ref_name') THEN
		       UPDATE organization_idp_profiles
		          SET secret_ref_name       = sm_api_secret_ref_name,
		              secret_ref_kv_path    = sm_api_kv_path,
		              secret_ref_property   = sm_api_property,
		              secret_ref_written_at = sm_api_written_at
		        WHERE ` + leftoverBackfillWhere + `;
		     END IF;
		   END IF;
		 END $$`,
	}
	for i, sql := range stmts {
		if err := db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("phase11_secret_ref_columns step %d: %w", i+1, err)
		}
	}
	warnDivergentLeftoverSecretRefs(ctx, db)
	return nil
}

func warnDivergentLeftoverSecretRefs(ctx context.Context, db *gorm.DB) {
	for _, table := range []string{
		"org_anthropic_credentials",
		"org_credentials",
		"organization_idp_profiles",
	} {
		var has int
		if err := db.WithContext(ctx).Raw(
			`SELECT COUNT(*) FROM information_schema.columns
			  WHERE table_schema='public' AND table_name=? AND column_name='sm_api_secret_ref_name'`,
			table,
		).Scan(&has).Error; err != nil || has == 0 {
			continue
		}
		var n int
		q := fmt.Sprintf(`SELECT COUNT(*) FROM %s
			WHERE NULLIF(sm_api_secret_ref_name, '') IS NOT NULL
			  AND NULLIF(secret_ref_name, '') IS NOT NULL
			  AND (secret_ref_name IS DISTINCT FROM sm_api_secret_ref_name
			    OR secret_ref_kv_path IS DISTINCT FROM sm_api_kv_path
			    OR secret_ref_property IS DISTINCT FROM sm_api_property)`, table)
		if err := db.WithContext(ctx).Raw(q).Scan(&n).Error; err != nil {
			slog.WarnContext(ctx, "could not count leftover sm_api_* rows that differ from secret_ref_*",
				"table", table, "err", err)
			continue
		}
		if n > 0 {
			slog.WarnContext(ctx, "keeping secret_ref_* where leftover sm_api_* differs; leftover columns will be dropped",
				"table", table, "rows", n)
		}
	}
}
