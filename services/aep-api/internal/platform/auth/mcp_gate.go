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

// This file holds the tenant gate for aep-api's raw (non-generated) MCP
// discovery mount (POST /internal/v1/mcp). Its one caller credential is an
// org's Thunder publisher client token (aud aep-publisher-<org>): the coding
// runner and the AE Studio tools pod's MCP proxy both present it. The acting
// org comes SOLELY from the verified token (audience org, cross-checked against
// ouHandle by PublisherTokenVerifier), never from the path, body or a header.
// No other token opens this mount: not an ae-studio-<org> client token (that
// opens ae-studio/ only), not a user JWT, not a token aep-api signs itself.

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
)

// mcpOrgCtxKey carries the org resolved by PublisherMCPGate.
type mcpOrgCtxKey struct{}

// WithMCPOrg returns a copy of ctx carrying the MCP-verified org handle. Set by
// PublisherMCPGate; read by the MCP handler via MCPOrgFromContext.
func WithMCPOrg(ctx context.Context, org string) context.Context {
	return context.WithValue(ctx, mcpOrgCtxKey{}, org)
}

// MCPOrgFromContext returns the org bound by PublisherMCPGate. The MCP handler
// reads it here — the org NEVER comes from the path/body/header. ok is false
// when the request never passed through the gate (a wiring bug): the handler
// then fails closed rather than acting on an unresolved org.
func MCPOrgFromContext(ctx context.Context) (string, bool) {
	org, ok := ctx.Value(mcpOrgCtxKey{}).(string)
	return org, ok
}

// PublisherMCPGate wraps next so it runs only for a verified publisher client
// token, with that token's org bound onto the context (WithMCPOrg). Every
// failure — missing or non-bearer header, bad signature, wrong issuer, a
// non-publisher audience, an ouHandle mismatch, expiry, a nil verifier — is a
// 401 with a generic body; the reason goes to the log only.
func PublisherMCPGate(publisher *PublisherTokenVerifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
			slog.WarnContext(r.Context(), "mcp auth rejected", "reason", "bearer token required")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		claims, err := publisher.Verify(header[len(prefix):]) // nil verifier: error, fails closed
		if err != nil {
			slog.WarnContext(r.Context(), "mcp auth rejected", "error", err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithMCPOrg(r.Context(), claims.OrgHandle)))
	})
}
