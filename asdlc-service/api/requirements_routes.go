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

func registerRequirementsRoutes(mux *http.ServeMux, c controllers.RequirementsController) {
	prefix := "/api/v1/organizations/{orgHandle}/projects/{projectName}/requirements"

	mux.HandleFunc("GET "+prefix, c.GetRequirements)
	mux.HandleFunc("PUT "+prefix+"/files/{name}", c.UpdateRequirementFile)
	mux.HandleFunc("DELETE "+prefix+"/files/{name}", c.DeleteRequirementFile)
	mux.HandleFunc("POST "+prefix+"/files/{name}/generate", c.GenerateRequirementFile)
	mux.HandleFunc("POST "+prefix+"/save", c.SaveAndProceed)
	mux.HandleFunc("POST "+prefix+"/discard", c.DiscardChanges)
	mux.HandleFunc("GET "+prefix+"/versions", c.ListVersions)
	mux.HandleFunc("GET "+prefix+"/versions/{tag}", c.GetRequirementsAtVersion)
}
