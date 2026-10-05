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

// RunPhase23OrgSecretRefs lets org_secrets hold the org secrets' reference
// rows (key = the secret, value NULL, secret_ref_name = the SecretReference
// holding the value) beside the legacy value rows, and adds the columns the
// AE Studio Thunder clients are recorded in. phase26 then drops the value
// rows and the value column, so the NOT NULL relaxation runs only while value
// exists. Idempotent.
func RunPhase23OrgSecretRefs(ctx context.Context, db *gorm.DB) error {
	db = db.WithContext(ctx)
	stmts := []string{
		`ALTER TABLE org_secrets ADD COLUMN IF NOT EXISTS secret_ref_name TEXT`,
		`ALTER TABLE org_secrets ADD COLUMN IF NOT EXISTS written_at TIMESTAMPTZ`,
	}
	if hasColumn(db, "org_secrets", "value") {
		stmts = append(stmts, `ALTER TABLE org_secrets ALTER COLUMN value DROP NOT NULL`)
	}
	stmts = append(stmts,
		`ALTER TABLE organization_idp_profiles ADD COLUMN IF NOT EXISTS publisher_thunder_app_id TEXT`,
		`ALTER TABLE organization_idp_profiles ADD COLUMN IF NOT EXISTS studio_client_id TEXT`,
		`ALTER TABLE organization_idp_profiles ADD COLUMN IF NOT EXISTS studio_thunder_app_id TEXT`,
	)
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("phase23_org_secret_refs: %w", err)
		}
	}
	return nil
}
