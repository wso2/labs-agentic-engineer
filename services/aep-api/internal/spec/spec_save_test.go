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
// in-memory AE Studio pod.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/platform/reqspec"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// validSpecSeed is a buildable spec: a feature file with a story, a
// design.cell declaring the components, and a valid design bundle (root + one
// enriched component claiming the story, with its type artifact) — everything
// the layout gates AND the build gate (#369) demand.
func validSpecSeed() map[string]string {
	return map[string]string{
		"specs/requirements/prd.md":                "# PRD\n\n## Features\n\n- F1 [Core](features/F1-core.md)\n",
		"specs/requirements/features/F1-core.md":   "# Core\n\n## User Stories\n\n- F1.1 As a user, I want the thing, so that value.\n",
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
	r.seed(map[string]string{"specs/requirements/prd.md": "# PRD v2\n\n## Features\n\n- F1 [Core](features/F1-core.md)\n"}, "spec edit")

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
	seed["specs/requirements/features/F1-core.md"] = "# Core\n\n## User Stories\n\n- F1.1 As a user, I want A, so that a.\n- F1.2 As a user, I want B, so that b.\n"
	seed["specs/requirements/features/F2-notify.md"] = "# Notify\n\n## User Stories\n\n- F2.1 As a user, I want S, so that s.\n"
	// A reference document is the user's source material, never a story source.
	seed["specs/requirements/references/brief.md"] = "- F3.1 As a user, I want R, so that r.\n"
	seed["specs/design/design.cell"] = "component svc service\ncomponent notify-svc service\n"
	// svc claims F1.2 and F1.1 (out of order); notify-svc claims F2.1. The
	// scope reads the claims from each design.json. (A claim of an ID the
	// requirements do not define is refused by the gate before any tag.)
	seed["specs/design/components/svc/design.json"] = `{"name":"svc","type":"service","version":"1.0.0","language":"go",` +
		`"buildpack":"go","appPath":".","entrypoint":"main.go","exposure":"internet",` +
		`"stories":["F1.2","F1.1"],"dependencies":[],"description":"a service"}`
	seed["specs/design/components/notify-svc/design.json"] = `{"name":"notify-svc","type":"service","version":"1.0.0","language":"go",` +
		`"buildpack":"go","appPath":".","entrypoint":"main.go","exposure":"internet",` +
		`"stories":["F2.1"],"dependencies":[],"description":"a service"}`
	// A declared service owes the same artifacts as svc, or the layout gate
	// refuses the save before any scope is read.
	seed["specs/design/components/notify-svc/design.md"] = "---\ntype: service\n---\n# notify-svc\n"
	seed["specs/design/components/notify-svc/openapi.yaml"] = "openapi: 3.0.3\n"
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
	if fmt.Sprint(scope.InScope) != "[F1.1 F1.2 F2.1]" {
		t.Errorf("inScope = %v", scope.InScope)
	}
	if scope.StoryTitles["F1.1"] != "As a user, I want A, so that a." || scope.StoryTitles["F2.1"] == "" {
		t.Errorf("story titles = %v", scope.StoryTitles)
	}
	// Claims are put in ID order.
	if fmt.Sprint(scope.ComponentStories["svc"]) != "[F1.1 F1.2]" || fmt.Sprint(scope.ComponentStories["notify-svc"]) != "[F2.1]" {
		t.Errorf("componentStories = %v", scope.ComponentStories)
	}
	// A version cut without a pick carries every feature with stories (B3).
	if fmt.Sprint(scope.Features) != "[{F1 Core []} {F2 Notify []}]" {
		t.Errorf("features = %v", scope.Features)
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

// A caller's commit sha must be the pod's shape (40 lowercase hex): an
// abbreviated or upper-case one is refused here, before any pod call.
func TestCommitSHA_OnlyAFullLowercaseShaReachesThePod(t *testing.T) {
	t.Parallel()
	f := aestudiotest.New()
	svc := NewArtifactService(memRepos(t, "default", "p", "https://github.com/acme/greeter"), f, f)
	ctx := context.Background()
	full := strings.Repeat("a", 40)
	for _, sha := range []string{full[:7], strings.ToUpper(full), full + "aa"} {
		if _, err := svc.SaveSpec(ctx, "default", "p", SaveRequest{CommitSHA: sha}); !errors.Is(err, ErrArtifactPathInvalid) {
			t.Errorf("SaveSpec(%q) err = %v, want ErrArtifactPathInvalid", sha, err)
		}
		if _, err := svc.GetDesignAtCommit(ctx, "default", "p", sha); !errors.Is(err, ErrArtifactPathInvalid) {
			t.Errorf("GetDesignAtCommit(%q) err = %v, want ErrArtifactPathInvalid", sha, err)
		}
	}
	if calls := f.Calls(); len(calls) != 0 {
		t.Fatalf("pod calls = %+v, want none", calls)
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

// The tag the save reports is the commit the tag resolves to, and it is HEAD —
// a save commits nothing.
func TestSaveSpec_TagShaConsistency(t *testing.T) {
	t.Parallel()
	r := newRig(t, validSpecSeed())

	res, err := r.svc.SaveSpec(context.Background(), r.org, r.proj, SaveRequest{})
	if err != nil {
		t.Fatalf("SaveSpec: %v", err)
	}
	if tagged := r.tagCommit("v1"); res.CommitHash != tagged {
		t.Errorf("CommitHash %s != v1's commit %s", res.CommitHash, tagged)
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
		"specs/requirements/prd.md": "# PRD\n\n## Features\n\n- F1 [Core](features/F1-core.md)\n\nmoved on\n",
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
// tag-list read and its Tag, forced via the pod's BeforeTag hook: the pod
// answers ErrTagAlreadyExists, and the recompute loop must refresh the tag
// list and land v2.
func TestSaveSpec_SuggestedNameCollision_InWindowClaim(t *testing.T) {
	t.Parallel()
	r := newRig(t, validSpecSeed())

	var tagAttempts int32
	var once sync.Once
	r.pod.BeforeTag(func(sourcecontrol.TagSpec) {
		atomic.AddInt32(&tagAttempts, 1)
		once.Do(func() {
			r.pod.BeforeTag(nil)
			r.tag("v1", specTagSubject+"v1")
			r.pod.BeforeTag(func(sourcecontrol.TagSpec) { atomic.AddInt32(&tagAttempts, 1) })
		})
	})

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
			err := s.createVersionTag(context.Background(), r.repoRef(), &tags, &name,
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
		if peeled := r.tagCommit(tag); peeled != head {
			t.Errorf("%s points at %s, want the pinned commit %s", tag, peeled, head)
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

// A design goes out of date per feature (E1): a change to one feature's
// words refuses the build naming that feature, and leaves every other
// feature's design standing.
func TestSaveSpec_DesignOutOfDatePerFeature(t *testing.T) {
	t.Parallel()
	seed := validSpecSeed()
	seed["specs/requirements/features/F2-notify.md"] = "# Notify\n\n## User Stories\n\n- F2.1 As a user, I want S, so that s.\n"
	seed["specs/design/components/svc/design.json"] = `{"name":"svc","type":"service","version":"1.0.0","language":"go",` +
		`"buildpack":"go","appPath":".","entrypoint":"main.go","exposure":"internet",` +
		`"stories":["F1.1","F2.1"],"dependencies":[],"description":"a service"}`
	r := newRig(t, seed)
	designedAt := r.headSHA()
	r.svc.SetDesignRunsResolver(func(context.Context, string, string) ([]DesignRun, error) {
		// One bare `/design` at the seed: it covered every designable feature.
		return []DesignRun{{BaseRef: designedAt}}, nil
	})

	// Confirming nothing, touching only the product page: no feature moved.
	r.seed(map[string]string{"specs/requirements/prd.md": "# PRD\n\nA new problem statement.\n"}, "frame edit")
	if _, err := r.svc.SaveSpec(context.Background(), r.org, r.proj, SaveRequest{}); err != nil {
		t.Fatalf("a product-page edit refused the build: %v", err)
	}

	r.seed(map[string]string{
		"specs/requirements/features/F2-notify.md": "# Notify\n\n## User Stories\n\n- F2.1 As a user, I want S by Slack, so that s.\n",
	}, "F2 edit")
	_, err := r.svc.SaveSpec(context.Background(), r.org, r.proj, SaveRequest{})
	var ve *SpecValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want the stale feature refused, got %v", err)
	}
	if len(ve.Files) != 1 || ve.Files[0].Code != codeFeatureNotBuildable || ve.Files[0].Path != "specs/requirements/features/F2-notify.md" {
		t.Fatalf("want only F2 out of date, got %+v", ve.Files)
	}

	// A later `/design F2` at the new commit designs F2 again; F1 keeps its
	// design from the first run.
	redesignedAt := r.headSHA()
	r.svc.SetDesignRunsResolver(func(context.Context, string, string) ([]DesignRun, error) {
		return []DesignRun{{BaseRef: redesignedAt, Features: []string{"F2"}}, {BaseRef: designedAt}}, nil
	})
	if _, err := r.svc.SaveSpec(context.Background(), r.org, r.proj, SaveRequest{}); err != nil {
		t.Fatalf("after F2's update the build was refused: %v", err)
	}
}

// TODO(main-sync Task 48, API-11): main's TestDesignedFeatures parsed a
// `/design F1 F2` command line (spec.DesignedFeatures, deleted with
// start_command.go). Under Q3 (1b) the pod reports the IDs and aep-api keeps
// them in agent_turns.Summary: test that reader (dedupe, F-pattern only, empty
// = nil = every designable feature) where it lands.

// A version is a selection (B1): a pick builds what it carries, records it in
// the tag, and the version's scope is exactly those stories. A stale feature
// the pick does not touch stands aside; a new pick on the same tree is a new
// version.
func TestSaveSpec_ABuildIsASelection(t *testing.T) {
	t.Parallel()
	seed := validSpecSeed()
	seed["specs/requirements/features/F2-notify.md"] = "# Notify\n\n## Purpose\n\nTells people.\n\nNeeds: F1.\n\n## User Stories\n\n- F2.1 As a user, I want S, so that s.\n"
	seed["specs/design/components/svc/design.json"] = `{"name":"svc","type":"service","version":"1.0.0","language":"go",` +
		`"buildpack":"go","appPath":".","entrypoint":"main.go","exposure":"internet",` +
		`"stories":["F1.1","F2.1"],"dependencies":[],"description":"a service"}`
	r := newRig(t, seed)
	ctx := context.Background()

	v1, err := r.svc.SaveSpec(ctx, r.org, r.proj, SaveRequest{Pick: &reqspec.Pick{Features: []string{"F1"}}})
	if err != nil {
		t.Fatalf("pick F1: %v", err)
	}
	scope, err := r.svc.BuildScopeAtTag(ctx, r.org, r.proj, v1.Tag)
	if err != nil || fmt.Sprint(scope.InScope) != "[F1.1]" {
		t.Fatalf("v1 scope = %v (%v), want F1's story only", scope.InScope, err)
	}

	// Same tree, a new pick: F2 needs F1, which v1 built, so F2 alone.
	v2, err := r.svc.SaveSpec(ctx, r.org, r.proj, SaveRequest{Pick: &reqspec.Pick{Features: []string{"F2"}}})
	if err != nil {
		t.Fatalf("pick F2: %v", err)
	}
	if v2.Tag == v1.Tag || v2.Status != SpecSaveApproved {
		t.Fatalf("a new pick on the same tree reused %s: %+v", v1.Tag, v2)
	}
	scope, _ = r.svc.BuildScopeAtTag(ctx, r.org, r.proj, v2.Tag)
	if fmt.Sprint(scope.InScope) != "[F2.1]" {
		t.Fatalf("v2 scope = %v, want F2's story only", scope.InScope)
	}

	// What each version built, oldest first, with each feature's lines as the
	// version's tag holds them (B5).
	versions, err := r.svc.ListVersions(ctx, r.org, r.proj)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 2 || versions[0].Name != v1.Tag || versions[1].Name != v2.Tag {
		t.Fatalf("versions = %+v, want %s then %s", versions, v1.Tag, v2.Tag)
	}
	if f := versions[1].Features; len(f) != 1 || f[0].ID != "F2" || f[0].Name != "Notify" ||
		fmt.Sprint(f[0].Lines) != "[{ Tells people.} {F2.1 F2.1 As a user, I want S, so that s.}]" {
		t.Errorf("v2 built %+v", f)
	}

	// What v2 validates (B4): everything built so far, and the version before
	// it to compare with.
	vs, ok, err := r.svc.ValidationScope(ctx, r.org, r.proj, v2.Tag)
	if err != nil || !ok || fmt.Sprint(vs.Features) != "[F1 F2]" || fmt.Sprint(vs.Built) != "[F2 Notify]" ||
		fmt.Sprint(vs.Earlier) != "["+v1.Tag+"]" {
		t.Errorf("ValidationScope(%s) = %+v, %v, %v", v2.Tag, vs, ok, err)
	}

	// The same pick again on the same tree is the same version.
	again, err := r.svc.SaveSpec(ctx, r.org, r.proj, SaveRequest{Pick: &reqspec.Pick{Features: []string{"F2"}}})
	if err != nil || again.Status != SpecSaveUnchanged || again.Tag != v2.Tag {
		t.Fatalf("the same pick again = %+v (%v), want %s unchanged", again, err, v2.Tag)
	}

	// An open dependency keeps its feature out; the pick that needs it is refused.
	_, err = r.svc.SaveSpec(ctx, r.org, r.proj, SaveRequest{
		Pick:    &reqspec.Pick{Features: []string{"F2"}},
		Blocked: map[string]string{"F2": "it waits on xero — no provider chosen yet"},
	})
	var ve *SpecValidationError
	if !errors.As(err, &ve) || len(ve.Files) != 1 || ve.Files[0].Code != codeFeatureNotBuildable ||
		ve.Files[0].Message != "F2 Notify: it waits on xero — no provider chosen yet" {
		t.Fatalf("blocked pick = %v", err)
	}
}

func TestScopeBodyRoundTrips(t *testing.T) {
	plan := reqspec.BuildPlan{Features: []string{"F1", "F2"}, ProductWide: []string{"P1"}, HeldBack: []string{"F2.4"}}
	got, ok := parseScope("Build\n\n" + scopeBody(plan))
	if !ok || !samePlan(got, plan) {
		t.Fatalf("parseScope = %+v, %v", got, ok)
	}
	if _, ok := parseScope("Build"); ok {
		t.Fatal("a body with no scope parsed as one")
	}
}
