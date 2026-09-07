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
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// The AUTHORED per-component design file lives at
// `specs/design/components/<name>/design.json`. It replaces the component-level
// design.md. The wire-contract source of truth is sachiniSam's TS
// `ComponentDesign` (services/agents/src/contracts/component-design.ts); the
// on-disk structs below mirror that contract, extended with the unified
// `dependencies[]` (Dependency) in place of her `connections[]` and the
// platform-owned blocks aep-api carries (exposesAPI /
// componentAgentInstructions). The codec is strict (sachiniSam's strictObject
// philosophy): unknown top-level keys and unknown keys inside `dependencies[]`
// entries are rejected. `status`/`reason` are read-time computed values and are
// NEVER read from or written to the file — the on-disk dependency struct has no
// such fields, so any occurrence is rejected as an unknown key.
//
// Field mapping (design.json key ↔ DesignComponent field), for the TS
// mirror coordination:
//
//	name                       ↔ Name          (must equal the component dir, kebab-case)
//	type                       ↔ ComponentType
//	version                    ↔ Version
//	language                   ↔ Language
//	buildpack                  ↔ Buildpack
//	appPath                    ↔ AppPath
//	entrypoint                 ↔ Entrypoint
//	exposure                   ↔ Exposure      ("internet" | "intranet")
//	description                ↔ Description    (the single-responsibility prose / former design.md body)
//	endpoint                   ↔ Endpoint       (optional; {name} — the workload endpoint name the api-configuration trait binds to)
//	dependencies               ↔ Dependencies  (unified kind-discriminated union; replaces connections[])
//	exposesAPI                 ↔ ExposesAPI     (platform-owned; {managed, auth, userContext, orgPublished})
//	componentAgentInstructions ↔ ComponentAgentInstructions (platform-owned; optional)
//	skillsPinned              ↔ SkillsPinned  (optional; skill names applied to this component)
//
// OpenAPISpec is NOT a design.json key: it stays in the sibling
// `components/<name>/openapi.yaml` file, assembled/split separately.
//
// The retired caller-identity field (formerly `callerIdentity`, superseded by
// the explicit `thunder-app` platform-resource dependency) is no longer part
// of this struct. Because the decoder above calls DisallowUnknownFields, this
// is NOT silently tolerated: a design.json still carrying a `callerIdentity`
// key (e.g. written by an old agent build) now fails to parse with `json:
// unknown field "callerIdentity"`, consistent with this codec's existing
// strict-unknown-key philosophy. Such files must be hand-edited to drop the
// key before they can be read again.

// componentDesignName validates the kebab-case name rule shared by read+write.
var componentDesignName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// componentDesignJSON is the on-disk shape of `components/<name>/design.json`.
type componentDesignJSON struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Version    string `json:"version,omitempty"`
	Language   string `json:"language,omitempty"`
	Buildpack  string `json:"buildpack,omitempty"`
	AppPath    string `json:"appPath,omitempty"`
	Entrypoint string `json:"entrypoint,omitempty"`
	Exposure   string `json:"exposure,omitempty"`
	// Stories is the agent-authored list of PRD stories this component serves
	// (#369) — the build gate's coverage check reads it.
	Stories      []int            `json:"stories,omitempty"`
	Description  string           `json:"description,omitempty"`
	Endpoint     *endpointJSON    `json:"endpoint,omitempty"`
	Dependencies []dependencyJSON `json:"dependencies"`
	// Platform-owned blocks (absent = zero value).
	ExposesAPI                 *exposesAPIJSON `json:"exposesAPI,omitempty"`
	ComponentAgentInstructions string          `json:"componentAgentInstructions,omitempty"`
	SkillsPinned               []string        `json:"skillsPinned,omitempty"`
}

// endpointJSON is the on-disk shape of the optional `endpoint` block. Only the
// name is carried (mirrors ComponentEndpoint); the port stays in
// workload.yaml.
type endpointJSON struct {
	Name string `json:"name"`
}

// dependencyJSON is the on-disk shape for one unified dependency entry. It
// mirrors Dependency MINUS Status/Reason (read-time computed, never
// persisted): omitting them makes DisallowUnknownFields reject any `status` or
// `reason` key inside a dependency entry. `style`/`package`/`specPath`/
// `candidates` are external-only (kind-conditioned validation lives in the
// write-gates — the zod superRefine + agentfold/designgate.go — not here: this
// decoder stays lenient about kind-specific fields, matching the rest of the
// struct).
type dependencyJSON struct {
	Kind         string          `json:"kind"`
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	Style        string          `json:"style,omitempty"`
	Package      string          `json:"package,omitempty"`
	SpecPath     string          `json:"specPath,omitempty"`
	Candidates   []candidateJSON `json:"candidates,omitempty"`
	Config       []configKeyJSON `json:"config,omitempty"`
	ResourceType string          `json:"resourceType,omitempty"`
	Parameters   map[string]any  `json:"parameters,omitempty"`
	// Wiring is the platform-stamped consumer-side wiring (ADR-0013). It is the
	// one derived field that IS persisted here — unlike status/reason, which stay
	// out of the codec because they are recomputed on every read. It must round-trip
	// in BOTH directions: dropping it on write silently un-stamps every derivation,
	// and dropping it on read makes the next derivation see no prior value and the
	// change detection commit on every save.
	Wiring *dependencyWiringJSON `json:"wiring,omitempty"`
}

// dependencyWiringJSON is the on-disk shape of a dependency's `wiring` object.
// Mirrors DependencyWiring; its own field order is the emitted key order.
//
// Every field is omitempty because the shape is a two-variant union (see
// DependencyWiring): the resources[] variant must not emit a null `endpoint`, and
// the endpoints[] variant must not emit an empty `ref` — either would fail the
// write gates, which require each variant to carry exactly its own keys.
type dependencyWiringJSON struct {
	Ref         string              `json:"ref,omitempty"`
	EnvBindings map[string]string   `json:"envBindings,omitempty"`
	Endpoint    *endpointWiringJSON `json:"endpoint,omitempty"`
}

// endpointWiringJSON is the on-disk shape of the `wiring.endpoint` object — one
// workload `dependencies.endpoints[]` entry. Mirrors EndpointWiring.
type endpointWiringJSON struct {
	Component   string            `json:"component"`
	Name        string            `json:"name"`
	Visibility  string            `json:"visibility"`
	EnvBindings map[string]string `json:"envBindings"`
}

// candidateJSON is the on-disk shape of one entry in a dependency's
// `candidates` array. Mirrors DependencyCandidate.
type candidateJSON struct {
	Name        string `json:"name"`
	Style       string `json:"style"`
	Description string `json:"description,omitempty"`
	Package     string `json:"package,omitempty"`
}

// configKeyJSON mirrors ConfigKey.
type configKeyJSON struct {
	Key          string `json:"key"`
	Secret       bool   `json:"secret,omitempty"`
	Description  string `json:"description,omitempty"`
	DefaultValue string `json:"defaultValue,omitempty"`
}

// exposesAPIJSON mirrors ExposesAPI.
type exposesAPIJSON struct {
	Managed      bool   `json:"managed,omitempty"`
	Auth         string `json:"auth,omitempty"`
	UserContext  string `json:"userContext,omitempty"`
	OrgPublished bool   `json:"orgPublished,omitempty"`
}

// parseComponentDesignJSON decodes a `components/<name>/design.json` body into a
// DesignComponent. `dir` is the component directory name; the file's `name`
// MUST equal it (kebab-case). Decoding is strict — unknown top-level keys or
// unknown keys inside a dependency entry (including status/reason) are errors.
// OpenAPISpec is filled from the sibling openapi.yaml by the caller, not here.
func parseComponentDesignJSON(dir, raw string) (DesignComponent, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var dj componentDesignJSON
	if err := dec.Decode(&dj); err != nil {
		return DesignComponent{}, fmt.Errorf("decode design.json: %w", err)
	}
	// Reject trailing content after the JSON object.
	if dec.More() {
		return DesignComponent{}, fmt.Errorf("decode design.json: unexpected trailing content")
	}

	if !componentDesignName.MatchString(dir) {
		return DesignComponent{}, fmt.Errorf("component directory %q is not kebab-case", dir)
	}
	if dj.Name != dir {
		return DesignComponent{}, fmt.Errorf("design.json name %q must equal the component directory %q", dj.Name, dir)
	}
	if err := validateExposure(dir, dj.Exposure); err != nil {
		return DesignComponent{}, err
	}

	deps, err := assembleDependencies(dir, dj.Dependencies)
	if err != nil {
		return DesignComponent{}, err
	}

	return DesignComponent{
		Name:                       dj.Name,
		ComponentType:              dj.Type,
		Version:                    dj.Version,
		Language:                   dj.Language,
		Dependencies:               deps,
		Entrypoint:                 dj.Entrypoint,
		Buildpack:                  dj.Buildpack,
		AppPath:                    dj.AppPath,
		Exposure:                   dj.Exposure,
		Stories:                    append([]int(nil), dj.Stories...),
		Description:                dj.Description,
		Endpoint:                   toModelEndpoint(dj.Endpoint),
		ComponentAgentInstructions: dj.ComponentAgentInstructions,
		ExposesAPI:                 toModelExposesAPI(dj.ExposesAPI),
		SkillsPinned:               append([]string(nil), dj.SkillsPinned...),
	}, nil
}

// validDependencyKinds is the closed set of dependency `kind` values a
// design.json may declare, in the canonical listing order used across every
// self-correction error message below.
const validDependencyKinds = "component | org-service | external | platform-resource"

// isValidDependencyKind reports whether kind is one of the four recognised
// dependency kinds.
func isValidDependencyKind(kind string) bool {
	switch kind {
	case DependencyKindComponent, DependencyKindOrgService, DependencyKindExternal, DependencyKindPlatformResource:
		return true
	default:
		return false
	}
}

// validateExposure enforces the `exposure` enum on read: only "internet",
// "intranet", or absent/empty are valid. The error is phrased for a writing
// agent to self-correct in one round trip.
func validateExposure(dir, exposure string) error {
	if exposure == "" || exposure == "internet" || exposure == "intranet" {
		return nil
	}
	return fmt.Errorf("components/%s/design.json: exposure %q is invalid — must be %q, %q, or omitted", dir, exposure, "internet", "intranet")
}

// assembleDependencies converts the on-disk dependency entries to the unified
// model. This is a PURE DECODE: no Status/Reason is ever computed here (this
// codec has no org/registry context to correctly resolve against — that
// requires the shared resolver, which reads the live catalog). Every
// resolution state (resolved/ambiguous/unresolved) is derived at READ time by
// that resolver from the presence/absence of Style/Package/Candidates/SpecPath
// — never stored, never computed here.
//
// A dependency entry missing `kind` or `name`, or declaring a `kind` outside
// the closed set, is a schema ERROR (the entry used to be silently dropped,
// which quietly lost data the architect authored). Errors are phrased for a
// writing agent's one-round-trip self-correction: they name the file, the
// offending entry's index, and the fix.
func assembleDependencies(dir string, in []dependencyJSON) ([]Dependency, error) {
	out := make([]Dependency, 0, len(in))
	for i, d := range in {
		if d.Kind == "" {
			return nil, fmt.Errorf("components/%s/design.json: dependencies[%d] is missing required key %q — every dependency needs kind (%s) and name",
				dir, i, "kind", validDependencyKinds)
		}
		if d.Name == "" {
			return nil, fmt.Errorf("components/%s/design.json: dependencies[%d] is missing required key %q — every dependency needs kind (%s) and name",
				dir, i, "name", validDependencyKinds)
		}
		if !isValidDependencyKind(d.Kind) {
			return nil, fmt.Errorf("components/%s/design.json: dependencies[%d] has unknown kind %q — every dependency needs kind (%s) and name",
				dir, i, d.Kind, validDependencyKinds)
		}
		out = append(out, Dependency{
			Kind:         d.Kind,
			Name:         d.Name,
			Description:  d.Description,
			Style:        d.Style,
			Package:      d.Package,
			SpecPath:     d.SpecPath,
			Candidates:   toModelCandidates(d.Candidates),
			Config:       toModelConfigKeys(d.Config),
			ResourceType: d.ResourceType,
			Parameters:   d.Parameters,
			Wiring:       toModelWiring(d.Wiring),
		})
	}
	return out, nil
}

// marshalComponentDesignJSON encodes a component to canonical design.json bytes:
// stable key order (via the struct), 2-space indent, trailing newline. It never
// emits status/reason (the on-disk struct has no such fields). `dir` is the
// component directory the file lives under; the component name must equal it.
func marshalComponentDesignJSON(dir string, comp DesignComponent) ([]byte, error) {
	if comp.Name == "" {
		return nil, fmt.Errorf("component with empty name")
	}
	if !componentDesignName.MatchString(comp.Name) {
		return nil, fmt.Errorf("component name %q is not kebab-case", comp.Name)
	}
	if comp.Name != dir {
		return nil, fmt.Errorf("component name %q must equal the component directory %q", comp.Name, dir)
	}

	dj := componentDesignJSON{
		Name:                       comp.Name,
		Type:                       comp.ComponentType,
		Version:                    comp.Version,
		Language:                   comp.Language,
		Buildpack:                  comp.Buildpack,
		AppPath:                    comp.AppPath,
		Entrypoint:                 comp.Entrypoint,
		Exposure:                   comp.Exposure,
		Stories:                    comp.Stories,
		Description:                comp.Description,
		Endpoint:                   toJSONEndpoint(comp.Endpoint),
		Dependencies:               toJSONDeps(comp.Dependencies),
		ExposesAPI:                 toJSONExposesAPI(comp.ExposesAPI),
		ComponentAgentInstructions: comp.ComponentAgentInstructions,
		SkillsPinned:               comp.SkillsPinned,
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(dj); err != nil {
		return nil, fmt.Errorf("encode design.json: %w", err)
	}
	// json.Encoder already appends a trailing newline.
	return buf.Bytes(), nil
}

// toJSONWiring / toModelWiring carry the platform-stamped wiring across the codec
// boundary. Both directions are load-bearing and each failed once: dropping it on
// WRITE silently discards every derivation (the design.json lands with no wiring
// and the coding agent is back to having nothing to copy), and dropping it on READ
// makes each derivation see no prior value, so the change detection reports a diff
// and commits on every single save.
//
// Both VARIANTS ride the same rule. The endpoints[] half is the one whose loss is
// invisible: a missing `ref` leaves the agent with nothing to write, but a missing
// `endpoint` leaves it free to guess a plausible sibling name, and a wrong one
// deploys and serves without ever reaching Ready.
func toJSONWiring(in *DependencyWiring) *dependencyWiringJSON {
	if in == nil {
		return nil
	}
	out := &dependencyWiringJSON{Ref: in.Ref, EnvBindings: in.EnvBindings}
	if in.Endpoint != nil {
		out.Endpoint = &endpointWiringJSON{
			Component:   in.Endpoint.Component,
			Name:        in.Endpoint.Name,
			Visibility:  in.Endpoint.Visibility,
			EnvBindings: in.Endpoint.EnvBindings,
		}
	}
	return out
}

func toModelWiring(in *dependencyWiringJSON) *DependencyWiring {
	if in == nil {
		return nil
	}
	out := &DependencyWiring{Ref: in.Ref, EnvBindings: in.EnvBindings}
	if in.Endpoint != nil {
		out.Endpoint = &EndpointWiring{
			Component:   in.Endpoint.Component,
			Name:        in.Endpoint.Name,
			Visibility:  in.Endpoint.Visibility,
			EnvBindings: in.Endpoint.EnvBindings,
		}
	}
	return out
}

// toJSONDeps converts the unified model back to on-disk dependency entries.
// Status/Reason are intentionally dropped. The result is always non-nil so the
// `dependencies` key marshals as `[]` (not null) for a clean, stable contract.
func toJSONDeps(in []Dependency) []dependencyJSON {
	out := make([]dependencyJSON, 0, len(in))
	for _, d := range in {
		if d.Name == "" || d.Kind == "" {
			continue
		}
		out = append(out, dependencyJSON{
			Kind:         d.Kind,
			Name:         d.Name,
			Description:  d.Description,
			Style:        d.Style,
			Package:      d.Package,
			SpecPath:     d.SpecPath,
			Candidates:   toJSONCandidates(d.Candidates),
			Config:       toJSONConfigKeys(d.Config),
			ResourceType: d.ResourceType,
			Parameters:   d.Parameters,
			Wiring:       toJSONWiring(d.Wiring),
		})
	}
	return out
}

// toModelCandidates/toJSONCandidates mirror toModelConfigKeys/toJSONConfigKeys
// for the `candidates` array (DependencyCandidate ⇄ candidateJSON).
func toModelCandidates(in []candidateJSON) []DependencyCandidate {
	if len(in) == 0 {
		return nil
	}
	out := make([]DependencyCandidate, 0, len(in))
	for _, c := range in {
		out = append(out, DependencyCandidate{
			Name:        c.Name,
			Style:       c.Style,
			Description: c.Description,
			Package:     c.Package,
		})
	}
	return out
}

func toJSONCandidates(in []DependencyCandidate) []candidateJSON {
	if len(in) == 0 {
		return nil
	}
	out := make([]candidateJSON, 0, len(in))
	for _, c := range in {
		out = append(out, candidateJSON{
			Name:        c.Name,
			Style:       c.Style,
			Description: c.Description,
			Package:     c.Package,
		})
	}
	return out
}

func toModelConfigKeys(in []configKeyJSON) []ConfigKey {
	if len(in) == 0 {
		return nil
	}
	out := make([]ConfigKey, 0, len(in))
	for _, c := range in {
		if c.Key == "" {
			continue
		}
		out = append(out, ConfigKey{Key: c.Key, Secret: c.Secret, Description: c.Description, DefaultValue: c.DefaultValue})
	}
	return out
}

func toJSONConfigKeys(in []ConfigKey) []configKeyJSON {
	if len(in) == 0 {
		return nil
	}
	out := make([]configKeyJSON, 0, len(in))
	for _, c := range in {
		out = append(out, configKeyJSON{Key: c.Key, Secret: c.Secret, Description: c.Description, DefaultValue: c.DefaultValue})
	}
	return out
}

// toModelEndpoint builds *ComponentEndpoint from the on-disk block,
// returning nil when the block is absent or carries an empty name (so callers
// fall back to the default endpoint name via DesignComponent.EndpointName).
func toModelEndpoint(in *endpointJSON) *ComponentEndpoint {
	if in == nil || in.Name == "" {
		return nil
	}
	return &ComponentEndpoint{Name: in.Name}
}

// toJSONEndpoint mirrors toModelEndpoint for the write path: nil or empty name
// omits the `endpoint` key entirely (the default is implicit).
func toJSONEndpoint(in *ComponentEndpoint) *endpointJSON {
	if in == nil || in.Name == "" {
		return nil
	}
	return &endpointJSON{Name: in.Name}
}

// toModelExposesAPI builds *ExposesAPI, returning nil when the block is
// absent or carries only zero values (mirrors the prior frontmatter gating).
func toModelExposesAPI(in *exposesAPIJSON) *ExposesAPI {
	if in == nil {
		return nil
	}
	if !in.Managed && in.Auth == "" && in.UserContext == "" && !in.OrgPublished {
		return nil
	}
	return &ExposesAPI{
		Managed:      in.Managed,
		Auth:         in.Auth,
		UserContext:  in.UserContext,
		OrgPublished: in.OrgPublished,
	}
}

func toJSONExposesAPI(in *ExposesAPI) *exposesAPIJSON {
	if in == nil {
		return nil
	}
	if !in.Managed && in.Auth == "" && in.UserContext == "" && !in.OrgPublished {
		return nil
	}
	return &exposesAPIJSON{
		Managed:      in.Managed,
		Auth:         in.Auth,
		UserContext:  in.UserContext,
		OrgPublished: in.OrgPublished,
	}
}
