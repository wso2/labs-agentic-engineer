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

package dependencies

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/wso2/aep/aep-api/models"
)

// mcpTool is the MCP tools/list descriptor.
type mcpTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

// externalResourceView is the JSON shape returned to the agent for one
// registered external resource.
type externalResourceView struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	ConfigKeys  []configKeyDTO `json:"configKeys"`
}

type configKeyDTO struct {
	Key    string `json:"key"`
	Secret bool   `json:"secret"`
}

// orgEndpointView is the JSON shape returned to the agent for one published org
// endpoint (an `org-service` dependency target).
type orgEndpointView struct {
	Name             string `json:"name"`             // org-service dep name = provider component
	Project          string `json:"project"`          // provider project
	Endpoint         string `json:"endpoint"`         // endpoint name on the provider
	Type             string `json:"type"`             // HTTP | gRPC | …
	NamespaceVisible bool   `json:"namespaceVisible"` // consumable cross-project as an org-service
}

// mcpTools returns the read-only tool descriptors advertised by tools/list.
func mcpTools() []mcpTool {
	return []mcpTool{
		{
			Name: "list_external_resources",
			Description: "List the external resources (third-party APIs/services) already registered in " +
				"this organization. Use this BEFORE proposing an `external` dependency so you reuse an " +
				"existing external resource name + its config-key schema instead of inventing a new one. " +
				"Returns each external resource's name, description, and config keys (with which are secret).",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name: "get_external_resource_schema",
			Description: "Get the config-key schema for one registered external resource by name " +
				"(the keys an `external` dependency on it must supply, and which are secret).",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string", "description": "external resource name"}},
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
				"dependency, with a `description` of what the type provides and when to depend on it, its " +
				"provisioning parameters, and the outputs it exposes. Pick the type whose description " +
				"matches the need. Read-only — you never author these.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}

// handleToolCall dispatches a tools/call request to the matching read-only port.
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
	case "list_external_resources":
		resources, err := h.resources.List(r.Context(), orgHandle)
		if err != nil {
			writeToolError(w, req.ID, fmt.Sprintf("list external resources: %v", err))
			return
		}
		views := make([]externalResourceView, 0, len(resources))
		for i := range resources {
			views = append(views, toExternalResourceView(&resources[i]))
		}
		writeToolText(w, req.ID, mustJSON(map[string]any{"externalResources": views}))
	case "get_external_resource_schema":
		if call.Arguments.Name == "" {
			writeToolError(w, req.ID, "missing required argument: name")
			return
		}
		res, err := h.resources.Get(r.Context(), orgHandle, call.Arguments.Name)
		if err != nil {
			writeToolError(w, req.ID, fmt.Sprintf("get external resource: %v", err))
			return
		}
		if res == nil {
			writeToolText(w, req.ID, mustJSON(map[string]any{"found": false, "name": call.Arguments.Name}))
			return
		}
		writeToolText(w, req.ID, mustJSON(map[string]any{"found": true, "externalResource": toExternalResourceView(res)}))
	case "list_org_endpoints":
		if h.orgEndpoints == nil {
			writeToolText(w, req.ID, mustJSON(map[string]any{"endpoints": []any{}}))
			return
		}
		infos, err := h.orgEndpoints.List(r.Context(), orgHandle)
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
		if h.resourceTypes == nil {
			writeToolText(w, req.ID, mustJSON(map[string]any{"resourceTypes": []any{}}))
			return
		}
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

// toExternalResourceView projects a stored ExternalResource to the agent-facing
// shape (name, description, and its config keys with the secret flag).
func toExternalResourceView(er *models.ExternalResource) externalResourceView {
	keys := make([]configKeyDTO, 0, len(er.ConfigKeys))
	for _, k := range er.ConfigKeys {
		keys = append(keys, configKeyDTO{Key: k.Key, Secret: k.Secret})
	}
	return externalResourceView{Name: er.Name, Description: er.Description, ConfigKeys: keys}
}
