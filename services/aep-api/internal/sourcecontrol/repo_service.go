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

package sourcecontrol

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

// RepoService manages git repository lifecycle (create, get, delete).
type RepoService interface {
	// CreateRepo provisions the project's GitHub repo. repoName == "" derives
	// the name from projectName (slug); either way the name is used VERBATIM —
	// a conflict fails with ErrRepoNameConflict (never suffixed away) so the
	// user can be asked for a different name.
	CreateRepo(ctx context.Context, orgID, projectID, projectName, repoName string) (*GitRepository, error)
	// EnsureBareRepo idempotently provisions a private repo with a STABLE name
	// (no random suffix) and NO local clone — used for the per-org skills repo
	// (sentinel projectID, e.g. "_skills"). The pod initialises it with a
	// `main` branch + base tree so the first commit has a parent. If the GitHub
	// repo already exists, the pod adopts it (AdoptExisting) so the call stays
	// idempotent across a lost DB row.
	// See docs/design/skills-repo-storage.md §10.
	EnsureBareRepo(ctx context.Context, orgID, projectID, repoName string) (*GitRepository, error)
	GetRepo(ctx context.Context, orgID, projectID string) (*GitRepository, error)
	// ListByOrg returns the org's repo rows (all statuses) — the source for
	// the project-list repoUrl annotation (#108).
	ListByOrg(ctx context.Context, orgID string) ([]GitRepository, error)
	// SetWebhookID is called by the webhook registration service after a hook
	// is provisioned for the repo on GitHub. Stored alongside the repo record
	// so cleanup can deregister.
	SetWebhookID(ctx context.Context, orgID, projectID string, hookID int64) error
	// DeleteRepo drops the repo record and trashes its workspace clone. It
	// ensures ABSENCE rather than performing a removal, so a project with no
	// repo row — never provisioned, or a teardown being re-run after it got
	// this far once — succeeds with nothing to do. The remote is untouched.
	DeleteRepo(ctx context.Context, orgID, projectID string) error
}

// OwnerLookup answers the GitHub account an org's repositories live under:
// the login it connected. An org with no GitHub connection is
// ErrAEStudioAbsent.
type OwnerLookup interface {
	GitHubOwner(ctx context.Context, org string) (string, error)
}

type repoService struct {
	repo    RepoRepository
	github  RepoAdmin
	owners  OwnerLookup
	repoVis string
	// workspaceTrash, when set (from the composition root), renames the
	// repo's on-disk workspace subtree into trash after the DB row is
	// deleted — phase 1 of the two-phase disk delete (design §14/D12).
	// Best-effort by contract: it returns nothing and must never fail the
	// caller; the reaper's orphan pass is the correctness backstop.
	workspaceTrash func(ctx context.Context, orgID, projectID, repoSlug string)
}

// RepoServiceOption customizes NewRepoService wiring without churning its
// positional signature.
type RepoServiceOption func(*repoService)

// WithWorkspaceTrash installs the best-effort disk-trash hook DeleteRepo
// fires after a successful DB delete. nil-safe (a nil fn leaves the hook
// unset).
func WithWorkspaceTrash(fn func(ctx context.Context, orgID, projectID, repoSlug string)) RepoServiceOption {
	return func(s *repoService) { s.workspaceTrash = fn }
}

func NewRepoService(
	repo RepoRepository,
	github RepoAdmin,
	owners OwnerLookup,
	repoVisibility string,
	opts ...RepoServiceOption,
) RepoService {
	s := &repoService{
		repo:    repo,
		github:  github,
		owners:  owners,
		repoVis: repoVisibility,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *repoService) CreateRepo(ctx context.Context, orgID, projectID, projectName, repoName string) (*GitRepository, error) {
	slog.InfoContext(ctx, "creating repository", "org", orgID, "project", projectID, "name", projectName, "repoName", repoName)
	if orgID == "" {
		return nil, fmt.Errorf("orgID is required")
	}

	// Idempotent on (ocOrgId, project): a repeat-create returns the existing
	// row instead of erroring. Repo provisioning is the entry-point for many
	// flows (project creation, retry, drift fix), all of which should be safe
	// to retry. See evolution-doc §7.1 and phase0 §1.11.
	existing, err := s.repo.GetByOrgAndProjectID(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("check existing repo: %w", err)
	}
	if existing != nil {
		slog.InfoContext(ctx, "repo already provisioned for project; returning existing row",
			"projectId", projectID, "orgId", orgID)
		return existing, nil
	}

	owner, err := s.githubOwner(ctx, orgID)
	if err != nil {
		return nil, err
	}

	description := fmt.Sprintf("WSO2 Labs Agentic Engineer project %s", projectName)

	if repoName == "" {
		repoName = slugifyProjectName(projectName)
	}
	// The name — user-chosen or derived — is created VERBATIM: it is what the
	// create form showed. A conflict propagates (ErrRepoNameConflict survives
	// the wrap) so the caller can ask the user for a different name; suffixing
	// it away would silently rename the repo behind their back.
	cloneURL, err := s.github.CreateOrgRepo(ctx, RepoRef{Org: orgID, Owner: owner, Repo: repoName, DefaultBranch: defaultBranchFallback}, CreateOrgRepoRequest{
		Private:     strings.EqualFold(s.repoVis, "private"),
		Description: description,
	})
	if err != nil {
		return nil, fmt.Errorf("create github repo: %w", err)
	}

	// Compute the per-repo slug from the GitHub clone URL — used by
	// StageBuildSecret to validate (ocOrgId, repoSlug) ownership. The
	// build credential itself is now pre-staged per WorkflowRun directly
	// as a K8s Secret in workflows-<ocOrgID> (see
	// docs/design/build-credential-injection.md), so no SecretReference
	// name is computed here; OcSecretRefName is left nil on new rows.
	repoSlug := RepoSlugFor(cloneURL)

	// The repo is ready the moment GitHub has it: the mirror is created
	// lazily on first access, so there is no "cloning" status to wait through
	// at create time.
	gitRepo := &GitRepository{
		OrgID:         orgID,
		ProjectID:     projectID,
		RepoURL:       cloneURL,
		DefaultBranch: defaultBranchFallback, // the pod initialises a main branch + base tree
		Status:        "ready",
		RepoSlug:      repoSlug,
	}

	if err := s.repo.Create(ctx, gitRepo); err != nil {
		return nil, fmt.Errorf("create repo record: %w", err)
	}

	slog.InfoContext(ctx, "created platform repo",
		"owner", owner, "name", repoName, "project", projectID, "org", orgID)

	return gitRepo, nil
}

func (s *repoService) EnsureBareRepo(ctx context.Context, orgID, projectID, repoName string) (*GitRepository, error) {
	if orgID == "" || projectID == "" || repoName == "" {
		return nil, fmt.Errorf("orgID, projectID and repoName are required")
	}
	// Idempotent on (ocOrgId, projectID): a repeat-create returns the existing row.
	existing, err := s.repo.GetByOrgAndProjectID(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("check existing repo: %w", err)
	}
	if existing != nil {
		return existing, nil
	}

	owner, err := s.githubOwner(ctx, orgID)
	if err != nil {
		return nil, err
	}

	// A pre-existing repo of the same name under this owner is adopted.
	cloneURL, err := s.github.CreateOrgRepo(ctx, RepoRef{Org: orgID, Owner: owner, Repo: repoName, DefaultBranch: defaultBranchFallback}, CreateOrgRepoRequest{
		Private:       true,
		Description:   "WSO2 Labs Agentic Engineer — org skills (single source of truth)",
		AdoptExisting: true,
	})
	if err != nil {
		return nil, fmt.Errorf("create github skills repo: %w", err)
	}

	gitRepo := &GitRepository{
		OrgID:         orgID,
		ProjectID:     projectID,
		RepoURL:       cloneURL,
		DefaultBranch: defaultBranchFallback,
		Status:        "ready", // the mirror is created lazily on first access
		RepoSlug:      RepoSlugFor(cloneURL),
	}
	if err := s.repo.Create(ctx, gitRepo); err != nil {
		// A concurrent caller (e.g. the skills list + updates-badge requests
		// firing together on page load) may have inserted the (org, projectID)
		// row first — the unique constraint rejects ours. Adopt the winner's
		// row so EnsureBareRepo stays idempotent under concurrency.
		if winner, gerr := s.repo.GetByOrgAndProjectID(ctx, orgID, projectID); gerr == nil && winner != nil {
			slog.InfoContext(ctx, "skills repo row created concurrently; adopting existing", "org", orgID, "project", projectID)
			return winner, nil
		}
		return nil, fmt.Errorf("create skills repo record: %w", err)
	}
	slog.InfoContext(ctx, "provisioned bare skills repo",
		"owner", owner, "name", repoName, "org", orgID)
	return gitRepo, nil
}

// githubOwner is the account the org's repositories are created under.
func (s *repoService) githubOwner(ctx context.Context, orgID string) (string, error) {
	owner, err := s.owners.GitHubOwner(ctx, orgID)
	if err != nil {
		return "", fmt.Errorf("github owner for org %q: %w", orgID, err)
	}
	return owner, nil
}

func (s *repoService) GetRepo(ctx context.Context, orgID, projectID string) (*GitRepository, error) {
	repo, err := s.repo.GetByOrgAndProjectID(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("get repo: %w", err)
	}
	if repo == nil {
		return nil, ErrRepoNotFound
	}
	return repo, nil
}

func (s *repoService) ListByOrg(ctx context.Context, orgID string) ([]GitRepository, error) {
	rows, err := s.repo.ListByOrg(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("list repos by org: %w", err)
	}
	return rows, nil
}

func (s *repoService) SetWebhookID(ctx context.Context, orgID, projectID string, hookID int64) error {
	repo, err := s.repo.GetByOrgAndProjectID(ctx, orgID, projectID)
	if err != nil {
		return fmt.Errorf("get repo: %w", err)
	}
	if repo == nil {
		return ErrRepoNotFound
	}
	id := hookID
	repo.WebhookID = &id
	return s.repo.Update(ctx, repo)
}

func (s *repoService) DeleteRepo(ctx context.Context, orgID, projectID string) error {
	repo, err := s.repo.GetByOrgAndProjectID(ctx, orgID, projectID)
	if err != nil {
		return fmt.Errorf("get repo: %w", err)
	}
	if repo == nil {
		// Nothing to delete IS the requested end state. Reporting ErrRepoNotFound
		// here made the project teardown log an error on every legitimate re-run
		// and gave callers no way to tell "already clean" from "cleanup broke".
		return nil
	}

	if err := s.repo.DeleteByOrgAndProjectID(ctx, orgID, projectID); err != nil {
		return fmt.Errorf("delete repo record: %w", err)
	}

	// The REMOTE is deliberately left standing, and this is the last moment
	// anything can name it: the row that resolved (org, project) to a URL has
	// just been dropped, so after this line the platform has no route back to
	// the repository, its issues, its milestones or the webhook registered at
	// create. Announce it rather than leak it silently — and note the practical
	// consequence, which is that the repo NAME stays taken: a project recreated
	// under it is refused with ErrRepoNameConflict until a human intervenes.
	slog.InfoContext(ctx, "repo record deleted; the remote repository is left in place",
		"org", orgID, "project", projectID, "repoUrl", repo.RepoURL)

	// Best-effort disk cleanup AFTER the DB delete succeeded: rename the
	// workspace subtree into trash (O(1); open fds keep working — design
	// §14). The hook logs its own failures and never fails this call.
	if s.workspaceTrash != nil {
		s.workspaceTrash(ctx, orgID, projectID, repo.WorkspaceSlug())
	}
	return nil
}

var repoSlugInvalid = regexp.MustCompile(`[^a-z0-9-]+`)

// slugifyProjectName produces the default repo name from the project name.
func slugifyProjectName(projectName string) string {
	slug := strings.ToLower(projectName)
	slug = repoSlugInvalid.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	if slug == "" {
		return "project"
	}
	return slug
}
