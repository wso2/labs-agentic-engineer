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

// DB tier for the spec_kickoffs claim and the turn Standing read (#485), on a
// real migrated Postgres (dbtest; skipped under -short). The claim's
// exactly-once IS the composite primary key, so an in-memory fake would test
// the fake.

import (
	"context"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/spec"
)

func TestKickoffRepo_ClaimedOncePerProject(t *testing.T) {
	t.Parallel()
	repo := spec.NewKickoffRepository(dbtest.New(t))
	ctx := context.Background()

	won, err := repo.TryClaim(ctx, "o1", "p1")
	if err != nil {
		t.Fatalf("first TryClaim: %v", err)
	}
	if !won {
		t.Fatal("the first claim must win")
	}
	again, err := repo.TryClaim(ctx, "o1", "p1")
	if err != nil {
		t.Fatalf("second TryClaim: %v", err)
	}
	if again {
		t.Fatal("a spent claim was handed out twice — the project would get two /start turns")
	}

	// Scope is (org, project): another project claims its own.
	other, err := repo.TryClaim(ctx, "o1", "p2")
	if err != nil || !other {
		t.Fatalf("other project TryClaim = %v, %v", other, err)
	}

	row, err := repo.Get(ctx, "o1", "p1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row == nil || row.Status != spec.KickoffStatusPending {
		t.Fatalf("fresh claim = %+v, want pending", row)
	}
	if missing, gerr := repo.Get(ctx, "o1", "never-created"); gerr != nil || missing != nil {
		t.Fatalf("a project with no claim must miss with (nil, nil), got %+v, %v", missing, gerr)
	}
}

func TestKickoffRepo_ConcurrentClaimsYieldOneWinner(t *testing.T) {
	t.Parallel()
	repo := spec.NewKickoffRepository(dbtest.New(t))
	ctx := context.Background()

	const racers = 8
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins int
	)
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			won, err := repo.TryClaim(ctx, "o1", "p1")
			if err != nil {
				t.Errorf("TryClaim: %v", err)
				return
			}
			if won {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("wins = %d, want exactly 1 — replicas racing a create must not each start an interview", wins)
	}
}

func TestKickoffRepo_OutcomeAndRearm(t *testing.T) {
	t.Parallel()
	repo := spec.NewKickoffRepository(dbtest.New(t))
	ctx := context.Background()

	if _, err := repo.TryClaim(ctx, "o1", "p1"); err != nil {
		t.Fatalf("TryClaim: %v", err)
	}
	if err := repo.SetOutcome(ctx, "o1", "p1", spec.KickoffStatusFailed, "the repo was still cloning"); err != nil {
		t.Fatalf("SetOutcome: %v", err)
	}
	row, err := repo.Get(ctx, "o1", "p1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.Status != spec.KickoffStatusFailed || row.Reason != "the repo was still cloning" {
		t.Fatalf("row = %+v, want the recorded failure", row)
	}

	// Rearm is what a Retry does to a spent claim: back to pending with the
	// stale reason cleared, so the card stops showing an error that is no
	// longer true.
	if err := repo.Rearm(ctx, "o1", "p1"); err != nil {
		t.Fatalf("Rearm: %v", err)
	}
	row, err = repo.Get(ctx, "o1", "p1")
	if err != nil {
		t.Fatalf("Get after rearm: %v", err)
	}
	if row.Status != spec.KickoffStatusPending || row.Reason != "" {
		t.Fatalf("row = %+v, want a cleared pending claim", row)
	}
	if row.UpdatedAt.Before(row.CreatedAt) {
		t.Fatalf("Rearm must move updated_at (the claim's liveness): %+v", row)
	}
}

func TestTurnRepo_Standing(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := spec.NewTurnRepository(db, nil)
	ctx := context.Background()

	empty, err := repo.Standing(ctx, "o1", "p1")
	if err != nil {
		t.Fatalf("Standing on a turn-less project: %v", err)
	}
	if empty.Progressed || empty.LastFailure != nil {
		t.Fatalf("standing = %+v, want nothing at all", empty)
	}

	// A turn that dies leaves the project un-progressed and names the cause —
	// which is what turns a claim that says `started` into a failed kickoff.
	first := newStandingTurn(t, repo, "o1", "p1")
	if _, err := repo.Finish(ctx, first.ID, spec.TurnTerminal{
		Status: "failed", Reason: "dispatch-failed", Message: "connection refused",
	}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	failed, err := repo.Standing(ctx, "o1", "p1")
	if err != nil {
		t.Fatalf("Standing after a failure: %v", err)
	}
	if failed.Progressed {
		t.Fatal("a project whose only turn died has not progressed")
	}
	if failed.LastFailure == nil || failed.LastFailure.Message != "connection refused" {
		t.Fatalf("LastFailure = %+v, want the dead turn's message", failed.LastFailure)
	}

	// A live turn settles it, whatever came before.
	newStandingTurn(t, repo, "o1", "p1")
	live, err := repo.Standing(ctx, "o1", "p1")
	if err != nil {
		t.Fatalf("Standing with a running turn: %v", err)
	}
	if !live.Progressed {
		t.Fatal("a running turn means the interview is in hands")
	}
	if live.LastFailure != nil {
		t.Fatal("an older failure says nothing once a turn is running")
	}
}

// newStandingTurn starts a running turn row for the standing tests.
func newStandingTurn(t *testing.T, repo spec.TurnRepository, orgID, projectID string) *spec.AgentTurn {
	t.Helper()
	row, err := repo.TryStart(context.Background(), &spec.AgentTurn{
		OrgID:          orgID,
		ProjectID:      projectID,
		ConversationID: "c1",
		UseCase:        "general",
		BaseRef:        "sha",
	})
	if err != nil {
		t.Fatalf("TryStart: %v", err)
	}
	return row
}
