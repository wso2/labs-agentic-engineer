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

package spec

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The kickoff's retry loop, decision by decision. The seams are the three
// kickoffDeps funcs; the full StartTurn path is covered by its own tests.

func TestRunKickoff_StartsWhenNoTurnEverRan(t *testing.T) {
	t.Parallel()
	started := 0
	err := runKickoff(context.Background(), kickoffDeps{
		hasAnyTurn: func(context.Context) (bool, error) { return false, nil },
		start:      func(context.Context) error { started++; return nil },
		interval:   time.Millisecond,
	})
	if err != nil {
		t.Fatalf("runKickoff: %v", err)
	}
	if started != 1 {
		t.Fatalf("starts = %d, want 1", started)
	}
}

// A user who typed anything before the kickoff got there owns the interview —
// an auto-/start over an open exchange is the #432 bug class (an unanswered
// question reads a fresh /start as the skip valve), so the kickoff stands down.
func TestRunKickoff_StandsDownWhenATurnAlreadyRan(t *testing.T) {
	t.Parallel()
	err := runKickoff(context.Background(), kickoffDeps{
		hasAnyTurn: func(context.Context) (bool, error) { return true, nil },
		start: func(context.Context) error {
			t.Fatal("start must not fire when a turn already ran")
			return nil
		},
		interval: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("runKickoff: %v", err)
	}
}

// Repo (and skills-repo) provisioning finishes after create returns; the loop
// waits it out and re-checks for a user-started turn on every pass.
func TestRunKickoff_RetriesWhileTheRepoProvisions(t *testing.T) {
	t.Parallel()
	checks, attempts := 0, 0
	err := runKickoff(context.Background(), kickoffDeps{
		hasAnyTurn: func(context.Context) (bool, error) { checks++; return false, nil },
		start: func(context.Context) error {
			attempts++
			if attempts < 3 {
				return ErrProjectRepoNotFound
			}
			return nil
		},
		interval: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("runKickoff: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if checks != 3 {
		t.Fatalf("prior-turn checks = %d, want one per attempt (3)", checks)
	}
}

func TestRunKickoff_RetriesWhileTheSkillsRepoProvisions(t *testing.T) {
	t.Parallel()
	attempts := 0
	err := runKickoff(context.Background(), kickoffDeps{
		hasAnyTurn: func(context.Context) (bool, error) { return false, nil },
		start: func(context.Context) error {
			attempts++
			if attempts == 1 {
				return ErrSkillsRepoUnavailable
			}
			return nil
		},
		interval: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("runKickoff: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

// Losing the D18 one-active race means a turn is running — the interview is
// in hands, which is the kickoff's goal state, not a failure.
func TestRunKickoff_TreatsAConcurrentTurnAsDone(t *testing.T) {
	t.Parallel()
	err := runKickoff(context.Background(), kickoffDeps{
		hasAnyTurn: func(context.Context) (bool, error) { return false, nil },
		start: func(context.Context) error {
			return &TurnInProgressError{ActiveTurnID: "t-1"}
		},
		interval: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("runKickoff: %v", err)
	}
}

func TestRunKickoff_SurfacesNonProvisioningFailures(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	attempts := 0
	err := runKickoff(context.Background(), kickoffDeps{
		hasAnyTurn: func(context.Context) (bool, error) { return false, nil },
		start:      func(context.Context) error { attempts++; return boom },
		interval:   time.Millisecond,
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry on a real failure)", attempts)
	}
}

func TestRunKickoff_GivesUpWhenTheDeadlineExpires(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := runKickoff(ctx, kickoffDeps{
		hasAnyTurn: func(context.Context) (bool, error) { return false, nil },
		start:      func(context.Context) error { return ErrProjectRepoNotFound },
		interval:   5 * time.Millisecond,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	if !errors.Is(err, ErrProjectRepoNotFound) {
		t.Fatalf("err = %v, want the last attempt's cause wrapped", err)
	}
}

// ---- KickoffSpec: the claim gate ------------------------------------------

type fakeKickoffClaims struct {
	claimed bool
	calls   int
	err     error
}

func (f *fakeKickoffClaims) TryClaim(context.Context, string, string) (bool, error) {
	f.calls++
	return f.claimed, f.err
}

// hasAnyOnlyTurnRepo panics on everything but HasAny — the only read a
// stood-down kickoff may make.
type hasAnyOnlyTurnRepo struct {
	TurnRepository
	hasAny bool
}

func (f *hasAnyOnlyTurnRepo) HasAny(context.Context, string, string) (bool, error) {
	return f.hasAny, nil
}

func TestKickoffSpec_SpentClaimIsANoOp(t *testing.T) {
	t.Parallel()
	claims := &fakeKickoffClaims{claimed: false}
	s := NewService(ServiceDeps{
		Kickoffs: claims,
		// Fully wired, and the stubs panic on use: a spent claim must return
		// without touching either store.
		Conversations: &memConvRepoStub{},
		Turns:         &neverCalledTurnRepo{},
	})
	if err := s.KickoffSpec(context.Background(), "acme", "shop"); err != nil {
		t.Fatalf("KickoffSpec: %v", err)
	}
	if claims.calls != 1 {
		t.Fatalf("claim attempts = %d, want 1", claims.calls)
	}
}

func TestKickoffSpec_WonClaimStandsDownOnAPriorTurn(t *testing.T) {
	t.Parallel()
	s := NewService(ServiceDeps{
		Kickoffs:      &fakeKickoffClaims{claimed: true},
		Turns:         &hasAnyOnlyTurnRepo{hasAny: true},
		Conversations: &memConvRepoStub{},
	})
	if err := s.KickoffSpec(context.Background(), "acme", "shop"); err != nil {
		t.Fatalf("KickoffSpec: %v", err)
	}
}

// neverCalledTurnRepo panics on every method — a call is a test bug.
type neverCalledTurnRepo struct{ TurnRepository }

// A store the kickoff needs but was not wired is a composition-root bug. It
// must be REFUSED, not discovered halfway through: the projects domain runs
// this on a detached goroutine, where a nil-store panic takes the process down
// instead of logging the best-effort failure it was promised.
func TestKickoffSpec_RefusesWhenUnwired(t *testing.T) {
	t.Parallel()
	for name, deps := range map[string]ServiceDeps{
		"nothing wired": {},
		"no claim store": {
			Conversations: &memConvRepoStub{},
			Turns:         &neverCalledTurnRepo{},
		},
		"no conversation store": {
			Kickoffs: &fakeKickoffClaims{claimed: true},
			Turns:    &neverCalledTurnRepo{},
		},
		"no turn store": {
			Kickoffs:      &fakeKickoffClaims{claimed: true},
			Conversations: &memConvRepoStub{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := NewService(deps)
			if err := s.KickoffSpec(context.Background(), "acme", "shop"); !errors.Is(err, ErrKickoffUnavailable) {
				t.Fatalf("err = %v, want ErrKickoffUnavailable", err)
			}
		})
	}
}

// memConvRepoStub satisfies ConversationRepository for paths that never reach
// it; any call is a test bug.
type memConvRepoStub struct{ ConversationRepository }
