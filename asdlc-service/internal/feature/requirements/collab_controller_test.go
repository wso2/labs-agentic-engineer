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

package requirements

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	jwtmw "github.com/wso2/asdlc/asdlc-service/middleware/jwt"
	"github.com/wso2/asdlc/asdlc-service/models"
)

type fakeRepoOracle struct {
	repo *models.GitRepository
	err  error
}

func (f fakeRepoOracle) GetRepo(_ context.Context, _, _ string) (*models.GitRepository, error) {
	return f.repo, f.err
}

// TestValidateCollabAccess_INT8 covers the INT-8 authorization logic of the
// server-to-server collab validate endpoint. JWT signature verification is
// delegated to the jwt middleware that wraps the route (so a forged token never
// reaches the handler → here the no-claims path is the backstop). The handler
// owns the project-ownership oracle check keyed off the verified org.
func TestValidateCollabAccess_INT8(t *testing.T) {
	const verifiedOrg = "org-a"

	tests := []struct {
		name     string
		claims   *jwtmw.Claims // nil → unverified / missing token
		room     string
		oracle   fakeRepoOracle
		wantCode int
	}{
		{
			name:     "no verified claims → 401 (forged/unverified token backstop)",
			claims:   nil,
			room:     "spec-org-a-proj1",
			oracle:   fakeRepoOracle{repo: &models.GitRepository{}},
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "missing X-Room-Id → 400",
			claims:   &jwtmw.Claims{OuHandle: verifiedOrg},
			room:     "",
			oracle:   fakeRepoOracle{repo: &models.GitRepository{}},
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "cross-org room → 403 (org-A denied org-B's room)",
			claims:   &jwtmw.Claims{OuHandle: verifiedOrg},
			room:     "spec-org-b-proj1",
			oracle:   fakeRepoOracle{repo: &models.GitRepository{}},
			wantCode: http.StatusForbidden,
		},
		{
			name:     "own org but project not provisioned → 403",
			claims:   &jwtmw.Claims{OuHandle: verifiedOrg},
			room:     "spec-org-a-ghost",
			oracle:   fakeRepoOracle{repo: nil},
			wantCode: http.StatusForbidden,
		},
		{
			name:     "own org + project exists → 200",
			claims:   &jwtmw.Claims{OuHandle: verifiedOrg},
			room:     "spec-org-a-proj1",
			oracle:   fakeRepoOracle{repo: &models.GitRepository{}},
			wantCode: http.StatusOK,
		},
		{
			name:     "own org + hyphenated project name resolves via known-org prefix → 200",
			claims:   &jwtmw.Claims{OuHandle: verifiedOrg},
			room:     "spec-org-a-extract-e2e-620",
			oracle:   fakeRepoOracle{repo: &models.GitRepository{}},
			wantCode: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NewCollabController(tc.oracle)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/collab/validate", nil)
			if tc.room != "" {
				req.Header.Set("X-Room-Id", tc.room)
			}
			if tc.claims != nil {
				req = req.WithContext(jwtmw.WithClaims(req.Context(), tc.claims))
			}
			rec := httptest.NewRecorder()
			c.ValidateCollabAccess(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("got %d, want %d (body: %s)", rec.Code, tc.wantCode, rec.Body.String())
			}
		})
	}
}
