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
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// askpassFileName must stay in sync with the engine's askpass shim basename.
const askpassFileName = "askpass.sh"

// reclaimTmp purges <root>/tmp entries older than TrashMaxAge, skipping the
// askpass shim. A SIGKILL or OOM mid-clone leaves complete bare trees here;
// nothing else reclaims them.
//
//deadcode:keep wired in Task 2.7
func (r *Reaper) reclaimTmp(ctx context.Context) error {
	tmpDir := repo.TmpDir(r.engine.Root())
	entries, err := os.ReadDir(tmpDir)
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
		if e.Name() == askpassFileName {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) <= r.cfg.TrashMaxAge {
			continue
		}
		if err := os.RemoveAll(filepath.Join(tmpDir, e.Name())); err != nil {
			slog.WarnContext(ctx, "reaper.tmp_purge_failed", "error", err)
		}
	}
	return nil
}
