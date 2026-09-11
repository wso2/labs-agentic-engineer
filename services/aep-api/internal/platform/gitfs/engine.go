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

package gitfs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Engine is the disk-backed implementation of Workspace + SnapshotProvider:
// git plumbing over bare mirrors under one workspace root (design D2). It is
// stateless apart from the root path — safe for concurrent use, and multiple
// Engine instances (processes, replicas) may share one root because every
// object-DB-touching section runs under the per-repo flock and every
// materialization is tmp/ + rename.
type Engine struct {
	root    string
	locks   Locker
	askpass string
	// execHook, when set (tests only, via export_test.go), observes every
	// git invocation's argv + env before it runs. It is the seam for the
	// credential-hygiene assertions and crash injection.
	execHook func(args []string, env []string)
	// diskUsagePct is the last volume used% recorded by the reaper (-1 =
	// unknown). Ensure refuses new snapshot materialization at >= 90%.
	diskUsagePct atomic.Int32
	// onENOSPC, when set (composition root → reaper.ForceSweep), runs on
	// detected ENOSPC before DiskFullError is returned.
	onENOSPC func()
}

// Compile-time port compliance.
var (
	_ Workspace        = (*Engine)(nil)
	_ SnapshotProvider = (*Engine)(nil)
)

// RootLayout reports whether New found an existing workspace root directory
// or created it (R8b boot identity — affinity-scatter diagnosis).
type RootLayout string

const (
	RootFound   RootLayout = "found"
	RootCreated RootLayout = "created"
)

// New builds an Engine rooted at root: creates repos/, tmp/, trash/ and
// writes the askpass shim. root is made absolute so git child processes are
// immune to cwd changes. layout is RootFound when abs already existed as a
// directory before layout creation, RootCreated when New created it.
func New(root string) (*Engine, RootLayout, error) {
	abs, err := absPath(root)
	if err != nil {
		return nil, "", fmt.Errorf("gitfs: resolve root %q: %w", root, err)
	}
	layout := RootCreated
	if st, err := os.Stat(abs); err == nil {
		if !st.IsDir() {
			return nil, "", fmt.Errorf("gitfs: root %q exists and is not a directory", abs)
		}
		layout = RootFound
	} else if !os.IsNotExist(err) {
		return nil, "", fmt.Errorf("gitfs: stat root %q: %w", abs, err)
	}
	for _, d := range []string{ReposDir(abs), TmpDir(abs), TrashDir(abs)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, "", fmt.Errorf("gitfs: create %s: %w", d, err)
		}
	}
	shim, err := writeAskpassShim(abs)
	if err != nil {
		return nil, "", err
	}
	e := &Engine{root: abs, locks: flockLocker{}, askpass: shim}
	e.diskUsagePct.Store(-1)
	return e, layout, nil
}

// Root returns the absolute workspace root the engine operates under.
func (e *Engine) Root() string { return e.root }

// SetDiskUsagePct records the workspace volume pressure percentage (0–100)
// from the reaper's last statfs read — max of byte used% and inode used%.
// Used by Ensure admission control.
func (e *Engine) SetDiskUsagePct(pct int) { e.diskUsagePct.Store(int32(pct)) }

// DiskUsagePct returns the last recorded pressure percentage, or 0 when unknown.
func (e *Engine) DiskUsagePct() int {
	v := e.diskUsagePct.Load()
	if v < 0 {
		return 0
	}
	return int(v)
}

// SetOnENOSPC registers the emergency handler invoked when mapDiskErr detects
// ENOSPC (composition root wires reaper.ForceSweep). Pass nil to clear.
func (e *Engine) SetOnENOSPC(fn func()) { e.onENOSPC = fn }

// mapDiskErr translates ENOSPC into DiskFullError after invoking onENOSPC.
// Non-ENOSPC errors (and nil) pass through unchanged.
func (e *Engine) mapDiskErr(err error) error {
	if err == nil || !isENOSPC(err) {
		return err
	}
	if e.onENOSPC != nil {
		e.onENOSPC()
	}
	return &DiskFullError{Root: e.root, UsedPct: e.DiskUsagePct()}
}

func absPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	return filepath.Abs(p)
}

// trashDest derives a fresh unique trash/<id> destination: a sortable
// nanosecond timestamp prefix plus random suffix (a ULID without adding a
// dependency — the reaper only needs uniqueness and rough age ordering).
func trashDest(root string) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	id := fmt.Sprintf("%016x-%s", uint64(time.Now().UnixNano()), hex.EncodeToString(b[:]))
	return filepath.Join(TrashDir(root), id)
}

// ----- git execution (hermetic child env, design D2/§8) -----

// forcedConfig is git config the engine imposes on every child, whatever the
// mirror's own config file says.
//
// maintenance.auto is here because the reaper must be the only thing that
// repacks a shared mirror — it is the only thing that does so under the
// per-repo EX flock. Git's default is the opposite: `fetch`, `push` and
// `commit` all end by spawning `git maintenance run --auto --detach`, which
// double-forks, so it goes on rewriting the object DB after the engine's
// critical section has closed and the lock is gone. Two concurrent repacks on
// one object DB is precisely the state the flock exists to prevent, and the
// detached one holds nothing. The value has to be maintenance.auto: git's auto
// strategy is geometric-repack, which never consults gc.auto.
//
// It is forced through the environment rather than stamped into each mirror
// because the shared volume outlives any single release. A stamp written at
// clone time cannot reach a mirror that was cloned before the rule existed;
// env-supplied config outranks the repo's own file and so covers every mirror
// already on disk.
var forcedConfig = []gitConfigRule{
	{"maintenance.auto", "false"},
}

// gitConfigRule is one config key/value the engine imposes on git children.
type gitConfigRule struct{ key, value string }

// forcedConfigEnv renders forcedConfig as git's GIT_CONFIG_COUNT /
// GIT_CONFIG_KEY_n / GIT_CONFIG_VALUE_n triplets (equivalent to `git -c`, and
// higher precedence than any config file). The indices are derived from the
// slice, so adding a rule above is the whole change — there is no count to
// keep in step by hand.
//
// MINIMUM GIT 2.31, which is where these variables were added. Below it they
// are inert and every rule above silently stops applying. The image installs
// Alpine's unversioned `git` package (services/aep-api/Dockerfile) and is not
// pinned to a floor: 2.31 shipped in March 2021, the image currently resolves
// 2.47, and pinning an apk version would break the build the first time the
// package is superseded — a certainty, against a regression that is not. What
// guards it instead is TestGitResolvesEveryForcedConfigRule, which asks the
// git binary on the box what it RESOLVES for every rule here: on a git too old
// to read this environment, that test fails rather than the platform quietly
// losing the rule.
func forcedConfigEnv() map[string]string {
	env := map[string]string{"GIT_CONFIG_COUNT": strconv.Itoa(len(forcedConfig))}
	for i, c := range forcedConfig {
		n := strconv.Itoa(i)
		env["GIT_CONFIG_KEY_"+n] = c.key
		env["GIT_CONFIG_VALUE_"+n] = c.value
	}
	return env
}

// baseEnv is the scrubbed environment every git child gets: no user/system
// config, no terminal prompts, C locale for machine-stable output, HOME
// pointed into tmp/ so nothing ambient leaks in, plus the forcedConfig rules
// the engine imposes on every mirror.
func (e *Engine) baseEnv() map[string]string {
	env := map[string]string{
		"PATH":                os.Getenv("PATH"),
		"HOME":                TmpDir(e.root),
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_SYSTEM":   os.DevNull,
		"GIT_TERMINAL_PROMPT": "0",
		"LC_ALL":              "C",
	}
	for k, v := range forcedConfigEnv() {
		env[k] = v
	}
	return env
}

// execOpts parametrizes one git invocation.
type execOpts struct {
	env   map[string]string // overlaid on the hermetic base env
	stdin []byte
}

// git runs one git command with the hermetic env (+overlay), returning raw
// stdout. Errors wrap the exit error with the command line and trimmed
// stderr — tokens never appear in either (askpass keeps them out of argv).
func (e *Engine) git(ctx context.Context, opts execOpts, args ...string) ([]byte, error) {
	cmd := e.buildCmd(ctx, opts, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("git %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// gitStream runs one git command handing its stdout to consume as a stream
// (used by `git archive` so snapshot materialization never buffers a whole
// tree in memory).
func (e *Engine) gitStream(ctx context.Context, opts execOpts, consume func(io.Reader) error, args ...string) error {
	cmd := e.buildCmd(ctx, opts, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("git %s: stdout pipe: %w", strings.Join(args, " "), err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("git %s: start: %w", strings.Join(args, " "), err)
	}
	consumeErr := consume(out)
	// Drain so the child never blocks on a full pipe, then reap it.
	_, _ = io.Copy(io.Discard, out)
	waitErr := cmd.Wait()
	if waitErr != nil {
		return fmt.Errorf("git %s: %w: %s",
			strings.Join(args, " "), waitErr, strings.TrimSpace(stderr.String()))
	}
	return consumeErr
}

func (e *Engine) buildCmd(ctx context.Context, opts execOpts, args ...string) *exec.Cmd {
	env := e.baseEnv()
	for k, v := range opts.env {
		env[k] = v
	}
	flat := make([]string, 0, len(env))
	for _, k := range sortedEnvKeys(env) {
		flat = append(flat, k+"="+env[k])
	}
	if e.execHook != nil {
		e.execHook(append([]string{"git"}, args...), flat)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = flat
	if opts.stdin != nil {
		cmd.Stdin = bytes.NewReader(opts.stdin)
	}
	return cmd
}

func sortedEnvKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// remoteGit runs one remote-touching git op (clone/fetch/push) with a token
// minted immediately before it (design D7). A mid-op auth failure re-mints
// once and retries the op.
func (e *Engine) remoteGit(ctx context.Context, ref RepoRef, opts execOpts, args ...string) ([]byte, error) {
	run := func() ([]byte, error) {
		credEnv, err := e.credEnv(ctx, ref)
		if err != nil {
			return nil, err
		}
		merged := execOpts{stdin: opts.stdin, env: map[string]string{}}
		for k, v := range opts.env {
			merged.env[k] = v
		}
		for k, v := range credEnv {
			merged.env[k] = v
		}
		return e.git(ctx, merged, args...)
	}
	out, err := run()
	if err != nil && ref.Cred != nil && isAuthFailure(err) {
		out, err = run() // re-mint once + retry (token revoked/expired mid-op)
	}
	return out, err
}

// ----- mirror lifecycle -----

// ensureMirror guarantees the bare mirror exists, cloning it atomically on
// demand: `git clone --mirror` into tmp/ staging, gc.auto=0 +
// repack.writeBitmaps=false stamped, then os.Rename into the canonical path —
// a crash mid-clone leaves only tmp/ debris, never a half-populated mirror.
// Returns whether this call cloned (a fresh clone is fresh — callers skip the
// next fetch).
func (e *Engine) ensureMirror(ctx context.Context, ref RepoRef, p repoPaths) (cloned bool, err error) {
	if mirrorExists(p.gitDir) {
		return false, nil
	}
	if err := os.MkdirAll(p.repoDir, 0o755); err != nil {
		return false, fmt.Errorf("gitfs: create repo dir: %w", err)
	}
	release, err := e.locks.Lock(ctx, p.lockPath)
	if err != nil {
		return false, err
	}
	defer release()
	if mirrorExists(p.gitDir) { // lost the clone race — someone else did it
		return false, nil
	}
	staging, err := os.MkdirTemp(TmpDir(e.root), "clone-*")
	if err != nil {
		return false, fmt.Errorf("gitfs: clone staging: %w", err)
	}
	defer os.RemoveAll(staging) // no-op debris cleanup after a successful rename
	stagingGit := filepath.Join(staging, "git")
	if _, err := e.remoteGit(ctx, ref, execOpts{}, "clone", "--mirror", ref.CloneURL, stagingGit); err != nil {
		return false, fmt.Errorf("gitfs: mirror clone %s: %w", ref.RepoSlug, err)
	}
	// gc.auto=0 turns off the classic `gc --auto` path only. It is NOT what
	// keeps automatic maintenance off a shared mirror: git's auto strategy is
	// geometric-repack, which never reads gc.auto. That is closed by
	// forcedConfig (maintenance.auto=false) on every child instead, which also
	// covers mirrors cloned before this rule existed. The stamp stays as the
	// narrower belt-and-braces — a mirror handed to a git that still routes
	// through gc --auto must not gc either, since the reaper's maintainRepos
	// pass owns repack/prune/pack-refs under the EX flock (never git gc: the
	// gc.pid hostname trap).
	if _, err := e.git(ctx, execOpts{}, "--git-dir", stagingGit, "config", "gc.auto", "0"); err != nil {
		return false, err
	}
	// Bitmaps are pure overhead: nothing clones FROM these mirrors.
	if _, err := e.git(ctx, execOpts{}, "--git-dir", stagingGit, "config", "repack.writeBitmaps", "false"); err != nil {
		return false, err
	}
	// clone --mirror marks the remote mirror=true, which forbids the
	// explicit-refspec pushes Mutate/Tag rely on ("--mirror can't be combined
	// with refspecs"). The engine always passes explicit refspecs on fetch
	// AND push, so the mirror property carries no behaviour we use.
	if _, err := e.git(ctx, execOpts{}, "--git-dir", stagingGit, "config", "remote.origin.mirror", "false"); err != nil {
		return false, err
	}
	if err := os.Rename(stagingGit, p.gitDir); err != nil {
		if mirrorExists(p.gitDir) {
			return false, nil // concurrent clone won the rename — fine
		}
		return false, fmt.Errorf("gitfs: publish mirror: %w", err)
	}
	return true, nil
}

func mirrorExists(gitDir string) bool {
	_, err := os.Stat(filepath.Join(gitDir, "HEAD"))
	return err == nil
}

// fetch refreshes all branches and tags from origin (a mirror fetch).
// Caller MUST hold the exclusive flock.
func (e *Engine) fetch(ctx context.Context, ref RepoRef, p repoPaths) error {
	_, err := e.remoteGit(ctx, ref, execOpts{},
		"--git-dir", p.gitDir, "fetch", "--prune", "origin",
		"+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*")
	if err != nil {
		return fmt.Errorf("gitfs: fetch %s: %w", ref.RepoSlug, err)
	}
	return nil
}

// freshenFor implements the freshness rule (design brief): reads addressed
// by branch/tag name fetch first; reads addressed by raw 40-hex shas use
// local objects, fetching only when an object is missing. cloned skips the
// fetch a fresh clone already implies.
func (e *Engine) freshenFor(ctx context.Context, ref RepoRef, p repoPaths, cloned bool, ats ...string) error {
	if cloned {
		return nil
	}
	needs := false
	for _, at := range ats {
		if !isHex40(at) {
			needs = true // symbolic addressing (branch/tag/"") → fetch first
			break
		}
	}
	if !needs {
		for _, at := range ats {
			ok, err := e.objectExists(ctx, ref, p, at)
			if err != nil {
				return err
			}
			if !ok {
				needs = true
				break
			}
		}
	}
	if !needs {
		return nil
	}
	release, err := e.locks.Lock(ctx, p.lockPath)
	if err != nil {
		return err
	}
	defer release()
	return e.fetch(ctx, ref, p)
}

// objectExists reports whether sha resolves to a commit in the local object
// DB (a pure object read — shared flock).
func (e *Engine) objectExists(ctx context.Context, ref RepoRef, p repoPaths, sha string) (bool, error) {
	release, err := e.locks.RLock(ctx, p.lockPath)
	if err != nil {
		return false, err
	}
	defer release()
	_, gerr := e.git(ctx, execOpts{}, "--git-dir", p.gitDir, "cat-file", "-e", sha+"^{commit}")
	return gerr == nil, nil
}

// ----- ref resolution -----

// defaultBranch applies the "main" fallback.
func defaultBranch(ref RepoRef) string {
	if ref.DefaultBranch == "" {
		return "main"
	}
	return ref.DefaultBranch
}

// atExpr normalizes an `at` address into a rev-parse expression: "" → the
// default branch, "tags/X"/"heads/X" → fully qualified, raw shas and bare
// names pass through (git's refname resolution order handles bare tag /
// branch names).
func atExpr(ref RepoRef, at string) string {
	switch {
	case at == "":
		return "refs/heads/" + defaultBranch(ref)
	case isHex40(at):
		return at
	case strings.HasPrefix(at, "tags/"), strings.HasPrefix(at, "heads/"):
		return "refs/" + at
	default:
		return at
	}
}

// resolveCommit resolves `at` to a commit sha, peeling annotated tags.
// Only a genuinely missing ref/object maps to ErrRefNotFound (`--verify
// --quiet` exits 1 for "not a valid object name"); repo-level failures —
// trashed/corrupt mirror (exit 128), killed subprocess — surface as plain
// errors so callers that treat "ref not found" as a valid state (an empty
// repo, a 404) never mistake an infrastructure failure for one. Caller must
// hold a flock (shared is enough — this is a pure ref/object read).
func (e *Engine) resolveCommit(ctx context.Context, ref RepoRef, p repoPaths, at string) (string, error) {
	expr := atExpr(ref, at)
	out, err := e.git(ctx, execOpts{}, "--git-dir", p.gitDir,
		"rev-parse", "--verify", "--quiet", "--end-of-options", expr+"^{commit}")
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", fmt.Errorf("gitfs: resolve %q in %s: %w: %w", at, ref.RepoSlug, ErrRefNotFound, err)
		}
		return "", fmt.Errorf("gitfs: resolve %q in %s: %w", at, ref.RepoSlug, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ----- mirror maintenance (consumed by the reaper) -----

// maintainLockTimeout bounds EX flock acquisition for MaintainMirror only
// (R4). Git repack/prune/pack-refs use the caller ctx — never this budget —
// so a contended lock skips within ~2s without SIGKILLing a slow maintain.
const maintainLockTimeout = 2 * time.Second

// MaintainMirror runs the reaper's git maintenance sequence under the EX
// flock: repack -ad --quiet, prune --expire=2.hours.ago, pack-refs --all --prune.
// Never git gc. Never git maintenance --task=loose-objects.
// Lock acquisition is bounded by maintainLockTimeout; git work uses ctx.
func (e *Engine) MaintainMirror(ctx context.Context, ref RepoRef) error {
	p, err := e.pathsFor(ref)
	if err != nil {
		return err
	}
	if !mirrorExists(p.gitDir) {
		return nil
	}
	lockCtx, cancel := context.WithTimeout(ctx, maintainLockTimeout)
	release, err := e.locks.Lock(lockCtx, p.lockPath)
	cancel()
	if err != nil {
		return err
	}
	defer release()
	if _, err := e.git(ctx, execOpts{}, "--git-dir", p.gitDir, "repack", "-ad", "--quiet"); err != nil {
		return fmt.Errorf("gitfs: repack %s: %w", ref.RepoSlug, err)
	}
	if _, err := e.git(ctx, execOpts{}, "--git-dir", p.gitDir, "prune", "--expire=2.hours.ago"); err != nil {
		return fmt.Errorf("gitfs: prune %s: %w", ref.RepoSlug, err)
	}
	if _, err := e.git(ctx, execOpts{}, "--git-dir", p.gitDir, "pack-refs", "--all", "--prune"); err != nil {
		return fmt.Errorf("gitfs: pack-refs %s: %w", ref.RepoSlug, err)
	}
	return nil
}

// CountObjects returns loose-object and pack counts for a mirror (count-objects -v).
func (e *Engine) CountObjects(ctx context.Context, ref RepoRef) (loose, packs int, err error) {
	p, err := e.pathsFor(ref)
	if err != nil {
		return 0, 0, err
	}
	out, err := e.git(ctx, execOpts{}, "--git-dir", p.gitDir, "count-objects", "-v")
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "count: "):
			loose, _ = strconv.Atoi(strings.TrimPrefix(line, "count: "))
		case strings.HasPrefix(line, "packs: "):
			packs, _ = strconv.Atoi(strings.TrimPrefix(line, "packs: "))
		}
	}
	return loose, packs, nil
}

// ----- trash primitives (consumed by the reaper, design D12) -----

// TrashRepo renames the repo's whole on-disk subtree (git/, repo.lock,
// snapshots/) into trash/<id> — the O(1) phase-1 of the two-phase delete. A
// missing subtree is a no-op. Mid-flight readers keep working through open
// fds (POSIX inode semantics); the next engine op on the ref self-heals by
// re-cloning.
func (e *Engine) TrashRepo(ctx context.Context, ref RepoRef) error {
	p, err := e.pathsFor(ref)
	if err != nil {
		return err
	}
	if _, err := os.Stat(p.repoDir); os.IsNotExist(err) {
		return nil
	}
	// Serialize with in-flight critical sections; the flock file moves with
	// the subtree, which is fine — held locks live on the open fd.
	release, err := e.locks.Lock(ctx, p.lockPath)
	if err != nil {
		return err
	}
	defer release()
	dest := trashDest(e.root)
	if err := os.Rename(p.repoDir, dest); err != nil {
		if os.IsNotExist(err) {
			return nil // concurrently trashed
		}
		return fmt.Errorf("gitfs: trash repo %s: %w", ref.RepoSlug, err)
	}
	return nil
}

// TrashOrg renames the org's whole repos/<orgId> subtree (all projects,
// including _skills) into trash/<id>. Missing subtree is a no-op.
func (e *Engine) TrashOrg(_ context.Context, orgID string) error {
	dir, err := OrgDir(e.root, orgID)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	if err := os.Rename(dir, trashDest(e.root)); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("gitfs: trash org %s: %w", orgID, err)
	}
	return nil
}
