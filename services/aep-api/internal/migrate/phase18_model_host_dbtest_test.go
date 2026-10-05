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
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/migrate"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/modelcost"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// modelHostTables mirrors the step's own list: every usage table that carries
// the host its cost was priced on.
var modelHostTables = []string{"agent_turns", "run_cycles", "executions", "agent_usage_ledger"}

// A fresh schema is already in the final shape — AutoMigrate builds it from the
// models — so the step finds nothing to do, and the seeded rates sit on
// api.anthropic.com.
func TestPhase18ModelHost_FreshSchema(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()

	if got := primaryKeyColumns(t, db, "model_rates"); got != "host,model_id" {
		t.Fatalf("model_rates primary key = (%s); want (host,model_id)", got)
	}
	for _, table := range modelHostTables {
		if !columnExists(t, db, table, "model_host") {
			t.Fatalf("%s has no model_host column", table)
		}
	}
	rows, err := migrate.LoadModelRates(ctx, db)
	if err != nil {
		t.Fatalf("load rates: %v", err)
	}
	for _, r := range rows {
		if r.Host != modelconn.AnthropicHost {
			t.Errorf("seeded rate %s is on host %q, want %q", r.ModelID, r.Host, modelconn.AnthropicHost)
		}
	}

	before := rowVersions(t, db)
	for i := range 2 {
		if err := migrate.RunPhase18ModelHost(ctx, db); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	if after := rowVersions(t, db); !reflect.DeepEqual(before, after) {
		t.Fatalf("a re-run on a fresh schema rewrote rows:\n before %v\n after  %v", before, after)
	}
}

// The upgrade a real deployment goes through: a populated database in the
// pre-host shape, then the actual boot path — AutoMigrate from the models, then
// every step in order, model_rates_seed (which runs BEFORE this step) included.
//
// Every usage row comes out on api.anthropic.com, the rates keep their figures
// under the widened key, and no frozen cost moves: each row's cost_usd, every
// rollup, and a host-aware Stamper's re-pricing of each captured cycle all equal
// what they were before. A second run rewrites nothing.
func TestPhase18ModelHost_UpgradesAPopulatedDatabase(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	const org = "acme"

	rates, err := migrate.LoadModelRates(ctx, db)
	if err != nil {
		t.Fatalf("load rates: %v", err)
	}
	stamper := modelcost.NewStamper(rates)
	cycles := populateUsage(t, db, stamper, org)

	ledger := delivery.NewAgentUsageLedgerRepository(db)
	turns := spec.NewTurnRepository(db, stamper)
	buildBefore, validationBefore, err := ledger.SumUsageByProjectPhase(ctx, org)
	if err != nil {
		t.Fatalf("ledger rollup before: %v", err)
	}
	turnsBefore, err := turns.SumUsageByProject(ctx, org)
	if err != nil {
		t.Fatalf("turn rollup before: %v", err)
	}
	costsBefore := frozenCosts(t, db)
	ratesBefore := rateFigures(t, db)

	// The pre-host shape: model_rates keyed on model_id alone, no model_host
	// anywhere. Dropping host takes the composite key with it.
	for _, stmt := range []string{
		`ALTER TABLE model_rates DROP COLUMN host`,
		`ALTER TABLE model_rates ADD PRIMARY KEY (model_id)`,
		`ALTER TABLE agent_turns DROP COLUMN model_host`,
		`ALTER TABLE run_cycles DROP COLUMN model_host`,
		`ALTER TABLE executions DROP COLUMN model_host`,
		`ALTER TABLE agent_usage_ledger DROP COLUMN model_host`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("reconstruct the pre-host shape (%s): %v", stmt, err)
		}
	}

	bootMigrate(t, db)

	if got := primaryKeyColumns(t, db, "model_rates"); got != "host,model_id" {
		t.Fatalf("model_rates primary key = (%s) after the upgrade; want (host,model_id)", got)
	}
	if got := rateFigures(t, db); !reflect.DeepEqual(got, ratesBefore) {
		t.Fatalf("model_rates changed across the upgrade:\n before %v\n after  %v", ratesBefore, got)
	}
	for _, table := range append([]string{"model_rates"}, modelHostTables...) {
		col := "model_host"
		if table == "model_rates" {
			col = "host"
		}
		var hosts []string
		if err := db.Raw(`SELECT DISTINCT COALESCE(` + col + `, '<null>') FROM ` + table).Scan(&hosts).Error; err != nil {
			t.Fatalf("read %s.%s: %v", table, col, err)
		}
		if len(hosts) != 1 || hosts[0] != modelconn.AnthropicHost {
			t.Errorf("%s.%s = %v after the upgrade, want every row on %s", table, col, hosts, modelconn.AnthropicHost)
		}
	}

	if got := frozenCosts(t, db); !reflect.DeepEqual(got, costsBefore) {
		t.Fatalf("a frozen cost_usd moved across the upgrade:\n before %v\n after  %v", costsBefore, got)
	}
	buildAfter, validationAfter, err := ledger.SumUsageByProjectPhase(ctx, org)
	if err != nil {
		t.Fatalf("ledger rollup after: %v", err)
	}
	if !reflect.DeepEqual(buildAfter, buildBefore) || !reflect.DeepEqual(validationAfter, validationBefore) {
		t.Fatalf("ledger rollup changed across the upgrade:\n build %v → %v\n validation %v → %v",
			buildBefore, buildAfter, validationBefore, validationAfter)
	}
	turnsAfter, err := turns.SumUsageByProject(ctx, org)
	if err != nil {
		t.Fatalf("turn rollup after: %v", err)
	}
	if !reflect.DeepEqual(turnsAfter, turnsBefore) {
		t.Fatalf("turn rollup changed across the upgrade: %v → %v", turnsBefore, turnsAfter)
	}

	// SumCost over the existing cycles is unchanged: a host-aware Stamper over
	// the migrated rates prices each captured cycle, on its backfilled host, to
	// exactly the figure frozen on it before the upgrade.
	migrated, err := migrate.LoadModelRates(ctx, db)
	if err != nil {
		t.Fatalf("load migrated rates: %v", err)
	}
	after := modelcost.NewStamper(migrated)
	for _, c := range cycles {
		var row delivery.RunCycle
		if err := db.First(&row, "id = ?", c.id).Error; err != nil {
			t.Fatalf("read cycle %s: %v", c.id, err)
		}
		slices := c.usage.PricingSlices()
		ts := make([]modelcost.Tokens, 0, len(slices))
		for _, s := range slices {
			ts = append(ts, modelcost.Tokens{
				Host: row.ModelHost, ModelID: s.Model, InputTokens: s.InputTokens, OutputTokens: s.OutputTokens,
				CacheReadTokens: s.CacheReadTokens, CacheCreationTokens: s.CacheCreationTokens,
			})
		}
		if got, want := after.SumCost(ts), costsBefore["run_cycles/"+c.id]; !equalCost(got, want) {
			t.Errorf("cycle %s re-prices to %v after the upgrade, frozen at %v", c.id, fmtCost(got), fmtCost(want))
		}
	}

	// A second boot is a no-op: no row is rewritten.
	versions := rowVersions(t, db)
	if err := migrate.RunPhase18ModelHost(ctx, db); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got := rowVersions(t, db); !reflect.DeepEqual(got, versions) {
		t.Fatalf("a second run rewrote rows:\n before %v\n after  %v", versions, got)
	}
	bootMigrate(t, db)
}

// The backfill is one-shot: a row written after the upgrade with no host — a
// cycle not yet dispatched — is not relabelled api.anthropic.com by the next
// boot. Only NULL, which only a pre-upgrade row can hold, is backfilled.
func TestPhase18ModelHost_LeavesAnUnstampedRowAlone(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()

	runs := delivery.NewMilestoneRunRepository(db)
	run := admitDevRun(t, runs, "acme", "shop")
	c := &delivery.RunCycle{OrgID: "acme", ProjectID: "shop", RunID: run.ID, Kind: delivery.CycleKindCoding}
	if err := delivery.NewRunCycleRepository(db, nil).Append(ctx, c); err != nil {
		t.Fatalf("append: %v", err)
	}

	if err := migrate.RunPhase18ModelHost(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var host *string
	if err := db.Raw(`SELECT model_host FROM run_cycles WHERE id = ?`, c.ID).Scan(&host).Error; err != nil {
		t.Fatalf("read host: %v", err)
	}
	if host == nil || *host != "" {
		t.Fatalf("undispatched cycle model_host = %v after a re-run, want the empty string it was written with", fmtHost(host))
	}
}

// capturedCycle is one cycle populateUsage stamped, with what it captured.
type capturedCycle struct {
	id    string
	usage contracts.CapturedUsage
}

// populateUsage writes usage the way the product does — dispatch, capture,
// stamp, ledger — so the rows carry real frozen costs: a single-model coding
// cycle, a multi-model one, a validation cycle, a cycle that captured nothing,
// two spec turns, and an execution.
func populateUsage(t *testing.T, db *gorm.DB, stamper *modelcost.Stamper, org string) []capturedCycle {
	t.Helper()
	ctx := context.Background()
	runs := delivery.NewMilestoneRunRepository(db)
	cycleRepo := delivery.NewRunCycleRepository(db, stamper)
	run := admitDevRun(t, runs, org, "shop")

	captures := []struct {
		kind  string
		usage contracts.CapturedUsage
	}{
		{delivery.CycleKindCoding, contracts.CapturedUsage{TokenUsage: contracts.TokenUsage{
			InputTokens: 1_234_567, OutputTokens: 89_012, CacheReadTokens: 3_000_000, CacheCreationTokens: 40_000,
			Model: "claude-sonnet-5",
		}}},
		{delivery.CycleKindFix, contracts.CapturedUsage{
			TokenUsage: contracts.TokenUsage{InputTokens: 1_017, OutputTokens: 1_636, CacheReadTokens: 95_000, CacheCreationTokens: 20_500},
			Models: []contracts.TokenUsage{
				{InputTokens: 17, OutputTokens: 1_536, CacheReadTokens: 95_000, CacheCreationTokens: 13_000, Model: "claude-sonnet-5"},
				{InputTokens: 1_000, OutputTokens: 100, CacheCreationTokens: 7_500, Model: "claude-haiku-4-5"},
			},
		}},
		{delivery.CycleKindValidation, contracts.CapturedUsage{TokenUsage: contracts.TokenUsage{
			InputTokens: 500_000, OutputTokens: 10_000, Model: "claude-haiku-4-5",
		}}},
	}
	var out []capturedCycle
	for _, capture := range captures {
		c := &delivery.RunCycle{OrgID: org, ProjectID: "shop", RunID: run.ID, Kind: capture.kind}
		if err := cycleRepo.Append(ctx, c); err != nil {
			t.Fatalf("append %s: %v", capture.kind, err)
		}
		if _, err := cycleRepo.NoteLaunch(ctx, c.ID, modelconn.AnthropicHost, "development", ""); err != nil {
			t.Fatalf("note host %s: %v", capture.kind, err)
		}
		if err := cycleRepo.RecordUsage(ctx, c.ID, capture.usage); err != nil {
			t.Fatalf("record usage %s: %v", capture.kind, err)
		}
		out = append(out, capturedCycle{id: c.ID, usage: capture.usage})
	}
	// A cycle whose agent died before its terminal message: no usage, no host.
	if err := cycleRepo.Append(ctx, &delivery.RunCycle{OrgID: org, ProjectID: "shop", RunID: run.ID, Kind: delivery.CycleKindConflict}); err != nil {
		t.Fatalf("append conflict: %v", err)
	}

	turns := spec.NewTurnRepository(db, stamper)
	for i, project := range []string{"shop", "billing"} {
		now := time.Now().UTC()
		usage := contracts.TokenUsage{InputTokens: int64(40_000 * (i + 1)), OutputTokens: 7_777, CacheReadTokens: 120_000, Model: "claude-sonnet-5"}
		if err := turns.RecordFinished(ctx, org, []spec.TurnRecord{{
			TurnID: uuid.NewString(), Project: project, ConversationID: "c1", Kind: spec.TurnKindBrowser, Status: "completed",
			BaseRef: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SkillsRef: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			ModelHost: modelconn.AnthropicHost, Usage: usage, StartedAt: now, FinishedAt: now,
		}}); err != nil {
			t.Fatalf("record turn: %v", err)
		}
	}

	exec := &delivery.Execution{
		OrgID: org, ProjectID: "shop", Repo: "acme/shop", IssueNumber: 3,
		Kind: "coding", Status: "succeeded", ModelHost: modelconn.AnthropicHost,
	}
	if err := db.Create(exec).Error; err != nil {
		t.Fatalf("create execution: %v", err)
	}
	if err := delivery.NewExecutionRepository(db, stamper).RecordUsage(ctx, exec.ID, contracts.CapturedUsage{
		TokenUsage: contracts.TokenUsage{InputTokens: 250_000, OutputTokens: 30_000, Model: "claude-haiku-4-5"},
	}); err != nil {
		t.Fatalf("record execution usage: %v", err)
	}
	return out
}

func admitDevRun(t *testing.T, runs delivery.MilestoneRunRepository, org, project string) *delivery.MilestoneRun {
	t.Helper()
	ok, row, err := runs.TryAdmit(context.Background(), &delivery.MilestoneRun{
		OrgID: org, ProjectID: project, MilestoneNumber: 1, MilestoneTitle: "v1",
		Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild,
	})
	if err != nil || !ok || row == nil {
		t.Fatalf("admit run = (%v, %v)", ok, err)
	}
	return row
}

// bootMigrate runs the production boot path over an existing database:
// AutoMigrate from the models, then every ordered step.
//
// It starts and ends on fresh connections. This test reads and writes the same
// tables before and after their shape changes, and the driver caches each
// statement's plan per connection: Postgres refuses a cached SELECT * whose
// result type has since changed. A booting service migrates before its first
// query, so it never holds such a plan; the test drops its idle connections to
// be in the same position.
func bootMigrate(t *testing.T, db *gorm.DB) {
	t.Helper()
	dropIdleConnections(t, db)
	if err := db.AutoMigrate(migrate.BaseModels()...); err != nil {
		t.Fatalf("auto-migrate: %v", err)
	}
	if err := migrate.RunAll(context.Background(), db, "dev"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	dropIdleConnections(t, db)
}

// dropIdleConnections closes the pool's idle connections, and with them every
// statement plan they cached; the next query opens a new one.
func dropIdleConnections(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	sqlDB.SetMaxIdleConns(0)
	sqlDB.SetMaxIdleConns(2) // database/sql's default
}

// frozenCosts reads every usage row's stamped cost, keyed "table/id". A NULL
// stamp is a nil pointer, which reflect.DeepEqual tells apart from $0.
func frozenCosts(t *testing.T, db *gorm.DB) map[string]*float64 {
	t.Helper()
	out := map[string]*float64{}
	for _, q := range []struct{ table, key string }{
		{"agent_turns", "id::text"}, {"run_cycles", "id::text"}, {"executions", "id::text"},
		{"agent_usage_ledger", "source_id"},
	} {
		var rows []struct {
			Key     string
			CostUsd *float64
		}
		if err := db.Raw(`SELECT ` + q.key + ` AS key, cost_usd FROM ` + q.table).Scan(&rows).Error; err != nil {
			t.Fatalf("read %s costs: %v", q.table, err)
		}
		for _, r := range rows {
			out[q.table+"/"+r.Key] = r.CostUsd
		}
	}
	return out
}

// rateFigures reads the price card, keyed by model id.
func rateFigures(t *testing.T, db *gorm.DB) map[string][4]float64 {
	t.Helper()
	var rows []struct {
		ModelID                                                          string
		InputPerMTok, OutputPerMTok, CacheReadPerMTok, CacheWritePerMTok float64
	}
	if err := db.Raw(`SELECT model_id, input_per_m_tok, output_per_m_tok, cache_read_per_m_tok, cache_write_per_m_tok
	                    FROM model_rates`).Scan(&rows).Error; err != nil {
		t.Fatalf("read rates: %v", err)
	}
	out := map[string][4]float64{}
	for _, r := range rows {
		out[r.ModelID] = [4]float64{r.InputPerMTok, r.OutputPerMTok, r.CacheReadPerMTok, r.CacheWritePerMTok}
	}
	return out
}

// rowVersions reads every row's xmin in the tables the step touches. Any
// UPDATE, even one writing the same value, gives the row a new xmin, so equal
// maps mean nothing was rewritten.
func rowVersions(t *testing.T, db *gorm.DB) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, q := range []struct{ table, key string }{
		{"model_rates", "model_id"}, {"agent_turns", "id::text"}, {"run_cycles", "id::text"},
		{"executions", "id::text"}, {"agent_usage_ledger", "source_id"},
	} {
		var rows []struct{ Key, Xmin string }
		if err := db.Raw(`SELECT ` + q.key + ` AS key, xmin::text AS xmin FROM ` + q.table).Scan(&rows).Error; err != nil {
			t.Fatalf("read %s row versions: %v", q.table, err)
		}
		for _, r := range rows {
			out[q.table+"/"+r.Key] = r.Xmin
		}
	}
	return out
}

func columnExists(t *testing.T, db *gorm.DB, table, column string) bool {
	t.Helper()
	var exists bool
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.columns
	                   WHERE table_schema='public' AND table_name=? AND column_name=?)`, table, column).
		Scan(&exists).Error; err != nil {
		t.Fatalf("probe %s.%s: %v", table, column, err)
	}
	return exists
}

func equalCost(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func fmtCost(c *float64) any {
	if c == nil {
		return "null"
	}
	return *c
}

func fmtHost(h *string) any {
	if h == nil {
		return "NULL"
	}
	return *h
}
