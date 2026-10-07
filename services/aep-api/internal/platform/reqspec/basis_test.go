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

package reqspec

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesignable(t *testing.T) {
	spec := Parse(readFixture(t, acmeFixture))
	got := map[string]bool{}
	for _, f := range spec.Features {
		got[f.ID] = f.Designable()
	}
	// F3 waits on a blocking question; F4 is a stub.
	want := map[string]bool{"F1": true, "F2": true, "F3": false, "F4": false, "F5": true}
	if !maps.Equal(got, want) {
		t.Errorf("designable = %v, want %v", got, want)
	}
}

func TestBasisMovesOnlyWhenTheWordsTheDesignReadsMove(t *testing.T) {
	files := readFixture(t, acmeFixture)
	f2 := Basis(files, "F2")
	if !strings.Contains(f2, "F2.5 As finance, I give a second approval on any claim over $1,000, after the manager.") {
		t.Fatalf("basis lacks F2.5's words:\n%s", f2)
	}
	if !strings.Contains(f2, "P1 Every approval") || strings.Contains(f2, "P3 ") {
		t.Errorf("basis should carry P1 (all) and not P3 (F1, F3):\n%s", f2)
	}

	edit := func(path, from, to string) map[string]string {
		out := maps.Clone(files)
		if !strings.Contains(out[path], from) {
			t.Fatalf("%s lacks %q", path, from)
		}
		out[path] = strings.Replace(out[path], from, to, 1)
		return out
	}
	const approvals = "features/F2-approvals.md"
	same := map[string]map[string]string{
		"confirming an assumed line": edit(approvals, "in my place. *assumed*", "in my place."),
		"a source added":             edit(approvals, "with a reason.", "with a reason. [Policy · p.2]"),
		"another feature's file":     edit("features/F1-submit-expenses.md", "Meals are capped at $50", "Meals are capped at $60"),
		"a rule for other features":  edit("product-wide.md", "Applies to: F1, F3.", "Applies to: F1, F3, F4."),
	}
	for name, changed := range same {
		if Basis(changed, "F2") != f2 {
			t.Errorf("%s moved F2's basis", name)
		}
	}
	moved := map[string]map[string]string{
		"a story's words":          edit(approvals, "with a reason.", "with a written reason."),
		"a rule now reaching F2":   edit("product-wide.md", "Applies to: F1, F3.", "Applies to: F1, F2, F3."),
		"a new rule for every one": edit("product-wide.md", "## Decisions", "- P5 Every page is accessible. Applies to: all.\n\n## Decisions"),
	}
	for name, changed := range moved {
		if Basis(changed, "F2") == f2 {
			t.Errorf("%s did not move F2's basis", name)
		}
	}
	if Basis(files, "F9") != "" {
		t.Error("a feature with no file has no basis")
	}
}

// A feature's lines as a build keeps them, held to the shared fixture the
// console's reader is held to as well (console builds/model/changes.ts).
func TestFeatureLines_SharedFixture(t *testing.T) {
	files := readFixture(t, acmeFixture)
	got := map[string][]Line{}
	for _, f := range Parse(files).Features {
		got[f.ID] = FeatureLines(files, f.ID)
	}
	raw, err := os.ReadFile(filepath.Join(acmeFixture, "feature-lines.json"))
	if err != nil {
		t.Fatalf("read feature-lines.json: %v", err)
	}
	var want map[string][]Line
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if g, w := canonical(t, got), canonical(t, want); g != w {
		t.Errorf("FeatureLines(acme-expenses) differs from feature-lines.json\n--- got\n%s\n--- want\n%s", g, w)
	}
}

// A feature's basis, held to the shared fixture the console's reader is held
// to (console spec/model/designWork.ts designBasis): the platform records
// what a design read, and the console compares its live basis with it to say
// a design is out of date, so the two must read the files identically.
func TestBasis_SharedFixture(t *testing.T) {
	files := readFixture(t, acmeFixture)
	raw, err := os.ReadFile(filepath.Join(acmeFixture, "basis.json"))
	if err != nil {
		t.Fatalf("read basis.json: %v", err)
	}
	var want map[string]string
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	for _, f := range Parse(files).Features {
		if got := Basis(files, f.ID); got != want[f.ID] {
			t.Errorf("Basis(%s) differs from basis.json\n--- got\n%s\n--- want\n%s", f.ID, got, want[f.ID])
		}
	}
}
