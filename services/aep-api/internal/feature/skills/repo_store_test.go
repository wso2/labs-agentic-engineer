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
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/credentials"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs/workspacetest"
	"github.com/wso2/aep/aep-api/internal/platform/gittest"
	"github.com/wso2/aep/aep-api/models"
)

// ---- engine-backed test host -------------------------------------------------

// testGitHost is the workspacetest-backed fixture these tests run against: one
// gitfs engine rooted in t.TempDir() plus one REAL bare file:// origin per org,
// provisioned lazily by EnsureBareRepo exactly like production provisions the
// GitHub repo (it supersedes the old in-memory git-host fake — the store now
// drives genuine git plumbing end to end). It implements gitrepo.RepoService
// (the row store) and hands out origins for arrange/assert.
type testGitHost struct {
	gitrepo.RepoService // embedded: unimplemented methods panic (untouched by these tests)

	t       *testing.T
	engine  *gitfs.Engine
	mu      sync.Mutex // models the DB's concurrency safety (the real GetRepo/EnsureBareRepo are serialized by Postgres)
	rows    map[string]*models.GitRepository
	origins map[string]*gittest.Remote
}

func newTestGitHost(t *testing.T) *testGitHost {
	return &testGitHost{
		t:       t,
		engine:  workspacetest.NewEngine(t),
		rows:    map[string]*models.GitRepository{},
		origins: map[string]*gittest.Remote{},
	}
}

func (h *testGitHost) GetRepo(_ context.Context, orgID, _ string) (*models.GitRepository, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r, ok := h.rows[orgID]; ok {
		return r, nil
	}
	return nil, gitrepo.ErrRepoNotFound
}

func (h *testGitHost) EnsureBareRepo(_ context.Context, orgID, projectID, repoName string) (*models.GitRepository, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r, ok := h.rows[orgID]; ok {
		return r, nil
	}
	origin := workspacetest.NewOrigin(h.t, nil)
	r := &models.GitRepository{
		OrgID:         orgID,
		ProjectID:     projectID,
		RepoURL:       origin.URL(),
		DefaultBranch: "main",
		Status:        "ready",
		// Production persists models.SlugForURL(cloneURL); file:// URLs have no
		// owner/repo shape, so the tests pin the stable repo name as the slug —
		// the path key the engine derives the mirror location from.
		RepoSlug: repoName,
	}
	h.origins[orgID] = origin
	h.rows[orgID] = r
	return r, nil
}

// origin returns the org's provisioned bare origin for arrange/assert (nil
// before the first read provisions it).
func (h *testGitHost) origin(orgID string) *gittest.Remote {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.origins[orgID]
}

// writeAtHead / removeAtHead commit content changes directly on the ORIGIN
// (advancing main) the way an external writer would — the store's branch-tip
// reads must observe them on the very next read (no cache to evict).
func (h *testGitHost) writeAtHead(orgID, path, content string) {
	h.origin(orgID).Seed(h.t, map[string]string{path: content}, "test write "+path)
}

func (h *testGitHost) removeAtHead(orgID, path string) {
	h.origin(orgID).Remove(h.t, "test remove "+path, path)
}

// mirrorGitDir is the engine-side bare mirror location for the org's skills repo.
func (h *testGitHost) mirrorGitDir(orgID string) (string, error) {
	h.mu.Lock()
	row := h.rows[orgID]
	h.mu.Unlock()
	return gitfs.GitDir(h.engine.Root(), gitrepo.WorkspaceRefFor(orgID, row, nil))
}

// ---- fake credential + resolver ----------------------------------------------

type fakeCred struct{}

func (fakeCred) Token(context.Context) (string, time.Time, error) { return "tok", time.Time{}, nil }
func (fakeCred) Identity() credentials.Identity {
	return credentials.Identity{Name: "Bot", Email: "bot@aep.dev"}
}
func (fakeCred) RepoOwner() string                            { return "test-org" }
func (fakeCred) WebhookStrategy() credentials.WebhookStrategy { return credentials.WebhookPlatform }

type fakeResolver struct{}

func (fakeResolver) Resolve(context.Context, string) (credentials.Credential, error) {
	return fakeCred{}, nil
}

// newTestStore builds a REAL SkillService over the engine-backed host: the
// production gitrepo.NewGitOpsService gateway with the workspacetest engine
// as the Workspace port.
func newTestStore(t *testing.T) (*SkillService, *testGitHost) {
	host := newTestGitHost(t)
	svc := NewSkillService(gitrepo.NewGitOpsService(fakeResolver{}, host.engine), host)
	return svc, host
}

func nameSet(skills []Skill) map[string]Skill {
	out := map[string]Skill{}
	for _, sk := range skills {
		out[sk.Name] = sk
	}
	return out
}

// gitDirOut runs one git plumbing command against a bare repo dir (an origin
// or an engine mirror) for integrity assertions.
func gitDirOut(t *testing.T, gitDir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"--git-dir", gitDir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, gitDir, err, out)
	}
	return string(out)
}

// ---- tests -----------------------------------------------------------------

func TestList_SeedsBuiltinsOnFirstRead(t *testing.T) {
	t.Parallel()
	svc, _ := newTestStore(t)
	got, err := svc.List(context.Background(), "org1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	by := nameSet(got)
	for _, want := range []string{"go", "api-management", "react-webapp", "thunder-authentication"} {
		sk, ok := by[want]
		if !ok {
			t.Fatalf("expected org skill %q to be seeded; got %v", want, keysOf(by))
		}
		if sk.Kind != models.SkillKindOrg {
			t.Fatalf("skill %q: kind = %q, want org", want, sk.Kind)
		}
	}
}

func TestFreshOrgProvisioning_SeedsEmbeddedLibrary(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()

	// A brand-new org lists the built-ins with zero Postgres skill state —
	// the first read provisions the repo and seeds both embedded kinds.
	summaries, err := svc.ListSummaries(ctx, "org1")
	if err != nil {
		t.Fatalf("ListSummaries: %v", err)
	}
	byName := map[string]SkillSummary{}
	for _, s := range summaries {
		byName[s.Name] = s
	}
	if _, ok := byName["go"]; !ok {
		t.Fatalf("fresh org must list built-ins, got %v", summaries)
	}
	// Platform skills are seeded and list READ-ONLY on the skills page.
	for _, platformName := range []string{"high-level-architecture", "excalidraw-wireframes", "openapi-conventions", "task-planning"} {
		sum, ok := byName[platformName]
		if !ok {
			t.Fatalf("platform skill %q missing from the user-facing list", platformName)
		}
		if sum.Kind != models.SkillKindPlatform || sum.Editable {
			t.Fatalf("platform skill %q must list read-only, got %+v", platformName, sum)
		}
	}

	// The internal catalog carries them as kind=platform, references included.
	all, _ := svc.List(ctx, "org1")
	by := nameSet(all)
	hla, ok := by["high-level-architecture"]
	if !ok || hla.Kind != models.SkillKindPlatform {
		t.Fatalf("internal catalog must carry platform skills; got %+v", hla)
	}
	oapi := by["openapi-conventions"]
	if oapi.References["references/wso2-rest-api-design-guidelines.md"] == "" {
		t.Fatalf("platform skill references not seeded: %v", keysOfStr(oapi.References))
	}

	// And they are genuinely IN the repo tree on origin under flat skills/.
	origin := host.origin("org1")
	if got := origin.FileAt(t, "main", "skills/high-level-architecture/SKILL.md"); !strings.Contains(got, "name: high-level-architecture") {
		t.Fatalf("platform SKILL.md not committed to origin:\n%s", got)
	}
	if got := origin.FileAt(t, "main", "skills/excalidraw-wireframes/references/wireframes-dsl-example.md"); got == "" {
		t.Fatal("platform reference file not committed to origin")
	}
}

func TestReconcile_RewritesMissingBuiltin(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil { // triggers seed
		t.Fatalf("seed: %v", err)
	}
	// Simulate the `go` built-in being deleted from the repo.
	host.removeAtHead("org1", skillRepoPath("go"))

	n, err := svc.Reconcile(ctx, "org1")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n != 1 {
		t.Fatalf("Reconcile wrote %d, want 1 (only `go` was missing)", n)
	}
	got, _ := svc.List(ctx, "org1")
	if _, ok := nameSet(got)["go"]; !ok {
		t.Fatalf("`go` should be restored after reconcile")
	}
}

func TestReconcile_ReseedsMissingFlowSkillAndPrunesStaleRefs(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Plant a stale reference under a platform skill, then delete its SKILL.md —
	// the reconcile must re-seed the skill AND replace the whole dir, so the
	// stale reference does not linger.
	host.writeAtHead("org1", "skills/task-planning/references/stale.md", "stale")
	host.removeAtHead("org1", skillRepoPath("task-planning"))

	n, err := svc.Reconcile(ctx, "org1")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n != 1 {
		t.Fatalf("Reconcile changed %d, want 1 (task-planning re-seed)", n)
	}
	got, _ := svc.List(ctx, "org1")
	tp, ok := nameSet(got)["task-planning"]
	if !ok || tp.Kind != models.SkillKindPlatform {
		t.Fatalf("task-planning should be restored as platform, got %+v", tp)
	}
	if _, lingers := tp.References["references/stale.md"]; lingers {
		t.Fatalf("stale reference survived the dir-replacing re-seed: %v", keysOfStr(tp.References))
	}
}

func TestReconcile_NoopWhenUpToDate(t *testing.T) {
	t.Parallel()
	svc, _ := newTestStore(t)
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	n, err := svc.Reconcile(ctx, "org1")
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n != 0 {
		t.Fatalf("Reconcile wrote %d, want 0 (already seeded at current versions)", n)
	}
	ups, err := svc.UpdatesAvailable(ctx, "org1")
	if err != nil {
		t.Fatalf("UpdatesAvailable: %v", err)
	}
	if len(ups) != 0 {
		t.Fatalf("UpdatesAvailable = %v, want none", ups)
	}
}

func TestCreateAndDeleteCustomSkill(t *testing.T) {
	t.Parallel()
	svc, _ := newTestStore(t)
	mut := NewSkillMutationService(svc)
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const skillMD = "---\n" +
		"name: payments-pci\n" +
		"description: PCI handling rules for payment components.\n" +
		"metadata:\n" +
		"  aep.version: \"1\"\n" +
		"---\n\nAlways tokenize PANs before persistence.\n"

	created, err := mut.Create(ctx, "org1", "tester", CreateSkillInput{Name: "payments-pci", SkillMD: skillMD})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created == nil || created.Kind != "custom" {
		t.Fatalf("created skill = %+v, want kind=custom", created)
	}

	resolved, err := svc.Resolve(ctx, "org1", "payments-pci")
	if err != nil || resolved == nil {
		t.Fatalf("Resolve after create: %v / %v", resolved, err)
	}
	summaries, _ := svc.ListSummaries(ctx, "org1")
	var found *SkillSummary
	for i := range summaries {
		if summaries[i].Name == "payments-pci" {
			found = &summaries[i]
		}
	}
	if found == nil || !found.Editable {
		t.Fatalf("custom skill should appear in summaries as editable; got %+v", found)
	}

	// Duplicate create → collision.
	if _, err := mut.Create(ctx, "org1", "tester", CreateSkillInput{Name: "payments-pci", SkillMD: skillMD}); err != ErrSkillNameCollision {
		t.Fatalf("duplicate create err = %v, want ErrSkillNameCollision", err)
	}

	// Delete → gone.
	if err := mut.Delete(ctx, "org1", "tester", "payments-pci"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	gone, _ := svc.Resolve(ctx, "org1", "payments-pci")
	if gone != nil {
		t.Fatalf("skill should be gone after delete, got %+v", gone)
	}
}

func TestDeleteBuiltinIsForbidden(t *testing.T) {
	t.Parallel()
	svc, _ := newTestStore(t)
	mut := NewSkillMutationService(svc)
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := mut.Delete(ctx, "org1", "tester", "go"); err != ErrSkillNotEditable {
		t.Fatalf("delete builtin err = %v, want ErrSkillNotEditable", err)
	}
}

func TestPlatformSkillReadOnlyAndNameReserved(t *testing.T) {
	t.Parallel()
	svc, _ := newTestStore(t)
	mut := NewSkillMutationService(svc)
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Platform skills resolve read-only on the by-name user surface…
	sk, _ := svc.Resolve(ctx, "org1", "task-planning")
	if sk == nil || sk.Kind != models.SkillKindPlatform {
		t.Fatalf("Resolve must surface platform skills read-only, got %+v", sk)
	}
	// …but never mutate: reconcile owns them.
	if _, err := mut.Update(ctx, "org1", "tester", "task-planning", UpdateSkillInput{SkillMD: skillMDNamed("task-planning", "")}); !errors.Is(err, ErrSkillNotEditable) {
		t.Fatalf("update platform err = %v, want ErrSkillNotEditable", err)
	}
	if err := mut.Delete(ctx, "org1", "tester", "task-planning"); !errors.Is(err, ErrSkillNotEditable) {
		t.Fatalf("delete platform err = %v, want ErrSkillNotEditable", err)
	}

	// Their names stay reserved: creating a same-named custom skill would
	// shadow the platform skill in the catalog and duplicate it in snapshots,
	// so the collision check sees platform kinds.
	_, err := mut.Create(ctx, "org1", "tester", CreateSkillInput{
		Name:    "task-planning",
		SkillMD: skillMDNamed("task-planning", ""),
	})
	if !errors.Is(err, ErrSkillNameCollision) {
		t.Fatalf("create over platform name err = %v, want ErrSkillNameCollision", err)
	}
}

// TestRead_SeesExternalOriginCommitImmediately pins the cache-less freshness
// contract that replaced the old soft-TTL catalog cache: reads address the branch
// tip, so a commit landed on origin by ANOTHER writer (another replica, a
// human) is visible on the very next read.
func TestRead_SeesExternalOriginCommitImmediately(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil { // provision + seed
		t.Fatalf("List: %v", err)
	}

	// An external writer commits a flat, custom-stamped skill (the layout the
	// service itself writes).
	external := mkSkillMD("external-skill", "custom", "external body")
	host.writeAtHead("org1", skillRepoPath("external-skill"), external)

	got, err := svc.List(ctx, "org1")
	if err != nil {
		t.Fatalf("List 2: %v", err)
	}
	sk, ok := nameSet(got)["external-skill"]
	if !ok || sk.Kind != models.SkillKindCustom {
		t.Fatalf("externally committed skill not visible on next read: %v", keysOf(nameSet(got)))
	}
}

func TestConcurrentReads_ProvisionOnceConsistently(t *testing.T) {
	t.Parallel()
	svc, _ := newTestStore(t)
	ctx := context.Background()
	const n = 8
	var wg sync.WaitGroup
	results := make([][]Skill, n)
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = svc.List(ctx, "org1")
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: List error %v", i, errs[i])
		}
		if _, ok := nameSet(results[i])["go"]; !ok {
			t.Fatalf("goroutine %d: expected built-ins present, got %v", i, keysOf(nameSet(results[i])))
		}
	}
}

// TestCommitFiles_ConcurrentCommitsSerialize pins the Phase-1 exit gate: two
// concurrent commitFiles for one org are serialized by the per-repo flock +
// origin push-CAS — both land (or one surfaces a clean non-fast-forward
// conflict), the origin history stays linear, and `git fsck` is clean on both
// the origin and the engine's mirror.
func TestCommitFiles_ConcurrentCommitsSerialize(t *testing.T) {
	t.Parallel()
	svc, host := newTestStore(t)
	ctx := context.Background()
	if _, err := svc.List(ctx, "org1"); err != nil { // provision + seed
		t.Fatalf("seed: %v", err)
	}
	repo, err := svc.ensureSkillsRepo(ctx, "org1")
	if err != nil {
		t.Fatalf("ensureSkillsRepo: %v", err)
	}

	names := []string{"raced-one", "raced-two"}
	errs := make([]error, len(names))
	var wg sync.WaitGroup
	wg.Add(len(names))
	for i, name := range names {
		go func(i int, name string) {
			defer wg.Done()
			writes := map[string][]byte{skillRepoPath(name): []byte(skillMDNamed(name, ""))}
			_, errs[i] = svc.commitFiles(ctx, "org1", repo, "add "+name, writes, nil)
		}(i, name)
	}
	wg.Wait()

	landed := 0
	for i, err := range errs {
		switch {
		case err == nil:
			landed++
			// A landed write must be visible in the catalog.
			sk, rerr := svc.Resolve(ctx, "org1", names[i])
			if rerr != nil || sk == nil {
				t.Fatalf("landed skill %q not resolvable: %v / %v", names[i], sk, rerr)
			}
		case errors.Is(err, gitrepo.ErrRefNotFastForward):
			// The one acceptable failure mode: a clean CAS conflict.
		default:
			t.Fatalf("commit %q failed with a non-conflict error: %v", names[i], err)
		}
	}
	if landed == 0 {
		t.Fatal("neither concurrent commit landed")
	}

	// Origin history stays linear (no merge commits) and fsck-clean.
	origin := host.origin("org1")
	if merges := strings.TrimSpace(gitDirOut(t, origin.Dir(), "rev-list", "--merges", "--count", "main")); merges != "0" {
		t.Fatalf("origin history not linear: %s merge commits", merges)
	}
	gitDirOut(t, origin.Dir(), "fsck", "--strict")

	// Engine mirror fsck-clean too.
	mirror, err := host.mirrorGitDir("org1")
	if err != nil {
		t.Fatalf("mirrorGitDir: %v", err)
	}
	gitDirOut(t, mirror, "fsck", "--strict")
}

func keysOf(m map[string]Skill) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func keysOfStr(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
