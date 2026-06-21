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

	"github.com/wso2/asdlc/asdlc-service/internal/feature/orgcreds"
	"github.com/wso2/asdlc/asdlc-service/utils/validate"
)

// registerAnthropicEffectiveKeyUnauth mounts the single resolver route
// the agents-service calls without an Authorization header. Lives on
// the outer mux (not behind ServiceJWT) — agents-service's cloud
// release-binding carries no SERVICE_AUTH_GIT_* envs. The handler never
// returns the raw key to any other caller; this endpoint is for
// agents-service alone.
//
// See docs/design/anthropic-key-dual-token.md.
func registerAnthropicEffectiveKeyUnauth(mux *http.ServeMux, svc *orgcreds.AnthropicCredentialService) {
	mux.HandleFunc("GET /internal/credentials/orgs/{orgHandle}/anthropic/effective-key", func(w http.ResponseWriter, r *http.Request) {
		ocOrgID := r.PathValue("orgHandle")
		if err := validate.Slug(ocOrgID); err != nil {
			writeJSONError(w, http.StatusBadRequest, "orgHandle: "+err.Error())
			return
		}
		out, err := svc.EffectiveKey(r.Context(), ocOrgID)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}
