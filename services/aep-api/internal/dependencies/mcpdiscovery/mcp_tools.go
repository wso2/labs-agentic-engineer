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
	"strings"
	"unicode/utf8"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/dependencies"
)

// mcpTool is the MCP tools/list descriptor.
type mcpTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

// externalResourceView is the JSON shape returned to the agent for one
// registered external resource (including zero-consumer Registered rows).
type externalResourceView struct {
	Name                    string           `json:"name"`
	Description             string           `json:"description,omitempty"`
	ConfigKeys              []configKeyDTO   `json:"configKeys"`
	ConsumptionInstructions string           `json:"consumptionInstructions,omitempty"`
	ResourceDocs            []resourceDocDTO `json:"resourceDocs,omitempty"`
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

// remoteGitFileView is the JSON shape returned by get_remote_git_file_contents —
// a file's decoded content + sha, or a directory's entries (folded like
// github-mcp-server's get_file_contents).
type remoteGitFileView struct {
	Content     string               `json:"content,omitempty"`
	SHA         string               `json:"sha,omitempty"`
	IsDirectory bool                 `json:"isDirectory"`
	Entries     []remoteGitEntryView `json:"entries,omitempty"`
	// Note explains a withheld or shortened Content: binary refusal, or text
	// truncation. Empty when Content is the whole file.
	Note string `json:"note,omitempty"`
}

type remoteGitEntryView struct {
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

// remoteGitSearchHitView is one item of search_remote_git_code's result.
type remoteGitSearchHitView struct {
	Path string `json:"path"`
	SHA  string `json:"sha"`
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

// maxToolSpecBytes is fetch_openapi_spec's tool-level size cap (256 KiB) —
// tighter than FetchSpecFromURL's own 5 MiB SSRF-hardened cap, applied purely
// for LLM context-window safety on top of the untouched network-level guard.
const maxToolSpecBytes = 256 << 10

// mcpTools returns the read-only tool descriptors advertised by tools/list.
func mcpTools() []mcpTool {
	return []mcpTool{
		{
			Name: "list_external_resources",
			Description: "List the external resources (third-party APIs/services) already registered in " +
				"this organization — including Registered records that no project has consumed yet. Use this " +
				"BEFORE proposing an `external` dependency so you reuse an existing external resource name + " +
				"its config-key schema instead of inventing a new one. Returns each external resource's name, " +
				"description, config keys (with which are secret), consumptionInstructions, and resourceDocs " +
				"pointers ({type, url} or {type, path} — never file bodies).",
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
			Name: "list_roles",
			Description: "List the roles that already exist on the platform identity provider. " +
				"Roles are SHARED across projects, so reuse a matching row's name verbatim instead " +
				"of inventing a near-duplicate — take the row whose `description` matches the need, " +
				"not the one whose name echoes the requirement's wording. Each row gives the role " +
				"`name`, its `description`, `platformCreated` (true when this platform created it — " +
				"only those can be given test users) and `memberCount`. Read-only: you never author " +
				"these, and nothing you do creates one. The platform creates the roles your " +
				"`specs/design/roles.json` declares when the user clicks Build.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			Name: "get_remote_git_file_contents",
			Description: "Read a file (or list a directory) from a repository in THIS organization over the " +
				"GitHub API — no clone. Use this AFTER list_org_component_endpoints reports a provider whose " +
				"`spec.availability` is `repo`: pass that row's owner/repo plus the spec path to read the real " +
				"OpenAPI document. A file returns decoded `content` + `sha`; a directory returns `entries[]` " +
				"(each with path/type/sha) so you can drill down. `ref` is optional (branch/tag/commit; " +
				"defaults to the repo's default branch). TEXT ONLY: a binary file (PDF, image, …) answers with " +
				"its sha and a `note` instead of content — do not retry, it will never return bytes; oversized " +
				"text is truncated with a note. Read-only, and restricted to your own organization's " +
				"repos — a request for any other owner is refused.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"owner": map[string]any{"type": "string", "description": "repo owner — MUST be your organization's GitHub account"},
					"repo":  map[string]any{"type": "string", "description": "repository name"},
					"path":  map[string]any{"type": "string", "description": "repo-relative file or directory path (empty = repo root)"},
					"ref":   map[string]any{"type": "string", "description": "optional branch/tag/commit"},
				},
				"required": []string{"owner", "repo", "path"},
			},
		},
		{
			Name: "search_remote_git_code",
			Description: "Search code in a repository in THIS organization over the GitHub API to LOCATE a " +
				"file when you do not know its exact path (e.g. find where an `openapi.yaml` lives before " +
				"reading it with get_remote_git_file_contents). Returns matching `items[]` of {path, sha}. " +
				"Read-only, and restricted to your own organization's repos — a request for any other owner " +
				"is refused.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"owner": map[string]any{"type": "string", "description": "repo owner — MUST be your organization's GitHub account"},
					"repo":  map[string]any{"type": "string", "description": "repository name"},
					"query": map[string]any{"type": "string", "description": "code search query (the repo scope is added for you)"},
				},
				"required": []string{"owner", "repo", "query"},
			},
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
	}
}

// handleToolCall dispatches a tools/call request to the matching read-only port.
func handleToolCall(w http.ResponseWriter, r *http.Request, h *mcpHandler, orgHandle string, req jsonrpcRequest) {
	var call struct {
		Name      string `json:"name"`
		Arguments struct {
			Name    string `json:"name"`
			Owner   string `json:"owner"`
			Repo    string `json:"repo"`
			Path    string `json:"path"`
			Ref     string `json:"ref"`
			Query   string `json:"query"`
			Content string `json:"content"`
			URL     string `json:"url"`
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
	case "list_org_component_endpoints":
		if h.orgEndpoints == nil {
			writeToolText(w, req.ID, mustJSON(map[string]any{"endpoints": []any{}}))
			return
		}
		resolved, err := h.orgEndpoints.ListResolved(r.Context(), orgHandle)
		if err != nil {
			writeToolError(w, req.ID, fmt.Sprintf("list org component endpoints: %v", err))
			return
		}
		views := make([]orgComponentEndpointView, 0, len(resolved))
		for i := range resolved {
			views = append(views, toOrgComponentEndpointView(&resolved[i]))
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
	case "list_roles":
		if h.roles == nil {
			writeToolText(w, req.ID, mustJSON(map[string]any{"roles": []any{}}))
			return
		}
		roles, err := h.roles.ListRoleCatalog(r.Context())
		if err != nil {
			writeToolError(w, req.ID, fmt.Sprintf("list roles: %v", err))
			return
		}
		writeToolText(w, req.ID, mustJSON(map[string]any{"roles": roles}))
	case "get_remote_git_file_contents":
		if h.remoteGit == nil {
			writeToolError(w, req.ID, "remote git reader not configured")
			return
		}
		if call.Arguments.Owner == "" || call.Arguments.Repo == "" {
			writeToolError(w, req.ID, "missing required arguments: owner and repo")
			return
		}
		// orgHandle is the verified ocOrgId claim — the reader resolves the org's
		// credential from it and refuses any owner that is not the org's own
		// GitHub account. The owner is NEVER trusted to name the org.
		file, err := h.remoteGit.GetFileContents(r.Context(), orgHandle,
			call.Arguments.Owner, call.Arguments.Repo, call.Arguments.Path, call.Arguments.Ref)
		if err != nil {
			writeToolError(w, req.ID, fmt.Sprintf("get remote git file contents: %v", err))
			return
		}
		writeToolText(w, req.ID, mustJSON(toRemoteGitFileView(file)))
	case "search_remote_git_code":
		if h.remoteGit == nil {
			writeToolError(w, req.ID, "remote git reader not configured")
			return
		}
		if call.Arguments.Owner == "" || call.Arguments.Repo == "" || call.Arguments.Query == "" {
			writeToolError(w, req.ID, "missing required arguments: owner, repo and query")
			return
		}
		hits, err := h.remoteGit.SearchCode(r.Context(), orgHandle,
			call.Arguments.Owner, call.Arguments.Repo, call.Arguments.Query)
		if err != nil {
			writeToolError(w, req.ID, fmt.Sprintf("search remote git code: %v", err))
			return
		}
		items := make([]remoteGitSearchHitView, 0, len(hits))
		for _, hit := range hits {
			items = append(items, remoteGitSearchHitView{Path: hit.Path, SHA: hit.SHA})
		}
		writeToolText(w, req.ID, mustJSON(map[string]any{"items": items}))
	case "validate_openapi_spec":
		if h.validateSpec == nil || h.normalizeSpec == nil {
			writeToolError(w, req.ID, "spec validator not configured")
			return
		}
		if call.Arguments.Content == "" {
			writeToolError(w, req.ID, "missing required argument: content")
			return
		}
		writeToolText(w, req.ID, mustJSON(h.validateAndNormalize([]byte(call.Arguments.Content))))
	case "fetch_openapi_spec":
		if h.fetchSpec == nil || h.validateSpec == nil || h.normalizeSpec == nil {
			writeToolError(w, req.ID, "spec fetcher not configured")
			return
		}
		if call.Arguments.URL == "" {
			writeToolError(w, req.ID, "missing required argument: url")
			return
		}
		raw, err := h.fetchSpec(r.Context(), call.Arguments.URL)
		if err != nil {
			// FetchSpecFromURL's own errors are already complete/user-facing
			// (e.g. "refusing to fetch from non-public address", "fetch spec:
			// <transport err>") — surfaced verbatim rather than re-wrapped, to
			// avoid a doubled "fetch spec: fetch spec: ..." prefix.
			writeToolError(w, req.ID, err.Error())
			return
		}
		// Tool-level context-safety cap, tighter than (and layered on top of,
		// never a substitute for) FetchSpecFromURL's own SSRF-hardened 5 MiB cap.
		if len(raw) > maxToolSpecBytes {
			writeToolError(w, req.ID, "spec too large — ask the user for a trimmed spec")
			return
		}
		result := h.validateAndNormalize(raw)
		if !result.Valid {
			writeToolError(w, req.ID, fmt.Sprintf("fetched spec failed validation: %s", strings.Join(result.Errors, "; ")))
			return
		}
		if len(result.Errors) > 0 {
			writeToolError(w, req.ID, fmt.Sprintf("normalize fetched spec: %s", strings.Join(result.Errors, "; ")))
			return
		}
		writeToolText(w, req.ID, mustJSON(fetchSpecView{
			Content:    result.NormalizedContent,
			Operations: result.Operations,
			SourceURL:  call.Arguments.URL,
		}))
	default:
		writeRPCError(w, req.ID, -32602, "unknown tool: "+call.Name)
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
	return externalResourceView{
		Name:                    er.Name,
		Description:             er.Description,
		ConfigKeys:              keys,
		ConsumptionInstructions: er.ConsumptionInstructions,
		ResourceDocs:            docs,
	}
}

// maxToolFileBytes caps the file content one tool result may carry. A tool
// result is prompt input: an 868KB PDF fetched through this tool once rode a
// live turn as ~1.5M junk tokens per model step, then killed the conversation's
// jsonb persist — Postgres rejects U+0000 anywhere in a jsonb document, and the
// PDF's bytes carried plenty. 128KB of text is far beyond any OpenAPI document
// this tool exists to read.
const maxToolFileBytes = 128 << 10

// toRemoteGitFileView projects a Contents API read to the agent-facing shape,
// guarding what may ride a prompt: binary content is withheld (its facts —
// path, sha, size — still answer), oversized text is truncated with a note.
func toRemoteGitFileView(f *RemoteGitFile) remoteGitFileView {
	v := remoteGitFileView{
		Content:     f.Content,
		SHA:         f.SHA,
		IsDirectory: f.IsDirectory,
	}
	switch {
	// NUL is checked separately: it IS valid UTF-8, but Postgres jsonb refuses
	// it, and no text document this tool exists to read contains one.
	case !f.IsDirectory && (!utf8.ValidString(f.Content) || strings.ContainsRune(f.Content, 0)):
		v.Content = ""
		v.Note = fmt.Sprintf("binary file (%d bytes) — content withheld; this tool reads text documents", len(f.Content))
	case !f.IsDirectory && len(f.Content) > maxToolFileBytes:
		cut := maxToolFileBytes
		for cut > 0 && !utf8.RuneStart(f.Content[cut]) {
			cut-- // never split a rune mid-sequence
		}
		v.Content = f.Content[:cut]
		v.Note = fmt.Sprintf("truncated to the first %d of %d bytes", cut, len(f.Content))
	}
	if len(f.Entries) > 0 {
		v.Entries = make([]remoteGitEntryView, 0, len(f.Entries))
		for _, e := range f.Entries {
			v.Entries = append(v.Entries, remoteGitEntryView{Path: e.Path, Type: e.Type, SHA: e.SHA})
		}
	}
	return v
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
