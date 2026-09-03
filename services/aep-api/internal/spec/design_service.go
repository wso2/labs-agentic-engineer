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
	"strings"
)

// ErrSpecNotApproved is the design-domain sentinel surfaced (as 409 by the
// controller) when a design is saved before the requirements spec has been
// approved (tagged). Owned by the design feature — it is the only consumer.
var ErrSpecNotApproved = errors.New("spec must be saved (tagged) before generating a design")

// ErrUnresolvedDependency is the design-domain sentinel surfaced (as 409 by the
// controller) when the tag-cut (SaveAndProceed) is attempted while a
// dependency in the design being tagged is still in a non-actionable state:
// an `org-service` that is not namespace-visible (unresolved/blocked/ambiguous
// against the live org catalog — see resolveOrgServices). external
// dependencies are NOT proceed-gated here for now: the needsSpec flag that
// used to drive this was dropped (dependency-management schema revision —
// derived-state model, see design_json.go); the equivalent check is reborn
// once the shared resolver (a later task) can derive an external dependency's
// resolution state from style/specPath against the live catalog.
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

// ErrUnknownResourceType is returned when a platform-resource dependency names
// a resourceType that is not in the installed ClusterResourceType catalog.
// Surfaced as 409 at build claim — the design is unsatisfiable on this cluster,
// and no version tag or Temporal run is created.
var ErrUnknownResourceType = errors.New("resourceType is not installed on this cluster")

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

// designService is the write surface for the multi-file design artifact
// stored under `specs/design/`. Per the GitHub-direct rework
// (docs/design/agents-generation-migration.md §12.2) the per-file PUT/DELETE,
// component delete, and the architect generate stream are gone — edits are
// frontend drafts committed via the Files API, and generation is the unified
// genai turn endpoint. The read + version HTTP surface (get-design-bundle,
// get-design-bundle-at-tag, discard-design-changes, list-design-versions) was
// removed outright — superseded by the Files API (list-files/read-file). The
// hard save gate (SaveAndProceed → tag at HEAD) was retired with it: tagging is
// the single-tag POST /build flow now, so the design feature is a write helper,
// not a gate. What remains: CollectSpec (dependency spec collection), the read
// model ListDependencies, and the pre-tag step the thin POST /build path reuses
// — DerivePlatformResourceFactsAtHead (issue #164). There is no exported interface —
// every caller holds the concrete type; the composition root adapts the
// build-path method onto a narrow consumer interface
// (internal/app/build_adapters.go) since build cannot import design.
type designService struct {
	store           *ArtifactStore
	artifactSvc     ArtifactService
	fileCommitter   designFileCommitter // for CollectSpec's committed-truth spec write; may be nil
	resourceCatalog resourceTypeCatalog // for DerivePlatformResourceFactsAtHead end-user-auth derivation; may be nil (fails closed when platform-resource deps exist)
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

// resourceTypeCatalog is design_service's narrow consumer port over the
// dependencies/resources catalog: it returns the installed resource types
// (CRTType keyed by resourceType name) that design-save's platform-resource
// derivation reads — the PE-authored markers the end-user-auth derivation keys on
// (replacing the deleted hardcoded thunder-app name), and the declared outputs
// the wiring derivation turns into env-var names.
// *resources.ResourceTypeCatalog satisfies it structurally. Wired via
// SetResourceCatalog at the composition root; a nil catalog fails the save
// closed (ErrResourceCatalogUnavailable) whenever the design declares a
// platform-resource dependency, so neither derivation is ever silently skipped.
type resourceTypeCatalog interface {
	ResourceTypesByName(ctx context.Context) (map[string]CRTType, error)
}

func NewDesignService(
	store *ArtifactStore,
	artifactSvc ArtifactService,
) *designService {
	return &designService{
		store:       store,
		artifactSvc: artifactSvc,
	}
}

// SetFileCommitter wires the committed-truth Files commit surface CollectSpec
// uses to persist a consumed spec + the design.json specPath edit. A nil
// committer makes CollectSpec fail (the route then returns 503).
func (s *designService) SetFileCommitter(c designFileCommitter) {
	s.fileCommitter = c
}

// SetResourceCatalog wires the CRT marker lookup DerivePlatformResourceFactsAtHead uses to
// decide which platform-resource dependencies stamp end-user auth. A nil catalog
// makes the derivation fail closed (ErrResourceCatalogUnavailable) whenever the
// design declares a platform-resource dependency; auth-free designs are unaffected.
func (s *designService) SetResourceCatalog(c resourceTypeCatalog) {
	s.resourceCatalog = c
}

// ListDependencies returns every component's dependencies in the design at
// HEAD, each already carrying its read-time computed Status/Reason —
// ReadDesign → AssembleDesignFrom runs the full resolution pass (both
// resolveOrgServices and the external/component/platform-resource pass, both
// via ComputeDependencyStatus, the single resolution authority) before
// returning, so this is a plain projection, not a second resolution pass. The
// single read model behind the console's dependency-status surface (GET
// /projects/{projectName}/design/dependencies). Returns (nil,
// ErrDesignNotFound) when no design exists yet.
func (s *designService) ListDependencies(ctx context.Context, orgID, projectID string) ([]DesignComponent, error) {
	design, err := s.store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("read design: %w", err)
	}
	if design == nil {
		return nil, ErrDesignNotFound
	}
	return design.Components, nil
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
		if IsNotFound(err) {
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
	if kind := design.Components[compIdx].Dependencies[depIdx].Kind; kind != DependencyKindExternal {
		return "", fmt.Errorf("%w: dependency %q on component %q has kind %q; spec collection applies only to %q",
			ErrDependencyWrongKind, depName, component, kind, DependencyKindExternal)
	}

	if hasURL {
		fetched, ferr := FetchSpecFromURL(ctx, specURL)
		if ferr != nil {
			return "", fmt.Errorf("%w: %v", ErrSpecFetchFailed, ferr)
		}
		rawSpec = fetched
	}

	// Validate + normalize; StoreConsumedSpec returns the component-relative
	// specPath and the normalized blob to commit.
	specPath, normalized, err := s.store.StoreConsumedSpec(ctx, orgID, projectID, component, depName, rawSpec)
	if err != nil {
		if errors.Is(err, ErrInvalidSpecContent) {
			return "", fmt.Errorf("%w: %v", ErrInvalidSpec, err)
		}
		return "", err
	}

	// Record specPath, then render ONLY this component's design.json through the
	// canonical codec.
	comp := design.Components[compIdx]
	comp.Dependencies[depIdx].SpecPath = specPath
	rendered, rerr := SplitDesign(&DesignFile{Components: []DesignComponent{comp}})
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
	designFull := DesignDir + "/" + designSub
	specFull := DesignDir + "/components/" + component + "/" + specPath
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
		if IsNotFound(err) {
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
		comp.ExposesAPI = &ExposesAPI{}
	}
	comp.ExposesAPI.OrgPublished = true

	rendered, err := SplitDesign(&DesignFile{Components: []DesignComponent{comp}})
	if err != nil {
		return fmt.Errorf("render component %q design.json: %w", component, err)
	}
	designSub := "components/" + component + "/design.json"
	content, ok := rendered[designSub]
	if !ok {
		return fmt.Errorf("render component %q design.json: %q missing from split", component, designSub)
	}
	designFull := DesignDir + "/" + designSub
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
