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

	"github.com/wso2/asdlc/asdlc-service/controllers"
)

// registerWebhookRoutes mounts the inbound GitHub webhook receiver. The routes
// live outside the JWT middleware (same pattern the now-removed /mcp/ mount
// used) — webhooks authenticate via HMAC, not JWT. Both patterns are more
// specific than the JWT-gated "/api/" subtree, so net/http's ServeMux routes
// them here rather than into the auth middleware.
//
// Two paths are registered for the same handler:
//   - /webhooks/github         — local/dev (smee delivers here).
//   - /api/v1/webhooks/github  — cloud. The gateway's webhook endpoint is
//     scoped to base path /api/v1/webhooks and forwards to the BFF verbatim,
//     so GitHub deliveries arrive here. (jwtAuth is disabled on that gateway
//     endpoint; the per-repo webhook URL is GITHUB_WEBHOOK_DELIVERY_URL.)
func registerWebhookRoutes(mux *http.ServeMux, c controllers.WebhookController) {
	mux.HandleFunc("POST /webhooks/github", c.Receive)
	mux.HandleFunc("POST /api/v1/webhooks/github", c.Receive)
}
