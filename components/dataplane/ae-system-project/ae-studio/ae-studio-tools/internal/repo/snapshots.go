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
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Snapshots are the plain-file trees ae-design-agent reads a turn from (moved
// from aep-api's gitfs/snapshots.go; design/route-groups.md). The agent mounts
// only <root>/snapshots, read-only, and reads
// /snapshots/projects/<project>/<sha> and /snapshots/skills/<sha>.

// EnsureSnapshot materializes the immutable tree of ref's commit sha at
// snapshots/projects/<project>/<sha> iff absent, and returns its path. The
// project names the snapshot (the agent's contract), not the mirror. sha
// must be a full 40-hex commit name.
func (e *Engine) EnsureSnapshot(ctx context.Context, ref RepoRef, project, sha string) (string, error) {
	dest, err := SnapshotDir(e.root, project, sha)
	if err != nil {
		return "", err
	}
	return dest, e.ensureTree(ctx, ref, sha, dest)
}

// EnsureSkillsSnapshot materializes the immutable tree of the Org skills
// repository's commit sha at snapshots/skills/<sha> iff absent, and returns
// its path. One pod serves one org, so the sha alone names it.
func (e *Engine) EnsureSkillsSnapshot(ctx context.Context, ref RepoRef, sha string) (string, error) {
	dest, err := SkillsSnapshotDir(e.root, sha)
	if err != nil {
		return "", err
	}
	return dest, e.ensureTree(ctx, ref, sha, dest)
}

// ensureTree publishes the tree at sha as dest. `git archive` streams the
// tree into tmp/ staging (Go's archive/tar, no tar binary), then one
// os.Rename publishes it. Concurrent callers are safe: both stage privately,
// one rename wins, the loser finds dest and cleans up, so a torn dir is never
// observable at dest.
//
// An existing dest is reused and its mtime set to now (markUsed): the
// snapshot-age pass and eviction then measure the time since a turn last
// asked for it, not since it was made, so a snapshot a running turn reads is
// not reaped under it. A dest the reaper trashed first is made again. A new
// one passes admission first (DiskAdmissionRefusePct).
func (e *Engine) ensureTree(ctx context.Context, ref RepoRef, sha, dest string) (err error) {
	defer func() { err = e.mapDiskErr(err) }()
	reused, err := e.markUsed(dest)
	if err != nil {
		return fmt.Errorf("repo: touch snapshot %s: %w", sha, err)
	}
	if reused {
		return nil
	}
	if err := e.admit(); err != nil {
		return err
	}
	p, err := e.pathsFor(ref)
	if err != nil {
		return err
	}
	cloned, err := e.ensureMirror(ctx, ref, p)
	if err != nil {
		return err
	}
	// A snapshot is addressed by raw sha: fetch only if the object is missing.
	if err := e.freshenFor(ctx, ref, p, cloned, sha); err != nil {
		return err
	}

	staging, err := os.MkdirTemp(TmpDir(e.root), "snapshot-*")
	if err != nil {
		return fmt.Errorf("repo: snapshot staging: %w", err)
	}
	defer os.RemoveAll(staging) // no-op after a successful rename

	release, err := e.locks.RLock(ctx, p.lockPath)
	if err != nil {
		return err
	}
	archiveErr := e.gitStream(ctx, execOpts{}, func(r io.Reader) error {
		return extractTar(r, staging)
	}, "--git-dir", p.gitDir, "archive", "--format=tar", sha)
	release()
	if archiveErr != nil {
		msg := strings.ToLower(archiveErr.Error())
		if strings.Contains(msg, "not a valid") || strings.Contains(msg, "not a tree") {
			return fmt.Errorf("repo: snapshot %s: %w: %w", sha, ErrRefNotFound, archiveErr)
		}
		return fmt.Errorf("repo: snapshot %s: %w", sha, archiveErr)
	}

	// os.MkdirTemp created the staging root 0700; widen it to 0755 so the
	// agent container (another UID, over its read-only mount) can traverse
	// it. Extracted files (0644/0755) and dirs (0755) are already readable.
	if err := os.Chmod(staging, 0o755); err != nil {
		return fmt.Errorf("repo: chmod snapshot root: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("repo: create snapshot parent: %w", err)
	}
	size := DirBytes(staging)
	if err := os.Rename(staging, dest); err != nil {
		if dirExists(dest) {
			return nil // lost the materialization race: identical content
		}
		return fmt.Errorf("repo: publish snapshot %s: %w", sha, err)
	}
	e.AddUsage(size)
	return nil
}

// markUsed sets an existing snapshot's mtime to now and reports whether it
// existed. It runs under snapshotUse, the lock TrashSnapshot decides under,
// so a reuse and a trash of one leaf never interleave: either the trash comes
// first and the caller makes the leaf again, or the touch comes first and the
// trash sees a leaf used after its cutoff.
func (e *Engine) markUsed(dest string) (bool, error) {
	e.snapshotUse.Lock()
	defer e.snapshotUse.Unlock()
	now := time.Now()
	err := os.Chtimes(dest, now, now)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// TrashSnapshot renames one snapshot leaf (snapshots/projects/<p>/<sha> or
// snapshots/skills/<sha>) into trash/<id>, for the reaper, iff it was last
// used before cutoff. The mtime is read under snapshotUse, at the rename, not
// from the reaper's earlier listing: a lookup that reused the leaf since
// keeps it (trashed false). Anything but a leaf is refused: the agent's
// subPath mount pins the snapshots dir's inode, so snapshots/,
// snapshots/projects/, snapshots/skills/ (and a project's dir) are never
// moved. A missing leaf is a no-op.
func (e *Engine) TrashSnapshot(dir string, cutoff time.Time) (trashed bool, err error) {
	if !e.isSnapshotLeaf(dir) {
		return false, fmt.Errorf("repo: %q is not a snapshot", dir)
	}
	e.snapshotUse.Lock()
	defer e.snapshotUse.Unlock()
	st, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("repo: stat snapshot: %w", err)
	}
	if !st.ModTime().Before(cutoff) {
		return false, nil
	}
	if err := os.Rename(dir, trashDest(e.root)); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("repo: trash snapshot: %w", err)
	}
	return true, nil
}

// isSnapshotLeaf reports whether dir is exactly a <sha> leaf under the
// project or skills snapshot roots (cleaned, so no dot segment can reach out).
func (e *Engine) isSnapshotLeaf(dir string) bool {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || !isHex40(filepath.Base(dir)) {
		return false
	}
	parent := filepath.Dir(dir)
	if parent == SkillsSnapshotsDir(e.root) {
		return true
	}
	return filepath.Dir(parent) == ProjectSnapshotsDir(e.root) && validateSegment("project", filepath.Base(parent)) == nil
}

// extractTar unpacks a git-archive tar stream into dir: regular files and
// directories only (symlinks and submodule entries are skipped: the agent
// reads plain spec and skill files, and a symlink could point outside the
// snapshot). Every entry path is fenced inside dir, defense in depth even
// though git archive never emits traversal.
func extractTar(r io.Reader, dir string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		target, err := fenceInside(dir, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("extract dir %s: %w", hdr.Name, err)
			}
		case tar.TypeReg:
			if err := extractFile(tr, hdr, target); err != nil {
				return err
			}
		default:
			// pax headers are consumed by archive/tar itself; symlinks and
			// exotic types are deliberately not materialized.
		}
	}
}

// extractFile writes one regular tar entry at target, 0644 (0755 when the
// blob is executable).
func extractFile(tr *tar.Reader, hdr *tar.Header, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("extract parent of %s: %w", hdr.Name, err)
	}
	mode := os.FileMode(0o644)
	if hdr.FileInfo().Mode()&0o100 != 0 {
		mode = 0o755
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("extract %s: %w", hdr.Name, err)
	}
	if _, err := io.Copy(f, tr); err != nil {
		_ = f.Close()
		return fmt.Errorf("extract %s: %w", hdr.Name, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("extract %s: %w", hdr.Name, err)
	}
	return nil
}

// fenceInside joins name under dir and rejects any resolved path escaping it.
func fenceInside(dir, name string) (string, error) {
	target := filepath.Join(dir, filepath.FromSlash(name))
	if target != dir && !strings.HasPrefix(target, dir+string(filepath.Separator)) {
		return "", fmt.Errorf("repo: tar entry %q escapes the snapshot dir", name)
	}
	return target, nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
