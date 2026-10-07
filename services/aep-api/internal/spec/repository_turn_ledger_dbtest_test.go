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

package spec_test

// DB tier for the finished-turn ledger (record-turn-usage): the AE
// Studio tools pod hands over finished turns, the store keeps each once
// (idempotent on the turn id), stamps cost_usd at ingest, and the
// readers that drive the status poll, the build gate's design baseline and
// kickoff idempotency see the rows unchanged.

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/modelcost"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// finishedTurn is a completed browser turn of project p1 that spent tokens.
func finishedTurn(project string) spec.TurnRecord {
	started := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	return spec.TurnRecord{
		TurnID:         uuid.NewString(),
		Project:        project,
		ConversationID: uuid.NewString(),
		Kind:           spec.TurnKindBrowser,
		Flow:           "design",
		Status:         "completed",
		BaseRef:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SkillsRef:      "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		StartedAt:      started,
		FinishedAt:     started.Add(90 * time.Second),
		AuthorID:       "ada@example.com",
		AuthorName:     "Ada",
		ModelHost:      modelconn.AnthropicHost,
		Usage: contracts.TokenUsage{
			InputTokens: 1_000_000, OutputTokens: 100_000,
			CacheReadTokens: 10, CacheCreationTokens: 20, Model: "claude-sonnet-5",
		},
	}
}

// getTurn reads org's ledger row for the turn in project; nil when there is
// none.
func getTurn(t *testing.T, db *gorm.DB, org, project, id string) *spec.AgentTurn {
	t.Helper()
	var rows []spec.AgentTurn
	if err := db.Where("org_id = ? AND project_id = ? AND id = ?", org, project, id).Find(&rows).Error; err != nil {
		t.Fatalf("read turn: %v", err)
	}
	if len(rows) == 0 {
		return nil
	}
	return &rows[0]
}

// A record lands as one row whatever the delivery count: the sender retries a
// batch whose answer it lost, and one POST may carry the same turn twice.
func TestRecordFinished_IdempotentOnTurnID(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := spec.NewTurnRepository(db, nil)
	ctx := context.Background()
	rec := finishedTurn("p1")

	if err := repo.RecordFinished(ctx, "o1", []spec.TurnRecord{rec, rec}); err != nil {
		t.Fatalf("RecordFinished (duplicate in one batch): %v", err)
	}
	changed := rec
	changed.Status = "failed"
	if err := repo.RecordFinished(ctx, "o1", []spec.TurnRecord{changed}); err != nil {
		t.Fatalf("RecordFinished (resend): %v", err)
	}
	var n int64
	if err := db.Model(&spec.AgentTurn{}).Where("org_id = ? AND id = ?", "o1", rec.TurnID).Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("rows for the turn = (%d, %v), want 1", n, err)
	}
	row := getTurn(t, db, "o1", "p1", rec.TurnID)
	if row.Status != "completed" {
		t.Fatalf("status = %q: a resend must keep the stored record, not overwrite it", row.Status)
	}
	sum, err := repo.SumUsageByProject(ctx, "o1")
	if err != nil {
		t.Fatalf("SumUsageByProject: %v", err)
	}
	if got := sum["p1"].Tokens.InputTokens; got != 1_000_000 {
		t.Fatalf("p1 input tokens = %d, want 1000000 (counted once)", got)
	}
}

// Every field of the record is stored where the ledger's readers look for it.
func TestRecordFinished_StoresTheRecord(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := spec.NewTurnRepository(db, nil)
	ctx := context.Background()
	rec := finishedTurn("p1")
	rec.Kind = spec.TurnKindPlan
	rec.Status = "failed"
	rec.Reason = "agent-error"
	rec.Code = "provider_limit"
	ctxTokens := int64(42_000)
	rec.ContextTokens = &ctxTokens

	if err := repo.RecordFinished(ctx, "o1", []spec.TurnRecord{rec}); err != nil {
		t.Fatalf("RecordFinished: %v", err)
	}
	row := getTurn(t, db, "o1", "p1", rec.TurnID)
	if row == nil {
		t.Fatal("the record was not stored")
	}
	if row.OrgID != "o1" || row.ProjectID != "p1" || row.ConversationID != rec.ConversationID {
		t.Errorf("identity = (%q, %q, %q)", row.OrgID, row.ProjectID, row.ConversationID)
	}
	if row.Kind != spec.TurnKindPlan || row.Flow != "design" || row.Status != "failed" ||
		row.Reason != "agent-error" || row.Code != "provider_limit" {
		t.Errorf("outcome = kind %q flow %q status %q reason %q code %q", row.Kind, row.Flow, row.Status, row.Reason, row.Code)
	}
	if row.BaseRef != rec.BaseRef || row.SkillsRef != rec.SkillsRef {
		t.Errorf("refs = (%q, %q)", row.BaseRef, row.SkillsRef)
	}
	if !row.StartedAt.Equal(rec.StartedAt) || row.FinishedAt == nil || !row.FinishedAt.Equal(rec.FinishedAt) {
		t.Errorf("times = (%v, %v), want (%v, %v)", row.StartedAt, row.FinishedAt, rec.StartedAt, rec.FinishedAt)
	}
	if row.AuthorID != "ada@example.com" || row.AuthorDisplayName != "Ada" {
		t.Errorf("author = (%q, %q)", row.AuthorID, row.AuthorDisplayName)
	}
	if row.ModelID != "claude-sonnet-5" || row.ModelHost != modelconn.AnthropicHost ||
		row.InputTokens != 1_000_000 || row.OutputTokens != 100_000 ||
		row.CacheReadTokens != 10 || row.CacheCreationTokens != 20 {
		t.Errorf("usage = %+v", row)
	}
	if row.ContextTokens == nil || *row.ContextTokens != 42_000 {
		t.Errorf("context tokens = %v, want 42000", row.ContextTokens)
	}
	if row.CostUsd != nil {
		t.Errorf("cost_usd = %v with no stamper, want null", *row.CostUsd)
	}
}

// cost_usd is stamped at ingest from the (host, model) rate in force;
// an unpriced host stays null.
func TestRecordFinished_StampsCost(t *testing.T) {
	t.Parallel()
	stamper := modelcost.NewStamper([]modelcost.ModelRate{
		{Host: modelconn.AnthropicHost, ModelID: "claude-sonnet-5", InputPerMTok: 2, OutputPerMTok: 10},
	})
	db := dbtest.New(t)
	repo := spec.NewTurnRepository(db, stamper)
	ctx := context.Background()

	priced := finishedTurn("p1") // $2 + $1
	unpriced := finishedTurn("p2")
	unpriced.ModelHost = modelconn.OllamaHost
	if err := repo.RecordFinished(ctx, "o1", []spec.TurnRecord{priced, unpriced}); err != nil {
		t.Fatalf("RecordFinished: %v", err)
	}
	row := getTurn(t, db, "o1", "p1", priced.TurnID)
	if row == nil || row.CostUsd == nil || *row.CostUsd != 3.00 {
		t.Fatalf("priced cost_usd = %+v, want 3.00", row)
	}
	row = getTurn(t, db, "o1", "p2", unpriced.TurnID)
	if row == nil || row.CostUsd != nil {
		t.Fatalf("unpriced row = %+v, want cost_usd null", row)
	}
	sum, err := repo.SumUsageByProject(ctx, "o1")
	if err != nil {
		t.Fatalf("SumUsageByProject: %v", err)
	}
	if c := sum["p1"].CostUsd; c == nil || *c != 3.00 {
		t.Fatalf("p1 usage cost = %v, want 3.00", c)
	}
	if h := sum["p1"].Host; h != modelconn.AnthropicHost {
		t.Fatalf("p1 usage host = %q", h)
	}
}

// The status poll and kickoff idempotency read Newest; the build gate's
// design baseline reads NewestCompletedFlow. Both see ledger rows unchanged:
// a failed record reads failed, and a completed design turn is the baseline.
func TestRecordFinished_ReadersSeeTheLedger(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := spec.NewTurnRepository(db, nil)
	ctx := context.Background()

	if got, err := repo.Newest(ctx, "o1", "p1"); err != nil || got != nil {
		t.Fatalf("Newest before any record = (%+v, %v), want (nil, nil)", got, err)
	}
	design := finishedTurn("p1")
	if err := repo.RecordFinished(ctx, "o1", []spec.TurnRecord{design}); err != nil {
		t.Fatalf("RecordFinished(design): %v", err)
	}
	failed := finishedTurn("p1")
	failed.Flow = ""
	failed.Status = "failed"
	failed.Reason = "stream-died"
	failed.StartedAt = design.FinishedAt.Add(time.Minute)
	failed.FinishedAt = failed.StartedAt.Add(time.Minute)
	if err := repo.RecordFinished(ctx, "o1", []spec.TurnRecord{failed}); err != nil {
		t.Fatalf("RecordFinished(failed): %v", err)
	}

	newest, err := repo.Newest(ctx, "o1", "p1")
	if err != nil || newest == nil || newest.ID != failed.TurnID || newest.Status != spec.TurnStatusFailed {
		t.Fatalf("Newest = (%+v, %v), want the failed record", newest, err)
	}
	baseline, err := repo.NewestCompletedFlow(ctx, "o1", "p1", "design")
	if err != nil || baseline == nil || baseline.ID != design.TurnID {
		t.Fatalf("NewestCompletedFlow(design) = (%+v, %v), want the design record", baseline, err)
	}
	if other, _ := repo.Newest(ctx, "o2", "p1"); other != nil {
		t.Fatalf("Newest for another org = %+v, want nil", other)
	}
}

// A ledger row sorts by when its turn started, not by when the record arrived:
// a batch may carry two turns of one project, in any order, and a delayed
// batch must not make an older turn the newest.
func TestRecordFinished_NewestIsTheLatestStartedTurn(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := spec.NewTurnRepository(db, nil)
	ctx := context.Background()
	earlier := finishedTurn("p1")
	later := finishedTurn("p1")
	later.Status = "failed"
	later.StartedAt = earlier.FinishedAt.Add(time.Second)
	later.FinishedAt = later.StartedAt.Add(time.Second)

	if err := repo.RecordFinished(ctx, "o1", []spec.TurnRecord{later, earlier}); err != nil {
		t.Fatalf("RecordFinished: %v", err)
	}
	newest, err := repo.Newest(ctx, "o1", "p1")
	if err != nil || newest == nil || newest.ID != later.TurnID {
		t.Fatalf("Newest = (%+v, %v), want the later-started turn", newest, err)
	}
	if !newest.CreatedAt.Equal(later.StartedAt) {
		t.Fatalf("created_at = %v, want the turn's start %v", newest.CreatedAt, later.StartedAt)
	}
}

// A pod clock running ahead must not pin a turn as the newest (3.16 carry):
// created_at is min(startedAt, now), so a turn recorded later still wins
// Newest, which drives the kickoff guard and spec.agent.
func TestRecordFinished_AFutureStartDoesNotPinNewest(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := spec.NewTurnRepository(db, nil)
	ctx := context.Background()
	skewed := finishedTurn("p1")
	skewed.StartedAt = time.Now().UTC().Add(24 * time.Hour).Truncate(time.Microsecond)
	skewed.FinishedAt = skewed.StartedAt.Add(time.Minute)

	before := time.Now().UTC()
	if err := repo.RecordFinished(ctx, "o1", []spec.TurnRecord{skewed}); err != nil {
		t.Fatalf("RecordFinished skewed: %v", err)
	}
	row := getTurn(t, db, "o1", "p1", skewed.TurnID)
	if row == nil || row.CreatedAt.After(time.Now().UTC()) || row.CreatedAt.Before(before.Add(-time.Second)) {
		t.Fatalf("created_at = %+v, want clamped to the ingest time", row)
	}
	if !row.StartedAt.Equal(skewed.StartedAt) {
		t.Fatalf("started_at = %v, want the pod's own %v", row.StartedAt, skewed.StartedAt)
	}

	next := finishedTurn("p1")
	next.StartedAt = time.Now().UTC().Add(time.Millisecond)
	next.FinishedAt = next.StartedAt.Add(time.Millisecond)
	time.Sleep(2 * time.Millisecond)
	if err := repo.RecordFinished(ctx, "o1", []spec.TurnRecord{next}); err != nil {
		t.Fatalf("RecordFinished next: %v", err)
	}
	newest, err := repo.Newest(ctx, "o1", "p1")
	if err != nil || newest == nil || newest.ID != next.TurnID {
		t.Fatalf("Newest = (%+v, %v), want the turn recorded after the skewed one", newest, err)
	}
}

// The ledger's identity is (org, turn id): the pod chooses turn ids (the
// kickoff's is uuidv5 of org/project, so anyone can compute it), and another
// org's row with the same id must never stand in for this org's record. Org B
// records X first; org A's X is still stored, and org B's row is untouched.
func TestRecordFinished_AnotherOrgsTurnIDNeverSuppressesThisOrgsRecord(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := spec.NewTurnRepository(db, nil)
	ctx := context.Background()
	squatted := finishedTurn("p1")
	squatted.Status = "failed"

	if err := repo.RecordFinished(ctx, "org-b", []spec.TurnRecord{squatted}); err != nil {
		t.Fatalf("org B RecordFinished: %v", err)
	}
	real := squatted
	real.Status = "completed"
	if err := repo.RecordFinished(ctx, "org-a", []spec.TurnRecord{real}); err != nil {
		t.Fatalf("org A RecordFinished: %v", err)
	}
	if row := getTurn(t, db, "org-a", "p1", real.TurnID); row == nil || row.Status != "completed" {
		t.Fatalf("org A's row = %+v, want its own completed record", row)
	}
	if row := getTurn(t, db, "org-b", "p1", squatted.TurnID); row == nil || row.Status != "failed" {
		t.Fatalf("org B's row = %+v, want it untouched", row)
	}
	if newest, err := repo.Newest(ctx, "org-a", "p1"); err != nil || newest == nil || newest.Status != "completed" {
		t.Fatalf("org A's Newest = (%+v, %v), want its own record", newest, err)
	}
}

// A marketplace turn has no project (C7): it is stored under project_id ”.
func TestRecordFinished_MarketplaceTurnHasNoProject(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := spec.NewTurnRepository(db, nil)
	ctx := context.Background()
	rec := finishedTurn("")

	if err := repo.RecordFinished(ctx, "o1", []spec.TurnRecord{rec}); err != nil {
		t.Fatalf("RecordFinished: %v", err)
	}
	row := getTurn(t, db, "o1", "", rec.TurnID)
	if row == nil || row.ProjectID != "" {
		t.Fatalf("marketplace row = %+v, want project_id ''", row)
	}
}

// A design turn's feature scope is stored as its IDs only, space-joined and
// deduplicated, in Summary, and reads back through DesignedFeatures. Any other
// flow stores none, even when a record carries some (the contract says they
// are ignored there, so a stray field never costs the batch).
func TestRecordFinished_StoresADesignTurnsFeatures(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := spec.NewTurnRepository(db, nil)
	ctx := context.Background()
	design := finishedTurn("p1")
	design.DesignFeatures = []string{"F2", "F1", "F2"}
	bare := finishedTurn("p1")
	chat := finishedTurn("p1")
	chat.Flow = "interview"
	chat.DesignFeatures = []string{"F3"}

	if err := repo.RecordFinished(ctx, "o1", []spec.TurnRecord{design, bare, chat}); err != nil {
		t.Fatalf("RecordFinished: %v", err)
	}
	if row := getTurn(t, db, "o1", "p1", design.TurnID); row == nil || row.Summary != "F2 F1" {
		t.Fatalf("design row summary = %+v, want \"F2 F1\"", row)
	} else if got := spec.DesignedFeatures(row.Summary); len(got) != 2 || got[0] != "F2" || got[1] != "F1" {
		t.Fatalf("DesignedFeatures(%q) = %v, want [F2 F1]", row.Summary, got)
	}
	if row := getTurn(t, db, "o1", "p1", bare.TurnID); row == nil || row.Summary != "" || spec.DesignedFeatures(row.Summary) != nil {
		t.Fatalf("bare /design row = %+v, want an empty summary (every feature)", row)
	}
	if row := getTurn(t, db, "o1", "p1", chat.TurnID); row == nil || row.Summary != "" {
		t.Fatalf("non-design row = %+v, want an empty summary", row)
	}
}

// CompletedFlows lists one project's completed runs of one flow, the
// latest-FINISHED first: the run that last designed a feature is the one whose
// work landed last, whatever order the runs started in or arrived. Failed
// runs, other flows and other projects are left out, and limit caps the list.
func TestCompletedFlows_LatestFinishedFirst(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := spec.NewTurnRepository(db, nil)
	ctx := context.Background()
	base := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	at := func(rec spec.TurnRecord, start, finish time.Duration) spec.TurnRecord {
		rec.StartedAt, rec.FinishedAt = base.Add(start), base.Add(finish)
		return rec
	}
	long := at(finishedTurn("p1"), 0, 30*time.Minute)              // started first, finished last
	short := at(finishedTurn("p1"), 5*time.Minute, 10*time.Minute) // started later, finished first
	oldest := at(finishedTurn("p1"), -time.Hour, -50*time.Minute)
	failed := at(finishedTurn("p1"), 0, time.Hour)
	failed.Status = "failed"
	other := at(finishedTurn("p1"), 0, time.Hour)
	other.Flow = "start"
	elsewhere := at(finishedTurn("p2"), 0, time.Hour)

	if err := repo.RecordFinished(ctx, "o1", []spec.TurnRecord{short, failed, oldest, long, other, elsewhere}); err != nil {
		t.Fatalf("RecordFinished: %v", err)
	}
	// A row the in-process engine wrote has no finished_at: it sorts by
	// created_at, here between the two runs above.
	legacy := spec.AgentTurn{
		ID: uuid.NewString(), OrgID: "o1", ProjectID: "p1", ConversationID: uuid.NewString(),
		Flow: "design", BaseRef: "cccccccccccccccccccccccccccccccccccccccc", Status: "completed",
		StartedAt: base.Add(20 * time.Minute), CreatedAt: base.Add(20 * time.Minute),
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	runs, err := repo.CompletedFlows(ctx, "o1", "p1", "design", 10)
	if err != nil {
		t.Fatalf("CompletedFlows: %v", err)
	}
	var ids []string
	for _, r := range runs {
		ids = append(ids, r.ID)
	}
	if want := []string{long.TurnID, legacy.ID, short.TurnID, oldest.TurnID}; !slices.Equal(ids, want) {
		t.Fatalf("CompletedFlows = %v, want %v (long, legacy, short, oldest)", ids, want)
	}
	if runs, err := repo.CompletedFlows(ctx, "o1", "p1", "design", 1); err != nil || len(runs) != 1 || runs[0].ID != long.TurnID {
		t.Fatalf("CompletedFlows limit 1 = (%v, %v), want the latest-finished run", runs, err)
	}
}
