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
	svc := sourcecontrol.NewRepoService(repo, aestudiotest.New(), fakeOwners{owner: "test-org"}, "private")

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
	svc := sourcecontrol.NewRepoService(repo, aestudiotest.New(), fakeOwners{owner: "test-org"}, "private")

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
	svc := sourcecontrol.NewRepoService(repo, aestudiotest.New(), fakeOwners{owner: "test-org"}, "private")

	for attempt := 1; attempt <= 2; attempt++ {
		if err := svc.DeleteRepo(testContext(), "org1", "proj1"); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
}
