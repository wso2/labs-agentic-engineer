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
	"context"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/gen"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
	"github.com/wso2/aep/ae-studio-tools/internal/repo/repotest"
)

// TestTrashRepo_MovesTheMirrorToTrash: trash-repo moves owner/repo's mirror
// into trash/ (any case of the name), a missing mirror is success, and the
// next op on the repository clones it again.
func TestTrashRepo_MovesTheMirrorToTrash(t *testing.T) {
	origin := repotest.NewOrigin(t, map[string]string{"specs/a.md": "a"})
	engine := NewEngine(t, nil)
	h := repo.NewHandler(engine, nil, func(string, string) string { return origin.URL() })
	ctx := context.Background()
	if _, err := h.GetHead(ctx, gen.GetHeadRequestObject{Owner: "acme", Repo: "greeter"}); err != nil {
		t.Fatal(err)
	}
	dir, err := repo.RepoDir(engine.Root(), repo.RepoRef{Owner: "acme", Repo: "greeter"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("mirror not cloned: %v", err)
	}
	for range 2 { // the second finds no mirror: still 204
		resp, err := h.TrashRepo(ctx, gen.TrashRepoRequestObject{Body: &gen.TrashRepoRequest{Owner: "ACME", Repo: "Greeter"}})
		if err != nil {
			t.Fatal(err)
		}
		if status, _ := answer(t, resp.VisitTrashRepoResponse); status != http.StatusNoContent {
			t.Fatalf("status %d", status)
		}
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mirror still at %s (%v)", dir, err)
	}
	if entries, _ := os.ReadDir(repo.TrashDir(engine.Root())); len(entries) != 1 {
		t.Fatalf("trash holds %d entries, want the one mirror", len(entries))
	}
	head, err := h.GetHead(ctx, gen.GetHeadRequestObject{Owner: "acme", Repo: "greeter"})
	if err != nil {
		t.Fatal(err)
	}
	if got := head.(gen.GetHead200JSONResponse).Sha; got != origin.HeadSHA(t) {
		t.Fatalf("head after trash %q", got)
	}
}

// TestTrashRepo_RefusesABadName: a name the store cannot hold is 400.
func TestTrashRepo_RefusesABadName(t *testing.T) {
	h := repo.NewHandler(NewEngine(t, nil), nil, repo.GitHubCloneURL)
	resp, err := h.TrashRepo(context.Background(), gen.TrashRepoRequestObject{Body: &gen.TrashRepoRequest{Owner: "..", Repo: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	wantProblem(t, resp.VisitTrashRepoResponse, http.StatusBadRequest, "validation_failed")
}
