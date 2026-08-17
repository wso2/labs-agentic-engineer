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
	"fmt"
	"slices"
	"strings"
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
	// row is what Get returns — the claim's presence, status and age,
	// independent of whether THIS caller won it.
	row *SpecKickoff
	// The write side: what the attempt recorded, and how often a spent claim
	// was put back into pending.
	outcomes [][2]string // (status, reason) per SetOutcome
	rearms   int
}

func (f *fakeKickoffClaims) TryClaim(context.Context, string, string) (bool, error) {
	f.calls++
	return f.claimed, f.err
}

func (f *fakeKickoffClaims) Get(context.Context, string, string) (*SpecKickoff, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.row, nil
}

func (f *fakeKickoffClaims) SetOutcome(_ context.Context, _, _, status, reason string) error {
	f.outcomes = append(f.outcomes, [2]string{status, reason})
	return nil
}

func (f *fakeKickoffClaims) Rearm(context.Context, string, string) error {
	f.rearms++
	return nil
}

// claimRow builds a claim in `status`, last touched `age` ago.
func claimRow(status string, age time.Duration) *SpecKickoff {
	return &SpecKickoff{
		OrgID:     "acme",
		ProjectID: "shop",
		Status:    status,
		CreatedAt: time.Now().Add(-age),
		UpdatedAt: time.Now().Add(-age),
	}
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

// ---- Kickoff: what became of the server-side /start ------------------------
//
// The console renders three things from this that nothing else can answer:
// "starting…" for the seconds between the claim and the turn row, a NAMED
// failure with a Retry when the attempt died, and silence once a turn exists.

func TestKickoff_PendingWhileClaimedWithNoTurnYet(t *testing.T) {
	t.Parallel()
	s := NewService(ServiceDeps{
		Kickoffs: &fakeKickoffClaims{row: claimRow(KickoffStatusPending, 0)},
		Turns:    &hasAnyOnlyTurnRepo{hasAny: false},
	})
	state, err := s.Kickoff(context.Background(), "acme", "shop")
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if state.Status != KickoffStatusPending {
		t.Fatalf("status = %q, want pending while the claim has no turn yet", state.Status)
	}
}

// A row written before the status column exists reads as "" — the same
// in-progress meaning, so old projects never render a failure they never had.
func TestKickoff_PendingForARowWithNoStatus(t *testing.T) {
	t.Parallel()
	s := NewService(ServiceDeps{
		Kickoffs: &fakeKickoffClaims{row: claimRow("", time.Minute)},
		Turns:    &hasAnyOnlyTurnRepo{hasAny: false},
	})
	state, err := s.Kickoff(context.Background(), "acme", "shop")
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if state.Status != KickoffStatusPending {
		t.Fatalf("status = %q, want pending", state.Status)
	}
}

func TestKickoff_StartedOnceTheTurnExists(t *testing.T) {
	t.Parallel()
	// Even while the row still says pending: the turn settles it.
	s := NewService(ServiceDeps{
		Kickoffs: &fakeKickoffClaims{row: claimRow(KickoffStatusPending, 0)},
		Turns:    &hasAnyOnlyTurnRepo{hasAny: true},
	})
	state, err := s.Kickoff(context.Background(), "acme", "shop")
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if state.Status != KickoffStatusStarted {
		t.Fatalf("status = %q, want started once a turn row exists", state.Status)
	}
}

func TestKickoff_FailedCarriesTheRecordedReason(t *testing.T) {
	t.Parallel()
	row := claimRow(KickoffStatusFailed, time.Minute)
	row.Reason = "The agents service was unreachable."
	s := NewService(ServiceDeps{
		Kickoffs: &fakeKickoffClaims{row: row},
		Turns:    &hasAnyOnlyTurnRepo{hasAny: false},
	})
	state, err := s.Kickoff(context.Background(), "acme", "shop")
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if state.Status != KickoffStatusFailed || state.Reason != row.Reason {
		t.Fatalf("state = %+v, want failed carrying the row's reason", state)
	}
}

// A pending claim older than the kickoff's own deadline cannot still be
// working — its process died mid-attempt with nothing left to record the
// failure. Reading it as failed is what puts Retry in front of the user.
func TestKickoff_StalledPendingReadsAsFailed(t *testing.T) {
	t.Parallel()
	s := NewService(ServiceDeps{
		Kickoffs: &fakeKickoffClaims{row: claimRow(KickoffStatusPending, KickoffWindow+time.Minute)},
		Turns:    &hasAnyOnlyTurnRepo{hasAny: false},
	})
	state, err := s.Kickoff(context.Background(), "acme", "shop")
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if state.Status != KickoffStatusFailed || state.Reason == "" {
		t.Fatalf("state = %+v, want failed with a reason once the window has passed", state)
	}
}

func TestKickoff_NoneWithoutAClaim(t *testing.T) {
	t.Parallel()
	s := NewService(ServiceDeps{
		Kickoffs: &fakeKickoffClaims{},
		Turns:    &neverCalledTurnRepo{},
	})
	state, err := s.Kickoff(context.Background(), "acme", "shop")
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if state.Status != KickoffStatusNone {
		t.Fatalf("status = %q, want none with no claim", state.Status)
	}
}

// The unwired seam degrades to "no kickoff" rather than refusing: this feeds a
// polled status read, where an error would brick the whole overview.
func TestKickoff_UnwiredReportsNone(t *testing.T) {
	t.Parallel()
	state, err := NewService(ServiceDeps{}).Kickoff(context.Background(), "acme", "shop")
	if err != nil {
		t.Fatalf("Kickoff: %v", err)
	}
	if state.Status != KickoffStatusNone {
		t.Fatalf("status = %q, want none when the stores are not wired", state.Status)
	}
}

func TestKickoff_SurfacesTheClaimReadFailure(t *testing.T) {
	t.Parallel()
	s := NewService(ServiceDeps{
		Kickoffs: &fakeKickoffClaims{err: errors.New("db down")},
		Turns:    &neverCalledTurnRepo{},
	})
	if _, err := s.Kickoff(context.Background(), "acme", "shop"); err == nil {
		t.Fatal("err = nil, want the claim read's failure")
	}
}

// ---- The outcome the attempt records ---------------------------------------

func TestKickoffSpec_RecordsTheStartedOutcome(t *testing.T) {
	t.Parallel()
	claims := &fakeKickoffClaims{claimed: true}
	s := NewService(ServiceDeps{
		Kickoffs:      claims,
		Turns:         &hasAnyOnlyTurnRepo{hasAny: true}, // stands down: a turn exists
		Conversations: &memConvRepoStub{},
	})
	if err := s.KickoffSpec(context.Background(), "acme", "shop"); err != nil {
		t.Fatalf("KickoffSpec: %v", err)
	}
	if len(claims.outcomes) != 1 || claims.outcomes[0][0] != KickoffStatusStarted {
		t.Fatalf("outcomes = %v, want one started", claims.outcomes)
	}
}

// The failure the console has to be able to SAY. A reason is recorded with it —
// a bare "failed" would leave the card at "something went wrong".
func TestKickoffFailureReason_NamesTheProvisioningCases(t *testing.T) {
	t.Parallel()
	for name, err := range map[string]error{
		"repo":   fmt.Errorf("start: %w", ErrProjectRepoNotFound),
		"skills": fmt.Errorf("start: %w", ErrSkillsRepoUnavailable),
	} {
		t.Run(name, func(t *testing.T) {
			if got := KickoffFailureReason(err); got == "" || strings.Contains(got, "%!w") {
				t.Fatalf("reason = %q, want a sentence", got)
			}
		})
	}
}

// Anything else carries its cause: this console's users are the platform's own
// engineers, and "something went wrong" costs them the one useful detail.
func TestKickoffFailureReason_CarriesAnUnknownCause(t *testing.T) {
	t.Parallel()
	got := KickoffFailureReason(errors.New("dial tcp agents:8080: connection refused"))
	if !strings.Contains(got, "connection refused") {
		t.Fatalf("reason = %q, want the cause carried", got)
	}
}

func TestKickoffFailureReason_CapsTheCarriedCause(t *testing.T) {
	t.Parallel()
	got := KickoffFailureReason(errors.New(strings.Repeat("x", 5000)))
	if len(got) > kickoffReasonLimit+100 {
		t.Fatalf("reason length = %d, want it capped", len(got))
	}
}

// ---- Retry: the ONLY way a project gets a second kickoff -------------------
//
// The create-time kickoff deliberately does not retry forever on its own: an
// agents service that is down has nothing to retry into, and a turn that starts
// an hour later starts with nobody watching. So the user clicks, and this is
// what that click does.

func recordingRetryDeps(attemptErr error) (*retryDeps, *[]string) {
	log := &[]string{}
	d := retryDeps{
		claim:   func(context.Context) error { *log = append(*log, "claim"); return nil },
		rearm:   func(context.Context) error { *log = append(*log, "rearm"); return nil },
		attempt: func(context.Context) error { *log = append(*log, "attempt"); return attemptErr },
		record: func(_ context.Context, err error) {
			if err != nil {
				*log = append(*log, "record:failed")
				return
			}
			*log = append(*log, "record:started")
		},
	}
	return &d, log
}

func TestRunRetry_ReArmsAndAttemptsAfterAFailure(t *testing.T) {
	t.Parallel()
	d, log := recordingRetryDeps(nil)
	state, err := runRetry(context.Background(), KickoffState{Status: KickoffStatusFailed}, *d)
	if err != nil {
		t.Fatalf("runRetry: %v", err)
	}
	if state.Status != KickoffStatusStarted {
		t.Fatalf("status = %q, want started", state.Status)
	}
	if want := []string{"rearm", "attempt", "record:started"}; !slices.Equal(*log, want) {
		t.Fatalf("steps = %v, want %v", *log, want)
	}
}

// Idempotence, both halves: a project already interviewing and one whose
// kickoff is still working are both left ALONE. A second click must not start a
// second attempt, and must not report failure for work still in flight.
func TestRunRetry_IsANoOpWhileTheKickoffIsInHand(t *testing.T) {
	t.Parallel()
	for _, status := range []string{KickoffStatusStarted, KickoffStatusPending} {
		t.Run(status, func(t *testing.T) {
			d, log := recordingRetryDeps(nil)
			state, err := runRetry(context.Background(), KickoffState{Status: status}, *d)
			if err != nil {
				t.Fatalf("runRetry: %v", err)
			}
			if state.Status != status {
				t.Fatalf("status = %q, want %q unchanged", state.Status, status)
			}
			if len(*log) != 0 {
				t.Fatalf("steps = %v, want none", *log)
			}
		})
	}
}

// A project that never had a claim (created before the kickoff existed, or one
// whose claim never landed): claiming IS the retry.
func TestRunRetry_ClaimsWhenNothingWasEverClaimed(t *testing.T) {
	t.Parallel()
	d, log := recordingRetryDeps(nil)
	if _, err := runRetry(context.Background(), KickoffState{Status: KickoffStatusNone}, *d); err != nil {
		t.Fatalf("runRetry: %v", err)
	}
	if want := []string{"claim", "attempt", "record:started"}; !slices.Equal(*log, want) {
		t.Fatalf("steps = %v, want %v", *log, want)
	}
}

// The service is still down: the same failure comes back — as the ANSWER, not
// an error — so the card says why and the button is still there.
func TestRunRetry_ReturnsTheFailureAsState(t *testing.T) {
	t.Parallel()
	d, log := recordingRetryDeps(errors.New("dial tcp agents:8080: connection refused"))
	state, err := runRetry(context.Background(), KickoffState{Status: KickoffStatusFailed}, *d)
	if err != nil {
		t.Fatalf("runRetry: %v", err)
	}
	if state.Status != KickoffStatusFailed {
		t.Fatalf("status = %q, want failed", state.Status)
	}
	if !strings.Contains(state.Reason, "connection refused") {
		t.Fatalf("reason = %q, want the cause", state.Reason)
	}
	if want := []string{"rearm", "attempt", "record:failed"}; !slices.Equal(*log, want) {
		t.Fatalf("steps = %v, want %v", *log, want)
	}
}

func TestRetryKickoff_RefusesWhenUnwired(t *testing.T) {
	t.Parallel()
	if _, err := NewService(ServiceDeps{}).RetryKickoff(context.Background(), "acme", "shop"); !errors.Is(err, ErrKickoffUnavailable) {
		t.Fatalf("err = %v, want ErrKickoffUnavailable", err)
	}
}
