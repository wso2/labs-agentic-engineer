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

	"github.com/wso2/asdlc/asdlc-service/internal/feature/requirements"
)

func registerCollabRoutes(rt *Router, mainMux *http.ServeMux, c requirements.CollabController, jwtMW func(http.Handler) http.Handler) {
	// User-facing: org-scoped, gated by the central tenant gate via the Router.
	rt.OrgScoped("GET /api/v1/organizations/{orgHandle}/projects/{projectName}/requirements/collab-session", c.GetCollabSession)

	// Server-to-server: the collab-server calls this with the user's Bearer JWT
	// (+ X-Room-Id) to authorize a room. INT-8 fix: wrapped with the jwt
	// verifier so the forwarded Bearer's signature is verified before the
	// handler runs; the handler then enforces that the verified caller's org
	// owns the room's project (§6.6g oracle). Registered on mainMux (not the
	// org-scoped apiMux) because it carries no {orgHandle} path var — the org
	// comes from the verified token, not the path.
	mainMux.Handle("GET /api/v1/collab/validate", jwtMW(http.HandlerFunc(c.ValidateCollabAccess)))
}
