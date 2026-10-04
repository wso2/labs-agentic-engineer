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

package skills

import (
	"context"
	"reflect"
	"testing"
)

// Ported from aep-api's skill_pure_test.go TestFrontmatterAudience.
func TestFrontmatterAudience(t *testing.T) {
	cases := []struct {
		name string
		fm   string
		want []string
	}{
		{"coding only", "---\nname: s\ndescription: d.\nmetadata:\n  aep:\n    audience: [coding]\n---\nbody", []string{"coding"}},
		{"design only", "---\nname: s\ndescription: d.\nmetadata:\n  aep:\n    audience: [design]\n---\nbody", []string{"design"}},
		{"both explicit", "---\nname: s\ndescription: d.\nmetadata:\n  aep:\n    audience: [design, coding]\n---\nbody", []string{"design", "coding"}},
		{"absent metadata", "---\nname: s\ndescription: d.\n---\nbody", []string{"design", "coding"}},
		{"empty list", "---\nname: s\ndescription: d.\nmetadata:\n  aep:\n    audience: []\n---\nbody", []string{"design", "coding"}},
		{"unrecognised values only", "---\nname: s\ndescription: d.\nmetadata:\n  aep:\n    audience: [ops, qa]\n---\nbody", []string{"design", "coding"}},
		{"unrecognised value dropped, one kept", "---\nname: s\ndescription: d.\nmetadata:\n  aep:\n    audience: [coding, ops]\n---\nbody", []string{"coding"}},
		{"whitespace value", "---\nname: s\ndescription: d.\nmetadata:\n  aep:\n    audience: ['  coding  ']\n---\nbody", []string{"coding"}},
		{"BOM and leading blank lines", "\n\ufeff---\nname: s\ndescription: d.\nmetadata:\n  aep:\n    audience: [coding]\n---\nbody", []string{"coding"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fm, err := parseSkillMD(tc.fm)
			if err != nil {
				t.Fatalf("parseSkillMD: %v", err)
			}
			if got := frontmatterAudience(fm); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("frontmatterAudience = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseSkillMD_Refusals(t *testing.T) {
	for name, md := range map[string]string{
		"no frontmatter":   "just a body",
		"unterminated":     "---\nname: s\ndescription: d.\nbody",
		"no name":          "---\ndescription: d.\n---\nbody",
		"no description":   "---\nname: s\n---\nbody",
		"not yaml":         "---\nname: [s\n---\nbody",
		"blank name/descr": "---\nname: ' '\ndescription: ' '\n---\nbody",
	} {
		if _, err := parseSkillMD(md); err == nil {
			t.Errorf("%s: parsed, want an error", name)
		}
	}
}

func TestParseBundleEntries_LayoutsDedupAndSkips(t *testing.T) {
	md := func(name, kind string) string {
		meta := ""
		if kind != "" {
			meta = "metadata:\n  aep:\n    kind: " + kind + "\n"
		}
		return "---\nname: " + name + "\ndescription: d.\n" + meta + "---\nbody " + name + "\n"
	}
	files := map[string]string{
		// flat, with aux files at any depth
		"skills/go/SKILL.md":              md("go", ""),
		"skills/go/references/style.md":   "STYLE",
		"skills/go/scripts/deep/setup.sh": "SETUP",
		// legacy layout
		"skills/flow/plan/SKILL.md":        md("plan", ""),
		"skills/flow/plan/references/x.md": "X",
		"skills/imported/ext/SKILL.md":     md("ext", ""),
		// same name in both layouts, same kind: the flat copy wins
		"skills/builtin/go/SKILL.md": md("go", "") + "LEGACY",
		// imported beats org on a name tie
		"skills/dup/SKILL.md":          md("dup", "org"),
		"skills/imported/dup/SKILL.md": md("dup", ""),
		// a flat skill literally named like a legacy kind dir keeps its aux files
		"skills/custom/SKILL.md":    md("custom", ""),
		"skills/custom/refs/a/b.md": "AB",
		// skipped: unparseable SKILL.md, aux file with no body
		"skills/broken/SKILL.md": "no frontmatter",
		"skills/orphan/notes.md": "N",
	}
	got := map[string]Skill{}
	for _, sk := range parseBundleEntries(context.Background(), files) {
		got[sk.Name] = sk
	}
	if want := []string{"custom", "dup", "ext", "go", "plan"}; !reflect.DeepEqual(keys(got), want) {
		t.Fatalf("skills %v, want %v", keys(got), want)
	}
	if g := got["go"]; g.SkillMD != md("go", "") || !reflect.DeepEqual(g.References, map[string]string{"references/style.md": "STYLE", "scripts/deep/setup.sh": "SETUP"}) || g.Kind != kindOrg {
		t.Errorf("go = %+v", g)
	}
	if p := got["plan"]; p.Kind != kindPlatform || p.References["references/x.md"] != "X" {
		t.Errorf("plan = %+v", p)
	}
	if d := got["dup"]; d.Kind != kindImported {
		t.Errorf("dup kind = %s, want imported", d.Kind)
	}
	if c := got["custom"]; c.References["refs/a/b.md"] != "AB" {
		t.Errorf("custom = %+v", c)
	}
	for _, sk := range got {
		if !sk.Enabled {
			t.Errorf("%s: parse alone must default to enabled", sk.Name)
		}
	}
}

func TestIsCatalogPath(t *testing.T) {
	for p, want := range map[string]bool{
		"skills/go/SKILL.md":        true,
		"skills/go/references/x.md": true,
		"skills/flow/plan/SKILL.md": true,
		"skills/go/.hidden":         false,
		"skills/.git/x/SKILL.md":    false,
		"skills/SKILL.md":           false,
		"skills-manifest.json":      false,
		"docs/skills/go/SKILL.md":   false,
	} {
		if got := isCatalogPath(p); got != want {
			t.Errorf("isCatalogPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestParseSkillsManifest(t *testing.T) {
	m := parseSkillsManifest([]byte(`{"go":{"origin":"platform","baseHash":"x","disabled":true},"py":{"kind":"platform","baseHash":"y"}}`))
	if !m["go"].Disabled || m["py"].Disabled || m["absent"].Disabled {
		t.Errorf("manifest = %+v", m)
	}
	for _, raw := range []string{"", "not json", "null", "[1]"} {
		if got := parseSkillsManifest([]byte(raw)); got == nil || len(got) != 0 {
			t.Errorf("%q → %v, want empty", raw, got)
		}
	}
}
