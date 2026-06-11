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

package services

import (
	"context"
	"fmt"
	"time"

	"github.com/wso2/asdlc/asdlc-service/internal/credentials"
)

// RefreshResponse is what the workspace credential helper consumes. The
// shape is identical for Phase 0 (long-lived PAT) and Phase 2 (short-lived
// App tokens) — only the ExpiresAt differs.
type RefreshResponse struct {
	Token     string               `json:"token"`
	ExpiresAt time.Time            `json:"expiresAt"`
	Identity  credentials.Identity `json:"identity"`
	TaskID    string               `json:"taskId"`
}

// CredentialsRefreshService returns a fresh GitHub token + identity for the
// task named in a verified per-task Task JWT.
//
// The Task JWT is verified at the controller layer via JWKS-backed RS256
// (jwtassertion). Its claims (taskID, ocOrgID) are trusted because the
// signature originates from the BFF's RSA private key. There is no
// callback into the BFF anymore — the JWT itself carries all the org
// context needed.
type CredentialsRefreshService interface {
	Refresh(ctx context.Context, taskID, ocOrgID string) (*RefreshResponse, error)
}

type credentialsRefreshService struct {
	resolver credentials.Resolver
}

// NewCredentialsRefreshService constructs the service.
func NewCredentialsRefreshService(resolver credentials.Resolver) CredentialsRefreshService {
	return &credentialsRefreshService{resolver: resolver}
}

func (s *credentialsRefreshService) Refresh(ctx context.Context, taskID, ocOrgID string) (*RefreshResponse, error) {
	cred, err := s.resolver.Resolve(ctx, ocOrgID)
	if err != nil {
		return nil, fmt.Errorf("resolve credential: %w", err)
	}
	token, expiresAt, err := cred.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("token: %w", err)
	}
	return &RefreshResponse{
		Token:     token,
		ExpiresAt: expiresAt,
		Identity:  cred.Identity(),
		TaskID:    taskID,
	}, nil
}
