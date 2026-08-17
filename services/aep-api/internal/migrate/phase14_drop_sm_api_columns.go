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

// RunPhase14DropSMAPIColumns drops leftover sm_api_* columns from the
// credential tables. secret_ref_* stay. This binary stamps secret_ref_*
// only; phase11 already copied leftover sm_api_* values into secret_ref_*
// where secret_ref_name was empty. After this step a pre-drop binary
// cannot connect/rotate/delete against the migrated database (it still
// writes sm_api_* keys).
//
// Idempotent — DROP COLUMN IF EXISTS.
func RunPhase14DropSMAPIColumns(ctx context.Context, db *gorm.DB) error {
	stmts := []string{
		`DO $$ BEGIN
		   IF EXISTS (SELECT FROM information_schema.tables
		              WHERE table_schema='public' AND table_name='org_anthropic_credentials') THEN
		     ALTER TABLE org_anthropic_credentials
		       DROP COLUMN IF EXISTS sm_api_secret_ref_name,
		       DROP COLUMN IF EXISTS sm_api_kv_path,
		       DROP COLUMN IF EXISTS sm_api_property;
		   END IF;
		 END $$`,
		`DO $$ BEGIN
		   IF EXISTS (SELECT FROM information_schema.tables
		              WHERE table_schema='public' AND table_name='org_credentials') THEN
		     ALTER TABLE org_credentials
		       DROP COLUMN IF EXISTS sm_api_secret_ref_name,
		       DROP COLUMN IF EXISTS sm_api_kv_path,
		       DROP COLUMN IF EXISTS sm_api_property,
		       DROP COLUMN IF EXISTS sm_api_written_at;
		   END IF;
		 END $$`,
		`DO $$ BEGIN
		   IF EXISTS (SELECT FROM information_schema.tables
		              WHERE table_schema='public' AND table_name='organization_idp_profiles') THEN
		     ALTER TABLE organization_idp_profiles
		       DROP COLUMN IF EXISTS sm_api_secret_ref_name,
		       DROP COLUMN IF EXISTS sm_api_kv_path,
		       DROP COLUMN IF EXISTS sm_api_property,
		       DROP COLUMN IF EXISTS sm_api_written_at;
		   END IF;
		 END $$`,
	}
	for i, sql := range stmts {
		if err := db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("phase14_drop_sm_api_columns step %d: %w", i+1, err)
		}
	}
	return nil
}
