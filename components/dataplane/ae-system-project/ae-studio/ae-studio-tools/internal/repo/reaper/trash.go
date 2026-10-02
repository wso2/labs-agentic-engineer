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
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// reclaimTrash is phase 2 of the two-phase delete: remove every trash/<id>
// entry older than TrashMaxAge. By POSIX inode semantics a still-open fd keeps
// its content readable through the purge, so a mid-flight reader is never
// corrupted.
func (r *Reaper) reclaimTrash(ctx context.Context) error {
	return r.purgeTrash(ctx, func(e os.DirEntry, now time.Time) bool {
		return now.Sub(trashedAt(e)) > r.cfg.TrashMaxAge
	})
}

// purgeTrashAll removes every trash/<id> entry regardless of age. The budget
// pass and ForceSweep use it under pressure, so a rename into trash actually
// frees bytes.
func (r *Reaper) purgeTrashAll(ctx context.Context) error {
	return r.purgeTrash(ctx, func(os.DirEntry, time.Time) bool { return true })
}

// purgeTrash removes the trash/<id> entries due reports true for.
func (r *Reaper) purgeTrash(ctx context.Context, due func(e os.DirEntry, now time.Time) bool) error {
	trashDir := repo.TrashDir(r.engine.Root())
	entries, err := os.ReadDir(trashDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	now := time.Now()
	for _, e := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !due(e, now) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(trashDir, e.Name())); err != nil {
			slog.WarnContext(ctx, "reaper.trash_purge_failed", "error", err)
		}
	}
	return nil
}

// trashedAt derives when an entry landed in trash/. The primary source is the
// id itself: the engine mints trash ids as "%016x-<rand>" with the rename-time
// UnixNano, and os.Rename PRESERVES the renamed dir's own mtime (a dormant
// repo's subtree can carry an mtime far older than its trash time, which would
// defeat the TrashMaxAge grace). Unparseable names (foreign debris) fall back
// to the entry mtime.
func trashedAt(e os.DirEntry) time.Time {
	if hexTS, _, ok := strings.Cut(e.Name(), "-"); ok && len(hexTS) == 16 {
		if nanos, err := strconv.ParseUint(hexTS, 16, 64); err == nil && nanos <= uint64(1)<<62 {
			return time.Unix(0, int64(nanos))
		}
	}
	if info, err := e.Info(); err == nil {
		return info.ModTime()
	}
	return time.Time{} // stat raced with removal: treat as ancient
}
