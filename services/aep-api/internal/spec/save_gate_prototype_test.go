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

// save_gate_prototype_test.go — a web-application's prototype.json on the save
// path. The rules are prototypespec's (shared with the agent's write gate
// through the published schema and the case table); these tests pin that the
// save gate runs them on every prototype in the bundle and refuses through the
// existing 422 channel, one row per finding.

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
)

const prototypeKey = "components/lunch-web/prototype.json"

// lunchWebPrototype is the shared expense-approval fixture re-homed to the
// lunch-web component of completeDesignFiles.
func lunchWebPrototype(t *testing.T, edit func(doc map[string]any)) string {
	t.Helper()
	raw, err := os.ReadFile("../../../../packages/prototype-model/fixtures/expense-approval.json")
	if err != nil {
		t.Fatalf("read the shared fixture — layout drift?: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc["component"] = "lunch-web"
	if edit != nil {
		edit(doc)
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func prototypeRows(t *testing.T, err error) []FileValidationError {
	t.Helper()
	var verr *DesignValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("want *DesignValidationError (422), got %T: %v", err, err)
	}
	var rows []FileValidationError
	for _, f := range verr.Files {
		if f.Path == prototypeKey {
			rows = append(rows, f)
		}
	}
	return rows
}

func TestSaveGate_AdmitsAValidPrototype(t *testing.T) {
	files := completeDesignFiles()
	files[prototypeKey] = lunchWebPrototype(t, nil)
	if err := validateDesignBundle(files); err != nil {
		t.Fatalf("a valid prototype must pass the save gate: %v", err)
	}
}

func TestSaveGate_IgnoresABundleWithoutAPrototype(t *testing.T) {
	if err := validateDesignBundle(completeDesignFiles()); err != nil {
		t.Fatalf("a save touching no prototype is unaffected: %v", err)
	}
}

func TestSaveGate_RefusesAnInvalidPrototype(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []FileValidationError
	}{
		{
			name: "malformed JSON",
			body: `{"schemaVersion": 1,`,
			want: []FileValidationError{{Path: prototypeKey, Code: "INVALID_JSON"}},
		},
		{
			name: "an unsupported version",
			body: lunchWebPrototype(t, func(d map[string]any) { d["schemaVersion"] = 2 }),
			want: []FileValidationError{{Path: prototypeKey, Code: "UNSUPPORTED_VERSION",
				Message: "schemaVersion: schemaVersion 2 is not supported; this platform reads version 1"}},
		},
		{
			name: "a dangling reference",
			body: lunchWebPrototype(t, func(d map[string]any) { d["defaultScreenId"] = "screen.ghost" }),
			want: []FileValidationError{{Path: prototypeKey, Code: "UNKNOWN_REFERENCE",
				Message: `defaultScreenId: "screen.ghost" does not name a screen`}},
		},
		{
			name: "a component that disagrees with its directory",
			body: lunchWebPrototype(t, func(d map[string]any) { d["component"] = "approvals-portal" }),
			want: []FileValidationError{{Path: prototypeKey, Code: "PROTOTYPE_COMPONENT_MISMATCH",
				Message: `component: component "approvals-portal" does not match its directory "lunch-web"`}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			files := completeDesignFiles()
			files[prototypeKey] = c.body
			rows := prototypeRows(t, validateDesignBundle(files))
			if len(rows) != len(c.want) {
				t.Fatalf("got rows %+v, want %+v", rows, c.want)
			}
			for i, want := range c.want {
				if rows[i].Code != want.Code || (want.Message != "" && rows[i].Message != want.Message) {
					t.Errorf("row %d = %+v, want %+v", i, rows[i], want)
				}
			}
		})
	}
}

// Every finding is its own row, so the console lists each fix.
func TestSaveGate_ReportsEveryPrototypeFinding(t *testing.T) {
	files := completeDesignFiles()
	files[prototypeKey] = lunchWebPrototype(t, func(d map[string]any) {
		d["defaultScreenId"] = "screen.ghost"
		d["flows"].([]any)[0].(map[string]any)["roleId"] = "auditor"
	})
	rows := prototypeRows(t, validateDesignBundle(files))
	if len(rows) != 2 || rows[0].Code != "UNKNOWN_REFERENCE" || rows[1].Code != "UNKNOWN_REFERENCE" {
		t.Fatalf("want two UNKNOWN_REFERENCE rows, got %+v", rows)
	}
}

// Only the component slot is a prototype: the same name elsewhere is not judged.
func TestSaveGate_JudgesOnlyTheComponentPrototypeSlot(t *testing.T) {
	files := completeDesignFiles()
	files["prototype.json"] = "{not json"
	files["components/lunch-web/drafts/prototype.json"] = "{not json"
	if err := validateDesignBundle(files); err != nil {
		t.Fatalf("a prototype.json outside components/<c>/ must not be judged: %v", err)
	}
}

// A blank prototype is not malformed JSON here: like a blank openapi.yaml it is
// a MISSING artifact, which the build gate names.
func TestSaveGate_LeavesABlankPrototypeToTheBuildGate(t *testing.T) {
	files := completeDesignFiles()
	files[prototypeKey] = " \n"
	if err := validateDesignBundle(files); err != nil {
		t.Fatalf("a blank prototype is the build gate's MISSING_COMPONENT_ARTIFACT, not a save refusal: %v", err)
	}
}
