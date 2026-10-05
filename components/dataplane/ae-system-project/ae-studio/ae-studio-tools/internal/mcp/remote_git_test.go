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

package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// The remote-git client, copied from aep-api's mcpdiscovery: the owner is
// AE_GITHUB_OWNER and the token the env gitpat, instead of the per-org
// credential resolver.

// githubStub serves handler and counts requests.
func githubStub(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func newRemote(base string) RemoteGit {
	return RemoteGit{Owner: "acme", Token: "t0k3n", APIBase: base}
}

func TestRemoteGit_GetFileContents_File(t *testing.T) {
	const raw = "openapi: 3.0.0\ninfo:\n  title: invoice\n"
	var gotAuth, gotAccept, gotPath, gotRef string
	srv, _ := githubStub(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotAccept = r.Header.Get("Authorization"), r.Header.Get("Accept")
		gotPath, gotRef = r.URL.Path, r.URL.Query().Get("ref")
		_, _ = fmt.Fprintf(w, `{"type":"file","sha":"abc123","content":%q,"encoding":"base64"}`,
			base64.StdEncoding.EncodeToString([]byte(raw)))
	})
	got, err := newRemote(srv.URL).GetFileContents(context.Background(), "acme", "billing-svc", "specs/openapi.yaml", "main")
	if err != nil {
		t.Fatal(err)
	}
	if got.IsDirectory || got.Content != raw || got.SHA != "abc123" {
		t.Fatalf("file = %+v", got)
	}
	if gotAuth != "Bearer t0k3n" || !strings.Contains(gotAccept, "vnd.github") {
		t.Fatalf("headers: auth set = %v, accept = %q", gotAuth == "Bearer t0k3n", gotAccept)
	}
	if gotPath != "/repos/acme/billing-svc/contents/specs/openapi.yaml" || gotRef != "main" {
		t.Fatalf("path = %q ref = %q", gotPath, gotRef)
	}
}

func TestRemoteGit_GetFileContents_Directory(t *testing.T) {
	srv, _ := githubStub(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `[{"type":"file","path":"specs/openapi.yaml","sha":"aaa"},{"type":"dir","path":"specs/schemas","sha":"bbb"}]`)
	})
	got, err := newRemote(srv.URL).GetFileContents(context.Background(), "acme", "billing-svc", "specs", "")
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsDirectory || got.Content != "" || len(got.Entries) != 2 || got.Entries[1].Type != "dir" {
		t.Fatalf("dir = %+v", got)
	}
}

func TestRemoteGit_SearchCode(t *testing.T) {
	var gotQuery string
	srv, _ := githubStub(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		_, _ = fmt.Fprint(w, `{"items":[{"path":"specs/openapi.yaml","sha":"aaa"},{"path":"api/openapi.yaml","sha":"bbb"}]}`)
	})
	hits, err := newRemote(srv.URL).SearchCode(context.Background(), "acme", "billing-svc", "openapi filename:openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].Path != "specs/openapi.yaml" {
		t.Fatalf("hits = %+v", hits)
	}
	if !strings.HasSuffix(gotQuery, " repo:acme/billing-svc") || !strings.HasPrefix(gotQuery, "openapi") {
		t.Fatalf("q = %q", gotQuery)
	}
}

// The owner guard: GitHub logins are case-insensitive; any other owner, an
// empty owner argument and an unset AE_GITHUB_OWNER are refused before any
// GitHub request.
func TestRemoteGit_OwnerGuard(t *testing.T) {
	srv, n := githubStub(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"type":"file","sha":"s","content":"","encoding":"base64"}`)
	})
	ctx := context.Background()
	if _, err := (RemoteGit{Owner: "Acme-GH", Token: "t", APIBase: srv.URL}).GetFileContents(ctx, "acme-gh", "r", "a", ""); err != nil {
		t.Fatalf("case-insensitive owner refused: %v", err)
	}
	if n.Load() != 1 {
		t.Fatalf("GitHub calls = %d", n.Load())
	}
	for name, c := range map[string]struct {
		configured, asked string
	}{
		"other owner":    {"acme", "evilcorp"},
		"empty argument": {"acme", ""},
		"unset owner":    {"", "acme"},
		"both empty":     {"", ""},
	} {
		g := RemoteGit{Owner: c.configured, Token: "t", APIBase: srv.URL}
		if _, err := g.GetFileContents(ctx, c.asked, "r", "a", ""); !errors.Is(err, ErrOwnerNotInOrg) {
			t.Fatalf("%s: read err = %v", name, err)
		}
		if _, err := g.SearchCode(ctx, c.asked, "r", "openapi"); !errors.Is(err, ErrOwnerNotInOrg) {
			t.Fatalf("%s: search err = %v", name, err)
		}
	}
	if n.Load() != 1 {
		t.Fatalf("GitHub called for a refused owner: %d calls", n.Load())
	}
}

func TestRemoteGit_SearchCode_EmbeddedScopeQualifierRefused(t *testing.T) {
	srv, n := githubStub(t, func(http.ResponseWriter, *http.Request) {})
	for _, q := range []string{"secret repo:acme/other-private", "secret org:evilcorp", "x USER:someone", "fork:true x"} {
		if _, err := newRemote(srv.URL).SearchCode(context.Background(), "acme", "billing-svc", q); !errors.Is(err, ErrQueryScopeQualifier) {
			t.Fatalf("%q: err = %v", q, err)
		}
	}
	if n.Load() != 0 {
		t.Fatalf("GitHub search called %d times", n.Load())
	}
}

// The coordinates cannot climb out of the owner's repo: a resolver on the way
// to GitHub may collapse "." / ".." segments, and url.PathEscape keeps dots, so
// an unchecked repo or path could re-point the read at /repos/<another
// owner>/… past the owner guard, and a repo carrying a space could smuggle a
// search qualifier. All refused before any GitHub request.
func TestRemoteGit_CoordinatesStayInTheOwnersRepo(t *testing.T) {
	srv, n := githubStub(t, func(http.ResponseWriter, *http.Request) {})
	ctx := context.Background()
	g := newRemote(srv.URL)
	for _, path := range []string{"../../../victim/private/contents/secrets.yaml", "specs/../../x", "./a", "a/.", ".."} {
		if _, err := g.GetFileContents(ctx, "acme", "svc", path, ""); !errors.Is(err, ErrPathDotSegment) {
			t.Errorf("path %q: err = %v", path, err)
		}
	}
	for _, repo := range []string{"..", ".", "svc repo:victim/x", "a b", "svc/../x", "x:y"} {
		if _, err := g.GetFileContents(ctx, "acme", repo, "a", ""); !errors.Is(err, ErrInvalidRepoName) {
			t.Errorf("read repo %q: err = %v", repo, err)
		}
		if _, err := g.SearchCode(ctx, "acme", repo, "openapi"); !errors.Is(err, ErrInvalidRepoName) {
			t.Errorf("search repo %q: err = %v", repo, err)
		}
	}
	if n.Load() != 0 {
		t.Fatalf("GitHub called %d times for refused coordinates", n.Load())
	}
}

func TestRemoteGit_OrdinaryDottedNamesStillRead(t *testing.T) {
	var gotPath string
	srv, _ := githubStub(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = fmt.Fprint(w, `[]`)
	})
	if _, err := newRemote(srv.URL).GetFileContents(context.Background(), "acme", "my.svc_v-2", "specs/.well-known/a..b.yaml", ""); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/repos/acme/my.svc_v-2/contents/specs/.well-known/a..b.yaml" {
		t.Fatalf("path = %q", gotPath)
	}
}

func TestRemoteGit_GetFileContents_Limits(t *testing.T) {
	big := strings.Repeat("a", 4096)
	srv, _ := githubStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/r/contents/big":
			_, _ = fmt.Fprintf(w, `{"type":"file","sha":"x","content":%q,"encoding":"base64"}`, base64.StdEncoding.EncodeToString([]byte(big)))
		case "/repos/acme/r/contents/huge":
			_, _ = fmt.Fprint(w, `{"type":"file","sha":"x","content":"","encoding":"none"}`)
		case "/repos/acme/r/contents/empty":
			_, _ = fmt.Fprint(w, `{"type":"file","sha":"x","content":"","encoding":"base64"}`)
		default:
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		}
	})
	g := newRemote(srv.URL)
	g.maxContentBytes = 1024
	ctx := context.Background()
	if _, err := g.GetFileContents(ctx, "acme", "r", "big", ""); err == nil {
		t.Fatal("content over the cap was returned")
	}
	if _, err := g.GetFileContents(ctx, "acme", "r", "huge", ""); !errors.Is(err, ErrFileTooLargeToInline) {
		t.Fatalf("encoding none: err = %v", err)
	}
	if f, err := g.GetFileContents(ctx, "acme", "r", "empty", ""); err != nil || f.Content != "" {
		t.Fatalf("empty file: %+v %v", f, err)
	}
	if _, err := g.GetFileContents(ctx, "acme", "r", "missing", ""); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("missing file: err = %v", err)
	}
}

// What rides the prompt: binary content is withheld with a note, oversized
// text is cut on a rune boundary with a note.
func TestFileView_GuardsThePrompt(t *testing.T) {
	bin := fileView(&RemoteGitFile{Content: "PDF\x00\x01", SHA: "s"})
	if bin.Content != "" || !strings.Contains(bin.Note, "binary") || bin.SHA != "s" {
		t.Fatalf("binary = %+v", bin)
	}
	long := strings.Repeat("é", maxToolFileBytes) // 2 bytes each
	cut := fileView(&RemoteGitFile{Content: long})
	if len(cut.Content) > maxToolFileBytes || !strings.Contains(cut.Note, "truncated") || !strings.HasSuffix(cut.Content, "é") {
		t.Fatalf("truncated to %d bytes, note %q", len(cut.Content), cut.Note)
	}
}

// callTool answers every failure as a tool error the model can read, and a
// success as one text block of JSON.
func TestRemoteGit_CallTool(t *testing.T) {
	srv, n := githubStub(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"type":"file","sha":"s","content":%q,"encoding":"base64"}`, base64.StdEncoding.EncodeToString([]byte("use gin")))
	})
	g := newRemote(srv.URL)
	ctx := context.Background()
	res := decodeToolResult(t, g.callTool(ctx, toolGetFileContents, remoteGitArgs{Owner: "acme", Repo: "r", Path: "a"}))
	if res.IsError || !strings.Contains(res.Content[0].Text, `"content":"use gin"`) {
		t.Fatalf("read = %+v", res)
	}
	for name, c := range map[string]struct {
		tool string
		args remoteGitArgs
		want string
	}{
		"read without repo":    {toolGetFileContents, remoteGitArgs{Owner: "acme"}, "missing required arguments"},
		"search without query": {toolSearchCode, remoteGitArgs{Owner: "acme", Repo: "r"}, "missing required arguments"},
		"other owner":          {toolSearchCode, remoteGitArgs{Owner: "evil", Repo: "r", Query: "q"}, "not in org"},
	} {
		res := decodeToolResult(t, g.callTool(ctx, c.tool, c.args))
		if !res.IsError || !strings.Contains(res.Content[0].Text, c.want) {
			t.Fatalf("%s: %+v", name, res)
		}
	}
	if n.Load() != 1 {
		t.Fatalf("GitHub calls = %d", n.Load())
	}
}

type decodedToolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

func decodeToolResult(t *testing.T, raw json.RawMessage) decodedToolResult {
	t.Helper()
	var r decodedToolResult
	if err := json.Unmarshal(raw, &r); err != nil || len(r.Content) != 1 || r.Content[0].Type != "text" {
		t.Fatalf("tool result %s: %v", raw, err)
	}
	return r
}
