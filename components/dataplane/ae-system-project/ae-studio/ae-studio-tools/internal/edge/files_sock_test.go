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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/files"
	"github.com/wso2/aep/ae-studio-tools/internal/projects"
	"github.com/wso2/aep/ae-studio-tools/internal/projects/projectstest"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
	"github.com/wso2/aep/ae-studio-tools/internal/repo/repotest"
)

// socketHarness serves FilesSocketRoutes on a real Unix socket bound by
// ListenFilesSocket, over a file:// origin that aep-api names acme/greeter.
type socketHarness struct {
	t        *testing.T
	path     string
	root     string
	origin   *repotest.Origin
	projects *projectstest.Fake
	client   *http.Client
}

// noCompletions is a Completer that completes nothing.
type noCompletions struct{}

func (noCompletions) Complete(context.Context, string, []files.WriteOp) (map[string]files.Completed, []files.Warning, error) {
	return nil, nil, nil
}

// fixedIdentity is the gitpat user every socket save commits as.
type fixedIdentity struct{}

func (fixedIdentity) Identity(context.Context) (string, string, error) {
	return "aep-bot", "aep-bot@users.noreply.github.com", nil
}

// shortSocketDir is a temp dir short enough for a Unix socket path (macOS
// caps sun_path at 104 bytes; t.TempDir() embeds the test name).
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "aefs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func newSocketHarness(t *testing.T, seed map[string]string) *socketHarness {
	t.Helper()
	origin := repotest.NewOrigin(t, seed)
	fake := projectstest.NewFake(map[string]projects.Repository{"greeter": {
		Owner: "acme", Repo: "greeter", DefaultBranch: repotest.Branch, CloneURL: origin.URL(),
	}})
	root := t.TempDir()
	engine, _, err := repo.New(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	applier := files.Applier{
		Reader:    files.Reader{Engine: engine, Projects: fake, Org: "default"},
		Completer: noCompletions{},
		Identity:  fixedIdentity{},
	}
	path := filepath.Join(shortSocketDir(t), "files.sock")
	ln, err := ListenFilesSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: FilesSocketRoutes(applier), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		},
	}}
	t.Cleanup(client.CloseIdleConnections)
	return &socketHarness{t: t, path: path, root: root, origin: origin, projects: fake, client: client}
}

type socketReply struct {
	Code   int
	Header http.Header
	Body   string
}

func (h *socketHarness) do(method, path string, body io.Reader) socketReply {
	h.t.Helper()
	req, err := http.NewRequest(method, "http://files"+path, body)
	if err != nil {
		h.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatal(err)
	}
	return socketReply{Code: resp.StatusCode, Header: resp.Header, Body: string(b)}
}

func (h *socketHarness) get(path string) socketReply { return h.do(http.MethodGet, path, nil) }

func (h *socketHarness) post(path, body string) socketReply {
	return h.do(http.MethodPost, path, strings.NewReader(body))
}

// originHead is the origin's branch tip.
func (h *socketHarness) originHead() string {
	h.t.Helper()
	return h.origin.HeadSHA(h.t)
}

// clonesOnDisk lists the engine's repos/ directory: empty until a git
// operation has run for some project.
func (h *socketHarness) clonesOnDisk() []string {
	h.t.Helper()
	entries, err := os.ReadDir(repo.ReposDir(h.root))
	if err != nil {
		h.t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func problemCode(t *testing.T, r socketReply) string {
	t.Helper()
	if ct := r.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content-type %q, body %s", ct, r.Body)
	}
	var p struct{ Code string }
	if err := json.Unmarshal([]byte(r.Body), &p); err != nil {
		t.Fatalf("problem body %q: %v", r.Body, err)
	}
	return p.Code
}

const oneWrite = `{"writes":[{"path":"specs/a.md","content":"b","baseSha":""}],"deletes":[],"message":"m"}`

// Review Focus 2: every call resolves the project through aep-api; a 404 is
// a denial and a 5xx a retryable 503, neither runs a git op; the socket
// schema has no owner/repo, so a request carrying one never reaches a lookup.
func TestFilesSocket_ResolvesEveryCallAndMapsErrors(t *testing.T) {
	h := newSocketHarness(t, map[string]string{"specs/a.md": "a", "src/main.go": "package main"})
	head := h.originHead()

	r := h.get("/projects/greeter")
	if r.Code != 200 {
		t.Fatalf("lookup = %d %s", r.Code, r.Body)
	}
	assertJSONEq(t, r.Body, `{"known":true,"owner":"acme","repo":"greeter","headSha":"`+head+`"}`)
	r = h.get("/projects/greeter/bundle?prefix=specs/")
	if r.Code != 200 {
		t.Fatalf("bundle = %d %s", r.Code, r.Body)
	}
	assertJSONEq(t, r.Body, `{"commitSha":"`+head+`","files":[{"path":"specs/a.md","content":"a","sha":"`+blobSHA("a")+`"}]}`)
	if n := h.projects.CallCount(); n != 2 {
		t.Fatalf("resolver calls = %d, want 2 (lookup + bundle each resolved)", n)
	}

	h.projects.SetErr(projects.ErrUnknown)
	r = h.post("/projects/greeter/apply", oneWrite)
	if r.Code != 404 || problemCode(t, r) != "project_unknown" {
		t.Fatalf("unknown project apply = %d %s", r.Code, r.Body)
	}
	if got := h.originHead(); got != head {
		t.Fatalf("origin moved after a denial: %s", got)
	}

	h.projects.SetErr(fmt.Errorf("%w: aep-api answered 503", projects.ErrUnavailable))
	r = h.post("/projects/greeter/apply", oneWrite)
	if r.Code != 503 || problemCode(t, r) != "aep_api_unavailable" || r.Header.Get("Retry-After") != "5" {
		t.Fatalf("aep-api down apply = %d Retry-After=%q %s", r.Code, r.Header.Get("Retry-After"), r.Body)
	}
	if got := h.originHead(); got != head {
		t.Fatalf("origin moved while aep-api was down: %s", got)
	}
	if n := h.projects.CallCount(); n != 4 {
		t.Fatalf("resolver calls = %d, want 4 (each apply resolved)", n)
	}

	h.projects.SetErr(nil)
	for _, body := range []string{
		`{"owner":"someone-else","repo":"x","writes":[],"deletes":[],"message":"m"}`,
		`{"writes":[{"path":"specs/a.md","content":"b","baseSha":"","owner":"someone-else"}],"deletes":[],"message":"m"}`,
		`{"writes":[],"deletes":[],"message":"m","cloneUrl":"https://github.com/someone-else/x"}`,
	} {
		r = h.post("/projects/greeter/apply", body)
		if r.Code != 400 || problemCode(t, r) != "path_invalid" {
			t.Fatalf("body %s = %d %s, want 400 (additionalProperties: false)", body, r.Code, r.Body)
		}
	}
	if n := h.projects.CallCount(); n != 4 {
		t.Fatalf("resolver calls = %d after rejected bodies, want 4 (the validator runs before any lookup)", n)
	}

	fi, err := os.Stat(h.path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSocket == 0 || fi.Mode().Perm() != 0o660 {
		t.Fatalf("socket mode = %v, want a 0660 socket", fi.Mode())
	}
}

// A denial or aep-api down on the first call of a fresh pod leaves no clone:
// the lookup runs before any git operation on every op.
func TestFilesSocket_DenialRunsNoGitOp(t *testing.T) {
	for _, c := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"unknown", projects.ErrUnknown, 404, "project_unknown"},
		{"unavailable", fmt.Errorf("%w: dial tcp: refused", projects.ErrUnavailable), 503, "aep_api_unavailable"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newSocketHarness(t, map[string]string{"specs/a.md": "a"})
			h.projects.SetErr(c.err)
			for _, r := range []socketReply{
				h.get("/projects/greeter"),
				h.get("/projects/greeter/bundle?prefix=specs/"),
				h.post("/projects/greeter/apply", oneWrite),
			} {
				if r.Code != c.status || problemCode(t, r) != c.code {
					t.Fatalf("got %d %s, want %d %s", r.Code, r.Body, c.status, c.code)
				}
			}
			if got := h.clonesOnDisk(); len(got) != 0 {
				t.Fatalf("clones on disk after denials: %v", got)
			}
			if n := h.projects.CallCount(); n != 3 {
				t.Fatalf("resolver calls = %d, want 3", n)
			}
		})
	}
}

// The Room's cycle: seed from bundle, apply with its shas, apply again with
// a stale sha.
func TestFilesSocket_ApplyResultsAndConflicts(t *testing.T) {
	h := newSocketHarness(t, map[string]string{"specs/a.md": "a", "specs/gone.md": "x"})

	r := h.post("/projects/greeter/apply", `{"writes":[
		{"path":"specs/a.md","content":"a2","baseSha":"`+blobSHA("a")+`"},
		{"path":"specs/bad.json","content":"{","baseSha":""}
	],"deletes":[{"path":"specs/gone.md","baseSha":"`+blobSHA("x")+`"}],"message":"Co-authored-by: A <a@x>"}`)
	if r.Code != 200 || r.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("apply = %d %q %s", r.Code, r.Header.Get("Content-Type"), r.Body)
	}
	head := h.originHead()
	assertJSONEq(t, r.Body, `{"commitSha":"`+head+`","changed":true,"files":[
		{"path":"specs/a.md","sha":"`+blobSHA("a2")+`"},
		{"path":"specs/bad.json","sha":"`+blobSHA("{")+`"}
	],"warnings":[{"path":"specs/bad.json","message":"content is not valid JSON"}]}`)

	// Byte-identical: no commit, the tip, and warnings is an empty array.
	r = h.post("/projects/greeter/apply", `{"writes":[{"path":"specs/a.md","content":"a2","baseSha":"`+blobSHA("a2")+`"}],"deletes":[],"message":""}`)
	if r.Code != 200 {
		t.Fatalf("no-op apply = %d %s", r.Code, r.Body)
	}
	assertJSONEq(t, r.Body, `{"commitSha":"`+head+`","changed":false,"files":[{"path":"specs/a.md","sha":"`+blobSHA("a2")+`"}],"warnings":[]}`)

	// A stale baseSha: 409 with the conflicts, nothing applied.
	r = h.post("/projects/greeter/apply", `{"writes":[
		{"path":"specs/a.md","content":"a3","baseSha":"`+blobSHA("a")+`"},
		{"path":"specs/new.md","content":"n","baseSha":""}
	],"deletes":[],"message":"m"}`)
	if r.Code != 409 || r.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("stale apply = %d %q %s", r.Code, r.Header.Get("Content-Type"), r.Body)
	}
	assertJSONEq(t, r.Body, `{"code":"conflict","conflicts":[{"path":"specs/a.md","baseSha":"`+blobSHA("a")+`","currentSha":"`+blobSHA("a2")+`"}]}`)
	if got := h.originHead(); got != head {
		t.Fatalf("origin moved on a conflict: %s", got)
	}
}

func TestFilesSocket_RequestErrors(t *testing.T) {
	h := newSocketHarness(t, map[string]string{"specs/a.md": "a"})
	cases := []struct {
		method, path, body string
		want               int
		code               string
	}{
		{"POST", "/projects/greeter/apply", `{"writes":[{"path":"src/main.go","content":"x","baseSha":""}],"deletes":[],"message":"m"}`, 400, "path_invalid"},
		{"POST", "/projects/greeter/apply", `{"writes":[],"deletes":[],"message":"m"}`, 400, "path_invalid"},
		{"POST", "/projects/greeter/apply", `{"writes":[]}`, 400, "path_invalid"},
		{"POST", "/projects/greeter/apply", `not json`, 400, "path_invalid"},
		{"GET", "/projects/greeter/apply", "", 404, "not_found"},
		{"PUT", "/projects/greeter", "", 404, "not_found"},
		{"GET", "/projects/greeter/files", "", 404, "not_found"},
		{"GET", "/projects/greeter/../greeter", "", 404, "not_found"},
		{"GET", "/projects//bundle", "", 404, "not_found"},
		{"GET", "/v1/projects/greeter/files", "", 404, "not_found"},
		{"GET", "/", "", 404, "not_found"},
	}
	for _, c := range cases {
		var body io.Reader
		if c.body != "" {
			body = strings.NewReader(c.body)
		}
		r := h.do(c.method, c.path, body)
		if r.Code != c.want || problemCode(t, r) != c.code {
			t.Errorf("%s %s = %d %s, want %d %s", c.method, c.path, r.Code, r.Body, c.want, c.code)
		}
	}
	// None resolves: the validator and the route table refuse the shape, and
	// the write rules run before the apply's lookup.
	if n := h.projects.CallCount(); n != 0 {
		t.Fatalf("resolver calls = %d, want 0", n)
	}
}

// unsized hides a reader's length, so the request is sent chunked.
type unsized struct{ io.Reader }

// The apply body cap is 25 MiB: a declared length over it and a chunked body
// that runs past it are both 413 before any lookup.
func TestFilesSocket_ApplyBodyCap(t *testing.T) {
	h := newSocketHarness(t, map[string]string{"specs/a.md": "a"})
	big := `{"writes":[{"path":"specs/a.md","content":"` + strings.Repeat("x", 25<<20) + `","baseSha":""}],"deletes":[],"message":"m"}`
	for name, body := range map[string]io.Reader{
		"declared": strings.NewReader(big),
		"chunked":  unsized{strings.NewReader(big)},
	} {
		r := h.do(http.MethodPost, "/projects/greeter/apply", body)
		if r.Code != 413 || problemCode(t, r) != "payload_too_large" {
			t.Fatalf("%s = %d %s", name, r.Code, r.Body)
		}
	}
	if n := h.projects.CallCount(); n != 0 {
		t.Fatalf("resolver calls = %d, want 0", n)
	}
}

func TestListenFilesSocket(t *testing.T) {
	t.Run("removes a stale socket and sets 0660", func(t *testing.T) {
		path := filepath.Join(shortSocketDir(t), "files.sock")
		stale, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		stale.(*net.UnixListener).SetUnlinkOnClose(false)
		_ = stale.Close() // the file stays behind, as after a crash
		ln, err := ListenFilesSocket(path)
		if err != nil {
			t.Fatalf("over a stale socket: %v", err)
		}
		defer func() { _ = ln.Close() }()
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o660 {
			t.Fatalf("mode = %v", fi.Mode())
		}
	})
	t.Run("refuses to replace a non-socket file", func(t *testing.T) {
		path := filepath.Join(shortSocketDir(t), "files.sock")
		if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		if ln, err := ListenFilesSocket(path); err == nil {
			_ = ln.Close()
			t.Fatal("bound over a regular file")
		}
		if b, err := os.ReadFile(path); err != nil || string(b) != "keep" {
			t.Fatalf("regular file touched: %q %v", b, err)
		}
	})
	t.Run("closing unlinks the socket", func(t *testing.T) {
		path := filepath.Join(shortSocketDir(t), "files.sock")
		ln, err := ListenFilesSocket(path)
		if err != nil {
			t.Fatal(err)
		}
		_ = ln.Close()
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("socket left after close: %v", err)
		}
	})
}

// deadlineResolver records the deadline of the context it is called with and
// answers unknown.
type deadlineResolver struct{ got chan time.Time }

func (r deadlineResolver) Resolve(ctx context.Context, project string) (projects.Repository, error) {
	d, ok := ctx.Deadline()
	if !ok {
		d = time.Time{}
	}
	r.got <- d
	return projects.Repository{}, fmt.Errorf("%w: %q", projects.ErrUnknown, project)
}

// Every socket request runs under the pod's own budget, so the pod always
// answers before ae-collab's per-call deadline (REQUEST_TIMEOUT_MS) gives up.
func TestFilesSocket_RequestsRunUnderTheBudget(t *testing.T) {
	res := deadlineResolver{got: make(chan time.Time, 1)}
	h := FilesSocketRoutes(files.Applier{Reader: files.Reader{Projects: res, Org: "default"}})
	start := time.Now()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/projects/greeter", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
	d := <-res.got
	if d.IsZero() {
		t.Fatal("the request ran with no deadline")
	}
	if left := d.Sub(start); left > filesSocketRequestBudget+time.Second || left < filesSocketRequestBudget-5*time.Second {
		t.Fatalf("deadline in %s, want about %s", left, filesSocketRequestBudget)
	}
	if filesSocketRequestBudget >= 45*time.Second {
		t.Fatalf("budget %s must end before ae-collab's 45 s per-call deadline", filesSocketRequestBudget)
	}
}
