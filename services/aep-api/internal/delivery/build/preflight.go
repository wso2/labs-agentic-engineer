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

package build

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/spec"
)

// PreflightDesignReader exposes the project's authored design components at
// HEAD — the same port the funnel's dispatch-time re-verification uses.
// Satisfied by the app-root designComponents adapter
// (internal/app/tasks_adapters.go).
type PreflightDesignReader interface {
	ReadDesignComponents(ctx context.Context, orgID, projectID string) ([]spec.DesignComponent, error)
}

// ProvisionStatusReader reports whether a dependency no longer needs a
// preflight item: it is a TRI-STATE collapsed to a bool — true when the
// dependency is already provisioned OR provisioning is in-flight, false only
// when nothing has been started yet. This is a DIFFERENT semantics than
// provisioning.Service.Status's DependencyStatus.Ready: that field is
// `b.IsReady()` (internal/feature/provisioning/status_service.go:53), which is
// false for BOTH Status:"unknown" (nothing started) and Status:"provisioning"
// (in-flight) — it does not collapse the tri-state on its own. The real
// adapter implementing this interface must therefore NOT simply return
// DependencyStatus.Ready; it must treat any non-"unknown" Status (e.g.
// "provisioning") as "already handled" so preflight does not re-ask for a
// dependency that is already in flight.
type ProvisionStatusReader interface {
	Ready(ctx context.Context, orgID, projectID, depName string) (bool, error)
}

// --- wire shapes (names drive the generated schema names — keep them exactly
// --- BuildPreflight / PreflightItem / ConfigKeyView) ------------------------

// ConfigKeyView is the key/secret view of an external dependency's config
// schema — never values. Mirrors spec.ConfigKey so a schema consumer receives
// the key, whether it is secret-routed, the optional description to render as a
// hint, and the optional (non-secret) defaultValue to pre-fill the field.
type ConfigKeyView struct {
	Key          string `json:"key"`
	Secret       bool   `json:"secret,omitempty"`
	Description  string `json:"description,omitempty"`
	DefaultValue string `json:"defaultValue,omitempty"`
}

// PreflightItem is one dependency facet emitted by preflight. External
// dependencies can raise both a resolution item and a non-blocking config
// schema item; not every emitted item requires user action before Build.
type PreflightItem struct {
	Component   string `json:"component" doc:"Owning component name"`
	Dependency  string `json:"dependency" doc:"Dependency name"`
	Kind        string `json:"kind" enum:"external-config,external-spec,external-ambiguous,external-unresolved,platform-resource,org-service"`
	Description string `json:"description"`
	// external-config only: key/secret schema views, never values.
	Config []ConfigKeyView `json:"config,omitempty"`
	// platform-resource only: the registered (Cluster)ResourceType + the
	// design-authored provisioning defaults.
	ResourceType string         `json:"resourceType,omitempty"`
	Parameters   map[string]any `json:"parameters,omitempty"`
}

// BuildPreflight is the get-build-preflight response. NeedsInput reports
// whether preflight emitted items, while NeedsResolution reports whether any
// emitted item blocks design resolution before Build. Items carries both the
// schema-bearing and resolution-blocking entries.
type BuildPreflight struct {
	NeedsInput      bool            `json:"needsInput"`
	NeedsResolution bool            `json:"needsResolution"`
	Items           []PreflightItem `json:"items"`
}

// PreflightDeps carries the preflight service's ports.
type PreflightDeps struct {
	Design PreflightDesignReader
	Status ProvisionStatusReader
}

// PreflightService computes build dependency preflight from the design at HEAD,
// filtering out anything already provisioned or in-flight.
type PreflightService struct {
	design PreflightDesignReader
	status ProvisionStatusReader
}

// NewPreflightService wires the preflight service.
func NewPreflightService(d PreflightDeps) *PreflightService {
	return &PreflightService{design: d.Design, status: d.Status}
}

// Preflight walks every component's dependencies at HEAD — service AND
// web-application alike (#252 Task 14: a web-application's unresolved
// dependency needs the same preflight treatment as a service's, since Task 9
// already surfaces its status chips and the coding-agent wiring already emits
// consumed-spec instructions for it) — and emits items for dependencies that
// still need resolution, schema projection, or automatic provisioning. Legacy
// provisioned/in-flight status suppresses automatic platform provisioning, but
// never suppresses the resolved external schema projection:
//
//   - external: a blocker item — "external-ambiguous" (2+ candidates),
//     "external-unresolved" (needs information only the user can supply), or
//     "external-spec" (no API spec yet) — when the dependency's already
//     computed Status/Reason (spec.ComputeDependencyStatus, via
//     dependencyBlocker) says so; this is the dependency-management proceed
//     gate Task 1 orphaned, reborn here. Otherwise, an "external-config" item
//     (key/secret views only) regardless of legacy provisioning readiness: it
//     is the durable schema projection used to correct values after Build.
//   - platform-resource: a "platform-resource" item when not yet Ready.
//   - org-service: an "org-service" item when Status is one of the three
//     non-resolved resolution states (unresolved | blocked | ambiguous);
//     resolved dependencies never surface here.
//   - component (sibling components): never emitted — no dependency preflight
//     item is required.
//
// itemsFor switches purely on the dependency's own Kind — never the owning
// component's ComponentType — so this walk is component-kind-agnostic by
// construction; there is no per-component-type branch left to skip.
func (s *PreflightService) Preflight(ctx context.Context, orgID, projectID string) (BuildPreflight, error) {
	comps, err := s.design.ReadDesignComponents(ctx, orgID, projectID)
	if err != nil {
		return BuildPreflight{}, err
	}

	items := make([]PreflightItem, 0)
	for _, c := range comps {
		for _, d := range c.Dependencies {
			deps, err := s.itemsFor(ctx, orgID, projectID, c.Name, d)
			if err != nil {
				return BuildPreflight{}, err
			}
			items = append(items, deps...)
		}
	}

	needsResolution := false
	for _, item := range items {
		if itemNeedsResolution(item) {
			needsResolution = true
			break
		}
	}

	return BuildPreflight{
		NeedsInput:      len(items) > 0,
		NeedsResolution: needsResolution,
		Items:           items,
	}, nil
}

func itemNeedsResolution(item PreflightItem) bool {
	switch item.Kind {
	case "external-ambiguous", "external-unresolved", "external-spec", "org-service":
		return true
	default:
		return false
	}
}

// itemsFor computes the 0, 1, or 2 preflight items a single dependency raises.
func (s *PreflightService) itemsFor(ctx context.Context, orgID, projectID, componentName string, d spec.Dependency) ([]PreflightItem, error) {
	switch d.Kind {
	case spec.DependencyKindExternal:
		return s.externalItems(ctx, orgID, projectID, componentName, d)
	case spec.DependencyKindPlatformResource:
		return s.platformResourceItems(ctx, orgID, projectID, componentName, d)
	case spec.DependencyKindOrgService:
		return orgServiceItems(componentName, d), nil
	case spec.DependencyKindComponent:
		return nil, nil // Sibling components need no dependency preflight item.
	default:
		return nil, nil
	}
}

// externalItems computes the preflight item(s) for one external dependency.
// dependencyBlocker (the single mapping the build hard-gate also uses) checks
// FIRST: an ambiguous/unresolved dependency raises exactly one blocker item
// (external-ambiguous / external-unresolved / external-spec) with a
// plain-language Description, and schema projection is skipped — there is
// no meaningful config schema until the dependency itself resolves (a
// still-ambiguous/unresolved dependency has no derived config keys yet). Once
// resolved (or when no resolver was ever wired — the fail-open empty Status),
// an external-config item (key/secret views only) is always emitted. Legacy
// provisioning readiness does not suppress this schema: Builds needs it for
// later value correction. The item does not gate Build; value readiness remains
// separate from ComputeDependencyStatus.
func (s *PreflightService) externalItems(ctx context.Context, orgID, projectID, componentName string, d spec.Dependency) ([]PreflightItem, error) {
	if kind, desc, blocked := dependencyBlocker(d); blocked {
		return []PreflightItem{{
			Kind:        kind,
			Component:   componentName,
			Dependency:  d.Name,
			Description: desc,
		}}, nil
	}
	return []PreflightItem{{
		Kind:        "external-config",
		Component:   componentName,
		Dependency:  d.Name,
		Description: d.Description,
		Config:      toConfigKeyViews(d.Config),
	}}, nil
}

func (s *PreflightService) platformResourceItems(ctx context.Context, orgID, projectID, componentName string, d spec.Dependency) ([]PreflightItem, error) {
	ready, err := s.status.Ready(ctx, orgID, projectID, d.Name)
	if err != nil {
		return nil, err
	}
	if ready {
		return nil, nil
	}
	return []PreflightItem{{
		Kind:         "platform-resource",
		Component:    componentName,
		Dependency:   d.Name,
		Description:  d.Description,
		ResourceType: d.ResourceType,
		Parameters:   d.Parameters,
	}}, nil
}

func orgServiceItems(componentName string, d spec.Dependency) []PreflightItem {
	switch d.Status {
	case spec.DependencyStatusUnresolved, spec.DependencyStatusBlocked, spec.DependencyStatusAmbiguous:
		return []PreflightItem{{
			Kind:        "org-service",
			Component:   componentName,
			Dependency:  d.Name,
			Description: d.Description,
		}}
	default:
		return nil
	}
}

func toConfigKeyViews(keys []spec.ConfigKey) []ConfigKeyView {
	if len(keys) == 0 {
		return nil
	}
	out := make([]ConfigKeyView, 0, len(keys))
	for _, k := range keys {
		out = append(out, ConfigKeyView{Key: k.Key, Secret: k.Secret, Description: k.Description, DefaultValue: k.DefaultValue})
	}
	return out
}
