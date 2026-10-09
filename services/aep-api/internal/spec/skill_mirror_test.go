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

// SyncProjectSkills resolves the skills and project repositories and the
// component pins, then hands the copy to the pod's mirror-skills. The copy
// rule itself (enabled coding skills plus pins, prune the rest) is the pod's
// and is pinned by its tests (ae-studio-tools internal/skills).

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

const testProjectID = "proj1"
const testProjectRepoName = "project-repo"

// provisionProjectRepo arranges the project's git repo row and repository,
// the way project creation already has by the time SyncProjectSkills is ever
// called (its call site sits AFTER WriteDescriptor, when the repo certainly
// exists).
func provisionProjectRepo(t *testing.T, host *testGitHost, orgID string) {
	t.Helper()
	if _, err := host.EnsureBareRepo(context.Background(), orgID, testProjectID, testProjectRepoName); err != nil {
		t.Fatalf("provision project repo: %v", err)
	}
}

func testSkillsRef(orgID string) sourcecontrol.RepoRef {
	return sourcecontrol.RepoRef{Org: orgID, Owner: "test-org", Repo: SkillsRepoName, DefaultBranch: "main"}
}

func testProjectRef(orgID string) sourcecontrol.RepoRef {
	return sourcecontrol.RepoRef{Org: orgID, Owner: "test-org", Repo: testProjectRepoName, DefaultBranch: "main"}
}

// mirrorCalls answers the mirror-skills calls the pod saw.
func mirrorCalls(f *aestudiotest.Fake) []aestudiotest.Call {
	var out []aestudiotest.Call
	for _, c := range f.Calls() {
		if c.Op == aestudiotest.OpMirrorSkills {
			out = append(out, c)
		}
	}
	return out
}

// The mirror names the project repository, the org's skills repository and
// the union of every component's skillsPinned, sorted.
func TestSyncProjectSkills_MirrorsThroughThePodWithThePins(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	orgID := "org1"
	if _, err := svc.List(ctx, orgID); err != nil { // provision + seed the skills repo
		t.Fatalf("seed skills repo: %v", err)
	}
	provisionProjectRepo(t, host, orgID)
	host.originFor(orgID, testProjectID).Seed(t, map[string]string{
		"specs/design/components/orders/design.json":   `{"name":"orders","type":"service","dependencies":[],"skillsPinned":["design-skill","go"]}`,
		"specs/design/components/web/design.json":      `{"name":"web","type":"webapp","dependencies":[],"skillsPinned":["design-skill","react"]}`,
		"specs/design/components/broken/design.json":   `{not json`,
		"specs/design/components/a/nested/design.json": `{"name":"nested","skillsPinned":["ignored"]}`,
	}, "design")

	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); err != nil {
		t.Fatalf("SyncProjectSkills: %v", err)
	}
	calls := mirrorCalls(host.pod)
	if len(calls) != 1 {
		t.Fatalf("mirror calls = %d, want 1", len(calls))
	}
	c := calls[0]
	if c.Ref != testProjectRef(orgID) || c.Skills != testSkillsRef(orgID) {
		t.Fatalf("mirror(%+v, %+v), want the project and the skills repository", c.Ref, c.Skills)
	}
	if want := []string{"design-skill", "go", "react"}; !reflect.DeepEqual(c.Pinned, want) {
		t.Fatalf("pinned = %v, want %v (the union, sorted; malformed and nested files skipped)", c.Pinned, want)
	}
}

// Pins come only from components/<name>/design.json: a stray top-level
// components/x.json is never parsed as a component design. The Fake enforces
// the pod's ext pattern, so a path-suffix sent as an ext fails here as the pod
// answers 400 validation_failed.
func TestSyncProjectSkills_PinsComeOnlyFromComponentDesignJSON(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	orgID := "org1"
	if _, err := svc.List(ctx, orgID); err != nil {
		t.Fatalf("seed skills repo: %v", err)
	}
	provisionProjectRepo(t, host, orgID)
	host.originFor(orgID, testProjectID).Seed(t, map[string]string{
		"specs/design/components/api/design.json": `{"name":"api","type":"service","dependencies":[],"skillsPinned":["go"]}`,
		"specs/design/components/x.json":          `{"name":"x","type":"service","dependencies":[],"skillsPinned":["stray"]}`,
	}, "design")

	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); err != nil {
		t.Fatalf("SyncProjectSkills: %v", err)
	}
	calls := mirrorCalls(host.pod)
	if len(calls) != 1 {
		t.Fatalf("mirror calls = %d, want 1", len(calls))
	}
	if want := []string{"go"}; !reflect.DeepEqual(calls[0].Pinned, want) {
		t.Fatalf("pinned = %v, want %v (design.json only)", calls[0].Pinned, want)
	}
}

// No design yet (a brand-new project) mirrors with zero pins, not an error —
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
	if calls := mirrorCalls(host.pod); len(calls) != 1 || len(calls[0].Pinned) != 0 {
		t.Fatalf("mirror calls = %+v, want one with no pins", calls)
	}
}

// An org with no skills repository yet gets one (created and seeded, as on
// any skills read) before the mirror: the pod reads the library from it.
func TestSyncProjectSkills_ProvisionsAMissingSkillsRepo(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	orgID := "org1"
	provisionProjectRepo(t, host, orgID)

	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); err != nil {
		t.Fatalf("SyncProjectSkills: %v", err)
	}
	if _, err := host.GetRepo(ctx, orgID, SkillsRepoProject); err != nil {
		t.Fatalf("skills repository row: %v", err)
	}
	if got := host.readAtHead(orgID, skillRepoPath("go")); got == "" {
		t.Fatal("the new skills repository was not seeded")
	}
	if calls := mirrorCalls(host.pod); len(calls) != 1 || calls[0].Skills != testSkillsRef(orgID) {
		t.Fatalf("mirror calls = %+v, want one naming the new skills repository", calls)
	}
}

// A failing pin read aborts before the mirror: mirroring without the pins
// would prune a pinned skill a build needs.
func TestSyncProjectSkills_FailingPinReadMirrorsNothing(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	orgID := "org1"
	if _, err := svc.List(ctx, orgID); err != nil {
		t.Fatalf("seed skills repo: %v", err)
	}
	provisionProjectRepo(t, host, orgID)
	host.pod.FailOp(aestudiotest.OpReadBundle, sourcecontrol.ErrAEStudioUnavailable)

	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); !errors.Is(err, sourcecontrol.ErrAEStudioUnavailable) {
		t.Fatalf("err = %v, want the pin read's failure", err)
	}
	if calls := mirrorCalls(host.pod); len(calls) != 0 {
		t.Fatalf("mirror ran after a failed pin read: %+v", calls)
	}
}

// The pod's mirror failure is the caller's error (callers log and continue).
func TestSyncProjectSkills_MirrorFailureSurfaces(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	orgID := "org1"
	if _, err := svc.List(ctx, orgID); err != nil {
		t.Fatalf("seed skills repo: %v", err)
	}
	provisionProjectRepo(t, host, orgID)
	host.pod.FailOp(aestudiotest.OpMirrorSkills, sourcecontrol.ErrCommitConflict)

	if err := svc.SyncProjectSkills(ctx, orgID, testProjectID); !errors.Is(err, sourcecontrol.ErrCommitConflict) {
		t.Fatalf("err = %v, want the mirror's failure", err)
	}
}

// A service with no mirror port refuses rather than panics.
func TestSyncProjectSkills_UnconfiguredMirrorIsAnError(t *testing.T) {
	t.Parallel()
	host := newTestGitHost(t)
	svc := NewSkillService(host.git(), nil, host, testLibraryFS(t))
	if err := svc.SyncProjectSkills(context.Background(), "org1", testProjectID); err == nil {
		t.Fatal("SyncProjectSkills without a mirror port = nil error")
	}
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
func TestRealLibrary_RunnerSkillsAreCodingAudience(t *testing.T) {
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
	// agent-browser is not in RequiredSkills (it loads on demand), but it is still
	// the coding agent's and still has to reach the mirror.
	if got := byName["agent-browser"].Audience; len(got) != 1 || got[0] != SkillAudienceCoding {
		t.Errorf("agent-browser audience = %v, want exactly [coding]", got)
	}
}
