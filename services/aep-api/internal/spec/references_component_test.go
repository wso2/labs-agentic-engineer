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

package spec_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
)

// The reference-document upload (#383 / console ADR-0017), end to end through
// the real handler chain: multipart in, bytes in the off-git store, and — the
// property the whole design rests on — those bytes appearing inside a turn's
// snapshot even though nothing was ever committed.

func referenceUpload(t *testing.T, docs map[string][]byte) (string, []byte) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	// Sorted for determinism; a map's range order would make a failure
	// unreproducible.
	for _, name := range sortedKeys(docs) {
		part, err := w.CreateFormFile("files", name)
		if err != nil {
			t.Fatalf("create part %q: %v", name, err)
		}
		if _, err := part.Write(docs[name]); err != nil {
			t.Fatalf("write part %q: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return w.FormDataContentType(), buf.Bytes()
}

func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// workspaceRefForRig is the mount ref the rig's repo row resolves to — the same
// derivation newFilesRig's GitRepository feeds the service. CloneURL and
// DefaultBranch matter: Ensure clones the mirror on first use, and a ref
// missing them fails with a bare "repository ” does not exist".
func (r *filesRig) workspaceRef() gitfs.RepoRef {
	return gitfs.RepoRef{
		OrgID:         filesTestOrg,
		ProjectID:     filesTestProj,
		RepoSlug:      testSlug,
		CloneURL:      r.remote.URL(),
		DefaultBranch: "main",
	}
}

func (r *filesRig) putReferences(t *testing.T, docs map[string][]byte) *httptest.ResponseRecorder {
	t.Helper()
	ct, body := referenceUpload(t, docs)
	return r.h.AsOrg(filesTestOrg).PostRaw(
		"/api/v1/projects/"+filesTestProj+"/references", ct, body)
}

// The load-bearing test of the whole feature. The bytes are uploaded, NOTHING
// is committed, and yet a turn's snapshot holds the document at the path agents
// read — because Ensure overlays the store after `git archive` extracts the
// tree. Delete the overlay and this is the test that fails.
func TestPutReferences_StoredOffGitAndOverlaidIntoTheSnapshot(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "x"})
	headBefore := r.remote.HeadSHA(t)
	// Bytes that are not valid UTF-8: a text-only channel would visibly mangle
	// them, and the store must not.
	pdf := []byte("%PDF-1.4\n\xff\xfe\x00 binary")

	if rec := r.putReferences(t, map[string][]byte{"claim-form.pdf": pdf}); rec.Code != http.StatusNoContent {
		t.Fatalf("upload code %d, want 204: %s", rec.Code, rec.Body.String())
	}

	// Nothing committed — the repo is untouched.
	if r.remote.HeadSHA(t) != headBefore {
		t.Fatal("HEAD advanced — reference documents must never be committed")
	}

	ref := r.workspaceRef()
	names, err := r.engine.ListReferences(t.Context(), ref)
	if err != nil {
		t.Fatalf("list references: %v", err)
	}
	if len(names) != 1 || names[0] != "claim-form.pdf" {
		t.Fatalf("stored names = %v, want [claim-form.pdf]", names)
	}

	// ...and yet the turn's workspace holds it, byte-exact.
	sha := r.remote.HeadSHA(t)
	if err := r.engine.Ensure(t.Context(), ref, sha); err != nil {
		t.Fatalf("ensure snapshot: %v", err)
	}
	dir, err := gitfs.SnapshotDir(r.engine.Root(), ref, sha)
	if err != nil {
		t.Fatalf("snapshot dir: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(gitfs.ReferenceOverlayDir), "claim-form.pdf"))
	if err != nil {
		t.Fatalf("reference not overlaid into the snapshot: %v", err)
	}
	if !bytes.Equal(got, pdf) {
		t.Fatalf("overlaid %d bytes, want the %d uploaded byte-identically", len(got), len(pdf))
	}
}

// The regression this exists for. Overlaying only at first materialization
// loses the create flow outright: `POST /projects` commits the descriptor and
// moves HEAD, anything that materializes that sha before the upload lands (a
// status poll, the spec view's file list) publishes a snapshot with no
// references, and the turn then reuses it — the agent gets a steer naming
// documents that are not in its workspace.
func TestEnsure_OverlaysReferencesUploadedAfterTheSnapshotExists(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "x"})
	ref := r.workspaceRef()
	sha := r.remote.HeadSHA(t)

	// Materialize FIRST, with nothing stored — the create flow's real order.
	if err := r.engine.Ensure(t.Context(), ref, sha); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	dir, err := gitfs.SnapshotDir(r.engine.Root(), ref, sha)
	if err != nil {
		t.Fatalf("snapshot dir: %v", err)
	}
	refPath := filepath.Join(dir, filepath.FromSlash(gitfs.ReferenceOverlayDir), "brief.md")
	if _, err := os.Stat(refPath); !os.IsNotExist(err) {
		t.Fatalf("nothing was uploaded yet, but the snapshot already holds a reference (stat err = %v)", err)
	}

	// Now upload, and run the turn's Ensure against the same sha.
	if rec := r.putReferences(t, map[string][]byte{"brief.md": []byte("# Brief")}); rec.Code != http.StatusNoContent {
		t.Fatalf("upload code %d: %s", rec.Code, rec.Body.String())
	}
	if err := r.engine.Ensure(t.Context(), ref, sha); err != nil {
		t.Fatalf("second ensure: %v", err)
	}

	got, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatalf("reference missing from an already-materialized snapshot: %v", err)
	}
	if string(got) != "# Brief" {
		t.Fatalf("overlaid %q, want the uploaded bytes", got)
	}
}

// The Retry-upload path: a re-upload must not leave the turn reading the
// superseded bytes out of a snapshot that already exists.
func TestEnsure_RefreshesAReplacedReferenceInAnExistingSnapshot(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "x"})
	ref := r.workspaceRef()
	sha := r.remote.HeadSHA(t)

	if rec := r.putReferences(t, map[string][]byte{"brief.md": []byte("old")}); rec.Code != http.StatusNoContent {
		t.Fatalf("first upload code %d", rec.Code)
	}
	if err := r.engine.Ensure(t.Context(), ref, sha); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if rec := r.putReferences(t, map[string][]byte{"brief.md": []byte("corrected and longer")}); rec.Code != http.StatusNoContent {
		t.Fatalf("second upload code %d", rec.Code)
	}
	if err := r.engine.Ensure(t.Context(), ref, sha); err != nil {
		t.Fatalf("second ensure: %v", err)
	}

	dir, err := gitfs.SnapshotDir(r.engine.Root(), ref, sha)
	if err != nil {
		t.Fatalf("snapshot dir: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(gitfs.ReferenceOverlayDir), "brief.md"))
	if err != nil {
		t.Fatalf("read reference: %v", err)
	}
	if string(got) != "corrected and longer" {
		t.Fatalf("snapshot still holds %q — the re-upload did not reach the turn", got)
	}
}

// A replacement upload that DROPS a name must retire the old file. This is not
// cosmetic: keepInTurnSnapshot admits every text file under the references
// directory into the turn's file map, so a lingering document reaches the model
// whether or not the turn's reference list still names it.
func TestEnsure_RetiresAReferenceDroppedByAReplacementUpload(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "x"})
	ref := r.workspaceRef()
	sha := r.remote.HeadSHA(t)

	if rec := r.putReferences(t, map[string][]byte{"old.md": []byte("superseded")}); rec.Code != http.StatusNoContent {
		t.Fatalf("first upload code %d", rec.Code)
	}
	if err := r.engine.Ensure(t.Context(), ref, sha); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	// A different NAME, so the old one is dropped rather than overwritten.
	if rec := r.putReferences(t, map[string][]byte{"new.md": []byte("current")}); rec.Code != http.StatusNoContent {
		t.Fatalf("second upload code %d", rec.Code)
	}
	if err := r.engine.Ensure(t.Context(), ref, sha); err != nil {
		t.Fatalf("second ensure: %v", err)
	}

	dir, err := gitfs.SnapshotDir(r.engine.Root(), ref, sha)
	if err != nil {
		t.Fatalf("snapshot dir: %v", err)
	}
	refDir := filepath.Join(dir, filepath.FromSlash(gitfs.ReferenceOverlayDir))
	if _, err := os.Stat(filepath.Join(refDir, "old.md")); !os.IsNotExist(err) {
		t.Errorf("dropped reference still in the snapshot (stat err = %v) — it would reach the model", err)
	}
	if got, err := os.ReadFile(filepath.Join(refDir, "new.md")); err != nil || string(got) != "current" {
		t.Errorf("new.md = %q, err %v; want the replacement content", got, err)
	}
}

// Retirement must never take a COMMITTED file with it. When an overlay masked a
// v1 project's committed reference of the same name, dropping the transient one
// restores the git blob rather than deleting the path.
func TestEnsure_RestoresACommittedReferenceTheOverlayHadMasked(t *testing.T) {
	r := newFilesRig(t, map[string]string{
		"specs/requirements/prd.md":              "x",
		"specs/requirements/references/brief.md": "# Committed under v1",
	})
	ref := r.workspaceRef()
	sha := r.remote.HeadSHA(t)

	// Mask the committed file with a transient one of the same name.
	if rec := r.putReferences(t, map[string][]byte{"brief.md": []byte("transient override")}); rec.Code != http.StatusNoContent {
		t.Fatalf("mask upload code %d", rec.Code)
	}
	if err := r.engine.Ensure(t.Context(), ref, sha); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	dir, err := gitfs.SnapshotDir(r.engine.Root(), ref, sha)
	if err != nil {
		t.Fatalf("snapshot dir: %v", err)
	}
	masked := filepath.Join(dir, filepath.FromSlash(gitfs.ReferenceOverlayDir), "brief.md")
	if got, _ := os.ReadFile(masked); string(got) != "transient override" {
		t.Fatalf("overlay did not mask the committed file: %q", got)
	}

	// Now drop it from the store — the committed content must come back.
	if rec := r.putReferences(t, map[string][]byte{"other.md": []byte("something else")}); rec.Code != http.StatusNoContent {
		t.Fatalf("replacement upload code %d", rec.Code)
	}
	if err := r.engine.Ensure(t.Context(), ref, sha); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	got, err := os.ReadFile(masked)
	if err != nil {
		t.Fatalf("committed reference was DELETED rather than restored: %v", err)
	}
	if string(got) != "# Committed under v1" {
		t.Errorf("restored content = %q, want the committed blob", got)
	}
}

// A project created under the feature's v1 has real COMMITTED references, which
// arrive through `git archive` like any other git content. The reconcile adds
// and updates but never removes, so an empty store must not delete them.
func TestEnsure_DoesNotDeleteCommittedV1References(t *testing.T) {
	r := newFilesRig(t, map[string]string{
		"specs/requirements/prd.md":                  "x",
		"specs/requirements/references/legacy-v1.md": "# Committed under v1",
	})
	ref := r.workspaceRef()
	sha := r.remote.HeadSHA(t)

	// Nothing in the store — the only references are the committed ones.
	if err := r.engine.Ensure(t.Context(), ref, sha); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	dir, err := gitfs.SnapshotDir(r.engine.Root(), ref, sha)
	if err != nil {
		t.Fatalf("snapshot dir: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(gitfs.ReferenceOverlayDir), "legacy-v1.md"))
	if err != nil {
		t.Fatalf("a committed v1 reference was removed from the snapshot: %v", err)
	}
	if string(got) != "# Committed under v1" {
		t.Fatalf("committed reference = %q, want it untouched", got)
	}
}

// A second upload REPLACES the set rather than merging into it, so a retry
// after a partial failure converges instead of accumulating documents the user
// can no longer see (the console lists references nowhere after create).
func TestPutReferences_ReplacesThePreviousSet(t *testing.T) {
	r := newFilesRig(t, nil)
	if rec := r.putReferences(t, map[string][]byte{"old.md": []byte("old")}); rec.Code != http.StatusNoContent {
		t.Fatalf("first upload code %d: %s", rec.Code, rec.Body.String())
	}
	if rec := r.putReferences(t, map[string][]byte{"new.md": []byte("new")}); rec.Code != http.StatusNoContent {
		t.Fatalf("second upload code %d: %s", rec.Code, rec.Body.String())
	}

	names, err := r.engine.ListReferences(t.Context(), r.workspaceRef())
	if err != nil {
		t.Fatalf("list references: %v", err)
	}
	if len(names) != 1 || names[0] != "new.md" {
		t.Fatalf("stored names = %v, want only [new.md] — the upload replaces, not merges", names)
	}
}

// A type agents cannot read is refused at the edge of the store rather than
// stored as dead weight in every future snapshot.
func TestPutReferences_UnsupportedTypeIs400(t *testing.T) {
	r := newFilesRig(t, nil)

	rec := r.putReferences(t, map[string][]byte{"spec.docx": []byte("PK\x03\x04")})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code %d, want 400 for .docx: %s", rec.Code, firstBytes(rec.Body.String(), 200))
	}
	names, _ := r.engine.ListReferences(t.Context(), r.workspaceRef())
	if len(names) != 0 {
		t.Fatalf("stored %v on a rejected upload — nothing should have been written", names)
	}
}

// The per-document cap is checked on the REAL bytes. It reads one byte past the
// limit deliberately: a plain io.LimitReader ends at EOF, which is
// indistinguishable from a small file ending, and silently storing a truncated
// PDF is worse than refusing the upload.
func TestPutReferences_OversizedDocumentIs400_NotTruncated(t *testing.T) {
	r := newFilesRig(t, nil)
	huge := bytes.Repeat([]byte("A"), gitfs.MaxReferenceBytes+1)

	rec := r.putReferences(t, map[string][]byte{"big.pdf": huge})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code %d, want 400 for an oversized document: %s", rec.Code, firstBytes(rec.Body.String(), 200))
	}
	names, _ := r.engine.ListReferences(t.Context(), r.workspaceRef())
	if len(names) != 0 {
		t.Fatalf("stored %v — an oversized upload must not land, truncated or otherwise", names)
	}
}

// A crafted part name cannot escape the store. The name is attacker-controlled
// and is later joined onto both a store path and a path inside a snapshot.
func TestPutReferences_TraversalNameIsContained(t *testing.T) {
	r := newFilesRig(t, nil)

	rec := r.putReferences(t, map[string][]byte{"../../../etc/passwd.md": []byte("nope")})
	// Either rejected outright or reduced to a bare name — never written
	// outside the store.
	names, _ := r.engine.ListReferences(t.Context(), r.workspaceRef())
	for _, n := range names {
		if strings.Contains(n, "/") || strings.Contains(n, "..") {
			t.Fatalf("stored an escaping name %q (upload code %d)", n, rec.Code)
		}
	}
	dir, err := gitfs.ReferenceStoreDir(r.engine.Root(), r.workspaceRef())
	if err != nil {
		t.Fatalf("store dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(dir)), "etc")); err == nil {
		t.Fatal("a file escaped the reference store")
	}
}

// A project that attached nothing must produce a snapshot byte-identical to one
// from before the feature existed — no stray directory, no empty overlay.
func TestEnsure_NoReferences_LeavesTheSnapshotUntouched(t *testing.T) {
	r := newFilesRig(t, map[string]string{"specs/requirements/prd.md": "x"})
	ref := r.workspaceRef()
	sha := r.remote.HeadSHA(t)

	if err := r.engine.Ensure(t.Context(), ref, sha); err != nil {
		t.Fatalf("ensure snapshot: %v", err)
	}
	dir, err := gitfs.SnapshotDir(r.engine.Root(), ref, sha)
	if err != nil {
		t.Fatalf("snapshot dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(gitfs.ReferenceOverlayDir))); !os.IsNotExist(err) {
		t.Fatalf("an empty references dir was created in the snapshot (stat err = %v)", err)
	}
}
