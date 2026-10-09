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

// RunPhase31RunCycleStartupClock adds run_cycles.startup_clock_at: when the
// current attempt's startup grace began, i.e. the later of its dispatch and
// the creation of its pod (or Job) as the JobWatcher first saw it. The grace
// used to count from dispatched_at, and Cloud OpenChoreo applies a release
// 8-13 min after it is requested, so every Cloud coding run failed before its
// pod existed.
//
// On the normal boot path AutoMigrate has already added it from the model
// (delivery.RunCycle); the ADD COLUMN keeps the step true on its own, which is
// what lets a dbtest rebuild the pre-migration shape. No backfill: a row
// without a clock is bounded by the apply cap from its dispatch until the
// watcher writes one. Idempotent (IF NOT EXISTS); a no-op before the table
// exists.
func RunPhase31RunCycleStartupClock(ctx context.Context, db *gorm.DB) error {
	if !hasTable(db, "run_cycles") {
		return nil
	}
	if err := db.WithContext(ctx).Exec(`
		ALTER TABLE run_cycles
			ADD COLUMN IF NOT EXISTS startup_clock_at timestamptz`).Error; err != nil {
		return fmt.Errorf("run_cycles startup clock column: %w", err)
	}
	return nil
}
