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
	"errors"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// newRepoSvcOnFake is a REAL repoService over the in-memory pod; every org's
// repositories live under the GitHub login "test-org".
func newRepoSvcOnFake(repo *fakeRepoRepo) (sourcecontrol.RepoService, *aestudiotest.Fake) {
	f := aestudiotest.New()
	return sourcecontrol.NewRepoService(repo, f, f, fakeOwners{owner: "test-org"}, "private"), f
}

// createCalls answers the create-repo calls the Fake saw.
func createCalls(f *aestudiotest.Fake) []aestudiotest.Call {
	var out []aestudiotest.Call
	for _, c := range f.Calls() {
		if c.Op == aestudiotest.OpCreateRepo {
			out = append(out, c)
		}
	}
	return out
}

func TestCreateRepo_HappyPathMarksReady(t *testing.T) {
	t.Parallel()
	repo := newFakeRepoRepo()
	svc, f := newRepoSvcOnFake(repo)

	got, err := svc.CreateRepo(testContext(), "org1", "proj1", "My Project", "")
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	// The repo is ready the moment GitHub has it — no clone, no "cloning" state.
	if got.Status != "ready" || got.DefaultBranch != "main" {
		t.Fatalf("row = {status:%q branch:%q}, want {ready main}", got.Status, got.DefaultBranch)
	}
	if got.RepoURL != "https://github.com/test-org/my-project.git" {
		t.Fatalf("row url = %q, want the pod's clone url", got.RepoURL)
	}

	// The create went to the org's pod, under the org's GitHub login, with
	// the visibility and the EXACT slug-derived name — never a random suffix;
	// the name the user saw in the create form is the name the repo gets.
	calls := createCalls(f)
	if len(calls) != 1 {
		t.Fatalf("create calls = %d, want 1", len(calls))
	}
	c := calls[0]
	if c.Ref != (sourcecontrol.RepoRef{Org: "org1", Owner: "test-org", Repo: "my-project", DefaultBranch: "main"}) {
		t.Fatalf("create ref = %+v", c.Ref)
	}
	if !c.Repo.Private || c.Repo.AdoptExisting {
		t.Fatalf("create request = %+v, want private, no adoption", c.Repo)
	}
	if !strings.Contains(c.Repo.Description, "My Project") {
		t.Fatalf("description = %q, want it to mention the project name", c.Repo.Description)
	}
}

// Conflicts are NEVER suffixed away — for the derived name too, the create
// fails with the sentinel so the user is asked for a different name.
func TestCreateRepo_DerivedNameConflictFailsWithoutRetry(t *testing.T) {
	t.Parallel()
	repo := newFakeRepoRepo()
	svc, f := newRepoSvcOnFake(repo)
	f.FailOp(aestudiotest.OpCreateRepo, sourcecontrol.ErrRepoNameConflict)

	_, err := svc.CreateRepo(testContext(), "org1", "proj1", "My Project", "")
	if !sourcecontrol.IsRepoNameConflict(err) {
		t.Fatalf("err = %v, want the ErrRepoNameConflict sentinel to survive", err)
	}
	if n := len(createCalls(f)); n != 1 {
		t.Fatalf("pod called %d times, want exactly 1 (no suffix retries)", n)
	}
}

func TestCreateRepo_IsIdempotentOnExistingRow(t *testing.T) {
	t.Parallel()
	repo := newFakeRepoRepo()
	existing := &sourcecontrol.GitRepository{OrgID: "org1", ProjectID: "proj1", RepoURL: "https://github.com/test-org/pre", Status: "ready"}
	repo.preload(existing)
	svc, f := newRepoSvcOnFake(repo)

	got, err := svc.CreateRepo(testContext(), "org1", "proj1", "My Project", "")
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if got.RepoURL != existing.RepoURL {
		t.Fatalf("returned row url = %q, want existing %q", got.RepoURL, existing.RepoURL)
	}
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("the pod was called %d times on an idempotent re-create, want 0", n)
	}
}

// A row a crashed project delete left `deleting` is never adopted by a
// create of the same project: its teardown (run supervisors, hook, row) has
// not finished, so the create is refused until the delete is re-run, and the
// pod is never asked.
func TestCreateRepo_RefusesARowLeftDeleting(t *testing.T) {
	t.Parallel()
	repo := newFakeRepoRepo()
	repo.preload(&sourcecontrol.GitRepository{OrgID: "org1", ProjectID: "proj1", RepoURL: "https://github.com/test-org/old", Status: sourcecontrol.RepoStatusDeleting})
	svc, f := newRepoSvcOnFake(repo)

	got, err := svc.CreateRepo(testContext(), "org1", "proj1", "My Project", "")
	if !errors.Is(err, sourcecontrol.ErrRepoDeletePending) || got != nil {
		t.Fatalf("got %+v, err %v; want nil and ErrRepoDeletePending", got, err)
	}
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("the pod was called %d times, want 0", n)
	}
	if r, _ := repo.GetByOrgAndProjectID(testContext(), "org1", "proj1"); r == nil || r.Status != sourcecontrol.RepoStatusDeleting {
		t.Fatalf("the deleting row must be left for the delete re-run, got %+v", r)
	}
}

func TestCreateRepo_ErrorPropagatesAndCreatesNoRow(t *testing.T) {
	t.Parallel()
	repo := newFakeRepoRepo()
	svc, f := newRepoSvcOnFake(repo)
	f.FailOrg("org1", sourcecontrol.ErrAEStudioUnavailable)

	_, err := svc.CreateRepo(testContext(), "org1", "proj1", "My Project", "")
	if !errors.Is(err, sourcecontrol.ErrAEStudioUnavailable) || !strings.Contains(err.Error(), "create github repo") {
		t.Fatalf("err = %v, want a create-github-repo ErrAEStudioUnavailable", err)
	}
	if r, _ := repo.GetByOrgAndProjectID(testContext(), "org1", "proj1"); r != nil {
		t.Fatalf("a repo row was created despite the pod failure: %+v", r)
	}
}

// An org with no GitHub connection has no owner to create under: the
// lookup's ErrAEStudioAbsent reaches the caller and the pod is never asked.
func TestCreateRepo_NoGitHubConnectionIsAbsent(t *testing.T) {
	t.Parallel()
	repo := newFakeRepoRepo()
	f := aestudiotest.New()
	svc := sourcecontrol.NewRepoService(repo, f, f, fakeOwners{err: sourcecontrol.ErrAEStudioAbsent}, "private")

	if _, err := svc.CreateRepo(testContext(), "org1", "proj1", "My Project", ""); !errors.Is(err, sourcecontrol.ErrAEStudioAbsent) {
		t.Fatalf("err = %v, want ErrAEStudioAbsent", err)
	}
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("the pod was called %d times, want 0", n)
	}
}

func TestCreateRepo_ExplicitRepoNameUsedVerbatim(t *testing.T) {
	t.Parallel()
	repo := newFakeRepoRepo()
	svc, f := newRepoSvcOnFake(repo)

	got, err := svc.CreateRepo(testContext(), "org1", "proj1", "My Project", "exact-repo")
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if got.RepoURL != "https://github.com/test-org/exact-repo.git" {
		t.Fatalf("row url = %q, want the exact-name clone url", got.RepoURL)
	}
	// A user-chosen repo name is used VERBATIM — no random suffix.
	if c := createCalls(f); len(c) != 1 || c[0].Ref.Repo != "exact-repo" {
		t.Fatalf("create calls = %+v, want one for exact-repo", c)
	}
}

func TestCreateRepo_ExplicitRepoNameConflictFailsWithoutRetry(t *testing.T) {
	t.Parallel()
	repo := newFakeRepoRepo()
	svc, f := newRepoSvcOnFake(repo)
	taken := sourcecontrol.RepoRef{Org: "org1", Owner: "test-org", Repo: "taken-repo"}
	if _, err := f.CreateOrgRepo(testContext(), taken, sourcecontrol.CreateOrgRepoRequest{}); err != nil {
		t.Fatal(err)
	}

	_, err := svc.CreateRepo(testContext(), "org1", "proj1", "My Project", "taken-repo")
	if !sourcecontrol.IsRepoNameConflict(err) {
		t.Fatalf("err = %v, want the ErrRepoNameConflict sentinel to survive", err)
	}
	// The name was chosen by the user — retrying with suffixes would betray it.
	if n := len(createCalls(f)); n != 2 {
		t.Fatalf("create calls = %d, want the seed + exactly 1 (no suffix retries)", n)
	}
	if r, _ := repo.GetByOrgAndProjectID(testContext(), "org1", "proj1"); r != nil {
		t.Fatalf("a repo row was created despite the conflict: %+v", r)
	}
}

// The skills repo keeps a stable name across a lost row: the pod adopts the
// repository that already carries it.
func TestEnsureBareRepo_AdoptsAnExistingRepo(t *testing.T) {
	t.Parallel()
	repo := newFakeRepoRepo()
	svc, f := newRepoSvcOnFake(repo)
	skills := sourcecontrol.RepoRef{Org: "org1", Owner: "test-org", Repo: "org-skills"}
	if _, err := f.CreateOrgRepo(testContext(), skills, sourcecontrol.CreateOrgRepoRequest{}); err != nil {
		t.Fatal(err)
	}

	got, err := svc.EnsureBareRepo(testContext(), "org1", "_skills", "org-skills")
	if err != nil {
		t.Fatalf("EnsureBareRepo: %v", err)
	}
	if got.RepoURL != "https://github.com/test-org/org-skills.git" {
		t.Fatalf("adopted url = %q", got.RepoURL)
	}
	if got.Status != "ready" || got.DefaultBranch != "main" {
		t.Fatalf("adopted row = {status:%q branch:%q}, want {ready main}", got.Status, got.DefaultBranch)
	}
	calls := createCalls(f)
	if last := calls[len(calls)-1]; !last.Repo.AdoptExisting || !last.Repo.Private || last.Ref.Repo != "org-skills" {
		t.Fatalf("ensure request = %+v, want a private adopting create of org-skills", last)
	}
	if r, _ := repo.GetByOrgAndProjectID(testContext(), "org1", "_skills"); r == nil {
		t.Fatal("adopted repo row was not persisted")
	}
}
