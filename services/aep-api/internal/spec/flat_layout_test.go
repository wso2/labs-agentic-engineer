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

// The flat repo layout + kind-in-frontmatter contract: skills/<name>/SKILL.md
// with `metadata.aep.kind` naming the kind (absent → org), the legacy
// skills/<kindDir>/<name>/ layout parsed for not-yet-migrated repos, the
// service-side kind stamping, and the reconcile-driven one-commit migration.
package spec

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
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
		out, err := stampFrontmatterKind(in, SkillKindOrg)
		if err != nil {
			t.Fatalf("stamp: %v", err)
		}
		fm, body, err := parseSkillMD(out)
		if err != nil {
			t.Fatalf("re-parse: %v", err)
		}
		if got := frontmatterKind(fm); got != SkillKindOrg {
			t.Fatalf("kind after stamp = %q, want org", got)
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
		in := mkSkillMD("alpha", "org", "BODY")
		out, err := stampFrontmatterKind(in, SkillKindOrg)
		if err != nil {
			t.Fatalf("stamp: %v", err)
		}
		if out != in {
			t.Fatalf("re-stamping the same kind must be byte-identical:\n in: %q\nout: %q", in, out)
		}
	})

	t.Run("overrides a spoofed kind", func(t *testing.T) {
		t.Parallel()
		// A create/import must not let user content claim platform status.
		in := mkSkillMD("alpha", "platform", "BODY")
		out, err := stampFrontmatterKind(in, SkillKindOrg)
		if err != nil {
			t.Fatalf("stamp: %v", err)
		}
		fm, _, err := parseSkillMD(out)
		if err != nil {
			t.Fatalf("re-parse: %v", err)
		}
		if got := frontmatterKind(fm); got != SkillKindOrg {
			t.Fatalf("kind after stamp = %q, want org (spoof must not survive)", got)
		}
	})

	t.Run("preserves sibling frontmatter and metadata keys", func(t *testing.T) {
		t.Parallel()
		in := "---\nname: alpha\ndescription: d.\nlicense: MIT\nmetadata:\n  team: platform-eng\n---\n\nBODY\n"
		out, err := stampFrontmatterKind(in, SkillKindImported)
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
		if got := frontmatterKind(fm); got != SkillKindImported {
			t.Fatalf("kind = %q, want imported", got)
		}
		if fm.License != "MIT" {
			t.Fatalf("license lost: %+v", fm)
		}
	})
}

// ---- provisioning seeds the FLAT layout ---------------------------------------

// lsTree lists every file path at the tip of the org's skills repo.
func lsTree(t *testing.T, c *ComponentStore, orgID string) []string {
	t.Helper()
	return c.host.origin(orgID).Paths(t)
}

func TestProvision_SeedsFlatLayout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := NewComponentStore(t)

	skills, err := c.Svc.List(ctx, "org1") // first read provisions + seeds
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// A freshly provisioned org's catalog is exactly the embedded library —
	// nothing else has been added yet. Derived from loadLibrary over the same
	// tree the seed read from, so this never needs bumping when a skill is
	// added to or removed from skills/.
	embedded, err := loadLibrary(testLibraryFS(t))
	if err != nil {
		t.Fatalf("loadLibrary: %v", err)
	}
	if len(skills) != len(embedded) {
		t.Fatalf("seeded catalog size = %d, want %d (embedded library size)", len(skills), len(embedded))
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
	goMD := c.host.origin("org1").FileAt(t, "skills/go/SKILL.md")
	if strings.Contains(goMD, "kind: platform") {
		t.Fatalf("org skill must not carry the platform marker:\n%s", goMD)
	}
	hlaMD := c.host.origin("org1").FileAt(t, "skills/architecture/SKILL.md")
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
		"skills/custom/mine/scripts/s.sh":     "#!/bin/sh\necho mine\n",
		"skills/custom/react-webapp/SKILL.md": mkSkillMD("react-webapp", "", "user shadow of an org skill"),
	}, "test: legacy layout")
	callsBefore := len(c.host.pod.Calls())

	n, err := c.Svc.Reconcile(ctx, "org1")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n == 0 {
		t.Fatalf("migration reconcile reported no changes")
	}

	// ONE migration commit on top of the legacy state.
	commits := 0
	for _, call := range c.host.pod.Calls()[callsBefore:] {
		if call.Op == aestudiotest.OpCommit {
			commits++
		}
	}
	if commits != 1 {
		t.Fatalf("migration must be a single commit, made %d", commits)
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
	// Every embedded default comes back, platform- and org-kind alike: this
	// legacy repo predates tombstones, so an absent org-kind name was never
	// deleted-with-a-tombstone — it is simply one this repo has never been
	// handed, and seeding it is the point. Present org-kind names (go,
	// react-webapp) are migrated/preserved rather than re-seeded. Plus the
	// preserved custom skill (mine). retired is purged. Both sides are derived
	// from the library rather than hardcoded, so this never needs bumping when
	// a skill is added to or removed from skills/.
	for _, orgDefault := range []string{"api-management", "thunder-authentication"} {
		if _, ok := byName[orgDefault]; !ok {
			t.Fatalf("org-kind default %q was not seeded on migration: %+v", orgDefault, skillKeysOf(byName))
		}
	}
	// The whole embedded library + mine (the preserved custom skill).
	const preservedCustom = 1
	if want := EmbeddedLibraryCount(t, "") + preservedCustom; len(skills) != want {
		t.Fatalf("catalog size after migration = %d, want %d: %+v", len(skills), want, skillKeysOf(byName))
	}
	if _, ok := byName["retired"]; ok {
		t.Fatalf("retired legacy builtin must be purged")
	}
	// Preserved custom skill: flat, references intact. The retired "custom"
	// legacy dir now folds to org (legacyKindDirs["custom"] == SkillKindOrg).
	mine := byName["mine"]
	if mine.Kind != SkillKindOrg {
		t.Fatalf("mine kind = %q, want org (custom folds into org)", mine.Kind)
	}
	if mine.References["references/r.md"] != "my ref" {
		t.Fatalf("mine references lost: %v", mine.References)
	}
	if mine.References["scripts/s.sh"] != "#!/bin/sh\necho mine\n" {
		t.Fatalf("mine scripts/s.sh lost: %v", mine.References)
	}
	// The migrated legacy-custom skill is stamped with its resolved kind, which
	// under the fold is org (custom folds into org — legacyKindDirs["custom"]
	// == SkillKindOrg), not the retired "custom".
	if !strings.Contains(origin.FileAt(t, "skills/mine/SKILL.md"), "kind: org") {
		t.Fatalf("migrated legacy-custom skill must be stamped kind: org (custom folds into org)")
	}
	// Shadow: the user copy owns the name; its diverged content is preserved as
	// an override, not clobbered back to the embedded org skill's content.
	rw := byName["react-webapp"]
	if rw.Kind != SkillKindOrg || !strings.Contains(rw.SkillMD, "user shadow") {
		t.Fatalf("shadow must preserve the user's content, got kind=%q body=%q", rw.Kind, rw.SkillMD)
	}
	// Divergent legacy copies of embedded names predate the manifest, so under
	// the three-way reconcile they are pre-manifest backfill-overrides: moved
	// flat and stamped, but their content is preserved, not clobbered back to
	// the embed (spec §3 — same rule TestReconcile_BackfillStampsPreManifestRepo
	// pins for the flat-layout case).
	if !strings.Contains(byName["go"].SkillMD, "OLD embedded go") {
		t.Fatalf("divergent pre-manifest legacy copy of go must be preserved as an override, not rewritten from the embed")
	}
	if !strings.Contains(byName["task-planning"].SkillMD, "OLD flow body") {
		t.Fatalf("divergent pre-manifest legacy copy of task-planning must be preserved as an override, not rewritten from the embed")
	}

	// react-webapp is now an org-kind override of an embedded name (org is no
	// longer user-owned by kind post-fold), so it correctly surfaces on the
	// badge as "overridden" rather than being skipped outright — the badge
	// must never offer it as an "update" (never propose clobbering the org's
	// content back to the embed).
	ups, err := c.Svc.UpdatesAvailable(ctx, "org1")
	if err != nil {
		t.Fatalf("UpdatesAvailable: %v", err)
	}
	for _, u := range ups {
		if u.Name == "react-webapp" && u.State != "overridden" {
			t.Fatalf("react-webapp must surface as overridden, not %q: %+v", u.State, ups)
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

// ---- org-repo write/read paths carry the full skill structure -----------------

// TestReconcile_CarriesStandardStructure: an embedded skill with scripts/,
// assets/, and a nested extra file round-trips seed → org repo → catalog read
// with all files intact, and a re-reconcile is a no-op (stable ContentSHA).
func TestReconcile_CarriesStandardStructure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := NewComponentStoreWithLibrary(t, fstest.MapFS{
		"demo/SKILL.md":        {Data: []byte(mkSkillMD("demo", "platform", "demo"))},
		"demo/scripts/run.mjs": {Data: []byte("console.log(1)\n")},
		"demo/assets/tpl.ts":   {Data: []byte("export {}\n")},
		"demo/extra/notes.txt": {Data: []byte("extra")},
		"demo/TOPLEVEL.txt":    {Data: []byte("root-level aux file, no subdir\n")},
	})
	skills, err := c.Svc.List(ctx, "org1") // first read provisions + seeds
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byName := map[string]Skill{}
	for _, sk := range skills {
		byName[sk.Name] = sk
	}
	demo := byName["demo"]
	for _, p := range []string{"scripts/run.mjs", "assets/tpl.ts", "extra/notes.txt", "TOPLEVEL.txt"} {
		if demo.References[p] == "" {
			t.Fatalf("catalog read lost %s: %v", p, skillKeysOf(byName))
		}
	}
	// Files are physically in the org repo.
	if got := c.host.origin("org1").FileAt(t, "skills/demo/scripts/run.mjs"); got != "console.log(1)\n" {
		t.Fatalf("org repo scripts file = %q", got)
	}
	// Re-reconcile: clean copy, no rewrite.
	if n, err := c.Svc.Reconcile(ctx, "org1"); err != nil || n != 0 {
		t.Fatalf("re-reconcile must be a no-op, wrote %d (err %v)", n, err)
	}
}

// TestReconcile_FlatSkillNamedLikeLegacyKindDir: a flat skill literally named
// "custom" — a legacyKindDirs key — must survive the catalog read intact,
// SKILL.md and aux file both. Depth alone discriminates flat vs. legacy
// layout, never the name; before this test's fix, skills/custom/SKILL.md
// (depth 3) was routed into the legacy branch, which requires depth >= 4, and
// the skill silently vanished from the catalog.
func TestReconcile_FlatSkillNamedLikeLegacyKindDir(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := NewComponentStoreWithLibrary(t, fstest.MapFS{
		"custom/SKILL.md":        {Data: []byte(mkSkillMD("custom", "platform", "flat skill literally named custom"))},
		"custom/scripts/run.mjs": {Data: []byte("console.log(1)\n")},
	})
	skills, err := c.Svc.List(ctx, "org1") // first read provisions + seeds
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byName := map[string]Skill{}
	for _, sk := range skills {
		byName[sk.Name] = sk
	}
	custom, ok := byName["custom"]
	if !ok {
		t.Fatalf("flat skill named %q vanished from the catalog: %v", "custom", skillKeysOf(byName))
	}
	if !strings.Contains(custom.SkillMD, "flat skill literally named custom") {
		t.Fatalf("custom SKILL.md body wrong: %q", custom.SkillMD)
	}
	if custom.References["scripts/run.mjs"] != "console.log(1)\n" {
		t.Fatalf("custom aux file lost: %v", custom.References)
	}
	// Re-reconcile: clean copy, no rewrite.
	if n, err := c.Svc.Reconcile(ctx, "org1"); err != nil || n != 0 {
		t.Fatalf("re-reconcile must be a no-op, wrote %d (err %v)", n, err)
	}
}
