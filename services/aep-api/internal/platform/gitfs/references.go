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

package gitfs

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

// Reference documents (console #383 / ADR-0017) are the files a user attaches
// on the create view. They are TRANSIENT TURN INPUTS, not spec artifacts:
// nothing commits them and they never reach GitHub.
//
// That leaves a delivery problem this file solves. A turn workspace is
// `git archive --format=tar <sha>` out of a bare mirror (snapshots.go) — there
// is no persistent working tree anywhere in the platform, so an untracked or
// .gitignore'd file has no way in. `.gitignore` prevents commits; it does not
// carry bytes. So the store lives beside the mirror, and Ensure OVERLAYS it
// into each freshly extracted snapshot at ReferenceOverlayDir. Agents read the
// same path they would have read if the documents had been committed, which is
// why nothing downstream of the snapshot had to change.

// ReferenceOverlayDir is where a stored reference lands inside a snapshot —
// the same path the feature's v1 committed to, so agents, the `start` skill,
// and the turn's reference list all keep addressing one location.
const ReferenceOverlayDir = "specs/requirements/references"

// MaxReferenceBytes is the per-document cap, checked on the real bytes (the
// console screens for it too; this is the authority). MaxReferenceCount caps
// the set so one project cannot fill an org's quota by itself.
const (
	MaxReferenceBytes = 5 << 20
	MaxReferenceCount = 10
)

// ErrReferenceRejected is a caller error — a bad name, an oversized document,
// or too many of them. Handlers map it to 400.
var ErrReferenceRejected = errors.New("gitfs: reference document rejected")

// referenceNamePattern is the allowed shape of a stored document's name. It is
// deliberately stricter than a filesystem would demand: the name is attacker-
// influenced (it comes off a multipart part), and it is later joined onto both
// the store path and a path inside the snapshot.
var referenceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)

// referenceExtensions are the types the models can actually read.
//
// Two groups, and the split matters downstream. The BINARY group is what the
// model reads NATIVELY as file parts — PDF plus the four image media types the
// Messages API accepts (image/png, image/jpeg, image/gif, image/webp); there is
// no fifth. The TEXT group is everything the model reads as plain text, so it
// is open-ended by nature — these are the formats a requirements brief or an
// API spec actually arrives in.
//
// Deliberately absent: .docx / .xlsx / .pptx. The models do not read Office
// formats natively — those need the code-execution Skills route — so accepting
// one here would store bytes no turn can use.
var referenceExtensions = map[string]bool{
	// Binary, read natively as file parts.
	".pdf": true, ".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	// Text, read as workspace files.
	".md": true, ".txt": true, ".csv": true, ".tsv": true, ".json": true,
	".yaml": true, ".yml": true, ".xml": true, ".html": true, ".rst": true,
}

// ReferenceDoc is one document to store: a bare file name and its raw bytes.
type ReferenceDoc struct {
	Name    string
	Content []byte
}

// validateReferenceName rejects traversal, hidden files, and anything outside
// the readable extensions. A name with no dot has no extension to check, and
// `..` cannot survive the pattern (it starts with a dot).
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

// PutReferences replaces the project's whole stored set. Replace, not merge:
// the console uploads once, immediately after create, and a partial merge would
// leave a half-failed retry's documents behind with no surface to notice them
// (the console shows references nowhere after create).
//
// The write is staged in tmp/ and renamed into place, so a reader — Ensure's
// overlay — never observes a half-written set.
func (e *Engine) PutReferences(ctx context.Context, ref RepoRef, docs []ReferenceDoc) (err error) {
	defer func() { err = e.mapDiskErr(err) }()
	dest, err := ReferenceStoreDir(e.root, ref)
	if err != nil {
		return err
	}
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
		// Two parts on one name would write the path twice and the later one
		// would silently replace the earlier document.
		if seen[d.Name] {
			return fmt.Errorf("%w: duplicate name %q", ErrReferenceRejected, d.Name)
		}
		seen[d.Name] = true
	}
	if pct := e.DiskUsagePct(); pct >= DiskAdmissionRefusePct {
		return fmt.Errorf("%w (usage=%d%%)", ErrDiskAdmission, pct)
	}

	if err := os.MkdirAll(TmpDir(e.root), 0o755); err != nil {
		return fmt.Errorf("gitfs: create tmp dir: %w", err)
	}
	staging, err := os.MkdirTemp(TmpDir(e.root), "references-*")
	if err != nil {
		return fmt.Errorf("gitfs: reference staging: %w", err)
	}
	defer os.RemoveAll(staging) // no-op after a successful rename

	for _, d := range docs {
		if err := os.WriteFile(filepath.Join(staging, d.Name), d.Content, 0o644); err != nil {
			return fmt.Errorf("gitfs: stage reference %q: %w", d.Name, err)
		}
	}
	// Same widening as a snapshot root: the agents pod reads this content over
	// the shared mount as a different UID, once it is overlaid.
	if err := os.Chmod(staging, 0o755); err != nil {
		return fmt.Errorf("gitfs: chmod reference staging: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("gitfs: create repo dir: %w", err)
	}
	// os.Rename cannot replace a non-empty directory, so the old set moves to
	// trash first. The gap between the two is why a concurrent overlay is
	// best-effort — see overlayReferences.
	if dirExists(dest) {
		if err := os.Rename(dest, e.referenceTrashDest()); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("gitfs: retire previous references: %w", err)
		}
	}
	if err := os.Rename(staging, dest); err != nil {
		return fmt.Errorf("gitfs: publish references: %w", err)
	}
	return nil
}

// referenceTrashDest is a fresh name under trash/ for the retired set; the
// reaper's trash pass purges it on its own schedule.
func (e *Engine) referenceTrashDest() string {
	dir := TrashDir(e.root)
	_ = os.MkdirAll(dir, 0o755)
	staged, err := os.MkdirTemp(dir, "references-*")
	if err != nil {
		// MkdirTemp failed, so nothing exists at this path — the rename below
		// creates it, which is exactly what is wanted.
		return filepath.Join(dir, "references-retired")
	}
	// Reserve the NAME, not the directory: os.Rename needs the destination to
	// not exist.
	_ = os.Remove(staged)
	return staged
}

// ListReferences returns the stored document names, sorted. A project with no
// store (the ordinary case — most attach nothing) returns nil, not an error.
func (e *Engine) ListReferences(ctx context.Context, ref RepoRef) ([]string, error) {
	dir, err := ReferenceStoreDir(e.root, ref)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, e.mapDiskErr(fmt.Errorf("gitfs: list references: %w", err))
	}
	var names []string
	for _, entry := range entries {
		// Regular files only. `IsDir()` alone is not that check: a SYMLINK is
		// neither a dir nor a regular file, so it survives an IsDir filter, and
		// the copy below would then follow it — `os.Open` resolves the link and
		// the mode check lands on the TARGET. A link planted in the store would
		// copy arbitrary readable content into the agent's workspace. Only
		// aep-api writes here, so this is defense in depth against a poisoned
		// mount — the same posture the rest of this package takes.
		if !entry.Type().IsRegular() {
			continue
		}
		// Re-validated on the way out: the store is a directory on a shared
		// volume, and a name that could not have been written by PutReferences
		// has no business being overlaid into a snapshot.
		if validateReferenceName(entry.Name()) != nil {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

// overlayReferences brings a snapshot's ReferenceOverlayDir up to date with the
// project's stored set. This is what makes a non-git document readable by a
// turn.
//
// It runs on EVERY Ensure, not only on first materialization, and that is the
// whole point. A snapshot is immutable with respect to the git SHA, but
// references are a SECOND input keyed to that same SHA and mutable
// independently of it — so addressing by SHA alone cannot express "same tree,
// different documents". Overlaying only at materialization loses the create
// flow outright: `POST /projects` commits the descriptor and moves HEAD to a
// sha, anything that materializes that sha before the upload lands (a status
// poll, the spec view's file list) publishes a snapshot with no references, and
// the `/start` turn then reuses it — the agent gets a steer naming documents
// that are not in its workspace. A re-upload (the confirm step's Retry) has the
// same shape with stale content instead of missing content.
//
// A replacement upload that DROPS a name retires the old file, and this is not
// cosmetic: `keepInTurnSnapshot` admits every text file under this directory
// into the turn's file map, so a lingering document reaches the model whether
// or not the turn's reference list still names it.
//
// Removal is scoped by a manifest (`.aep-references.json`, dot-led so the
// snapshot walk skips it) recording exactly which names this overlay wrote.
// Only those are ever retired — a project created under the feature's v1 has
// real committed references here, arriving through `git archive` like any other
// git content, and they never enter the manifest. When an overlay had MASKED a
// committed file of the same name, retiring it restores the git blob rather
// than deleting it, so dropping a transient document cannot take a committed
// one with it.
//
// Each file is written temp-then-rename inside the destination directory, so a
// turn reading concurrently sees the old bytes or the new bytes, never a torn
// file.
//
// Best-effort by design, matching the steer it feeds: a failure here must not
// fail the snapshot, because that would take down every turn on the project —
// including the ones that never attached a document. The cost is that a lost
// overlay is silent (the agent simply interviews as if nothing were attached),
// which is why it is logged at WARN rather than swallowed.
func (e *Engine) overlayReferences(ctx context.Context, ref RepoRef, sha, staging string) {
	names, err := e.ListReferences(ctx, ref)
	if err != nil {
		slog.WarnContext(ctx, "gitfs: references unlistable; snapshot published without them",
			"org", ref.OrgID, "project", ref.ProjectID, "error", err)
		return
	}
	src, err := ReferenceStoreDir(e.root, ref)
	if err != nil {
		return
	}
	dst := filepath.Join(staging, filepath.FromSlash(ReferenceOverlayDir))
	prev := readOverlayManifest(dst)
	// Nothing stored and nothing previously written: leave the snapshot exactly
	// as `git archive` produced it — no directory, no manifest.
	if len(names) == 0 && len(prev) == 0 {
		return
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		slog.WarnContext(ctx, "gitfs: reference overlay dir; snapshot published without them",
			"org", ref.OrgID, "project", ref.ProjectID, "error", err)
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
			slog.WarnContext(ctx, "gitfs: stale reference not retired",
				"org", ref.OrgID, "project", ref.ProjectID, "name", old, "error", err)
		}
	}
	for _, name := range names {
		from, to := filepath.Join(src, name), filepath.Join(dst, name)
		stale, err := differs(from, to)
		if err != nil {
			slog.WarnContext(ctx, "gitfs: reference not compared; recopying",
				"org", ref.OrgID, "project", ref.ProjectID, "name", name, "error", err)
			stale = true
		}
		if !stale {
			continue
		}
		if err := copyFileAtomic(from, to); err != nil {
			slog.WarnContext(ctx, "gitfs: reference not overlaid",
				"org", ref.OrgID, "project", ref.ProjectID, "name", name, "error", err)
		}
	}
	if err := writeOverlayManifest(dst, names); err != nil {
		slog.WarnContext(ctx, "gitfs: overlay manifest not written; a later drop may not retire",
			"org", ref.OrgID, "project", ref.ProjectID, "error", err)
	}
}

// overlayManifestName records which names THIS overlay wrote, so a later
// reconcile can retire exactly those and never a committed file. Dot-led: the
// snapshot walk skips dot segments at any depth, so it is invisible to turns.
const overlayManifestName = ".aep-references.json"

type overlayManifest struct {
	Written []string `json:"written"`
}

func readOverlayManifest(dir string) []string {
	raw, err := os.ReadFile(filepath.Join(dir, overlayManifestName))
	if err != nil {
		return nil // absent (or unreadable) → nothing is known to be ours
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
	staged, err := os.CreateTemp(dir, ".manifest-*")
	if err != nil {
		return err
	}
	if _, err := staged.Write(raw); err != nil {
		staged.Close()
		os.Remove(staged.Name())
		return err
	}
	if err := staged.Close(); err != nil {
		os.Remove(staged.Name())
		return err
	}
	return os.Rename(staged.Name(), filepath.Join(dir, overlayManifestName))
}

// retireOverlayFile drops a name this overlay wrote and the store no longer
// lists. If the same path exists in the commit's tree, the overlay had been
// MASKING committed content — restore the git blob instead of deleting, so a
// dropped transient document cannot take a v1 project's committed one with it.
func (e *Engine) retireOverlayFile(ctx context.Context, ref RepoRef, sha, dst, name string) error {
	target := filepath.Join(dst, name)
	p, err := e.pathsFor(ref)
	if err != nil {
		return err
	}
	committed, err := e.git(ctx, execOpts{},
		"--git-dir", p.gitDir, "show", sha+":"+ReferenceOverlayDir+"/"+name)
	if err != nil {
		// Not in the tree at this sha — purely ours, so remove it.
		if rmErr := os.Remove(target); rmErr != nil && !os.IsNotExist(rmErr) {
			return rmErr
		}
		return nil
	}
	staged, err := os.CreateTemp(dst, ".restore-*")
	if err != nil {
		return err
	}
	if _, err := staged.Write(committed); err != nil {
		staged.Close()
		os.Remove(staged.Name())
		return err
	}
	if err := staged.Chmod(0o644); err != nil {
		staged.Close()
		os.Remove(staged.Name())
		return err
	}
	if err := staged.Close(); err != nil {
		os.Remove(staged.Name())
		return err
	}
	return os.Rename(staged.Name(), target)
}

// differs reports whether the destination needs rewriting: absent, a different
// size, or older than the source. Size-plus-mtime rather than a content hash —
// this runs on every Ensure, and hashing up to 50 MB per turn to detect a
// change that only happens on an explicit re-upload is not a trade worth
// making. A rewrite that turns out to be unnecessary is harmless.
func differs(src, dst string) (bool, error) {
	di, err := os.Stat(dst)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return true, err
	}
	si, err := os.Stat(src)
	if err != nil {
		return true, err
	}
	return si.Size() != di.Size() || di.ModTime().Before(si.ModTime()), nil
}

// copyFileAtomic copies one regular file into place, 0644 — the same mode
// extractTar gives a snapshot blob, so an overlaid document is
// indistinguishable from a committed one to everything downstream.
//
// Staged beside the destination and renamed onto it, because a published
// snapshot can be read by a turn while this runs: rename is atomic within a
// directory, so a concurrent reader gets the old file or the new one and never
// a half-written one.
//
// The source is opened with O_NOFOLLOW: the mode check has to happen on the
// file this actually reads, and checking after a plain Open would already have
// resolved a symlink. ListReferences filters links out too — both, because
// either one alone leaves a window.
func copyFileAtomic(src, dst string) (err error) {
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
		return fmt.Errorf("gitfs: %q is not a regular file", src)
	}

	staged, err := os.CreateTemp(filepath.Dir(dst), ".reference-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(staged.Name())
		}
	}()
	if _, err = io.Copy(staged, in); err != nil {
		staged.Close()
		return err
	}
	if err = staged.Chmod(0o644); err != nil {
		staged.Close()
		return err
	}
	if err = staged.Close(); err != nil {
		return err
	}
	return os.Rename(staged.Name(), dst)
}
