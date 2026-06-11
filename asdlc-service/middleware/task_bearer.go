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

package middleware

import (
	"context"
	"net/http"

	"github.com/wso2/asdlc/asdlc-service/middleware/jwtassertion"
	"github.com/wso2/asdlc/asdlc-service/internal/credentials"
)

type bearerContextKey string

const claimsKey bearerContextKey = "taskBearerClaims"

// RequireTaskBearer returns a middleware that verifies the Authorization
// header as an RS256 Task JWT, parses claims, and stores them in the
// request context for downstream handlers.
//
// The Task JWT is signed by the BFF (RSA private key) and verified here
// against the BFF's published JWKS. Audience MUST equal "git-service" so
// a token leaked to a different service can't be replayed against this
// /credentials/refresh endpoint.
func RequireTaskBearer(verifier jwtassertion.Middleware) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return verifier(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tc := jwtassertion.GetTokenClaims(r.Context())
			if tc == nil {
				// jwtassertion.Authenticator already wrote the 401; this
				// branch only runs in the (impossible) success-without-claims
				// path. Defensive.
				http.Error(w, "missing claims", http.StatusUnauthorized)
				return
			}
			claims := &credentials.TaskBearerClaims{
				TaskID:  tc.TaskID,
				OcOrgID: tc.OcOrgID,
			}
			if tc.IssuedAt != nil {
				claims.IssuedAt = tc.IssuedAt.Unix()
			}
			if tc.ExpiresAt != nil {
				claims.ExpiresAt = tc.ExpiresAt.Unix()
			}
			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		}))
	}
}

// TaskBearerClaims pulls the verified claims out of the request context.
// Returns nil if the request did not pass through RequireTaskBearer.
func TaskBearerClaims(ctx context.Context) *credentials.TaskBearerClaims {
	if c, ok := ctx.Value(claimsKey).(*credentials.TaskBearerClaims); ok {
		return c
	}
	return nil
}
