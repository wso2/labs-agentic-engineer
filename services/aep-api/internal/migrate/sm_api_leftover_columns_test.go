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

package migrate_test

import (
	"testing"

	"gorm.io/gorm"
)

// addLeftoverSMAPIColumns reconstructs the pre-drop schema so a test can
// exercise phase11 backfill and phase14 DROP against leftover sm_api_*
// columns. dbtest.New applies every step, so those columns are already gone.
func addLeftoverSMAPIColumns(t *testing.T, db *gorm.DB) {
	t.Helper()
	stmts := []string{
		`ALTER TABLE org_anthropic_credentials
		   ADD COLUMN IF NOT EXISTS sm_api_secret_ref_name TEXT,
		   ADD COLUMN IF NOT EXISTS sm_api_kv_path TEXT,
		   ADD COLUMN IF NOT EXISTS sm_api_property TEXT`,
		`ALTER TABLE org_credentials
		   ADD COLUMN IF NOT EXISTS sm_api_secret_ref_name TEXT,
		   ADD COLUMN IF NOT EXISTS sm_api_kv_path TEXT,
		   ADD COLUMN IF NOT EXISTS sm_api_property TEXT,
		   ADD COLUMN IF NOT EXISTS sm_api_written_at TIMESTAMPTZ`,
		`ALTER TABLE organization_idp_profiles
		   ADD COLUMN IF NOT EXISTS sm_api_secret_ref_name TEXT,
		   ADD COLUMN IF NOT EXISTS sm_api_kv_path TEXT,
		   ADD COLUMN IF NOT EXISTS sm_api_property TEXT,
		   ADD COLUMN IF NOT EXISTS sm_api_written_at TIMESTAMPTZ`,
	}
	for _, sql := range stmts {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("add leftover sm_api_* columns: %v", err)
		}
	}
}

func leftoverColumnExists(t *testing.T, db *gorm.DB, table, column string) bool {
	t.Helper()
	var n int
	if err := db.Raw(
		`SELECT COUNT(*) FROM information_schema.columns
		  WHERE table_schema='public' AND table_name=? AND column_name=?`,
		table, column,
	).Scan(&n).Error; err != nil {
		t.Fatalf("column probe %s.%s: %v", table, column, err)
	}
	return n == 1
}
