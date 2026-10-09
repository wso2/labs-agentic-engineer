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
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// TestTrashThenPurgeKeepsOpenFds proves the two-phase delete's core safety
// property: a reader holding an open fd survives BOTH phases (TrashRepo's
// rename and reclaimTrash's RemoveAll) by POSIX inode semantics.
func TestTrashThenPurgeKeepsOpenFds(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1 << 30, TrashMaxAge: time.Nanosecond})
	dir := writeMirror(t, root, "acme/r", 64, time.Now())
	f, err := os.Open(filepath.Join(repo.GitSubdir(dir), "payload"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	if err := r.engine.TrashRepo(ctx, repo.RepoRef{Owner: "acme", Repo: "r"}); err != nil {
		t.Fatalf("TrashRepo: %v", err)
	}
	mustNotExist(t, dir)
	if err := r.reclaimTrash(ctx); err != nil {
		t.Fatalf("reclaimTrash: %v", err)
	}
	if entries, _ := os.ReadDir(repo.TrashDir(root)); len(entries) != 0 {
		t.Fatalf("trash not purged: %d entries", len(entries))
	}
	content, err := io.ReadAll(f)
	if err != nil || len(content) != 64 {
		t.Fatalf("read after purge = %d bytes, %v; want 64", len(content), err)
	}
}

// TestReclaimTrashHonorsMaxAge: age comes from the name-embedded rename
// timestamp, NOT the dir mtime (rename preserves mtime, so a dormant subtree
// would otherwise purge instantly).
func TestReclaimTrashHonorsMaxAge(t *testing.T) {
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1 << 30, TrashMaxAge: 24 * time.Hour})
	old := trashEntry(t, root, trashName(time.Now().Add(-48*time.Hour), "dead"), 8)
	fresh := trashEntry(t, root, trashName(time.Now(), "beef"), 8)
	chtimes(t, fresh, time.Now().Add(-72*time.Hour))

	if err := r.reclaimTrash(context.Background()); err != nil {
		t.Fatalf("reclaimTrash: %v", err)
	}
	mustNotExist(t, old)
	mustExist(t, fresh)
}

// TestTrashedAtFallsBackToMtime: a foreign (unparseable) entry name derives
// its age from mtime.
func TestTrashedAtFallsBackToMtime(t *testing.T) {
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1 << 30, TrashMaxAge: 24 * time.Hour})
	debris := trashEntry(t, root, "debris", 4)
	chtimes(t, debris, time.Now().Add(-48*time.Hour))

	if err := r.reclaimTrash(context.Background()); err != nil {
		t.Fatalf("reclaimTrash: %v", err)
	}
	mustNotExist(t, debris)
}
