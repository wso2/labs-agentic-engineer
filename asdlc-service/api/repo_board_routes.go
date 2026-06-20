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
	"net/http"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/gitrepo"
)

// registerRepoBoardRoutes wires the GitHub Projects board endpoints through the
// typed ServiceRouter. The org segment was added (was `/repos/{projectId}/board`)
// so the route names the org and can be repo-scoped — closing the F3
// confused-deputy where any caller could read/move another project's board by
// projectId alone (§6.6f). RepoScoped applies RequireOrgScope, which validates
// the (orgId, projectId) pair against git_repositories and 404s a cross-org pair
// before the handler runs.
func registerRepoBoardRoutes(rt *ServiceRouter, bc gitrepo.RepoBoardController) {
	if bc == nil {
		return
	}
	rt.HandleRepoScoped("GET /api/v1/repos/{orgId}/{projectId}/board", http.HandlerFunc(bc.GetBoard))
	rt.HandleRepoScoped("POST /api/v1/repos/{orgId}/{projectId}/board/move", http.HandlerFunc(bc.MoveIssueToStatus))
}
