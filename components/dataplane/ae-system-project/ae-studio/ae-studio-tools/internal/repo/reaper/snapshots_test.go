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
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

func fakeSha(n int) string { return fmt.Sprintf("%040x", n) }

// writeMirrorAt lays out repos/<org>/<project>/<slug>/git with HEAD on main
// and main at head (a loose ref), so the reaper resolves it as the mirror's
// current HEAD.
func writeMirrorAt(t *testing.T, root, org, project, slug, head string) {
	t.Helper()
	gitDir := repo.GitSubdir(filepath.Join(repo.ReposDir(root), org, project, slug))
	mkFile(t, filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"))
	mkFile(t, filepath.Join(gitDir, "refs", "heads", "main"), []byte(head+"\n"))
}

// writeSnapshot lays out a snapshot leaf with one size-byte file and pins
// the leaf's mtime (its last use) to at.
func writeSnapshot(t *testing.T, dir string, size int, at time.Time) string {
	t.Helper()
	mkFile(t, filepath.Join(dir, "specs", "f.md"), bytes.Repeat([]byte("x"), size))
	chtimes(t, dir, at)
	return dir
}

func projectSnap(root, project, sha string) string {
	return filepath.Join(repo.ProjectSnapshotsDir(root), project, sha)
}

func skillsSnap(root, sha string) string {
	return filepath.Join(repo.SkillsSnapshotsDir(root), sha)
}

// The snapshot-age pass (20 §3) trashes a <sha> leaf unused for longer than
// SnapshotMaxAge unless it is its repository mirror's current HEAD; the
// skills snapshots follow the org skills mirror. A project with no mirror
// (evicted, or never cloned since a roll) has no HEAD to keep.
func TestSnapshotAgeReap(t *testing.T) {
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1 << 30})
	head, skillsHead := fakeSha(1), fakeSha(2)
	writeMirrorAt(t, root, "acme", "greeter", "acme-greeter", head)
	writeMirrorAt(t, root, "acme", repo.SkillsProject, "acme-org-skills", skillsHead)
	aged, fresh := time.Now().Add(-25*time.Hour), time.Now()

	keepHead := writeSnapshot(t, projectSnap(root, "greeter", head), 10, aged)
	reapOld := writeSnapshot(t, projectSnap(root, "greeter", fakeSha(3)), 10, aged)
	keepFresh := writeSnapshot(t, projectSnap(root, "greeter", fakeSha(4)), 10, fresh)
	reapOrphan := writeSnapshot(t, projectSnap(root, "gone", fakeSha(5)), 10, aged)
	keepSkillsHead := writeSnapshot(t, skillsSnap(root, skillsHead), 10, aged)
	reapSkillsOld := writeSnapshot(t, skillsSnap(root, fakeSha(6)), 10, aged)
	keepSkillsFresh := writeSnapshot(t, skillsSnap(root, fakeSha(7)), 10, fresh)

	if err := r.reapSnapshots(context.Background()); err != nil {
		t.Fatalf("reapSnapshots: %v", err)
	}
	for _, p := range []string{reapOld, reapOrphan, reapSkillsOld} {
		mustNotExist(t, p)
	}
	for _, p := range []string{keepHead, keepFresh, keepSkillsHead, keepSkillsFresh} {
		mustExist(t, p)
	}
	trash, err := os.ReadDir(repo.TrashDir(root))
	if err != nil || len(trash) != 3 {
		t.Fatalf("trash has %d entries (%v), want the 3 reaped snapshots", len(trash), err)
	}
}

// Over the high mark, eviction takes snapshots first, oldest last use first,
// before any mirror; it never takes a HEAD snapshot or one used within
// snapshotEvictMinAge, which a running turn may still be reading. (The age
// pass is held off with a long SnapshotMaxAge so only eviction acts.)
func TestBudget_EvictsSnapshotsBeforeMirrors(t *testing.T) {
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1, SnapshotMaxAge: 24 * time.Hour})
	head := fakeSha(1)
	writeMirrorAt(t, root, "acme", "greeter", "acme-greeter", head)
	mirror := writeMirror(t, root, "acme/greeter/acme-greeter", 300*kib, time.Now().Add(-48*time.Hour))
	oldest := writeSnapshot(t, projectSnap(root, "greeter", fakeSha(2)), 400*kib, time.Now().Add(-3*time.Hour))
	headSnap := writeSnapshot(t, projectSnap(root, "greeter", head), 400*kib, time.Now().Add(-4*time.Hour))
	inUse := writeSnapshot(t, skillsSnap(root, fakeSha(3)), 400*kib, time.Now().Add(-10*time.Minute))
	r.cfg.Budget = budgetAt(t, root, 90)

	evicted, err := r.sweepOnce(context.Background())
	if err != nil {
		t.Fatalf("sweepOnce: %v", err)
	}
	if want := []string{"snapshots/projects/greeter/" + fakeSha(2)}; !reflect.DeepEqual(evicted, want) {
		t.Fatalf("evicted = %v, want %v", evicted, want)
	}
	mustNotExist(t, oldest)
	for _, p := range []string{headSnap, inUse, mirror} {
		mustExist(t, p)
	}
}

// When snapshots alone do not free enough, mirrors go next, LRU.
func TestBudget_EvictsMirrorsAfterSnapshots(t *testing.T) {
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1, SnapshotMaxAge: 24 * time.Hour})
	snap := writeSnapshot(t, projectSnap(root, "greeter", fakeSha(2)), 50*kib, time.Now().Add(-3*time.Hour))
	mirror := writeMirror(t, root, "acme/greeter/acme-greeter", 800*kib, time.Now().Add(-48*time.Hour))
	r.cfg.Budget = budgetAt(t, root, 90)

	evicted, err := r.sweepOnce(context.Background())
	if err != nil {
		t.Fatalf("sweepOnce: %v", err)
	}
	if want := []string{"snapshots/projects/greeter/" + fakeSha(2), "acme/greeter/acme-greeter"}; !reflect.DeepEqual(evicted, want) {
		t.Fatalf("evicted = %v, want %v", evicted, want)
	}
	mustNotExist(t, snap)
	mustNotExist(t, mirror)
}

// Q-24: ae-design-agent's subPath mount pins the snapshots dir's inode. No
// pass (age, budget, trash purge, a forced sweep) and no TrashRepo ever
// removes or renames snapshots/, snapshots/projects/ or snapshots/skills/:
// only <sha> leaves go.
func TestSnapshotRootsAreNeverMoved(t *testing.T) {
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1, TrashMaxAge: time.Nanosecond})
	roots := []string{repo.SnapshotsDir(root), repo.ProjectSnapshotsDir(root), repo.SkillsSnapshotsDir(root)}
	before := make([]os.FileInfo, len(roots))
	for i, d := range roots {
		st, err := os.Stat(d)
		if err != nil {
			t.Fatalf("stat %s: %v", d, err)
		}
		before[i] = st
	}
	aged := time.Now().Add(-48 * time.Hour)
	writeSnapshot(t, projectSnap(root, "greeter", fakeSha(1)), 300*kib, aged)
	writeSnapshot(t, skillsSnap(root, fakeSha(2)), 300*kib, aged)
	writeMirror(t, root, "acme/greeter/acme-greeter", 300*kib, aged)
	r.cfg.Budget = budgetAt(t, root, 100)

	if _, err := r.sweepOnce(context.Background()); err != nil {
		t.Fatalf("sweepOnce: %v", err)
	}
	r.ForceSweep(context.Background())
	if err := r.engine.TrashRepo(context.Background(), repo.RepoRef{Org: "acme", Project: "greeter", RepoSlug: "acme-greeter"}); err != nil {
		t.Fatalf("TrashRepo: %v", err)
	}
	for i, d := range roots {
		st, err := os.Stat(d)
		if err != nil {
			t.Fatalf("snapshot root %s gone: %v", d, err)
		}
		if !os.SameFile(before[i], st) {
			t.Fatalf("snapshot root %s was replaced (new inode)", d)
		}
	}
	mustNotExist(t, projectSnap(root, "greeter", fakeSha(1)))
	mustNotExist(t, skillsSnap(root, fakeSha(2)))
}

// mirrorHead reads the ref store directly: a loose ref wins, packed-refs is
// the fallback, a missing mirror is "".
func TestMirrorHeadResolvesLooseAndPackedRefs(t *testing.T) {
	looseSha, packedSha := fakeSha(3), fakeSha(4)
	gitDir := filepath.Join(t.TempDir(), "git")
	mkFile(t, filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"))
	mkFile(t, filepath.Join(gitDir, "packed-refs"),
		[]byte("# pack-refs with: peeled fully-peeled sorted\n"+packedSha+" refs/heads/main\n"))
	if got := mirrorHead(gitDir); got != packedSha {
		t.Fatalf("packed-refs fallback = %q, want %q", got, packedSha)
	}
	mkFile(t, filepath.Join(gitDir, "refs", "heads", "main"), []byte(looseSha+"\n"))
	if got := mirrorHead(gitDir); got != looseSha {
		t.Fatalf("loose ref = %q, want %q", got, looseSha)
	}
	if got := mirrorHead(filepath.Join(t.TempDir(), "absent")); got != "" {
		t.Fatalf("missing mirror = %q, want empty", got)
	}
}

// A lookup that reuses a leaf after the age pass listed it (and so refreshed
// its mtime) keeps it: the pass decides on the mtime at the rename, not at
// the listing.
func TestSnapshotTouchedAfterListingIsNotReaped(t *testing.T) {
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1 << 30})
	leaf := writeSnapshot(t, projectSnap(root, "greeter", fakeSha(3)), 10, time.Now().Add(-25*time.Hour))
	leaves, err := r.snapshotLeaves(context.Background())
	if err != nil || len(leaves) != 1 {
		t.Fatalf("snapshotLeaves = %v, %v", leaves, err)
	}
	chtimes(t, leaf, time.Now()) // the lookup's touch
	if err := r.reapLeaves(context.Background(), leaves, time.Now()); err != nil {
		t.Fatal(err)
	}
	mustExist(t, leaf)
}

// The same holds for eviction: a leaf reused after the listing is skipped
// and its bytes are not counted as freed.
func TestEvictionSkipsALeafTouchedAfterListing(t *testing.T) {
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1})
	leaf := writeSnapshot(t, projectSnap(root, "greeter", fakeSha(3)), 10*kib, time.Now().Add(-3*time.Hour))
	leaves, err := r.snapshotLeaves(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	chtimes(t, leaf, time.Now())
	evicted, freed := r.evictLeaves(context.Background(), leaves, 1<<30, time.Now())
	if len(evicted) != 0 || freed != 0 {
		t.Fatalf("evicted = %v, freed = %d; want none", evicted, freed)
	}
	mustExist(t, leaf)
}
