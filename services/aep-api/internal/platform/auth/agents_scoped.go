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

package auth

// This file holds the by-construction tenant gate for the BFF's raw (non-Huma)
// MCP discovery mount (POST /internal/v1/mcp) — the middleware analogue of
// RunnerScopedInput. Where RunnerScopedInput binds the org from a verified
// runner credential on the internal Huma surface, AgentsScopedVerifier binds it
// from a BFF-signed identity JWT (aud AudienceMCP) or a Thunder publisher CC
// token on the raw MCP mount. Both derive the acting org SOLELY from a verified
// claim — never a trusted path/body/header. The source mounted its MCP server
// UNGATED under an {orgHandle} path; that trusted-path posture is banned here,
// so the org travels only in the signed ocOrgId claim (BFF) or OrgHandle claim
// (publisher). The verifier reuses the TaskTokenManager key material (the same
// JWKS at /auth/external/jwks.json) for BFF tokens, so an MCP token is verified
// exactly like every other BFF-signed S2S token; publisher tokens are verified
// via PublisherTokenVerifier when wired.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// mcpOrgCtxKey carries the org resolved by the MCP auth middleware.
type mcpOrgCtxKey struct{}

// WithMCPOrg returns a copy of ctx carrying the MCP-verified org handle. Set by
// AgentsScopedVerifier.Middleware; read by the MCP handler via MCPOrgFromContext.
func WithMCPOrg(ctx context.Context, org string) context.Context {
	return context.WithValue(ctx, mcpOrgCtxKey{}, org)
}

// MCPOrgFromContext returns the org bound by AgentsScopedVerifier.Middleware.
// The MCP handler reads it here — the org NEVER comes from the path/body/header.
// ok is false when the request never passed through the verifier (a wiring bug):
// the handler then fails closed rather than acting on an unresolved org.
func MCPOrgFromContext(ctx context.Context) (string, bool) {
	org, ok := ctx.Value(mcpOrgCtxKey{}).(string)
	return org, ok
}

// AgentsScopedVerifier authenticates a caller to the BFF's raw MCP discovery
// mount and binds the verified org onto the request context. It is the inbound
// half of the symmetric S2S identity model for the MCP surface — the analogue of
// the user-JWT verifier on the public edge and RunnerAuthorizer on the internal
// Huma surface (dual-accept BFF MCP token, then Thunder publisher CC).
type AgentsScopedVerifier struct {
	tokens    *TaskTokenManager
	publisher *PublisherTokenVerifier
}

// NewAgentsScopedVerifier builds the verifier over the BFF's token manager and
// an optional publisher verifier. A nil manager yields a verifier whose
// Middleware fails closed (503) — the mount site should simply not mount when
// the manager is absent. A nil publisher means BFF-only acceptance.
func NewAgentsScopedVerifier(tokens *TaskTokenManager, publisher *PublisherTokenVerifier) *AgentsScopedVerifier {
	return &AgentsScopedVerifier{tokens: tokens, publisher: publisher}
}

// Middleware verifies Authorization: Bearer <token> as a BFF-signed token with
// aud AudienceMCP or a Thunder publisher CC token, binds the org from the
// ocOrgId or OrgHandle claim onto the request context, and calls next. Every
// failure — missing/garbage token, bad signature, wrong aud, expired — is a 401
// with a generic body (no leak of why). An unconfigured verifier is a 503 wiring
// guard, never a hot path.
func (v *AgentsScopedVerifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v == nil || v.tokens == nil {
			// Publisher-only MCP is unreachable: BFF MCP tokens need this
			// manager. A nil publisher still accepts BFF-signed MCP tokens.
			http.Error(w, "mcp auth not configured", http.StatusServiceUnavailable)
			return
		}
		org, err := v.resolveOrg(r.Header.Get("Authorization"))
		if err != nil {
			slog.WarnContext(r.Context(), "mcp auth rejected", "error", err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithMCPOrg(r.Context(), org)))
	})
}

// resolveOrg verifies the bearer and returns the org from the ocOrgId claim
// (BFF MCP token) or OrgHandle claim (publisher CC token).
func (v *AgentsScopedVerifier) resolveOrg(authHeader string) (string, error) {
	const prefix = "Bearer "
	if len(authHeader) <= len(prefix) || !strings.EqualFold(authHeader[:len(prefix)], prefix) {
		return "", fmt.Errorf("bearer token required")
	}
	raw := authHeader[len(prefix):]
	claims, err := v.tokens.Verify(raw)
	if err == nil {
		if !hasAudience(claims.Audience, AudienceMCP) {
			return "", fmt.Errorf("token audience %v is not %q", claims.Audience, AudienceMCP)
		}
		if claims.OcOrgID == "" {
			return "", fmt.Errorf("token has no ocOrgId claim")
		}
		return claims.OcOrgID, nil
	}
	if v.publisher != nil {
		pub, perr := v.publisher.Verify(raw)
		if perr == nil {
			if pub.OrgHandle == "" {
				return "", fmt.Errorf("publisher token has no org")
			}
			return pub.OrgHandle, nil
		}
		return "", errors.Join(err, perr)
	}
	return "", err
}

// hasAudience reports whether want is present in the token's aud claim.
func hasAudience(auds jwt.ClaimStrings, want string) bool {
	for _, a := range auds {
		if a == want {
			return true
		}
	}
	return false
}
