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
	"context"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// OrgComponentEndpoint is a catalog row enriched with the provider's repo
// coordinates and a discovered OpenAPI contract — the material an architect (or
// a coding agent, via the A3 MCP tool) needs to consume an `org-service` by its
// real spec instead of guessing. It is the resolved projection of a single
// WorkloadEndpointInfo.
type OrgComponentEndpoint struct {
	Project   string // owner project NAME (== the app-factory project id; see resolveRepoCoords)
	Component string // owner component NAME (the org-service key)
	Endpoint  string // endpoint name (spec.endpoints key)
	Type      string // HTTP | gRPC | GraphQL | Websocket | TCP | UDP
	Port      int32
	BasePath  string
	// NamespaceVisible mirrors the raw row: whether the provider published this
	// endpoint for cross-project (org-service) consumption.
	NamespaceVisible bool
	// Owner/Repo/Subdir/Branch are the provider's repo coordinates, "" when
	// unknown (a BYO-image provider with no git_repositories row and no
	// off-platform repo coords surfaced by the OC client — see resolve.go's
	// package note on the Workflow.Parameters gap).
	Owner, Repo, Subdir, Branch string
	Spec                        EndpointSpec
}

// EndpointSpec tags how the endpoint's OpenAPI contract is available.
//
//	inline — InlineContent holds the spec document verbatim (from the deployed
//	         Workload CR schema, or a provider's committed design-bundle
//	         openapi.yaml). Path names the design-bundle source for provenance.
//	repo   — no inline spec, but the provider's repo coords are known; Path is
//	         the subdir hint where an agent should look for the contract.
//	none   — neither a spec nor repo coords are resolvable (BYO-image provider).
//
// `local` is deliberately NOT produced here — it is a coding-agent-side
// classification for same-project deps (Part C), which never appear in this
// org-wide list.
type EndpointSpec struct {
	Availability  string // inline | repo | none
	InlineContent string // set iff Availability == inline
	Path          string // provenance (inline from design bundle) or subdir hint (repo)
}

const (
	availabilityInline = "inline"
	availabilityRepo   = "repo"
	availabilityNone   = "none"
)

// RepoLocator resolves an app-factory provider's provisioned git repo row by
// its (org, project-id) tuple. Satisfied structurally by
// repositories.RepoRepository. The provider project NAME carried on a workload
// endpoint (spec.owner.projectName) IS the app-factory project id: project
// creation seeds git_repositories.project_id from the OC project name
// (project_service → repoSvc.CreateRepo(orgName, project.Name, …)), so the
// endpoint's Project field keys these lookups directly — no name→id mapping
// table is needed.
type RepoLocator interface {
	GetByOrgAndProjectID(ctx context.Context, ocOrgID, projectID string) (*sourcecontrol.GitRepository, error)
}

// DesignBundleReader reads a provider project's committed design bundle (assembled
// per-component design.json + sibling openapi.yaml). Satisfied structurally by
// *spec.ArtifactStore. Keyed by the same (org, project-id) tuple as
// RepoLocator (see its note).
type DesignBundleReader interface {
	ReadDesign(ctx context.Context, orgID, projectID string) (*spec.DesignFile, error)
}

// CatalogOption configures the optional resolver collaborators on a Catalog.
// Absent options degrade gracefully: without a RepoLocator/DesignBundleReader the
// resolver simply can't reach the repo/design-bundle sources and availability
// falls back accordingly (inline-from-CR still works; everything else → none).
type CatalogOption func(*Catalog)

// WithRepoLocator wires the git-repository lookup used for step-3 repo coords
// (and provenance on the inline/repo paths).
func WithRepoLocator(r RepoLocator) CatalogOption {
	return func(c *Catalog) { c.repos = r }
}

// WithDesignReader wires the design-bundle reader used for step-2 inline specs
// (a provider's committed openapi.yaml) and the component subdir hint.
func WithDesignReader(d DesignBundleReader) CatalogOption {
	return func(c *Catalog) { c.design = d }
}

// ListResolved returns every provider-side endpoint enriched with repo coords
// and a discovered spec (see Resolve). It propagates the underlying
// ListWorkloadEndpoints error; per-endpoint resolution is fail-open (a repo /
// design lookup error degrades that one row's availability, never the list).
// Returns nil when the catalog is not wired (nil receiver or nil client).
//
// A project publishing K endpoints would otherwise trigger K redundant
// ArtifactStore.ReadDesign calls (a remote git-bundle read) in a single pass,
// so this method memoizes the design-bundle read per provider project in a
// map scoped to THIS call only — it is never a long-lived/global cache (no
// staleness risk) and does not affect the single-endpoint Resolve below.
func (c *Catalog) ListResolved(ctx context.Context, orgHandle string) ([]OrgComponentEndpoint, error) {
	if c == nil || c.rc == nil {
		return nil, nil
	}
	infos, err := c.rc.ListWorkloadEndpoints(ctx, orgHandle)
	if err != nil {
		return nil, err
	}
	designCache := make(map[string]*spec.DesignFile)
	out := make([]OrgComponentEndpoint, 0, len(infos))
	for i := range infos {
		out = append(out, c.resolve(ctx, orgHandle, infos[i], designCache))
	}
	return out, nil
}

// Resolve enriches one raw endpoint into an OrgComponentEndpoint: it resolves
// the provider's repo coordinates and computes the spec availability in the
// fixed order
//
//  1. the deployed Workload CR already carries an inline schema  → inline (CR);
//  2. the app-factory provider committed a design-bundle openapi.yaml → inline
//     (read server-side; Path = the design-bundle path, for provenance);
//  3. repo coords resolved (git_repositories) → repo (Path = subdir hint);
//  4. otherwise → none.
//
// All collaborator lookups are fail-open: an error leaves that source
// unresolved (logged) rather than failing the whole resolution.
//
// Resolve always reads the design bundle directly (no memoization) — the
// per-project cache is a ListResolved-pass-only optimization (see
// designCache in ListResolved); this single-endpoint entry point's behavior
// is unchanged.
func (c *Catalog) Resolve(ctx context.Context, orgHandle string, e openchoreo.WorkloadEndpointInfo) OrgComponentEndpoint {
	return c.resolve(ctx, orgHandle, e, nil)
}

// resolve is the shared implementation behind Resolve and ListResolved.
// designCache, when non-nil, memoizes the provider project's design-bundle
// read for the duration of one ListResolved pass; nil (the public Resolve's
// case) means "always read".
func (c *Catalog) resolve(ctx context.Context, orgHandle string, e openchoreo.WorkloadEndpointInfo, designCache map[string]*spec.DesignFile) OrgComponentEndpoint {
	oce := OrgComponentEndpoint{
		Project:          e.Project,
		Component:        e.Component,
		Endpoint:         e.Name,
		Type:             e.Type,
		Port:             e.Port,
		BasePath:         e.BasePath,
		NamespaceVisible: e.NamespaceVisible(),
	}

	// Read the provider's design bundle once — it feeds both the step-2 inline
	// spec and the component subdir hint used on the repo path.
	comp := c.providerComponent(ctx, orgHandle, e.Project, e.Component, designCache)

	// Resolve repo coords (step-3 source + provenance on the inline paths).
	c.resolveRepoCoords(ctx, orgHandle, e.Project, comp, &oce)

	// Compute spec availability in the fixed resolution order.
	switch {
	case e.SchemaContent != "":
		oce.Spec = EndpointSpec{Availability: availabilityInline, InlineContent: e.SchemaContent}
	case comp != nil && strings.TrimSpace(comp.OpenAPISpec) != "":
		oce.Spec = EndpointSpec{
			Availability:  availabilityInline,
			InlineContent: comp.OpenAPISpec,
			// comp is matched case-insensitively (see providerComponent), so use
			// its actual committed Name — not e.Component — for the folder
			// segment, matching the real design-bundle path casing.
			Path: designOpenAPIPath(comp.Name),
		}
	case oce.Repo != "":
		oce.Spec = EndpointSpec{Availability: availabilityRepo, Path: oce.Subdir}
	default:
		oce.Spec = EndpointSpec{Availability: availabilityNone}
	}
	return oce
}

// providerComponent reads the provider project's design bundle and returns the
// component named `component` (case-insensitive, mirroring
// spec.ResolveDesignComponent), or nil when the reader is unwired, the
// project has no design bundle, or the component is absent. Fail-open: a read
// error is logged and returns nil. designCache is forwarded to readDesign
// (see its doc) — nil for the public single-endpoint Resolve.
//
// The raw component name is tried first. An app-factory-created component's
// OC name (spec.owner.componentName, carried on the endpoint's Component
// field) is project-prefixed — e.g. "myproj-svc" — but the design bundle
// stores it under its unprefixed authored name ("svc"), so a raw-name miss on
// a "<project>-" prefixed component retries once with the prefix stripped.
// Hand-applied / non-app-factory components (unprefixed to begin with) are
// unaffected: the raw-name lookup already finds them.
func (c *Catalog) providerComponent(ctx context.Context, orgHandle, project, component string, designCache map[string]*spec.DesignFile) *spec.DesignComponent {
	if c.design == nil {
		return nil
	}
	design, err := c.readDesign(ctx, orgHandle, project, designCache)
	if err != nil {
		slog.WarnContext(ctx, "endpoint resolver: design read failed",
			"org", orgHandle, "project", project, "component", component, "error", err)
		return nil
	}
	if design == nil {
		return nil
	}
	if comp := findDesignComponent(design, component); comp != nil {
		return comp
	}
	if prefix := project + "-"; strings.HasPrefix(component, prefix) {
		return findDesignComponent(design, strings.TrimPrefix(component, prefix))
	}
	return nil
}

// findDesignComponent returns the design bundle component named `name`
// (case-insensitive), or nil when absent.
func findDesignComponent(design *spec.DesignFile, name string) *spec.DesignComponent {
	for i := range design.Components {
		if strings.EqualFold(design.Components[i].Name, name) {
			return &design.Components[i]
		}
	}
	return nil
}

// readDesign reads a provider project's design bundle, memoizing the result
// in designCache when it is non-nil (the ListResolved-pass-scoped cache — see
// ListResolved's doc). A nil designCache (the public Resolve's case) always
// reads through. Only successful reads (including a genuinely absent design
// bundle, i.e. a nil *DesignFile) are cached; a read error is neither cached
// nor swallowed here — it is returned for the caller (providerComponent) to
// log and fail open on, so a transient error doesn't poison the pass.
func (c *Catalog) readDesign(ctx context.Context, orgHandle, project string, designCache map[string]*spec.DesignFile) (*spec.DesignFile, error) {
	if designCache != nil {
		if design, ok := designCache[project]; ok {
			return design, nil
		}
	}
	design, err := c.design.ReadDesign(ctx, orgHandle, project)
	if err != nil {
		return nil, err
	}
	if designCache != nil {
		designCache[project] = design
	}
	return design, nil
}

// resolveRepoCoords fills oce.Owner/Repo/Subdir/Branch from the app-factory
// provider's git_repositories row, using the design-bundle component's appPath
// as the subdir hint. Fail-open: no row (or a lookup error) leaves the coords
// empty, and availability then falls to repo-if-coords-known-else-none.
//
// The off-platform-with-build fallback (repo coords from the OC Component's
// Workflow.Parameters) is intentionally NOT wired: the aep-api OC client's
// GetComponent projects the CR onto gen.Component, which drops the workflow
// spec, so those coords are not reachable without extending the client. Left
// best-effort per the Task A2 brief; app-factory providers resolve fully via
// the git_repositories path above.
func (c *Catalog) resolveRepoCoords(ctx context.Context, orgHandle, project string, comp *spec.DesignComponent, oce *OrgComponentEndpoint) {
	if c.repos == nil {
		return
	}
	gr, err := c.repos.GetByOrgAndProjectID(ctx, orgHandle, project)
	if err != nil {
		slog.WarnContext(ctx, "endpoint resolver: repo lookup failed",
			"org", orgHandle, "project", project, "error", err)
		return
	}
	if gr == nil {
		return
	}
	owner, repo, perr := sourcecontrol.ParseOwnerRepo(gr.RepoURL)
	if perr != nil {
		return
	}
	oce.Owner, oce.Repo, oce.Branch = owner, repo, gr.DefaultBranch
	if comp != nil {
		oce.Subdir = comp.AppPath
	}
}

// designOpenAPIPath is the repo-relative path of a component's committed
// OpenAPI contract in the design bundle — the provenance recorded on a
// step-2 inline spec.
func designOpenAPIPath(component string) string {
	return "specs/design/components/" + component + "/openapi.yaml"
}
