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
	ref := repo.RepoRef{Owner: "Acme-GH", Repo: "Org-Skills"}
	const root = "/workspaces"

	repoDir, err := repo.RepoDir(root, ref)
	if err != nil || repoDir != "/workspaces/repos/acme-gh/org-skills" {
		t.Fatalf("RepoDir = (%q, %v), want the lower-cased owner/repo key", repoDir, err)
	}
	if repo.TrashDir(root) != "/workspaces/trash" || repo.TmpDir(root) != "/workspaces/tmp" ||
		repo.ReposDir(root) != "/workspaces/repos" {
		t.Fatal("root-level dirs derived wrong")
	}
}

// GitHub names are case-insensitive: every spelling of one repository is one
// mirror, so the Room's save path and a direct owner/repo caller share
// one lock and one disk copy.
func TestRepoDirIsCaseInsensitive(t *testing.T) {
	const root = "/workspaces"
	a, errA := repo.RepoDir(root, repo.RepoRef{Owner: "Acme", Repo: "Greeter-App"})
	b, errB := repo.RepoDir(root, repo.RepoRef{Owner: "acme", Repo: "greeter-app"})
	if errA != nil || errB != nil || a != b {
		t.Fatalf("RepoDir = (%q, %v) vs (%q, %v), want one dir", a, errA, b, errB)
	}
}

func TestPathDerivationRejectsHostileSegments(t *testing.T) {
	const root = "/workspaces"
	base := repo.RepoRef{Owner: "owner", Repo: "repo"}

	mutations := []func(*repo.RepoRef){
		func(r *repo.RepoRef) { r.Owner = "" },
		func(r *repo.RepoRef) { r.Owner = ".." },
		func(r *repo.RepoRef) { r.Owner = "a/b" },
		func(r *repo.RepoRef) { r.Repo = "" },
		func(r *repo.RepoRef) { r.Repo = "." },
		func(r *repo.RepoRef) { r.Repo = "p\\q" },
		func(r *repo.RepoRef) { r.Repo = "s g" },
		func(r *repo.RepoRef) { r.Repo = "../../etc" },
		func(r *repo.RepoRef) { r.Repo = strings.Repeat("x", 201) },
	}
	for i, mutate := range mutations {
		ref := base
		mutate(&ref)
		if _, err := repo.RepoDir(root, ref); err == nil {
			t.Errorf("mutation %d: RepoDir accepted hostile segment %+v", i, ref)
		}
	}
}

// Segments are validated as GitHub's ASCII charset BEFORE lower-casing: a
// non-ASCII name whose Unicode lower case is ASCII (the Kelvin sign K, U+212A,
// lowers to "k") must be refused, never alias an ASCII mirror or store.
func TestOwnerRepoRefusesNonASCIIThatLowersToASCII(t *testing.T) {
	const root = "/workspaces"
	for _, ref := range []repo.RepoRef{
		{Owner: "\u212Aacme", Repo: "greeter"},
		{Owner: "acme", Repo: "greeter-\u212A"},
	} {
		if dir, err := repo.RepoDir(root, ref); err == nil {
			t.Errorf("RepoDir(%+v) = %q, want refused", ref, dir)
		}
		if dir, err := repo.ReferenceStoreDir(root, ref.OwnerRepo()); err == nil {
			t.Errorf("ReferenceStoreDir(%+v) = %q, want refused", ref, dir)
		}
	}
}
