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

package orgcreds

import (
	"net/http"

	"github.com/wso2/asdlc/asdlc-service/utils"
	"github.com/wso2/asdlc/asdlc-service/utils/validate"
)

// requireOrgHandle validates the {orgHandle} path param. Returns true if
// validation passed; on failure writes a 400 to w. orgHandle flows into
// OpenChoreo namespace lookups, GitHub repo paths, and OpenBao keys —
// the slug invariant is the cross-tenant fence.
//
// This mirrors the same helper in package controllers; the credential
// controllers that moved into orgcreds keep a local copy so the package
// has no dependency on controllers.
func requireOrgHandle(w http.ResponseWriter, v string) bool {
	if err := validate.Slug(v); err != nil {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "orgHandle: "+err.Error())
		return false
	}
	return true
}
