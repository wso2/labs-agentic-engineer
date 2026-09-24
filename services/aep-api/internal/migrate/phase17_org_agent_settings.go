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

// RunPhase17OrgAgentSettings renames org_coding_agent_settings to
// org_agent_settings: the row now holds the one model EVERY agent uses, not only
// the coding agent's. AutoMigrate (BaseModels) has already created the new
// table from organization.OrgAgentSettings by the time this runs, so the rename
// is a copy: every old row moves across (an existing new row wins — it can only
// have been written by this build), then the old table is dropped. Idempotent:
// once the old table is gone the step does nothing.
func RunPhase17OrgAgentSettings(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var exists bool
		if err := tx.Raw(`SELECT to_regclass('public.org_coding_agent_settings') IS NOT NULL`).Scan(&exists).Error; err != nil {
			return fmt.Errorf("phase17 probe old table: %w", err)
		}
		if !exists {
			return nil
		}
		if err := tx.Exec(`
			INSERT INTO org_agent_settings (oc_org_id, runtime, model, updated_by, updated_at)
			SELECT oc_org_id, runtime, model, updated_by, updated_at
			  FROM org_coding_agent_settings
			ON CONFLICT (oc_org_id) DO NOTHING`).Error; err != nil {
			return fmt.Errorf("phase17 copy rows: %w", err)
		}
		if err := tx.Exec(`DROP TABLE org_coding_agent_settings`).Error; err != nil {
			return fmt.Errorf("phase17 drop old table: %w", err)
		}
		return nil
	})
}
