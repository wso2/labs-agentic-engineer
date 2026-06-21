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

package tenant

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/wso2/asdlc/asdlc-service/internal/platform/httpkit"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/ids"
	"github.com/wso2/asdlc/asdlc-service/middleware/jwt"
)

// GateMode controls whether BindUserOrg enforces the org match or only
// observes it. "log" computes + emits the would-deny canary then passes
// through (observe-only); "enforce" 404s a cross-org request. Rollback from
// enforce is setting TENANT_GATE_MODE=log.
type GateMode string

const (
	GateModeLog     GateMode = "log"
	GateModeEnforce GateMode = "enforce"
)

// ParseGateMode reads TENANT_GATE_MODE. The gate is ENFORCE BY DEFAULT: only an
// explicit "log" (case-insensitive, whitespace-trimmed) selects observe-only
// mode; anything else — unset, "enforce", or an unrecognized value — enforces.
// This makes the secure posture the zero-config default (no env var needed in
// any environment, incl. cloud release bindings) and fail-secure on a typo.
// To run the observe-only canary, set TENANT_GATE_MODE=log explicitly.
func ParseGateMode(s string) GateMode {
	if strings.EqualFold(strings.TrimSpace(s), string(GateModeLog)) {
		return GateModeLog
	}
	return GateModeEnforce
}

// BindUserOrg is the central tenant gate for User-JWT routes, applied PER
// ROUTE (Go's ServeMux binds path wildcards only after the inner mux matches,
// so a wrapper around the mux would see an empty PathValue — §6.1b). It
// compares the org on the path against the org in the verified JWT
// (case-insensitive) and, in enforce mode, 404s on mismatch with the same body
// as no-such-org so cross-org existence is never leaked — closing the systemic
// IDOR (IDOR-1..5). It also binds a Caller into the request context for
// downstream handlers/repositories.
//
// In GateModeLog it computes the same decision and emits a
// "[SHAKEOUT:would-deny]" canary line at Warn but always passes the request
// through, so production traffic can be observed before enforcement is enabled.
//
// JIT org-ensure/namespace-warm is intentionally NOT done here —
// orgensure.Middleware owns that seam. This gate adds only the claim-vs-path
// check, the Caller binding, and the dual-plane SHAKEOUT instrumentation (§6.13).
func BindUserOrg(pathVar string, mode GateMode) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			claims := jwt.ClaimsFromContext(ctx)
			tokenOrg := jwt.ResolveOuHandle(claims)
			pathOrg := r.PathValue(pathVar)

			var ouID, ouHandle, ouName, subject string
			if claims != nil {
				ouID, ouHandle, ouName, subject = claims.OuId, claims.OuHandle, claims.OuName, claims.Subject
			}

			// [SHAKEOUT:CLAIMS] — exactly what the verifier projected, so the
			// local-vs-cloud claim shapes can be diffed (§6.13). Info level:
			// cloud may not run Debug. Never logs a secret/token value.
			slog.InfoContext(ctx, "[SHAKEOUT:CLAIMS]",
				"path", r.URL.Path,
				"pathVar", pathVar,
				"ouHandle", ouHandle,
				"ouName", ouName,
				"ouId", ouID,
				"subject", subject,
				"resolvedTokenOrg", tokenOrg,
				"pathOrg", pathOrg,
				"mode", string(mode),
			)

			// Missing org claim — the verified token cannot name an org.
			if tokenOrg == "" {
				slog.WarnContext(ctx, "[SHAKEOUT:would-deny]",
					"reason", "no-org-claim", "mode", string(mode), "path", r.URL.Path)
				if mode == GateModeEnforce {
					httpkit.Write401(w)
					return
				}
			}

			// Malformed path identifier is a 400 in both modes (a syntactic
			// error, not a cross-org existence question).
			if pathOrg != "" {
				if err := ids.Slug(pathOrg); err != nil {
					httpkit.Write400(w, "orgHandle: "+err.Error())
					return
				}
			}

			// THE missing check: path org must equal the verified token org.
			mismatch := tokenOrg != "" && pathOrg != "" && !strings.EqualFold(tokenOrg, pathOrg)
			if mismatch {
				slog.WarnContext(ctx, "[SHAKEOUT:would-deny]",
					"reason", "org-mismatch", "mode", string(mode),
					"tokenOrg", tokenOrg, "pathOrg", pathOrg, "path", r.URL.Path)
				if mode == GateModeEnforce {
					httpkit.Write404(w, "organization not found") // closes IDOR-1..5
					return
				}
			}

			// Bind the authorized Caller for downstream use. Org is the path
			// org the caller is acting on (in enforce mode this has been proven
			// to equal the token org). ThunderUUID is carried for impersonation
			// + wc- namespace derivation only.
			org := pathOrg
			if org == "" {
				org = tokenOrg
			}
			scope := Caller{
				Org:         OrgHandle(org),
				ThunderUUID: ouID,
				Subject:     subject,
				Source:      SourceUserJWT,
			}
			next.ServeHTTP(w, r.WithContext(With(ctx, scope)))
		})
	}
}
