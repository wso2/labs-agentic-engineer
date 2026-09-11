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
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/spec"
)

// PreflightDesignReader exposes the project's authored design components at
// HEAD — the same port the funnel's dispatch-time re-verification uses.
// Satisfied by the app-root designComponents adapter
// (internal/app/tasks_adapters.go).
type PreflightDesignReader interface {
	ReadDesignComponents(ctx context.Context, orgID, projectID string) ([]spec.DesignComponent, error)
}

// ProvisionStatusReader reports whether a dependency is already handled — no
// longer something preflight should report: it is a TRI-STATE collapsed to a bool — true when the dependency is
// already provisioned OR provisioning is in-flight, false only when nothing
// has been started yet. This is a DIFFERENT semantics than
// provisioning.Service.Status's DependencyStatus.Ready: that field is
// `b.IsReady()` (internal/feature/provisioning/status_service.go:53), which is
// false for BOTH Status:"unknown" (nothing started) and Status:"provisioning"
// (in-flight) — it does not collapse the tri-state on its own. The real
// adapter implementing this interface must therefore NOT simply return
// DependencyStatus.Ready; it must treat any non-"unknown" Status (e.g.
// "provisioning") as "already handled" so preflight does not re-report a
// dependency that is already in flight.
type ProvisionStatusReader interface {
	Ready(ctx context.Context, orgID, projectID, depName string) (bool, error)
}

// OrgCatalogReader reports whether a logical name already holds values on the
// org catalog plane (Registered External — non-empty env cells). Nil means
// fail-open: treat the name as Project External and still emit external-config.
type OrgCatalogReader interface {
	HasOrgEnvCells(ctx context.Context, orgID, name string) bool
}

// --- wire shapes (names drive the generated schema names — keep them exactly
// --- BuildPreflight / PreflightItem / ConfigKeyView) ------------------------

// ConfigKeyView is the key/secret view of an external dependency's config
// schema — never values. Mirrors spec.ConfigKey: a client rendering the schema
// needs the key, whether it is secret-routed, the optional description to render
// as a hint, and the optional (non-secret) defaultValue to pre-fill the field.
type ConfigKeyView struct {
	Key          string `json:"key"`
	Secret       bool   `json:"secret,omitempty"`
	Description  string `json:"description,omitempty"`
	DefaultValue string `json:"defaultValue,omitempty"`
}

// PreflightItem is one thing a version's dependencies still need: a single
// dependency (or one facet of a dependency — external deps can raise both a
// spec and a config item) that is not yet settled.
//
// Two families, with very different urgency:
//
//   - RESOLUTION blockers — external-unresolved,
//     external-spec, org-service — are things the design itself cannot
//     answer. They gate the version cut: nothing downstream can be authored
//     while the dependency has no identity.
//   - external-config and platform-resource are NOT gates. Their values /
//     approvals are collected alongside the running build and enforced at the
//     deploy gate, so a build starts without them.
type PreflightItem struct {
	Component   string `json:"component" doc:"Owning component name"`
	Dependency  string `json:"dependency" doc:"Dependency name"`
	Kind        string `json:"kind" enum:"external-config,external-spec,external-unresolved,platform-resource,org-service"`
	Description string `json:"description"`
	// external-config only: the key/secret schema whose values are collected
	// while the build runs — views only, never values.
	Config []ConfigKeyView `json:"config,omitempty"`
	// platform-resource only: the registered (Cluster)ResourceType + the
	// design-authored provisioning defaults.
	ResourceType string         `json:"resourceType,omitempty"`
	Parameters   map[string]any `json:"parameters,omitempty"`
}

// BuildPreflight is the get-build-preflight response: what a version's
// dependencies still need, and which of it actually blocks the cut.
//
// NeedsInput is the broad "there is something to show" flag — any item at all.
// It does NOT gate Build. NeedsResolution is the only gate: true when at least
// one item is a resolution blocker (see PreflightItem). External config values
// are collected on the Builds page while the coding agent runs and are enforced
// at the deploy gate, so they never hold up starting a build.
type BuildPreflight struct {
	NeedsInput      bool            `json:"needsInput"`
	NeedsResolution bool            `json:"needsResolution"`
	Items           []PreflightItem `json:"items"`
	// The version half of the same answer (console ADR-0029/ADR-0030): what the
	// newest version is called, what to prefill the name field with, whether the
	// tree has moved at all, and what this version would change. Empty when the
	// version read could not be made — a degraded dialog, never a refused build.
	CurrentVersion   string        `json:"currentVersion,omitempty"`
	SuggestedVersion string        `json:"suggestedVersion,omitempty"`
	SpecUnchanged    bool          `json:"specUnchanged,omitempty"`
	Changes          []BuildChange `json:"changes,omitempty"`
}

// BuildChange is one row of the Start build dialog's change list: what this
// version does to one component, dependency, resource, or to the requirements.
type BuildChange struct {
	Name  string `json:"name"`
	Kind  string `json:"kind" enum:"component,external,platform-resource,requirements"`
	State string `json:"state" enum:"new,changed,removed"`
}

// VersionFactsReader reports what the next version would be called and carry.
// Satisfied by the app-root adapter over spec.ArtifactService. Nil leaves the
// version half of the preflight empty — the feature unwired, not broken.
type VersionFactsReader interface {
	BuildVersionFacts(ctx context.Context, orgID, projectID string) (spec.VersionFacts, error)
}

// resolutionBlockerKinds are the item kinds that gate the version cut: the
// design cannot name the dependency yet, so nothing downstream can be authored
// for it. Every other kind is collected-later, deploy-gated work.
var resolutionBlockerKinds = map[string]bool{
	"external-unresolved": true,
	"external-spec":       true,
	"org-service":         true,
}

// PreflightDeps carries the preflight service's ports.
type PreflightDeps struct {
	Design   PreflightDesignReader
	Status   ProvisionStatusReader
	Catalog  OrgCatalogReader
	Versions VersionFactsReader
}

// PreflightService computes the build preflight from the design at HEAD —
// what the project's dependencies still need before the version can ship —
// filtering out anything already provisioned or in-flight. It reports, never
// collects: resolution blockers gate the version cut, while external config
// values are gathered on the Builds page and enforced at the deploy gate.
type PreflightService struct {
	design   PreflightDesignReader
	status   ProvisionStatusReader
	catalog  OrgCatalogReader
	versions VersionFactsReader
}

// NewPreflightService wires the preflight service.
func NewPreflightService(d PreflightDeps) *PreflightService {
	return &PreflightService{design: d.Design, status: d.Status, catalog: d.Catalog, versions: d.Versions}
}

// Preflight walks every component's dependencies at HEAD — service AND
// web-application alike (#252 Task 14: a web-application's unresolved
// dependency needs the same reporting/gate treatment as a service's, since
// Task 9 already surfaces its status chips and the coding-agent wiring already
// emits consumed-spec instructions for it) — and emits an item for each
// dependency that is not yet settled and not already provisioned or in-flight:
//
//   - external: a blocker item — "external-unresolved" (the user has not
//     chosen a service, or must accept an assumption), or
//     "external-spec" (no API spec yet) — when the dependency's already
//     computed Status/Reason (spec.ComputeDependencyStatus, via
//     dependencyBlocker) says so; this is the dependency-management proceed
//     gate Task 1 orphaned, reborn here. Otherwise, an "external-config" item
//     (key/secret views only) when the dependency is not yet Ready and is not
//     a Registered External (org catalog already holds env cells — nothing
//     should re-collect secrets that live on the org record).
//   - platform-resource: a "platform-resource" item when not yet Ready.
//   - org-service: an "org-service" item when Status is one of the three
//     non-resolved resolution states (unresolved | blocked);
//     resolved dependencies never surface here.
//   - component (sibling components): never emitted — not provisioned per
//     project.
//
// NeedsResolution is then true iff at least one emitted item is a resolution
// blocker; NeedsInput stays the broad "any item at all" flag.
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
	for _, it := range items {
		if resolutionBlockerKinds[it.Kind] {
			needsResolution = true
			break
		}
	}

	out := BuildPreflight{NeedsInput: len(items) > 0, NeedsResolution: needsResolution, Items: items}

	// What the click is about to DO, beside what it still needs. It is the same
	// answer to the same press, so it rides the same response rather than a
	// second request the console would have to wait on (console ADR-0029).
	//
	// Best-effort: a version read that fails costs the dialog its name field and
	// its change list, which is a degraded dialog — refusing the whole preflight
	// would instead refuse the BUILD, over a list nobody has to act on.
	if s.versions != nil {
		facts, ferr := s.versions.BuildVersionFacts(ctx, orgID, projectID)
		if ferr != nil {
			slog.WarnContext(ctx, "preflight: version facts read failed",
				"project", projectID, "error", ferr)
			return out, nil
		}
		out.CurrentVersion = facts.CurrentVersion
		out.SuggestedVersion = facts.SuggestedVersion
		out.SpecUnchanged = facts.SpecUnchanged
		out.Changes = make([]BuildChange, 0, len(facts.Changes))
		for _, c := range facts.Changes {
			out.Changes = append(out.Changes, BuildChange{Name: c.Name, Kind: c.Kind, State: c.State})
		}
	}
	return out, nil
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
		return nil, nil // sibling components are not provisioned per project.
	default:
		return nil, nil
	}
}

// externalItems computes the preflight item(s) for one external dependency.
// dependencyBlocker (the single mapping the build hard-gate also uses) checks
// FIRST: an unresolved dependency raises exactly one blocker item
// (external-unresolved / external-spec) with a
// plain-language Description, and config collection is skipped — there is
// nothing meaningful to collect until the dependency itself resolves (a
// still-unresolved dependency has no derived config keys yet). Once
// resolved (or when no resolver was ever wired — the fail-open empty Status),
// the pre-existing external-config item (key/secret views only) is emitted
// when the dependency is not yet Ready and is not Registered (org catalog
// env cells). The config schema stays a provisioning-readiness concern local
// to this preflight, not part of ComputeDependencyStatus. Registered names
// still author from org cells at POST /build; they just do not surface here.
func (s *PreflightService) externalItems(ctx context.Context, orgID, projectID, componentName string, d spec.Dependency) ([]PreflightItem, error) {
	if kind, desc, blocked := dependencyBlocker(d); blocked {
		return []PreflightItem{{
			Kind:        kind,
			Component:   componentName,
			Dependency:  d.Name,
			Description: desc,
		}}, nil
	}
	if s.catalog != nil && s.catalog.HasOrgEnvCells(ctx, orgID, d.Name) {
		// Org cells already hold values; project Ready is irrelevant until
		// POST /build authors the instance from those cells.
		return nil, nil
	}
	ready, err := s.status.Ready(ctx, orgID, projectID, d.Name)
	if err != nil {
		return nil, err
	}
	if ready {
		return nil, nil
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
	case spec.DependencyStatusUnresolved, spec.DependencyStatusBlocked:
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
