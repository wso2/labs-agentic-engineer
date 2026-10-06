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
	"net/http"
	"strings"

	"github.com/wso2/aep/aep-api/internal/projects"

	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/dependencies/mcpdiscovery"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
	"github.com/wso2/aep/aep-api/internal/platform/obs"
)

// internalV1 is the path root for the BFF's internal / server-to-server
// route group: runner-pod callbacks and dev-only helpers. It is deliberately
// distinct from the client-facing /api/v1 edge namespace (user-JWT,
// gateway-advertised, served contract-first from packages/contracts/api/v1)
// so each prefix tells the truth about its audience and auth regime, with the
// version in a fixed slot right after the audience root. These routes mount on the outer
// mux, escaping the /api/ user-JWT wrapper, and authenticate via their own
// publisher-cc (or ae-studio client) posture inside the handler. Never gateway-advertised.
const internalV1 = "/internal/v1"

// AppParams holds all dependencies needed to build the HTTP handler.
type AppParams struct {
	Config config.Config

	// Deps carries the feature services the strict handlers call
	// (internal/api/handlers_*.go — the generated router serves the committed
	// contract, packages/contracts/api/v1). main.go fills it.
	Deps Deps

	// InternalDeps carries the services + authorizers for the internal S2S
	// route group (runner callbacks, SRE handoff), served contract-first from
	// packages/contracts/api/internal/v1 behind internalGate. Its MCP
	// handler is filled by routes() from the MCP fields below.
	InternalDeps InternalDeps

	ConfigRepo projects.ConfigRepository

	// OrganizationService backs the JIT org-provisioning middleware. nil
	// disables the middleware (tests, dev configurations without a DB).
	OrganizationService organization.OrganizationService

	// ThunderJWKS verifies User JWTs and Service JWTs presented to the BFF.
	// Required for inbound auth: when nil, every /api/ request fails closed
	// (401) — there is no unsigned-claim fallback. Both planes set JWKS_URL.
	ThunderJWKS *jwtassertion.JWKSCache

	// InboundAuth, when non-nil, REPLACES the JWKS-backed jwt.Middleware on the
	// public /api/ edge.
	// Production leaves it nil → publicChain builds the real RS256/JWKS verifier
	// from ThunderJWKS. A component test sets it to a claims-injector so the real
	// tenant gate runs in ENFORCE with no Thunder/JWKS; ThunderJWKS is then unused
	// (and may be nil). It only substitutes the verifier — orgensure and the
	// deny-by-default tenant gate chain are untouched.
	InboundAuth func(http.Handler) http.Handler

	// MCP discovery ports (dependencies feature). The composition root wires
	// them concretely (external-resource repository / org endpoint catalog /
	// platform resource-type catalog); the mounted handler nil-guards each —
	// a nil MCPExternalResources 503s the route group, a nil lister degrades its
	// one tool to an empty result. The mount itself (mcpRoutes, internal.go)
	// needs Deps.PublisherTokens, its only caller credential.
	MCPExternalResources mcpdiscovery.ExternalResourceReader
	MCPOrgEndpoints      mcpdiscovery.OrgEndpointLister
	MCPResourceTypes     mcpdiscovery.ResourceTypeLister
	MCPGroupCatalog      mcpdiscovery.GroupCatalogLister
	// MCPSpecValidator/MCPSpecNormalizer/MCPSpecFetcher back the OpenAPI spec
	// MCP tools (validate_openapi_spec, fetch_openapi_spec). Wired to the
	// spec package's ValidateOpenAPI/NormalizeOpenAPIYAML/FetchSpecFromURL
	// functions as-is (FetchSpecFromURL is PLATFORM-TOUCHING SSRF hardening —
	// never wrap it with a looser guard). Nil makes the two spec tools return a
	// tool error; it never affects the other tools.
	MCPSpecValidator  mcpdiscovery.SpecValidator
	MCPSpecNormalizer mcpdiscovery.SpecNormalizer
	MCPSpecFetcher    mcpdiscovery.SpecFetcher
	MCPSpecSlicer     mcpdiscovery.SpecSlicer
}

// NewHandler assembles the full HTTP handler with middleware and routes.
// The console's nginx proxy strips the /aep-api-service prefix before
// forwarding, so routes are registered at root level.
func NewHandler(params AppParams) http.Handler {
	// Every route group (public / internal / dev + health) is a row in
	// the mount table, routes.go: the whole request boundary on one screen.
	mux := mountRoutes(params)

	// Global middleware stack (outermost applied last). AddCorrelationID resolves
	// the correlation ID into the context; the global obs.ContextHandler then
	// stamps it onto every slog record, so no per-request logger needs to be
	// stashed.
	var handler http.Handler = mux
	handler = auth.ExtractAuthToken()(handler)
	handler = obs.AddCorrelationID()(handler)
	handler = obs.RecovererOnPanic()(handler)

	return handler
}

// SplitAndTrim splits a comma-separated env value into a list, dropping
// empty entries. Lets JWT_ISSUER / JWT_AUDIENCE accept multiple values
// (e.g. "AEP_CONSOLE,local-dev-seeder") so a single BFF can
// accept both end-user and S2S tokens that carry different `aud`
// claims, without weakening the matcher to a wildcard.
func SplitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
