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

// Consumer-side ports for the MCP discovery surface. This child package depends
// on collaborators through narrow interfaces wired concretely at the
// composition root (app.Build, D5). Each is satisfied structurally by an
// existing type in the parent `dependencies` package:
//
//   - ExternalResourceReader ← *dependencies.ExternalResourceCatalog (Task 3 —
//     org-namespaced OpenChoreo ResourceTypes, not the external_resources table)
//   - OrgEndpointLister       ← *dependencies.Catalog (C1)
//   - ResourceTypeLister      ← *dependencies.ResourceTypeCatalog (C3)

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/dependencies"
)

// ExternalResourceReader is the read slice of the org external-resource catalog
// the MCP surface exposes (list every registered external resource, get one by
// name). Sourced from the org's OpenChoreo ResourceTypes via
// openchoreo.ExternalDefinitionFromRT. Ensure authors the RT at
// register, so a zero-consumer Registered row is already listable here (MCP
// List is RT-backed; the agent JSON view carries consumptionInstructions and
// resourceDocs pointers). A design-only `external` dependency that never went
// through register/Ensure still has no RT and is not discoverable. Get returns
// (nil, nil) when the name is not registered.
type ExternalResourceReader interface {
	List(ctx context.Context, orgID string) ([]openchoreo.ExternalResourceDefinition, error)
	Get(ctx context.Context, orgID, name string) (*openchoreo.ExternalResourceDefinition, error)
}

// OrgEndpointLister is the read slice of the org endpoint catalog — the
// published-service targets an `org-service` dependency can point at (List),
// plus each one resolved with the provider's repo coordinates and a
// discovered OpenAPI contract (ListResolved) for the A3 MCP tool
// (list_org_component_endpoints).
type OrgEndpointLister interface {
	List(ctx context.Context, orgHandle string) ([]openchoreo.WorkloadEndpointInfo, error)
	ListResolved(ctx context.Context, orgHandle string) ([]dependencies.OrgComponentEndpoint, error)
}

// ResourceTypeLister is the read slice of the platform resource-type catalog —
// the installed cluster ResourceTypes a platform-resource dependency references.
type ResourceTypeLister interface {
	List(ctx context.Context) ([]dependencies.PlatformResourceType, error)
}

// RoleCatalogLister reads the GROUPS that already exist on the environment
// directory of the org — the catalog the design agent consults before naming a
// group in `security.json`'s `roles[].assignTo`. A group is a set of people and
// is owned by the organisation, which is precisely what makes reuse meaningful,
// and it is why the rows carry who is in it and who already depends on it and
// nothing about what it can do.
//
// It takes the ORG HANDLE because the catalog is per (org, environment): every
// environment has its own identity provider, so "which groups exist" has no
// cluster-wide answer. The handle is the verified `ocOrgId` claim and never a
// tool argument — a model naming the org would be a model choosing whose
// directory it sees.
//
// Satisfied by *identity.CatalogService. Read-only with no write counterpart
// anywhere on this surface — groups are created at BUILD time, deterministically,
// never by a model.
//
// The type still says "Role" because it predates the split between a directory
// group and a project role; the rename rides with the `list_roles` alias removal
// in scopes phase 2, when a project role becomes a separate directory object.
type RoleCatalogLister interface {
	ListRoleCatalog(ctx context.Context, orgHandle string) ([]RoleCatalogEntry, error)
}

// RoleCatalogEntry is one row of the group catalog as the tool renders it. It is
// mcpdiscovery's own view type — the projection every other tool here goes
// through — so a field added to the identity domain cannot leak into an LLM
// prompt by accident.
type RoleCatalogEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// PlatformCreated is true when the platform created this group and may
	// therefore give it test users. False means somebody else made the group;
	// the platform will leave it alone.
	PlatformCreated bool `json:"platformCreated"`
	// MemberCount is how many users are in the group today.
	MemberCount int `json:"memberCount"`
	// Projects is how many projects already bind a role to this group — the
	// signal that reusing the name is a decision about people who already hold
	// roles elsewhere. 0 until scopes phase 2 records the bindings.
	Projects int `json:"projects"`
}

// PlatformResourceConsumerLister derives, per installed platform ResourceType,
// the components across the calling org's projects that declare a
// platform-resource dependency on it (the "used by" overlay on the resource-type
// catalog). Keyed by lowercased ResourceType name. Satisfied structurally by
// *provisioning.Service (PlatformResourceConsumersByType) — kept as a narrow
// port so this cluster-global catalog package never imports the org-scoped
// provisioning package concretely. The overlay is additive and best-effort: a
// nil lister or an error degrades to an empty "used by", never failing the list.
type PlatformResourceConsumerLister interface {
	PlatformResourceConsumersByType(ctx context.Context, orgID string) (map[string][]dependencies.ExternalResourceConsumer, error)
}

// RemoteGitReader reads an org's OWN GitHub repos over the REST API (Contents +
// Code Search, no clone) for endpoint spec discovery — the two MCP tools an
// agent uses to read a provider's OpenAPI file straight from its repo. Both
// methods take ocOrgID (the verified MCP claim, never a tool parameter) and
// MUST refuse (ErrOwnerNotInOrg) any `owner` that is not the org credential's
// GitHub account, so a caller in one org can never read another org's repos.
// Satisfied by *RemoteGitClient (remote_git.go).
type RemoteGitReader interface {
	GetFileContents(ctx context.Context, ocOrgID, owner, repo, path, ref string) (*RemoteGitFile, error)
	SearchCode(ctx context.Context, ocOrgID, owner, repo, query string) ([]RemoteGitSearchHit, error)
}

// SpecValidator parses an OpenAPI 3.x document and returns its operation
// count (method entries under paths), or an error when the document does not
// parse or is not a valid OpenAPI 3.x doc. Backs validate_openapi_spec and the
// validate half of fetch_openapi_spec. Satisfied by artifacts.ValidateOpenAPI.
type SpecValidator func(raw []byte) (operations int, err error)

// SpecNormalizer returns the canonical-form encoding of an already-valid
// OpenAPI document. Backs validate_openapi_spec and fetch_openapi_spec.
// Satisfied by artifacts.NormalizeOpenAPIYAML.
type SpecNormalizer func(content string) (normalized string, err error)

// SpecFetcher fetches an OpenAPI spec from a user-supplied https URL. Backs
// fetch_openapi_spec. Satisfied by artifacts.FetchSpecFromURL — PLATFORM-
// TOUCHING (SSRF-hardened: https-only, public-IP-only, redirect-guarded, size-
// and time-capped). This port MUST always be wired to that function as-is; the
// MCP tool layer only adds a TIGHTER context-safety cap on top, never a looser
// SSRF posture.
type SpecFetcher func(ctx context.Context, url string) ([]byte, error)

// SpecSlicer cuts, from a whole OpenAPI document, the operations named by the
// selectors (operationId, "METHOD /path", or "/path") plus every schema they
// reference, as a standalone canonical document. Backs slice_openapi_spec.
// Satisfied by spec.SliceOpenAPI — the deterministic tool that lets a design
// commit the slice of a provider's document it actually uses, whatever the
// size of the source.
type SpecSlicer func(raw []byte, selectors []string) ([]byte, error)
