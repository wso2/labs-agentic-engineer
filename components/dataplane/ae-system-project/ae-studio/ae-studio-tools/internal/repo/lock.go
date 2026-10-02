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

package repo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// Locker is the per-repo mirror-integrity lock (design D8): SHARED around
// pure object reads, EXCLUSIVE around fetch/push/ref-move/tag critical
// sections (sub-second — never across a turn). It guards the shared bare
// object DB against concurrent fetch/gc corruption; write CORRECTNESS is
// arbitrated by origin push-CAS, not by this lock.
//
// The default implementation is POSIX flock(2). Under the shipped RWO +
// co-located placement (services/aep-api/design/shared-workspace-volume.md)
// every holder shares one node, so flock is local — no cross-node flock
// requirement and no Postgres advisory-lock fallback.
//
// flock has no fairness: continuous back-to-back SHARED holders can starve
// an EXCLUSIVE acquirer. That risk is real under the shared volume —
// ReadBundle holds SHARED across its N-blob cat-file loop, which is not
// bounded to sub-second. Mitigation today: git maintenance acquires EX with a
// ~2s timeout and skips the mirror on contention (see reaper maintainRepos)
// rather than blocking forever. If starvation becomes observable in production,
// add writer-preference here rather than at call sites.
type Locker interface {
	// RLock acquires the lock shared. release is never nil on success.
	RLock(ctx context.Context, path string) (release func(), err error)
	// Lock acquires the lock exclusive.
	Lock(ctx context.Context, path string) (release func(), err error)
}

// flockLocker implements Locker with flock(2) on the given lock file. Each
// acquisition opens its own file descriptor, so two goroutines in one
// process contend exactly like two processes (flock is per open file
// description); release closes the fd, dropping the lock even on crash.
type flockLocker struct{}

// lockPollInterval is the retry cadence for a contended flock. Polling with
// LOCK_NB keeps acquisition cancellable by ctx (a blocking flock(2) is not).
const lockPollInterval = 5 * time.Millisecond

func (flockLocker) RLock(ctx context.Context, path string) (func(), error) {
	return flockAcquire(ctx, path, syscall.LOCK_SH)
}

func (flockLocker) Lock(ctx context.Context, path string) (func(), error) {
	return flockAcquire(ctx, path, syscall.LOCK_EX)
}

func flockAcquire(ctx context.Context, path string, how int) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("repo: open lock file %s: %w", path, err)
	}
	for {
		err := syscall.Flock(int(f.Fd()), how|syscall.LOCK_NB)
		if err == nil {
			return func() {
				// LOCK_UN before close is redundant (close releases) but
				// makes the intent explicit and survives dup'd fds.
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = f.Close()
			return nil, fmt.Errorf("repo: flock %s: %w", path, err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("repo: waiting for lock %s: %w", path, ctx.Err())
		case <-time.After(lockPollInterval):
		}
	}
}
