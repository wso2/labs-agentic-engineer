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

// Component tier for the Files API: the REAL contract-first handler chain
// (strict server via componenttest) over the production gitrepo gateway —
// reads AND the Apply write through the REAL gitfs Workspace engine mirroring
// a REAL bare file:// origin (pure workspacetest fixture; the Git-Data fake is
// gone with the REST write path).
// Only the repo row + credential resolver are faked, so list/read/apply run
// against genuine git object-store semantics — a stale baseSha is a real 409,
// a multi-write+delete apply is a real single commit pushed to origin under
// --force-with-lease, and a read right after an apply proves the mirror
// freshening.
package spec_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs/workspacetest"
	"github.com/wso2/aep/aep-api/internal/platform/gittest"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

const (
	filesTestOrg  = "acme-org"
	filesTestProj = "widgets"
	testSlug      = "acme-widgets"
	apiBase       = "/api/v1/projects/" + filesTestProj + "/files"
)

// ---- faked edges ----

// filesStubRepoResolver hands out the single repo row, keyed by the AUTHENTICATED
// org — a caller resolved to any other org gets ErrRepoNotFound (the 404),
// mirroring the production (org_id, project_id) row lookup.
type filesStubRepoResolver struct{ rec *sourcecontrol.GitRepository }

func (s filesStubRepoResolver) GetRepo(_ context.Context, orgID, _ string) (*sourcecontrol.GitRepository, error) {
	if s.rec == nil || orgID != s.rec.OrgID {
		return nil, sourcecontrol.ErrRepoNotFound
	}
	return s.rec, nil
}

type filesStubCred struct{}

func (filesStubCred) Token(context.Context) (string, time.Time, error) {
	return "test-token", time.Time{}, nil
}
func (filesStubCred) Identity() secrets.Identity {
	return secrets.Identity{Name: "Bot", Email: "bot@aep.dev", Login: "bot"}
}
func (filesStubCred) RepoOwner() string                        { return "acme" }
func (filesStubCred) WebhookStrategy() secrets.WebhookStrategy { return secrets.WebhookPlatform }

type filesStubResolver struct{}

func (filesStubResolver) Resolve(context.Context, string) (secrets.Credential, error) {
	return filesStubCred{}, nil
}

// ---- harness ----

type filesRig struct {
	h        *componenttest.Harness
	remote   *gittest.Remote
	engine   *gitfs.Engine
	activity *captureSpecUpdated
}

// captureSpecUpdated records spec.SpecUpdatedRecorder calls so an apply's
// activity line (issue #239) can be asserted without a database.
type captureSpecUpdated struct {
	mu      sync.Mutex
	commits []string
}

func (c *captureSpecUpdated) RecordSpecUpdated(_ context.Context, _, _, commitSHA string, _ []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.commits = append(c.commits, commitSHA)
}

func (c *captureSpecUpdated) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.commits...)
}

func newFilesRig(t *testing.T, seed map[string]string) *filesRig {
	t.Helper()
	remote := gittest.NewRemote(t, gittest.WithSeed(seed, "seed"))
	rec := &sourcecontrol.GitRepository{
		OrgID:         filesTestOrg,
		ProjectID:     filesTestProj,
		RepoURL:       remote.URL(),
		RepoSlug:      testSlug, // pinned — SlugForURL can't parse file:// URLs
		DefaultBranch: "main",
		Status:        "ready",
	}
	// The production gateway over the real engine: every read AND the Apply
	// write run through the Workspace port (the REST git-object port is nil —
	// files never touches it).
	engine := workspacetest.NewEngine(t)
	gitOps := sourcecontrol.NewGitOpsService(filesStubResolver{}, engine)
	svc := spec.NewFilesService(filesStubRepoResolver{rec: rec}, gitOps)
	act := &captureSpecUpdated{}
	h := componenttest.New(t, componenttest.Options{Deps: edge.Deps{
		Spec: mustSpecHandlers(t, spec.Deps{Files: svc, FilesActivity: act}),
	}})
	return &filesRig{h: h, remote: remote, engine: engine, activity: act}
}

// mirrorRevParse resolves rev inside the ENGINE's bare mirror (not the origin)
// — the C8 sha-consistency probe.
func (r *filesRig) mirrorRevParse(t *testing.T, rev string) string {
	t.Helper()
	repoDir, err := gitfs.RepoDir(r.engine.Root(), gitfs.RepoRef{
		OrgID: filesTestOrg, ProjectID: filesTestProj, RepoSlug: testSlug,
	})
	if err != nil {
		t.Fatalf("mirror git dir: %v", err)
	}
	gitDir := gitfs.GitSubdir(repoDir)
	cmd := exec.Command("git", "--git-dir", gitDir, "rev-parse", "--verify", rev)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mirror rev-parse %s: %v\n%s", rev, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *filesRig) get(path string) *httptest.ResponseRecorder {
	return r.h.AsOrg(filesTestOrg).Get(path)
}

func (r *filesRig) apply(body string) *httptest.ResponseRecorder {
	return r.h.AsOrg(filesTestOrg).Post(apiBase+"/apply", body)
}

// readSHA reads a file through the API and returns its blob sha (the draft's
// baseSha).
func (r *filesRig) readSHA(t *testing.T, path string) string {
	t.Helper()
	rec := r.get(apiBase + "/" + path)
	if rec.Code != http.StatusOK {
		t.Fatalf("read %s: code %d (%s)", path, rec.Code, rec.Body.String())
	}
	var fc spec.FileContent
	if err := json.Unmarshal(rec.Body.Bytes(), &fc); err != nil {
		t.Fatalf("decode read: %v", err)
	}
	return fc.SHA
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// ---- tests ----

func TestListAtHead_FilteredByPrefix(t *testing.T) {
	r := newFilesRig(t, map[string]string{
		"specs/requirements/prd.md":    "req",
		"specs/design/domain-model.md": "des",
		"README.md":                    "root",
	})
	rec := r.get(apiBase + "?prefix=specs/design/")
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d: %s", rec.Code, rec.Body.String())
	}
	var metas []spec.FileMeta
	if err := json.Unmarshal(rec.Body.Bytes(), &metas); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(metas) != 1 || metas[0].Path != "specs/design/domain-model.md" {
		t.Fatalf("prefix filter wrong: %+v", metas)
	}
	if metas[0].SHA == "" || metas[0].Size == 0 {
		t.Errorf("meta missing sha/size: %+v", metas[0])
	}
}

func TestReadAtHead(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "hello world"})
	rec := r.get(apiBase + "/specs/requirements/prd.md")
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d: %s", rec.Code, rec.Body.String())
	}
	var fc spec.FileContent
	_ = json.Unmarshal(rec.Body.Bytes(), &fc)
	if fc.Content != "hello world" || fc.Path != "specs/requirements/prd.md" || fc.SHA == "" {
		t.Fatalf("read wrong: %+v", fc)
	}

	if miss := r.get(apiBase + "/specs/requirements/missing.md"); miss.Code != http.StatusNotFound {
		t.Errorf("missing file: code %d, want 404", miss.Code)
	}
}

// TestReadAtHead_ValidationReportAllowListed pins the read-only escape hatch:
// tests/validation/report.json (a non-specs/ path) is READABLE, but any other
// non-specs/ path stays 400, and WRITING the report is still rejected — the
// write path is never widened.
func TestReadAtHead_ValidationReportAllowListed(t *testing.T) {
	report := `{"schemaVersion":1,"criteria":[]}`
	r := newFilesRig(t, map[string]string{
		"tests/validation/report.json": report,
		"README.md":                    "root",
	})

	// The one allow-listed non-specs/ path reads at HEAD.
	rec := r.get(apiBase + "/tests/validation/report.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("read report: code %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var fc spec.FileContent
	_ = json.Unmarshal(rec.Body.Bytes(), &fc)
	if fc.Content != report || fc.Path != "tests/validation/report.json" || fc.SHA == "" {
		t.Fatalf("read report wrong: %+v", fc)
	}

	// A different non-specs/ path is still forbidden (allow-list is exact).
	if bad := r.get(apiBase + "/README.md"); bad.Code != http.StatusBadRequest {
		t.Errorf("read README.md: code %d, want 400 (%s)", bad.Code, bad.Body.String())
	}

	// The write path is NOT widened: applying to the allow-listed path is 400.
	writeReport := mustJSON(t, spec.ApplyRequest{
		Writes: []spec.WriteOp{{Path: "tests/validation/report.json", Content: report}},
	})
	if w := r.apply(writeReport); w.Code != http.StatusBadRequest {
		t.Errorf("apply report write: code %d, want 400 (%s)", w.Code, w.Body.String())
	}
}

func TestApply_MultiWriteAndDelete_SingleCommit(t *testing.T) {
	r := newFilesRig(t, map[string]string{
		"specs/requirements/prd.md":  "old",
		"specs/requirements/todo.md": "scratch",
	})
	reqSHA := r.readSHA(t, "specs/requirements/prd.md")
	todoSHA := r.readSHA(t, "specs/requirements/todo.md")
	headBefore := r.remote.HeadSHA(t)

	body := mustJSON(t, spec.ApplyRequest{
		Writes: []spec.WriteOp{
			{Path: "specs/requirements/prd.md", Content: "new", BaseSHA: reqSHA},
			{Path: "specs/design/domain-model.md", Content: "# Design"}, // baseSha omitted ⇒ create
		},
		Deletes: []spec.DeleteOp{{Path: "specs/requirements/todo.md", BaseSHA: todoSHA}},
		Message: "from test",
	})
	rec := r.apply(body)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply code %d: %s", rec.Code, rec.Body.String())
	}
	var res spec.ApplyResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.CommitSHA == "" || len(res.Files) != 2 {
		t.Fatalf("apply result wrong: %+v", res)
	}

	// Exactly one new commit; content applied; delete honored.
	if r.remote.HeadSHA(t) == headBefore {
		t.Error("HEAD did not advance")
	}
	if got := r.remote.FileAt(t, "main", "specs/requirements/prd.md"); got != "new" {
		t.Errorf("requirements.md = %q, want new", got)
	}
	if got := r.remote.FileAt(t, "main", "specs/design/domain-model.md"); got != "# Design" {
		t.Errorf("domain-model.md = %q", got)
	}
	tags := r.remote.Tags(t) // deletes leave no tag; just confirm no crash
	_ = tags
	// todo.md gone: reading it now 404s.
	if miss := r.get(apiBase + "/specs/requirements/todo.md"); miss.Code != http.StatusNotFound {
		t.Errorf("todo.md still present: code %d", miss.Code)
	}
}

// An apply that lands a real commit records the spec_updated activity line
// (issue #239), keyed by the commit sha; a byte-identical re-apply makes no
// commit and records nothing.
func TestApply_RecordsSpecUpdatedActivity(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "old"})
	sha := r.readSHA(t, "specs/requirements/prd.md")

	rec := r.apply(mustJSON(t, spec.ApplyRequest{
		Writes:  []spec.WriteOp{{Path: "specs/requirements/prd.md", Content: "new", BaseSHA: sha}},
		Message: "from test",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("apply code %d: %s", rec.Code, rec.Body.String())
	}
	var res spec.ApplyResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)

	recorded := r.activity.all()
	if len(recorded) != 1 {
		t.Fatalf("recorded commits = %v, want exactly one", recorded)
	}
	if recorded[0] != res.CommitSHA {
		t.Errorf("recorded commit = %q, want %q", recorded[0], res.CommitSHA)
	}

	// Byte-identical re-apply: preconditions pass, nothing changes, no line.
	sha2 := r.readSHA(t, "specs/requirements/prd.md")
	rec2 := r.apply(mustJSON(t, spec.ApplyRequest{
		Writes:  []spec.WriteOp{{Path: "specs/requirements/prd.md", Content: "new", BaseSHA: sha2}},
		Message: "noop",
	}))
	if rec2.Code != http.StatusOK {
		t.Fatalf("no-op apply code %d: %s", rec2.Code, rec2.Body.String())
	}
	if recorded := r.activity.all(); len(recorded) != 1 {
		t.Fatalf("no-op apply recorded a line: %v", recorded)
	}
}

func TestApply_StaleBaseSHA_409_NothingApplied(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "v1"})
	headBefore := r.remote.HeadSHA(t)

	body := mustJSON(t, spec.ApplyRequest{
		Writes: []spec.WriteOp{
			{Path: "specs/requirements/prd.md", Content: "v2", BaseSHA: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"},
		},
	})
	rec := r.apply(body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("apply code %d, want 409: %s", rec.Code, rec.Body.String())
	}
	// THE FROZEN 409 CONTRACT — exact field set and values (key order is not
	// part of JSON; the shape itself is contract-tied via ApplyConflicts).
	// currentSha is the git blob sha of "v1" (deterministic), baseSha echoes
	// the stale sha the caller sent.
	var got409 struct {
		Conflicts []map[string]string `json:"conflicts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got409); err != nil {
		t.Fatalf("409 body not JSON: %v\n%s", err, rec.Body.String())
	}
	want409 := []map[string]string{{
		"path":       "specs/requirements/prd.md",
		"baseSha":    "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		"currentSha": "28c218c44b49222f91536daf5b4d9871638edc8e",
	}}
	if !reflect.DeepEqual(got409.Conflicts, want409) {
		t.Fatalf("409 body drifted:\n got: %s\nwant: %+v", rec.Body.String(), want409)
	}
	// Nothing applied — HEAD unchanged, content unchanged.
	if r.remote.HeadSHA(t) != headBefore {
		t.Error("HEAD advanced on a conflicting apply")
	}
	if got := r.remote.FileAt(t, "main", "specs/requirements/prd.md"); got != "v1" {
		t.Errorf("content mutated on conflict: %q", got)
	}
}

// A batch where only ONE op conflicts is rejected wholesale: the valid delete
// must not be applied (all-or-nothing), and every conflict is collected.
func TestApply_BatchConflict_AllOrNothing_CollectsAllConflicts(t *testing.T) {
	r := newFilesRig(t, map[string]string{
		"specs/requirements/prd.md":  "keep me",
		"specs/requirements/todo.md": "scratch",
	})
	todoSHA := r.readSHA(t, "specs/requirements/todo.md")
	headBefore := r.remote.HeadSHA(t)

	body := mustJSON(t, spec.ApplyRequest{
		Writes: []spec.WriteOp{
			{Path: "specs/requirements/prd.md", Content: "clobber", BaseSHA: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"},
			{Path: "specs/design/domain-model.md", Content: "new", BaseSHA: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"}, // absent + baseSha set ⇒ conflict too
		},
		Deletes: []spec.DeleteOp{{Path: "specs/requirements/todo.md", BaseSHA: todoSHA}}, // valid — must still NOT apply
	})
	rec := r.apply(body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code %d, want 409: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Conflicts []spec.Conflict `json:"conflicts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode conflicts: %v", err)
	}
	if len(got.Conflicts) != 2 {
		t.Fatalf("conflicts = %+v, want ALL 2 collected", got.Conflicts)
	}
	if r.remote.HeadSHA(t) != headBefore {
		t.Error("HEAD advanced on a conflicting batch")
	}
	if got := r.remote.FileAt(t, "main", "specs/requirements/todo.md"); got != "scratch" {
		t.Errorf("valid delete leaked through a conflicting batch: todo.md = %q", got)
	}
}

func TestApply_BaseSHAOmittedButExists_409(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "exists"})
	body := mustJSON(t, spec.ApplyRequest{
		Writes: []spec.WriteOp{
			{Path: "specs/requirements/prd.md", Content: "clobber"}, // no baseSha ⇒ must-not-exist
		},
	})
	rec := r.apply(body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code %d, want 409: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Conflicts []spec.Conflict `json:"conflicts"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Conflicts) != 1 || got.Conflicts[0].BaseSHA != "" || got.Conflicts[0].CurrentSHA == "" {
		t.Fatalf("expected must-not-exist conflict: %+v", got.Conflicts)
	}
}

func TestApply_PathRejections(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "x"})
	cases := map[string]string{
		"traversal": mustJSON(t, spec.ApplyRequest{Writes: []spec.WriteOp{{Path: "specs/../etc/passwd", Content: "x"}}}),
		"non-specs": mustJSON(t, spec.ApplyRequest{Writes: []spec.WriteOp{{Path: "README.md", Content: "x"}}}),
		"absolute":  mustJSON(t, spec.ApplyRequest{Writes: []spec.WriteOp{{Path: "/specs/x.md", Content: "x"}}}),
	}
	for name, body := range cases {
		if rec := r.apply(body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: code %d, want 400 (%s)", name, rec.Code, rec.Body.String())
		}
	}
}

func TestApply_SizeCap(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "x"})
	huge := strings.Repeat("A", (5<<20)+1)
	body := mustJSON(t, spec.ApplyRequest{Writes: []spec.WriteOp{{Path: "specs/requirements/big.md", Content: huge}}})
	if rec := r.apply(body); rec.Code != http.StatusBadRequest {
		t.Errorf("size cap: code %d, want 400", rec.Code)
	}
}

// A text file keeps today's wire shape exactly — no encoding key at all, so
// every existing consumer (the spec editor, the FE viewer) is untouched.
func TestRead_TextFileKeepsTodaysWireShape(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "# PRD\nplain text ✅\n"})

	rec := r.get(apiBase + "/specs/requirements/prd.md")
	if rec.Code != http.StatusOK {
		t.Fatalf("read code %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, `"encoding"`) {
		t.Fatalf("text read grew an encoding key — the default must stay implicit: %s", firstBytes(body, 200))
	}
	var fc spec.FileContent
	if err := json.Unmarshal(rec.Body.Bytes(), &fc); err != nil {
		t.Fatalf("decode read: %v", err)
	}
	if fc.Content != "# PRD\nplain text ✅\n" {
		t.Fatalf("content = %q, want it verbatim", fc.Content)
	}
}

func firstBytes(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func TestApply_WarningsNonBlocking(t *testing.T) {
	r := newFilesRig(t, nil)
	body := mustJSON(t, spec.ApplyRequest{
		Writes: []spec.WriteOp{
			{Path: "specs/design/components/foo/design.json", Content: "{ not valid json"},
		},
	})
	rec := r.apply(body)
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid json must NOT block apply: code %d (%s)", rec.Code, rec.Body.String())
	}
	var res spec.ApplyResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if len(res.Warnings) != 1 || res.Warnings[0].Code != "INVALID_JSON" {
		t.Fatalf("expected one INVALID_JSON warning: %+v", res.Warnings)
	}
	// The (invalid) file was still committed — warnings never block.
	if got := r.remote.FileAt(t, "main", "specs/design/components/foo/design.json"); got != "{ not valid json" {
		t.Errorf("file not committed: %q", got)
	}
}

func TestApply_SchemaViolationWarning_NonBlocking(t *testing.T) {
	r := newFilesRig(t, nil)
	// Valid JSON + valid schema, but name != component directory ("bar" != "foo").
	valid := `{"name":"bar","type":"service","version":"1","language":"go","buildpack":"go","appPath":".","entrypoint":"m","exposure":"intranet","connections":[],"description":"d"}`
	body := mustJSON(t, spec.ApplyRequest{
		Writes: []spec.WriteOp{{Path: "specs/design/components/foo/design.json", Content: valid}},
	})
	rec := r.apply(body)
	if rec.Code != http.StatusOK {
		t.Fatalf("schema violation must NOT block apply: code %d (%s)", rec.Code, rec.Body.String())
	}
	var res spec.ApplyResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if len(res.Warnings) != 1 || res.Warnings[0].Code != "SCHEMA_VIOLATION" {
		t.Fatalf("expected one SCHEMA_VIOLATION warning: %+v", res.Warnings)
	}
	if got := r.remote.FileAt(t, "main", "specs/design/components/foo/design.json"); got != valid {
		t.Errorf("file not committed despite warning: %q", got)
	}
}

func TestFiles_NoAuth_401(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "x"})
	if rec := r.h.NoAuth().Get(apiBase); rec.Code != http.StatusUnauthorized {
		t.Errorf("no-auth list: code %d, want 401", rec.Code)
	}
}

// The org is derived solely from the verified token, and the repo row is keyed
// by it — a caller from another org resolves no repo and gets a 404, never the
// project's files (the mount path deriver consults only the row, D6).
func TestFiles_CrossOrg_404(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "secret"})
	if rec := r.h.AsOrg("intruder-org").Get(apiBase); rec.Code != http.StatusNotFound {
		t.Errorf("cross-org list: code %d, want 404 (%s)", rec.Code, rec.Body.String())
	}
	if rec := r.h.AsOrg("intruder-org").Get(apiBase + "/specs/requirements/prd.md"); rec.Code != http.StatusNotFound {
		t.Errorf("cross-org read: code %d, want 404", rec.Code)
	}
}

// A unicode path survives the whole chain — URL escaping, the ServeMux
// {path...} catch-all (server.go) + wrapper PathValue decoding, ls-tree -z
// (unquoted NUL plumbing), cat-file — byte-identically.
func TestReadAtHead_UnicodePath(t *testing.T) {
	const path = "specs/requirements/仕様-résumé ノート.md"
	const content = "非ASCIIコンテンツ — ünïcödé\n"
	r := newFilesRig(t, map[string]string{path: content})

	escaped := (&url.URL{Path: "/" + path}).EscapedPath()
	rec := r.get(apiBase + escaped)
	if rec.Code != http.StatusOK {
		t.Fatalf("unicode read: code %d (%s)", rec.Code, rec.Body.String())
	}
	var fc spec.FileContent
	if err := json.Unmarshal(rec.Body.Bytes(), &fc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fc.Path != path || fc.Content != content || fc.SHA == "" {
		t.Fatalf("unicode read wrong: %+v", fc)
	}

	list := r.get(apiBase + "?prefix=specs/requirements/")
	var metas []spec.FileMeta
	_ = json.Unmarshal(list.Body.Bytes(), &metas)
	if len(metas) != 1 || metas[0].Path != path {
		t.Fatalf("unicode list wrong: %+v", metas)
	}
}

// A ~1 MiB file reads back byte-identically with the exact blob size in the
// listing (well under the 5 MiB write cap, well over any pipe-buffer size).
func TestReadAtHead_LargeFile(t *testing.T) {
	const path = "specs/design/big.md"
	content := strings.Repeat("0123456789abcdef", 1<<16) // 1 MiB
	r := newFilesRig(t, map[string]string{path: content})

	rec := r.get(apiBase + "/" + path)
	if rec.Code != http.StatusOK {
		t.Fatalf("large read: code %d", rec.Code)
	}
	var fc spec.FileContent
	if err := json.Unmarshal(rec.Body.Bytes(), &fc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fc.Content != content {
		t.Fatalf("large read: content mismatch (%d bytes, want %d)", len(fc.Content), len(content))
	}

	list := r.get(apiBase + "?prefix=specs/design/")
	var metas []spec.FileMeta
	_ = json.Unmarshal(list.Body.Bytes(), &metas)
	if len(metas) != 1 || metas[0].Size != int64(len(content)) {
		t.Fatalf("large list wrong: %+v", metas)
	}
}

// SHA consistency (design C8): the commitSha the apply returns is the sha on
// the ORIGIN's branch tip AND the mirror's local ref; the per-file shas in the
// response are the exact blob shas a subsequent read (ls-tree) returns — the
// FE folds them into its next baseShas.
func TestApply_ShaConsistency_OriginMirrorAndReadBack(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "v1"})
	reqSHA := r.readSHA(t, "specs/requirements/prd.md")

	body := mustJSON(t, spec.ApplyRequest{
		Writes: []spec.WriteOp{
			{Path: "specs/requirements/prd.md", Content: "v2", BaseSHA: reqSHA},
			{Path: "specs/design/domain-model.md", Content: "# Design"},
		},
	})
	rec := r.apply(body)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply code %d: %s", rec.Code, rec.Body.String())
	}
	var res spec.ApplyResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Returned commit sha == origin tip == mirror ref.
	if origin := r.remote.HeadSHA(t); res.CommitSHA != origin {
		t.Errorf("commitSha %s != origin tip %s", res.CommitSHA, origin)
	}
	if mirror := r.mirrorRevParse(t, "refs/heads/main"); res.CommitSHA != mirror {
		t.Errorf("commitSha %s != mirror ref %s", res.CommitSHA, mirror)
	}
	// Returned per-file shas == what a subsequent read serves.
	for _, f := range res.Files {
		if got := r.readSHA(t, f.Path); got != f.SHA {
			t.Errorf("%s: apply returned sha %s, read returns %s", f.Path, f.SHA, got)
		}
	}
}

// Two concurrent applies to DISJOINT paths: one fast-forward-lands, the other
// is push-rejected, re-fetches, re-checks its (still valid) preconditions and
// lands — both 200, both files present, exactly two commits, linear history.
func TestApply_ConcurrentDisjointApplies_BothLand(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "seed"})
	base := r.remote.HeadSHA(t)

	bodies := []string{
		mustJSON(t, spec.ApplyRequest{Writes: []spec.WriteOp{{Path: "specs/design/a.md", Content: "A"}}}),
		mustJSON(t, spec.ApplyRequest{Writes: []spec.WriteOp{{Path: "specs/design/b.md", Content: "B"}}}),
	}
	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i := range bodies {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = r.apply(bodies[i]).Code
		}(i)
	}
	wg.Wait()
	if codes[0] != http.StatusOK || codes[1] != http.StatusOK {
		t.Fatalf("codes = %v, want both 200 (disjoint applies must both land)", codes)
	}
	if got := r.remote.FileAt(t, "main", "specs/design/a.md"); got != "A" {
		t.Errorf("a.md = %q", got)
	}
	if got := r.remote.FileAt(t, "main", "specs/design/b.md"); got != "B" {
		t.Errorf("b.md = %q", got)
	}
	// Exactly two new commits on a linear history (the loser REPLAYED, it did
	// not merge or clobber).
	if n := r.remote.HeadSHA(t); n == base {
		t.Fatal("HEAD did not advance")
	}
}

// Two concurrent applies to the SAME path with the same baseSha: exactly one
// lands, the other re-runs its precondition against the winner's commit and
// gets the clean 409 — never a lost update.
func TestApply_ConcurrentSamePath_OneLandsOne409(t *testing.T) {
	const path = "specs/requirements/prd.md"
	r := newFilesRig(t, map[string]string{path: "seed"})
	baseSHA := r.readSHA(t, path)

	bodies := []string{
		mustJSON(t, spec.ApplyRequest{Writes: []spec.WriteOp{{Path: path, Content: "first", BaseSHA: baseSHA}}}),
		mustJSON(t, spec.ApplyRequest{Writes: []spec.WriteOp{{Path: path, Content: "second", BaseSHA: baseSHA}}}),
	}
	codes := make([]int, 2)
	var wg sync.WaitGroup
	for i := range bodies {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = r.apply(bodies[i]).Code
		}(i)
	}
	wg.Wait()
	ok, conflict := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflict++
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("codes = %v, want exactly one 200 and one 409", codes)
	}
	got := r.remote.FileAt(t, "main", path)
	if got != "first" && got != "second" {
		t.Errorf("content = %q, want the single winner's write", got)
	}
}

// Branch-tip reads freshen the mirror on every request: a commit made directly
// on the ORIGIN (an external writer) is visible on the very next read, with the
// new blob sha — there is no cache tier to go stale.
func TestRead_SeesOriginAdvanceImmediately(t *testing.T) {
	const path = "specs/requirements/prd.md"
	r := newFilesRig(t, map[string]string{path: "v1"})

	sha1 := r.readSHA(t, path)

	// External writer advances origin (not through the API).
	r.remote.Seed(t, map[string]string{path: "v2 external"}, "external edit")

	rec := r.get(apiBase + "/" + path)
	if rec.Code != http.StatusOK {
		t.Fatalf("re-read: code %d", rec.Code)
	}
	var fc spec.FileContent
	_ = json.Unmarshal(rec.Body.Bytes(), &fc)
	if fc.Content != "v2 external" {
		t.Fatalf("content = %q, want the origin's new commit (no stale cache)", fc.Content)
	}
	if fc.SHA == sha1 {
		t.Error("blob sha did not change across an origin advance")
	}
}

// TestApply_ScaffoldsComponentsFromCell pins the scaffold engine (#371): a
// batch that writes specs/design/design.cell also lands a design.json skeleton
// for every deployable component the cell declares that has none yet — in the
// SAME commit. Non-deployable nodes (database) get no directory; a component
// whose design.json already exists (in the tree or in the batch) is untouched.
func TestApply_ScaffoldsComponentsFromCell(t *testing.T) {
	existing := `{"name":"lunch-api","type":"service","version":"0.1.0","language":"Go","buildpack":"docker","appPath":"lunch-api","entrypoint":"deployment/service","exposure":"intranet","dependencies":[],"description":"hand-written"}`
	r := newFilesRig(t, map[string]string{
		"specs/design/components/lunch-api/design.json": existing,
	})

	cell := "component lunch-api service\n" +
		"component lunch-web web-application\n" +
		"component slack-notifier service\n" +
		"component orders-db database\n"
	body := mustJSON(t, spec.ApplyRequest{
		Writes:  []spec.WriteOp{{Path: "specs/design/design.cell", Content: cell}},
		Message: "design cell",
	})
	rec := r.apply(body)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply code %d: %s", rec.Code, rec.Body.String())
	}

	// Scaffolded skeletons exist, valid per the design gate, name == dir.
	for id, wantType := range map[string]string{"lunch-web": "web-application", "slack-notifier": "service"} {
		content := r.remote.FileAt(t, "main", "specs/design/components/"+id+"/design.json")
		var parsed map[string]any
		if err := json.Unmarshal([]byte(content), &parsed); err != nil {
			t.Fatalf("scaffolded %s design.json not JSON: %v", id, err)
		}
		if parsed["name"] != id || parsed["type"] != wantType {
			t.Errorf("scaffold %s = name %v type %v", id, parsed["name"], parsed["type"])
		}
		if parsed["language"] == "" || parsed["description"] == "" {
			t.Errorf("scaffold %s missing enrichable defaults: %v", id, parsed)
		}
	}
	// Existing component: a cell save never touches an existing design.json.
	var existingParsed map[string]any
	if err := json.Unmarshal([]byte(r.remote.FileAt(t, "main", "specs/design/components/lunch-api/design.json")), &existingParsed); err != nil {
		t.Fatalf("existing design.json parse: %v", err)
	}
	if existingParsed["language"] != "Go" || existingParsed["description"] != "hand-written" {
		t.Errorf("existing enrichment clobbered: %v", existingParsed)
	}
	// The database node scaffolds nothing.
	if rec := r.get(apiBase + "/specs/design/components/orders-db/design.json"); rec.Code != http.StatusNotFound {
		t.Errorf("orders-db dir should not exist: code %d", rec.Code)
	}
}

// TestApply_AuthoredStoriesSurviveCellSave pins the authored `stories` field
// (#369): the agent claims a component's stories in design.json during
// enrichment, and a later cell save must not touch the claim — scaffolds are
// born without one.
func TestApply_AuthoredStoriesSurviveCellSave(t *testing.T) {
	enriched := `{"name":"lunch-api","type":"service","version":"0.1.0","language":"Go","buildpack":"docker","appPath":"lunch-api","entrypoint":"deployment/service","exposure":"intranet","dependencies":[],"description":"hand-written","stories":[9]}`
	r := newFilesRig(t, map[string]string{
		"specs/design/components/lunch-api/design.json": enriched,
	})

	cell := "component lunch-api service\n" +
		"component lunch-web web-application\n"
	rec := r.apply(mustJSON(t, spec.ApplyRequest{
		Writes:  []spec.WriteOp{{Path: "specs/design/design.cell", Content: cell}},
		Message: "design cell",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("apply code %d: %s", rec.Code, rec.Body.String())
	}

	var parsed map[string]any
	// Scaffolded component is born without a stories claim.
	if err := json.Unmarshal([]byte(r.remote.FileAt(t, "main", "specs/design/components/lunch-web/design.json")), &parsed); err != nil {
		t.Fatalf("scaffold parse: %v", err)
	}
	if _, present := parsed["stories"]; present {
		t.Errorf("scaffold carries stories = %v, want the agent to author it", parsed["stories"])
	}
	// The existing component's authored [9] survives the cell save.
	if err := json.Unmarshal([]byte(r.remote.FileAt(t, "main", "specs/design/components/lunch-api/design.json")), &parsed); err != nil {
		t.Fatalf("existing parse: %v", err)
	}
	if got := fmt.Sprint(parsed["stories"]); got != "[9]" {
		t.Errorf("authored stories = %v, want [9] untouched", parsed["stories"])
	}
	if parsed["description"] != "hand-written" || parsed["language"] != "Go" {
		t.Errorf("cell save clobbered enrichment: %v", parsed)
	}
}
