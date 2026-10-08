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

// Package mcp is the MCP socket's JSON-RPC server: the in-pod design
// agent's only tool surface. It answers tools/list from the pinned local
// descriptors, refuses tools/call of any name outside AllowedTools, runs the
// two remote-git tools in the pod with the gitpat and forwards the other ten
// to aep-api's /internal/v1/mcp with the org's ae-studio client token.
package mcp

import "slices"

// AllowedTools is the pinned list of the tools the in-pod agent may call
// Whatever aep-api serves, tools/list answers exactly these and
// tools/call refuses any other name. Changing it is a reviewed decision;
// TestAllowedTools_Pinned pins it.
var AllowedTools = []string{
	"list_external_resources",
	"get_external_resource_schema",
	"list_org_endpoints",
	"list_org_component_endpoints",
	"list_platform_resource_types",
	"list_groups",
	"list_guardrail_policies",
	toolGetFileContents,
	toolSearchCode,
	"validate_openapi_spec",
	"fetch_openapi_spec",
	"slice_openapi_spec",
}

// The two tools served in the pod (remote_git.go); the other ten go to
// aep-api.
const (
	toolGetFileContents = "get_remote_git_file_contents"
	toolSearchCode      = "search_remote_git_code"
)

// allowed reports whether name is on the pinned list.
func allowed(name string) bool { return slices.Contains(AllowedTools, name) }

// Tool is one tools/list descriptor.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// The descriptors below are copied from aep-api's
// internal/dependencies/mcpdiscovery/mcp_tools.go (mcpTools), which keeps
// serving them to the legacy runner. Keep the two in step.

// listGroupsDescription is the description text behind `list_groups` — the ONE
// place a model is told what the directory-group catalog is and which of its
// fields mean what.
const listGroupsDescription = "Lists the directory groups in this organization's environment directory. " +
	"Use it before writing security.json roles[].assignTo: reuse an existing group name when the " +
	"people who should hold the role already form a group; otherwise declare a new name in groups[] " +
	"(it is created at build). memberCount is the number of users in the group today; projects is how " +
	"many projects already bind a role to it; platformCreated says whether this platform created it."

// Tools returns the descriptors tools/list answers, one per AllowedTools
// entry, in that order.
func Tools() []Tool {
	return []Tool{
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
