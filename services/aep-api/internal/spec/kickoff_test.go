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

// The kickoff's decisions (#485), each of which was a live incident before it
// was a test: a dispatch that returned while its turn died read as a started
// interview; a Retry stood down on the corpse of a failed attempt and reported
// success; a claim whose process died sat "pending" forever.

package spec

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestKickoffStateOf(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	fresh := &SpecKickoff{Status: KickoffStatusPending, CreatedAt: now.Add(-time.Minute)}

	tests := []struct {
		name       string
		claim      *SpecKickoff
		standing   TurnStanding
		wantStatus string
		wantReason string
	}{
		{
			name:       "a running turn is a started interview",
			claim:      fresh,
			standing:   TurnStanding{Progressed: true},
			wantStatus: KickoffStatusStarted,
		},
		{
			// The whole point: dispatch returned and stamped `started`, then the
			// turn died seconds later with the agents service unreachable.
			name:       "a dead first turn beats a claim that says started",
			claim:      &SpecKickoff{Status: KickoffStatusStarted, CreatedAt: now.Add(-time.Minute)},
			standing:   TurnStanding{LastFailure: &AgentTurn{Reason: "dispatch-failed", Message: "dial tcp: connection refused"}},
			wantStatus: KickoffStatusFailed,
			wantReason: "dial tcp: connection refused",
		},
		{
			name:       "a turn that failed with nothing to say still gets a sentence",
			claim:      fresh,
			standing:   TurnStanding{LastFailure: &AgentTurn{}},
			wantStatus: KickoffStatusFailed,
			wantReason: "failed before it wrote anything",
		},
		{
			name:       "a recorded dispatch failure carries its own reason",
			claim:      &SpecKickoff{Status: KickoffStatusFailed, Reason: "the repo was still being created", CreatedAt: now},
			wantStatus: KickoffStatusFailed,
			wantReason: "the repo was still being created",
		},
		{
			name:       "a fresh claim with no turn yet is pending",
			claim:      fresh,
			wantStatus: KickoffStatusPending,
		},
		{
			// The attempt's process died mid-flight with nothing left to record
			// the failure; past its own deadline it cannot still be working.
			name:       "a claim older than the kickoff window is a failure the user can retry",
			claim:      &SpecKickoff{Status: KickoffStatusPending, CreatedAt: now.Add(-KickoffWindow - time.Minute)},
			wantStatus: KickoffStatusFailed,
			wantReason: "Retry",
		},
		{
			name:       "updated_at is the claim's liveness, so a retried claim is pending again",
			claim:      &SpecKickoff{Status: KickoffStatusPending, CreatedAt: now.Add(-2 * KickoffWindow), UpdatedAt: now},
			wantStatus: KickoffStatusPending,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := kickoffStateOf(tc.claim, tc.standing, now)
			if got.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", got.Status, tc.wantStatus)
			}
			if tc.wantReason != "" && !strings.Contains(got.Reason, tc.wantReason) {
				t.Fatalf("reason = %q, want it to contain %q", got.Reason, tc.wantReason)
			}
			if tc.wantStatus != KickoffStatusFailed && got.Reason != "" {
				t.Fatalf("a non-failed state must carry no reason, got %q", got.Reason)
			}
		})
	}
}

func TestRunKickoffOnce(t *testing.T) {
	t.Parallel()

	t.Run("stands down when a turn already progressed", func(t *testing.T) {
		t.Parallel()
		started := false
		err := runKickoffOnce(context.Background(), kickoffDeps{
			progressed: func(context.Context) (bool, error) { return true, nil },
			start:      func(context.Context) error { started = true; return nil },
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if started {
			t.Fatal("fired /start over an interview that is already in hands")
		}
	})

	t.Run("a project whose every turn died is NOT progressed, so it starts", func(t *testing.T) {
		t.Parallel()
		started := false
		err := runKickoffOnce(context.Background(), kickoffDeps{
			// Standing reports Progressed=false for a project with only failed
			// turns — the corpse a "has any turn row" check stood down on.
			progressed: func(context.Context) (bool, error) { return false, nil },
			start:      func(context.Context) error { started = true; return nil },
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !started {
			t.Fatal("stood down on a failed attempt instead of restarting the interview")
		}
	})

	t.Run("a lost one-active race is the same outcome as success", func(t *testing.T) {
		t.Parallel()
		err := runKickoffOnce(context.Background(), kickoffDeps{
			progressed: func(context.Context) (bool, error) { return false, nil },
			start:      func(context.Context) error { return &TurnInProgressError{ActiveTurnID: "t1"} },
		})
		if err != nil {
			t.Fatalf("a concurrent turn must not fail the kickoff, got %v", err)
		}
	})

	t.Run("a real failure is returned", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("boom")
		err := runKickoffOnce(context.Background(), kickoffDeps{
			progressed: func(context.Context) (bool, error) { return false, nil },
			start:      func(context.Context) error { return boom },
		})
		if !errors.Is(err, boom) {
			t.Fatalf("err = %v, want it to wrap boom", err)
		}
	})
}

func TestRunKickoffWaiting(t *testing.T) {
	t.Parallel()

	t.Run("waits out a repo that is still provisioning", func(t *testing.T) {
		t.Parallel()
		attempts := 0
		err := runKickoffWaiting(context.Background(), kickoffDeps{
			progressed: func(context.Context) (bool, error) { return false, nil },
			start: func(context.Context) error {
				attempts++
				if attempts < 3 {
					return ErrProjectRepoNotFound
				}
				return nil
			},
		}, time.Millisecond)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if attempts != 3 {
			t.Fatalf("attempts = %d, want 3", attempts)
		}
	})

	t.Run("a non-provisioning failure returns at once", func(t *testing.T) {
		t.Parallel()
		attempts := 0
		err := runKickoffWaiting(context.Background(), kickoffDeps{
			progressed: func(context.Context) (bool, error) { return false, nil },
			start:      func(context.Context) error { attempts++; return errors.New("no anthropic key") },
		}, time.Millisecond)
		if err == nil {
			t.Fatal("want an error")
		}
		if attempts != 1 {
			t.Fatalf("attempts = %d, want 1 — a real failure is not retried", attempts)
		}
	})

	t.Run("an expired deadline carries the last cause", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()
		err := runKickoffWaiting(ctx, kickoffDeps{
			progressed: func(context.Context) (bool, error) { return false, nil },
			start:      func(context.Context) error { return ErrSkillsRepoUnavailable },
		}, time.Millisecond)
		if !errors.Is(err, ErrSkillsRepoUnavailable) {
			t.Fatalf("err = %v, want the last cause preserved", err)
		}
	})
}

func TestKickoffFailureReason(t *testing.T) {
	t.Parallel()
	if got := KickoffFailureReason(nil); got != "" {
		t.Fatalf("no error must be no reason, got %q", got)
	}
	if got := KickoffFailureReason(ErrProjectRepoNotFound); !strings.Contains(got, "repository was still being created") {
		t.Fatalf("provisioning failures are named, got %q", got)
	}
	// Platform engineers are this console's users: the cause is worth more than
	// a tidy sentence with nothing in it.
	if got := KickoffFailureReason(errors.New("dial tcp 10.0.0.1:4000: i/o timeout")); !strings.Contains(got, "i/o timeout") {
		t.Fatalf("the cause must survive, got %q", got)
	}
	long := KickoffFailureReason(errors.New(strings.Repeat("x", 5000)))
	if len(long) > kickoffReasonLimit+100 {
		t.Fatalf("reason must be capped, got %d chars", len(long))
	}
}

// --- Retry, over the real service --------------------------------------------

// fakeKickoffs is the claim store in memory: one row, and a record of what the
// retry did to it.
type fakeKickoffs struct {
	row     *SpecKickoff
	claims  int
	rearms  int
	outcome []string
}

func (f *fakeKickoffs) TryClaim(context.Context, string, string) (bool, error) {
	f.claims++
	if f.row != nil {
		return false, nil
	}
	f.row = &SpecKickoff{Status: KickoffStatusPending, CreatedAt: time.Now()}
	return true, nil
}

func (f *fakeKickoffs) Get(context.Context, string, string) (*SpecKickoff, error) {
	return f.row, nil
}

func (f *fakeKickoffs) SetOutcome(_ context.Context, _, _, status, reason string) error {
	f.outcome = append(f.outcome, status)
	if f.row != nil {
		f.row.Status, f.row.Reason, f.row.UpdatedAt = status, reason, time.Now()
	}
	return nil
}

func (f *fakeKickoffs) Rearm(ctx context.Context, orgID, projectID string) error {
	f.rearms++
	return f.SetOutcome(ctx, orgID, projectID, KickoffStatusPending, "")
}

// fakeStandingTurns answers only what the kickoff asks of the turn store.
type fakeStandingTurns struct {
	TurnRepository
	standing TurnStanding
}

func (f fakeStandingTurns) Standing(context.Context, string, string) (TurnStanding, error) {
	return f.standing, nil
}

// fakeConversations answers ResolveCurrent so the attempt reaches StartTurn.
type fakeConversations struct{ ConversationRepository }

func (fakeConversations) ResolveCurrent(context.Context, string, string, string, string) (*ProjectConversation, error) {
	return &ProjectConversation{ID: "conv-1"}, nil
}

func (fakeConversations) IsCurrent(context.Context, string, string, string, string) (bool, error) {
	return true, nil
}

func retryService(kickoffs *fakeKickoffs, standing TurnStanding) *Service {
	// Repos is deliberately unwired: StartTurn then refuses with
	// ErrProjectRepoNotFound, which is exactly the "the repo is still cloning"
	// answer Retry is supposed to hand back rather than sleep on.
	return NewService(ServiceDeps{
		Kickoffs:      kickoffs,
		Turns:         fakeStandingTurns{standing: standing},
		Conversations: fakeConversations{},
	})
}

func TestRetryKickoff(t *testing.T) {
	t.Parallel()

	t.Run("stands down on an interview that is under way", func(t *testing.T) {
		t.Parallel()
		kickoffs := &fakeKickoffs{row: &SpecKickoff{Status: KickoffStatusStarted, CreatedAt: time.Now()}}
		state, err := retryService(kickoffs, TurnStanding{Progressed: true}).
			RetryKickoff(context.Background(), "o1", "p1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if state.Status != KickoffStatusStarted {
			t.Fatalf("status = %q, want started", state.Status)
		}
		if kickoffs.rearms != 0 || len(kickoffs.outcome) != 0 {
			t.Fatal("a running interview must be left completely alone")
		}
	})

	t.Run("stands down on an attempt that is still working", func(t *testing.T) {
		t.Parallel()
		kickoffs := &fakeKickoffs{row: &SpecKickoff{Status: KickoffStatusPending, CreatedAt: time.Now()}}
		state, err := retryService(kickoffs, TurnStanding{}).RetryKickoff(context.Background(), "o1", "p1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if state.Status != KickoffStatusPending {
			t.Fatalf("status = %q, want pending — a second click must not report failure for live work", state.Status)
		}
		if len(kickoffs.outcome) != 0 {
			t.Fatal("a pending attempt must not be re-stamped")
		}
	})

	t.Run("re-attempts over the corpse of a failed attempt", func(t *testing.T) {
		t.Parallel()
		// The claim exists and says failed; standing down on it is the bug that
		// made Retry report success while starting nothing.
		kickoffs := &fakeKickoffs{row: &SpecKickoff{
			Status: KickoffStatusFailed, Reason: "agents service down", CreatedAt: time.Now(),
		}}
		state, err := retryService(kickoffs, TurnStanding{}).RetryKickoff(context.Background(), "o1", "p1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if kickoffs.rearms != 1 {
			t.Fatalf("rearms = %d, want 1 — a spent claim must be re-opened", kickoffs.rearms)
		}
		// The attempt really ran (Repos is unwired, so it refuses), and its
		// answer is the BODY rather than an HTTP error.
		if state.Status != KickoffStatusFailed {
			t.Fatalf("status = %q, want failed", state.Status)
		}
		if !strings.Contains(state.Reason, "repository was still being created") {
			t.Fatalf("reason = %q, want the attempt's own cause", state.Reason)
		}
		if len(kickoffs.outcome) == 0 || kickoffs.outcome[len(kickoffs.outcome)-1] != KickoffStatusFailed {
			t.Fatalf("outcomes = %v, want the new attempt recorded", kickoffs.outcome)
		}
	})

	t.Run("claims a project that never had one", func(t *testing.T) {
		t.Parallel()
		kickoffs := &fakeKickoffs{}
		state, err := retryService(kickoffs, TurnStanding{}).RetryKickoff(context.Background(), "o1", "p1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if kickoffs.claims != 1 {
			t.Fatalf("claims = %d, want 1", kickoffs.claims)
		}
		if state.Status != KickoffStatusFailed {
			t.Fatalf("status = %q, want the attempt's answer", state.Status)
		}
	})

	t.Run("an unwired store is a wiring error, not a silent no-op", func(t *testing.T) {
		t.Parallel()
		_, err := NewService(ServiceDeps{}).RetryKickoff(context.Background(), "o1", "p1")
		if !errors.Is(err, ErrKickoffUnavailable) {
			t.Fatalf("err = %v, want ErrKickoffUnavailable", err)
		}
	})
}

// KickoffSpec is idempotent per project: create calls it unconditionally, and a
// spent claim must start nothing.
func TestKickoffSpec_ClaimIsSpentOnce(t *testing.T) {
	t.Parallel()
	kickoffs := &fakeKickoffs{row: &SpecKickoff{Status: KickoffStatusStarted, CreatedAt: time.Now()}}
	svc := retryService(kickoffs, TurnStanding{})
	if err := svc.KickoffSpec(context.Background(), "o1", "p1"); err != nil {
		t.Fatalf("a spent claim must be a quiet no-op, got %v", err)
	}
	if len(kickoffs.outcome) != 0 {
		t.Fatal("a spent claim must not be re-stamped")
	}
}
