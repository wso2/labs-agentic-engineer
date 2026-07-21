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
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Tag implements Workspace (design §10): annotated tag + push, under the
// EXCLUSIVE flock. The fetch --tags precheck narrows the collision window;
// origin's push rejection closes it — either path returns
// ErrTagAlreadyExists so the caller can recompute the next vN and retry.
func (e *Engine) Tag(ctx context.Context, ref RepoRef, spec TagSpec) error {
	if spec.Name == "" {
		return fmt.Errorf("gitfs: empty tag name")
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
		return fmt.Errorf("gitfs: tag %q: %w", spec.Name, ErrTagAlreadyExists)
	}

	target, err := e.resolveCommit(ctx, ref, p, spec.Target)
	if err != nil {
		return err
	}
	env := identityEnv(spec.Tagger, spec.Tagger) // tagger = committer identity
	if _, err := e.git(ctx, execOpts{env: env}, "--git-dir", p.gitDir,
		"tag", "-a", spec.Name, "-m", spec.Message, target); err != nil {
		return fmt.Errorf("gitfs: create tag %q: %w", spec.Name, err)
	}
	if _, err := e.remoteGit(ctx, ref, execOpts{}, "--git-dir", p.gitDir,
		"push", "origin", "refs/tags/"+spec.Name+":refs/tags/"+spec.Name); err != nil {
		// Roll the local tag back so the mirror never claims a tag origin
		// refused — the mirror must stay a faithful cache of origin.
		_, _ = e.git(ctx, execOpts{}, "--git-dir", p.gitDir, "update-ref", "-d", "refs/tags/"+spec.Name)
		if strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return fmt.Errorf("gitfs: tag %q: %w: %w", spec.Name, ErrTagAlreadyExists, err)
		}
		return fmt.Errorf("gitfs: push tag %q: %w", spec.Name, err)
	}
	return nil
}

// ListTags implements Workspace: fetches origin tags then lists
// refs/tags/<prefix>* via for-each-ref plumbing. CommitHash is the PEELED
// commit (annotated tags dereferenced); Message is the tag message subject,
// empty for lightweight tags. Use this on the freshness-critical paths (the
// version-list endpoints, the save collision precheck, the plan lineage).
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
	return e.readTags(ctx, ref, p, prefix)
}

// ListTagsLocal implements Workspace: like ListTags but WITHOUT the origin
// fetch — it lists whatever tags the shared mirror already holds. The mirror is
// authoritative for every tag this platform creates (Tag pushes AND updates the
// mirror before returning, and the mirror lives on the shared RWX volume, so a
// tag cut by any instance is visible to all), so this is correctness-equivalent
// to ListTags for the platform-owned `v*` tags — only truly out-of-band pushes
// are missed until the next fetch-bearing op. Intended for best-effort,
// hot-path reads (the task stale-design attention flag) that must not pay a
// per-read network round-trip. ensureMirror still clones on first-ever access
// (a stat when the mirror is already present).
func (e *Engine) ListTagsLocal(ctx context.Context, ref RepoRef, prefix string) ([]TagInfo, error) {
	p, err := e.pathsFor(ref)
	if err != nil {
		return nil, err
	}
	if _, err := e.ensureMirror(ctx, ref, p); err != nil {
		return nil, err
	}
	return e.readTags(ctx, ref, p, prefix)
}

// readTags lists refs/tags/<prefix>* from the local mirror under the shared
// flock (a pure ref read — no fetch). Shared by ListTags/ListTagsLocal.
func (e *Engine) readTags(ctx context.Context, ref RepoRef, p repoPaths, prefix string) ([]TagInfo, error) {
	release, err := e.locks.RLock(ctx, p.lockPath)
	if err != nil {
		return nil, err
	}
	defer release()
	out, err := e.git(ctx, execOpts{}, "--git-dir", p.gitDir,
		"for-each-ref",
		"--format=%(refname:short)%00%(objectname)%00%(*objectname)%00%(creatordate:iso-strict)%00%(contents:subject)",
		"refs/tags/"+prefix+"*")
	if err != nil {
		return nil, fmt.Errorf("gitfs: list tags %q*: %w", prefix, err)
	}
	var tags []TagInfo
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\x00", 5)
		if len(fields) != 5 {
			return nil, fmt.Errorf("gitfs: unexpected for-each-ref record %q", line)
		}
		name, object, peeled, created, subject := fields[0], fields[1], fields[2], fields[3], fields[4]
		info := TagInfo{Name: name, CommitHash: object}
		if t, terr := time.Parse(time.RFC3339, created); terr == nil {
			info.CreatedAt = t.UTC()
		}
		if peeled != "" { // annotated: dereference to the commit, keep the tag message
			info.CommitHash = peeled
			info.Message = subject
		}
		tags = append(tags, info)
	}
	return tags, nil
}

// Diff implements Workspace: the local `git diff base...head` (three-dot —
// merge-base to head, matching the retired GitHub compare), with rename
// detection, per-file numstat, and per-file unified hunks (ChangedFile.Patch,
// the GitHub compare `patch` shape — added for the task plan lineage diff).
func (e *Engine) Diff(ctx context.Context, ref RepoRef, base, head string) (*CompareResult, error) {
	p, err := e.pathsFor(ref)
	if err != nil {
		return nil, err
	}
	cloned, err := e.ensureMirror(ctx, ref, p)
	if err != nil {
		return nil, err
	}
	if err := e.freshenFor(ctx, ref, p, cloned, base, head); err != nil {
		return nil, err
	}
	release, err := e.locks.RLock(ctx, p.lockPath)
	if err != nil {
		return nil, err
	}
	defer release()

	baseSHA, err := e.resolveCommit(ctx, ref, p, base)
	if err != nil {
		return nil, err
	}
	headSHA, err := e.resolveCommit(ctx, ref, p, head)
	if err != nil {
		return nil, err
	}

	files, err := e.diffFiles(ctx, p, baseSHA, headSHA)
	if err != nil {
		return nil, err
	}
	ahead, err := e.revListCount(ctx, p, baseSHA, headSHA)
	if err != nil {
		return nil, err
	}
	behind, err := e.revListCount(ctx, p, headSHA, baseSHA)
	if err != nil {
		return nil, err
	}
	return &CompareResult{
		Status:       compareStatus(ahead, behind),
		AheadBy:      ahead,
		BehindBy:     behind,
		TotalCommits: ahead, // GitHub semantics: total_commits == ahead_by for base...head
		Files:        files,
	}, nil
}

// diffFiles merges `diff --name-status -z -M` (statuses, rename pairs) with
// `diff --numstat -z -M` (per-file additions/deletions). Caller holds a flock.
func (e *Engine) diffFiles(ctx context.Context, p repoPaths, baseSHA, headSHA string) ([]ChangedFile, error) {
	rangeExpr := baseSHA + "..." + headSHA
	nsOut, err := e.git(ctx, execOpts{}, "--git-dir", p.gitDir,
		"diff", "--name-status", "-z", "-M", rangeExpr)
	if err != nil {
		return nil, fmt.Errorf("gitfs: diff --name-status %s: %w", rangeExpr, err)
	}
	files, err := parseNameStatus(splitNUL(nsOut))
	if err != nil {
		return nil, err
	}
	numOut, err := e.git(ctx, execOpts{}, "--git-dir", p.gitDir,
		"diff", "--numstat", "-z", "-M", rangeExpr)
	if err != nil {
		return nil, fmt.Errorf("gitfs: diff --numstat %s: %w", rangeExpr, err)
	}
	stats, err := parseNumstat(splitNUL(numOut))
	if err != nil {
		return nil, err
	}
	for i := range files {
		if st, ok := stats[files[i].Filename]; ok {
			files[i].Additions = st.additions
			files[i].Deletions = st.deletions
			files[i].Changes = st.additions + st.deletions
		}
		patch, perr := e.filePatch(ctx, p, rangeExpr, files[i].Filename)
		if perr != nil {
			return nil, perr
		}
		files[i].Patch = patch
	}
	return files, nil
}

// filePatch returns one file's unified hunks for the range — a path-scoped
// `git diff` (":(literal)" pathspec so glob characters in paths stay literal)
// with the per-file header stripped, matching GitHub's compare `patch` field.
// Binary files (no hunks) return "" like GitHub; a rename limited to its new
// path degrades to an add-shaped diff (display-only context, accepted).
func (e *Engine) filePatch(ctx context.Context, p repoPaths, rangeExpr, path string) (string, error) {
	out, err := e.git(ctx, execOpts{}, "--git-dir", p.gitDir,
		"diff", rangeExpr, "--", ":(literal)"+path)
	if err != nil {
		return "", fmt.Errorf("gitfs: diff patch %q: %w", path, err)
	}
	return extractHunks(string(out)), nil
}

// extractHunks strips everything before the first hunk marker (the
// diff --git/index/---/+++ header lines) and trims the trailing newline.
func extractHunks(patch string) string {
	if strings.HasPrefix(patch, "@@") {
		return strings.TrimRight(patch, "\n")
	}
	idx := strings.Index(patch, "\n@@")
	if idx < 0 {
		return "" // binary or empty diff — no hunks
	}
	return strings.TrimRight(patch[idx+1:], "\n")
}

// parseNameStatus walks the NUL token stream of `diff --name-status -z`:
// <status> <path> pairs, with rename/copy carrying <status> <old> <new>.
func parseNameStatus(tokens []string) ([]ChangedFile, error) {
	var files []ChangedFile
	for i := 0; i < len(tokens); {
		status := tokens[i]
		if status == "" {
			i++
			continue
		}
		letter := status[:1]
		if letter == "R" || letter == "C" {
			if i+2 >= len(tokens) {
				return nil, fmt.Errorf("gitfs: truncated rename record in name-status output")
			}
			files = append(files, ChangedFile{Filename: tokens[i+2], Status: githubStatus(letter)})
			i += 3
			continue
		}
		if i+1 >= len(tokens) {
			return nil, fmt.Errorf("gitfs: truncated record in name-status output")
		}
		files = append(files, ChangedFile{Filename: tokens[i+1], Status: githubStatus(letter)})
		i += 2
	}
	return files, nil
}

// githubStatus maps git's status letters onto GitHub's compare vocabulary so
// downstream consumers (lineage diff rendering) see unchanged strings.
func githubStatus(letter string) string {
	switch letter {
	case "A":
		return "added"
	case "D":
		return "removed"
	case "M":
		return "modified"
	case "R":
		return "renamed"
	case "C":
		return "copied"
	case "T":
		return "changed"
	default:
		return strings.ToLower(letter)
	}
}

type numstatEntry struct{ additions, deletions int }

// parseNumstat walks the NUL token stream of `diff --numstat -z`: each token
// is "added\tdeleted\tpath", except renames where the path field is empty
// and the next two tokens are old and new path. Binary files report "-".
func parseNumstat(tokens []string) (map[string]numstatEntry, error) {
	stats := map[string]numstatEntry{}
	for i := 0; i < len(tokens); {
		tok := tokens[i]
		if tok == "" {
			i++
			continue
		}
		parts := strings.SplitN(tok, "\t", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("gitfs: unexpected numstat record %q", tok)
		}
		entry := numstatEntry{additions: atoiDash(parts[0]), deletions: atoiDash(parts[1])}
		path := parts[2]
		if path == "" { // rename: two path tokens follow (old, new)
			if i+2 >= len(tokens) {
				return nil, fmt.Errorf("gitfs: truncated rename record in numstat output")
			}
			stats[tokens[i+2]] = entry
			i += 3
			continue
		}
		stats[path] = entry
		i++
	}
	return stats, nil
}

// atoiDash parses a numstat count; "-" (binary) counts as 0.
func atoiDash(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func (e *Engine) revListCount(ctx context.Context, p repoPaths, from, to string) (int, error) {
	out, err := e.git(ctx, execOpts{}, "--git-dir", p.gitDir,
		"rev-list", "--count", from+".."+to)
	if err != nil {
		return 0, fmt.Errorf("gitfs: rev-list --count %s..%s: %w", from, to, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("gitfs: rev-list count parse: %w", err)
	}
	return n, nil
}

func compareStatus(ahead, behind int) string {
	switch {
	case ahead == 0 && behind == 0:
		return "identical"
	case behind == 0:
		return "ahead"
	case ahead == 0:
		return "behind"
	default:
		return "diverged"
	}
}
