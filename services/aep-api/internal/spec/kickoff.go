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

// The server-side `/start` kickoff (#485). EVERY new project's spec interview
// is started HERE, so the turn is already narrating by the time the user
// reaches the spec view — and so the console never composes `/start` at all.
// A console that also started the first turn is the bug this replaces: the two
// raced, and real users met "An agent turn is already running for this
// project".
//
// The projects domain calls KickoffSpec through a narrow port right after
// stamping the descriptor. The instruction is the bare `/start` token, so the
// idea rides in from the descriptor exactly as it does for a user-typed
// command (start_command.go — one expansion path, not two). With no idea
// captured the expansion attaches none and the start skill opens by asking
// what the user is building; the conversation begins either way, which is why
// there is no prompt gate.
//
// Exactly-once is layered:
//   - the spec_kickoffs claim — one auto kickoff per project, ever;
//   - TurnStanding.Progressed — a user who beat the kickoff to the first turn
//     owns the interview, and firing `/start` over an unanswered question form
//     reads to the start skill as the skip valve;
//   - the D18 one-active guard — a turn racing the kickoff's StartTurn makes
//     it lose cleanly (TurnInProgressError, treated as done).
//
// What the console is told is the TURN's outcome, not the dispatch's: an
// attempt that returns cleanly and whose turn dies seconds later (the agents
// service unreachable) is a FAILED kickoff, and says so. See Kickoff.

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

// Kickoff statuses — SpecKickoff.Status and the contract's enum.
const (
	// KickoffStatusNone is never stored: it is what the read reports for a
	// project with no claim at all.
	KickoffStatusNone    = "none"
	KickoffStatusPending = "pending"
	KickoffStatusFailed  = "failed"
	KickoffStatusStarted = "started"
)

// KickoffWindow bounds how long a claim without a turn still counts as work in
// progress. It is the kickoff's own lifetime — the deadline the caller gives
// it — so the two cannot drift. Past it the attempt cannot still be running, so
// a pending claim is read as a failure the user can retry.
const KickoffWindow = 10 * time.Minute

// kickoffRetryInterval paces the wait for the freshly-created repo: project
// create returns while the repo still provisions (clone is async), and
// StartTurn refuses a not-ready repo with ErrProjectRepoNotFound. The caller's
// ctx deadline bounds the total wait.
const kickoffRetryInterval = 3 * time.Second

// kickoffOutcomeWriteTimeout bounds the detached outcome write.
const kickoffOutcomeWriteTimeout = 5 * time.Second

// kickoffReasonLimit caps the carried cause — a reason is a sentence for a
// card, not a stack trace.
const kickoffReasonLimit = 300

const kickoffStalledReason = "The spec interview did not start, and the attempt is no longer running. Retry to start it."

// ErrKickoffUnavailable means the service was assembled without one of the
// stores the kickoff needs — a wiring bug, not a runtime condition.
var ErrKickoffUnavailable = errors.New("spec kickoff store not configured")

// KickoffState is what became of a project's server-side kickoff.
type KickoffState struct {
	// Status is one of the KickoffStatus* values.
	Status string
	// Reason is the user-facing failure sentence; empty unless Status is failed.
	Reason string
}

// KickoffSpec starts the project's first `/start` turn. Idempotent per project
// (the claim); a no-op when a turn already progressed (a user got there
// first). Retries while the repo or the org's skills repo is still
// provisioning, until ctx expires.
//
// The outcome is RECORDED either way, which is what lets the console say
// "starting…" or "couldn't start: <why>" instead of showing a project that
// looks busy forever.
func (s *Service) KickoffSpec(ctx context.Context, orgID, projectID string) error {
	// `turns` included: the claimed path reads Standing, and the caller runs
	// this on a detached goroutine — a nil there is a process crash, not the
	// logged best-effort failure the projects domain is promised.
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
	err = runKickoffWaiting(ctx, s.kickoffAttempt(orgID, projectID), kickoffRetryInterval)
	s.recordKickoffOutcome(ctx, orgID, projectID, err)
	return err
}

// Kickoff reports what became of this project's server-side `/start`.
//
// Three states the console can render from nothing else: "starting…" for the
// seconds between the claim and the turn row, a NAMED failure with a Retry when
// the interview died, and silence once a turn is under way (from there the turn
// reads say everything).
//
// The failure half keys off the first-run turn's OUTCOME, not the dispatch's.
// The two are different events: the BFF creates the turn and stamps the claim
// `started` synchronously, then the turn runs detached and can fail seconds
// later with the agents service down. Reading the claim alone told the console
// a kickoff had succeeded on projects where the interview never ran — no error,
// no Retry, a spec view that never filled in.
//
// Derived at read time and never written back, so the next attempt's real
// outcome still wins. Only ever asked for a project with NO SPEC (the status
// read gates it), which is what makes "no turn progressed" mean "nothing came
// of this".
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
	standing, err := s.turns.Standing(ctx, orgID, projectID)
	if err != nil {
		return KickoffState{}, fmt.Errorf("read turn standing: %w", err)
	}
	return kickoffStateOf(claim, standing, time.Now()), nil
}

// kickoffStateOf is the read's whole decision, over the two facts it holds.
// Pure, so every branch below is a test rather than a live incident.
func kickoffStateOf(claim *SpecKickoff, standing TurnStanding, now time.Time) KickoffState {
	// What the TURN did settles it, whatever the claim says. A turn running or
	// completed means the interview is in hands — the kickoff's, or a user's
	// that beat it — and the claim being `started` means only that dispatch
	// returned, so it cannot answer this on its own.
	if standing.Progressed {
		return KickoffState{Status: KickoffStatusStarted}
	}
	// The turn was created and then DIED — an unreachable agents service is the
	// ordinary way — and this read only runs for a project with no spec, so
	// nothing came of it. The claim says `started` and always will: the failure
	// happened long after the attempt that stamped it returned.
	if standing.LastFailure != nil {
		return KickoffState{
			Status: KickoffStatusFailed,
			Reason: kickoffTurnFailureReason(standing.LastFailure),
		}
	}
	if claim.Status == KickoffStatusFailed {
		return KickoffState{Status: KickoffStatusFailed, Reason: claim.Reason}
	}
	// Pending. A claim older than the kickoff's own deadline cannot still be
	// working: its process died mid-attempt (a restart, a lost pod) with nothing
	// left to record the failure. Reading it as failed is what puts Retry in
	// front of the user.
	if now.Sub(kickoffTouchedAt(claim)) > KickoffWindow {
		return KickoffState{Status: KickoffStatusFailed, Reason: kickoffStalledReason}
	}
	return KickoffState{Status: KickoffStatusPending}
}

// RetryKickoff re-attempts the kickoff on the user's say-so — the only way a
// project gets a second one, and the reason the create-time kickoff does not
// retry forever on its own: an agents service that is down has nothing to retry
// into, and a turn that starts an hour later starts with nobody watching.
//
// Idempotent by STATE, not by the claim's existence. Standing down merely
// because a row exists is what made Retry report success while starting
// nothing: a failed attempt leaves its claim behind. Only a kickoff that
// actually progressed, or one still inside its window, is left alone.
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
	switch state.Status {
	case KickoffStatusStarted, KickoffStatusPending:
		// Already in hand. A second click must not start a second attempt, and
		// must not report failure for work that is simply still running.
		return state, nil
	case KickoffStatusNone:
		if _, cerr := s.kickoffs.TryClaim(ctx, orgID, projectID); cerr != nil {
			return KickoffState{}, fmt.Errorf("claim kickoff: %w", cerr)
		}
	default: // failed — the corpse of an earlier attempt, re-opened
		if rerr := s.kickoffs.Rearm(ctx, orgID, projectID); rerr != nil {
			return KickoffState{}, fmt.Errorf("rearm kickoff: %w", rerr)
		}
	}
	attemptErr := runKickoffOnce(ctx, s.kickoffAttempt(orgID, projectID))
	s.recordKickoffOutcome(ctx, orgID, projectID, attemptErr)
	if attemptErr != nil {
		// The failure is the ANSWER, not an error: the caller renders the same
		// card it would have rendered from the status read.
		return KickoffState{Status: KickoffStatusFailed, Reason: KickoffFailureReason(attemptErr)}, nil
	}
	return KickoffState{Status: KickoffStatusStarted}, nil
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
			// The kickoff mints (or converges on) the project's current thread —
			// the same thread the console resolves — so the interview and the
			// chat around it are one conversation. createdBy stays empty: there
			// is no acting user on a create-time kickoff.
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

// kickoffDeps narrows an attempt to its two side effects, so the decisions
// around it are testable without the turn machinery.
type kickoffDeps struct {
	progressed func(context.Context) (bool, error)
	start      func(context.Context) error
}

// runKickoffOnce is one pass: stand down if the interview has already got
// somewhere, otherwise start it, treating a lost D18 race as the same outcome.
//
// The stand-down asks whether a turn PROGRESSED, not whether a row exists: a
// project whose every turn died has no interview to fire over, and standing
// down on that corpse is a Retry that reports success and does nothing.
func runKickoffOnce(ctx context.Context, d kickoffDeps) error {
	has, err := d.progressed(ctx)
	if err != nil {
		return fmt.Errorf("check prior turns: %w", err)
	}
	if has {
		return nil // somebody's turn is already the interview — never fire over it
	}
	err = d.start(ctx)
	var inProgress *TurnInProgressError
	if errors.As(err, &inProgress) {
		return nil // lost the D18 race to a concurrent turn — same outcome
	}
	return err
}

// runKickoffWaiting is the create-time kickoff's loop. Provisioning-shaped
// failures (repo not ready, skills repo not usable yet) wait and retry —
// project create returns while the clone is still running; anything else is a
// real failure and returns. Only this path waits: the user's Retry deliberately
// does not, because somebody is watching that click.
func runKickoffWaiting(ctx context.Context, d kickoffDeps, interval time.Duration) error {
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
		case <-time.After(interval):
		}
	}
}

// kickoffTouchedAt is the claim's last movement — Retry stamps updated_at.
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
// fallback, and a row with neither still gets a sentence, because the card must
// never render an error with nothing in it.
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
	return "The spec interview could not be started: " + text.Truncate(err.Error(), kickoffReasonLimit)
}
