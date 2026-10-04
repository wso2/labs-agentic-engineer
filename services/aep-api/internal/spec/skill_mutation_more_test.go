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

// UNIT tier: the read/write-surface branches repo_store_test.go leaves open —
// the SkillMutationService.Update paths beyond the component tier's 403/404
// (happy round-trip, NAME_IMMUTABLE, imported ⇒ not-found), and the read
// degrade path (a pod outage serves empty and never fails the run, §12).
// The commit retry on a baseSha conflict is pinned in repo_store_test.go and
// skills_commit_test.go.
package spec

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

func TestUpdate_HappyAndGuards(t *testing.T) {
	t.Parallel()
	svc, _ := newTestStore(t)
	mut := NewSkillMutationService(svc)
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	create := "---\nname: my-skill\ndescription: initial.\nmetadata:\n  aep.version: \"1\"\n---\n\nv1 body\n"
	if _, err := mut.Create(ctx, "org1", "tester", CreateSkillInput{Name: "my-skill", SkillMD: create}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	t.Run("happy update round-trips new content", func(t *testing.T) {
		updated := "---\nname: my-skill\ndescription: revised.\nmetadata:\n  aep.version: \"2\"\n---\n\nv2 body\n"
		sk, err := mut.Update(ctx, "org1", "tester", "my-skill", UpdateSkillInput{SkillMD: updated})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if sk == nil || sk.Kind != SkillKindOrg || sk.Description != "revised." {
			t.Fatalf("updated skill = %+v, want the revised org-kind content", sk)
		}
		got, _ := svc.Resolve(ctx, "org1", "my-skill")
		if got == nil || got.Description != "revised." {
			t.Fatalf("read-back after update = %+v, want the revised content", got)
		}
	})

	t.Run("rename via update is rejected (NAME_IMMUTABLE)", func(t *testing.T) {
		renamed := "---\nname: renamed-skill\ndescription: d.\nmetadata:\n  aep.version: \"1\"\n---\n\nbody\n"
		_, err := mut.Update(ctx, "org1", "tester", "my-skill", UpdateSkillInput{SkillMD: renamed})
		assertIssueCode(t, err, "NAME_IMMUTABLE")
	})

	t.Run("updating an org skill (platform-seeded) is now allowed", func(t *testing.T) {
		// Editability is decoupled from ownership: "go" is a platform-seeded
		// org skill, and org is editable per SkillEditable — editing it keeps
		// its kind org (an override reconcile will leave alone).
		body := "---\nname: go\ndescription: d.\nmetadata:\n  aep.version: \"1\"\n---\n\nbody\n"
		sk, err := mut.Update(ctx, "org1", "tester", "go", UpdateSkillInput{SkillMD: body})
		if err != nil {
			t.Fatalf("Update go: %v", err)
		}
		if sk.Kind != SkillKindOrg {
			t.Fatalf("updated go kind = %q, want org (kind preserved)", sk.Kind)
		}
	})

	t.Run("updating a platform skill is forbidden", func(t *testing.T) {
		body := "---\nname: task-planning\ndescription: d.\nmetadata:\n  aep.version: \"1\"\n---\n\nbody\n"
		if _, err := mut.Update(ctx, "org1", "tester", "task-planning", UpdateSkillInput{SkillMD: body}); !errors.Is(err, ErrSkillNotEditable) {
			t.Fatalf("update platform skill err = %v, want ErrSkillNotEditable", err)
		}
	})

	t.Run("updating a missing skill is not-found", func(t *testing.T) {
		body := "---\nname: ghost\ndescription: d.\nmetadata:\n  aep.version: \"1\"\n---\n\nbody\n"
		if _, err := mut.Update(ctx, "org1", "tester", "ghost", UpdateSkillInput{SkillMD: body}); !errors.Is(err, ErrSkillNotFound) {
			t.Fatalf("update missing err = %v, want ErrSkillNotFound", err)
		}
	})
}

func TestUpdate_ImportedIsUpdatable(t *testing.T) {
	t.Parallel()
	svc, _ := newTestStore(t)
	mut := NewSkillMutationService(svc)
	imp := NewSkillImportService(svc)
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Import a skill, then PUT it — imported is editable per SkillEditable, and
	// Update preserves the kind (imported stays imported).
	tgz := makeTarGz(t, map[string]string{
		"imp-skill/":         "",
		"imp-skill/SKILL.md": skillMDNamed("imp-skill", ""),
	})
	if _, err := imp.Import(ctx, "org1", "tester", bytes.NewReader(tgz)); err != nil {
		t.Fatalf("Import: %v", err)
	}
	body := skillMDNamed("imp-skill", "")
	sk, err := mut.Update(ctx, "org1", "tester", "imp-skill", UpdateSkillInput{SkillMD: body})
	if err != nil {
		t.Fatalf("update imported err = %v, want success", err)
	}
	if sk.Kind != SkillKindImported {
		t.Fatalf("updated imported kind = %q, want imported (kind preserved)", sk.Kind)
	}
}

func TestCatalog_DegradesOnReadError(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Simulate an outage: the org's pod stops serving. The catalog must
	// degrade (serve empty) rather than fail the design/task run (§12).
	host.pod.FailOrg("org1", sourcecontrol.ErrAEStudioUnavailable)

	got, err := svc.List(ctx, "org1")
	if err != nil {
		t.Fatalf("List during outage must not error, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("outage degrade should serve empty, got %v", namesOf(got))
	}
}

// Editing a platform-seeded org skill in-console makes it an override:
// content changes, manifest baseline is preserved, reconcile won't revert it.
func TestUpdate_SeededOrgSkillBecomesOverride(t *testing.T) {
	t.Parallel()
	svc, host := newTestStoreWithLibrary(t, libWith("v1")) // "demo" is org-kind, seeded
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil { // seed + stamp baseline
		t.Fatalf("seed: %v", err)
	}
	baseBefore := parseSkillsManifest([]byte(host.readAtHead("org1", skillsManifestPath)))["demo"].BaseHash

	mut := NewSkillMutationService(svc)
	if _, err := mut.Update(ctx, "org1", "tester", "demo", UpdateSkillInput{SkillMD: demoMD("org edit")}); err != nil {
		t.Fatalf("update seeded org skill should be allowed: %v", err)
	}
	if got := host.readAtHead("org1", skillRepoPath("demo")); !strings.Contains(got, "org edit") {
		t.Fatalf("edit did not land: %q", got)
	}
	baseAfter := parseSkillsManifest([]byte(host.readAtHead("org1", skillsManifestPath)))["demo"].BaseHash
	if baseAfter != baseBefore {
		t.Fatalf("edit must NOT move the manifest baseline (that is what makes it an override): %s -> %s", baseBefore, baseAfter)
	}
	// Reconcile now sees repo != baseHash, platform == baseHash → override, left alone.
	if _, err := svc.Reconcile(ctx, "org1"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := host.readAtHead("org1", skillRepoPath("demo")); !strings.Contains(got, "org edit") {
		t.Fatalf("reconcile reverted an in-console edit: %q", got)
	}
}

// A user-authored org skill round-trips: create -> delete succeeds (it has no
// platform manifest entry).
func TestDelete_UserAuthoredOrgSkill_RoundTrips(t *testing.T) {
	t.Parallel()
	svc, _ := newTestStore(t)
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil { // provision
		t.Fatalf("provision: %v", err)
	}
	mut := NewSkillMutationService(svc)
	create := "---\nname: acme-notes\ndescription: mine.\nmetadata:\n  aep.version: \"1\"\n---\n\nbody\n"
	if _, err := mut.Create(ctx, "org1", "tester", CreateSkillInput{Name: "acme-notes", SkillMD: create}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if !SkillDeletable(SkillKindOrg) {
		t.Fatal("SkillDeletable(org) = false, want true")
	}
	if err := mut.Delete(ctx, "org1", "tester", "acme-notes"); err != nil {
		t.Fatalf("user-authored org skill must be deletable: %v", err)
	}
}

// A platform-kind skill is never editable, and therefore never deletable —
// deletable = editable (Task 2), so this is the only kind that stays
// forbidden. (Previously this pinned a platform-SEEDED ORG-kind skill as
// forbidden; #310's seeded-org gate is gone — see
// TestDelete_SeededOrgSkill_NowDeletable below.)
func TestDelete_SeededOrgSkill_Forbidden(t *testing.T) {
	t.Parallel()
	svc, _ := newTestStore(t)
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil { // seed + stamp baseline
		t.Fatalf("seed: %v", err)
	}
	mut := NewSkillMutationService(svc)
	if err := mut.Delete(ctx, "org1", "tester", "task-planning"); !errors.Is(err, ErrSkillNotEditable) {
		t.Fatalf("delete platform-kind skill err = %v, want ErrSkillNotEditable", err)
	}

	// The pure projection (spec.SkillDeletable, feeding the GetSkill/Update
	// detail response) must agree with Delete's own guard.
	if SkillDeletable(SkillKindPlatform) {
		t.Fatal("SkillDeletable(platform) = true, want false — platform kind is never editable/deletable")
	}
	if SkillEditable(SkillKindPlatform) {
		t.Fatal("platform kind must never be editable")
	}
}

// A platform-seeded org skill (demo) is now deletable — Task 1's reconcile
// change means the delete sticks (no auto-re-add on the next reconcile).
func TestDelete_SeededOrgSkill_NowDeletable(t *testing.T) {
	t.Parallel()
	svc, host := newTestStoreWithLibrary(t, orgLib("demo", "v1")) // demo = seeded org
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// baseline: manifest has demo (origin platform)
	if parseSkillsManifest([]byte(host.readAtHead("org1", skillsManifestPath)))["demo"].Origin == "" {
		t.Fatal("precondition: demo should have a manifest entry")
	}
	mut := NewSkillMutationService(svc)
	if err := mut.Delete(ctx, "org1", "tester", "demo"); err != nil {
		t.Fatalf("a seeded org skill must now be deletable: %v", err)
	}
	if got := host.readAtHead("org1", skillRepoPath("demo")); got != "" {
		t.Fatalf("delete did not remove files: %q", got)
	}
	// Files gone, but the manifest entry OUTLIVES them as a tombstone — it is
	// the only record that this org threw the skill away, and it is what stops
	// the next reconcile handing it straight back.
	e, ok := parseSkillsManifest([]byte(host.readAtHead("org1", skillsManifestPath)))["demo"]
	if !ok {
		t.Fatal("delete must leave a tombstone entry, not drop the manifest entry")
	}
	if !e.Removed {
		t.Fatalf("manifest entry survived delete but is not tombstoned: %#v", e)
	}
	// The point of the tombstone: an ongoing sync must not resurrect it.
	if _, err := svc.Reconcile(ctx, "org1"); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got := host.readAtHead("org1", skillRepoPath("demo")); got != "" {
		t.Fatalf("sync resurrected a deleted skill: %q", got)
	}
}

// orgKindMD is an org-kind skill md WITH its kind explicitly stamped, so a
// console edit (which always stamps the kind) can reproduce it byte-for-byte —
// the precondition for a converging edit. (An unstamped org skill like the
// real "go" can never converge through the console, since the console adds a
// stamp the embedded copy lacks; that path stays an override, which is
// correct.)
func orgKindMD(name, body string) string {
	return "---\nname: " + name + "\ndescription: Convergence fixture.\nmetadata:\n  aep:\n    kind: org\n---\n\n# " +
		name + "\n\n" + body + "\n"
}

func orgLibKind(name, body string) fstest.MapFS {
	return fstest.MapFS{name + "/SKILL.md": &fstest.MapFile{Data: []byte(orgKindMD(name, body))}}
}

func stateOf(ups []SkillUpdate, name string) string {
	for _, u := range ups {
		if u.Name == name {
			return u.State
		}
	}
	return ""
}

// A console edit that CONVERGES a platform-managed org skill onto the
// platform's CURRENT version advances the manifest baseline in the same
// commit. Without this the baseline stays frozen at the version the org last
// synced from, and the next platform release reads the now-identical copy as
// an org edit diverged from a stale base — a false conflict (skills-experience
// spec §3). This is the console counterpart of reconcile's converged-copy
// backfill.
func TestUpdate_ConvergingEditAdvancesBaseline(t *testing.T) {
	t.Parallel()
	svc, host := newTestStoreWithLibrary(t, orgLibKind("demo", "v1"))
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil { // seed at v1, stamp baseline
		t.Fatalf("seed: %v", err)
	}
	// Platform ships v2; the org has NOT synced (its copy is still v1).
	svc.SwapLibrary(orgLibKind("demo", "v2"))
	emb, err := loadLibrary(svc.library)
	if err != nil {
		t.Fatalf("load library: %v", err)
	}
	v2SHA := contentSHAOf(t, emb, "demo")

	// The user edits demo in-console to exactly the platform's v2 content.
	mut := NewSkillMutationService(svc)
	if _, err := mut.Update(ctx, "org1", "tester", "demo", UpdateSkillInput{SkillMD: orgKindMD("demo", "v2")}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// The baseline advanced to the platform's current version.
	base := parseSkillsManifest([]byte(host.readAtHead("org1", skillsManifestPath)))["demo"].BaseHash
	if base != v2SHA {
		t.Fatalf("converging edit must advance baseHash to the embedded v2 SHA: got %q want %q", base, v2SHA)
	}

	// So a later platform release (v3) reads as a CLEAN update, not a false
	// conflict (a stale v1 base would have yielded "conflict").
	svc.SwapLibrary(orgLibKind("demo", "v3"))
	ups, err := svc.UpdatesAvailable(ctx, "org1")
	if err != nil {
		t.Fatalf("UpdatesAvailable: %v", err)
	}
	if st := stateOf(ups, "demo"); st != "update" {
		t.Fatalf("after convergence + platform move, demo state = %q, want \"update\"", st)
	}
}

// Reading updates against a repo with NO manifest (a team that predates it,
// or a manually deleted one) lazily backfills the manifest — self-heal on the
// read path, so a pre-manifest repo reaches a stamped baseline without needing
// an "update" row to light up the Sync button first.
func TestUpdatesAvailable_BackfillsMissingManifest(t *testing.T) {
	t.Parallel()
	svc, host := newTestStoreWithLibrary(t, orgLibKind("demo", "v1"))
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil { // seed (writes the manifest)
		t.Fatalf("seed: %v", err)
	}
	// Simulate a pre-manifest / manifest-deleted repo: remove the file outright
	// (an empty file would read as PRESENT and never trigger the backfill).
	host.removeAtHead("org1", skillsManifestPath)
	if got := host.readAtHead("org1", skillsManifestPath); got != "" {
		t.Fatalf("precondition: manifest should be gone, got %q", got)
	}

	if _, err := svc.UpdatesAvailable(ctx, "org1"); err != nil {
		t.Fatalf("UpdatesAvailable: %v", err)
	}

	// The read recreated the manifest with a real baseline for the seeded skill.
	raw := host.readAtHead("org1", skillsManifestPath)
	if raw == "" {
		t.Fatal("updates-read did not backfill the missing manifest")
	}
	if m := parseSkillsManifest([]byte(raw)); m["demo"].Origin != ManifestOriginPlatform || m["demo"].BaseHash == "" {
		t.Fatalf("backfilled manifest missing demo baseline: %#v", m)
	}
}

func namesOf(skills []Skill) []string {
	out := make([]string, 0, len(skills))
	for _, sk := range skills {
		out = append(out, sk.Name)
	}
	return out
}
