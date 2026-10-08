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
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// The shared fixture both requirements readers are held to (the console's is
// TypeScript). reqspec → platform → internal → aep-api → services → repo root.
const acmeFixture = "../../../../../packages/contracts/requirements/acme-expenses"

func readFixture(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".md" {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		files[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("read fixture %s — layout drift?: %v", dir, err)
	}
	return files
}

// Both sides go through JSON so an empty list and an absent one compare equal,
// as they mean the same thing.
func canonical(t *testing.T, v any) string {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseMatchesTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(acmeFixture, "expected.json"))
	if err != nil {
		t.Fatalf("read expected.json: %v", err)
	}
	var want Spec
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decode expected.json: %v", err)
	}
	got := Parse(readFixture(t, acmeFixture))
	if g, w := canonical(t, got), canonical(t, want); g != w {
		t.Errorf("Parse(acme-expenses) differs from expected.json\n--- got\n%s\n--- want\n%s", g, w)
	}
}

func TestParseLine(t *testing.T) {
	cases := []struct {
		name, in string
		want     line
	}{
		{"words only", "A claim can hold expenses from any dates.",
			line{text: "A claim can hold expenses from any dates."}},
		{"story with source, needs and tag", "F1.2 As an employee, I pick a category. [T&E Policy v3 · p.3] Needs: F5, F6. *assumed*",
			line{id: "F1.2", text: "As an employee, I pick a category.", needs: []string{"F5", "F6"}, tag: tagAssumed}},
		{"moved story", "F5.1 (was F2.3) As a manager, I see the route.",
			line{id: "F5.1", was: "F2.3", text: "As a manager, I see the route."}},
		// The eval has seen the source written after the clause; it still reads.
		{"source after applies-to", "P1 Every user signs in via SSO. Applies to: all. [org default]",
			line{id: "P1", text: "Every user signs in via SSO.", appliesTo: []string{"all"}}},
		{"underscore tag and trailing period", "P2 Amounts in cents. Applies to: F1, F3. _assumed_.",
			line{id: "P2", text: "Amounts in cents.", appliesTo: []string{"F1", "F3"}, tag: tagAssumed}},
		{"a tag mid-line is words", "The *assumed* default is kept.",
			line{text: "The *assumed* default is kept."}},
		{"a feature ID is not a line's ID", "F2 [Approvals](features/F2-approvals.md)",
			line{text: "F2 (features/F2-approvals.md)"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseLine(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseLine(%q)\n got %+v\nwant %+v", tc.in, got, tc.want)
			}
		})
	}
}

// Agents wrap long list items; a wrapped line is still one story, and the tag
// that closes its last line still closes the story.
func TestParseReadsWrappedItems(t *testing.T) {
	spec := Parse(map[string]string{"features/F1-teams.md": `# Teams

## User Stories
- F1.1 As a team member, I create a team so my colleagues can join it and
  share daily lunch orders.
- F1.2 As a team member, I switch teams if I move.
  *assumed*

## Open Questions
1. How does a member find a team to join — an invite link, or
   browsing? *blocking*
   - An invite link from a teammate
   - Browsing every team
`})
	f := spec.Features[0]
	if got := f.Stories[0].Text; got != "As a team member, I create a team so my colleagues can join it and share daily lunch orders." {
		t.Errorf("wrapped story = %q", got)
	}
	if !f.Stories[1].Assumed || f.ToConfirm != 1 {
		t.Errorf("wrapped tag: assumed=%v toConfirm=%d", f.Stories[1].Assumed, f.ToConfirm)
	}
	want := []Question{{Question: "How does a member find a team to join — an invite link, or browsing?", Options: []string{"An invite link from a teammate", "Browsing every team"}}}
	if !reflect.DeepEqual(f.Blocking, want) {
		t.Errorf("blocking = %+v", f.Blocking)
	}
}

func TestParseIgnoresStoriesOfAnotherFeature(t *testing.T) {
	spec := Parse(map[string]string{"features/F2-approvals.md": "# Approvals\n\n## User Stories\n- F2.1 Mine.\n- F3.1 Pasted from Payroll export.\n- Not a story.\n"})
	if got := spec.Stories(); len(got) != 1 || got[0].ID != "F2.1" {
		t.Errorf("stories = %+v", got)
	}
}

func TestCompareIDs(t *testing.T) {
	ids := []string{"F10", "F2.10", "P2", "F2.9", "F2", "P10", "F9.1"}
	slices.SortFunc(ids, CompareIDs)
	want := []string{"F2", "F2.9", "F2.10", "F9.1", "F10", "P2", "P10"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("sorted = %v, want %v", ids, want)
	}
}

func TestIsNestedFile(t *testing.T) {
	for rel, want := range map[string]bool{
		"features/F2-approvals.md":  true,
		"product-wide/security.md":  true,
		"features/approvals.md":     false,
		"features/F2-approvals.txt": false,
		"references/policy.md":      false,
		"features/F2/deep.md":       false,
		"prd.md":                    false,
	} {
		if got := IsNestedFile(rel); got != want {
			t.Errorf("IsNestedFile(%q) = %v, want %v", rel, got, want)
		}
	}
}

// P items live in product-wide.md and any topic file split out of it; their
// IDs are one sequence, and each file's Retired section counts toward it.
func TestParseReadsProductWideTopicFiles(t *testing.T) {
	spec := Parse(map[string]string{
		"product-wide.md":          "# Product-wide\n\n## Requirements\n\n- P2 Records are kept 7 years. Applies to: all.\n\n## Retired\n\n- P1 dropped\n",
		"product-wide/security.md": "# Security\n\n## Requirements\n\n- P10 Sessions end after 8 hours. Applies to: F1, F2.\n- P3 Staff sign in with SSO. [org default] Applies to: all.\n",
		"references/brief.md":      "## Requirements\n\n- P9 Not a requirement: a source document.\n",
	})
	var got []string
	for _, it := range spec.ProductWide {
		got = append(got, it.ID)
	}
	if want := []string{"P2", "P3", "P10"}; !reflect.DeepEqual(got, want) {
		t.Errorf("product-wide IDs = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(spec.ProductWide[2].AppliesTo, []string{"F1", "F2"}) {
		t.Errorf("P10 applies to %v", spec.ProductWide[2].AppliesTo)
	}
	if !reflect.DeepEqual(spec.RetiredProductWide, []string{"P1"}) {
		t.Errorf("retired = %v", spec.RetiredProductWide)
	}
}
