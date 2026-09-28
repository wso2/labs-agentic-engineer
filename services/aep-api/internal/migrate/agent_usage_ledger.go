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

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// RunAgentUsageLedger finishes the agent-usage ledger: the idempotency index its
// writers upsert against, and the one-time backfill from the dispatch rows that
// carried spend before the ledger existed.
//
// AutoMigrate (migrate.BaseModels) creates the table and its indexes from the
// model, this step's index create included — it is repeated here because the
// ledger's two writers use ON CONFLICT (source, source_id), and an INSERT whose
// arbiter index is missing does not degrade, it errors. The one thing that must
// exist for a capture to land is therefore asserted where it can be read, not
// left to be inferred from a struct tag.
//
// THE BACKFILL is the whole reason this is a migration rather than only a model.
// Spend used to live on run_cycles.cost_usd and executions.cost_usd, and the
// rollup now reads neither; without this, every deployment would show a usage
// page that had forgotten everything before the upgrade. It runs ON CONFLICT DO
// NOTHING, so a re-run adds nothing and never overwrites a capture the live
// writers have since refreshed.
//
// What it CANNOT recover is the spend of projects already deleted: their cycles
// and executions were purged by the delete cascade this ledger exists to survive,
// so there is nothing left to copy. Those projects start the ledger empty, and
// that is a fact about the past, not a defect in the backfill.
//
// captured_at is COALESCEd against now(): the dispatch row's own creation time is
// the honest capture date, but it is a Go-side default rather than a column
// default, so a row written by anything other than the repository can carry null
// — and an entry with no capture date at all would be worse than an approximate
// one.
//
// Idempotent throughout: CREATE … IF NOT EXISTS, and both backfills conflict
// against the same index.
func RunAgentUsageLedger(ctx context.Context, db *gorm.DB) error {
	if !hasTable(db, "agent_usage_ledger") {
		return nil
	}
	// The arbiter for both writers' ON CONFLICT. Named, not anonymous, so a
	// future predicate change is a rename rather than a silent no-op.
	if err := db.WithContext(ctx).Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS ux_agent_usage_ledger_source
		ON agent_usage_ledger (source, source_id)`).Error; err != nil {
		return fmt.Errorf("agent_usage_ledger source index: %w", err)
	}
	// The list read: "what did this org's project spend", newest lifetime first.
	if err := db.WithContext(ctx).Exec(`
		CREATE INDEX IF NOT EXISTS idx_agent_usage_ledger_org_project
		ON agent_usage_ledger (org_id, project_id, retired_at)`).Error; err != nil {
		return fmt.Errorf("agent_usage_ledger org/project index: %w", err)
	}

	if hasTable(db, "run_cycles") {
		// The version columns mirror MilestoneRun.SpecTag: the tag is empty on
		// pre-phase and early incident rows, whose milestone title IS the version.
		// LEFT JOIN, because a cycle whose run row is already gone still spent what
		// it spent.
		if err := db.WithContext(ctx).Exec(`
			INSERT INTO agent_usage_ledger (
			  org_id, project_id, source, source_id, phase, milestone_number, tag,
			  model_id, model_host, input_tokens, output_tokens, cache_read_tokens,
			  cache_creation_tokens, cost_usd, captured_at)
			SELECT c.org_id, c.project_id, ?, c.id::text, `+delivery.UsagePhaseCaseSQL("c.kind")+`,
			       COALESCE(m.milestone_number, 0),
			       COALESCE(NULLIF(m.tag, ''), m.milestone_title, ''),
			       c.model_id, c.model_host, c.input_tokens, c.output_tokens, c.cache_read_tokens,
			       c.cache_creation_tokens, c.cost_usd, COALESCE(c.created_at, now())
			FROM run_cycles c
			LEFT JOIN milestone_runs m ON m.id::text = c.run_id
			WHERE c.org_id <> '' AND c.project_id <> ''
			  AND c.input_tokens + c.output_tokens + c.cache_read_tokens + c.cache_creation_tokens > 0
			ON CONFLICT (source, source_id) DO NOTHING`,
			delivery.UsageLedgerSourceRunCycle,
		).Error; err != nil {
			return fmt.Errorf("agent_usage_ledger backfill from run_cycles: %w", err)
		}
	}

	if hasTable(db, "executions") {
		if err := db.WithContext(ctx).Exec(`
			INSERT INTO agent_usage_ledger (
			  org_id, project_id, source, source_id, phase, milestone_number, tag,
			  model_id, model_host, input_tokens, output_tokens, cache_read_tokens,
			  cache_creation_tokens, cost_usd, captured_at)
			SELECT e.org_id, e.project_id, ?, e.id::text, `+delivery.UsagePhaseCaseSQL("e.kind")+`,
			       0, '',
			       e.model_id, e.model_host, e.input_tokens, e.output_tokens, e.cache_read_tokens,
			       e.cache_creation_tokens, e.cost_usd, COALESCE(e.created_at, now())
			FROM executions e
			WHERE e.org_id <> '' AND e.project_id <> ''
			  AND e.input_tokens + e.output_tokens + e.cache_read_tokens + e.cache_creation_tokens > 0
			ON CONFLICT (source, source_id) DO NOTHING`,
			delivery.UsageLedgerSourceExecution,
		).Error; err != nil {
			return fmt.Errorf("agent_usage_ledger backfill from executions: %w", err)
		}
	}
	return nil
}
