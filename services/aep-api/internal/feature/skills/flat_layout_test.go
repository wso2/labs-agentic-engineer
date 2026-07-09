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

// The flat repo layout + kind-in-frontmatter contract
// (docs/design/skills-unified-library-migration.md): skills/<name>/SKILL.md
// with `metadata.aep.kind` naming the kind (absent → org), the legacy
// skills/<kindDir>/<name>/ layout parsed for not-yet-migrated repos, the
// service-side kind stamping, and the reconcile-driven one-commit migration.
package skills

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/models"
)

// mkSkillMD builds a minimal valid SKILL.md; kind == "" leaves the metadata
// block out entirely (the unmarked → org case).
func mkSkillMD(name, kind, body string) string {
	meta := ""
	if kind != "" {
		meta = "metadata:\n  aep:\n    kind: " + kind + "\n"
	}
	return fmt.Sprintf("---\nname: %s\ndescription: d.\n%s---\n\n%s\n", name, meta, body)
}

// ---- stampFrontmatterKind ----------------------------------------------------

func TestStampFrontmatterKind(t *testing.T) {
	t.Parallel()

	t.Run("stamps an unmarked skill", func(t *testing.T) {
		t.Parallel()
		in := mkSkillMD("alpha", "", "BODY-α")
		out, err := stampFrontmatterKind(in, models.SkillKindCustom)
		if err != nil {
			t.Fatalf("stamp: %v", err)
		}
		fm, body, err := parseSkillMD(out)
		if err != nil {
			t.Fatalf("re-parse: %v", err)
		}
		if got := frontmatterKind(fm); got != models.SkillKindCustom {
			t.Fatalf("kind after stamp = %q, want custom", got)
		}
		if fm.Name != "alpha" || fm.Description == "" {
			t.Fatalf("stamp lost frontmatter fields: %+v", fm)
		}
		if !strings.Contains(body, "BODY-α") {
			t.Fatalf("stamp altered body: %q", body)
		}
	})

	t.Run("idempotent for an already-stamped kind", func(t *testing.T) {
		t.Parallel()
		in := mkSkillMD("alpha", "custom", "BODY")
		out, err := stampFrontmatterKind(in, models.SkillKindCustom)
		if err != nil {
			t.Fatalf("stamp: %v", err)
		}
		if out != in {
			t.Fatalf("re-stamping the same kind must be byte-identical:\n in: %q\nout: %q", in, out)
		}
	})

	t.Run("overrides a spoofed kind", func(t *testing.T) {
		t.Parallel()
		// A create/import must not let user content claim platform/org status.
		in := mkSkillMD("alpha", "org", "BODY")
		out, err := stampFrontmatterKind(in, models.SkillKindCustom)
		if err != nil {
			t.Fatalf("stamp: %v", err)
		}
		fm, _, err := parseSkillMD(out)
		if err != nil {
			t.Fatalf("re-parse: %v", err)
		}
		if got := frontmatterKind(fm); got != models.SkillKindCustom {
			t.Fatalf("kind after stamp = %q, want custom (spoof must not survive)", got)
		}
	})

	t.Run("preserves sibling frontmatter and metadata keys", func(t *testing.T) {
		t.Parallel()
		in := "---\nname: alpha\ndescription: d.\nlicense: MIT\nmetadata:\n  team: platform-eng\n---\n\nBODY\n"
		out, err := stampFrontmatterKind(in, models.SkillKindImported)
		if err != nil {
			t.Fatalf("stamp: %v", err)
		}
		if !strings.Contains(out, "license: MIT") || !strings.Contains(out, "team: platform-eng") {
			t.Fatalf("stamp dropped sibling keys:\n%s", out)
		}
		fm, _, err := parseSkillMD(out)
		if err != nil {
			t.Fatalf("re-parse: %v", err)
		}
		if got := frontmatterKind(fm); got != models.SkillKindImported {
			t.Fatalf("kind = %q, want imported", got)
		}
		if fm.License != "MIT" {
			t.Fatalf("license lost: %+v", fm)
		}
	})
}

// ---- parseBundle: dual layout -------------------------------------------------

func TestParseBundle_DualLayout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	files := map[string]string{
		// Flat layout: kind from frontmatter.
		"skills/alpha/SKILL.md":           mkSkillMD("alpha", "", "flat-alpha"), // unmarked → org
		"skills/beta/SKILL.md":            mkSkillMD("beta", "platform", "flat-beta"),
		"skills/beta/references/notes.md": "beta ref",
		"skills/gamma/SKILL.md":           mkSkillMD("gamma", "custom", "flat-gamma"),
		// Legacy layout: kind from the path, mapped to the new vocabulary.
		"skills/builtin/delta/SKILL.md":         mkSkillMD("delta", "", "legacy-delta"),
		"skills/flow/epsilon/SKILL.md":          mkSkillMD("epsilon", "", "legacy-epsilon"),
		"skills/flow/epsilon/references/ref.md": "epsilon ref",
		"skills/imported/zeta/SKILL.md":         mkSkillMD("zeta", "", "legacy-zeta"),
		// Shadow: a legacy custom skill vs a flat org skill of the same name —
		// the user kind must win the catalog regardless of layout.
		"skills/custom/alpha/SKILL.md": mkSkillMD("alpha", "", "legacy-custom-alpha"),
		// Same kind in both layouts (transient state): flat wins.
		"skills/custom/gamma/SKILL.md": mkSkillMD("gamma", "", "legacy-custom-gamma"),
	}

	got := parseBundle(ctx, files)
	byName := map[string]Skill{}
	for _, sk := range got {
		byName[sk.Name] = sk
	}

	want := map[string]struct {
		kind string
		body string
	}{
		"alpha":   {models.SkillKindCustom, "legacy-custom-alpha"}, // user kind wins the shadow
		"beta":    {models.SkillKindPlatform, "flat-beta"},
		"gamma":   {models.SkillKindCustom, "flat-gamma"}, // flat wins the same-kind tie
		"delta":   {models.SkillKindOrg, "legacy-delta"},
		"epsilon": {models.SkillKindPlatform, "legacy-epsilon"},
		"zeta":    {models.SkillKindImported, "legacy-zeta"},
	}
	if len(got) != len(want) {
		names := make([]string, 0, len(got))
		for _, sk := range got {
			names = append(names, sk.Kind+"/"+sk.Name)
		}
		sort.Strings(names)
		t.Fatalf("catalog size = %d, want %d: %v", len(got), len(want), names)
	}
	for name, w := range want {
		sk, ok := byName[name]
		if !ok {
			t.Fatalf("skill %q missing from catalog", name)
		}
		if sk.Kind != w.kind {
			t.Fatalf("%q kind = %q, want %q", name, sk.Kind, w.kind)
		}
		if !strings.Contains(sk.SkillMD, w.body) {
			t.Fatalf("%q resolved the wrong copy: %q", name, sk.SkillMD)
		}
	}
	if got := byName["beta"].References["references/notes.md"]; got != "beta ref" {
		t.Fatalf("flat references not attached: %v", byName["beta"].References)
	}
	if got := byName["epsilon"].References["references/ref.md"]; got != "epsilon ref" {
		t.Fatalf("legacy references not attached: %v", byName["epsilon"].References)
	}
}

// ---- provisioning seeds the FLAT layout ---------------------------------------

// lsTree lists every blob path at main on the org's origin.
func lsTree(t *testing.T, c *ComponentStore, orgID string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", c.host.origin(orgID).Dir(), "ls-tree", "-r", "--name-only", "main").Output()
	if err != nil {
		t.Fatalf("ls-tree: %v", err)
	}
	return strings.Fields(strings.TrimSpace(string(out)))
}

func TestProvision_SeedsFlatLayout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := NewComponentStore(t)

	skills, err := c.Svc.List(ctx, "org1") // first read provisions + seeds
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(skills) != 9 {
		t.Fatalf("seeded catalog size = %d, want 9", len(skills))
	}

	paths := lsTree(t, c, "org1")
	for _, p := range paths {
		parts := strings.Split(p, "/")
		if parts[0] != "skills" {
			continue
		}
		if legacyKindDirs[parts[1]] != "" {
			t.Fatalf("seed wrote a legacy kind dir: %s", p)
		}
	}
	// Spot-check both kinds land flat, with the platform marker only on
	// platform skills.
	goMD := c.host.origin("org1").FileAt(t, "main", "skills/go/SKILL.md")
	if strings.Contains(goMD, "kind: platform") {
		t.Fatalf("org skill must not carry the platform marker:\n%s", goMD)
	}
	hlaMD := c.host.origin("org1").FileAt(t, "main", "skills/high-level-architecture/SKILL.md")
	if !strings.Contains(hlaMD, "kind: platform") {
		t.Fatalf("platform skill must carry metadata.aep.kind: platform:\n%s", hlaMD)
	}
}

// ---- reconcile migrates a legacy repo ------------------------------------------

func TestReconcile_MigratesLegacyRepo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := NewComponentStore(t)

	// Provision (seeds flat), then rebuild the origin into the LEGACY layout an
	// old deployment would have left: nested kind dirs only.
	if _, err := c.Svc.List(ctx, "org1"); err != nil {
		t.Fatalf("List: %v", err)
	}
	origin := c.host.origin("org1")
	origin.Remove(t, "test: drop flat seed", lsTree(t, c, "org1")...)
	origin.Seed(t, map[string]string{
		"skills/builtin/go/SKILL.md":          mkSkillMD("go", "", "OLD embedded go"),
		"skills/flow/task-planning/SKILL.md":  mkSkillMD("task-planning", "", "OLD flow body"),
		"skills/builtin/retired/SKILL.md":     mkSkillMD("retired", "", "no longer shipped"),
		"skills/custom/mine/SKILL.md":         mkSkillMD("mine", "", "my custom skill"),
		"skills/custom/mine/references/r.md":  "my ref",
		"skills/custom/react-webapp/SKILL.md": mkSkillMD("react-webapp", "", "user shadow of an org skill"),
	}, "test: legacy layout")
	before := origin.HeadSHA(t)

	n, err := c.Svc.Reconcile(ctx, "org1")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n == 0 {
		t.Fatalf("migration reconcile reported no changes")
	}

	// ONE migration commit on top of the legacy state.
	out, err := exec.Command("git", "-C", origin.Dir(), "rev-parse", "main^").Output()
	if err != nil {
		t.Fatalf("rev-parse main^: %v", err)
	}
	if parent := strings.TrimSpace(string(out)); parent != before {
		t.Fatalf("migration must be a single commit: head parent = %s, want %s", parent, before)
	}

	// No legacy kind dirs remain.
	for _, p := range lsTree(t, c, "org1") {
		parts := strings.Split(p, "/")
		if parts[0] == "skills" && legacyKindDirs[parts[1]] != "" {
			t.Fatalf("legacy path survived migration: %s", p)
		}
	}

	skills, err := c.Svc.List(ctx, "org1")
	if err != nil {
		t.Fatalf("List after migration: %v", err)
	}
	byName := map[string]Skill{}
	for _, sk := range skills {
		byName[sk.Name] = sk
	}
	// The full embedded library is back (9) plus the preserved custom skill,
	// minus nothing — react-webapp is user-owned now. retired is purged.
	if len(skills) != 10 {
		t.Fatalf("catalog size after migration = %d, want 10: %+v", len(skills), keysOf(byName))
	}
	if _, ok := byName["retired"]; ok {
		t.Fatalf("retired legacy builtin must be purged")
	}
	// Preserved custom skill: flat, stamped, references intact, editable kind.
	mine := byName["mine"]
	if mine.Kind != models.SkillKindCustom {
		t.Fatalf("mine kind = %q, want custom", mine.Kind)
	}
	if mine.References["references/r.md"] != "my ref" {
		t.Fatalf("mine references lost: %v", mine.References)
	}
	if !strings.Contains(origin.FileAt(t, "main", "skills/mine/SKILL.md"), "kind: custom") {
		t.Fatalf("migrated custom skill must be stamped")
	}
	// Shadow: the user copy owns the name; the embedded org skill is skipped.
	rw := byName["react-webapp"]
	if rw.Kind != models.SkillKindCustom || !strings.Contains(rw.SkillMD, "user shadow") {
		t.Fatalf("shadow must resolve custom-wins, got kind=%q body=%q", rw.Kind, rw.SkillMD)
	}
	// Drifted embedded skills were rewritten from the embed.
	if strings.Contains(byName["go"].SkillMD, "OLD embedded go") {
		t.Fatalf("embedded go must be rewritten from the embed")
	}
	if strings.Contains(byName["task-planning"].SkillMD, "OLD flow body") {
		t.Fatalf("embedded task-planning must be rewritten from the embed")
	}

	// The badge must not offer an "update" for a user-owned name.
	ups, err := c.Svc.UpdatesAvailable(ctx, "org1")
	if err != nil {
		t.Fatalf("UpdatesAvailable: %v", err)
	}
	for _, u := range ups {
		if u.Name == "react-webapp" {
			t.Fatalf("user-owned name must not surface on the updates badge: %+v", ups)
		}
	}

	// Steady state: a second reconcile is a no-op.
	n2, err := c.Svc.Reconcile(ctx, "org1")
	if err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("second reconcile must be a no-op, changed %d", n2)
	}
}
