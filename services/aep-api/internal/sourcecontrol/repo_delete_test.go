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
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// DeleteRepo ensures ABSENCE rather than performing a removal. Its only caller
// is the project teardown, which is best-effort and re-runnable, so "there was
// nothing to delete" has to be reported as success — reporting ErrRepoNotFound
// made every legitimate re-run log a cleanup error and left callers unable to
// tell "already clean" from "cleanup broke".

func TestDeleteRepo_DropsTheRow(t *testing.T) {
	t.Parallel()
	repo := newFakeRepoRepo()
	repo.put(&sourcecontrol.GitRepository{
		OrgID: "org1", ProjectID: "proj1",
		RepoURL: "https://github.com/test-org/proj1.git",
		Status:  "ready", RepoSlug: "test-org-proj1",
	})
	svc := sourcecontrol.NewRepoService(repo, aestudiotest.New(), aestudiotest.New(), fakeOwners{owner: "test-org"}, "private")

	if err := svc.DeleteRepo(testContext(), "org1", "proj1"); err != nil {
		t.Fatalf("DeleteRepo: %v", err)
	}
	// GetRepo, unlike DeleteRepo, is a lookup: an absent row is ErrRepoNotFound.
	if _, err := svc.GetRepo(testContext(), "org1", "proj1"); !errors.Is(err, sourcecontrol.ErrRepoNotFound) {
		t.Fatalf("row survived the delete: %v", err)
	}
}

// TestDeleteRepo_AbsentRowIsSuccess covers both shapes of "nothing to delete":
// a project whose repo was never provisioned, and a teardown being re-run after
// an earlier attempt already got this far.
func TestDeleteRepo_AbsentRowIsSuccess(t *testing.T) {
	t.Parallel()
	repo := newFakeRepoRepo()
	svc := sourcecontrol.NewRepoService(repo, aestudiotest.New(), aestudiotest.New(), fakeOwners{owner: "test-org"}, "private")

	if err := svc.DeleteRepo(testContext(), "org1", "never-provisioned"); err != nil {
		t.Fatalf("deleting an absent repo must succeed, got %v", err)
	}
}

func TestDeleteRepo_IsIdempotent(t *testing.T) {
	t.Parallel()
	repo := newFakeRepoRepo()
	repo.put(&sourcecontrol.GitRepository{
		OrgID: "org1", ProjectID: "proj1",
		RepoURL: "https://github.com/test-org/proj1.git",
		Status:  "ready", RepoSlug: "test-org-proj1",
	})
	svc := sourcecontrol.NewRepoService(repo, aestudiotest.New(), aestudiotest.New(), fakeOwners{owner: "test-org"}, "private")

	for attempt := 1; attempt <= 2; attempt++ {
		if err := svc.DeleteRepo(testContext(), "org1", "proj1"); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
}

// DeleteRepo trashes the pod's mirror and reference documents of the
// project's repository BEFORE it drops the row: the row is
// what names the repository, so after it is gone nothing can ask the pod to
// drop them.
func TestDeleteRepo_TrashesThePodMirrorBeforeTheRow(t *testing.T) {
	t.Parallel()
	repo := newFakeRepoRepo()
	repo.put(&sourcecontrol.GitRepository{
		OrgID: "org1", ProjectID: "proj1",
		RepoURL: "https://github.com/test-org/proj1.git",
		Status:  "ready", RepoSlug: "test-org-proj1",
	})
	pod := aestudiotest.New()
	pod.BeforeTrash(func() {
		if row, _ := repo.GetByOrgAndProjectID(testContext(), "org1", "proj1"); row == nil {
			t.Error("the row was dropped before the trash")
		}
	})
	svc := sourcecontrol.NewRepoService(repo, pod, pod, fakeOwners{owner: "test-org"}, "private")

	if err := svc.DeleteRepo(testContext(), "org1", "proj1"); err != nil {
		t.Fatalf("DeleteRepo: %v", err)
	}
	var trashed []sourcecontrol.RepoRef
	for _, c := range pod.Calls() {
		if c.Op == aestudiotest.OpTrashRepo {
			trashed = append(trashed, c.Ref)
		}
	}
	want := sourcecontrol.RepoRef{Org: "org1", Owner: "test-org", Repo: "proj1", DefaultBranch: "main"}
	if len(trashed) != 1 || trashed[0] != want {
		t.Fatalf("trash calls = %+v, want one for %+v", trashed, want)
	}
}

// A trash the pod cannot do (restarting, or absent after a disconnect) does
// not keep the row: a row left behind would keep a deleted project in every
// sweep and in the hook repair. The delete is best-effort, as today.
func TestDeleteRepo_TrashFailureStillDropsTheRow(t *testing.T) {
	t.Parallel()
	repo := newFakeRepoRepo()
	repo.put(&sourcecontrol.GitRepository{
		OrgID: "org1", ProjectID: "proj1",
		RepoURL: "https://github.com/test-org/proj1.git",
		Status:  "ready",
	})
	pod := aestudiotest.New()
	pod.FailOp(aestudiotest.OpTrashRepo, sourcecontrol.ErrAEStudioUnavailable)
	svc := sourcecontrol.NewRepoService(repo, pod, pod, fakeOwners{owner: "test-org"}, "private")

	if err := svc.DeleteRepo(testContext(), "org1", "proj1"); err != nil {
		t.Fatalf("DeleteRepo: %v", err)
	}
	if _, err := svc.GetRepo(testContext(), "org1", "proj1"); !errors.Is(err, sourcecontrol.ErrRepoNotFound) {
		t.Fatalf("row survived a failed trash: %v", err)
	}
}

// BeginDelete marks a ready row deleting (no sweep lists it, no hook id lands
// on it); AbortDelete puts it back. No row is success for both.
func TestRepoDeleteMark_BeginAndAbort(t *testing.T) {
	t.Parallel()
	repo := newFakeRepoRepo()
	repo.put(&sourcecontrol.GitRepository{OrgID: "org1", ProjectID: "proj1", RepoURL: "https://github.com/test-org/proj1.git", Status: "ready"})
	pod := aestudiotest.New()
	svc := sourcecontrol.NewRepoService(repo, pod, pod, fakeOwners{owner: "test-org"}, "private")

	if err := svc.BeginDelete(testContext(), "org1", "proj1"); err != nil {
		t.Fatal(err)
	}
	if row, _ := svc.GetRepo(testContext(), "org1", "proj1"); row.Status != sourcecontrol.RepoStatusDeleting {
		t.Fatalf("status %q, want deleting", row.Status)
	}
	if err := svc.SetWebhookID(testContext(), "org1", "proj1", 5); !errors.Is(err, sourcecontrol.ErrRepoNotFound) {
		t.Fatalf("SetWebhookID on a deleting row: %v, want ErrRepoNotFound", err)
	}
	if err := svc.AbortDelete(testContext(), "org1", "proj1"); err != nil {
		t.Fatal(err)
	}
	if row, _ := svc.GetRepo(testContext(), "org1", "proj1"); row.Status != sourcecontrol.RepoStatusReady {
		t.Fatalf("status %q, want ready", row.Status)
	}
	if err := svc.BeginDelete(testContext(), "org1", "none"); err != nil {
		t.Fatalf("BeginDelete with no row: %v", err)
	}
}
