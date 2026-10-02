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

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
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
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1 << 30})
	gitDir := looseHeavyMirror(t, root, "blob")
	before := countLoose(t, gitDir)
	if _, err := r.maintainRepos(context.Background()); err != nil {
		t.Fatalf("maintain: %v", err)
	}
	if after := countLoose(t, gitDir); after >= before {
		t.Fatalf("loose objects did not drop: before=%d after=%d", before, after)
	}
}

// TestMaintainReposSlowGitCompletes: the ~2s budget bounds lock acquisition
// only. A repack that takes longer than 2s must still finish.
func TestMaintainReposSlowGitCompletes(t *testing.T) {
	if testing.Short() {
		t.Skip("sleeps 2.5s inside a git wrapper")
	}
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1 << 30})
	gitDir := looseHeavyMirror(t, root, "slow")
	installSlowRepackGit(t)
	before := countLoose(t, gitDir)
	if _, err := r.maintainRepos(context.Background()); err != nil {
		t.Fatalf("maintain: %v", err)
	}
	if after := countLoose(t, gitDir); after >= before {
		t.Fatalf("slow maintain must complete (not SIGKILL at 2s): loose before=%d after=%d", before, after)
	}
}

func TestMaintainReposSkipsBusyLock(t *testing.T) {
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1 << 30})
	gitDir := looseHeavyMirror(t, root, "busy")
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(gitDir), "repo.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	n, err := r.maintainRepos(context.Background())
	if err != nil {
		t.Fatalf("maintain: %v", err)
	}
	if n != 0 {
		t.Fatalf("maintained %d mirrors while the lock was held", n)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("busy lock should skip within ~2s, took %s", time.Since(start))
	}
}

// looseHeavyMirror creates repos/o1/p1/r1/git holding >1000 reachable loose
// objects (committed through a temporary work tree so repack can reclaim
// them) and returns the git dir.
func looseHeavyMirror(t *testing.T, root, prefix string) string {
	t.Helper()
	gitDir := repo.GitSubdir(filepath.Join(repo.ReposDir(root), "o1", "p1", "r1"))
	initSyntheticMirror(t, gitDir)
	work := t.TempDir()
	for i := 0; i < 1101; i++ {
		if err := os.WriteFile(filepath.Join(work, "f"+strconv.Itoa(i)), []byte(prefix+"-"+strconv.Itoa(i)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, gitDir, "--work-tree="+work, "add", ".")
	runGit(t, gitDir, "--work-tree="+work, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "seed")
	if n := countLoose(t, gitDir); n <= 1000 {
		t.Fatalf("setup: want >1000 loose, got %d", n)
	}
	return gitDir
}

// installSlowRepackGit puts a PATH wrapper ahead of real git that sleeps
// >2s on `repack`, so a lock budget wrapping the whole MaintainMirror call
// would cancel the child mid-work.
func installSlowRepackGit(t *testing.T) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
for a in "$@"; do
  if [ "$a" = "repack" ]; then
    sleep 2.5
    break
  fi
done
exec %q "$@"
`, real)
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// initSyntheticMirror creates a bare repo with maintenance.auto off: the
// seeding `git commit` would otherwise spawn a detached repack that races
// t.TempDir's cleanup (the engine forces the same rule on its own children).
func initSyntheticMirror(t *testing.T, gitDir string) {
	t.Helper()
	if out, err := exec.Command("git", "init", "--bare", gitDir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	runGit(t, gitDir, "config", "maintenance.auto", "false")
}

func countLoose(t *testing.T, gitDir string) int {
	t.Helper()
	out, err := exec.Command("git", "--git-dir="+gitDir, "count-objects", "-v").Output()
	if err != nil {
		t.Fatalf("count-objects: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(line, "count: "); ok {
			n, _ := strconv.Atoi(v)
			return n
		}
	}
	t.Fatal("no count: line")
	return 0
}

func runGit(t *testing.T, gitDir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"--git-dir=" + gitDir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
