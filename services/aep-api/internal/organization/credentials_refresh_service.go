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

package organization

import (
	"context"
	"fmt"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// RefreshResponse is what the workspace credential helper consumes. The
// shape is identical for long-lived PATs and short-lived App tokens —
// only the ExpiresAt differs. The `taskId` JSON field name is wire contract
// with the runner's anti-misroute tripwire — since the §9.2 re-key it carries
// the EXECUTION id the refresh was scoped to.
type RefreshResponse struct {
	Token     string           `json:"token"`
	ExpiresAt time.Time        `json:"expiresAt"`
	Identity  secrets.Identity `json:"identity"`
	TaskID    string           `json:"taskId"`
}

// CredentialsRefreshService returns a fresh GitHub token + identity for the
// execution named in a verified runner bearer.
//
// The bearer is verified at the edge via auth.RunnerAuthorizer (Thunder
// publisher-cc). Claims are trusted because the signature originates from
// the platform IDP — the token itself carries the org context.
type CredentialsRefreshService interface {
	Refresh(ctx context.Context, executionID, ocOrgID string) (*RefreshResponse, error)
}

type credentialsRefreshService struct {
	resolver secrets.Resolver
}

// NewCredentialsRefreshService constructs the service.
func NewCredentialsRefreshService(resolver secrets.Resolver) CredentialsRefreshService {
	return &credentialsRefreshService{resolver: resolver}
}

func (s *credentialsRefreshService) Refresh(ctx context.Context, executionID, ocOrgID string) (*RefreshResponse, error) {
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
		TaskID:    executionID,
	}, nil
}
