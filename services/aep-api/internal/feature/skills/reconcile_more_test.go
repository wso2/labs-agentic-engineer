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
// skill, and the no-op. This file adds: content-diff overwrite (embedded
// content SHA ≠ the repo copy's), the purge of a retired built-in the embed no
// longer ships, the UpdatesAvailable rows (stale + absent), the embedded
// loaders for both kinds, and the EnsureProvisioned guards.
package skills

import (
	"context"
	"testing"
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
	emb, err := loadEmbeddedLibrary()
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
	t.Fatalf("skill %q not present in %v", name, keysOf(nameSet(skills)))
	return ""
}

func TestReconcile_OverwritesStaleBuiltin(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil { // seed at embed content
		t.Fatalf("seed: %v", err)
	}
	// Plant a `go` whose content differs from the embed, then reconcile — the
	// content-diff branch must overwrite it back to the embedded copy.
	host.writeAtHead("org1", skillRepoPath("go"), goBuiltinStale())

	n, err := svc.Reconcile(ctx, "org1")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n != 1 {
		t.Fatalf("Reconcile wrote %d, want 1 (only stale `go`)", n)
	}
	// After the overwrite the repo copy matches the embedded content byte-for-byte.
	got, _ := svc.List(ctx, "org1")
	if sha := contentSHAOf(t, got, "go"); sha != embeddedSkill(t, "go").ContentSHA {
		t.Fatalf("after overwrite go content SHA = %s, want the embedded copy's", sha)
	}
}

func TestReconcile_PurgesRetiredBuiltin(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// A built-in the embed no longer ships lingers in the repo — reconcile must
	// delete it, or it would keep getting inlined into agent prompts forever.
	host.writeAtHead("org1", skillRepoPath("retired-legacy"),
		"---\nname: retired-legacy\ndescription: No longer shipped.\n---\n\ngone\n")

	n, err := svc.Reconcile(ctx, "org1")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n != 1 {
		t.Fatalf("Reconcile changed %d, want 1 (the retired purge)", n)
	}
	got, _ := svc.List(ctx, "org1")
	if _, present := nameSet(got)["retired-legacy"]; present {
		t.Fatalf("retired built-in should be purged, still present: %v", keysOf(nameSet(got)))
	}
	// The real built-ins survive the purge.
	if _, ok := nameSet(got)["go"]; !ok {
		t.Fatalf("purge removed a live built-in")
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

	t.Run("absent built-in surfaces on the badge", func(t *testing.T) {
		t.Parallel()
		svc, host := newTestStore(t)
		ctx := context.Background()
		if _, err := svc.List(ctx, "org1"); err != nil {
			t.Fatalf("seed: %v", err)
		}
		host.removeAtHead("org1", skillRepoPath("go"))

		ups, err := svc.UpdatesAvailable(ctx, "org1")
		if err != nil {
			t.Fatalf("UpdatesAvailable: %v", err)
		}
		var found bool
		for i := range ups {
			if ups[i].Name == "go" {
				found = true
			}
		}
		if !found {
			t.Fatalf("absent go must surface on the badge, got %+v", ups)
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
	if err := NewSkillService(nil, nil).EnsureProvisioned(ctx, "org1"); err != nil {
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
		t.Fatalf("provision did not seed built-ins: %v", keysOf(nameSet(got)))
	}
}

// The unified embedded library: every skill vendored from repo-root skills/
// loads with its kind read from frontmatter — platform for the 5 generation
// skills (stamped metadata.aep.kind: platform), org for the 4 unmarked stack
// skills (absent → org). One loader, one source tree.
func TestLoadEmbeddedLibrary(t *testing.T) {
	t.Parallel()
	got, err := loadEmbeddedLibrary()
	if err != nil {
		t.Fatalf("loadEmbeddedLibrary: %v", err)
	}
	by := nameSet(got)
	if len(got) != 9 {
		t.Fatalf("library size = %d, want 9: %v", len(got), keysOf(by))
	}
	wantKinds := map[string]string{
		"api-management": "org", "go": "org", "react-webapp": "org", "thunder-authentication": "org",
		"excalidraw-wireframes": "platform", "high-level-architecture": "platform",
		"openapi-conventions": "platform", "task-breakdown": "platform", "task-planning": "platform",
	}
	for name, kind := range wantKinds {
		sk, ok := by[name]
		if !ok {
			t.Fatalf("embedded skill %q missing; got %v", name, keysOf(by))
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
