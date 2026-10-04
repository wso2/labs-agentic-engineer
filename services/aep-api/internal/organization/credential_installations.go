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

// credential_installations.go — the webhook routing lookups (installation
// id / repo full name) the GitHub receiver resolves an org with.

package organization

import (
	"context"
	"fmt"
)

// ----------------------------------------------------------------------------
// Routing lookup — used by the BFF webhook receiver
// ----------------------------------------------------------------------------

// OrgIDByInstallationID returns the ocOrgId bound to the given
// installation_id. Used by the BFF webhook receiver to route App-mode
// events. NotFoundError if no row matches.
func (s *CredentialService) OrgIDByInstallationID(ctx context.Context, installationID int64) (string, error) {
	row, err := s.repo.GetByInstallationID(ctx, installationID)
	if err != nil {
		return "", err
	}
	if row == nil {
		return "", &NotFoundError{What: fmt.Sprintf("installation %d", installationID)}
	}
	return row.OcOrgID, nil
}

// OrgIDByRepoFullName returns the ocOrgId that owns the given GitHub repo
// (full_name = "owner/repo"). Resolved against git_repositories.org_id —
// every provisioned repo carries the OC org slug it belongs to.
//
// Used by the BFF webhook receiver to route PAT-mode (and App-mode
// per-repo) events: pull_request, push, issue_comment, issues. The
// installation-id-based path handles the App-mode lifecycle events;
// repo-keyed events use this lookup so PAT-mode events route correctly.
func (s *CredentialService) OrgIDByRepoFullName(ctx context.Context, fullName string) (string, error) {
	if fullName == "" {
		return "", &NotFoundError{What: "empty repo full_name"}
	}
	// INT-2 (routing leg): resolve against the canonical clone URL, anchored on
	// host+owner+repo (no unanchored LIKE, which could route a webhook to the
	// wrong org). The anchored match lives in the repository — see
	// OrgCredentialRepository.OrgIDByRepoURL.
	orgID, err := s.repo.OrgIDByRepoURL(ctx, fullName)
	if err != nil {
		return "", fmt.Errorf("repo lookup: %w", err)
	}
	if orgID == "" {
		return "", &NotFoundError{What: fmt.Sprintf("repo %s", fullName)}
	}
	return orgID, nil
}
