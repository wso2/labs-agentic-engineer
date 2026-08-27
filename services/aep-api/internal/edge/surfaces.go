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

package edge

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/dependencies/mcpdiscovery"
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
//	public         /api/v1              Thunder user JWT + org gate                handlers_*.go · tenant_gate.go
//	               (jwt → orgensure)    (org from the verified token, never input)  ← packages/contracts/api/v1 (source of truth)
//	internal S2S   /internal/v1/validation/, publisher-cc (iss platform-idp)        internal.go · runnerAuthGate
//	               /internal/v1/executions/  (INT-6 fence keyed to the run CYCLE     ← packages/contracts/api/internal/v1 (non-public)
//	               (deny-by-default gate)     id)
//	internal MCP   /internal/v1/mcp     BFF JWT aud=aep-api-mcp or Thunder          dependencies/mcp_server.go ·
//	               (POST, JSON-RPC)     publisher CC (org from ocOrgId or           auth.AgentsScopedVerifier (no spec — JSON-RPC)
//	                                    PublisherClaims.OrgHandle, never request)
//	               /mcp/playground-token  NONE — flag-gated only                   dependencies/playground_token.go
//	               (POST, local dev)      (PLAYGROUND_TOKEN_ENABLED, off by         (mounted only when the flag is true —
//	                                      default; docker-compose sets it)          404 by absence otherwise)
//	external       /api/v1/webhooks,    per-route bespoke: GitHub HMAC /           webhook_routes.go · org_github_routes.go
//	               .../github/connect    signed connect-state (org from payload)    (no generated spec; paths kept — Q4)
//	dev/test       /_dev/v1             none — registration-gated to dev tier      dev.go · RegisterAllDev
//	               (gated mount)        + on no HTTPRoute (loopback only)           (no spec)
//
//	discovery: /healthz (liveness), /readyz (workspace readiness), /auth/external/jwks.json — public, no auth.
//
// The reusable identity primitive underneath S2S: the BFF issues MCP tokens
// (internal/platform/auth.TaskTokenManager.IssueServiceToken), verified against
// the one JWKS at /auth/external/jwks.json. Runner callbacks accept Thunder
// publisher CC tokens (iss platform-idp) only. Org always travels in a
// verified claim, never a trusted header.
//
// "Where do I change X?" → credential verify/mint: internal/platform/auth ·
// who-may-touch-what gates: tenant_gate.go (public) + internal.go runnerAuthGate (internal) ·
// what's exposed: this file.
func mountSurfaces(params AppParams) *http.ServeMux {
	mux := http.NewServeMux()

	// ── discovery ────────────────────────────────────────────────────────────
	// Liveness — unauthenticated. `/healthz` (k8s idiom) platform-wide; always 200.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
	})
	// Readiness — workspace root health (R8b). 503 when the shared mount fails
	// root-health so kubelet marks the pod NotReady (PVC-prune detector).
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if params.WorkspaceReady != nil && !params.WorkspaceReady.Ready() {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"not_ready"}`)) //nolint:errcheck
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`)) //nolint:errcheck
	})

	// Task-JWT public key set (JWKS) — unauthenticated discovery for BFF-signed
	// tokens (design-agent MCP / IssueServiceToken). Publisher CC tokens verify
	// against platform-idp JWKS, not this endpoint. A plain handler (not a
	// contract op) so it stays off the /api/v1 server base path: the public
	// contract is base-pathed at /api/v1, and this endpoint deliberately lives
	// outside that subtree.
	mux.HandleFunc("GET /auth/external/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if tt := params.Deps.TaskTokens; tt != nil {
			_ = json.NewEncoder(w).Encode(tt.JWKS())
			return
		}
		_, _ = w.Write([]byte(`{"keys":[]}`))
	})

	// ── public edge (/api/v1) ────────────────────────────────────────────────
	// User-JWT authenticated (JWKS-backed RS256), served CONTRACT-FIRST: the
	// generated strict router (internal/api/gen, from packages/contracts/api/v1)
	// replaces the Huma mux. Every operation passes the deny-by-default tenant
	// gate (tenant_gate.go) — org derived SOLELY from the verified token, bound
	// into context, handed to services explicitly; carve-outs are enumerated in
	// tenantGateCarveOuts. Requests are validated against the committed contract
	// before any handler runs (validator.go). apiV1 mounts under the jwt +
	// orgensure middleware applied to "/api/" below. The contract itself is
	// not served over HTTP — it is a build-time artifact
	// (packages/contracts/api/v1, embedded only for the validator).
	gateMode := tenant.ParseGateMode(params.Config.TenantGateMode)
	slog.Info("tenant gate active", "mode", string(gateMode))
	apiV1 := newAPIV1Handler(params.Deps)

	// ── dev/test surface (/_dev/v1) ──────────────────────────────────────────
	// Local-only tooling, no request auth by design; safety is structural
	// (registration gate to the dev tier + on no HTTPRoute). All gating + handler
	// bodies live in dev.go.
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
	// Served contract-first from packages/contracts/api/internal/v1 (strict
	// server in internal/igen), NOT wrapped by the /api/ user-JWT
	// middleware. Every operation passes the deny-by-default runnerAuthGate
	// (publisher-cc verified against the path id) and is never
	// gateway-advertised. Every runner callback is keyed to the run CYCLE the
	// platform dispatched the pod for — the id it carries as AEP_TASK_ID.
	//
	// TWO prefixes, ONE handler: the validation callbacks live under the feature
	// that owns them, and token refresh keeps the `/executions/` prefix it was
	// published under (the id it names is a cycle, the same wire-compat debt
	// AEP_TASK_ID carries). Both must be mounted — the inner mux registers the
	// contract's full paths, so a prefix that is not mounted here 404s before any
	// handler or auth gate is reached.
	internalHandler := newInternalV1Handler(params.InternalDeps)
	mux.Handle(internalV1+"/executions/", internalHandler)
	mux.Handle(internalV1+"/validation/", internalHandler)

	// ── internal MCP discovery (POST /internal/v1/mcp) ───────────────────────
	// A raw (non-Huma) JSON-RPC mount: the MCP server the agents service's
	// designing LLM queries for the org's registered external resources,
	// published endpoints, and platform resource types. Gated by
	// auth.AgentsScopedVerifier — the caller presents a BFF-signed token with
	// aud aep-api-mcp or a Thunder publisher CC token; the acting org is bound
	// from ocOrgId or PublisherClaims.OrgHandle, never from the request.
	// Mounted only when the token manager exists (same conditional posture as
	// the internal S2S mount): without it nothing could verify a caller, so the
	// path 404s instead of 503-ing forever. Without PublisherTokens wired, only
	// BFF-signed MCP tokens are accepted. A nil MCPExternalResources/OrgEndpoints/ResourceTypes degrades
	// the corresponding tool to an empty result (see dependencies.NewMCPHandler).
	if params.Deps.TaskTokens != nil {
		mcpVerifier := auth.NewAgentsScopedVerifier(params.Deps.TaskTokens, params.Deps.PublisherTokens)
		mcpHandler := mcpdiscovery.NewMCPHandler(
			params.MCPExternalResources, params.MCPOrgEndpoints, params.MCPResourceTypes,
			params.MCPRoleCatalog, params.MCPRemoteGit,
			params.MCPSpecValidator, params.MCPSpecNormalizer, params.MCPSpecFetcher)
		mux.Handle("POST "+internalV1+"/mcp", mcpVerifier.Middleware(mcpHandler))

		// ── playground-token mint (POST /internal/v1/mcp/playground-token) ────
		// LOCAL DEV ONLY, and only when explicitly opted in via
		// PlaygroundTokenEnabled (docker-compose sets it; nothing else does).
		// Lets a developer drive the @aep/playground CLI against a
		// live aep-api without a caller-auth story for this route — production
		// agent→BFF authentication remains an open decision this endpoint
		// deliberately does not prejudge. Disabled ⇒ not mounted at all (404 by
		// absence, matching the MCP mount's own conditional-mount posture).
		if params.Config.PlaygroundTokenEnabled {
			mux.Handle("POST "+internalV1+"/mcp/playground-token",
				mcpdiscovery.NewPlaygroundTokenHandler(params.Deps.TaskTokens))
		}
	}

	// ── /api/ user-JWT wrapper ───────────────────────────────────────────────
	// JIT org-onboarding sits between JWT verification and the org-aware route
	// handlers. Tenants materialise on first authenticated request; no env var,
	// manifest, or seed names an org. See default-org-seed-removal.md §3.2.
	//
	// The inbound verifier is injectable (params.InboundAuth) so a component test
	// can swap the JWKS-backed verifier for a claims-injector and run the real
	// gate in ENFORCE with no Thunder. Production
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
	// on the mode.
	stampGateMode := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(tenant.WithGateMode(r.Context(), gateMode)))
		})
	}
	mux.Handle("/api/", jwt(ensureOrg(stampGateMode(apiV1))))

	return mux
}
