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
	"path/filepath"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
)

// mkRecording lays down runs/<org>/<cycle>/{events.1.ndjson,state.json} with
// one payload of the given size, then pins the events file's mtime to `when` —
// which is what the pass ages a recording from (a recording is appended to for
// the whole run, so the DIRECTORY's mtime would date it from the moment the run
// started rather than the moment it last wrote).
func mkRecording(t *testing.T, root, org, cycle string, size int, when time.Time) string {
	t.Helper()
	dir := filepath.Join(gitfs.RunsDir(root), org, cycle)
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
	cfg := testCfg()
	cfg.RecordingMaxAge = 30 * 24 * time.Hour
	r, root := newSyntheticReaper(t, cfg, nil)
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
	cfg := testCfg()
	cfg.RecordingMaxAge = 30 * 24 * time.Hour
	r, root := newSyntheticReaper(t, cfg, nil)
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
	cfg := testCfg()
	cfg.RecordingMaxAge = 30 * 24 * time.Hour
	// Two recordings' worth (each holds two files), so a third puts the org over.
	cfg.OrgQuotaBytes = 4 * payload
	r, root := newSyntheticReaper(t, cfg, nil)
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

	r, _ := newSyntheticReaper(t, testCfg(), nil)
	if err := r.reapRecordings(context.Background()); err != nil {
		t.Fatalf("reapRecordings on a volume with no runs/: %v", err)
	}
}

// TestSweepRunsRecordingRetention proves the pass is actually wired into the
// sweep, not merely callable: an unregistered pass is a retention policy that
// never runs.
func TestSweepRunsRecordingRetention(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	cfg := testCfg()
	cfg.RecordingMaxAge = 30 * 24 * time.Hour
	r, root := newSyntheticReaper(t, cfg, staticLister{})
	r.now = func() time.Time { return now }
	r.diskUsage = func(string) (uint64, uint64, uint64, uint64, error) {
		return 1000, 900, 1000, 1000, nil // well under the watermark
	}

	aged := mkRecording(t, root, "acme", "cycle-old", 128, now.Add(-40*24*time.Hour))
	r.Sweep(context.Background())
	mustNotExist(t, aged)
}
