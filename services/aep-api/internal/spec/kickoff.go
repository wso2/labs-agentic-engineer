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
// Generate spec before anything happened. So the platform starts it: `POST
// /projects` fires `/start` server-side, and the user lands on a project
// whose agent is already interviewing them. A create that declared reference
// documents holds it, and the references upload fires it instead.
//
// The turn runs in the org's AE Studio pod (07 §12): aep-api asks the pod's
// ae-studio-tools to start a `start` turn for the project and reads its
// NDJSON stream. The pod owns the turn from there — its Room, its model key,
// its usage record; aep-api only starts it.
//
// IDEMPOTENT twice over. The finished-turn ledger refuses a project that has
// already run a turn (the 3.16 ledger records every pod turn), and the turn
// id is deterministic, uuidv5 of org/project (07 §5): a retry while the
// interview is still running (a create, then its references upload; a
// retried upload) reattaches to the same turn instead of starting a second
// interview (Review Focus 3).

package spec

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/platform/async"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// kickoffBudget bounds the whole kickoff on aep-api's side: the ledger read,
// the start, and the time spent following the stream. It bounds aep-api, not
// the turn: the pod runs the interview on when aep-api stops reading.
const kickoffBudget = 20 * time.Second

// kickoffNamespace is the uuidv5 namespace of kickoff turn ids (phase-3
// exact values).
var kickoffNamespace = uuid.MustParse("6f1a2c1e-6c39-4f0e-9a51-7a1d0e5a6b10")

// kickoffTurnID is the project's kickoff turn id: the same for every attempt,
// so a retry reattaches.
func kickoffTurnID(orgID, projectID string) string {
	return uuid.NewSHA1(kickoffNamespace, []byte(orgID+"/"+projectID)).String()
}

// ErrKickoffAlreadyRan reports that the project has already had an agent turn,
// so there is no kickoff left to fire. Not a failure: it is what makes Kickoff
// safe to call from more than one place, and the reason a retried references
// upload does not start a second interview.
var ErrKickoffAlreadyRan = errors.New("project has already run an agent turn")

// KickoffLedger is the finished-turn ledger's newest-turn read.
// TurnRepository satisfies it.
type KickoffLedger interface {
	Newest(ctx context.Context, orgID, projectID string) (*AgentTurn, error)
}

// KickoffService fires a project's opening `/start` turn in its org's pod.
type KickoffService struct {
	turns  aestudiotools.Turns
	repos  sourcecontrol.ProjectRepoRows
	ledger KickoffLedger
}

// NewKickoffService wires the kickoff.
func NewKickoffService(turns aestudiotools.Turns, repos sourcecontrol.ProjectRepoRows, ledger KickoffLedger) *KickoffService {
	return &KickoffService{turns: turns, repos: repos, ledger: ledger}
}

// Kickoff fires the project's opening `/start` turn and never returns an
// error: a kickoff that cannot start must not fail a creation the user has
// already committed to. The project is then simply un-started, which the spec
// view offers to begin.
//
// The start is INLINE, so the create answers once the pod has the turn and
// the console finds it running on arrival. The stream is then followed in the
// background for what is left of kickoffBudget, only to log how the turn went
// if it ends that soon; the create does not wait for it.
//
// The context keeps its values (the caller's verified claims, which the turn
// is credited to) but not its cancellation: a closed tab must not stop a
// turn that was going to start.
func (k *KickoffService) Kickoff(ctx context.Context, orgID, projectID string) {
	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), kickoffBudget)
	turnID, events, err := k.StartKickoff(bg, orgID, projectID)
	if err != nil {
		cancel()
		if errors.Is(err, ErrKickoffAlreadyRan) {
			slog.InfoContext(bg, "kickoff skipped: the project has already run a turn",
				"org", orgID, "project", projectID)
			return
		}
		slog.ErrorContext(bg, "kickoff failed (the project reports never-started; the spec view offers Retry)",
			"org", orgID, "project", projectID, "error", err)
		return
	}
	slog.InfoContext(bg, "kickoff started", "org", orgID, "project", projectID, "turn", turnID)
	async.Go(bg, "kickoff stream", func(ctx context.Context) {
		defer cancel() // ends the stream, and with it the open response body
		followKickoff(ctx, orgID, projectID, turnID, events)
	})
}

// StartKickoff starts (or reattaches to) the project's kickoff turn and
// returns its id and stream. The caller must range the stream or cancel ctx:
// it holds the pod's response open.
//
// Refused with ErrKickoffAlreadyRan when the ledger holds a turn of the
// project: the guard is a turn record rather than a spec file because the
// whole point is to cover the window BEFORE the first file lands.
func (k *KickoffService) StartKickoff(ctx context.Context, orgID, projectID string) (string, iter.Seq2[aestudiotools.TurnEvent, error], error) {
	newest, err := k.ledger.Newest(ctx, orgID, projectID)
	if err != nil {
		return "", nil, fmt.Errorf("read newest turn: %w", err)
	}
	if newest != nil {
		return "", nil, ErrKickoffAlreadyRan
	}
	ref, _, err := sourcecontrol.RepoRefFor(ctx, k.repos, orgID, projectID)
	if err != nil {
		return "", nil, err
	}
	turnID := kickoffTurnID(orgID, projectID)
	events, err := k.turns.StartTurn(ctx, ref, aestudiotools.TurnRequest{
		TurnID:  turnID,
		Project: projectID,
		Kind:    aestudiotools.TurnKindStart,
		Credit:  creditFrom(ctx),
	})
	if err != nil {
		return "", nil, err
	}
	return turnID, events, nil
}

// followKickoff reads the kickoff's stream until it ends or ctx (the
// remaining budget) does, and logs the outcome. Running out of budget is the
// ordinary case for an interview of minutes: the pod keeps the turn.
func followKickoff(ctx context.Context, orgID, projectID, turnID string, events iter.Seq2[aestudiotools.TurnEvent, error]) {
	for ev, err := range events {
		if ctx.Err() != nil {
			// The read failed because the budget ran out, not the pod.
			slog.InfoContext(ctx, "kickoff: stopped following the turn at the budget; the pod runs it on",
				"org", orgID, "project", projectID, "turn", turnID)
			return
		}
		if err != nil {
			slog.WarnContext(ctx, "kickoff: the turn stream broke off; the pod runs the turn on",
				"org", orgID, "project", projectID, "turn", turnID, "error", err)
			return
		}
		if ev.Type == aestudiotools.EventResult {
			slog.InfoContext(ctx, "kickoff: the turn ended",
				"org", orgID, "project", projectID, "turn", turnID, "status", ev.Status, "code", ev.Code)
			return
		}
	}
}
