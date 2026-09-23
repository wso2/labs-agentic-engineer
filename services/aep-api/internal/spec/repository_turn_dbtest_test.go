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

	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
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

// The prototype staleness baseline (#817) is the newest successful prototype
// GENERATION. Walked as the Outdated flag sees it — the design as it stood at
// the baseline's base commit against the design now: a design change marks the
// prototypes outdated, a feedback batch after it (a revision turn) must NOT
// clear that, and a regeneration does.
func TestTurnRepo_PrototypeBaselineSkipsFeedbackRevisions(t *testing.T) {
	t.Parallel()
	repo := spec.NewTurnRepository(dbtest.New(t), nil)
	ctx := context.Background()

	designAt := map[string]string{} // base commit → the design fingerprint there
	land := func(baseRef, design string, revision bool) {
		t.Helper()
		designAt[baseRef] = design
		turn := newTurn("o1", "p1", "c1", "general")
		turn.Flow, turn.Revision, turn.BaseRef = "prototype", revision, baseRef
		row, err := repo.TryStart(ctx, turn)
		if err != nil {
			t.Fatalf("TryStart: %v", err)
		}
		if ok, err := repo.Finish(ctx, row.ID, spec.TurnTerminal{Status: "completed"}); err != nil || !ok {
			t.Fatalf("Finish = (%v, %v)", ok, err)
		}
	}
	outdated := func(designNow string) bool {
		t.Helper()
		base, err := repo.NewestCompletedDerivation(ctx, "o1", "p1", "prototype")
		if err != nil || base == nil {
			t.Fatalf("baseline = (%v, %v)", base, err)
		}
		return designAt[base.BaseRef] != designNow
	}

	land("gen-1", "design-v1", false)
	if outdated("design-v1") {
		t.Fatal("a fresh generation read as outdated")
	}
	if !outdated("design-v2") {
		t.Fatal("a design change after the generation did not read as outdated")
	}
	land("feedback-1", "design-v2", true)
	if !outdated("design-v2") {
		t.Fatal("a feedback turn became the baseline and cleared Outdated without regenerating")
	}
	land("gen-2", "design-v2", false)
	if outdated("design-v2") {
		t.Fatal("regenerating did not clear Outdated")
	}
}
