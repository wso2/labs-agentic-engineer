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
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/reqspec"
)

// gateRequirements is the lunch product's requirements folder: two features
// with stories (F1.3 retired, so the numbers have a gap), and a stub the
// coverage check must not ask about.
var gateRequirements = map[string]string{
	"prd.md": "# Lunch\n\n## Features\n\n- F1 [Ordering](features/F1-ordering.md)\n" +
		"- F2 [Notifications](features/F2-notifications.md)\n- F3 [Reports](features/F3-reports.md)\n",
	"features/F1-ordering.md": `# Ordering

## User Stories

- F1.1 As a member, I browse today's order, so that I can join.
- F1.2 As a member, I add my item, so that it is counted.
- F1.4 As a coordinator, I lock the round at cutoff, so that the order is final.

## Retired

- F1.3 dropped
`,
	"features/F2-notifications.md": "# Notifications\n\n## User Stories\n\n- F2.1 As a member, I get a Slack message on close, so that I don't miss it.\n",
	"features/F3-reports.md":       "# Reports\n\n## Purpose\n\nSpend by team.\n",
}

const gateCell = `component lunch-api service
component lunch-web web-application
component slack-notifier service
component orders-db database
`

// storyList renders story IDs as the body of a JSON array.
func storyList(ids []string) string {
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = `"` + id + `"`
	}
	return strings.Join(quoted, ",")
}

func enriched(id, typ string, stories ...string) string {
	return `{"name":"` + id + `","type":"` + typ + `","version":"0.1.0","language":"Ballerina","buildpack":"docker","appPath":"` + id + `","entrypoint":"deployment/` + typ + `","exposure":"intranet","stories":[` + storyList(stories) + `],"dependencies":[],"description":"real responsibility text"}`
}

func completeDesignFiles() map[string]string {
	return map[string]string{
		"design.cell":                            gateCell,
		"components/lunch-api/design.json":       enriched("lunch-api", "service", "F1.1", "F1.2", "F1.4"),
		"components/lunch-api/openapi.yaml":      "openapi: 3.0.3\n",
		"components/lunch-web/design.json":       enriched("lunch-web", "web-application", "F1.1", "F1.2"),
		"components/lunch-web/wireframes.dsl":    "screen home\n",
		"components/slack-notifier/design.json":  enriched("slack-notifier", "service", "F2.1"),
		"components/slack-notifier/openapi.yaml": "openapi: 3.0.3\n",
	}
}

func gateErrors(t *testing.T, designFiles map[string]string) []FileValidationError {
	t.Helper()
	return validateBuildGate(gateRequirements, designFiles, everyStory(gateRequirements))
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
	errs := validateBuildGate(gateRequirements, map[string]string{}, everyStory(gateRequirements))
	if len(errs) != 1 || errs[0].Code != "MISSING_DESIGN_CELL" {
		t.Fatalf("want MISSING_DESIGN_CELL, got %+v", errs)
	}
}

// Every story must be claimed by some component's design.json `stories` —
// the anti-disappearance net between requirements and design. The stub F3
// has no stories and is not asked about.
func TestBuildGate_UncoveredStory(t *testing.T) {
	files := completeDesignFiles()
	files["components/lunch-api/design.json"] = enriched("lunch-api", "service", "F1.1", "F1.4")
	files["components/lunch-web/design.json"] = enriched("lunch-web", "web-application", "F1.1")
	errs := gateErrors(t, files)
	found := false
	for _, e := range errs {
		if e.Code == "UNCOVERED_STORY" && strings.Contains(e.Message, "story F1.2") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want UNCOVERED_STORY for story F1.2, got %+v", errs)
	}
}

func TestBuildGate_DeployableComponentDemandsArtifactsAndEnrichment(t *testing.T) {
	files := completeDesignFiles()
	delete(files, "components/lunch-api/openapi.yaml")
	// The pod's scaffold for lunch-web, never enriched.
	files["components/lunch-web/design.json"] = `{"name":"lunch-web","type":"web-application","version":"0.1.0",` +
		`"language":"TBD","buildpack":"docker","appPath":"lunch-web","entrypoint":"deployment/web-application",` +
		`"exposure":"internet","dependencies":[],"description":"Scaffolded from design.cell — enrich it."}`
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
		enriched("lunch-api", "service", "F1.1", "F1.2", "F1.4"), `"language":"Ballerina"`, `"language":"TBD"`, 1)
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

// TestBuildGate_FormattedSentinelRefused pins the STRUCTURED enrichment read:
// design.json is stored byte-verbatim as the agent wrote it, so a formatting
// variant a substring check would miss ("language" : "TBD") must still refuse
// the tag.
func TestBuildGate_FormattedSentinelRefused(t *testing.T) {
	files := completeDesignFiles()
	files["components/lunch-api/design.json"] = "{\n  \"name\": \"lunch-api\",\n  \"type\": \"service\",\n  \"version\": \"0.1.0\",\n  \"language\" : \"TBD\",\n  \"buildpack\": \"docker\",\n  \"appPath\": \"lunch-api\",\n  \"entrypoint\": \"deployment/service\",\n  \"exposure\": \"intranet\",\n  \"stories\": [\"F1.1\", \"F1.2\", \"F1.4\"],\n  \"dependencies\": [],\n  \"description\": \"real responsibility text\"\n}"
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

// Requirements with no readable stories must refuse the tag rather than
// silently disarm the coverage check. A flat PRD's numbered User Stories are
// exactly that: stories live in feature files now (skills/prd-contract), and
// a project written before them is not read.
func TestBuildGate_UnparseableStoriesRefused(t *testing.T) {
	files := completeDesignFiles()
	errs := validateBuildGate(map[string]string{
		"prd.md": "# PRD\n\n## User Stories\n\n1. As a user, I want A, so that a.\n",
	}, files, nil)
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
	if got := designJSONStories(`{"stories": ["F2.1", " F1.3 ", ""]}`); !reflect.DeepEqual(got, []string{"F2.1", "F1.3"}) {
		t.Errorf("stories = %v, want [F2.1 F1.3]", got)
	}
	for _, content := range []string{"", "not json", `{"name":"x"}`, `{"stories": [1, 2]}`} {
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
func authService(id string, stories ...string) string {
	return `{"name":"` + id + `","type":"service","version":"0.1.0","language":"Ballerina",` +
		`"buildpack":"docker","appPath":"` + id + `","entrypoint":"deployment/service",` +
		`"exposure":"intranet","stories":[` + storyList(stories) + `],"dependencies":[],` +
		`"description":"real responsibility text","exposesAPI":{"auth":"end-user-required"}}`
}

// rolesDoc is a security.json v3 for the lunch design: one resource owned by
// lunch-api, one role that grants from it and is assigned to an org group, one
// test user.
func rolesDoc(stories ...string) string {
	return `{"version":3,` +
		`"permissions":[{"resource":"rounds","component":"lunch-api","actions":[` +
		`{"handle":"read"},{"handle":"join"}]}],` +
		`"groups":[{"name":"Lunch Members","description":"Everyone who orders lunch"}],` +
		`"roles":[{"name":"Member","description":"Joins today's order.","stories":[` + storyList(stories) + `],` +
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
	files["components/lunch-api/design.json"] = authService("lunch-api", "F1.1", "F1.2", "F1.4")
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
	files["security.json"] = rolesDoc("F1.1", "F1.2")

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
	files["security.json"] = strings.Replace(rolesDoc("F1.1"), `"rounds:join"`, `"rounds:audit"`, 1)

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
			files["security.json"] = strings.Replace(rolesDoc("F1.1"), `"component":"lunch-api"`, `"component":"ghost-api"`, 1)
		},
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			files := signInDesignFiles()
			files["security.json"] = rolesDoc("F1.1")
			edit(files)

			errs := gateErrors(t, files)
			if !slices.Contains(codesOf(errs), codeInvalidRolesDocument) {
				t.Fatalf("want %s, got %+v", codeInvalidRolesDocument, errs)
			}
		})
	}
}

// The two coverage warnings are NON-BLOCKING: they never appear among the
// gate's errors.
func TestBuildGate_CoverageWarningsDoNotBlock(t *testing.T) {
	files := signInDesignFiles()
	files["components/lunch-api/openapi.yaml"] = lunchAPISpec
	// `rounds:audit` is declared and used by nothing; `rounds:join` is required
	// by POST /rounds/join and granted by nobody once the role drops it.
	doc := strings.Replace(rolesDoc("F1.1"),
		`{"handle":"join"}`,
		`{"handle":"join"},{"handle":"audit"}`, 1)
	files["security.json"] = strings.Replace(doc, `"grants":["rounds:read","rounds:join"]`, `"grants":["rounds:read"]`, 1)

	if errs := gateErrors(t, files); len(errs) != 0 {
		t.Fatalf("a coverage warning must not fail the gate, got %+v", errs)
	}
}

// A role citing a story the requirements do not define means the design and the
// requirements have drifted, and the permissions it grants trace to nothing.
// The gate is the only place this is checkable: securityspec validates one file,
// and only the gate also sees the requirements.
func TestBuildGate_RoleCitingAStoryThePRDDoesNotDefine(t *testing.T) {
	files := signInDesignFiles()
	files["security.json"] = rolesDoc("F1.1", "F9.9")

	errs := gateErrors(t, files)
	if !slices.Contains(codesOf(errs), codeUnknownRoleStory) {
		t.Fatalf("want %s, got %+v", codeUnknownRoleStory, errs)
	}
	for _, e := range errs {
		if e.Code == codeUnknownRoleStory && !strings.Contains(e.Message, "F9.9") {
			t.Fatalf("message should name the offending story: %q", e.Message)
		}
	}
}

// A roles document present on a design with NO sign-in is still validated —
// it is the same file the platform will provision from either way.
func TestBuildGate_RolesDocumentValidatedEvenWithoutSignIn(t *testing.T) {
	files := completeDesignFiles()
	files["security.json"] = rolesDoc("F1.1", "F9.9")

	errs := gateErrors(t, files)
	if !slices.Contains(codesOf(errs), codeUnknownRoleStory) {
		t.Fatalf("want %s, got %+v", codeUnknownRoleStory, errs)
	}
}

// A design that cites a story the requirements no longer have names what to
// cite instead: a retired story its replacement, a feature its stories.
func TestBuildGate_StaleStoryCitations(t *testing.T) {
	reqs := map[string]string{}
	for k, v := range gateRequirements {
		reqs[k] = v
	}
	reqs["features/F1-ordering.md"] = strings.Replace(reqs["features/F1-ordering.md"], "- F1.3 dropped", "- F1.3 moved to F2.1", 1)
	files := completeDesignFiles()
	files["components/lunch-web/design.json"] = enriched("lunch-web", "web-application", "F1.1", "F1.2", "F1.3", "F2", "F9.9")
	var got []string
	for _, e := range validateBuildGate(reqs, files, everyStory(reqs)) {
		if e.Code == codeStaleStoryCitation {
			got = append(got, e.Message)
		}
	}
	want := []string{
		"`stories` cites F1.3 — F1.3 is retired: it is now F2.1; cite F2.1",
		"`stories` cites F2 — F2 is not a story; cite the stories it holds",
		"`stories` cites F9.9 — F9.9 is not in the requirements; cite a real story or drop it",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stale citations =\n%v\nwant\n%v", got, want)
	}
}

// The requirements' own ID problems refuse the save, pointing at the file.
func TestValidateSpecBundles_RequirementIDProblems(t *testing.T) {
	reqs := map[string]string{}
	for k, v := range gateRequirements {
		reqs[k] = v
	}
	reqs["features/F2-notifications.md"] += "- F2.1 As a member, I get the same message twice.\n"
	err := validateSpecBundles(reqs, completeDesignFiles(), nil, nil, nil)
	var ve *SpecValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want a SpecValidationError, got %v", err)
	}
	found := false
	for _, f := range ve.Files {
		if f.Path == RequirementsDir+"/features/F2-notifications.md" && f.Code == "DUPLICATE_ID" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want DUPLICATE_ID on the feature file, got %+v", ve.Files)
	}
}

// everyStory is every story the requirements define: a build of the whole
// product, which is what these gate tests check.
func everyStory(reqFiles map[string]string) map[string]bool {
	out := map[string]bool{}
	for _, st := range reqspec.Parse(reqFiles).Stories() {
		out[st.ID] = true
	}
	return out
}
