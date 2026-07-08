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

package gitrepo

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/credentials"
	"github.com/wso2/aep/aep-api/repositories"
)

// IssueService creates and lists GitHub issues on project repositories.
type IssueService interface {
	CreateIssue(ctx context.Context, orgID, projectID string, req CreateIssueRequest) (*IssueResult, error)
	ListIssues(ctx context.Context, orgID, projectID string, labels []string) ([]IssueInfo, error)
	// CloseIssue closes the issue, optionally posting a closing comment first.
	CloseIssue(ctx context.Context, orgID, projectID string, number int, comment string) error
	// CommentIssue posts a comment on the issue.
	CommentIssue(ctx context.Context, orgID, projectID string, number int, body string) error
	// EditIssueBody replaces the issue's body. Used by the tech-lead detail
	// phase to write the LLM-authored body after the placeholder issue was
	// created.
	EditIssueBody(ctx context.Context, orgID, projectID string, number int, body string) error
	// EditIssueTitle replaces the issue's title. Used by the plan tap on a
	// planned-Task rename (updateTask set.title).
	EditIssueTitle(ctx context.Context, orgID, projectID string, number int, title string) error
	// AddLabels adds labels to an existing issue (merges; a present label is a
	// no-op). Ensures each label exists in the repo first. Used by the platform
	// to stamp aep:status/* and aep:attention projections.
	AddLabels(ctx context.Context, orgID, projectID string, number int, labels []string) error
	// RemoveLabel removes one label from an issue (404 = already absent = ok).
	// Used to consume the aep:execute command label after dispatch.
	RemoveLabel(ctx context.Context, orgID, projectID string, number int, label string) error
	// SetLabels replaces the issue's entire label set. Used when the projection
	// must be authoritative (clearing stale aep:status/* in one call).
	SetLabels(ctx context.Context, orgID, projectID string, number int, labels []string) error
	// GetPullRequestState returns a pull request's live state — the sweep's
	// PR-state reconciliation input (§5).
	GetPullRequestState(ctx context.Context, orgID, projectID string, number int) (*PullRequestState, error)
	// MergePullRequest merges a pull request. Idempotent (see IssueOps). Used
	// by auto code-review mode's orchestration AutoMerge activity (§R3.5).
	MergePullRequest(ctx context.Context, orgID, projectID string, number int) error
}

type issueService struct {
	repo     repositories.RepoRepository
	github   IssueOps
	resolver credentials.Resolver
}

func NewIssueService(repo repositories.RepoRepository, github IssueOps, resolver credentials.Resolver) IssueService {
	return &issueService{
		repo:     repo,
		github:   github,
		resolver: resolver,
	}
}

func (s *issueService) CreateIssue(ctx context.Context, orgID, projectID string, req CreateIssueRequest) (*IssueResult, error) {
	if strings.TrimSpace(req.Title) == "" {
		return nil, fmt.Errorf("title is required")
	}

	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}

	// Ensure all requested labels exist in the repo before creating the issue.
	// GitHub silently drops labels that don't exist, so we create them up-front.
	for _, label := range req.Labels {
		color := labelColor(label)
		if ensureErr := s.github.EnsureLabel(ctx, owner, repoName, cred, label, color); ensureErr != nil {
			// Non-fatal: log and continue; the issue will be created without the missing label.
			slog.WarnContext(ctx, "ensure github label failed", "label", label, "error", ensureErr)
		}
	}

	// GitHub Projects v2 is dropped (tasks-github-native §4): no lazy board
	// create/link/add on issue creation. Tasks are plain GitHub issues.
	return s.github.CreateIssue(ctx, owner, repoName, cred, req)
}

func (s *issueService) ListIssues(ctx context.Context, orgID, projectID string, labels []string) ([]IssueInfo, error) {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return s.github.ListIssues(ctx, owner, repoName, cred, labels)
}

func (s *issueService) CloseIssue(ctx context.Context, orgID, projectID string, number int, comment string) error {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}

	// Post the closing comment first (best-effort: log and continue on failure).
	if strings.TrimSpace(comment) != "" {
		if commentErr := s.github.CommentIssue(ctx, owner, repoName, cred, number, comment); commentErr != nil {
			slog.WarnContext(ctx, "failed to post closing comment", "project", projectID, "issue", number, "error", commentErr)
		}
	}

	return s.github.CloseIssue(ctx, owner, repoName, cred, number)
}

func (s *issueService) CommentIssue(ctx context.Context, orgID, projectID string, number int, body string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("comment body is required")
	}

	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}

	return s.github.CommentIssue(ctx, owner, repoName, cred, number, body)
}

func (s *issueService) EditIssueBody(ctx context.Context, orgID, projectID string, number int, body string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("body is required")
	}
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	return s.github.EditIssueBody(ctx, owner, repoName, cred, number, body)
}

func (s *issueService) EditIssueTitle(ctx context.Context, orgID, projectID string, number int, title string) error {
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("title is required")
	}
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	return s.github.EditIssueTitle(ctx, owner, repoName, cred, number, title)
}

func (s *issueService) AddLabels(ctx context.Context, orgID, projectID string, number int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	// Ensure each label exists first — GitHub silently drops unknown labels.
	for _, label := range labels {
		if ensureErr := s.github.EnsureLabel(ctx, owner, repoName, cred, label, labelColor(label)); ensureErr != nil {
			slog.WarnContext(ctx, "ensure github label failed", "label", label, "error", ensureErr)
		}
	}
	return s.github.AddIssueLabels(ctx, owner, repoName, cred, number, labels)
}

func (s *issueService) RemoveLabel(ctx context.Context, orgID, projectID string, number int, label string) error {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	return s.github.RemoveIssueLabel(ctx, owner, repoName, cred, number, label)
}

func (s *issueService) SetLabels(ctx context.Context, orgID, projectID string, number int, labels []string) error {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	for _, label := range labels {
		if ensureErr := s.github.EnsureLabel(ctx, owner, repoName, cred, label, labelColor(label)); ensureErr != nil {
			slog.WarnContext(ctx, "ensure github label failed", "label", label, "error", ensureErr)
		}
	}
	return s.github.SetIssueLabels(ctx, owner, repoName, cred, number, labels)
}

func (s *issueService) GetPullRequestState(ctx context.Context, orgID, projectID string, number int) (*PullRequestState, error) {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return s.github.GetPullRequest(ctx, owner, repoName, cred, number)
}

func (s *issueService) MergePullRequest(ctx context.Context, orgID, projectID string, number int) error {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	return s.github.MergePullRequest(ctx, owner, repoName, cred, number)
}

// resolveRepoAndCredential looks up the project's git repository, parses its
// owner/repo from the clone URL, and resolves the org's credential. Every
// GitHub-bound op routes through here — the multi-tenant invariant
// (operations parametrised by ocOrgID) is enforced at one place.
func (s *issueService) resolveRepoAndCredential(ctx context.Context, orgID, projectID string) (owner, repo string, cred credentials.Credential, err error) {
	gitRepo, err := s.repo.GetByOrgAndProjectID(ctx, orgID, projectID)
	if err != nil {
		return "", "", nil, fmt.Errorf("get repo: %w", err)
	}
	if gitRepo == nil {
		return "", "", nil, ErrRepoNotFound
	}

	owner, repo, err = ParseOwnerRepo(gitRepo.RepoURL)
	if err != nil {
		return "", "", nil, fmt.Errorf("parse repo url %q: %w", gitRepo.RepoURL, err)
	}

	cred, err = s.resolver.Resolve(ctx, gitRepo.OrgID)
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve credential for org %q: %w", gitRepo.OrgID, err)
	}
	return owner, repo, cred, nil
}

// ParseOwnerRepo extracts the "owner" and "repo" segments from a GitHub clone URL.
// Supports https://github.com/owner/repo.git and https://github.com/owner/repo forms.
func ParseOwnerRepo(cloneURL string) (owner, repo string, err error) {
	u := strings.TrimSpace(cloneURL)
	for _, prefix := range []string{"https://github.com/", "http://github.com/", "git@github.com:"} {
		if strings.HasPrefix(u, prefix) {
			u = strings.TrimPrefix(u, prefix)
			break
		}
	}
	u = strings.TrimSuffix(u, ".git")
	u = strings.Trim(u, "/")

	parts := strings.SplitN(u, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("not a github repo url")
	}
	return parts[0], parts[1], nil
}

// labelColor returns a hex color (without #) for well-known AEP labels,
// falling back to a neutral grey for anything else (e.g. phase-N labels).
func labelColor(name string) string {
	switch name {
	case "aep":
		return "0075ca" // blue
	case "implementation":
		return "7057ff" // purple
	case "pending":
		return "e4e669" // yellow
	default:
		return "ededed" // light grey for phase-N and other dynamic labels
	}
}
