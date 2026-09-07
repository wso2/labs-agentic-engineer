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

// Component tier for read-file-bundle, over the same real chain as the rest of
// the Files API (contract-first handler → real gitfs engine → real bare file://
// origin). Two properties carry the feature and neither is visible from the
// response body alone, so they are asserted separately:
//
//   - the bundle AGREES with the per-file reads it replaces (content, shas, and
//     the read gate), and
//   - it resolves the branch tip EXACTLY ONCE and addresses every subsequent
//     read by that object name.
//
// The second is the whole point. gitfs revalidates the mirror against origin for
// any read addressed by a ref and serves object-addressed reads from local
// objects, so "one ref-addressed call" is what makes the bundle one network
// round trip instead of one per file — and pinning is also what stops a push
// landing mid-read from splitting the result across two trees.
package spec_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs/workspacetest"
	"github.com/wso2/aep/aep-api/internal/platform/gittest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

const bundleBase = apiBase + "/bundle"

// ---- wire shapes (decoded structurally, so the contract's field names are
// pinned here rather than assumed from the Go service types) ----

type wireFileContent struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA     string `json:"sha"`
}

type wireFileBundle struct {
	CommitSHA string            `json:"commitSha"`
	Files     []wireFileContent `json:"files"`
}

func (r *filesRig) bundle(t *testing.T, query string) wireFileBundle {
	t.Helper()
	rec := r.get(bundleBase + query)
	if rec.Code != http.StatusOK {
		t.Fatalf("bundle%s: code %d (%s)", query, rec.Code, rec.Body.String())
	}
	var out wireFileBundle
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	return out
}

func (b wireFileBundle) byPath() map[string]wireFileContent {
	out := make(map[string]wireFileContent, len(b.Files))
	for _, f := range b.Files {
		out[f.Path] = f
	}
	return out
}

// ---- tests ----

// The bundle must be substitutable for the fan-out it replaces: same content,
// same blob shas (they become the caller's baseSha preconditions), same commit
// the mirror is on. Reaching this handler at all also proves the routing: the
// literal `bundle` segment sits underneath read-file's trailing wildcard, and if
// ServeMux preferred the wildcard this would arrive as a read of a file named
// "bundle" and fail the path gate instead.
func TestBundle_AgreesWithThePerFileReadsItReplaces(t *testing.T) {
	r := newFilesRig(t, map[string]string{
		"specs/requirements/prd.md":    "req body",
		"specs/design/domain-model.md": "design body",
		"specs/design/design.cell":     "cell body",
	})

	got := r.bundle(t, "?prefix=specs/")

	if want := r.mirrorRevParse(t, "refs/heads/main"); got.CommitSHA != want {
		t.Errorf("commitSha = %s, want the branch tip %s", got.CommitSHA, want)
	}
	if len(got.Files) != 3 {
		t.Fatalf("bundle has %d files, want 3: %+v", len(got.Files), got.Files)
	}
	// Sorted by path, so a caller can diff two bundles without normalising.
	for i := 1; i < len(got.Files); i++ {
		if got.Files[i-1].Path >= got.Files[i].Path {
			t.Errorf("files are not sorted by path: %+v", got.Files)
			break
		}
	}
	for path, want := range map[string]string{
		"specs/requirements/prd.md":    "req body",
		"specs/design/domain-model.md": "design body",
		"specs/design/design.cell":     "cell body",
	} {
		f, ok := got.byPath()[path]
		if !ok {
			t.Errorf("%s missing from the bundle", path)
			continue
		}
		if f.Content != want {
			t.Errorf("%s content = %q, want %q", path, f.Content, want)
		}
		if sha := r.readSHA(t, path); f.SHA != sha {
			t.Errorf("%s sha = %s, want the per-file read's %s", path, f.SHA, sha)
		}
	}
}

// prefix narrows; the read gate decides. A tree holds application code as well
// as specs, so a path the gate refuses is OMITTED rather than turned into an
// error — otherwise a bundle of any real repo could never succeed. The gate is
// the same one read-file applies, so this exposes nothing a caller could not
// already read one file at a time.
func TestBundle_OmitsWhatTheReadGateRefuses(t *testing.T) {
	r := newFilesRig(t, map[string]string{
		"specs/requirements/prd.md": "req",
		"README.md":                 "readme",
		"src/main.go":               "package main",
		".github/workflows/ci.yml":  "on: push",
	})

	// Empty prefix asks for everything; only the gate stands between the caller
	// and the rest of the tree.
	got := r.bundle(t, "")

	if len(got.Files) != 1 || got.Files[0].Path != "specs/requirements/prd.md" {
		t.Fatalf("bundle leaked past the read gate: %+v", got.Files)
	}
	// And the per-file read agrees about each refusal, so the two surfaces
	// cannot drift into disagreeing about what is readable.
	for _, path := range []string{"README.md", "src/main.go", ".github/workflows/ci.yml"} {
		if rec := r.get(apiBase + "/" + path); rec.Code == http.StatusOK {
			t.Errorf("read-file allows %s but the bundle omits it — the gates disagree", path)
		}
	}
}

func TestBundle_PrefixNarrowsWithinTheGate(t *testing.T) {
	r := newFilesRig(t, map[string]string{
		"specs/requirements/prd.md":    "req",
		"specs/design/domain-model.md": "des",
	})
	got := r.bundle(t, "?prefix=specs/design/")
	if len(got.Files) != 1 || got.Files[0].Path != "specs/design/domain-model.md" {
		t.Fatalf("prefix filter wrong: %+v", got.Files)
	}
}

// `ref` pins the bundle to one commit, exactly like read-file's. Without it the
// only readable answer is "whatever the tip says now", which is useless for
// anything reconstructing a past state.
func TestBundle_ReadsAtAPinnedCommit(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "v1"})
	before := r.bundle(t, "?prefix=specs/")

	baseSHA := r.readSHA(t, "specs/requirements/prd.md")
	if rec := r.apply(mustJSON(t, map[string]any{
		"writes": []map[string]string{
			{"path": "specs/requirements/prd.md", "content": "v2", "baseSha": baseSHA},
		},
	})); rec.Code != http.StatusOK {
		t.Fatalf("apply: code %d (%s)", rec.Code, rec.Body.String())
	}

	if head := r.bundle(t, "?prefix=specs/"); head.Files[0].Content != "v2" {
		t.Fatalf("unpinned bundle = %q, want the new tip content %q", head.Files[0].Content, "v2")
	}
	pinned := r.bundle(t, "?prefix=specs/&ref="+before.CommitSHA)
	if pinned.CommitSHA != before.CommitSHA {
		t.Errorf("pinned commitSha = %s, want %s", pinned.CommitSHA, before.CommitSHA)
	}
	if pinned.Files[0].Content != "v1" {
		t.Errorf("pinned content = %q, want %q", pinned.Files[0].Content, "v1")
	}
}

// `ref` takes an object name and nothing else — the same gate read-file applies.
// A revision expression would turn a prefix read into a browser over the repo's
// whole history.
func TestBundle_RejectsARevisionExpressionAsRef(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "v1"})
	for _, ref := range []string{"main", "HEAD~1", "refs/heads/main"} {
		if rec := r.get(bundleBase + "?ref=" + ref); rec.Code != http.StatusBadRequest {
			t.Errorf("ref=%s: code %d, want 400", ref, rec.Code)
		}
	}
}

// ---- the addressing property ----

// recordingWorkspace wraps the real engine and records how each read ADDRESSED
// the repo. gitfs fetches origin for a symbolic address and serves an object
// name from local objects, so the recorded values are what decide whether a
// bundle costs one network round trip or N — and whether every file in it came
// from the same tree.
type recordingWorkspace struct {
	sourcecontrol.Workspace
	mu   sync.Mutex
	head []string // `at` of every Head call
	list []string // `at` of every List call
	bulk []string // `at` of every ReadBundle call
	one  []string // `at` of every ReadFile call — the fan-out's per-file read
}

func (w *recordingWorkspace) record(dst *[]string, at string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	*dst = append(*dst, at)
}

func (w *recordingWorkspace) Head(ctx context.Context, ref sourcecontrol.RepoRef, at string) (string, error) {
	w.record(&w.head, at)
	return w.Workspace.Head(ctx, ref, at)
}

func (w *recordingWorkspace) List(ctx context.Context, ref sourcecontrol.RepoRef, at string) ([]sourcecontrol.Entry, string, error) {
	w.record(&w.list, at)
	return w.Workspace.List(ctx, ref, at)
}

func (w *recordingWorkspace) ReadBundle(ctx context.Context, ref sourcecontrol.RepoRef, at string, keep func(string) bool) (map[string]string, string, error) {
	w.record(&w.bulk, at)
	return w.Workspace.ReadBundle(ctx, ref, at, keep)
}

func (w *recordingWorkspace) ReadFile(ctx context.Context, ref sourcecontrol.RepoRef, at, path string) ([]byte, string, error) {
	w.record(&w.one, at)
	return w.Workspace.ReadFile(ctx, ref, at, path)
}

// addresses returns the `at` of every recorded read, in no particular order —
// what each read addressed is the only thing these assertions care about.
func (w *recordingWorkspace) addresses() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, 0, len(w.head)+len(w.list)+len(w.bulk)+len(w.one))
	for _, group := range [][]string{w.head, w.list, w.bulk, w.one} {
		out = append(out, group...)
	}
	return out
}

// refAddressed counts the reads that named a REF rather than an object. Each one
// costs an origin round trip under the mirror's exclusive lock, and each one
// resolves the tip for itself — so this number is both the network cost and the
// number of independent answers a caller is being handed.
func (w *recordingWorkspace) refAddressed() int {
	n := 0
	for _, at := range w.addresses() {
		if at == "" {
			n++
		}
	}
	return n
}

func (w *recordingWorkspace) snapshot() (head, list, bulk []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.head...), append([]string(nil), w.list...), append([]string(nil), w.bulk...)
}

// newRecordingRig is newFilesRig with the Workspace wrapped, so the assertions
// below run against the same real engine and origin as every other test here.
func newRecordingRig(t *testing.T, seed map[string]string) (*componenttest.Harness, *recordingWorkspace) {
	t.Helper()
	remote := gittest.NewRemote(t, gittest.WithSeed(seed, "seed"))
	rec := &sourcecontrol.GitRepository{
		OrgID:         filesTestOrg,
		ProjectID:     filesTestProj,
		RepoURL:       remote.URL(),
		RepoSlug:      testSlug,
		DefaultBranch: "main",
		Status:        "ready",
	}
	ws := &recordingWorkspace{Workspace: workspacetest.NewEngine(t)}
	gitOps := sourcecontrol.NewGitOpsService(filesStubResolver{}, ws)
	svc := spec.NewFilesService(filesStubRepoResolver{rec: rec}, gitOps)
	h := componenttest.New(t, componenttest.Options{Deps: edge.Deps{
		Spec: mustSpecHandlers(t, spec.Deps{Files: svc, FilesActivity: &captureSpecUpdated{}}),
	}})
	return h, ws
}

func TestBundle_ResolvesTheTipOnceThenAddressesObjects(t *testing.T) {
	h, ws := newRecordingRig(t, map[string]string{
		"specs/requirements/prd.md":    "req",
		"specs/design/domain-model.md": "des",
		"specs/design/design.cell":     "cell",
		"README.md":                    "readme",
	})
	if rec := h.AsOrg(filesTestOrg).Get(bundleBase + "?prefix=specs/"); rec.Code != http.StatusOK {
		t.Fatalf("bundle: code %d (%s)", rec.Code, rec.Body.String())
	}

	head, list, bulk := ws.snapshot()
	if len(head) != 1 || head[0] != "" {
		t.Fatalf("Head calls = %q, want exactly one at the branch tip — this is the ONLY read allowed to touch origin", head)
	}
	resolved := ws.mustResolved(t)
	// Everything after the resolve addresses that object name. An "" here would
	// mean another origin round trip AND a second, independent tip resolution.
	if len(list) != 1 || list[0] != resolved {
		t.Errorf("List calls = %q, want exactly one at %s", list, resolved)
	}
	if len(bulk) != 1 || bulk[0] != resolved {
		t.Errorf("ReadBundle calls = %q, want exactly one at %s", bulk, resolved)
	}
}

// mustResolved returns the commit the recorded Head resolved to, read back from
// the same wrapped engine.
func (w *recordingWorkspace) mustResolved(t *testing.T) string {
	t.Helper()
	_, list, _ := w.snapshot()
	if len(list) == 0 {
		t.Fatal("no List call recorded — nothing to resolve against")
	}
	return list[0]
}

// The fan-out this replaces resolved the tip once PER FILE. Reading three files
// one at a time is three ref-addressed reads; the bundle is one. Asserted as a
// comparison so the saving is stated in the same units the old path cost, and so
// a regression to per-file reads inside Bundle fails here.
func TestBundle_CostsOneRefAddressedReadWhereTheFanOutCostsOnePerFile(t *testing.T) {
	seed := map[string]string{
		"specs/requirements/prd.md":    "req",
		"specs/design/domain-model.md": "des",
		"specs/design/design.cell":     "cell",
	}

	fanOut, fanWS := newRecordingRig(t, seed)
	if rec := fanOut.AsOrg(filesTestOrg).Get(apiBase); rec.Code != http.StatusOK {
		t.Fatalf("list: code %d", rec.Code)
	}
	for path := range seed {
		if rec := fanOut.AsOrg(filesTestOrg).Get(apiBase + "/" + path); rec.Code != http.StatusOK {
			t.Fatalf("read %s: code %d", path, rec.Code)
		}
	}
	// list-files plus one read per file, each resolving the tip for itself.
	if got, want := fanWS.refAddressed(), 1+len(seed); got != want {
		t.Fatalf("fan-out made %d ref-addressed reads, expected %d — the baseline this test compares against has moved", got, want)
	}

	bundled, bundleWS := newRecordingRig(t, seed)
	if rec := bundled.AsOrg(filesTestOrg).Get(bundleBase + "?prefix=specs/"); rec.Code != http.StatusOK {
		t.Fatalf("bundle: code %d", rec.Code)
	}
	if got := bundleWS.refAddressed(); got != 1 {
		t.Errorf("bundle made %d ref-addressed reads, want 1 (the fan-out made %d for the same files)",
			got, fanWS.refAddressed())
	}
}
