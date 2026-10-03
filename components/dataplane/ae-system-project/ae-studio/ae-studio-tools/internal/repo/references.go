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

package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
)

// Reference documents (console #383 / ADR-0017; moved from aep-api's
// gitfs/references.go, ticket 09 §2) are the files a user attaches on the
// create view. They are transient turn inputs, not spec artifacts: nothing
// commits them and they never reach GitHub.
//
// A snapshot is `git archive` of a commit, so an uncommitted file has no way
// in. The store therefore lives on studio-data at
// references/<owner>/<repo>/ (never mounted into the agent), and
// OverlayReferences lays it over a project snapshot at ReferenceOverlayDir.
// The agent reads the same path it would have read had the documents been
// committed.

// OwnerRepo names a GitHub repository: the reference store's key, as aep-api
// names the repository on upload and the project lookup resolves it.
type OwnerRepo struct{ Owner, Repo string }

// ReferenceOverlayDir is where a stored reference lands inside a snapshot:
// the path the feature's v1 committed to, so the agent, the `start` skill and
// the turn's reference list all address one location.
const ReferenceOverlayDir = "specs/requirements/references"

// MaxReferenceBytes is the per-document cap, checked on the real bytes (the
// console screens for it too; this is the authority). MaxReferenceCount caps
// the set so one project cannot fill the studio's budget by itself.
const (
	MaxReferenceBytes = 5 << 20
	MaxReferenceCount = 10
)

// ErrReferenceRejected is a caller error: a bad name, an oversized document,
// or too many of them. Handlers map it to 400 reference_rejected.
var ErrReferenceRejected = errors.New("repo: reference document rejected")

// referenceNamePattern is the allowed shape of a stored document's name,
// stricter than a filesystem demands: the name comes off a multipart part and
// is joined onto both the store path and a path inside the snapshot.
var referenceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)

// referenceExtensions are the types the models can read. The binary group
// (PDF and the four image types the Messages API accepts) is read natively as
// file parts; the text group is read as workspace files. Office formats are
// deliberately absent: the models do not read them natively.
var referenceExtensions = map[string]bool{
	".pdf": true, ".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".md": true, ".txt": true, ".csv": true, ".tsv": true, ".json": true,
	".yaml": true, ".yml": true, ".xml": true, ".html": true, ".rst": true,
}

// ReferenceDoc is one document to store: a bare file name and its bytes.
type ReferenceDoc struct {
	Name    string
	Content []byte
}

// validateReferenceName rejects traversal, hidden files and anything outside
// the readable extensions (`..` cannot pass the pattern: it starts with a dot).
func validateReferenceName(name string) error {
	if !referenceNamePattern.MatchString(name) {
		return fmt.Errorf("%w: invalid name %q", ErrReferenceRejected, name)
	}
	ext := strings.ToLower(filepath.Ext(name))
	if !referenceExtensions[ext] {
		return fmt.Errorf("%w: unsupported type %q", ErrReferenceRejected, ext)
	}
	return nil
}

// PutReferences replaces the repository's whole stored set. Replace, not
// merge: the console uploads once, right after create, and a merge would keep
// a half-failed retry's documents with no surface to notice them. The set is
// staged in tmp/ and renamed into place, so an overlay never reads a
// half-written set. A new set passes admission (DiskAdmissionRefusePct).
func (e *Engine) PutReferences(_ context.Context, r OwnerRepo, docs []ReferenceDoc) (err error) {
	defer func() { err = e.mapDiskErr(err) }()
	dest, err := ReferenceStoreDir(e.root, r)
	if err != nil {
		return err
	}
	if err := validateReferenceSet(docs); err != nil {
		return err
	}
	if err := e.admit(); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(TmpDir(e.root), "references-*")
	if err != nil {
		return fmt.Errorf("repo: reference staging: %w", err)
	}
	defer os.RemoveAll(staging) // no-op after a successful rename
	for _, d := range docs {
		if err := os.WriteFile(filepath.Join(staging, d.Name), d.Content, 0o644); err != nil {
			return fmt.Errorf("repo: stage reference %q: %w", d.Name, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("repo: create reference store parent: %w", err)
	}
	// os.Rename cannot replace a non-empty dir, so the old set goes to trash
	// first. The gap between the two renames is why an overlay is
	// best-effort.
	if dirExists(dest) {
		if err := os.Rename(dest, trashDest(e.root)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("repo: retire previous references: %w", err)
		}
	}
	if err := os.Rename(staging, dest); err != nil {
		return fmt.Errorf("repo: publish references: %w", err)
	}
	e.AddUsage(DirBytes(dest))
	return nil
}

// validateReferenceSet checks the count, every name, every size and that no
// name appears twice (a later part would silently replace an earlier one).
func validateReferenceSet(docs []ReferenceDoc) error {
	if len(docs) > MaxReferenceCount {
		return fmt.Errorf("%w: at most %d documents (got %d)", ErrReferenceRejected, MaxReferenceCount, len(docs))
	}
	seen := make(map[string]bool, len(docs))
	for _, d := range docs {
		if err := validateReferenceName(d.Name); err != nil {
			return err
		}
		if len(d.Content) > MaxReferenceBytes {
			return fmt.Errorf("%w: %q is larger than %d bytes", ErrReferenceRejected, d.Name, MaxReferenceBytes)
		}
		if seen[d.Name] {
			return fmt.Errorf("%w: duplicate name %q", ErrReferenceRejected, d.Name)
		}
		seen[d.Name] = true
	}
	return nil
}

// ListReferences returns the stored document names, sorted. A repository
// with no store (the ordinary case) returns nil, not an error. Only regular
// files whose names PutReferences could have written are listed: a symlink
// is neither a dir nor a regular file and would be followed by the copy, and
// the store is a dir on a shared volume.
func (e *Engine) ListReferences(_ context.Context, r OwnerRepo) ([]string, error) {
	dir, err := ReferenceStoreDir(e.root, r)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, e.mapDiskErr(fmt.Errorf("repo: list references: %w", err))
	}
	var names []string
	for _, entry := range entries {
		if !entry.Type().IsRegular() || validateReferenceName(entry.Name()) != nil {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

// OverlayReferences brings the snapshot of ref's commit sha up to date with
// the repository's stored set at ReferenceOverlayDir. The snapshot must
// already exist (EnsureSnapshot); a missing one is left alone.
//
// It runs on every project lookup, not only when a snapshot is first made: a
// snapshot is immutable for its sha, but references are a second input keyed
// to that same sha and change independently of it. The create flow commits
// the descriptor (moving the head), and anything that snapshots that sha
// before the upload lands publishes it without references; the `/start` turn
// then reuses it. A re-upload has the same shape with stale content. (A
// re-upload at the same head still leaves the previous turn's overlay until
// the next lookup; best-effort, 09 §1.)
//
// A replacement that drops a name retires the old file, so a lingering
// document never reaches the model. Removal is scoped by a manifest
// (`.aep-references.json`, dot-led so the agent's snapshot walk skips it) of
// exactly the names this overlay wrote; a committed file of the same name
// never enters it, and one an overlay had masked is restored from git rather
// than deleted.
//
// Each file is written temp-then-rename in the destination dir, so a
// concurrent reader sees the old bytes or the new, never a torn file.
//
// Best-effort by design: a failure must not fail the lookup, which every
// turn on the project needs, including those that attached nothing. A lost
// overlay is logged (references.overlay_failed), not returned.
func (e *Engine) OverlayReferences(ctx context.Context, ref RepoRef, store OwnerRepo, sha string) {
	snap, err := SnapshotDir(e.root, ref.Project, sha)
	if err != nil || !dirExists(snap) {
		return
	}
	warn := func(step string, err error) {
		slog.WarnContext(ctx, "references.overlay_failed", "project", ref.Project, "step", step, "error", err)
	}
	names, err := e.ListReferences(ctx, store)
	if err != nil {
		warn("list", err)
		return
	}
	src, err := ReferenceStoreDir(e.root, store)
	if err != nil {
		warn("store", err)
		return
	}
	dst := filepath.Join(snap, filepath.FromSlash(ReferenceOverlayDir))
	prev := readOverlayManifest(dst)
	// Nothing stored and nothing written before: leave the snapshot exactly
	// as git produced it, with no dir and no manifest.
	if len(names) == 0 && len(prev) == 0 {
		return
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		warn("mkdir", err)
		return
	}
	current := make(map[string]bool, len(names))
	for _, n := range names {
		current[n] = true
	}
	for _, old := range prev {
		if current[old] {
			continue
		}
		if err := e.retireOverlayFile(ctx, ref, sha, dst, old); err != nil {
			warn("retire", err)
		}
	}
	for _, name := range names {
		from, to := filepath.Join(src, name), filepath.Join(dst, name)
		if stale, err := differs(from, to); err == nil && !stale {
			continue
		}
		if err := copyFileAtomic(from, to); err != nil {
			warn("copy", err)
		}
	}
	if err := writeOverlayManifest(dst, names); err != nil {
		warn("manifest", err)
	}
}

// overlayManifestName records which names the overlay wrote, so a later one
// retires exactly those and never a committed file.
const overlayManifestName = ".aep-references.json"

type overlayManifest struct {
	Written []string `json:"written"`
}

func readOverlayManifest(dir string) []string {
	raw, err := os.ReadFile(filepath.Join(dir, overlayManifestName))
	if err != nil {
		return nil // absent or unreadable: nothing is known to be ours
	}
	var m overlayManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m.Written
}

func writeOverlayManifest(dir string, names []string) error {
	raw, err := json.Marshal(overlayManifest{Written: names})
	if err != nil {
		return err
	}
	return writeFileAtomic(dir, ".manifest-*", filepath.Join(dir, overlayManifestName), raw)
}

// retireOverlayFile drops a name the overlay wrote and the store no longer
// lists. If the commit's tree has the same path, the overlay had masked
// committed content: the git blob is restored instead.
func (e *Engine) retireOverlayFile(ctx context.Context, ref RepoRef, sha, dst, name string) error {
	target := filepath.Join(dst, name)
	committed, found, err := e.committedReference(ctx, ref, sha, name)
	if err != nil {
		return err
	}
	if !found {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return writeFileAtomic(dst, ".restore-*", target, committed)
}

// committedReference reads ReferenceOverlayDir/name at sha from the mirror;
// found is false when the tree has no such file.
func (e *Engine) committedReference(ctx context.Context, ref RepoRef, sha, name string) ([]byte, bool, error) {
	p, err := e.pathsFor(ref)
	if err != nil {
		return nil, false, err
	}
	content, _, err := e.readBlobLocked(ctx, p, sha, ReferenceOverlayDir+"/"+name)
	if errors.Is(err, ErrPathNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return content, true, nil
}

// readBlobLocked is readBlobAt under the mirror's shared flock.
func (e *Engine) readBlobLocked(ctx context.Context, p repoPaths, commit, path string) ([]byte, string, error) {
	release, err := e.locks.RLock(ctx, p.lockPath)
	if err != nil {
		return nil, "", err
	}
	defer release()
	return e.readBlobAt(ctx, p, commit, path)
}

// differs reports whether dst needs rewriting: absent, another size, or
// older than src. Size and mtime rather than a hash: this runs on every
// lookup, and a needless rewrite is harmless.
func differs(src, dst string) (bool, error) {
	di, err := os.Stat(dst)
	if err != nil {
		return true, nil
	}
	si, err := os.Stat(src)
	if err != nil {
		return true, err
	}
	return si.Size() != di.Size() || di.ModTime().Before(si.ModTime()), nil
}

// copyFileAtomic copies one regular file into place, 0644 like a snapshot
// blob, staged beside dst and renamed onto it. The source is opened with
// O_NOFOLLOW so the regular-file check is on the file actually read.
func copyFileAtomic(src, dst string) error {
	in, err := os.OpenFile(src, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("repo: %q is not a regular file", src)
	}
	content, err := io.ReadAll(io.LimitReader(in, MaxReferenceBytes+1))
	if err != nil {
		return err
	}
	if len(content) > MaxReferenceBytes {
		return fmt.Errorf("repo: %q is larger than %d bytes", src, MaxReferenceBytes)
	}
	return writeFileAtomic(filepath.Dir(dst), ".reference-*", dst, content)
}

// writeFileAtomic writes content to a temp file named by pattern in dir,
// 0644, and renames it onto dst.
func writeFileAtomic(dir, pattern, dst string, content []byte) (err error) {
	staged, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(staged.Name())
		}
	}()
	if _, err = staged.Write(content); err != nil {
		_ = staged.Close()
		return err
	}
	if err = staged.Chmod(0o644); err != nil {
		_ = staged.Close()
		return err
	}
	if err = staged.Close(); err != nil {
		return err
	}
	return os.Rename(staged.Name(), dst)
}
