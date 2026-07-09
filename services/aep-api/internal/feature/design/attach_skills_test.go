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

package design

import (
	"context"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/internal/feature/dependencies/resources"
	"github.com/wso2/aep/aep-api/models"
)

// --- attachAnnotatedSkills (pure function) -----------------------------------

// resourceDep builds a platform-resource dependency of the given resourceType.
func resourceDep(name, resourceType string) models.Dependency {
	return models.Dependency{Kind: models.DependencyKindPlatformResource, Name: name, ResourceType: resourceType}
}

// skillMarker returns a marker map flagging resourceType as carrying the
// given skill annotation.
func skillMarker(resourceType, skill string) map[string]resources.TypeMarkers {
	return map[string]resources.TypeMarkers{resourceType: {Skill: skill}}
}

// (a) dep with Skill marker → skillsApplied gains it.
func TestAttachAnnotatedSkills_AttachesWhenAnnotated(t *testing.T) {
	t.Parallel()
	d := &artifacts.DesignFile{
		Components: []models.DesignComponent{{
			Name:         "storefront-web",
			Dependencies: []models.Dependency{resourceDep("user-auth", "thunder-app")},
		}},
	}
	changed := attachAnnotatedSkills(d, skillMarker("thunder-app", "thunder-authentication"))
	if !changed {
		t.Fatal("want changed=true")
	}
	if want := []string{"thunder-authentication"}; !equalStrings(d.SkillsApplied, want) {
		t.Fatalf("SkillsApplied = %v, want %v", d.SkillsApplied, want)
	}
}

// (b) already present → no duplicate, changed=false.
func TestAttachAnnotatedSkills_NoDuplicateWhenAlreadyPresent(t *testing.T) {
	t.Parallel()
	d := &artifacts.DesignFile{
		SkillsApplied: []string{"thunder-authentication"},
		Components: []models.DesignComponent{{
			Name:         "storefront-web",
			Dependencies: []models.Dependency{resourceDep("user-auth", "thunder-app")},
		}},
	}
	changed := attachAnnotatedSkills(d, skillMarker("thunder-app", "thunder-authentication"))
	if changed {
		t.Fatal("want changed=false — skill already present")
	}
	if want := []string{"thunder-authentication"}; !equalStrings(d.SkillsApplied, want) {
		t.Fatalf("SkillsApplied = %v, want %v (no duplicate)", d.SkillsApplied, want)
	}
}

// (c) unannotated type → untouched.
func TestAttachAnnotatedSkills_UnannotatedTypeUntouched(t *testing.T) {
	t.Parallel()
	d := &artifacts.DesignFile{
		Components: []models.DesignComponent{{
			Name:         "orders-api",
			Dependencies: []models.Dependency{resourceDep("orders-db", "postgres-cnpg")},
		}},
	}
	// postgres-cnpg carries no skill annotation.
	changed := attachAnnotatedSkills(d, map[string]resources.TypeMarkers{"postgres-cnpg": {}})
	if changed {
		t.Fatal("want changed=false — type carries no skill annotation")
	}
	if len(d.SkillsApplied) != 0 {
		t.Fatalf("SkillsApplied = %v, want empty", d.SkillsApplied)
	}
}

// (d) multiple deps with the same skill → one entry.
func TestAttachAnnotatedSkills_MultipleDepsSameSkillOneEntry(t *testing.T) {
	t.Parallel()
	d := &artifacts.DesignFile{
		Components: []models.DesignComponent{
			{
				Name:         "storefront-web",
				Dependencies: []models.Dependency{resourceDep("user-auth", "thunder-app")},
			},
			{
				Name:         "orders-api",
				Dependencies: []models.Dependency{resourceDep("service-auth", "thunder-app")},
			},
		},
	}
	changed := attachAnnotatedSkills(d, skillMarker("thunder-app", "thunder-authentication"))
	if !changed {
		t.Fatal("want changed=true")
	}
	if want := []string{"thunder-authentication"}; !equalStrings(d.SkillsApplied, want) {
		t.Fatalf("SkillsApplied = %v, want exactly one entry %v", d.SkillsApplied, want)
	}
}

// (e) existing skillsApplied entries preserved verbatim, append-only ordering.
func TestAttachAnnotatedSkills_PreservesExistingEntriesVerbatim(t *testing.T) {
	t.Parallel()
	d := &artifacts.DesignFile{
		SkillsApplied: []string{"z-first-manual-skill", "a-second-manual-skill"},
		Components: []models.DesignComponent{{
			Name:         "storefront-web",
			Dependencies: []models.Dependency{resourceDep("user-auth", "thunder-app")},
		}},
	}
	changed := attachAnnotatedSkills(d, skillMarker("thunder-app", "thunder-authentication"))
	if !changed {
		t.Fatal("want changed=true")
	}
	want := []string{"z-first-manual-skill", "a-second-manual-skill", "thunder-authentication"}
	if !equalStrings(d.SkillsApplied, want) {
		t.Fatalf("SkillsApplied = %v, want %v (existing entries first, untouched order)", d.SkillsApplied, want)
	}
}

// Non-platform-resource dependency kinds must never qualify, even if their
// (meaningless) ResourceType field happens to collide with a marked type.
func TestAttachAnnotatedSkills_NonPlatformResourceDepIgnored(t *testing.T) {
	t.Parallel()
	d := &artifacts.DesignFile{
		Components: []models.DesignComponent{{
			Name: "orders-api",
			Dependencies: []models.Dependency{
				{Kind: models.DependencyKindOrgService, Name: "billing", ResourceType: "thunder-app"},
			},
		}},
	}
	changed := attachAnnotatedSkills(d, skillMarker("thunder-app", "thunder-authentication"))
	if changed {
		t.Fatal("want changed=false — org-service dependency must never qualify")
	}
	if len(d.SkillsApplied) != 0 {
		t.Fatalf("SkillsApplied = %v, want empty", d.SkillsApplied)
	}
}

// A nil markers map (no platform-resource dependency in the design → no
// catalog fetch, see resourceMarkersForAuthDerivation) qualifies nothing.
func TestAttachAnnotatedSkills_NilMarkersNoop(t *testing.T) {
	t.Parallel()
	d := &artifacts.DesignFile{
		Components: []models.DesignComponent{{
			Name:         "orders-api",
			Dependencies: []models.Dependency{resourceDep("user-auth", "thunder-app")},
		}},
	}
	if changed := attachAnnotatedSkills(d, nil); changed {
		t.Fatal("want changed=false with a nil marker map")
	}
	if len(d.SkillsApplied) != 0 {
		t.Fatalf("SkillsApplied = %v, want empty", d.SkillsApplied)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- wiring: SaveAndProceed persists the skill attach before the tag-cut ----

// designFilesWithDepsAndSkills is designFilesWithDeps (proceed_gate_test.go)
// with the root design.md frontmatter carrying pre-existing skillsApplied
// entries — used to assert append-only behavior against on-disk state.
func designFilesWithDepsAndSkills(depsJSON string, existingSkills []string) map[string]string {
	files := designFilesWithDeps(depsJSON)
	var fm strings.Builder
	fm.WriteString("---\nsourceSpec: v1\n")
	if len(existingSkills) > 0 {
		fm.WriteString("skillsApplied:\n")
		for _, s := range existingSkills {
			fm.WriteString("  - " + s + "\n")
		}
	}
	fm.WriteString("---\n\nOverview.\n")
	files[artifacts.DesignRootFile] = fm.String()
	return files
}

// (a) dep with Skill marker → skillsApplied gains it, persisted to root
// design.md BEFORE the tag-cut (mirrors
// TestSaveAndProceed_DerivesEndUserAuthAndPersistsBeforeTag).
func TestSaveAndProceed_AttachesAnnotatedSkillAndPersists(t *testing.T) {
	t.Parallel()
	deps := `[{"kind":"platform-resource","name":"user-auth","resourceType":"thunder-app"}]`
	fake := happySave(designFilesWithDeps(deps))
	svc := newService(fake)
	fc := &fakeCommitter{}
	svc.fileCommitter = fc
	// Skill-only marker (no EndUserAuth) isolates this to a single skill-attach
	// commit — the auth derivation must not also fire and commit here.
	svc.resourceCatalog = &fakeMarkerCatalog{markers: skillMarker("thunder-app", "thunder-authentication")}

	got, err := svc.SaveAndProceed(context.Background(), "acme", "web", "")
	if err != nil {
		t.Fatalf("SaveAndProceed: unexpected error: %v", err)
	}
	if got.Status != "approved" {
		t.Fatalf("status = %q, want approved", got.Status)
	}
	if fc.commits != 1 {
		t.Fatalf("want exactly one skill-attach commit, got %d", fc.commits)
	}
	if len(fc.writes) != 1 {
		t.Fatalf("want a single-file commit (root design.md), got %d writes", len(fc.writes))
	}
	w := fc.writes[0]
	if !strings.HasSuffix(w.Path, "design.md") || strings.Contains(w.Path, "components/") {
		t.Fatalf("commit path = %q, want the root design.md", w.Path)
	}
	if w.BaseSHA != "sha-design" {
		t.Fatalf("commit must CAS on the read sha, got %q", w.BaseSHA)
	}
	if !strings.Contains(w.Content, "thunder-authentication") {
		t.Fatalf("committed design.md missing the attached skill:\n%s", w.Content)
	}
}

// (b) already present → no duplicate, no extra commit.
func TestSaveAndProceed_SkillAlreadyPresentNoExtraCommit(t *testing.T) {
	t.Parallel()
	deps := `[{"kind":"platform-resource","name":"user-auth","resourceType":"thunder-app"}]`
	files := designFilesWithDepsAndSkills(deps, []string{"thunder-authentication"})
	fake := happySave(files)
	svc := newService(fake)
	fc := &fakeCommitter{}
	svc.fileCommitter = fc
	svc.resourceCatalog = &fakeMarkerCatalog{markers: skillMarker("thunder-app", "thunder-authentication")}

	if _, err := svc.SaveAndProceed(context.Background(), "acme", "web", ""); err != nil {
		t.Fatalf("SaveAndProceed: unexpected error: %v", err)
	}
	if fc.commits != 0 {
		t.Fatalf("skill already present — want zero commits, got %d", fc.commits)
	}
}

// (c) unannotated types → untouched, no commit.
func TestSaveAndProceed_UnannotatedTypeSkillsUntouched(t *testing.T) {
	t.Parallel()
	deps := `[{"kind":"platform-resource","name":"orders-db","resourceType":"postgres-cnpg"}]`
	fake := happySave(designFilesWithDeps(deps))
	svc := newService(fake)
	fc := &fakeCommitter{}
	svc.fileCommitter = fc
	// postgres-cnpg carries no skill annotation.
	svc.resourceCatalog = &fakeMarkerCatalog{markers: map[string]resources.TypeMarkers{"postgres-cnpg": {}}}

	if _, err := svc.SaveAndProceed(context.Background(), "acme", "web", ""); err != nil {
		t.Fatalf("SaveAndProceed: unexpected error: %v", err)
	}
	if fc.commits != 0 {
		t.Fatalf("unannotated resource type — want zero commits, got %d", fc.commits)
	}
}

// (d) multiple deps with the same skill → one entry persisted.
func TestSaveAndProceed_MultipleDepsSameSkillPersistsOneEntry(t *testing.T) {
	t.Parallel()
	deps := `[` +
		`{"kind":"platform-resource","name":"user-auth","resourceType":"thunder-app"},` +
		`{"kind":"platform-resource","name":"service-auth","resourceType":"thunder-app"}` +
		`]`
	fake := happySave(designFilesWithDeps(deps))
	svc := newService(fake)
	fc := &fakeCommitter{}
	svc.fileCommitter = fc
	svc.resourceCatalog = &fakeMarkerCatalog{markers: skillMarker("thunder-app", "thunder-authentication")}

	if _, err := svc.SaveAndProceed(context.Background(), "acme", "web", ""); err != nil {
		t.Fatalf("SaveAndProceed: unexpected error: %v", err)
	}
	if fc.commits != 1 {
		t.Fatalf("want exactly one commit, got %d", fc.commits)
	}
	w := fc.writes[0]
	if got := strings.Count(w.Content, "thunder-authentication"); got != 1 {
		t.Fatalf("committed design.md must list the skill exactly once, got %d occurrences in:\n%s", got, w.Content)
	}
}

// Composition pin: a SINGLE resource type whose markers carry BOTH the
// end-user-auth role AND a skill annotation (the real thunder-app CRT shape
// once G5 lands), declared by a service component, on a design that already
// has a skillsApplied entry. The save must land exactly TWO commits in order
// — first the auth-derivation design.json commit, then the skillsApplied
// design.md commit — with disjoint paths, fetch the marker map exactly once,
// and only THEN cut the tag at HEAD (CommitSHA "" — the re-read-after-commit
// convention guarantees the tagged tree includes both changes). This pins the
// two-commit composition so a future coalescing refactor can't silently
// break it.
func TestSaveAndProceed_AuthRoleAndSkillMarkerComposeTwoCommitsThenTag(t *testing.T) {
	t.Parallel()
	deps := `[{"kind":"platform-resource","name":"user-auth","resourceType":"thunder-app"}]`
	files := designFilesWithDepsAndSkills(deps, []string{"manually-added-skill"})
	fake := happySave(files)
	fc := &fakeCommitter{}

	// Pin the initial read to a commit: both persistence steps must drop the
	// pin (re-read at HEAD) before the tag-cut.
	fake.GetDesignAtCommitFunc = func(context.Context, string, string, string) (map[string]string, error) {
		return files, nil
	}

	// Capture the tag-cut moment: how many derive/attach commits had landed by
	// the time SaveDesign ran, and which CommitSHA it was asked to tag.
	commitsAtTagCut := -1
	tagCutCommitSHA := "sentinel-not-called"
	inner := fake.SaveDesignFunc
	fake.SaveDesignFunc = func(ctx context.Context, org, project string, req artifacts.SaveRequest) (*artifacts.DesignSaveResult, error) {
		commitsAtTagCut = fc.commits
		tagCutCommitSHA = req.CommitSHA
		return inner(ctx, org, project, req)
	}

	svc := newService(fake)
	svc.fileCommitter = fc
	cat := &fakeMarkerCatalog{markers: map[string]resources.TypeMarkers{
		"thunder-app": {EndUserAuth: true, Skill: "thunder-authentication"},
	}}
	svc.resourceCatalog = cat

	got, err := svc.SaveAndProceed(context.Background(), "acme", "web", "pinned-sha")
	if err != nil {
		t.Fatalf("SaveAndProceed: unexpected error: %v", err)
	}
	if got.Status != "approved" {
		t.Fatalf("status = %q, want approved", got.Status)
	}

	// (4) one catalog fetch serves both consumers.
	if cat.calls != 1 {
		t.Fatalf("marker map must be fetched exactly once, got %d calls", cat.calls)
	}

	// (1) exactly two commits, auth derivation first, skill attach second,
	// disjoint paths.
	if fc.commits != 2 || len(fc.commitLog) != 2 {
		t.Fatalf("want exactly two commits (auth derivation + skill attach), got %d (%d logged)", fc.commits, len(fc.commitLog))
	}
	authWrites, skillWrites := fc.commitLog[0], fc.commitLog[1]
	if len(authWrites) != 1 || !strings.HasSuffix(authWrites[0].Path, "components/consumer/design.json") {
		t.Fatalf("first commit must be the consumer design.json auth derivation, got %+v", authWrites)
	}
	if !strings.Contains(authWrites[0].Content, `"auth": "end-user-required"`) {
		t.Fatalf("first commit missing the derived auth:\n%s", authWrites[0].Content)
	}
	if len(skillWrites) != 1 || strings.Contains(skillWrites[0].Path, "components/") ||
		!strings.HasSuffix(skillWrites[0].Path, "design.md") {
		t.Fatalf("second commit must be the root design.md skill attach, got %+v", skillWrites)
	}
	if authWrites[0].Path == skillWrites[0].Path {
		t.Fatalf("the two commits must touch disjoint paths, both hit %q", authWrites[0].Path)
	}

	// (2) the skill commit carries the pre-existing entry AND the attached one.
	if !strings.Contains(skillWrites[0].Content, "manually-added-skill") {
		t.Fatalf("skill commit lost the pre-existing entry:\n%s", skillWrites[0].Content)
	}
	if !strings.Contains(skillWrites[0].Content, "thunder-authentication") {
		t.Fatalf("skill commit missing the attached skill:\n%s", skillWrites[0].Content)
	}

	// (3) the tag-cut ran after BOTH commits and resolved at HEAD (the pinned
	// sha was dropped by the re-read-after-commit convention, so the tagged
	// tree includes both changes).
	if commitsAtTagCut != 2 {
		t.Fatalf("SaveDesign must run after both commits, saw %d at tag-cut", commitsAtTagCut)
	}
	if tagCutCommitSHA != "" {
		t.Fatalf("tag-cut CommitSHA = %q, want \"\" (HEAD, past the pinned sha and both commits)", tagCutCommitSHA)
	}
}

// (e) existing skillsApplied entries preserved verbatim through a save that
// also attaches a new one.
func TestSaveAndProceed_ExistingSkillsAppliedPreservedOnAttach(t *testing.T) {
	t.Parallel()
	deps := `[{"kind":"platform-resource","name":"user-auth","resourceType":"thunder-app"}]`
	files := designFilesWithDepsAndSkills(deps, []string{"manually-added-skill"})
	fake := happySave(files)
	svc := newService(fake)
	fc := &fakeCommitter{}
	svc.fileCommitter = fc
	svc.resourceCatalog = &fakeMarkerCatalog{markers: skillMarker("thunder-app", "thunder-authentication")}

	if _, err := svc.SaveAndProceed(context.Background(), "acme", "web", ""); err != nil {
		t.Fatalf("SaveAndProceed: unexpected error: %v", err)
	}
	if fc.commits != 1 {
		t.Fatalf("want exactly one commit, got %d", fc.commits)
	}
	w := fc.writes[0]
	if !strings.Contains(w.Content, "manually-added-skill") {
		t.Fatalf("committed design.md lost the pre-existing skill entry:\n%s", w.Content)
	}
	if !strings.Contains(w.Content, "thunder-authentication") {
		t.Fatalf("committed design.md missing the newly attached skill:\n%s", w.Content)
	}
}
