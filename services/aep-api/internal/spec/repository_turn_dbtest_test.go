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

// DB tier for the agent_turns store: the D18 partial-unique guard
// (ux_agent_turns_active), the guarded Finish, and the stale-heartbeat sweep —
// against a real migrated Postgres (dbtest; skipped under -short).

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/modelcost"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// decodePaths mirrors the repository's internal Paths decode (nil for
// empty/invalid) so this black-box test can assert on the persisted JSON array
// without exporting the production helper.
func decodePaths(raw string) []string {
	var p []string
	_ = json.Unmarshal([]byte(raw), &p)
	return p
}

func newTurn(org, project, conv, useCase string) *spec.AgentTurn {
	return &spec.AgentTurn{
		OrgID:          org,
		ProjectID:      project,
		ConversationID: conv,
		UseCase:        useCase,
		BaseRef:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SkillsRef:      "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
}

func TestTurnRepo_GuardAndLifecycle(t *testing.T) {
	t.Parallel()
	repo := spec.NewTurnRepository(dbtest.New(t), nil)
	ctx := context.Background()

	first, err := repo.TryStart(ctx, newTurn("o1", "p1", "c1", "requirements-chat"))
	if err != nil {
		t.Fatalf("first TryStart: %v", err)
	}
	if first.ID == "" || first.Status != "running" {
		t.Fatalf("first row = %+v", first)
	}

	// D18: a second start on the same project (any use case / conversation)
	// hits the partial unique index and returns the active row.
	active, err := repo.TryStart(ctx, newTurn("o1", "p1", "c2", "design-generate"))
	if err != spec.ErrTurnActive {
		t.Fatalf("second TryStart err = %v, want spec.ErrTurnActive", err)
	}
	if active == nil || active.ID != first.ID {
		t.Fatalf("active = %+v, want the first row", active)
	}
	// A different project is not blocked.
	if _, err := repo.TryStart(ctx, newTurn("o1", "p2", "c1", "requirements-chat")); err != nil {
		t.Fatalf("other-project TryStart: %v", err)
	}

	// GetActive / Get honour the (org, project) fence.
	got, err := repo.GetActive(ctx, "o1", "p1")
	if err != nil || got == nil || got.ID != first.ID {
		t.Fatalf("GetActive = (%+v, %v)", got, err)
	}
	if foreign, err := repo.Get(ctx, "o2", "p1", first.ID); err != nil || foreign != nil {
		t.Fatalf("cross-org Get = (%+v, %v), want (nil, nil)", foreign, err)
	}

	// Heartbeat bumps the running row.
	before := first.HeartbeatAt
	time.Sleep(15 * time.Millisecond)
	if err := repo.Heartbeat(ctx, first.ID); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	beat, _ := repo.Get(ctx, "o1", "p1", first.ID)
	if !beat.HeartbeatAt.After(before) {
		t.Fatalf("heartbeat not bumped: %v → %v", before, beat.HeartbeatAt)
	}

	// Finish is guarded on running and releases the guard.
	ok, err := repo.Finish(ctx, first.ID, spec.TurnTerminal{
		Status: "failed", Reason: "base-moved",
		Paths: []string{"specs/requirements/prd.md"}, Message: "conflict",
	})
	if err != nil || !ok {
		t.Fatalf("Finish = (%v, %v)", ok, err)
	}
	if again, err := repo.Finish(ctx, first.ID, spec.TurnTerminal{Status: "completed"}); err != nil || again {
		t.Fatalf("double Finish = (%v, %v), want (false, nil)", again, err)
	}
	done, _ := repo.Get(ctx, "o1", "p1", first.ID)
	if done.Status != "failed" || done.Reason != "base-moved" ||
		len(decodePaths(done.Paths)) != 1 {
		t.Fatalf("terminal row = %+v", done)
	}
	if active, _ := repo.GetActive(ctx, "o1", "p1"); active != nil {
		t.Fatalf("guard not released: %+v", active)
	}

	// LastTerminal keys on the conversation.
	last, err := repo.LastTerminal(ctx, "o1", "p1", "c1")
	if err != nil || last == nil || last.ID != first.ID {
		t.Fatalf("LastTerminal = (%+v, %v)", last, err)
	}
	if none, _ := repo.LastTerminal(ctx, "o1", "p1", "c9"); none != nil {
		t.Fatalf("foreign conversation LastTerminal = %+v", none)
	}

	// Guard released → a new turn on the project is admitted.
	if _, err := repo.TryStart(ctx, newTurn("o1", "p1", "c1", "requirements-chat")); err != nil {
		t.Fatalf("post-terminal TryStart: %v", err)
	}
}

// A turn the agents service ended with a coded error frame keeps the code
// and the provider's reset time on its row; a turn without them stores none
// (reset_at NULL, not a zero time).
func TestTurnRepo_FinishStoresTheErrorCode(t *testing.T) {
	t.Parallel()
	repo := spec.NewTurnRepository(dbtest.New(t), nil)
	ctx := context.Background()

	limited, err := repo.TryStart(ctx, newTurn("o1", "p1", "c1", "general"))
	if err != nil {
		t.Fatalf("TryStart: %v", err)
	}
	resetAt := time.Date(2026, 9, 26, 14, 5, 0, 0, time.UTC)
	if ok, err := repo.Finish(ctx, limited.ID, spec.TurnTerminal{
		Status: "failed", Reason: "agent-error", Message: "usage limit is reached",
		Code: spec.TurnErrorProviderLimit, ResetAt: &resetAt, Host: "ollama.com",
	}); err != nil || !ok {
		t.Fatalf("Finish = (%v, %v)", ok, err)
	}
	got, _ := repo.Get(ctx, "o1", "p1", limited.ID)
	if got.Code != spec.TurnErrorProviderLimit || got.ResetAt == nil || !got.ResetAt.Equal(resetAt) {
		t.Fatalf("row code/resetAt = %q/%v", got.Code, got.ResetAt)
	}

	plain, err := repo.TryStart(ctx, newTurn("o1", "p1", "c1", "general"))
	if err != nil {
		t.Fatalf("second TryStart: %v", err)
	}
	if ok, err := repo.Finish(ctx, plain.ID, spec.TurnTerminal{Status: "failed", Reason: "stream-died"}); err != nil || !ok {
		t.Fatalf("Finish = (%v, %v)", ok, err)
	}
	got, _ = repo.Get(ctx, "o1", "p1", plain.ID)
	if got.Code != "" || got.ResetAt != nil {
		t.Fatalf("uncoded row code/resetAt = %q/%v, want none", got.Code, got.ResetAt)
	}
}

func TestTurnRepo_SweepStale(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := spec.NewTurnRepository(db, nil)
	ctx := context.Background()

	stale, err := repo.TryStart(ctx, newTurn("o1", "p1", "c1", "requirements-chat"))
	if err != nil {
		t.Fatalf("TryStart stale: %v", err)
	}
	fresh, err := repo.TryStart(ctx, newTurn("o1", "p2", "c1", "requirements-chat"))
	if err != nil {
		t.Fatalf("TryStart fresh: %v", err)
	}
	// Backdate the stale row's heartbeat.
	if err := db.Model(&spec.AgentTurn{}).Where("id = ?", stale.ID).
		Update("heartbeat_at", time.Now().Add(-5*time.Minute)).Error; err != nil {
		t.Fatalf("backdate: %v", err)
	}

	swept, err := repo.SweepStale(ctx, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("SweepStale: %v", err)
	}
	if len(swept) != 1 || swept[0].ID != stale.ID ||
		swept[0].Status != "failed" || swept[0].Reason != "stream-died" {
		t.Fatalf("swept = %+v", swept)
	}
	// The fresh row is untouched; the stale project's guard is released.
	if row, _ := repo.Get(ctx, "o1", "p2", fresh.ID); row.Status != "running" {
		t.Fatalf("fresh row = %+v", row)
	}
	if _, err := repo.TryStart(ctx, newTurn("o1", "p1", "c1", "requirements-chat")); err != nil {
		t.Fatalf("post-sweep TryStart (guard must be released): %v", err)
	}
	// Idempotent: nothing left to sweep.
	if again, err := repo.SweepStale(ctx, time.Now().Add(-time.Minute)); err != nil || len(again) != 0 {
		t.Fatalf("second sweep = (%+v, %v)", again, err)
	}
}

// A new turn carries the host it was admitted on, and Finish prices its usage
// on (host, model): the same model on a host with no rate row stamps null, never
// another host's figure.
func TestTurnRepo_ModelHostPricesTheTurn(t *testing.T) {
	t.Parallel()
	stamper := modelcost.NewStamper([]modelcost.ModelRate{
		{Host: modelconn.AnthropicHost, ModelID: "claude-sonnet-5", InputPerMTok: 2, OutputPerMTok: 10},
	})
	repo := spec.NewTurnRepository(dbtest.New(t), stamper)
	ctx := context.Background()
	usage := &contracts.TokenUsage{InputTokens: 1_000_000, OutputTokens: 100_000, Model: "claude-sonnet-5"} // $2 + $1

	for _, tc := range []struct {
		project, host string
		wantCost      *float64
	}{
		{"p-anthropic", modelconn.AnthropicHost, ptr(3.00)},
		{"p-ollama", modelconn.OllamaHost, nil},
	} {
		turn := newTurn("o1", tc.project, "c1", "requirements-chat")
		turn.ModelHost = tc.host
		started, err := repo.TryStart(ctx, turn)
		if err != nil {
			t.Fatalf("TryStart(%s): %v", tc.project, err)
		}
		admitted, _ := repo.Get(ctx, "o1", tc.project, started.ID)
		if admitted == nil || admitted.ModelHost != tc.host {
			t.Fatalf("admitted row = %+v, want model_host %q", admitted, tc.host)
		}
		if ok, err := repo.Finish(ctx, started.ID, spec.TurnTerminal{Status: "completed", Usage: usage}); err != nil || !ok {
			t.Fatalf("Finish(%s) = (%v, %v)", tc.project, ok, err)
		}
		done, _ := repo.Get(ctx, "o1", tc.project, started.ID)
		switch {
		case tc.wantCost == nil && done.CostUsd != nil:
			t.Errorf("(%s, claude-sonnet-5) cost_usd = %v, want null", tc.host, *done.CostUsd)
		case tc.wantCost != nil && (done.CostUsd == nil || *done.CostUsd != *tc.wantCost):
			t.Errorf("(%s, claude-sonnet-5) cost_usd = %v, want %v", tc.host, done.CostUsd, *tc.wantCost)
		}
	}
}

func ptr(f float64) *float64 { return &f }

// The rotation check's read: the conversation's newest MEASURED turn. A later
// turn that left no measure (it failed before the agents service saved it)
// does not reset the conversation to empty, and other conversations are not
// read.
func TestTurnRepo_LastContextTokens(t *testing.T) {
	t.Parallel()
	repo := spec.NewTurnRepository(dbtest.New(t), nil)
	ctx := context.Background()

	if got, err := repo.LastContextTokens(ctx, "o1", "p1", "c1"); err != nil || got != nil {
		t.Fatalf("no turns: LastContextTokens = (%v, %v), want (nil, nil)", got, err)
	}
	finish := func(conv string, term spec.TurnTerminal) {
		t.Helper()
		started, err := repo.TryStart(ctx, newTurn("o1", "p1", conv, "general"))
		if err != nil {
			t.Fatalf("TryStart: %v", err)
		}
		if ok, err := repo.Finish(ctx, started.ID, term); err != nil || !ok {
			t.Fatalf("Finish = (%v, %v)", ok, err)
		}
	}
	tokens := func(n int64) *int64 { return &n }

	finish("c1", spec.TurnTerminal{Status: "completed", ContextTokens: tokens(40_000)})
	finish("c1", spec.TurnTerminal{Status: "completed", ContextTokens: tokens(70_000)})
	finish("c1", spec.TurnTerminal{Status: "failed", Reason: "stream-died"})
	finish("c2", spec.TurnTerminal{Status: "completed", ContextTokens: tokens(5_000)})

	got, err := repo.LastContextTokens(ctx, "o1", "p1", "c1")
	if err != nil || got == nil || *got != 70_000 {
		t.Fatalf("LastContextTokens(c1) = (%v, %v), want 70000", got, err)
	}
	if got, _ := repo.LastContextTokens(ctx, "o1", "p1", "c2"); got == nil || *got != 5_000 {
		t.Fatalf("LastContextTokens(c2) = %v, want 5000", got)
	}
}

// The roll-up's host: kept while every turn that spent tokens was billed by
// the same host, "" once they mix. A turn that captured nothing has no say.
func TestTurnRepo_SumUsageByProjectHost(t *testing.T) {
	t.Parallel()
	repo := spec.NewTurnRepository(dbtest.New(t), nil)
	ctx := context.Background()
	spent := &contracts.TokenUsage{InputTokens: 100, OutputTokens: 10, Model: "m"}

	run := func(project, host string, usage *contracts.TokenUsage) {
		t.Helper()
		turn := newTurn("o1", project, "c1", "general")
		turn.ModelHost = host
		started, err := repo.TryStart(ctx, turn)
		if err != nil {
			t.Fatalf("TryStart(%s): %v", project, err)
		}
		if ok, err := repo.Finish(ctx, started.ID, spec.TurnTerminal{Status: "completed", Usage: usage}); err != nil || !ok {
			t.Fatalf("Finish(%s) = (%v, %v)", project, ok, err)
		}
	}
	run("p-one", modelconn.OllamaHost, spent)
	run("p-one", modelconn.OllamaHost, spent)
	run("p-one", modelconn.AnthropicHost, nil) // captured nothing
	run("p-mixed", modelconn.OllamaHost, spent)
	run("p-mixed", modelconn.AnthropicHost, spent)

	got, err := repo.SumUsageByProject(ctx, "o1")
	if err != nil {
		t.Fatalf("SumUsageByProject: %v", err)
	}
	if h := got["p-one"].Host; h != modelconn.OllamaHost {
		t.Errorf("p-one host = %q, want %q", h, modelconn.OllamaHost)
	}
	if h := got["p-mixed"].Host; h != "" {
		t.Errorf("p-mixed host = %q, want \"\" (mixed hosts)", h)
	}
}
