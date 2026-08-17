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

// DB tier for the spec_kickoffs claim (#485): the composite-PK
// insert-on-conflict is what makes the auto-/start exactly-once per project —
// racing claimers must resolve to ONE winner (the #420 admission pattern).
// Also covers TurnRepository.HasAny, the kickoff's stand-down read.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/spec"
)

func TestKickoffRepo_ClaimIsExactlyOncePerProject(t *testing.T) {
	t.Parallel()
	repo := spec.NewKickoffRepository(dbtest.New(t))
	ctx := context.Background()

	won, err := repo.TryClaim(ctx, "o1", "p1")
	if err != nil {
		t.Fatalf("first TryClaim: %v", err)
	}
	if !won {
		t.Fatal("first claim must win")
	}
	again, err := repo.TryClaim(ctx, "o1", "p1")
	if err != nil {
		t.Fatalf("second TryClaim: %v", err)
	}
	if again {
		t.Fatal("second claim on the same project must lose")
	}

	// Scope is (org, project): another project — and the same project name
	// under another org — each get their own claim.
	if won, err = repo.TryClaim(ctx, "o1", "p2"); err != nil || !won {
		t.Fatalf("other-project claim = (%v, %v), want a win", won, err)
	}
	if won, err = repo.TryClaim(ctx, "o2", "p1"); err != nil || !won {
		t.Fatalf("other-org claim = (%v, %v), want a win", won, err)
	}
}

// Get is the read-only view of the same row — the project status's only
// evidence that a kickoff is coming before its turn exists (#485), and the
// source of the timestamp that bounds how long that evidence counts.
func TestKickoffRepo_GetReadsTheClaimWithoutTakingIt(t *testing.T) {
	t.Parallel()
	repo := spec.NewKickoffRepository(dbtest.New(t))
	ctx := context.Background()

	row, err := repo.Get(ctx, "o1", "unclaimed")
	if err != nil {
		t.Fatalf("Get on empty: %v", err)
	}
	if row != nil {
		t.Fatal("Get returned a row before any claim")
	}

	before := time.Now().Add(-time.Minute)
	if _, err = repo.TryClaim(ctx, "o1", "unclaimed"); err != nil {
		t.Fatalf("TryClaim: %v", err)
	}
	row, err = repo.Get(ctx, "o1", "unclaimed")
	if err != nil || row == nil {
		t.Fatalf("Get after the claim = (%v, %v), want the row", row, err)
	}
	if row.CreatedAt.Before(before) {
		t.Errorf("createdAt = %v, want the claim's own time (after %v)", row.CreatedAt, before)
	}
	// Reading must not consume: the kickoff's own claim is still spent, so a
	// later TryClaim still loses.
	if won, terr := repo.TryClaim(ctx, "o1", "unclaimed"); terr != nil || won {
		t.Fatalf("TryClaim after a read = (%v, %v), want a loss", won, terr)
	}
	// Same (org, project) scope as the claim itself.
	if row, err = repo.Get(ctx, "o2", "unclaimed"); err != nil || row != nil {
		t.Fatalf("Get across orgs = (%v, %v), want no row", row, err)
	}
}

// The claim carries the attempt's OUTCOME (#485): a bare claim reads the same
// whether the turn was created or the attempt died, which is exactly what left
// the console unable to say "couldn't start" — or to offer Retry.
func TestKickoffRepo_ClaimCarriesTheAttemptsOutcome(t *testing.T) {
	t.Parallel()
	repo := spec.NewKickoffRepository(dbtest.New(t))
	ctx := context.Background()

	if _, err := repo.TryClaim(ctx, "o1", "outcome"); err != nil {
		t.Fatalf("TryClaim: %v", err)
	}
	row, err := repo.Get(ctx, "o1", "outcome")
	if err != nil || row == nil {
		t.Fatalf("Get = (%v, %v), want the row", row, err)
	}
	if row.Status != spec.KickoffStatusPending {
		t.Fatalf("status = %q, want a fresh claim to be pending", row.Status)
	}

	const reason = "The agents service was unreachable."
	if err = repo.SetOutcome(ctx, "o1", "outcome", spec.KickoffStatusFailed, reason); err != nil {
		t.Fatalf("SetOutcome: %v", err)
	}
	if row, err = repo.Get(ctx, "o1", "outcome"); err != nil || row == nil {
		t.Fatalf("Get after SetOutcome = (%v, %v), want the row", row, err)
	}
	if row.Status != spec.KickoffStatusFailed || row.Reason != reason {
		t.Fatalf("row = %+v, want the failure and its reason", row)
	}
	if row.UpdatedAt.Before(row.CreatedAt) {
		t.Errorf("updatedAt = %v, want it moved to the outcome's time (created %v)", row.UpdatedAt, row.CreatedAt)
	}

	// Rearm is the user's Retry: back to pending, reason cleared. The claim is
	// still spent, so nothing else can start a second kickoff.
	if err = repo.Rearm(ctx, "o1", "outcome"); err != nil {
		t.Fatalf("Rearm: %v", err)
	}
	if row, err = repo.Get(ctx, "o1", "outcome"); err != nil || row == nil {
		t.Fatalf("Get after Rearm = (%v, %v), want the row", row, err)
	}
	if row.Status != spec.KickoffStatusPending || row.Reason != "" {
		t.Fatalf("row = %+v, want pending with no reason", row)
	}
	if won, terr := repo.TryClaim(ctx, "o1", "outcome"); terr != nil || won {
		t.Fatalf("TryClaim after a rearm = (%v, %v), want a loss", won, terr)
	}
}

// SetOutcome never invents a claim: recording an outcome for a project that
// never claimed one would let a lost race stamp somebody else's kickoff.
func TestKickoffRepo_SetOutcomeDoesNotCreateAClaim(t *testing.T) {
	t.Parallel()
	repo := spec.NewKickoffRepository(dbtest.New(t))
	ctx := context.Background()

	if err := repo.SetOutcome(ctx, "o1", "never-claimed", spec.KickoffStatusFailed, "boom"); err != nil {
		t.Fatalf("SetOutcome: %v", err)
	}
	row, err := repo.Get(ctx, "o1", "never-claimed")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row != nil {
		t.Fatalf("row = %+v, want none — SetOutcome must not claim", row)
	}
}

func TestKickoffRepo_RacingClaimersResolveToOneWinner(t *testing.T) {
	t.Parallel()
	repo := spec.NewKickoffRepository(dbtest.New(t))
	ctx := context.Background()

	const racers = 8
	var wg sync.WaitGroup
	wins := make(chan bool, racers)
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			won, err := repo.TryClaim(ctx, "o1", "raced")
			if err != nil {
				t.Errorf("TryClaim: %v", err)
				return
			}
			wins <- won
		}()
	}
	wg.Wait()
	close(wins)
	total := 0
	for won := range wins {
		if won {
			total++
		}
	}
	if total != 1 {
		t.Fatalf("winners = %d, want exactly 1", total)
	}
}

func TestTurnRepo_HasAnySeesRunningAndTerminalRows(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := spec.NewTurnRepository(db, nil)
	ctx := context.Background()

	has, err := repo.HasAny(ctx, "o1", "p1")
	if err != nil {
		t.Fatalf("HasAny on empty: %v", err)
	}
	if has {
		t.Fatal("HasAny = true on a project with no turns")
	}

	row, err := repo.TryStart(ctx, &spec.AgentTurn{
		OrgID: "o1", ProjectID: "p1", ConversationID: "c1",
		UseCase: "general", BaseRef: "abc", Status: "running",
	})
	if err != nil {
		t.Fatalf("TryStart: %v", err)
	}
	if has, err = repo.HasAny(ctx, "o1", "p1"); err != nil || !has {
		t.Fatalf("HasAny with a running row = (%v, %v), want true", has, err)
	}

	// Terminal rows still count — "did any turn EVER run" is the question.
	if _, err = repo.Finish(ctx, row.ID, spec.TurnTerminal{Status: "completed"}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if has, err = repo.HasAny(ctx, "o1", "p1"); err != nil || !has {
		t.Fatalf("HasAny with a terminal row = (%v, %v), want true", has, err)
	}

	// The tenant scope is (org, project) — a neighbor sees nothing.
	if has, err = repo.HasAny(ctx, "o2", "p1"); err != nil || has {
		t.Fatalf("HasAny across orgs = (%v, %v), want false", has, err)
	}
}
