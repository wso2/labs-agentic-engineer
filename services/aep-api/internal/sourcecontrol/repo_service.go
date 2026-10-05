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
	// user can be asked for a different name. A ready row of the project is
	// returned as is; a row still `deleting` is ErrRepoDeletePending.
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
	// so cleanup can deregister. Only a `ready` row takes it: a row that is
	// gone or whose project is being deleted is ErrRepoNotFound.
	SetWebhookID(ctx context.Context, orgID, projectID string, hookID int64) error
	// BeginDelete marks the project's row `deleting` before its teardown
	// starts, so no sweep lists it and no hook id lands on it from then on.
	// No row (or one already marked) is success.
	BeginDelete(ctx context.Context, orgID, projectID string) error
	// AbortDelete puts a `deleting` row back to `ready` (a teardown that
	// stopped before it changed anything).
	AbortDelete(ctx context.Context, orgID, projectID string) error
	// DeleteRepo trashes the org pod's mirror and reference documents of the
	// repository, then drops the repo record. It ensures ABSENCE rather than
	// performing a removal, so a project with no repo row — never
	// provisioned, or a teardown being re-run after it got this far once —
	// succeeds with nothing to do. A trash the pod cannot do is logged and the
	// row goes anyway. The remote is untouched.
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
	trash   TrashOps
	owners  OwnerLookup
	repoVis string
}

func NewRepoService(
	repo RepoRepository,
	github RepoAdmin,
	trash TrashOps,
	owners OwnerLookup,
	repoVisibility string,
) RepoService {
	return &repoService{
		repo:    repo,
		github:  github,
		trash:   trash,
		owners:  owners,
		repoVis: repoVisibility,
	}
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
		// A row left `deleting` belongs to a project delete that stopped
		// midway: adopting it would write the new project into the old
		// repository while its teardown is still owed.
		if existing.Status != RepoStatusReady {
			slog.WarnContext(ctx, "repo.create_refused_delete_pending", "org", orgID, "project", projectID, "status", existing.Status)
			return nil, ErrRepoDeletePending
		}
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
	// StageBuildSecret to validate (ocOrgId, repoSlug) ownership. A build
	// references the org's github-pat SecretReference, not a per-repo one, so
	// no SecretReference name is computed here; OcSecretRefName is left nil on
	// new rows.
	repoSlug := RepoSlugFor(cloneURL)

	// The repo is ready the moment GitHub has it: the mirror is created
	// lazily on first access, so there is no "cloning" status to wait through
	// at create time.
	gitRepo := &GitRepository{
		OrgID:         orgID,
		ProjectID:     projectID,
		RepoURL:       cloneURL,
		DefaultBranch: defaultBranchFallback, // the pod initialises a main branch + base tree
		Status:        RepoStatusReady,
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
		Status:        RepoStatusReady, // the mirror is created lazily on first access
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
	took, err := s.repo.SetWebhookIDIfReady(ctx, orgID, projectID, hookID)
	if err != nil {
		return fmt.Errorf("store webhook id: %w", err)
	}
	if !took {
		return ErrRepoNotFound
	}
	return nil
}

func (s *repoService) BeginDelete(ctx context.Context, orgID, projectID string) error {
	if _, err := s.repo.SetStatusIf(ctx, orgID, projectID, RepoStatusReady, RepoStatusDeleting); err != nil {
		return fmt.Errorf("mark repo deleting: %w", err)
	}
	return nil
}

func (s *repoService) AbortDelete(ctx context.Context, orgID, projectID string) error {
	if _, err := s.repo.SetStatusIf(ctx, orgID, projectID, RepoStatusDeleting, RepoStatusReady); err != nil {
		return fmt.Errorf("unmark repo deleting: %w", err)
	}
	return nil
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

	// Trash BEFORE the row goes: the row is what names the repository, so
	// once it is gone nothing can ask the pod to drop its mirror and the
	// project's reference documents. Best-effort, like the rest of the
	// teardown: a pod that cannot trash (restarting, or absent after a
	// disconnect, when its data went with the Resource) must not keep a
	// deleted project's row alive in every sweep and in the hook repair.
	if ref, rerr := RefForRow(orgID, repo); rerr != nil {
		slog.WarnContext(ctx, "repo.trash_failed", "org", orgID, "project", projectID, "error", rerr)
	} else if terr := s.trash.TrashRepo(ctx, ref); terr != nil {
		slog.WarnContext(ctx, "repo.trash_failed", "org", orgID, "project", projectID, "error", terr)
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
