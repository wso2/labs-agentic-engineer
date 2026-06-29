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

package artifacts

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/wso2/asdlc/asdlc-service/models"
)

// OrgServiceResolver answers whether an `org-service` dependency name is
// published namespace-visible in the org — the dynamic org endpoint catalog
// (P3). Satisfied by connections.OrgEndpointCatalog. Injected so the artifacts
// package stays free of an OC-client dependency.
type OrgServiceResolver interface {
	IsNamespaceVisible(ctx context.Context, orgHandle, name string) (bool, error)
	// ExistsAnyVisibility reports whether a component named `name` publishes ANY
	// endpoint in the org catalog regardless of visibility — used to refine an
	// unresolved org-service dep into `unpublished` (exists, project-only) vs
	// `not-found` (no such component). P3.5.
	ExistsAnyVisibility(ctx context.Context, orgHandle, name string) (bool, error)
}

// ArtifactStore wraps the in-process artifact service to add value beyond
// pure file I/O: external-API catalog resolution and the typed `DesignFile`
// shape (YAML split/assemble).
type ArtifactStore struct {
	artifactSvc  ArtifactService
	externalAPIs *ExternalAPICatalog
	orgServices  OrgServiceResolver // dynamic org-service catalog (P3); nil ⇒ no live resolution
}

func NewArtifactStore(artifactSvc ArtifactService) *ArtifactStore {
	store := &ArtifactStore{artifactSvc: artifactSvc, externalAPIs: DefaultExternalAPICatalog()}
	registerSplitDesignCatalog(store.externalAPIs)
	return store
}

// SetOrgServiceResolver wires the dynamic org endpoint catalog used to mark
// `org-service` dependencies resolved/unresolved at design-read time (P3).
func (s *ArtifactStore) SetOrgServiceResolver(r OrgServiceResolver) {
	if s == nil {
		return
	}
	s.orgServices = r
}

// SetExternalAPICatalog overrides the catalog the store uses to resolve
// architect-declared dependent-API names into concrete URLs. Optional —
// without it, NewArtifactStore wires the shipped default catalog.
func (s *ArtifactStore) SetExternalAPICatalog(c *ExternalAPICatalog) {
	if s == nil {
		return
	}
	s.externalAPIs = c
	registerSplitDesignCatalog(c)
}

// splitDesignCatalogRef is a process-wide pointer the free-function
// SplitDesign reads to strip catalog-resolved URLs on save. Set by
// NewArtifactStore so production paths get the catalog automatically;
// nil in tests / standalone SplitDesign callers.
var splitDesignCatalogRef *ExternalAPICatalog

func registerSplitDesignCatalog(c *ExternalAPICatalog) { splitDesignCatalogRef = c }
func splitDesignCatalog() *ExternalAPICatalog          { return splitDesignCatalogRef }

// ---- Requirements (multi-file Markdown directory) -----------------------

// RequirementsMainFile is the canonical primary requirements document. It
// cannot be deleted/renamed via the API — controllers should reject
// destructive operations on it.
const RequirementsMainFile = "requirements.md"

// ListRequirements returns the working-tree file map under
// `specs/requirements/`. A first-time project with no requirements yet
// returns an empty map (not an error).
func (s *ArtifactStore) ListRequirements(ctx context.Context, orgID, projectID string) (map[string]string, error) {
	files, err := s.artifactSvc.ListRequirementFiles(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if files == nil {
		files = map[string]string{}
	}
	return files, nil
}

// ReadRequirementFile reads a single requirement file by basename.
func (s *ArtifactStore) ReadRequirementFile(ctx context.Context, orgID, projectID, name string) (string, error) {
	res, err := s.artifactSvc.GetFile(ctx, orgID, projectID, path.Join(RequirementsDir, name))
	if err != nil {
		return "", err
	}
	return res.Content, nil
}

// WriteRequirementFile creates or overwrites a single requirement file.
// The optional ifMatch sha (returned by the previous PUT) gives the
// streaming caller optimistic concurrency control.
func (s *ArtifactStore) WriteRequirementFile(ctx context.Context, orgID, projectID, name, content string) (sha string, err error) {
	res, err := s.artifactSvc.PutFile(ctx, orgID, projectID, path.Join(RequirementsDir, name), content, "")
	if err != nil {
		return "", fmt.Errorf("write requirement file %q: %w", name, err)
	}
	return res.SHA, nil
}

// DeleteRequirementFile removes a requirement file from the working tree.
// The change is persisted on the next SaveRequirements call.
func (s *ArtifactStore) DeleteRequirementFile(ctx context.Context, orgID, projectID, name string) error {
	if name == RequirementsMainFile {
		return fmt.Errorf("cannot delete %s", RequirementsMainFile)
	}
	if err := s.artifactSvc.DeleteRequirementFile(ctx, orgID, projectID, name); err != nil {
		return fmt.Errorf("delete requirement file %q: %w", name, err)
	}
	return nil
}

// ---- Design (multi-file directory) --------------------------------------

// DesignFile is the BFF's in-memory representation of the multi-file design
// artifact. It assembles from / splits to the working-tree layout under
// `specs/design/`:
//
//	design.md                              # overview prose + sourceSpec frontmatter
//	components/<name>/design.md            # frontmatter (type, language, dependsOn,
//	                                       # buildpack, appPath, entrypoint) + body
//	                                       # (componentAgentInstructions)
//	components/<name>/openapi.yaml         # OpenAPI 3.0.3 (service components only)
type DesignFile struct {
	Overview      string                   `json:"overview"`
	Components    []models.DesignComponent `json:"components"`
	SourceSpec    string                   `json:"sourceSpec,omitempty"`
	SkillsApplied []string                 `json:"skillsApplied,omitempty"`
}

// DesignRootFile is the canonical root design document. It cannot be deleted
// via the API.
const DesignRootFile = "design.md"

// componentDirPrefix is the path prefix under specs/design/ for per-component
// directories.
const componentDirPrefix = "components/"

// ListDesignFiles returns the working-tree file map under `specs/design/`.
// Keys are paths relative to that directory, using forward slashes (e.g.
// `design.md`, `components/user-api/design.md`).
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

// ReadDesignFile reads a single design file by sub-path.
func (s *ArtifactStore) ReadDesignFile(ctx context.Context, orgID, projectID, subPath string) (string, error) {
	res, err := s.artifactSvc.GetFile(ctx, orgID, projectID, path.Join(DesignDir, subPath))
	if err != nil {
		return "", err
	}
	return res.Content, nil
}

// WriteDesignFile creates or overwrites a single design file. The path is
// relative to `specs/design/` (forward slashes; nested components allowed).
func (s *ArtifactStore) WriteDesignFile(ctx context.Context, orgID, projectID, subPath, content string) (sha string, err error) {
	res, err := s.artifactSvc.PutFile(ctx, orgID, projectID, path.Join(DesignDir, subPath), content, "")
	if err != nil {
		return "", fmt.Errorf("write design file %q: %w", subPath, err)
	}
	return res.SHA, nil
}

// DeleteDesignFile removes a single design file. Refuses to delete the root
// `design.md`.
func (s *ArtifactStore) DeleteDesignFile(ctx context.Context, orgID, projectID, subPath string) error {
	if subPath == DesignRootFile {
		return fmt.Errorf("cannot delete %s", DesignRootFile)
	}
	if err := s.artifactSvc.DeleteDesignFile(ctx, orgID, projectID, subPath); err != nil {
		return fmt.Errorf("delete design file %q: %w", subPath, err)
	}
	return nil
}

// DeleteDesignDirectory removes a directory under `specs/design/` and all
// its contents (e.g. `components/user-api` to remove a component's whole
// subtree).
func (s *ArtifactStore) DeleteDesignDirectory(ctx context.Context, orgID, projectID, subPath string) error {
	if err := s.artifactSvc.DeleteDesignDirectory(ctx, orgID, projectID, subPath); err != nil {
		return fmt.Errorf("delete design directory %q: %w", subPath, err)
	}
	return nil
}

// ReadDesign lists the working-tree design files and assembles them into the
// flat `DesignFile` shape that the rest of the BFF expects (task generation,
// OC provisioning, issue bodies, etc.). Returns
// (nil, ErrArtifactNotFound) when no design root exists yet.
func (s *ArtifactStore) ReadDesign(ctx context.Context, orgID, projectID string) (*DesignFile, error) {
	files, err := s.artifactSvc.ListDesignFiles(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 || strings.TrimSpace(files[DesignRootFile]) == "" {
		return nil, nil
	}
	design, err := AssembleDesign(files)
	if err != nil {
		return nil, err
	}
	// Mark org-service dependencies resolved/unresolved against the live org
	// endpoint catalog so the architecture view renders status without user
	// action. The concrete URL is injected at wiring time (P3, cascade).
	s.resolveOrgServices(ctx, orgID, design)
	return design, nil
}

// resolveOrgServices marks each `org-service` dependency `resolved` when its
// target is published namespace-visible in the org endpoint catalog, and
// `unresolved` otherwise (the provider hasn't published it cross-project yet).
// Falls back to the static external-API catalog when no dynamic resolver is
// wired (tests / standalone). Best-effort: catalog errors leave status
// untouched and never fail the read. orgID is the OC namespace (locally). The
// concrete address is injected by the consumer wiring at dispatch (P3).
func (s *ArtifactStore) resolveOrgServices(ctx context.Context, orgID string, d *DesignFile) {
	if s == nil || d == nil {
		return
	}
	for i := range d.Components {
		for j := range d.Components[i].Dependencies {
			dep := &d.Components[i].Dependencies[j]
			if dep.Kind != models.DependencyKindOrgService {
				continue
			}
			if s.orgServices != nil {
				visible, err := s.orgServices.IsNamespaceVisible(ctx, orgID, dep.Name)
				if err != nil {
					continue // transient OC error — leave whatever status is stored
				}
				if visible {
					dep.Status = "resolved"
					dep.Reason = ""
					continue
				}
				// Unresolved: refine the reason so the consumer view can offer a
				// "Request access" affordance only when the provider component
				// actually exists (project-only ⇒ `unpublished`/requestable;
				// missing ⇒ `not-found`). Best-effort: an ExistsAnyVisibility error
				// leaves the reason empty rather than failing the read (P3.5).
				dep.Status = "unresolved"
				dep.Reason = ""
				if exists, err := s.orgServices.ExistsAnyVisibility(ctx, orgID, dep.Name); err == nil {
					if exists {
						dep.Reason = "unpublished"
					} else {
						dep.Reason = "not-found"
					}
				}
				continue
			}
			// Static fallback (no dynamic catalog wired).
			if dep.Status == "" && s.externalAPIs != nil {
				if entry := s.externalAPIs.Lookup(dep.Name); entry.URL != "" {
					dep.Status = "resolved"
				}
			}
		}
	}
}

// WriteDesign splits the in-memory design into multiple files, then writes
// every file via the git-service. Files no longer referenced by the new
// design (e.g. components removed by a regeneration) are NOT auto-deleted —
// the caller is expected to call DeleteDesignDirectory for removed
// components separately.
func (s *ArtifactStore) WriteDesign(ctx context.Context, orgID, projectID string, design *DesignFile) error {
	files, err := SplitDesign(design)
	if err != nil {
		return fmt.Errorf("split design: %w", err)
	}
	for subPath, content := range files {
		if _, err := s.WriteDesignFile(ctx, orgID, projectID, subPath, content); err != nil {
			return fmt.Errorf("write %s: %w", subPath, err)
		}
	}
	return nil
}

// WriteComponentDesign writes ONLY the given component's `design.md` (frontmatter
// + body) to the LOCAL CLONE's working tree, leaving every sibling file
// untouched. It renders through the same SplitDesign machinery used by
// WriteDesign — wrapping the single component in a throwaway DesignFile — so the
// frontmatter round-trips identically to a full write (no risk of a hand-rolled
// YAML drifting from the canonical encoder).
//
// IMPORTANT: this is a working-tree write only — it does NOT commit or push. A
// subsequent SaveDesign is required to persist the change. For internal
// durability touch-ups that must land on remote main WITHOUT minting a new
// design version (e.g. the P3.5 grant cascade), use SetComponentOrgPublished,
// which commits via the Git Data API.
func (s *ArtifactStore) WriteComponentDesign(ctx context.Context, orgID, projectID string, comp models.DesignComponent) error {
	if comp.Name == "" {
		return fmt.Errorf("write component design: empty component name")
	}
	files, err := SplitDesign(&DesignFile{Components: []models.DesignComponent{comp}})
	if err != nil {
		return fmt.Errorf("render component %q design: %w", comp.Name, err)
	}
	subPath := componentDirPrefix + comp.Name + "/design.md"
	content, ok := files[subPath]
	if !ok {
		return fmt.Errorf("render component %q design: %q missing from split", comp.Name, subPath)
	}
	if _, err := s.WriteDesignFile(ctx, orgID, projectID, subPath, content); err != nil {
		return fmt.Errorf("write %s: %w", subPath, err)
	}
	return nil
}

// SetComponentOrgPublished durably persists `exposesAPI.orgPublished:true` on
// the provider component and COMMITS that one `design.md` to remote main
// (no new design version tag). This is the P3.5 grant-cascade durability write:
// when a provider's `org-publish` task deploys, the flag must survive a future
// re-implementation so namespace visibility isn't silently dropped.
//
// `componentName` may be the design's logical component name OR the OC component
// name `<project>-<logical>` — both forms match. Idempotent: a no-op (no commit)
// when the component already has the flag set, when no design exists, or when no
// matching component is found. Unlike WriteComponentDesign (working-tree only),
// this reaches a real GitHub commit so the change is never lost.
func (s *ArtifactStore) SetComponentOrgPublished(ctx context.Context, orgID, projectID, componentName string) error {
	design, err := s.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		return fmt.Errorf("read design: %w", err)
	}
	if design == nil {
		return nil // no design yet — nothing to persist.
	}
	for i := range design.Components {
		comp := design.Components[i]
		if !designComponentMatches(comp.Name, projectID, componentName) {
			continue
		}
		if comp.ExposesAPI == nil {
			comp.ExposesAPI = &models.ExposesAPI{}
		}
		if comp.ExposesAPI.OrgPublished {
			return nil // idempotent — already durable, no commit.
		}
		comp.ExposesAPI.OrgPublished = true

		// Render ONLY this component's design.md through the canonical encoder
		// (same machinery as WriteComponentDesign) so the frontmatter round-trips
		// identically, then commit that single file to remote main without tagging.
		files, ferr := SplitDesign(&DesignFile{Components: []models.DesignComponent{comp}})
		if ferr != nil {
			return fmt.Errorf("render component %q design: %w", comp.Name, ferr)
		}
		subPath := componentDirPrefix + comp.Name + "/design.md"
		content, ok := files[subPath]
		if !ok {
			return fmt.Errorf("render component %q design: %q missing from split", comp.Name, subPath)
		}
		msg := fmt.Sprintf("chore(marketplace): mark %s org-published (namespace visibility)", comp.Name)
		if _, cerr := s.artifactSvc.CommitDesignFile(ctx, orgID, projectID, subPath, content, msg); cerr != nil {
			return fmt.Errorf("commit %s: %w", subPath, cerr)
		}
		return nil
	}
	return nil // no matching component — nothing to persist.
}

// designComponentMatches reports whether a design component named `logical` is
// the provider identified by `target`, which may be the bare logical name or
// the OC component name `<project>-<logical>`. Mirrors the cascade's
// componentMatches so the durability write lands in either form.
func designComponentMatches(logical, project, target string) bool {
	if strings.EqualFold(logical, target) {
		return true
	}
	return strings.EqualFold(project+"-"+logical, target)
}

// ---- Helpers ------------------------------------------------------------

// IsNotFound is sugar for callers that want to distinguish "no artifact yet"
// from a real error.
func IsNotFound(err error) bool { return errors.Is(err, ErrArtifactNotFound) }

// DesignFilesEqual compares two design file maps after trimming whitespace
// from each value. Used by the has-unsaved-changes check.
func DesignFilesEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok {
			return false
		}
		if strings.TrimSpace(va) != strings.TrimSpace(vb) {
			return false
		}
	}
	return true
}

// rootFrontmatter is the YAML frontmatter we accept on the root `design.md`.
type rootFrontmatter struct {
	SourceSpec    string   `yaml:"sourceSpec,omitempty"`
	SkillsApplied []string `yaml:"skillsApplied,omitempty"`
}

// componentFrontmatter is the YAML frontmatter we accept on each
// `components/<name>/design.md`. Field names mirror the user-facing keys
// (snake-free) so frontmatter the architect emits is human-editable.
//
// `Dependencies` is the unified model (write path emits only this). `DependsOn`
// + `DependentApis` are LEGACY read-compat: when `Dependencies` is absent we
// fold them in (one phase; see plan migration notes). Writes never emit them.
type componentFrontmatter struct {
	Type           string                `yaml:"type"`
	Language       string                `yaml:"language,omitempty"`
	Dependencies   []dependencyConfig    `yaml:"dependencies,omitempty"`
	Origin         string                `yaml:"origin,omitempty"`
	Image          string                `yaml:"image,omitempty"`
	Config         []configKeyConfig     `yaml:"config,omitempty"`
	Buildpack      string                `yaml:"buildpack,omitempty"`
	AppPath        string                `yaml:"appPath,omitempty"`
	Entrypoint     string                `yaml:"entrypoint,omitempty"`
	ExposesAPI     *exposesAPIConfig     `yaml:"exposesAPI,omitempty"`
	CallerIdentity *callerIdentityConfig `yaml:"callerIdentity,omitempty"`
	// Legacy (read-only fold-in when Dependencies is empty).
	DependsOn     []string             `yaml:"dependsOn,omitempty"`
	DependentApis []dependentApiConfig `yaml:"dependentApis,omitempty"`
}

// dependencyConfig is the on-disk shape for one unified dependency entry.
// Mirrors models.Dependency.
type dependencyConfig struct {
	Kind         string                      `yaml:"kind"`
	Name         string                      `yaml:"name"`
	Description  string                      `yaml:"description,omitempty"`
	NeedsSpec    bool                        `yaml:"needsSpec,omitempty"`
	SpecPath     string                      `yaml:"specPath,omitempty"`
	Status       string                      `yaml:"status,omitempty"`
	Config       []configKeyConfig           `yaml:"config,omitempty"`
	ResourceType string                      `yaml:"resourceType,omitempty"`
	Parameters   map[string]string           `yaml:"parameters,omitempty"`
	Candidates   []dependencyCandidateConfig `yaml:"candidates,omitempty"`
}

// configKeyConfig is the on-disk shape for one config key (connection schema
// or component config). Mirrors models.ConfigKey.
type configKeyConfig struct {
	Key             string `yaml:"key"`
	Secret          bool   `yaml:"secret"`
	CredentialClass string `yaml:"credentialClass,omitempty"`
}

// dependencyCandidateConfig mirrors models.DependencyCandidate.
type dependencyCandidateConfig struct {
	Label       string `yaml:"label"`
	Description string `yaml:"description,omitempty"`
	URL         string `yaml:"url,omitempty"`
}

func upperSnake(s string) string {
	return strings.ToUpper(strings.ReplaceAll(s, "-", "_"))
}

func toModelConfigKeys(in []configKeyConfig) []models.ConfigKey {
	if len(in) == 0 {
		return nil
	}
	out := make([]models.ConfigKey, 0, len(in))
	for _, c := range in {
		if c.Key == "" {
			continue
		}
		out = append(out, models.ConfigKey{Key: c.Key, Secret: c.Secret, CredentialClass: c.CredentialClass})
	}
	return out
}

func toCfgConfigKeys(in []models.ConfigKey) []configKeyConfig {
	if len(in) == 0 {
		return nil
	}
	out := make([]configKeyConfig, 0, len(in))
	for _, c := range in {
		out = append(out, configKeyConfig{Key: c.Key, Secret: c.Secret, CredentialClass: c.CredentialClass})
	}
	return out
}

func toModelCandidates(in []dependencyCandidateConfig) []models.DependencyCandidate {
	if len(in) == 0 {
		return nil
	}
	out := make([]models.DependencyCandidate, 0, len(in))
	for _, c := range in {
		out = append(out, models.DependencyCandidate{Label: c.Label, Description: c.Description, URL: c.URL})
	}
	return out
}

func toCfgCandidates(in []models.DependencyCandidate) []dependencyCandidateConfig {
	if len(in) == 0 {
		return nil
	}
	out := make([]dependencyCandidateConfig, 0, len(in))
	for _, c := range in {
		out = append(out, dependencyCandidateConfig{Label: c.Label, Description: c.Description, URL: c.URL})
	}
	return out
}

// assembleDependencies builds the unified dependency list from frontmatter.
// Prefers the `dependencies` field; when absent, folds the legacy `dependsOn`
// (→ kind component) and `dependentApis` (→ org-service when name-only, else
// external) so pre-migration designs still load.
func assembleDependencies(cfm componentFrontmatter) []models.Dependency {
	if len(cfm.Dependencies) > 0 {
		out := make([]models.Dependency, 0, len(cfm.Dependencies))
		for _, d := range cfm.Dependencies {
			if d.Name == "" || d.Kind == "" {
				continue
			}
			dep := models.Dependency{
				Kind:         d.Kind,
				Name:         d.Name,
				Description:  d.Description,
				NeedsSpec:    d.NeedsSpec,
				SpecPath:     d.SpecPath,
				Status:       d.Status,
				Config:       toModelConfigKeys(d.Config),
				ResourceType: d.ResourceType,
				Parameters:   d.Parameters,
				Candidates:   toModelCandidates(d.Candidates),
			}
			// External deps that declare needsSpec but have no specPath yet are
			// unresolved at read time — the user must supply the spec before the
			// design can be saved. Do not override a status already set by a
			// prior resolution pass for a different reason.
			if dep.Kind == models.DependencyKindExternal && dep.NeedsSpec && strings.TrimSpace(dep.SpecPath) == "" && dep.Status == "" {
				dep.Status = "unresolved"
			}
			out = append(out, dep)
		}
		return out
	}
	out := make([]models.Dependency, 0, len(cfm.DependsOn)+len(cfm.DependentApis))
	for _, name := range cfm.DependsOn {
		if name == "" {
			continue
		}
		out = append(out, models.Dependency{Kind: models.DependencyKindComponent, Name: name})
	}
	for _, d := range cfm.DependentApis {
		if d.Name == "" {
			continue
		}
		out = append(out, foldLegacyDependentApi(d))
	}
	return out
}

// foldLegacyDependentApi maps a legacy dependentApi to the unified model:
// name-only (catalog/org-published) → org-service; URL-bearing third-party →
// external with a base-URL config key (+ a credential key for the auth scheme).
func foldLegacyDependentApi(d dependentApiConfig) models.Dependency {
	if d.URL == "" {
		return models.Dependency{
			Kind:        models.DependencyKindOrgService,
			Name:        d.Name,
			Description: d.Description,
		}
	}
	cfg := []models.ConfigKey{{Key: upperSnake(d.Name) + "_BASE_URL", Secret: false}}
	switch d.Authentication {
	case "bearer":
		cfg = append(cfg, models.ConfigKey{Key: upperSnake(d.Name) + "_TOKEN", Secret: true})
	case "api-key":
		cfg = append(cfg, models.ConfigKey{Key: upperSnake(d.Name) + "_API_KEY", Secret: true})
	}
	return models.Dependency{
		Kind:        models.DependencyKindExternal,
		Name:        d.Name,
		Description: d.Description,
		Config:      cfg,
	}
}

// toCfgDeps converts the unified model back to on-disk frontmatter shape.
func toCfgDeps(in []models.Dependency) []dependencyConfig {
	if len(in) == 0 {
		return nil
	}
	out := make([]dependencyConfig, 0, len(in))
	for _, d := range in {
		if d.Name == "" || d.Kind == "" {
			continue
		}
		out = append(out, dependencyConfig{
			Kind:         d.Kind,
			Name:         d.Name,
			Description:  d.Description,
			NeedsSpec:    d.NeedsSpec,
			SpecPath:     d.SpecPath,
			Status:       d.Status,
			Config:       toCfgConfigKeys(d.Config),
			ResourceType: d.ResourceType,
			Parameters:   d.Parameters,
			Candidates:   toCfgCandidates(d.Candidates),
		})
	}
	return out
}

// exposesAPIConfig is the on-disk shape for a service component's API
// exposure policy. `Auth: "end-user-required"` ⇒ gateway validates a
// user JWT and injects UserContext upstream.
type exposesAPIConfig struct {
	Managed      bool   `yaml:"managed,omitempty"`
	Auth         string `yaml:"auth,omitempty"`
	UserContext  string `yaml:"userContext,omitempty"`
	OrgPublished bool   `yaml:"orgPublished,omitempty"` // P3: endpoint consumable cross-project (namespace visibility)
}

// callerIdentityConfig is the on-disk shape for a web-app's caller-
// identity intent. `Mode: "end-user"` ⇒ the SPA performs OIDC + PKCE
// against the platform IDP.
type callerIdentityConfig struct {
	Mode string `yaml:"mode,omitempty"`
}

// dependentApiConfig is the on-disk shape for an external upstream API the
// component consumes at runtime. See models.DependentAPI for the wire shape.
type dependentApiConfig struct {
	Name           string `yaml:"name"`
	URL            string `yaml:"url"`
	Description    string `yaml:"description,omitempty"`
	Authentication string `yaml:"authentication,omitempty"`
}

// SplitFrontmatter separates the leading YAML frontmatter block (delimited
// by `---` lines) from the body. If the file has no frontmatter, returns
// ("", content, nil).
func SplitFrontmatter(content string) (fm string, body string, err error) {
	trimmed := strings.TrimLeft(content, " \t\r\n")
	// Strip an optional UTF-8 BOM (U+FEFF) before frontmatter detection.
	trimmed = strings.TrimPrefix(trimmed, "\ufeff")
	if !strings.HasPrefix(trimmed, "---") {
		return "", content, nil
	}
	// Find the closing fence — must be a `---` on its own line after the open.
	rest := trimmed[3:]
	// Skip optional newline directly after opening fence.
	rest = strings.TrimLeft(rest, " \t")
	if !strings.HasPrefix(rest, "\n") && !strings.HasPrefix(rest, "\r\n") {
		// Open fence was not followed by a newline — treat as no frontmatter.
		return "", content, nil
	}
	// Locate the end fence.
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", content, fmt.Errorf("frontmatter: unterminated --- block")
	}
	fm = strings.TrimSpace(rest[:end])
	after := rest[end+len("\n---"):]
	// Drop optional newline + spaces after the closing fence.
	after = strings.TrimPrefix(after, "\r")
	after = strings.TrimPrefix(after, "\n")
	return fm, after, nil
}

// joinFrontmatter writes the YAML frontmatter block + body. If the
// frontmatter is empty, returns the body unchanged.
func joinFrontmatter(fm string, body string) string {
	fm = strings.TrimSpace(fm)
	body = strings.TrimLeft(body, "\r\n")
	if fm == "" {
		return body
	}
	return "---\n" + fm + "\n---\n\n" + body
}

// AssembleDesign reconstructs a flat DesignFile from the multi-file working
// tree map. Path separator is `/`. Returns an error if the root `design.md`
// is missing (callers handle that as "no design yet").
func AssembleDesign(files map[string]string) (*DesignFile, error) {
	root, ok := files[DesignRootFile]
	if !ok {
		return nil, fmt.Errorf("design.md missing")
	}

	fm, body, err := SplitFrontmatter(root)
	if err != nil {
		return nil, fmt.Errorf("parse design.md frontmatter: %w", err)
	}
	var rfm rootFrontmatter
	if fm != "" {
		if err := yaml.Unmarshal([]byte(fm), &rfm); err != nil {
			return nil, fmt.Errorf("decode design.md frontmatter: %w", err)
		}
	}
	out := &DesignFile{
		Overview:      strings.TrimSpace(body),
		SourceSpec:    rfm.SourceSpec,
		SkillsApplied: append([]string(nil), rfm.SkillsApplied...),
	}

	// Iterate component dirs in deterministic order.
	componentNames := ComponentNamesIn(files)
	out.Components = make([]models.DesignComponent, 0, len(componentNames))
	for _, name := range componentNames {
		designPath := componentDirPrefix + name + "/design.md"
		raw, ok := files[designPath]
		if !ok {
			continue
		}
		comp, err := assembleComponent(name, raw, files)
		if err != nil {
			return nil, fmt.Errorf("assemble component %q: %w", name, err)
		}
		out.Components = append(out.Components, comp)
	}
	return out, nil
}

func assembleComponent(name, designMd string, files map[string]string) (models.DesignComponent, error) {
	fm, body, err := SplitFrontmatter(designMd)
	if err != nil {
		return models.DesignComponent{}, fmt.Errorf("frontmatter: %w", err)
	}
	var cfm componentFrontmatter
	if fm != "" {
		if err := yaml.Unmarshal([]byte(fm), &cfm); err != nil {
			return models.DesignComponent{}, fmt.Errorf("decode frontmatter: %w", err)
		}
	}
	openapi := files[componentDirPrefix+name+"/openapi.yaml"]
	if openapi == "" {
		// Fallback: support .yml as well.
		openapi = files[componentDirPrefix+name+"/openapi.yml"]
	}
	deps := assembleDependencies(cfm)
	if deps == nil {
		deps = []models.Dependency{}
	}
	var exposes *models.ExposesAPI
	if cfm.ExposesAPI != nil && (cfm.ExposesAPI.Auth != "" || cfm.ExposesAPI.Managed || cfm.ExposesAPI.UserContext != "" || cfm.ExposesAPI.OrgPublished) {
		exposes = &models.ExposesAPI{
			Managed:      cfm.ExposesAPI.Managed,
			Auth:         cfm.ExposesAPI.Auth,
			UserContext:  cfm.ExposesAPI.UserContext,
			OrgPublished: cfm.ExposesAPI.OrgPublished,
		}
	}
	var caller *models.CallerIdentity
	if cfm.CallerIdentity != nil && cfm.CallerIdentity.Mode != "" {
		caller = &models.CallerIdentity{Mode: cfm.CallerIdentity.Mode}
	}
	return models.DesignComponent{
		Name:                       name,
		ComponentType:              cfm.Type,
		Language:                   cfm.Language,
		Dependencies:               deps,
		Origin:                     cfm.Origin,
		Image:                      cfm.Image,
		Config:                     toModelConfigKeys(cfm.Config),
		Entrypoint:                 cfm.Entrypoint,
		Buildpack:                  cfm.Buildpack,
		AppPath:                    cfm.AppPath,
		OpenAPISpec:                openapi,
		ComponentAgentInstructions: strings.TrimSpace(body),
		ExposesAPI:                 exposes,
		CallerIdentity:             caller,
	}, nil
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

// SplitDesign takes a flat in-memory design and produces the file map for
// the working tree. The caller is responsible for deleting any
// pre-existing files NOT present in the returned map (e.g. components
// removed across a regeneration).
func SplitDesign(d *DesignFile) (map[string]string, error) {
	if d == nil {
		return nil, fmt.Errorf("nil design")
	}
	out := make(map[string]string, 1+2*len(d.Components))

	// Root design.md — body + optional frontmatter. SourceSpec is encoded
	// in the design tag name (`v<N>-<M>`); we only write it to the file
	// frontmatter when there is some other field that requires the block
	// (currently: skillsApplied per docs/design/skills-system.md). The
	// console's markdown preview strips frontmatter via SplitFrontmatter,
	// so the visible Overview prose is unchanged.
	if len(d.SkillsApplied) > 0 {
		// Sorted copy for stable diffs.
		sortedSkills := append([]string(nil), d.SkillsApplied...)
		sort.Strings(sortedSkills)
		rfm := rootFrontmatter{SkillsApplied: sortedSkills}
		rfmBytes, err := marshalFrontmatter(rfm)
		if err != nil {
			return nil, fmt.Errorf("encode root frontmatter: %w", err)
		}
		out[DesignRootFile] = joinFrontmatter(string(rfmBytes), strings.TrimSpace(d.Overview)+"\n")
	} else {
		out[DesignRootFile] = strings.TrimSpace(d.Overview) + "\n"
	}

	for _, comp := range d.Components {
		if comp.Name == "" {
			return nil, fmt.Errorf("component with empty name")
		}
		base := componentDirPrefix + comp.Name
		cfm := componentFrontmatter{
			Type:         comp.ComponentType,
			Language:     comp.Language,
			Dependencies: toCfgDeps(comp.Dependencies),
			Origin:       comp.Origin,
			Image:        comp.Image,
			Config:       toCfgConfigKeys(comp.Config),
			Buildpack:    comp.Buildpack,
			AppPath:      comp.AppPath,
			Entrypoint:   comp.Entrypoint,
		}
		// Preserve any non-empty field — gating on `Auth != ""` would drop
		// designs that set only `managed` or `userContext`.
		if comp.ExposesAPI != nil && (comp.ExposesAPI.Auth != "" || comp.ExposesAPI.Managed || comp.ExposesAPI.UserContext != "" || comp.ExposesAPI.OrgPublished) {
			cfm.ExposesAPI = &exposesAPIConfig{
				Managed:      comp.ExposesAPI.Managed,
				Auth:         comp.ExposesAPI.Auth,
				UserContext:  comp.ExposesAPI.UserContext,
				OrgPublished: comp.ExposesAPI.OrgPublished,
			}
		}
		if comp.CallerIdentity != nil && comp.CallerIdentity.Mode != "" {
			cfm.CallerIdentity = &callerIdentityConfig{Mode: comp.CallerIdentity.Mode}
		}
		cfmBytes, err := marshalFrontmatter(cfm)
		if err != nil {
			return nil, fmt.Errorf("encode component %q frontmatter: %w", comp.Name, err)
		}
		header := fmt.Sprintf("# %s\n\n", comp.Name)
		out[base+"/design.md"] = joinFrontmatter(string(cfmBytes), header+strings.TrimSpace(comp.ComponentAgentInstructions)+"\n")
		if openapi := strings.TrimSpace(comp.OpenAPISpec); openapi != "" {
			out[base+"/openapi.yaml"] = openapi + "\n"
		}
	}
	return out, nil
}

// ComponentDirPath returns the directory path for a given component name
// (relative to specs/design/), used by DeleteDesignDirectory.
func ComponentDirPath(componentName string) string {
	return path.Join(componentDirPrefix, componentName)
}

// marshalFrontmatter encodes v as YAML, but returns an empty string when
// the encoded form is "{}\n" (yaml.Marshal of an all-zero struct).
func marshalFrontmatter(v interface{}) ([]byte, error) {
	out, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "{}" {
		return []byte{}, nil
	}
	return []byte(trimmed), nil
}
