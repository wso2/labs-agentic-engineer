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

package edge

import (
	"net/http"
	"strings"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/repo/repotest"
)

// mirror-skills through the real routes; the mirror's own behaviour is
// internal/skills' tests.

const skillsMirrorPath = gitRepoPath + "/skills-mirror"

func skillsHarness(t *testing.T) (*harness, *repotest.Origin) {
	t.Helper()
	library := repotest.NewOrigin(t, map[string]string{
		"skills/review/SKILL.md": "---\nname: review\ndescription: d.\nmetadata:\n  aep:\n    audience: [coding]\n---\nguidance body\n",
		"skills/chat/SKILL.md":   "---\nname: chat\ndescription: d.\nmetadata:\n  aep:\n    audience: [design]\n---\nchat body\n",
	})
	project := repotest.NewOrigin(t, map[string]string{".claude/skills/stale/SKILL.md": "old"})
	return newHarness(t, withGitOrigins(map[string]*repotest.Origin{"acme-gh/org-skills": library, "acme-gh/greeter": project})), project
}

func TestInternalSkills_MirrorOverHTTP(t *testing.T) {
	h, project := skillsHarness(t)
	body := `{"skillsRepo":{"owner":"Acme-GH","repo":"org-skills","defaultBranch":"main"},"pinned":["chat"]}`

	rec := h.doJSON("POST", skillsMirrorPath+"?defaultBranch=main", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	got := jsonBody(t, rec.Body.Bytes())
	if got["changed"] != true || got["commitSha"] != project.HeadSHA(t) {
		t.Fatalf("%v, tip %s", got, project.HeadSHA(t))
	}
	tree := project.Git(t, "ls-tree", "-r", "--name-only", "main")
	if tree != ".claude/skills/chat/SKILL.md\n.claude/skills/review/SKILL.md" {
		t.Fatalf("tree:\n%s", tree)
	}

	rec = h.doJSON("POST", skillsMirrorPath, body)
	if again := jsonBody(t, rec.Body.Bytes()); rec.Code != http.StatusOK || again["changed"] != false || again["commitSha"] != got["commitSha"] {
		t.Fatalf("second: %d %v", rec.Code, again)
	}
}

func TestInternalSkills_Refusals(t *testing.T) {
	h, project := skillsHarness(t)
	before := project.HeadSHA(t)

	t.Run("skillsRepo under another owner", func(t *testing.T) {
		rec := h.doJSON("POST", skillsMirrorPath, `{"skillsRepo":{"owner":"someone-else","repo":"org-skills"},"pinned":[]}`)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"code":"owner_not_allowed"`) {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	})
	for name, body := range map[string]string{
		"pinned missing":       `{"skillsRepo":{"owner":"acme-gh","repo":"org-skills"}}`,
		"skillsRepo missing":   `{"pinned":[]}`,
		"unknown field":        `{"skillsRepo":{"owner":"acme-gh","repo":"org-skills"},"pinned":[],"x":1}`,
		"dot-dot skills repo":  `{"skillsRepo":{"owner":"acme-gh","repo":".."},"pinned":[]}`,
		"empty pin":            `{"skillsRepo":{"owner":"acme-gh","repo":"org-skills"},"pinned":[""]}`,
		"bad branch character": `{"skillsRepo":{"owner":"acme-gh","repo":"org-skills","defaultBranch":"a b"},"pinned":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if rec := h.doJSON("POST", skillsMirrorPath, body); rec.Code != http.StatusBadRequest {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
		})
	}
	if project.HeadSHA(t) != before {
		t.Fatal("a refused request moved the project")
	}
	if strings.Contains(h.logs(), "repo.clone") {
		t.Fatal("a refused request reached the engine")
	}
}
