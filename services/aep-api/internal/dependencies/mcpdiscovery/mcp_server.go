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

package mcpdiscovery

import (
	"net/http"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/mcprpc"
)

// MCP discovery server. aep-api hosts a minimal Model Context Protocol server
// (platform/mcprpc: JSON-RPC over Streamable HTTP, one request, one
// application/json answer). The coding runner and the AE Studio tools pod
// connect as MCP clients and the LLM calls the exposed read-only tools so it
// proposes dependencies against resources/endpoints that ALREADY exist in the
// org instead of inventing names/shapes. The two remote-git tools
// (get_remote_git_file_contents, search_remote_git_code) are not served here:
// the runner and the tools pod serve them in-process with the org's own GitHub
// credential.
//
// Read-only tools (see mcp_tools.go):
//   - list_external_resources        → every registered external resource + its config-key schema
//   - get_external_resource_schema   → one external resource's config-key schema
//   - list_org_endpoints             → every service endpoint published across the org
//   - list_org_component_endpoints   → list_org_endpoints resolved with repo coords + discovered OpenAPI spec
//   - list_platform_resource_types   → the platform-provisioned resource types on the cluster
//   - list_groups                    → the org's directory groups
//   - validate_openapi_spec          → validate + normalize an OpenAPI doc the caller already has
//   - fetch_openapi_spec             → SSRF-hardened fetch of an OpenAPI doc by URL, then validate + normalize
//   - slice_openapi_spec             → cut the named operations + their schemas out of a provider's OpenAPI doc
//   - list_guardrail_policies        → the AI-gateway guardrails an ai-agent of the org may declare
//
// Mounted at POST /internal/v1/mcp behind auth.MCPGate, which binds the acting
// org onto the request context from a verified publisher client token
// (aep-publisher-<org>, the coding runner's) or a verified, recorded AE Studio
// client token (ae-studio-<org>, the AE Studio tools pod's). The org is read
// ONLY from that context — never
// the path/body/header (the source read it from an {orgHandle} path; that is
// banned here).

// mcpHandler holds the read-only ports the JSON-RPC MCP server exposes.
type mcpHandler struct {
	resources     ExternalResourceReader
	orgEndpoints  OrgEndpointLister
	resourceTypes ResourceTypeLister
	groupCatalog  GroupCatalogLister
	validateSpec  SpecValidator
	normalizeSpec SpecNormalizer
	fetchSpec     SpecFetcher
	sliceSpec     SpecSlicer
	guardrails    GuardrailCatalogLister
}

// NewMCPHandler returns the JSON-RPC MCP handler over the external-resource
// reader, the org endpoint lister, the platform resource-type lister, the group
// catalog, and the OpenAPI spec validate/normalize/fetch/slice functions
// (validate_openapi_spec, fetch_openapi_spec, slice_openapi_spec).
// The acting org is resolved from the request context (bound by the auth
// middleware), never from the request itself. A nil external-resource reader
// makes the surface unavailable (503 — it is the surface's core catalog). A
// nil orgEndpoints/resourceTypes/groupCatalog/guardrails degrades that one
// tool to an empty result; a nil validateSpec/normalizeSpec/fetchSpec/sliceSpec
// makes the spec tool that needs it return a tool error.
func NewMCPHandler(
	er ExternalResourceReader, ep OrgEndpointLister, rt ResourceTypeLister, gc GroupCatalogLister,
	vs SpecValidator, ns SpecNormalizer, fs SpecFetcher, ss SpecSlicer,
	gl GuardrailCatalogLister,
) http.Handler {
	h := &mcpHandler{
		resources: er, orgEndpoints: ep, resourceTypes: rt, groupCatalog: gc,
		validateSpec: vs, normalizeSpec: ns, fetchSpec: fs, sliceSpec: ss, guardrails: gl,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.resources == nil {
			http.Error(w, "external resource registry not configured", http.StatusServiceUnavailable)
			return
		}
		// Org comes SOLELY from the verified MCP token (bound by the auth
		// middleware). An unbound org means the mount was not wrapped in the
		// verifier — a wiring bug; fail closed rather than act org-less.
		orgHandle, ok := auth.MCPOrgFromContext(r.Context())
		if !ok {
			http.Error(w, "org not resolved", http.StatusUnauthorized)
			return
		}

		mcprpc.Server{
			Name: "aep-dependencies", Version: "1.0.0", Tools: mcpTools(),
			// Tool executions act on the org's behalf with the BFF's own OC
			// service identity. Without this marker the OC transport would see
			// the request's MCP bearer (aud aep-api-mcp — OUR token, not an OC
			// one) as a forwardable user JWT and every OC-backed lookup would
			// 401, silently emptying the catalogs (caught live in E2E S3).
			Call: func(w http.ResponseWriter, r *http.Request, req mcprpc.Request) {
				handleToolCall(w, r.WithContext(auth.WithServiceIdentity(r.Context())), h, orgHandle, req)
			},
		}.Serve(w, r)
	})
}
