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

package skills

import (
	"bytes"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"testing"
)

// repoRootSkillsDir is the canonical single-source skill library at the repo
// root (skills/). Every embedded copy in this module is a go:embed-only mirror
// of one directory here — go:embed cannot reach outside the module tree, so the
// copies exist only so the BFF binary is self-contained, and the guards below
// fail loud the moment a copy drifts from its repo-root source.
// services/aep-api/skills/ -> ../ (aep-api) -> ../../ (services) -> ../../../
// is the repo root.
const repoRootSkillsDir = "../../../skills"

// TestBuiltinSkillsMatchRepoRootCopies guards every embedded builtin/<name>/
// SKILL.md against silently drifting from its repo-root skills/<name>/SKILL.md
// source. The four stack skills (go, react-webapp, api-management,
// thunder-authentication) are bootstrapped into the `skills` table from
// BuiltinFS, but the authored source is the repo-root copy the playground /
// design agent also pushes (ADR-0005). Nothing enforces that an edit to one
// gets copied to the other, so this reads both and fails loud — with a
// copy-paste-able fix — the moment any pair diverges.
func TestBuiltinSkillsMatchRepoRootCopies(t *testing.T) {
	entries, err := fs.ReadDir(BuiltinFS, "builtin")
	if err != nil {
		t.Fatalf("read embedded builtin dir: %v", err)
	}
	var checked int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		embeddedPath := path.Join("builtin", name, "SKILL.md")
		repoRootPath := filepath.Join(repoRootSkillsDir, name, "SKILL.md")

		embedded, err := fs.ReadFile(BuiltinFS, embeddedPath)
		if err != nil {
			t.Errorf("read embedded %s: %v", embeddedPath, err)
			continue
		}
		repoRoot, err := os.ReadFile(repoRootPath)
		if err != nil {
			t.Errorf("read repo-root copy %s: %v (the repo-root skills/%s/SKILL.md "+
				"is the authored source; create it or copy the embedded mirror there)",
				repoRootPath, err, name)
			continue
		}
		if !bytes.Equal(embedded, repoRoot) {
			t.Errorf(
				"embedded %s has drifted from the repo-root copy at %s — these must "+
					"stay byte-identical (skills/%s/SKILL.md is the single authored "+
					"source; %s is its go:embed-only mirror). Fix: from "+
					"services/aep-api/skills/, run:\n\n"+
					"\tcp %s builtin/%s/SKILL.md\n\n"+
					"then re-run this test.",
				embeddedPath, repoRootPath, name, embeddedPath,
				repoRootPath, name,
			)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no embedded builtin skills found — expected at least the four stack skills")
	}
}

// TestArchitectHighLevelArchitectureSkillMatchesRepoRootCopy guards the
// embedded architect/high-level-architecture/SKILL.md (pushed directly on every
// architect call, mirroring how PlannerFS backs the task-planner) against
// drifting from its repo-root skills/high-level-architecture/SKILL.md source.
func TestArchitectHighLevelArchitectureSkillMatchesRepoRootCopy(t *testing.T) {
	embedded, err := ArchitectFS.ReadFile(HighLevelArchitectureSkillPath)
	if err != nil {
		t.Fatalf("read embedded %s: %v", HighLevelArchitectureSkillPath, err)
	}
	repoRootPath := filepath.Join(repoRootSkillsDir, "high-level-architecture", "SKILL.md")
	repoRoot, err := os.ReadFile(repoRootPath)
	if err != nil {
		t.Fatalf("read repo-root copy %s: %v", repoRootPath, err)
	}
	if !bytes.Equal(embedded, repoRoot) {
		t.Fatalf(
			"embedded %s has drifted from the repo-root copy at %s — these must stay "+
				"byte-identical (skills/high-level-architecture/SKILL.md is the single "+
				"authored source; %s is its go:embed-only mirror). Fix: from "+
				"services/aep-api/skills/, run:\n\n"+
				"\tcp %s architect/high-level-architecture/SKILL.md\n\n"+
				"then re-run this test.",
			HighLevelArchitectureSkillPath, repoRootPath, HighLevelArchitectureSkillPath,
			repoRootPath,
		)
	}
}

// repoRootTaskBreakdownSkillPath is the canonical repo-root copy of the
// task-breakdown skill (the one `services/agents`' eval/playground harness and
// any human reader load from `skills/`, per ADR-0002). `PlannerFS` below embeds
// a SECOND copy at planner/task-breakdown/SKILL.md because `go:embed` cannot
// reach outside this module's tree — the two must stay byte-identical.
// services/aep-api/skills/ -> ../ (aep-api) -> ../../ (services) -> ../../../
// is the repo root.
const repoRootTaskBreakdownSkillPath = "../../../skills/task-breakdown/SKILL.md"

// TestPlannerFSTaskBreakdownSkillMatchesRepoRootCopy guards against the
// embedded planner/task-breakdown/SKILL.md silently drifting from the
// repo-root skills/task-breakdown/SKILL.md it mirrors. Nothing enforces that
// an edit to one gets copied to the other, so this test reads both and fails
// loud — with a copy-paste-able fix — the moment they diverge.
func TestPlannerFSTaskBreakdownSkillMatchesRepoRootCopy(t *testing.T) {
	embedded, err := PlannerFS.ReadFile(TaskBreakdownSkillPath)
	if err != nil {
		t.Fatalf("read embedded %s: %v", TaskBreakdownSkillPath, err)
	}

	repoRoot, err := os.ReadFile(repoRootTaskBreakdownSkillPath)
	if err != nil {
		t.Fatalf("read repo-root copy %s: %v", repoRootTaskBreakdownSkillPath, err)
	}

	if !bytes.Equal(embedded, repoRoot) {
		t.Fatalf(
			"embedded %s has drifted from the repo-root copy at %s — these must "+
				"stay byte-identical (skills/task-breakdown/SKILL.md is the single "+
				"authored source; %s is its go:embed-only mirror). Fix: from "+
				"services/aep-api/skills/, run:\n\n"+
				"\tcp %s planner/task-breakdown/SKILL.md\n\n"+
				"then re-run this test.",
			TaskBreakdownSkillPath, repoRootTaskBreakdownSkillPath, TaskBreakdownSkillPath,
			repoRootTaskBreakdownSkillPath,
		)
	}
}
