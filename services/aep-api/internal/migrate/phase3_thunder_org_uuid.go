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

// Thunder org UUID alignment.
//
// SM-API derives per-org namespaces (`wc-<orgUUID8>-<orgHash8>`) from
// the JWT's `ouId` claim. A locally-generated `organizations.uuid`
// (via `uuid.New()`) never matches the NS SM-API writes into, so the
// dispatch path (which must compute the same NS to find the
// materialized Secret) needs Thunder's authoritative `ouId`.
//
// This migration adds a nullable `thunder_org_uuid` column so the BFF
// can persist Thunder's authoritative ouId alongside the local PK
// without an FK-cascade migration. The orgensure middleware fills it
// lazily on first authed request; SecretRefWriter reads it from the JWT
// context directly; dispatcher reads it from the row.
package migrate

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

func RunPhase3ThunderOrgUUID(ctx context.Context, db *gorm.DB) error {
	stmt := `DO $$ BEGIN
		   IF EXISTS (SELECT FROM information_schema.tables
		              WHERE table_schema='public' AND table_name='organizations') THEN
		     ALTER TABLE organizations
		       ADD COLUMN IF NOT EXISTS thunder_org_uuid UUID;
		     CREATE INDEX IF NOT EXISTS idx_organizations_thunder_org_uuid
		       ON organizations(thunder_org_uuid)
		       WHERE thunder_org_uuid IS NOT NULL;
		   END IF;
		 END $$`
	if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
		return fmt.Errorf("phase3_thunder_org_uuid: %w", err)
	}
	return nil
}
