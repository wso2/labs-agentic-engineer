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

// UNIT tier: the reconcile.go branches repo_store_test.go doesn't reach. That
// file proves seed-on-first-read (built-ins + flow), rewrite-of-a-missing
// skill, and the no-op. This file adds: the org-edit-preserved branch (a
// diverged, manifest-tracked repo copy the platform hasn't moved is left
// alone — reconcile_manifest_test.go covers the rest of the three-way
// matrix), an unmanaged org skill surviving reconcile untouched, the
// UpdatesAvailable rows (stale + absent), the embedded loaders for both
// kinds, and the EnsureProvisioned guards.
package spec

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

// goBuiltinStale is a minimal valid `go` SKILL.md whose body differs from the
// embedded built-in — planted in the repo so the content-diff reconcile
// branches fire (the embedded `go`'s content SHA never equals this).
func goBuiltinStale() string {
	return "---\nname: go\ndescription: Minimal go built-in for the reconcile tests.\n---\n\n# Go\n\nstale body\n"
}

// embeddedSkill returns one embedded skill by name (its canonical content),
// so a test can assert the repo copy converged to it.
func embeddedSkill(t *testing.T, name string) Skill {
	t.Helper()
	emb, err := loadLibrary(testLibraryFS(t))
	if err != nil {
		t.Fatalf("loadEmbeddedLibrary: %v", err)
	}
	if sk, ok := nameSet(emb)[name]; ok {
		return sk
	}
	t.Fatalf("embedded skill %q missing", name)
	return Skill{}
}

// contentSHAOf returns the resolved skill's content SHA, or fails the test.
func contentSHAOf(t *testing.T, skills []Skill, name string) string {
	t.Helper()
	for _, sk := range skills {
		if sk.Name == name {
			return sk.ContentSHA
		}
	}
	t.Fatalf("skill %q not present in %v", name, skillKeysOf(nameSet(skills)))
	return ""
}

// TestReconcile_OrgEditPreserved pins the three-way semantics (spec §3): once
// a skill's baseline is stamped by seeding, a repo copy that diverges from
// the baseline while the platform side does NOT move is an ORG EDIT
// (actionOverride) — reconcile must leave it alone, not clobber it back to
// the embedded content. This supersedes the old two-way
// "content-diff always wins" behavior.
func TestReconcile_OrgEditPreserved(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil { // seed at embed content (stamps the baseline)
		t.Fatalf("seed: %v", err)
	}
	// Plant a `go` whose content differs from the embed — an org edit, since
	// the embed hasn't moved.
	host.writeAtHead("org1", skillRepoPath("go"), goBuiltinStale())

	n, err := svc.Reconcile(ctx, "org1")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n != 0 {
		t.Fatalf("Reconcile changed %d, want 0 (org edit must be preserved)", n)
	}
	// The planted content survives untouched.
	got, _ := svc.List(ctx, "org1")
	if !strings.Contains(contentOf(t, got, "go"), "stale body") {
		t.Fatalf("org edit to `go` was clobbered: %+v", nameSet(got)["go"])
	}
}

// contentOf returns the resolved skill's SKILL.md, or fails the test.
func contentOf(t *testing.T, skills []Skill, name string) string {
	t.Helper()
	for _, sk := range skills {
		if sk.Name == name {
			return sk.SkillMD
		}
	}
	t.Fatalf("skill %q not present in %v", name, skillKeysOf(nameSet(skills)))
	return ""
}

// TestReconcile_KeepsUnmanagedOrgSkill pins the three-way purge scope (spec
// §3): reconcile purges ONLY names the manifest still tracks as
// platform-shipped that the embed no longer ships. A repo-only skill that was
// never platform-shipped (no manifest entry — org-authored, or a name that
// simply never round-tripped through seed/reconcile) is never touched, even
// though it happens to share a name pattern with a retired built-in. This
// supersedes the old two-way behavior, which purged any name absent from the
// embed regardless of provenance.
func TestReconcile_KeepsUnmanagedOrgSkill(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// A skill the embed has never shipped, written directly to the repo — org
	// authored, no manifest entry.
	host.writeAtHead("org1", skillRepoPath("retired-legacy"),
		"---\nname: retired-legacy\ndescription: No longer shipped.\n---\n\ngone\n")

	n, err := svc.Reconcile(ctx, "org1")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n != 0 {
		t.Fatalf("Reconcile changed %d, want 0 (unmanaged org skill must be kept)", n)
	}
	got, _ := svc.List(ctx, "org1")
	if _, present := nameSet(got)["retired-legacy"]; !present {
		t.Fatalf("unmanaged org skill should be kept, was purged: %v", skillKeysOf(nameSet(got)))
	}
	// The real built-ins are unaffected.
	if _, ok := nameSet(got)["go"]; !ok {
		t.Fatalf("reconcile removed a live built-in")
	}
}

func TestUpdatesAvailable_ReportsStaleAndAbsent(t *testing.T) {
	t.Parallel()

	t.Run("stale built-in surfaces on the badge", func(t *testing.T) {
		t.Parallel()
		svc, host := newTestStore(t)
		ctx := context.Background()
		if _, err := svc.List(ctx, "org1"); err != nil {
			t.Fatalf("seed: %v", err)
		}
		host.writeAtHead("org1", skillRepoPath("go"), goBuiltinStale())

		ups, err := svc.UpdatesAvailable(ctx, "org1")
		if err != nil {
			t.Fatalf("UpdatesAvailable: %v", err)
		}
		if len(ups) != 1 || ups[0].Name != "go" {
			t.Fatalf("updates = %+v, want one {go}", ups)
		}
	})

	t.Run("absent org-kind default does NOT surface (opt-in)", func(t *testing.T) {
		t.Parallel()
		svc, host := newTestStore(t)
		ctx := context.Background()
		if _, err := svc.List(ctx, "org1"); err != nil {
			t.Fatalf("seed: %v", err)
		}
		// go is org-kind. An absent org-kind default (org-deleted, or a
		// newly-shipped default not yet added) is OPT-IN on ongoing sync:
		// Reconcile won't seed it, so UpdatesAvailable must NOT advertise it
		// as an "update" (mirrors reconcileEmbedded's org-kind-absent skip).
		// It belongs to the separate "available to add" surface, not the
		// updates badge — otherwise the badge invites a sync that no-ops.
		host.removeAtHead("org1", skillRepoPath("go"))

		ups, err := svc.UpdatesAvailable(ctx, "org1")
		if err != nil {
			t.Fatalf("UpdatesAvailable: %v", err)
		}
		for i := range ups {
			if ups[i].Name == "go" {
				t.Fatalf("absent org-kind default must NOT surface on the updates badge, got %+v", ups)
			}
		}
	})

	t.Run("a missing platform skill surfaces on the badge", func(t *testing.T) {
		t.Parallel()
		svc, host := newTestStore(t)
		ctx := context.Background()
		if _, err := svc.List(ctx, "org1"); err != nil {
			t.Fatalf("seed: %v", err)
		}
		// Platform skills list read-only on the skills page, so their drift
		// participates in the badge like any embedded skill.
		host.removeAtHead("org1", skillRepoPath("task-planning"))

		ups, err := svc.UpdatesAvailable(ctx, "org1")
		if err != nil {
			t.Fatalf("UpdatesAvailable: %v", err)
		}
		if len(ups) != 1 || ups[0].Name != "task-planning" {
			t.Fatalf("missing platform skill must surface on the badge, got %+v", ups)
		}
	})
}

func TestEnsureProvisioned_Guards(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// nil service, nil repos, and empty org are all no-op successes — never a
	// panic, never a spurious repo creation.
	var nilSvc *SkillService
	if err := nilSvc.EnsureProvisioned(ctx, "org1"); err != nil {
		t.Fatalf("nil service: %v", err)
	}
	if err := NewSkillService(nil, nil, nil).EnsureProvisioned(ctx, "org1"); err != nil {
		t.Fatalf("nil repos: %v", err)
	}
	svc, _ := newTestStore(t)
	if err := svc.EnsureProvisioned(ctx, ""); err != nil {
		t.Fatalf("empty org: %v", err)
	}

	// A real provision seeds the built-ins and is idempotent on a second call.
	if err := svc.EnsureProvisioned(ctx, "org1"); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if err := svc.EnsureProvisioned(ctx, "org1"); err != nil {
		t.Fatalf("re-provision: %v", err)
	}
	got, _ := svc.List(ctx, "org1")
	if _, ok := nameSet(got)["go"]; !ok {
		t.Fatalf("provision did not seed built-ins: %v", skillKeysOf(nameSet(got)))
	}
}

// The unified embedded library: every skill vendored from repo-root skills/
// loads with its kind read from frontmatter — platform for the generation
// skills, org for the stack skills, both stamped metadata.aep.kind explicitly
// (an absent kind still defaults to org). One loader, one source tree.
func TestLoadEmbeddedLibrary(t *testing.T) {
	t.Parallel()
	fsys := testLibraryFS(t)
	got, err := loadLibrary(fsys)
	if err != nil {
		t.Fatalf("loadEmbeddedLibrary: %v", err)
	}
	by := nameSet(got)
	// Every top-level dir under skills/ is presently a well-formed skill, so
	// the loader must carry all of them through. Derived from the tree itself
	// (not a hardcoded literal) so this never needs bumping when a skill is
	// added to or removed from skills/. Loose files at the library root are not
	// skills and are not counted — skills/AGENTS.md is one.
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		t.Fatalf("read skills dir: %v", err)
	}
	skillDirs := 0
	for _, e := range entries {
		if e.IsDir() {
			skillDirs++
		}
	}
	if len(got) != skillDirs {
		t.Fatalf("library size = %d, want %d (dirs in skills/): %v", len(got), skillDirs, skillKeysOf(by))
	}
	wantKinds := map[string]string{
		"api-management": "org", "ballerina": "org", "go": "org", "react-webapp": "org",
		"thunder-authentication": "org",
		"cell-design":            "platform", "design": "platform",
		"wireframes": "platform", "grilling": "platform",
		"architecture": "platform", "openapi-conventions": "platform", "start": "platform",
		"task-planning": "platform", "validation-criteria": "platform",
	}
	for name, kind := range wantKinds {
		sk, ok := by[name]
		if !ok {
			t.Fatalf("embedded skill %q missing; got %v", name, skillKeysOf(by))
		}
		if sk.Kind != kind {
			t.Fatalf("%q kind = %q, want %q", name, sk.Kind, kind)
		}
		if sk.ContentSHA == "" || sk.SkillMD == "" || sk.Description == "" {
			t.Fatalf("%q has empty body/sha/description", name)
		}
	}
	// References ride along where the source tree has them.
	if got := by["openapi-conventions"].References["references/wso2-rest-api-design-guidelines.md"]; got == "" {
		t.Fatalf("openapi-conventions reference missing")
	}
}

// TestLoadLibrary_StandardStructure: a skill dir carrying scripts/, assets/,
// references/ (non-.md included), a nested extra dir, and a binary file loads
// with every aux file byte-faithful under its relative path; a dotfile and a
// nested dot-dir file are both skipped.
func TestLoadLibrary_StandardStructure(t *testing.T) {
	t.Parallel()
	bin := string([]byte{0x00, 0xFF, 0x10, 0x80}) // not valid UTF-8
	fsys := fstest.MapFS{
		"demo/SKILL.md":             {Data: []byte(mkSkillMD("demo", "platform", "demo body"))},
		"demo/references/a.md":      {Data: []byte("ref a")},
		"demo/references/data.json": {Data: []byte(`{"k":1}`)},
		"demo/scripts/run.mjs":      {Data: []byte("console.log(1)\n")},
		"demo/assets/t.template.ts": {Data: []byte("export const T = 1\n")},
		"demo/extra/notes.txt":      {Data: []byte("extra file")},
		"demo/assets/logo.png":      {Data: []byte(bin)},
		"demo/.hidden":              {Data: []byte("skip me")},
		"demo/scripts/.cache/x":     {Data: []byte("skip me too")},
	}
	got, err := loadLibrary(fsys)
	if err != nil {
		t.Fatalf("loadLibrary: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 skill, got %d", len(got))
	}
	sk := got[0]
	want := map[string]string{
		"references/a.md":      "ref a",
		"references/data.json": `{"k":1}`,
		"scripts/run.mjs":      "console.log(1)\n",
		"assets/t.template.ts": "export const T = 1\n",
		"extra/notes.txt":      "extra file",
		"assets/logo.png":      bin,
	}
	if len(sk.References) != len(want) {
		t.Fatalf("aux files = %v, want %v", keysOf(sk.References), keysOf(want))
	}
	for p, content := range want {
		if sk.References[p] != content {
			t.Fatalf("%s not byte-faithful", p)
		}
	}
	if _, ok := sk.References["scripts/.cache/x"]; ok {
		t.Fatalf("nested dot-dir file must not be carried: %v", keysOf(sk.References))
	}
}

// TestLoadLibrary_RefsOnlySHAUnchanged: a references-only skill's ContentSHA
// is computed from the same inputs as before the standard-structure change.
func TestLoadLibrary_RefsOnlySHAUnchanged(t *testing.T) {
	t.Parallel()
	md := mkSkillMD("solo", "platform", "solo body")
	fsys := fstest.MapFS{
		"solo/SKILL.md":        {Data: []byte(md)},
		"solo/references/r.md": {Data: []byte("r")},
	}
	got, err := loadLibrary(fsys)
	if err != nil || len(got) != 1 {
		t.Fatalf("loadLibrary: %v (%d skills)", err, len(got))
	}
	if want := contentSHA(md, map[string]string{"references/r.md": "r"}); got[0].ContentSHA != want {
		t.Fatalf("SHA drifted: %s != %s", got[0].ContentSHA, want)
	}
}
