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

package api

import (
	"github.com/wso2/asdlc/asdlc-service/controllers"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/gitrepo"
)

// registerRepoOnlyRoutes wires every Service-JWT-gated repo / git-ops
// endpoint through the typed ServiceRouter (allowlist-by-construction). The
// {orgId}/{projectId} routes register via RepoScoped (RequireOrgScope —
// validates the pair, 404s a cross-org miss, binds a Service-JWT Caller). The
// repoScope inside the ServiceRouter may be nil in dev/test setups where the
// middleware is disabled (no RepoRepository wired); those routes degrade to a
// no-op wrap while keeping their handler bindings.
func registerRepoOnlyRoutes(
	rt *ServiceRouter,
	rc gitrepo.RepoController,
	gc gitrepo.GitOpsController,
	ic gitrepo.IssueController,
	bc gitrepo.BranchController,
	pc gitrepo.PullRequestController,
	wc gitrepo.WebhookRegistrationController,
	ac controllers.ArtifactController,
) {
	// CreateRepo is the one route without a path projectId — orgId travels
	// in the body and is validated downstream by the controller. Enumerated
	// carve-out: no path org var to scope on.
	rt.Public("POST /api/v1/repos", rc.CreateRepo)

	// ---- Repo lifecycle ----
	rt.RepoScoped("GET /api/v1/repos/{orgId}/{projectId}", rc.GetRepo)
	rt.RepoScoped("DELETE /api/v1/repos/{orgId}/{projectId}", rc.DeleteRepo)

	// ---- Git ops ----
	rt.RepoScoped("POST /api/v1/repos/{orgId}/{projectId}/commit", gc.Commit)
	rt.RepoScoped("POST /api/v1/repos/{orgId}/{projectId}/push", gc.Push)
	rt.RepoScoped("POST /api/v1/repos/{orgId}/{projectId}/pull", gc.Pull)
	rt.RepoScoped("GET /api/v1/repos/{orgId}/{projectId}/status", gc.Status)

	// ---- Tag primitives (still used by build watcher etc.) ----
	rt.RepoScoped("POST /api/v1/repos/{orgId}/{projectId}/tags", gc.CreateTag)
	rt.RepoScoped("GET /api/v1/repos/{orgId}/{projectId}/tags", gc.ListTags)
	rt.RepoScoped("GET /api/v1/repos/{orgId}/{projectId}/tags/{tag}/file", gc.GetFileAtTag)

	// ---- Issues ----
	rt.RepoScoped("POST /api/v1/repos/{orgId}/{projectId}/issues", ic.CreateIssue)
	rt.RepoScoped("GET /api/v1/repos/{orgId}/{projectId}/issues", ic.ListIssues)
	rt.RepoScoped("POST /api/v1/repos/{orgId}/{projectId}/issues/{number}/close", ic.CloseIssue)
	rt.RepoScoped("POST /api/v1/repos/{orgId}/{projectId}/issues/{number}/comments", ic.CommentIssue)
	rt.RepoScoped("PATCH /api/v1/repos/{orgId}/{projectId}/issues/{number}/body", ic.EditIssueBody)

	// ---- Branch / PR / webhook ----
	rt.RepoScoped("POST /api/v1/repos/{orgId}/{projectId}/branches", bc.CreateBranch)
	rt.RepoScoped("POST /api/v1/repos/{orgId}/{projectId}/branches/seed-commit", bc.SeedCommit)
	rt.RepoScoped("POST /api/v1/repos/{orgId}/{projectId}/pulls", pc.CreateDraftPR)
	rt.RepoScoped("POST /api/v1/repos/{orgId}/{projectId}/webhooks", wc.Register)
	rt.RepoScoped("DELETE /api/v1/repos/{orgId}/{projectId}/webhooks", wc.Deregister)

	// ---- Artifacts: requirements (multi-file directory, tagged v<N>) ----
	if ac != nil {
		rt.RepoScoped("GET /api/v1/repos/{orgId}/{projectId}/artifacts/requirements", ac.ListRequirements)
		rt.RepoScoped("GET /api/v1/repos/{orgId}/{projectId}/artifacts/requirements/files/{name}", ac.GetRequirementFile)
		rt.RepoScoped("PUT /api/v1/repos/{orgId}/{projectId}/artifacts/requirements/files/{name}", ac.PutRequirementFile)
		rt.RepoScoped("DELETE /api/v1/repos/{orgId}/{projectId}/artifacts/requirements/files/{name}", ac.DeleteRequirementFile)
		rt.RepoScoped("POST /api/v1/repos/{orgId}/{projectId}/artifacts/requirements/save", ac.SaveRequirements)
		rt.RepoScoped("POST /api/v1/repos/{orgId}/{projectId}/artifacts/requirements/discard", ac.DiscardRequirements)
		rt.RepoScoped("GET /api/v1/repos/{orgId}/{projectId}/artifacts/requirements/versions", ac.ListRequirementsVersions)
		rt.RepoScoped("GET /api/v1/repos/{orgId}/{projectId}/artifacts/requirements/versions/{tag}", ac.GetRequirementsVersion)

		// Requirements snapshots (BFF-only — IDs are `t_<ulid>` for per-turn
		// undo and `sb_<ulid>` for chat session baselines).
		rt.RepoScoped("POST /api/v1/repos/{orgId}/{projectId}/artifacts/requirements/snapshots/{id}", ac.CaptureRequirementsSnapshot)
		rt.RepoScoped("POST /api/v1/repos/{orgId}/{projectId}/artifacts/requirements/snapshots/{id}/restore", ac.RestoreRequirementsSnapshot)
		rt.RepoScoped("DELETE /api/v1/repos/{orgId}/{projectId}/artifacts/requirements/snapshots/{id}", ac.DeleteRequirementsSnapshot)
		rt.RepoScoped("GET /api/v1/repos/{orgId}/{projectId}/artifacts/requirements/snapshots/{id}/files/{name}", ac.GetRequirementsSnapshotFile)

		// ---- Artifacts: design (multi-file directory, tagged v<N>-<M>) ----
		rt.RepoScoped("GET /api/v1/repos/{orgId}/{projectId}/artifacts/design", ac.ListDesign)
		rt.RepoScoped("GET /api/v1/repos/{orgId}/{projectId}/artifacts/design/files/{path...}", ac.GetDesignFile)
		rt.RepoScoped("PUT /api/v1/repos/{orgId}/{projectId}/artifacts/design/files/{path...}", ac.PutDesignFile)
		rt.RepoScoped("DELETE /api/v1/repos/{orgId}/{projectId}/artifacts/design/files/{path...}", ac.DeleteDesignFile)
		rt.RepoScoped("DELETE /api/v1/repos/{orgId}/{projectId}/artifacts/design/directories/{path...}", ac.DeleteDesignDirectory)
		rt.RepoScoped("POST /api/v1/repos/{orgId}/{projectId}/artifacts/design/save", ac.SaveDesign)
		rt.RepoScoped("POST /api/v1/repos/{orgId}/{projectId}/artifacts/design/discard", ac.DiscardDesign)
		rt.RepoScoped("GET /api/v1/repos/{orgId}/{projectId}/artifacts/design/versions", ac.ListDesignVersions)
		rt.RepoScoped("GET /api/v1/repos/{orgId}/{projectId}/artifacts/design/versions/{tag}", ac.GetDesignVersion)
	}
}
