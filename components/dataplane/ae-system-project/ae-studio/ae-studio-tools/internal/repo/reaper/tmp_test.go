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
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

func TestReclaimTmpPurgesAgedSkipsAskpass(t *testing.T) {
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1 << 30, TrashMaxAge: time.Hour})
	tmp := repo.TmpDir(root)

	aged := filepath.Join(tmp, "clone-old")
	mkFile(t, filepath.Join(aged, "x"), []byte("12345678"))
	chtimes(t, aged, time.Now().Add(-2*time.Hour))

	fresh := filepath.Join(tmp, "clone-fresh")
	if err := os.MkdirAll(fresh, 0o755); err != nil {
		t.Fatal(err)
	}
	askpass := filepath.Join(tmp, "askpass.sh")        // written by repo.New
	chtimes(t, askpass, time.Now().Add(-48*time.Hour)) // aged but protected

	if err := r.reclaimTmp(context.Background()); err != nil {
		t.Fatalf("reclaimTmp: %v", err)
	}
	mustNotExist(t, aged)
	mustExist(t, fresh)
	mustExist(t, askpass)
}

// A failed purge logs the error's class, never its text: the path it names
// can be anything a clone left behind.
func TestReclaimTmpPurgeFailureLogsClassNotText(t *testing.T) {
	root := t.TempDir()
	r := newReaperForTest(t, root, Config{Budget: 1 << 30, TrashMaxAge: time.Hour})
	stuck := filepath.Join(repo.TmpDir(root), "clone-planted-token-0123456789abcdef")
	mkFile(t, filepath.Join(stuck, "x"), []byte("1"))
	// The entry's own dir is read-only, so its child cannot be removed.
	if err := os.Chmod(stuck, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(stuck, 0o755) })
	chtimes(t, stuck, time.Now().Add(-2*time.Hour))

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	if err := r.reclaimTmp(context.Background()); err != nil {
		t.Fatalf("reclaimTmp: %v", err)
	}
	logs := buf.String()
	if !strings.Contains(logs, `"msg":"reaper.tmp_purge_failed"`) || !strings.Contains(logs, `"class":"fs"`) {
		t.Fatalf("want a reaper.tmp_purge_failed line with class fs, got %s", logs)
	}
	if strings.Contains(logs, "planted-token") || strings.Contains(logs, `"error"`) {
		t.Fatalf("raw error text reached the log: %s", logs)
	}
}
