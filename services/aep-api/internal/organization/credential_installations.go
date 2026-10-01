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

// credential_installations.go — App-installation lifecycle: webhook
// routing lookups (installation id / repo full name), suspend/unsuspend,
// selected-repo merge, installation + repo fetches, and the OAuth
// discover-then-bind path.

package organization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
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

// SuspendInstallation flips the org_credentials row bound to installationID
// to status='suspended'. Idempotent.
func (s *CredentialService) SuspendInstallation(ctx context.Context, installationID int64) error {
	return s.setInstallationStatus(ctx, installationID, "suspended")
}

// UnsuspendInstallation flips the row to status='active'. Idempotent.
func (s *CredentialService) UnsuspendInstallation(ctx context.Context, installationID int64) error {
	return s.setInstallationStatus(ctx, installationID, "active")
}

func (s *CredentialService) setInstallationStatus(ctx context.Context, installationID int64, status string) error {
	return s.repo.Tx(ctx, func(tx OrgCredentialTx) error {
		if err := tx.AdvisoryLock(fmt.Sprintf("install:%d", installationID)); err != nil {
			return err
		}
		// 200 idempotent — a no-op status update when no row matches (webhooks
		// may arrive before the connect callback has finished; the missing row
		// is recoverable via the next connect).
		return tx.UpdateStatusByInstallationID(installationID, status)
	})
}

// MergeSelectedRepos applies an installation_repositories.added/removed
// JSON merge under the org-scoped lock. delta carries lists of full names
// to add/remove (intersection vs. current state determines the new set).
func (s *CredentialService) MergeSelectedRepos(ctx context.Context, installationID int64, added, removed []string) error {
	return s.repo.Tx(ctx, func(tx OrgCredentialTx) error {
		// Read the row (by installation_id) BEFORE taking the org lock, exactly
		// as the inline transaction did — the lock is keyed on the org handle we
		// learn from that read.
		row, err := tx.GetByInstallationID(installationID)
		if err != nil {
			return err
		}
		if row == nil {
			return &NotFoundError{What: fmt.Sprintf("installation %d", installationID)}
		}
		if err := tx.AdvisoryLock("org:" + row.OcOrgID); err != nil {
			return err
		}

		current := map[string]bool{}
		for _, r := range row.SelectedRepos {
			current[r] = true
		}
		for _, r := range removed {
			delete(current, r)
		}
		for _, r := range added {
			current[r] = true
		}
		merged := make([]string, 0, len(current))
		for r := range current {
			merged = append(merged, r)
		}

		now := time.Now().UTC()
		return tx.UpdateColumns(row.OcOrgID, map[string]any{
			"selected_repos":    JSONStringList(merged),
			"last_validated_at": now,
		})
	})
}

// fetchInstallation mints an App JWT and reads /app/installations/{id} to
// extract account.{login,type} + selected_repos. Used at App-mode connect.
// accountType is "Organization" or "User" — the connect path refuses
// "User" because GitHub's POST /user/repos endpoint is not accessible to
// App installation tokens, so repo provisioning would 403 silently.
func (s *CredentialService) fetchInstallation(ctx context.Context, installationID int64) (accountLogin, accountType string, selectedRepos []string, err error) {
	jwt, err := s.minter.SignAppJWT(time.Now())
	if err != nil {
		return "", "", nil, &ConflictError{Reason: fmt.Sprintf("app sign: %v", err)}
	}
	url := fmt.Sprintf("%s/app/installations/%d", s.githubAPI, installationID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", "", nil, fmt.Errorf("fetch install: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 404 {
		return "", "", nil, &ValidationError{Code: "installation_not_found", Message: fmt.Sprintf("installation %d not found (was the App uninstalled?)", installationID)}
	}
	if resp.StatusCode != 200 {
		return "", "", nil, &ValidationError{Code: "github_error", Message: fmt.Sprintf("GET /app/installations/%d: %d %s", installationID, resp.StatusCode, truncateForError(body))}
	}
	var inst struct {
		Account struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"account"`
		RepositorySelection string `json:"repository_selection"`
	}
	if err := json.Unmarshal(body, &inst); err != nil {
		return "", "", nil, fmt.Errorf("decode install: %w", err)
	}
	if inst.Account.Login == "" {
		return "", "", nil, &ValidationError{Code: "install_no_account", Message: "installation response missing account.login"}
	}

	// Pull the selected-repos list. Empty for repository_selection=all.
	selectedRepos, err = s.listInstallationRepos(ctx, installationID)
	if err != nil {
		// Best-effort — log and continue with empty list.
		slog.WarnContext(ctx, "list installation repos failed", "installationId", installationID, "error", err)
	}

	return inst.Account.Login, inst.Account.Type, selectedRepos, nil
}

// ListInstallationRepos calls GET /installation/repositories with a fresh
// installation token. Used by the reach-reconciliation Phase B cascade to
// confirm GitHub agrees the install has shrunk before abandoning tasks
// (§6.8). Public wrapper around the private helper that's also used by the
// connect flow.
func (s *CredentialService) ListInstallationRepos(ctx context.Context, installationID int64) ([]string, error) {
	return s.listInstallationRepos(ctx, installationID)
}

func (s *CredentialService) listInstallationRepos(ctx context.Context, installationID int64) ([]string, error) {
	token, _, err := s.minter.MintForInstallation(ctx, installationID)
	if err != nil {
		return nil, err
	}
	out := []string{}
	page := 1
	for {
		url := fmt.Sprintf("%s/installation/repositories?per_page=100&page=%d", s.githubAPI, page)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		resp, err := s.httpClient.Do(req)
		if err != nil {
			return out, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return out, fmt.Errorf("list repos: %d %s", resp.StatusCode, truncateForError(body))
		}
		var page1 struct {
			TotalCount   int `json:"total_count"`
			Repositories []struct {
				FullName string `json:"full_name"`
			} `json:"repositories"`
		}
		if err := json.Unmarshal(body, &page1); err != nil {
			return out, err
		}
		for _, r := range page1.Repositories {
			out = append(out, r.FullName)
		}
		if len(page1.Repositories) < 100 {
			break
		}
		page++
	}
	return out, nil
}

// fetchAppBotIdentity calls GET /app to learn the App's bot login. The
// "name" field is the App's display name; the "slug" is what appears as
// `<slug>[bot]` on commits.
func (s *CredentialService) fetchAppBotIdentity(ctx context.Context) (secrets.Identity, error) {
	jwt, err := s.minter.SignAppJWT(time.Now())
	if err != nil {
		return secrets.Identity{}, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.githubAPI+"/app", nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return secrets.Identity{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return secrets.Identity{}, fmt.Errorf("GET /app: %d %s", resp.StatusCode, truncateForError(body))
	}
	var info struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return secrets.Identity{}, err
	}
	if info.Slug == "" {
		return secrets.Identity{}, errors.New("/app response missing slug")
	}
	// GitHub's commit-attribution convention uses {numericUserID}+{slug}[bot]
	// as the noreply email's local-part. We don't have the numeric user ID
	// at this layer; leave the slug-based shape (GitHub still attributes
	// correctly when the email belongs to the App's verified noreply domain).
	return secrets.Identity{
		Name:  info.Name,
		Email: fmt.Sprintf("%s[bot]@users.noreply.github.com", info.Slug),
		Login: fmt.Sprintf("%s[bot]", info.Slug),
	}, nil
}
