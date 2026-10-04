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

// Shared harness for the artifact tests. The gittest tier runs the REAL code
// paths, not mocked git:
//
//   - a real bare repo whose `main` tip IS the draft "working tree"
//     (gittest.NewRemote; arranged with r.seed / r.tag),
//   - the REAL gitfs Workspace engine mirroring that repo over file://
//     (workspacetest.NewEngine) — the engine the AE Studio pod runs. The
//     service's reads reach it through workspaceGit (the sourcecontrol.Git
//     port with the pod's semantics: fetch unless local, sha reads local)
//     and its save tag through the production NewGitOpsService,
//   - the REAL artifacts.ArtifactService over all of the above.
//
// Task 4.16 moves the tag onto Git.Tag and deletes gitfs; this rig then moves
// onto aestudiotest.Fake.
//
// Only the two edges the flow doesn't own are faked: the RepoRepository row (a
// single in-memory GitRepository naming a GitHub repository, which RefForRow
// needs; the engine is pointed at the file:// origin instead) and the
// credential Resolver (a static token + identity).
// save→tag / discard→revert-commit / read-at-HEAD / read-at-tag therefore run
// end-to-end over genuine git object-store semantics, offline.
//
// hookedWorkspace replaces the retired Git-Data server's ref-move/tag-create
// race-injection hooks: it wraps the real engine and lets a test act right
// before a Tag push attempt or inside each Mutate fn attempt (post-fetch,
// pre-push) — the deterministic windows for CAS / tag-collision races.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs/workspacetest"
	"github.com/wso2/aep/aep-api/internal/platform/gittest"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// ----- faked edges: RepoRepository row + credential resolver -----

// stubRepoRepo returns one fixed GitRepository row.
type stubRepoRepo struct{ rec *sourcecontrol.GitRepository }

var _ sourcecontrol.RepoRepository = (*stubRepoRepo)(nil)

func (s *stubRepoRepo) GetByOrgAndProjectID(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return s.rec, nil
}
func (s *stubRepoRepo) FindInOrgByFullName(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return nil, nil
}

func (s *stubRepoRepo) GetByOrgAndSlug(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return nil, sourcecontrol.ErrRepoNotFound
}
func (s *stubRepoRepo) ListAllReady(context.Context) ([]sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (s *stubRepoRepo) ListByOrg(context.Context, string) ([]sourcecontrol.GitRepository, error) {
	panic("stubRepoRepo: ListByOrg not expected in artifacts tests")
}
func (s *stubRepoRepo) ListAll(context.Context) ([]sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (s *stubRepoRepo) Create(context.Context, *sourcecontrol.GitRepository) error { return nil }
func (s *stubRepoRepo) Update(context.Context, *sourcecontrol.GitRepository) error { return nil }
func (s *stubRepoRepo) DeleteByOrgAndProjectID(context.Context, string, string) error {
	return nil
}

// stubCred / stubResolver hand the save flow a static token + committer
// identity. ResolveSaveIdentities reads Identity(); the Git Data API fake
// accepts any Authorization header, so the token is never checked.
type stubCred struct{}

func (stubCred) Token(context.Context) (string, time.Time, error) {
	return "test-token", time.Time{}, nil
}
func (stubCred) Identity() secrets.Identity {
	return secrets.Identity{Name: "Bot", Email: "bot@aep.dev", Login: "bot"}
}
func (stubCred) RepoOwner() string                        { return "acme" }
func (stubCred) WebhookStrategy() secrets.WebhookStrategy { return secrets.WebhookPlatform }

type stubResolver struct{}

func (stubResolver) Resolve(context.Context, string) (secrets.Credential, error) {
	return stubCred{}, nil
}

// ----- race-injection seam (the ex-Git-Data-server hooks' successor) -----

// hookedWorkspace delegates to the real engine, exposing two deterministic
// injection points: BeforeTag fires before every Tag push attempt (the
// tag-collision window), and BeforeMutateFn fires inside every Mutate fn
// attempt with its 1-based attempt number — fn runs AFTER the engine's fetch
// and BEFORE its push, so seeding the origin there makes that attempt's push a
// genuine non-fast-forward.
//
// origin is the file:// origin every write pushes to: the row names the
// GitHub repository (RefForRow needs owner/repo), so the writes the gateway
// resolves from it are re-pointed here.
type hookedWorkspace struct {
	sourcecontrol.Workspace
	origin         string
	BeforeTag      func(spec sourcecontrol.TagSpec)
	BeforeMutateFn func(attempt int)
}

func (h *hookedWorkspace) Tag(ctx context.Context, ref sourcecontrol.WorkspaceRef, spec sourcecontrol.TagSpec) error {
	if h.BeforeTag != nil {
		h.BeforeTag(spec)
	}
	ref.CloneURL = h.origin
	return h.Workspace.Tag(ctx, ref, spec)
}

func (h *hookedWorkspace) Mutate(ctx context.Context, ref sourcecontrol.WorkspaceRef, fn func(sourcecontrol.Tx) error, opts sourcecontrol.CommitOpts) (gitfs.CommitResult, error) {
	ref.CloneURL = h.origin
	if h.BeforeMutateFn == nil {
		return h.Workspace.Mutate(ctx, ref, fn, opts)
	}
	attempt := 0
	return h.Workspace.Mutate(ctx, ref, func(tx sourcecontrol.Tx) error {
		attempt++
		h.BeforeMutateFn(attempt)
		return fn(tx)
	}, opts)
}

// ----- rig -----

type rig struct {
	t      *testing.T
	svc    ArtifactService
	remote *gittest.Remote
	engine *gitfs.Engine
	ws     *hookedWorkspace
	rec    *sourcecontrol.GitRepository
	org    string
	proj   string
}

var idSanitize = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// idsFor derives a unique (orgID, projectID) from the test name so parallel
// tests never share a mount path key.
func idsFor(t *testing.T) (string, string) {
	safe := idSanitize.ReplaceAllString(t.Name(), "-")
	return "org-" + safe, "proj-" + safe
}

// newRig seeds a bare origin's `main` with `seed` (repo-relative path →
// content) as the initial draft, stands up a real workspace engine over it,
// and wires the production gitOps GitGateway + artifact service. Reads AND
// writes (tag, revert) all run through the engine.
func newRig(t *testing.T, seed map[string]string) *rig {
	t.Helper()
	org, proj := idsFor(t)
	remote := gittest.NewRemote(t, gittest.WithSeed(seed, "seed"))

	rec := &sourcecontrol.GitRepository{
		OrgID:         org,
		ProjectID:     proj,
		RepoURL:       "https://github.com/acme/widgets",
		RepoSlug:      "acme-widgets",
		DefaultBranch: "main",
		Status:        "ready",
	}
	repoRepo := &stubRepoRepo{rec: rec}
	engine := workspacetest.NewEngine(t)
	ws := &hookedWorkspace{Workspace: engine, origin: remote.URL()}
	gitOps := sourcecontrol.NewGitOpsService(stubResolver{}, ws)
	r := &rig{t: t, remote: remote, engine: engine, ws: ws, rec: rec, org: org, proj: proj}
	r.svc = NewArtifactService(repoRepo, workspaceGit{ws: ws, refFor: fixedWorkspaceRef(r.workspaceRef())}, gitOps)
	return r
}

// workspaceRef derives the mount RepoRef production resolves for the row,
// pointed at the file:// origin.
func (r *rig) workspaceRef() sourcecontrol.WorkspaceRef {
	ref := sourcecontrol.WorkspaceRefFor(r.org, r.rec, stubCred{})
	ref.CloneURL = r.remote.URL()
	return ref
}

// ----- the Git port over the rig's engine -----

// errRigWrite: the tests' writes go through the gitfs workspace (the
// VersionTagGateway, Workspace.Mutate) until Task 4.16, never the read port.
var errRigWrite = errors.New("workspaceGit: writes go through the gitfs workspace")

// workspaceGit is sourcecontrol.Git's reads over a gitfs engine, with the
// pod's semantics: a read at "" fetches first unless Local() is passed (a
// local read pins the mirror's tip), a sha read is local, and ReadBundle
// applies the pod's filter (exact Paths, else Prefix + any of Exts). refFor
// maps the port's RepoRef to the engine's mount ref (file:// origin).
type workspaceGit struct {
	ws     sourcecontrol.Workspace
	refFor func(sourcecontrol.RepoRef) (sourcecontrol.WorkspaceRef, error)
}

var _ sourcecontrol.Git = workspaceGit{}

// fixedWorkspaceRef answers ref for every RepoRef (the artifact rig's one
// repository).
func fixedWorkspaceRef(ref sourcecontrol.WorkspaceRef) func(sourcecontrol.RepoRef) (sourcecontrol.WorkspaceRef, error) {
	return func(sourcecontrol.RepoRef) (sourcecontrol.WorkspaceRef, error) { return ref, nil }
}

// pin answers the mount ref and the `at` a read addresses: a local read of
// the tip is the mirror's tip sha.
func (g workspaceGit) pin(ctx context.Context, ref sourcecontrol.RepoRef, at string, opts []sourcecontrol.ReadOption) (sourcecontrol.WorkspaceRef, string, error) {
	wref, err := g.refFor(ref)
	if err != nil {
		return wref, "", err
	}
	if at == "" && sourcecontrol.ReadOptionsOf(opts...).Local {
		at, err = g.ws.HeadLocal(ctx, wref)
	}
	return wref, at, err
}

func (g workspaceGit) Head(ctx context.Context, ref sourcecontrol.RepoRef, at string, opts ...sourcecontrol.ReadOption) (string, error) {
	wref, err := g.refFor(ref)
	if err != nil {
		return "", err
	}
	if at == "" && sourcecontrol.ReadOptionsOf(opts...).Local {
		return g.ws.HeadLocal(ctx, wref)
	}
	return g.ws.Head(ctx, wref, at)
}

func (g workspaceGit) List(ctx context.Context, ref sourcecontrol.RepoRef, at string, opts ...sourcecontrol.ReadOption) ([]sourcecontrol.Entry, string, error) {
	wref, at, err := g.pin(ctx, ref, at, opts)
	if err != nil {
		return nil, "", err
	}
	return g.ws.List(ctx, wref, at)
}

func (g workspaceGit) ReadFile(ctx context.Context, ref sourcecontrol.RepoRef, at, path string, opts ...sourcecontrol.ReadOption) ([]byte, string, error) {
	wref, at, err := g.pin(ctx, ref, at, opts)
	if err != nil {
		return nil, "", err
	}
	return g.ws.ReadFile(ctx, wref, at, path)
}

func (g workspaceGit) ReadBundle(ctx context.Context, ref sourcecontrol.RepoRef, at string, f sourcecontrol.BundleFilter, opts ...sourcecontrol.ReadOption) (map[string]string, string, error) {
	wref, at, err := g.pin(ctx, ref, at, opts)
	if err != nil {
		return nil, "", err
	}
	return g.ws.ReadBundle(ctx, wref, at, func(path string) bool {
		if len(f.Paths) > 0 {
			return slices.Contains(f.Paths, path)
		}
		if !strings.HasPrefix(path, f.Prefix) {
			return false
		}
		return len(f.Exts) == 0 || slices.ContainsFunc(f.Exts, func(e string) bool { return strings.HasSuffix(path, e) })
	})
}

func (g workspaceGit) ListTags(ctx context.Context, ref sourcecontrol.RepoRef, prefix string, opts ...sourcecontrol.ReadOption) ([]sourcecontrol.TagInfo, error) {
	wref, err := g.refFor(ref)
	if err != nil {
		return nil, err
	}
	if sourcecontrol.ReadOptionsOf(opts...).Local {
		return g.ws.ListTagsLocal(ctx, wref, prefix)
	}
	return g.ws.ListTags(ctx, wref, prefix)
}

func (workspaceGit) Tag(context.Context, sourcecontrol.RepoRef, sourcecontrol.TagSpec) error {
	return errRigWrite
}

func (workspaceGit) Commit(context.Context, sourcecontrol.RepoRef, sourcecontrol.CommitRequest) (sourcecontrol.CommitResult, error) {
	return sourcecontrol.CommitResult{}, errRigWrite
}

// mirrorRevParse resolves rev inside the ENGINE's bare mirror (not the origin)
// — the C8 sha-consistency probe.
func (r *rig) mirrorRevParse(rev string) string {
	r.t.Helper()
	repoDir, err := gitfs.RepoDir(r.engine.Root(), gitfs.RepoRef{
		OrgID: r.org, ProjectID: r.proj, RepoSlug: r.rec.RepoSlug,
	})
	if err != nil {
		r.t.Fatalf("mirror git dir: %v", err)
	}
	gitDir := gitfs.GitSubdir(repoDir)
	c := exec.Command("git", "--git-dir", gitDir, "rev-parse", "--verify", rev)
	c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
	out, err := c.CombinedOutput()
	if err != nil {
		r.t.Fatalf("mirror rev-parse %s: %v\n%s", rev, err, out)
	}
	return strings.TrimSpace(string(out))
}

// originRevParse resolves rev on the bare ORIGIN.
func (r *rig) originRevParse(rev string) string {
	r.t.Helper()
	c := exec.Command("git", "--git-dir="+r.remote.Dir(), "rev-parse", "--verify", rev)
	c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull)
	out, err := c.CombinedOutput()
	if err != nil {
		r.t.Fatalf("origin rev-parse %s: %v\n%s", rev, err, out)
	}
	return strings.TrimSpace(string(out))
}

// ----- arrange / assert helpers (against the bare origin = the draft) -----

// seed advances `main` with the given files (a new draft commit).
func (r *rig) seed(files map[string]string, msg string) string {
	r.t.Helper()
	return r.remote.Seed(r.t, files, msg)
}

// tag creates an annotated tag on the current `main` tip.
func (r *rig) tag(name, msg string) {
	r.t.Helper()
	r.remote.Tag(r.t, name, msg)
}

func (r *rig) tags() []string  { return r.remote.Tags(r.t) }
func (r *rig) headSHA() string { return r.remote.HeadSHA(r.t) }

// validComponentDesignJSON is a component design.json that satisfies the design
// schema gate — the shared seed for every save/read test.
func validComponentDesignJSON(name string) string {
	return `{"name":"` + name + `","type":"service","version":"1.0.0","language":"go",` +
		`"buildpack":"go","appPath":".","entrypoint":"main.go","exposure":"internet",` +
		`"stories":[1],"dependencies":[],"description":"a service"}`
}

// memRepos is a project-repository table holding one ready row: org's
// project p, at repository url. Any other org or project has no row.
func memRepos(t *testing.T, org, project, url string) sourcecontrol.RepoRepository {
	t.Helper()
	return &orgScopedRepoRepo{stubRepoRepo: stubRepoRepo{rec: &sourcecontrol.GitRepository{
		OrgID: org, ProjectID: project, RepoURL: url, DefaultBranch: "main", Status: "ready",
	}}}
}

// orgScopedRepoRepo answers its row only for the row's own org and project.
type orgScopedRepoRepo struct{ stubRepoRepo }

func (s *orgScopedRepoRepo) GetByOrgAndProjectID(_ context.Context, org, project string) (*sourcecontrol.GitRepository, error) {
	if org != s.rec.OrgID || project != s.rec.ProjectID {
		return nil, nil
	}
	return s.rec, nil
}

// repoRef is the repository the service's reads address for the row.
func (r *rig) repoRef() sourcecontrol.RepoRef {
	r.t.Helper()
	ref, err := sourcecontrol.RefForRow(r.org, r.rec)
	if err != nil {
		r.t.Fatalf("repo ref: %v", err)
	}
	return ref
}
