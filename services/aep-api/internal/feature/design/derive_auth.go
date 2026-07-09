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
	"fmt"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/internal/feature/dependencies/resources"
	"github.com/wso2/aep/aep-api/models"
)

// Auth-as-platform-resource (learning/thunder-resource/PLAN-generalization.md):
// a `service` component that declares a `platform-resource` dependency whose
// ClusterResourceType carries the PE-authored `aep.wso2.com/role:
// end-user-auth` label (resources.TypeMarkers.EndUserAuth) gets end-user
// gateway auth on its managed API for free — the platform derives
// `exposesAPI.auth` from the dependency instead of requiring the architect (or
// a human editor) to author it separately. The membership test keys on the CRT
// role MARKER, never on a hardcoded resourceType name: adding a new auth
// flavor is a cluster install (a new labeled CRT), not an app-factory release.
// The two must never disagree: an explicit `service-required` on such a
// component is a self-contradiction (the dependency says "this API sits behind
// the end-user login the SPA performs"; the flag says "no end-user ever reaches
// this API") and is rejected as a validation error rather than silently
// overridden.
const (
	authEndUserRequired = "end-user-required"
	authServiceRequired = "service-required"
)

// deriveEndUserAuth stamps exposesAPI.auth=end-user-required on service
// components that declare a platform-resource dependency whose resourceType
// carries the end-user-auth role marker (markers[resourceType].EndUserAuth),
// and rejects an explicit conflicting service-required as a validation error.
// Mutates components in place. web-app components and services with no
// qualifying dependency (including a platform-resource dependency of a type
// that carries NO end-user-auth marker, e.g. postgres-cnpg) are left completely
// untouched: SPAs aren't gateway-exposed managed APIs, and a bare
// dependency-less/differently-marked service has nothing to derive from. A nil
// markers map (no platform-resource deps → no catalog fetch) qualifies nothing.
// On a conflict, nothing in components is mutated — the caller sees the
// original, unmodified state.
func deriveEndUserAuth(components []models.DesignComponent, markers map[string]resources.TypeMarkers) error {
	for i := range components {
		comp := &components[i]
		if comp.ComponentType != models.ComponentTypeService {
			continue
		}
		dep, ok := endUserAuthDependency(comp.Dependencies, markers)
		if !ok {
			continue
		}
		if comp.ExposesAPI != nil && comp.ExposesAPI.Auth == authServiceRequired {
			return fmt.Errorf(
				"component %q: dependency %q (platform-resource, resourceType %q) requires exposesAPI.auth=%q, but the component explicitly declares exposesAPI.auth=%q",
				comp.Name, dep.Name, dep.ResourceType, authEndUserRequired, comp.ExposesAPI.Auth,
			)
		}
		if comp.ExposesAPI == nil {
			comp.ExposesAPI = &models.ExposesAPI{}
		}
		comp.ExposesAPI.Auth = authEndUserRequired
	}
	return nil
}

// endUserAuthDependency returns the first platform-resource dependency in deps
// whose resourceType carries the end-user-auth role marker, if any. A nil
// markers map (a Go nil-map read is a safe zero-value lookup) matches nothing.
func endUserAuthDependency(deps []models.Dependency, markers map[string]resources.TypeMarkers) (models.Dependency, bool) {
	for _, d := range deps {
		if d.Kind == models.DependencyKindPlatformResource && markers[d.ResourceType].EndUserAuth {
			return d, true
		}
	}
	return models.Dependency{}, false
}

// resourceMarkersForAuthDerivation returns the CRT marker map the auth
// derivation keys on. It makes at most ONE OC catalog call, and ONLY when the
// design declares a platform-resource dependency — auth-free saves never touch
// the catalog. Fail-closed: when the design DOES declare a platform-resource
// dependency but the catalog is unreachable (or unwired), the save must stop
// with ErrResourceCatalogUnavailable rather than silently skip the derivation
// (a silent skip could leave an API that must sit behind end-user login
// exposed). Returns (nil, nil) when there is no platform-resource dependency:
// deriveEndUserAuth over a nil map qualifies nothing, which is exactly right.
func (s *designService) resourceMarkersForAuthDerivation(ctx context.Context, designFile *artifacts.DesignFile) (map[string]resources.TypeMarkers, error) {
	if !hasPlatformResourceDependency(designFile.Components) {
		return nil, nil
	}
	if s.resourceCatalog == nil {
		return nil, fmt.Errorf("%w: no resource-type catalog wired", ErrResourceCatalogUnavailable)
	}
	markers, err := s.resourceCatalog.MarkersByName(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrResourceCatalogUnavailable, err)
	}
	return markers, nil
}

// hasPlatformResourceDependency reports whether any component declares at least
// one platform-resource dependency — the gate on whether design-save fetches
// the CRT marker catalog at all.
func hasPlatformResourceDependency(components []models.DesignComponent) bool {
	for i := range components {
		for _, d := range components[i].Dependencies {
			if d.Kind == models.DependencyKindPlatformResource {
				return true
			}
		}
	}
	return false
}

// exposesAPIEqual reports whether two (possibly nil) ExposesAPI pointers
// describe the same value — used to detect which components deriveEndUserAuth
// actually changed, so persistEndUserAuthDerivation commits only those.
func exposesAPIEqual(a, b *models.ExposesAPI) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// persistEndUserAuthDerivation runs deriveEndUserAuth over designFile's
// components and, for every component it stamps, commits the updated
// design.json to main via the committed-truth write surface (the same
// designFileCommitter port + per-component SplitDesign render CollectSpec
// uses). It runs from SaveAndProceed BEFORE the tag-cut, so an auth-labeled
// dependency's derived exposesAPI.auth is already on disk — and therefore
// already what the NEXT design read sees (EnsureComponent's create-time trait
// derivation, component.TraitSyncService, the Explorer) — by the time
// anything downstream (dispatch, OC) acts on the tagged design.
//
// Returns (true, nil) when at least one commit landed — the caller must then
// re-resolve HEAD (its designFile + any pinned commitSHA are now stale), the
// same convention SaveAndProceed's auto-fetch-on-save step already follows.
// Returns a non-nil error (wrapping ErrEndUserAuthConflict) with NO commit
// attempted when deriveEndUserAuth itself rejects the design — the save must
// stop there, exactly like the unresolved-dependency proceed-gate.
//
// A nil fileCommitter (degraded boot — mirrors CollectSpec) is a best-effort
// no-op after a successful derivation: designFile.Components is still mutated
// in place so THIS response reflects the derived value, but nothing is
// persisted, so it will not survive to the next independent design read.
func (s *designService) persistEndUserAuthDerivation(ctx context.Context, orgID, projectID string, designFile *artifacts.DesignFile, markers map[string]resources.TypeMarkers) (bool, error) {
	// Snapshot a COPY of each ExposesAPI value (not the pointer): when a
	// component already carries a non-nil ExposesAPI, deriveEndUserAuth
	// mutates its Auth field THROUGH that same pointer — capturing the
	// pointer itself here would alias the post-mutation value and the
	// change-detection below would never see a diff.
	before := make([]*models.ExposesAPI, len(designFile.Components))
	for i, c := range designFile.Components {
		if c.ExposesAPI != nil {
			v := *c.ExposesAPI
			before[i] = &v
		}
	}
	if err := deriveEndUserAuth(designFile.Components, markers); err != nil {
		return false, fmt.Errorf("%w: %v", ErrEndUserAuthConflict, err)
	}
	if s.fileCommitter == nil {
		return false, nil
	}

	var writes []DesignFileWrite
	for i := range designFile.Components {
		if exposesAPIEqual(before[i], designFile.Components[i].ExposesAPI) {
			continue
		}
		comp := designFile.Components[i]
		rendered, rerr := artifacts.SplitDesign(&artifacts.DesignFile{Components: []models.DesignComponent{comp}})
		if rerr != nil {
			return false, fmt.Errorf("render component %q design.json: %w", comp.Name, rerr)
		}
		designSub := "components/" + comp.Name + "/design.json"
		content, ok := rendered[designSub]
		if !ok {
			return false, fmt.Errorf("render component %q design.json: %q missing from split", comp.Name, designSub)
		}
		designFull := artifacts.DesignDir + "/" + designSub
		_, sha, exists, rerr := s.fileCommitter.ReadFile(ctx, orgID, projectID, designFull)
		if rerr != nil {
			return false, fmt.Errorf("read %q for CAS: %w", designFull, rerr)
		}
		if !exists {
			return false, fmt.Errorf("component %q design.json missing on disk", comp.Name)
		}
		writes = append(writes, DesignFileWrite{Path: designFull, Content: content, BaseSHA: sha})
	}
	if len(writes) == 0 {
		return false, nil
	}
	if err := s.fileCommitter.Commit(ctx, orgID, projectID, writes,
		"Derive exposesAPI.auth from platform-resource dependency"); err != nil {
		return false, err
	}
	slog.InfoContext(ctx, "design save: derived end-user-required auth persisted",
		"org", orgID, "project", projectID, "components", len(writes))
	return true, nil
}
