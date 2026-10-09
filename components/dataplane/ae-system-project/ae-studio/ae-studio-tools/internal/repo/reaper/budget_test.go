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
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

func TestBudget_LRUEvictsDownTo70(t *testing.T) {
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1})
	writeMirror(t, root, "acme/old", 500*kib, time.Now().Add(-2*time.Hour))
	writeMirror(t, root, "acme/new", 400*kib, time.Now())
	r.cfg.Budget = budgetAt(t, root, 90)
	evicted, err := r.sweepOnce(context.Background()) // usage 90% ≥ 85%
	if err != nil {
		t.Fatalf("sweepOnce: %v", err)
	}
	if want := []string{"acme/old"}; !reflect.DeepEqual(evicted, want) {
		t.Fatalf("evicted = %v, want %v (least recently used first)", evicted, want)
	}
	if got := r.UsagePct(); got > 70 {
		t.Fatalf("UsagePct = %d after eviction, want <= 70", got)
	}
}

func TestUsagePct_TakesHigherOfBudgetAndStatfs(t *testing.T) {
	r := newReaperForTest(t, t.TempDir(), Config{Budget: 1 << 30})
	r.statfs = func(string) (used, total uint64, err error) { return 95, 100, nil }
	if got := r.UsagePct(); got != 95 {
		t.Fatalf("UsagePct = %d, want 95 (node statfs above the budget share)", got)
	}
}

func TestNoOrphanPass(t *testing.T) {
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1 << 30})
	writeMirror(t, root, "acme/unlisted", 10, time.Now())
	if _, err := r.sweepOnce(context.Background()); err != nil {
		t.Fatalf("sweepOnce: %v", err)
	}
	// Nothing lists live repos; LRU and rolls clean up.
	mustExist(t, filepath.Join(root, "repos/acme/unlisted"))
}

// Under the high mark nothing is evicted and young trash is left for the
// age-gated pass.
func TestBudget_UnderHighEvictsNothing(t *testing.T) {
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1})
	writeMirror(t, root, "acme/r", 500*kib, time.Now().Add(-48*time.Hour))
	young := trashEntry(t, root, trashName(time.Now(), "young"), 100*kib)
	r.cfg.Budget = budgetAt(t, root, 50)
	evicted, err := r.sweepOnce(context.Background())
	if err != nil || len(evicted) != 0 {
		t.Fatalf("sweepOnce = %v, %v; want no eviction", evicted, err)
	}
	mustExist(t, young)
}

// Over the high mark, trash is purged before anything is evicted: if that
// alone brings usage under the mark, every mirror survives.
func TestBudget_PurgesTrashBeforeEvicting(t *testing.T) {
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1})
	m := writeMirror(t, root, "acme/r", 500*kib, time.Now().Add(-48*time.Hour))
	young := trashEntry(t, root, trashName(time.Now(), "young"), 1400*kib) // fresh: age-gated pass keeps it
	r.cfg.Budget = budgetAt(t, root, 100)                                  // trash purge alone drops it to ~26%
	evicted, err := r.sweepOnce(context.Background())
	if err != nil || len(evicted) != 0 {
		t.Fatalf("sweepOnce = %v, %v; want trash purge only", evicted, err)
	}
	mustNotExist(t, young)
	mustExist(t, m)
}

// A mirror whose repo.lock is held (in use) is skipped, never waited on; the
// next LRU candidate goes instead.
func TestBudget_NeverEvictsLockedMirror(t *testing.T) {
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1})
	busy := writeMirror(t, root, "acme/busy", 500*kib, time.Now().Add(-3*time.Hour))
	next := writeMirror(t, root, "acme/next", 400*kib, time.Now().Add(-1*time.Hour))

	lock, err := os.OpenFile(filepath.Join(busy, "repo.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("open lock: %v", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_SH); err != nil {
		t.Fatalf("flock: %v", err)
	}
	r.cfg.Budget = budgetAt(t, root, 90)

	evicted, err := r.sweepOnce(context.Background())
	if err != nil {
		t.Fatalf("sweepOnce: %v", err)
	}
	if want := []string{"acme/next"}; !reflect.DeepEqual(evicted, want) {
		t.Fatalf("evicted = %v, want %v", evicted, want)
	}
	mustExist(t, busy)
	mustNotExist(t, next)
}

// Eviction walks real directories under repos/ only: a symlink planted in the
// tree is never followed, so nothing outside the studio-data root is touched.
func TestBudget_StaysInsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1})
	foreign := writeMirror(t, outside, "evil/r", 900*kib, time.Now().Add(-48*time.Hour))
	if err := os.Symlink(filepath.Join(repo.ReposDir(outside), "evil"), filepath.Join(repo.ReposDir(root), "evil")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	writeMirror(t, root, "acme/r", 900*kib, time.Now())
	r.cfg.Budget = budgetAt(t, root, 90)

	if _, err := r.sweepOnce(context.Background()); err != nil {
		t.Fatalf("sweepOnce: %v", err)
	}
	mustExist(t, foreign)
}

// The bytes the engine records after a clone count toward usage between
// sweeps, so admission is not blind until the next du.
func TestUsagePct_CountsClonesSinceSweep(t *testing.T) {
	root := t.TempDir()
	const budget = 1 << 30
	r := newReaperForTest(t, root, Config{Budget: budget})
	if _, err := r.sweepOnce(context.Background()); err != nil {
		t.Fatalf("sweepOnce: %v", err)
	}
	before := r.UsagePct()
	r.engine.AddUsage(budget / 2)
	if got := r.UsagePct(); got != before+50 {
		t.Fatalf("UsagePct = %d after a half-budget clone, want %d", got, before+50)
	}
}

// Each sweep publishes UsagePct onto the engine, which DiskFullError reports.
func TestSweep_PublishesUsageToEngine(t *testing.T) {
	r := newReaperForTest(t, t.TempDir(), Config{Budget: 1 << 30})
	r.statfs = func(string) (used, total uint64, err error) { return 42, 100, nil }
	if _, err := r.sweepOnce(context.Background()); err != nil {
		t.Fatalf("sweepOnce: %v", err)
	}
	if got := r.engine.DiskUsagePct(); got != 42 {
		t.Fatalf("engine DiskUsagePct = %d, want 42", got)
	}
}

func TestNew_PanicsOnNonPositiveBudget(t *testing.T) {
	eng, _, err := repo.New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic for Budget 0")
		}
	}()
	New(eng, Config{})
}
