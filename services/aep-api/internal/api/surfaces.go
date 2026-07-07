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

package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/feature/dependencies"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// mountSurfaces wires every HTTP surface onto one outer mux and returns it. This
// is the single screen that answers "what is exposed and who guards it" — the
// whole boundary of the BFF in one place. There are four request surfaces plus
// the unauthenticated discovery endpoints:
//
//	Surface        Root                 Guard (who may call)                       Where it lives / spec
//	───────────────────────────────────────────────────────────────────────────────────────────────────
//	public         /api/v1              Thunder user JWT + org gate                *_huma.go · humakit.OrgScopedInput
//	               (jwt → orgensure)    (org from the verified token, never input)  → api/openapi.yaml
//	internal S2S   /internal/v1/executions/  BFF Task-JWT or publisher-cc          internal.go · auth.ExecutionScopedInput
//	               (per-op resolver)         (dual-token verify + INT-6 fence)      → api/internal-openapi.yaml (non-public)
//	internal MCP   /internal/v1/mcp     BFF-signed JWT, aud aep-api-mcp            dependencies/mcp_server.go ·
//	               (POST, JSON-RPC)     (org from ocOrgId claim, never input)       auth.AgentsScopedVerifier (no spec — JSON-RPC)
//	               /mcp/playground-token  NONE — flag-gated only                   dependencies/playground_token.go
//	               (POST, local dev)      (PLAYGROUND_TOKEN_ENABLED, off by         (mounted only when the flag is true —
//	                                      default; docker-compose sets it)          404 by absence otherwise)
//	external       /api/v1/webhooks,    per-route bespoke: GitHub HMAC /           webhook_routes.go · org_github_routes.go
//	               .../github/connect    signed connect-state (org from payload)    (no generated spec; paths kept — Q4)
//	dev/test       /_dev/v1             none — registration-gated to dev tier      dev.go · RegisterAllDev
//	               (gated mount)        + on no HTTPRoute (loopback only)           (no spec)
//
//	discovery: /healthz, /auth/external/jwks.json, /openapi.yaml, /docs — public, no auth.
//
// The reusable identity primitive underneath both S2S directions: the BFF is the
// single issuer of org-bearing RS256 tokens (internal/platform/auth.TaskTokenManager
// — Issue inbound, IssueServiceToken outbound), all verified against the one
// JWKS at /auth/external/jwks.json. Org always travels in a verified claim,
// never a trusted header. See docs/design/internal-s2s-api.md.
//
// "Where do I change X?" → credential verify/mint: internal/platform/auth ·
// who-may-touch-what gates: humakit (public) + internal/platform/auth (internal) ·
// what's exposed: this file.
func mountSurfaces(params AppParams) *http.ServeMux {
	mux := http.NewServeMux()

	// ── discovery ────────────────────────────────────────────────────────────
	// Health check — unauthenticated. `/healthz` (k8s idiom) platform-wide.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
	})

	// Task-JWT public key set (JWKS) — unauthenticated discovery, fetched by
	// every verifier before any auth. A plain handler (not a Huma op) so it stays
	// off the /api/v1 server base path: the public spec is now base-pathed at
	// /api/v1, and this endpoint deliberately lives outside that subtree.
	mux.HandleFunc("GET /auth/external/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if tt := params.HumaDeps.TaskTokens; tt != nil {
			_ = json.NewEncoder(w).Encode(tt.JWKS())
			return
		}
		_, _ = w.Write([]byte(`{"keys":[]}`))
	})

	// ── public edge (/api/v1) ────────────────────────────────────────────────
	// User-JWT authenticated (JWKS-backed RS256). Every org-scoped operation
	// embeds humakit.OrgScopedInput, whose Resolve derives the active org SOLELY
	// from the verified token (there is no {orgHandle} param) and applies the
	// tenant gate — the allowlist-by-construction IDOR fence. Carve-outs (org
	// listing, idp discover) read no org and bypass it. The Huma API is mounted
	// on apiMux so every op inherits the jwt + orgensure middleware applied to
	// "/api/" below; the public spec + docs are served on the outer mux.
	apiMux := http.NewServeMux()
	gateMode := tenant.ParseGateMode(params.Config.TenantGateMode)
	slog.Info("tenant gate active", "mode", string(gateMode))
	humaAPI := newHumaAPI(apiMux)
	registerHumaDocs(mux, humaAPI)
	RegisterAllHuma(humaAPI, params.HumaDeps)

	// ── dev/test surface (/_dev/v1) ──────────────────────────────────────────
	// Local-only tooling, no request auth by design; safety is structural
	// (registration gate to the dev tier + on no HTTPRoute). All gating + handler
	// bodies live in dev.go. See docs/design/internal-s2s-api.md §3.4.
	RegisterAllDev(mux, params)

	// ── external-inbound (webhook + connect-callback) ────────────────────────
	// Callers are outside the platform (GitHub, a mid-OAuth browser); each route
	// keeps its own bespoke auth (HMAC / signed connect-state) and derives org
	// from the verified payload — not a session or service token. Both sit
	// outside the /api/ user-JWT wrapper via their more-specific patterns.
	if params.WebhookController != nil {
		registerWebhookRoutes(mux, params.WebhookController)
	}
	if params.OrgGitHubController != nil {
		registerConnectCallbackRoute(mux, params.OrgGitHubController)
	}

	// ── internal S2S surface ─────────────────────────────────────────────────
	// Its own Huma API on its own mux, NOT wrapped by the /api/ user-JWT
	// middleware. Each operation authenticates by construction via
	// auth.ExecutionScopedInput (BFF Task-JWT or publisher-cc) and is never
	// gateway-advertised. All runner callbacks (skills, credentials refresh)
	// are keyed to the execution id — tasks-github-native §9.2. See
	// docs/design/internal-s2s-api.md §3.
	internalMux := http.NewServeMux()
	internalAPI := newInternalAPI(internalMux)
	RegisterAllInternal(internalAPI, params.InternalDeps)
	mux.Handle(internalV1+"/executions/", internalMux)
	mux.Handle(internalV1+"/orchestration/", internalMux)

	// ── internal MCP discovery (POST /internal/v1/mcp) ───────────────────────
	// A raw (non-Huma) JSON-RPC mount: the MCP server the agents service's
	// designing LLM queries for the org's registered external resources,
	// published endpoints, and platform resource types. Gated by
	// auth.AgentsScopedVerifier — the caller presents a BFF-signed token with
	// aud aep-api-mcp and the acting org is bound from its ocOrgId claim, never
	// from the request. Mounted only when the token manager exists (same
	// conditional posture as the internal S2S mount): without it nothing could
	// verify a caller, so the path 404s instead of 503-ing forever. A nil
	// MCPExternalResources/OrgEndpoints/ResourceTypes degrades the corresponding
	// tool to an empty result (see dependencies.NewMCPHandler).
	if params.HumaDeps.TaskTokens != nil {
		mcpVerifier := auth.NewAgentsScopedVerifier(params.HumaDeps.TaskTokens)
		mcpHandler := dependencies.NewMCPHandler(
			params.MCPExternalResources, params.MCPOrgEndpoints, params.MCPResourceTypes)
		mux.Handle("POST "+internalV1+"/mcp", mcpVerifier.Middleware(mcpHandler))

		// ── playground-token mint (POST /internal/v1/mcp/playground-token) ────
		// LOCAL DEV ONLY, and only when explicitly opted in via
		// PlaygroundTokenEnabled (docker-compose sets it; nothing else does).
		// Lets a developer drive the services/agents playground CLI against a
		// live aep-api without a caller-auth story for this route — production
		// agent→BFF authentication remains an open decision this endpoint
		// deliberately does not prejudge. Disabled ⇒ not mounted at all (404 by
		// absence, matching the MCP mount's own conditional-mount posture).
		if params.Config.PlaygroundTokenEnabled {
			mux.Handle("POST "+internalV1+"/mcp/playground-token",
				dependencies.NewPlaygroundTokenHandler(params.HumaDeps.TaskTokens))
		}
	}

	// ── /api/ user-JWT wrapper ───────────────────────────────────────────────
	// JIT org-onboarding sits between JWT verification and the org-aware route
	// handlers. Tenants materialise on first authenticated request; no env var,
	// manifest, or seed names an org. See default-org-seed-removal.md §3.2.
	//
	// The inbound verifier is injectable (params.InboundAuth) so a component test
	// can swap the JWKS-backed verifier for a claims-injector and run the real
	// gate in ENFORCE with no Thunder — bff-component-testing.md §8.2. Production
	// leaves InboundAuth nil and gets the real RS256/JWKS middleware built here;
	// only that seam differs, orgensure + the Huma gate are identical.
	jwt := params.InboundAuth
	if jwt == nil {
		jwt = auth.JWTMiddleware(auth.JWTConfig{
			JWKS:                params.ThunderJWKS,
			AllowedIssuers:      SplitAndTrim(params.Config.JWTAllowedIssuer),
			AllowedAudiences:    SplitAndTrim(params.Config.JWTAllowedAudience),
			ResourceMetadataURL: params.Config.JWTResourceMetadataURL,
		})
	}
	ensureOrg := auth.EnsureOrgMiddleware(params.OrganizationService)
	// Stamp the configured tenant-gate mode onto every /api/ request context;
	// humakit.OrgScopedInput.Resolve reads it per-request (ENFORCE default when
	// unstamped). Request-scoped instead of a package global so concurrently
	// built handlers (prod + parallel component-test harnesses) can never race
	// on the mode. bff-component-testing.md §8.3.
	stampGateMode := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(tenant.WithGateMode(r.Context(), gateMode)))
		})
	}
	mux.Handle("/api/", jwt(ensureOrg(stampGateMode(apiMux))))

	return mux
}
