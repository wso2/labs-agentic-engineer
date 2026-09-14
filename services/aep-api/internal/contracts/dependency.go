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

package contracts

// Dependency wire DTOs live here (the dependency-free leaf) so the generated
// contract package (internal/gen) can name them via x-go-type without importing
// a domain — keeping gen, and therefore everything reachable from platform,
// domain-free (TestPlatformImportsNoDomain / TestContractsIsLeaf). The spec
// domain re-exports these as spec.Dependency / spec.ConfigKey / … and owns all
// behaviour over them (ComputeDependencyStatus, the enum consts, validators);
// this file carries only the shapes, per the "domains re-export contracts
// types, never the reverse" rule.

// DependencyKind discriminates the unified Dependency entry. The concrete kind
// consts (component / org-service / external / platform-resource) live in the
// spec domain, which owns the resolution algebra.
type DependencyKind = string

// DependencyStyle is the closed set of external dependency shapes — rest-api,
// graphql, sdk (mirrors the agent-stream TS `DependencyStyle`). Meaningful only
// on kind=external. The concrete style consts live in the spec domain.
type DependencyStyle = string

// Dependency is the unified, kind-discriminated dependency entry on a
// component. It subsumes the legacy DependsOn (sibling components) and the
// external HTTP APIs a component consumed at runtime. Go has no native
// discriminated union, so a single struct carries every kind's fields; `Kind`
// selects which are meaningful (Config for external; ResourceType/Parameters
// for platform-resource; the rest common). Mirrors the agents-service Zod
// `Dependency`.
type Dependency struct {
	Kind        DependencyKind `json:"kind"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	// Status and Reason are READ-TIME computed fields, derived by
	// ComputeDependencyStatus (the single resolution authority) against
	// freshly-fetched resolver-port lookups on every design read; they are NOT
	// persisted and carry NO gorm/yaml tags — plain wire JSON only. The
	// architect never sets them.
	//   Status: resolved|unresolved|blocked
	//   Reason: needs-contract|needs-acceptance|needs-input|not-found|access-required
	Status string `json:"status,omitempty"`
	Reason string `json:"reason,omitempty"`
	// external: the definition, HYDRATED at read time from the dependency's own
	// file (specs/design/dependencies/<name>/dependency.json — one dependency,
	// one definition, referenced by name from every component that uses it).
	// A component's design.json never carries these; the read path copies them
	// onto the edge so every downstream reader keeps one flat shape.
	//
	// Source: "project" (agent-authored in this repo) or "org" (a platform-
	// stamped copy of a Registered External resource — no values collected).
	Source string `json:"source,omitempty"`
	// Provider: the concrete system chosen ("Stripe"). Absent until the user
	// chose one; never beside Suggestions.
	Provider string `json:"provider,omitempty"`
	// Style: how the component talks to it — rest-api | graphql | sdk.
	Style DependencyStyle `json:"style,omitempty"`
	// Contract: the contract FILE NAME in the dependency's directory
	// (openapi.yaml / schema.graphql); ContractPath joins it. Absent ⇒ the
	// dependency still needs its contract.
	Contract string `json:"contract,omitempty"`
	// SDK: for an sdk-style dependency, the manifest FILE NAME (sdk.json) —
	// present only when the manifest is on disk beside the definition.
	SDK string `json:"sdk,omitempty"`
	// Package: for an sdk-style dependency, the ecosystem-prefixed package for
	// THIS component's implementation language, picked from the SDK manifest
	// (sdk.json) at hydration. Empty when the manifest has no entry for it.
	Package string `json:"package,omitempty"`
	// Provenance: where the contract came from (see DependencyProvenance).
	Provenance *DependencyProvenance `json:"provenance,omitempty"`
	// Suggestions: services the user might choose, while no provider is
	// chosen. Choosing one REMOVES the field and sets Provider.
	Suggestions []DependencySuggestion `json:"suggestions,omitempty"`
	// ContractAssumed: the contract file on disk is one the design agent wrote
	// from research (it carries `x-aep-assumed: true`; an sdk.json carries
	// `"assumed": true`). Until the user accepts it (Assumed) the dependency is
	// not resolved.
	ContractAssumed bool `json:"contractAssumed,omitempty"`
	// ContractDerived: the contract file on disk was written by the design
	// agent from the provider's own developer reference (it carries
	// `x-aep-derived: true`; an sdk.json carries `"derived": true`) — every
	// operation cited from a page. Resolved, flagged `derived`; no
	// authorization is asked, unlike an assumption.
	ContractDerived bool `json:"contractDerived,omitempty"`
	// Assumed: the user's permission to build against an agent-written
	// contract (see DependencyAssumption). Read-only for the agent.
	Assumed *DependencyAssumption `json:"assumed,omitempty"`
	// Flags: read-time qualifiers on a resolved dependency — "registered",
	// "assumed", "sdk-only". Never authored.
	Flags []string `json:"flags,omitempty"`
	// external: the config key schema the consuming component codes against.
	Config []ConfigKey `json:"config,omitempty"`
	// platform-resource: the registered (Cluster)ResourceType + provisioning params.
	// Parameter values are mixed scalar types (string | number | bool) per the
	// target (Cluster)ResourceType's OpenAPI v3 schema — e.g. postgres-cnpg's
	// `instances` is an integer while `storage`/`version` are strings — so the
	// map is any-valued and marshalled verbatim into the OC Resource
	// spec.parameters (numbers must stay JSON numbers for CRD validation).
	ResourceType string         `json:"resourceType,omitempty"`
	Parameters   map[string]any `json:"parameters,omitempty"`
	// component / platform-resource / external: the platform-stamped consumer-side
	// wiring. See DependencyWiring — derived at design save, overwritten on every
	// save, never authored by an agent.
	Wiring *DependencyWiring `json:"wiring,omitempty"`
}

// DependencyWiring is the resolved consumer-side wiring for a dependency: what
// the coding agent copies into its component's workload.yaml `dependencies:`
// block. It carries ONE VARIANT PER sub-block of that block, and each variant
// mirrors one entry of its sub-block exactly, so the agent copies rather than
// transforms:
//
//   - Ref + EnvBindings → one `dependencies.resources[]` entry (provisioning's
//     workloadResourceDepYAML), for kind platform-resource / external.
//   - Endpoint          → one `dependencies.endpoints[]` entry (provisioning's
//     workloadEndpointDepYAML), for kind component.
//
// The variants are EXCLUSIVE — a dependency resolves to a resource or to an
// endpoint, never both, and Kind already says which. Go cannot express a union,
// so both live on one struct and exactly-one is enforced by the write gates (the
// zod union in agent-stream, agentfold.validateDependencyWiring here); the TS
// contract states it as a real union type.
//
// Every variant is knowable at design save because every field is a pure function
// of the design (plus, for a resource, the resource type's DECLARED outputs) — no
// binding, no gate, no cluster state. Mirrors the agent-stream TS
// `DependencyWiring`.
type DependencyWiring struct {
	Ref         string            `json:"ref,omitempty"`
	EnvBindings map[string]string `json:"envBindings,omitempty"`
	Endpoint    *EndpointWiring   `json:"endpoint,omitempty"`
}

// EndpointWiring is one `dependencies.endpoints[]` entry: a sibling component's
// endpoint in the same project.
//
// Component is the SCOPED OC component name (ocname.ScopedComponentName), because
// that is the key OpenChoreo resolves an endpoint dependency by. Stamping it here
// is what removed the ordering dependency that made this unknowable: the live
// endpoint catalog only lists components that have DEPLOYED, so on a first
// delivery — siblings coded in one cycle, nothing running yet — dispatch-time
// resolution could answer nothing, and an agent left to guess wrote the FRIENDLY
// name. OpenChoreo then matched no binding, and the consumer sat at
// `Ready=False / ConnectionsPending` while the platform reported "deploying".
//
// Mirrors the agent-stream TS `EndpointWiring`.
type EndpointWiring struct {
	Component string `json:"component"`
	Name      string `json:"name"`
	// Visibility is the target endpoint's reachability — "project" for a
	// same-project sibling.
	Visibility string `json:"visibility"`
	// EnvBindings maps the endpoint output to the env var OpenChoreo injects it
	// as: one key, `address`, bound to ocname.ServiceURLEnvName(depName).
	EnvBindings map[string]string `json:"envBindings"`
}

// DependencySuggestion is a service commonly used for a dependency's
// capability, named from the design agent's knowledge while no provider is
// chosen — a starting point for the user's choice, never a researched fit and
// never turned into a provider by the agent. Mirrors the agent-stream TS
// `DependencySuggestion`.
type DependencySuggestion struct {
	Name        string          `json:"name"`
	Style       DependencyStyle `json:"style,omitempty"`
	Description string          `json:"description,omitempty"`
}

// ConfigKey is one env-var key a component reads at runtime. For an external
// resource these keys form the resource's schema (drives the OC ResourceType).
// Secret keys route through the secret path. `secret` is optional and omitted
// when false — a key with no `secret` field is a plain (non-secret) config value.
type ConfigKey struct {
	Key    string `json:"key"`
	Secret bool   `json:"secret,omitempty"`
	// Description is an optional human-readable note on what this value is for,
	// authored alongside the key. The Build dependency drawer renders it under
	// the field so the user knows what to supply.
	Description string `json:"description,omitempty"`
	// DefaultValue is an optional suggested initial value the agent MAY set for a
	// NON-secret key it can infer a sensible default for (a region, a base URL).
	// The Build dependency drawer pre-fills the field with it. Never set for a
	// secret key — a credential has no default to invent.
	DefaultValue string `json:"defaultValue,omitempty"`
}

// DependencyProvenance records where a committed contract came from, so a
// reader can tell a slice of the provider's published document from something
// typed by hand, and re-derive the slice when the source moves. Mirrors the
// agent-stream TS `DependencyProvenance`.
type DependencyProvenance struct {
	// SourceURL is the document the contract was taken from — a URL, or the
	// name of an uploaded file.
	SourceURL string `json:"sourceUrl,omitempty"`
	// SHA256 (hex) of the FULL source document, not of the slice.
	SHA256 string `json:"sha256,omitempty"`
	// FetchedAt is the RFC 3339 instant the source was read.
	FetchedAt string `json:"fetchedAt,omitempty"`
	// Sliced is true when the committed contract is a slice of the source.
	Sliced bool `json:"sliced,omitempty"`
}

// DependencyAssumption is the user's permission to build against a contract
// the agent wrote from research rather than the provider's document. The
// platform writes it when the user accepts the agent's proposal; the agent may
// echo it but never introduce or change it. Mirrors the agent-stream TS
// `DependencyAssumption`.
type DependencyAssumption struct {
	// By is the accepting user's login.
	By string `json:"by"`
	// At is the RFC 3339 instant of the acceptance.
	At string `json:"at"`
	// Note is the agent's own statement of what it was unsure about.
	Note string `json:"note,omitempty"`
}

// DependencyDefinition is the one definition of an external dependency — the
// wire shape of specs/design/dependencies/<name>/dependency.json (agent-stream
// TS `DependencyDesign`). Components reference it by Name; the spec domain
// hydrates each reference from it at read time.
type DependencyDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Source      string                 `json:"source,omitempty"`
	Provider    string                 `json:"provider,omitempty"`
	Style       DependencyStyle        `json:"style,omitempty"`
	Contract    string                 `json:"contract,omitempty"`
	SDK         string                 `json:"sdk,omitempty"`
	Provenance  *DependencyProvenance  `json:"provenance,omitempty"`
	Suggestions []DependencySuggestion `json:"suggestions,omitempty"`
	Config      []ConfigKey            `json:"config,omitempty"`
	Assumed     *DependencyAssumption  `json:"assumed,omitempty"`
}

// SdkManifest is the wire shape of specs/design/dependencies/<name>/sdk.json:
// where each implementation language finds the provider's SDK. Mirrors the
// agent-stream TS `SdkManifest`.
type SdkManifest struct {
	// Packages maps a lower-case language ("typescript", "go") to an
	// ecosystem-prefixed package identifier ("npm:stripe@^14").
	Packages map[string]string `json:"packages"`
	DocsURL  string            `json:"docsUrl,omitempty"`
	// Calls lists the SDK calls the design relies on, in the SDK's own naming.
	Calls []string `json:"calls,omitempty"`
	// Derived marks a manifest the design agent wrote from the provider's
	// SDK reference — the sdk.json twin of a contract's `x-aep-derived: true`.
	Derived bool `json:"derived,omitempty"`
	// Assumed marks a manifest the design agent wrote without a published
	// source — the sdk.json twin of a contract's `x-aep-assumed: true`.
	Assumed bool `json:"assumed,omitempty"`
}
