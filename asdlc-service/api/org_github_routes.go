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

	"github.com/wso2/asdlc/asdlc-service/internal/feature/orgcreds"
)

// registerOrgGitHubRoutes wires the per-org GitHub integration surface.
//
// Org-scoped routes mount under the existing /api/v1/organizations/{orgHandle}
// prefix and inherit the central tenant gate that protects every other
// org-scoped route. The connect callback is unscoped (registerConnectCallbackRoute)
// — GitHub's single configured callback URL has no orgHandle to thread, so the
// JWT-carried `state` parameter is the org-binding signal.
func registerOrgGitHubRoutes(rt *Router, c orgcreds.OrgGitHubController) {
	rt.OrgScoped("POST /api/v1/organizations/{orgHandle}/github/connect/start", c.StartConnect)
	rt.OrgScoped("POST /api/v1/organizations/{orgHandle}/github/pat", c.ConnectPAT)
	rt.OrgScoped("GET /api/v1/organizations/{orgHandle}/github", c.GetStatus)
	rt.OrgScoped("DELETE /api/v1/organizations/{orgHandle}/github", c.Disconnect)
}

// registerConnectCallbackRoute mounts the App-mode connect callback
// OUTSIDE the JWT-protected mux. GitHub redirects the user's browser
// here with the OAuth code or post-install installation_id; we verify
// the connect-state JWT (issued by StartConnect) instead of the console
// JWT. This is an enumerated carve-out: the signed connect-state is the
// authn, bound to the org from that state (SourcePublisherCC, §6.6f).
func registerConnectCallbackRoute(mux *http.ServeMux, c orgcreds.OrgGitHubController) {
	mux.HandleFunc("GET /api/v1/github/connect/callback", c.HandleConnectCallback)
}
