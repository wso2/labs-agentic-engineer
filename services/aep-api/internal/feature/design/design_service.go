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

package design

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/internal/feature/dependencies/resources"
	"github.com/wso2/aep/aep-api/models"
)

// ErrSpecNotApproved is the design-domain sentinel surfaced (as 409 by the
// controller) when a design is saved before the requirements spec has been
// approved (tagged). Owned by the design feature — it is the only consumer.
var ErrSpecNotApproved = errors.New("spec must be saved (tagged) before generating a design")

// ErrUnresolvedDependency is the design-domain sentinel surfaced (as 409 by the
// controller) when the tag-cut (SaveAndProceed) is attempted while a
// dependency in the design being tagged is still in a non-actionable state:
// an `org-service` that is not namespace-visible (unresolved/blocked/ambiguous
// against the live org catalog — see artifacts.resolveOrgServices), or an
// `external` dependency that declares needsSpec but has no collected spec yet.
// external-values (config-only) and platform-resource dependencies are NOT
// proceed-gated here — they are dispatch-gated in Phase 6. The tag-cut is the
// only path that checks this; committed-truth has no autosave to gate.
var ErrUnresolvedDependency = errors.New("design has unresolved dependencies — resolve them before saving")

// ErrEndUserAuthConflict is the design-domain sentinel surfaced (as 409 by the
// controller, mirroring ErrUnresolvedDependency) when the tag-cut is attempted
// on a design where a service component declares a platform-resource dependency
// whose type carries the end-user-auth role marker (auth-as-platform-resource)
// AND explicitly sets exposesAPI.auth to service-required. The dependency
// requires end-user-required; the platform derives it automatically, so this
// only fires on a genuine, explicit contradiction — see deriveEndUserAuth.
var ErrEndUserAuthConflict = errors.New("service component declares an end-user-auth platform-resource dependency but explicitly sets a conflicting exposesAPI.auth")

// ErrResourceCatalogUnavailable is the fail-closed sentinel surfaced (as 503 by
// the controller) when a design declares at least one platform-resource
// dependency but the OC ClusterResourceType catalog cannot be read — so the
// platform cannot tell whether any of those dependencies' types carries the
// end-user-auth role. Rather than silently skip the derivation (which could
// leave an API that must sit behind end-user login exposed — the silent-open
// risk), the save fails with this retryable error. Auth-free saves (no
// platform-resource dependency) never fetch the catalog and so never hit this.
var ErrResourceCatalogUnavailable = errors.New("resource-type catalog unavailable — retry the save")

// Spec-collection sentinels (dependency-management — the collect-dependency-spec
// route). The HTTP layer maps each to a status: ErrDependencyNotFound→404,
// ErrDependencyWrongKind/ErrInvalidSpec→400, ErrSpecFetchFailed→502,
// ErrSpecCommitConflict→409.
var (
	// ErrDependencyNotFound: the {component, depName} pair is absent from the
	// current design.
	ErrDependencyNotFound = errors.New("dependency not found in design")
	// ErrDependencyWrongKind: the dependency exists but is not `external` — spec
	// collection applies only to external API dependencies.
	ErrDependencyWrongKind = errors.New("dependency is not an external dependency")
	// ErrSpecFetchFailed: the SSRF-guarded fetch of a user-supplied spec URL
	// failed (bad URL, blocked target, non-2xx, oversized).
	ErrSpecFetchFailed = errors.New("failed to fetch spec from URL")
	// ErrInvalidSpec: the supplied/fetched document is not a valid OpenAPI 3.x
	// spec (or the depName is unsafe).
	ErrInvalidSpec = errors.New("invalid OpenAPI spec")
	// ErrSpecCommitConflict: the design.json moved under the collect between the
	// read and the atomic commit (stale baseSha) — the caller should reload.
	ErrSpecCommitConflict = errors.New("design changed while collecting the spec — reload and retry")
)

// AssembleDesignFromFiles wraps the artifact-store assembler and rejects an
// empty file map. Used to assemble a tagged design file map read out of band.
func AssembleDesignFromFiles(files map[string]string) (*artifacts.DesignFile, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("decode design: empty file map")
	}
	return artifacts.AssembleDesign(files)
}

// DesignBundle is the file-map view returned to the architecture page. It
// pairs the raw per-file contents (used by the Explorer) with the assembled
// flat Design (used by the cell diagram + downstream code).
type DesignBundle struct {
	Files  map[string]string `json:"files"`
	Design *models.Design    `json:"design"`
}

// DesignService is the read + version surface for the multi-file design
// artifact stored under `specs/design/`. Per the GitHub-direct rework
// (docs/design/agents-generation-migration.md §12.2) the per-file PUT/DELETE,
// component delete, and the architect generate stream are gone — edits are
// frontend drafts committed via the Files API, and generation is the unified
// genai turn endpoint. What remains: read the bundle at HEAD / at a tag, save
// (hard gate → tag at HEAD, then task reconciliation), discard (revert to last
// tag), and version reads.
type DesignService interface {
	GetDesignBundle(ctx context.Context, orgID, projectID string) (*DesignBundle, error)
	GetDesignBundleAtTag(ctx context.Context, orgID, projectID, tag string) (*DesignBundle, error)
	// SaveAndProceed cuts the next `v<N>-<M>` tag. commitSHA, when non-empty, is
	// the commit the caller's files-apply just created — the save reads, gates,
	// and tags that exact commit instead of re-reading `heads/main`.
	SaveAndProceed(ctx context.Context, orgID, projectID, commitSHA string) (*models.Design, error)
	DiscardChanges(ctx context.Context, orgID, projectID string) (*models.Design, error)
	ListDesignVersions(ctx context.Context, orgID, projectID string) ([]models.ArtifactVersion, error)
	// CollectSpec fetches (SSRF-guarded, when specURL is set) / accepts (rawSpec)
	// an OpenAPI contract for a component's `external` dependency, validates +
	// normalizes it, and atomically commits the spec file plus the design.json
	// specPath edit that clears the external-needs-spec proceed-gate. Returns the
	// component-relative stored path.
	CollectSpec(ctx context.Context, orgID, projectID, component, depName string, rawSpec []byte, specURL string) (string, error)
}

type designService struct {
	store               *artifacts.ArtifactStore
	artifactSvc         artifacts.ArtifactService
	taskSvc             taskReconciler            // for SaveAndProceed reconciliation; may be nil in tests
	externalResourceReg externalResourceRegistrar // for SaveAndProceed external-resource registration; may be nil
	provisionMinter     provisionIssueMinter      // for SaveAndProceed aep:provision gate minting; may be nil
	fileCommitter       designFileCommitter       // for CollectSpec's committed-truth spec write; may be nil
	resourceCatalog     resourceMarkerCatalog     // for SaveAndProceed end-user-auth derivation; may be nil (fails closed when platform-resource deps exist)
}

// DesignFileWrite is one file in a CollectSpec atomic commit. Path is the full
// repo path (e.g. `specs/design/components/orders/design.json`); BaseSHA is the
// blob's current sha for an update (optimistic-concurrency CAS) or "" for a
// create.
type DesignFileWrite struct {
	Path    string
	Content string
	BaseSHA string
}

// designFileCommitter is design_service's narrow consumer port over the Files
// API (feature/files) — the committed-truth single-commit write surface. The
// composition root adapts *files.service to it (design imports only artifacts;
// the port keeps the files package out of this feature). Nil is a documented
// no-op: CollectSpec then returns an error and the route 503s.
type designFileCommitter interface {
	// ReadFile returns the file's current content + blob sha (the CAS token),
	// ok=false when the path does not exist yet, or an error on infra failure.
	ReadFile(ctx context.Context, orgID, projectID, path string) (content, sha string, ok bool, err error)
	// Commit atomically writes every file (per-file BaseSHA CAS) in one commit to
	// main. A stale BaseSHA surfaces as ErrSpecCommitConflict.
	Commit(ctx context.Context, orgID, projectID string, writes []DesignFileWrite, message string) error
}

// resourceMarkerCatalog is design_service's narrow consumer port over the
// dependencies/resources catalog: it returns the PE-authored CRT marker map
// (resources.TypeMarkers keyed by resourceType name) that design-save keys the
// end-user-auth derivation on — replacing the deleted hardcoded thunder-app
// resourceType name. *resources.ResourceTypeCatalog satisfies it structurally. Wired via
// SetResourceCatalog at the composition root; a nil catalog fails the save
// closed (ErrResourceCatalogUnavailable) whenever the design declares a
// platform-resource dependency, so the derivation is never silently skipped.
type resourceMarkerCatalog interface {
	MarkersByName(ctx context.Context) (map[string]resources.TypeMarkers, error)
}

// provisionIssueMinter is design_service's narrow consumer port for the
// provisioning feature: on a design tag-cut it ensures one aep:provision gate
// issue per distinct external / platform-resource dependency exists
// (dependency-management §3.6). *provisioning.Service satisfies it. Wired via
// SetProvisionIssueMinter at the composition root; nil is a no-op.
type provisionIssueMinter interface {
	EnsureProvisionIssues(ctx context.Context, orgID, projectID, designTag string) error
}

// externalResourceRegistrar is design_service's narrow consumer port for the
// dependency-management external-resource catalog. *repositories.ExternalResourceRepository
// satisfies it; defined here (consumer side) so design needn't import the
// repositories package concretely. Wired via SetExternalResourceRegistry at the
// composition root.
type externalResourceRegistrar interface {
	Upsert(ctx context.Context, orgID, name, description string, schema []models.ConfigKey) (*models.ExternalResource, error)
}

// taskReconciler is design_service's narrow consumer port for the task
// feature's reconciliation hook. The full task.TaskService satisfies it.
// Defining it here keeps design_service from importing the task package
// (the reconcile edge is one-way: main wires task.TaskService into this
// setter).
type taskReconciler interface {
	ReconcilePendingForDesignChange(ctx context.Context, orgID, projectID string) error
}

func NewDesignService(
	store *artifacts.ArtifactStore,
	artifactSvc artifacts.ArtifactService,
) *designService {
	return &designService{
		store:       store,
		artifactSvc: artifactSvc,
	}
}

func (s *designService) SetTaskService(taskSvc taskReconciler) {
	s.taskSvc = taskSvc
}

// SetExternalResourceRegistry wires the external-resource catalog so `external`
// dependencies are registered (best-effort) whenever a design is tagged. A nil
// registry is a documented no-op (mirrors SetTaskService).
func (s *designService) SetExternalResourceRegistry(reg externalResourceRegistrar) {
	s.externalResourceReg = reg
}

// SetProvisionIssueMinter wires the provisioning feature so external /
// platform-resource dependency gate issues are minted (best-effort) whenever a
// design is tagged. A nil minter is a documented no-op.
func (s *designService) SetProvisionIssueMinter(m provisionIssueMinter) {
	s.provisionMinter = m
}

// SetFileCommitter wires the committed-truth Files commit surface CollectSpec
// uses to persist a consumed spec + the design.json specPath edit. A nil
// committer makes CollectSpec fail (the route then returns 503).
func (s *designService) SetFileCommitter(c designFileCommitter) {
	s.fileCommitter = c
}

// SetResourceCatalog wires the CRT marker lookup SaveAndProceed uses to decide
// which platform-resource dependencies stamp end-user auth. A nil catalog makes
// the save fail closed (ErrResourceCatalogUnavailable) whenever the design
// declares a platform-resource dependency; auth-free saves are unaffected.
func (s *designService) SetResourceCatalog(c resourceMarkerCatalog) {
	s.resourceCatalog = c
}

// registerExternalResources best-effort upserts every distinct `external`
// dependency in the tagged design into the org's external-resource catalog (the
// reusable definition layer consumers request access to later). Non-fatal: a
// registry hiccup must never fail a design save — the value/wiring provisioning
// happens later, independently, when a consumer requests access (Phase 6).
func (s *designService) registerExternalResources(ctx context.Context, orgID string, design *artifacts.DesignFile) {
	if s.externalResourceReg == nil || design == nil {
		return
	}
	seen := map[string]struct{}{}
	for _, c := range design.Components {
		for _, dep := range c.Dependencies {
			if dep.Kind != models.DependencyKindExternal {
				continue
			}
			if _, ok := seen[dep.Name]; ok {
				continue
			}
			seen[dep.Name] = struct{}{}
			if _, err := s.externalResourceReg.Upsert(ctx, orgID, dep.Name, dep.Description, dep.Config); err != nil {
				slog.WarnContext(ctx, "failed to register external resource",
					"org", orgID, "resource", dep.Name, "error", err)
			}
		}
	}
}

// CollectSpec resolves the {component, depName} external dependency in the
// current design, fetches (SSRF-guarded) or accepts its OpenAPI contract,
// validates + normalizes it, and atomically commits TWO files to main: the
// normalized spec at `specs/design/components/<c>/dependencies/<dep>.openapi.yaml`
// and the component's design.json with the dependency's specPath recorded (which
// clears the external-needs-spec proceed-gate on the next read; the transient
// specUrl hint is dropped). Read-time status/reason are never persisted — the
// design.json codec (SplitDesign → dependencyJSON) omits them (ADR-0003).
func (s *designService) CollectSpec(ctx context.Context, orgID, projectID, component, depName string, rawSpec []byte, specURL string) (string, error) {
	hasRaw := len(rawSpec) > 0
	hasURL := strings.TrimSpace(specURL) != ""
	switch {
	case !hasRaw && !hasURL:
		return "", fmt.Errorf("%w: provide rawSpec or specUrl", ErrInvalidSpec)
	case hasRaw && hasURL:
		return "", fmt.Errorf("%w: provide only one of rawSpec or specUrl", ErrInvalidSpec)
	}
	if s.fileCommitter == nil {
		return "", fmt.Errorf("spec collection unavailable: no committed-truth write surface wired")
	}

	// Resolve the target dependency BEFORE fetching/storing anything, so an
	// unknown dep (404) or a non-external dep (400) never leaves an orphan blob.
	design, err := s.store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		if artifacts.IsNotFound(err) {
			return "", fmt.Errorf("%w: no design for project %q", ErrDependencyNotFound, projectID)
		}
		return "", fmt.Errorf("read design: %w", err)
	}
	if design == nil {
		return "", fmt.Errorf("%w: no design for project %q", ErrDependencyNotFound, projectID)
	}
	compIdx, depIdx := -1, -1
	for i := range design.Components {
		if design.Components[i].Name != component {
			continue
		}
		compIdx = i
		for j := range design.Components[i].Dependencies {
			if design.Components[i].Dependencies[j].Name == depName {
				depIdx = j
			}
		}
	}
	if compIdx < 0 {
		return "", fmt.Errorf("%w: component %q not found in design", ErrDependencyNotFound, component)
	}
	if depIdx < 0 {
		return "", fmt.Errorf("%w: dependency %q not found on component %q", ErrDependencyNotFound, depName, component)
	}
	if kind := design.Components[compIdx].Dependencies[depIdx].Kind; kind != models.DependencyKindExternal {
		return "", fmt.Errorf("%w: dependency %q on component %q has kind %q; spec collection applies only to %q",
			ErrDependencyWrongKind, depName, component, kind, models.DependencyKindExternal)
	}

	if hasURL {
		fetched, ferr := artifacts.FetchSpecFromURL(ctx, specURL)
		if ferr != nil {
			return "", fmt.Errorf("%w: %v", ErrSpecFetchFailed, ferr)
		}
		rawSpec = fetched
	}

	// Validate + normalize; StoreConsumedSpec returns the component-relative
	// specPath and the normalized blob to commit.
	specPath, normalized, err := s.store.StoreConsumedSpec(ctx, orgID, projectID, component, depName, rawSpec)
	if err != nil {
		if errors.Is(err, artifacts.ErrInvalidSpecContent) {
			return "", fmt.Errorf("%w: %v", ErrInvalidSpec, err)
		}
		return "", err
	}

	// Record specPath + drop the transient specUrl hint, then render ONLY this
	// component's design.json through the canonical codec.
	comp := design.Components[compIdx]
	comp.Dependencies[depIdx].SpecPath = specPath
	comp.Dependencies[depIdx].SpecUrl = ""
	rendered, rerr := artifacts.SplitDesign(&artifacts.DesignFile{Components: []models.DesignComponent{comp}})
	if rerr != nil {
		return "", fmt.Errorf("render component %q design.json: %w", component, rerr)
	}
	designSub := "components/" + component + "/design.json" // relative to specs/design/
	designContent, ok := rendered[designSub]
	if !ok {
		return "", fmt.Errorf("render component %q design.json: %q missing from split", component, designSub)
	}

	// Full repo paths + CAS shas. design.json must exist; the spec file may not
	// yet (a fresh collect creates it, a re-collect overwrites at its sha).
	designFull := artifacts.DesignDir + "/" + designSub
	specFull := artifacts.DesignDir + "/components/" + component + "/" + specPath
	_, designSHA, designExists, rerr := s.fileCommitter.ReadFile(ctx, orgID, projectID, designFull)
	if rerr != nil {
		return "", fmt.Errorf("read design.json for CAS: %w", rerr)
	}
	if !designExists {
		return "", fmt.Errorf("%w: component %q design.json missing on disk", ErrDependencyNotFound, component)
	}
	_, specSHA, _, rerr := s.fileCommitter.ReadFile(ctx, orgID, projectID, specFull)
	if rerr != nil {
		return "", fmt.Errorf("read spec file for CAS: %w", rerr)
	}

	writes := []DesignFileWrite{
		{Path: specFull, Content: normalized, BaseSHA: specSHA}, // BaseSHA "" ⇒ create
		{Path: designFull, Content: designContent, BaseSHA: designSHA},
	}
	if err := s.fileCommitter.Commit(ctx, orgID, projectID, writes,
		fmt.Sprintf("Collect OpenAPI spec for %s dependency %s", component, depName)); err != nil {
		return "", err // ErrSpecCommitConflict (409) or infra (500)
	}
	slog.InfoContext(ctx, "collected consumed spec",
		"org", orgID, "project", projectID, "component", component, "dependency", depName, "specPath", specPath)
	return specPath, nil
}

// MarkOrgPublished commits the `exposesAPI.orgPublished` durability marker on a
// provider component's design.json — the provisioning grant cascade calls it
// when a provider deploys and a cross-project access request resolves. It is
// idempotent (already-published or absent component → no-op) and best-effort:
// the FUNCTIONAL org-service gate already clears via Phase 5's live-catalog
// proceed-gate; this persists the provider's deliberate publish decision so it
// survives a redeploy. Provider-owned, source-of-truth: the platform sets it
// only as the recorded outcome of a grant, never speculatively.
func (s *designService) MarkOrgPublished(ctx context.Context, orgID, projectID, component string) error {
	if s.fileCommitter == nil {
		return fmt.Errorf("cannot mark org-published: no committed-truth write surface wired")
	}
	design, err := s.store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		if artifacts.IsNotFound(err) {
			return nil // no design — nothing to mark
		}
		return fmt.Errorf("read design: %w", err)
	}
	if design == nil {
		return nil
	}
	idx := -1
	for i := range design.Components {
		if design.Components[i].Name == component {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil // component absent — best-effort no-op
	}
	comp := design.Components[idx]
	if comp.ExposesAPI != nil && comp.ExposesAPI.OrgPublished {
		return nil // idempotent — already published
	}
	if comp.ExposesAPI == nil {
		comp.ExposesAPI = &models.ExposesAPI{}
	}
	comp.ExposesAPI.OrgPublished = true

	rendered, err := artifacts.SplitDesign(&artifacts.DesignFile{Components: []models.DesignComponent{comp}})
	if err != nil {
		return fmt.Errorf("render component %q design.json: %w", component, err)
	}
	designSub := "components/" + component + "/design.json"
	content, ok := rendered[designSub]
	if !ok {
		return fmt.Errorf("render component %q design.json: %q missing from split", component, designSub)
	}
	designFull := artifacts.DesignDir + "/" + designSub
	_, sha, exists, err := s.fileCommitter.ReadFile(ctx, orgID, projectID, designFull)
	if err != nil {
		return fmt.Errorf("read design.json for CAS: %w", err)
	}
	if !exists {
		return nil // no on-disk design.json — nothing to mark
	}
	if err := s.fileCommitter.Commit(ctx, orgID, projectID,
		[]DesignFileWrite{{Path: designFull, Content: content, BaseSHA: sha}},
		fmt.Sprintf("Publish %s org-wide (exposesAPI.orgPublished)", component)); err != nil {
		return err
	}
	slog.InfoContext(ctx, "committed orgPublished marker", "org", orgID, "project", projectID, "component", component)
	return nil
}

func (s *designService) GetDesign(ctx context.Context, orgID, projectID string) (*models.Design, error) {
	files, err := s.store.ListDesignFiles(ctx, orgID, projectID)
	if err != nil {
		if artifacts.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read design: %w", err)
	}
	return s.designFromFiles(ctx, orgID, projectID, files)
}

// designFromFiles assembles the models.Design view from an already-listed HEAD
// design file map — the bundle is read from GitHub exactly once per request
// (assembly and the has-unsaved-changes compare reuse the same map). Returns
// (nil, nil) when no design exists yet.
func (s *designService) designFromFiles(ctx context.Context, orgID, projectID string, files map[string]string) (*models.Design, error) {
	designFile, err := s.store.AssembleDesignFrom(ctx, orgID, files)
	if err != nil {
		return nil, fmt.Errorf("read design: %w", err)
	}
	if designFile == nil {
		return nil, nil
	}

	tag := ""
	revision := 0
	parentReq := ""
	status := "draft"
	var versions []models.ArtifactVersion

	if s.artifactSvc != nil {
		v, err := s.artifactSvc.ListDesignVersions(ctx, orgID, projectID)
		if err != nil {
			slog.WarnContext(ctx, "failed to list design versions", "error", err)
		} else {
			versions = mapDesignVersions(v)
			if len(v) > 0 {
				tag = v[0].Tag
				revision = v[0].DesignRevision
				parentReq = fmt.Sprintf("v%d", v[0].RequirementsVersion)
				status = "approved"
			}
		}
	}

	// has-unsaved-changes: HEAD design files vs latest-tag files (compared as a
	// flat map of trimmed contents). With no tag yet, any content is by
	// definition unsaved (a draft awaiting its first publish).
	unsaved := false
	if tag == "" {
		unsaved = true
	} else if s.artifactSvc != nil {
		tagged, err := s.artifactSvc.GetDesignAtTag(ctx, orgID, projectID, tag)
		if err == nil && !artifacts.DesignFilesEqual(files, tagged) {
			unsaved = true
		}
	}

	// SourceSpec resolution. The file's SourceSpec reflects the current working
	// copy (set at stream time); the latest tag's parent requirements version
	// reflects the last approved snapshot.
	sourceSpec := designFile.SourceSpec
	if sourceSpec == "" {
		sourceSpec = parentReq
	}

	return &models.Design{
		ProjectID:         projectID,
		OrgID:             orgID,
		Overview:          designFile.Overview,
		Components:        designFile.Components,
		Status:            status,
		Version:           revision,
		Versions:          versions,
		HasUnsavedChanges: unsaved,
		SourceSpec:        sourceSpec,
	}, nil
}

func (s *designService) GetDesignAtTag(ctx context.Context, orgID, projectID, tag string) (*models.Design, error) {
	if s.artifactSvc == nil {
		return nil, fmt.Errorf("git client not configured")
	}
	files, err := s.artifactSvc.GetDesignAtTag(ctx, orgID, projectID, tag)
	if err != nil {
		if errors.Is(err, artifacts.ErrArtifactNotFound) {
			return nil, artifacts.ErrDesignNotFound
		}
		return nil, fmt.Errorf("get design at %s: %w", tag, err)
	}
	designFile, err := AssembleDesignFromFiles(files)
	if err != nil {
		return nil, fmt.Errorf("parse design at %s: %w", tag, err)
	}

	parent := ""
	if parentN, _, ok := decodeDesignTag(tag); ok {
		parent = fmt.Sprintf("v%d", parentN)
	}

	return &models.Design{
		ProjectID:  projectID,
		OrgID:      orgID,
		Overview:   designFile.Overview,
		Components: designFile.Components,
		Status:     "approved",
		SourceSpec: parent,
	}, nil
}

// GetDesignBundle returns the HEAD file map alongside the assembled Design (so
// the Explorer can render the tree and the cell diagram can render in one
// round-trip).
func (s *designService) GetDesignBundle(ctx context.Context, orgID, projectID string) (*DesignBundle, error) {
	files, err := s.store.ListDesignFiles(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if files == nil {
		files = map[string]string{}
	}
	d, err := s.designFromFiles(ctx, orgID, projectID, files)
	if err != nil {
		// If there's no design yet, return an empty bundle (not an error) so
		// the page can render a "no design" state.
		if errors.Is(err, artifacts.ErrDesignNotFound) {
			return &DesignBundle{Files: files, Design: nil}, nil
		}
		return nil, err
	}
	return &DesignBundle{Files: files, Design: d}, nil
}

// GetDesignBundleAtTag returns the file map + assembled Design at a specific
// version tag. Used by the version selector when browsing history.
func (s *designService) GetDesignBundleAtTag(ctx context.Context, orgID, projectID, tag string) (*DesignBundle, error) {
	if s.artifactSvc == nil {
		return nil, fmt.Errorf("git client not configured")
	}
	files, err := s.artifactSvc.GetDesignAtTag(ctx, orgID, projectID, tag)
	if err != nil {
		if errors.Is(err, artifacts.ErrArtifactNotFound) {
			return nil, artifacts.ErrDesignNotFound
		}
		return nil, fmt.Errorf("get design at %s: %w", tag, err)
	}
	d, err := s.GetDesignAtTag(ctx, orgID, projectID, tag)
	if err != nil {
		return nil, err
	}
	return &DesignBundle{Files: files, Design: d}, nil
}

// SaveAndProceed runs the hard save gate and cuts the next `v<N>-<M>` tag,
// where N is the latest requirements version. commitSHA, when non-empty, pins
// every read + the tag to the commit the caller's files-apply just created —
// re-resolving HEAD here loses to GitHub's ref-read lag (a publish's save saw
// the pre-apply tree seconds after the apply landed). Surfaces
// ErrSpecNotApproved (409) when no requirements tag exists yet, and design
// reconciliation runs afterwards so pending tasks for removed components
// auto-close.
func (s *designService) SaveAndProceed(ctx context.Context, orgID, projectID, commitSHA string) (*models.Design, error) {
	if s.artifactSvc == nil {
		return nil, fmt.Errorf("git client not configured")
	}

	readDesign := func() (*artifacts.DesignFile, error) {
		if commitSHA != "" {
			return s.store.ReadDesignAt(ctx, orgID, projectID, commitSHA)
		}
		return s.store.ReadDesign(ctx, orgID, projectID)
	}
	designFile, err := readDesign()
	if err != nil {
		if artifacts.IsNotFound(err) {
			slog.WarnContext(ctx, "design save: design not found (pre-gate read) — 404",
				"project", projectID, "commitSha", commitSHA, "error", err)
			return nil, artifacts.ErrDesignNotFound
		}
		return nil, fmt.Errorf("read design: %w", err)
	}
	if designFile == nil {
		// With no pinned commit, an empty read right after a files-apply that
		// wrote the design is a stale HEAD caught in the act — see the adjacent
		// "bundle read at head" DEBUG line for the commit it resolved.
		slog.WarnContext(ctx, "design save: no design at read commit (empty or missing design.md) — 404",
			"project", projectID, "commitSha", commitSHA)
		return nil, artifacts.ErrDesignNotFound
	}

	// Auto-fetch-on-save (dependency-management): before the gate, fetch any
	// `external` dependency the architect flagged with a specUrl hint but no
	// specPath yet, through CollectSpec (SSRF-guarded fetch → validate/normalize
	// → atomic commit of the spec + the design.json specPath edit that clears the
	// needs-spec gate). A fetch failure is non-fatal: the gate then blocks the
	// save until the spec is supplied by hand. Each success commits to main,
	// advancing HEAD past any pinned commit, so we re-read at HEAD afterwards.
	fetched := false
	for ci := range designFile.Components {
		comp := designFile.Components[ci]
		for di := range comp.Dependencies {
			dep := comp.Dependencies[di]
			if dep.Kind != models.DependencyKindExternal || dep.SpecUrl == "" || dep.SpecPath != "" {
				continue
			}
			if _, ferr := s.CollectSpec(ctx, orgID, projectID, comp.Name, dep.Name, nil, dep.SpecUrl); ferr != nil {
				slog.WarnContext(ctx, "design save: auto-fetch spec failed (non-fatal — gate may block)",
					"project", projectID, "component", comp.Name, "dependency", dep.Name, "error", ferr)
				continue
			}
			fetched = true
		}
	}
	if fetched {
		// HEAD advanced past the (possibly pinned) commit — re-read at HEAD so the
		// gate + tag observe the freshly committed specPaths.
		commitSHA = ""
		designFile, err = s.store.ReadDesign(ctx, orgID, projectID)
		if err != nil {
			return nil, fmt.Errorf("re-read design after auto-fetch: %w", err)
		}
		if designFile == nil {
			return nil, artifacts.ErrDesignNotFound
		}
	}

	// Derive exposesAPI.auth from an end-user-auth platform-resource dependency
	// (auth-as-platform-resource): a service component that declares a
	// platform-resource dependency whose CRT carries the end-user-auth role
	// marker gets exposesAPI.auth stamped end-user-required, persisted to its
	// design.json BEFORE the tag-cut so the derived value is already on disk the
	// next time the design is read (EnsureComponent's create-time trait
	// derivation, component.TraitSyncService, the Explorer) — same
	// re-read-after-commit convention as auto-fetch-on-save above. The CRT
	// marker map is fetched ONCE, and only when the design declares a
	// platform-resource dependency (auth-free saves never touch the catalog);
	// when it declares one but the catalog is unreachable, the save fails closed
	// with ErrResourceCatalogUnavailable rather than silently skipping the
	// derivation. An explicit conflicting service-required is rejected as
	// ErrEndUserAuthConflict and the save is blocked, mirroring the
	// unresolved-dependency proceed-gate immediately below. This SAME marker
	// map is also the one the skill-attach step below keys on (Task G4) — one
	// catalog fetch serves both consumers in this pass.
	markers, mErr := s.resourceMarkersForAuthDerivation(ctx, designFile)
	if mErr != nil {
		return nil, mErr
	}
	derivedCommit, derr := s.persistEndUserAuthDerivation(ctx, orgID, projectID, designFile, markers)
	if derr != nil {
		return nil, derr
	}
	if derivedCommit {
		commitSHA = ""
		designFile, err = s.store.ReadDesign(ctx, orgID, projectID)
		if err != nil {
			return nil, fmt.Errorf("re-read design after auth derivation: %w", err)
		}
		if designFile == nil {
			return nil, artifacts.ErrDesignNotFound
		}
	}

	// Generic conditional skill attachment (Task G4 — attach_skills.go): every
	// platform-resource dependency whose CRT carries the `aep.wso2.com/skill`
	// annotation gets that skill name ensured in the design's skillsApplied,
	// append-only, persisted to root design.md BEFORE the tag-cut. Reuses the
	// SAME marker map fetched above for the auth derivation — no second catalog
	// call, no behavior change when there is no platform-resource dependency or
	// none of its types carry the annotation.
	skillsCommit, skErr := s.persistSkillsApplied(ctx, orgID, projectID, designFile, markers)
	if skErr != nil {
		return nil, skErr
	}
	if skillsCommit {
		commitSHA = ""
		designFile, err = s.store.ReadDesign(ctx, orgID, projectID)
		if err != nil {
			return nil, fmt.Errorf("re-read design after skill attach: %w", err)
		}
		if designFile == nil {
			return nil, artifacts.ErrDesignNotFound
		}
	}

	// Proceed-gate (dependency-management Phase 5): block the tag-cut when a
	// dependency is still non-actionable. The design was just read through
	// ReadDesign/ReadDesignAt → AssembleDesignFrom → resolveOrgServices, so
	// org-service statuses are computed against the live catalog. Only the
	// tag-cut gates — committed-truth has no autosave path.
	if compName, depName, status, blocked := firstUnresolvedDependency(designFile.Components); blocked {
		slog.InfoContext(ctx, "design save: proceed-gate blocked — unresolved dependency",
			"project", projectID, "component", compName, "dependency", depName, "status", status)
		return nil, fmt.Errorf("%w: component %q dep %q (status: %s)",
			ErrUnresolvedDependency, compName, depName, status)
	}

	res, err := s.artifactSvc.SaveDesign(ctx, orgID, projectID, artifacts.SaveRequest{
		Message:   "Update design",
		CommitSHA: commitSHA,
	})
	if err != nil {
		// Translate the "no requirements baseline" case into the design
		// feature's not-approved sentinel so callers + UIs render the same
		// message they already do for spec-not-approved.
		if errors.Is(err, artifacts.ErrNoRequirementsBaseline) {
			return nil, ErrSpecNotApproved
		}
		return nil, fmt.Errorf("save design: %w", err)
	}

	versions, err := s.artifactSvc.ListDesignVersions(ctx, orgID, projectID)
	if err != nil {
		slog.WarnContext(ctx, "list versions after save failed", "error", err)
	}

	slog.InfoContext(ctx, "design save completed",
		"project", projectID, "tag", res.Tag, "status", res.Status)

	// Register every distinct `external` dependency in the tagged design into the
	// org's external-resource catalog (best-effort; never fails the save).
	s.registerExternalResources(ctx, orgID, designFile)

	// Mint one aep:provision gate issue per distinct external / platform-resource
	// dependency so consumer coding tasks hold until each is provisioned
	// (best-effort; never fails the save).
	if s.provisionMinter != nil {
		if merr := s.provisionMinter.EnsureProvisionIssues(ctx, orgID, projectID, res.Tag); merr != nil {
			slog.WarnContext(ctx, "provision issue minting after design save failed", "error", merr)
		}
	}

	if s.taskSvc != nil {
		if rerr := s.taskSvc.ReconcilePendingForDesignChange(ctx, orgID, projectID); rerr != nil {
			slog.WarnContext(ctx, "task reconciliation after design save failed", "error", rerr)
		}
	}

	return &models.Design{
		ProjectID:  projectID,
		OrgID:      orgID,
		Overview:   designFile.Overview,
		Components: designFile.Components,
		Status:     "approved",
		Version:    res.DesignRevision,
		Versions:   mapDesignVersions(versions),
		SourceSpec: fmt.Sprintf("v%d", res.RequirementsVersion),
	}, nil
}

// firstUnresolvedDependency returns the first dependency (in component then
// dependency order) that blocks the proceed tag-cut, plus a short status
// string for the error message. It gates exactly two conditions and no others:
//
//   - an `org-service` dependency that is not namespace-visible — its computed
//     Status (from artifacts.resolveOrgServices) is unresolved, blocked, or
//     ambiguous. An empty Status (the resolver was never wired → fail-open)
//     does NOT block, and `resolved` does not block.
//   - an `external` dependency that declares needsSpec but has no collected
//     spec yet (SpecPath empty).
//
// external-values (config-only external) and platform-resource dependencies
// are deliberately NOT gated here — they are dispatch-gated in Phase 6. ok is
// false when every dependency is actionable (the common case).
func firstUnresolvedDependency(components []models.DesignComponent) (componentName, depName, status string, ok bool) {
	for _, c := range components {
		for _, dep := range c.Dependencies {
			switch dep.Kind {
			case models.DependencyKindOrgService:
				switch dep.Status {
				case models.DependencyStatusUnresolved, models.DependencyStatusBlocked, models.DependencyStatusAmbiguous:
					return c.Name, dep.Name, dep.Status, true
				}
			case models.DependencyKindExternal:
				if dep.NeedsSpec && strings.TrimSpace(dep.SpecPath) == "" {
					return c.Name, dep.Name, "needs-spec", true
				}
			}
		}
	}
	return "", "", "", false
}

func (s *designService) DiscardChanges(ctx context.Context, orgID, projectID string) (*models.Design, error) {
	if s.artifactSvc == nil {
		return nil, fmt.Errorf("git client not configured")
	}
	if _, err := s.artifactSvc.DiscardDesign(ctx, orgID, projectID); err != nil {
		return nil, fmt.Errorf("discard design: %w", err)
	}
	return s.GetDesign(ctx, orgID, projectID)
}

func (s *designService) ListDesignVersions(ctx context.Context, orgID, projectID string) ([]models.ArtifactVersion, error) {
	if s.artifactSvc == nil {
		return nil, nil
	}
	v, err := s.artifactSvc.ListDesignVersions(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list design versions: %w", err)
	}
	return mapDesignVersions(v), nil
}

// decodeDesignTag parses a `v<N>-<M>` design tag and returns (parent, revision, ok).
// Lives in this file so callers don't have to import the artifact tag helpers.
var designTagPattern = regexp.MustCompile(`^v(\d+)-(\d+)$`)

func decodeDesignTag(tag string) (int, int, bool) {
	m := designTagPattern.FindStringSubmatch(tag)
	if m == nil {
		return 0, 0, false
	}
	var n, r int
	if _, err := fmt.Sscanf(m[1], "%d", &n); err != nil || n < 1 {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(m[2], "%d", &r); err != nil || r < 1 {
		return 0, 0, false
	}
	return n, r, true
}
