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
	"strings"

	"github.com/wso2/aep/aep-api/internal/contracts"
)

// Component type vocabulary. AEP uses OpenChoreo's OWN terms end-to-end —
// these are OC's ComponentType names minus the `deployment/` prefix (OC:
// `deployment/service`, `deployment/web-application`), so no translation
// layer exists anywhere: the agent contract emits these values, design.json
// stores them verbatim, and platform code compares them directly.
const (
	ComponentTypeService        = "service"
	ComponentTypeWebApplication = "web-application"
)

// DesignComponent describes a single component within a design.
// This matches the structured output schema from the AI Agent SDK.
type DesignComponent struct {
	Name          string       `json:"name"`
	ComponentType string       `json:"componentType"`
	Version       string       `json:"version,omitempty"`
	Language      string       `json:"language"`
	Dependencies  []Dependency `json:"dependencies"`
	Entrypoint    string       `json:"entrypoint"`
	Buildpack     string       `json:"buildpack"`
	AppPath       string       `json:"appPath"`
	// Exposure is the gateway exposure of the component's endpoint
	// ("internet" | "intranet"). It is distinct from ExposesAPI (which is the
	// managed-API auth policy) — a component can be intranet-exposed without a
	// managed API and vice versa. Sourced from the design.json `exposure` key.
	Exposure string `json:"exposure,omitempty"`
	// Stories are the PRD story numbers this component serves (#369) —
	// platform-recomputed from the cell's citations on save; read here so
	// traceability surfaces and the codec round-trip carry them.
	Stories []int `json:"stories,omitempty"`
	// Description is the single-responsibility prose (what the component does /
	// does NOT do) — the design.json `description` key. This is the successor to
	// the per-component design.md markdown body.
	Description string `json:"description,omitempty"`
	// Endpoint is the component's single network endpoint, sourced from the
	// design.json `endpoint` key. Only its Name is declared — the shared key
	// the coding agent's workload.yaml and the platform's api-configuration
	// trait both reference. Nil when the design declares no endpoint; callers
	// use EndpointName() to get the effective name (defaulting to "http").
	Endpoint                   *ComponentEndpoint `json:"endpoint,omitempty"`
	OpenAPISpec                string             `json:"openAPISpec"`
	ComponentAgentInstructions string             `json:"componentAgentInstructions"`
	ExposesAPI                 *ExposesAPI        `json:"exposesAPI,omitempty"`
	// SkillsPinned are the skill names applied to THIS component (per-component
	// — the coding runner materializes exactly these when building it). Sourced
	// from the component design.json `skillsPinned` key.
	SkillsPinned []string `json:"skillsPinned,omitempty"`
	// DisableAutoRca opts this component out of the platform's default
	// "error → RCA" observability-alert-rule trait (auto-provisioned for
	// service components). Default false ⇒ auto-RCA on. Sourced from the
	// design.json `disableAutoRca` key. See ResolveAutoRCAEnabled.
	DisableAutoRca bool `json:"disableAutoRca,omitempty"`
}

// DefaultEndpointName is the conventional workload endpoint name the platform's
// managed-API (api-configuration) trait binds to when a component's design.json
// declares no explicit endpoint.
const DefaultEndpointName = "http"

const (
	// EndpointVisibilityProject is the reachability of a same-project sibling's
	// endpoint — the `visibility` on a workload `dependencies.endpoints[]` entry
	// pointing at one.
	EndpointVisibilityProject = "project"
	// EndpointAddressOutput is the single output an endpoint dependency exposes:
	// the provider's resolved base URL. It is the `envBindings` KEY on an
	// endpoints[] entry (the value is the env var it lands in).
	EndpointAddressOutput = "address"
)

// ComponentEndpoint is the component's single network endpoint as declared in
// design.json. Only Name is carried: it is the SINGLE SOURCE OF TRUTH for the
// endpoint name shared between the coding agent's workload.yaml
// (spec.endpoints[].name) and the api-configuration trait's endpointName. The
// port is deliberately NOT declared here — it stays in workload.yaml, chosen to
// match the app's actual listen port. Mirrors the agent-stream TS `Endpoint`.
type ComponentEndpoint struct {
	Name string `json:"name"`
}

// EndpointName returns the effective workload endpoint name for the component:
// the design.json `endpoint.name` when declared, otherwise the conventional
// DefaultEndpointName ("http"). This is the one place the default lives, so
// every consumer (trait emit, instance naming) agrees.
func (c DesignComponent) EndpointName() string {
	if c.Endpoint != nil && c.Endpoint.Name != "" {
		return c.Endpoint.Name
	}
	return DefaultEndpointName
}

// DependencyKind discriminates the unified Dependency entry. The wire type
// lives in the contracts leaf (so the generated contract package stays
// domain-free); the spec domain owns the kind consts + resolution algebra.
type DependencyKind = contracts.DependencyKind

const (
	DependencyKindComponent        DependencyKind = "component"
	DependencyKindOrgService       DependencyKind = "org-service"
	DependencyKindExternal         DependencyKind = "external"
	DependencyKindPlatformResource DependencyKind = "platform-resource"
)

// Dependency Status/Reason enum values. These are read-time computed (see
// the Dependency.Status/Reason doc below) — never authored, never persisted.
// ComputeDependencyStatus (dependency_status.go) is the single authority: for
// kind=org-service, namespace-visible → Resolved, catalog-visible elsewhere →
// Blocked/AccessRequired, absent → Unresolved/NotFound. For kind=external,
// 2+ Candidates → Ambiguous (no reason); a registry-known name → Resolved; no
// Style → Unresolved/NeedsInput; Style=rest-api with no SpecPath →
// Unresolved/NeedsSpec; Style=sdk with no Package → Unresolved/NeedsInput;
// otherwise Resolved. component/platform-resource are always Resolved here.
// The design-save proceed-gate (design.ErrUnresolvedDependency) blocks on all
// non-resolved states.
const (
	DependencyStatusResolved   = "resolved"
	DependencyStatusBlocked    = "blocked"
	DependencyStatusUnresolved = "unresolved"
	DependencyStatusAmbiguous  = "ambiguous"

	DependencyReasonAccessRequired = "access-required"
	DependencyReasonNotFound       = "not-found"
	// DependencyReasonNeedsSpec pairs with DependencyStatusUnresolved on an
	// external `style: rest-api` dependency with no specPath yet — the
	// contract-collection state (see ComputeDependencyStatus rule 4).
	DependencyReasonNeedsSpec = "needs-spec"
	// DependencyReasonNeedsInput pairs with DependencyStatusUnresolved on an
	// external dependency the platform cannot resolve without more from the
	// architect: no style at all, or an `sdk` style with no package yet (see
	// ComputeDependencyStatus rules 3 and 5).
	DependencyReasonNeedsInput = "needs-input"
)

// DependencyStyle is the closed set of external dependency shapes (mirrors the
// agent-stream TS `DependencyStyle`). Meaningful only on kind=external. The wire
// type lives in the contracts leaf; the style consts live here.
type DependencyStyle = contracts.DependencyStyle

const (
	DependencyStyleRestAPI DependencyStyle = "rest-api"
	DependencyStyleSDK     DependencyStyle = "sdk"
)

// Dependency is the unified, kind-discriminated dependency entry on a
// component. The wire shape lives in the contracts leaf (re-exported here) so
// the generated contract package can name it without importing this domain; the
// spec domain owns all behaviour over it (ComputeDependencyStatus, validators).
type Dependency = contracts.Dependency

// DependencyCandidate is one option in an ambiguous external dependency's
// resolution set (see Dependency.Candidates). Wire shape in the contracts leaf.
type DependencyCandidate = contracts.DependencyCandidate

// DependencyWiring is the platform-stamped consumer-side wiring for a component /
// platform-resource / external dependency (see derive_wiring.go). Wire shape in
// the contracts leaf.
type DependencyWiring = contracts.DependencyWiring

// EndpointWiring is the `endpoints[]` variant of a dependency's wiring — a
// sibling component's endpoint (see derive_wiring.go). Wire shape in the
// contracts leaf.
type EndpointWiring = contracts.EndpointWiring

// ConfigKey is one env-var key a component reads at runtime. Wire shape in the
// contracts leaf (re-exported here).
type ConfigKey = contracts.ConfigKey

// ComponentDependsOn returns the names of this component's sibling-component
// dependencies — the successor to the legacy DependsOn []string field. Used
// for task deploy-gating, runtime-config URL wiring, and task diffing.
func (c DesignComponent) ComponentDependsOn() []string {
	out := make([]string, 0, len(c.Dependencies))
	for _, d := range c.Dependencies {
		if d.Kind == DependencyKindComponent {
			out = append(out, d.Name)
		}
	}
	return out
}

// OrgServiceDependsOn returns the names of this component's cross-project
// `org-service` dependencies. Each name is the provider component name — the
// key the org endpoint catalog resolves to a namespace-visible endpoint. Used
// to wire the consumer Workload's OC WorkloadConnection post-deploy.
func (c DesignComponent) OrgServiceDependsOn() []string {
	out := make([]string, 0, len(c.Dependencies))
	for _, d := range c.Dependencies {
		if d.Kind == DependencyKindOrgService {
			out = append(out, d.Name)
		}
	}
	return out
}

// ProvisionDependsOn returns the names of this component's dependencies that
// gate dispatch on a provisioning run (dependency-management §3.6): external
// (config-collection) and platform-resource (resource-provisioning). org-service
// is gated at PROCEED, not dispatch, so it is excluded. Used by the funnel's
// dependency-kind-aware gate to hold a consumer coding task until each provision
// dependency's `provision` gate issue derives deployed.
func (c DesignComponent) ProvisionDependsOn() []string {
	out := make([]string, 0, len(c.Dependencies))
	for _, d := range c.Dependencies {
		if d.Kind == DependencyKindExternal || d.Kind == DependencyKindPlatformResource {
			out = append(out, d.Name)
		}
	}
	return out
}

// ExternalDependencies returns the external + org-service dependencies — the
// resource-bearing entries. Used to surface context into issue bodies and to
// drive value collection.
func (c DesignComponent) ExternalDependencies() []Dependency {
	out := make([]Dependency, 0, len(c.Dependencies))
	for _, d := range c.Dependencies {
		if d.Kind == DependencyKindExternal || d.Kind == DependencyKindOrgService {
			out = append(out, d)
		}
	}
	return out
}

// ExposesAPI declares HTTP API exposure policy for a service component.
// Absent / nil ⇒ public (no gateway hop). `Auth: "end-user-required"` ⇒
// the API Platform gateway validates an end-user JWT and injects
// UserContext (default "X-User-Id") before forwarding upstream.
type ExposesAPI struct {
	Managed     bool   `json:"managed,omitempty"`
	Auth        string `json:"auth,omitempty"`        // "end-user-required" | "service-required" | "none"
	UserContext string `json:"userContext,omitempty"` // injected header name
	// OrgPublished marks a service's endpoint as consumable by OTHER projects in
	// the org. When set, the coding agent writes `visibility: [external,
	// namespace]` on the endpoint in its workload.yaml, and the org endpoint
	// catalog lists it as a cross-project `org-service` target. Deliberate +
	// source-of-truth: the provider owns this; the platform never patches it.
	OrgPublished bool `json:"orgPublished,omitempty"`
}

// UnionExternalConfigKeys scans comps and returns, per external dependency
// name, the UNION of Config[] declared by every component whose
// Dependencies[] names it — a key present in ANY component's Config is
// included, and Secret is OR'd across every declaring component, so a key
// already marked secret by one component can never be downgraded to plain by
// another component that omits it or marks it plain (secret wins). Dependency
// names are grouped case-insensitively (the same fold match
// provisioning.findDepInProject uses to locate a dependency) but keyed in the
// result by the FIRST-SEEN exact casing, so a stray casing difference between
// two components declaring "the same" external dependency cannot split it
// into two separate map entries.
//
// This is the single source of truth for build-time authoring, value saving,
// runner secret resolution, and readiness classification. Keeping every
// consumer on this helper prevents schema drift between those paths.
func UnionExternalConfigKeys(comps []DesignComponent) map[string][]ConfigKey {
	out := map[string][]ConfigKey{}
	canonName := map[string]string{}        // lower(name) -> first-seen exact name
	keyIndex := map[string]map[string]int{} // lower(name) -> config key -> index into out[canonName[lower(name)]]
	for _, c := range comps {
		for _, d := range c.Dependencies {
			if d.Kind != DependencyKindExternal {
				continue
			}
			lower := strings.ToLower(d.Name)
			name, ok := canonName[lower]
			if !ok {
				name = d.Name
				canonName[lower] = name
				keyIndex[lower] = map[string]int{}
			}
			idx := keyIndex[lower]
			merged := out[name]
			for _, k := range d.Config {
				if i, exists := idx[k.Key]; exists {
					if k.Secret {
						merged[i].Secret = true
					}
					continue
				}
				idx[k.Key] = len(merged)
				merged = append(merged, k)
			}
			out[name] = merged
		}
	}
	return out
}

// UnionExternalConfigFor returns the UNION Config[] schema (see
// UnionExternalConfigKeys) for the external dependency matching depName
// case-insensitively. The boolean distinguishes an external dependency with an
// empty schema from a dependency that is not declared as external. Callers that
// already know the exact dependency name they want use this instead of scanning
// UnionExternalConfigKeys's full map themselves.
func UnionExternalConfigFor(comps []DesignComponent, depName string) ([]ConfigKey, bool) {
	for name, cfg := range UnionExternalConfigKeys(comps) {
		if strings.EqualFold(name, depName) {
			return cfg, true
		}
	}
	return nil, false
}

// DesignComponents is a slice of DesignComponent.
type DesignComponents []DesignComponent

type Design struct {
	ProjectID         string            `json:"projectId"`
	OrgID             string            `json:"-"`
	Components        DesignComponents  `json:"components"`
	Status            string            `json:"status"`
	Version           int               `json:"version"`
	Versions          []ArtifactVersion `json:"versions,omitempty"`
	HasUnsavedChanges bool              `json:"hasUnsavedChanges"`
	SourceSpec        string            `json:"sourceSpec,omitempty"`
}
