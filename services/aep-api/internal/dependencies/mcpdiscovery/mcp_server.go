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
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// MCP discovery server. The BFF hosts a minimal Model Context Protocol server
// over JSON-RPC (Streamable-HTTP transport, non-streaming single-response form:
// the client POSTs a JSON-RPC request and we answer with application/json). The
// agent services connect as MCP clients and the LLM calls the exposed read-only
// tools during design so it proposes dependencies against resources/endpoints
// that ALREADY exist in the org instead of inventing names/shapes.
//
// Read-only tools (see mcp_tools.go):
//   - list_external_resources        → every registered external resource + its config-key schema
//   - get_external_resource_schema   → one external resource's config-key schema
//   - list_org_endpoints             → every service endpoint published across the org
//   - list_org_component_endpoints   → list_org_endpoints resolved with repo coords + discovered OpenAPI spec
//   - list_platform_resource_types   → the platform-provisioned resource types on the cluster
//   - get_remote_git_file_contents   → read a file/directory from an org repo (endpoint spec discovery)
//   - search_remote_git_code         → locate a file by code search in an org repo
//   - validate_openapi_spec          → validate + normalize an OpenAPI doc the caller already has
//   - fetch_openapi_spec             → SSRF-hardened fetch of an OpenAPI doc by URL, then validate + normalize
//
// Mounted at POST /internal/v1/mcp behind auth.AgentsScopedVerifier, which binds
// the acting org onto the request context from a verified BFF-signed token
// (ocOrgId claim). The org is read ONLY from that context — never the
// path/body/header (the source read it from an {orgHandle} path; that is banned
// here).

const mcpProtocolVersion = "2024-11-05"

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"` // absent for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// mcpHandler holds the read-only ports the JSON-RPC MCP server exposes.
type mcpHandler struct {
	resources     ExternalResourceReader
	orgEndpoints  OrgEndpointLister
	resourceTypes ResourceTypeLister
	roles         RoleCatalogLister
	remoteGit     RemoteGitReader
	validateSpec  SpecValidator
	normalizeSpec SpecNormalizer
	fetchSpec     SpecFetcher
}

// NewMCPHandler returns the JSON-RPC MCP handler over the external-resource
// reader, the org endpoint lister, the platform resource-type lister, the
// read-only remote-git reader (endpoint spec discovery), and the OpenAPI spec
// validate/normalize/fetch functions (validate_openapi_spec, fetch_openapi_spec).
// The acting org is resolved from the request context (bound by the auth
// middleware), never from the request itself. A nil external-resource reader
// makes the surface unavailable (503 — it is the surface's core catalog). A
// nil orgEndpoints/resourceTypes/roles degrades that one tool to an empty result; a
// nil remoteGit makes the two remote-git tools return a tool error; a nil
// validateSpec/normalizeSpec/fetchSpec makes the two spec tools return a tool
// error.
func NewMCPHandler(
	er ExternalResourceReader, ep OrgEndpointLister, rt ResourceTypeLister, rc RoleCatalogLister,
	rg RemoteGitReader, vs SpecValidator, ns SpecNormalizer, fs SpecFetcher,
) http.Handler {
	h := &mcpHandler{
		resources: er, orgEndpoints: ep, resourceTypes: rt, roles: rc, remoteGit: rg,
		validateSpec: vs, normalizeSpec: ns, fetchSpec: fs,
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

		var req jsonrpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeRPCError(w, nil, -32700, "parse error")
			return
		}

		// Notifications (no id) get a 202 with no body — e.g. notifications/initialized.
		if len(req.ID) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}

		switch req.Method {
		case "initialize":
			writeRPCResult(w, req.ID, map[string]any{
				"protocolVersion": mcpProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "aep-dependencies", "version": "1.0.0"},
			})
		case "ping":
			writeRPCResult(w, req.ID, map[string]any{})
		case "tools/list":
			writeRPCResult(w, req.ID, map[string]any{"tools": mcpTools()})
		case "tools/call":
			// Tool executions act on the org's behalf with the BFF's own OC
			// service identity. Without this marker the OC transport would see
			// the request's MCP bearer (aud aep-api-mcp — OUR token, not an OC
			// one) as a forwardable user JWT and every OC-backed lookup would
			// 401, silently emptying the catalogs (caught live in E2E S3).
			handleToolCall(w, r.WithContext(auth.WithServiceIdentity(r.Context())), h, orgHandle, req)
		default:
			writeRPCError(w, req.ID, -32601, "method not found: "+req.Method)
		}
	})
}

// ---- JSON-RPC / MCP write helpers ------------------------------------------

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeJSON(w, jsonrpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	writeJSON(w, jsonrpcResponse{JSONRPC: "2.0", ID: id, Error: &jsonrpcError{Code: code, Message: msg}})
}

// writeToolText returns a successful tools/call result with a single text block.
func writeToolText(w http.ResponseWriter, id json.RawMessage, text string) {
	writeRPCResult(w, id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	})
}

// writeToolError returns a tools/call result flagged isError (MCP tool-level error).
func writeToolError(w http.ResponseWriter, id json.RawMessage, text string) {
	writeRPCResult(w, id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": true,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("mcp: encode response", "error", err)
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("{\"error\":%q}", err.Error())
	}
	return string(b)
}
