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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/gen"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
	"github.com/wso2/aep/ae-studio-tools/internal/repo/repotest"
)

// newHandlerWithOrigin serves the handler over a real engine (rooted in
// t.TempDir()) whose every owner/repo clones the one file:// origin seeded
// with files.
func newHandlerWithOrigin(t *testing.T, files map[string]string) (repo.Handler, *repotest.Origin) {
	t.Helper()
	origin := repotest.NewOrigin(t, files)
	return repo.NewHandler(NewEngine(t, nil), nil, func(string, string) string { return origin.URL() }), origin
}

// answer writes resp through its generated Visit method and returns the
// status and the decoded JSON body.
func answer(t *testing.T, visit func(http.ResponseWriter) error) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := visit(rec); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body %q: %v", rec.Body.String(), err)
		}
	}
	return rec.Code, body
}

// wantProblem asserts a problem answer's status and code.
func wantProblem(t *testing.T, visit func(http.ResponseWriter) error, status int, code string) map[string]any {
	t.Helper()
	got, body := answer(t, visit)
	if got != status || body["code"] != code {
		t.Fatalf("got %d %v, want %d %s", got, body, status, code)
	}
	return body
}

func TestCreateCommit_BaseShaConflictAnd200(t *testing.T) {
	h, origin := newHandlerWithOrigin(t, map[string]string{"specs/requirements.md": "v1"})
	ctx := context.Background()
	cur := origin.BlobSHA(t, "specs/requirements.md")

	ok, err := h.CreateCommit(ctx, gen.CreateCommitRequestObject{
		Owner: "acme", Repo: "greeter",
		Body: &gen.CreateCommitJSONRequestBody{
			Writes:  []gen.CommitWrite{{Path: "specs/requirements.md", Content: []byte("v2"), BaseSha: cur}},
			Message: "edit",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	res := ok.(gen.CreateCommit200JSONResponse)
	if !res.Changed || len(res.CommitSha) != 40 {
		t.Fatalf("got %+v, want a changed commit", res)
	}

	stale, err := h.CreateCommit(ctx, gen.CreateCommitRequestObject{
		Owner: "acme", Repo: "greeter",
		Body: &gen.CreateCommitJSONRequestBody{
			Writes:  []gen.CommitWrite{{Path: "specs/requirements.md", Content: []byte("v3"), BaseSha: cur}},
			Message: "stale",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	conflict, isConflict := stale.(gen.CreateCommit409ApplicationProblemPlusJSONResponse)
	if !isConflict || conflict.Code != "conflict" || len(conflict.Conflicts) != 1 {
		t.Fatalf("got %#v, want 409 conflict with one entry", stale)
	}
}

func TestReadBundle_ExactPathAndExtFilter(t *testing.T) {
	h, _ := newHandlerWithOrigin(t, map[string]string{
		"specs/.agentic-engineer.toml":          "idea",
		"specs/design/components/a/design.json": "{}",
		"specs/design/components/a/notes.md":    "x",
	})
	got, err := h.ReadBundle(context.Background(), gen.ReadBundleRequestObject{
		Owner: "acme", Repo: "greeter",
		Params: gen.ReadBundleParams{Prefix: "specs/design/components/", Ext: []string{".json"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	files := got.(gen.ReadBundle200JSONResponse).Files
	if len(files) != 1 || string(files["specs/design/components/a/design.json"]) != "{}" {
		t.Fatalf("files = %v", files)
	}
	exact, _ := h.ReadBundle(context.Background(), gen.ReadBundleRequestObject{
		Owner: "acme", Repo: "greeter",
		Params: gen.ReadBundleParams{Path: []string{"specs/.agentic-engineer.toml"}},
	})
	if string(exact.(gen.ReadBundle200JSONResponse).Files["specs/.agentic-engineer.toml"]) != "idea" {
		t.Fatal("exact-path form must read dot-files")
	}
}

// pngHead is the first bytes of a PNG file: not valid UTF-8 (0x89).
const pngHead = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\xff\xfe"

// TestReadBundle_BinaryFilesSurviveTheWire reads a PNG and an invalid-UTF-8
// blob through the handler and its JSON reply: each file is base64 on the
// wire and decodes to the exact bytes committed.
func TestReadBundle_BinaryFilesSurviveTheWire(t *testing.T) {
	files := map[string]string{
		"skills/a/logo.png": pngHead,
		"skills/a/raw.bin":  "ok\xc3\x28\xa0\xa1end",
		"skills/a/SKILL.md": "# A ✓",
	}
	h, _ := newHandlerWithOrigin(t, files)
	resp, err := h.ReadBundle(context.Background(), gen.ReadBundleRequestObject{
		Owner: "acme", Repo: "greeter", Params: gen.ReadBundleParams{Prefix: "skills/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := resp.VisitReadBundleResponse(rec); err != nil {
		t.Fatal(err)
	}
	var wire gen.Bundle
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	if len(wire.Files) != len(files) {
		t.Fatalf("files = %d, want %d", len(wire.Files), len(files))
	}
	for p, want := range files {
		if got := string(wire.Files[p]); got != want {
			t.Fatalf("%s = %q, want %q", p, got, want)
		}
	}
}

// commitReq is a create-commit request for acme/greeter.
func commitReq(body gen.CreateCommitRequest) gen.CreateCommitRequestObject {
	return gen.CreateCommitRequestObject{Owner: "acme", Repo: "greeter", Body: &body}
}

func TestCreateCommit_RefusesMalformedRequests(t *testing.T) {
	h, origin := newHandlerWithOrigin(t, map[string]string{"a.md": "a"})
	a := origin.BlobSHA(t, "a.md")
	head := origin.HeadSHA(t)
	cases := []struct {
		name string
		body gen.CreateCommitRequest
		code string
	}{
		{"no write or delete", gen.CreateCommitRequest{Message: "m"}, "validation_failed"},
		{"blank message", gen.CreateCommitRequest{Message: "  \n", Writes: []gen.CommitWrite{{Path: "b.md", Content: []byte("b")}}}, "validation_failed"},
		{"a path written twice", gen.CreateCommitRequest{Message: "m", Writes: []gen.CommitWrite{
			{Path: "b.md", Content: []byte("1")}, {Path: "b.md", Content: []byte("2")},
		}}, "validation_failed"},
		{"a path deleted twice", gen.CreateCommitRequest{Message: "m", Deletes: []gen.CommitDelete{
			{Path: "a.md", BaseSha: a}, {Path: "a.md"},
		}}, "validation_failed"},
		{"a path written and deleted", gen.CreateCommitRequest{Message: "m",
			Writes:  []gen.CommitWrite{{Path: "a.md", Content: []byte("2"), BaseSha: a}},
			Deletes: []gen.CommitDelete{{Path: "a.md", BaseSha: a}},
		}, "validation_failed"},
		{"a traversal path", gen.CreateCommitRequest{Message: "m", Writes: []gen.CommitWrite{{Path: "../x", Content: []byte("x")}}}, "path_invalid"},
		{"a .git path", gen.CreateCommitRequest{Message: "m", Writes: []gen.CommitWrite{{Path: "x/.git/config", Content: []byte("x")}}}, "path_invalid"},
		{"an absolute delete", gen.CreateCommitRequest{Message: "m", Deletes: []gen.CommitDelete{{Path: "/a.md"}}}, "path_invalid"},
		{"a short baseSha", gen.CreateCommitRequest{Message: "m", Writes: []gen.CommitWrite{{Path: "a.md", Content: []byte("x"), BaseSha: a[:7]}}}, "validation_failed"},
		{"a bad committer", gen.CreateCommitRequest{Message: "m", Writes: []gen.CommitWrite{{Path: "b.md", Content: []byte("x")}},
			Committer: gen.GitIdentity{Name: "x\nGIT_DIR=/", Email: "x@y"}}, "validation_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := h.CreateCommit(context.Background(), commitReq(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			wantProblem(t, resp.VisitCreateCommitResponse, http.StatusBadRequest, tc.code)
		})
	}
	if got := origin.HeadSHA(t); got != head {
		t.Fatalf("a refused request moved the branch: %s → %s", head, got)
	}
}

func TestCreateCommit_FilesAndIdentities(t *testing.T) {
	origin := repotest.NewOrigin(t, map[string]string{"a.md": "a", "gone.md": "g"})
	h := repo.NewHandler(NewEngine(t, nil), fixedIdentity{name: "Gitpat User", email: "gitpat@users.noreply.github.com"},
		func(string, string) string { return origin.URL() })
	ctx := context.Background()

	resp, err := h.CreateCommit(ctx, commitReq(gen.CreateCommitRequest{
		Message: "edit",
		Writes: []gen.CommitWrite{
			{Path: "a.md", Content: []byte("a2"), BaseSha: origin.BlobSHA(t, "a.md")},
			{Path: "specs/new.md", Content: []byte("new")},
		},
		Deletes: []gen.CommitDelete{{Path: "gone.md"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	res := resp.(gen.CreateCommit200JSONResponse)
	if res.CommitSha != origin.HeadSHA(t) || !res.Changed {
		t.Fatalf("result %+v, origin head %s", res, origin.HeadSHA(t))
	}
	want := []gen.CommitFile{{Path: "a.md", Sha: origin.BlobSHA(t, "a.md")}, {Path: "specs/new.md", Sha: origin.BlobSHA(t, "specs/new.md")}}
	if fmt.Sprint(res.Files) != fmt.Sprint(want) || res.Warnings == nil || len(res.Warnings) != 0 {
		t.Fatalf("files %v warnings %#v, want %v and an empty list", res.Files, res.Warnings, want)
	}
	if got := origin.Git(t, "log", "-1", "--format=%an <%ae>|%cn <%ce>|%s"); got != "Gitpat User <gitpat@users.noreply.github.com>|Gitpat User <gitpat@users.noreply.github.com>|edit" {
		t.Fatalf("no author: commit is %q, want the gitpat user as both", got)
	}

	resp, err = h.CreateCommit(ctx, commitReq(gen.CreateCommitRequest{
		Message: "by a user",
		Writes:  []gen.CommitWrite{{Path: "a.md", Content: []byte("a3"), BaseSha: origin.BlobSHA(t, "a.md")}},
		Author:  gen.GitIdentity{Name: "Ada", Email: "ada@example.com"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := resp.(gen.CreateCommit200JSONResponse); !ok {
		t.Fatalf("got %#v", resp)
	}
	if got := origin.Git(t, "log", "-1", "--format=%an <%ae>|%cn <%ce>"); got != "Ada <ada@example.com>|Ada <ada@example.com>" {
		t.Fatalf("author only: commit is %q, want the author as committer too", got)
	}

	resp, err = h.CreateCommit(ctx, commitReq(gen.CreateCommitRequest{
		Message:   "with a committer",
		Writes:    []gen.CommitWrite{{Path: "a.md", Content: []byte("a4"), BaseSha: origin.BlobSHA(t, "a.md")}},
		Author:    gen.GitIdentity{Name: "Ada", Email: "ada@example.com"},
		Committer: gen.GitIdentity{Name: "AEP Bot", Email: "bot@example.com"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := resp.(gen.CreateCommit200JSONResponse); !ok {
		t.Fatalf("got %#v", resp)
	}
	if got := origin.Git(t, "log", "-1", "--format=%an <%ae>|%cn <%ce>"); got != "Ada <ada@example.com>|AEP Bot <bot@example.com>" {
		t.Fatalf("author and committer: commit is %q", got)
	}

	same, err := h.CreateCommit(ctx, commitReq(gen.CreateCommitRequest{
		Message: "no-op",
		Writes:  []gen.CommitWrite{{Path: "a.md", Content: []byte("a4"), BaseSha: origin.BlobSHA(t, "a.md")}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if r := same.(gen.CreateCommit200JSONResponse); r.Changed || r.CommitSha != origin.HeadSHA(t) {
		t.Fatalf("identical content: %+v, want unchanged at the tip", r)
	}
}

// fixedIdentity is the gitpat user's commit identity.
type fixedIdentity struct{ name, email string }

func (f fixedIdentity) Identity(context.Context) (string, string, error) { return f.name, f.email, nil }

func TestGetHead_LocalReadsTheMirrorWithoutFetching(t *testing.T) {
	h, origin := newHandlerWithOrigin(t, map[string]string{"a.md": "a"})
	ctx := context.Background()
	first := origin.HeadSHA(t)
	head := func(params gen.GetHeadParams) string {
		t.Helper()
		resp, err := h.GetHead(ctx, gen.GetHeadRequestObject{Owner: "acme", Repo: "greeter", Params: params})
		if err != nil {
			t.Fatal(err)
		}
		r, ok := resp.(gen.GetHead200JSONResponse)
		if !ok {
			t.Fatalf("got %#v", resp)
		}
		return r.Sha
	}
	if got := head(gen.GetHeadParams{}); got != first {
		t.Fatalf("head = %s, want %s", got, first)
	}
	second := origin.Commit(t, map[string]string{"a.md": "a2"}, "out of band")
	if got := head(gen.GetHeadParams{Local: true}); got != first {
		t.Fatalf("local head = %s, want the mirror's %s (no fetch)", got, first)
	}
	if got := head(gen.GetHeadParams{}); got != second {
		t.Fatalf("head = %s, want the fetched %s", got, second)
	}
	origin.Tag(t, "v1", "first release")
	if got := head(gen.GetHeadParams{At: "tags/v1"}); got != second {
		t.Fatalf("tags/v1 = %s, want %s", got, second)
	}

	resp, err := h.GetHead(ctx, gen.GetHeadRequestObject{Owner: "acme", Repo: "greeter", Params: gen.GetHeadParams{Local: true, At: "tags/v1"}})
	if err != nil {
		t.Fatal(err)
	}
	wantProblem(t, resp.VisitGetHeadResponse, http.StatusBadRequest, "validation_failed")
	resp, err = h.GetHead(ctx, gen.GetHeadRequestObject{Owner: "acme", Repo: "greeter", Params: gen.GetHeadParams{At: "tags/v9"}})
	if err != nil {
		t.Fatal(err)
	}
	wantProblem(t, resp.VisitGetHeadResponse, http.StatusNotFound, "ref_not_found")
}

func TestGetHead_DefaultBranchIsTheRequests(t *testing.T) {
	h, origin := newHandlerWithOrigin(t, map[string]string{"a.md": "a"})
	main := origin.HeadSHA(t)
	origin.Git(t, "update-ref", "refs/heads/trunk", main)
	trunk := strings.TrimSpace(origin.Git(t, "commit-tree", origin.Git(t, "rev-parse", main+"^{tree}"), "-p", main, "-m", "trunk only"))
	origin.Git(t, "update-ref", "refs/heads/trunk", trunk)

	for _, local := range []bool{false, true} {
		resp, err := h.GetHead(context.Background(), gen.GetHeadRequestObject{Owner: "acme", Repo: "greeter",
			Params: gen.GetHeadParams{DefaultBranch: "trunk", Local: local}})
		if err != nil {
			t.Fatal(err)
		}
		if got := resp.(gen.GetHead200JSONResponse).Sha; got != trunk {
			t.Fatalf("local=%v: head = %s, want trunk's %s", local, got, trunk)
		}
	}
	resp, err := h.GetHead(context.Background(), gen.GetHeadRequestObject{Owner: "acme", Repo: "greeter",
		Params: gen.GetHeadParams{DefaultBranch: "-trunk"}})
	if err != nil {
		t.Fatal(err)
	}
	wantProblem(t, resp.VisitGetHeadResponse, http.StatusBadRequest, "validation_failed")
}

func TestListTree_PrefixAndLocal(t *testing.T) {
	h, origin := newHandlerWithOrigin(t, map[string]string{"README.md": "r", "specs/a.md": "a", "specs/d/b.md": "bb"})
	ctx := context.Background()
	list := func(params gen.ListTreeParams) gen.ListTree200JSONResponse {
		t.Helper()
		resp, err := h.ListTree(ctx, gen.ListTreeRequestObject{Owner: "acme", Repo: "greeter", Params: params})
		if err != nil {
			t.Fatal(err)
		}
		r, ok := resp.(gen.ListTree200JSONResponse)
		if !ok {
			t.Fatalf("got %#v", resp)
		}
		return r
	}
	first := origin.HeadSHA(t)
	got := list(gen.ListTreeParams{Prefix: "specs/"})
	want := []gen.TreeEntry{{Path: "specs/a.md", Sha: origin.BlobSHA(t, "specs/a.md"), Size: 1}, {Path: "specs/d/b.md", Sha: origin.BlobSHA(t, "specs/d/b.md"), Size: 2}}
	if got.CommitSha != first || fmt.Sprint(got.Entries) != fmt.Sprint(want) {
		t.Fatalf("got %+v, want %s %v", got, first, want)
	}
	origin.Commit(t, map[string]string{"specs/c.md": "c"}, "out of band")
	if local := list(gen.ListTreeParams{Local: true}); local.CommitSha != first || len(local.Entries) != 3 {
		t.Fatalf("local tree = %+v, want the mirror's %s", local, first)
	}
	if fetched := list(gen.ListTreeParams{}); fetched.CommitSha == first || len(fetched.Entries) != 4 {
		t.Fatalf("tree = %+v, want the fetched tip", fetched)
	}
}

func TestReadFile_AnyPathNoAllowList(t *testing.T) {
	h, origin := newHandlerWithOrigin(t, map[string]string{"src/main.go": "package main", "specs/a.md": "a"})
	ctx := context.Background()
	read := func(path, at string) gen.ReadFileResponseObject {
		t.Helper()
		resp, err := h.ReadFile(ctx, gen.ReadFileRequestObject{Owner: "acme", Repo: "greeter", Path: path, Params: gen.ReadFileParams{At: at}})
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	got, ok := read("src/main.go", "").(gen.ReadFile200JSONResponse)
	if !ok || string(got.Content) != "package main" || got.Sha != origin.BlobSHA(t, "src/main.go") || got.CommitSha != origin.HeadSHA(t) || got.Path != "src/main.go" {
		t.Fatalf("got %+v", got)
	}
	pinned := origin.HeadSHA(t)
	origin.Commit(t, map[string]string{"src/main.go": "package main // v2"}, "v2")
	if old := read("src/main.go", pinned).(gen.ReadFile200JSONResponse); string(old.Content) != "package main" || old.CommitSha != pinned {
		t.Fatalf("at a sha: %+v", old)
	}
	wantProblem(t, read("src/none.go", "").VisitReadFileResponse, http.StatusNotFound, "path_not_found")
	wantProblem(t, read("src", "").VisitReadFileResponse, http.StatusNotFound, "path_not_found")
	wantProblem(t, read("../etc/passwd", "").VisitReadFileResponse, http.StatusBadRequest, "path_invalid")
	wantProblem(t, read("a.md", strings.Repeat("0", 40)).VisitReadFileResponse, http.StatusNotFound, "ref_not_found")
}

func TestReadBundle_LocalAndAt(t *testing.T) {
	h, origin := newHandlerWithOrigin(t, map[string]string{"specs/a.md": "a"})
	first := origin.HeadSHA(t)
	origin.Tag(t, "v1", "release")
	origin.Commit(t, map[string]string{"specs/a.md": "a2"}, "out of band")
	ctx := context.Background()
	bundle := func(params gen.ReadBundleParams) gen.ReadBundle200JSONResponse {
		t.Helper()
		resp, err := h.ReadBundle(ctx, gen.ReadBundleRequestObject{Owner: "acme", Repo: "greeter", Params: params})
		if err != nil {
			t.Fatal(err)
		}
		return resp.(gen.ReadBundle200JSONResponse)
	}
	if got := bundle(gen.ReadBundleParams{At: "tags/v1"}); got.CommitSha != first || string(got.Files["specs/a.md"]) != "a" {
		t.Fatalf("at tags/v1: %+v", got)
	}
	// The first read cloned the mirror after the out-of-band commit; local
	// serves that clone's tip.
	if got := bundle(gen.ReadBundleParams{Local: true}); string(got.Files["specs/a.md"]) != "a2" {
		t.Fatalf("local: %+v", got)
	}
	if got := bundle(gen.ReadBundleParams{Path: []string{"specs/a.md", "specs/missing.md"}}); len(got.Files) != 1 {
		t.Fatalf("a missing exact path is left out: %+v", got)
	}
}

func TestTags_CreateListAndRefusals(t *testing.T) {
	h, origin := newHandlerWithOrigin(t, map[string]string{"a.md": "a"})
	ctx := context.Background()
	create := func(body gen.CreateTagRequest) gen.CreateTagResponseObject {
		t.Helper()
		resp, err := h.CreateTag(ctx, gen.CreateTagRequestObject{Owner: "acme", Repo: "greeter", Body: &body})
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	status, _ := answer(t, create(gen.CreateTagRequest{Name: "v1.0.0", Message: "first", Tagger: gen.GitIdentity{Name: "Ada", Email: "ada@example.com"}}).VisitCreateTagResponse)
	if status != http.StatusCreated {
		t.Fatalf("create = %d", status)
	}
	if got := origin.Git(t, "for-each-ref", "--format=%(taggername) %(taggeremail)|%(contents:subject)", "refs/tags/v1.0.0"); got != "Ada <ada@example.com>|first" {
		t.Fatalf("origin tag = %q", got)
	}
	wantProblem(t, create(gen.CreateTagRequest{Name: "v1.0.0", Message: "again"}).VisitCreateTagResponse, http.StatusConflict, "tag_exists")
	wantProblem(t, create(gen.CreateTagRequest{Name: "v2", Message: "m", Target: "tags/none"}).VisitCreateTagResponse, http.StatusNotFound, "ref_not_found")
	for _, bad := range []string{"-v1", "a..b", "a/", "/a", "a.lock", "a//b", "a@{1", ".a", "a/.b", "@", "a.", "a b"} {
		wantProblem(t, create(gen.CreateTagRequest{Name: bad, Message: "m"}).VisitCreateTagResponse, http.StatusBadRequest, "validation_failed")
	}

	origin.Tag(t, "v2.0.0", "made elsewhere")
	list := func(params gen.ListTagsParams) []gen.Tag {
		t.Helper()
		resp, err := h.ListTags(ctx, gen.ListTagsRequestObject{Owner: "acme", Repo: "greeter", Params: params})
		if err != nil {
			t.Fatal(err)
		}
		return resp.(gen.ListTags200JSONResponse).Tags
	}
	local := list(gen.ListTagsParams{Local: true, Prefix: "v"})
	if len(local) != 1 || local[0].Name != "v1.0.0" || local[0].CommitHash != origin.HeadSHA(t) || local[0].Message != "first" || local[0].CreatedAt == nil {
		t.Fatalf("local tags = %+v, want only the studio's v1.0.0", local)
	}
	if fetched := list(gen.ListTagsParams{Prefix: "v2"}); len(fetched) != 1 || fetched[0].Name != "v2.0.0" {
		t.Fatalf("fetched tags = %+v", fetched)
	}
	if none := list(gen.ListTagsParams{Prefix: "x"}); none == nil || len(none) != 0 {
		t.Fatalf("no match = %#v, want an empty list", none)
	}
	// The prefix is a literal start of a tag name: a glob character would
	// widen the for-each-ref pattern, so it is refused.
	for _, bad := range []string{"*", "v?", "v[12]", `v\1`, "v 1"} {
		for _, local := range []bool{true, false} {
			resp, err := h.ListTags(ctx, gen.ListTagsRequestObject{Owner: "acme", Repo: "greeter", Params: gen.ListTagsParams{Prefix: bad, Local: local}})
			if err != nil {
				t.Fatal(err)
			}
			wantProblem(t, resp.VisitListTagsResponse, http.StatusBadRequest, "validation_failed")
		}
	}
}

// failingWorkspace answers every engine call with err.
type failingWorkspace struct {
	repo.Workspace
	err error
}

func (f failingWorkspace) Head(context.Context, repo.RepoRef, string) (string, error) {
	return "", f.err
}

func (f failingWorkspace) Commit(context.Context, repo.RepoRef, []repo.CommitWrite, []repo.CommitDelete, string, *repo.GitIdentity, *repo.GitIdentity) (repo.CommitResult, []repo.Conflict, error) {
	return repo.CommitResult{}, nil, f.err
}

func (f failingWorkspace) Tag(context.Context, repo.RepoRef, repo.TagSpec) error { return f.err }

// gitExit is a failed git command as the engine reports it: the exit error
// wrapped with git's stderr.
func gitExit(stderr string) error {
	err := exec.Command("false").Run()
	return fmt.Errorf("repo: push origin: git push: %w: %s", err, stderr)
}

func TestHandler_ErrorMap(t *testing.T) {
	cases := []struct {
		name         string
		err          error
		status       int
		code         string
		githubStatus float64
	}{
		{"ref", fmt.Errorf("x: %w", repo.ErrRefNotFound), 404, "ref_not_found", 0},
		{"path", fmt.Errorf("x: %w", repo.ErrPathNotFound), 404, "path_not_found", 0},
		{"tag taken", fmt.Errorf("x: %w", repo.ErrTagAlreadyExists), 409, "tag_exists", 0},
		{"not fast-forward", repo.ErrRefNotFastForward, 409, "not_fast_forward", 0},
		{"disk full", &repo.DiskFullError{Root: "/w", UsedPct: 99}, 503, "disk_full", 0},
		{"push 401", gitExit("remote: Invalid username or token.\nfatal: Authentication failed for 'https://github.com/acme/greeter.git/'"), 502, "github_error", 401},
		{"push 403", gitExit("remote: Permission to acme/greeter.git denied to bot.\nfatal: unable to access 'https://github.com/acme/greeter.git/': The requested URL returned error: 403"), 502, "github_error", 403},
		{"push 404", gitExit("remote: Repository not found.\nfatal: repository 'https://github.com/acme/greeter.git/' not found"), 502, "github_error", 404},
		{"other git failure", gitExit("fatal: unable to access 'https://github.com/acme/greeter.git/': Could not resolve host: github.com"), 502, "github_error", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := repo.NewHandler(failingWorkspace{err: tc.err}, nil, repo.GitHubCloneURL)
			ctx := context.Background()
			head, err := h.GetHead(ctx, gen.GetHeadRequestObject{Owner: "acme", Repo: "greeter"})
			if err != nil {
				t.Fatal(err)
			}
			commit, err := h.CreateCommit(ctx, commitReq(gen.CreateCommitRequest{Message: "m", Writes: []gen.CommitWrite{{Path: "a.md", Content: []byte("a")}}}))
			if err != nil {
				t.Fatal(err)
			}
			tag, err := h.CreateTag(ctx, gen.CreateTagRequestObject{Owner: "acme", Repo: "greeter", Body: &gen.CreateTagRequest{Name: "v1", Message: "m"}})
			if err != nil {
				t.Fatal(err)
			}
			for _, visit := range []func(http.ResponseWriter) error{head.VisitGetHeadResponse, commit.VisitCreateCommitResponse, tag.VisitCreateTagResponse} {
				body := wantProblem(t, visit, tc.status, tc.code)
				gs, _ := body["githubStatus"].(float64)
				if gs != tc.githubStatus {
					t.Fatalf("githubStatus = %v, want %v", body["githubStatus"], tc.githubStatus)
				}
				if d, _ := body["detail"].(string); strings.Contains(d, "github.com") {
					t.Fatalf("detail leaks git's text: %q", d)
				}
			}
		})
	}

	t.Run("a cancelled call is the caller's, not a GitHub failure", func(t *testing.T) {
		h := repo.NewHandler(failingWorkspace{err: gitExit("fatal: the remote end hung up unexpectedly")}, nil, repo.GitHubCloneURL)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := h.GetHead(ctx, gen.GetHeadRequestObject{Owner: "acme", Repo: "greeter"}); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	})
}

func TestHandler_AddressesGitHubByOwnerRepo(t *testing.T) {
	var got repo.RepoRef
	h := repo.NewHandler(recordingWorkspace{ref: &got}, nil, repo.GitHubCloneURL)
	if _, err := h.GetHead(context.Background(), gen.GetHeadRequestObject{Owner: "Acme", Repo: "Greeter", Params: gen.GetHeadParams{DefaultBranch: "trunk"}}); err != nil {
		t.Fatal(err)
	}
	want := repo.RepoRef{Owner: "Acme", Repo: "Greeter", CloneURL: "https://github.com/Acme/Greeter.git", DefaultBranch: "trunk"}
	if got != want {
		t.Fatalf("ref = %+v, want %+v", got, want)
	}
}

// recordingWorkspace records the ref Head is called with.
type recordingWorkspace struct {
	repo.Workspace
	ref *repo.RepoRef
}

func (r recordingWorkspace) Head(_ context.Context, ref repo.RepoRef, _ string) (string, error) {
	*r.ref = ref
	return strings.Repeat("a", 40), nil
}
