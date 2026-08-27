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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

func archOnlyLibrary() fstest.MapFS {
	return fstest.MapFS{
		"architecture/SKILL.md": &fstest.MapFile{Data: []byte("---\nname: architecture\ndescription: d\nmetadata:\n  aep:\n    kind: platform\n---\nbody\n")},
	}
}

func archPlusRegisterLibrary() fstest.MapFS {
	lib := archOnlyLibrary()
	lib["register-external-resource/SKILL.md"] = &fstest.MapFile{Data: []byte("---\nname: register-external-resource\ndescription: Use when registering a Registered External resource from Marketplace chat.\nmetadata:\n  aep:\n    kind: platform\n    audience: [design]\n---\n# Register\n")}
	return lib
}

// TestSkillsRepoForTurns_SnapshotIncludesNewPlatformSkill is the live
// register-chat miss: org _skills was first-provisioned before
// register-external-resource shipped. EnsureProvisioned is a no-op on an
// existing repo, so the SkillsRef snapshot never sees the skill. The turn
// resolver must Reconcile; StartTurn then Ensures. Both the loadSkill dest
// (_skills/org-skills) and the marketplace-register dual dest must list the
// new skill, and skills already in the library (architecture) must remain.
func TestSkillsRepoForTurns_SnapshotIncludesNewPlatformSkill(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, host := newTestStoreWithLibrary(t, archOnlyLibrary())
	if err := svc.EnsureProvisioned(ctx, "org1"); err != nil {
		t.Fatalf("first provision: %v", err)
	}
	svc.SwapLibrary(archPlusRegisterLibrary())

	row, err := SkillsRepoForTurns(svc, host)(ctx, "org1")
	if err != nil {
		t.Fatalf("SkillsRepoForTurns: %v", err)
	}
	git := sourcecontrol.NewGitOpsService(fakeResolver{}, host.engine)
	skillsRef := sourcecontrol.WorkspaceRefFor("org1", row, fakeCred{})
	sha, err := git.Workspace().Head(ctx, skillsRef, "")
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if err := host.engine.Ensure(ctx, skillsRef, sha); err != nil {
		t.Fatalf("Ensure skills: %v", err)
	}
	marketRef := skillsRef
	marketRef.ProjectID = MarketplaceRegisterProjectID
	marketRef.RepoSlug = marketplaceRegisterRepoSlug
	if err := host.engine.Ensure(ctx, marketRef, sha); err != nil {
		t.Fatalf("Ensure marketplace-register: %v", err)
	}

	wantRel := filepath.Join("skills", "register-external-resource", "SKILL.md")
	stillRel := filepath.Join("skills", "architecture", "SKILL.md")
	for _, ref := range []sourcecontrol.RepoRef{skillsRef, marketRef} {
		dir, err := gitfs.SnapshotDir(host.engine.Root(), ref, sha)
		if err != nil {
			t.Fatalf("SnapshotDir: %v", err)
		}
		body, err := os.ReadFile(filepath.Join(dir, wantRel))
		if err != nil {
			t.Fatalf("loadSkill path %s missing %s: %v", dir, wantRel, err)
		}
		if !strings.Contains(string(body), "name: register-external-resource") {
			t.Errorf("snapshot %s: SKILL.md = %q, want name register-external-resource", dir, body)
		}
		arch, err := os.ReadFile(filepath.Join(dir, stillRel))
		if err != nil {
			t.Fatalf("existing platform skill missing from %s: %v", dir, err)
		}
		if !strings.Contains(string(arch), "name: architecture") {
			t.Errorf("snapshot %s: architecture clobbered: %q", dir, arch)
		}
	}
}
