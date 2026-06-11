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

// registerSkillRoutes wires the org-scoped skills catalogue. Inherits the
// JWT + org-ensure middleware that protects every other org-scoped route.
//
// `POST .../skills/import` is more specific than `{name}` and is registered
// for POST only, so it never collides with the create / update / delete
// `{name}` patterns. See docs/design/skills-system.md > "REST API".
func registerSkillRoutes(mux *http.ServeMux, c controllers.SkillController) {
	mux.HandleFunc("GET /api/v1/organizations/{orgHandle}/skills", c.List)
	mux.HandleFunc("POST /api/v1/organizations/{orgHandle}/skills", c.Create)
	mux.HandleFunc("POST /api/v1/organizations/{orgHandle}/skills/import", c.Import)
	mux.HandleFunc("GET /api/v1/organizations/{orgHandle}/skills/{name}", c.Get)
	mux.HandleFunc("PUT /api/v1/organizations/{orgHandle}/skills/{name}", c.Update)
	mux.HandleFunc("DELETE /api/v1/organizations/{orgHandle}/skills/{name}", c.Delete)
}
