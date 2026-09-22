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

package prototypespec

// prototypespec_test.go — the Go half of the validator's parity proof. It reads
// @aep/prototype-model's own case table and fixtures over the module boundary
// rather than copying them, because the promise is that a document one gate
// accepts the other accepts, and a copied fixture is a promise that decays
// silently.

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// prototype-model package, from prototypespec → … → repo root.
const modelPackage = "../../../../../packages/prototype-model/"

type expectedIssue struct {
	Code string `json:"code"`
	Path string `json:"path,omitempty"`
}

type patchOp struct {
	Op    string `json:"op"`
	Path  []any  `json:"path"`
	Value any    `json:"value"`
}

type validationCase struct {
	Name      string          `json:"name"`
	Component string          `json:"component"`
	Patch     []patchOp       `json:"patch"`
	Issues    []expectedIssue `json:"issues"`
}

type caseFile struct {
	Base  json.RawMessage  `json:"base"`
	Cases []validationCase `json:"cases"`
}

func readCases(t *testing.T) caseFile {
	t.Helper()
	raw, err := os.ReadFile(modelPackage + "test/validation-cases.json")
	if err != nil {
		t.Fatalf("read the shared case table — layout drift?: %v", err)
	}
	var cf caseFile
	if err := json.Unmarshal(raw, &cf); err != nil {
		t.Fatalf("decode case table: %v", err)
	}
	return cf
}

// patched applies a case's patch to a fresh copy of the base document, exactly
// as test/cases.ts does on the TypeScript side.
func patched(t *testing.T, base json.RawMessage, patch []patchOp) []byte {
	t.Helper()
	var doc any
	if err := json.Unmarshal(base, &doc); err != nil {
		t.Fatal(err)
	}
	for _, op := range patch {
		parent := doc
		for _, seg := range op.Path[:len(op.Path)-1] {
			parent = child(parent, seg)
		}
		last := op.Path[len(op.Path)-1]
		switch p := parent.(type) {
		case map[string]any:
			if op.Op == "set" {
				p[last.(string)] = op.Value
			} else {
				delete(p, last.(string))
			}
		case []any:
			i := int(last.(float64))
			if op.Op != "set" {
				t.Fatalf("remove from an array is not used by the case table")
			}
			p[i] = op.Value
		}
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func child(node any, seg any) any {
	switch n := node.(type) {
	case map[string]any:
		return n[seg.(string)]
	case []any:
		return n[int(seg.(float64))]
	}
	return nil
}

func TestParseMatchesTheSharedCaseTable(t *testing.T) {
	cf := readCases(t)
	if len(cf.Cases) == 0 {
		t.Fatal("the case table is empty")
	}
	for _, c := range cf.Cases {
		t.Run(c.Name, func(t *testing.T) {
			model, issues := Parse(c.Component, patched(t, cf.Base, c.Patch))
			if len(c.Issues) == 0 {
				if len(issues) > 0 || model == nil {
					t.Fatalf("want valid, got %+v", issues)
				}
				return
			}
			if model != nil {
				t.Fatal("a refused document must not yield a model")
			}
			for _, i := range issues {
				if i.Message == "" {
					t.Errorf("issue %+v carries no message", i)
				}
			}
			codeOnly := true
			for _, e := range c.Issues {
				if e.Path != "" {
					codeOnly = false
				}
			}
			if codeOnly {
				for _, i := range issues {
					if i.Code != c.Issues[0].Code {
						t.Fatalf("want only %s, got %+v", c.Issues[0].Code, issues)
					}
				}
				if len(issues) == 0 {
					t.Fatal("want an issue, got none")
				}
				return
			}
			got := make([]expectedIssue, 0, len(issues))
			for _, i := range issues {
				got = append(got, expectedIssue{Code: i.Code, Path: i.Path})
			}
			if !reflect.DeepEqual(got, c.Issues) {
				t.Fatalf("got %+v\nwant %+v", got, c.Issues)
			}
		})
	}
}

func TestTheSharedFixturesParse(t *testing.T) {
	for _, tc := range []struct{ file, component string }{
		{"expense-approval.json", "approvals-portal"},
		{"integration-monitor.json", "integration-console"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			raw, err := os.ReadFile(modelPackage + "fixtures/" + tc.file)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			model, issues := Parse(tc.component, raw)
			if len(issues) > 0 {
				t.Fatalf("fixture refused: %+v", issues)
			}
			if model.Component != tc.component || len(model.Roles) < 2 || len(model.Screens) == 0 {
				t.Fatalf("fixture decoded wrong: %+v", model)
			}
		})
	}
}

func TestMalformedJSONIsInvalidJSON(t *testing.T) {
	_, issues := Parse("approvals-portal", []byte(`{"schemaVersion": 1,`))
	if len(issues) != 1 || issues[0].Code != CodeInvalidJSON || issues[0].Path != "" {
		t.Fatalf("got %+v", issues)
	}
}

func TestAStructuralIssueNamesItsPath(t *testing.T) {
	cf := readCases(t)
	raw := patched(t, cf.Base, []patchOp{{Op: "set", Path: []any{"screens", 0.0, "content", 1.0, "kind"}, Value: "carousel"}})
	_, issues := Parse("", raw)
	want := []Issue{{Code: CodeSchemaViolation, Path: "screens[0].content[1].kind", Message: "is not one of the allowed kinds"}}
	if !reflect.DeepEqual(issues, want) {
		t.Fatalf("got %+v, want %+v", issues, want)
	}
}

func TestReferenceMessagesMatchTheAgentGate(t *testing.T) {
	cf := readCases(t)
	raw := patched(t, cf.Base, []patchOp{{Op: "set", Path: []any{"flows", 0.0, "roleId"}, Value: "auditor"}})
	_, issues := Parse("", raw)
	want := `"auditor" does not name a declared role`
	if len(issues) != 1 || issues[0].Message != want {
		t.Fatalf("got %+v, want message %q", issues, want)
	}
}

func TestBundleComponent(t *testing.T) {
	for key, want := range map[string]string{
		"components/approvals-portal/prototype.json":         "approvals-portal",
		"components/approvals-portal/design.json":            "",
		"components/approvals-portal/screens/prototype.json": "",
		"prototype.json":               "",
		"components/prototype.json":    "",
		"components/../prototype.json": "",
	} {
		got, ok := BundleComponent(key)
		if got != want || ok != (want != "") {
			t.Errorf("BundleComponent(%q) = %q, %v; want %q", key, got, ok, want)
		}
	}
}
