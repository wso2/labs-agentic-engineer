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

// Save = hard semantic gate → annotated tag at HEAD (no commit). These run
// over the real gitfs Workspace engine, so the tag lands as a genuine
// annotated tag object pushed to the bare origin.

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// validComponentDesignJSON is a component design.json that satisfies the
// published schema (docs/design/agents-generation-migration.md §8) for a
// component whose directory name is `name`.
func validComponentDesignJSON(name string) string {
	return `{"name":"` + name + `","type":"service","version":"1.0.0","language":"go",` +
		`"buildpack":"go","appPath":".","entrypoint":"main.go","exposure":"internet",` +
		`"stories":[1],"dependencies":[],"description":"a service"}`
}

func TestSaveRequirements_TagsAtHead(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "v1 body\n"})
	head := r.headSHA()

	res, err := r.svc.SaveRequirements(context.Background(), r.org, r.proj, SaveRequest{Message: "cut v1"})
	if err != nil {
		t.Fatalf("SaveRequirements: %v", err)
	}
	if res.Status != "approved" || res.Tag != "v1" || res.Version != 1 {
		t.Fatalf("result = %+v, want approved/v1/1", res)
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

func TestSaveRequirements_Unchanged(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "v1 body\n"})
	ctx := context.Background()
	if _, err := r.svc.SaveRequirements(ctx, r.org, r.proj, SaveRequest{}); err != nil {
		t.Fatalf("first save: %v", err)
	}
	// HEAD still equals v1's content → re-save is a no-op.
	res, err := r.svc.SaveRequirements(ctx, r.org, r.proj, SaveRequest{})
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if res.Status != "unchanged" || res.Tag != "v1" {
		t.Fatalf("result = %+v, want unchanged/v1", res)
	}
	if got := r.tags(); len(got) != 1 {
		t.Errorf("tags = %v, want a single v1 (no duplicate tag)", got)
	}
}

func TestSaveRequirements_GateMissingMain(t *testing.T) {
	t.Parallel()
	// A requirements dir with only a sibling doc, no requirements.md → gate fails.
	r := newRig(t, map[string]string{"specs/requirements/functional.md": "stuff\n"})
	_, err := r.svc.SaveRequirements(context.Background(), r.org, r.proj, SaveRequest{})
	if !errors.Is(err, ErrArtifactPathInvalid) {
		t.Fatalf("err = %v, want ErrArtifactPathInvalid (requirements.md missing)", err)
	}
	if got := r.tags(); len(got) != 0 {
		t.Errorf("tags = %v, want none (nothing may be tagged when the gate fails)", got)
	}
}

// ----- Save at a caller-provided commit (the publish flow's apply→save) -----
//
// A publish is `files/apply` (commit to main) followed by save (tag). Re-reading
// `heads/main` between the two loses to GitHub's read-after-write lag — the ref
// read can return the pre-apply commit seconds after the apply succeeded (seen
// live 2026-07-05: apply c6659d08 at 14:29:59, save's ref read a882aba2 at
// 14:30:00). The contract: when the caller names the commit it just applied,
// the save gates AND tags at that exact commit — HEAD is never consulted.

func TestSaveRequirements_AtProvidedCommit_TagsThatCommit(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "published body\n"})
	applied := r.headSHA()
	// main moves on after the apply (another writer, or a stale ref would
	// resolve elsewhere) — the save must still pin the caller's commit.
	r.seed(map[string]string{"specs/requirements/prd.md": "newer draft\n"}, "later edit")

	res, err := r.svc.SaveRequirements(context.Background(), r.org, r.proj, SaveRequest{CommitSHA: applied})
	if err != nil {
		t.Fatalf("SaveRequirements: %v", err)
	}
	if res.Status != "approved" || res.Tag != "v1" {
		t.Fatalf("result = %+v, want approved/v1", res)
	}
	if res.CommitHash != applied {
		t.Errorf("tag points at %s, want the provided commit %s (not HEAD %s)",
			res.CommitHash, applied, r.headSHA())
	}
}

func TestSaveRequirements_AtProvidedCommit_GateReadsThatCommit(t *testing.T) {
	t.Parallel()
	// The provided commit has NO requirements.md; HEAD does. The gate must fail —
	// proving both gate and tag operate on the provided commit, not on any ref
	// read that could race.
	r := newRig(t, map[string]string{"specs/requirements/functional.md": "no main doc\n"})
	early := r.headSHA()
	r.seed(map[string]string{"specs/requirements/prd.md": "arrived later\n"}, "add main doc")

	_, err := r.svc.SaveRequirements(context.Background(), r.org, r.proj, SaveRequest{CommitSHA: early})
	if !errors.Is(err, ErrArtifactPathInvalid) {
		t.Fatalf("err = %v, want ErrArtifactPathInvalid (gate at the provided commit)", err)
	}
	if got := r.tags(); len(got) != 0 {
		t.Errorf("tags = %v, want none", got)
	}
}

func TestSaveRequirements_InvalidCommitSHA(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "body\n"})
	_, err := r.svc.SaveRequirements(context.Background(), r.org, r.proj, SaveRequest{CommitSHA: "not-a-sha!"})
	if !errors.Is(err, ErrArtifactPathInvalid) {
		t.Fatalf("err = %v, want ErrArtifactPathInvalid (malformed commit sha)", err)
	}
	if got := r.tags(); len(got) != 0 {
		t.Errorf("tags = %v, want none", got)
	}
}

func TestSaveDesign_AtProvidedCommit_TagsThatCommit(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "spec\n"})
	ctx := context.Background()
	if _, err := r.svc.SaveRequirements(ctx, r.org, r.proj, SaveRequest{}); err != nil {
		t.Fatalf("save requirements: %v", err)
	}
	applied := r.seed(map[string]string{
		"specs/design/design.cell":                "# System\n",
		"specs/design/components/svc/design.md":   "---\ntype: service\n---\n# svc\n",
		"specs/design/components/svc/design.json": validComponentDesignJSON("svc"),
	}, "apply design")
	// main moves on; the save must pin the applied commit regardless.
	r.seed(map[string]string{"specs/requirements/notes.md": "unrelated\n"}, "later edit")

	res, err := r.svc.SaveDesign(ctx, r.org, r.proj, SaveRequest{CommitSHA: applied})
	if err != nil {
		t.Fatalf("SaveDesign: %v", err)
	}
	if res.Status != "approved" || res.Tag != "v1-1" {
		t.Fatalf("result = %+v, want approved/v1-1", res)
	}
	if res.CommitHash != applied {
		t.Errorf("tag points at %s, want the provided commit %s", res.CommitHash, applied)
	}
}

func TestSaveRequirements_TagCollision_RecomputesToNextName(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "body\n"})
	// v1 already claimed externally at an earlier state, and the draft has since
	// moved on → save wants a new tag but must skip the taken v1 and land v2.
	r.tag("v1", "external v1")
	r.seed(map[string]string{"specs/requirements/prd.md": "moved on\n"}, "draft edit")

	res, err := r.svc.SaveRequirements(context.Background(), r.org, r.proj, SaveRequest{})
	if err != nil {
		t.Fatalf("SaveRequirements: %v", err)
	}
	if res.Tag != "v2" || res.Version != 2 {
		t.Fatalf("result = %+v, want v2/2 (skip the taken v1)", res)
	}
	tags := r.tags()
	if len(tags) != 2 || tags[0] != "v1" || tags[1] != "v2" {
		t.Errorf("tags = %v, want [v1 v2] (v1 preserved)", tags)
	}
}

// TestSaveRequirements_TagCollision_InWindowClaim forces a true
// external-pusher collision in the window between the save's fresh tag-list
// read and its Tag push, via the harness BeforeTag hook: the engine's own
// fetch+precheck (or origin's push rejection) surfaces ErrTagAlreadyExists,
// and the collision-recompute loop must refresh the tag list and land v2.
func TestSaveRequirements_TagCollision_InWindowClaim(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "body\n"})

	var tagAttempts int32
	var once sync.Once
	r.ws.BeforeTag = func(sourcecontrol.TagSpec) {
		atomic.AddInt32(&tagAttempts, 1)
		once.Do(func() { r.tag("v1", "external claim in the race window") })
	}

	res, err := r.svc.SaveRequirements(context.Background(), r.org, r.proj, SaveRequest{})
	if err != nil {
		t.Fatalf("SaveRequirements: %v", err)
	}
	if res.Tag != "v2" || res.Version != 2 {
		t.Fatalf("result = %+v, want v2/2 (retry past the claimed v1)", res)
	}
	if n := atomic.LoadInt32(&tagAttempts); n < 2 {
		t.Errorf("Tag attempts = %d, want ≥2 (first collides, recompute lands v2)", n)
	}
	tags := r.tags()
	if len(tags) != 2 || tags[0] != "v1" || tags[1] != "v2" {
		t.Errorf("tags = %v, want [v1 v2] (external v1 preserved)", tags)
	}
}

// The exit-gate concurrency pin: two goroutines race the SAME next tag name
// (both start from an empty tag list, both compute v1). One lands v1; the
// other collides, re-lists, recomputes, and lands v2 — both succeed, both
// tags point at the pinned commit.
func TestCreateAnnotatedTag_ConcurrentSameName_LoserRecomputesToNext(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "body\n"})
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
			var n int
			var name string
			err := s.createAnnotatedTag(context.Background(), ref, &tags, &n, &name,
				"Requirements (race)", head, 0, "requirements")
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

// SHA consistency (design C8): the CommitHash a save reports == the peeled tag
// commit on the ORIGIN == the mirror's view of the same tag.
func TestSaveRequirements_TagShaConsistency_OriginAndMirror(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "body\n"})

	res, err := r.svc.SaveRequirements(context.Background(), r.org, r.proj, SaveRequest{})
	if err != nil {
		t.Fatalf("SaveRequirements: %v", err)
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

// A well-formed but UNKNOWN pinned sha fails the gate read with the engine's
// ref-not-found — same surface as before this phase (the pinned bundle read
// runs first; no tag is ever attempted).
func TestSaveRequirements_UnknownPinnedSha_RefNotFound(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "body\n"})
	_, err := r.svc.SaveRequirements(context.Background(), r.org, r.proj,
		SaveRequest{CommitSHA: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"})
	if !errors.Is(err, sourcecontrol.ErrRefNotFound) {
		t.Fatalf("err = %v, want wrapped sourcecontrol.ErrRefNotFound", err)
	}
	if got := r.tags(); len(got) != 0 {
		t.Errorf("tags = %v, want none (unknown sha must never acquire a tag)", got)
	}
}

func TestSaveDesign_TagsAtHead(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "spec\n"})
	ctx := context.Background()
	// A requirements baseline must exist for a design tag.
	if _, err := r.svc.SaveRequirements(ctx, r.org, r.proj, SaveRequest{}); err != nil {
		t.Fatalf("save requirements: %v", err)
	}
	// Add a valid design bundle to main.
	r.seed(map[string]string{
		"specs/design/design.cell":                "# System\n",
		"specs/design/components/svc/design.md":   "---\ntype: service\n---\n# svc\n",
		"specs/design/components/svc/design.json": validComponentDesignJSON("svc"),
	}, "add design")
	head := r.headSHA()

	res, err := r.svc.SaveDesign(ctx, r.org, r.proj, SaveRequest{})
	if err != nil {
		t.Fatalf("SaveDesign: %v", err)
	}
	if res.Status != "approved" || res.Tag != "v1-1" || res.RequirementsVersion != 1 || res.DesignRevision != 1 {
		t.Fatalf("result = %+v, want approved/v1-1/1/1", res)
	}
	if res.CommitHash != head || r.headSHA() != head {
		t.Errorf("save must tag HEAD without committing (head=%s got=%s)", head, r.headSHA())
	}
}

func TestSaveDesign_NoRequirementsBaseline(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/design/design.cell": "# System\n"})
	_, err := r.svc.SaveDesign(context.Background(), r.org, r.proj, SaveRequest{})
	if !errors.Is(err, ErrNoRequirementsBaseline) {
		t.Fatalf("err = %v, want ErrNoRequirementsBaseline", err)
	}
}

func TestSaveDesign_GateMissingLayout(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "spec\n"})
	ctx := context.Background()
	if _, err := r.svc.SaveRequirements(ctx, r.org, r.proj, SaveRequest{}); err != nil {
		t.Fatalf("save requirements: %v", err)
	}
	// A component but no root design.cell → layout gate fails.
	r.seed(map[string]string{
		"specs/design/components/svc/design.json": validComponentDesignJSON("svc"),
	}, "design without root")
	_, err := r.svc.SaveDesign(ctx, r.org, r.proj, SaveRequest{})
	if !errors.Is(err, ErrArtifactPathInvalid) {
		t.Fatalf("err = %v, want ErrArtifactPathInvalid (missing design.cell)", err)
	}
}

func TestSaveDesign_GateSchemaViolation(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "spec\n"})
	ctx := context.Background()
	if _, err := r.svc.SaveRequirements(ctx, r.org, r.proj, SaveRequest{}); err != nil {
		t.Fatalf("save requirements: %v", err)
	}
	// design.json missing the required "description" → SCHEMA_VIOLATION. (dependencies
	// present + valid so the assemble step passes and the schema gate is what rejects it.)
	bad := `{"name":"svc","type":"service","version":"1.0.0","language":"go",` +
		`"buildpack":"go","appPath":".","entrypoint":"main.go","exposure":"internet","dependencies":[]}`
	r.seed(map[string]string{
		"specs/design/design.cell":                "# System\n",
		"specs/design/components/svc/design.json": bad,
	}, "bad design.json")

	_, err := r.svc.SaveDesign(ctx, r.org, r.proj, SaveRequest{})
	var ve *DesignValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want *DesignValidationError", err)
	}
	if len(ve.Files) == 0 || ve.Files[0].Code != "SCHEMA_VIOLATION" {
		t.Fatalf("validation files = %+v, want a SCHEMA_VIOLATION on the design.json", ve.Files)
	}
	if got := r.tags(); len(got) != 1 { // only the v1 requirements tag; no design tag
		t.Errorf("tags = %v, want just the requirements v1 (malformed design never tagged)", got)
	}
}

func TestSaveDesign_GateNameMismatch(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "spec\n"})
	ctx := context.Background()
	if _, err := r.svc.SaveRequirements(ctx, r.org, r.proj, SaveRequest{}); err != nil {
		t.Fatalf("save requirements: %v", err)
	}
	// design.json valid against the schema but name != component directory.
	r.seed(map[string]string{
		"specs/design/design.cell":                "# System\n",
		"specs/design/components/svc/design.json": validComponentDesignJSON("other"),
	}, "name mismatch")

	_, err := r.svc.SaveDesign(ctx, r.org, r.proj, SaveRequest{})
	var ve *DesignValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want *DesignValidationError (name != dir)", err)
	}
}

func TestSaveDesign_GateBrokenOpenAPI(t *testing.T) {
	t.Parallel()
	r := newRig(t, map[string]string{"specs/requirements/prd.md": "spec\n"})
	ctx := context.Background()
	if _, err := r.svc.SaveRequirements(ctx, r.org, r.proj, SaveRequest{}); err != nil {
		t.Fatalf("save requirements: %v", err)
	}
	r.seed(map[string]string{
		"specs/design/design.cell":                 "# System\n",
		"specs/design/components/svc/openapi.yaml": "this: : : not valid yaml: [\n",
	}, "broken openapi")

	_, err := r.svc.SaveDesign(ctx, r.org, r.proj, SaveRequest{})
	var ve *DesignValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want *DesignValidationError (broken openapi)", err)
	}
	if ve.Files[0].Code != codeInvalidOpenAPI {
		t.Errorf("code = %s, want %s", ve.Files[0].Code, codeInvalidOpenAPI)
	}
}
