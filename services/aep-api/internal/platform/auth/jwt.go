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

// The BFF inbound JWT identity layer. It wraps the jwtassertion subpackage
// (JWKS-backed RS256 verification) and exposes a thin Claims projection for the
// rest of the service to consume.
package auth

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/wso2/aep/aep-api/internal/authz"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
)

// Claims is the BFF-internal projection of the verified JWT.
type Claims struct {
	Subject  string
	ClientID string
	// Organisation claims sourced from Thunder. ResolveOuHandle is the
	// only place that picks one — keep the precedence in lockstep with
	// the console.
	OuHandle string
	OuName   string
	OuId     string
	// Scope is the raw space-delimited OAuth scope claim. AE permission
	// keys (e.g. "ae:build") arrive as entries in this string alongside
	// unrelated scopes (openid, profile, system, ...); see Permissions.
	Scope string
}

// ResolveOuHandle returns the canonical OC org handle from a verified
// JWT, preferring `ouHandle` over `ouName` over `ouId`. Returns "" when
// the token has none of those claims (which the caller must surface as
// a fail-loud error rather than silently substitute an org).
//
// The console mirrors this precedence verbatim
// (console/src/utils/orgClaims.ts). Any change here MUST land on both
// sides simultaneously.
func ResolveOuHandle(c *Claims) string {
	if c == nil {
		return ""
	}
	if c.OuHandle != "" {
		return c.OuHandle
	}
	if c.OuName != "" {
		return c.OuName
	}
	return c.OuId
}

func (c *Claims) Permissions() []authz.Permission {
	if c == nil {
		return nil
	}
	return filterPermissions(c.Scope)
}

func filterPermissions(scope string) []authz.Permission {
	known := make(map[authz.Permission]struct{}, len(authz.AllPermissions))
	for _, p := range authz.AllPermissions {
		known[p] = struct{}{}
	}

	var perms []authz.Permission
	for _, token := range strings.Fields(scope) {
		p := authz.Permission(token)
		if _, ok := known[p]; ok {
			perms = append(perms, p)
		}
	}
	return perms
}

type claimsContextKey struct{}

// WithClaims returns a copy of ctx that carries the given Claims value.
func WithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsContextKey{}, claims)
}

// ClaimsFromContext retrieves the Claims stored by WithClaims.
func ClaimsFromContext(ctx context.Context) *Claims {
	c, _ := ctx.Value(claimsContextKey{}).(*Claims)
	return c
}

// JWTConfig configures the inbound JWT verifier. It is a thin shim over
// jwtassertion.Config with the same fields.
type JWTConfig struct {
	JWKS                *jwtassertion.JWKSCache
	AllowedIssuers      []string
	AllowedAudiences    []string
	ResourceMetadataURL string
}

// JWTMiddleware returns an HTTP middleware that verifies the Authorization
// header against cfg, projects the verified token into a Claims record, and
// forwards the request with the projection in context.
//
// The underlying jwtassertion middleware emits RFC 9728 WWW-Authenticate
// challenges on failure. The thin projection only exists so the rest of
// the BFF doesn't have to care about the full TokenClaims shape.
func JWTMiddleware(cfg JWTConfig) func(http.Handler) http.Handler {
	verifier := jwtassertion.Authenticator(jwtassertion.Config{
		JWKS:                cfg.JWKS,
		AllowedIssuers:      cfg.AllowedIssuers,
		AllowedAudiences:    cfg.AllowedAudiences,
		ResourceMetadataURL: cfg.ResourceMetadataURL,
	})
	return func(next http.Handler) http.Handler {
		return verifier(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tc := jwtassertion.GetTokenClaims(r.Context())
			if tc != nil {
				// tc.Sub, not tc.Subject: TokenClaims declares its own
				// `Sub string json:"sub"` at depth 0, which shadows the
				// embedded jwt.RegisteredClaims.Subject (same tag, deeper) —
				// encoding/json decodes ONLY the shallower field, so the
				// promoted Subject is always empty on this edge (found via
				// e2e: turn commits fell back to the credential identity).
				claims := &Claims{
					Subject:  tc.Sub,
					ClientID: tc.ClientID,
					OuHandle: tc.OuHandle,
					OuName:   tc.OuName,
					OuId:     tc.OuId,
					Scope:    tc.Scope,
				}
				ctx := WithClaims(r.Context(), claims)
				r = r.WithContext(ctx)

				// Debug-only visibility into which AE permission keys this
				// request's token actually carries — the same derivation
				// permission_gate.go checks against, not a re-parse of the
				// raw scope, so this can never drift from what the gate
				// decides. Deliberately omits the raw Scope/token: only the
				// filtered, known AE keys are logged.
				slog.DebugContext(ctx, "console request authenticated",
					"subject", claims.Subject,
					"org", ResolveOuHandle(claims),
					"permissions", claims.Permissions(),
					"method", r.Method, "path", r.URL.Path)
			}
			next.ServeHTTP(w, r)
		}))
	}
}
