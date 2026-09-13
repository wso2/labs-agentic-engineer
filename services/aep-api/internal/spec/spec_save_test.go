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

// SaveSpec = whole-spec hard gate (requirements + design) → one annotated tag
// covering the specs/ tree, named by the user or suggested. These run over the
// real gitfs Workspace engine.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// validSpecSeed is a buildable spec: a PRD with a User Stories section, a
// design.cell declaring the components, and a valid design bundle (root + one
// enriched component claiming the story, with its type artifact) — everything
// the layout gates AND the build gate (#369) demand.
func validSpecSeed() map[string]string {
	return map[string]string{
		"specs/requirements/prd.md":                "# PRD\n\n## User Stories\n\n1. As a user, I want the thing, so that value.\n",
		"specs/design/design.cell":                 "component svc service\n",
		"specs/design/components/svc/design.md":    "---\ntype: service\n---\n# svc\n",
		"specs/design/components/svc/design.json":  validComponentDesignJSON("svc"),
		"specs/design/components/svc/openapi.yaml": "openapi: 3.0.3\n",
	}
}

func specErrPaths(t *testing.T, err error) []string {
	t.Helper()
	var se *SpecValidationError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *SpecValidationError", err)
	}
	paths := make([]string, 0, len(se.Files))
	for _, f := range se.Files {
		paths = append(paths, f.Path)
	}
	return paths
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

func TestSaveSpec_TagsAtHead(t *testing.T) {
	t.Parallel()
	r := newRig(t, validSpecSeed())
	head := r.headSHA()

	res, err := r.svc.SaveSpec(context.Background(), r.org, r.proj, SaveRequest{Message: "build v1"})
	if err != nil {
		t.Fatalf("SaveSpec: %v", err)
	}
	if res.Status != "approved" || res.Tag != "v1" {
		t.Fatalf("result = %+v, want approved/v1", res)
	}
	if res.CommitHash != head {
		t.Errorf("tag points at %s, want HEAD %s (no new commit on save)", res.CommitHash, head)
	}
	if r.headSHA() != head {
		t.Errorf("HEAD moved to %s — save must NOT commit", r.headSHA())
	}
	if got := r.tags(); len(got) != 1 || got[0] != "v1" {
		t.Errorf("tags = %v, want [v1]", got)
	}
}

func TestSaveSpec_GateRequirementsMissing(t *testing.T) {
	// The gate these assert is switched OFF (specGateDisabled), so it refuses
	// nothing and every assertion below would fail. Skipped by the SAME constant
	// rather than deleted or weakened: flipping the constant back re-arms the gate
	// and its tests together, which is what stops the gate returning unguarded.
	if specGateDisabled {
		t.Skip("whole-spec gate disabled (specGateDisabled)")
	}
	t.Parallel()
	seed := validSpecSeed()
	delete(seed, "specs/requirements/prd.md")
	r := newRig(t, seed)

	_, err := r.svc.SaveSpec(context.Background(), r.org, r.proj, SaveRequest{})
	paths := specErrPaths(t, err)
	if !containsPath(paths, "specs/requirements/prd.md") {
		t.Fatalf("validation paths = %v, want specs/requirements/prd.md", paths)
	}
	if got := r.tags(); len(got) != 0 {
		t.Errorf("tags = %v, want none (nothing may be tagged when the gate fails)", got)
	}
}

func TestSaveSpec_GateDesignMissing(t *testing.T) {
	// The gate these assert is switched OFF (specGateDisabled), so it refuses
	// nothing and every assertion below would fail. Skipped by the SAME constant
	// rather than deleted or weakened: flipping the constant back re-arms the gate
	// and its tests together, which is what stops the gate returning unguarded.
	if specGateDisabled {
		t.Skip("whole-spec gate disabled (specGateDisabled)")
	}
	t.Parallel()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "the spec\n"})

	_, err := r.svc.SaveSpec(context.Background(), r.org, r.proj, SaveRequest{})
	paths := specErrPaths(t, err)
	if !containsPath(paths, "specs/design/design.cell") {
		t.Fatalf("validation paths = %v, want specs/design/design.cell", paths)
	}
	if got := r.tags(); len(got) != 0 {
		t.Errorf("tags = %v, want none", got)
	}
}

func TestSaveSpec_GateDesignInvalid(t *testing.T) {
	// The gate these assert is switched OFF (specGateDisabled), so it refuses
	// nothing and every assertion below would fail. Skipped by the SAME constant
	// rather than deleted or weakened: flipping the constant back re-arms the gate
	// and its tests together, which is what stops the gate returning unguarded.
	if specGateDisabled {
		t.Skip("whole-spec gate disabled (specGateDisabled)")
	}
	t.Parallel()
	seed := validSpecSeed()
	// design.json missing the required "description" → SCHEMA_VIOLATION.
	seed["specs/design/components/svc/design.json"] = `{"name":"svc","type":"service","version":"1.0.0",` +
		`"language":"go","buildpack":"go","appPath":".","entrypoint":"main.go","exposure":"internet","dependencies":[]}`
	r := newRig(t, seed)

	_, err := r.svc.SaveSpec(context.Background(), r.org, r.proj, SaveRequest{})
	paths := specErrPaths(t, err)
	if !containsPath(paths, "specs/design/components/svc/design.json") {
		t.Fatalf("validation paths = %v, want the invalid design.json (design-prefixed)", paths)
	}
	if got := r.tags(); len(got) != 0 {
		t.Errorf("tags = %v, want none (malformed design never tagged)", got)
	}
}

func TestSaveSpec_GateAggregatesRequirementsAndDesign(t *testing.T) {
	// The gate these assert is switched OFF (specGateDisabled), so it refuses
	// nothing and every assertion below would fail. Skipped by the SAME constant
	// rather than deleted or weakened: flipping the constant back re-arms the gate
	// and its tests together, which is what stops the gate returning unguarded.
	if specGateDisabled {
		t.Skip("whole-spec gate disabled (specGateDisabled)")
	}
	t.Parallel()
	// Both gates fail at once → ONE SpecValidationError carrying both entries.
	r := newRig(t, map[string]string{
		"specs/design/components/svc/design.json": validComponentDesignJSON("svc"), // no root design.cell
	})

	_, err := r.svc.SaveSpec(context.Background(), r.org, r.proj, SaveRequest{})
	paths := specErrPaths(t, err)
	if !containsPath(paths, "specs/requirements/prd.md") || !containsPath(paths, "specs/design/design.cell") {
		t.Fatalf("validation paths = %v, want both the requirements and design entries", paths)
	}
}

func TestSaveSpec_Unchanged(t *testing.T) {
	t.Parallel()
	r := newRig(t, validSpecSeed())
	ctx := context.Background()
	if _, err := r.svc.SaveSpec(ctx, r.org, r.proj, SaveRequest{}); err != nil {
		t.Fatalf("first save: %v", err)
	}
	// A non-specs/ change must not count as spec movement.
	r.seed(map[string]string{"README.md": "docs only\n"}, "readme edit")

	res, err := r.svc.SaveSpec(ctx, r.org, r.proj, SaveRequest{})
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if res.Status != "unchanged" || res.Tag != "v1" {
		t.Fatalf("result = %+v, want unchanged/v1/1", res)
	}
	if got := r.tags(); len(got) != 1 {
		t.Errorf("tags = %v, want a single v1 (no duplicate tag)", got)
	}
}

// The semantic fix over SaveRequirements: a design-only edit MUST cut a new
// spec version (the requirements-only unchanged check would have reused v1,
// pointing at the pre-edit commit).
func TestSaveSpec_DesignOnlyChange_CutsNewTag(t *testing.T) {
	t.Parallel()
	r := newRig(t, validSpecSeed())
	ctx := context.Background()
	if _, err := r.svc.SaveSpec(ctx, r.org, r.proj, SaveRequest{}); err != nil {
		t.Fatalf("first save: %v", err)
	}
	r.seed(map[string]string{"specs/design/domain-model.md": "# Domain model — revised\n"}, "design-only edit")
	head := r.headSHA()

	res, err := r.svc.SaveSpec(ctx, r.org, r.proj, SaveRequest{})
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if res.Status != "approved" || res.Tag != "v2" {
		t.Fatalf("result = %+v, want approved/v2/2 (design-only change bumps the spec version)", res)
	}
	if res.CommitHash != head {
		t.Errorf("v2 points at %s, want the design-edit commit %s", res.CommitHash, head)
	}
}

// Legacy `v<N>-<M>` design-revision tags are not part of the spec sequence:
// they neither satisfy the unchanged check nor advance the next version.
func TestSaveSpec_LegacyDesignTagsExcluded(t *testing.T) {
	t.Parallel()
	r := newRig(t, validSpecSeed())
	r.tag("v1", specTagSubject+"v1")
	r.tag("v1-1", "legacy design rev")
	r.tag("v1-2", "legacy design rev")
	r.seed(map[string]string{"specs/requirements/prd.md": "# PRD v2\n\n## User Stories\n\n1. As a user, I want the thing, so that value.\n"}, "spec edit")

	res, err := r.svc.SaveSpec(context.Background(), r.org, r.proj, SaveRequest{})
	if err != nil {
		t.Fatalf("SaveSpec: %v", err)
	}
	if res.Tag != "v2" {
		t.Fatalf("result = %+v, want v2/2 (legacy design tags excluded from the sequence)", res)
	}
}

func TestSaveSpec_AtProvidedCommit_TagsThatCommit(t *testing.T) {
	t.Parallel()
	r := newRig(t, validSpecSeed())
	applied := r.headSHA()
	// main moves on after the apply — the save must still pin the caller's commit.
	r.seed(map[string]string{"specs/requirements/prd.md": "newer draft\n"}, "later edit")

	res, err := r.svc.SaveSpec(context.Background(), r.org, r.proj, SaveRequest{CommitSHA: applied})
	if err != nil {
		t.Fatalf("SaveSpec: %v", err)
	}
	if res.Status != "approved" || res.Tag != "v1" {
		t.Fatalf("result = %+v, want approved/v1", res)
	}
	if res.CommitHash != applied {
		t.Errorf("tag points at %s, want the provided commit %s (not HEAD %s)",
			res.CommitHash, applied, r.headSHA())
	}
}

func TestBuildScopeAtTag(t *testing.T) {
	t.Parallel()
	seed := validSpecSeed()
	seed["specs/requirements/prd.md"] = "# PRD\n\n## User Stories\n\n1. As a user, I want A, so that a.\n2. As a user, I want B, so that b.\n7. As a user, I want S, so that s.\n"
	seed["specs/design/design.cell"] = "component svc service\ncomponent notify-svc service\n"
	// svc claims stories 1, 2 and a junk number the PRD never defines;
	// notify-svc claims 7. The scope reads the claims from each design.json.
	seed["specs/design/components/svc/design.json"] = `{"name":"svc","type":"service","version":"1.0.0","language":"go",` +
		`"buildpack":"go","appPath":".","entrypoint":"main.go","exposure":"internet",` +
		`"stories":[1,2,9],"dependencies":[],"description":"a service"}`
	seed["specs/design/components/notify-svc/design.json"] = `{"name":"notify-svc","type":"service","version":"1.0.0","language":"go",` +
		`"buildpack":"go","appPath":".","entrypoint":"main.go","exposure":"internet",` +
		`"stories":[7],"dependencies":[],"description":"a service"}`
	r := newRig(t, seed)
	if _, err := r.svc.SaveSpec(context.Background(), r.org, r.proj, SaveRequest{}); err != nil {
		t.Fatalf("save: %v", err)
	}

	scope, err := r.svc.BuildScopeAtTag(context.Background(), r.org, r.proj, "v1")
	if err != nil {
		t.Fatalf("BuildScopeAtTag: %v", err)
	}
	if scope.Tag != "v1" {
		t.Fatalf("scope identity = %+v", scope)
	}
	// The milestone a scope claims is named after the version.
	if scope.MilestoneTitle() != "v1" {
		t.Errorf("MilestoneTitle() = %q, want the tag", scope.MilestoneTitle())
	}
	if fmt.Sprint(scope.InScope) != "[1 2 7]" {
		t.Errorf("inScope = %v", scope.InScope)
	}
	if scope.StoryTitles[1] == "" || scope.StoryTitles[7] == "" {
		t.Errorf("story titles = %v", scope.StoryTitles)
	}
	// Claims are filtered to PRD stories (the junk 9 is dropped).
	if fmt.Sprint(scope.ComponentStories["svc"]) != "[1 2]" || fmt.Sprint(scope.ComponentStories["notify-svc"]) != "[7]" {
		t.Errorf("componentStories = %v", scope.ComponentStories)
	}
}

// -- the save's shared plumbing: the commit it pins, and the name it lands ----

func TestSaveSpec_InvalidCommitSHA(t *testing.T) {
	t.Parallel()
	r := newRig(t, validSpecSeed())
	_, err := r.svc.SaveSpec(context.Background(), r.org, r.proj, SaveRequest{CommitSHA: "not-a-sha!"})
	if !errors.Is(err, ErrArtifactPathInvalid) {
		t.Fatalf("err = %v, want ErrArtifactPathInvalid (malformed commit sha)", err)
	}
	if got := r.tags(); len(got) != 0 {
		t.Errorf("tags = %v, want none", got)
	}
}

// A well-formed but UNKNOWN pinned sha fails the gate read with the engine's
// ref-not-found: the pinned bundle read runs first, so no tag is ever attempted.
func TestSaveSpec_UnknownPinnedSha_RefNotFound(t *testing.T) {
	t.Parallel()
	r := newRig(t, validSpecSeed())
	_, err := r.svc.SaveSpec(context.Background(), r.org, r.proj,
		SaveRequest{CommitSHA: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"})
	if !errors.Is(err, sourcecontrol.ErrRefNotFound) {
		t.Fatalf("err = %v, want wrapped sourcecontrol.ErrRefNotFound", err)
	}
	if got := r.tags(); len(got) != 0 {
		t.Errorf("tags = %v, want none (unknown sha must never acquire a tag)", got)
	}
}

// The tag the save reports is the same object origin and the mirror resolve,
// and it is HEAD — a save commits nothing.
func TestSaveSpec_TagShaConsistency_OriginAndMirror(t *testing.T) {
	t.Parallel()
	r := newRig(t, validSpecSeed())

	res, err := r.svc.SaveSpec(context.Background(), r.org, r.proj, SaveRequest{})
	if err != nil {
		t.Fatalf("SaveSpec: %v", err)
	}
	if origin := r.originRevParse("v1^{commit}"); res.CommitHash != origin {
		t.Errorf("CommitHash %s != origin peeled v1 %s", res.CommitHash, origin)
	}
	if mirror := r.mirrorRevParse("v1^{commit}"); res.CommitHash != mirror {
		t.Errorf("CommitHash %s != mirror peeled v1 %s", res.CommitHash, mirror)
	}
	if head := r.headSHA(); res.CommitHash != head {
		t.Errorf("CommitHash %s != origin tip %s (save must tag HEAD)", res.CommitHash, head)
	}
}

// A SUGGESTED name may be re-suggested past a taken one: the suggestion is the
// platform's, so stepping it costs the caller nothing. (A name the USER typed
// is terminal instead — delivery/build asserts that 409.)
func TestSaveSpec_SuggestedNameCollision_RecomputesToNextName(t *testing.T) {
	t.Parallel()
	r := newRig(t, validSpecSeed())
	// v1 already claimed externally, and the draft has since moved on → the save
	// wants a tag but must skip the taken v1 and land v2.
	r.tag("v1", specTagSubject+"v1")
	r.seed(map[string]string{
		"specs/requirements/prd.md": "# PRD\n\n## User Stories\n\n1. As a user, I want the thing, so that value.\n\nmoved on\n",
	}, "draft edit")

	res, err := r.svc.SaveSpec(context.Background(), r.org, r.proj, SaveRequest{})
	if err != nil {
		t.Fatalf("SaveSpec: %v", err)
	}
	if res.Tag != "v2" {
		t.Fatalf("result = %+v, want v2 (skip the taken v1)", res)
	}
	if tags := r.tags(); len(tags) != 2 || tags[0] != "v1" || tags[1] != "v2" {
		t.Errorf("tags = %v, want [v1 v2] (v1 preserved)", tags)
	}
}

// A true external-pusher collision in the window between the save's fresh
// tag-list read and its Tag push, forced via the harness BeforeTag hook: the
// engine's fetch+precheck surfaces ErrTagAlreadyExists, and the recompute loop
// must refresh the tag list and land v2.
func TestSaveSpec_SuggestedNameCollision_InWindowClaim(t *testing.T) {
	t.Parallel()
	r := newRig(t, validSpecSeed())

	var tagAttempts int32
	var once sync.Once
	r.ws.BeforeTag = func(sourcecontrol.TagSpec) {
		atomic.AddInt32(&tagAttempts, 1)
		once.Do(func() { r.tag("v1", specTagSubject+"v1") })
	}

	res, err := r.svc.SaveSpec(context.Background(), r.org, r.proj, SaveRequest{})
	if err != nil {
		t.Fatalf("SaveSpec: %v", err)
	}
	if res.Tag != "v2" {
		t.Fatalf("result = %+v, want v2 (retry past the claimed v1)", res)
	}
	if n := atomic.LoadInt32(&tagAttempts); n < 2 {
		t.Errorf("Tag attempts = %d, want ≥2 (first collides, recompute lands v2)", n)
	}
	if tags := r.tags(); len(tags) != 2 || tags[0] != "v1" || tags[1] != "v2" {
		t.Errorf("tags = %v, want [v1 v2] (external v1 preserved)", tags)
	}
}

// The exit-gate concurrency pin: two goroutines race the SAME suggested name
// (both start from an empty tag list, so both compute `v1`). One lands it; the
// other collides, re-lists, recomputes and lands `v2` — both succeed, and both
// tags point at the pinned commit.
//
// Only a SUGGESTED name may be recomputed like this (resuggest=true). A name
// the user typed is terminal on collision, which delivery/build pins as a 409.
func TestCreateVersionTag_ConcurrentSameSuggestion_LoserRecomputesToNext(t *testing.T) {
	t.Parallel()
	r := newRig(t, validSpecSeed())
	s := r.svc.(*artifactService)
	ref := r.workspaceRef()
	head := r.headSHA()

	type outcome struct {
		name string
		err  error
	}
	results := make([]outcome, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tags := []sourcecontrol.TagInfo{} // both believe no tags exist yet
			name := suggestedVersionName(tags)
			err := s.createVersionTag(context.Background(), ref, &tags, &name,
				"race", head, true)
			results[i] = outcome{name: name, err: err}
		}(i)
	}
	wg.Wait()

	for i, res := range results {
		if res.err != nil {
			t.Fatalf("goroutine %d: %v", i, res.err)
		}
	}
	got := map[string]bool{results[0].name: true, results[1].name: true}
	if !got["v1"] || !got["v2"] {
		t.Fatalf("tag names = %s/%s, want exactly {v1, v2}", results[0].name, results[1].name)
	}
	for _, tag := range []string{"v1", "v2"} {
		if peeled := r.originRevParse(tag + "^{commit}"); peeled != head {
			t.Errorf("%s peels to %s on origin, want the pinned commit %s", tag, peeled, head)
		}
	}
}

// The story-scope read applies the same name rule as every other read at a
// version: its argument becomes `tags/<name>`, so a name no version could carry
// is refused before it reaches ref resolution.
func TestBuildScopeAtTag_RefusesANameNoVersionCouldCarry(t *testing.T) {
	t.Parallel()
	r := newRig(t, validSpecSeed())
	ctx := context.Background()
	for _, name := range []string{"../heads/main", "has space", "", ".hidden", "ends.lock", "a..b"} {
		if _, err := r.svc.BuildScopeAtTag(ctx, r.org, r.proj, name); !errors.Is(err, ErrInvalidVersionTag) {
			t.Errorf("BuildScopeAtTag(%q) err = %v, want ErrInvalidVersionTag", name, err)
		}
	}
}
