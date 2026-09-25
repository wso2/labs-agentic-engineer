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

// RunPhase16CodingRoleSubscriptionOnly makes the coding role of
// org_anthropic_credentials hold a Claude subscription token and nothing else
// (ADR-0036). A separate coding API key is no longer a thing the platform
// offers, so every one of them is deleted — its encrypted bytes in org_secrets
// with it — and the org's coding runs bill its default API key from then on.
//
// Ordered, idempotent, one transaction:
//
//  1. delete the coding-role bytes (org_secrets 'anthropic/coding-key') of
//     every org whose coding row is not a subscription token, and of every org
//     that has no coding row at all (unreachable bytes nothing can clean up);
//  2. delete the api_key coding rows;
//  3. verify no row breaks the new rule (abort otherwise);
//  4. replace the CHECK: default ⇔ api_key, coding ⇔ oauth_token.
//
// The SM-API/OpenBao copies of the deleted keys (entity "anthropic-coding")
// cannot be removed here — deleting one needs a signed-in user's context — so
// they stay orphaned until the org next saves a subscription, which overwrites
// that vault path. Nothing reads them: dispatch resolves through the row, which
// is gone.
func RunPhase16CodingRoleSubscriptionOnly(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			DELETE FROM org_secrets s
			 WHERE s.key = 'anthropic/coding-key'
			   AND NOT EXISTS (
			     SELECT 1 FROM org_anthropic_credentials c
			      WHERE c.oc_org_id = s.oc_org_id
			        AND c.role = 'coding'
			        AND c.credential_kind = 'oauth_token')`).Error; err != nil {
			return fmt.Errorf("phase16 delete coding api-key bytes: %w", err)
		}
		if err := tx.Exec(`
			DELETE FROM org_anthropic_credentials
			 WHERE role = 'coding' AND credential_kind <> 'oauth_token'`).Error; err != nil {
			return fmt.Errorf("phase16 delete coding api-key rows: %w", err)
		}

		var bad int64
		if err := tx.Raw(`
			SELECT count(*) FROM org_anthropic_credentials
			 WHERE NOT ((role = 'default' AND credential_kind = 'api_key')
			         OR (role = 'coding'  AND credential_kind = 'oauth_token'))`).Scan(&bad).Error; err != nil {
			return fmt.Errorf("phase16 verify: %w", err)
		}
		if bad > 0 {
			return fmt.Errorf("phase16 aborted: %d org_anthropic_credentials row(s) pair a role with the wrong "+
				"credential kind; correct them before adding the CHECK", bad)
		}

		// ADD CONSTRAINT has no IF NOT EXISTS, and this list re-runs on every
		// boot, so the add is guarded. The constraint it replaces is dropped
		// idempotently; phase13 skips re-adding it once this one exists.
		stmts := []string{
			`ALTER TABLE org_anthropic_credentials
			   DROP CONSTRAINT IF EXISTS org_anthropic_credentials_oauth_is_coding_only`,
			`DO $$
			 BEGIN
			   IF NOT EXISTS (
			     SELECT 1 FROM pg_constraint
			      WHERE conrelid = 'org_anthropic_credentials'::regclass
			        AND conname  = 'org_anthropic_credentials_role_kind'
			   ) THEN
			     ALTER TABLE org_anthropic_credentials
			       ADD CONSTRAINT org_anthropic_credentials_role_kind
			       CHECK ((role = 'default' AND credential_kind = 'api_key')
			           OR (role = 'coding'  AND credential_kind = 'oauth_token'));
			   END IF;
			 END $$`,
		}
		for i, sql := range stmts {
			if err := tx.Exec(sql).Error; err != nil {
				return fmt.Errorf("phase16 contract step %d: %w", i+1, err)
			}
		}
		return nil
	})
}
