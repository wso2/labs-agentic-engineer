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
)

func registerDesignRoutes(rt *Router, c controllers.DesignController) {
	// Assembled Design view (used by cell diagram + downstream code).
	rt.OrgScoped("GET /api/v1/organizations/{orgHandle}/projects/{projectName}/design", c.GetDesign)

	// Multi-file bundle view (used by the Explorer architecture page).
	rt.OrgScoped("GET /api/v1/organizations/{orgHandle}/projects/{projectName}/design/bundle", c.GetDesignBundle)
	rt.OrgScoped("PUT /api/v1/organizations/{orgHandle}/projects/{projectName}/design/files/{path...}", c.UpdateDesignFile)
	rt.OrgScoped("DELETE /api/v1/organizations/{orgHandle}/projects/{projectName}/design/files/{path...}", c.DeleteDesignFile)
	rt.OrgScoped("DELETE /api/v1/organizations/{orgHandle}/projects/{projectName}/design/components/{componentName}", c.DeleteComponent)

	// Whole-design generation (architect agent).
	rt.OrgScoped("POST /api/v1/organizations/{orgHandle}/projects/{projectName}/design/generate", c.GenerateDesign)

	// Save / discard / versions.
	rt.OrgScoped("POST /api/v1/organizations/{orgHandle}/projects/{projectName}/design/save", c.SaveAndProceed)
	rt.OrgScoped("POST /api/v1/organizations/{orgHandle}/projects/{projectName}/design/discard", c.DiscardChanges)
	rt.OrgScoped("GET /api/v1/organizations/{orgHandle}/projects/{projectName}/design/versions", c.ListDesignVersions)
	rt.OrgScoped("GET /api/v1/organizations/{orgHandle}/projects/{projectName}/design/versions/{tag}", c.GetDesignAtTag)
	rt.OrgScoped("GET /api/v1/organizations/{orgHandle}/projects/{projectName}/design/versions/{tag}/bundle", c.GetDesignBundleAtTag)
}
