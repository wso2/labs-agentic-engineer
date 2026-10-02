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
	"context"
	"crypto/sha1" //nolint:gosec // git object names are SHA-1 by definition
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/files"
	"github.com/wso2/aep/ae-studio-tools/internal/platform"
	"github.com/wso2/aep/ae-studio-tools/internal/projects"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
	"github.com/wso2/aep/ae-studio-tools/internal/repo/repotest"
)

// blobSHA is git's blob object name for content.
func blobSHA(content string) string {
	h := sha1.New() //nolint:gosec // git object names are SHA-1 by definition
	fmt.Fprintf(h, "blob %d\x00", len(content))
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}

const unicodeSpec = "specs/requirements/仕様-résumé ノート.md"

// greeterHarness serves project greeter from a file:// origin seeded with
// files; aep-api names the repo acme/greeter.
func greeterHarness(t *testing.T, files map[string]string) (*harness, *repotest.Origin) {
	t.Helper()
	origin := repotest.NewOrigin(t, files)
	h := newHarness(t, withProjects(map[string]projects.Repository{"greeter": {
		Owner: "acme", Repo: "greeter", DefaultBranch: repotest.Branch, CloneURL: origin.URL(),
	}}))
	return h, origin
}

func TestV1Files_PathRulesAndOrg(t *testing.T) {
	h, _ := greeterHarness(t, map[string]string{
		"specs/requirements/prd.md": "# prd", "src/main.go": "package main", "tests/acceptance/report.json": "{}",
		"workload.yaml": "kind: Workload", unicodeSpec: "ünïcödé",
	})
	u := h.user("default", "ou-1") // the pod's org

	r := h.do("GET", "/v1/projects/greeter/files?prefix=specs/requirements/prd", u, "", nil)
	if r.Code != 200 || r.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("list = %d %q %s", r.Code, r.Header().Get("Content-Type"), r.Body.String())
	}
	assertJSONEq(t, r.Body.String(), `[{"path":"specs/requirements/prd.md","sha":"`+blobSHA("# prd")+`","size":5}]`)

	// A nested path reaches read-file through the catch-all.
	r = h.do("GET", "/v1/projects/greeter/files/specs/requirements/prd.md", u, "", nil)
	if r.Code != 200 {
		t.Fatalf("nested read = %d %s", r.Code, r.Body.String())
	}
	assertJSONEq(t, r.Body.String(), `{"path":"specs/requirements/prd.md","content":"# prd","sha":"`+blobSHA("# prd")+`"}`)
	escaped := (&url.URL{Path: "/" + unicodeSpec}).EscapedPath()
	if r = h.do("GET", "/v1/projects/greeter/files"+escaped, u, "", nil); r.Code != 200 || !strings.Contains(r.Body.String(), "ünïcödé") {
		t.Fatalf("unicode read = %d %s", r.Code, r.Body.String())
	}

	cases := []struct {
		method, path, token string
		want                int
		code                string
	}{
		{"GET", "/v1/projects/greeter/files/tests/acceptance/report.json", u, 200, ""},
		{"GET", "/v1/projects/greeter/files/workload.yaml", u, 200, ""},                                  // one segment: the generated route
		{"GET", "/v1/projects/greeter/files/src/main.go", u, 400, "path_invalid"},                        // outside the read rules
		{"GET", "/v1/projects/greeter/files/README.md", u, 400, "path_invalid"},                          // one segment, refused
		{"GET", "/v1/projects/greeter/files/specs/%2e%2e/src/main.go", u, 400, "path_invalid"},           // traversal reaches the rules
		{"GET", "/v1/projects/greeter/files/specs/../src/main.go", u, 404, "not_found"},                  // unclean path (phase 1)
		{"GET", "/v1/projects/greeter/files/specs//a.md", u, 404, "not_found"},                           // unclean path (phase 1)
		{"GET", "/v1/projects/greeter/files/specs/requirements/prd.md?ref=main", u, 400, "path_invalid"}, // ref must be hex
		{"GET", "/v1/projects/greeter/files/specs/requirements/missing.md", u, 404, "path_not_found"},
		{"GET", "/v1/projects/greeter/files/specs/requirements/prd.md?ref=0123456789abcdef0123456789abcdef01234567", u, 404, "ref_not_found"},
		{"GET", "/v1/projects/nope/files", u, 404, "project_unknown"},
		{"GET", "/v1/projects/nope/files/specs/requirements/prd.md", u, 404, "project_unknown"},
		{"GET", "/v1/projects/greeter/files", h.user("e2e-other", "ou-2"), 403, ""}, // org rule (phase 1 gate)
		{"GET", "/v1/projects/greeter/files/specs/requirements/prd.md", h.user("e2e-other", "ou-2"), 403, ""},
		{"GET", "/v1/projects/greeter/files/specs/requirements/prd.md", h.m2m(), 401, ""},
		{"POST", "/v1/projects/greeter/files/apply", u, 404, "not_found"}, // no writes on /v1
		{"PUT", "/v1/projects/greeter/files/specs/requirements/prd.md", u, 404, "not_found"},
		{"HEAD", "/v1/projects/greeter/files/specs/requirements/prd.md", u, 404, "not_found"},
		{"GET", "/v1/projects/greeter/nope", u, 404, "not_found"},
	}
	for _, c := range cases {
		var body io.Reader
		if c.method == "POST" {
			body = strings.NewReader(`{}`)
		}
		rec := h.do(c.method, c.path, c.token, "", body)
		if rec.Code != c.want {
			t.Errorf("%s %s = %d, want %d (%s)", c.method, c.path, rec.Code, c.want, rec.Body.String())
			continue
		}
		if c.want >= 400 && c.method != "HEAD" && rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s %s content-type %q", c.method, c.path, rec.Header().Get("Content-Type"))
		}
		if c.code != "" && !strings.Contains(rec.Body.String(), `"code":"`+c.code+`"`) {
			t.Errorf("%s %s body = %s, want code %s", c.method, c.path, rec.Body.String(), c.code)
		}
	}
	// Only requests past the gate, the validator and the path rules resolve
	// the project: list, the nested and unicode reads, report.json,
	// workload.yaml, missing.md, the unknown ref and the two "nope" rows. A
	// 401/403, a refused path or ref, an unclean path and a route miss cost no
	// aep-api call.
	if n := h.projects.CallCount(); n != 9 {
		t.Fatalf("resolver calls = %d, want 9", n)
	}
}

func TestV1Files_Bundle(t *testing.T) {
	h, origin := greeterHarness(t, map[string]string{
		"specs/requirements/prd.md": "v1", "src/main.go": "package main",
	})
	u := h.user("default", "ou-1")
	var first struct {
		CommitSha string `json:"commitSha"`
		Files     []struct{ Path, Content, Sha string }
	}
	r := h.do("GET", "/v1/projects/greeter/files/bundle", u, "", nil)
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &first) != nil || len(first.Files) != 1 || first.Files[0].Path != "specs/requirements/prd.md" {
		t.Fatalf("bundle = %d %s", r.Code, r.Body.String())
	}
	origin.Commit(t, map[string]string{"specs/requirements/prd.md": "v2"}, "commit")
	r = h.do("GET", "/v1/projects/greeter/files/bundle?prefix=specs/&ref="+first.CommitSha, u, "", nil)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"content":"v1"`) {
		t.Fatalf("pinned bundle = %d %s", r.Code, r.Body.String())
	}
	if r = h.do("GET", "/v1/projects/greeter/files/bundle?ref=HEAD~1", u, "", nil); r.Code != 400 {
		t.Fatalf("revision-expression ref = %d %s", r.Code, r.Body.String())
	}
}

// Every request resolves its project, so a project aep-api stops knowing is
// refused on the next request, and aep-api down is a retryable 503.
func TestV1Files_ResolvesEveryRequest(t *testing.T) {
	h, _ := greeterHarness(t, map[string]string{"specs/requirements/prd.md": "v1"})
	u := h.user("default", "ou-1")
	const read = "/v1/projects/greeter/files/specs/requirements/prd.md"
	if r := h.do("GET", read, u, "", nil); r.Code != 200 {
		t.Fatalf("read = %d %s", r.Code, r.Body.String())
	}
	h.projects.SetErr(projects.ErrUnknown)
	if r := h.do("GET", read, u, "", nil); r.Code != 404 || !strings.Contains(r.Body.String(), "project_unknown") {
		t.Fatalf("removed project = %d %s", r.Code, r.Body.String())
	}
	h.projects.SetErr(fmt.Errorf("%w: aep-api answered 503", projects.ErrUnavailable))
	r := h.do("GET", read, u, "", nil)
	if r.Code != 503 || r.Header().Get("Retry-After") != "5" || !strings.Contains(r.Body.String(), "aep_api_unavailable") {
		t.Fatalf("aep-api down = %d Retry-After=%q %s", r.Code, r.Header().Get("Retry-After"), r.Body.String())
	}
	if n := h.projects.CallCount(); n != 3 {
		t.Fatalf("resolver calls = %d, want 3", n)
	}
}

// TestV1Files_ErrorMapping pins every error class to its problem and checks
// the log lines carry no error text from the resolver.
func TestV1Files_ErrorMapping(t *testing.T) {
	urlErr := &url.Error{Op: "Get", URL: "http://aep-api.internal/secret-path", Err: errors.New("dial tcp: refused")}
	cases := []struct {
		name       string
		err        error
		status     int
		code       string
		retryAfter string
		logMsg     string
	}{
		{"path", fmt.Errorf("x: %w", files.ErrPathInvalid), 400, "path_invalid", "", ""},
		{"file", files.ErrFileNotFound, 404, "path_not_found", "", ""},
		{"ref", fmt.Errorf("resolve: %w", repo.ErrRefNotFound), 404, "ref_not_found", "", ""},
		{"unknown", fmt.Errorf("%w: %q", projects.ErrUnknown, "p"), 404, "project_unknown", "", ""},
		{"unavailable", fmt.Errorf("%w: %w", projects.ErrUnavailable, urlErr), 503, "aep_api_unavailable", "5", "aep_api.unavailable"},
		{"aep-api 401", fmt.Errorf("%w: %w: aep-api answered 401", projects.ErrUnavailable, projects.ErrMisconfigured), 503, "aep_api_unavailable", "", "aep_api.auth_rejected"},
		{"token endpoint", fmt.Errorf("%w: %w: %w", projects.ErrUnavailable, projects.ErrMisconfigured, fmt.Errorf("%w: %w", platform.ErrClientRejected, urlErr)), 503, "aep_api_unavailable", "", "aep_api.auth_rejected"},
		{"disk", fmt.Errorf("read: %w", &repo.DiskFullError{Root: "/studio-data", UsedPct: 99}), 503, "disk_full", "", "files.disk_full"},
		{"not fast-forward", &files.RepoError{Repo: "acme/greeter", Err: fmt.Errorf("apply: push http://aep-api.internal/secret-path: %w", repo.ErrRefNotFastForward)}, 409, "not_fast_forward", "", "files.not_fast_forward"},
		{"git", &files.RepoError{Repo: "acme/greeter", Err: fmt.Errorf("git fetch http://aep-api.internal/secret-path: %w", &exec.ExitError{})}, 502, "github_error", "", "files.git_failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			logs := captureLogs(t)
			rec := httptest.NewRecorder()
			_ = filesProblem(context.Background(), "read", "greeter", c.err).VisitReadFileResponse(rec)
			if rec.Code != c.status || !strings.Contains(rec.Body.String(), `"code":"`+c.code+`"`) {
				t.Fatalf("got %d %s", rec.Code, rec.Body.String())
			}
			if rec.Header().Get("Content-Type") != "application/problem+json" || rec.Header().Get("Retry-After") != c.retryAfter {
				t.Fatalf("headers = %v", rec.Header())
			}
			if c.logMsg != "" && !strings.Contains(logs.String(), `"msg":"`+c.logMsg+`"`) {
				t.Fatalf("no %s line in %s", c.logMsg, logs.String())
			}
			if strings.Contains(logs.String(), "secret-path") || strings.Contains(rec.Body.String(), "secret-path") {
				t.Fatalf("resolver error text leaked: %s", logs.String())
			}
		})
	}
	logs := captureLogs(t)
	_ = filesProblem(context.Background(), "read", "greeter", &files.RepoError{Repo: "acme/greeter", Err: &exec.ExitError{}})
	if !strings.Contains(logs.String(), `"repo":"acme/greeter"`) || !strings.Contains(logs.String(), `"class":"git_exit"`) {
		t.Fatalf("git_failed line = %s", logs.String())
	}
	logs = captureLogs(t)
	_ = filesProblem(context.Background(), "list", "greeter", fmt.Errorf("%w: %w: %w", projects.ErrUnavailable, projects.ErrMisconfigured, platform.ErrClientRejected))
	if !strings.Contains(logs.String(), `"cause":"token_endpoint"`) || !strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Fatalf("auth_rejected line = %s", logs.String())
	}
}

func assertJSONEq(t *testing.T, got, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatalf("got is not JSON: %s", got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("want is not JSON: %s", want)
	}
	gb, _ := json.Marshal(g)
	wb, _ := json.Marshal(w)
	if !bytes.Equal(gb, wb) {
		t.Fatalf("JSON = %s, want %s", gb, wb)
	}
}

// TestNestedReadFile pins which requests the /v1 route finder treats as a
// nested read-file address, and the probe and path it derives.
func TestNestedReadFile(t *testing.T) {
	req := func(method, rawURL string) *http.Request {
		return httptest.NewRequest(method, rawURL, nil)
	}
	for _, c := range []struct {
		name      string
		r         *http.Request
		ok        bool
		path      string
		probePath string
	}{
		{"nested", req("GET", "/v1/projects/greeter/files/specs/a.md?ref=abc1234"), true, "specs/a.md", "/v1/projects/greeter/files/_"},
		{"escaped segments decode", req("GET", "/v1/projects/greeter/files/specs/%E4%BB%95%20x.md"), true, "specs/仕 x.md", "/v1/projects/greeter/files/_"},
		{"dot-dot escape decodes for the path rules", req("GET", "/v1/projects/greeter/files/specs/%2e%2e/x"), true, "specs/../x", "/v1/projects/greeter/files/_"},
		{"trailing slash is nested", req("GET", "/v1/projects/greeter/files/specs/"), true, "specs/", "/v1/projects/greeter/files/_"},
		// EscapedPath re-escapes a Path whose RawPath is not valid, so a stray
		// '%' arrives as %25 and decodes back to itself.
		{"literal percent", &http.Request{Method: "GET", URL: &url.URL{Path: "/v1/projects/greeter/files/specs/%zz"}}, true, "specs/%zz", "/v1/projects/greeter/files/_"},
		{"one segment is the generated route", req("GET", "/v1/projects/greeter/files/workload.yaml"), false, "", ""},
		{"non-GET", req("POST", "/v1/projects/greeter/files/specs/a.md"), false, "", ""},
		{"HEAD", req("HEAD", "/v1/projects/greeter/files/specs/a.md"), false, "", ""},
		{"missing files segment", req("GET", "/v1/projects/greeter/blobs/specs/a.md"), false, "", ""},
		{"empty project", req("GET", "/v1/projects//files/specs/a.md"), false, "", ""},
		{"no project", req("GET", "/v1/projects/"), false, "", ""},
		{"other group", req("GET", "/internal/v1/projects/greeter/files/specs/a.md"), false, "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			probe, path, ok := nestedReadFile(c.r)
			if ok != c.ok || path != c.path {
				t.Fatalf("nestedReadFile = (%q, %v), want (%q, %v)", path, ok, c.path, c.ok)
			}
			if !ok {
				return
			}
			if probe.URL.Path != c.probePath || probe.URL.RawQuery != c.r.URL.RawQuery || probe.Method != http.MethodGet {
				t.Fatalf("probe = %s %s?%s", probe.Method, probe.URL.Path, probe.URL.RawQuery)
			}
		})
	}
}
