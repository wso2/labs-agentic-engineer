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
//   - HasAny — a user who beat the kickoff to the first turn owns the
//     interview; firing `/start` over it is the #432 bug class server-side
//     (an unanswered question reads a fresh `/start` as the skip valve).
//   - the D18 one-active guard — a turn racing the kickoff's StartTurn makes
//     it lose cleanly (TurnInProgressError → treated as done).

package spec

import (
	"context"
	"errors"
	"fmt"
	"time"
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
	return runKickoff(ctx, kickoffDeps{
		hasAnyTurn: func(c context.Context) (bool, error) {
			return s.turns.HasAny(c, orgID, projectID)
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
	})
}

// kickoffDeps narrows the retry loop's world to three seams so the loop's
// decisions are testable without the full turn machinery.
type kickoffDeps struct {
	hasAnyTurn func(context.Context) (bool, error)
	start      func(context.Context) error
	interval   time.Duration
}

// runKickoff is the claimed kickoff's retry loop. Provisioning-shaped
// failures (repo not ready, skills repo not usable yet) wait and retry;
// "somebody's turn exists / is running" ends the loop cleanly — the interview
// is in hands already; anything else is a real failure and returns.
func runKickoff(ctx context.Context, d kickoffDeps) error {
	for {
		has, err := d.hasAnyTurn(ctx)
		if err != nil {
			return fmt.Errorf("check prior turns: %w", err)
		}
		if has {
			return nil // a user started the first turn — never fire /start over it
		}
		err = d.start(ctx)
		if err == nil {
			return nil
		}
		var inProgress *TurnInProgressError
		if errors.As(err, &inProgress) {
			return nil // lost the D18 race to a concurrent turn — same outcome
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
