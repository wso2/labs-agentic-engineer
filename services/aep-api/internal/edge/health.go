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

package edge

import (
	"encoding/json"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// healthz is liveness: unauthenticated, always 200.
func healthz() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
	})
}

// readyz is readiness: 200 once the server is up. aep-api holds no local git
// state, so there is nothing else for it to wait on.
func readyz() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
	})
}

// taskTokenJWKS serves the public key set for BFF-signed tokens. A plain
// handler, not a contract op, so it stays off the /api/v1 base path. Publisher
// tokens verify against platform-idp's JWKS, not this endpoint.
func taskTokenJWKS(tt *auth.TaskTokenManager) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if tt != nil {
			_ = json.NewEncoder(w).Encode(tt.JWKS())
			return
		}
		_, _ = w.Write([]byte(`{"keys":[]}`))
	})
}
