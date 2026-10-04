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

package codingagent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/config"
)

// mkRecording lays down runs/<org>/<cycle>/{events.1.ndjson,state.json} with
// one payload of the given size, then pins the events file's mtime to `when` —
// which is what the pass ages a recording from (a recording is appended to for
// the whole run, so the DIRECTORY's mtime would date it from the moment the run
// started rather than the moment it last wrote).
func mkRecording(t *testing.T, root, org, cycle string, size int, when time.Time) string {
	t.Helper()
	dir := filepath.Join(root, "runs", org, cycle)
	mkFile(t, filepath.Join(dir, "events.1.ndjson"), size)
	mkFile(t, filepath.Join(dir, "state.json"), 64)
	chtimes(t, filepath.Join(dir, "events.1.ndjson"), when)
	chtimes(t, filepath.Join(dir, "state.json"), when)
	chtimes(t, dir, when)
	return dir
}

// Every test below freezes the clock. The window this pass enforces is thirty
// days: a test cannot wait one out, and one that back-dates files far enough
// instead would be asserting os.Chtimes rather than the rule.

// TestReapRecordings_RemovesAgedAndKeepsFresh is the retention window itself.
// It is measured in DAYS, unlike every other age gate on this mount, because
// this is the one tree that is not a rebuildable cache: deleting a recording
// destroys the only copy of a run's feed there has ever been.
func TestReapRecordings_RemovesAgedAndKeepsFresh(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	cfg := config.WorkspaceConfig{}
	cfg.RecordingMaxAge = 30 * 24 * time.Hour
	r, root := newRetention(t, cfg)
	r.now = func() time.Time { return now }

	aged := mkRecording(t, root, "acme", "cycle-old", 128, now.Add(-31*24*time.Hour))
	edge := mkRecording(t, root, "acme", "cycle-edge", 128, now.Add(-29*24*time.Hour))
	fresh := mkRecording(t, root, "acme", "cycle-new", 128, now.Add(-time.Hour))

	if err := r.reapRecordings(context.Background()); err != nil {
		t.Fatalf("reapRecordings: %v", err)
	}
	mustNotExist(t, aged)
	mustExist(t, edge)
	mustExist(t, fresh)
}

// TestReapRecordings_AgesFromTheNEWESTFileInTheDirectory pins why the pass does
// not read the directory's own mtime. A directory mtime only moves when an
// entry is created, so a long-running recording — appended to for an hour after
// its directory appeared — would otherwise be aged from the moment it started.
func TestReapRecordings_AgesFromTheNEWESTFileInTheDirectory(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	cfg := config.WorkspaceConfig{}
	cfg.RecordingMaxAge = 30 * 24 * time.Hour
	r, root := newRetention(t, cfg)
	r.now = func() time.Time { return now }

	dir := mkRecording(t, root, "acme", "cycle-long", 128, now.Add(-40*24*time.Hour))
	// The recording was still being written yesterday, even though the directory
	// (and its first file) date from long before the window.
	chtimes(t, filepath.Join(dir, "events.1.ndjson"), now.Add(-24*time.Hour))

	if err := r.reapRecordings(context.Background()); err != nil {
		t.Fatalf("reapRecordings: %v", err)
	}
	mustExist(t, dir)
}

// TestReapRecordings_QuotaEvictsOldestFirst pins the backstop. The age window is
// the mechanism; this is what holds an org that records faster than the window
// clears. Whole recordings, oldest first — never a partial file, which would be
// a feed with no way to say where it was cut.
func TestReapRecordings_QuotaEvictsOldestFirst(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	payload := blockPayloadSize(t) // what duDir charges for one mkFile
	cfg := config.WorkspaceConfig{}
	cfg.RecordingMaxAge = 30 * 24 * time.Hour
	// Two recordings' worth (each holds two files), so a third puts the org over.
	cfg.OrgQuotaBytes = 4 * payload
	r, root := newRetention(t, cfg)
	r.now = func() time.Time { return now }

	oldest := mkRecording(t, root, "acme", "cycle-1", 64, now.Add(-72*time.Hour))
	middle := mkRecording(t, root, "acme", "cycle-2", 64, now.Add(-48*time.Hour))
	newest := mkRecording(t, root, "acme", "cycle-3", 64, now.Add(-24*time.Hour))
	// Another org is untouched — the quota is per org.
	other := mkRecording(t, root, "globex", "cycle-9", 64, now.Add(-72*time.Hour))

	if err := r.reapRecordings(context.Background()); err != nil {
		t.Fatalf("reapRecordings: %v", err)
	}
	mustNotExist(t, oldest)
	mustExist(t, middle)
	mustExist(t, newest)
	mustExist(t, other)
}

// TestReapRecordings_NoRunsTreeIsNotAnError pins that a volume nothing has
// recorded on yet is an ordinary state, not a failed pass — the sweep would
// otherwise log a warning on every tick of a fresh deployment.
func TestReapRecordings_NoRunsTreeIsNotAnError(t *testing.T) {
	t.Parallel()

	r, _ := newRetention(t, config.WorkspaceConfig{})
	if err := r.reapRecordings(context.Background()); err != nil {
		t.Fatalf("reapRecordings on a volume with no runs/: %v", err)
	}
}

func newRetention(t *testing.T, cfg config.WorkspaceConfig) (*RecordingRetention, string) {
	t.Helper()
	root := t.TempDir()
	return NewRecordingRetention(root, cfg), root
}

func mkFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func chtimes(t *testing.T, path string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

// blockPayloadSize returns the bytes duDir charges for one mkFile payload
// (allocated blocks, not apparent size).
func blockPayloadSize(t *testing.T) int64 {
	t.Helper()
	dir := t.TempDir()
	mkFile(t, filepath.Join(dir, "probe"), 1)
	return duDir(dir)
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

func TestNewRecordingRetention_Defaults(t *testing.T) {
	t.Parallel()
	r := NewRecordingRetention("/w", config.WorkspaceConfig{})
	if r.maxAge != 30*24*time.Hour {
		t.Errorf("maxAge = %v, want 30 days", r.maxAge)
	}
	if r.interval != 5*time.Minute {
		t.Errorf("interval = %v, want 5m", r.interval)
	}
	r = NewRecordingRetention("/w", config.WorkspaceConfig{
		RecordingMaxAge: time.Hour, ReapInterval: time.Second, OrgQuotaBytes: 7,
	})
	if r.maxAge != time.Hour || r.interval != time.Second || r.quota != 7 {
		t.Errorf("configured values not honoured: %+v", r)
	}
}

// Run sweeps at start (aged goes, fresh stays, quota holds) and stops on cancel.
func TestRecordingRetention_RunSweepsAndStopsOnCancel(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	payload := blockPayloadSize(t)
	cfg := config.WorkspaceConfig{
		RecordingMaxAge: 30 * 24 * time.Hour,
		OrgQuotaBytes:   4 * payload,
		ReapInterval:    time.Hour,
	}
	r, root := newRetention(t, cfg)
	r.now = func() time.Time { return now }

	aged := mkRecording(t, root, "acme", "cycle-old", 64, now.Add(-40*24*time.Hour))
	q1 := mkRecording(t, root, "globex", "c1", 64, now.Add(-72*time.Hour))
	q2 := mkRecording(t, root, "globex", "c2", 64, now.Add(-48*time.Hour))
	q3 := mkRecording(t, root, "globex", "c3", 64, now.Add(-24*time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	deadline := time.After(10 * time.Second)
	for {
		if _, err := os.Stat(aged); os.IsNotExist(err) {
			if _, err := os.Stat(q1); os.IsNotExist(err) {
				break
			}
		}
		select {
		case <-deadline:
			t.Fatal("startup sweep did not remove the aged / over-quota recordings")
		case <-time.After(10 * time.Millisecond):
		}
	}
	mustExist(t, q2)
	mustExist(t, q3)

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}
