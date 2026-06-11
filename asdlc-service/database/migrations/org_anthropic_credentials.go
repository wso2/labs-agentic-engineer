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

package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// RunOrgAnthropicCredentialsMigration creates the org_anthropic_credentials
// table — metadata-only projection for per-org Anthropic keys. The encrypted
// key bytes live in `org_secrets(oc_org_id, key="anthropic/key")` (the same
// generic KV store the GitHub PAT uses). This table holds prefix + last4 for
// the UI, plus status / connected_at / last_validated_at / validation_error.
//
// Idempotent: safe to re-run on an already-migrated database.
func RunOrgAnthropicCredentialsMigration(ctx context.Context, db *gorm.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS org_anthropic_credentials (
		   oc_org_id          TEXT PRIMARY KEY,
		   key_prefix         TEXT NOT NULL,
		   key_last4          TEXT NOT NULL,
		   status             TEXT NOT NULL DEFAULT 'active'
		                          CHECK (status IN ('active','invalid','disconnected')),
		   connected_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
		   last_validated_at  TIMESTAMPTZ,
		   validation_error   TEXT
		 )`,
	}
	for i, sql := range stmts {
		if err := db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("org_anthropic_credentials migration step %d: %w", i+1, err)
		}
	}
	return nil
}
