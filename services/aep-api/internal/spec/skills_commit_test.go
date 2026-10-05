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

package spec

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// skillsRepoRows is a git_repositories table holding org's skills repository
// row only, at url.
func skillsRepoRows(t *testing.T, org, url string) *testGitHost {
	t.Helper()
	host := newTestGitHost(t)
	host.rows[repoKey(org, SkillsRepoProject)] = &sourcecontrol.GitRepository{
		OrgID: org, ProjectID: SkillsRepoProject, RepoURL: url, DefaultBranch: "main", Status: "ready",
	}
	return host
}

// TestSkillsCommit_RetriesOnConflictAndKeepsConcurrentManifestEntry: a
// concurrent writer adds "other" to the manifest between the store's read
// and its commit; the commit's manifest baseSha no longer holds, the store
// re-reads and re-merges, and both entries survive.
func TestSkillsCommit_RetriesOnConflictAndKeepsConcurrentManifestEntry(t *testing.T) {
	ctx := context.Background()
	f := aestudiotest.New()
	skillsRef := sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "org-skills"}
	f.SeedRepo(skillsRef, map[string]string{
		"skills-manifest.json": `{}`,
		skillRepoPath("mine"):  mkSkillMD("mine", "", "my skill"),
	})
	raced := false
	f.BeforeCommit(func() {
		if raced {
			return
		}
		raced = true // a concurrent writer adds "other" between our read and our commit
		_, sha, _ := f.ReadFile(ctx, skillsRef, "", "skills-manifest.json")
		_, _ = f.Commit(ctx, skillsRef, sourcecontrol.CommitRequest{Message: "theirs", Writes: []sourcecontrol.FileWrite{{
			Path: "skills-manifest.json", Content: `{"other":{"origin":"imported","baseHash":"x"}}`, BaseSHA: sha}}})
	})
	svc := NewSkillService(f, f, skillsRepoRows(t, "default", "https://github.com/acme/org-skills"), fstest.MapFS{})

	if _, err := NewSkillMutationService(svc).SetEnabled(ctx, "default", "tester", "mine", false); err != nil {
		t.Fatal(err)
	}
	got, _, _ := f.ReadFile(ctx, skillsRef, "", "skills-manifest.json")
	if !strings.Contains(string(got), `"other"`) || !strings.Contains(string(got), `"mine"`) {
		t.Fatalf("manifest lost an entry: %s", got)
	}
	commits := 0
	for _, c := range f.Calls() {
		if c.Op == aestudiotest.OpCommit {
			commits++
		}
	}
	if commits != 3 { // ours (conflict), the racer's, ours again
		t.Fatalf("commits = %d, want 3", commits)
	}
}

// TestSkillsCommit_GivesUpAfterThreeConflicts: a path that keeps moving is a
// conflict after sourcecontrol.CommitAttempts tries, not an endless loop.
func TestSkillsCommit_GivesUpAfterThreeConflicts(t *testing.T) {
	ctx := context.Background()
	f := aestudiotest.New()
	skillsRef := sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "org-skills"}
	f.SeedRepo(skillsRef, map[string]string{skillRepoPath("mine"): mkSkillMD("mine", "", "my skill")})
	f.FailOp(aestudiotest.OpCommit, &sourcecontrol.CommitConflictError{Conflicts: []sourcecontrol.Conflict{{Path: "skills-manifest.json"}}})
	svc := NewSkillService(f, f, skillsRepoRows(t, "default", "https://github.com/acme/org-skills"), fstest.MapFS{})

	_, err := NewSkillMutationService(svc).SetEnabled(ctx, "default", "tester", "mine", false)
	if !errors.Is(err, sourcecontrol.ErrCommitConflict) {
		t.Fatalf("err = %v, want ErrCommitConflict", err)
	}
	commits := 0
	for _, c := range f.Calls() {
		if c.Op == aestudiotest.OpCommit {
			commits++
		}
	}
	if commits != sourcecontrol.CommitAttempts {
		t.Fatalf("commits = %d, want %d", commits, sourcecontrol.CommitAttempts)
	}
}

// TestSkillsCommit_PrefixDeleteExpandsToExactPaths: deleting a skill removes
// every file under its directory (the pod deletes exact paths), and a
// sibling whose name shares the prefix survives.
func TestSkillsCommit_PrefixDeleteExpandsToExactPaths(t *testing.T) {
	ctx := context.Background()
	f := aestudiotest.New()
	skillsRef := sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "org-skills"}
	f.SeedRepo(skillsRef, map[string]string{
		skillRepoPath("mine"):             mkSkillMD("mine", "", "my skill"),
		"skills/mine/references/r.md":     "ref",
		"skills/mine/scripts/run.sh":      "echo",
		skillRepoPath("mine-two"):         mkSkillMD("mine-two", "", "sibling"),
		"skills/mine-two/references/r.md": "sibling ref",
	})
	svc := NewSkillService(f, f, skillsRepoRows(t, "default", "https://github.com/acme/org-skills"), fstest.MapFS{})

	if err := NewSkillMutationService(svc).Delete(ctx, "default", "tester", "mine"); err != nil {
		t.Fatal(err)
	}
	files, _, _ := f.ReadBundle(ctx, skillsRef, "", sourcecontrol.BundleFilter{Prefix: "skills/"})
	for p := range files {
		if strings.HasPrefix(p, "skills/mine/") {
			t.Errorf("%s survived the delete", p)
		}
	}
	if files[skillRepoPath("mine-two")] == "" || files["skills/mine-two/references/r.md"] == "" {
		t.Fatalf("the sibling skill was deleted: %v", files)
	}
}
