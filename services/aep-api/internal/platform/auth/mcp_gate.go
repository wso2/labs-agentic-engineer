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
// discovery mount (POST /internal/v1/mcp). It takes two caller credentials,
// each verified exactly as on its own route group: an org's Thunder publisher
// client token (aud aep-publisher-<org>, PublisherTokenVerifier), which the
// coding runner presents, and an org's AE Studio client token (aud
// ae-studio-<org>, StudioClientVerifier: only the client recorded for that
// org), which the org's AE Studio tools pod presents. The acting org comes
// SOLELY from the verifier that accepted the token, never from the path, body
// or a header. No other token opens this mount: not a user JWT, not the
// AE-only client, not a token aep-api signs itself. Opening MCP to the
// ae-studio client opens nothing else to it: runs/ stays publisher-only
// (RunnerAuthorizer).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// mcpOrgCtxKey carries the org resolved by MCPGate.
type mcpOrgCtxKey struct{}

// WithMCPOrg returns a copy of ctx carrying the MCP-verified org handle. Set by
// MCPGate; read by the MCP handler via MCPOrgFromContext.
func WithMCPOrg(ctx context.Context, org string) context.Context {
	return context.WithValue(ctx, mcpOrgCtxKey{}, org)
}

// MCPOrgFromContext returns the org bound by MCPGate. The MCP handler reads it
// here — the org NEVER comes from the path/body/header. ok is false when the
// request never passed through the gate (a wiring bug): the handler then fails
// closed rather than acting on an unresolved org.
func MCPOrgFromContext(ctx context.Context) (string, bool) {
	org, ok := ctx.Value(mcpOrgCtxKey{}).(string)
	return org, ok
}

// MCPGate wraps next so it runs only for a verified publisher client token or
// a verified, recorded ae-studio client token, with the org that verifier
// bound onto the context (WithMCPOrg). Every refusal — missing or non-bearer
// header, bad signature, wrong issuer, an audience neither verifier accepts,
// an ouHandle mismatch, an ae-studio client not recorded for its org, expiry,
// a nil verifier — is the same 401 with a generic body; the reason goes to
// the log only. A recorded ae-studio client that could not be read is a 503
// (no verdict on the caller), as on the ae-studio/ ops.
func MCPGate(publisher *PublisherTokenVerifier, studio *StudioClientVerifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		header := r.Header.Get("Authorization")
		if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
			slog.WarnContext(r.Context(), "mcp auth rejected", "reason", "bearer token required")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		org, err := verifyMCPCaller(r.Context(), publisher, studio, header[len(prefix):])
		if errors.Is(err, ErrStudioClientLookup) {
			slog.ErrorContext(r.Context(), "mcp auth: ae-studio client lookup failed", "error", err)
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		if err != nil {
			slog.WarnContext(r.Context(), "mcp auth rejected", "error", err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithMCPOrg(r.Context(), org)))
	})
}

// verifyMCPCaller answers the org token binds: the publisher token's org, or
// the org an ae-studio client is recorded for. A nil verifier refuses its
// tokens (both Verify methods fail closed on a nil receiver).
func verifyMCPCaller(ctx context.Context, publisher *PublisherTokenVerifier, studio *StudioClientVerifier, token string) (string, error) {
	claims, perr := publisher.Verify(token)
	if perr == nil {
		return claims.OrgHandle, nil
	}
	org, serr := studio.Verify(ctx, token)
	if serr == nil {
		return org, nil
	}
	if errors.Is(serr, ErrStudioClientLookup) {
		return "", serr
	}
	return "", fmt.Errorf("not a publisher token (%v) nor a recorded ae-studio client token (%w)", perr, serr)
}
