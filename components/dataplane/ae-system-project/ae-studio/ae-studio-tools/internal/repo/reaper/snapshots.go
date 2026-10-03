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

package reaper

import (
	"bufio"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// snapshotLeaf is one snapshots/projects/<project>/<sha> or
// snapshots/skills/<sha> tree.
type snapshotLeaf struct {
	path    string
	project string // repo.SkillsProject for a skills snapshot
	// isHead is true when sha is the current HEAD of the project's mirror:
	// the tree a new turn on the project reads.
	isHead bool
	// lastUse is the leaf's mtime: set when it is made and again whenever a
	// lookup reuses it (repo.Engine.EnsureSnapshot).
	lastUse time.Time
}

// rel is the leaf's path under the studio-data root, for eviction results.
func (l snapshotLeaf) rel(root string) string {
	rel, err := filepath.Rel(root, l.path)
	if err != nil {
		return l.path
	}
	return filepath.ToSlash(rel)
}

// reapSnapshots is the snapshot-age pass: trash every snapshot leaf unused
// for longer than SnapshotMaxAge whose sha is not its mirror's current HEAD.
// Snapshots are immutable, so age and not-HEAD are the whole liveness rule; a
// project with no mirror has no HEAD, so all its aged snapshots go. Only
// <sha> leaves are ever trashed (repo.Engine.TrashSnapshot).
func (r *Reaper) reapSnapshots(ctx context.Context) error {
	leaves, err := r.snapshotLeaves(ctx)
	if err != nil {
		return err
	}
	return r.reapLeaves(ctx, leaves, time.Now())
}

// reapLeaves trashes the listed leaves unused for longer than SnapshotMaxAge
// at now, HEAD ones excepted. The listing's mtime only preselects:
// TrashSnapshot re-reads it under the engine's use lock, so a leaf a lookup
// reused since the listing is kept.
func (r *Reaper) reapLeaves(ctx context.Context, leaves []snapshotLeaf, now time.Time) error {
	cutoff := now.Add(-r.cfg.SnapshotMaxAge)
	for _, l := range leaves {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if l.isHead || !l.lastUse.Before(cutoff) {
			continue
		}
		if _, err := r.engine.TrashSnapshot(l.path, cutoff); err != nil {
			slog.WarnContext(ctx, "reaper.snapshot_trash_failed", "project", l.project, "error", err)
		}
	}
	return nil
}

// snapshotLeaves lists every snapshot leaf with its HEAD flag. Only real
// directories are listed (a symlink is never followed). A level that cannot
// be read is skipped: a concurrent trash rename is normal.
func (r *Reaper) snapshotLeaves(ctx context.Context) ([]snapshotLeaf, error) {
	heads, err := r.mirrorHeads(ctx)
	if err != nil {
		return nil, err
	}
	root := r.engine.Root()
	var leaves []snapshotLeaf
	if projects, err := os.ReadDir(repo.ProjectSnapshotsDir(root)); err == nil {
		for _, p := range projects {
			if p.IsDir() {
				leaves = appendLeaves(leaves, filepath.Join(repo.ProjectSnapshotsDir(root), p.Name()), p.Name(), heads[p.Name()])
			}
		}
	}
	return appendLeaves(leaves, repo.SkillsSnapshotsDir(root), repo.SkillsProject, heads[repo.SkillsProject]), nil
}

// appendLeaves adds the <sha> dirs under dir, all belonging to project.
func appendLeaves(leaves []snapshotLeaf, dir, project string, heads map[string]bool) []snapshotLeaf {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return leaves
	}
	for _, e := range entries {
		if !e.IsDir() || !sha40.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		leaves = append(leaves, snapshotLeaf{
			path:    filepath.Join(dir, e.Name()),
			project: project,
			isHead:  heads[e.Name()],
			lastUse: info.ModTime(),
		})
	}
	return leaves
}

// mirrorHeads maps each project (repo.SkillsProject for the org skills
// repository) to the current HEAD shas of its mirrors.
func (r *Reaper) mirrorHeads(ctx context.Context) (map[string]map[string]bool, error) {
	heads := map[string]map[string]bool{}
	err := r.walkRepoDirs(ctx, func(ref repo.RepoRef, repoDir string) {
		head := mirrorHead(repo.GitSubdir(repoDir))
		if head == "" {
			return
		}
		if heads[ref.Project] == nil {
			heads[ref.Project] = map[string]bool{}
		}
		heads[ref.Project][head] = true
	})
	return heads, err
}

var sha40 = regexp.MustCompile(`^[0-9a-f]{40}$`)

// mirrorHead resolves the bare mirror's default-branch tip by reading the
// ref store directly: the cheapest correct read (no child process; the sweep
// visits every repo). HEAD names the default branch; the branch resolves
// through a loose ref first (written by every fetch and push) and falls back
// to packed-refs (where a fresh clone leaves all refs). "" when the mirror is
// missing or the ref cannot be resolved: the caller then treats every
// snapshot of the project as not HEAD.
func mirrorHead(gitDir string) string {
	data, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(data))
	if sha40.MatchString(head) {
		return head // detached HEAD: already the tip sha
	}
	refName, ok := strings.CutPrefix(head, "ref: ")
	if !ok {
		return ""
	}
	refName = strings.TrimSpace(refName)
	if data, err := os.ReadFile(filepath.Join(gitDir, filepath.FromSlash(refName))); err == nil {
		if sha := strings.TrimSpace(string(data)); sha40.MatchString(sha) {
			return sha
		}
	}
	return packedRef(gitDir, refName)
}

// packedRef scans <gitDir>/packed-refs for refName. Lines are
// "<sha> <refname>"; "#" comment and "^" peel lines are skipped.
func packedRef(gitDir, refName string) string {
	f, err := os.Open(filepath.Join(gitDir, "packed-refs"))
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || line[0] == '#' || line[0] == '^' {
			continue
		}
		sha, name, ok := strings.Cut(line, " ")
		if ok && name == refName && sha40.MatchString(sha) {
			return sha
		}
	}
	// A line over the scanner's buffer ends the loop early, and "" means "no
	// such ref", so say which one it was.
	if err := scanner.Err(); err != nil {
		slog.Warn("reaper.packed_refs_scan_stopped", "error", err)
	}
	return ""
}
