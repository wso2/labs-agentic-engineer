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
	"testing"
	"time"
)

// threadTurnsStub answers the two reads RemoveIssueThread makes: the running
// turn of the use case, and that turn again after the mark.
type threadTurnsStub struct {
	TurnRepository // panic on anything else
	active         *AgentTurn
	reread         *AgentTurn
}

func (s threadTurnsStub) GetActive(context.Context, string, string, string) (*AgentTurn, error) {
	return s.active, nil
}

func (s threadTurnsStub) Get(context.Context, string, string, string) (*AgentTurn, error) {
	return s.reread, nil
}

// threadRowsStub records DeleteUseCase.
type threadRowsStub struct {
	ConversationRepository // panic on anything else
	deletes                []string
}

func (s *threadRowsStub) DeleteUseCase(_ context.Context, _, _, useCase string) ([]string, error) {
	s.deletes = append(s.deletes, useCase)
	return nil, nil
}

func TestRemoveIssueThread_TurnEndedBeforeTheMark(t *testing.T) {
	key := pendingRemovalKey("o", "p", "issue-7")
	running := &AgentTurn{ID: "turn-a", Status: turnStatusRunning, HeartbeatAt: time.Now()}
	ended := &AgentTurn{ID: "turn-a", Status: turnStatusCompleted}
	cases := []struct {
		name        string
		turns       threadTurnsStub
		wantDeletes int
		wantMarkFor string // the turn the mark is left for; "" for none
	}{
		// The turn finished before the close looked: nothing to wait for.
		{"no running turn", threadTurnsStub{}, 1, ""},
		// The turn finished between the read and the mark, so its finish
		// found no mark to take: the re-read sees it ended and removes now.
		{"ended between read and mark", threadTurnsStub{active: running, reread: ended}, 1, ""},
		{"still running", threadTurnsStub{active: running, reread: running}, 0, "turn-a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows := &threadRowsStub{}
			s := &Service{turns: tc.turns, conversations: rows}
			s.pendingRemovals.mark(key, "turn-gone") // a stale mark from a dead turn
			if err := s.RemoveIssueThread(context.Background(), "o", "p", 7); err != nil {
				t.Fatalf("RemoveIssueThread: %v", err)
			}
			if len(rows.deletes) != tc.wantDeletes {
				t.Fatalf("deletes = %v, want %d", rows.deletes, tc.wantDeletes)
			}
			if tc.wantMarkFor == "" {
				if _, marked := s.pendingRemovals.keys[key]; marked {
					t.Errorf("a mark outlived the removal: %q", s.pendingRemovals.keys[key])
				}
				return
			}
			if !s.pendingRemovals.take(key, tc.wantMarkFor) {
				t.Errorf("no mark left for %s", tc.wantMarkFor)
			}
		})
	}
}

// A mark is taken only by the turn it waited for; any other turn's end drops it.
func TestPendingRemovals_BoundToTheirTurn(t *testing.T) {
	var p pendingRemovals
	p.mark("k", "turn-a")
	if p.take("k", "turn-b") {
		t.Fatal("turn-b took turn-a's removal")
	}
	if p.take("k", "turn-a") {
		t.Fatal("the mark survived another turn's end")
	}
	p.mark("k", "turn-a")
	if !p.take("k", "turn-a") || p.take("k", "turn-a") {
		t.Fatal("turn-a's mark not taken exactly once")
	}
}
