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

package componenttest

import (
	"encoding/json"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
)

// hdrClaims carries a JSON-encoded auth.Claims from the request builder to
// fakeInboundAuth. A private header (deleted before the request proceeds) keeps
// the injector parallel-safe: no shared process state, one claims value per
// request, so table tests with t.Parallel() don't race.
const hdrClaims = "X-Componenttest-Claims"

// TestBearer is the stand-in bearer fakeInboundAuth stores alongside the
// claims (production: ExtractAuthToken). Tests asserting a forwarded caller
// token compare against this.
const TestBearer = "componenttest-bearer"

// fakeInboundAuth is the component tier's stand-in for the production
// JWKS-backed auth.Middleware. It does NOT verify a
// token — it projects the harness-supplied claims into the request context at
// the exact auth.WithClaims seam a verified Thunder token would use, then runs
// the request through the rest of the real chain (orgensure → the
// deny-by-default tenant gate in ENFORCE). No header (NoAuth) ⇒ no claims ⇒ an
// org-scoped op 401s at the gate's no-claims branch — the same code path
// production runs, minus the signature.
func fakeInboundAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if raw := r.Header.Get(hdrClaims); raw != "" {
			var c auth.Claims
			if err := json.Unmarshal([]byte(raw), &c); err == nil {
				ctx := auth.WithClaims(r.Context(), &c)
				// Production's verifier keeps the verified token claims in
				// context too (the vault path reads the ouId from them);
				// mirror the projection back.
				ctx = jwtassertion.ContextWithTokenClaims(ctx, tokenClaimsOf(&c))
				// Production's ExtractAuthToken stores the request bearer next
				// to the verified claims; mirror it with a fixed stand-in so
				// features that forward the caller's token (collab turns,
				// OC user-JWT path) see one. TestBearer is the assertable value.
				r = r.WithContext(auth.WithAuthToken(ctx, TestBearer))
			}
			r.Header.Del(hdrClaims)
		}
		next.ServeHTTP(w, r)
	})
}

// tokenClaimsOf is the inverse of the production projection (auth.claimsOf):
// the token claims a verified token with these claims carries.
func tokenClaimsOf(c *auth.Claims) *jwtassertion.TokenClaims {
	return &jwtassertion.TokenClaims{
		Sub:        c.Subject,
		ClientID:   c.ClientID,
		OuHandle:   c.OuHandle,
		OuName:     c.OuName,
		OuId:       c.OuId,
		Name:       c.Name,
		Email:      c.Email,
		GivenName:  c.GivenName,
		FamilyName: c.FamilyName,
	}
}
