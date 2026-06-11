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
	"log/slog"

	"gorm.io/gorm"
)

// RunPerOrgSecretName collapses the per-repo `oc_secret_ref_name` column
// to the post-2f26614 per-org shape (`git-<ocOrgID>`). Every active
// git_repositories row in an org references the same per-org build secret
// (see models.BuildSecretName / docs/design/cross-component-wiring-gaps.md
// follow-up).
//
// Backfill rule: for each (org_id, project_id) row whose oc_secret_ref_name
// is non-NULL, overwrite to `git-<lower(org_id)>`. NULL rows stay NULL —
// the first MintBuildToken call after this migration re-populates them via
// the standard provisioning path.
//
// Idempotent: re-running is a no-op once every row already has the
// canonical per-org value.
func RunPerOrgSecretName(ctx context.Context, db *gorm.DB) error {
	var exists bool
	if err := db.WithContext(ctx).Raw(
		`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_schema='public' AND table_name='git_repositories')`,
	).Scan(&exists).Error; err != nil {
		return fmt.Errorf("per_org_secret_name: check table existence: %w", err)
	}
	if !exists {
		return nil
	}

	res := db.WithContext(ctx).Exec(`
		UPDATE git_repositories
		   SET oc_secret_ref_name = 'git-' || lower(org_id)
		 WHERE oc_secret_ref_name IS NOT NULL
		   AND oc_secret_ref_name <> 'git-' || lower(org_id)
	`)
	if res.Error != nil {
		return fmt.Errorf("per_org_secret_name: backfill: %w", res.Error)
	}
	if res.RowsAffected > 0 {
		slog.Info("per_org_secret_name migration: collapsed oc_secret_ref_name to per-org shape",
			"rows", res.RowsAffected)
	}
	return nil
}
