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
	"github.com/wso2/asdlc/asdlc-service/internal/feature/idp"
)

// registerIDPRoutes wires the /api/v1/organizations/{orgHandle}/idp-profile
// endpoints. GetProfile is what the console org-settings page reads;
// RegenerateSecret is the admin-only emergency-rotate path. Per-org IDP
// profile creation is automatic — no POST endpoint here because trait_sync
// triggers EnsureOrgPublisher on first protected-component deploy.
//
// The org path var was renamed {orgId}→{orgHandle} so every gated route names
// the org uniformly and the gate's allowlist invariant holds (closes IDOR-2,
// §6.1b/§6.6f). The value has always been the handle, not a UUID.
func registerIDPRoutes(rt *Router, c idp.IDPController) {
	rt.OrgScoped("GET /api/v1/organizations/{orgHandle}/idp-profile", c.GetProfile)
	rt.OrgScoped("PUT /api/v1/organizations/{orgHandle}/idp-profile", c.UpdateProfile)
	rt.OrgScoped("POST /api/v1/organizations/{orgHandle}/idp-profile/rotate", c.RegenerateSecret)
	// Unscoped helper used by the IDP picker — needs only a User JWT, not an
	// org assignment (enumerated carve-out, §6.6f). Phase 7 BYO-IDP form
	// auto-populates JWKS URL.
	rt.Public("GET /api/v1/idp/discover", c.DiscoverIssuer)
}
