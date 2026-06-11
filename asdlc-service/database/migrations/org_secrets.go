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

// RunOrgSecretsMigration creates the org_secrets table that stores per-org
// credentials (GitHub PATs, build tokens) in git-service's own Postgres DB.
// Replaces the previous OpenBao-backed store.
//
// Idempotent: safe to re-run on an already-migrated database.
func RunOrgSecretsMigration(ctx context.Context, db *gorm.DB) error {
	if err := db.WithContext(ctx).Exec(`
		CREATE TABLE IF NOT EXISTS org_secrets (
		  oc_org_id   TEXT        NOT NULL,
		  key         TEXT        NOT NULL,
		  value       TEXT        NOT NULL,
		  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
		  PRIMARY KEY (oc_org_id, key)
		)`).Error; err != nil {
		return fmt.Errorf("org_secrets migration: %w", err)
	}
	return nil
}
