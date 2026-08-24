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

// RunMilestoneRuns enforces ONE LIVE RUN PER MILESTONE, whatever its kind: a
// partial unique index on (org_id, project_id, milestone_number) covering only
// the non-terminal states.
//
// This is not a new rule — it is the one the whole loop already assumes and
// nothing enforced. The workflow id is per-milestone, the read model states that
// only the newest run can be live, and adoption refuses to start a second. Two
// live runs on one milestone would put two agents on one branch.
//
// The per-project build mutex cannot express it: that one is keyed on (org,
// project) and narrowed to dev runs, which is a rule about starting a new
// VERSION (milestone_run_kind.go). Every other kind sits outside it, so the
// guard against a second run on one milestone was a read-then-insert in
// application code — a check two concurrent requests both pass. The loser's row
// is then admitted with no workflow behind it (Temporal answers AlreadyStarted
// on the reused id), and because it is non-terminal it makes LiveRunForMilestone
// answer forever: every later revalidation of that version is refused until
// somebody cancels a run that was never running.
//
// Insertion goes through INSERT … ON CONFLICT DO NOTHING, which names no
// conflict target and so catches this index as well as the build mutex — the
// losing racer writes zero rows and TryAdmit reports admitted=false.
//
// AutoMigrate creates the milestone_runs and run_cycles tables from the models
// (migrate.BaseModels) but cannot express a partial (WHERE-clause) index, so it
// is added here.
//
// Idempotent: CREATE … IF NOT EXISTS and DROP … IF EXISTS are both no-ops on
// re-run, and the step no-ops entirely if the table is not present yet.
func RunMilestoneRuns(ctx context.Context, db *gorm.DB) error {
	if !hasTable(db, "milestone_runs") {
		return nil
	}
	// The retired origin-keyed mutex, from before the predicate was widened to
	// cover the planning state. Its successor is dropped by milestone_run_kind,
	// which replaces it with the kind-keyed index.
	if err := db.WithContext(ctx).Exec(
		`DROP INDEX IF EXISTS ux_milestone_runs_spec_active`).Error; err != nil {
		return fmt.Errorf("milestone_runs drop superseded mutex index: %w", err)
	}
	if err := db.WithContext(ctx).Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS ux_milestone_runs_active_per_milestone_v1
		ON milestone_runs (org_id, project_id, milestone_number)
		WHERE state IN ('planning', 'waiting', 'running')`).Error; err != nil {
		return fmt.Errorf("milestone_runs per-milestone active index: %w", err)
	}
	return nil
}
