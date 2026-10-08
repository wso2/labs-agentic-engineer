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
	"log/slog"
	"time"
)

const (
	// turnSweepInterval is the sweep cadence; turnSweepStaleAfter is how old
	// a heartbeat must be before the row counts as abandoned (the runner
	// beats every turnHeartbeatEvery=15s, so 60s of silence means a crashed
	// or wedged replica — the execution.Sweep shape).
	turnSweepInterval   = 60 * time.Second
	turnSweepStaleAfter = 60 * time.Second
)

// TurnSweeper is the agent_turns crash-safety watcher (design D17): a
// stale-heartbeat running row is marked failed(stream-died), which releases
// the D18 one-active guard; if this replica still buffers the turn's stream,
// attached viewers get the terminal event too, and whatever waited for the
// turn's end (SweptTurnHandler) runs.
type TurnSweeper struct {
	turns      TurnRepository
	broker     *TurnBroker
	swept      SweptTurnHandler
	interval   time.Duration
	staleAfter time.Duration
}

// SweptTurnHandler is told of every turn the sweep failed — the turn's end
// when its own finishTurn never runs. The Service is the one handler: a closed
// issue's thread removal that waited for the turn runs here (TurnSwept).
type SweptTurnHandler interface {
	TurnSwept(ctx context.Context, t AgentTurn)
}

// NewTurnSweeper wires the sweeper; swept may be nil. Non-positive
// interval/staleAfter fall back to the 60s defaults.
func NewTurnSweeper(turns TurnRepository, broker *TurnBroker, swept SweptTurnHandler, interval, staleAfter time.Duration) *TurnSweeper {
	if interval <= 0 {
		interval = turnSweepInterval
	}
	if staleAfter <= 0 {
		staleAfter = turnSweepStaleAfter
	}
	return &TurnSweeper{turns: turns, broker: broker, swept: swept, interval: interval, staleAfter: staleAfter}
}

// Run drives the sweep on its interval until ctx is canceled (the app.Watcher
// lifecycle convention).
func (s *TurnSweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Sweep(ctx); err != nil {
				slog.WarnContext(ctx, "genai: turn sweep failed", "error", err)
			}
		}
	}
}

// Sweep runs one pass. Exported so tests (or an admin trigger) can drive a
// single pass without the ticker.
func (s *TurnSweeper) Sweep(ctx context.Context) error {
	swept, err := s.turns.SweepStale(ctx, time.Now().Add(-s.staleAfter))
	if err != nil {
		return err
	}
	for i := range swept {
		t := &swept[i]
		slog.WarnContext(ctx, "genai: swept stale turn — guard released",
			"turn", t.ID, "org", t.OrgID, "project", t.ProjectID, "useCase", t.UseCase)
		if s.broker != nil && s.broker.Has(t.ID) {
			s.broker.Terminal(t.ID, terminalEventJSON(TurnTerminal{
				Status:  turnStatusFailed,
				Reason:  turnReasonStreamDied,
				Message: "replica crashed or hung",
			}))
		}
		if s.swept != nil {
			s.swept.TurnSwept(ctx, *t)
		}
	}
	return nil
}
