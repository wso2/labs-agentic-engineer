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

package spec

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ArtifactStore wraps the GitHub-direct ArtifactService to add value beyond raw
// file reads: the typed `DesignFile` shape (design.json decode + root
// frontmatter assemble). It is a read + assemble surface — mutations happen via
// the Files API and are committed straight to `main`.
//
// Dependency resolution (the old static ExternalAPICatalog) is gone: every
// dependency's Status/Reason is computed at READ time (AssembleDesignFrom) by
// the single resolution authority, models.ComputeDependencyStatus, fed with
// freshly-fetched resolver-port lookups (resolveOrgServices for `org-service`,
// resolveExternalDependencies for everything else) — never from a shipped
// static table.
type ArtifactStore struct {
	artifactSvc ArtifactService
	// orgServices resolves `org-service` dependencies against the live org
	// endpoint catalog at design-read time (see resolveOrgServices). Nil until
	// the composition root wires a concrete provider via SetOrgServiceResolver
	// — until then, org-service dependencies keep whatever Status/Reason they
	// already carry (always empty: Status/Reason are read-time computed and
	// never persisted to design.json).
	orgServices OrgServiceResolver
	// externals resolves `external` dependencies against the live org
	// ResourceType catalog at design-read time (see resolveExternalDependencies).
	// Nil until the composition root wires a concrete provider via
	// SetExternalResourceResolver — until then, registryHit is false and status
	// derives from stored intent alone.
	externals ExternalResourceResolver
}

func NewArtifactStore(artifactSvc ArtifactService) *ArtifactStore {
	return &ArtifactStore{artifactSvc: artifactSvc}
}

// OrgServiceResolver answers whether an `org-service` dependency name is
// published namespace-visible in the org — the dynamic org endpoint catalog.
// Declared here (consumer side) so the artifacts package stays free of an
// OC-client / dependencies-feature dependency; the concrete provider (the
// dependencies feature's *endpoints.Catalog) is wired in by the composition
// root via SetOrgServiceResolver.
type OrgServiceResolver interface {
	IsNamespaceVisible(ctx context.Context, orgHandle, name string) (bool, error)
	// ExistsAnyVisibility reports whether a component named `name` publishes
	// ANY endpoint in the org catalog regardless of visibility — used to refine
	// an org-service dependency into `blocked`/`access-required` (exists,
	// project-only) vs `unresolved`/`not-found` (no such component).
	ExistsAnyVisibility(ctx context.Context, orgHandle, name string) (bool, error)
}

// SetOrgServiceResolver wires the dynamic org endpoint catalog used to mark
// `org-service` dependencies resolved/unresolved at design-read time. A nil
// store is a documented no-op (mirrors the other Set* setters).
func (s *ArtifactStore) SetOrgServiceResolver(r OrgServiceResolver) {
	if s == nil {
		return
	}
	s.orgServices = r
}

// ExternalResourceResolver answers whether an `external` dependency name is
// in the org's ResourceType-backed catalog (a Registered External, or a
// Project External that already has an authored RT). Declared here (consumer
// side) so spec stays free of an OC-client / dependencies-feature dependency;
// the concrete provider (*dependencies.ExternalResourceCatalog) is wired in
// by the composition root via SetExternalResourceResolver.
type ExternalResourceResolver interface {
	IsRegistered(ctx context.Context, orgID, name string) (bool, error)
}

// SetExternalResourceResolver wires the org external-resource catalog used to
// mark `external` dependencies resolved at design-read time (rule 2,
// registry reuse). A nil store is a documented no-op.
func (s *ArtifactStore) SetExternalResourceResolver(r ExternalResourceResolver) {
	if s == nil {
		return
	}
	s.externals = r
}

// ---- Design (multi-file directory) --------------------------------------

// DesignFile is the BFF's in-memory representation of the multi-file design
// artifact. It assembles from the repo layout under `specs/design/`:
//
//	design.cell                            # the root: cell DSL + optional sourceSpec frontmatter
//	domain-model.md                        # the ER diagram (entities become the API schemas)
//	flows/<slug>.md                        # one key flow per file (sequence diagram)
//	components/<name>/design.json          # the authored component model
//	components/<name>/openapi.yaml         # OpenAPI 3.0.3 (service components only)
//
// Only the structured facts are assembled (components + the root frontmatter);
// the diagram documents are read as plain bundle files by whoever needs them.
type DesignFile struct {
	Components []DesignComponent `json:"components"`
	SourceSpec string            `json:"sourceSpec,omitempty"`
}

// DesignRootFile is the canonical root design document: the cell. Its presence
// is the design-exists marker, and it carries the optional `sourceSpec` YAML
// frontmatter (the spec version the design derived from). Both cell parsers
// (parseCellFacts here, the TS grammar in @aep/cell-diagram-react) skip the
// frontmatter block.
const DesignRootFile = "design.cell"

// componentDirPrefix is the path prefix under specs/design/ for per-component
// directories.
const componentDirPrefix = "components/"

// ListDesignFiles returns the design file map at HEAD, under `specs/design/`.
// Keys are paths relative to that directory, using forward slashes (e.g.
// `design.cell`, `components/user-api/design.json`).
func (s *ArtifactStore) ListDesignFiles(ctx context.Context, orgID, projectID string) (map[string]string, error) {
	files, err := s.artifactSvc.ListDesignFiles(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if files == nil {
		files = map[string]string{}
	}
	return files, nil
}

// ReadDesign lists the design files at HEAD and assembles them into the flat
// `DesignFile` shape the rest of the BFF expects (task generation, OC
// provisioning, issue bodies, etc.). Returns (nil, nil) when no design root
// exists yet.
func (s *ArtifactStore) ReadDesign(ctx context.Context, orgID, projectID string) (*DesignFile, error) {
	files, err := s.artifactSvc.ListDesignFiles(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return s.AssembleDesignFrom(ctx, orgID, files)
}

// ReadDesignAt is ReadDesign pinned to an exact commit — the publish flow's
// save path reads the commit its apply just created instead of re-resolving
// HEAD (ref reads lag writes on GitHub's side).
func (s *ArtifactStore) ReadDesignAt(ctx context.Context, orgID, projectID, commitSHA string) (*DesignFile, error) {
	files, err := s.artifactSvc.GetDesignAtCommit(ctx, orgID, projectID, commitSHA)
	if err != nil {
		return nil, err
	}
	return s.AssembleDesignFrom(ctx, orgID, files)
}

// AssembleDesignFrom assembles the flat DesignFile from an already-listed
// design file map — the single-read path for callers that also need the raw
// map (no second HEAD walk). Returns (nil, nil) when no design root exists.
//
// It layers read-time dependency resolution on top of the raw assemble, via
// the single resolution authority models.ComputeDependencyStatus: each
// `org-service` dependency is resolved against the live org endpoint catalog
// (resolveOrgServices), and every other dependency — `external` against the
// precedence table (catalog hit, then stored intent fields),
// `component`/`platform-resource` trivially — is resolved by
// resolveExternalDependencies. orgID is the OC namespace the org's Workloads
// live in. Fail-open: a resolver error never fails the design read.
func (s *ArtifactStore) AssembleDesignFrom(ctx context.Context, orgID string, files map[string]string) (*DesignFile, error) {
	if len(files) == 0 || strings.TrimSpace(files[DesignRootFile]) == "" {
		return nil, nil
	}
	design, err := AssembleDesign(files)
	if err != nil {
		return nil, err
	}
	s.resolveOrgServices(ctx, orgID, design)
	s.resolveExternalDependencies(ctx, orgID, design)
	return design, nil
}

// resolveOrgServices marks each `org-service` dependency with a 4-state status
// at read time: `resolved` (namespace-visible), `blocked` +
// `access-required` (exists but project-only — consumer must request access),
// or `unresolved` + `not-found` (absent from the catalog) — all via
// models.ComputeDependencyStatus, the single resolution authority. orgID is
// the OC namespace (locally, the org handle).
//
// A no-op until the composition root wires a resolver via
// SetOrgServiceResolver — until then org-service dependencies keep the empty
// Status/Reason AssembleDesign left them with.
//
// Fail-open: a resolver error never fails the design read.
//   - An IsNamespaceVisible error leaves the dependency's Status/Reason
//     completely untouched.
//   - An ExistsAnyVisibility error leaves Status = unresolved (already set
//     before the refinement call) with an empty Reason — this specific
//     partial state has no ComputeDependencyStatus equivalent (a normal
//     org-service resolution never pairs unresolved with an empty reason), so
//     it is set directly rather than routed through the pure function.
func (s *ArtifactStore) resolveOrgServices(ctx context.Context, orgID string, d *DesignFile) {
	if s == nil || d == nil || s.orgServices == nil {
		return
	}
	for i := range d.Components {
		for j := range d.Components[i].Dependencies {
			dep := &d.Components[i].Dependencies[j]
			if dep.Kind != DependencyKindOrgService {
				continue
			}
			visible, err := s.orgServices.IsNamespaceVisible(ctx, orgID, dep.Name)
			if err != nil {
				slog.WarnContext(ctx, "org-service resolver: namespace-visible check failed",
					"org", orgID, "dependency", dep.Name, "error", err)
				continue
			}
			if visible {
				dep.Status, dep.Reason = ComputeDependencyStatus(*dep, false, OrgServiceHit{Visible: true})
				continue
			}
			// Not namespace-visible: refine into `blocked` (project-only —
			// requestable via access request) vs `unresolved` (absent — not in
			// the catalog at all).
			dep.Status = DependencyStatusUnresolved
			dep.Reason = ""
			exists, err := s.orgServices.ExistsAnyVisibility(ctx, orgID, dep.Name)
			if err != nil {
				slog.WarnContext(ctx, "org-service resolver: exists-any-visibility check failed",
					"org", orgID, "dependency", dep.Name, "error", err)
				continue
			}
			dep.Status, dep.Reason = ComputeDependencyStatus(*dep, false, OrgServiceHit{Exists: exists})
		}
	}
}

// resolveExternalDependencies computes every NON-org-service dependency's
// read-time Status/Reason via ComputeDependencyStatus: `external` against
// the precedence table (rule 2 is a live catalog lookup via
// ExternalResourceResolver; a miss or unwired resolver falls through to
// stored intent); `component`/`platform-resource` trivially (no lookup).
// `org-service` dependencies are left untouched — resolveOrgServices owns
// them (its per-call fail-open error handling doesn't fit this simpler loop).
//
// Fail-open: a resolver error never fails the design read — that dependency
// is resolved from stored intent (registryHit=false), matching org-service.
func (s *ArtifactStore) resolveExternalDependencies(ctx context.Context, orgID string, d *DesignFile) {
	if s == nil || d == nil {
		return
	}
	hits := map[string]bool{}
	for i := range d.Components {
		for j := range d.Components[i].Dependencies {
			dep := &d.Components[i].Dependencies[j]
			if dep.Kind == DependencyKindOrgService {
				continue
			}
			registryHit := false
			if dep.Kind == DependencyKindExternal && s.externals != nil {
				if cached, ok := hits[dep.Name]; ok {
					registryHit = cached
				} else {
					hit, err := s.externals.IsRegistered(ctx, orgID, dep.Name)
					if err != nil {
						slog.WarnContext(ctx, "external-resource resolver: catalog check failed",
							"org", orgID, "dependency", dep.Name, "error", err)
					} else {
						registryHit = hit
					}
					hits[dep.Name] = registryHit
				}
			}
			dep.Status, dep.Reason = ComputeDependencyStatus(*dep, registryHit, OrgServiceHit{})
		}
	}
}

// ---- Helpers ------------------------------------------------------------

// IsNotFound is sugar for callers that want to distinguish "no artifact yet"
// from a real error.
func IsNotFound(err error) bool { return errors.Is(err, ErrArtifactNotFound) }

// rootFrontmatter is the YAML frontmatter we accept on the root `design.cell`.
type rootFrontmatter struct {
	SourceSpec string `yaml:"sourceSpec,omitempty"`
}

// SplitFrontmatter separates the leading YAML frontmatter block (delimited by
// `---` lines) from the body. If the file has no frontmatter, returns
// ("", content, nil).
func SplitFrontmatter(content string) (fm string, body string, err error) {
	trimmed := strings.TrimLeft(content, " \t\r\n")
	// Strip an optional UTF-8 BOM (U+FEFF) before frontmatter detection.
	trimmed = strings.TrimPrefix(trimmed, "\ufeff")
	if !strings.HasPrefix(trimmed, "---") {
		return "", content, nil
	}
	rest := trimmed[3:]
	rest = strings.TrimLeft(rest, " \t")
	if !strings.HasPrefix(rest, "\n") && !strings.HasPrefix(rest, "\r\n") {
		return "", content, nil
	}
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", content, fmt.Errorf("frontmatter: unterminated --- block")
	}
	fm = strings.TrimSpace(rest[:end])
	after := rest[end+len("\n---"):]
	after = strings.TrimPrefix(after, "\r")
	after = strings.TrimPrefix(after, "\n")
	return fm, after, nil
}

// AssembleDesign reconstructs a flat DesignFile from the multi-file map (keys
// relative to specs/design/, forward slashes). Returns an error if the root
// `design.cell` is missing (callers handle that as "no design yet").
func AssembleDesign(files map[string]string) (*DesignFile, error) {
	root, ok := files[DesignRootFile]
	if !ok {
		return nil, fmt.Errorf("design.cell missing")
	}

	fm, _, err := SplitFrontmatter(root)
	if err != nil {
		return nil, fmt.Errorf("parse design.cell frontmatter: %w", err)
	}
	var rfm rootFrontmatter
	if fm != "" {
		if err := yaml.Unmarshal([]byte(fm), &rfm); err != nil {
			return nil, fmt.Errorf("decode design.cell frontmatter: %w", err)
		}
	}
	out := &DesignFile{
		SourceSpec: rfm.SourceSpec,
	}

	componentNames := ComponentNamesIn(files)
	out.Components = make([]DesignComponent, 0, len(componentNames))
	for _, name := range componentNames {
		// design.json is the authored component model — the agent's write gate and
		// the save-time gate both validate it (design_json.go / @aep/agent-stream).
		// The legacy per-component design.md frontmatter path was retired with the
		// dependency-management migration (design.md was removed upstream).
		raw, ok := files[componentDirPrefix+name+"/design.json"]
		if !ok {
			continue
		}
		comp, err := parseComponentDesignJSON(name, raw)
		if err != nil {
			return nil, fmt.Errorf("assemble component %q: %w", name, err)
		}
		// OpenAPISpec is not a design.json key — fill it from the sibling openapi.yaml.
		openapi := files[componentDirPrefix+name+"/openapi.yaml"]
		if openapi == "" {
			openapi = files[componentDirPrefix+name+"/openapi.yml"]
		}
		comp.OpenAPISpec = openapi
		out.Components = append(out.Components, comp)
	}
	return out, nil
}

// SplitDesign marshals a DesignFile's COMPONENTS back into the multi-file map
// (keys relative to specs/design/, forward slashes):
//
//	components/<name>/design.json    # the authored component model (design_json.go codec)
//	components/<name>/openapi.yaml    # the sibling spec (service components), when present
//
// The root `design.cell` is never emitted here: the cell (and its sourceSpec
// frontmatter) is authored, not rendered — SplitDesign is a per-component
// render surface (derive.go, design_service.go), not AssembleDesign's inverse.
func SplitDesign(d *DesignFile) (map[string]string, error) {
	if d == nil {
		return nil, fmt.Errorf("nil design")
	}
	files := make(map[string]string, 2*len(d.Components))

	for _, comp := range d.Components {
		body, err := marshalComponentDesignJSON(comp.Name, comp)
		if err != nil {
			return nil, fmt.Errorf("marshal component %q: %w", comp.Name, err)
		}
		files[componentDirPrefix+comp.Name+"/design.json"] = string(body)
		if strings.TrimSpace(comp.OpenAPISpec) != "" {
			files[componentDirPrefix+comp.Name+"/openapi.yaml"] = comp.OpenAPISpec
		}
	}
	return files, nil
}

// ComponentNamesIn walks the file map and returns the unique component
// directory names found under `components/`, sorted alphabetically.
func ComponentNamesIn(files map[string]string) []string {
	seen := make(map[string]struct{})
	for p := range files {
		if !strings.HasPrefix(p, componentDirPrefix) {
			continue
		}
		rest := p[len(componentDirPrefix):]
		slash := strings.IndexByte(rest, '/')
		if slash <= 0 {
			continue
		}
		seen[rest[:slash]] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
