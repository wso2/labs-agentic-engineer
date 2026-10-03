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
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
)

// snapshotEvictMinAge is the floor age a snapshot must reach before quota/LRU
// eviction may trash it: 30m, the longest a reader of a snapshot was ever
// allowed to run (the retired in-process turn runner's timeout). The mirror's
// current-HEAD snapshot is protected
// unconditionally (isHead) regardless of age; mirrors themselves stay
// evictable.
const snapshotEvictMinAge = 30 * time.Minute

// purgeTrashAll removes every trash/<id> entry regardless of age. Used when
// the volume is over the high watermark so a trash rename from a prior tick
// actually frees bytes before we evict more repos/. Open readers survive
// (POSIX keeps deleted-but-open inodes alive).
func (r *Reaper) purgeTrashAll(ctx context.Context) error {
	trashDir := gitfs.TrashDir(r.engine.Root())
	entries, err := os.ReadDir(trashDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		path := filepath.Join(trashDir, e.Name())
		if err := os.RemoveAll(path); err != nil {
			slog.WarnContext(ctx, "reaper: pressure purge trash failed", "path", path, "error", err)
		}
	}
	return nil
}

// enforceQuota is pass 6 / last leader-only pass (after orphans + maintenance):
// per-org quota first (errors logged, never abort the global path), then the
// global statfs watermark. When over high: purge trash/ unconditionally FIRST,
// re-read statfs, and only then evict if still over. Returns successful
// snapshot/mirror eviction count this pass (R8b usage line).
func (r *Reaper) enforceQuota(ctx context.Context) (int, error) {
	root := r.engine.Root()
	evictions := 0
	if r.cfg.OrgQuotaBytes > 0 {
		n, err := r.enforceOrgQuotas(ctx)
		evictions += n
		if err != nil {
			slog.WarnContext(ctx, "reaper: org quota pass failed — continuing to global watermark", "error", err)
		}
	}
	total, avail, inodesTotal, inodesFree, err := r.diskUsage(root)
	if err != nil || total == 0 {
		return evictions, err
	}
	r.recordDiskUsage(total, avail, inodesTotal, inodesFree)
	used := total - avail
	inodeUsed := inodesTotal - inodesFree
	overBytes := used*100 > uint64(r.cfg.DiskHighPct)*total
	overInodes := inodesTotal > 0 && inodeUsed*100 > uint64(r.cfg.DiskHighPct)*inodesTotal
	if !overBytes && !overInodes {
		return evictions, nil
	}
	inodeUsedPct := uint64(0)
	if inodesTotal > 0 {
		inodeUsedPct = inodeUsed * 100 / inodesTotal
	}
	slog.InfoContext(ctx, "reaper: disk over high watermark — purging trash before eviction",
		"usedPct", used*100/total, "highPct", r.cfg.DiskHighPct,
		"inodeUsedPct", inodeUsedPct, "overBytes", overBytes, "overInodes", overInodes)
	if err := r.purgeTrashAll(ctx); err != nil {
		slog.WarnContext(ctx, "reaper: pressure trash purge failed", "error", err)
	}
	total, avail, inodesTotal, inodesFree, err = r.diskUsage(root)
	if err != nil || total == 0 {
		return evictions, err
	}
	r.recordDiskUsage(total, avail, inodesTotal, inodesFree)
	used = total - avail
	inodeUsed = inodesTotal - inodesFree
	overBytes = used*100 > uint64(r.cfg.DiskHighPct)*total
	overInodes = inodesTotal > 0 && inodeUsed*100 > uint64(r.cfg.DiskHighPct)*inodesTotal
	if !overBytes && !overInodes {
		if inodesTotal > 0 {
			inodeUsedPct = inodeUsed * 100 / inodesTotal
		} else {
			inodeUsedPct = 0
		}
		slog.InfoContext(ctx, "reaper: under high watermark after trash purge — skipping eviction",
			"usedPct", used*100/total, "inodeUsedPct", inodeUsedPct)
		return evictions, nil
	}
	var target int64
	if overBytes {
		lowBytes := total * uint64(r.cfg.DiskLowPct) / 100
		target = int64(used - lowBytes)
	} else {
		reposDir := gitfs.ReposDir(root)
		target = duDir(reposDir) / 4
		if target <= 0 {
			target = 1
		}
	}
	if inodesTotal > 0 {
		inodeUsedPct = inodeUsed * 100 / inodesTotal
	}
	slog.InfoContext(ctx, "reaper: still over high watermark — evicting",
		"usedPct", used*100/total, "highPct", r.cfg.DiskHighPct, "lowPct", r.cfg.DiskLowPct,
		"inodeUsedPct", inodeUsedPct, "targetBytes", target)
	n, err := r.evictWithin(ctx, gitfs.ReposDir(root), target)
	return evictions + n, err
}

// enforceOrgQuotas walks repos/<orgId> subtrees and evicts within any org
// whose on-disk usage exceeds OrgQuotaBytes, down to the quota.
func (r *Reaper) enforceOrgQuotas(ctx context.Context) (int, error) {
	reposDir := gitfs.ReposDir(r.engine.Root())
	orgDirs, err := os.ReadDir(reposDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	evictions := 0
	for _, org := range orgDirs {
		if ctx.Err() != nil {
			return evictions, ctx.Err()
		}
		if !org.IsDir() {
			continue
		}
		orgDir := filepath.Join(reposDir, org.Name())
		usage := duDir(orgDir)
		if usage <= r.cfg.OrgQuotaBytes {
			continue
		}
		slog.InfoContext(ctx, "reaper: org over quota — evicting",
			"org", org.Name(), "usageBytes", usage, "quotaBytes", r.cfg.OrgQuotaBytes)
		n, err := r.evictWithin(ctx, orgDir, usage-r.cfg.OrgQuotaBytes)
		evictions += n
		if err != nil {
			slog.WarnContext(ctx, "reaper: org quota eviction failed", "org", org.Name(), "error", err)
		}
	}
	return evictions, nil
}

// snapshotCandidate / mirrorCandidate are eviction units within a scope.
type snapshotCandidate struct {
	path    string
	slugDir string // owning repo dir, for the mirror-size adjustment
	size    int64
	mtime   time.Time
	isHead  bool // sha == mirror's current HEAD — an in-flight turn may read it
}

type mirrorCandidate struct {
	ref     gitfs.RepoRef
	slugDir string
	size    int64 // whole repo dir (git + lock + remaining snapshots)
	lastUse time.Time
}

// evictWithin trashes candidates under scopeDir until ~target bytes are
// freed: snapshots first (oldest mtime first — they are pure derived data),
// then whole mirror dirs in LRU order. A mirror whose repo.lock is currently
// held is in live use and is SKIPPED, never waited on: TrashRepo acquires
// the EX flock through the gitfs Locker, and the bounded lockCtx turns that
// into a non-blocking try.
func (r *Reaper) evictWithin(ctx context.Context, scopeDir string, target int64) (int, error) {
	snaps, mirrors, err := r.collectCandidates(ctx, scopeDir)
	if err != nil {
		return 0, err
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].mtime.Before(snaps[j].mtime) })
	sort.Slice(mirrors, func(i, j int) bool { return mirrors[i].lastUse.Before(mirrors[j].lastUse) })

	freed := int64(0)
	count := 0
	now := time.Now()
	evictedSnapBytes := map[string]int64{} // slugDir → snapshot bytes already trashed
	for _, s := range snaps {
		if freed >= target || ctx.Err() != nil {
			break
		}
		// Protect in-flight turns (unlike a plain mtime sweep): never evict the
		// snapshot the mirror HEAD points at, nor one younger than the turn
		// runner's detached timeout — the agents pod reads a turn's
		// base/_skills tree lazily, so a current-HEAD or recent snapshot may
		// still be feeding a live turn. Mirrors stay evictable; a resulting
		// shortfall is covered by the "exhausted candidates" warning below.
		if s.isHead || now.Sub(s.mtime) < snapshotEvictMinAge {
			continue
		}
		if err := os.Rename(s.path, r.trashDest()); err != nil {
			if !os.IsNotExist(err) {
				slog.WarnContext(ctx, "reaper: evict snapshot failed", "path", s.path, "error", err)
			}
			continue
		}
		freed += s.size
		count++
		evictedSnapBytes[s.slugDir] += s.size
	}
	for _, m := range mirrors {
		if freed >= target || ctx.Err() != nil {
			break
		}
		lockCtx, cancel := context.WithTimeout(ctx, evictLockTimeout)
		err := r.engine.TrashRepo(lockCtx, m.ref)
		cancel()
		if err != nil {
			// Lock held (live use) or validation/rename failure — skip to the
			// next LRU candidate; never block the sweep on a busy mirror.
			slog.InfoContext(ctx, "reaper: skipping mirror eviction",
				"org", m.ref.OrgID, "project", m.ref.ProjectID, "slug", m.ref.RepoSlug, "reason", err)
			continue
		}
		freed += m.size - evictedSnapBytes[m.slugDir] // don't double-count its evicted snapshots
		count++
		slog.InfoContext(ctx, "reaper: evicted LRU mirror",
			"org", m.ref.OrgID, "project", m.ref.ProjectID, "slug", m.ref.RepoSlug, "bytes", m.size)
	}
	if freed < target {
		slog.WarnContext(ctx, "reaper: eviction exhausted candidates short of target",
			"scope", scopeDir, "freedBytes", freed, "targetBytes", target)
	}
	return count, nil
}

// collectCandidates gathers the snapshot + mirror eviction candidates under
// scopeDir (the whole repos/ tree, or one org subtree).
func (r *Reaper) collectCandidates(ctx context.Context, scopeDir string) ([]snapshotCandidate, []mirrorCandidate, error) {
	var snaps []snapshotCandidate
	var mirrors []mirrorCandidate
	scopePrefix := scopeDir + string(filepath.Separator)
	err := r.walkRepoDirs(ctx, func(orgID, projectID, repoSlug, slugDir string) {
		if slugDir != scopeDir && !strings.HasPrefix(slugDir, scopePrefix) {
			return
		}
		snapsDir := gitfs.SnapshotsSubdir(slugDir)
		gitDir := gitfs.GitSubdir(slugDir)
		if entries, err := os.ReadDir(snapsDir); err == nil {
			// Resolve the mirror HEAD once per slugDir so the current-HEAD
			// snapshot (a running turn's base) is never a candidate. "" when
			// the mirror is missing/unresolvable → nothing is treated as HEAD.
			head := mirrorHead(gitDir)
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				info, err := e.Info()
				if err != nil {
					continue
				}
				path := filepath.Join(snapsDir, e.Name())
				snaps = append(snaps, snapshotCandidate{
					path:    path,
					slugDir: slugDir,
					size:    duDir(path),
					mtime:   info.ModTime(),
					isHead:  head != "" && e.Name() == head,
				})
			}
		}
		gitInfo, err := os.Stat(gitDir)
		if err != nil {
			return // no mirror (snapshot-only husk) — snapshots above still count
		}
		lastUse := gitInfo.ModTime()
		// FETCH_HEAD is rewritten by every fetch — a better recency signal
		// than the dir mtime (which only moves when a direct entry changes).
		if fh, err := os.Stat(filepath.Join(gitDir, "FETCH_HEAD")); err == nil && fh.ModTime().After(lastUse) {
			lastUse = fh.ModTime()
		}
		mirrors = append(mirrors, mirrorCandidate{
			ref:     gitfs.RepoRef{OrgID: orgID, ProjectID: projectID, RepoSlug: repoSlug},
			slugDir: slugDir,
			size:    duDir(slugDir),
			lastUse: lastUse,
		})
	})
	return snaps, mirrors, err
}

// duDir sums allocated blocks (st_blocks*512) for regular files under path.
// Directory entries themselves are not counted; errors are skipped — a trash
// rename mid-walk is normal.
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
					total += info.Size() // non-unix fallback for tests on exotic GOOS
				}
			}
		}
		return nil
	})
	return total
}

// statfsUsage is the default diskUsage seam: total/available bytes and inode
// counts of the filesystem backing path, df-style (available = Bavail).
func statfsUsage(path string) (total, avail, inodesTotal, inodesFree uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, 0, 0, err
	}
	bsize := uint64(st.Bsize)
	return st.Blocks * bsize, st.Bavail * bsize, st.Files, st.Ffree, nil
}
