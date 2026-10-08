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

func (s *threadRowsStub) DeleteUseCase(_ context.Context, _, _, useCase string, _ time.Time) ([]string, error) {
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
			s.pendingRemovals.mark(key, "turn-gone", time.Now()) // a stale mark from a dead turn
			if err := s.RemoveIssueThread(context.Background(), "o", "p", 7, time.Now()); err != nil {
				t.Fatalf("RemoveIssueThread: %v", err)
			}
			if len(rows.deletes) != tc.wantDeletes {
				t.Fatalf("deletes = %v, want %d", rows.deletes, tc.wantDeletes)
			}
			if tc.wantMarkFor == "" {
				if _, marked := s.pendingRemovals.keys[key]; marked {
					t.Errorf("a mark outlived the removal: %+v", s.pendingRemovals.keys[key])
				}
				return
			}
			if left, ok := s.pendingRemovals.take(key); !ok || left.turnID != tc.wantMarkFor {
				t.Errorf("mark = %+v, want one for %s", left, tc.wantMarkFor)
			}
		})
	}
}

// A mark is bound to its turn: marking the same turn again keeps the later
// bound (it covers both removals); a mark for another turn replaces a stale one.
func TestPendingRemovals_BoundToTheirTurn(t *testing.T) {
	var p pendingRemovals
	early, late := time.Unix(100, 0), time.Unix(200, 0)
	p.mark("k", "turn-a", late)
	p.mark("k", "turn-a", early)
	if got, ok := p.take("k"); !ok || got.turnID != "turn-a" || !got.before.Equal(late) {
		t.Fatalf("take = %+v, want turn-a with the later bound", got)
	}
	if _, ok := p.take("k"); ok {
		t.Fatal("a mark was taken twice")
	}
	p.mark("k", "turn-a", late)
	p.mark("k", "turn-b", early)
	if got, _ := p.take("k"); got.turnID != "turn-b" || !got.before.Equal(early) {
		t.Fatalf("take = %+v, want turn-b's mark replacing turn-a's", got)
	}
}
