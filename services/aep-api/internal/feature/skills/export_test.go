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

// The Go export_test.go pattern: exported test-only helpers that let the
// EXTERNAL component-test package (skills_test, which must live outside package
// skills to break the api→skills import cycle) build a REAL *SkillService over
// the engine-backed git fixture the repo_store_test.go pattern provides (a
// gitfs engine rooted in t.TempDir() + one real bare file:// origin per org) —
// without duplicating the host or reaching its unexported doubles. This is the
// engine-injection seam the later ports (files/artifacts, Phases 2–3) reuse:
// production gateway (gitrepo.NewGitOpsService) + workspacetest engine + a
// RepoService fake whose rows carry file:// CloneURLs and a pinned RepoSlug.
// These symbols are compiled only into the test binary.

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
)

// ComponentStore is an exported handle around a real SkillService backed by
// the gitfs engine over real bare origins. The component tier wires Svc into
// api.HumaDeps and drives the real HTTP chain against it end-to-end.
type ComponentStore struct {
	Svc  *SkillService
	host *testGitHost
}

// NewComponentStore builds a SkillService over a fresh engine + per-org real
// origins, all rooted in t.TempDir(). The first read for an org lazily
// provisions the repo and seeds the embedded built-ins + flow skills, exactly
// as production does.
func NewComponentStore(t *testing.T) *ComponentStore {
	host := newTestGitHost(t)
	svc := NewSkillService(gitrepo.NewGitOpsService(fakeResolver{}, host.engine), host)
	return &ComponentStore{Svc: svc, host: host}
}

// DriftOrg rewrites an org-kind skill's SKILL.md directly on the org's ORIGIN
// (advancing main), so a subsequent read/UpdatesAvailable sees a repo copy
// whose content differs from the embedded copy — the state that drives the "updates available"
// badge. Reads address the branch tip, so the change is visible immediately
// (no cache to evict). The repo row already exists after the first read, so
// this write is not re-reconciled away.
func (c *ComponentStore) DriftOrg(orgID, name, skillMD string) {
	c.host.writeAtHead(orgID, skillRepoPath(name), skillMD)
}
