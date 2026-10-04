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

package eventcore

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// hookRepair is the sweep's hook backstop (05 §7 step 4). A project create
// whose hook registration failed keeps going with a warning, and the row is
// left with no hook id; each pass ensures a hook for every such ready row,
// whatever its age (a row that holds an id keeps its hook: R12, no migration
// of old hooks).
//
// What it must never do is call GitHub every minute forever:
//
//   - an AE Studio that is not serving (restarting, provisioning) or absent
//     (the org's GitHub is not connected) is the ORG's state, so the org's
//     remaining rows wait for the next pass, and a pod that comes back or a
//     reconnect heals without a restart;
//   - any other permanent refusal (the repository gone from GitHub, an owner
//     the pod refuses, aep-api's own token refused) skips that row for the
//     life of the process, with one value-free eventcore.hook_repair_skipped
//     line. A repository fixed by hand gets its hook at the next restart;
//   - anything else (a 5xx, a rate limit) is retried on the next pass.
type hookRepair struct {
	ensurer HookEnsurer

	mu      sync.Mutex
	skipped map[RepoRef]struct{} // rows refused for good, keyed without HasHook
}

func (h *hookRepair) repair(ctx context.Context, repos []RepoRef) {
	if h.ensurer == nil {
		return
	}
	waiting := map[string]bool{} // orgs whose AE Studio does not serve this pass
	for _, repo := range repos {
		if repo.HasHook || waiting[repo.OrgID] || h.isSkipped(repo) {
			continue
		}
		_, err := h.ensurer.Register(ctx, repo.OrgID, repo.ProjectID)
		switch {
		case err == nil:
			slog.InfoContext(ctx, "eventcore.hook_repaired", "org", repo.OrgID, "project", repo.ProjectID)
		case errors.Is(err, sourcecontrol.ErrAEStudioUnavailable), errors.Is(err, sourcecontrol.ErrAEStudioAbsent):
			waiting[repo.OrgID] = true
			slog.DebugContext(ctx, "eventcore.hook_repair_deferred", "org", repo.OrgID, "reason", skipReason(err))
		case sourcecontrol.IsPermanent(err):
			h.skip(repo)
			slog.WarnContext(ctx, "eventcore.hook_repair_skipped", "org", repo.OrgID, "project", repo.ProjectID, "reason", skipReason(err))
		default:
			slog.WarnContext(ctx, "eventcore.hook_repair_failed", "org", repo.OrgID, "project", repo.ProjectID, "error", err)
		}
	}
}

func (h *hookRepair) isSkipped(repo RepoRef) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.skipped[skipKey(repo)]
	return ok
}

func (h *hookRepair) skip(repo RepoRef) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.skipped == nil {
		h.skipped = map[RepoRef]struct{}{}
	}
	h.skipped[skipKey(repo)] = struct{}{}
}

func skipKey(repo RepoRef) RepoRef {
	repo.HasHook = false
	return repo
}

// skipReason names why the hook repair passed a row by, without the error's
// text.
func skipReason(err error) string {
	switch {
	case errors.Is(err, sourcecontrol.ErrAEStudioAbsent):
		return "ae_studio_absent"
	case errors.Is(err, sourcecontrol.ErrAEStudioUnavailable):
		return "ae_studio_unavailable"
	case errors.Is(err, sourcecontrol.ErrAEStudioMisconfigured):
		return "ae_studio_misconfigured"
	case errors.Is(err, sourcecontrol.ErrOwnerNotAllowed):
		return "owner_not_allowed"
	case errors.Is(err, sourcecontrol.ErrRepoNotFound),
		sourcecontrol.IsHTTPStatus(err, http.StatusNotFound),
		sourcecontrol.IsHTTPStatus(err, http.StatusGone):
		return "repo_not_found"
	case sourcecontrol.IsHTTPStatus(err, http.StatusUnauthorized):
		return "github_unauthorized"
	default:
		return "refused"
	}
}
