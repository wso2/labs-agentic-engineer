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
	"strconv"
	"strings"
)

// The Workspace read surface: git plumbing against the bare mirror, no
// checkout ever (design D2). Every method follows the same shape —
// ensure-mirror → freshen (branch/tag addressing fetches; raw shas only when
// the object is missing) → resolve + read under a SHARED flock.

// Head implements Workspace.
//
//deadcode:keep wired in Task 2.7
func (e *Engine) Head(ctx context.Context, ref RepoRef, at string) (string, error) {
	var sha string
	err := e.read(ctx, ref, at, func(p repoPaths, commit string) error {
		sha = commit
		return nil
	})
	return sha, err
}

// List implements Workspace: every blob (recursive) at `at`.
//
//deadcode:keep wired in Task 2.7
func (e *Engine) List(ctx context.Context, ref RepoRef, at string) ([]Entry, string, error) {
	var entries []Entry
	var head string
	err := e.read(ctx, ref, at, func(p repoPaths, commit string) error {
		head = commit
		var lerr error
		entries, lerr = e.lsTree(ctx, p, commit)
		return lerr
	})
	if err != nil {
		return nil, "", err
	}
	return entries, head, nil
}

// ReadFile implements Workspace: one blob's content + blob sha at `at`.
//
//deadcode:keep wired in Task 2.7
func (e *Engine) ReadFile(ctx context.Context, ref RepoRef, at, path string) ([]byte, string, error) {
	var content []byte
	var blobSHA string
	err := e.read(ctx, ref, at, func(p repoPaths, commit string) error {
		var rerr error
		content, blobSHA, rerr = e.readBlobAt(ctx, p, commit, path)
		return rerr
	})
	if err != nil {
		return nil, "", err
	}
	return content, blobSHA, nil
}

// ReadBundle implements Workspace: path→content for every blob at `at`
// accepted by keep (nil keeps everything). Streams blob-by-blob through
// cat-file — in-process, no checkout, no temp files.
//
//deadcode:keep wired in Task 2.7
func (e *Engine) ReadBundle(ctx context.Context, ref RepoRef, at string, keep func(rel string) bool) (map[string]string, string, error) {
	files := map[string]string{}
	var head string
	err := e.read(ctx, ref, at, func(p repoPaths, commit string) error {
		head = commit
		entries, lerr := e.lsTree(ctx, p, commit)
		if lerr != nil {
			return lerr
		}
		for _, entry := range entries {
			if keep != nil && !keep(entry.Path) {
				continue
			}
			out, cerr := e.git(ctx, execOpts{}, "--git-dir", p.gitDir, "cat-file", "blob", entry.SHA)
			if cerr != nil {
				return fmt.Errorf("repo: read blob %s (%s): %w", entry.Path, entry.SHA, cerr)
			}
			files[entry.Path] = string(out)
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return files, head, nil
}

// read is the shared read skeleton: validate → ensure mirror → freshen →
// resolve `at` and run fn under one SHARED flock.
//
//deadcode:keep wired in Task 2.7
func (e *Engine) read(ctx context.Context, ref RepoRef, at string, fn func(p repoPaths, commit string) error) error {
	p, err := e.pathsFor(ref)
	if err != nil {
		return err
	}
	cloned, err := e.ensureMirror(ctx, ref, p)
	if err != nil {
		return err
	}
	if err := e.freshenFor(ctx, ref, p, cloned, at); err != nil {
		return err
	}
	release, err := e.locks.RLock(ctx, p.lockPath)
	if err != nil {
		return err
	}
	defer release()
	commit, err := e.resolveCommit(ctx, ref, p, at)
	if err != nil {
		return err
	}
	return fn(p, commit)
}

// lsTree lists every blob of the commit's tree via `ls-tree -r -l -z` — the
// DEFAULT long format (`<mode> <type> <sha> <size>\t<path>`, NUL-terminated
// records). The default format is the only one whose -z mode emits pathnames
// verbatim: a custom `--format=%(path)` C-quotes non-ASCII / special paths
// even under -z (and core.quotepath=off still quotes `"` and `\`), which would
// corrupt unicode paths. Caller holds a flock.
//
//deadcode:keep wired in Task 2.7
func (e *Engine) lsTree(ctx context.Context, p repoPaths, commit string) ([]Entry, error) {
	out, err := e.git(ctx, execOpts{}, "--git-dir", p.gitDir,
		"ls-tree", "-r", "-l", "-z", commit)
	if err != nil {
		return nil, fmt.Errorf("repo: ls-tree %s: %w", commit, err)
	}
	var entries []Entry
	for _, record := range splitNUL(out) {
		header, path, ok := strings.Cut(record, "\t")
		if !ok {
			return nil, fmt.Errorf("repo: ls-tree %s: malformed record %q", commit, record)
		}
		// <mode> SP <type> SP <sha> SP+ <size-or-dash>; size is right-aligned
		// with padding spaces, so split on any whitespace run.
		fields := strings.Fields(header)
		if len(fields) != 4 {
			return nil, fmt.Errorf("repo: ls-tree %s: malformed header %q", commit, header)
		}
		typ, sha, sizeStr := fields[1], fields[2], fields[3]
		if typ != "blob" {
			continue // submodule commits / nested trees are not files
		}
		size, perr := strconv.ParseInt(sizeStr, 10, 64)
		if perr != nil {
			return nil, fmt.Errorf("repo: ls-tree %s: bad size %q for %s", commit, sizeStr, path)
		}
		entries = append(entries, Entry{Path: path, SHA: sha, Size: size})
	}
	return entries, nil
}

// readBlobAt returns content + blob sha of path at commit. The commit is
// already verified, so a failed path lookup maps to ErrPathNotFound (the
// later phases' 404). Caller holds a flock.
//
//deadcode:keep wired in Task 2.7
func (e *Engine) readBlobAt(ctx context.Context, p repoPaths, commit, path string) ([]byte, string, error) {
	out, err := e.git(ctx, execOpts{}, "--git-dir", p.gitDir,
		"rev-parse", "--verify", "--end-of-options", commit+":"+path)
	if err != nil {
		return nil, "", fmt.Errorf("repo: %q at %s: %w: %w", path, commit, ErrPathNotFound, err)
	}
	blobSHA := strings.TrimSpace(string(out))
	content, err := e.git(ctx, execOpts{}, "--git-dir", p.gitDir, "cat-file", "blob", blobSHA)
	if err != nil {
		// The path resolved to a non-blob (a directory) — not a file.
		return nil, "", fmt.Errorf("repo: %q at %s is not a file: %w: %w", path, commit, ErrPathNotFound, err)
	}
	return content, blobSHA, nil
}

// splitNUL splits NUL-separated output, dropping the trailing empty field.
//
//deadcode:keep wired in Task 2.7
func splitNUL(out []byte) []string {
	s := string(out)
	s = strings.TrimSuffix(s, "\x00")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\x00")
}
