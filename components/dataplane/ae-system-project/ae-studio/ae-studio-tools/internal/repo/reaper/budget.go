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
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// The one budget over studio-data (ticket 20 §2): eviction starts at
// highPct of the budget and stops once usage is back at lowPct.
const (
	highPct = 85
	lowPct  = 70
)

// evictLockTimeout bounds the try on a mirror's repo.lock during eviction:
// long enough for a few flock polls, far shorter than any real critical
// section. A mirror in use is skipped, never waited on.
const evictLockTimeout = 100 * time.Millisecond

// UsagePct is the studio-data pressure: the higher of the budget share (the
// last du plus the clones since) and the node filesystem's byte used%. On an
// emptyDir statfs reports the node's disk, which can fill for other reasons.
// Inodes are not watched. The budget share can exceed 100.
func (r *Reaper) UsagePct() int {
	pct := int(r.engine.UsedBytes() * 100 / r.cfg.Budget)
	if used, total, err := r.statfs(r.engine.Root()); err == nil && total > 0 {
		if node := int(used * 100 / total); node > pct {
			pct = node
		}
	}
	return pct
}

// enforceBudget measures the root and, from highPct of the budget, first
// purges trash and re-measures, then evicts mirrors least recently used first
// until usage is back at lowPct. Evicted mirrors go through trash, which is
// purged again so the bytes are actually freed. Phase 2 has no snapshots;
// phase 3 adds them to the front of the eviction order.
func (r *Reaper) enforceBudget(ctx context.Context) ([]string, error) {
	used := r.measure()
	if !r.atOrAbove(used, highPct) {
		return nil, nil
	}
	if err := r.purgeTrashAll(ctx); err != nil {
		return nil, err
	}
	used = r.measure()
	if !r.atOrAbove(used, highPct) {
		return nil, nil
	}
	evicted, err := r.evictLRU(ctx, used-r.cfg.Budget*lowPct/100)
	if len(evicted) > 0 {
		err = errors.Join(err, r.purgeTrashAll(ctx))
		r.measure()
	}
	return evicted, err
}

// measure records a fresh du of the root on the engine and returns it. A
// clone that lands while the walk runs may be missed until the next sweep.
func (r *Reaper) measure() int64 {
	n := repo.DirBytes(r.engine.Root())
	r.engine.SetUsedBytes(n)
	return n
}

func (r *Reaper) atOrAbove(used int64, pct int64) bool {
	return used*100 >= pct*r.cfg.Budget
}

// mirrorCandidate is one eviction unit: a whole repos/<org>/<project>/<slug>.
type mirrorCandidate struct {
	ref     repo.RepoRef
	size    int64
	lastUse time.Time
}

// evictLRU trashes mirrors least recently used first until about target
// bytes are freed. TrashRepo takes the repo's EX flock; the bounded lockCtx
// makes that a try, so a mirror in use (any SH or EX holder) is skipped.
// Returns the evicted repos as "org/project/slug".
func (r *Reaper) evictLRU(ctx context.Context, target int64) ([]string, error) {
	mirrors, err := r.collectMirrors(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(mirrors, func(i, j int) bool { return mirrors[i].lastUse.Before(mirrors[j].lastUse) })
	var evicted []string
	freed := int64(0)
	for _, m := range mirrors {
		if freed >= target || ctx.Err() != nil {
			break
		}
		lockCtx, cancel := context.WithTimeout(ctx, evictLockTimeout)
		err := r.engine.TrashRepo(lockCtx, m.ref)
		cancel()
		if err != nil {
			slog.InfoContext(ctx, "reaper.evict_skipped", "repo", repoName(m.ref), "reason", err)
			continue
		}
		freed += m.size
		evicted = append(evicted, repoName(m.ref))
		slog.InfoContext(ctx, "reaper.evicted", "repo", repoName(m.ref), "bytes", m.size)
	}
	if freed < target {
		slog.WarnContext(ctx, "reaper.evict_short", "freedBytes", freed, "targetBytes", target)
	}
	return evicted, nil
}

// collectMirrors lists every repo dir with its size and last use. Recency is
// the last fetch, not the last read: the git dir's mtime, or FETCH_HEAD's
// when newer (every fetch rewrites it), so a mirror read only by sha looks
// older than it is. A repo dir with no git dir (a failed clone) falls back to
// its own mtime.
func (r *Reaper) collectMirrors(ctx context.Context) ([]mirrorCandidate, error) {
	var mirrors []mirrorCandidate
	err := r.walkRepoDirs(ctx, func(ref repo.RepoRef, repoDir string) {
		var lastUse time.Time
		gitDir := repo.GitSubdir(repoDir)
		if st, err := os.Stat(gitDir); err == nil {
			lastUse = st.ModTime()
			if fh, err := os.Stat(filepath.Join(gitDir, "FETCH_HEAD")); err == nil && fh.ModTime().After(lastUse) {
				lastUse = fh.ModTime()
			}
		} else if st, err := os.Stat(repoDir); err == nil {
			lastUse = st.ModTime()
		}
		mirrors = append(mirrors, mirrorCandidate{ref: ref, size: repo.DirBytes(repoDir), lastUse: lastUse})
	})
	return mirrors, err
}

// statfsUsage is the default statfs seam: used and total bytes of the
// filesystem backing path, df-style (used = total - available).
func statfsUsage(path string) (used, total uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bsize := uint64(st.Bsize)
	return (st.Blocks - st.Bavail) * bsize, st.Blocks * bsize, nil
}
