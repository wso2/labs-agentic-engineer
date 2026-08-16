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
