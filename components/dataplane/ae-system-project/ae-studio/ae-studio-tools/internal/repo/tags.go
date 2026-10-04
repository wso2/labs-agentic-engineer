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
	"fmt"
	"strings"
	"time"
)

// Tags, ported from aep-api's gitfs/tags.go (Task 4.2a; behaviour 1:1, 05 §3).
// The diff surface stays behind: nothing in the pod needs it.

// Tag implements Workspace (design §10): annotated tag + push, under the
// EXCLUSIVE flock. The fetch precheck narrows the collision window; origin's
// push rejection closes it — either path returns ErrTagAlreadyExists so the
// caller can recompute the next name and retry.
//
//deadcode:keep wired in Task 4.2b (create-tag)
func (e *Engine) Tag(ctx context.Context, ref RepoRef, spec TagSpec) error {
	if spec.Name == "" {
		return fmt.Errorf("repo: empty tag name")
	}
	p, err := e.pathsFor(ref)
	if err != nil {
		return err
	}
	cloned, err := e.ensureMirror(ctx, ref, p)
	if err != nil {
		return err
	}
	release, err := e.locks.Lock(ctx, p.lockPath)
	if err != nil {
		return err
	}
	defer release()

	if !cloned {
		if err := e.fetch(ctx, ref, p); err != nil {
			return err
		}
	}
	// Precheck: the fetched view already carries the name → collide early.
	if _, gerr := e.git(ctx, execOpts{}, "--git-dir", p.gitDir,
		"rev-parse", "--verify", "--end-of-options", "refs/tags/"+spec.Name); gerr == nil {
		return fmt.Errorf("repo: tag %q: %w", spec.Name, ErrTagAlreadyExists)
	}

	target, err := e.resolveCommit(ctx, ref, p, spec.Target)
	if err != nil {
		return err
	}
	env := identityEnv(spec.Tagger, spec.Tagger) // tagger = committer identity
	if _, err := e.git(ctx, execOpts{env: env}, "--git-dir", p.gitDir,
		"tag", "-a", spec.Name, "-m", spec.Message, target); err != nil {
		return fmt.Errorf("repo: create tag %q: %w", spec.Name, err)
	}
	if _, err := e.remoteGit(ctx, ref, execOpts{}, "--git-dir", p.gitDir,
		"push", "origin", "refs/tags/"+spec.Name+":refs/tags/"+spec.Name); err != nil {
		// Roll the local tag back so the mirror never claims a tag origin
		// refused — the mirror must stay a faithful cache of origin.
		_, _ = e.git(ctx, execOpts{}, "--git-dir", p.gitDir, "update-ref", "-d", "refs/tags/"+spec.Name)
		if strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return fmt.Errorf("repo: tag %q: %w: %w", spec.Name, ErrTagAlreadyExists, err)
		}
		return fmt.Errorf("repo: push tag %q: %w", spec.Name, err)
	}
	return nil
}

// ListTags implements Workspace: fetches origin tags then lists
// refs/tags/<prefix>* via for-each-ref plumbing. CommitHash is the PEELED
// commit (annotated tags dereferenced); Message is the tag message subject,
// empty for lightweight tags. Use this on the freshness-critical paths (the
// version lists, the save collision precheck, the plan lineage).
//
//deadcode:keep wired in Task 4.2b (list-tags)
func (e *Engine) ListTags(ctx context.Context, ref RepoRef, prefix string) ([]TagInfo, error) {
	p, err := e.pathsFor(ref)
	if err != nil {
		return nil, err
	}
	cloned, err := e.ensureMirror(ctx, ref, p)
	if err != nil {
		return nil, err
	}
	if !cloned {
		release, lerr := e.locks.Lock(ctx, p.lockPath)
		if lerr != nil {
			return nil, lerr
		}
		ferr := e.fetch(ctx, ref, p)
		release()
		if ferr != nil {
			return nil, ferr
		}
	}
	return e.readTags(ctx, p, prefix)
}

// ListTagsLocal implements Workspace: like ListTags but WITHOUT the origin
// fetch — it lists whatever tags the shared mirror already holds. The mirror
// is authoritative for every tag the engine creates (Tag pushes AND updates
// the mirror before returning, and one pod serves the org on one volume), so
// this is correctness-equivalent to ListTags for the platform-owned tags —
// only truly out-of-band pushes are missed until the next fetch-bearing op.
// Intended for best-effort, hot-path reads that must not pay a per-read
// network round-trip. ensureMirror still clones on first-ever access (a stat
// when the mirror is already present).
//
//deadcode:keep wired in Task 4.2b (list-tags local=true)
func (e *Engine) ListTagsLocal(ctx context.Context, ref RepoRef, prefix string) ([]TagInfo, error) {
	p, err := e.pathsFor(ref)
	if err != nil {
		return nil, err
	}
	if _, err := e.ensureMirror(ctx, ref, p); err != nil {
		return nil, err
	}
	return e.readTags(ctx, p, prefix)
}

// readTags lists refs/tags/<prefix>* from the local mirror under the shared
// flock (a pure ref read — no fetch). Shared by ListTags/ListTagsLocal.
func (e *Engine) readTags(ctx context.Context, p repoPaths, prefix string) ([]TagInfo, error) {
	release, err := e.locks.RLock(ctx, p.lockPath)
	if err != nil {
		return nil, err
	}
	defer release()
	out, err := e.git(ctx, execOpts{}, "--git-dir", p.gitDir,
		"for-each-ref",
		"--format=%(refname:short)%00%(objectname)%00%(*objectname)%00%(contents:subject)%00%(creatordate:iso-strict)",
		"refs/tags/"+prefix+"*")
	if err != nil {
		return nil, fmt.Errorf("repo: list tags %q*: %w", prefix, err)
	}
	var tags []TagInfo
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\x00", 5)
		if len(fields) != 5 {
			return nil, fmt.Errorf("repo: unexpected for-each-ref record %q", line)
		}
		name, object, peeled, subject, created := fields[0], fields[1], fields[2], fields[3], fields[4]
		info := TagInfo{Name: name, CommitHash: object}
		if peeled != "" { // annotated: dereference to the commit, keep the tag message
			info.CommitHash = peeled
			info.Message = subject
		}
		// An undatable ref leaves the zero time rather than failing the listing:
		// ordering degrades for that one tag, which is better than no tags at
		// all on a repo carrying something odd.
		if at, perr := time.Parse(time.RFC3339, created); perr == nil {
			info.CreatedAt = at
		}
		tags = append(tags, info)
	}
	return tags, nil
}
