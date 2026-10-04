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

// recordings.go — retention for the coding-agent run recordings under
// <root>/runs/<orgId>/<cycleId>.
//
// This tree is the one part of the workspace mount that is NOT a rebuildable
// cache. Every other pass reclaims something git or the database can produce
// again; a run's feed existed while its pod did and nowhere else, so deleting a
// recording destroys the only copy. That is the whole reason its window is
// measured in DAYS while snapshots and trash are measured in hours, and why
// this pass will not evict a recording for disk pressure the way quota/LRU
// evicts a mirror.
//
// Two rules, in order:
//
//  1. AGE — a cycle directory older than RecordingMaxAge (30 days) goes. Age is
//     taken from the NEWEST file in the directory, not from the directory's own
//     mtime: a recording is appended to for the whole run, and a directory
//     mtime only moves when an entry is created, so a long run would otherwise
//     be aged from the moment it started.
//  2. QUOTA — per org, oldest first, until runs/<orgId> is back under
//     OrgQuotaBytes. This is the backstop, not the mechanism: recordings are
//     small (a 55-minute run wrote about 300KB) and the age window is what
//     ordinarily bounds the tree.
//
// It holds no leader lock (it left the workspace reaper, which elects one
// replica for its global passes): both rules are idempotent and derive entirely
// from what is on disk, so replicas racing reach the same answer. aep-api runs
// single-replica anyway — the volume is ReadWriteOnce. Phase 5 deletes the
// recordings tree and this file with it.

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/wso2/aep/aep-api/internal/config"
)

// recordingCandidate is one cycle's recording directory, as this pass sees it.
type recordingCandidate struct {
	path    string
	org     string
	cycle   string
	newest  time.Time
	sizeAll int64
}

// RecordingRetention is the background watcher that applies the age and quota
// rules to the run recordings under <root>/runs.
type RecordingRetention struct {
	root     string
	maxAge   time.Duration
	quota    int64
	interval time.Duration
	now      func() time.Time
}

// NewRecordingRetention builds the watcher over the workspace mount root, using
// cfg.ReapInterval, cfg.RecordingMaxAge and cfg.OrgQuotaBytes.
func NewRecordingRetention(root string, cfg config.WorkspaceConfig) *RecordingRetention {
	maxAge, interval := cfg.RecordingMaxAge, cfg.ReapInterval
	if maxAge <= 0 {
		maxAge = 30 * 24 * time.Hour
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return &RecordingRetention{
		root:     root,
		maxAge:   maxAge,
		quota:    cfg.OrgQuotaBytes,
		interval: interval,
		now:      time.Now,
	}
}

// Run sweeps once at start (a pod restarting into a full volume must not wait a
// whole interval) and then on every tick until ctx is canceled.
func (r *RecordingRetention) Run(ctx context.Context) {
	r.sweep(ctx)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.sweep(ctx)
		}
	}
}

// sweep runs one retention pass; a failure is logged and the next tick retries.
func (r *RecordingRetention) sweep(ctx context.Context) {
	if err := r.reapRecordings(ctx); err != nil {
		slog.WarnContext(ctx, "recording retention: pass failed", "error", err)
	}
}

// reapRecordings purges aged recordings and then holds each org under quota.
func (r *RecordingRetention) reapRecordings(ctx context.Context) error {
	runsDir := filepath.Join(r.root, "runs")
	orgs, err := os.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing has been recorded on this volume yet
		}
		return err
	}
	now := r.now()
	maxAge := r.maxAge
	for _, org := range orgs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !org.IsDir() {
			continue
		}
		orgDir := filepath.Join(runsDir, org.Name())
		kept := make([]recordingCandidate, 0, 16)
		var orgBytes int64
		for _, c := range r.recordingsIn(orgDir, org.Name()) {
			if maxAge > 0 && now.Sub(c.newest) > maxAge {
				r.removeRecording(ctx, c, "age")
				continue
			}
			kept = append(kept, c)
			orgBytes += c.sizeAll
		}
		r.holdRecordingQuota(ctx, org.Name(), kept, orgBytes)
	}
	return nil
}

// recordingsIn lists one org's cycle directories with their age and size.
func (r *RecordingRetention) recordingsIn(orgDir, org string) []recordingCandidate {
	entries, err := os.ReadDir(orgDir)
	if err != nil {
		return nil
	}
	out := make([]recordingCandidate, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(orgDir, e.Name())
		out = append(out, recordingCandidate{
			path:    dir,
			org:     org,
			cycle:   e.Name(),
			newest:  newestMTime(dir),
			sizeAll: duDir(dir),
		})
	}
	return out
}

// holdRecordingQuota deletes a whole org's oldest recordings until the org is
// back under OrgQuotaBytes. Whole recordings, never partial ones: half a feed
// with no marker saying where it was cut is exactly the thing the recording
// state machine refuses to serve, and a truncated file would have no way to say
// so.
func (r *RecordingRetention) holdRecordingQuota(ctx context.Context, org string, kept []recordingCandidate, orgBytes int64) {
	if r.quota <= 0 || orgBytes <= r.quota {
		return
	}
	sort.Slice(kept, func(i, j int) bool {
		if kept[i].newest.Equal(kept[j].newest) {
			return kept[i].cycle < kept[j].cycle // deterministic under equal mtimes
		}
		return kept[i].newest.Before(kept[j].newest)
	})
	slog.InfoContext(ctx, "recording retention: org over quota on run recordings — evicting oldest",
		"org", org, "usageBytes", orgBytes, "quotaBytes", r.quota)
	for _, c := range kept {
		if orgBytes <= r.quota || ctx.Err() != nil {
			return
		}
		if r.removeRecording(ctx, c, "quota") {
			orgBytes -= c.sizeAll
		}
	}
}

// removeRecording deletes one cycle's recording directory outright.
//
// No two-phase trash rename: that exists so a delete of a LIVE repo tree frees
// its canonical path instantly while readers finish, and a recording has no
// canonical path to free and no writer racing for it — the only writer is a
// session for a cycle that is, by the age gate, at least a month over.
func (r *RecordingRetention) removeRecording(ctx context.Context, c recordingCandidate, why string) bool {
	if err := os.RemoveAll(c.path); err != nil {
		slog.WarnContext(ctx, "recording retention: purge run recording failed", "path", c.path, "error", err)
		return false
	}
	slog.InfoContext(ctx, "recording retention: purged run recording",
		"org", c.org, "cycle", c.cycle, "reason", why, "bytes", c.sizeAll)
	return true
}

// newestMTime is the modification time of the newest entry in dir (the dir's
// own mtime when it holds nothing). A recording is appended to for the whole
// run, so this — and not the directory's mtime — is when it was last written.
func newestMTime(dir string) time.Time {
	info, err := os.Stat(dir)
	if err != nil {
		return time.Time{}
	}
	newest := info.ModTime()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return newest
	}
	for _, e := range entries {
		fi, ferr := e.Info()
		if ferr != nil {
			continue
		}
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	return newest
}

// duDir sums allocated blocks (st_blocks*512) for regular files under path.
// Errors are skipped: a concurrent removal mid-walk is normal.
func duDir(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip unreadable entries, keep walking
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				if st, ok := info.Sys().(*syscall.Stat_t); ok {
					total += st.Blocks * 512
				} else {
					total += info.Size()
				}
			}
		}
		return nil
	})
	return total
}
