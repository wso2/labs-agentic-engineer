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

// The Go export_test.go pattern: exported test-only helpers that let the
// EXTERNAL component-test package (skills_test, which must live outside package
// skills to break the api→skills import cycle) build a REAL *SkillService over
// the engine-backed git fixture the repo_store_test.go pattern provides (a
// gitfs engine rooted in t.TempDir() + one real bare file:// origin per org) —
// without duplicating the host or reaching its unexported doubles. This is the
// engine-injection seam the later ports (files/artifacts, Phases 2–3) reuse:
// production gateway (sourcecontrol.NewGitOpsService) + workspacetest engine + a
// RepoService fake whose rows carry file:// CloneURLs and a pinned RepoSlug.
// These symbols are compiled only into the test binary.

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// testLibraryFS is the platform skill source for tests: the repo-root skills/
// directory (the single authored library), located relative to this file so it
// is independent of the working directory — five levels up from
// the skills library dir to the repo root, then skills/. Mirrors what
// production injects via os.DirFS(config.SkillsDir).
func testLibraryFS(t *testing.T) fs.FS {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("skills: runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(self), "..", "..", "..", "..", "skills")
	return os.DirFS(root)
}

// ComponentStore is an exported handle around a real SkillService backed by
// the gitfs engine over real bare origins. The component tier wires Svc into
// edge.Deps and drives the real HTTP chain against it end-to-end.
type ComponentStore struct {
	Svc  *SkillService
	host *testGitHost
}

// NewComponentStore builds a SkillService over a fresh engine + per-org real
// origins, all rooted in t.TempDir(). The first read for an org lazily
// provisions the repo and seeds the embedded built-ins + flow skills, exactly
// as production does.
func NewComponentStore(t *testing.T) *ComponentStore {
	return NewComponentStoreWithLibrary(t, testLibraryFS(t))
}

// NewComponentStoreWithLibrary is NewComponentStore with an injected library
// fs.FS in place of the real repo-root skills/ dir — for tests that need a
// fixture skill (e.g. one carrying scripts/ or assets/) without leaking it
// into the shared library and perturbing the count-pinned tests that assert
// against the real embedded skill count.
func NewComponentStoreWithLibrary(t *testing.T, library fs.FS) *ComponentStore {
	host := newTestGitHost(t)
	svc := NewSkillService(sourcecontrol.NewGitOpsService(fakeResolver{}, host.engine), host, library)
	return &ComponentStore{Svc: svc, host: host}
}

// newTestStoreWithLibrary is newTestStore with an injected platform library —
// lets a test swap the library between reconciles (platform-moved / retired
// scenarios), which testLibraryFS(t)'s fixed on-disk tree cannot express.
func newTestStoreWithLibrary(t *testing.T, fsys fs.FS) (*SkillService, *testGitHost) {
	t.Helper()
	host := newTestGitHost(t)
	svc := NewSkillService(sourcecontrol.NewGitOpsService(fakeResolver{}, host.engine), host, fsys)
	return svc, host
}

// SwapLibrary points an existing service at a new platform library, modeling
// a platform release: same org repos, new embedded content.
func (s *SkillService) SwapLibrary(fsys fs.FS) { s.library = fsys }

// EmbeddedLibraryCount returns the number of skills of the given kind (e.g.
// SkillKindPlatform) in the real embedded library (repo-root skills/, via
// loadLibrary), for external test packages (spec_test) that assert against
// the catalog's kind composition without hardcoding a literal that rots every
// time a skill is added to or removed from skills/. kind == "" counts every
// embedded skill regardless of kind.
func EmbeddedLibraryCount(t *testing.T, kind string) int {
	t.Helper()
	skills, err := loadLibrary(testLibraryFS(t))
	if err != nil {
		t.Fatalf("loadLibrary: %v", err)
	}
	if kind == "" {
		return len(skills)
	}
	n := 0
	for _, sk := range skills {
		if sk.Kind == kind {
			n++
		}
	}
	return n
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

// SetHeartbeatEveryForTest drives the agent_turns heartbeat faster than its
// 15s production cadence so a component test can observe it inside a normal
// test runtime. Test-only, mirroring the sweeper's injectable staleAfter.
func (s *Service) SetHeartbeatEveryForTest(d time.Duration) {
	s.heartbeatEvery = d
}
