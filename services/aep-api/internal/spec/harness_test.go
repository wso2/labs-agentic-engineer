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
//     (workspacetest.NewEngine + production NewGitOpsService) — every read,
//     the save tag, AND the discard revert run through the mount plumbing;
//     the Git-Data fake is gone with the REST write path,
//   - the REAL artifacts.ArtifactService over all of the above.
//
// Only the two edges the flow doesn't own are faked: the RepoRepository row (a
// single in-memory GitRepository, RepoSlug pinned — SlugForURL can't parse
// file:// URLs) and the credential Resolver (a static token + identity).
// save→tag / discard→revert-commit / read-at-HEAD / read-at-tag therefore run
// end-to-end over genuine git object-store semantics, offline.
//
// hookedWorkspace replaces the retired Git-Data server's ref-move/tag-create
// race-injection hooks: it wraps the real engine and lets a test act right
// before a Tag push attempt or inside each Mutate fn attempt (post-fetch,
// pre-push) — the deterministic windows for CAS / tag-collision races.

import (
	"context"
	"os"
	"os/exec"
	"regexp"
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
type hookedWorkspace struct {
	sourcecontrol.Workspace
	BeforeTag      func(spec sourcecontrol.TagSpec)
	BeforeMutateFn func(attempt int)
}

func (h *hookedWorkspace) Tag(ctx context.Context, ref sourcecontrol.RepoRef, spec sourcecontrol.TagSpec) error {
	if h.BeforeTag != nil {
		h.BeforeTag(spec)
	}
	return h.Workspace.Tag(ctx, ref, spec)
}

func (h *hookedWorkspace) Mutate(ctx context.Context, ref sourcecontrol.RepoRef, fn func(sourcecontrol.Tx) error, opts sourcecontrol.CommitOpts) (sourcecontrol.CommitResult, error) {
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
		RepoURL:       remote.URL(),
		RepoSlug:      "acme-widgets", // pinned — SlugForURL can't parse file:// URLs
		DefaultBranch: "main",
		Status:        "ready",
	}
	repoRepo := &stubRepoRepo{rec: rec}
	engine := workspacetest.NewEngine(t)
	ws := &hookedWorkspace{Workspace: engine}
	gitOps := sourcecontrol.NewGitOpsService(stubResolver{}, ws)
	svc := NewArtifactService(repoRepo, gitOps)

	return &rig{t: t, svc: svc, remote: remote, engine: engine, ws: ws, rec: rec, org: org, proj: proj}
}

// workspaceRef derives the same mount RepoRef production resolves for the row.
func (r *rig) workspaceRef() sourcecontrol.RepoRef {
	return sourcecontrol.WorkspaceRefFor(r.org, r.rec, stubCred{})
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
