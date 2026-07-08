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

	"github.com/wso2/aep/aep-api/internal/credentials"
)

// The git-provider capability ports.
//
// These interfaces are the provider-neutral seam between gitrepo's domain
// services and whatever git host actually serves the requests. The GitHub
// implementation lives in clients/github and is selected by GIT_PROVIDER; a
// GitLab/Gitea impl would be a sibling package satisfying the same ports with
// zero consumer changes. The seam is capability-sliced by CONSUMER need so each
// domain service depends only on the verbs it drives.
//
// Every call takes a credentials.Credential (or an App minter / raw token) —
// the client is the only place that mints Authorization headers, so tokens
// never cross a service boundary. All ports/types are stateless and
// concurrency-safe.
//
// The sentinels the implementations must return (ErrRepoNameConflict,
// HTTPStatusError codes, ...) and the wire DTOs (IssueResult, IssueInfo,
// GitHubUser, ...) are part of this contract — see errors.go and wire.go.
// The orgcreds validator keys off the sentinels, so any provider impl MUST
// reproduce them. Repo CONTENT (blobs/trees/commits/refs/tags) never goes
// through these ports: it runs on the Workspace engine (workspace.go /
// internal/platform/gitfs).

// RepoAdmin is the repository-lifecycle surface. Consumed by repoService
// (CreateRepo / EnsureBareRepo). Repo delete/get are DB operations in
// repoService itself; the host only provisions the remote repo.
type RepoAdmin interface {
	// CreateOrgRepo creates a repo owned by the credential's RepoOwner() (org
	// or, on 404-fallback, user account). Returns ErrRepoNameConflict when the
	// name is taken.
	CreateOrgRepo(ctx context.Context, cred credentials.Credential, req CreateOrgRepoRequest) (cloneURL string, err error)
}

// IssueOps is the issue surface (create / list / close / comment / labels).
// Consumed by issueService.
type IssueOps interface {
	CreateIssue(ctx context.Context, owner, repo string, cred credentials.Credential, req CreateIssueRequest) (*IssueResult, error)
	ListIssues(ctx context.Context, owner, repo string, cred credentials.Credential, labels []string) ([]IssueInfo, error)
	// EnsureLabel creates a label in the repository if it does not already exist.
	// It is idempotent — a 422 Unprocessable Entity response (already exists) is treated as success.
	EnsureLabel(ctx context.Context, owner, repo string, cred credentials.Credential, name, color string) error
	// CloseIssue sets the issue state to closed with reason "completed".
	CloseIssue(ctx context.Context, owner, repo string, cred credentials.Credential, number int) error
	// CommentIssue posts a comment on the issue.
	CommentIssue(ctx context.Context, owner, repo string, cred credentials.Credential, number int, body string) error
	// EditIssueBody replaces the issue body via PATCH /issues/{number}.
	// Used by the tech-lead detail phase to write the LLM-authored body
	// after the placeholder issue was created.
	EditIssueBody(ctx context.Context, owner, repo string, cred credentials.Credential, number int, body string) error
	// EditIssueTitle replaces the issue title via PATCH /issues/{number}.
	// Used by the plan tap when a planned Task is renamed (updateTask).
	EditIssueTitle(ctx context.Context, owner, repo string, cred credentials.Credential, number int, title string) error
	// AddIssueLabels adds labels to an existing issue (merges with current;
	// adding a present label is a no-op). Used to stamp aep:status/* projection
	// and aep:attention flags.
	AddIssueLabels(ctx context.Context, owner, repo string, cred credentials.Credential, number int, labels []string) error
	// RemoveIssueLabel removes one label from an issue. A 404 (already absent)
	// is treated as success. Used to consume the aep:execute command label and
	// clear stale aep:status/* projections.
	RemoveIssueLabel(ctx context.Context, owner, repo string, cred credentials.Credential, number int, label string) error
	// SetIssueLabels replaces the issue's entire label set (labels absent from
	// the slice are removed). Used by block-repair projection when the full set
	// must be authoritative.
	SetIssueLabels(ctx context.Context, owner, repo string, cred credentials.Credential, number int, labels []string) error
	// GetPullRequest returns a pull request's live state (open/closed + merged +
	// merge SHA) for the sweep's PR-state reconciliation (§5).
	GetPullRequest(ctx context.Context, owner, repo string, cred credentials.Credential, number int) (*PullRequestState, error)
	// MergePullRequest merges a pull request. Idempotent: merging an
	// already-merged PR is a no-op success. Used by auto code-review mode
	// (§R3.5) — human mode and webhook-driven merges are unaffected.
	MergePullRequest(ctx context.Context, owner, repo string, cred credentials.Credential, number int) error
}

// WebhookOps is the repo-webhook surface. Consumed by webhookService.
type WebhookOps interface {
	// RegisterWebhook installs a repository webhook delivering to deliveryURL,
	// signed with hmacSecret. Returns the host-assigned hook ID.
	RegisterWebhook(ctx context.Context, owner, repo string, cred credentials.Credential, deliveryURL, hmacSecret string, events []string) (hookID int64, err error)
	// UpdateWebhookEvents replaces the subscribed-event list of an existing repo
	// webhook (PATCH /hooks/{id}). RegisterWebhook's already-exists path returns
	// a pre-existing hook without touching its events, so a hook created before
	// "issues" joined the subscription must be PATCHed to add it
	// (docs/design/tasks-github-native.md §9.2 cutover).
	UpdateWebhookEvents(ctx context.Context, owner, repo string, cred credentials.Credential, hookID int64, events []string) error
}

// AppInstallOps is the GitHub-App installation lifecycle + credential-account
// probe surface (GetUser, GetAppInstallation, ListAppInstallations,
// DeleteInstallation, ExchangeOAuthCode, GetUserInstallations). Consumed by
// feature/orgcreds — the validator's PAT/App liveness probes and the
// discover-then-bind connect + disconnect cascade.
//
// Unlike the four ports above, this surface is GitHub-specific by nature (per
// the provider-coupling map in docs/design/aep-api-target-structure.md); it is
// its own future seam if a second provider becomes real. It is grouped here so
// orgcreds holds one narrow port rather than the whole Host.
type AppInstallOps interface {
	// GetUser returns identity from GET /user. Used by the periodic
	// validator to probe a PAT credential for liveness and identity drift.
	// Returns an HTTPStatusError wrapping 401/404 etc. so callers can
	// trigger the disconnect cascade selectively.
	GetUser(ctx context.Context, cred credentials.Credential) (*GitHubUser, error)
	// GetAppInstallation calls GET /app/installations/{id} using the App
	// JWT directly (not an installation token) so it can reach the App-level
	// endpoint. Used by the validator's App-mode probe to refresh
	// account.login on rename and to detect 404/410 (install deleted).
	GetAppInstallation(ctx context.Context, minter *credentials.AppTokenMinter, installationID int64) (*AppInstallationInfo, error)
	// ListAppInstallations calls GET /app/installations using the App JWT.
	// Returns the full list of installations our App has across GitHub.
	// Used by the discover-then-bind path to surface installations the
	// platform has no row for yet.
	ListAppInstallations(ctx context.Context, minter *credentials.AppTokenMinter) ([]AppInstallationSummary, error)
	// ExchangeOAuthCode exchanges a GitHub OAuth code for a user-to-server
	// access token via POST github.com/login/oauth/access_token. Used by
	// the discover-then-bind path to obtain a user token whose
	// /user/installations response proves the user actually administers
	// the installation they're trying to bind.
	ExchangeOAuthCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (userToken string, err error)
	// GetUserInstallations calls GET /user/installations with a user-token.
	// Returns the list of installation IDs the authenticated user has
	// admin access to (per GitHub's "explicit permission" semantics).
	// Used by BindAppInstallation to verify the user is actually an admin
	// of the installation they're binding.
	GetUserInstallations(ctx context.Context, userToken string) ([]int64, error)
	// DeleteInstallation uninstalls the App from a GitHub account by calling
	// DELETE /app/installations/{id} with the App JWT. 204 means uninstalled,
	// 404 is treated as success (already gone). Used by the disconnect cascade
	// to make platform disconnect symmetric with the GitHub side — without
	// this, disconnects leave orphan installs visible to discover.
	DeleteInstallation(ctx context.Context, minter *credentials.AppTokenMinter, installationID int64) error
}

// Host is the whole git-provider surface: every capability port a single
// provider implementation exposes. The composition root selects one Host by
// GIT_PROVIDER and threads it into each domain service, where it narrows to the
// port that service consumes. clients/github's *Client satisfies Host.
type Host interface {
	RepoAdmin
	IssueOps
	WebhookOps
	AppInstallOps
}
