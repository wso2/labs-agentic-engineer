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

	"gorm.io/gorm"
)

// RunPhase13AnthropicCredentialRole re-keys org_anthropic_credentials from
// (oc_org_id) to (oc_org_id, role), so an org can hold a second Anthropic key
// used only by the coding agent (ADR-0016). Every existing row is the org's
// DEFAULT key, which is exactly what the column default backfills.
//
// Forward-only, expand→backfill→verify→contract:
//
//  1. expand   — add `role` with DEFAULT 'default' so existing rows are
//     backfilled by the ALTER itself and the column can be NOT NULL from the
//     start (no separate UPDATE pass, no nullable window).
//  2. verify   — abort if any row carries a role outside the allowed set. On a
//     fresh expand this cannot happen; the check is what makes the CHECK
//     constraint in step 3 provably safe to add without a validation scan
//     failing mid-deploy.
//  3. contract — swap the primary key to the composite. Guarded against
//     pg_constraint so a re-run neither drops a PK it then fails to re-add nor
//     errors on an already-composite table.
//
// Idempotent: safe to re-run on an already-migrated database. Recovery for a
// failed run is an RDS snapshot restore + corrected re-run (forward-only; no
// down migration).
func RunPhase13AnthropicCredentialRole(ctx context.Context, db *gorm.DB) error {
	// 1. expand — DEFAULTs backfill existing rows as part of the ALTER. Every
	//    pre-migration row is the org's default Console API key, which is
	//    exactly what both defaults say.
	if err := db.WithContext(ctx).Exec(
		`ALTER TABLE org_anthropic_credentials
		   ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'default'`).Error; err != nil {
		return fmt.Errorf("phase13 expand (role column): %w", err)
	}
	// credential_kind decides how a credential is probed and which env var a
	// coding run receives it as. Dispatch reads this row and never the secret
	// bytes, so the kind has to be stored rather than re-derived from the key.
	if err := db.WithContext(ctx).Exec(
		`ALTER TABLE org_anthropic_credentials
		   ADD COLUMN IF NOT EXISTS credential_kind TEXT NOT NULL DEFAULT 'api_key'`).Error; err != nil {
		return fmt.Errorf("phase13 expand (credential_kind column): %w", err)
	}

	// 2. verify — expected 0 for both; the column defaults admit nothing else yet.
	var unknown int64
	if err := db.WithContext(ctx).Raw(
		`SELECT count(*) FROM org_anthropic_credentials
		   WHERE role NOT IN ('default','coding')
		      OR credential_kind NOT IN ('api_key','oauth_token')`).Scan(&unknown).Error; err != nil {
		return fmt.Errorf("phase13 verify: %w", err)
	}
	if unknown > 0 {
		return fmt.Errorf("phase13 aborted: %d org_anthropic_credentials row(s) carry a role or "+
			"credential_kind outside the allowed sets; correct them before adding the CHECKs", unknown)
	}

	// 3. contract — CHECK + composite PK. Both are wrapped in DO blocks because
	//    ADD CONSTRAINT has no IF NOT EXISTS: a bare re-run would error rather
	//    than no-op, and this list is re-applied on every boot.
	stmts := []string{
		`DO $$
		 BEGIN
		   IF NOT EXISTS (
		     SELECT 1 FROM pg_constraint
		      WHERE conrelid = 'org_anthropic_credentials'::regclass
		        AND conname  = 'org_anthropic_credentials_role_check'
		   ) THEN
		     ALTER TABLE org_anthropic_credentials
		       ADD CONSTRAINT org_anthropic_credentials_role_check
		       CHECK (role IN ('default','coding'));
		   END IF;
		 END $$`,

		`DO $$
		 BEGIN
		   IF NOT EXISTS (
		     SELECT 1 FROM pg_constraint
		      WHERE conrelid = 'org_anthropic_credentials'::regclass
		        AND conname  = 'org_anthropic_credentials_credential_kind_check'
		   ) THEN
		     ALTER TABLE org_anthropic_credentials
		       ADD CONSTRAINT org_anthropic_credentials_credential_kind_check
		       CHECK (credential_kind IN ('api_key','oauth_token'));
		   END IF;
		 END $$`,

		// An OAuth token bills a Claude subscription and is presented by Claude
		// Code itself, so only the coding agent can carry one — the design
		// agent is an AI SDK model call that speaks API keys only. Enforced in
		// the schema because a default-role OAuth row would leave every
		// non-coding reader unable to authenticate, with no obvious cause.
		//
		// phase16 replaced this CHECK with a stricter one
		// (org_anthropic_credentials_role_kind), so it is added only while that
		// successor does not exist yet — re-adding it on every boot after would
		// churn a constraint phase16 immediately drops again.
		`DO $$
		 BEGIN
		   IF NOT EXISTS (
		     SELECT 1 FROM pg_constraint
		      WHERE conrelid = 'org_anthropic_credentials'::regclass
		        AND conname IN ('org_anthropic_credentials_oauth_is_coding_only',
		                        'org_anthropic_credentials_role_kind')
		   ) THEN
		     ALTER TABLE org_anthropic_credentials
		       ADD CONSTRAINT org_anthropic_credentials_oauth_is_coding_only
		       CHECK (credential_kind = 'api_key' OR role = 'coding');
		   END IF;
		 END $$`,

		// Drop the single-column PK and add the composite one in ONE statement,
		// so the table is never left without a primary key if the boot is
		// interrupted between them. The guard reads the current PK's column
		// list: it fires only while the PK is still (oc_org_id) alone.
		`DO $$
		 BEGIN
		   IF EXISTS (
		     SELECT 1 FROM pg_constraint c
		      WHERE c.conrelid = 'org_anthropic_credentials'::regclass
		        AND c.contype  = 'p'
		        AND c.conkey   = ARRAY[
		              (SELECT attnum FROM pg_attribute
		                WHERE attrelid = 'org_anthropic_credentials'::regclass
		                  AND attname  = 'oc_org_id')
		            ]::smallint[]
		   ) THEN
		     ALTER TABLE org_anthropic_credentials
		       DROP CONSTRAINT org_anthropic_credentials_pkey,
		       ADD  PRIMARY KEY (oc_org_id, role);
		   END IF;
		 END $$`,
	}
	for i, sql := range stmts {
		if err := db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("phase13 contract step %d: %w", i+1, err)
		}
	}
	return nil
}
