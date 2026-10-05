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

// RunPhase25RunCycleSettling adds the settle sweep's partial index. The
// ComponentSettler lists closed coding cycles whose Component is not yet
// deleted, least recently checked first (delivery RunCycleRepository
// ListSettling), and every closed cycle is in that set until it settles, so
// the index covers exactly the sweep's predicate and its order.
//
// AutoMigrate (BaseModels) adds the settle_checked_at column but cannot
// express a partial index. Idempotent (IF NOT EXISTS); a no-op before the
// table exists.
func RunPhase25RunCycleSettling(ctx context.Context, db *gorm.DB) error {
	if !hasTable(db, "run_cycles") {
		return nil
	}
	if err := db.WithContext(ctx).Exec(`
		CREATE INDEX IF NOT EXISTS ix_run_cycles_settling
		ON run_cycles (settle_checked_at ASC NULLS FIRST, ended_at ASC)
		WHERE ended_at IS NOT NULL AND component_deleted_at IS NULL AND job_ref LIKE 'ca-%'`).Error; err != nil {
		return fmt.Errorf("run_cycles settling index: %w", err)
	}
	return nil
}
