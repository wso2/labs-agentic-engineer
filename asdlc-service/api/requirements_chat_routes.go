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

func registerRequirementsChatRoutes(mux *http.ServeMux, c controllers.RequirementsChatController) {
	prefix := "/api/v1/organizations/{orgHandle}/projects/{projectName}/requirements"
	mux.HandleFunc("POST "+prefix+"/chat", c.StreamChat)
	mux.HandleFunc("POST "+prefix+"/chat/turns/{turnId}/undo", c.UndoTurn)
	// Per-file Accept / Revert against the chat-session baseline. The
	// baseline ID is established by the SSE `data-session-baseline` frame
	// on the first turn and persisted client-side under the chat blob.
	mux.HandleFunc("GET "+prefix+"/chat/baseline/{baselineId}/files/{name}", c.GetBaselineFile)
	mux.HandleFunc("POST "+prefix+"/chat/baseline/{baselineId}/files/{name}/revert", c.RevertBaselineFile)
	mux.HandleFunc("DELETE "+prefix+"/chat/baseline/{baselineId}", c.DropBaseline)
}
