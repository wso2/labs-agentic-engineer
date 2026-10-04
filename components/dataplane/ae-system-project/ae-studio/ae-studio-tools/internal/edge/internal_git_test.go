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

package edge

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
	"github.com/wso2/aep/ae-studio-tools/internal/repo/repotest"
)

// The /internal/v1 git content ops through the real routes: cap → gate →
// owner guard → validator → repo.Handler. The handler's own behaviour is
// internal/repo's handler_test; these pin the wiring.

const gitRepoPath = "/internal/v1/repos/acme-gh/greeter"

// gitHarness serves the routes over a file:// origin seeded with files.
func gitHarness(t *testing.T, files map[string]string) (*harness, *repotest.Origin) {
	t.Helper()
	origin := repotest.NewOrigin(t, files)
	return newHarness(t, withGitOrigin(origin)), origin
}

// jsonBody decodes rec's body.
func jsonBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	return m
}

// doJSON sends a JSON body to the routes as aep-api would.
func (h *harness) doJSON(method, path, body string) *httptest.ResponseRecorder {
	h.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+h.m2m())
	r.Header.Set("X-Impersonate-Org", "ou-1")
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, r)
	return rec
}

// repoOps is one valid request per repository-scoped op, under base.
func repoOps(h *harness, base string) map[string]func() *httptest.ResponseRecorder {
	get := func(p string) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder { return h.do("GET", base+p, h.m2m(), "ou-1", nil) }
	}
	post := func(p, body string) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder { return h.doJSON("POST", base+p, body) }
	}
	return map[string]func() *httptest.ResponseRecorder{
		"get-head":        get("/head"),
		"list-tree":       get("/tree?prefix=specs/"),
		"read-file":       get("/files/specs/a.md"),
		"read-file (one)": get("/files/README.md"),
		"read-bundle":     get("/bundle?ext=.md"),
		"list-tags":       get("/tags"),
		"create-tag":      post("/tags", `{"name":"v1","message":"m"}`),
		"create-commit":   post("/commits", `{"message":"m","writes":[{"path":"specs/b.md","content":"Yg==","baseSha":""}]}`),
		"start-repo-turn": post("/turns", turnRequest("11111111-1111-5111-8111-111111111111")),
		"put-repo-references": func() *httptest.ResponseRecorder {
			return h.putReferences(base+"/references", h.m2m(), false, refFile("notes.md", 10))
		},
	}
}

func TestInternalGit_OwnerGuardCoversEveryRepoOp(t *testing.T) {
	origin := repotest.NewOrigin(t, map[string]string{"specs/a.md": "a"})
	h := newHarness(t, withGitOrigin(origin), withProjects(greeterRepo))
	for _, owner := range []string{"someone-else", "acme-gh-2", "Acme-GH-"} {
		for name, op := range repoOps(h, "/internal/v1/repos/"+owner+"/greeter") {
			rec := op()
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"code":"owner_not_allowed"`) {
				t.Fatalf("%s as %s: %d %s", name, owner, rec.Code, rec.Body.String())
			}
		}
	}
	if origin.Git(t, "rev-list", "--count", "main") != "1" || origin.Git(t, "tag") != "" || !h.referencesStoreEmpty() {
		t.Fatal("a refused owner's write landed")
	}
	if strings.Contains(h.logs(), "repo.clone") {
		t.Fatal("a refused owner's request reached the engine")
	}

	// The connected owner, in any case, passes the guard.
	rec := h.do("GET", "/internal/v1/repos/ACME-gh/greeter/head", h.m2m(), "ou-1", nil)
	if rec.Code != http.StatusOK || jsonBody(t, rec.Body.Bytes())["sha"] != origin.HeadSHA(t) {
		t.Fatalf("connected owner: %d %s", rec.Code, rec.Body.String())
	}

	t.Run("no connected owner refuses every repo op", func(t *testing.T) {
		h := newHarness(t, withGitOrigin(origin), withProjects(greeterRepo))
		server := internalServer{Handler: repo.NewHandler(h.engine, nil, func(string, string) string { return origin.URL() })}
		h.handler = capOpBody(internalRouteFinder, internalBodyCaps, internalBodyBytes, internalHandler(internalRouteFinder, server))
		for name, op := range repoOps(h, gitRepoPath) {
			if rec := op(); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"code":"owner_not_allowed"`) {
				t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("the gate runs first", func(t *testing.T) {
		rec := h.do("GET", "/internal/v1/repos/someone-else/greeter/head", "", "", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated = %d, want 401 before the owner guard", rec.Code)
		}
	})
}

func TestInternalGit_ReadFileServesNestedPaths(t *testing.T) {
	h, origin := gitHarness(t, map[string]string{"src/pkg/main.go": "package main", "README.md": "hi"})
	for path, want := range map[string]string{"src/pkg/main.go": "package main", "README.md": "hi"} {
		rec := h.do("GET", gitRepoPath+"/files/"+path, h.m2m(), "ou-1", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
		body := jsonBody(t, rec.Body.Bytes())
		content, _ := base64.StdEncoding.DecodeString(body["content"].(string))
		if string(content) != want || body["path"] != path || body["sha"] != origin.BlobSHA(t, path) || body["commitSha"] != origin.HeadSHA(t) {
			t.Fatalf("%s: %v", path, body)
		}
	}
	wantProblem(t, h.do("GET", gitRepoPath+"/files/src/none.go", h.m2m(), "ou-1", nil), http.StatusNotFound, "path_not_found")
	wantProblem(t, h.do("GET", gitRepoPath+"/files/src/pkg/main.go?at=main", h.m2m(), "ou-1", nil), http.StatusBadRequest, "validation_failed")
	wantProblem(t, h.do("GET", gitRepoPath+"/files/src/%2e%2e/x", h.m2m(), "ou-1", nil), http.StatusBadRequest, "path_invalid")
}

func TestInternalGit_CommitOverHTTP(t *testing.T) {
	h, origin := gitHarness(t, map[string]string{"specs/a.md": "a"})
	base := origin.BlobSHA(t, "specs/a.md")
	body := fmt.Sprintf(`{"message":"edit","writes":[{"path":"specs/a.md","content":%q,"baseSha":%q}]}`,
		base64.StdEncoding.EncodeToString([]byte("a2")), base)
	rec := h.doJSON("POST", gitRepoPath+"/commits", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("commit: %d %s", rec.Code, rec.Body.String())
	}
	assertJSONEq(t, rec.Body.String(), `{"commitSha":"`+origin.HeadSHA(t)+`","changed":true,"files":[{"path":"specs/a.md","sha":"`+origin.BlobSHA(t, "specs/a.md")+`"}],"warnings":[]}`)

	rec = h.doJSON("POST", gitRepoPath+"/commits", body)
	if rec.Code != http.StatusConflict || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("stale: %d %s", rec.Code, rec.Body.String())
	}
	got := jsonBody(t, rec.Body.Bytes())
	want := []any{map[string]any{"path": "specs/a.md", "baseSha": base, "currentSha": origin.BlobSHA(t, "specs/a.md")}}
	if got["code"] != "conflict" || fmt.Sprint(got["conflicts"]) != fmt.Sprint(want) {
		t.Fatalf("stale body %v", got)
	}

	wantProblem(t, h.doJSON("POST", gitRepoPath+"/commits", `{"message":"m","writes":[{"path":"a","content":"YQ==","baseSha":"abc"}]}`),
		http.StatusBadRequest, "validation_failed")
	wantProblem(t, h.doJSON("POST", gitRepoPath+"/commits", `{"message":"m","writes":[{"path":"a","content":"YQ==","baseSha":""}],"scaffold":true}`),
		http.StatusBadRequest, "validation_failed")
}

// The commit cap is 16 MiB (05 §3): past it is 413 before the gate, under
// it a body bigger than the 1 MiB default commits.
func TestInternalGit_CommitBodyCap(t *testing.T) {
	h, origin := gitHarness(t, map[string]string{"specs/a.md": "a"})
	over := bytes.NewReader(make([]byte, commitBodyBytes+1))
	wantProblem(t, h.do("POST", gitRepoPath+"/commits", "", "", over), http.StatusRequestEntityTooLarge, "payload_too_large")

	big := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("x"), 3<<20))
	body := `{"message":"big","writes":[{"path":"specs/big.md","content":"` + big + `","baseSha":""}]}`
	if int64(len(body)) <= internalBodyBytes {
		t.Fatalf("body %d is not over the default cap", len(body))
	}
	rec := h.doJSON("POST", gitRepoPath+"/commits", body)
	if rec.Code != http.StatusOK || origin.BlobSHA(t, "specs/big.md") == "" {
		t.Fatalf("a 4 MiB commit: %d %s", rec.Code, rec.Body.String())
	}
}

func TestInternalGit_GitHubFailureNamesNoURL(t *testing.T) {
	h := newHarness(t) // no origin: every clone fails
	rec := h.do("GET", gitRepoPath+"/head", h.m2m(), "ou-1", nil)
	wantProblem(t, rec, http.StatusBadGateway, "github_error")
	if strings.Contains(rec.Body.String(), "file:") || strings.Contains(h.logs(), "file:") {
		t.Fatalf("a git failure leaked its text: %s / %s", rec.Body.String(), h.logs())
	}
	if line := h.logLine("repo.git_failed"); line["op"] != "get-head" || line["repo"] != "acme-gh/greeter" {
		t.Fatalf("log line %v", line)
	}
}
