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

//go:build dbtest

package repositories_test

import (
	"context"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/internal/platform/dbtest"
	"github.com/wso2/asdlc/asdlc-service/models"
	"github.com/wso2/asdlc/asdlc-service/repositories"
)

// TestRepoRepository_OrgScopedIsolation exercises real Postgres (no mock) to
// confirm RepoRepository.GetByOrgAndProjectID scopes by org and never returns
// another org's row.
func TestRepoRepository_OrgScopedIsolation(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db, &models.GitRepository{})
	dbtest.CleanupRows(t, db, &models.GitRepository{}, "org_id LIKE ?", "dbtest-%")

	repo := repositories.NewRepoRepository(db)
	ctx := context.Background()

	mk := func(org, proj, url string) {
		t.Helper()
		if err := repo.Create(ctx, &models.GitRepository{
			OrgID: org, ProjectID: proj, RepoURL: url, Status: "ready",
		}); err != nil {
			t.Fatalf("create (%s,%s): %v", org, proj, err)
		}
	}
	mk("dbtest-orga", "dbtest-proj-a", "https://github.com/a/proj-a")
	mk("dbtest-orgb", "dbtest-proj-b", "https://github.com/b/proj-b")

	// Org-scoped lookup returns the caller's own row.
	got, err := repo.GetByOrgAndProjectID(ctx, "dbtest-orga", "dbtest-proj-a")
	if err != nil {
		t.Fatalf("GetByOrgAndProjectID(orga): %v", err)
	}
	if got == nil || got.OrgID != "dbtest-orga" {
		t.Fatalf("expected orga row, got %+v", got)
	}

	// A different org asking for org-a's project resolves to nil — no leak.
	cross, err := repo.GetByOrgAndProjectID(ctx, "dbtest-orgb", "dbtest-proj-a")
	if err != nil {
		t.Fatalf("GetByOrgAndProjectID(orgb→proj-a): %v", err)
	}
	if cross != nil {
		t.Fatalf("cross-org lookup leaked a row: %+v", cross)
	}
}

// TestRepoRepository_TwoOrgsSameProjectSlug confirms the composite
// (org_id, project_id) unique lets two different orgs own a project with the
// same slug, and each org's GetByOrgAndProjectID resolves only its own row.
func TestRepoRepository_TwoOrgsSameProjectSlug(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db, &models.GitRepository{})
	dbtest.CleanupRows(t, db, &models.GitRepository{}, "org_id LIKE ?", "dbtest-%")

	repo := repositories.NewRepoRepository(db)
	ctx := context.Background()

	const slug = "dbtest-shared-slug"
	// Same project slug under two different orgs — permitted by the composite
	// (org_id, project_id) unique.
	if err := repo.Create(ctx, &models.GitRepository{OrgID: "dbtest-orga", ProjectID: slug, RepoURL: "https://github.com/a/x", Status: "ready"}); err != nil {
		t.Fatalf("create (orga,%s): %v", slug, err)
	}
	if err := repo.Create(ctx, &models.GitRepository{OrgID: "dbtest-orgb", ProjectID: slug, RepoURL: "https://github.com/b/x", Status: "ready"}); err != nil {
		t.Fatalf("create (orgb,%s) — composite unique should permit a same-slug project in another org: %v", slug, err)
	}

	a, err := repo.GetByOrgAndProjectID(ctx, "dbtest-orga", slug)
	if err != nil || a == nil {
		t.Fatalf("get (orga,%s): %v / %v", slug, a, err)
	}
	b, err := repo.GetByOrgAndProjectID(ctx, "dbtest-orgb", slug)
	if err != nil || b == nil {
		t.Fatalf("get (orgb,%s): %v / %v", slug, b, err)
	}
	if a.ID == b.ID || a.OrgID == b.OrgID {
		t.Fatalf("same-slug rows not isolated by org: a=%+v b=%+v", a, b)
	}
	if a.RepoURL != "https://github.com/a/x" || b.RepoURL != "https://github.com/b/x" {
		t.Fatalf("org-scoped lookup returned the wrong org's row: a=%s b=%s", a.RepoURL, b.RepoURL)
	}
}
