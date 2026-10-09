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

package app

import (
	"context"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/delivery/validation"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// oneRepoRow is a git_repositories table holding org's project p only.
type oneRepoRow struct{ org, project, url string }

func (r oneRepoRow) GetByOrgAndProjectID(_ context.Context, org, project string) (*sourcecontrol.GitRepository, error) {
	if org != r.org || project != r.project {
		return nil, nil
	}
	return &sourcecontrol.GitRepository{OrgID: org, ProjectID: project, RepoURL: r.url, Status: "ready"}, nil
}

var greeterRef = sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "greeter"}

func greeterFiles(t *testing.T, files map[string]string) (*aestudiotest.Fake, projectFiles) {
	t.Helper()
	f := aestudiotest.New()
	f.SeedRepo(greeterRef, files)
	return f, projectFiles{git: f, repos: oneRepoRow{org: "default", project: "p", url: "https://github.com/acme/greeter"}}
}

func TestWorkloadReader_PresentAndAbsent(t *testing.T) {
	ctx := context.Background()
	_, pf := greeterFiles(t, map[string]string{"services/api/workload.yaml": "kind: Workload"})
	r := workloadReader{pf}

	got, ok, err := r.ReadFile(ctx, "default", "p", "services/api/workload.yaml")
	if err != nil || !ok || got != "kind: Workload" {
		t.Fatalf("present = (%q, %v, %v), want the file", got, ok, err)
	}
	got, ok, err = r.ReadFile(ctx, "default", "p", "services/web/workload.yaml")
	if err != nil || ok || got != "" {
		t.Fatalf("absent = (%q, %v, %v), want found=false and no error", got, ok, err)
	}
}

func TestDesignFilesCommitter_ReadFileCarriesTheBlobSha(t *testing.T) {
	ctx := context.Background()
	f, pf := greeterFiles(t, map[string]string{"specs/design/design.cell": "cell"})
	c := designFilesCommitter{git: pf.git, repos: pf.repos}

	content, sha, ok, err := c.ReadFile(ctx, "default", "p", "specs/design/design.cell")
	if err != nil || !ok || content != "cell" {
		t.Fatalf("ReadFile = (%q, %v, %v)", content, ok, err)
	}
	_, want, _ := f.ReadFile(ctx, greeterRef, "", "specs/design/design.cell")
	if sha != want {
		t.Errorf("sha = %q, want the blob sha %q (the CAS token)", sha, want)
	}
	if _, _, ok, err := c.ReadFile(ctx, "default", "p", "specs/design/dependencies/x/dependency.json"); ok || err != nil {
		t.Errorf("absent file = (ok %v, err %v), want ok=false and no error", ok, err)
	}
}

// TestDesignCommitter_CommitsUnderEachBaseSha: every write lands in one
// commit, the existing file under its read blob sha and the new one under ""
// (must not exist yet).
func TestDesignCommitter_CommitsUnderEachBaseSha(t *testing.T) {
	ctx := context.Background()
	f, pf := greeterFiles(t, map[string]string{"specs/design/design.cell": "cell"})
	c := designFilesCommitter{git: pf.git, repos: pf.repos}
	_, sha, _, _ := c.ReadFile(ctx, "default", "p", "specs/design/design.cell")
	head, _ := f.Head(ctx, greeterRef, "")

	err := c.Commit(ctx, "default", "p", []spec.DesignFileWrite{
		{Path: "specs/design/design.cell", Content: "cell v2", BaseSHA: sha},
		{Path: "specs/design/dependencies/x/dependency.json", Content: "{}"},
	}, "design: x")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	files, after, _ := f.ReadBundle(ctx, greeterRef, "", sourcecontrol.BundleFilter{Prefix: "specs/design/"})
	if files["specs/design/design.cell"] != "cell v2" || files["specs/design/dependencies/x/dependency.json"] != "{}" {
		t.Fatalf("tip = %v, want both writes", files)
	}
	if after == head {
		t.Fatal("no commit landed")
	}
}

// TestDesignCommitter_MapsConflict: a baseSha that no longer holds is
// spec.ErrSpecCommitConflict (the route's 409), not retried.
func TestDesignCommitter_MapsConflict(t *testing.T) {
	f := aestudiotest.New()
	f.SeedRepo(sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "g"}, map[string]string{})
	f.FailOp(aestudiotest.OpCommit, &sourcecontrol.CommitConflictError{Conflicts: []sourcecontrol.Conflict{{Path: "specs/design/design.cell"}}})
	err := designFilesCommitter{git: f, repos: oneRepoRow{org: "default", project: "p", url: "https://github.com/acme/g"}}.
		Commit(context.Background(), "default", "p", []spec.DesignFileWrite{{Path: "specs/design/design.cell", Content: "x"}}, "m")
	if !errors.Is(err, spec.ErrSpecCommitConflict) {
		t.Fatalf("err = %v, want ErrSpecCommitConflict", err)
	}
	commits := 0
	for _, c := range f.Calls() {
		if c.Op == aestudiotest.OpCommit {
			commits++
		}
	}
	if commits != 1 {
		t.Fatalf("commits = %d, want 1 (a stale baseSha is not retried)", commits)
	}
}

func TestAcceptanceCriteria_ScenarioFilesOfTheDirectoryOnly(t *testing.T) {
	ctx := context.Background()
	f, pf := greeterFiles(t, map[string]string{
		validation.AcceptanceDirPath + "/b.feature":         "Feature: b",
		validation.AcceptanceDirPath + "/a.feature":         "Feature: a",
		validation.AcceptanceDirPath + "/README.md":         "notes",
		validation.AcceptanceDirPath + "-old/stale.feature": "Feature: stale",
		"specs/requirements/prd.md":                         "# prd",
		validation.ReportFilePath:                           `{"verdict":"pass"}`,
	})
	a := acceptanceCriteria{pf}

	files, found, err := a.ReadAcceptanceCriteria(ctx, "default", "p")
	if err != nil || !found {
		t.Fatalf("ReadAcceptanceCriteria = (found %v, err %v)", found, err)
	}
	if len(files) != 2 || files[0].Path != validation.AcceptanceDirPath+"/a.feature" || files[1].Path != validation.AcceptanceDirPath+"/b.feature" {
		t.Fatalf("files = %+v, want a.feature then b.feature only", files)
	}

	head, _ := f.Head(ctx, greeterRef, "")
	at, err := a.CriteriaAt(ctx, "default", "p", head)
	if err != nil || len(at) != 2 || at[0].Content != "Feature: a" {
		t.Fatalf("CriteriaAt(head) = (%+v, %v)", at, err)
	}
	report, found, err := a.ReportAt(ctx, "default", "p", head)
	if err != nil || !found || report != `{"verdict":"pass"}` {
		t.Fatalf("ReportAt = (%q, %v, %v)", report, found, err)
	}
}

func TestAcceptanceCriteria_AbsentOracleAndReport(t *testing.T) {
	ctx := context.Background()
	f, pf := greeterFiles(t, map[string]string{"specs/requirements/prd.md": "# prd"})
	a := acceptanceCriteria{pf}
	head, _ := f.Head(ctx, greeterRef, "")

	if files, found, err := a.ReadAcceptanceCriteria(ctx, "default", "p"); err != nil || found || len(files) != 0 {
		t.Fatalf("absent oracle = (%v, %v, %v), want found=false", files, found, err)
	}
	if files, err := a.CriteriaAt(ctx, "default", "p", head); err != nil || files == nil || len(files) != 0 {
		t.Fatalf("CriteriaAt absent = (%v, %v), want an empty slice", files, err)
	}
	if _, found, err := a.ReportAt(ctx, "default", "p", head); err != nil || found {
		t.Fatalf("ReportAt absent = (found %v, err %v), want found=false", found, err)
	}
	// A commit the repository does not have is an absent oracle, not a failure.
	if files, err := a.CriteriaAt(ctx, "default", "p", "0123456789abcdef0123456789abcdef01234567"); err != nil || len(files) != 0 {
		t.Fatalf("CriteriaAt unknown commit = (%v, %v), want empty", files, err)
	}
	raw, err := runValidation{projectFiles: pf}.report(ctx, "default", "p", head)
	if err != nil || raw != nil {
		t.Fatalf("run report absent = (%q, %v), want nil bytes (unreported)", raw, err)
	}
}

func TestProjectFiles_ErrorsAreNotAbsence(t *testing.T) {
	ctx := context.Background()
	f, pf := greeterFiles(t, map[string]string{"specs/requirements/prd.md": "# prd"})

	if _, _, err := (workloadReader{pf}).ReadFile(ctx, "other-org", "p", "workload.yaml"); !errors.Is(err, sourcecontrol.ErrRepoNotFound) {
		t.Errorf("another org's project: err = %v, want ErrRepoNotFound", err)
	}
	f.FailOp(aestudiotest.OpReadFile, sourcecontrol.ErrAEStudioUnavailable)
	f.FailOp(aestudiotest.OpReadBundle, sourcecontrol.ErrAEStudioUnavailable)
	if _, ok, err := (workloadReader{pf}).ReadFile(ctx, "default", "p", "workload.yaml"); ok || !errors.Is(err, sourcecontrol.ErrAEStudioUnavailable) {
		t.Errorf("pod down: (ok %v, err %v), want ErrAEStudioUnavailable", ok, err)
	}
	if _, found, err := (acceptanceCriteria{pf}).ReadAcceptanceCriteria(ctx, "default", "p"); found || !errors.Is(err, sourcecontrol.ErrAEStudioUnavailable) {
		t.Errorf("pod down: (found %v, err %v), want ErrAEStudioUnavailable", found, err)
	}
}
