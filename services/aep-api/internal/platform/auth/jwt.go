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
	"net/http"

	"github.com/wso2/aep/aep-api/internal/platform/auth/dialect"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
)

// Claims is the BFF-internal projection of the verified JWT.
type Claims struct {
	Subject  string
	ClientID string
	// The caller's org as the platform IdP's dialect read it: OrgHandle is
	// the name the platform knows the org by (the OpenChoreo namespace),
	// OrgID the IdP's own id for it, OrgName a display name. Empty when the
	// token carries no org, which a caller surfaces as a fail-loud error
	// rather than substituting one.
	OrgHandle string
	OrgID     string
	OrgName   string
	// Display claims of the verified token: who to credit a user's work to
	// (spec.displayIdentity holds the name rule). Empty when the IdP sends
	// none.
	Name       string
	Email      string
	GivenName  string
	FamilyName string
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

// claimsOf projects a verified token onto Claims.
//
// tc.Sub, not tc.Subject: TokenClaims declares its own `Sub string json:"sub"`
// at depth 0, which shadows the embedded jwt.RegisteredClaims.Subject (same
// tag, deeper) — encoding/json decodes ONLY the shallower field, so the
// promoted Subject is always empty on this edge (found via e2e: turn commits
// fell back to the credential identity).
func claimsOf(tc *jwtassertion.TokenClaims) *Claims {
	return &Claims{
		Subject:    tc.Sub,
		ClientID:   tc.ClientID,
		OrgHandle:  tc.OrgHandle,
		OrgID:      tc.OrgID,
		OrgName:    tc.OrgName,
		Name:       tc.Name,
		Email:      tc.Email,
		GivenName:  tc.GivenName,
		FamilyName: tc.FamilyName,
	}
}

// JWTConfig configures the inbound JWT verifier. It is a thin shim over
// jwtassertion.Config with the same fields.
type JWTConfig struct {
	JWKS                *jwtassertion.JWKSCache
	AllowedIssuers      []string
	AllowedAudiences    []string
	ResourceMetadataURL string
	Dialect             dialect.Dialect
}

// JWTMiddleware returns an HTTP middleware that verifies the Authorization
// header against cfg, projects the verified token into a Claims record, and
// forwards the request with the projection in context.
//
// It verifies USER tokens only: a client_credentials token is refused with
// the same 401 invalid_token challenge as any other verifier refusal, whatever
// its audience or org claim. Machine callers have their own routes
// (/internal/v1/*) with their own verifiers.
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
		Dialect:             cfg.Dialect,
		UserTokensOnly:      true,
	})
	return func(next http.Handler) http.Handler {
		return verifier(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tc := jwtassertion.GetTokenClaims(r.Context()); tc != nil {
				r = r.WithContext(WithClaims(r.Context(), claimsOf(tc)))
			}
			next.ServeHTTP(w, r)
		}))
	}
}
