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
	"encoding/json"
	"testing"
	"time"
)

// sweepRepoStub returns fixed swept rows.
type sweepRepoStub struct {
	TurnRepository // panic on anything else
	swept          []AgentTurn
	gotOlderThan   time.Time
}

func (s *sweepRepoStub) SweepStale(_ context.Context, olderThan time.Time) ([]AgentTurn, error) {
	s.gotOlderThan = olderThan
	return s.swept, nil
}

// TestTurnSweeper_EmitsBrokerTerminalForLocalBuffers: a swept row whose stream
// is buffered on this replica gets the stream-died terminal event; unknown
// buffers are skipped silently.
func TestTurnSweeper_EmitsBrokerTerminalForLocalBuffers(t *testing.T) {
	broker := NewTurnBroker()
	broker.Open("turn-local")
	sub, err := broker.Subscribe("turn-local", 0)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Cancel()

	repo := &sweepRepoStub{swept: []AgentTurn{
		{ID: "turn-local", OrgID: "o", ProjectID: "p", UseCase: "requirements-chat"},
		{ID: "turn-elsewhere", OrgID: "o", ProjectID: "q", UseCase: "design-generate"},
	}}
	sweeper := NewTurnSweeper(repo, broker, nil, 0, 0)
	if err := sweeper.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if age := time.Since(repo.gotOlderThan); age < turnSweepStaleAfter || age > turnSweepStaleAfter+5*time.Second {
		t.Errorf("olderThan cutoff = %v ago, want ~%v", age, turnSweepStaleAfter)
	}

	select {
	case e := <-sub.C:
		if !e.Terminal {
			t.Fatalf("event = %+v, want terminal", e)
		}
		var payload struct {
			Type   string `json:"type"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(e.Data, &payload); err != nil ||
			payload.Type != "turn-failed" || payload.Reason != "stream-died" {
			t.Fatalf("terminal payload = %s", e.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no terminal event delivered to the local buffer")
	}
}
