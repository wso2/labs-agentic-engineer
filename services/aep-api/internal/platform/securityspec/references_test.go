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

package securityspec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The rules that read sibling spec files. The three worked examples come with
// the files their cross-checks need — a design.cell, the owning components'
// openapi.yaml, the web apps' wireframes.dsl — copied from the agent's fixtures
// so both gates judge the SAME bundle. All three must come out clean: a rule
// that would refuse one of the design's own documents fails here rather than in
// a live run.

// bundleOf folds one fixture directory into the paths the real spec tree uses.
// On disk the files are flat (`expense-api.openapi.yaml`) so the directory
// reads as a list; the rules want `components/<component>/openapi.yaml`.
func bundleOf(t *testing.T, scenario string) DesignBundle {
	t.Helper()
	dir := filepath.Join("testdata", scenario)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read fixture dir %s: %v", dir, err)
	}
	bundle := DesignBundle{}
	for _, entry := range entries {
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		name := entry.Name()
		switch {
		case name == "design.cell":
			bundle["design.cell"] = string(body)
		case strings.HasSuffix(name, ".openapi.yaml"):
			component := strings.TrimSuffix(name, ".openapi.yaml")
			bundle["components/"+component+"/openapi.yaml"] = string(body)
		case strings.HasSuffix(name, ".wireframes.dsl"):
			component := strings.TrimSuffix(name, ".wireframes.dsl")
			bundle["components/"+component+"/wireframes.dsl"] = string(body)
		}
	}
	return bundle
}

func findingsFor(t *testing.T, scenario string, edit func(m map[string]any)) []Finding {
	t.Helper()
	raw := fixture(t, scenario+".json")
	if edit != nil {
		raw = mutate(t, scenario+".json", edit)
	}
	doc, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return ReferenceFindings(doc, bundleOf(t, scenario))
}

func firstOfKind(found []Finding, key string) (Finding, bool) {
	for _, f := range found {
		if f.Key == key {
			return f, true
		}
	}
	return Finding{}, false
}

// Every worked example passes the WHOLE rule set against its own bundle — the
// most valuable assertion in this file, because the design's first Expense
// Tracker draft did not.
func TestEveryDesignFixtureIsCleanAgainstItsOwnBundle(t *testing.T) {
	for _, scenario := range []string{"expense-tracker", "clinic", "vendor"} {
		t.Run(scenario, func(t *testing.T) {
			for _, f := range findingsFor(t, scenario, nil) {
				if f.Severity == SeverityError {
					t.Errorf("%s: %s", f.Key, f.Message)
				}
			}
		})
	}
}

// Δ P6 §5 — the rule the design's own example failed. `Approvals` is gated on
// `claims:approve`; the list it renders is `GET /claims`, guarded on
// `claims:read`; an Approver granted neither reaches a screen that loads into a
// bare 401 the SPA cannot tell from an expired session.
func TestScreenOperationNotGranted(t *testing.T) {
	found := findingsFor(t, "expense-tracker", func(m map[string]any) {
		// read-all goes too, or B1's rule fires first and this one never runs.
		roleNamed(t, m, "Approver")["grants"] = []any{"claims:approve", "claims:reject", "reports:read"}
	})
	f, ok := firstOfKind(found, MsgScreenOperationNotGranted)
	if !ok {
		t.Fatalf("want %s, got %+v", MsgScreenOperationNotGranted, found)
	}
	for _, want := range []string{"Approver", "Approvals", "claims:read"} {
		if !strings.Contains(f.Message, want) {
			t.Errorf("message %q does not name %q", f.Message, want)
		}
	}
}

// A service role holds an app principal's token and reaches no screen, so the
// reachability loop must skip it — otherwise the Vendor Portal's nightly job
// would be refused for not holding a screen's scope.
func TestReachabilitySkipsServiceRoles(t *testing.T) {
	found := findingsFor(t, "vendor", nil)
	for _, f := range found {
		if f.Key == MsgScreenOperationNotGranted {
			t.Fatalf("a service role was judged against a screen: %s", f.Message)
		}
	}
}

func TestSiblingFileRules(t *testing.T) {
	cases := []struct {
		name     string
		scenario string
		edit     func(m map[string]any)
		key      string
	}{
		{
			name:     "a resource owned by a component the cell does not declare",
			scenario: "expense-tracker",
			edit: func(m map[string]any) {
				m["permissions"].([]any)[0].(map[string]any)["component"] = "ghost-api"
			},
			key: MsgResourceComponentUnknown,
		},
		{
			name:     "a screen on a component the cell does not declare",
			scenario: "expense-tracker",
			edit: func(m map[string]any) {
				m["screens"].([]any)[0].(map[string]any)["component"] = "ghost-webapp"
			},
			key: MsgScreenComponentUnknown,
		},
		{
			name:     "a screen the wireframe does not declare",
			scenario: "expense-tracker",
			edit: func(m map[string]any) {
				m["screens"].([]any)[0].(map[string]any)["screen"] = "Archive"
			},
			key: MsgScreenUnknown,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found := findingsFor(t, tc.scenario, tc.edit)
			if _, ok := firstOfKind(found, tc.key); !ok {
				t.Fatalf("want %s, got %+v", tc.key, found)
			}
		})
	}
}

// `"My Claims"` in the document and `screen MyClaims` in the wireframe are the
// same screen; neither file format changes to make them look alike.
func TestScreenNamesAreComparedNormalized(t *testing.T) {
	found := findingsFor(t, "expense-tracker", func(m map[string]any) {
		m["screens"].([]any)[0].(map[string]any)["screen"] = "my-claims!"
	})
	if f, ok := firstOfKind(found, MsgScreenUnknown); ok {
		t.Fatalf("a differently punctuated spelling must still match: %s", f.Message)
	}
}

// The wireframe grammar anchors the WHOLE line, so trailing junk is not a
// declaration. A looser reading here would invent the screen `Approvals` out of
// a line the compiler ignores, and the design would pass a rule the wireframe
// cannot satisfy.
func TestScreenDeclarationMustMatchTheWholeGrammar(t *testing.T) {
	doc, err := Parse(fixture(t, "expense-tracker.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	bundle := bundleOf(t, "expense-tracker")
	dsl := bundle["components/expense-webapp/wireframes.dsl"]
	if !strings.Contains(dsl, "screen Approvals \"Claims waiting on me\"") {
		t.Fatal("fixture no longer declares Approvals — rewrite this case")
	}
	bundle["components/expense-webapp/wireframes.dsl"] = strings.Replace(dsl,
		"screen Approvals \"Claims waiting on me\"", "screen Approvals bar baz", 1)

	f, ok := firstOfKind(ReferenceFindings(doc, bundle), MsgScreenUnknown)
	if !ok {
		t.Fatalf("`screen Approvals bar baz` is not a declaration, so Approvals must be unknown")
	}
	if !strings.Contains(f.Message, "Approvals") {
		t.Errorf("message %q does not name the screen", f.Message)
	}
}

// A wireframes.dsl this package cannot read is NOT "a wireframe with no
// screens": the rule has no ground truth, so it is skipped — the same verdict
// the agent side reaches when compileWireframes fails.
func TestUnreadableWireframeSkipsTheScreenRule(t *testing.T) {
	doc, err := Parse(fixture(t, "expense-tracker.json"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	bundle := bundleOf(t, "expense-tracker")
	bundle["components/expense-webapp/wireframes.dsl"] = "{ this is not the DSL at all }\n  screen Indented\n"

	if f, ok := firstOfKind(ReferenceFindings(doc, bundle), MsgScreenUnknown); ok {
		t.Fatalf("an unreadable wireframe must skip the rule, got: %s", f.Message)
	}
}

// A rule whose input the bundle does not hold is skipped in SILENCE: the design
// lineup writes security.json before the component artifacts exist, and a
// refusal there would make the agent fix a file that is not written yet.
func TestSiblingRulesAreSkippedWhenTheFileIsAbsent(t *testing.T) {
	doc, err := Parse(mutate(t, "expense-tracker.json", func(m map[string]any) {
		m["screens"].([]any)[0].(map[string]any)["screen"] = "Archive"
		m["permissions"].([]any)[0].(map[string]any)["component"] = "ghost-api"
	}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if msg := FirstError(ReferenceFindings(doc, DesignBundle{})); msg != "" {
		t.Fatalf("an empty bundle must produce no sibling-file refusal, got %q", msg)
	}
}

// An assignTo group this document does not declare is LEGAL — that is how a
// project reuses an org group — and is recorded as info, never as a refusal.
func TestAReusedOrgGroupIsInfoNotAnError(t *testing.T) {
	found := findingsFor(t, "expense-tracker", nil)
	f, ok := firstOfKind(found, MsgAssignToDirectoryChecked)
	if !ok {
		t.Fatalf("want the %s note for Finance, got %+v", MsgAssignToDirectoryChecked, found)
	}
	if f.Severity != SeverityInfo {
		t.Fatalf("severity = %q, want info", f.Severity)
	}
	if f.Params["group"] != "Finance" {
		t.Fatalf("params = %+v, want the Finance group", f.Params)
	}
}

// The two coverage warnings: declared and used nowhere, required and granted by
// nobody. Neither blocks a build.
func TestCoverageWarnings(t *testing.T) {
	found := findingsFor(t, "expense-tracker", func(m map[string]any) {
		// reports:export is required by GET /reports/export and granted by no
		// role; claims:reject stops being required once the Approver keeps it
		// but the screen set never names it — it stays used by an operation, so
		// the "used nowhere" case is made with a fresh handle instead.
		perms := m["permissions"].([]any)
		reports := perms[1].(map[string]any)
		reports["actions"] = append(reports["actions"].([]any),
			map[string]any{"handle": "archive", "ownership": "any"})
	})
	unused, ok := firstOfKind(found, MsgHandleUsedNowhere)
	if !ok || unused.Params["handle"] != "reports:archive" {
		t.Fatalf("want reports:archive used nowhere, got %+v", found)
	}
	if unused.Severity != SeverityWarning {
		t.Fatalf("severity = %q, want warning", unused.Severity)
	}
	unreachable, ok := firstOfKind(found, MsgHandleUnreachable)
	if !ok || unreachable.Params["handle"] != "reports:export" {
		t.Fatalf("want reports:export unreachable, got %+v", found)
	}
}

// `X:read-all` guards no operation and no screen by design — the service reads
// it off the token while serving `X:read` — so warning about it every time
// would make the line noise. It only speaks when even `X:read` is unused.
func TestReadAllIsExemptFromTheUsedNowhereWarning(t *testing.T) {
	for _, f := range findingsFor(t, "expense-tracker", nil) {
		if f.Key == MsgHandleUsedNowhere && f.Params["handle"] == "claims:read-all" {
			t.Fatalf("claims:read-all must not warn while claims:read is used: %s", f.Message)
		}
	}
}

// Three states, all standard OpenAPI: no override inherits the document default
// (signed in), one requirement object names exactly one scope, and `security: []`
// is public. A zero-scope requirement object reads as signed-in too.
func TestOperationSecurityReadsTheThreeStates(t *testing.T) {
	const spec = `
openapi: 3.0.3
security:
  - oauth2: []
paths:
  /health:
    get:
      security: []
  /me:
    get: {}
  /claims:
    get:
      security: [{oauth2: [claims:read]}]
`
	byPath := map[string]specOperation{}
	for _, op := range specOperations(spec) {
		byPath[op.Path] = op
	}
	if got := byPath["/health"]; !got.Public || got.Scope != "" {
		t.Errorf("/health = %+v, want public", got)
	}
	if got := byPath["/me"]; got.Public || got.Scope != "" {
		t.Errorf("/me = %+v, want signed-in with no scope", got)
	}
	if got := byPath["/claims"]; got.Scope != "claims:read" {
		t.Errorf("/claims = %+v, want claims:read", got)
	}
}
