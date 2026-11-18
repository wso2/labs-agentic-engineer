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

// RunPhase32DropIDPClientEntityIDs drops the two columns that held an IdP's
// own entity id of an org client: organization_idp_profiles
// .publisher_thunder_app_id and .studio_thunder_app_id. aep-api looked a
// client up on ThunderID by that id while it created the clients itself. The
// clients are created before aep-api runs now, nothing looks one up, and the
// columns had no reader left. The client ids stay, in publisher_client_id and
// studio_client_id.
//
// Idempotent (IF EXISTS); a no-op before the table exists. It cannot be
// undone on a database that held values, and loses nothing but a copy of the
// client id, which is derived from the org's name.
func RunPhase32DropIDPClientEntityIDs(ctx context.Context, db *gorm.DB) error {
	if !hasTable(db, "organization_idp_profiles") {
		return nil
	}
	if err := db.WithContext(ctx).Exec(`ALTER TABLE organization_idp_profiles
		DROP COLUMN IF EXISTS publisher_thunder_app_id,
		DROP COLUMN IF EXISTS studio_thunder_app_id`).Error; err != nil {
		return fmt.Errorf("phase32_drop_idp_client_entity_ids: %w", err)
	}
	return nil
}
