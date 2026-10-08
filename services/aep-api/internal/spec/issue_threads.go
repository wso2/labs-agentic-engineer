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
)

// RemoveIssueThread removes a closed issue's chat thread (round three §4):
// every project_conversations row of its issue-<n> use case, current and
// demoted, and the agents-service conversations behind them. Its two callers
// are the issue agent's close_issue and GitHub's issues.closed webhook, so it
// is idempotent: a thread already gone is not an error. Only that use case is
// touched — the main chat, the Issues chat and every other issue keep theirs.
//
// A turn running on the thread is never interrupted: the removal is recorded
// and runs when that turn finishes (finishTurn). That is the issue agent's own
// case — close_issue runs inside the issue's turn — so a deferred removal
// never uses ctx after this returns.
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
	// Mark first, then look for a running turn. The turn's finish takes the
	// mark after its terminal write, so whichever of the two takes the mark
	// removes the thread, exactly once: if the turn finished before the mark,
	// GetActive below no longer sees it running.
	key := pendingRemovalKey(orgID, projectID, useCase)
	s.pendingRemovals.mark(key)
	active, err := s.turns.GetActive(ctx, orgID, projectID, useCase)
	if err != nil {
		s.pendingRemovals.take(key)
		return fmt.Errorf("read the issue thread's running turn: %w", err)
	}
	if active != nil {
		slog.InfoContext(ctx, "genai: issue thread removal waits for its running turn",
			"org", orgID, "project", projectID, "useCase", useCase, "turn", active.ID)
		return nil
	}
	if !s.pendingRemovals.take(key) {
		return nil // the turn's finish took it
	}
	return s.removeThreadsNow(ctx, orgID, projectID, useCase)
}

// removePendingThread runs a removal that waited for this turn, once the turn
// has written its terminal state. ctx is the finish's own detached budget.
func (s *Service) removePendingThread(ctx context.Context, job turnJob) {
	if job.chat.View != ChatViewIssue {
		return
	}
	// The view passed useCaseFor at admission, so this cannot miss.
	useCase, _ := useCaseFor(job.chat)
	if !s.pendingRemovals.take(pendingRemovalKey(job.orgID, job.projectID, useCase)) {
		return
	}
	if err := s.removeThreadsNow(ctx, job.orgID, job.projectID, useCase); err != nil {
		slog.ErrorContext(ctx, "genai: could not remove the closed issue's thread after its turn",
			"org", job.orgID, "project", job.projectID, "useCase", useCase, "turn", job.turnID, "error", err)
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

// pendingRemovals is the set of issue threads whose removal waits for a
// running turn. In memory: aep-api runs one replica, and the turn that the
// removal waits for runs in this process (a turn on another replica, or one
// lost to a restart, leaves its thread's rows behind — a closed issue's
// thread is refused anyway, and the next close removes them).
type pendingRemovals struct {
	mu   sync.Mutex
	keys map[string]struct{}
}

func pendingRemovalKey(orgID, projectID, useCase string) string {
	return orgID + "/" + projectID + "/" + useCase
}

func (p *pendingRemovals) mark(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.keys == nil {
		p.keys = map[string]struct{}{}
	}
	p.keys[key] = struct{}{}
}

// take removes key and reports whether it was marked.
func (p *pendingRemovals) take(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.keys[key]
	delete(p.keys, key)
	return ok
}
