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

// Package reaper is the disk-lifecycle authority for the studio-data volume
// (moved from services/aep-api/internal/platform/gitfs/reaper; ticket 20 §3).
// The volume is a cache of GitHub: a pod roll wipes it and every repo
// re-clones on first use. One pod serves one org and runs one reaper, so there
// is no leader lock. Five passes per sweep, each isolated (one failing never
// stops the next):
//
//  1. tmp reclamation: purge tmp/ entries older than TrashMaxAge, skipping
//     the engine's askpass shim;
//  2. trash reclamation: purge trash/<id> entries older than TrashMaxAge;
//  3. snapshot age: trash snapshot <sha> leaves unused for longer than
//     SnapshotMaxAge whose sha is no mirror's current HEAD;
//  4. git maintenance: repack/prune/pack-refs on loose- or pack-heavy
//     mirrors under the repo's EX flock (before the budget, so eviction sees
//     reclaimed space);
//  5. budget: du (block usage) of the root against the one budget; from 85 %
//     purge trash, then evict snapshots least recently used first, then
//     mirrors least recently fetched first, down to 70 %.
//
// No pass removes or renames snapshots/ or its projects/ and skills/ dirs:
// ae-design-agent's subPath mount pins that inode, so only <sha> leaves go.
//
// A forced sweep follows an ENOSPC, which the engine only sees when the
// node's disk fills: the studio-data emptyDir's sizeLimit is enforced by
// kubelet evicting the pod, never by ENOSPC, which is why the budget sits
// under it.
//
// There is no orphan pass: nothing lists the live repos. An orphan (a failed
// trash on project delete) is unreachable, because every request resolves its
// project through aep-api; LRU evicts it under pressure and the next roll
// wipes it.
package reaper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// Config is the reaper's knobs. Budget comes from AE_STORAGE_BUDGET_BYTES;
// zero Interval, TrashMaxAge and SnapshotMaxAge take the defaults below.
type Config struct {
	Budget         int64
	Interval       time.Duration
	TrashMaxAge    time.Duration
	SnapshotMaxAge time.Duration
}

const (
	defaultInterval    = 5 * time.Minute
	defaultTrashMaxAge = time.Hour
	// defaultSnapshotMaxAge is above the 30-min turn cap, so a snapshot a
	// running turn reads is never age-reaped under it.
	defaultSnapshotMaxAge = time.Hour
)

// Reaper runs the sweep on cfg.Interval, and on demand after an ENOSPC.
type Reaper struct {
	engine *repo.Engine
	cfg    Config
	// force carries at most one pending forced-sweep request from the
	// engine's ENOSPC hook to Run.
	force chan struct{}
	// statfs is the node filesystem seam: used and total bytes of the
	// filesystem backing path. On an emptyDir it reports the node's disk,
	// not the sizeLimit. Defaults to statfsUsage; tests pin it.
	statfs func(path string) (used, total uint64, err error)
}

// New builds the reaper over engine's root and registers its forced-sweep
// request as the engine's ENOSPC handler and its UsagePct as the engine's
// admission gauge. Call it before the engine serves.
// It panics on a non-positive Budget: config.Load guarantees one.
func New(engine *repo.Engine, cfg Config) *Reaper {
	if cfg.Budget <= 0 {
		panic(fmt.Sprintf("reaper: budget must be positive, got %d", cfg.Budget))
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.TrashMaxAge <= 0 {
		cfg.TrashMaxAge = defaultTrashMaxAge
	}
	if cfg.SnapshotMaxAge <= 0 {
		cfg.SnapshotMaxAge = defaultSnapshotMaxAge
	}
	r := &Reaper{engine: engine, cfg: cfg, force: make(chan struct{}, 1), statfs: statfsUsage}
	engine.SetOnENOSPC(r.requestForceSweep)
	engine.SetUsageGauge(r.UsagePct)
	return r
}

// Run sweeps once at start (a pod restarting into a full volume must not wait
// an interval), then on every tick and on every forced-sweep request, until
// ctx is canceled. All sweeps run on this goroutine, so they never overlap.
func (r *Reaper) Run(ctx context.Context) {
	r.sweep(ctx)
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.sweep(ctx)
		case <-r.force:
			r.ForceSweep(ctx)
		}
	}
}

// ForceSweep is the ENOSPC emergency path: purge every trash/<id> entry
// regardless of age, then run a full sweep. Run calls it for each request the
// engine's ENOSPC hook queues.
func (r *Reaper) ForceSweep(ctx context.Context) {
	if err := r.purgeTrashAll(ctx); err != nil {
		slog.WarnContext(ctx, "reaper.pass_failed", "pass", "force-trash-purge", "class", repo.ErrorClass(err))
	}
	r.sweep(ctx)
}

// requestForceSweep queues one forced sweep for Run without blocking: it runs
// on the request goroutine that hit ENOSPC, and a request already pending
// covers this one.
func (r *Reaper) requestForceSweep() {
	select {
	case r.force <- struct{}{}:
	default:
	}
}

// sweep runs sweepOnce and logs each failed pass.
func (r *Reaper) sweep(ctx context.Context) {
	if _, err := r.sweepOnce(ctx); err != nil {
		slog.WarnContext(ctx, "reaper.pass_failed", "class", repo.ErrorClass(err))
	}
}

// sweepOnce runs the five passes, publishes UsagePct onto the engine (it
// feeds DiskFullError) and logs the reaper.sweep line. It returns what the
// budget evicted (snapshots as their path under the root, then repos as
// "owner/repo") and the joined errors of the passes that failed; a
// failed pass never stops the next one.
func (r *Reaper) sweepOnce(ctx context.Context) ([]string, error) {
	var errs []error
	pass := func(name string, fn func(context.Context) error) {
		if ctx.Err() != nil {
			return
		}
		if err := fn(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	var evicted []string
	pass("tmp-reclamation", r.reclaimTmp)
	pass("trash-reclamation", r.reclaimTrash)
	pass("snapshot-age", r.reapSnapshots)
	pass("git-maintenance", func(ctx context.Context) error {
		_, err := r.maintainRepos(ctx)
		return err
	})
	pass("budget", func(ctx context.Context) error {
		var err error
		evicted, err = r.enforceBudget(ctx)
		return err
	})
	pct := r.UsagePct()
	r.engine.SetDiskUsagePct(pct)
	r.logSweep(ctx, pct, len(evicted))
	return evicted, errors.Join(errs...)
}

// walkRepoDirs visits every repos/<owner>/<repo> mirror directory. Only real
// directories are visited (a symlink is never followed), so every visited
// path lies inside the root. Unreadable levels are skipped: a concurrent trash
// rename is normal and the next sweep reconverges.
func (r *Reaper) walkRepoDirs(ctx context.Context, visit func(ref repo.RepoRef, repoDir string)) error {
	reposDir := repo.ReposDir(r.engine.Root())
	owners, err := os.ReadDir(reposDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, owner := range owners {
		if !owner.IsDir() {
			continue
		}
		ownerDir := filepath.Join(reposDir, owner.Name())
		names, err := os.ReadDir(ownerDir)
		if err != nil {
			continue
		}
		for _, name := range names {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !name.IsDir() {
				continue
			}
			ref := repo.RepoRef{Owner: owner.Name(), Repo: name.Name()}
			visit(ref, filepath.Join(ownerDir, name.Name()))
		}
	}
	return nil
}
