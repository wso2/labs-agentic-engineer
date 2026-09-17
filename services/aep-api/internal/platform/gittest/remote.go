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

// Package gittest is the real-git test harness. It gives git-flow tests a
// real git origin without Docker or network, so the
// save/discard/versioning flows run against genuine git object-store
// semantics in the fast `make test` lane.
//
// Two pieces (the Git-Data HTTP fake was retired with the REST git-object
// path — the gitfs Workspace engine + workspacetest supersede it):
//
//   - NewRemote(t): a bare repo in t.TempDir() acting as a file:// origin
//     (clone/fetch/push work unchanged; GIT_ASKPASS never fires on file
//     remotes). Seed/Tag/read helpers arrange and assert via git plumbing.
//   - NewStub(t): a route-registry httptest fake for the JSON endpoints —
//     repos, issues, milestones, webhooks, and the single POST /graphql
//     endpoint — where a git-backed fake would be over-engineering.
//
// Hermeticity: every git subprocess runs with GIT_CONFIG_GLOBAL/SYSTEM pointed
// at os.DevNull and a fixed identity supplied via env, so a run depends on
// neither the developer's ~/.gitconfig nor an ambient committer identity, and
// touches no git config file. No `-short` skip is needed — local git is
// millisecond-fast.
package gittest

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// defaultBranch is the branch every remote is initialised on. The save flow
// resolves "heads/" + repoRecord.DefaultBranch, which is "main" in this stack.
const defaultBranch = "main"

const (
	// defaultActorName/Email identify the harness's own commits and tags.
	defaultActorName  = "gittest"
	defaultActorEmail = "gittest@aep.test"

	// fixedDate keeps the harness's own commits deterministic (git raw format:
	// "<unix-seconds> <tz>"), so seeded commit SHAs are stable across runs.
	// 1767225600 == 2026-01-01T00:00:00Z.
	fixedDate = "1767225600 +0000"
)

// Remote is a bare git repository in t.TempDir() serving as a file:// origin.
// It is safe for concurrent plumbing: object writes are content-addressed and
// atomic, ref updates take lockfiles, and every index operation uses its own
// GIT_INDEX_FILE.
type Remote struct {
	dir string // absolute path to the bare repo (the GIT_DIR)
}

// Option customises NewRemote.
type Option func(*config)

type config struct {
	seedFiles map[string]string
	seedMsg   string
}

// WithSeed seeds the initial commit on main with the given repo-relative files
// (path → content) and commit message. Without it, the initial commit is an
// empty tree — enough for main to exist so clones and GetRef succeed.
func WithSeed(files map[string]string, msg string) Option {
	return func(c *config) {
		c.seedFiles = files
		c.seedMsg = msg
	}
}

// NewRemote creates a bare repo on the default branch `main` with an initial
// commit, and returns a Remote whose file:// URL works with git clone/fetch.
func NewRemote(t *testing.T, opts ...Option) *Remote {
	t.Helper()
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}
	dir := filepath.Join(t.TempDir(), "origin.git")
	if _, err := runGit("", nil, nil, "init", "--bare", "-b", defaultBranch, dir); err != nil {
		t.Fatalf("gittest: init bare repo: %v", err)
	}
	r := &Remote{dir: dir}
	// Every push into this repo makes receive-pack run `git gc --auto`, which
	// with gc.autoDetach (on by default) forks a process that outlives the
	// push. A test that pushes hard enough finishes while that child is still
	// writing under objects/, and t.TempDir's RemoveAll then fails the test
	// with "directory not empty" — after the body passed. A fixture repo lives
	// for one test and never needs maintenance, so switch both off.
	for _, kv := range [][2]string{{"gc.auto", "0"}, {"maintenance.auto", "false"}} {
		r.mustExec(t, nil, nil, "config", kv[0], kv[1])
	}
	msg := cfg.seedMsg
	if msg == "" {
		msg = "initial commit"
	}
	sha := r.commit(t, "", cfg.seedFiles, msg)
	r.mustExec(t, nil, nil, "update-ref", "refs/heads/"+defaultBranch, sha)
	return r
}

// URL returns the file:// URL of the bare repo (three-slash form, since the
// path is absolute). GIT_ASKPASS never fires on file remotes, so no credential
// plumbing is needed.
func (r *Remote) URL() string { return "file://" + r.dir }

// Dir returns the absolute path of the bare repo (the GIT_DIR). Useful for
// tests that need to drive git directly against the origin.
func (r *Remote) Dir() string { return r.dir }

// Seed commits files (path → content) onto main directly in the bare repo via a
// temporary index, and returns the new commit SHA.
func (r *Remote) Seed(t *testing.T, files map[string]string, msg string) string {
	t.Helper()
	parent := r.HeadSHA(t)
	sha := r.commit(t, parent, files, msg)
	r.mustExec(t, nil, nil, "update-ref", "refs/heads/"+defaultBranch, sha)
	return sha
}

// Remove commits a deletion of the given blob paths onto main directly in the
// bare repo (temp index; deletes staged via `update-index --index-info` mode 0,
// the bare-repo-safe form) and returns the new commit SHA.
func (r *Remote) Remove(t *testing.T, msg string, paths ...string) string {
	t.Helper()
	parent := r.HeadSHA(t)
	idx := filepath.Join(t.TempDir(), "index")
	idxEnv := map[string]string{"GIT_INDEX_FILE": idx}
	r.mustExec(t, idxEnv, nil, "read-tree", parent)
	var stdin strings.Builder
	for _, p := range paths {
		stdin.WriteString("0 0000000000000000000000000000000000000000\t" + p + "\n")
	}
	r.mustExec(t, idxEnv, []byte(stdin.String()), "update-index", "--index-info")
	tree := strings.TrimSpace(r.mustExec(t, idxEnv, nil, "write-tree"))
	sha := strings.TrimSpace(r.mustExec(t, nil, nil, "commit-tree", tree, "-p", parent, "-m", msg))
	r.mustExec(t, nil, nil, "update-ref", "refs/heads/"+defaultBranch, sha)
	return sha
}

// Tag creates an annotated tag with the given message on the current main tip.
func (r *Remote) Tag(t *testing.T, name, msg string) {
	t.Helper()
	head := r.HeadSHA(t)
	r.mustExec(t, nil, nil, "tag", "-a", name, head, "-m", msg)
}

// HeadSHA returns the current main tip SHA.
func (r *Remote) HeadSHA(t *testing.T) string {
	t.Helper()
	return strings.TrimSpace(r.mustExec(t, nil, nil, "rev-parse", "refs/heads/"+defaultBranch))
}

// FileAt returns the exact bytes of a blob at ref:path (e.g. FileAt(t, "main",
// "specs/requirements/main.md") or FileAt(t, "v1", ...)). No trimming — the
// stored content is returned verbatim.
func (r *Remote) FileAt(t *testing.T, ref, path string) string {
	t.Helper()
	return r.mustExec(t, nil, nil, "cat-file", "blob", ref+":"+path)
}

// Tags returns the tag names in the bare repo (lexicographically sorted by
// `git tag -l`).
func (r *Remote) Tags(t *testing.T) []string {
	t.Helper()
	out := strings.TrimSpace(r.mustExec(t, nil, nil, "tag", "-l"))
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// Clone produces a working clone of the remote in t.TempDir() and returns its
// path. The clone's default branch is main. Handy for artifacts tests that need
// a real ClonePath to run git-exec paths against.
func (r *Remote) Clone(t *testing.T) string {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "clone")
	if _, err := runGit("", nil, nil, "clone", r.URL(), dest); err != nil {
		t.Fatalf("gittest: clone %s: %v", r.URL(), err)
	}
	return dest
}

// commit builds a commit from `files` layered over `parent` (empty for a root
// commit) using a fresh temporary index, and returns the commit SHA WITHOUT
// moving any ref. Callers update refs/heads/main themselves.
func (r *Remote) commit(t *testing.T, parent string, files map[string]string, msg string) string {
	t.Helper()
	idx := filepath.Join(t.TempDir(), "index")
	idxEnv := map[string]string{"GIT_INDEX_FILE": idx}
	if parent != "" {
		r.mustExec(t, idxEnv, nil, "read-tree", parent)
	}
	for _, p := range sortedKeys(files) {
		blob := strings.TrimSpace(r.mustExec(t, nil, []byte(files[p]), "hash-object", "-w", "--stdin"))
		r.mustExec(t, idxEnv, nil, "update-index", "--add", "--cacheinfo", "100644,"+blob+","+p)
	}
	tree := strings.TrimSpace(r.mustExec(t, idxEnv, nil, "write-tree"))
	args := []string{"commit-tree", tree, "-m", msg}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	return strings.TrimSpace(r.mustExec(t, nil, nil, args...))
}

// exec runs a git plumbing command against the bare repo (GIT_DIR set), with
// `env` overlaid on the hermetic base env. It never touches *testing.T, so
// callers that need the raw error (expected-failure probes) can map it.
func (r *Remote) exec(env map[string]string, stdin []byte, args ...string) (string, error) {
	full := map[string]string{"GIT_DIR": r.dir}
	for k, v := range env {
		full[k] = v
	}
	return runGit("", full, stdin, args...)
}

// mustExec is exec for arrange/assert helpers: it fails the test on error.
func (r *Remote) mustExec(t *testing.T, env map[string]string, stdin []byte, args ...string) string {
	t.Helper()
	out, err := r.exec(env, stdin, args...)
	if err != nil {
		t.Fatalf("gittest: %v", err)
	}
	return out
}

// runGit executes git with a hermetic environment (no user/system config, fixed
// identity) plus `env`. dir sets the working directory (empty = inherit).
// Returns raw stdout (untrimmed, so blob bytes survive) and, on failure, an
// error carrying stderr.
func runGit(dir string, env map[string]string, stdin []byte, args ...string) (string, error) {
	base := hermeticEnv()
	for k, v := range env {
		base[k] = v
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = flattenEnv(base)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("git %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// hermeticEnv is the base environment for every git subprocess. Redirecting
// GIT_CONFIG_GLOBAL/SYSTEM to os.DevNull makes git ignore the developer's
// config (identity comes from the GIT_*_NAME/EMAIL/DATE vars below), which is
// why NewRemote must pass `-b main` explicitly rather than relying on
// init.defaultBranch.
func hermeticEnv() map[string]string {
	return map[string]string{
		"PATH":                os.Getenv("PATH"),
		"HOME":                os.Getenv("HOME"),
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_SYSTEM":   os.DevNull,
		"GIT_TERMINAL_PROMPT": "0",
		"GIT_AUTHOR_NAME":     defaultActorName,
		"GIT_AUTHOR_EMAIL":    defaultActorEmail,
		"GIT_COMMITTER_NAME":  defaultActorName,
		"GIT_COMMITTER_EMAIL": defaultActorEmail,
		"GIT_AUTHOR_DATE":     fixedDate,
		"GIT_COMMITTER_DATE":  fixedDate,
	}
}

func flattenEnv(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
