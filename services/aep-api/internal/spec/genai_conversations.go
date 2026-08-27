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

// Project-scoped conversations (#430): the console no longer mints chat
// conversation ids — it resolves the project's CURRENT thread here, and every
// member of the project resolves the same one. ListConversations is
// deliberately plural-shaped (one element today) so the multi-conversation
// future grows the array instead of renaming the endpoint.

import (
	"context"
	"errors"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// ErrConversationsUnavailable means the service was assembled without the
// thread store (ServiceDeps.Conversations is a nil-tolerated test seam) yet a
// conversations endpoint was reached — a wiring bug, not a client error.
// StartTurn/Rehydrate degrade gracefully on a nil store (fence skipped, 404
// preserved); these two endpoints ARE the store, so they refuse loudly
// instead of panicking on a nil interface call.
var ErrConversationsUnavailable = errors.New("conversation store not configured")

// ListConversations returns the project's threads — today exactly one, the
// current thread, created lazily on first read so a project's first visitor
// (whoever they are) mints it and teammates converge on it.
func (s *Service) ListConversations(ctx context.Context, orgID, projectID string) ([]ProjectConversation, error) {
	if s.conversations == nil {
		return nil, ErrConversationsUnavailable
	}
	// Real projects still require a git repo row. The Marketplace register
	// chat project is synthetic — no git_repositories row — so skip that fence.
	if !isMarketplaceRegisterProject(projectID) {
		if _, err := s.resolveRepo(ctx, orgID, projectID); err != nil {
			return nil, err
		}
	}
	row, err := s.conversations.ResolveCurrent(ctx, orgID, projectID, useCaseGeneral, displayIdentityFrom(ctx))
	if err != nil {
		return nil, fmt.Errorf("resolve current conversation: %w", err)
	}
	return []ProjectConversation{*row}, nil
}

// RotateConversation starts a fresh thread for the WHOLE project (#430 D4):
// the current thread is demoted and a new one minted. Deliberately ungated —
// rotation is the escape hatch from an abandoned interview, so the console
// confirms intent (naming what is at stake) rather than this refusing.
func (s *Service) RotateConversation(ctx context.Context, orgID, projectID string) (*ProjectConversation, error) {
	if s.conversations == nil {
		return nil, ErrConversationsUnavailable
	}
	if !isMarketplaceRegisterProject(projectID) {
		if _, err := s.resolveRepo(ctx, orgID, projectID); err != nil {
			return nil, err
		}
	}
	row, err := s.conversations.Rotate(ctx, orgID, projectID, useCaseGeneral, displayIdentityFrom(ctx))
	if err != nil {
		return nil, fmt.Errorf("rotate conversation: %w", err)
	}
	return row, nil
}

// displayIdentityFrom projects a best-effort display name off the request's
// bearer for the created_by stamp — informational only, empty on any parse
// failure (same posture as collab presence).
func displayIdentityFrom(ctx context.Context) string {
	token := auth.GetAuthToken(ctx)
	if token == "" {
		return ""
	}
	name, email := parseDisplayIdentity("Bearer " + token)
	if name != "" {
		return name
	}
	return email
}
