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
	"errors"

	"gorm.io/gorm"
)

// RepoRepository manages GitRepository persistence. All lookups/deletes are
// org-scoped by signature (§6.1c): there is no org-blind accessor, so "forgot
// the org filter" is a compile error. project_id is only composite-unique with
// org_id, so an org-less query could touch another org's row.
type RepoRepository interface {
	GetByOrgAndProjectID(ctx context.Context, ocOrgID, projectID string) (*GitRepository, error)
	GetByOrgAndSlug(ctx context.Context, ocOrgID, repoSlug string) (*GitRepository, error)
	// FindInOrgByFullName returns the org's repo row whose clone URL is the
	// GitHub repository fullName ("owner/name"), or nil. The webhook ingest
	// checks a delivery's repository with it, so a repository another org
	// owns is never found.
	FindInOrgByFullName(ctx context.Context, ocOrgID, fullName string) (*GitRepository, error)
	// ListAllReady returns every repo in `ready` status across all orgs.
	// Used by cross-org sweeps (eventcore) and org-filtered project listing
	// (provisioning) — not a clone pre-warm. Bounded by the table size; not
	// paginated because the caller bounds concurrency separately.
	ListAllReady(ctx context.Context) ([]GitRepository, error)
	// ListAll returns every repo row across all orgs and ALL statuses.
	// Bounded by the table size, like ListAllReady.
	ListAll(ctx context.Context) ([]GitRepository, error)
	// ListByOrg returns the org's repo rows (all statuses). Feeds the
	// project-list repoUrl annotation (#108); one indexed query per page.
	ListByOrg(ctx context.Context, ocOrgID string) ([]GitRepository, error)
	Create(ctx context.Context, repo *GitRepository) error
	Update(ctx context.Context, repo *GitRepository) error
	// SetWebhookIDIfReady stores hookID on the (org, project) row only while
	// it is `ready`, as a column update (never re-inserting a row a
	// concurrent delete dropped). It reports whether a row took it.
	SetWebhookIDIfReady(ctx context.Context, ocOrgID, projectID string, hookID int64) (bool, error)
	// ClearWebhookIDs forgets the hook id of every row of the org.
	ClearWebhookIDs(ctx context.Context, ocOrgID string) error
	// SetStatusIf moves the (org, project) row from status `from` to `to` as
	// a column update, and reports whether it did.
	SetStatusIf(ctx context.Context, ocOrgID, projectID, from, to string) (bool, error)
	// DeleteByOrgAndProjectID deletes the repo row scoped to (ocOrgID,
	// projectID). Org-scoped because project_id is only composite-unique with
	// org_id — an org-less delete could remove another org's row.
	DeleteByOrgAndProjectID(ctx context.Context, ocOrgID, projectID string) error
}

type repoRepository struct {
	db *gorm.DB
}

func NewRepoRepository(db *gorm.DB) RepoRepository {
	return &repoRepository{db: db}
}

// GetByOrgAndProjectID returns the repo row matching the (ocOrgID, projectID)
// tuple or nil. Used by the org-scope middleware on the new
// /api/v1/repos/{orgId}/{projectId}/... routes to fail loudly (404) when a
// caller passes a path that doesn't match a stored row, instead of silently
// cross-accessing another org's repo.
func (r *repoRepository) GetByOrgAndProjectID(ctx context.Context, ocOrgID, projectID string) (*GitRepository, error) {
	var repo GitRepository
	if err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ?", ocOrgID, projectID).
		First(&repo).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &repo, nil
}

// GetByOrgAndSlug returns the repo row matching the (ocOrgID, repoSlug) tuple
// or nil. The fence behind MintBuildToken — `repoSlug` is treated as untrusted
// input from the BFF and only resolves if there's an active matching row.
func (r *repoRepository) GetByOrgAndSlug(ctx context.Context, ocOrgID, repoSlug string) (*GitRepository, error) {
	var repo GitRepository
	if err := r.db.WithContext(ctx).
		Where("org_id = ? AND repo_slug = ?", ocOrgID, repoSlug).
		First(&repo).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &repo, nil
}

func (r *repoRepository) FindInOrgByFullName(ctx context.Context, ocOrgID, fullName string) (*GitRepository, error) {
	if ocOrgID == "" || fullName == "" {
		return nil, nil
	}
	var rows []GitRepository
	if err := r.db.WithContext(ctx).
		Where("org_id = ? AND repo_url IN ?", ocOrgID, githubRepoURLs(fullName)).
		Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (r *repoRepository) ListAllReady(ctx context.Context) ([]GitRepository, error) {
	var rows []GitRepository
	if err := r.db.WithContext(ctx).
		Where("status = ?", RepoStatusReady).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *repoRepository) ListAll(ctx context.Context) ([]GitRepository, error) {
	var rows []GitRepository
	if err := r.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *repoRepository) ListByOrg(ctx context.Context, ocOrgID string) ([]GitRepository, error) {
	var rows []GitRepository
	if err := r.db.WithContext(ctx).
		Where("org_id = ?", ocOrgID).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *repoRepository) Create(ctx context.Context, repo *GitRepository) error {
	return r.db.WithContext(ctx).Create(repo).Error
}

func (r *repoRepository) Update(ctx context.Context, repo *GitRepository) error {
	return r.db.WithContext(ctx).Save(repo).Error
}

func (r *repoRepository) SetWebhookIDIfReady(ctx context.Context, ocOrgID, projectID string, hookID int64) (bool, error) {
	res := r.db.WithContext(ctx).Model(&GitRepository{}).
		Where("org_id = ? AND project_id = ? AND status = ?", ocOrgID, projectID, RepoStatusReady).
		Update("webhook_id", hookID)
	return res.RowsAffected > 0, res.Error
}

func (r *repoRepository) ClearWebhookIDs(ctx context.Context, ocOrgID string) error {
	return r.db.WithContext(ctx).Model(&GitRepository{}).
		Where("org_id = ? AND webhook_id IS NOT NULL", ocOrgID).
		Update("webhook_id", nil).Error
}

func (r *repoRepository) SetStatusIf(ctx context.Context, ocOrgID, projectID, from, to string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&GitRepository{}).
		Where("org_id = ? AND project_id = ? AND status = ?", ocOrgID, projectID, from).
		Update("status", to)
	return res.RowsAffected > 0, res.Error
}

func (r *repoRepository) DeleteByOrgAndProjectID(ctx context.Context, ocOrgID, projectID string) error {
	return r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ?", ocOrgID, projectID).
		Delete(&GitRepository{}).Error
}

// LookupOrgProjectByRepoURL translates a GitHub repo full_name to its owning
// AEP (orgID, projectID) via the git_repositories table. The repo row is the
// authority: repo URLs are globally unique, so the matched row disambiguates
// the project. Callers MUST scope task lookups by BOTH org_id and project_id —
// project_id is only a per-org slug and is reused across orgs, so a
// project_id-only filter collides across orgs that share a slug and can absorb
// a webhook into the wrong org's task. Pass a request-scoped *gorm.DB
// (db.WithContext(ctx)) or an open transaction.
func LookupOrgProjectByRepoURL(db *gorm.DB, repoFullName string) (orgID, projectID string, err error) {
	if repoFullName == "" {
		return "", "", nil
	}
	var r struct {
		OrgID     string
		ProjectID string
	}
	// INT-2 (sink): match the canonical clone URL EXACTLY, not with an
	// unanchored `ILIKE '%'+fullName` whose leading wildcard matched any host
	// and any path suffix — a payload could otherwise resolve another org's
	// repo. Anchored on host+owner+repo; both `.git` and bare shapes.
	err = db.Raw(`
		SELECT org_id, project_id
		FROM git_repositories
		WHERE repo_url IN ?
		LIMIT 1
	`, githubRepoURLs(repoFullName)).Scan(&r).Error
	if err != nil {
		return "", "", err
	}
	return r.OrgID, r.ProjectID, nil
}

// LookupOrgProjectByRepoURLInOrg is LookupOrgProjectByRepoURL restricted to
// orgID's rows: a webhook handler running under a delivery org resolves a
// repository only there, so a payload naming another org's repository yields
// ("", "", nil), the same as an unknown one.
func LookupOrgProjectByRepoURLInOrg(db *gorm.DB, orgID, repoFullName string) (string, string, error) {
	if orgID == "" || repoFullName == "" {
		return "", "", nil
	}
	var r struct {
		OrgID     string
		ProjectID string
	}
	err := db.Raw(`
		SELECT org_id, project_id
		FROM git_repositories
		WHERE org_id = ? AND repo_url IN ?
		LIMIT 1
	`, orgID, githubRepoURLs(repoFullName)).Scan(&r).Error
	if err != nil {
		return "", "", err
	}
	return r.OrgID, r.ProjectID, nil
}

// githubRepoURLs are the clone URLs a repo row stores for a GitHub full name:
// the canonical URL, bare and with ".git". Matched exactly (INT-2), never as a
// pattern.
func githubRepoURLs(fullName string) []string {
	canonical := "https://github.com/" + fullName
	return []string{canonical, canonical + ".git"}
}
