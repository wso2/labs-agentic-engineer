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
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// TestMain silences the reaper's log lines; tests that assert on a line
// install their own handler.
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

// newReaperForTest builds a reaper over an engine rooted at root. The node
// statfs seam is pinned quiet (0 of 100 used) so the host disk of whoever runs
// the tests never moves a budget assertion; tests that exercise statfs set it.
func newReaperForTest(t *testing.T, root string, cfg Config) *Reaper {
	t.Helper()
	eng, _, err := repo.New(root, nil)
	if err != nil {
		t.Fatalf("repo.New: %v", err)
	}
	r := New(eng, cfg)
	r.statfs = func(string) (used, total uint64, err error) { return 0, 100, nil }
	return r
}

// writeMirror lays out repos/<owner>/<repo>/git (ownerRepo is "owner/repo")
// by hand with one size-byte payload
// file, then pins the git dir's mtime
// (the LRU signal) to lastUse. It has no HEAD, so maintenance passes it by.
// Returns the repo dir.
func writeMirror(t *testing.T, root, ownerRepo string, size int, lastUse time.Time) string {
	t.Helper()
	dir := filepath.Join(repo.ReposDir(root), filepath.FromSlash(ownerRepo))
	gitDir := repo.GitSubdir(dir)
	mkFile(t, filepath.Join(gitDir, "payload"), bytes.Repeat([]byte("x"), size))
	chtimes(t, gitDir, lastUse)
	return dir
}

const kib = 1024

// budgetAt returns the budget at which the root's current block usage
// (repo.DirBytes, what the reaper measures) is pct percent.
func budgetAt(t *testing.T, root string, pct int64) int64 {
	t.Helper()
	used := repo.DirBytes(root)
	if used <= 0 {
		t.Fatalf("budgetAt: root measures %d bytes", used)
	}
	return used * 100 / pct
}

// mkFile writes body at path (parents created).
func mkFile(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func chtimes(t *testing.T, path string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

// trashEntry creates trash/<name>/payload of size bytes.
func trashEntry(t *testing.T, root, name string, size int) string {
	t.Helper()
	dir := filepath.Join(repo.TrashDir(root), name)
	mkFile(t, filepath.Join(dir, "payload"), bytes.Repeat([]byte("x"), size))
	return dir
}

// trashName mints a trash id in the engine's "%016x-<rand>" scheme, stamped at.
func trashName(at time.Time, suffix string) string {
	return fmt.Sprintf("%016x-%s", uint64(at.UnixNano()), suffix)
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be gone, stat err=%v", path, err)
	}
}

func gone(path string) bool {
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}
