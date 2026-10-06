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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// /api/v1 takes user JWTs only: the SRE handoff bearer that once
// cleared list-issues/create-issue there is refused on the real JWT path, even
// with the verifier configured. Its ops now live under /internal/v1/sre
// (internal_sre_test.go).
func TestSREHandoff_RefusedOnPublicAPI(t *testing.T) {
	handler := NewHandler(AppParams{
		Config:       config.Config{},
		InternalDeps: InternalDeps{SREHandoff: auth.NewSREHandoffVerifier("s3cr3t", "acme")},
	})
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/v1/projects/hello/issues", nil)
			req.Header.Set("Authorization", "Bearer s3cr3t")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (the SRE bearer is not a user JWT)", w.Code)
			}
		})
	}
}
