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
// Also covers TurnRepository.Standing — the kickoff read's one input, and the
// stand-down that stops a Retry firing over a turn that already progressed.

import (
	"context"
	"strings"
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

func TestTurnRepo_StandingSeparatesALiveTurnFromADeadOne(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := spec.NewTurnRepository(db, nil)
	ctx := context.Background()

	st, err := repo.Standing(ctx, "o1", "p1")
	if err != nil {
		t.Fatalf("Standing on empty: %v", err)
	}
	if st.Progressed || st.LastFailure != nil {
		t.Fatalf("Standing on a project with no turns = %+v, want the zero value", st)
	}

	row, err := repo.TryStart(ctx, &spec.AgentTurn{
		OrgID: "o1", ProjectID: "p1", ConversationID: "c1",
		UseCase: "general", BaseRef: "abc", Status: "running",
	})
	if err != nil {
		t.Fatalf("TryStart: %v", err)
	}
	if st, err = repo.Standing(ctx, "o1", "p1"); err != nil || !st.Progressed {
		t.Fatalf("Standing with a running row = (%+v, %v), want progressed", st, err)
	}

	// The turn dies. The ROW is still there — which is exactly what an
	// existence check could not tell apart, and why the console believed a
	// kickoff had succeeded on a project where nothing ran (#485 round 5).
	if _, err = repo.Finish(ctx, row.ID, spec.TurnTerminal{
		Status:  "failed",
		Reason:  "dispatch-failed",
		Message: "agents dispatch failed: connection refused",
	}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	st, err = repo.Standing(ctx, "o1", "p1")
	if err != nil {
		t.Fatalf("Standing after the failure: %v", err)
	}
	if st.Progressed {
		t.Fatal("Standing.Progressed = true with only a failed turn")
	}
	if st.LastFailure == nil || st.LastFailure.Message != "agents dispatch failed: connection refused" {
		t.Fatalf("LastFailure = %+v, want the dead turn with its message", st.LastFailure)
	}

	// A later turn that GETS somewhere outranks the corpse — a retry that
	// worked must not leave the card reading failed.
	if _, err = repo.TryStart(ctx, &spec.AgentTurn{
		OrgID: "o1", ProjectID: "p1", ConversationID: "c1",
		UseCase: "general", BaseRef: "abc", Status: "running",
	}); err != nil {
		t.Fatalf("second TryStart: %v", err)
	}
	if st, err = repo.Standing(ctx, "o1", "p1"); err != nil || !st.Progressed {
		t.Fatalf("Standing after a retry = (%+v, %v), want progressed", st, err)
	}
	if st.LastFailure != nil {
		t.Fatalf("LastFailure = %+v, want nil once something progressed", st.LastFailure)
	}

	// The tenant scope is (org, project) — a neighbor sees nothing.
	if st, err = repo.Standing(ctx, "o2", "p1"); err != nil || st.Progressed || st.LastFailure != nil {
		t.Fatalf("Standing across orgs = (%+v, %v), want the zero value", st, err)
	}
}

// The live failure, reproduced against Postgres end to end (#485 round 5):
// `docker stop aep-agents`, create a project — aep-api created the turn, wrote
// `started` on the claim, and the turn then failed asynchronously. The claim
// never moved again, so every read of it said the kickoff had succeeded: no
// error on the Spec card, no Retry, a spec view that never filled in.
//
// This is the state transition that must now flip the card, exercised through
// the real repositories rather than fakes, because the whole bug was the gap
// between what the claim row said and what the turn row did.
func TestKickoff_ADeadFirstRunTurnReadsAsAFailedKickoff(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	kickoffs := spec.NewKickoffRepository(db)
	turns := spec.NewTurnRepository(db, nil)
	svc := spec.NewService(spec.ServiceDeps{Kickoffs: kickoffs, Turns: turns})
	ctx := context.Background()

	// --- create: the claim is taken and the turn is dispatched.
	if won, err := kickoffs.TryClaim(ctx, "o1", "p1"); err != nil || !won {
		t.Fatalf("TryClaim = (%v, %v), want a win", won, err)
	}
	row, err := turns.TryStart(ctx, &spec.AgentTurn{
		OrgID: "o1", ProjectID: "p1", ConversationID: "c1",
		UseCase: "general", BaseRef: "abc", Status: "running",
	})
	if err != nil {
		t.Fatalf("TryStart: %v", err)
	}
	// StartTurn returned, so the attempt recorded success. It is the last
	// thing that will ever write this row.
	if err = kickoffs.SetOutcome(ctx, "o1", "p1", spec.KickoffStatusStarted, ""); err != nil {
		t.Fatalf("SetOutcome: %v", err)
	}
	state, err := svc.Kickoff(ctx, "o1", "p1")
	if err != nil {
		t.Fatalf("Kickoff while running: %v", err)
	}
	if state.Status != spec.KickoffStatusStarted {
		t.Fatalf("status = %q while the turn runs, want started", state.Status)
	}

	// --- seconds later: the agents service is down, and the turn dies.
	if _, err = turns.Finish(ctx, row.ID, spec.TurnTerminal{
		Status:  "failed",
		Reason:  "dispatch-failed",
		Message: "agents dispatch failed: Post \"http://aep-agents:4000/turns\": dial tcp: connect: connection refused",
	}); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	state, err = svc.Kickoff(ctx, "o1", "p1")
	if err != nil {
		t.Fatalf("Kickoff after the turn died: %v", err)
	}
	if state.Status != spec.KickoffStatusFailed {
		t.Fatalf("status = %q after the turn died, want failed", state.Status)
	}
	if !strings.Contains(state.Reason, "connection refused") {
		t.Fatalf("reason = %q, want the turn's own cause", state.Reason)
	}

	// Derived, not written back: the claim is untouched, so the next real
	// outcome (a Retry's) still wins. No backfill, no sweep.
	claim, err := kickoffs.Get(ctx, "o1", "p1")
	if err != nil || claim == nil {
		t.Fatalf("Get = (%+v, %v), want the claim", claim, err)
	}
	if claim.Status != spec.KickoffStatusStarted {
		t.Fatalf("claim status = %q, want it left at started", claim.Status)
	}

	// --- Retry: a new turn attaches, and the card goes back to working.
	if _, err = turns.TryStart(ctx, &spec.AgentTurn{
		OrgID: "o1", ProjectID: "p1", ConversationID: "c1",
		UseCase: "general", BaseRef: "abc", Status: "running",
	}); err != nil {
		t.Fatalf("retry TryStart: %v", err)
	}
	if state, err = svc.Kickoff(ctx, "o1", "p1"); err != nil {
		t.Fatalf("Kickoff after the retry: %v", err)
	}
	if state.Status != spec.KickoffStatusStarted {
		t.Fatalf("status = %q after a retry attached, want started", state.Status)
	}
}
