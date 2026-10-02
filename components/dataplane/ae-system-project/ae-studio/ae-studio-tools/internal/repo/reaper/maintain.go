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

const (
	maintainLooseGate  = 1000
	maintainPackGate   = 20
	maintainRepoBudget = 10
	// maintainWorkTimeout caps one mirror's repack/prune/pack-refs so a stuck
	// git child cannot pin the sweep. Lock acquisition is bounded inside
	// Engine.MaintainMirror (~2s), not here.
	maintainWorkTimeout = 5 * time.Minute
)

func shouldMaintain(loose, packs int) bool {
	return loose > maintainLooseGate || packs > maintainPackGate
}

// maintainRepos runs before the budget pass so eviction sees reclaimed space.
// Never git gc (gc.pid hostname trap), never `git maintenance
// --task=loose-objects` (transient growth). A mirror whose lock is busy is
// skipped until the next sweep. Returns the number of mirrors maintained.
func (r *Reaper) maintainRepos(ctx context.Context) (int, error) {
	done := 0
	err := r.walkRepoDirs(ctx, func(ref repo.RepoRef, repoDir string) {
		if done >= maintainRepoBudget || ctx.Err() != nil {
			return
		}
		if _, err := os.Stat(filepath.Join(repo.GitSubdir(repoDir), "HEAD")); err != nil {
			return
		}
		loose, packs, err := r.engine.CountObjects(ctx, ref)
		if err != nil {
			slog.WarnContext(ctx, "reaper.count_objects_failed", "repo", repoName(ref), "error", err)
			return
		}
		if !shouldMaintain(loose, packs) {
			return
		}
		workCtx, cancel := context.WithTimeout(ctx, maintainWorkTimeout)
		err = r.engine.MaintainMirror(workCtx, ref)
		cancel()
		if err != nil {
			slog.InfoContext(ctx, "reaper.maintain_skipped", "repo", repoName(ref), "reason", err)
			return
		}
		done++
		slog.InfoContext(ctx, "reaper.maintained", "repo", repoName(ref), "looseBefore", loose, "packsBefore", packs)
	})
	return done, err
}
