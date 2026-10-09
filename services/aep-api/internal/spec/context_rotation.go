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

// Rotates a spec conversation whose last measured context passed 80% of the
// connection's stated window (see README).

import (
	"context"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

// Rotation fires once the recorded context is past rotateAtNum/rotateAtDen of
// the window, leaving the next turn (a prompt, the CURRENT STATE files, a few
// tool steps) room to finish before the window is full.
const (
	rotateAtNum = 4
	rotateAtDen = 5
)

// contextFull reports whether tokens is past the rotation share of window.
func contextFull(tokens int64, window int) bool {
	return tokens*rotateAtDen > int64(window)*rotateAtNum
}

// rotateIfContextFull is StartTurn's rotation check, run after the addressed
// conversation passed the current-thread fence of its use case (chat view).
// It returns ErrConversationRotated when the conversation is full (whether
// this call rotated it or a concurrent sender did first), a
// TurnInProgressError when it is full but a turn is still running in its use
// case, and nil otherwise.
//
// A connection that states no window (Anthropic's own API) returns before
// any query.
func (s *Service) rotateIfContextFull(ctx context.Context, orgID, projectID, useCase, conversationID string, conn modelconn.Connection) error {
	if conn.ContextWindow == nil || *conn.ContextWindow <= 0 || s.conversations == nil {
		return nil
	}
	tokens, err := s.turns.LastContextTokens(ctx, orgID, projectID, conversationID)
	if err != nil {
		return fmt.Errorf("read conversation context size: %w", err)
	}
	if tokens == nil || !contextFull(*tokens, *conn.ContextWindow) {
		return nil
	}
	// A running turn is still adding to this conversation, and rotating
	// under it would land its answer in a thread nobody is looking at. The
	// send was going to be refused for the running turn anyway; say that.
	active, err := s.turns.GetActive(ctx, orgID, projectID, useCase)
	if err != nil {
		return fmt.Errorf("get active turn: %w", err)
	}
	if active != nil {
		return &TurnInProgressError{ActiveTurnID: active.ID}
	}
	if _, err := s.conversations.RotateIfCurrent(ctx, orgID, projectID, useCase, conversationID, displayIdentityFrom(ctx)); err != nil {
		return fmt.Errorf("rotate full conversation: %w", err)
	}
	return ErrConversationRotated
}
