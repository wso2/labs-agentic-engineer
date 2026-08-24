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

// RunMilestoneRunKind moves the build mutex onto milestone_runs.kind — what a
// run DOES — and off origin, which only says where it came from.
//
// The column itself arrives from AutoMigrate (delivery.MilestoneRun is in
// BaseModels), so this step owns exactly two things: the backfill that gives
// every existing row a kind, and the re-keyed partial unique index.
//
// # Why the order inside this function is load-bearing
//
// EXPAND (AutoMigrate) → BACKFILL → CREATE the new index → DROP the old one.
//
// A partial index's predicate cannot be altered in place: CREATE UNIQUE INDEX
// IF NOT EXISTS matches on the NAME alone, so an index that already exists under
// the old predicate is silently kept and the rename is the only thing that makes
// the new predicate reachable. Hence a new name, ux_milestone_runs_dev_active_v3.
//
// And an index created BEFORE the backfill matches nothing: every pre-existing
// row would still hold the AutoMigrate default, so `WHERE kind = 'dev'` would
// select an empty set and the mutex would be quietly unenforced. Nothing fails,
// nothing logs — two concurrent build clicks are both admitted, and the symptom
// is two agents on one branch hours later. Backfilling first is what makes the
// index real at the instant it is created.
//
// Create-then-drop, in that order, for the same reason the v2 index was created
// before its predecessor was dropped: dropping first leaves the invariant
// unguarded for the width of the migration, which is exactly the window a
// double-click lands in.
//
// # Why the backfill is derived from origin, and unconditional
//
// origin is still NOT NULL and every writer sets both columns from the same
// decision, so origin is a total and correct function of kind for every row that
// exists. Re-deriving rather than filling only the blanks makes the step
// self-healing and idempotent — and IS DISTINCT FROM means a converged database
// writes nothing on re-run. This step retires together with the origin column.
//
// ux_milestone_runs_active_per_milestone_v1 (state-only, per-milestone) is
// deliberately untouched: it already makes "one live run per milestone, of ANY
// kind" true, and the kind split needs no new rule for it.
//
// Idempotent throughout, and a no-op when the table is not present yet.
func RunMilestoneRunKind(ctx context.Context, db *gorm.DB) error {
	if !hasTable(db, "milestone_runs") {
		return nil
	}
	// BACKFILL FIRST. The CASE mirrors delivery.RunKindForOrigin, and the ELSE
	// preserves whatever the column already holds — which for a row older than
	// the column is the AutoMigrate default, `dev` (see MilestoneRun.Kind, where
	// that default exists only so the ADD COLUMN can be NOT NULL). So an origin
	// this mapping does not know keeps `dev`, and that is sound rather than a
	// guess: origin is a NOT NULL closed enum validated at admission, so no
	// writer can produce one, and the only rows that could carry an unknown
	// origin predate both columns — when `dev` was the only kind a run could be.
	if err := db.WithContext(ctx).Exec(`
		UPDATE milestone_runs
		SET kind = CASE origin
			WHEN 'spec-build' THEN 'dev'
			WHEN 'incident-adoption' THEN 'task'
			WHEN 'revalidate' THEN 'validation'
			ELSE kind
		END
		WHERE kind IS DISTINCT FROM CASE origin
			WHEN 'spec-build' THEN 'dev'
			WHEN 'incident-adoption' THEN 'task'
			WHEN 'revalidate' THEN 'validation'
			ELSE kind
		END`).Error; err != nil {
		return fmt.Errorf("milestone_runs kind backfill: %w", err)
	}
	// THEN the mutex, against rows that now carry a kind. The state list must
	// stay exactly delivery.nonTerminalRunStates — a state in one and not the
	// other lets a second dev run in.
	if err := db.WithContext(ctx).Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS ux_milestone_runs_dev_active_v3
		ON milestone_runs (org_id, project_id)
		WHERE kind = 'dev' AND state IN ('planning', 'waiting', 'running')`).Error; err != nil {
		return fmt.Errorf("milestone_runs dev-run mutex index: %w", err)
	}
	// The origin-keyed predecessor. It refuses exactly the same set of rows the
	// new index refuses — kind is derived from origin — so by here the invariant
	// has never been unguarded.
	if err := db.WithContext(ctx).Exec(
		`DROP INDEX IF EXISTS ux_milestone_runs_spec_active_v2`).Error; err != nil {
		return fmt.Errorf("milestone_runs drop superseded mutex index: %w", err)
	}
	return nil
}
