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

// build_gate.go — the build-tag gate (spec-agent redesign #369). A `v<N>` tag
// names a buildable snapshot of the whole spec, so before the tag is cut the
// platform verifies, mechanically:
//
//   - design.cell exists and its facts parse;
//   - every PRD story is claimed by at least one component — each component's
//     design.json carries the `stories` it serves, and the union must cover
//     the PRD's User Stories list (the coverage check — the anti-disappearance
//     net that keeps requirements from silently vanishing between PRD and
//     design);
//   - every deployable component is ENRICHED (its design.json moved off the
//     scaffold placeholder, a language decided) and carries its type-mandated
//     artifact (service → openapi.yaml, web-application → wireframes.dsl);
//   - a design with END-USER SIGN-IN carries specs/design/security.json, it parses,
//     and every story its roles cite is a real PRD story. The platform creates
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
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/securityspec"
)

// Build-gate error codes (join the designspec/save vocabulary the console
// renders).
const (
	codeMissingDesignCell        = "MISSING_DESIGN_CELL"
	codeInvalidDesignCell        = "INVALID_DESIGN_CELL"
	codeMissingUserStories       = "MISSING_USER_STORIES"
	codeUncoveredStory           = "UNCOVERED_STORY"
	codeUnenrichedComponent      = "UNENRICHED_COMPONENT"
	codeMissingComponentArtifact = "MISSING_COMPONENT_ARTIFACT"
	// codeMissingRolesDocument — the design has sign-in but declares no roles.
	codeMissingRolesDocument = "MISSING_ROLES_DOCUMENT"
	// codeInvalidRolesDocument — security.json does not parse, or breaks a
	// referential rule the platform depends on at build time.
	codeInvalidRolesDocument = "INVALID_ROLES_DOCUMENT"
	// codeUnknownRoleStory — a role cites a PRD story that does not exist.
	codeUnknownRoleStory = "UNKNOWN_ROLE_STORY"
)

// The security design's NON-BLOCKING codes. They ride Warning, which the
// console renders beside a file, so they are spelled in the same error-code
// vocabulary as everything above rather than as the message table's key —
// the key names a SENTENCE, the code names a CHANNEL entry.
const (
	// codeSecurityHandleUsedNowhere — a catalog handle no operation and no
	// screen requires.
	codeSecurityHandleUsedNowhere = "SECURITY_HANDLE_USED_NOWHERE"
	// codeSecurityHandleUnreachable — a handle an operation requires that no
	// role grants.
	codeSecurityHandleUnreachable = "SECURITY_HANDLE_UNREACHABLE"
	// codeSecurityAssignToDirectoryChecked — INFO: a role delegates to a group
	// the org directory already holds, so the platform creates nothing.
	codeSecurityAssignToDirectoryChecked = "SECURITY_ASSIGN_TO_DIRECTORY_CHECKED"
	// codeSecurityDesignNotice — the fallback for a non-blocking finding this
	// file does not yet have a code for. A new securityspec rule reaches the
	// channel with a readable message rather than being dropped; giving it its
	// own code here is the follow-up.
	codeSecurityDesignNotice = "SECURITY_DESIGN_NOTICE"
)

// securityNoticeCodes maps a securityspec message key to the Warning code that
// carries it.
var securityNoticeCodes = map[string]string{
	securityspec.MsgHandleUsedNowhere:        codeSecurityHandleUsedNowhere,
	securityspec.MsgHandleUnreachable:        codeSecurityHandleUnreachable,
	securityspec.MsgAssignToDirectoryChecked: codeSecurityAssignToDirectoryChecked,
}

// scaffoldPlaceholderMarker is how the gate tells a scaffold that was never
// enriched: the platform-authored description survives verbatim.
const scaffoldPlaceholderMarker = "Scaffolded from design.cell"

// validateBuildGate runs the build gate over the requirements bundle (keys
// relative to specs/requirements/) and the design bundle (keys relative to
// specs/design/). It returns FileValidationError rows (repo-relative paths are
// stamped by the caller) — empty means the gate passes.
func validateBuildGate(reqFiles, designFiles map[string]string) []FileValidationError {
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

	// Coverage: every PRD story claimed by some component's design.json. An
	// unparseable story list is its own refusal — a silently empty set would
	// disarm the whole check.
	prdStories := parsePRDStories(reqFiles[requirementsMainFile])
	if len(prdStories) == 0 {
		errs = append(errs, FileValidationError{
			Path: DesignRootFile, Code: codeMissingUserStories,
			Message: "the PRD yields no stories to cover — its `## User Stories` section must hold a numbered `N. As a …` list",
		})
	}
	claimed := map[int]bool{}
	for _, stories := range componentStoryClaims(facts, designFiles) {
		for _, n := range stories {
			claimed[n] = true
		}
	}
	for _, n := range slices.Sorted(maps.Keys(prdStories)) {
		if !claimed[n] {
			errs = append(errs, FileValidationError{
				Path: DesignRootFile, Code: codeUncoveredStory,
				Message: fmt.Sprintf("story %d is in the PRD but no component's design.json lists it in `stories` — extend the design or drop the story", n),
			})
		}
	}

	errs = append(errs, validateRolesDocument(designFiles, prdStories)...)

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
// gate sees the PRD: securityspec validates one file, this validates the bundle.
func validateRolesDocument(designFiles map[string]string, prdStories map[int]string) []FileValidationError {
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
	// component is a node of design.cell, a screen exists in its component's
	// wireframes.dsl, and — the one that matters most — every role that reaches
	// a screen is granted the scope of the operation that screen loads. Parse
	// ran the document-only half already; only the gate holds the whole bundle,
	// where every sibling file is present by construction.
	var errs []FileValidationError
	for _, finding := range securityspec.ReferenceFindings(doc, securityspec.DesignBundle(designFiles)) {
		if finding.Severity != securityspec.SeverityError {
			continue // warnings and info ride buildGateWarnings, never a refusal
		}
		errs = append(errs, FileValidationError{
			Path: securityspec.BundleKey, Code: codeInvalidRolesDocument, Message: finding.Message,
		})
	}

	// Every cited story is a real one. A role pointing at a story the PRD does
	// not have means the design and the requirements have drifted, and the
	// permissions it grants trace to nothing.
	for _, role := range doc.Roles {
		for _, n := range role.Stories {
			if _, ok := prdStories[n]; !ok {
				errs = append(errs, FileValidationError{
					Path: securityspec.BundleKey, Code: codeUnknownRoleStory,
					Message: fmt.Sprintf("role %q cites story %d, which the PRD does not define — "+
						"cite a real story or drop it", role.Name, n),
				})
			}
		}
	}
	return errs
}

// buildGateWarnings are the security design's NON-BLOCKING findings over a
// whole design bundle — the two coverage warnings the design asks the Security
// page to render inline ("declared, used nowhere", "unreachable by any role")
// and the INFO note that a role's assignTo group is one the org directory
// already holds. None of them refuses anything: the design may legitimately be
// ahead of the code, and all three are the shape of a typo rather than proof of
// one. The errors are the gate's business and are excluded here — one defect,
// one message.
//
// They ride the existing soft-warning type rather than a new endpoint (plan §5
// decision 3); the apply path attaches them to ApplyResult.Warnings, SaveSpec
// logs them, and phase 5 gives the console a structured read.
//
// Paths are BUNDLE-relative (`security.json`), like every other row this file
// produces; a caller whose channel speaks repo paths prefixes DesignDir.
func buildGateWarnings(designFiles map[string]string) []Warning {
	raw, present := designFiles[securityspec.BundleKey]
	if !present || strings.TrimSpace(raw) == "" {
		return nil
	}
	doc, err := securityspec.Parse([]byte(raw))
	if err != nil {
		return nil // a document that does not parse is the gate's business, not a warning's
	}
	var out []Warning
	for _, finding := range securityspec.ReferenceFindings(doc, securityspec.DesignBundle(designFiles)) {
		if finding.Severity == securityspec.SeverityError {
			continue
		}
		code, ok := securityNoticeCodes[finding.Key]
		if !ok {
			code = codeSecurityDesignNotice
		}
		out = append(out, Warning{Path: securityspec.BundleKey, Code: code, Message: finding.Message})
	}
	return out
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

// componentStoryClaims maps each cell component to the stories its design.json
// claims — the ONE claims read both the gate (coverage union) and
// BuildScopeAtTag (per-component scope) consume, so they can never disagree on
// where claims come from.
func componentStoryClaims(facts *CellFacts, designFiles map[string]string) map[string][]int {
	out := map[string][]int{}
	for _, c := range facts.Components {
		if stories := designJSONStories(designFiles["components/"+c.ID+"/design.json"]); len(stories) > 0 {
			out[c.ID] = stories
		}
	}
	return out
}

// designJSONStories reads the `stories` list a component's design.json claims.
// Malformed JSON or a missing field yields nothing — the design write-gates
// own rejecting bad JSON; this reader only collects claims.
func designJSONStories(content string) []int {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	var doc struct {
		Stories []int `json:"stories"`
	}
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return nil
	}
	out := make([]int, 0, len(doc.Stories))
	for _, n := range doc.Stories {
		if n > 0 {
			out = append(out, n)
		}
	}
	return out
}

// storyLinePattern matches one numbered PRD story line: "7. As a member, ...".
// Leading whitespace is tolerated — markdown authors indent list items — and
// the title must contain a non-whitespace character; both rules mirror the
// console's cut-drawer preview (parsePrdStories), which must compute the same
// story set this gate does.
var storyLinePattern = regexp.MustCompile(`(?m)^\s*(\d+)\.\s+(\S.*)$`)

// parsePRDStories extracts story number → title from the PRD's
// "## User Stories" section ("N. <title>" lines).
func parsePRDStories(prd string) map[int]string {
	out := map[int]string{}
	for _, m := range storyLinePattern.FindAllStringSubmatch(markdownSection(prd, "User Stories"), -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 0 {
			continue
		}
		out[n] = strings.TrimSpace(m[2])
	}
	return out
}

// markdownSection returns the body of the `## <title>` section (up to the next
// `## ` heading), "" when absent.
func markdownSection(doc, title string) string {
	lines := strings.Split(doc, "\n")
	var body []string
	in := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			if in {
				break
			}
			in = strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")), title)
			continue
		}
		if in {
			body = append(body, line)
		}
	}
	return strings.Join(body, "\n")
}
