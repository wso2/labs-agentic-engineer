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

package repo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"strings"
	"time"
)

// Mutate implements Workspace (design D5): it owns the ONE CAS retry loop
// every writer shares (the per-feature REST retry helpers are gone). Per
// attempt, under the EXCLUSIVE flock: fetch → base := branch tip → fn stages
// the overlay against Tx.Base() → throwaway-index write-tree → commit-tree →
// `push --force-with-lease=<branch>:<observedSHA>` (origin is the CAS
// arbiter) → local ref advanced only AFTER the push lands, so
// committed-local ≡ committed-remote at rest (design §11).
//
//   - Non-fast-forward push rejection → re-fetch + re-run fn against the new
//     base, bounded by opts.Retry (default 4 attempts, jittered backoff);
//     exhaustion surfaces ErrRefNotFastForward.
//   - Any error from fn aborts immediately with that error, no retry — the
//     caller's conflict/409 path.
//   - A no-op overlay (nothing staged, or staged content identical to base)
//     returns CommitResult{Changed: false} without committing or pushing.
func (e *Engine) Mutate(ctx context.Context, ref RepoRef, fn func(Tx) error, opts CommitOpts) (res CommitResult, err error) {
	defer func() { err = e.mapDiskErr(err) }()
	p, err := e.pathsFor(ref)
	if err != nil {
		return CommitResult{}, err
	}
	cloned, err := e.ensureMirror(ctx, ref, p)
	if err != nil {
		return CommitResult{}, err
	}
	policy := opts.Retry.withDefaults()
	var lastErr error
	for attempt := 0; attempt < policy.Attempts; attempt++ {
		if attempt > 0 {
			if err := sleepJittered(ctx, policy.Backoff, attempt-1); err != nil {
				return CommitResult{}, err
			}
		}
		res, err := e.mutateOnce(ctx, ref, p, fn, opts, cloned && attempt == 0)
		if err == nil {
			return res, nil
		}
		if !errors.Is(err, ErrRefNotFastForward) {
			return CommitResult{}, err
		}
		lastErr = err
	}
	return CommitResult{}, fmt.Errorf("repo mutate %s: %w (after %d attempts)", ref.RepoSlug, lastErr, policy.Attempts)
}

// mutateOnce is one CAS attempt under the exclusive flock.
func (e *Engine) mutateOnce(ctx context.Context, ref RepoRef, p repoPaths, fn func(Tx) error, opts CommitOpts, freshClone bool) (CommitResult, error) {
	release, err := e.locks.Lock(ctx, p.lockPath)
	if err != nil {
		return CommitResult{}, err
	}
	defer release()

	if !freshClone {
		if err := e.fetch(ctx, ref, p); err != nil {
			return CommitResult{}, err
		}
	}
	branch := defaultBranch(ref)
	baseSHA, err := e.resolveCommit(ctx, ref, p, "heads/"+branch)
	if err != nil {
		return CommitResult{}, err
	}

	t := &tx{base: &mirrorSnapshot{ctx: ctx, e: e, p: p, commit: baseSHA}}
	if err := fn(t); err != nil {
		return CommitResult{}, err // fn's own error: abort, no retry (the 409 path)
	}
	if len(t.ops) == 0 {
		return CommitResult{CommitSHA: baseSHA, Changed: false}, nil
	}

	newTree, err := e.buildTree(ctx, p, baseSHA, t.ops)
	if err != nil {
		return CommitResult{}, err
	}
	baseTree, err := e.git(ctx, execOpts{}, "--git-dir", p.gitDir, "rev-parse", baseSHA+"^{tree}")
	if err != nil {
		return CommitResult{}, fmt.Errorf("repo: resolve base tree: %w", err)
	}
	if newTree == strings.TrimSpace(string(baseTree)) {
		return CommitResult{CommitSHA: baseSHA, Changed: false}, nil
	}

	commitSHA, err := e.commitTree(ctx, p, newTree, baseSHA, opts)
	if err != nil {
		return CommitResult{}, err
	}
	if err := e.pushBranchCAS(ctx, ref, p, branch, baseSHA, commitSHA); err != nil {
		return CommitResult{}, err
	}
	// Origin accepted — only now advance the local ref (no persistent
	// unpushed local state, design §11). A failure here is deliberately not
	// surfaced: the mutation is durable on origin, the mirror is a
	// rebuildable cache, and the next fetch heals the local ref.
	if _, err := e.git(ctx, execOpts{}, "--git-dir", p.gitDir,
		"update-ref", "refs/heads/"+branch, commitSHA); err != nil {
		slog.WarnContext(ctx, "repo: advance local ref after push failed (next fetch heals)",
			"repo", ref.RepoSlug, "branch", branch, "error", err)
	}
	return CommitResult{CommitSHA: commitSHA, Changed: true}, nil
}

// buildTree materializes the op log into a new tree via a throwaway index:
// read-tree <base> then hash-object + update-index per op, then write-tree.
// The mirror is never checked out; blobs land straight in the object DB.
func (e *Engine) buildTree(ctx context.Context, p repoPaths, baseSHA string, ops []txOp) (string, error) {
	idx, err := os.CreateTemp(TmpDir(e.root), "index-*")
	if err != nil {
		return "", fmt.Errorf("repo: temp index: %w", err)
	}
	idxPath := idx.Name()
	_ = idx.Close()
	_ = os.Remove(idxPath) // read-tree wants to create it fresh
	defer os.Remove(idxPath)
	idxEnv := map[string]string{"GIT_INDEX_FILE": idxPath}

	if _, err := e.git(ctx, execOpts{env: idxEnv}, "--git-dir", p.gitDir, "read-tree", baseSHA); err != nil {
		return "", fmt.Errorf("repo: read-tree %s: %w", baseSHA, err)
	}
	for _, op := range ops {
		if err := validateTreePath(op.path); err != nil {
			return "", err
		}
		if op.delete {
			// `update-index --index-info` with mode 0 removes the entry and,
			// unlike --force-remove, needs no work tree (the mirror is bare).
			line := "0 0000000000000000000000000000000000000000\t" + op.path + "\n"
			if _, err := e.git(ctx, execOpts{env: idxEnv, stdin: []byte(line)}, "--git-dir", p.gitDir,
				"update-index", "--index-info"); err != nil {
				return "", fmt.Errorf("repo: delete %q: %w", op.path, err)
			}
			continue
		}
		blob, err := e.git(ctx, execOpts{stdin: op.content}, "--git-dir", p.gitDir,
			"hash-object", "-w", "--stdin")
		if err != nil {
			return "", fmt.Errorf("repo: hash-object %q: %w", op.path, err)
		}
		if _, err := e.git(ctx, execOpts{env: idxEnv}, "--git-dir", p.gitDir,
			"update-index", "--add", "--cacheinfo",
			"100644,"+strings.TrimSpace(string(blob))+","+op.path); err != nil {
			return "", fmt.Errorf("repo: stage %q: %w", op.path, err)
		}
	}
	tree, err := e.git(ctx, execOpts{env: idxEnv}, "--git-dir", p.gitDir, "write-tree")
	if err != nil {
		return "", fmt.Errorf("repo: write-tree: %w", err)
	}
	return strings.TrimSpace(string(tree)), nil
}

// commitTree creates the commit object with author/committer from opts
// (falling back to the AEP identity) supplied via env, never argv.
func (e *Engine) commitTree(ctx context.Context, p repoPaths, tree, parent string, opts CommitOpts) (string, error) {
	env := identityEnv(opts.Author, opts.Committer)
	out, err := e.git(ctx, execOpts{env: env}, "--git-dir", p.gitDir,
		"commit-tree", tree, "-p", parent, "-m", opts.Message)
	if err != nil {
		return "", fmt.Errorf("repo: commit-tree: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// pushBranchCAS pushes the new commit under --force-with-lease pinned to the
// observed remote SHA — the exact analog of the retired REST fast-forward-only
// ref update.
// A rejected lease maps to ErrRefNotFastForward.
func (e *Engine) pushBranchCAS(ctx context.Context, ref RepoRef, p repoPaths, branch, observedSHA, newSHA string) error {
	_, err := e.remoteGit(ctx, ref, execOpts{},
		"--git-dir", p.gitDir, "push",
		"--force-with-lease=refs/heads/"+branch+":"+observedSHA,
		"origin", newSHA+":refs/heads/"+branch)
	if err != nil {
		if isNonFastForward(err) {
			return fmt.Errorf("repo: push %s: %w: %w", branch, ErrRefNotFastForward, err)
		}
		return fmt.Errorf("repo: push %s: %w", branch, err)
	}
	return nil
}

// nonFFPatterns are the stderr shapes of a CAS-rejected push (the lease was
// stale — origin advanced behind our back).
var nonFFPatterns = []string{"stale info", "non-fast-forward", "fetch first"}

func isNonFastForward(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, pat := range nonFFPatterns {
		if strings.Contains(msg, pat) {
			return true
		}
	}
	return false
}

// validateTreePath rejects paths that could escape or corrupt the staged
// tree: absolute, empty, traversal, or NUL-bearing.
func validateTreePath(path string) error {
	if path == "" || strings.HasPrefix(path, "/") || strings.ContainsRune(path, 0) {
		return fmt.Errorf("repo: invalid tree path %q", path)
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" || seg == "." || seg == ".." || seg == ".git" {
			return fmt.Errorf("repo: invalid tree path %q", path)
		}
	}
	return nil
}

// identityEnv builds the author/committer env for commit-tree / tag, falling
// back to the AEP default identity (matching ResolveSaveIdentities' fallback).
func identityEnv(author, committer *GitIdentity) map[string]string {
	env := map[string]string{}
	a := identityOrDefault(author)
	c := identityOrDefault(committer)
	env["GIT_AUTHOR_NAME"] = a.Name
	env["GIT_AUTHOR_EMAIL"] = a.Email
	if a.Date != "" {
		env["GIT_AUTHOR_DATE"] = a.Date
	}
	env["GIT_COMMITTER_NAME"] = c.Name
	env["GIT_COMMITTER_EMAIL"] = c.Email
	if c.Date != "" {
		env["GIT_COMMITTER_DATE"] = c.Date
	}
	return env
}

func identityOrDefault(id *GitIdentity) GitIdentity {
	out := GitIdentity{Name: "AEP", Email: "noreply@aep.dev"}
	if id != nil {
		if id.Name != "" {
			out.Name = id.Name
		}
		if id.Email != "" {
			out.Email = id.Email
		}
		out.Date = id.Date
	}
	return out
}

// sleepJittered waits the policy's base delay for retry index i, jittered by
// ±50%, honouring ctx cancellation.
func sleepJittered(ctx context.Context, backoff []time.Duration, i int) error {
	if i >= len(backoff) {
		i = len(backoff) - 1
	}
	base := backoff[i]
	if base <= 0 {
		// A non-positive base delay (a caller passed a 0/negative Backoff
		// element) means "no wait" — and rand.N would panic on it. Retry
		// immediately, but still surface a canceled ctx.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	// Uniform in [base/2, base*3/2).
	d := base/2 + rand.N(base)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
