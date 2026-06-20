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
	"github.com/wso2/asdlc/asdlc-service/internal/feature/requirements"
)

func registerRequirementsRoutes(rt *Router, c requirements.RequirementsController) {
	prefix := "/api/v1/organizations/{orgHandle}/projects/{projectName}/requirements"

	rt.OrgScoped("GET "+prefix, c.GetRequirements)
	rt.OrgScoped("PUT "+prefix+"/files/{name}", c.UpdateRequirementFile)
	rt.OrgScoped("DELETE "+prefix+"/files/{name}", c.DeleteRequirementFile)
	rt.OrgScoped("POST "+prefix+"/files/{name}/generate", c.GenerateRequirementFile)
	rt.OrgScoped("POST "+prefix+"/save", c.SaveAndProceed)
	rt.OrgScoped("POST "+prefix+"/discard", c.DiscardChanges)
	rt.OrgScoped("GET "+prefix+"/versions", c.ListVersions)
	rt.OrgScoped("GET "+prefix+"/versions/{tag}", c.GetRequirementsAtVersion)
}
