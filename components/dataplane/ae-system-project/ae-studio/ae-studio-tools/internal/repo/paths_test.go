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

package repo_test

import (
	"strings"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

func TestPathDerivation(t *testing.T) {
	ref := repo.RepoRef{Org: "org-1", Project: "_skills", RepoSlug: "org-skills"}
	const root = "/workspaces"

	repoDir, err := repo.RepoDir(root, ref)
	if err != nil || repoDir != "/workspaces/repos/org-1/_skills/org-skills" {
		t.Fatalf("RepoDir = (%q, %v)", repoDir, err)
	}
	if repo.TrashDir(root) != "/workspaces/trash" || repo.TmpDir(root) != "/workspaces/tmp" ||
		repo.ReposDir(root) != "/workspaces/repos" {
		t.Fatal("root-level dirs derived wrong")
	}
}

func TestPathDerivationRejectsHostileSegments(t *testing.T) {
	const root = "/workspaces"
	base := repo.RepoRef{Org: "org", Project: "proj", RepoSlug: "slug"}

	mutations := []func(*repo.RepoRef){
		func(r *repo.RepoRef) { r.Org = "" },
		func(r *repo.RepoRef) { r.Org = ".." },
		func(r *repo.RepoRef) { r.Org = "a/b" },
		func(r *repo.RepoRef) { r.Project = "." },
		func(r *repo.RepoRef) { r.Project = "p\\q" },
		func(r *repo.RepoRef) { r.RepoSlug = "s g" },
		func(r *repo.RepoRef) { r.RepoSlug = "../../etc" },
		func(r *repo.RepoRef) { r.RepoSlug = strings.Repeat("x", 201) },
	}
	for i, mutate := range mutations {
		ref := base
		mutate(&ref)
		if _, err := repo.RepoDir(root, ref); err == nil {
			t.Errorf("mutation %d: RepoDir accepted hostile segment %+v", i, ref)
		}
	}
}
