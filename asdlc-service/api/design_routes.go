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

func registerDesignRoutes(mux *http.ServeMux, c controllers.DesignController) {
	// Assembled Design view (used by cell diagram + downstream code).
	mux.HandleFunc("GET /api/v1/organizations/{orgHandle}/projects/{projectName}/design", c.GetDesign)

	// Multi-file bundle view (used by the Explorer architecture page).
	mux.HandleFunc("GET /api/v1/organizations/{orgHandle}/projects/{projectName}/design/bundle", c.GetDesignBundle)
	mux.HandleFunc("PUT /api/v1/organizations/{orgHandle}/projects/{projectName}/design/files/{path...}", c.UpdateDesignFile)
	mux.HandleFunc("DELETE /api/v1/organizations/{orgHandle}/projects/{projectName}/design/files/{path...}", c.DeleteDesignFile)
	mux.HandleFunc("DELETE /api/v1/organizations/{orgHandle}/projects/{projectName}/design/components/{componentName}", c.DeleteComponent)

	// Whole-design generation (architect agent).
	mux.HandleFunc("POST /api/v1/organizations/{orgHandle}/projects/{projectName}/design/generate", c.GenerateDesign)

	// Save / discard / versions.
	mux.HandleFunc("POST /api/v1/organizations/{orgHandle}/projects/{projectName}/design/save", c.SaveAndProceed)
	mux.HandleFunc("POST /api/v1/organizations/{orgHandle}/projects/{projectName}/design/discard", c.DiscardChanges)
	mux.HandleFunc("GET /api/v1/organizations/{orgHandle}/projects/{projectName}/design/versions", c.ListDesignVersions)
	mux.HandleFunc("GET /api/v1/organizations/{orgHandle}/projects/{projectName}/design/versions/{tag}", c.GetDesignAtTag)
	mux.HandleFunc("GET /api/v1/organizations/{orgHandle}/projects/{projectName}/design/versions/{tag}/bundle", c.GetDesignBundleAtTag)
}
