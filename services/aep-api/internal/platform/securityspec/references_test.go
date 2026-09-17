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
// the files their cross-checks need — a design.cell and the owning components'
// openapi.yaml — copied from the agent's fixtures so both gates judge the SAME
// bundle. All three must come out clean: a rule that would refuse one of the
// design's own documents fails here rather than in a live run.

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

// A rule whose input the bundle does not hold is skipped in SILENCE: the design
// lineup writes security.json before the component artifacts exist, and a
// refusal there would make the agent fix a file that is not written yet.
func TestSiblingRulesAreSkippedWhenTheFileIsAbsent(t *testing.T) {
	doc, err := Parse(mutate(t, "expense-tracker.json", func(m map[string]any) {
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

// A role name is refused for the characters that cannot be escaped downstream.
//
// A "|" is the one that bites: the role is published in the build ticket's
// markdown table, and the validation agent parses that table to learn which
// login exercises which criteria — a pipe inside a cell silently shifts every
// column after it. The "/" is the platform's own separator in the directory
// name `<project>/<Role>`. Everything a PRD actor noun needs — spaces, dots,
// hyphens, underscores, digits, non-ASCII letters — is still accepted.
//
// The rule is a document-only one, so Parse itself refuses: the refusal is what
// the write gate shows the model, not a finding a caller has to look for.
func TestRoleNameCharset(t *testing.T) {
	rename := func(name string) func(map[string]any) {
		return func(m map[string]any) {
			role := roleNamed(t, m, "Approver")
			role["name"] = name
			// testUsers[].roles and assignableBy name it too; keep the document
			// referentially whole so this rule is the only one that speaks.
			role["assignableBy"] = []any{name}
			for _, entry := range m["testUsers"].([]any) {
				user := entry.(map[string]any)
				if user["username"] == "test-approver" {
					user["roles"] = []any{name}
				}
			}
		}
	}
	for _, tc := range []struct {
		name    string
		refused bool
	}{
		{"Approver | admin", true},
		{"Approver/admin", true},
		{"Approver\nadmin", true},
		{"Approver`admin`", true},
		{"Compliance Admin", false},
		{"Level-2 Approver", false},
		{"Sr. Approver_2", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := Parse(mutate(t, "expense-tracker.json", rename(tc.name)))
			if tc.refused {
				if err == nil {
					t.Fatalf("role %q was accepted", tc.name)
				}
				want := Msg(MsgRoleNameInvalid, "role", tc.name)
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("Parse refused with %q, want the %s sentence %q", err, MsgRoleNameInvalid, want)
				}
				return
			}
			if err != nil {
				t.Fatalf("role %q was refused: %v", tc.name, err)
			}
			for _, f := range ReferenceFindings(doc, bundleOf(t, "expense-tracker")) {
				if f.Key == MsgRoleNameInvalid {
					t.Fatalf("role %q was refused by the charset rule: %s", tc.name, f.Message)
				}
			}
		})
	}
}

// The two coverage warnings: declared and used nowhere, required and granted by
// nobody. Neither blocks a build.
func TestCoverageWarnings(t *testing.T) {
	found := findingsFor(t, "expense-tracker", func(m map[string]any) {
		// reports:export is required by GET /reports/export and granted by no
		// role. Every other catalog handle IS required by an operation, so the
		// "used nowhere" case is made with a fresh handle no operation names.
		perms := m["permissions"].([]any)
		reports := perms[1].(map[string]any)
		reports["actions"] = append(reports["actions"].([]any),
			map[string]any{"handle": "archive"})
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

// `claims:read-all` guards `GET /claims` — an operation of its own, at its own
// reach (ADR-0031) — so it is used like any other handle and nothing is exempt
// by name.
func TestReadAllIsUsedLikeAnyOtherHandle(t *testing.T) {
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
