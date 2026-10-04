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
// It runs on EVERY replica, alongside the other local reclamation passes, for
// the same reason they do: both rules are idempotent and derive entirely from
// what is on disk, so two replicas racing reach the same answer. (aep-api runs
// single-replica anyway — the volume is ReadWriteOnce — so the distinction is
// theoretical here and the pass is written not to depend on it.)

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
)

// recordingCandidate is one cycle's recording directory, as this pass sees it.
type recordingCandidate struct {
	path    string
	org     string
	cycle   string
	newest  time.Time
	sizeAll int64
}

// reapRecordings purges aged recordings and then holds each org under quota.
func (r *Reaper) reapRecordings(ctx context.Context) error {
	runsDir := gitfs.RunsDir(r.engine.Root())
	orgs, err := os.ReadDir(runsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing has been recorded on this volume yet
		}
		return err
	}
	now := r.now()
	maxAge := r.cfg.RecordingMaxAge
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
func (r *Reaper) recordingsIn(orgDir, org string) []recordingCandidate {
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
func (r *Reaper) holdRecordingQuota(ctx context.Context, org string, kept []recordingCandidate, orgBytes int64) {
	if r.cfg.OrgQuotaBytes <= 0 || orgBytes <= r.cfg.OrgQuotaBytes {
		return
	}
	sort.Slice(kept, func(i, j int) bool {
		if kept[i].newest.Equal(kept[j].newest) {
			return kept[i].cycle < kept[j].cycle // deterministic under equal mtimes
		}
		return kept[i].newest.Before(kept[j].newest)
	})
	slog.InfoContext(ctx, "reaper: org over quota on run recordings — evicting oldest",
		"org", org, "usageBytes", orgBytes, "quotaBytes", r.cfg.OrgQuotaBytes)
	for _, c := range kept {
		if orgBytes <= r.cfg.OrgQuotaBytes || ctx.Err() != nil {
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
func (r *Reaper) removeRecording(ctx context.Context, c recordingCandidate, why string) bool {
	if err := os.RemoveAll(c.path); err != nil {
		slog.WarnContext(ctx, "reaper: purge run recording failed", "path", c.path, "error", err)
		return false
	}
	slog.InfoContext(ctx, "reaper: purged run recording",
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
