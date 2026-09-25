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

	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
)

const (
	maintainLooseGate  = 1000
	maintainPackGate   = 20
	maintainRepoBudget = 10
	// maintainWorkTimeout caps one mirror's repack/prune/pack-refs so a
	// stuck git child cannot pin the leader sweep indefinitely. Lock
	// acquisition is bounded inside Engine.MaintainMirror (~2s), not here.
	maintainWorkTimeout = 5 * time.Minute
)

func shouldMaintain(loose, packs int) bool {
	return loose > maintainLooseGate || packs > maintainPackGate
}

// maintainRepos is leader-only, ordered BEFORE quota so eviction sees
// reclaimed space. Never git gc (gc.pid hostname trap). Never
// `git maintenance --task=loose-objects` (transient growth). Returns the
// number of mirrors successfully maintained this pass (R8b usage line).
func (r *Reaper) maintainRepos(ctx context.Context) (int, error) {
	done := 0
	err := r.walkRepoDirs(ctx, func(orgID, projectID, repoSlug, slugDir string) {
		if done >= maintainRepoBudget || ctx.Err() != nil {
			return
		}
		gitDir := gitfs.GitSubdir(slugDir)
		if _, err := os.Stat(filepath.Join(gitDir, "HEAD")); err != nil {
			return
		}
		ref := gitfs.RepoRef{OrgID: orgID, ProjectID: projectID, RepoSlug: repoSlug}
		loose, packs, err := r.engine.CountObjects(ctx, ref)
		if err != nil {
			slog.WarnContext(ctx, "reaper: count-objects failed", "gitDir", gitDir, "error", err)
			return
		}
		if !shouldMaintain(loose, packs) {
			return
		}
		workCtx, cancel := context.WithTimeout(ctx, maintainWorkTimeout)
		err = r.engine.MaintainMirror(workCtx, ref)
		cancel()
		if err != nil {
			slog.InfoContext(ctx, "reaper: skipping mirror maintenance",
				"org", orgID, "project", projectID, "slug", repoSlug, "reason", err)
			return
		}
		done++
		slog.InfoContext(ctx, "reaper: maintained mirror",
			"org", orgID, "project", projectID, "slug", repoSlug, "looseBefore", loose, "packsBefore", packs)
	})
	return done, err
}
