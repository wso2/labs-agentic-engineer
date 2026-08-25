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

// The kickoff: the journey starts itself (#562).
//
// Creating a project used to stop the user dead — they described what they
// wanted, confirmed a name, and landed on a dashboard asking them to press
// Generate spec before anything happened. The platform had everything it
// needed and waited for a click to begin work it could have started itself.
// So it starts it: `POST /projects` fires `/start` server-side, and the user
// lands on a project whose agent is already interviewing them.
//
// The turn is ROOM-SCOPED, exactly like the one the console used to send: the
// agent joins the project's spec room, edits the shared doc live, and the
// collab committer projects it into git. Firing a committed turn instead would
// make the kickoff the one turn in the product that behaves differently from
// every other, and the spec view would show nothing until it landed.
//
// It carries the CREATING USER's bearer, which is what lets the agent join
// the room — and why this runs from the create request rather than from a
// reconciler: a background sweep has no user to act as.
//
// The dispatch is INLINE, so `POST /projects` answers only once the turn row
// exists. That is what lets the console paint the kickoff on arrival instead of
// discovering it a poll later — and, more than the latency, it is what makes
// `spec.agent == \"\"` mean one thing: this project was never started. While the
// dispatch was detached it meant that OR "starting right now", and no surface
// could tell which.

package spec

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// kickoffBudget bounds the DISPATCH, not the turn. StartTurn returns as soon as
// the agents service has the turn (it runs detached from there), so this only
// has to cover the conversation resolve and the git reads in front of it —
// measured at ~2s on a warm org.
//
// Tight because this now runs INSIDE the create request. The one case that can
// blow it is an org's very first project, where the shared skills repo has to
// be provisioned before its head can be read; creation already fires that
// provisioning eagerly and asynchronously, so the budget is a ceiling on the
// unlucky case rather than the normal cost. Blowing it leaves the project
// created and un-started — which the status reports as `never-started` rather
// than as idle, so the spec view can offer a way to begin rather than a
// spinner for work that is not coming.
const kickoffBudget = 20 * time.Second

// ErrKickoffAlreadyRan reports that the project has already had an agent turn,
// so there is no kickoff left to fire. Not a failure: it is what makes Kickoff
// safe to call from more than one place, and the reason a retried references
// upload does not start a second interview.
var ErrKickoffAlreadyRan = errors.New("project has already run an agent turn")

// Kickoff fires the project's opening `/start` turn, INLINE, and never returns
// an error: a kickoff that cannot start must not fail a creation the user has
// already committed to — the same posture as every other post-create step.
//
// Inline rather than detached, which it used to be. Detaching returned the
// create response ~2s before the turn row existed, and in that window the
// project genuinely could not be told apart from one that was never started:
// `spec.agent` reads "" for both. So the console landed on a card offering to
// start work that was already starting, then rewrote it a second later. Paying
// those 2s inside a create that already takes ~7s buys an arrival that is
// simply correct — and the user spends them on the "Creating your project…"
// label they are already watching, rather than on an empty chat.
//
// The context keeps its values but not its cancellation: the caller's request
// may end (a closed tab) while the dispatch is mid-flight, and a turn that was
// going to start should still start.
func (s *Service) Kickoff(ctx context.Context, orgID, projectID string) {
	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), kickoffBudget)
	defer cancel()
	turnID, err := s.StartKickoff(bg, orgID, projectID)
	switch {
	case errors.Is(err, ErrKickoffAlreadyRan):
		slog.InfoContext(bg, "kickoff skipped: the project has already run a turn",
			"org", orgID, "project", projectID)
	case err != nil:
		slog.ErrorContext(bg, "kickoff failed (the project reports never-started; the spec view offers Retry)",
			"org", orgID, "project", projectID, "error", err)
	default:
		slog.InfoContext(bg, "kickoff dispatched",
			"org", orgID, "project", projectID, "turn", turnID)
	}
}

// StartKickoff resolves the project's thread and starts the `/start` turn on
// it, returning the turn id. The synchronous half of Kickoff — separate so it
// is testable and so a caller that wants the error can have it.
//
// IDEMPOTENT on "has this project ever run a turn": both callers can fire
// (a create whose reference upload then lands, a retried upload), and neither
// may start a second interview over the first. The guard is a turn row rather
// than a spec file because the whole point is to cover the window BEFORE the
// first file lands — that window is the entire kickoff.
func (s *Service) StartKickoff(ctx context.Context, orgID, projectID string) (string, error) {
	if s == nil || s.conversations == nil {
		return "", ErrConversationsUnavailable
	}
	// A blank bearer means the agent cannot join the spec room, and a
	// room-scoped turn is refused for it inside StartTurn. Say so here, where
	// the cause is legible, rather than letting it surface as a collab error
	// on a turn nobody asked for.
	if auth.GetAuthToken(ctx) == "" {
		return "", ErrCollabNoToken
	}
	newest, err := s.turns.Newest(ctx, orgID, projectID)
	if err != nil {
		return "", fmt.Errorf("read newest turn: %w", err)
	}
	if newest != nil {
		return "", ErrKickoffAlreadyRan
	}
	conv, err := s.conversations.ResolveCurrent(ctx, orgID, projectID, useCaseGeneral, displayIdentityFrom(ctx))
	if err != nil {
		return "", fmt.Errorf("resolve current conversation: %w", err)
	}
	// The bare token: the idea rides the descriptor, which only this side can
	// read, and turnSpecFor attaches it — the same path a typed `/start`
	// takes, so a kickoff and a typed command produce identical turns.
	return s.StartTurn(ctx, orgID, projectID, TurnInput{
		ConversationID: conv.ID,
		Instruction:    startCommand,
		Collab:         true,
	})
}
