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

// RunPhase26SecretsRefsOnly leaves Postgres holding secret reference names
// only: org_secrets becomes (oc_org_id, secret, secret_ref_name, written_at),
// keyed by (oc_org_id, secret), where a row means "set" and its value lives
// in the vault behind the named SecretReference. Every column that held a
// secret value, a sealed copy, a preview of one or a vault path is dropped.
//
// It is the one convergence step for fresh and upgraded databases alike:
// org_secrets is still created in its legacy shape (RunOrgSecretsMigration),
// so the steps before this one meet the same schema on a first boot as on an
// upgrade, and every step before it tolerates the converged shape on the
// boots after.
//
// Every ref-less org_secrets row is deleted: the value rows of every earlier
// release (github/pat, anthropic/key, model/key, anthropic/coding-key) named
// no reference, and nothing reads them. No reference is backfilled; an org
// without its rows re-enters the secret on the setup screen.
//
// One transaction, so a failed boot leaves the previous shape whole.
// Idempotent: every statement is guarded or IF EXISTS.
func RunPhase26SecretsRefsOnly(ctx context.Context, db *gorm.DB) error {
	stmts := []string{
		// org_secrets: key → secret. updated_at is dropped, not renamed:
		// phase23 added written_at beside it, and a reference row stamps
		// written_at; a row stamped before then gets now().
		`DO $$ BEGIN
		   IF EXISTS (SELECT 1 FROM information_schema.columns
		               WHERE table_schema = 'public' AND table_name = 'org_secrets' AND column_name = 'key') THEN
		     ALTER TABLE org_secrets RENAME COLUMN key TO secret;
		   END IF;
		 END $$`,
		`DELETE FROM org_secrets WHERE secret_ref_name IS NULL OR secret_ref_name = ''`,
		`UPDATE org_secrets SET written_at = now() WHERE written_at IS NULL`,
		`ALTER TABLE org_secrets DROP COLUMN IF EXISTS value, DROP COLUMN IF EXISTS updated_at`,
		`ALTER TABLE org_secrets ALTER COLUMN secret_ref_name SET NOT NULL,
		   ALTER COLUMN written_at SET DEFAULT now(), ALTER COLUMN written_at SET NOT NULL`,
		// org_credentials: the CHECK names webhook_secrets, so it goes first.
		`ALTER TABLE org_credentials DROP CONSTRAINT IF EXISTS secrets_shape_per_kind`,
		`ALTER TABLE org_credentials DROP COLUMN IF EXISTS webhook_secrets, DROP COLUMN IF EXISTS pat_secret_ref,
		   DROP COLUMN IF EXISTS secret_ref_name, DROP COLUMN IF EXISTS secret_ref_kv_path,
		   DROP COLUMN IF EXISTS secret_ref_property, DROP COLUMN IF EXISTS secret_ref_written_at`,
		`ALTER TABLE org_model_connections DROP COLUMN IF EXISTS key_preview, DROP COLUMN IF EXISTS secret_ref_name,
		   DROP COLUMN IF EXISTS secret_ref_kv_path, DROP COLUMN IF EXISTS secret_ref_property`,
		`ALTER TABLE org_anthropic_credentials DROP COLUMN IF EXISTS key_prefix, DROP COLUMN IF EXISTS key_last4,
		   DROP COLUMN IF EXISTS secret_ref_name, DROP COLUMN IF EXISTS secret_ref_kv_path,
		   DROP COLUMN IF EXISTS secret_ref_property`,
		`ALTER TABLE organization_idp_profiles DROP COLUMN IF EXISTS publisher_client_secret,
		   DROP COLUMN IF EXISTS publisher_secret_ref, DROP COLUMN IF EXISTS secret_ref_name,
		   DROP COLUMN IF EXISTS secret_ref_kv_path, DROP COLUMN IF EXISTS secret_ref_property,
		   DROP COLUMN IF EXISTS secret_ref_written_at`,
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i, sql := range stmts {
			if err := tx.Exec(sql).Error; err != nil {
				return fmt.Errorf("phase26_secrets_refs_only step %d: %w", i+1, err)
			}
		}
		return nil
	})
}
