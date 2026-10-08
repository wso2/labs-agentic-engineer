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

// build_gate.go — the build-tag gate (spec-agent redesign #369). A version tag
// names a buildable snapshot of the whole spec, so before the tag is cut the
// platform verifies, mechanically:
//
//   - design.cell exists and its facts parse;
//   - every story is claimed by at least one component — each component's
//     design.json carries the story IDs it serves (`F2.3`), and the union must
//     cover every story the feature files define (the coverage check — the
//     anti-disappearance net that keeps requirements from silently vanishing
//     between requirements and design);
//   - every deployable component is ENRICHED (its design.json moved off the
//     scaffold placeholder, a language decided) and carries its type-mandated
//     artifact (service → openapi.yaml, web-application → wireframes.dsl);
//   - a design with END-USER SIGN-IN carries specs/design/security.json, it parses,
//     and every story its roles cite is a real story. The platform creates
//     the roles and test users that file declares when the tag is built, so a
//     design that signs users in but declares no roles ships an app whose
//     role-gated behaviour nothing can exercise.
//
// Story-less infrastructure nodes (database, cache, …) are not deployable and
// never gate. Failures surface as FileValidationError rows through the
// existing SaveSpec 422 channel; nothing is tagged.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/reqspec"
	"github.com/wso2/aep/aep-api/internal/platform/securityspec"
)

// Build-gate error codes (join the designspec/save vocabulary the console
// renders).
const (
	codeMissingDesignCell  = "MISSING_DESIGN_CELL"
	codeInvalidDesignCell  = "INVALID_DESIGN_CELL"
	codeMissingUserStories = "MISSING_USER_STORIES"
	codeUncoveredStory     = "UNCOVERED_STORY"
	// codeStaleStoryCitation — a design.json cites a story the requirements
	// do not have: retired (the message names its replacement) or unknown.
	codeStaleStoryCitation       = "STALE_STORY_CITATION"
	codeUnenrichedComponent      = "UNENRICHED_COMPONENT"
	codeMissingComponentArtifact = "MISSING_COMPONENT_ARTIFACT"
	// codeMissingRolesDocument — the design has sign-in but declares no roles.
	codeMissingRolesDocument = "MISSING_ROLES_DOCUMENT"
	// codeInvalidRolesDocument — security.json does not parse, or breaks a
	// referential rule the platform depends on at build time.
	codeInvalidRolesDocument = "INVALID_ROLES_DOCUMENT"
	// codeUnknownRoleStory — a role cites a story the requirements do not define.
	codeUnknownRoleStory = "UNKNOWN_ROLE_STORY"
)

// deployableCellTypes maps a cell node type to the design.json component type
// it deploys as. Nodes typed otherwise (database, cache, identity-server, …)
// are dependencies of components, never component directories.
var deployableCellTypes = map[string]string{
	"service":         "service",
	"web-application": "web-application",
	"web-app":         "web-application",
	"worker":          "worker",
	"scheduled-task":  "scheduled-task",
}

// scaffoldLanguageSentinel is what the AE Studio pod's scaffold writes for
// `language`: a non-empty value (the schema demands one) that this gate
// REFUSES. Language is a judgment call the platform never makes — the agent
// fills it from the organization skill's Tech stack default, else the
// requirements, else the platform stack default the architecture skill names.
const scaffoldLanguageSentinel = "TBD"

// scaffoldPlaceholderMarker is how the gate tells a scaffold that was never
// enriched: the platform-authored description survives verbatim.
const scaffoldPlaceholderMarker = "Scaffolded from design.cell"

// validateBuildGate runs the build gate over the requirements bundle (keys
// relative to specs/requirements/) and the design bundle (keys relative to
// specs/design/). It returns FileValidationError rows (repo-relative paths are
// stamped by the caller) — empty means the gate passes.
//
// inScope is the story IDs the version carries: only they must be claimed.
func validateBuildGate(reqFiles, designFiles map[string]string, inScope map[string]bool) []FileValidationError {
	cellSource, ok := designFiles[DesignRootFile]
	if !ok || strings.TrimSpace(cellSource) == "" {
		return []FileValidationError{{
			Path: DesignRootFile, Code: codeMissingDesignCell,
			Message: "design.cell missing — the cell is the primary design source; generate the design before building",
		}}
	}
	facts, err := parseCellFacts(cellSource)
	if err != nil {
		return []FileValidationError{{Path: DesignRootFile, Code: codeInvalidDesignCell, Message: err.Error()}}
	}

	var errs []FileValidationError

	// Coverage: every story claimed by some component's design.json. A spec
	// with no readable stories is its own refusal — a silently empty set would
	// disarm the whole check.
	stories := reqspec.Parse(reqFiles).Stories()
	if len(stories) == 0 {
		errs = append(errs, FileValidationError{
			Path: DesignRootFile, Code: codeMissingUserStories,
			Message: "the requirements hold no stories to cover — each feature's stories go in the `## User Stories` of its features/F<n>-<slug>.md, one `- F<n>.<m> As a …` line each",
		})
	}
	defined := map[string]bool{}
	for _, st := range stories {
		defined[st.ID] = true
	}
	spec := reqspec.Parse(reqFiles)
	claimed := map[string]bool{}
	for _, c := range facts.Components {
		for _, id := range designJSONStories(designFiles["components/"+c.ID+"/design.json"]) {
			claimed[id] = true
			if !defined[id] {
				errs = append(errs, FileValidationError{
					Path: "components/" + c.ID + "/design.json", Code: codeStaleStoryCitation,
					Message: fmt.Sprintf("`stories` cites %s — %s", id, notAStory(spec, id, "cite")),
				})
			}
		}
	}
	for _, st := range stories {
		if inScope[st.ID] && !claimed[st.ID] {
			errs = append(errs, FileValidationError{
				Path: DesignRootFile, Code: codeUncoveredStory,
				Message: fmt.Sprintf("story %s is in the requirements but no component's design.json lists it in `stories` — extend the design or drop the story", st.ID),
			})
		}
	}

	errs = append(errs, validateRolesDocument(designFiles, spec)...)

	// The openapi.yaml SECURITY gate over EVERY component (task 1.6). The save
	// gate runs the same rules per file, but a save only ever holds the siblings
	// of that one write, and the agent's own write gate cannot see security.json
	// in a later turn's bundle at all — so this is the backstop where the whole
	// tag is present by construction and every scope can be judged against the
	// catalog that defines it.
	errs = append(errs, openapiSecurityFindings(designFiles)...)

	// Per-component completeness for deployable components.
	for _, c := range facts.Components {
		componentType, deployable := deployableCellTypes[strings.ToLower(strings.TrimSpace(c.Type))]
		if !deployable {
			continue
		}
		designPath := "components/" + c.ID + "/design.json"
		content, ok := designFiles[designPath]
		if !ok {
			errs = append(errs, FileValidationError{
				Path: designPath, Code: codeMissingComponentArtifact,
				Message: fmt.Sprintf("component %q has no design.json — save the design so the scaffold lands, then enrich it", c.ID),
			})
			continue
		}
		// Structured, not substring: the file is stored byte-verbatim as the
		// agent wrote it, so any whitespace/escaping variant must still read
		// as the same field values. Malformed JSON never reaches here — the
		// layout gates run first and own rejecting it.
		var doc struct {
			Language    string `json:"language"`
			Description string `json:"description"`
		}
		_ = json.Unmarshal([]byte(content), &doc)
		if strings.Contains(doc.Description, scaffoldPlaceholderMarker) {
			errs = append(errs, FileValidationError{
				Path: designPath, Code: codeUnenrichedComponent,
				Message: fmt.Sprintf("component %q is still the platform scaffold — enrich its design.json before building", c.ID),
			})
		} else if strings.TrimSpace(doc.Language) == scaffoldLanguageSentinel {
			errs = append(errs, FileValidationError{
				Path: designPath, Code: codeUnenrichedComponent,
				Message: fmt.Sprintf("component %q has no language decided — set it from the organization Tech stack default, the requirements, or the platform default", c.ID),
			})
		}
		var artifact string
		switch componentType {
		case "service":
			artifact = "openapi.yaml"
		case "web-application":
			artifact = "wireframes.dsl"
		}
		if artifact != "" {
			artifactPath := "components/" + c.ID + "/" + artifact
			if strings.TrimSpace(designFiles[artifactPath]) == "" {
				errs = append(errs, FileValidationError{
					Path: artifactPath, Code: codeMissingComponentArtifact,
					Message: fmt.Sprintf("component %q (%s) needs %s", c.ID, componentType, artifact),
				})
			}
		}
	}
	return errs
}

// validateRolesDocument checks the security design: the permission catalog, the
// roles that grant from it, the screens that require from it, and the test
// users.
//
// Presence is keyed on END-USER SIGN-IN, read off committed truth rather than a
// live catalog call: design-save already derives `exposesAPI.auth =
// end-user-required` onto every service that declares a platform-resource
// dependency whose resourceType carries the `aep.wso2.com/role: end-user-auth`
// marker (derive_auth.go). So the marker's consequence is already in the bundle,
// and the gate needs no cluster round-trip and no hardcoded resourceType name.
//
// The story cross-check lives here rather than in securityspec because only the
// gate sees the requirements: securityspec validates one file, this validates
// the bundle.
func validateRolesDocument(designFiles map[string]string, spec reqspec.Spec) []FileValidationError {
	stories := map[string]bool{}
	for _, st := range spec.Stories() {
		stories[st.ID] = true
	}
	raw, present := designFiles[securityspec.BundleKey]
	hasRoles := present && strings.TrimSpace(raw) != ""

	if !hasRoles {
		if !hasEndUserSignIn(designFiles) {
			return nil
		}
		return []FileValidationError{{
			Path: securityspec.BundleKey, Code: codeMissingRolesDocument,
			Message: "this design signs users in but declares no roles — write " +
				"specs/design/security.json with the roles the PRD's actors need and a test user " +
				"for each, or the platform has nothing to provision and validation cannot " +
				"exercise role-gated behaviour",
		}}
	}

	doc, err := securityspec.Parse([]byte(raw))
	if err != nil {
		var ve *securityspec.ValidationError
		msg := err.Error()
		if errors.As(err, &ve) {
			msg = ve.Message
		}
		return []FileValidationError{{
			Path: securityspec.BundleKey, Code: codeInvalidRolesDocument, Message: msg,
		}}
	}

	// The referential rules that need MORE than security.json: a permission's
	// component is a node of design.cell, and the two coverage warnings read
	// the owner components' openapi.yaml. Parse ran the document-only half
	// already; only the gate holds the whole bundle, where every sibling file is
	// present by construction.
	var errs []FileValidationError
	for _, finding := range securityspec.ReferenceFindings(doc, securityspec.DesignBundle(designFiles)) {
		if finding.Severity != securityspec.SeverityError {
			continue // warnings and info never refuse a build
		}
		errs = append(errs, FileValidationError{
			Path: securityspec.BundleKey, Code: codeInvalidRolesDocument, Message: finding.Message,
		})
	}

	// Every cited story is a real one. A role pointing at a story the
	// requirements do not have means the design and the requirements have
	// drifted, and the permissions it grants trace to nothing.
	for _, role := range doc.Roles {
		for _, id := range role.Stories {
			if !stories[id] {
				errs = append(errs, FileValidationError{
					Path: securityspec.BundleKey, Code: codeUnknownRoleStory,
					Message: fmt.Sprintf("role %q cites story %s — %s", role.Name, id, notAStory(spec, id, "cite")),
				})
			}
		}
	}
	return errs
}

// hasEndUserSignIn reports whether any component's design.json carries
// exposesAPI.auth = end-user-required — design-save's stamp for "this API sits
// behind the end-user login the SPA performs". It is the committed-truth
// signal that the design has sign-in at all.
func hasEndUserSignIn(designFiles map[string]string) bool {
	for rel, content := range designFiles {
		if !strings.HasSuffix(rel, "/design.json") {
			continue
		}
		var doc struct {
			ExposesAPI struct {
				Auth string `json:"auth"`
			} `json:"exposesAPI"`
		}
		if json.Unmarshal([]byte(content), &doc) != nil {
			continue
		}
		if doc.ExposesAPI.Auth == authEndUserRequired {
			return true
		}
	}
	return false
}

// notAStory says why a cited ID is not one of the requirements' stories, and
// what to cite instead when the requirements say. verb is what the citing
// file does with a story: a design.json cites one, an acceptance file tags one.
func notAStory(spec reqspec.Spec, id, verb string) string {
	c := spec.Cite(id)
	switch {
	case c.Status == reqspec.Live:
		return fmt.Sprintf("%s is not a story; %s the stories it holds", id, verb)
	case c.Status == reqspec.Retired && c.Replacement != "":
		return c.Describe(id) + "; " + verb + " " + c.Replacement
	case c.Status == reqspec.Retired:
		return c.Describe(id) + "; drop it"
	default:
		return c.Describe(id) + "; " + verb + " a real story or drop it"
	}
}

// componentStoryClaims maps each cell component to the stories its design.json
// claims — the ONE claims read both the gate (coverage union) and
// BuildScopeAtTag (per-component scope) consume, so they can never disagree on
// where claims come from.
func componentStoryClaims(facts *CellFacts, designFiles map[string]string) map[string][]string {
	out := map[string][]string{}
	for _, c := range facts.Components {
		if stories := designJSONStories(designFiles["components/"+c.ID+"/design.json"]); len(stories) > 0 {
			out[c.ID] = stories
		}
	}
	return out
}

// designJSONStories reads the story IDs a component's design.json claims.
// Malformed JSON or a missing field yields nothing — the design write-gates
// own rejecting bad JSON; this reader only collects claims.
func designJSONStories(content string) []string {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	var doc struct {
		Stories []string `json:"stories"`
	}
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return nil
	}
	out := make([]string, 0, len(doc.Stories))
	for _, id := range doc.Stories {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}
