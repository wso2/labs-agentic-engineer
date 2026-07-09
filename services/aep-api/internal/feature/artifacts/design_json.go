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
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/wso2/aep/aep-api/models"
)

// The AUTHORED per-component design file lives at
// `specs/design/components/<name>/design.json`. It replaces the component-level
// design.md. The wire-contract source of truth is sachiniSam's TS
// `ComponentDesign` (services/agents/src/contracts/component-design.ts); the
// on-disk structs below mirror that contract, extended with the unified
// `dependencies[]` (models.Dependency) in place of her `connections[]` and the
// platform-owned blocks aep-api carries (exposesAPI /
// componentAgentInstructions). The codec is strict (sachiniSam's strictObject
// philosophy): unknown top-level keys and unknown keys inside `dependencies[]`
// entries are rejected. `status`/`reason` are read-time computed values and are
// NEVER read from or written to the file — the on-disk dependency struct has no
// such fields, so any occurrence is rejected as an unknown key.
//
// Field mapping (design.json key ↔ models.DesignComponent field), for the TS
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
//	dependencies               ↔ Dependencies  (unified kind-discriminated union; replaces connections[])
//	exposesAPI                 ↔ ExposesAPI     (platform-owned; {managed, auth, userContext, orgPublished})
//	componentAgentInstructions ↔ ComponentAgentInstructions (platform-owned; optional)
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
	Name         string           `json:"name"`
	Type         string           `json:"type"`
	Version      string           `json:"version,omitempty"`
	Language     string           `json:"language,omitempty"`
	Buildpack    string           `json:"buildpack,omitempty"`
	AppPath      string           `json:"appPath,omitempty"`
	Entrypoint   string           `json:"entrypoint,omitempty"`
	Exposure     string           `json:"exposure,omitempty"`
	Description  string           `json:"description,omitempty"`
	Dependencies []dependencyJSON `json:"dependencies"`
	// Platform-owned blocks (absent = zero value).
	ExposesAPI                 *exposesAPIJSON `json:"exposesAPI,omitempty"`
	ComponentAgentInstructions string          `json:"componentAgentInstructions,omitempty"`
}

// dependencyJSON is the on-disk shape for one unified dependency entry. It
// mirrors models.Dependency MINUS Status/Reason (read-time computed, never
// persisted): omitting them makes DisallowUnknownFields reject any `status` or
// `reason` key inside a dependency entry.
type dependencyJSON struct {
	Kind         string                    `json:"kind"`
	Name         string                    `json:"name"`
	Description  string                    `json:"description,omitempty"`
	NeedsSpec    bool                      `json:"needsSpec,omitempty"`
	SpecPath     string                    `json:"specPath,omitempty"`
	SpecUrl      string                    `json:"specUrl,omitempty"`
	Config       []configKeyJSON           `json:"config,omitempty"`
	ResourceType string                    `json:"resourceType,omitempty"`
	Parameters   map[string]string         `json:"parameters,omitempty"`
	Candidates   []dependencyCandidateJSON `json:"candidates,omitempty"`
}

// configKeyJSON mirrors models.ConfigKey.
type configKeyJSON struct {
	Key             string `json:"key"`
	Secret          bool   `json:"secret"`
	CredentialClass string `json:"credentialClass,omitempty"`
}

// dependencyCandidateJSON mirrors models.DependencyCandidate.
type dependencyCandidateJSON struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
}

// exposesAPIJSON mirrors models.ExposesAPI.
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
func parseComponentDesignJSON(dir, raw string) (models.DesignComponent, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var dj componentDesignJSON
	if err := dec.Decode(&dj); err != nil {
		return models.DesignComponent{}, fmt.Errorf("decode design.json: %w", err)
	}
	// Reject trailing content after the JSON object.
	if dec.More() {
		return models.DesignComponent{}, fmt.Errorf("decode design.json: unexpected trailing content")
	}

	if !componentDesignName.MatchString(dir) {
		return models.DesignComponent{}, fmt.Errorf("component directory %q is not kebab-case", dir)
	}
	if dj.Name != dir {
		return models.DesignComponent{}, fmt.Errorf("design.json name %q must equal the component directory %q", dj.Name, dir)
	}
	if err := validateExposure(dir, dj.Exposure); err != nil {
		return models.DesignComponent{}, err
	}

	deps, err := assembleDependencies(dir, dj.Dependencies)
	if err != nil {
		return models.DesignComponent{}, err
	}

	return models.DesignComponent{
		Name:                       dj.Name,
		ComponentType:              dj.Type,
		Version:                    dj.Version,
		Language:                   dj.Language,
		Dependencies:               deps,
		Entrypoint:                 dj.Entrypoint,
		Buildpack:                  dj.Buildpack,
		AppPath:                    dj.AppPath,
		Exposure:                   dj.Exposure,
		Description:                dj.Description,
		ComponentAgentInstructions: dj.ComponentAgentInstructions,
		ExposesAPI:                 toModelExposesAPI(dj.ExposesAPI),
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
	case models.DependencyKindComponent, models.DependencyKindOrgService, models.DependencyKindExternal, models.DependencyKindPlatformResource:
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
// model and ports the read-time needs-spec computation: an external dependency
// that declares needsSpec but has no specPath yet is unresolved at read time
// (the user must supply the spec before the design can be saved).
//
// A dependency entry missing `kind` or `name`, or declaring a `kind` outside
// the closed set, is a schema ERROR (the entry used to be silently dropped,
// which quietly lost data the architect authored). Errors are phrased for a
// writing agent's one-round-trip self-correction: they name the file, the
// offending entry's index, and the fix.
func assembleDependencies(dir string, in []dependencyJSON) ([]models.Dependency, error) {
	out := make([]models.Dependency, 0, len(in))
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
		dep := models.Dependency{
			Kind:         d.Kind,
			Name:         d.Name,
			Description:  d.Description,
			NeedsSpec:    d.NeedsSpec,
			SpecPath:     d.SpecPath,
			SpecUrl:      d.SpecUrl,
			Config:       toModelConfigKeys(d.Config),
			ResourceType: d.ResourceType,
			Parameters:   d.Parameters,
			Candidates:   toModelCandidates(d.Candidates),
		}
		// External deps that declare needsSpec but carry no specPath yet are
		// unresolved at read time. Status/Reason are computed here (never read
		// from the file).
		if dep.Kind == models.DependencyKindExternal && dep.NeedsSpec && strings.TrimSpace(dep.SpecPath) == "" {
			dep.Status = "unresolved"
			dep.Reason = "needs-spec"
		}
		out = append(out, dep)
	}
	return out, nil
}

// marshalComponentDesignJSON encodes a component to canonical design.json bytes:
// stable key order (via the struct), 2-space indent, trailing newline. It never
// emits status/reason (the on-disk struct has no such fields). `dir` is the
// component directory the file lives under; the component name must equal it.
func marshalComponentDesignJSON(dir string, comp models.DesignComponent) ([]byte, error) {
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
		Description:                comp.Description,
		Dependencies:               toJSONDeps(comp.Dependencies),
		ExposesAPI:                 toJSONExposesAPI(comp.ExposesAPI),
		ComponentAgentInstructions: comp.ComponentAgentInstructions,
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

// toJSONDeps converts the unified model back to on-disk dependency entries.
// Status/Reason are intentionally dropped. The result is always non-nil so the
// `dependencies` key marshals as `[]` (not null) for a clean, stable contract.
func toJSONDeps(in []models.Dependency) []dependencyJSON {
	out := make([]dependencyJSON, 0, len(in))
	for _, d := range in {
		if d.Name == "" || d.Kind == "" {
			continue
		}
		out = append(out, dependencyJSON{
			Kind:         d.Kind,
			Name:         d.Name,
			Description:  d.Description,
			NeedsSpec:    d.NeedsSpec,
			SpecPath:     d.SpecPath,
			SpecUrl:      d.SpecUrl,
			Config:       toJSONConfigKeys(d.Config),
			ResourceType: d.ResourceType,
			Parameters:   d.Parameters,
			Candidates:   toJSONCandidates(d.Candidates),
		})
	}
	return out
}

func toModelConfigKeys(in []configKeyJSON) []models.ConfigKey {
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

func toJSONConfigKeys(in []models.ConfigKey) []configKeyJSON {
	if len(in) == 0 {
		return nil
	}
	out := make([]configKeyJSON, 0, len(in))
	for _, c := range in {
		out = append(out, configKeyJSON{Key: c.Key, Secret: c.Secret, CredentialClass: c.CredentialClass})
	}
	return out
}

func toModelCandidates(in []dependencyCandidateJSON) []models.DependencyCandidate {
	if len(in) == 0 {
		return nil
	}
	out := make([]models.DependencyCandidate, 0, len(in))
	for _, c := range in {
		out = append(out, models.DependencyCandidate{Label: c.Label, Description: c.Description, URL: c.URL})
	}
	return out
}

func toJSONCandidates(in []models.DependencyCandidate) []dependencyCandidateJSON {
	if len(in) == 0 {
		return nil
	}
	out := make([]dependencyCandidateJSON, 0, len(in))
	for _, c := range in {
		out = append(out, dependencyCandidateJSON{Label: c.Label, Description: c.Description, URL: c.URL})
	}
	return out
}

// toModelExposesAPI builds *models.ExposesAPI, returning nil when the block is
// absent or carries only zero values (mirrors the prior frontmatter gating).
func toModelExposesAPI(in *exposesAPIJSON) *models.ExposesAPI {
	if in == nil {
		return nil
	}
	if !in.Managed && in.Auth == "" && in.UserContext == "" && !in.OrgPublished {
		return nil
	}
	return &models.ExposesAPI{
		Managed:      in.Managed,
		Auth:         in.Auth,
		UserContext:  in.UserContext,
		OrgPublished: in.OrgPublished,
	}
}

func toJSONExposesAPI(in *models.ExposesAPI) *exposesAPIJSON {
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
