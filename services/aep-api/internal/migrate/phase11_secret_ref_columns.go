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

// RunPhase11SecretRefColumns is the EXPAND half of the sm_api_* →
// secret_ref_* rename (ticket 09 / phase-03 item 14). It adds the
// provider-neutral columns alongside the legacy sm_api_* ones, then
// backfills from old → new where the new side is still null.
//
// CONTRACT (drop sm_api_*) is deferred to phase 09, after the old
// deployment that still reads sm_api_* from the shared app_factory_db
// is retired. Until then writers dual-write both column sets and
// readers prefer secret_ref_* with an sm_api_* fallback.
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
		   END IF;
		 END $$`,
		`UPDATE org_anthropic_credentials
		    SET secret_ref_name     = sm_api_secret_ref_name,
		        secret_ref_kv_path  = sm_api_kv_path,
		        secret_ref_property = sm_api_property
		  WHERE sm_api_secret_ref_name IS NOT NULL
		    AND secret_ref_name IS NULL`,

		`DO $$ BEGIN
		   IF EXISTS (SELECT FROM information_schema.tables
		              WHERE table_schema='public' AND table_name='org_credentials') THEN
		     ALTER TABLE org_credentials
		       ADD COLUMN IF NOT EXISTS secret_ref_name       TEXT,
		       ADD COLUMN IF NOT EXISTS secret_ref_kv_path    TEXT,
		       ADD COLUMN IF NOT EXISTS secret_ref_property   TEXT,
		       ADD COLUMN IF NOT EXISTS secret_ref_written_at TIMESTAMPTZ;
		   END IF;
		 END $$`,
		`UPDATE org_credentials
		    SET secret_ref_name       = sm_api_secret_ref_name,
		        secret_ref_kv_path    = sm_api_kv_path,
		        secret_ref_property   = sm_api_property,
		        secret_ref_written_at = sm_api_written_at
		  WHERE sm_api_secret_ref_name IS NOT NULL
		    AND secret_ref_name IS NULL`,

		`DO $$ BEGIN
		   IF EXISTS (SELECT FROM information_schema.tables
		              WHERE table_schema='public' AND table_name='organization_idp_profiles') THEN
		     ALTER TABLE organization_idp_profiles
		       ADD COLUMN IF NOT EXISTS secret_ref_name       TEXT,
		       ADD COLUMN IF NOT EXISTS secret_ref_kv_path    TEXT,
		       ADD COLUMN IF NOT EXISTS secret_ref_property   TEXT,
		       ADD COLUMN IF NOT EXISTS secret_ref_written_at TIMESTAMPTZ;
		   END IF;
		 END $$`,
		`UPDATE organization_idp_profiles
		    SET secret_ref_name       = sm_api_secret_ref_name,
		        secret_ref_kv_path    = sm_api_kv_path,
		        secret_ref_property   = sm_api_property,
		        secret_ref_written_at = sm_api_written_at
		  WHERE sm_api_secret_ref_name IS NOT NULL
		    AND secret_ref_name IS NULL`,
	}
	for i, sql := range stmts {
		if err := db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("phase11_secret_ref_columns step %d: %w", i+1, err)
		}
	}
	return nil
}
