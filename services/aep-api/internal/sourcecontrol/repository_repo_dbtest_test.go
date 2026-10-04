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

package sourcecontrol_test

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// TestRepoRepository_OrgScopedIsolation exercises real Postgres (no mock) to
// confirm RepoRepository.GetByOrgAndProjectID scopes by org and never returns
// another org's row. Runs on a pristine per-test database (dbtest.New); it
// skips under -short.
func TestRepoRepository_OrgScopedIsolation(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)

	repo := sourcecontrol.NewRepoRepository(db)
	ctx := context.Background()

	mk := func(org, proj, url string) {
		t.Helper()
		if err := repo.Create(ctx, &sourcecontrol.GitRepository{
			OrgID: org, ProjectID: proj, RepoURL: url, Status: "ready",
		}); err != nil {
			t.Fatalf("create (%s,%s): %v", org, proj, err)
		}
	}
	mk("orga", "proj-a", "https://github.com/a/proj-a")
	mk("orgb", "proj-b", "https://github.com/b/proj-b")

	// Org-scoped lookup returns the caller's own row.
	got, err := repo.GetByOrgAndProjectID(ctx, "orga", "proj-a")
	if err != nil {
		t.Fatalf("GetByOrgAndProjectID(orga): %v", err)
	}
	if got == nil || got.OrgID != "orga" {
		t.Fatalf("expected orga row, got %+v", got)
	}

	// A different org asking for org-a's project resolves to nil — no leak.
	cross, err := repo.GetByOrgAndProjectID(ctx, "orgb", "proj-a")
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
	t.Parallel()
	db := dbtest.New(t)

	repo := sourcecontrol.NewRepoRepository(db)
	ctx := context.Background()

	const slug = "shared-slug"
	// Same project slug under two different orgs — permitted by the composite
	// (org_id, project_id) unique.
	if err := repo.Create(ctx, &sourcecontrol.GitRepository{OrgID: "orga", ProjectID: slug, RepoURL: "https://github.com/a/x", Status: "ready"}); err != nil {
		t.Fatalf("create (orga,%s): %v", slug, err)
	}
	if err := repo.Create(ctx, &sourcecontrol.GitRepository{OrgID: "orgb", ProjectID: slug, RepoURL: "https://github.com/b/x", Status: "ready"}); err != nil {
		t.Fatalf("create (orgb,%s) — composite unique should permit a same-slug project in another org: %v", slug, err)
	}

	a, err := repo.GetByOrgAndProjectID(ctx, "orga", slug)
	if err != nil || a == nil {
		t.Fatalf("get (orga,%s): %v / %v", slug, a, err)
	}
	b, err := repo.GetByOrgAndProjectID(ctx, "orgb", slug)
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

// mkRepo persists a GitRepository row against real Postgres and returns it.
func mkRepo(t *testing.T, repo sourcecontrol.RepoRepository, r *sourcecontrol.GitRepository) *sourcecontrol.GitRepository {
	t.Helper()
	if r.Status == "" {
		r.Status = "ready"
	}
	if err := repo.Create(context.Background(), r); err != nil {
		t.Fatalf("create repo (%s/%s): %v", r.OrgID, r.ProjectID, err)
	}
	return r
}

// TestRepoRepository_GetByOrgAndSlug_OrgScoped confirms the (org_id, repo_slug)
// fence behind MintBuildToken: a decoy row with the same slug under another org
// must not resolve for the caller's org.
func TestRepoRepository_GetByOrgAndSlug_OrgScoped(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := sourcecontrol.NewRepoRepository(db)
	ctx := context.Background()

	mkRepo(t, repo, &sourcecontrol.GitRepository{OrgID: "orga", ProjectID: "proj-a", RepoURL: "https://github.com/a/todo", RepoSlug: "a-todo"})
	mkRepo(t, repo, &sourcecontrol.GitRepository{OrgID: "orgb", ProjectID: "proj-b", RepoURL: "https://github.com/b/todo", RepoSlug: "a-todo"}) // same slug, other org

	got, err := repo.GetByOrgAndSlug(ctx, "orga", "a-todo")
	if err != nil {
		t.Fatalf("GetByOrgAndSlug: %v", err)
	}
	if got == nil || got.OrgID != "orga" || got.RepoURL != "https://github.com/a/todo" {
		t.Fatalf("GetByOrgAndSlug returned the wrong row: %+v", got)
	}

	// Unknown slug under the owner org → nil.
	miss, err := repo.GetByOrgAndSlug(ctx, "orga", "no-such-slug")
	if err != nil {
		t.Fatalf("GetByOrgAndSlug(miss): %v", err)
	}
	if miss != nil {
		t.Fatalf("unknown slug leaked a row: %+v", miss)
	}
}

// TestRepoRepository_ListAllReady returns only `ready` rows and — by design for
// cross-org sweeps — is NOT org-scoped (it spans every org's ready repos).
func TestRepoRepository_ListAllReady(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := sourcecontrol.NewRepoRepository(db)
	ctx := context.Background()

	mkRepo(t, repo, &sourcecontrol.GitRepository{OrgID: "orga", ProjectID: "p1", RepoURL: "https://github.com/a/p1", Status: "ready"})
	mkRepo(t, repo, &sourcecontrol.GitRepository{OrgID: "orgb", ProjectID: "p2", RepoURL: "https://github.com/b/p2", Status: "ready"})
	mkRepo(t, repo, &sourcecontrol.GitRepository{OrgID: "orga", ProjectID: "p3", RepoURL: "https://github.com/a/p3", Status: "pending"}) // excluded

	got, err := repo.ListAllReady(ctx)
	if err != nil {
		t.Fatalf("ListAllReady: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 ready rows across orgs; got %d (%+v)", len(got), got)
	}
	for _, r := range got {
		if r.Status != "ready" {
			t.Fatalf("non-ready row returned: %+v", r)
		}
	}
}

// TestRepoRepository_ListAll returns every row across orgs and ALL statuses —
// the disk reaper's orphan pass must see pending/error rows too, or their
// dirs would be misread as orphans.
func TestRepoRepository_ListAll(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := sourcecontrol.NewRepoRepository(db)
	ctx := context.Background()

	mkRepo(t, repo, &sourcecontrol.GitRepository{OrgID: "orga", ProjectID: "p1", RepoURL: "https://github.com/a/p1", Status: "ready"})
	mkRepo(t, repo, &sourcecontrol.GitRepository{OrgID: "orgb", ProjectID: "p2", RepoURL: "https://github.com/b/p2", Status: "pending"})
	mkRepo(t, repo, &sourcecontrol.GitRepository{OrgID: "orga", ProjectID: "p3", RepoURL: "https://github.com/a/p3", Status: "error"})

	got, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want all 3 rows regardless of status; got %d (%+v)", len(got), got)
	}
}

// TestRepoRepository_Update round-trips a full-row Save.
func TestRepoRepository_Update(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := sourcecontrol.NewRepoRepository(db)
	ctx := context.Background()

	r := mkRepo(t, repo, &sourcecontrol.GitRepository{OrgID: "orga", ProjectID: "proj", RepoURL: "https://github.com/a/proj", Status: "pending"})
	r.Status = "ready"
	r.DefaultBranch = "trunk"
	if err := repo.Update(ctx, r); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := repo.GetByOrgAndProjectID(ctx, "orga", "proj")
	if err != nil || got == nil {
		t.Fatalf("reload: %v / %v", got, err)
	}
	if got.Status != "ready" || got.DefaultBranch != "trunk" {
		t.Fatalf("Update did not persist: %+v", got)
	}
}

// TestRepoRepository_DeleteByOrgAndProjectID_OrgScoped deletes only the scoped
// row; a same-slug row in another org survives.
func TestRepoRepository_DeleteByOrgAndProjectID_OrgScoped(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := sourcecontrol.NewRepoRepository(db)
	ctx := context.Background()

	mkRepo(t, repo, &sourcecontrol.GitRepository{OrgID: "orga", ProjectID: "shared", RepoURL: "https://github.com/a/x"})
	mkRepo(t, repo, &sourcecontrol.GitRepository{OrgID: "orgb", ProjectID: "shared", RepoURL: "https://github.com/b/x"})

	if err := repo.DeleteByOrgAndProjectID(ctx, "orga", "shared"); err != nil {
		t.Fatalf("DeleteByOrgAndProjectID: %v", err)
	}
	if got, _ := repo.GetByOrgAndProjectID(ctx, "orga", "shared"); got != nil {
		t.Fatalf("scoped delete did not remove the row: %+v", got)
	}
	// The other org's same-slug row is untouched.
	if got, _ := repo.GetByOrgAndProjectID(ctx, "orgb", "shared"); got == nil {
		t.Fatalf("scoped delete crossed into another org's row")
	}
}

// TestLookupOrgProjectByRepoURL confirms the anchored (exact) match on both the
// bare and `.git` clone URL shapes, and — critically (INT-2) — that it does NOT
// do an unanchored suffix/substring match.
func TestLookupOrgProjectByRepoURL(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := sourcecontrol.NewRepoRepository(db)
	ctx := context.Background()

	mkRepo(t, repo, &sourcecontrol.GitRepository{OrgID: "orga", ProjectID: "proj-a", RepoURL: "https://github.com/acme/todo"})
	mkRepo(t, repo, &sourcecontrol.GitRepository{OrgID: "orgb", ProjectID: "proj-b", RepoURL: "https://github.com/other/todo.git"})

	// Bare full_name resolves the bare-URL row.
	org, proj, err := sourcecontrol.LookupOrgProjectByRepoURL(db.WithContext(ctx), "acme/todo")
	if err != nil {
		t.Fatalf("Lookup(acme/todo): %v", err)
	}
	if org != "orga" || proj != "proj-a" {
		t.Fatalf("Lookup(acme/todo) = (%q,%q); want (orga,proj-a)", org, proj)
	}

	// full_name resolves the `.git`-suffixed row too (canonical+".git").
	org, proj, err = sourcecontrol.LookupOrgProjectByRepoURL(db.WithContext(ctx), "other/todo")
	if err != nil {
		t.Fatalf("Lookup(other/todo): %v", err)
	}
	if org != "orgb" || proj != "proj-b" {
		t.Fatalf("Lookup(other/todo) = (%q,%q); want (orgb,proj-b)", org, proj)
	}

	// Empty input short-circuits to empty (no query).
	if o, p, err := sourcecontrol.LookupOrgProjectByRepoURL(db.WithContext(ctx), ""); err != nil || o != "" || p != "" {
		t.Fatalf("Lookup(\"\") = (%q,%q,%v); want empty", o, p, err)
	}

	// A bare repo name that a leading-wildcard LIKE would have matched must NOT
	// resolve — the anchored equality is the INT-2 fix.
	if o, p, err := sourcecontrol.LookupOrgProjectByRepoURL(db.WithContext(ctx), "todo"); err != nil || o != "" || p != "" {
		t.Fatalf("Lookup(todo) = (%q,%q,%v); want empty (no unanchored suffix match)", o, p, err)
	}
}

// TestRepoRepository_ListByOrg proves the list-projects repoUrl join source
// (#108): only the caller org's rows come back, all statuses included.
func TestRepoRepository_ListByOrg(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)

	repo := sourcecontrol.NewRepoRepository(db)
	ctx := context.Background()

	for _, r := range []sourcecontrol.GitRepository{
		{OrgID: "orga", ProjectID: "web", RepoURL: "https://github.com/a/web.git", Status: "ready"},
		{OrgID: "orga", ProjectID: "api", RepoURL: "https://github.com/a/api.git", Status: "pending"},
		{OrgID: "orgb", ProjectID: "web", RepoURL: "https://github.com/b/web.git", Status: "ready"},
	} {
		if err := repo.Create(ctx, &r); err != nil {
			t.Fatalf("create %s/%s: %v", r.OrgID, r.ProjectID, err)
		}
	}

	rows, err := repo.ListByOrg(ctx, "orga")
	if err != nil {
		t.Fatalf("ListByOrg: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ListByOrg(orga) returned %d rows, want 2 (all statuses, no cross-org leak)", len(rows))
	}
	for _, r := range rows {
		if r.OrgID != "orga" {
			t.Fatalf("cross-org leak: %+v", r)
		}
	}

	empty, err := repo.ListByOrg(ctx, "orgc")
	if err != nil {
		t.Fatalf("ListByOrg(orgc): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("ListByOrg(orgc) = %d rows, want 0", len(empty))
	}
}

// FindInOrgByFullName finds only the org's own row, by the bare or .git clone
// URL, and never by a pattern: the same repository URL on another org's row,
// a longer name sharing the prefix, or another host is no match.
func TestRepoRepository_FindInOrgByFullName(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := sourcecontrol.NewRepoRepository(db)
	ctx := context.Background()
	mkRepo(t, repo, &sourcecontrol.GitRepository{OrgID: "orga", ProjectID: "p1", RepoURL: "https://github.com/acme/greeter.git", Status: "ready"})
	mkRepo(t, repo, &sourcecontrol.GitRepository{OrgID: "orgb", ProjectID: "p9", RepoURL: "https://github.com/acme/other", Status: "ready"})
	mkRepo(t, repo, &sourcecontrol.GitRepository{OrgID: "orga", ProjectID: "p2", RepoURL: "https://evil.example/acme/hosted", Status: "ready"})

	got, err := repo.FindInOrgByFullName(ctx, "orga", "acme/greeter")
	if err != nil || got == nil || got.ProjectID != "p1" {
		t.Fatalf("orga acme/greeter = %+v %v, want p1", got, err)
	}
	for _, tc := range []struct{ org, name string }{
		{"orgb", "acme/greeter"}, // another org's repository
		{"orga", "acme/other"},   // orgb's repository, asked in orga
		{"orga", "acme/greet"},   // a prefix
		{"orga", "acme/hosted"},  // another host
		{"orga", ""},
		{"", "acme/greeter"},
	} {
		if got, err := repo.FindInOrgByFullName(ctx, tc.org, tc.name); err != nil || got != nil {
			t.Errorf("FindInOrgByFullName(%q, %q) = %+v %v, want nil", tc.org, tc.name, got, err)
		}
	}
}

// The hook id and the delete mark are column updates: a store lands only on
// a ready row, never re-creates a row a delete dropped, and the mark moves
// only from the status it expects.
func TestRepoRepository_HookIDAndDeleteMarkAreColumnUpdates(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := sourcecontrol.NewRepoRepository(db)
	ctx := context.Background()
	mkRepo(t, repo, &sourcecontrol.GitRepository{OrgID: "orga", ProjectID: "p1", RepoURL: "https://github.com/a/p1", Status: "ready"})
	mkRepo(t, repo, &sourcecontrol.GitRepository{OrgID: "orgb", ProjectID: "p2", RepoURL: "https://github.com/b/p2", Status: "ready"})

	if took, err := repo.SetWebhookIDIfReady(ctx, "orga", "p1", 7); err != nil || !took {
		t.Fatalf("store on a ready row: took=%v err=%v", took, err)
	}
	if took, err := repo.SetWebhookIDIfReady(ctx, "orga", "gone", 8); err != nil || took {
		t.Fatalf("store on no row: took=%v err=%v", took, err)
	}
	if got, _ := repo.GetByOrgAndProjectID(ctx, "orga", "gone"); got != nil {
		t.Fatal("a store re-created a row")
	}

	if moved, err := repo.SetStatusIf(ctx, "orga", "p1", sourcecontrol.RepoStatusReady, sourcecontrol.RepoStatusDeleting); err != nil || !moved {
		t.Fatalf("mark deleting: moved=%v err=%v", moved, err)
	}
	if moved, _ := repo.SetStatusIf(ctx, "orga", "p1", sourcecontrol.RepoStatusReady, sourcecontrol.RepoStatusDeleting); moved {
		t.Fatal("a second mark moved a row that is not ready")
	}
	if took, _ := repo.SetWebhookIDIfReady(ctx, "orga", "p1", 9); took {
		t.Fatal("a deleting row took a hook id")
	}
	if rows, _ := repo.ListAllReady(ctx); len(rows) != 1 || rows[0].ProjectID != "p2" {
		t.Fatalf("ListAllReady = %v, want only orgb/p2", rows)
	}

	if _, err := repo.SetWebhookIDIfReady(ctx, "orgb", "p2", 10); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClearWebhookIDs(ctx, "orga"); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.GetByOrgAndProjectID(ctx, "orga", "p1"); got.WebhookID != nil {
		t.Fatalf("orga/p1 keeps hook id %d", *got.WebhookID)
	}
	if got, _ := repo.GetByOrgAndProjectID(ctx, "orgb", "p2"); got.WebhookID == nil || *got.WebhookID != 10 {
		t.Fatal("another org's hook id was cleared")
	}
}
