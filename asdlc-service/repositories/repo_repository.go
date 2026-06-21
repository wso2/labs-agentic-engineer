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

package repositories

import (
	"context"
	"errors"

	"github.com/wso2/asdlc/asdlc-service/models"
	"gorm.io/gorm"
)

// RepoRepository manages GitRepository persistence. All lookups/deletes are
// org-scoped by signature (§6.1c): there is no org-blind accessor, so "forgot
// the org filter" is a compile error. project_id is only composite-unique with
// org_id, so an org-less query could touch another org's row.
type RepoRepository interface {
	GetByOrgAndProjectID(ctx context.Context, ocOrgID, projectID string) (*models.GitRepository, error)
	GetByOrgAndSlug(ctx context.Context, ocOrgID, repoSlug string) (*models.GitRepository, error)
	// ListAllReady returns every repo in `ready` status. Used by the
	// startup pre-warm path to ensure clones are on disk before traffic
	// arrives. Bounded by the table size; not paginated because the
	// caller bounds concurrency separately.
	ListAllReady(ctx context.Context) ([]models.GitRepository, error)
	Create(ctx context.Context, repo *models.GitRepository) error
	Update(ctx context.Context, repo *models.GitRepository) error
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
func (r *repoRepository) GetByOrgAndProjectID(ctx context.Context, ocOrgID, projectID string) (*models.GitRepository, error) {
	var repo models.GitRepository
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
func (r *repoRepository) GetByOrgAndSlug(ctx context.Context, ocOrgID, repoSlug string) (*models.GitRepository, error) {
	var repo models.GitRepository
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

func (r *repoRepository) ListAllReady(ctx context.Context) ([]models.GitRepository, error) {
	var rows []models.GitRepository
	if err := r.db.WithContext(ctx).
		Where("status = ?", "ready").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *repoRepository) Create(ctx context.Context, repo *models.GitRepository) error {
	return r.db.WithContext(ctx).Create(repo).Error
}

func (r *repoRepository) Update(ctx context.Context, repo *models.GitRepository) error {
	return r.db.WithContext(ctx).Save(repo).Error
}

func (r *repoRepository) DeleteByOrgAndProjectID(ctx context.Context, ocOrgID, projectID string) error {
	return r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ?", ocOrgID, projectID).
		Delete(&models.GitRepository{}).Error
}

// LookupOrgProjectByRepoURL translates a GitHub repo full_name to its owning
// ASDLC (orgID, projectID) via the git_repositories table. The repo row is the
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
	canonical := "https://github.com/" + repoFullName
	err = db.Raw(`
		SELECT org_id, project_id
		FROM git_repositories
		WHERE repo_url = ? OR repo_url = ?
		LIMIT 1
	`, canonical, canonical+".git").Scan(&r).Error
	if err != nil {
		return "", "", err
	}
	return r.OrgID, r.ProjectID, nil
}
