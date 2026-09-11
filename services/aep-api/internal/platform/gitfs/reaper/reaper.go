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

// Package reaper is the single disk-lifecycle authority for the shared
// workspace mount. git_repositories is authoritative and the mount is a rebuildable cache in
// every part, so the synchronous delete hooks (TrashRepo / TrashOrg) are
// best-effort and CORRECTNESS lives here, in the background sweep. Seven
// passes per tick, each isolated (one failing never stops the next):
//
//  1. tmp reclamation — purge tmp/ entries older than TrashMaxAge (same 1h
//     binding), skipping askpass.sh (local + idempotent: every replica runs it);
//  2. trash reclamation — purge trash/<id> entries older than TrashMaxAge
//     (local + idempotent: every replica runs it);
//  3. snapshot age-reap — trash snapshots/<sha> dirs older than
//     SnapshotMaxAge that are not the mirror's current HEAD (immutability
//     makes this safe: no leases, no heartbeats);
//  4. recording retention — purge runs/<orgId>/<cycleId> coding-agent feed
//     recordings older than RecordingMaxAge (30 days), then hold each org
//     under OrgQuotaBytes oldest-first (local + idempotent). This tree is the
//     one part of the mount that is NOT rebuildable, which is why its window
//     is days rather than hours and why it is never evicted for disk pressure
//     (ADR-0027);
//  5. orphan reconciliation — set-difference the on-disk repos/… tree
//     against the DB rows, with a 2×interval mtime grace (leader-only);
//  6. git maintenance — repack/prune/pack-refs on loose-/pack-heavy mirrors
//     under EX flock (leader-only; before quota so eviction sees reclaimed space);
//  7. quota/LRU eviction — statfs high-watermark + per-org quota; snapshots
//     evicted first (oldest mtime), then whole mirrors LRU by git/ mtime,
//     until under the low watermark (leader-only). It walks repos/ only:
//     recordings are bounded by pass 4 instead.
//
// The leader gate for passes 5–7 is a non-blocking flock on
// <root>/.reaper.lock — replicas that lose it skip the global passes for the
// tick and retry next tick.
package reaper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
)

// RepoCoordinate is the reaper's own view of a repo row: just its on-disk
// identity. Deliberately NOT sourcecontrol.GitRepository — the reaper is a kernel
// package, and naming the domain entity would be the kernel depending on a
// domain. It needs three things to decide whether a directory is an orphan;
// this names exactly those. The composition root projects the DB rows onto it.
type RepoCoordinate struct {
	OrgID         string
	ProjectID     string
	WorkspaceSlug string
}

// RepoLister is the reaper-side port: every repo coordinate across all orgs and
// statuses. Pending/error rows count — their on-disk dirs must not be misread as
// orphans. The concrete repository is adapted onto it at the composition root.
type RepoLister interface {
	ListAll(ctx context.Context) ([]RepoCoordinate, error)
}

// leaderLockName is the root-level flock file serializing the global passes
// (orphan reconciliation + git maintenance + quota/LRU) to one replica per tick.
const leaderLockName = ".reaper.lock"

// evictLockTimeout bounds the "non-blocking try" on a mirror's repo.lock
// during LRU eviction: long enough for a few flock polls, far shorter than
// any real fetch/push critical section — a busy mirror is skipped, never
// waited on.
const evictLockTimeout = 100 * time.Millisecond

// Reaper is the app.Watcher running the six disk-lifecycle passes on
// cfg.ReapInterval.
type Reaper struct {
	engine *gitfs.Engine
	repos  RepoLister
	cfg    config.WorkspaceConfig
	// ready is the shared R8b readiness gate (GET /readyz). Updated every
	// sweep from root-health; nil is treated as always-ready.
	ready *ReadyGate
	// leaderLease is how long a lock-file lease is marked valid after acquire
	// (written as expiry metadata). Defaults to 2×ReapInterval; tests shorten
	// it. Flock is still the real gate — expiry is diagnostic + documents
	// intent when a dead holder's kernel-released flock leaves a stale file.
	leaderLease time.Duration
	// diskUsage is the statfs seam (total/available bytes and inode counts of
	// the filesystem backing the workspace root). Defaults to syscall.Statfs;
	// tests inject a fake to simulate watermark pressure.
	diskUsage func(path string) (total, avail, inodesTotal, inodesFree uint64, err error)
	// now is the clock seam. It exists for the recording-retention pass, whose
	// window is THIRTY DAYS: a test cannot wait one out and a test that back-dates
	// files instead is asserting os.Chtimes rather than the rule. Defaults to
	// time.Now.
	now func() time.Time
}

// New wires the reaper over the engine's workspace root. Zero/negative cfg
// knobs fall back to the design §14 defaults (defensive — the config loader
// already bakes them in). OrgQuotaBytes <= 0 disables the per-org quota
// check; the global watermark check always runs. ready may be nil (tests);
// when non-nil it is updated every sweep from root-health.
func New(engine *gitfs.Engine, repos RepoLister, cfg config.WorkspaceConfig, ready *ReadyGate) *Reaper {
	if cfg.ReapInterval <= 0 {
		cfg.ReapInterval = 5 * time.Minute
	}
	if cfg.SnapshotMaxAge <= 0 {
		cfg.SnapshotMaxAge = time.Hour
	}
	if cfg.TrashMaxAge <= 0 {
		cfg.TrashMaxAge = time.Hour
	}
	if cfg.RecordingMaxAge <= 0 {
		cfg.RecordingMaxAge = 30 * 24 * time.Hour
	}
	if cfg.DiskHighPct <= 0 {
		cfg.DiskHighPct = 85
	}
	if cfg.DiskLowPct <= 0 {
		cfg.DiskLowPct = 70
	}
	if cfg.DiskLowPct >= cfg.DiskHighPct || cfg.DiskHighPct > 100 {
		panic(fmt.Sprintf(
			"reaper: invalid disk watermarks: DiskLowPct=%d DiskHighPct=%d (need 0 < low < high <= 100)",
			cfg.DiskLowPct, cfg.DiskHighPct,
		))
	}
	return &Reaper{
		engine:      engine,
		repos:       repos,
		cfg:         cfg,
		ready:       ready,
		leaderLease: 2 * cfg.ReapInterval,
		diskUsage:   statfsUsage,
		now:         time.Now,
	}
}

// Run drives the sweep on cfg.ReapInterval until ctx is canceled. Matches
// the watcher lifecycle convention (execution.Sweep): a plain goroutine per
// watcher, all durable state on disk / in Postgres.
func (r *Reaper) Run(ctx context.Context) {
	// Startup sweep: a pod restarting into a full volume must not wait a full
	// ReapInterval before reclaiming. If the full volume is crash-looping
	// aep-api, waiting on the ticker may never run.
	r.Sweep(ctx)
	ticker := time.NewTicker(r.cfg.ReapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Sweep(ctx)
		}
	}
}

// Sweep runs one full reap cycle. Exported so a test (or an admin trigger)
// can drive a single pass without the ticker. Passes are isolated: a failing
// pass is logged and the next one still runs. Root-health runs first and
// drives readiness (R8b). After all passes, a usage INFO line is emitted and
// admission usage is re-recorded (even when this replica was not leader).
func (r *Reaper) Sweep(ctx context.Context) {
	r.applyRootHealth(ctx)

	r.pass(ctx, "tmp-reclamation", r.reclaimTmp)
	r.pass(ctx, "trash-reclamation", r.reclaimTrash)
	r.pass(ctx, "snapshot-age-reap", r.reapSnapshots)
	r.pass(ctx, "recording-retention", r.reapRecordings)

	var maintained, evictions int
	// Global passes run on at most one replica per tick: non-blocking leader
	// flock — losing it simply defers to whichever replica holds it.
	release, held := r.tryLeaderLock(ctx)
	if held {
		r.pass(ctx, "orphan-reconciliation", r.reconcileOrphans)
		r.pass(ctx, "git-maintenance", func(ctx context.Context) error {
			n, err := r.maintainRepos(ctx)
			maintained = n
			return err
		})
		r.pass(ctx, "quota-lru-eviction", func(ctx context.Context) error {
			n, err := r.enforceQuota(ctx)
			evictions = n
			return err
		})
		release()
	}
	r.logSweepUsage(ctx, held, maintained, evictions)
}

// pass runs one pass, isolating its failure to a log line so the remaining
// passes still run (design §14 — hooks and passes are all best-effort; the
// next tick retries).
func (r *Reaper) pass(ctx context.Context, name string, fn func(context.Context) error) {
	if ctx.Err() != nil {
		return
	}
	if err := fn(ctx); err != nil {
		slog.WarnContext(ctx, "reaper: pass failed", "pass", name, "error", err)
	}
}

// tryLeaderLock takes the global <root>/.reaper.lock exclusively without
// blocking. held=false means another replica leads this tick (or the lock
// file could not be opened — logged, treated as not-leader). On acquire the
// lock file is rewritten with expiryUnixNano\npodName so losers can name the
// holder at Debug. Flock released by a dead process is stealable next tick;
// lease expiry alone cannot override a live holder's flock.
func (r *Reaper) tryLeaderLock(ctx context.Context) (release func(), held bool) {
	path := filepath.Join(r.engine.Root(), leaderLockName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		slog.WarnContext(ctx, "reaper: open leader lock failed", "path", path, "error", err)
		return nil, false
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		holder := readLeaderMeta(path)
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			slog.WarnContext(ctx, "reaper: leader flock failed", "path", path, "holder", holder, "error", err)
		} else {
			slog.DebugContext(ctx, "reaper: not leader this tick", "holder", holder, "error", err)
		}
		return nil, false
	}
	pod := os.Getenv("POD_NAME")
	if pod == "" {
		pod, _ = os.Hostname()
	}
	expiry := time.Now().Add(r.leaderLease).UnixNano()
	meta := []byte(fmt.Sprintf("%d\n%s\n", expiry, pod))
	_ = f.Truncate(0)
	if _, err := f.WriteAt(meta, 0); err != nil {
		slog.WarnContext(ctx, "reaper: write leader lease failed", "path", path, "error", err)
	}
	slog.DebugContext(ctx, "reaper: acquired leader lock", "pod", pod)
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, true
}

// readLeaderMeta returns the pod name from a lock file formatted as
// expiryUnixNano\npodName\n. Empty string when unreadable or malformed.
func readLeaderMeta(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.SplitN(string(b), "\n", 3)
	if len(lines) < 2 {
		return ""
	}
	return lines[1]
}

// walkRepoDirs visits every repos/<orgId>/<projectId>/<repoSlug> directory.
// Non-directory strays are skipped; visit errors are the visitor's own
// business (it logs and continues) — the walk only fails on an unreadable
// repos/ root.
func (r *Reaper) walkRepoDirs(ctx context.Context, visit func(orgID, projectID, repoSlug, slugDir string)) error {
	reposDir := gitfs.ReposDir(r.engine.Root())
	orgs, err := os.ReadDir(reposDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, org := range orgs {
		if !org.IsDir() {
			continue
		}
		orgDir := filepath.Join(reposDir, org.Name())
		projects, err := os.ReadDir(orgDir)
		if err != nil {
			continue // raced with a trash rename — next tick reconverges
		}
		for _, proj := range projects {
			if !proj.IsDir() {
				continue
			}
			projDir := filepath.Join(orgDir, proj.Name())
			slugs, err := os.ReadDir(projDir)
			if err != nil {
				continue
			}
			for _, slug := range slugs {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if !slug.IsDir() {
					continue
				}
				visit(org.Name(), proj.Name(), slug.Name(), filepath.Join(projDir, slug.Name()))
			}
		}
	}
	return nil
}
