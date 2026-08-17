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

// The BE-side `/start` kickoff (#485): EVERY new project gets its spec
// interview started server-side, so the turn is already narrating by the time
// the user reaches the spec view — the console injects nothing. The projects
// domain calls KickoffSpec (through its narrow specKickoff port) right after
// stamping the descriptor; the instruction is the bare `/start` token, so the
// idea rides in from the descriptor exactly as it does for a user-typed
// command (start_command.go — one expansion path, not two). With no idea
// captured the expansion attaches none and the start skill opens by asking
// what the user is building — the conversation begins either way.
//
// Exactly-once is layered:
//   - spec_kickoffs claim (TryClaim) — one AUTO kickoff per project, ever.
//   - TurnStanding.Progressed — a user who beat the kickoff to the first turn
//     owns the interview; firing `/start` over it is the #432 bug class
//     server-side (an unanswered question reads a fresh `/start` as the skip
//     valve).
//   - the D18 one-active guard — a turn racing the kickoff's StartTurn makes
//     it lose cleanly (TurnInProgressError → treated as done).
//
// What the console is told is the TURN's outcome, not the dispatch's: an
// attempt that returns cleanly and whose turn dies seconds later (the agents
// service unreachable) is a failed kickoff, and says so. See Service.Kickoff.

package spec

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/text"
)

// ErrKickoffUnavailable means the service was assembled without one of the
// stores the kickoff needs (ServiceDeps.Kickoffs / Conversations / Turns) — a
// wiring bug, not a runtime condition; production always wires all three.
var ErrKickoffUnavailable = errors.New("spec kickoff store not configured")

// kickoffRetryInterval paces the wait for the freshly-created repo: project
// create returns while the repo still provisions (clone is async), and
// StartTurn refuses a not-ready repo with ErrProjectRepoNotFound. The caller's
// ctx deadline bounds the total wait.
const kickoffRetryInterval = 3 * time.Second

// KickoffSpec starts the project's first `/start` turn server-side. Idempotent
// per project (the spec_kickoffs claim); a no-op when a turn already ran or is
// running (a user got there first). Retries while the repo or the org skills
// repo is still provisioning, until ctx expires.
//
// The outcome is RECORDED on the claim either way, which is what lets the
// console say "starting…" or "couldn't start: <why>" instead of showing a
// project that looks busy forever.
func (s *Service) KickoffSpec(ctx context.Context, orgID, projectID string) error {
	// `turns` included: the claimed path calls HasAny on it, and the caller
	// runs this on a detached goroutine — a nil there is a process crash, not
	// the logged best-effort failure the projects domain is promised.
	if s.kickoffs == nil || s.conversations == nil || s.turns == nil {
		return ErrKickoffUnavailable
	}
	claimed, err := s.kickoffs.TryClaim(ctx, orgID, projectID)
	if err != nil {
		return fmt.Errorf("claim kickoff: %w", err)
	}
	if !claimed {
		return nil // already kicked off — the claim is spent
	}
	err = runKickoff(ctx, s.kickoffAttempt(orgID, projectID))
	s.recordKickoffOutcome(ctx, orgID, projectID, err)
	return err
}

// kickoffAttempt is the one attempt, shared by the create-time kickoff and the
// user's Retry so both start the interview identically.
func (s *Service) kickoffAttempt(orgID, projectID string) kickoffDeps {
	return kickoffDeps{
		progressed: func(c context.Context) (bool, error) {
			st, err := s.turns.Standing(c, orgID, projectID)
			return st.Progressed, err
		},
		start: func(c context.Context) error {
			// The kickoff mints (or converges on) the project's current thread
			// — the same thread the console resolves — so the interview and the
			// chat around it share one history. createdBy stays empty: there is
			// no acting user on a create-time kickoff.
			row, rerr := s.conversations.ResolveCurrent(c, orgID, projectID, useCaseGeneral, "")
			if rerr != nil {
				return fmt.Errorf("resolve current conversation: %w", rerr)
			}
			_, terr := s.StartTurn(c, orgID, projectID, TurnInput{
				ConversationID: row.ID,
				Instruction:    startCommand,
			})
			return terr
		},
		interval: kickoffRetryInterval,
	}
}

// recordKickoffOutcome stamps the attempt's result on the claim. Detached from
// the caller's ctx: the commonest failure IS the deadline expiring, and a write
// on the dead context would lose exactly the outcome the user needs to see.
// Best-effort — the attempt's own error is what the caller acts on.
func (s *Service) recordKickoffOutcome(ctx context.Context, orgID, projectID string, attemptErr error) {
	status, reason := KickoffStatusStarted, ""
	if attemptErr != nil {
		status, reason = KickoffStatusFailed, KickoffFailureReason(attemptErr)
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), kickoffOutcomeWriteTimeout)
	defer cancel()
	if err := s.kickoffs.SetOutcome(wctx, orgID, projectID, status, reason); err != nil {
		slog.ErrorContext(wctx, "failed to record spec kickoff outcome",
			"org", orgID, "project", projectID, "status", status, "error", err)
	}
}

// KickoffWindow bounds how long a claim without a turn still counts as a
// kickoff IN PROGRESS. It is the kickoff's own lifetime — the deadline the
// caller gives runKickoff (projects.specKickoffTimeout reads it from here, so
// the two cannot drift). Past it the attempt cannot still be running, so a
// claim with no outcome is read as a failure the user can retry.
const KickoffWindow = 10 * time.Minute

// Kickoff statuses (SpecKickoff.Status and the contract's enum).
const (
	// KickoffStatusNone is not a stored value: it is what the read reports for
	// a project with no claim at all.
	KickoffStatusNone    = "none"
	KickoffStatusPending = "pending"
	KickoffStatusFailed  = "failed"
	KickoffStatusStarted = "started"
)

// kickoffOutcomeWriteTimeout bounds the detached outcome write.
const kickoffOutcomeWriteTimeout = 5 * time.Second

// kickoffReasonLimit caps the carried cause — a reason is a sentence for a
// card, not a stack trace.
const kickoffReasonLimit = 300

const kickoffStalledReason = "The spec interview did not start, and the attempt is no longer running. Retry to start it."

// KickoffState is what became of a project's server-side kickoff.
type KickoffState struct {
	// Status is one of the KickoffStatus* values.
	Status string
	// Reason is the user-facing failure sentence; empty unless Status is failed.
	Reason string
}

// Kickoff reports what became of this project's server-side `/start` (#485).
//
// The console renders three states from it that nothing else can answer:
// "starting…" for the seconds between the claim and the turn row, a NAMED
// failure with a Retry when the interview died, and silence once a turn is
// under way (from there the turn reads say everything). Deriving those from
// the running turn, the thread and the spec files reads "untouched project" in
// the first case and forever in the second.
//
// The failure half keys off the first-run turn's OUTCOME, not the dispatch's.
// The two are different events: aep-api creates the turn and stamps the claim
// `started` synchronously, then the turn runs on a detached goroutine and can
// fail seconds later with the agents service down. Reading the claim alone
// (and treating any turn row as success) told the console a kickoff had
// succeeded on projects where the interview never ran — no error, no Retry, a
// spec view that never filled in.
//
// Only ever asked for a project with no spec (populateStages gates it), which
// is what makes "no turn progressed" mean "nothing came of this".
//
// Unwired stores answer `none` rather than refusing: this rides a polled status
// read, and the nil seam is a test assembly.
func (s *Service) Kickoff(ctx context.Context, orgID, projectID string) (KickoffState, error) {
	if s.kickoffs == nil || s.turns == nil {
		return KickoffState{Status: KickoffStatusNone}, nil
	}
	claim, err := s.kickoffs.Get(ctx, orgID, projectID)
	if err != nil {
		return KickoffState{}, fmt.Errorf("read kickoff claim: %w", err)
	}
	if claim == nil {
		return KickoffState{Status: KickoffStatusNone}, nil
	}
	// What the TURN did settles it, whatever the claim row says. A turn
	// running or completed means the interview is under way (the kickoff's, or
	// a user's that beat it) — and the claim being `started` means only that
	// dispatch returned, so it cannot answer this on its own.
	standing, err := s.turns.Standing(ctx, orgID, projectID)
	if err != nil {
		return KickoffState{}, fmt.Errorf("read turn standing: %w", err)
	}
	if standing.Progressed {
		return KickoffState{Status: KickoffStatusStarted}, nil
	}
	// The turn was created and then DIED — the agents service unreachable is
	// the ordinary way — and this read only runs for a project with no spec, so
	// nothing came of it. The claim says `started` and always will: the failure
	// happened asynchronously, long after the attempt that stamped it returned.
	// The turn's own outcome is the only honest answer, and it is what puts the
	// error and the Retry on the card. Derived, never written back — the next
	// attempt's real outcome still wins.
	if standing.LastFailure != nil {
		return KickoffState{
			Status: KickoffStatusFailed,
			Reason: kickoffTurnFailureReason(standing.LastFailure),
		}, nil
	}
	if claim.Status == KickoffStatusFailed {
		return KickoffState{Status: KickoffStatusFailed, Reason: claim.Reason}, nil
	}
	// Pending — including rows from before the status column, which read as ""
	// and are treated the same way. A pending claim older than the kickoff's own
	// deadline cannot still be working: its process died mid-attempt (a restart,
	// a lost pod) with nothing left to record the failure. Reading it as failed
	// is what puts Retry in front of the user; nothing is rewritten here, so the
	// next real outcome still wins.
	if time.Since(kickoffTouchedAt(claim)) > KickoffWindow {
		return KickoffState{Status: KickoffStatusFailed, Reason: kickoffStalledReason}, nil
	}
	return KickoffState{Status: KickoffStatusPending}, nil
}

// RetryKickoff re-attempts the kickoff on the user's say-so — the only way a
// project gets a second one, and the reason the create-time kickoff does not
// retry forever on its own: an agents service that is down has nothing to
// retry into, and a turn that starts an hour later starts with nobody
// watching.
//
// Idempotent by state, not by lock: a project already interviewing, or one
// whose kickoff is still working, is left alone and its state returned. Two
// clicks that do race resolve on the one-active-turn guard, which the attempt
// already treats as success.
//
// The attempt is ONE pass — no provisioning retry loop — because the user is
// waiting on the answer. A repo that is still cloning comes back as a failure
// that says so, and the button is right there.
func (s *Service) RetryKickoff(ctx context.Context, orgID, projectID string) (KickoffState, error) {
	if s.kickoffs == nil || s.conversations == nil || s.turns == nil {
		return KickoffState{}, ErrKickoffUnavailable
	}
	state, err := s.Kickoff(ctx, orgID, projectID)
	if err != nil {
		return KickoffState{}, err
	}
	attempt := s.kickoffAttempt(orgID, projectID)
	return runRetry(ctx, state, retryDeps{
		claim: func(c context.Context) error {
			_, cerr := s.kickoffs.TryClaim(c, orgID, projectID)
			return cerr
		},
		rearm: func(c context.Context) error {
			return s.kickoffs.Rearm(c, orgID, projectID)
		},
		// ONE pass, not runKickoff's loop: the user is waiting on this click,
		// so a repo that is still cloning is an answer, not a reason to sleep.
		attempt: func(c context.Context) error { return runKickoffOnce(c, attempt) },
		record: func(c context.Context, aerr error) {
			s.recordKickoffOutcome(c, orgID, projectID, aerr)
		},
	})
}

// retryDeps narrows the retry's world to its four side effects, so the
// decisions above are testable without the turn machinery — same seam shape as
// kickoffDeps.
type retryDeps struct {
	claim   func(context.Context) error
	rearm   func(context.Context) error
	attempt func(context.Context) error
	record  func(context.Context, error)
}

// runRetry is the retry's decision, given what the kickoff's state already is.
func runRetry(ctx context.Context, state KickoffState, d retryDeps) (KickoffState, error) {
	switch state.Status {
	case KickoffStatusStarted, KickoffStatusPending:
		// Already in hand. A second click must not start a second attempt, and
		// must not report failure for work that is simply still running.
		return state, nil
	case KickoffStatusNone:
		if err := d.claim(ctx); err != nil {
			return KickoffState{}, fmt.Errorf("claim kickoff: %w", err)
		}
	default: // failed
		if err := d.rearm(ctx); err != nil {
			return KickoffState{}, fmt.Errorf("rearm kickoff: %w", err)
		}
	}
	err := d.attempt(ctx)
	d.record(ctx, err)
	if err != nil {
		// The failure is the ANSWER, not an error: the caller renders the same
		// card it would have rendered from the status read.
		return KickoffState{Status: KickoffStatusFailed, Reason: KickoffFailureReason(err)}, nil
	}
	return KickoffState{Status: KickoffStatusStarted}, nil
}

// kickoffTouchedAt is the claim's last movement — the retry stamps updated_at,
// and rows written before that column carry the zero time.
func kickoffTouchedAt(c *SpecKickoff) time.Time {
	if c.UpdatedAt.IsZero() {
		return c.CreatedAt
	}
	return c.UpdatedAt
}

// kickoffTurnFailureReason renders a DEAD first-run turn as the card's
// sentence — the other half of KickoffFailureReason, which only ever sees a
// dispatch that never produced a turn at all.
//
// The turn's message carries the cause (an unreachable agents service arrives
// as `dispatch-failed` plus the dial error); the bare failure class is the
// fallback, and a row with neither still gets a sentence, because the card
// must never render an error with nothing in it.
func kickoffTurnFailureReason(t *AgentTurn) string {
	detail := strings.TrimSpace(t.Message)
	if detail == "" {
		detail = strings.TrimSpace(t.Reason)
	}
	if detail == "" {
		return "The spec interview's first turn failed before it wrote anything."
	}
	return "The spec interview's first turn failed: " + text.Truncate(detail, kickoffReasonLimit)
}

// KickoffFailureReason turns an attempt's error into a sentence the console can
// show. The two provisioning cases are named because they are ordinary and
// self-resolving; anything else carries its cause, because this console's users
// are the platform's own engineers and "something went wrong" would cost them
// the one detail worth having.
func KickoffFailureReason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrProjectRepoNotFound):
		return "The project repository was still being created, so the spec interview could not start yet."
	case errors.Is(err, ErrSkillsRepoUnavailable):
		return "The organization's skills repository was not ready, so the spec interview could not start yet."
	}
	return "The spec interview could not be started: " +
		text.Truncate(err.Error(), kickoffReasonLimit)
}

// kickoffDeps narrows the retry loop's world to three seams so the loop's
// decisions are testable without the full turn machinery.
type kickoffDeps struct {
	progressed func(context.Context) (bool, error)
	start      func(context.Context) error
	interval   time.Duration
}

// runKickoff is the claimed kickoff's retry loop. Provisioning-shaped
// failures (repo not ready, skills repo not usable yet) wait and retry;
// "somebody's turn exists / is running" ends the loop cleanly — the interview
// is in hands already; anything else is a real failure and returns.
func runKickoff(ctx context.Context, d kickoffDeps) error {
	for {
		err := runKickoffOnce(ctx, d)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrProjectRepoNotFound) && !errors.Is(err, ErrSkillsRepoUnavailable) {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("kickoff wait expired: %w (last: %w)", ctx.Err(), err)
		case <-time.After(d.interval):
		}
	}
}

// runKickoffOnce is one pass: stand down if the interview has already got
// somewhere, otherwise start it, treating the lost D18 race as the same
// outcome. It waits for nothing — the retry loop above owns waiting, and the
// user's Retry deliberately does not, because somebody is watching that click.
//
// The stand-down asks whether a turn PROGRESSED, not whether a row exists: a
// project whose every turn died has no interview to fire over, and Retry over
// one that stood down on the corpse of the failed attempt would have been a
// button that reported success and did nothing.
func runKickoffOnce(ctx context.Context, d kickoffDeps) error {
	has, err := d.progressed(ctx)
	if err != nil {
		return fmt.Errorf("check prior turns: %w", err)
	}
	if has {
		return nil // a user started the first turn — never fire /start over it
	}
	err = d.start(ctx)
	var inProgress *TurnInProgressError
	if errors.As(err, &inProgress) {
		return nil // lost the D18 race to a concurrent turn — same outcome
	}
	return err
}
