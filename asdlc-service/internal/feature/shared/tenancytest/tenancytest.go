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

// Package tenancytest is the shared kit that asserts the tenant gate's
// cross-org deny for any gated route. It plugs into the controller http_test
// template (§8.0): every gated route gets an AssertCrossOrgDenied case, which
// is the structural regression lock that keeps the IDOR class closed (§10).
package tenancytest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/middleware/jwt"
)

// UserRequest builds an *http.Request carrying the User-JWT claims the auth
// middleware would have projected for tokenOrg, targeting target. It lets a
// test drive a gated handler without standing up the JWKS verifier.
func UserRequest(method, target, tokenOrg string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	claims := &jwt.Claims{Subject: "user"}
	if tokenOrg != "" {
		claims.OuHandle = tokenOrg
		claims.OuId = tokenOrg + "-ouid"
		claims.Subject = "user@" + tokenOrg
	}
	return r.WithContext(jwt.WithClaims(r.Context(), claims))
}

// AssertCrossOrgDenied fails t unless handler returns 404 for a request whose
// JWT org (tokenOrg) differs from the org named in target. The same 404 body
// must be returned for wrong-org and no-such-org so existence is never leaked.
func AssertCrossOrgDenied(t testing.TB, handler http.Handler, method, target, tokenOrg string) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, UserRequest(method, target, tokenOrg))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-org %s %s as org %q: want 404, got %d (body=%s)",
			method, target, tokenOrg, rec.Code, rec.Body.String())
	}
}
