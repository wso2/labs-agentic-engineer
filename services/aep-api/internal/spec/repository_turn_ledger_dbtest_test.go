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

// DB tier for the finished-turn ledger (record-turn-usage, 07 §12): the AE
// Studio tools pod hands over finished turns, the store keeps each once
// (idempotent on the turn id), stamps cost_usd at ingest (07 §7), and the
// readers that drive the status poll, the build gate's design baseline and
// kickoff idempotency see the rows unchanged.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

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

func countTurns(t *testing.T, repo spec.TurnRepository, org, project, id string) int {
	t.Helper()
	row, err := repo.Get(context.Background(), org, project, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row == nil {
		return 0
	}
	return 1
}

// A record lands as one row whatever the delivery count: the sender retries a
// batch whose answer it lost, and one POST may carry the same turn twice.
func TestRecordFinished_IdempotentOnTurnID(t *testing.T) {
	t.Parallel()
	repo := spec.NewTurnRepository(dbtest.New(t), nil)
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
	if n := countTurns(t, repo, "o1", "p1", rec.TurnID); n != 1 {
		t.Fatalf("rows for the turn = %d, want 1", n)
	}
	row, _ := repo.Get(ctx, "o1", "p1", rec.TurnID)
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
	repo := spec.NewTurnRepository(dbtest.New(t), nil)
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
	row, err := repo.Get(ctx, "o1", "p1", rec.TurnID)
	if err != nil || row == nil {
		t.Fatalf("Get = (%+v, %v)", row, err)
	}
	if row.OrgID != "o1" || row.ProjectID != "p1" || row.ConversationID != rec.ConversationID {
		t.Errorf("identity = (%q, %q, %q)", row.OrgID, row.ProjectID, row.ConversationID)
	}
	if row.Kind != spec.TurnKindPlan || row.Flow != "design" || row.Status != "failed" ||
		row.Reason != "agent-error" || row.Code != "provider_limit" {
		t.Errorf("outcome = kind %q flow %q status %q reason %q code %q", row.Kind, row.Flow, row.Status, row.Reason, row.Code)
	}
	// Until Task 3.21 drops use_case (NOT NULL), it is written from the kind.
	if row.UseCase != "task-plan" {
		t.Errorf("use_case = %q, want task-plan for a plan turn", row.UseCase)
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

// The use_case a kind writes until Task 3.21 drops the column (Q-9).
func TestRecordFinished_UseCaseFromKind(t *testing.T) {
	t.Parallel()
	repo := spec.NewTurnRepository(dbtest.New(t), nil)
	ctx := context.Background()
	for kind, want := range map[string]string{
		spec.TurnKindBrowser: "general",
		spec.TurnKindKickoff: "general",
		spec.TurnKindPlan:    "task-plan",
	} {
		rec := finishedTurn("p-" + kind)
		rec.Kind = kind
		if err := repo.RecordFinished(ctx, "o1", []spec.TurnRecord{rec}); err != nil {
			t.Fatalf("RecordFinished(%s): %v", kind, err)
		}
		row, _ := repo.Get(ctx, "o1", "p-"+kind, rec.TurnID)
		if row == nil || row.Kind != kind || row.UseCase != want {
			t.Errorf("%s: row = %+v, want use_case %q", kind, row, want)
		}
	}
}

// cost_usd is stamped at ingest from the (host, model) rate in force (07 §7);
// an unpriced host stays null.
func TestRecordFinished_StampsCost(t *testing.T) {
	t.Parallel()
	stamper := modelcost.NewStamper([]modelcost.ModelRate{
		{Host: modelconn.AnthropicHost, ModelID: "claude-sonnet-5", InputPerMTok: 2, OutputPerMTok: 10},
	})
	repo := spec.NewTurnRepository(dbtest.New(t), stamper)
	ctx := context.Background()

	priced := finishedTurn("p1") // $2 + $1
	unpriced := finishedTurn("p2")
	unpriced.ModelHost = modelconn.OllamaHost
	if err := repo.RecordFinished(ctx, "o1", []spec.TurnRecord{priced, unpriced}); err != nil {
		t.Fatalf("RecordFinished: %v", err)
	}
	row, _ := repo.Get(ctx, "o1", "p1", priced.TurnID)
	if row == nil || row.CostUsd == nil || *row.CostUsd != 3.00 {
		t.Fatalf("priced cost_usd = %+v, want 3.00", row)
	}
	row, _ = repo.Get(ctx, "o1", "p2", unpriced.TurnID)
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
	repo := spec.NewTurnRepository(dbtest.New(t), nil)
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
	repo := spec.NewTurnRepository(dbtest.New(t), nil)
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

// A marketplace turn has no project (C7): it is stored under project_id ''.
func TestRecordFinished_MarketplaceTurnHasNoProject(t *testing.T) {
	t.Parallel()
	repo := spec.NewTurnRepository(dbtest.New(t), nil)
	ctx := context.Background()
	rec := finishedTurn("")

	if err := repo.RecordFinished(ctx, "o1", []spec.TurnRecord{rec}); err != nil {
		t.Fatalf("RecordFinished: %v", err)
	}
	row, err := repo.Get(ctx, "o1", "", rec.TurnID)
	if err != nil || row == nil || row.ProjectID != "" {
		t.Fatalf("marketplace row = (%+v, %v), want project_id ''", row, err)
	}
}
