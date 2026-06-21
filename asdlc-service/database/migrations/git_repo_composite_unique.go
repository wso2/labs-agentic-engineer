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

package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// RunGitRepoCompositeUnique replaces the global UNIQUE on
// git_repositories.project_id with a composite UNIQUE (org_id, project_id), so
// two orgs can own a same-named project and repo resolution MUST be org-scoped
// (GetByOrgAndProjectID). Forward-only, expand→backfill→verify→contract (§9.1):
//
//  1. expand   — add the composite unique index (idempotent; AutoMigrate may
//                already have created it from the model tag).
//  2. verify   — abort if any project_id spans multiple orgs. The old global
//                unique guaranteed it cannot, but we re-check before dropping
//                it so the contract step is provably safe.
//  3. contract — drop the old global unique index on project_id.
//
// No backfill is needed (no new NOT NULL column). Recovery for a failed run is
// an RDS snapshot restore + corrected re-run (forward-only; no down migration).
// Must run AFTER AutoMigrate(&GitRepository{}) — AutoMigrate creates the new
// composite index from the model tag but never drops the old one.
func RunGitRepoCompositeUnique(ctx context.Context, db *gorm.DB) error {
	// 1. expand
	if err := db.WithContext(ctx).Exec(
		`CREATE UNIQUE INDEX IF NOT EXISTS ux_git_repositories_org_project
		   ON git_repositories (org_id, project_id)`).Error; err != nil {
		return fmt.Errorf("F2 expand (composite unique): %w", err)
	}

	// 2. verify — audit SQL from §9.1; expected 0 (the global unique forbade dupes).
	var spanning int64
	if err := db.WithContext(ctx).Raw(
		`SELECT count(*) FROM (
		   SELECT project_id FROM git_repositories
		   GROUP BY project_id HAVING count(DISTINCT org_id) > 1
		 ) t`).Scan(&spanning).Error; err != nil {
		return fmt.Errorf("F2 verify: %w", err)
	}
	if spanning > 0 {
		return fmt.Errorf("F2 aborted: %d project_id value(s) span multiple orgs; "+
			"resolve the collisions before dropping the global unique", spanning)
	}

	// 3. contract — drop the old global unique on project_id.
	if err := db.WithContext(ctx).Exec(
		`DROP INDEX IF EXISTS idx_git_repositories_project_id`).Error; err != nil {
		return fmt.Errorf("F2 contract (drop global project_id unique): %w", err)
	}
	return nil
}
