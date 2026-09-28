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

package delivery_test

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/modelcost"
)

// ledgerFixture is the whole delivery write side against one throwaway schema:
// runs, cycles (which mirror into the ledger as they stamp), and the ledger's
// own read/retire surface.
type ledgerFixture struct {
	db     *gorm.DB
	runs   delivery.MilestoneRunRepository
	cycles delivery.RunCycleRepository
	ledger delivery.AgentUsageLedgerRepository
}

// newLedgerFixture prices model-a at round figures ($1/MTok in, $10/MTok out) so
// an expected stamp reads by inspection rather than reverse-engineering.
func newLedgerFixture(t *testing.T) ledgerFixture {
	t.Helper()
	db := dbtest.New(t)
	stamper := modelcost.NewStamper([]modelcost.ModelRate{
		{Host: modelconn.AnthropicHost, ModelID: "model-a", InputPerMTok: 1, OutputPerMTok: 10},
	})
	return ledgerFixture{
		db:     db,
		runs:   delivery.NewMilestoneRunRepository(db),
		cycles: delivery.NewRunCycleRepository(db, stamper),
		ledger: delivery.NewAgentUsageLedgerRepository(db),
	}
}

// spend appends a cycle of the given kind under run and captures usage on it —
// one agent dispatch, from dispatch to stamp.
func (f ledgerFixture) spend(t *testing.T, run *delivery.MilestoneRun, kind string, in, out int64, model string) *delivery.RunCycle {
	t.Helper()
	ctx := context.Background()
	c := &delivery.RunCycle{OrgID: run.OrgID, ProjectID: run.ProjectID, RunID: run.ID, Kind: kind}
	if err := f.cycles.Append(ctx, c); err != nil {
		t.Fatalf("Append(%s): %v", kind, err)
	}
	dispatchedOn(t, f.cycles, c, modelconn.AnthropicHost)
	if err := f.cycles.RecordUsage(ctx, c.ID, contracts.CapturedUsage{TokenUsage: contracts.TokenUsage{
		InputTokens: in, OutputTokens: out, Model: model,
	}}); err != nil {
		t.Fatalf("RecordUsage(%s): %v", kind, err)
	}
	return c
}

// entries reads an org's ledger rows straight from the table. The repository
// exposes only the aggregate — the raw entries are what pin the snapshot
// columns and the retirement stamps, which the aggregate deliberately folds away.
func (f ledgerFixture) entries(t *testing.T, orgID string) []delivery.AgentUsageLedgerEntry {
	t.Helper()
	var rows []delivery.AgentUsageLedgerEntry
	if err := f.db.Where("org_id = ?", orgID).Order("captured_at").Find(&rows).Error; err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	return rows
}

func liveScope(project string) contracts.UsageScope {
	return contracts.UsageScope{ProjectID: project}
}

func retiredScope(project string) contracts.UsageScope {
	return contracts.UsageScope{ProjectID: project, Retired: true}
}

// TestUsageLedger_CaptureMirrorsTheCycleAndSplitsPhases: the ledger is written as
// part of the cycle's own stamp, carries the version the spend belongs to, and
// classifies each kind into the SDLC phase the usage page reports.
func TestUsageLedger_CaptureMirrorsTheCycleAndSplitsPhases(t *testing.T) {
	t.Parallel()
	f := newLedgerFixture(t)
	ctx := context.Background()
	run := admitRun(t, f.runs, "orgl", "shop", 7, "v7")

	f.spend(t, run, delivery.CycleKindCoding, 1_000_000, 100_000, "model-a")  // $1 + $1 = $2
	f.spend(t, run, delivery.CycleKindFix, 1_000_000, 0, "model-a")           // $1
	f.spend(t, run, delivery.CycleKindValidation, 500_000, 10_000, "model-a") // $0.60
	f.spend(t, run, delivery.CycleKindConflict, 0, 0, "")                     // captured nothing

	build, validation, err := f.ledger.SumUsageByProjectPhase(ctx, "orgl")
	if err != nil {
		t.Fatalf("SumUsageByProjectPhase: %v", err)
	}
	b := build[liveScope("shop")]
	if b.Tokens.InputTokens != 2_000_000 || b.Tokens.OutputTokens != 100_000 {
		t.Fatalf("build tokens = %+v, want 2M/100k", b.Tokens)
	}
	if b.CostUsd == nil || *b.CostUsd != 3.00 {
		t.Fatalf("build cost = %v, want 3.00 (2.00 + 1.00)", b.CostUsd)
	}
	// The conflict cycle spent nothing, so it must neither add a row nor drag the
	// phase's agreed model to "unknown".
	if b.Tokens.Model != "model-a" {
		t.Fatalf("build model = %q, want model-a", b.Tokens.Model)
	}
	v := validation[liveScope("shop")]
	if v.Tokens.InputTokens != 500_000 || v.Tokens.OutputTokens != 10_000 {
		t.Fatalf("validation tokens = %+v, want 500k/10k", v.Tokens)
	}

	// The entry knows which version it paid for — the whole reason the ledger
	// snapshots the milestone instead of joining back to a row that gets purged.
	entries := f.entries(t, "orgl")
	if len(entries) != 4 {
		t.Fatalf("ledger entries = %d, want 4 (one per dispatch)", len(entries))
	}
	for _, e := range entries {
		if e.MilestoneNumber != 7 || e.Tag != "v7" {
			t.Errorf("entry %s milestone/tag = %d/%q, want 7/v7", e.SourceID, e.MilestoneNumber, e.Tag)
		}
		if e.Source != delivery.UsageLedgerSourceRunCycle {
			t.Errorf("entry source = %q, want run_cycle", e.Source)
		}
		if e.RetiredAt != nil {
			t.Errorf("a live project's entry must not be retired: %v", e.RetiredAt)
		}
	}

	// Org fence.
	if b2, v2, err := f.ledger.SumUsageByProjectPhase(ctx, "other-org"); err != nil ||
		len(b2) != 0 || len(v2) != 0 {
		t.Fatalf("cross-org rollup = (%v, %v, %v), want empty", b2, v2, err)
	}
}

// TestUsageLedger_ReCaptureDoesNotDoubleCount: the runner's terminal log is read
// more than once and re-derives the same figures, so a repeat capture must
// update the entry in place. Without the (source, source_id) arbiter every tick
// of the watcher would add another copy of the same bill.
func TestUsageLedger_ReCaptureDoesNotDoubleCount(t *testing.T) {
	t.Parallel()
	f := newLedgerFixture(t)
	ctx := context.Background()
	run := admitRun(t, f.runs, "orgr", "shop", 1, "v1")

	cycle := f.spend(t, run, delivery.CycleKindCoding, 1_000_000, 0, "model-a")
	// The same capture again, and then a REVISED one (the log grew between
	// ticks) — the ledger must hold the latest figure, once.
	f.recapture(t, cycle.ID, 1_000_000, 0)
	f.recapture(t, cycle.ID, 3_000_000, 0)

	build, _, err := f.ledger.SumUsageByProjectPhase(ctx, "orgr")
	if err != nil {
		t.Fatalf("SumUsageByProjectPhase: %v", err)
	}
	b := build[liveScope("shop")]
	if b.Tokens.InputTokens != 3_000_000 {
		t.Fatalf("input tokens = %d, want 3M (the latest capture, counted once)", b.Tokens.InputTokens)
	}
	if b.CostUsd == nil || *b.CostUsd != 3.00 {
		t.Fatalf("cost = %v, want 3.00", b.CostUsd)
	}
}

func (f ledgerFixture) recapture(t *testing.T, cycleID string, in, out int64) {
	t.Helper()
	if err := f.cycles.RecordUsage(context.Background(), cycleID, contracts.CapturedUsage{
		TokenUsage: contracts.TokenUsage{InputTokens: in, OutputTokens: out, Model: "model-a"},
	}); err != nil {
		t.Fatalf("re-capture: %v", err)
	}
}

// TestUsageLedger_SpendSurvivesTheProjectDeletePurge is the rule this table
// exists for: the delete takes the run and its cycles, and what they cost stays
// — attributed to the incarnation that spent it.
func TestUsageLedger_SpendSurvivesTheProjectDeletePurge(t *testing.T) {
	t.Parallel()
	f := newLedgerFixture(t)
	ctx := context.Background()
	run := admitRun(t, f.runs, "orgd", "shop", 3, "v3")
	f.spend(t, run, delivery.CycleKindCoding, 2_000_000, 0, "model-a") // $2

	// The delete cascade, in the order the composition root runs it.
	if err := f.ledger.RetireByProject(ctx, "orgd", "shop"); err != nil {
		t.Fatalf("RetireByProject: %v", err)
	}
	if err := f.cycles.DeleteByProject(ctx, "orgd", "shop"); err != nil {
		t.Fatalf("cycles.DeleteByProject: %v", err)
	}
	if err := f.runs.DeleteByProject(ctx, "orgd", "shop"); err != nil {
		t.Fatalf("runs.DeleteByProject: %v", err)
	}

	// The working state really is gone.
	if rows, err := f.runs.ListByProject(ctx, "orgd", "shop"); err != nil || len(rows) != 0 {
		t.Fatalf("runs after delete = (%d, %v), want none", len(rows), err)
	}

	build, _, err := f.ledger.SumUsageByProjectPhase(ctx, "orgd")
	if err != nil {
		t.Fatalf("SumUsageByProjectPhase: %v", err)
	}
	if _, live := build[liveScope("shop")]; live {
		t.Error("a deleted project's spend must not read as the live lifetime's")
	}
	r := build[retiredScope("shop")]
	if r.Tokens.InputTokens != 2_000_000 {
		t.Fatalf("retired tokens = %d, want 2M (spend survives the purge)", r.Tokens.InputTokens)
	}
	if r.CostUsd == nil || *r.CostUsd != 2.00 {
		t.Fatalf("retired cost = %v, want 2.00", r.CostUsd)
	}
}

// TestUsageLedger_RecreatedProjectDoesNotInheritTheDeletedOnesSpend: a slug is
// not an identity. The retirement stamp is what keeps the two lifetimes apart,
// so the fresh project starts at zero while the bill it did not run stays
// readable under the retired scope.
func TestUsageLedger_RecreatedProjectDoesNotInheritTheDeletedOnesSpend(t *testing.T) {
	t.Parallel()
	f := newLedgerFixture(t)
	ctx := context.Background()

	first := admitRun(t, f.runs, "orgx", "shop", 1, "v1")
	f.spend(t, first, delivery.CycleKindCoding, 5_000_000, 0, "model-a") // $5, the first lifetime

	if err := f.ledger.RetireByProject(ctx, "orgx", "shop"); err != nil {
		t.Fatalf("RetireByProject: %v", err)
	}
	if err := f.cycles.DeleteByProject(ctx, "orgx", "shop"); err != nil {
		t.Fatalf("cycles.DeleteByProject: %v", err)
	}
	if err := f.runs.DeleteByProject(ctx, "orgx", "shop"); err != nil {
		t.Fatalf("runs.DeleteByProject: %v", err)
	}

	// Same name, new project, new run.
	second := admitRun(t, f.runs, "orgx", "shop", 1, "v1")
	f.spend(t, second, delivery.CycleKindCoding, 1_000_000, 0, "model-a") // $1, the second lifetime

	build, _, err := f.ledger.SumUsageByProjectPhase(ctx, "orgx")
	if err != nil {
		t.Fatalf("SumUsageByProjectPhase: %v", err)
	}
	live := build[liveScope("shop")]
	if live.Tokens.InputTokens != 1_000_000 {
		t.Fatalf("live tokens = %d, want 1M — the recreated project pays only for its own work", live.Tokens.InputTokens)
	}
	if live.CostUsd == nil || *live.CostUsd != 1.00 {
		t.Fatalf("live cost = %v, want 1.00", live.CostUsd)
	}
	retired := build[retiredScope("shop")]
	if retired.Tokens.InputTokens != 5_000_000 {
		t.Fatalf("retired tokens = %d, want 5M — the deleted lifetime's bill is still readable", retired.Tokens.InputTokens)
	}

	// Retiring again closes the second lifetime without disturbing the first: two
	// deletes leave two generations, not one merged heap.
	if err := f.ledger.RetireByProject(ctx, "orgx", "shop"); err != nil {
		t.Fatalf("second RetireByProject: %v", err)
	}
	entries := f.entries(t, "orgx")
	stamps := map[string]bool{}
	for _, e := range entries {
		if e.RetiredAt == nil {
			t.Fatal("every entry is retired after the second delete")
		}
		stamps[e.RetiredAt.String()] = true
	}
	if len(stamps) != 2 {
		t.Fatalf("distinct retirement stamps = %d, want 2 (one generation per delete)", len(stamps))
	}
}
