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

// RunPhase30RunCycleStartupWait adds the durable startup wait to run_cycles:
// startup_wait_reason (the stuck pod's waiting reason, empty when none) and
// startup_wait_since (when the watcher first saw the attempt stuck). The
// JobWatcher writes them on an open cycle and the run view reads them, so the
// console can show "waiting to start" without a cluster read.
//
// On the normal boot path AutoMigrate has already added both from the model
// (delivery.RunCycle); the ADD COLUMNs keep the step true on its own, which is
// what lets a dbtest rebuild the pre-migration shape. No backfill: a row
// written before the columns has no wait to show, and the default says so.
// Idempotent (IF NOT EXISTS); a no-op before the table exists.
func RunPhase30RunCycleStartupWait(ctx context.Context, db *gorm.DB) error {
	if !hasTable(db, "run_cycles") {
		return nil
	}
	if err := db.WithContext(ctx).Exec(`
		ALTER TABLE run_cycles
			ADD COLUMN IF NOT EXISTS startup_wait_reason text NOT NULL DEFAULT '',
			ADD COLUMN IF NOT EXISTS startup_wait_since timestamptz`).Error; err != nil {
		return fmt.Errorf("run_cycles startup wait columns: %w", err)
	}
	return nil
}
