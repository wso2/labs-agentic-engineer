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

// RunExecutionsReadModel creates the partial unique index backing the
// orchestration TaskDriver's read-model upsert (§R3.3): at most one row per
// non-empty workflow_id. Rows written only through the admission-mutex path
// (TryAdmit, no workflow attached) keep an empty workflow_id and are excluded
// from this constraint, so the two write paths never collide. AutoMigrate adds
// the workflow_id/version columns from the model but cannot express a partial
// index, so it is added here — same pattern as RunExecutions.
//
// Idempotent: CREATE UNIQUE INDEX IF NOT EXISTS is a no-op on re-run, and the
// step no-ops entirely if the table is not present yet.
func RunExecutionsReadModel(ctx context.Context, db *gorm.DB) error {
	if !hasTable(db, "executions") {
		return nil
	}
	if err := db.WithContext(ctx).Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS ux_executions_readmodel
		ON executions (workflow_id)
		WHERE workflow_id <> ''`).Error; err != nil {
		return fmt.Errorf("executions read-model index: %w", err)
	}
	return nil
}
