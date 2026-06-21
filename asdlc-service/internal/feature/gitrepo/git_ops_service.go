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

package gitrepo

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wso2/asdlc/asdlc-service/internal/credentials"
	"github.com/wso2/asdlc/asdlc-service/models"
	"github.com/wso2/asdlc/asdlc-service/repositories"
)

// cryptoRandRead is exposed as a func var so tests can substitute a
// deterministic source. Real callers use crypto/rand.Read.
var cryptoRandRead = cryptorand.Read

// TagInfo describes a git tag.
type TagInfo struct {
	Name       string `json:"name"`
	CommitHash string `json:"commitHash"`
	Message    string `json:"message,omitempty"`
}

// GitOpsService handles git operations (commit, push, pull, status, tags).
type GitOpsService interface {
	// Startup lifecycle:
	//   - CleanupOrphanTmpClones removes leftover .tmpclone-* dirs from a
	//     prior crash mid-clone.
	//   - PreWarmClones ensures every ready repo's clone is on disk before
	//     the first request arrives, so cold-start traffic doesn't pay the
	//     multi-minute clone latency under the per-project mutex.
	CleanupOrphanTmpClones()
	PreWarmClones(ctx context.Context, workers int) (warmed, failed int)

	// GitWorkspace surface — the per-project clone primitives the artifacts
	// save flow drives through the GitOpsService port (no concrete cast).
	// Spelled out here (not embedded from artifacts) so gitrepo never imports
	// the artifacts feature; artifacts declares a narrower GitWorkspace port
	// that this interface structurally satisfies.
	RepoLock(projectID string) *sync.Mutex
	EnsureCloneReady(ctx context.Context, repoRecord *models.GitRepository) error
	PrepareAuthedEnv(ctx context.Context, repoRecord *models.GitRepository) ([]string, func(), error)
	ResolveSaveIdentities(cred credentials.Credential) (*GitIdentity, *GitIdentity)
	BestEffortPullDefaultBranch(ctx context.Context, repoRecord *models.GitRepository) error
	GitHubClient() GitHubClient
	Resolver() credentials.Resolver
}

type gitOpsService struct {
	repo         repositories.RepoRepository
	resolver     credentials.Resolver
	repoBasePath string
	locks        sync.Map // per-project mutex
	// gitHub is the GitHubClient injected for artifact-store save flows.
	// May be nil in test wiring that doesn't exercise SaveDesign /
	// SaveRequirements; the save flows nil-check before dereferencing.
	gitHub GitHubClient
}

// NewGitOpsService builds the service. The optional `github` arg is the
// GitHub HTTP client used by the artifact-store v2 save flow. Pass nil from
// tests that don't exercise save paths.
func NewGitOpsService(repo repositories.RepoRepository, resolver credentials.Resolver, repoBasePath string, github GitHubClient) GitOpsService {
	return &gitOpsService{repo: repo, resolver: resolver, repoBasePath: repoBasePath, gitHub: github}
}

func (s *gitOpsService) RepoLock(projectID string) *sync.Mutex {
	val, _ := s.locks.LoadOrStore(projectID, &sync.Mutex{})
	return val.(*sync.Mutex)
}

// resolveToken returns the GitHub token for the org owning the given repo.
// All on-disk git operations (clone/fetch/push) use this to authenticate.
func (s *gitOpsService) resolveToken(ctx context.Context, gitRepo *models.GitRepository) (string, credentials.Identity, error) {
	cred, err := s.resolver.Resolve(ctx, gitRepo.OrgID)
	if err != nil {
		return "", credentials.Identity{}, fmt.Errorf("resolve credential: %w", err)
	}
	token, _, err := cred.Token(ctx)
	if err != nil {
		return "", credentials.Identity{}, fmt.Errorf("token: %w", err)
	}
	return token, cred.Identity(), nil
}

// ErrCloneCorrupt is returned when the working tree exists with content but
// `.git` is missing/corrupt. We refuse to destructively re-clone in this
// state — a `RemoveAll` would silently lose any unsaved spec/design content —
// and instead surface the state to the operator (503 + runbook).
var ErrCloneCorrupt = errors.New("clone has content but .git is missing or corrupt")

// EnsureCloneReady verifies that the clone directory exists on disk. If the
// stored clone path is stale (e.g. REPO_BASE_PATH changed between runs) it
// updates the path. If the .git dir is absent, this performs a
// non-destructive re-clone: clone into a sibling `.tmpclone-<ts>` dir, then
// atomically rename over the existing path. If the existing path holds
// content but no .git (corruption), refuse to nuke it and return
// ErrCloneCorrupt.
func (s *gitOpsService) EnsureCloneReady(ctx context.Context, repoRecord *models.GitRepository) error {
	expectedPath := filepath.Join(s.repoBasePath, repoRecord.OrgID, repoRecord.ProjectID)

	// If the stored clone path doesn't match the current base path, update it.
	if repoRecord.ClonePath != expectedPath {
		slog.InfoContext(ctx, "clone path changed, updating",
			"project", repoRecord.ProjectID,
			"old", repoRecord.ClonePath,
			"new", expectedPath)
		repoRecord.ClonePath = expectedPath
	}

	// Check if the .git directory exists at the expected path.
	if _, err := os.Stat(filepath.Join(repoRecord.ClonePath, ".git")); err == nil {
		return nil // clone exists and looks valid
	}

	// .git is missing. Before re-cloning, refuse to delete a non-empty
	// working tree — that's the data-loss guard. If the directory exists
	// and has content (anything other than an empty dir or non-existent),
	// surface ErrCloneCorrupt to the caller.
	if hasContent(repoRecord.ClonePath) {
		slog.ErrorContext(ctx, "clone directory has content but no .git; refusing destructive re-clone",
			"project", repoRecord.ProjectID, "path", repoRecord.ClonePath)
		return ErrCloneCorrupt
	}

	slog.InfoContext(ctx, "clone directory missing, re-cloning",
		"project", repoRecord.ProjectID, "path", repoRecord.ClonePath)

	if err := s.cloneIntoPath(ctx, repoRecord); err != nil {
		return err
	}

	// Update branch and path in the DB.
	repoRecord.DefaultBranch = detectDefaultBranch(repoRecord.ClonePath)
	repoRecord.Status = "ready"
	repoRecord.ErrorMessage = ""
	if err := s.repo.Update(ctx, repoRecord); err != nil {
		slog.ErrorContext(ctx, "failed to update repo after re-clone", "error", err)
	}

	slog.InfoContext(ctx, "re-clone completed", "project", repoRecord.ProjectID)
	return nil
}

// hasContent returns true if path exists and has at least one entry. An
// empty dir or non-existent path is "no content" (safe to re-clone into).
func hasContent(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false // doesn't exist or unreadable → not "content we'd lose"
	}
	return len(entries) > 0
}

// cloneIntoPath does the git clone non-destructively: clone into a sibling
// `.tmpclone-<random>` directory, then atomically rename it onto the
// expected path. Any existing empty directory at the path is removed first
// so the rename can land. The temp dir is also cleaned on error so retries
// don't accumulate orphans.
func (s *gitOpsService) cloneIntoPath(ctx context.Context, repoRecord *models.GitRepository) error {
	parent := filepath.Dir(repoRecord.ClonePath)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	// Sibling tmpclone path. Random suffix so concurrent retries on the
	// same project don't collide if one is still running. cleanupOrphanTmpClones
	// at startup handles any survivors.
	suffix := make([]byte, 6)
	if _, err := cryptoRandRead(suffix); err != nil {
		return fmt.Errorf("random suffix: %w", err)
	}
	tmpPath := filepath.Join(parent, ".tmpclone-"+repoRecord.ProjectID+"-"+hex.EncodeToString(suffix))
	defer os.RemoveAll(tmpPath) //nolint:errcheck — cleanup best-effort on success too

	token, _, err := s.resolveToken(ctx, repoRecord)
	if err != nil {
		return err
	}
	askPassScript, err := createAskPassScript(token)
	if err != nil {
		return fmt.Errorf("create askpass script: %w", err)
	}
	defer os.Remove(askPassScript)

	cloneCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(cloneCtx, "git", "clone", repoRecord.RepoURL, tmpPath)
	cmd.Env = append(os.Environ(),
		"GIT_ASKPASS="+askPassScript,
		"GIT_TERMINAL_PROMPT=0",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("re-clone failed: %s", stderr.String())
	}

	// Drop any pre-existing empty directory so the rename can land.
	if entries, err := os.ReadDir(repoRecord.ClonePath); err == nil && len(entries) == 0 {
		_ = os.Remove(repoRecord.ClonePath)
	}

	if err := os.Rename(tmpPath, repoRecord.ClonePath); err != nil {
		return fmt.Errorf("rename tmpclone into place: %w", err)
	}
	return nil
}

// CleanupOrphanTmpClones removes any leftover `.tmpclone-*` directories
// under repoBasePath. Called once at server startup before pre-warm so a
// previous crash mid-clone doesn't leave the disk littered.
func (s *gitOpsService) CleanupOrphanTmpClones() {
	orgs, err := os.ReadDir(s.repoBasePath)
	if err != nil {
		return
	}
	for _, org := range orgs {
		if !org.IsDir() {
			continue
		}
		orgDir := filepath.Join(s.repoBasePath, org.Name())
		entries, err := os.ReadDir(orgDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".tmpclone-") {
				path := filepath.Join(orgDir, e.Name())
				if err := os.RemoveAll(path); err == nil {
					slog.Info("cleaned orphan tmpclone", "path", path)
				}
			}
		}
	}
}

// PreWarmClones reads every git_repositories row and ensures each clone is
// on disk. Concurrency-bounded by `workers`; failures are surfaced via
// per-row warning logs (caller's gauges already track pending/failed
// counts). Called once at server startup so cold-start traffic doesn't pay
// the multi-minute clone latency under the per-project mutex.
func (s *gitOpsService) PreWarmClones(ctx context.Context, workers int) (warmed, failed int) {
	repos, err := s.allRepos(ctx)
	if err != nil {
		slog.Error("pre-warm: list repos failed", "error", err)
		return 0, 0
	}
	if workers <= 0 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := range repos {
		repo := &repos[i]
		if repo.Status != "ready" {
			continue
		}
		// The per-org skills repo (sentinel project) is read/written via the
		// GitHub API only — never cloned. Skip it. See skills-repo-storage.md §10.
		if repo.ProjectID == models.SkillsRepoSentinelProjectID {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			lock := s.RepoLock(repo.ProjectID)
			lock.Lock()
			defer lock.Unlock()

			if err := s.EnsureCloneReady(ctx, repo); err != nil {
				slog.Warn("pre-warm: ensure clone failed",
					"project", repo.ProjectID, "org", repo.OrgID, "error", err)
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}
			mu.Lock()
			warmed++
			mu.Unlock()
		}()
	}
	wg.Wait()
	return warmed, failed
}

// allRepos paginates through git_repositories. The repo layer doesn't have
// a "list everything" method (intentionally — every other path is keyed on
// a specific project), so we go around it with a direct GORM query for
// pre-warm only.
func (s *gitOpsService) allRepos(ctx context.Context) ([]models.GitRepository, error) {
	type lister interface {
		ListAllReady(ctx context.Context) ([]models.GitRepository, error)
	}
	if l, ok := s.repo.(lister); ok {
		return l.ListAllReady(ctx)
	}
	return nil, nil
}

// runGitOutput runs a git command and returns its stdout. On failure, stderr
// is included in the returned error so caller logs show git's own diagnostic.
func runGitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", err
		}
		return "", fmt.Errorf("%s: %w", msg, err)
	}
	return string(out), nil
}

// ----- GitWorkspace surface -----
// These operate on gitOpsService internals (token resolution, clone, GitHub
// client, credential resolver) and are exposed to the artifacts feature
// through the GitOpsService port, so the artifacts package never imports
// gitrepo's concrete type.

// GitHubClient returns the GitHubClient wired into gitOpsService at
// construction (the artifact-store v2 save flow client).
func (s *gitOpsService) GitHubClient() GitHubClient { return s.gitHub }

// Resolver returns the credential resolver wired into gitOpsService.
func (s *gitOpsService) Resolver() credentials.Resolver { return s.resolver }

// PrepareAuthedEnv resolves the org's GitHub token and returns an env slice
// configured with GIT_ASKPASS, plus a cleanup func that removes the temp
// askpass script.
func (s *gitOpsService) PrepareAuthedEnv(ctx context.Context, repoRecord *models.GitRepository) ([]string, func(), error) {
	token, _, err := s.resolveToken(ctx, repoRecord)
	if err != nil {
		return nil, func() {}, err
	}
	askPass, err := createAskPassScript(token)
	if err != nil {
		return nil, func() {}, fmt.Errorf("askpass: %w", err)
	}
	cleanup := func() { _ = os.Remove(askPass) }
	env := append(os.Environ(),
		"GIT_ASKPASS="+askPass,
		"GIT_TERMINAL_PROMPT=0",
	)
	return env, cleanup, nil
}

// ResolveSaveIdentities returns (author, committer) identities for the save.
// Both are set to the credential identity (falling back to a default ASDLC
// name/email when the credential carries none).
func (s *gitOpsService) ResolveSaveIdentities(cred credentials.Credential) (*GitIdentity, *GitIdentity) {
	id := cred.Identity()
	if id.Name == "" {
		id.Name = "ASDLC"
	}
	if id.Email == "" {
		id.Email = "noreply@asdlc.dev"
	}
	gi := &GitIdentity{Name: id.Name, Email: id.Email}
	return gi, gi
}

// BestEffortPullDefaultBranch advances the local clone's HEAD to remote main
// so the next save's `git status` against HEAD reflects current remote state.
// The working-tree files are LEFT ALONE — they're the user's draft surface
// and we just wrote them on remote as the save's commit. We also refresh
// the index so `git status` doesn't report every working-tree file as
// "modified vs new HEAD."
//
// Implementation note: we use `update-ref` + `read-tree` rather than
// `merge --ff-only` because the working tree contains untracked or
// otherwise-modified files that merge would refuse to overwrite.
func (s *gitOpsService) BestEffortPullDefaultBranch(ctx context.Context, repoRecord *models.GitRepository) error {
	authedEnv, cleanup, err := s.PrepareAuthedEnv(ctx, repoRecord)
	if err != nil {
		return err
	}
	defer cleanup()
	clonePath := repoRecord.ClonePath
	branch := repoRecord.DefaultBranch

	// 1. Fetch the remote ref.
	cmd := exec.CommandContext(ctx, "git", "fetch", "origin", branch)
	cmd.Dir = clonePath
	cmd.Env = authedEnv
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git fetch: %s", stderr.String())
	}

	// 2. Read the new remote SHA.
	originSHA, err := runGitOutput(ctx, clonePath, "rev-parse", "origin/"+branch)
	if err != nil {
		return fmt.Errorf("rev-parse origin/%s: %w", branch, err)
	}
	originSHA = strings.TrimSpace(originSHA)

	// 3. Move local branch ref to origin's tip without touching working tree.
	cmd = exec.CommandContext(ctx, "git", "update-ref", "refs/heads/"+branch, originSHA)
	cmd.Dir = clonePath
	stderr.Reset()
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git update-ref refs/heads/%s: %s", branch, stderr.String())
	}

	// 4. Refresh the index to the new HEAD so `git status` against the working
	// tree shows only the user's local drafts as modified, not every file that
	// was renamed by the move.
	cmd = exec.CommandContext(ctx, "git", "read-tree", "HEAD")
	cmd.Dir = clonePath
	stderr.Reset()
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git read-tree HEAD: %s", stderr.String())
	}
	return nil
}
