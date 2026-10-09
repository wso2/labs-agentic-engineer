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

// credential_identity.go — the credential's GitHub identity view:
// IdentityFor, validator-driven identity refresh/drift, validated-at, and
// the validator's row listing.

package organization

import (
	"context"
	"fmt"
	"time"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// ----------------------------------------------------------------------------
// Identity projection — GET /internal/credentials/orgs/{ocOrgId}/identity
// ----------------------------------------------------------------------------

// Identity is the identity-only projection used by the BFF dispatch path.
type Identity struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Login       string `json:"login"`
	GitHubLogin string `json:"githubLogin"`
}

func (s *CredentialService) IdentityFor(ctx context.Context, ocOrgID string) (*Identity, error) {
	row, err := s.fetchRow(ctx, ocOrgID)
	if err != nil {
		return nil, err
	}
	if row.Status != "active" {
		return nil, &ConflictError{Reason: fmt.Sprintf("org %s status=%s", ocOrgID, row.Status)}
	}
	return &Identity{
		Kind:        row.Kind,
		Name:        row.IdentityName,
		Email:       row.IdentityEmail,
		Login:       row.IdentityLogin,
		GitHubLogin: row.GitHubLogin,
	}, nil
}

// GitHubOwner answers the GitHub account the org's repositories are created
// under: the github_login it connected (sourcecontrol.OwnerLookup). An org
// with no GitHub connection is sourcecontrol.ErrAEStudioAbsent, the same
// answer its pod gives.
func (s *CredentialService) GitHubOwner(ctx context.Context, ocOrgID string) (string, error) {
	row, err := s.repo.GetByOrg(ctx, ocOrgID)
	if err != nil {
		return "", fmt.Errorf("read github connection: %w", err)
	}
	if row == nil || row.GitHubLogin == "" {
		return "", sourcecontrol.ErrAEStudioAbsent
	}
	return row.GitHubLogin, nil
}

// RecordIdentityFromGitHub atomically updates an OrgCredential row's
// identity columns (login/name/email) and last_validated_at. If the new
// login differs from stored identity_login, it also records prev_identity_login
// and identity_changed_at per phase2.md §6.6.
//
// Used by:
//   - the PAT-replace flow
//   - the periodic validator on a successful identity read
//
// Caller passes (login, name, email) — the same triple ghIdentity carries.
// Returns true if drift was recorded.
func (s *CredentialService) RecordIdentityFromGitHub(ctx context.Context, ocOrgID, login, name, email string) (drifted bool, err error) {
	row, err := s.fetchRow(ctx, ocOrgID)
	if err != nil {
		return false, err
	}
	if row == nil {
		return false, &NotFoundError{What: "org_credentials:" + ocOrgID}
	}
	if name == "" {
		name = login
	}
	if email == "" {
		email = fmt.Sprintf("%s@users.noreply.github.com", login)
	}
	now := time.Now().UTC()
	updates := map[string]any{
		"identity_name":     name,
		"identity_email":    email,
		"identity_login":    login,
		"last_validated_at": now,
	}
	if login != row.IdentityLogin {
		prev := row.IdentityLogin
		updates["prev_identity_login"] = &prev
		updates["identity_changed_at"] = now
		drifted = true
	}
	if err := s.repo.UpdateColumns(ctx, ocOrgID, updates); err != nil {
		return false, fmt.Errorf("update identity: %w", err)
	}
	return drifted, nil
}

// ListActiveRows returns all OrgCredential rows in 'active' or 'suspended'
// status. The validator (pkg/credentials/validator.go) walks this list
// once per tick. The result is materialised — the validator releases the
// validator-scoped advisory lock before iterating.
func (s *CredentialService) ListActiveRows(ctx context.Context) ([]OrgCredential, error) {
	return s.repo.ListActiveRows(ctx)
}
