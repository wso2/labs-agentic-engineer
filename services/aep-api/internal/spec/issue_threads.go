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
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// RemoveIssueThread removes a closed issue's chat thread (round three §4):
// every project_conversations row of its issue-<n> use case, current and
// demoted, and the agents-service conversations behind them. Its callers are
// the issue agent's close_issue and GitHub's issues.closed and issues.reopened
// webhooks (a reopen removes whatever a lost or failed close left, so a
// reopened issue always starts a fresh thread), so it is idempotent: a thread
// already gone is not an error. Only that use case is touched — the main chat,
// the Issues chat and every other issue keep theirs.
//
// A turn running on the thread is never interrupted: the removal is marked
// for that turn and runs when it ends — its finishTurn, or the TurnSweeper
// failing it after a crash (TurnSwept). That is the issue agent's own case —
// close_issue runs inside the issue's turn — so a deferred removal never uses
// ctx after this returns. A running row whose heartbeat is older than the
// sweep's stale threshold is a dead turn, not a running one: the thread is
// removed now.
//
// The agents-service delete is best-effort: a failure is logged and the rows
// are deleted regardless; the agents store's TTL sweep reaps the orphan.
func (s *Service) RemoveIssueThread(ctx context.Context, orgID, projectID string, issueNumber int) error {
	useCase, err := useCaseFor(ChatScope{View: ChatViewIssue, IssueNumber: issueNumber})
	if err != nil {
		return err
	}
	if s.conversations == nil {
		return nil // test seam: no thread store, no threads
	}
	key := pendingRemovalKey(orgID, projectID, useCase)
	active, err := s.turns.GetActive(ctx, orgID, projectID, useCase)
	if err != nil {
		return fmt.Errorf("read the issue thread's running turn: %w", err)
	}
	if active == nil || time.Since(active.HeartbeatAt) > turnSweepStaleAfter {
		// No live turn: any mark left on the thread belonged to a turn that is
		// gone, so it is dropped with the thread.
		s.pendingRemovals.take(key, "")
		return s.removeThreadsNow(ctx, orgID, projectID, useCase)
	}
	// Mark for the running turn, then look again: its end takes the mark
	// after the terminal write, so if the turn ended before the mark, the
	// re-read sees it ended and the removal runs here — exactly once either way.
	s.pendingRemovals.mark(key, active.ID)
	again, err := s.turns.Get(ctx, orgID, projectID, active.ID)
	if err != nil {
		s.pendingRemovals.take(key, active.ID)
		return fmt.Errorf("re-read the issue thread's running turn: %w", err)
	}
	if again != nil && again.Status == turnStatusRunning {
		slog.InfoContext(ctx, "genai: issue thread removal waits for its running turn",
			"org", orgID, "project", projectID, "useCase", useCase, "turn", active.ID)
		return nil
	}
	if !s.pendingRemovals.take(key, active.ID) {
		return nil // the turn's end took it
	}
	return s.removeThreadsNow(ctx, orgID, projectID, useCase)
}

// TurnSwept runs a removal that waited for a turn the TurnSweeper failed: a
// turn lost to a crash never reaches its finishTurn, and one that does finds
// the row swept, so the sweep is the turn's end.
func (s *Service) TurnSwept(ctx context.Context, t AgentTurn) {
	s.removePendingThread(ctx, t.OrgID, t.ProjectID, t.UseCase, t.ID)
}

// removePendingThread runs a removal that waited for turnID, once that turn
// has ended (written its terminal state, or been swept). A mark for any other
// turn is stale — one turn runs per use case, so that one ended without
// taking it — and is dropped without removing: it must never delete a thread
// a later turn is using. ctx is the caller's own budget, never a request's.
func (s *Service) removePendingThread(ctx context.Context, orgID, projectID, useCase, turnID string) {
	if !s.pendingRemovals.take(pendingRemovalKey(orgID, projectID, useCase), turnID) {
		return
	}
	if err := s.removeThreadsNow(ctx, orgID, projectID, useCase); err != nil {
		slog.ErrorContext(ctx, "genai: could not remove the closed issue's thread after its turn",
			"org", orgID, "project", projectID, "useCase", useCase, "turn", turnID, "error", err)
	}
}

// removeThreadsNow deletes the use case's rows, then each one's agents-service
// conversation (best-effort, logged).
func (s *Service) removeThreadsNow(ctx context.Context, orgID, projectID, useCase string) error {
	ids, err := s.conversations.DeleteUseCase(ctx, orgID, projectID, useCase)
	if err != nil {
		return fmt.Errorf("delete the issue thread: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	repo, err := s.resolveRepo(ctx, orgID, projectID)
	if err != nil {
		slog.WarnContext(ctx, "genai: issue thread rows deleted; its agents-service history is left to the TTL sweep",
			"org", orgID, "project", projectID, "useCase", useCase, "error", err)
		return nil
	}
	for _, id := range ids {
		if err := s.client.DeleteConversation(ctx, namespacedID(repo, useCase, id), orgID); err != nil {
			slog.WarnContext(ctx, "genai: could not delete an issue thread's agents-service history; the TTL sweep will",
				"org", orgID, "project", projectID, "useCase", useCase, "conversation", id, "error", err)
		}
	}
	return nil
}

// pendingRemovals holds the issue threads whose removal waits for a running
// turn, each bound to the turn it waits for. In memory: aep-api runs one
// replica, and that turn runs in this process. A mark lost to a restart
// leaves the closed issue's rows behind — its thread is refused while it is
// closed, and the next close or a reopen removes them.
type pendingRemovals struct {
	mu   sync.Mutex
	keys map[string]string // key -> the turn id the removal waits for
}

func pendingRemovalKey(orgID, projectID, useCase string) string {
	return orgID + "/" + projectID + "/" + useCase
}

func (p *pendingRemovals) mark(key, turnID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.keys == nil {
		p.keys = map[string]string{}
	}
	p.keys[key] = turnID
}

// take removes key's mark, whichever turn it was for, and reports whether it
// was for turnID.
func (p *pendingRemovals) take(key, turnID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	marked, ok := p.keys[key]
	delete(p.keys, key)
	return ok && marked == turnID
}
