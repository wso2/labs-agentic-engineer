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
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/securityspec"
)

const gatePRD = `# Lunch — PRD

## User Stories

1. As a member, I want to browse today's order, so that I can join.
2. As a member, I want to add my item, so that it is counted.
4. As a coordinator, I want the round locked at cutoff, so that the order is final.
7. As a member, I want a Slack message on close, so that I don't miss it.
`

const gateCell = `component lunch-api service
component lunch-web web-application
component slack-notifier service
component orders-db database
`

func enriched(id, typ string, stories string) string {
	return `{"name":"` + id + `","type":"` + typ + `","version":"0.1.0","language":"Ballerina","buildpack":"docker","appPath":"` + id + `","entrypoint":"deployment/` + typ + `","exposure":"intranet","stories":[` + stories + `],"dependencies":[],"description":"real responsibility text"}`
}

func completeDesignFiles() map[string]string {
	return map[string]string{
		"design.cell":                            gateCell,
		"components/lunch-api/design.json":       enriched("lunch-api", "service", "1, 2, 4"),
		"components/lunch-api/openapi.yaml":      "openapi: 3.0.3\n",
		"components/lunch-web/design.json":       enriched("lunch-web", "web-application", "1, 2"),
		"components/lunch-web/prototype.json":    gatePrototype("Member"),
		"components/slack-notifier/design.json":  enriched("slack-notifier", "service", "7"),
		"components/slack-notifier/openapi.yaml": "openapi: 3.0.3\n",
	}
}

// gatePrototype is the smallest valid prototype.json for lunch-web: one screen
// every given role reaches. Role IDs are security.json role names, verbatim —
// the prototype skill's rule — so "Member" matches rolesDoc's only role.
func gatePrototype(roleIDs ...string) string {
	roles := make([]string, 0, len(roleIDs))
	quoted := make([]string, 0, len(roleIDs))
	for _, id := range roleIDs {
		roles = append(roles, `{"id":"`+id+`","name":"`+id+`"}`)
		quoted = append(quoted, `"`+id+`"`)
	}
	return `{"schemaVersion":1,"component":"lunch-web","name":"Lunch","defaultScreenId":"screen.home",` +
		`"roles":[` + strings.Join(roles, ",") + `],"states":[{"id":"state.default","name":"Default"}],` +
		`"flows":[],"navigation":[],` +
		`"screens":[{"id":"screen.home","name":"Home","roleIds":[` + strings.Join(quoted, ",") + `],"content":[]}]}`
}

func gateErrors(t *testing.T, designFiles map[string]string) []FileValidationError {
	t.Helper()
	return validateBuildGate(map[string]string{requirementsMainFile: gatePRD}, designFiles)
}

func codesOf(errs []FileValidationError) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, e.Code)
	}
	return out
}

func TestBuildGate_CompleteDesignPasses(t *testing.T) {
	errs := gateErrors(t, completeDesignFiles())
	if len(errs) != 0 {
		t.Fatalf("complete design should pass, got %+v", errs)
	}
}

func TestBuildGate_MissingCell(t *testing.T) {
	errs := validateBuildGate(map[string]string{requirementsMainFile: gatePRD}, map[string]string{})
	if len(errs) != 1 || errs[0].Code != "MISSING_DESIGN_CELL" {
		t.Fatalf("want MISSING_DESIGN_CELL, got %+v", errs)
	}
}

// Every PRD story must be claimed by some component's design.json `stories` —
// the anti-disappearance net between PRD and design.
func TestBuildGate_UncoveredStory(t *testing.T) {
	files := completeDesignFiles()
	files["components/lunch-api/design.json"] = enriched("lunch-api", "service", "1, 4")
	files["components/lunch-web/design.json"] = enriched("lunch-web", "web-application", "1")
	errs := gateErrors(t, files)
	found := false
	for _, e := range errs {
		if e.Code == "UNCOVERED_STORY" && strings.Contains(e.Message, "story 2") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want UNCOVERED_STORY for story 2, got %+v", errs)
	}
}

func TestBuildGate_DeployableComponentDemandsArtifactsAndEnrichment(t *testing.T) {
	files := completeDesignFiles()
	delete(files, "components/lunch-api/openapi.yaml")
	files["components/lunch-web/design.json"] = renderScaffold("lunch-web", "web-application")
	errs := gateErrors(t, files)
	codes := strings.Join(codesOf(errs), ",")
	if !strings.Contains(codes, "MISSING_COMPONENT_ARTIFACT") {
		t.Errorf("want MISSING_COMPONENT_ARTIFACT for lunch-api openapi.yaml, got %+v", errs)
	}
	if !strings.Contains(codes, "UNENRICHED_COMPONENT") {
		t.Errorf("want UNENRICHED_COMPONENT for scaffold-placeholder lunch-web, got %+v", errs)
	}
}

// Infrastructure nodes (database, cache, …) are not deployable: no design.json
// directory, no artifact, and the gate must never ask for one.
func TestBuildGate_InfrastructureExempt(t *testing.T) {
	files := completeDesignFiles()
	errs := gateErrors(t, files)
	for _, e := range errs {
		if strings.Contains(e.Path, "orders-db") {
			t.Errorf("infrastructure leaked into the gate: %+v", e)
		}
	}
}

// TestBuildGate_LanguageSentinelRefused pins that the platform never decides a
// component's language: a design.json enriched everywhere EXCEPT the
// scaffold's "TBD" language sentinel still refuses the tag — the agent must
// set it (org Tech stack default → requirements → platform default).
func TestBuildGate_LanguageSentinelRefused(t *testing.T) {
	files := completeDesignFiles()
	files["components/lunch-api/design.json"] = strings.Replace(
		enriched("lunch-api", "service", "1, 2, 4"), `"language":"Ballerina"`, `"language":"TBD"`, 1)
	errs := gateErrors(t, files)
	found := false
	for _, e := range errs {
		if e.Code == "UNENRICHED_COMPONENT" && strings.Contains(e.Message, "language") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want UNENRICHED_COMPONENT for the TBD language sentinel, got %+v", errs)
	}
}

func TestParsePRDStories(t *testing.T) {
	stories := parsePRDStories(gatePRD)
	if got := slices.Sorted(maps.Keys(stories)); !reflect.DeepEqual(got, []int{1, 2, 4, 7}) {
		t.Fatalf("story numbers = %v, want [1 2 4 7]", got)
	}
	if !strings.Contains(stories[4], "locked at cutoff") {
		t.Errorf("story 4 title = %q", stories[4])
	}
	if len(parsePRDStories("# PRD\n\nno stories section")) != 0 {
		t.Error("PRD without a User Stories section should yield no stories")
	}
	// Markdown authors indent list items; the gate and the console preview
	// must read them the same way.
	indented := "## User Stories\n\n  1. As a user, I want A, so that a.\n  2. As a user, I want B, so that b.\n"
	if got := slices.Sorted(maps.Keys(parsePRDStories(indented))); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Errorf("indented story numbers = %v, want [1 2]", got)
	}
	// A numbered item with a whitespace-only title is not a story — the same
	// rule the console preview applies, or the drawer would preview no story
	// while the gate demands coverage for it.
	blank := "## User Stories\n\n1. As a user, I want A, so that a.\n2.   \n"
	if got := slices.Sorted(maps.Keys(parsePRDStories(blank))); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("whitespace-only item counted as a story: %v, want [1]", got)
	}
}

// TestBuildGate_FormattedSentinelRefused pins the STRUCTURED enrichment read:
// design.json is stored byte-verbatim as the agent wrote it, so a formatting
// variant a substring check would miss ("language" : "TBD") must still refuse
// the tag.
func TestBuildGate_FormattedSentinelRefused(t *testing.T) {
	files := completeDesignFiles()
	files["components/lunch-api/design.json"] = "{\n  \"name\": \"lunch-api\",\n  \"type\": \"service\",\n  \"version\": \"0.1.0\",\n  \"language\" : \"TBD\",\n  \"buildpack\": \"docker\",\n  \"appPath\": \"lunch-api\",\n  \"entrypoint\": \"deployment/service\",\n  \"exposure\": \"intranet\",\n  \"stories\": [1, 2, 4],\n  \"dependencies\": [],\n  \"description\": \"real responsibility text\"\n}"
	errs := gateErrors(t, files)
	found := false
	for _, e := range errs {
		if e.Code == "UNENRICHED_COMPONENT" && strings.Contains(e.Message, "language") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want UNENRICHED_COMPONENT for the formatted TBD sentinel, got %+v", errs)
	}
}

// A PRD whose User Stories section yields no numbered stories must refuse the
// tag rather than silently disarm the coverage check.
func TestBuildGate_UnparseableStoriesRefused(t *testing.T) {
	files := completeDesignFiles()
	errs := validateBuildGate(map[string]string{
		requirementsMainFile: "# PRD\n\n## User Stories\n\n- As a user, I want bullets, so that no numbers parse.\n",
	}, files)
	found := false
	for _, e := range errs {
		if e.Code == "MISSING_USER_STORIES" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want MISSING_USER_STORIES, got %+v", errs)
	}
}

// A design.json with malformed JSON or no stories field claims nothing — the
// write-gates own rejecting bad JSON; the gate only collects claims.
func TestDesignJSONStories(t *testing.T) {
	if got := designJSONStories(`{"stories": [3, 1]}`); !reflect.DeepEqual(got, []int{3, 1}) {
		t.Errorf("stories = %v, want [3 1]", got)
	}
	for _, content := range []string{"", "not json", `{"name":"x"}`} {
		if got := designJSONStories(content); len(got) != 0 {
			t.Errorf("designJSONStories(%q) = %v, want none", content, got)
		}
	}
}

// ---- the roles document ----------------------------------------------------
//
// The security design is the ONE spec file the platform acts on
// deterministically at build time: it creates the roles and test users
// the file declares. These pin the three things the gate owns about it —
// presence when the design signs users in, parseability, and that the stories
// its roles cite are real.

// authService is a service the design-save auth derivation has already stamped
// as sitting behind end-user sign-in. That stamp, not a live catalog call, is
// what tells the gate this design has sign-in.
func authService(id, stories string) string {
	return `{"name":"` + id + `","type":"service","version":"0.1.0","language":"Ballerina",` +
		`"buildpack":"docker","appPath":"` + id + `","entrypoint":"deployment/service",` +
		`"exposure":"intranet","stories":[` + stories + `],"dependencies":[],` +
		`"description":"real responsibility text","exposesAPI":{"auth":"end-user-required"}}`
}

// rolesDoc is a security.json v3 for the lunch design: one resource owned by
// lunch-api, one role that grants from it and is assigned to an org group, one
// test user.
func rolesDoc(stories string) string {
	return `{"version":3,` +
		`"permissions":[{"resource":"rounds","component":"lunch-api","actions":[` +
		`{"handle":"read"},{"handle":"join"}]}],` +
		`"groups":[{"name":"Lunch Members","description":"Everyone who orders lunch"}],` +
		`"roles":[{"name":"Member","description":"Joins today's order.","stories":[` + stories + `],` +
		`"grants":["rounds:read","rounds:join"],"assignTo":["Lunch Members"]}],` +
		`"testUsers":[{"username":"test-member","roles":["Member"]}]}`
}

// protectedStubSpec is the smallest openapi.yaml a component BEHIND SIGN-IN can
// carry: the oauth2 scheme the gateway and the generated server are rendered
// from, and the document default that makes an operation whose security block
// is forgotten fail closed. Without both, the openapi security gate refuses the
// component — which is the whole point of it.
const protectedStubSpec = `openapi: 3.0.3
components:
  securitySchemes:
    oauth2:
      type: oauth2
security:
  - oauth2: []
paths: {}
`

// lunchAPISpec is lunch-api's openapi.yaml with real operations, so the rules
// that read a component spec — the two coverage warnings — have something to
// read.
const lunchAPISpec = `openapi: 3.0.3
components:
  securitySchemes:
    oauth2:
      type: oauth2
security:
  - oauth2: []
paths:
  /rounds:
    get:
      security: [{oauth2: [rounds:read]}]
  /rounds/join:
    post:
      security: [{oauth2: [rounds:join]}]
`

// signInDesignFiles is the complete design with lunch-api moved behind end-user
// sign-in: the design.json the auth derivation stamped AND the openapi.yaml a
// protected component must then carry. The two travel together because the
// platform's own gates treat them as one fact.
func signInDesignFiles() map[string]string {
	files := completeDesignFiles()
	files["components/lunch-api/design.json"] = authService("lunch-api", "1, 2, 4")
	files["components/lunch-api/openapi.yaml"] = protectedStubSpec
	return files
}

// A design with no sign-in needs no roles document — most designs are this.
func TestBuildGate_NoSignInNeedsNoRolesDocument(t *testing.T) {
	if errs := gateErrors(t, completeDesignFiles()); len(errs) != 0 {
		t.Fatalf("a design with no sign-in should not be asked for security.json, got %+v", errs)
	}
}

// The one that matters: sign-in without a roles document ships an app whose
// role-gated behaviour nothing can exercise, because the platform has no roles
// or test users to create and validation has no login to sign in as.
func TestBuildGate_SignInWithoutRolesDocument(t *testing.T) {
	files := signInDesignFiles()

	errs := gateErrors(t, files)
	if !slices.Contains(codesOf(errs), codeMissingRolesDocument) {
		t.Fatalf("want %s, got %+v", codeMissingRolesDocument, errs)
	}
}

func TestBuildGate_SignInWithARolesDocumentPasses(t *testing.T) {
	files := signInDesignFiles()
	files["security.json"] = rolesDoc("1, 2")

	if errs := gateErrors(t, files); len(errs) != 0 {
		t.Fatalf("want a clean gate, got %+v", errs)
	}
}

// A roles document that acquired a tag but does not parse is a hard failure, not
// a warning: the platform provisions credentials from it.
func TestBuildGate_UnparseableRolesDocument(t *testing.T) {
	files := signInDesignFiles()
	files["security.json"] = `{"version":1,`

	errs := gateErrors(t, files)
	if !slices.Contains(codesOf(errs), codeInvalidRolesDocument) {
		t.Fatalf("want %s, got %+v", codeInvalidRolesDocument, errs)
	}
}

// A referential rule securityspec owns surfaces through the same gate code, so
// the two halves of the validation cannot drift apart.
func TestBuildGate_RolesDocumentBreakingAReferentialRule(t *testing.T) {
	files := signInDesignFiles()
	// A grant naming a handle the catalog does not declare.
	files["security.json"] = strings.Replace(rolesDoc("1"), `"rounds:join"`, `"rounds:audit"`, 1)

	errs := gateErrors(t, files)
	if !slices.Contains(codesOf(errs), codeInvalidRolesDocument) {
		t.Fatalf("want %s, got %+v", codeInvalidRolesDocument, errs)
	}
}

// The rules that need MORE than security.json run here and nowhere else: only
// the gate holds the cell and every component spec at once.
func TestBuildGate_RulesThatNeedTheWholeBundle(t *testing.T) {
	cases := map[string]func(files map[string]string){
		"a resource owned by a component the cell does not declare": func(files map[string]string) {
			files["security.json"] = strings.Replace(rolesDoc("1"), `"component":"lunch-api"`, `"component":"ghost-api"`, 1)
		},
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			files := signInDesignFiles()
			files["security.json"] = rolesDoc("1")
			edit(files)

			errs := gateErrors(t, files)
			if !slices.Contains(codesOf(errs), codeInvalidRolesDocument) {
				t.Fatalf("want %s, got %+v", codeInvalidRolesDocument, errs)
			}
		})
	}
}

// The two coverage warnings are NON-BLOCKING: they never appear among the gate's
// errors, and they do name the handle nobody uses and the one nobody can reach.
func TestBuildGate_CoverageWarningsDoNotBlock(t *testing.T) {
	files := signInDesignFiles()
	files["components/lunch-api/openapi.yaml"] = lunchAPISpec
	// `rounds:audit` is declared and used by nothing; `rounds:join` is required
	// by POST /rounds/join and granted by nobody once the role drops it.
	doc := strings.Replace(rolesDoc("1"),
		`{"handle":"join"}`,
		`{"handle":"join"},{"handle":"audit"}`, 1)
	files["security.json"] = strings.Replace(doc, `"grants":["rounds:read","rounds:join"]`, `"grants":["rounds:read"]`, 1)

	if errs := gateErrors(t, files); len(errs) != 0 {
		t.Fatalf("a coverage warning must not fail the gate, got %+v", errs)
	}
	var codes []string
	for _, w := range buildGateWarnings(files) {
		codes = append(codes, w.Code)
		// Bundle-relative, like every row this file produces; the apply path
		// prefixes DesignDir for the channel that speaks repo paths.
		if w.Path != securityspec.BundleKey {
			t.Errorf("warning path %q is not bundle-relative", w.Path)
		}
	}
	for _, want := range []string{codeSecurityHandleUsedNowhere, codeSecurityHandleUnreachable} {
		if !slices.Contains(codes, want) {
			t.Fatalf("want a %s warning, got %v", want, codes)
		}
	}
}

// INFO is not dropped: a role whose assignTo names a group the document does
// not declare is a deliberate delegation to the org directory, and the record
// of that decision rides the same channel as the warnings.
func TestBuildGate_DirectoryCheckedNoteRidesTheWarningsChannel(t *testing.T) {
	files := signInDesignFiles()
	files["components/lunch-api/openapi.yaml"] = lunchAPISpec
	// `Finance` is not one of the document's own groups, so it can only be one
	// the org directory already holds.
	files["security.json"] = strings.Replace(rolesDoc("1"),
		`"assignTo":["Lunch Members"]`, `"assignTo":["Finance"]`, 1)

	var codes []string
	for _, w := range buildGateWarnings(files) {
		codes = append(codes, w.Code)
	}
	if !slices.Contains(codes, codeSecurityAssignToDirectoryChecked) {
		t.Fatalf("want a %s note, got %v", codeSecurityAssignToDirectoryChecked, codes)
	}
}

// A role citing a story the PRD does not define means the design and the
// requirements have drifted, and the permissions it grants trace to nothing.
// The gate is the only place this is checkable: securityspec validates one file,
// and only the gate also sees the PRD.
func TestBuildGate_RoleCitingAStoryThePRDDoesNotDefine(t *testing.T) {
	files := signInDesignFiles()
	files["security.json"] = rolesDoc("1, 99")

	errs := gateErrors(t, files)
	if !slices.Contains(codesOf(errs), codeUnknownRoleStory) {
		t.Fatalf("want %s, got %+v", codeUnknownRoleStory, errs)
	}
	for _, e := range errs {
		if e.Code == codeUnknownRoleStory && !strings.Contains(e.Message, "99") {
			t.Fatalf("message should name the offending story: %q", e.Message)
		}
	}
}

// A roles document present on a design with NO sign-in is still validated —
// it is the same file the platform will provision from either way.
func TestBuildGate_RolesDocumentValidatedEvenWithoutSignIn(t *testing.T) {
	files := completeDesignFiles()
	files["security.json"] = rolesDoc("1, 99")

	errs := gateErrors(t, files)
	if !slices.Contains(codesOf(errs), codeUnknownRoleStory) {
		t.Fatalf("want %s, got %+v", codeUnknownRoleStory, errs)
	}
}

// ---- the web-application prototype ------------------------------------------
//
// A web-application's build artifact is its prototype.json (#820): the review
// the user did on the Prototype stage is what Build approves. wireframes.dsl is
// neither required nor sufficient.

const gatePrototypeKey = "components/lunch-web/prototype.json"

func gateRow(errs []FileValidationError, path, code string) (FileValidationError, bool) {
	for _, e := range errs {
		if e.Path == path && e.Code == code {
			return e, true
		}
	}
	return FileValidationError{}, false
}

func TestBuildGate_AValidPrototypeWithoutDSLPasses(t *testing.T) {
	files := completeDesignFiles()
	if _, ok := files["components/lunch-web/wireframes.dsl"]; ok {
		t.Fatal("fixture drift: the complete design must not carry wireframes.dsl")
	}
	if errs := gateErrors(t, files); len(errs) != 0 {
		t.Fatalf("a valid prototype and no DSL should pass, got %+v", errs)
	}
}

func TestBuildGate_MissingPrototypeNamesPrototypeJSON(t *testing.T) {
	files := completeDesignFiles()
	delete(files, gatePrototypeKey)

	row, ok := gateRow(gateErrors(t, files), gatePrototypeKey, codeMissingComponentArtifact)
	if !ok {
		t.Fatalf("want %s at %s, got %+v", codeMissingComponentArtifact, gatePrototypeKey, gateErrors(t, files))
	}
	if !strings.Contains(row.Message, "prototype.json") || !strings.Contains(row.Message, "lunch-web") {
		t.Errorf("message should name the component and prototype.json: %q", row.Message)
	}
}

// wireframes.dsl alone no longer satisfies a new Build.
func TestBuildGate_WireframesDSLAloneIsNotEnough(t *testing.T) {
	files := completeDesignFiles()
	delete(files, gatePrototypeKey)
	files["components/lunch-web/wireframes.dsl"] = "screen home\n"

	if _, ok := gateRow(gateErrors(t, files), gatePrototypeKey, codeMissingComponentArtifact); !ok {
		t.Fatalf("wireframes.dsl must not stand in for prototype.json, got %+v", gateErrors(t, files))
	}
}

// A blank prototype.json is a missing one, the same as a blank openapi.yaml —
// at Build, not refused earlier as malformed JSON by the save gate.
func TestBuild_BlankPrototypeIsMissing(t *testing.T) {
	for name, body := range map[string]string{"empty": "", "whitespace": "  \n\t"} {
		t.Run(name, func(t *testing.T) {
			files := completeDesignFiles()
			files[gatePrototypeKey] = body

			rows := specRows(t, validateSpecBundles(map[string]string{requirementsMainFile: gatePRD}, files))
			if len(rows) != 1 || rows[0].Path != DesignDir+"/"+gatePrototypeKey || rows[0].Code != codeMissingComponentArtifact {
				t.Fatalf("want one %s row at %s, got %+v", codeMissingComponentArtifact, gatePrototypeKey, rows)
			}
		})
	}
}

// Build refuses a present-but-invalid prototype through the design bundle's
// validation, which runs first, with prototypespec's own stable codes — the
// codes the save gate names for the same file. The build gate adds no second
// row for it.
func TestBuild_InvalidPrototypeRefusedWithItsOwnCode(t *testing.T) {
	cases := map[string]struct{ body, code string }{
		"not JSON":          {"{", "INVALID_JSON"},
		"schema violation":  {`{"schemaVersion":1}`, "SCHEMA_VIOLATION"},
		"another component": {strings.Replace(gatePrototype("Member"), `"component":"lunch-web"`, `"component":"lunch-api"`, 1), "PROTOTYPE_COMPONENT_MISMATCH"},
		"dangling reference": {strings.Replace(gatePrototype("Member"),
			`"defaultScreenId":"screen.home"`, `"defaultScreenId":"screen.ghost"`, 1), "UNKNOWN_REFERENCE"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			files := completeDesignFiles()
			files[gatePrototypeKey] = c.body

			rows := specRows(t, validateSpecBundles(map[string]string{requirementsMainFile: gatePRD}, files))
			for _, row := range rows {
				if row.Path != DesignDir+"/"+gatePrototypeKey || row.Code != c.code {
					t.Fatalf("want only %s rows at %s, got %+v", c.code, gatePrototypeKey, rows)
				}
				if strings.TrimSpace(row.Message) == "" {
					t.Error("an invalid prototype row must say what is wrong")
				}
			}
			if len(rows) == 0 {
				t.Fatalf("an invalid prototype must refuse Build")
			}
			if errs := gateErrors(t, files); len(errs) != 0 {
				t.Errorf("the build gate leaves an invalid prototype to the design validation, got %+v", errs)
			}
		})
	}
}

func specRows(t *testing.T, err error) []FileValidationError {
	t.Helper()
	var verr *SpecValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("want *SpecValidationError, got %T: %v", err, err)
	}
	return verr.Files
}

func TestBuildGate_PrototypeRoleMustBeADeclaredSecurityRole(t *testing.T) {
	files := signInDesignFiles()
	files["security.json"] = rolesDoc("1, 2")
	files[gatePrototypeKey] = gatePrototype("Member", "Coordinator")

	errs := gateErrors(t, files)
	row, ok := gateRow(errs, gatePrototypeKey, codeUnknownPrototypeRole)
	if !ok {
		t.Fatalf("want %s, got %+v", codeUnknownPrototypeRole, errs)
	}
	if !strings.Contains(row.Message, `"Coordinator"`) {
		t.Errorf("message should name the undeclared role: %q", row.Message)
	}
	if len(errs) != 1 {
		t.Errorf("only the undeclared role refuses, got %+v", errs)
	}
}

// Without security.json the prototype's roles are free-form.
func TestBuildGate_PrototypeRolesAreFreeWithoutASecurityDesign(t *testing.T) {
	files := completeDesignFiles()
	files[gatePrototypeKey] = gatePrototype("anyone")

	if errs := gateErrors(t, files); len(errs) != 0 {
		t.Fatalf("roles are free-form without security.json, got %+v", errs)
	}
}

// An unparseable security.json is INVALID_ROLES_DOCUMENT's business; the role
// cross-check has nothing to compare against and adds no noise.
func TestBuildGate_UnparseableSecurityDesignSkipsTheRoleCheck(t *testing.T) {
	files := signInDesignFiles()
	files["security.json"] = `{"version":1,`

	errs := gateErrors(t, files)
	if slices.Contains(codesOf(errs), codeUnknownPrototypeRole) {
		t.Fatalf("no role check against an unparseable security.json, got %+v", errs)
	}
}
