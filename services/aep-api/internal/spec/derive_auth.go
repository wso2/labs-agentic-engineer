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
	"fmt"
	"sort"
	"strings"
)

// Auth-as-platform-resource (learning/thunder-resource/PLAN-generalization.md):
// a `service` component that declares a `platform-resource` dependency whose
// ClusterResourceType carries the PE-authored `aep.wso2.com/role:
// end-user-auth` label (CRTType.EndUserAuth) gets end-user
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
func deriveEndUserAuth(components []DesignComponent, markers map[string]CRTType) error {
	for i := range components {
		comp := &components[i]
		if comp.ComponentType != ComponentTypeService {
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
			comp.ExposesAPI = &ExposesAPI{}
		}
		comp.ExposesAPI.Auth = authEndUserRequired
	}
	return nil
}

// endUserAuthDependency returns the first platform-resource dependency in deps
// whose resourceType carries the end-user-auth role marker, if any. A nil
// markers map (a Go nil-map read is a safe zero-value lookup) matches nothing.
func endUserAuthDependency(deps []Dependency, markers map[string]CRTType) (Dependency, bool) {
	for _, d := range deps {
		if d.Kind == DependencyKindPlatformResource && markers[d.ResourceType].EndUserAuth {
			return d, true
		}
	}
	return Dependency{}, false
}

// resourceTypesForDerivation returns the installed resource types BOTH design-save
// derivations key on — the markers for end-user auth, the declared outputs for
// dependency wiring. It makes at most ONE OC catalog call, and ONLY when the
// design declares a platform-resource dependency — a design with none never
// touches the catalog. Fail-closed: when the design DOES declare a
// platform-resource dependency but the catalog is unreachable (or unwired), the
// save must stop with ErrResourceCatalogUnavailable rather than silently skip the
// derivations (a silent skip could leave an API that must sit behind end-user
// login exposed, and would ship a component with no wiring for a resource it
// declares). Returns (nil, nil) when there is no platform-resource dependency:
// both derivations over a nil map qualify nothing, which is exactly right — an
// external dependency's wiring comes off its own config keys, not the catalog.
func (s *designService) resourceTypesForDerivation(ctx context.Context, designFile *DesignFile) (map[string]CRTType, error) {
	if !hasPlatformResourceDependency(designFile.Components) {
		return nil, nil
	}
	if s.resourceCatalog == nil {
		return nil, fmt.Errorf("%w: no resource-type catalog wired", ErrResourceCatalogUnavailable)
	}
	types, err := s.resourceCatalog.ResourceTypesByName(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrResourceCatalogUnavailable, err)
	}
	return types, nil
}

// hasPlatformResourceDependency reports whether any component declares at least
// one platform-resource dependency — the gate on whether design-save fetches
// the CRT marker catalog at all.
func hasPlatformResourceDependency(components []DesignComponent) bool {
	for i := range components {
		for _, d := range components[i].Dependencies {
			if d.Kind == DependencyKindPlatformResource {
				return true
			}
		}
	}
	return false
}

// rejectUnknownResourceTypes fails when any platform-resource dependency names
// a resourceType absent from the installed CRT catalog. An empty or nil catalog
// (PLATFORM_RESOURCES_ENABLED=false) skips membership — fail-open for the
// disabled path. Membership is against the live catalog map, never a hardcoded
// resourceType name (ADR-0007).
func rejectUnknownResourceTypes(components []DesignComponent, types map[string]CRTType) error {
	if len(types) == 0 {
		return nil
	}
	available := make([]string, 0, len(types))
	for name := range types {
		available = append(available, name)
	}
	sort.Strings(available)
	var unknown []string
	for i := range components {
		for _, d := range components[i].Dependencies {
			if d.Kind != DependencyKindPlatformResource {
				continue
			}
			if _, ok := types[d.ResourceType]; !ok {
				unknown = append(unknown, fmt.Sprintf("%s (%q)", d.Name, d.ResourceType))
			}
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s; available: %s",
		ErrUnknownResourceType, strings.Join(unknown, ", "), strings.Join(available, ", "))
}

// exposesAPIEqual reports whether two (possibly nil) ExposesAPI pointers
// describe the same value — used to detect which components deriveEndUserAuth
// actually changed, so persistPlatformResourceDerivation commits only those.
func exposesAPIEqual(a, b *ExposesAPI) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
