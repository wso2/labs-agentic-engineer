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

package repo_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

func projectSnapshotDir(t *testing.T, fx *Fixture, sha string) string {
	t.Helper()
	d, err := repo.SnapshotDir(fx.Engine.Root(), defaultProject, sha)
	if err != nil {
		t.Fatalf("SnapshotDir: %v", err)
	}
	return d
}

func assertSnapshotContent(t *testing.T, dir string) {
	t.Helper()
	readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil || string(readme) != "hello\n" {
		t.Fatalf("snapshot README.md = (%q, %v)", readme, err)
	}
	req, err := os.ReadFile(filepath.Join(dir, "specs", "requirements", "prd.md"))
	if err != nil || string(req) != "req v1\n" {
		t.Fatalf("snapshot prd.md = (%q, %v)", req, err)
	}
}

// The layout the agent's subPath mount sees: snapshots/projects/<p>/<sha>
// and snapshots/skills/<sha>, nothing per repo.
func TestSnapshotPaths(t *testing.T) {
	sha := strings.Repeat("ab", 20)
	got, err := repo.SnapshotDir("/data", "greeter", sha)
	if err != nil || got != "/data/snapshots/projects/greeter/"+sha {
		t.Fatalf("SnapshotDir = %q, %v", got, err)
	}
	got, err = repo.SkillsSnapshotDir("/data", sha)
	if err != nil || got != "/data/snapshots/skills/"+sha {
		t.Fatalf("SkillsSnapshotDir = %q, %v", got, err)
	}
	for _, bad := range []string{"main", "", strings.Repeat("A", 40), sha[:39]} {
		if _, err := repo.SnapshotDir("/data", "greeter", bad); err == nil {
			t.Errorf("SnapshotDir accepted sha %q", bad)
		}
		if _, err := repo.SkillsSnapshotDir("/data", bad); err == nil {
			t.Errorf("SkillsSnapshotDir accepted sha %q", bad)
		}
	}
	for _, bad := range []string{"..", ".", "a/b", ""} {
		if _, err := repo.SnapshotDir("/data", bad, sha); err == nil {
			t.Errorf("SnapshotDir accepted project %q", bad)
		}
	}
}

// New lays out the snapshot roots the agent's subPath mount binds, so they
// exist before any snapshot is written.
func TestNewCreatesSnapshotRoots(t *testing.T) {
	e := NewEngine(t, nil)
	for _, d := range []string{repo.SnapshotsDir(e.Root()), repo.ProjectSnapshotsDir(e.Root()), repo.SkillsSnapshotsDir(e.Root())} {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			t.Fatalf("%s: %v", d, err)
		}
	}
}

func TestEnsureSnapshotMaterializesImmutableTree(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()
	sha := mustHead(t, fx, "")

	dir, err := fx.Engine.EnsureSnapshot(ctx, fx.Ref, defaultProject, sha)
	if err != nil {
		t.Fatalf("EnsureSnapshot: %v", err)
	}
	if want := projectSnapshotDir(t, fx, sha); dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
	assertSnapshotContent(t, dir)

	// Idempotent: a second call short-circuits without running git at all.
	rec := recordCommands(t, fx.Engine)
	again, err := fx.Engine.EnsureSnapshot(ctx, fx.Ref, defaultProject, sha)
	if err != nil || again != dir {
		t.Fatalf("EnsureSnapshot(again) = %q, %v", again, err)
	}
	if n := len(rec.all()); n != 0 {
		t.Fatalf("idempotent EnsureSnapshot ran %d git commands, want 0", n)
	}

	// tmp/ staging left no debris behind.
	entries, err := os.ReadDir(repo.TmpDir(fx.Engine.Root()))
	if err != nil {
		t.Fatalf("read tmp: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "snapshot-") {
			t.Fatalf("staging debris left in tmp/: %s", e.Name())
		}
	}
}

// A reused snapshot is stamped as used now, so the age pass and eviction
// measure the time since the last turn asked for it, not since it was made.
func TestEnsureSnapshotReuseRefreshesAge(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()
	sha := mustHead(t, fx, "")
	dir, err := fx.Engine.EnsureSnapshot(ctx, fx.Ref, defaultProject, sha)
	if err != nil {
		t.Fatalf("EnsureSnapshot: %v", err)
	}
	old := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.Engine.EnsureSnapshot(ctx, fx.Ref, defaultProject, sha); err != nil {
		t.Fatalf("EnsureSnapshot(again): %v", err)
	}
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(st.ModTime()) > time.Minute {
		t.Fatalf("reused snapshot mtime = %v, want now", st.ModTime())
	}
}

func TestEnsureSnapshotFetchesUnseenShaAndRejectsUnknown(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()
	mustHead(t, fx, "") // prime the mirror
	sha2 := fx.Origin.Commit(t, map[string]string{"new.md": "new\n"}, "second")

	dir, err := fx.Engine.EnsureSnapshot(ctx, fx.Ref, defaultProject, sha2)
	if err != nil {
		t.Fatalf("EnsureSnapshot(unseen sha): %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "new.md"))
	if err != nil || string(content) != "new\n" {
		t.Fatalf("snapshot new.md = (%q, %v)", content, err)
	}

	missing := strings.Repeat("deadbeef", 5)
	if _, err := fx.Engine.EnsureSnapshot(ctx, fx.Ref, defaultProject, missing); !errors.Is(err, repo.ErrRefNotFound) {
		t.Fatalf("EnsureSnapshot(unknown sha) err = %v, want ErrRefNotFound", err)
	}
	if _, err := os.Stat(projectSnapshotDir(t, fx, missing)); !os.IsNotExist(err) {
		t.Fatalf("unknown-sha snapshot dir published (err=%v)", err)
	}

	if _, err := fx.Engine.EnsureSnapshot(ctx, fx.Ref, defaultProject, "main"); err == nil {
		t.Fatal("EnsureSnapshot(symbolic ref) succeeded, want validation error")
	}
}

// The published tree is readable by another UID (ae-design-agent reads it
// over its read-only subPath mount).
func TestEnsureSnapshotIsCrossUIDReadable(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	sha := mustHead(t, fx, "")
	dir, err := fx.Engine.EnsureSnapshot(context.Background(), fx.Ref, defaultProject, sha)
	if err != nil {
		t.Fatalf("EnsureSnapshot: %v", err)
	}
	for path, want := range map[string]os.FileMode{
		dir:                             0o755,
		filepath.Join(dir, "specs"):     0o055,
		filepath.Join(dir, "README.md"): 0o044,
		filepath.Dir(dir):               0o055, // snapshots/projects/<p>
		repo.ProjectSnapshotsDir(fx.Engine.Root()): 0o055,
	} {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if st.Mode().Perm()&want != want {
			t.Fatalf("%s perm = %04o, want at least %04o", path, st.Mode().Perm(), want)
		}
	}
}

func TestEnsureSnapshotConcurrentNeverTearsDir(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()
	sha := mustHead(t, fx, "")

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = fx.Engine.EnsureSnapshot(ctx, fx.Ref, defaultProject, sha)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent EnsureSnapshot #%d: %v", i, err)
		}
	}
	dir := projectSnapshotDir(t, fx, sha)
	assertSnapshotContent(t, dir)
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil {
		t.Fatalf("read project snapshots dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != sha {
		t.Fatalf("project snapshots dir has %d entries, want exactly [%s]", len(entries), sha)
	}
}

// The Org skills snapshot lands at snapshots/skills/<sha>, keyed by sha
// only: one org, one skills repository.
func TestEnsureSkillsSnapshot(t *testing.T) {
	fx := NewFixture(t, map[string]string{"designer/SKILL.md": "# designer\n"})
	sha := mustHead(t, fx, "")
	dir, err := fx.Engine.EnsureSkillsSnapshot(context.Background(), fx.Ref, sha)
	if err != nil {
		t.Fatalf("EnsureSkillsSnapshot: %v", err)
	}
	want, _ := repo.SkillsSnapshotDir(fx.Engine.Root(), sha)
	if dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "designer", "SKILL.md")); err != nil || string(b) != "# designer\n" {
		t.Fatalf("skill = (%q, %v)", b, err)
	}
	if _, err := os.Stat(repo.ProjectSnapshotsDir(fx.Engine.Root()) + "/" + defaultProject); !os.IsNotExist(err) {
		t.Fatalf("a skills snapshot wrote under snapshots/projects (err=%v)", err)
	}
}

// Admission: at 90 % usage a new snapshot is refused with a disk-full
// error and nothing is published; an existing one is still served.
func TestEnsureSnapshotAdmissionAt90(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()
	sha := mustHead(t, fx, "")
	existing, err := fx.Engine.EnsureSnapshot(ctx, fx.Ref, defaultProject, sha)
	if err != nil {
		t.Fatal(err)
	}
	sha2 := fx.Origin.Commit(t, map[string]string{"n.md": "n\n"}, "n")

	fx.Engine.SetDiskUsagePct(90)
	_, err = fx.Engine.EnsureSnapshot(ctx, fx.Ref, defaultProject, sha2)
	if !errors.Is(err, repo.ErrDiskFull) || !errors.Is(err, repo.ErrDiskAdmission) {
		t.Fatalf("err = %v, want ErrDiskAdmission (an ErrDiskFull)", err)
	}
	if _, err := os.Stat(projectSnapshotDir(t, fx, sha2)); !os.IsNotExist(err) {
		t.Fatalf("refused snapshot published (err=%v)", err)
	}
	if _, err := fx.Engine.EnsureSkillsSnapshot(ctx, fx.Ref, sha2); !errors.Is(err, repo.ErrDiskAdmission) {
		t.Fatalf("skills err = %v, want ErrDiskAdmission", err)
	}
	if got, err := fx.Engine.EnsureSnapshot(ctx, fx.Ref, defaultProject, sha); err != nil || got != existing {
		t.Fatalf("existing snapshot at 90%% = %q, %v; want served", got, err)
	}

	// The gauge (the reaper's live UsagePct) wins over the last sweep's value.
	fx.Engine.SetDiskUsagePct(0)
	fx.Engine.SetUsageGauge(func() int { return 95 })
	if _, err := fx.Engine.EnsureSnapshot(ctx, fx.Ref, defaultProject, sha2); !errors.Is(err, repo.ErrDiskAdmission) {
		t.Fatalf("gauge 95: err = %v, want ErrDiskAdmission", err)
	}
	fx.Engine.SetUsageGauge(func() int { return 89 })
	if _, err := fx.Engine.EnsureSnapshot(ctx, fx.Ref, defaultProject, sha2); err != nil {
		t.Fatalf("gauge 89: %v", err)
	}
}

// Only <sha> leaves are ever trashed. The snapshot roots the agent's
// subPath mount pins are refused, as is anything outside them.
func TestTrashSnapshotOnlyTakesShaLeaves(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()
	sha := mustHead(t, fx, "")
	dir, err := fx.Engine.EnsureSnapshot(ctx, fx.Ref, defaultProject, sha)
	if err != nil {
		t.Fatal(err)
	}
	skills, err := fx.Engine.EnsureSkillsSnapshot(ctx, fx.Ref, sha)
	if err != nil {
		t.Fatal(err)
	}
	root := fx.Engine.Root()
	future := time.Now().Add(time.Hour)
	for _, bad := range []string{
		repo.SnapshotsDir(root),
		repo.ProjectSnapshotsDir(root),
		repo.SkillsSnapshotsDir(root),
		filepath.Dir(dir), // snapshots/projects/<p>
		filepath.Join(dir, "specs"),
		repo.ReposDir(root),
		filepath.Join(root, "snapshots", "projects", "..", "..", "repos"),
		"relative/" + sha,
	} {
		if _, err := fx.Engine.TrashSnapshot(bad, future); err == nil {
			t.Errorf("TrashSnapshot(%s) succeeded, want refused", bad)
		}
	}
	for _, leaf := range []string{dir, skills} {
		if trashed, err := fx.Engine.TrashSnapshot(leaf, future); err != nil || !trashed {
			t.Fatalf("TrashSnapshot(%s) = %v, %v", leaf, trashed, err)
		}
		if _, err := os.Stat(leaf); !os.IsNotExist(err) {
			t.Fatalf("%s still present (err=%v)", leaf, err)
		}
	}
	for _, d := range []string{repo.SnapshotsDir(root), repo.ProjectSnapshotsDir(root), repo.SkillsSnapshotsDir(root)} {
		if _, err := os.Stat(d); err != nil {
			t.Fatalf("snapshot root %s gone: %v", d, err)
		}
	}
}

// A lookup racing the reaper: the leaf it found is trashed before it marks
// the use. EnsureSnapshot must materialize it again, never answer a path that
// no longer exists.
func TestEnsureSnapshotRematerializesATrashedLeaf(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()
	sha := mustHead(t, fx, "")
	dir, err := fx.Engine.EnsureSnapshot(ctx, fx.Ref, defaultProject, sha)
	if err != nil {
		t.Fatal(err)
	}
	if trashed, err := fx.Engine.TrashSnapshot(dir, time.Now().Add(time.Hour)); err != nil || !trashed {
		t.Fatalf("TrashSnapshot = %v, %v", trashed, err)
	}
	again, err := fx.Engine.EnsureSnapshot(ctx, fx.Ref, defaultProject, sha)
	if err != nil || again != dir {
		t.Fatalf("EnsureSnapshot(after trash) = %q, %v", again, err)
	}
	assertSnapshotContent(t, dir)
}

// TrashSnapshot decides on the leaf's mtime read under the same lock a
// lookup marks its use under: a leaf used after the cutoff (a lookup that
// came after the reaper listed it) is kept.
func TestTrashSnapshotKeepsALeafUsedAfterTheCutoff(t *testing.T) {
	fx := NewFixture(t, seedFiles())
	ctx := context.Background()
	sha := mustHead(t, fx, "")
	dir, err := fx.Engine.EnsureSnapshot(ctx, fx.Ref, defaultProject, sha)
	if err != nil {
		t.Fatal(err)
	}
	cutoff := time.Now().Add(-time.Hour)
	old := cutoff.Add(-time.Hour)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatal(err)
	}
	// The reaper listed it as unused; a lookup now reuses it.
	if _, err := fx.Engine.EnsureSnapshot(ctx, fx.Ref, defaultProject, sha); err != nil {
		t.Fatal(err)
	}
	trashed, err := fx.Engine.TrashSnapshot(dir, cutoff)
	if err != nil || trashed {
		t.Fatalf("TrashSnapshot = %v, %v; want kept", trashed, err)
	}
	assertSnapshotContent(t, dir)
}
