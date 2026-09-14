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
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestShouldMaintainGate(t *testing.T) {
	if !shouldMaintain(1001, 0) {
		t.Fatal("loose>1000 must maintain")
	}
	if !shouldMaintain(0, 21) {
		t.Fatal("packs>20 must maintain")
	}
	if shouldMaintain(10, 2) {
		t.Fatal("quiet repo must skip")
	}
}

func TestMaintainReposRepacksLooseHeavyMirror(t *testing.T) {
	r, root := newSyntheticReaper(t, testCfg(), staticLister([]RepoCoordinate{{
		OrgID: "o1", ProjectID: "p1", WorkspaceSlug: "r1",
	}}))
	r.diskUsage = fakeDisk(1000, 900)
	slug := mkSlugDir(t, root, "o1", "p1", "r1")
	gitDir := filepath.Join(slug, "git")
	initSyntheticMirror(t, gitDir)
	// Seed >1000 reachable loose objects. hash-object alone leaves blobs
	// unreachable, and `repack -ad` only packs/drops reachable objects —
	// commit them through a temporary work tree so repack can reclaim.
	work := t.TempDir()
	for i := 0; i < 1101; i++ {
		name := filepath.Join(work, "f"+strconv.Itoa(i))
		if err := os.WriteFile(name, []byte("blob-"+strconv.Itoa(i)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGitWorktree(t, gitDir, work, "add", ".")
	runGitWorktree(t, gitDir, work, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "seed")
	before := countLoose(t, gitDir)
	if before <= 1000 {
		t.Fatalf("setup: want >1000 loose, got %d", before)
	}
	// Touch repo.lock so MaintainMirror lock path exists.
	if _, err := os.OpenFile(filepath.Join(slug, "repo.lock"), os.O_CREATE|os.O_RDWR, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.maintainRepos(context.Background()); err != nil {
		t.Fatalf("maintain: %v", err)
	}
	after := countLoose(t, gitDir)
	if after >= before {
		t.Fatalf("loose objects did not drop: before=%d after=%d", before, after)
	}
}

// TestMaintainReposSlowGitCompletes proves R4: the ~2s budget bounds lock
// acquisition only. A repack that takes longer than 2s must still finish —
// wrapping MaintainMirror in the lock timeout would SIGKILL mid-repack.
func TestMaintainReposSlowGitCompletes(t *testing.T) {
	r, root := newSyntheticReaper(t, testCfg(), staticLister([]RepoCoordinate{{
		OrgID: "o1", ProjectID: "p1", WorkspaceSlug: "r1",
	}}))
	r.diskUsage = fakeDisk(1000, 900)
	slug := mkSlugDir(t, root, "o1", "p1", "r1")
	gitDir := filepath.Join(slug, "git")
	initSyntheticMirror(t, gitDir)
	work := t.TempDir()
	for i := 0; i < 1101; i++ {
		name := filepath.Join(work, "f"+strconv.Itoa(i))
		if err := os.WriteFile(name, []byte("slow-"+strconv.Itoa(i)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGitWorktree(t, gitDir, work, "add", ".")
	runGitWorktree(t, gitDir, work, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "seed")
	if _, err := os.OpenFile(filepath.Join(slug, "repo.lock"), os.O_CREATE|os.O_RDWR, 0o644); err != nil {
		t.Fatal(err)
	}
	installSlowRepackGit(t)

	before := countLoose(t, gitDir)
	if before <= 1000 {
		t.Fatalf("setup: want >1000 loose, got %d", before)
	}
	if _, err := r.maintainRepos(context.Background()); err != nil {
		t.Fatalf("maintain: %v", err)
	}
	after := countLoose(t, gitDir)
	if after >= before {
		t.Fatalf("slow maintain must complete (not SIGKILL at 2s): loose before=%d after=%d", before, after)
	}
}

// installSlowRepackGit puts a PATH wrapper ahead of real git that sleeps
// >2s on `repack` so a lock-budget wrapping the whole MaintainMirror call
// would cancel the child mid-work.
func installSlowRepackGit(t *testing.T) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "git")
	script := fmt.Sprintf(`#!/bin/sh
for a in "$@"; do
  if [ "$a" = "repack" ]; then
    sleep 2.5
    break
  fi
done
exec %q "$@"
`, real)
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestMaintainReposSkipsBusyLock(t *testing.T) {
	r, root := newSyntheticReaper(t, testCfg(), staticLister([]RepoCoordinate{{
		OrgID: "o1", ProjectID: "p1", WorkspaceSlug: "r1",
	}}))
	r.diskUsage = fakeDisk(1000, 900)
	slug := mkSlugDir(t, root, "o1", "p1", "r1")
	gitDir := filepath.Join(slug, "git")
	initSyntheticMirror(t, gitDir)
	work := t.TempDir()
	for i := 0; i < 1101; i++ {
		name := filepath.Join(work, "f"+strconv.Itoa(i))
		if err := os.WriteFile(name, []byte("busy-"+strconv.Itoa(i)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGitWorktree(t, gitDir, work, "add", ".")
	runGitWorktree(t, gitDir, work, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "seed")
	lock, err := os.OpenFile(filepath.Join(slug, "repo.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := r.maintainRepos(context.Background()); err != nil {
		t.Fatalf("maintain: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("busy lock should skip within ~2s timeout, took %s", time.Since(start))
	}
	// Still locked — objects unrepacked is fine; the point is skip, not hang.
}

// initSyntheticMirror creates the bare mirror a maintain test operates on.
// The config stamp is load-bearing, not hygiene: `git commit` (and `git
// fetch`) hand the repo to `git maintenance run --auto --detach`, which
// double-forks and outlives the command that spawned it. That detached repack
// keeps creating objects/pack/tmp_pack_* after the seeding commit has already
// exited, so it races t.TempDir's RemoveAll at test end — RemoveAll lists
// objects/, unlinks what it saw, then rmdir's it and gets ENOTEMPTY from a
// pack that appeared in between. The knob has to be maintenance.auto: git's
// auto strategy is geometric-repack, which never consults gc.auto, so
// gc.auto=0 leaves the detached repack running. Tests seed through porcelain
// on a repo they created with `git init --bare`, so they must disable it
// themselves.
func initSyntheticMirror(t *testing.T, gitDir string) {
	t.Helper()
	runGit(t, "", "init", "--bare", gitDir)
	runGit(t, gitDir, "config", "maintenance.auto", "false")
}

func countLoose(t *testing.T, gitDir string) int {
	t.Helper()
	out, err := exec.Command("git", "--git-dir="+gitDir, "count-objects", "-v").Output()
	if err != nil {
		t.Fatalf("count-objects: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "count: ") {
			n, _ := strconv.Atoi(strings.TrimPrefix(line, "count: "))
			return n
		}
	}
	t.Fatal("no count: line")
	return 0
}

func runGit(t *testing.T, gitDir string, args ...string) {
	t.Helper()
	full := append([]string{"git"}, args...)
	if gitDir != "" {
		full = append([]string{"git", "--git-dir=" + gitDir}, args...)
	}
	cmd := exec.Command(full[0], full[1:]...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func runGitWorktree(t *testing.T, gitDir, work string, args ...string) {
	t.Helper()
	full := append([]string{"--git-dir=" + gitDir, "--work-tree=" + work}, args...)
	cmd := exec.Command("git", full...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
