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

// Package orgensure is the verify-and-cache seam for tenant identity. On
// every authenticated request that reaches an org-scoped route, the
// middleware resolves the OC org handle from the JWT claims (via
// middleware/jwt.ResolveOuHandle) and verifies the corresponding OC
// namespace exists, caching the local Organization row's UUID for FK
// use. It does NOT create namespaces — that is the platform's job
// (`platform-api-service` in hosted, `seed-admin-org.sh` in local).
//
// Behaviour:
//   - No claims in context (request did not pass through the JWT verifier
//     — e.g. /webhooks, /auth/external/jwks.json) → pass through.
//   - Claims present but no ouHandle/ouName/ouId → pass through. Route
//     handlers that need an org context surface their own 4xx.
//   - Namespace not yet provisioned → log and pass through so the
//     downstream controller can render a user-meaningful error rather
//     than 5xx the request.
package orgensure

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/wso2/asdlc/asdlc-service/middleware/jwt"
)

// OrgEnsurer is the narrow JIT-ensure port this middleware needs. The
// organization feature's OrganizationService satisfies it; defining it here
// (consumer side) keeps the middleware layer from importing a feature/the flat
// services package — the concrete is wired at the composition root.
type OrgEnsurer interface {
	EnsureForOuHandle(ctx context.Context, ouHandle, thunderOrgUUID string) error
}

// Middleware verifies the inbound request's ouHandle has a matching OC
// namespace and warms the local row cache. Best-effort — never 5xx the
// user's request: missing namespace, transient OC blip, and DB issues
// all log and pass through.
func Middleware(svc OrgEnsurer) func(http.Handler) http.Handler {
	if svc == nil {
		// Tests + dev configurations may run without a DB; the
		// middleware is a passthrough in that case.
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := jwt.ClaimsFromContext(r.Context())
			ouHandle := jwt.ResolveOuHandle(claims)
			if ouHandle == "" {
				next.ServeHTTP(w, r)
				return
			}
			thunderOrgUUID := ""
			if claims != nil {
				thunderOrgUUID = claims.OuId
			}
			// [SHAKEOUT:ORG] — the org/namespace-resolution seam. Local hardcodes
			// ouHandle=default with a pre-created namespace; cloud derives the
			// wc-<first8>-<sha256[:8]> namespace from the Thunder ouId. Logging
			// both at Info (cloud may not run Debug) lets the two planes be
			// diffed (§6.13). Never logs a secret value.
			err := svc.EnsureForOuHandle(r.Context(), ouHandle, thunderOrgUUID)
			slog.InfoContext(r.Context(), "[SHAKEOUT:ORG]",
				"ouHandle", ouHandle,
				"thunderOrgUUID", thunderOrgUUID,
				"ensured", err == nil,
				"ensureError", errStr(err))
			if err != nil {
				slog.WarnContext(r.Context(), "org-ensure verify failed; continuing", "ouHandle", ouHandle, "error", err)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// errStr renders an error for the SHAKEOUT log without panicking on nil.
func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
