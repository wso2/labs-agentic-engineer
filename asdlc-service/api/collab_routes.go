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

func registerCollabRoutes(apiMux *http.ServeMux, mainMux *http.ServeMux, c controllers.CollabController) {
	// User-facing: protected by the JWT middleware via apiMux.
	apiMux.HandleFunc("GET /api/v1/organizations/{orgHandle}/projects/{projectName}/requirements/collab-session", c.GetCollabSession)

	// Server-to-server: collab-server calls this with the user's Bearer JWT to
	// validate identity. Registered on mainMux so it bypasses the standard JWT
	// middleware; the controller validates the token inline.
	mainMux.HandleFunc("GET /api/v1/collab/validate", c.ValidateCollabAccess)
}
