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
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Ensure implements SnapshotProvider (design D4): materialize the immutable
// plain-file tree snapshots/<sha>/ iff absent. `git archive` streams the
// tree into tmp/ staging (Go's archive/tar — no tar binary dependency), then
// one os.Rename publishes it. Concurrent double-invocation is safe: both
// stage privately, one rename wins, the loser detects the existing dir and
// cleans up — a torn dir is never observable at the canonical path.
func (e *Engine) Ensure(ctx context.Context, ref RepoRef, sha string) (err error) {
	defer func() { err = e.mapDiskErr(err) }()
	p, err := e.pathsFor(ref)
	if err != nil {
		return err
	}
	dest, err := SnapshotDir(e.root, ref, sha)
	if err != nil {
		return err
	}
	if dirExists(dest) {
		// The git tree at this sha is already materialized and immutable — but
		// references are a SECOND input keyed to the same sha and mutable
		// independently of it, so "the snapshot exists" does not mean "the
		// snapshot is current". Reconcile them before handing it to a turn; see
		// overlayReferences for the create-flow ordering this exists to fix.
		e.overlayReferences(ctx, ref, sha, dest)
		return nil
	}
	if pct := e.DiskUsagePct(); pct >= DiskAdmissionRefusePct {
		return fmt.Errorf("%w (usage=%d%%)", ErrDiskAdmission, pct)
	}
	cloned, err := e.ensureMirror(ctx, ref, p)
	if err != nil {
		return err
	}
	// A snapshot is addressed by raw sha: fetch only if the object is missing.
	if !cloned {
		ok, oerr := e.objectExists(ctx, ref, p, sha)
		if oerr != nil {
			return oerr
		}
		if !ok {
			release, lerr := e.locks.Lock(ctx, p.lockPath)
			if lerr != nil {
				return lerr
			}
			ferr := e.fetch(ctx, ref, p)
			release()
			if ferr != nil {
				return ferr
			}
		}
	}

	staging, err := os.MkdirTemp(TmpDir(e.root), "snapshot-*")
	if err != nil {
		return fmt.Errorf("gitfs: snapshot staging: %w", err)
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
		if strings.Contains(strings.ToLower(archiveErr.Error()), "not a valid") ||
			strings.Contains(strings.ToLower(archiveErr.Error()), "not a tree") {
			return fmt.Errorf("gitfs: snapshot %s: %w: %w", sha, ErrRefNotFound, archiveErr)
		}
		return fmt.Errorf("gitfs: snapshot %s: %w", sha, archiveErr)
	}

	// Reference documents are not in the tree `git archive` just streamed —
	// they are never committed (references.go) — so they are laid over the
	// extracted staging dir here, while it is still private. Best-effort: a
	// missing overlay must not fail a snapshot that every turn depends on.
	e.overlayReferences(ctx, ref, sha, staging)

	// os.MkdirTemp created the staging root as 0700; widen it to 0755 so a
	// cross-service reader (the agents pod runs as a different UID over the RO
	// mount) can traverse the published snapshot. Extracted files (0644/0755)
	// and subdirs (0755) already carry cross-UID-readable modes.
	if err := os.Chmod(staging, 0o755); err != nil {
		return fmt.Errorf("gitfs: chmod snapshot root: %w", err)
	}
	if err := os.MkdirAll(p.snapshotsDir, 0o755); err != nil {
		return fmt.Errorf("gitfs: create snapshots dir: %w", err)
	}
	if err := os.Rename(staging, dest); err != nil {
		if dirExists(dest) {
			return nil // lost the materialization race — identical content, fine
		}
		return fmt.Errorf("gitfs: publish snapshot %s: %w", sha, err)
	}
	return nil
}

// extractTar unpacks a git-archive tar stream into dir: regular files and
// directories only (symlinks and submodule entries are skipped — agents read
// plain spec/skill files, and a symlink could point outside the snapshot).
// Every entry path is fenced inside dir — defense in depth, even though
// git archive never emits traversal.
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
		default:
			// pax headers are consumed by archive/tar itself; symlinks and
			// exotic types are deliberately not materialized.
		}
	}
}

// fenceInside joins name under dir and rejects any resolved path escaping it.
func fenceInside(dir, name string) (string, error) {
	target := filepath.Join(dir, filepath.FromSlash(name))
	if target != dir && !strings.HasPrefix(target, dir+string(filepath.Separator)) {
		return "", fmt.Errorf("gitfs: tar entry %q escapes the snapshot dir", name)
	}
	return target, nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
