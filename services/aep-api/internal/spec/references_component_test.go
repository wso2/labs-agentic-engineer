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
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
)

// The workspace engine's reference store (#383 / console ADR-0017) that the
// old in-process turns read: bytes in the off-git store and — the property the
// design rests on — those bytes appearing inside a turn's snapshot even though
// nothing was ever committed. The upload itself goes to the org's AE Studio
// pod now (spec/files); these pin the store until the old turns go (Task
// 3.21).

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

// putReferences replaces the stored set through the engine, in name order.
func (r *filesRig) putReferences(t *testing.T, docs map[string][]byte) error {
	t.Helper()
	names := make([]string, 0, len(docs))
	for n := range docs {
		names = append(names, n)
	}
	slices.Sort(names)
	set := make([]gitfs.ReferenceDoc, 0, len(names))
	for _, n := range names {
		set = append(set, gitfs.ReferenceDoc{Name: n, Content: docs[n]})
	}
	return r.engine.PutReferences(t.Context(), r.workspaceRef(), set)
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

	if err := r.putReferences(t, map[string][]byte{"claim-form.pdf": pdf}); err != nil {
		t.Fatalf("upload: %v", err)
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
	if err := r.putReferences(t, map[string][]byte{"brief.md": []byte("# Brief")}); err != nil {
		t.Fatalf("upload: %v", err)
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

	if err := r.putReferences(t, map[string][]byte{"brief.md": []byte("old")}); err != nil {
		t.Fatalf("first upload: %v", err)
	}
	if err := r.engine.Ensure(t.Context(), ref, sha); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if err := r.putReferences(t, map[string][]byte{"brief.md": []byte("corrected and longer")}); err != nil {
		t.Fatalf("second upload: %v", err)
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

	if err := r.putReferences(t, map[string][]byte{"old.md": []byte("superseded")}); err != nil {
		t.Fatalf("first upload: %v", err)
	}
	if err := r.engine.Ensure(t.Context(), ref, sha); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	// A different NAME, so the old one is dropped rather than overwritten.
	if err := r.putReferences(t, map[string][]byte{"new.md": []byte("current")}); err != nil {
		t.Fatalf("second upload: %v", err)
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
	if err := r.putReferences(t, map[string][]byte{"brief.md": []byte("transient override")}); err != nil {
		t.Fatalf("mask upload: %v", err)
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
	if err := r.putReferences(t, map[string][]byte{"other.md": []byte("something else")}); err != nil {
		t.Fatalf("replacement upload: %v", err)
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
	if err := r.putReferences(t, map[string][]byte{"old.md": []byte("old")}); err != nil {
		t.Fatalf("first upload: %v", err)
	}
	if err := r.putReferences(t, map[string][]byte{"new.md": []byte("new")}); err != nil {
		t.Fatalf("second upload: %v", err)
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

	if err := r.putReferences(t, map[string][]byte{"spec.docx": []byte("PK\x03\x04")}); !errors.Is(err, gitfs.ErrReferenceRejected) {
		t.Fatalf("err = %v, want ErrReferenceRejected for .docx", err)
	}
	names, _ := r.engine.ListReferences(t.Context(), r.workspaceRef())
	if len(names) != 0 {
		t.Fatalf("stored %v on a rejected upload — nothing should have been written", names)
	}
}

// The per-document cap is checked on the REAL bytes: an oversized document is
// refused, never stored truncated.
func TestPutReferences_OversizedDocumentIs400_NotTruncated(t *testing.T) {
	r := newFilesRig(t, nil)
	huge := bytes.Repeat([]byte("A"), gitfs.MaxReferenceBytes+1)

	if err := r.putReferences(t, map[string][]byte{"big.pdf": huge}); !errors.Is(err, gitfs.ErrReferenceRejected) {
		t.Fatalf("err = %v, want ErrReferenceRejected for an oversized document", err)
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

	putErr := r.putReferences(t, map[string][]byte{"../../../etc/passwd.md": []byte("nope")})
	// Either rejected outright or reduced to a bare name — never written
	// outside the store.
	names, _ := r.engine.ListReferences(t.Context(), r.workspaceRef())
	for _, n := range names {
		if strings.Contains(n, "/") || strings.Contains(n, "..") {
			t.Fatalf("stored an escaping name %q (upload err %v)", n, putErr)
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
