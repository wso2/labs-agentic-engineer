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
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/platform/mcprpc"
)

// mcpTool is the MCP tools/list descriptor.
type mcpTool = mcprpc.Tool

// externalResourceView is the JSON shape returned to the agent for one
// registered external resource (including zero-consumer Registered rows).
type externalResourceView struct {
	Name                    string           `json:"name"`
	Description             string           `json:"description,omitempty"`
	Provider                string           `json:"provider,omitempty"`
	ConfigKeys              []configKeyDTO   `json:"configKeys"`
	Contract                *contractDTO     `json:"contract,omitempty"`
	ConsumptionInstructions string           `json:"consumptionInstructions,omitempty"`
	ResourceDocs            []resourceDocDTO `json:"resourceDocs,omitempty"`
}

// contractDTO is the record's contract document pointer — `{type, path}` into
// the org docs repo. The design agent never reads it: it writes the stub and
// the platform copies the document into the project at save.
type contractDTO struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

type configKeyDTO struct {
	Key    string `json:"key"`
	Secret bool   `json:"secret,omitempty"`
}

// resourceDocDTO is an org resource-docs pointer (type + URL or path) — never
// file bodies or secret values.
type resourceDocDTO struct {
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
	Path string `json:"path,omitempty"`
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

// orgComponentEndpointView is the JSON shape returned to the agent for one
// resolved org-wide component endpoint — list_org_endpoints's rows enriched
// with the provider's repo coordinates and a discovered OpenAPI contract
// (endpoint spec discovery). Mirrors endpoints.OrgComponentEndpoint.
type orgComponentEndpointView struct {
	Project          string           `json:"project"`
	Component        string           `json:"component"`
	Endpoint         string           `json:"endpoint"`
	Type             string           `json:"type"`
	Port             int32            `json:"port,omitempty"`
	BasePath         string           `json:"basePath,omitempty"`
	NamespaceVisible bool             `json:"namespaceVisible"`
	Owner            string           `json:"owner,omitempty"`
	Repo             string           `json:"repo,omitempty"`
	Subdir           string           `json:"subdir,omitempty"`
	Branch           string           `json:"branch,omitempty"`
	Spec             endpointSpecView `json:"spec"`
}

// endpointSpecView is the JSON shape for an OrgComponentEndpoint's discovered
// OpenAPI contract availability (see endpoints.EndpointSpec).
type endpointSpecView struct {
	Availability  string `json:"availability"`
	InlineContent string `json:"inlineContent,omitempty"`
	Path          string `json:"path,omitempty"`
}

// validateSpecView is the JSON shape returned by validate_openapi_spec: parse
// result, operation count, the normalized doc (only when validation AND
// normalization both succeed), and any errors encountered.
type validateSpecView struct {
	Valid             bool     `json:"valid"`
	Operations        int      `json:"operations"`
	NormalizedContent string   `json:"normalizedContent,omitempty"`
	Errors            []string `json:"errors"`
}

// fetchSpecView is the JSON shape returned by fetch_openapi_spec: the fetched
// spec normalized to canonical form, its operation count, and the URL it was
// fetched from.
type fetchSpecView struct {
	Content    string `json:"content"`
	Operations int    `json:"operations"`
	SourceURL  string `json:"sourceUrl"`
}

// sliceSpecView is the JSON shape returned by slice_openapi_spec: the slice
// (canonical form), its operation count, and the provenance block the agent
// copies verbatim into the dependency's dependency.json.
type sliceSpecView struct {
	Content    string              `json:"content"`
	Operations int                 `json:"operations"`
	Provenance sliceProvenanceView `json:"provenance"`
}

type sliceProvenanceView struct {
	SourceURL string `json:"sourceUrl,omitempty"`
	SHA256    string `json:"sha256"`
	FetchedAt string `json:"fetchedAt"`
	Sliced    bool   `json:"sliced"`
}

// maxToolSpecBytes is fetch_openapi_spec's tool-level size cap (256 KiB) —
// tighter than FetchSpecFromURL's own 5 MiB SSRF-hardened cap, applied purely
// for LLM context-window safety on top of the untouched network-level guard.
const maxToolSpecBytes = 256 << 10

// listGroupsDescription is the description text behind `list_groups` — the ONE
// place a model is told what the directory-group catalog is and which of its
// fields mean what.
const listGroupsDescription = "Lists the directory groups in this organization's environment directory. " +
	"Use it before writing security.json roles[].assignTo: reuse an existing group name when the " +
	"people who should hold the role already form a group; otherwise declare a new name in groups[] " +
	"(it is created at build). memberCount is the number of users in the group today; projects is how " +
	"many projects already bind a role to it; platformCreated says whether this platform created it."

// mcpTools returns the read-only tool descriptors advertised by tools/list.
func mcpTools() []mcpTool {
	return []mcpTool{
		{
			Name: "list_external_resources",
			Description: "List the Registered External resources (third-party APIs/services) the organization " +
				"registered — including ones no project has used yet. A resource another project defined for " +
				"itself is NOT listed: it belongs to that project. Use this BEFORE proposing an `external` " +
				"dependency: when a row fits, write the stub dependency.json " +
				"`{ \"name\": <row name>, \"resource\": { \"ref\": <row name>, \"name\": <row name> } }` and the " +
				"platform copies the record (provider, config keys, instructions, contract document) into the " +
				"project at save — never retype them. Returns each resource's name, description, provider, config " +
				"keys (with which are secret), consumptionInstructions, its contract pointer ({type, path}) and " +
				"resourceDocs pointers — never file bodies.",
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
			Name: "list_org_component_endpoints",
			Description: "List every org-wide component endpoint published across this organization, each " +
				"resolved with the provider's real OpenAPI contract (when discoverable) and repo coordinates. " +
				"Use this INSTEAD of list_org_endpoints when you need the endpoint's actual request/response " +
				"contract to integrate against it — not just its name and type. Each row's `spec.availability` " +
				"is `inline` (spec.inlineContent carries the OpenAPI document verbatim — read it directly), " +
				"`repo` (no inline spec, but owner/repo/subdir/branch locate the provider's source so you can " +
				"read the contract from there), or `none` (neither is resolvable — treat the integration as " +
				"undocumented).",
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
		{
			Name:        "list_groups",
			Description: listGroupsDescription,
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name: "list_guardrail_policies",
			Description: "List the AI-gateway guardrails an ai-agent of this organization may declare in its " +
				"agent.afm.md `x-aep.guardrails`. Each entry is a policy this organization's gateway offers " +
				"and the platform can apply: its exact `name` (the `policy` you write), a `description`, and " +
				"`parameters` — the JSON Schema of the settings you may set in `params`. Path settings " +
				"(jsonPath) are the platform's and are not listed. Call this before declaring a guardrail; " +
				"declare only names it returns. An empty list means none can be applied here. Read-only.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name: "validate_openapi_spec",
			Description: "Validate an OpenAPI 3.x document you already have (pasted, generated, or read via " +
				"get_remote_git_file_contents) BEFORE proposing it as a dependency's spec. Parses the document " +
				"and counts its operations; on success also returns a normalized canonical-form encoding. Fetches " +
				"nothing and stores nothing — pass the spec content directly. `valid` is false and `errors` is " +
				"populated when the document does not parse or is not a valid OpenAPI 3.x doc.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"content": map[string]any{"type": "string", "description": "OpenAPI 3.x document content (YAML or JSON)"}},
				"required":   []string{"content"},
			},
		},
		{
			Name: "fetch_openapi_spec",
			Description: "Fetch an OpenAPI spec from a user-supplied URL, then validate and normalize it in one " +
				"step. The fetch is SSRF-hardened (https only, public IPs only, redirect-guarded, size- and " +
				"time-capped) and additionally capped at 256 KiB for this tool — if the spec is larger, ask the " +
				"user for a trimmed spec instead of retrying. Read-only; stores nothing.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"url": map[string]any{"type": "string", "description": "absolute https URL to fetch the OpenAPI document from"}},
				"required":   []string{"url"},
			},
		},
		{
			Name: "slice_openapi_spec",
			Description: "Cut the contract a design commits to from a provider's OpenAPI document: the operations " +
				"you name plus every schema they reference, as a standalone document, with the provenance block " +
				"to record beside it. Pass `url` (the published document — fetched whole, outside your context, " +
				"so its size does not matter) OR `content` (a document the user supplied), and `operations`: " +
				"operationIds, \"METHOD /path\" pairs, or bare \"/path\" entries. An operation not in the " +
				"document is an error naming it. Then addFile the returned `content` as the dependency's " +
				"openapi.yaml and copy `provenance` into its dependency.json. Read-only; stores nothing.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url":     map[string]any{"type": "string", "description": "absolute https URL of the whole OpenAPI document (alternative to content)"},
					"content": map[string]any{"type": "string", "description": "the whole OpenAPI document (alternative to url)"},
					"operations": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "the operations the design uses: operationId, \"METHOD /path\", or \"/path\"",
					},
				},
				"required": []string{"operations"},
			},
		},
	}
}

// handleToolCall dispatches a tools/call request to the matching read-only port.
func handleToolCall(w http.ResponseWriter, r *http.Request, h *mcpHandler, orgHandle string, req mcprpc.Request) {
	var call struct {
		Name      string `json:"name"`
		Arguments struct {
			Name       string   `json:"name"`
			Content    string   `json:"content"`
			URL        string   `json:"url"`
			Operations []string `json:"operations"`
		} `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &call); err != nil {
		mcprpc.WriteError(w, req.ID, mcprpc.CodeInvalidParams, "invalid params")
		return
	}
	slog.InfoContext(r.Context(), "mcp tool call", "org", orgHandle, "tool", call.Name, "arg", call.Arguments.Name)

	switch call.Name {
	case "list_external_resources":
		resources, err := h.resources.List(r.Context(), orgHandle)
		if err != nil {
			mcprpc.WriteToolError(w, req.ID, fmt.Sprintf("list external resources: %v", err))
			return
		}
		views := make([]externalResourceView, 0, len(resources))
		for i := range resources {
			views = append(views, toExternalResourceView(&resources[i]))
		}
		mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(map[string]any{"externalResources": views}))
	case "get_external_resource_schema":
		if call.Arguments.Name == "" {
			mcprpc.WriteToolError(w, req.ID, "missing required argument: name")
			return
		}
		res, err := h.resources.Get(r.Context(), orgHandle, call.Arguments.Name)
		if err != nil {
			mcprpc.WriteToolError(w, req.ID, fmt.Sprintf("get external resource: %v", err))
			return
		}
		if res == nil {
			mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(map[string]any{"found": false, "name": call.Arguments.Name}))
			return
		}
		mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(map[string]any{"found": true, "externalResource": toExternalResourceView(res)}))
	case "list_org_endpoints":
		if h.orgEndpoints == nil {
			mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(map[string]any{"endpoints": []any{}}))
			return
		}
		infos, err := h.orgEndpoints.List(r.Context(), orgHandle)
		if err != nil {
			mcprpc.WriteToolError(w, req.ID, fmt.Sprintf("list org endpoints: %v", err))
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
		mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(map[string]any{"endpoints": views}))
	case "list_org_component_endpoints":
		if h.orgEndpoints == nil {
			mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(map[string]any{"endpoints": []any{}}))
			return
		}
		resolved, err := h.orgEndpoints.ListResolved(r.Context(), orgHandle)
		if err != nil {
			mcprpc.WriteToolError(w, req.ID, fmt.Sprintf("list org component endpoints: %v", err))
			return
		}
		views := make([]orgComponentEndpointView, 0, len(resolved))
		for i := range resolved {
			views = append(views, toOrgComponentEndpointView(&resolved[i]))
		}
		mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(map[string]any{"endpoints": views}))
	case "list_platform_resource_types":
		if h.resourceTypes == nil {
			mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(map[string]any{"resourceTypes": []any{}}))
			return
		}
		types, err := h.resourceTypes.List(r.Context())
		if err != nil {
			mcprpc.WriteToolError(w, req.ID, fmt.Sprintf("list platform resource types: %v", err))
			return
		}
		mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(map[string]any{"resourceTypes": types}))
	case "list_groups":
		if h.groupCatalog == nil {
			mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(map[string]any{"groups": []any{}}))
			return
		}
		// orgHandle is the verified ocOrgId claim: the catalog belongs to that
		// org's environment directory, and no tool argument may choose it.
		groups, err := h.groupCatalog.ListGroupCatalog(r.Context(), orgHandle)
		if err != nil {
			mcprpc.WriteToolError(w, req.ID, fmt.Sprintf("list groups: %v", err))
			return
		}
		mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(map[string]any{"groups": groups}))
	case "list_guardrail_policies":
		if h.guardrails == nil {
			mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(map[string]any{"guardrails": []any{}}))
			return
		}
		// orgHandle is the verified ocOrgId claim: the catalog is that org's
		// gateway's, and no tool argument may choose it.
		policies, err := h.guardrails.GuardrailCatalog(r.Context(), orgHandle)
		if err != nil {
			mcprpc.WriteToolError(w, req.ID, fmt.Sprintf("list guardrail policies: %v", err))
			return
		}
		if policies == nil {
			policies = []GuardrailPolicy{}
		}
		mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(map[string]any{"guardrails": policies}))
	case "validate_openapi_spec":
		if h.validateSpec == nil || h.normalizeSpec == nil {
			mcprpc.WriteToolError(w, req.ID, "spec validator not configured")
			return
		}
		if call.Arguments.Content == "" {
			mcprpc.WriteToolError(w, req.ID, "missing required argument: content")
			return
		}
		mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(h.validateAndNormalize([]byte(call.Arguments.Content))))
	case "fetch_openapi_spec":
		if h.fetchSpec == nil || h.validateSpec == nil || h.normalizeSpec == nil {
			mcprpc.WriteToolError(w, req.ID, "spec fetcher not configured")
			return
		}
		if call.Arguments.URL == "" {
			mcprpc.WriteToolError(w, req.ID, "missing required argument: url")
			return
		}
		raw, err := h.fetchSpec(r.Context(), call.Arguments.URL)
		if err != nil {
			// FetchSpecFromURL's own errors are already complete/user-facing
			// (e.g. "refusing to fetch from non-public address", "fetch spec:
			// <transport err>") — surfaced verbatim rather than re-wrapped, to
			// avoid a doubled "fetch spec: fetch spec: ..." prefix.
			mcprpc.WriteToolError(w, req.ID, err.Error())
			return
		}
		// Tool-level context-safety cap, tighter than (and layered on top of,
		// never a substitute for) FetchSpecFromURL's own SSRF-hardened 5 MiB cap.
		if len(raw) > maxToolSpecBytes {
			mcprpc.WriteToolError(w, req.ID, "spec too large — ask the user for a trimmed spec")
			return
		}
		result := h.validateAndNormalize(raw)
		if !result.Valid {
			mcprpc.WriteToolError(w, req.ID, fmt.Sprintf("fetched spec failed validation: %s", strings.Join(result.Errors, "; ")))
			return
		}
		if len(result.Errors) > 0 {
			mcprpc.WriteToolError(w, req.ID, fmt.Sprintf("normalize fetched spec: %s", strings.Join(result.Errors, "; ")))
			return
		}
		mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(fetchSpecView{
			Content:    result.NormalizedContent,
			Operations: result.Operations,
			SourceURL:  call.Arguments.URL,
		}))
	case "slice_openapi_spec":
		if h.sliceSpec == nil || h.validateSpec == nil {
			mcprpc.WriteToolError(w, req.ID, "spec slicer not configured")
			return
		}
		if len(call.Arguments.Operations) == 0 {
			mcprpc.WriteToolError(w, req.ID, "missing required argument: operations")
			return
		}
		var raw []byte
		switch {
		case call.Arguments.URL != "" && call.Arguments.Content != "":
			mcprpc.WriteToolError(w, req.ID, "pass url OR content, not both")
			return
		case call.Arguments.URL != "":
			if h.fetchSpec == nil {
				mcprpc.WriteToolError(w, req.ID, "spec fetcher not configured")
				return
			}
			fetched, err := h.fetchSpec(r.Context(), call.Arguments.URL)
			if err != nil {
				mcprpc.WriteToolError(w, req.ID, err.Error())
				return
			}
			raw = fetched
		case call.Arguments.Content != "":
			raw = []byte(call.Arguments.Content)
		default:
			mcprpc.WriteToolError(w, req.ID, "missing required argument: url or content")
			return
		}
		slice, err := h.sliceSpec(raw, call.Arguments.Operations)
		if err != nil {
			mcprpc.WriteToolError(w, req.ID, err.Error())
			return
		}
		if len(slice) > maxToolSpecBytes {
			mcprpc.WriteToolError(w, req.ID, "the slice is still too large — name fewer operations")
			return
		}
		ops, err := h.validateSpec(slice)
		if err != nil {
			mcprpc.WriteToolError(w, req.ID, fmt.Sprintf("slice failed validation: %v", err))
			return
		}
		mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(sliceSpecView{
			Content:    string(slice),
			Operations: ops,
			Provenance: sliceProvenanceView{
				SourceURL: call.Arguments.URL,
				SHA256:    fmt.Sprintf("%x", sha256.Sum256(raw)),
				FetchedAt: time.Now().UTC().Format(time.RFC3339),
				Sliced:    true,
			},
		}))
	default:
		mcprpc.WriteError(w, req.ID, mcprpc.CodeInvalidParams, "unknown tool: "+call.Name)
	}
}

// validateAndNormalize runs h.validateSpec then h.normalizeSpec over raw spec
// content and projects the outcome into validate_openapi_spec's agent-facing
// shape (also reused as the validate+normalize half of fetch_openapi_spec).
func (h *mcpHandler) validateAndNormalize(raw []byte) validateSpecView {
	ops, err := h.validateSpec(raw)
	if err != nil {
		return validateSpecView{Valid: false, Errors: []string{err.Error()}}
	}
	normalized, err := h.normalizeSpec(string(raw))
	if err != nil {
		return validateSpecView{Valid: true, Operations: ops, Errors: []string{fmt.Sprintf("normalize: %v", err)}}
	}
	return validateSpecView{Valid: true, Operations: ops, NormalizedContent: normalized, Errors: []string{}}
}

// toExternalResourceView projects an external resource's definition —
// reconstructed from its authored OpenChoreo ResourceType via
// openchoreo.ExternalDefinitionFromRT — to the agent-facing shape (name,
// description, config keys with the secret flag, consumptionInstructions, and
// resourceDocs pointers). Ensure authors the RT at register, so a
// zero-consumer Registered row is listable here once these fields are carried.
func toExternalResourceView(er *openchoreo.ExternalResourceDefinition) externalResourceView {
	keys := make([]configKeyDTO, 0, len(er.Config))
	for _, k := range er.Config {
		keys = append(keys, configKeyDTO{Key: k.Key, Secret: k.Secret})
	}
	docs := make([]resourceDocDTO, 0, len(er.ResourceDocs))
	for _, d := range er.ResourceDocs {
		docs = append(docs, resourceDocDTO{Type: d.Type, URL: d.URL, Path: d.Path})
	}
	view := externalResourceView{
		Name:                    er.Name,
		Description:             er.Description,
		Provider:                er.Provider,
		ConfigKeys:              keys,
		ConsumptionInstructions: er.ConsumptionInstructions,
		ResourceDocs:            docs,
	}
	if er.Contract != nil {
		view.Contract = &contractDTO{Type: er.Contract.Type, Path: er.Contract.Path}
	}
	return view
}

// toOrgComponentEndpointView projects a resolved OrgComponentEndpoint to the
// agent-facing shape (coords + discovered spec availability).
func toOrgComponentEndpointView(e *dependencies.OrgComponentEndpoint) orgComponentEndpointView {
	return orgComponentEndpointView{
		Project:          e.Project,
		Component:        e.Component,
		Endpoint:         e.Endpoint,
		Type:             e.Type,
		Port:             e.Port,
		BasePath:         e.BasePath,
		NamespaceVisible: e.NamespaceVisible,
		Owner:            e.Owner,
		Repo:             e.Repo,
		Subdir:           e.Subdir,
		Branch:           e.Branch,
		Spec: endpointSpecView{
			Availability:  e.Spec.Availability,
			InlineContent: e.Spec.InlineContent,
			Path:          e.Spec.Path,
		},
	}
}
