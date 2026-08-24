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

package migrate_test

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/migrate"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// TestAgentUsageLedger_BacksfillsSpendCapturedBeforeTheLedgerExisted.
//
// Every deployment upgrading into the ledger already has spend, stamped on
// run_cycles and executions by the code that came before it. The rollup reads
// neither of those any more, so without this backfill the Settings → Usage page
// would come back from the upgrade having forgotten everything.
//
// The seed goes in by raw SQL on purpose: going through the repository would
// write the ledger entry itself, which is precisely the state this migration
// exists to repair.
func TestAgentUsageLedger_BacksfillsSpendCapturedBeforeTheLedgerExisted(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()

	runs := delivery.NewMilestoneRunRepository(db)
	admitted, run, err := runs.TryAdmit(ctx, &delivery.MilestoneRun{
		OrgID: "orgb", ProjectID: "shop", MilestoneNumber: 4, MilestoneTitle: "v4", Tag: "v4",
		Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild, State: delivery.RunStateWaiting,
	})
	if err != nil || !admitted {
		t.Fatalf("TryAdmit = (%v, %v)", admitted, err)
	}

	// A pre-ledger capture: tokens and a frozen stamp on the cycle row, nothing
	// in the ledger.
	if err := db.Exec(`
		INSERT INTO run_cycles (org_id, project_id, run_id, kind, attempts,
		  input_tokens, output_tokens, cost_usd)
		VALUES (?, ?, ?, ?, 1, 1000000, 100000, 7.5)`,
		run.OrgID, run.ProjectID, run.ID, delivery.CycleKindCoding).Error; err != nil {
		t.Fatalf("seed legacy cycle: %v", err)
	}
	// A validation cycle too, so the backfill is proven to carry the phase split
	// rather than dumping everything into build.
	if err := db.Exec(`
		INSERT INTO run_cycles (org_id, project_id, run_id, kind, attempts, input_tokens)
		VALUES (?, ?, ?, ?, 1, 250000)`,
		run.OrgID, run.ProjectID, run.ID, delivery.CycleKindValidation).Error; err != nil {
		t.Fatalf("seed legacy validation cycle: %v", err)
	}
	// A cycle that captured nothing must not become a ledger entry.
	if err := db.Exec(`
		INSERT INTO run_cycles (org_id, project_id, run_id, kind, attempts)
		VALUES (?, ?, ?, ?, 1)`,
		run.OrgID, run.ProjectID, run.ID, delivery.CycleKindConflict).Error; err != nil {
		t.Fatalf("seed empty cycle: %v", err)
	}

	ledger := delivery.NewAgentUsageLedgerRepository(db)
	if build, validation, err := ledger.SumUsageByProjectPhase(ctx, "orgb"); err != nil ||
		len(build) != 0 || len(validation) != 0 {
		t.Fatalf("ledger before the backfill = (%v, %v, %v), want empty", build, validation, err)
	}

	if err := migrate.RunAgentUsageLedger(ctx, db); err != nil {
		t.Fatalf("RunAgentUsageLedger: %v", err)
	}

	live := contracts.UsageScope{ProjectID: "shop"}
	build, validation, err := ledger.SumUsageByProjectPhase(ctx, "orgb")
	if err != nil {
		t.Fatalf("SumUsageByProjectPhase: %v", err)
	}
	b := build[live]
	if b.Tokens.InputTokens != 1_000_000 || b.Tokens.OutputTokens != 100_000 {
		t.Fatalf("backfilled build tokens = %+v, want 1M/100k", b.Tokens)
	}
	if b.CostUsd == nil || *b.CostUsd != 7.5 {
		t.Fatalf("backfilled cost = %v, want the stamp frozen on the row (7.5)", b.CostUsd)
	}
	if v := validation[live]; v.Tokens.InputTokens != 250_000 {
		t.Fatalf("backfilled validation tokens = %+v, want 250k in its own phase", v.Tokens)
	}
	// Backfilled spend belongs to the LIVE lifetime — the project still exists.
	if _, retired := build[contracts.UsageScope{ProjectID: "shop", Retired: true}]; retired {
		t.Error("backfilled entries must not arrive pre-retired")
	}

	// Re-running the migration is a no-op, not a second bill. Upgrades re-run
	// every step, so this is the ordinary case rather than the exotic one.
	if err := migrate.RunAgentUsageLedger(ctx, db); err != nil {
		t.Fatalf("RunAgentUsageLedger (second run): %v", err)
	}
	again, _, err := ledger.SumUsageByProjectPhase(ctx, "orgb")
	if err != nil {
		t.Fatalf("SumUsageByProjectPhase after re-run: %v", err)
	}
	if again[live].Tokens.InputTokens != 1_000_000 {
		t.Fatalf("re-run doubled the ledger: input tokens = %d, want 1M", again[live].Tokens.InputTokens)
	}
}
