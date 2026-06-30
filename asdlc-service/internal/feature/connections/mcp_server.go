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

package connections

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/resources"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// MCP discovery server (plan §3 / §9 — "the designing agent figures out the
// existing registered dependencies through MCP tools"). The BFF hosts a minimal
// Model Context Protocol server over JSON-RPC (Streamable-HTTP transport, the
// non-streaming single-response form: the client POSTs a JSON-RPC request and we
// answer with application/json). The architect (agents-service) connects as an
// MCP client and the LLM calls the exposed tools during design so it proposes
// `external` dependencies against connections that ALREADY exist in the org's
// registry instead of inventing names/shapes.
//
// Read-only tools:
//   - list_connections        → every registered `external` connection (name, description, keys)
//   - get_connection_schema    → one connection's config-key schema
//   - list_org_endpoints       → every service endpoint published across the org's
//                                projects + its visibility (the `org-service` catalog, P3)
//
// Mounted ungated at /internal/organizations/{orgHandle}/mcp (service-to-service
// on the compose/cluster network, same trust boundary as the Anthropic
// effective-key resolver the agents-service already calls). orgHandle is the OC
// org id the Registry keys on (e.g. "default"); locally it is also the OC
// namespace the catalog enumerates Workloads in.

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

// mcpTool is the MCP tools/list descriptor.
type mcpTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

// connectionView is the JSON shape returned to the agent for a connection.
type connectionView struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	ConfigKeys  []connectionKeyDTO `json:"configKeys"`
}

type connectionKeyDTO struct {
	Key    string `json:"key"`
	Secret bool   `json:"secret"`
}

// orgEndpointView is the JSON shape returned to the agent for one published
// org endpoint (the `org-service` catalog row).
type orgEndpointView struct {
	Name             string `json:"name"`             // org-service dep name = provider component
	Project          string `json:"project"`          // provider project
	Endpoint         string `json:"endpoint"`         // endpoint name on the provider
	Type             string `json:"type"`             // HTTP | gRPC | …
	NamespaceVisible bool   `json:"namespaceVisible"` // consumable cross-project as an org-service
}

// mcpHandler holds the dependencies for the JSON-RPC MCP server.
type mcpHandler struct {
	registry      *Registry
	catalog       *OrgEndpointCatalog
	resourceTypes *resources.ResourceTypeCatalog
}

// NewMCPHandler returns the JSON-RPC MCP handler for the connection registry +
// the org endpoint catalog + the platform-resource-type catalog. orgHandle is
// read from the request path. catalog may be nil (the list_org_endpoints tool
// then reports an empty catalog). resourceTypes may be nil (the
// list_platform_resource_types tool then reports an empty list).
func NewMCPHandler(registry *Registry, catalog *OrgEndpointCatalog, resourceTypes *resources.ResourceTypeCatalog) http.Handler {
	h := &mcpHandler{registry: registry, catalog: catalog, resourceTypes: resourceTypes}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.registry == nil {
			http.Error(w, "connections registry not configured", http.StatusServiceUnavailable)
			return
		}
		orgHandle := r.PathValue("orgHandle")

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
				"serverInfo":      map[string]any{"name": "asdlc-connections", "version": "1.0.0"},
			})
		case "ping":
			writeRPCResult(w, req.ID, map[string]any{})
		case "tools/list":
			writeRPCResult(w, req.ID, map[string]any{"tools": mcpTools()})
		case "tools/call":
			handleToolCall(w, r, h, orgHandle, req)
		default:
			writeRPCError(w, req.ID, -32601, "method not found: "+req.Method)
		}
	})
}

func mcpTools() []mcpTool {
	return []mcpTool{
		{
			Name: "list_connections",
			Description: "List the external connections (third-party APIs/services) already registered in " +
				"this organization. Use this BEFORE proposing an `external` dependency so you reuse an " +
				"existing connection name + its config-key schema instead of inventing a new one. Returns " +
				"each connection's name, description, and config keys (with which are secret).",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name: "get_connection_schema",
			Description: "Get the config-key schema for one registered external connection by name " +
				"(the keys an `external` dependency on it must supply, and which are secret).",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string", "description": "connection name"}},
				"required":   []string{"name"},
			},
		},
		{
			Name: "list_org_endpoints",
			Description: "List the service endpoints published by OTHER projects in this organization — the " +
				"catalog of `org-service` dependency targets. Use this when a component needs to call an " +
				"existing in-org service (instead of building it or treating it as `external`). Each row gives " +
				"the org-service `name` (= the provider component name to put in the dependency), its project, " +
				"endpoint, type, and `namespaceVisible`. Only propose an `org-service` dependency when " +
				"`namespaceVisible` is true; a row with namespaceVisible=false exists but the provider has NOT " +
				"published it cross-project, so it cannot be consumed yet.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name: "list_platform_resource_types",
			Description: "List the platform-provisioned resource types (databases, caches, queues) installed " +
				"on the cluster. Each entry is a resourceType you can reference in a platform-resource " +
				"dependency, with its provisioning parameters and the outputs it exposes. Read-only — you " +
				"never author these.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}

func handleToolCall(w http.ResponseWriter, r *http.Request, h *mcpHandler, orgHandle string, req jsonrpcRequest) {
	var call struct {
		Name      string `json:"name"`
		Arguments struct {
			Name string `json:"name"`
		} `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &call); err != nil {
		writeRPCError(w, req.ID, -32602, "invalid params")
		return
	}
	slog.InfoContext(r.Context(), "mcp tool call", "org", orgHandle, "tool", call.Name, "arg", call.Arguments.Name)

	switch call.Name {
	case "list_connections":
		conns, err := h.registry.List(r.Context(), orgHandle)
		if err != nil {
			writeToolError(w, req.ID, fmt.Sprintf("list connections: %v", err))
			return
		}
		views := make([]connectionView, 0, len(conns))
		for i := range conns {
			views = append(views, toConnectionView(&conns[i]))
		}
		writeToolText(w, req.ID, mustJSON(map[string]any{"connections": views}))
	case "get_connection_schema":
		if call.Arguments.Name == "" {
			writeToolError(w, req.ID, "missing required argument: name")
			return
		}
		c, err := h.registry.Get(r.Context(), orgHandle, call.Arguments.Name)
		if err != nil {
			writeToolError(w, req.ID, fmt.Sprintf("get connection: %v", err))
			return
		}
		if c == nil {
			writeToolText(w, req.ID, mustJSON(map[string]any{"found": false, "name": call.Arguments.Name}))
			return
		}
		writeToolText(w, req.ID, mustJSON(map[string]any{"found": true, "connection": toConnectionView(c)}))
	case "list_org_endpoints":
		infos, err := h.catalog.List(r.Context(), orgHandle)
		if err != nil {
			writeToolError(w, req.ID, fmt.Sprintf("list org endpoints: %v", err))
			return
		}
		views := make([]orgEndpointView, 0, len(infos))
		for _, e := range infos {
			views = append(views, orgEndpointView{
				Name:             e.Component,
				Project:          e.Project,
				Endpoint:         e.Name,
				Type:             e.Type,
				NamespaceVisible: e.NamespaceVisible(),
			})
		}
		writeToolText(w, req.ID, mustJSON(map[string]any{"endpoints": views}))
	case "list_platform_resource_types":
		types, err := h.resourceTypes.List(r.Context())
		if err != nil {
			writeToolError(w, req.ID, fmt.Sprintf("list platform resource types: %v", err))
			return
		}
		writeToolText(w, req.ID, mustJSON(map[string]any{"resourceTypes": types}))
	default:
		writeRPCError(w, req.ID, -32602, "unknown tool: "+call.Name)
	}
}

func toConnectionView(c *models.Connection) connectionView {
	keys := make([]connectionKeyDTO, 0, len(c.ConfigSchema))
	for _, k := range c.ConfigSchema {
		keys = append(keys, connectionKeyDTO{Key: k.Key, Secret: k.Secret})
	}
	return connectionView{Name: c.Name, Description: c.Description, ConfigKeys: keys}
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
