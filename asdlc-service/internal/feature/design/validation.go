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

package design

import (
	"net/http"

	"github.com/wso2/asdlc/asdlc-service/internal/platform/httpkit"
)

// requireOrgHandle validates the {orgHandle} path param. Returns true if
// validation passed; on failure writes a 400 to w. orgHandle flows into
// OpenChoreo namespace lookups, GitHub repo paths, and OpenBao keys —
// the slug invariant is the cross-tenant fence. Thin delegate over the
// shared httpkit.RequireSlug logic.
func requireOrgHandle(w http.ResponseWriter, v string) bool {
	return httpkit.RequireSlug(w, "orgHandle", v)
}

// requireProjectName validates the {projectName} path param. Same shape as
// orgHandle — DNS-label-shaped slug. Used in repo paths, k8s resource
// names, GitHub repo slugs.
func requireProjectName(w http.ResponseWriter, v string) bool {
	return httpkit.RequireSlug(w, "projectName", v)
}
