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

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// ---- Task 2: desiredMirror (pure) ------------------------------------------

func TestDesiredMirror(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		lib    []Skill
		pinned map[string]bool
		wantMD map[string]string // skill name -> expected SKILL.md content, absent = must not appear
		absent []string          // skill names that must NOT appear at all
	}{
		{
			name: "coding audience + enabled is included",
			lib: []Skill{
				{Name: "a", Enabled: true, Audience: []string{SkillAudienceCoding}, SkillMD: "MD-A"},
			},
			wantMD: map[string]string{"a": "MD-A"},
		},
		{
			name: "design-only is excluded",
			lib: []Skill{
				{Name: "b", Enabled: true, Audience: []string{SkillAudienceDesign}, SkillMD: "MD-B"},
			},
			absent: []string{"b"},
		},
		{
			name: "disabled is excluded",
			lib: []Skill{
				{Name: "c", Enabled: false, Audience: []string{SkillAudienceCoding}, SkillMD: "MD-C"},
			},
			absent: []string{"c"},
		},
		{
			name: "disabled BUT pinned is included (drift guard)",
			lib: []Skill{
				{Name: "d", Enabled: false, Audience: []string{SkillAudienceCoding}, SkillMD: "MD-D"},
			},
			pinned: map[string]bool{"d": true},
			wantMD: map[string]string{"d": "MD-D"},
		},
		{
			name: "design-only AND disabled BUT pinned is still included — pin overrides both axes",
			lib: []Skill{
				{Name: "d2", Enabled: false, Audience: []string{SkillAudienceDesign}, SkillMD: "MD-D2"},
			},
			pinned: map[string]bool{"d2": true},
			wantMD: map[string]string{"d2": "MD-D2"},
		},
		{
			name: "unmarked audience (permissive default) is included",
			lib: []Skill{
				{Name: "e", Enabled: true, Audience: nil, SkillMD: "MD-E"},
			},
			wantMD: map[string]string{"e": "MD-E"},
		},
		{
			name: "not pinned and not the org's own — absent entirely",
			lib: []Skill{
				{Name: "f", Enabled: true, Audience: []string{SkillAudienceDesign}, SkillMD: "MD-F"},
			},
			pinned: map[string]bool{"unrelated": true},
			absent: []string{"f"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := desiredMirror(tc.lib, tc.pinned)
			for name, want := range tc.wantMD {
				key := ".claude/skills/" + name + "/SKILL.md"
				gotContent, ok := got[key]
				if !ok {
					t.Fatalf("desiredMirror missing %q; got keys %v", key, mirrorKeys(got))
				}
				if string(gotContent) != want {
					t.Fatalf("desiredMirror[%q] = %q, want %q", key, gotContent, want)
				}
			}
			for _, name := range tc.absent {
				key := ".claude/skills/" + name + "/SKILL.md"
				if _, ok := got[key]; ok {
					t.Fatalf("desiredMirror must NOT include %q, got keys %v", key, mirrorKeys(got))
				}
			}
		})
	}
}

// References are carried at `.claude/skills/<name>/<refPath>`, alongside the
// SKILL.md, for a copied skill.
func TestDesiredMirror_ReferencesCarriedAtRightPaths(t *testing.T) {
	t.Parallel()
	lib := []Skill{
		{
			Name:     "with-refs",
			Enabled:  true,
			Audience: []string{SkillAudienceCoding},
			SkillMD:  "MD-BODY",
			References: map[string]string{
				"references/guide.md": "GUIDE",
				"scripts/setup.sh":    "SETUP",
			},
		},
	}
	got := desiredMirror(lib, nil)
	want := map[string]string{
		".claude/skills/with-refs/SKILL.md":            "MD-BODY",
		".claude/skills/with-refs/references/guide.md": "GUIDE",
		".claude/skills/with-refs/scripts/setup.sh":    "SETUP",
	}
	if len(got) != len(want) {
		t.Fatalf("desiredMirror = %v, want exactly %v", mirrorKeys(got), keysOfStr(want))
	}
	for k, v := range want {
		if string(got[k]) != v {
			t.Fatalf("desiredMirror[%q] = %q, want %q", k, got[k], v)
		}
	}
}

// A skill excluded by the copy rule contributes NOTHING — not even its
// reference files — to the desired tree.
func TestDesiredMirror_ExcludedSkillCarriesNoReferences(t *testing.T) {
	t.Parallel()
	lib := []Skill{
		{
			Name:       "excluded",
			Enabled:    false,
			Audience:   []string{SkillAudienceCoding},
			SkillMD:    "MD",
			References: map[string]string{"references/x.md": "X"},
		},
	}
	got := desiredMirror(lib, nil)
	if len(got) != 0 {
		t.Fatalf("desiredMirror for an excluded skill = %v, want empty", mirrorKeys(got))
	}
}

func mirrorKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---- Task 1: ListForMirror -------------------------------------------------

// The mirror's whole safety story rests on ListForMirror surfacing a git
// failure as an error rather than degrading to empty like List/catalog do —
// an empty return here would be indistinguishable from "the org genuinely has
// no skills" and would make SyncProjectSkills prune every copy in every
// project. This test drives a REAL failure: the org's skills-repo origin is
// deleted out from under an already-provisioned service, so the next read's
// `git fetch` fails.
func TestListForMirror_FailingHostReturnsError(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()

	// Provision + seed once so there's a real origin to break.
	if _, err := svc.List(ctx, "org1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	origin := host.origin("org1")
	if origin == nil {
		t.Fatal("origin not provisioned")
	}
	if err := os.RemoveAll(origin.Dir()); err != nil {
		t.Fatalf("break origin: %v", err)
	}

	if _, err := svc.ListForMirror(ctx, "org1"); err == nil {
		t.Fatal("ListForMirror on a failing git host = nil error, want an error")
	}

	// Contrast: the degraded read (List) must NOT error — it serves empty.
	// This is the exact behaviour ListForMirror exists to avoid relying on.
	got, err := svc.List(ctx, "org1")
	if err != nil {
		t.Fatalf("List must never error even when the host is failing: %v", err)
	}
	_ = got
}

// ---- Task 3: SyncProjectSkills ---------------------------------------------

const testProjectID = "proj1"
const testProjectRepoName = "project-repo"

// mkSkillMDAudience builds a minimal valid SKILL.md whose only metadata is
// `metadata.aep.audience` — kind is left unmarked (defaults to org), which is
// irrelevant to the mirror's copy rule.
func mkSkillMDAudience(name string, audience []string, body string) string {
	meta := ""
	if len(audience) > 0 {
		meta = "metadata:\n  aep:\n    audience: [" + strings.Join(audience, ", ") + "]\n"
	}
	return fmt.Sprintf("---\nname: %s\ndescription: d.\n%s---\n\n%s\n", name, meta, body)
}

// provisionProjectRepo arranges the project's git repo row + real origin, the
// way project creation already has by the time SyncProjectSkills is ever
// called (Task 4's call site sits AFTER WriteDescriptor, when the repo
// certainly exists).
func provisionProjectRepo(t *testing.T, host *testGitHost, orgID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := host.EnsureBareRepo(ctx, orgID, testProjectID, testProjectRepoName); err != nil {
		t.Fatalf("provision project repo: %v", err)
	}
}

func TestSyncProjectSkills_FreshRepoGainsTree(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	orgID := "org1"

	if _, err := svc.List(ctx, orgID); err != nil { // provision + seed the skills repo
		t.Fatalf("seed skills repo: %v", err)
	}
	host.writeAtHead(orgID, skillRepoPath("cool-skill"), mkSkillMDAudience("cool-skill", []string{SkillAudienceCoding}, "guidance body"))
	provisionProjectRepo(t, host, orgID)

	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); err != nil {
		t.Fatalf("SyncProjectSkills: %v", err)
	}

	got := host.readAtHeadIn(orgID, testProjectID, ".claude/skills/cool-skill/SKILL.md")
	if !strings.Contains(got, "guidance body") {
		t.Fatalf("project repo missing the mirrored skill; got %q", got)
	}
}

func TestSyncProjectSkills_UpToDateRepoProducesNoSecondCommit(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	orgID := "org1"

	if _, err := svc.List(ctx, orgID); err != nil {
		t.Fatalf("seed skills repo: %v", err)
	}
	host.writeAtHead(orgID, skillRepoPath("cool-skill"), mkSkillMDAudience("cool-skill", []string{SkillAudienceCoding}, "guidance body"))
	provisionProjectRepo(t, host, orgID)

	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	origin := host.originFor(orgID, testProjectID)
	before := gitDirOut(t, origin.Dir(), "rev-parse", "main")

	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	after := gitDirOut(t, origin.Dir(), "rev-parse", "main")

	if before != after {
		t.Fatalf("re-syncing an up-to-date repo committed again: before=%s after=%s", before, after)
	}
}

func TestSyncProjectSkills_DisabledSkillIsPruned(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	orgID := "org1"

	if _, err := svc.List(ctx, orgID); err != nil {
		t.Fatalf("seed skills repo: %v", err)
	}
	host.writeAtHead(orgID, skillRepoPath("cool-skill"), mkSkillMDAudience("cool-skill", []string{SkillAudienceCoding}, "guidance body"))
	provisionProjectRepo(t, host, orgID)

	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if got := host.readAtHeadIn(orgID, testProjectID, ".claude/skills/cool-skill/SKILL.md"); got == "" {
		t.Fatal("precondition failed: skill not mirrored on first sync")
	}

	mut := NewSkillMutationService(svc)
	if _, err := mut.SetEnabled(ctx, orgID, "tester", "cool-skill", false); err != nil {
		t.Fatalf("disable skill: %v", err)
	}

	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if got := host.readAtHeadIn(orgID, testProjectID, ".claude/skills/cool-skill/SKILL.md"); got != "" {
		t.Fatalf("disabled skill must be pruned from the mirror, still found: %q", got)
	}
}

func TestSyncProjectSkills_HandEditedCopyReverts(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	orgID := "org1"

	if _, err := svc.List(ctx, orgID); err != nil {
		t.Fatalf("seed skills repo: %v", err)
	}
	host.writeAtHead(orgID, skillRepoPath("cool-skill"), mkSkillMDAudience("cool-skill", []string{SkillAudienceCoding}, "guidance body"))
	provisionProjectRepo(t, host, orgID)

	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// A developer (or a stray tool) hand-edits the mirrored copy directly on
	// the project repo.
	host.writeAtHeadIn(orgID, testProjectID, ".claude/skills/cool-skill/SKILL.md", "HAND EDITED — SHOULD NOT SURVIVE")

	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	got := host.readAtHeadIn(orgID, testProjectID, ".claude/skills/cool-skill/SKILL.md")
	if strings.Contains(got, "HAND EDITED") {
		t.Fatalf("hand edit survived a sync: %q", got)
	}
	if !strings.Contains(got, "guidance body") {
		t.Fatalf("sync did not restore the generated content: %q", got)
	}
}

// The single most important safety property: a failing org-library read must
// abort with NO writes and NO deletes — never prune a project's guidance
// because the org-skills repo blipped.
func TestSyncProjectSkills_FailingLibraryReadAbortsWithNoWrites(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	orgID := "org1"

	if _, err := svc.List(ctx, orgID); err != nil {
		t.Fatalf("seed skills repo: %v", err)
	}
	host.writeAtHead(orgID, skillRepoPath("cool-skill"), mkSkillMDAudience("cool-skill", []string{SkillAudienceCoding}, "guidance body"))
	provisionProjectRepo(t, host, orgID)

	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	projectOrigin := host.originFor(orgID, testProjectID)
	before := gitDirOut(t, projectOrigin.Dir(), "rev-parse", "main")
	beforeContent := host.readAtHeadIn(orgID, testProjectID, ".claude/skills/cool-skill/SKILL.md")

	// Break the ORG skills repo (the library read), leaving the project repo
	// untouched and reachable.
	skillsOrigin := host.origin(orgID)
	if err := os.RemoveAll(skillsOrigin.Dir()); err != nil {
		t.Fatalf("break skills origin: %v", err)
	}

	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); err == nil {
		t.Fatal("SyncProjectSkills with a failing library read = nil error, want an error")
	}

	after := gitDirOut(t, projectOrigin.Dir(), "rev-parse", "main")
	if before != after {
		t.Fatalf("a failing library read must not commit to the project repo: before=%s after=%s", before, after)
	}
	afterContent := host.readAtHeadIn(orgID, testProjectID, ".claude/skills/cool-skill/SKILL.md")
	if afterContent != beforeContent {
		t.Fatalf("a failing library read must not change the mirrored content: before=%q after=%q", beforeContent, afterContent)
	}
}

// A component's skillsPinned is read from the project's OWN specs/design/
// tree before the org library is applied — a pinned skill lands even when
// its own audience/enabled state alone would exclude it.
func TestSyncProjectSkills_ComponentPinIncludesDesignOnlySkill(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	orgID := "org1"

	if _, err := svc.List(ctx, orgID); err != nil {
		t.Fatalf("seed skills repo: %v", err)
	}
	// design-audience-only, so the plain copy rule would exclude it.
	host.writeAtHead(orgID, skillRepoPath("design-skill"), mkSkillMDAudience("design-skill", []string{SkillAudienceDesign}, "design guidance"))
	provisionProjectRepo(t, host, orgID)

	const designJSON = `{
  "name": "orders",
  "type": "service",
  "dependencies": [],
  "skillsPinned": ["design-skill"]
}
`
	host.writeAtHeadIn(orgID, testProjectID, "specs/design/components/orders/design.json", designJSON)

	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); err != nil {
		t.Fatalf("SyncProjectSkills: %v", err)
	}
	got := host.readAtHeadIn(orgID, testProjectID, ".claude/skills/design-skill/SKILL.md")
	if !strings.Contains(got, "design guidance") {
		t.Fatalf("pinned design-only skill was not mirrored: %q", got)
	}
}

// No design yet (a brand-new project) must resolve zero pins, not error —
// the project-creation case.
func TestSyncProjectSkills_NoDesignYetIsNotAnError(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	orgID := "org1"

	if _, err := svc.List(ctx, orgID); err != nil {
		t.Fatalf("seed skills repo: %v", err)
	}
	provisionProjectRepo(t, host, orgID)

	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); err != nil {
		t.Fatalf("SyncProjectSkills on a design-less project: %v", err)
	}
}

// ---- Task 5: the lifecycle -------------------------------------------------

// The mirror walked through a project's life in the order the three call sites
// fire it: seeded at creation with no design at all, refreshed once a design
// pins skills, and refreshed again after an admin changes availability. Each
// step asserts the whole tree, not just the skill it moved — a rule that admits
// the right skill while quietly admitting a wrong one is still broken.
//
// The three call sites themselves are thin best-effort wrappers over this one
// method (the deadcode gate proves they reach it from main), so exercising the
// SEQUENCE is what validates the feature.
func TestSyncProjectSkills_Lifecycle(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	orgID := "org1"

	if _, err := svc.List(ctx, orgID); err != nil { // provision + seed the org library
		t.Fatalf("seed skills repo: %v", err)
	}
	// A library spanning every branch of the copy rule.
	host.writeAtHead(orgID, skillRepoPath("go"), mkSkillMDAudience("go", []string{SkillAudienceCoding}, "go guidance"))
	host.writeAtHead(orgID, skillRepoPath("react"), mkSkillMDAudience("react", []string{SkillAudienceCoding}, "react guidance"))
	host.writeAtHead(orgID, skillRepoPath("planning"), mkSkillMDAudience("planning", []string{SkillAudienceDesign}, "planning guidance"))
	host.writeAtHead(orgID, skillRepoPath("wireframes"),
		mkSkillMDAudience("wireframes", []string{SkillAudienceDesign, SkillAudienceCoding}, "wireframe guidance"))
	provisionProjectRepo(t, host, orgID)

	mirrored := func(t *testing.T, name string) bool {
		t.Helper()
		return host.readAtHeadIn(orgID, testProjectID, ".claude/skills/"+name+"/SKILL.md") != ""
	}
	assertTree := func(t *testing.T, step string, want map[string]bool) {
		t.Helper()
		for _, name := range []string{"go", "react", "planning", "wireframes"} {
			if got := mirrored(t, name); got != want[name] {
				t.Fatalf("%s: %q mirrored = %v, want %v", step, name, got, want[name])
			}
		}
	}

	// 1. Creation: no design exists, so no pins. Only the coding-audience
	//    skills land; a design-only skill never reaches a build's clone.
	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
	assertTree(t, "after creation", map[string]bool{
		"go": true, "react": true, "wireframes": true, "planning": false,
	})

	// 2. The design agent pins skills onto a component — including `planning`,
	//    which the audience rule alone would exclude. A pin is a component
	//    declaring it needs the skill, which outranks the default.
	host.writeAtHeadIn(orgID, testProjectID, "specs/design/components/orders/design.json", `{
  "name": "orders",
  "type": "service",
  "dependencies": [],
  "skillsPinned": ["go", "planning"]
}
`)
	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); err != nil {
		t.Fatalf("post-design sync: %v", err)
	}
	assertTree(t, "after design pins planning", map[string]bool{
		"go": true, "react": true, "wireframes": true, "planning": true,
	})

	// 3. An admin disables both a pinned skill and an unpinned one. The pinned
	//    one SURVIVES — the drift guard, so a settings toggle never breaks a
	//    build already designed against it — while the unpinned one is pruned.
	mut := NewSkillMutationService(svc)
	if _, err := mut.SetEnabled(ctx, orgID, "admin", "go", false); err != nil {
		t.Fatalf("disable go: %v", err)
	}
	if _, err := mut.SetEnabled(ctx, orgID, "admin", "react", false); err != nil {
		t.Fatalf("disable react: %v", err)
	}
	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); err != nil {
		t.Fatalf("post-disable sync: %v", err)
	}
	assertTree(t, "after disabling a pinned and an unpinned skill", map[string]bool{
		"go": true, "react": false, "wireframes": true, "planning": true,
	})

	// 4. The design drops the pins. `go` is disabled AND no longer pinned, so
	//    nothing holds it in any more; `planning` returns to design-only.
	host.writeAtHeadIn(orgID, testProjectID, "specs/design/components/orders/design.json", `{
  "name": "orders",
  "type": "service",
  "dependencies": [],
  "skillsPinned": []
}
`)
	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); err != nil {
		t.Fatalf("post-unpin sync: %v", err)
	}
	assertTree(t, "after the design drops its pins", map[string]bool{
		"go": false, "react": false, "wireframes": true, "planning": false,
	})

	// 5. Re-enabling restores availability without any design change.
	if _, err := mut.SetEnabled(ctx, orgID, "admin", "react", true); err != nil {
		t.Fatalf("re-enable react: %v", err)
	}
	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); err != nil {
		t.Fatalf("post-reenable sync: %v", err)
	}
	assertTree(t, "after re-enabling react", map[string]bool{
		"go": false, "react": true, "wireframes": true, "planning": false,
	})
}

// The seam between the AUTHORED library and the mirror: the runner's own skills
// reach a build only because their frontmatter says `audience: [coding]`, and
// nothing else in the system re-states that. There is no runner-side selection
// left to fall back on (ADR-0005), so if this frontmatter is reformatted, dropped,
// or a fourth runner skill is added without it, a coding run silently loses its
// procedure and `requireWorkflowBodies` fails every build.
//
// Driven against the REAL library rather than a fixture, because the thing under
// test is the authored bytes as much as the parser — flow-style `[coding]` has to
// decode the same way the TS mirror decodes it.
func TestRealLibrary_RunnerSkillsAreCodingAudienceAndMirrored(t *testing.T) {
	t.Parallel()
	lib, err := loadLibrary(testLibraryFS(t))
	if err != nil {
		t.Fatalf("loadLibrary: %v", err)
	}

	byName := map[string]Skill{}
	for _, sk := range lib {
		byName[sk.Name] = sk
	}

	// Every skill the runner reads on its own behalf, and what it must be.
	for name := range RequiredSkills {
		sk, ok := byName[name]
		if !ok {
			t.Fatalf("the authored library has no %q — every coding run needs it", name)
		}
		if got := sk.Audience; len(got) != 1 || got[0] != SkillAudienceCoding {
			t.Errorf("%s audience = %v, want exactly [coding] — the design agent must not be able to load it", name, got)
		}
		if sk.Kind != SkillKindPlatform {
			t.Errorf("%s kind = %q, want %q (read-only in the console)", name, sk.Kind, SkillKindPlatform)
		}
	}
	// playwright-cli is not in RequiredSkills (it loads on demand), but it is still
	// the coding agent's and still has to reach the mirror.
	if got := byName["playwright-cli"].Audience; len(got) != 1 || got[0] != SkillAudienceCoding {
		t.Errorf("playwright-cli audience = %v, want exactly [coding]", got)
	}

	// …and the copy rule admits them with nothing pinned, which is what a real
	// dispatch looks like before any design has pinned a stack skill.
	//
	// `Enabled` is stamped here rather than read off the library, and the
	// asymmetry is worth naming: `loadLibrary` describes the SHIPPED bytes and
	// leaves Enabled at its zero value, because availability is a per-org fact
	// that lives in that org's `skills-manifest.json`. What `desiredMirror`
	// consumes in production is the ORG's library (`newSkillValue`, which sets
	// Enabled from the manifest) — so feeding it `loadLibrary` output directly
	// would mirror nothing at all. `true` here is a freshly-seeded org, where no
	// admin has disabled anything.
	enabled := make([]Skill, 0, len(lib))
	for _, sk := range lib {
		sk.Enabled = true
		enabled = append(enabled, sk)
	}
	mirror := desiredMirror(enabled, nil)
	for _, name := range []string{"aep", "aep-validation", "playwright-cli"} {
		if _, ok := mirror[claudeSkillsDir+"/"+name+"/SKILL.md"]; !ok {
			t.Errorf("%s is absent from the mirror — a dispatched run would not receive it", name)
		}
	}
	// The `aep` skill's references travel too: a fan-out subagent is handed
	// component-contract.md by absolute path and can read nothing else.
	if _, ok := mirror[claudeSkillsDir+"/aep/references/component-contract.md"]; !ok {
		t.Error("aep/references/component-contract.md is absent from the mirror — fan-out subagents get a dead path")
	}
	// aep-validation is useless without its report generator, which lives under
	// scripts/ rather than references/ — proving References is not refs-only.
	if _, ok := mirror[claudeSkillsDir+"/aep-validation/scripts/generate-report.mjs"]; !ok {
		t.Error("aep-validation/scripts/generate-report.mjs is absent from the mirror")
	}
	// And the design-flow skills stay out, which is what replaced the runner's
	// explicit base-plugin selection.
	for _, name := range []string{"design", "task-planning", "high-level-architecture"} {
		if _, ok := mirror[claudeSkillsDir+"/"+name+"/SKILL.md"]; ok {
			t.Errorf("%s reached a build's mirror — a coding session must not see it", name)
		}
	}
	// Compose-time input is never skill content, in production as in the playground.
	for p := range mirror {
		if strings.Contains(p, "/overlays/") {
			t.Errorf("mirror carries compose-time input: %s", p)
		}
	}
}
