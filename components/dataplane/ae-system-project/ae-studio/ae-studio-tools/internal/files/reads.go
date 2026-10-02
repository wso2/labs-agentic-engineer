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

// Package files is the AE Studio Files API over the pod's git engine: list,
// read and bundle (this file), with today's read path rules (paths.go). Every
// call resolves its project through aep-api first, so the repository a request
// reaches is aep-api's answer for the pod's own org, never the caller's input.
//
// The read flow is moved from services/aep-api/internal/spec/files_service.go;
// the aep-api copy is deleted in phase 4.
package files

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/wso2/aep/ae-studio-tools/internal/projects"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
	"github.com/wso2/aep/ae-studio-tools/internal/repo/naming"
)

var (
	// ErrPathInvalid means a path or ref the read rules refuse: outside
	// specs/ and the read allow-list, non-canonical, traversing, or a ref
	// that is not a hex object name. It maps to 400 path_invalid.
	ErrPathInvalid = errors.New("invalid file path")
	// ErrFileNotFound means the path is not a file in the addressed tree. It
	// maps to 404 path_not_found.
	ErrFileNotFound = errors.New("file not found")
)

// Reader serves the read-only Files operations for the pod's org. Errors are
// the sentinels above, projects.ErrUnknown / ErrUnavailable from the lookup,
// or a *RepoError from the engine (which wraps repo.ErrRefNotFound for a ref
// that names no commit, repo.ErrDiskFull, or a git failure).
type Reader struct {
	Engine   *repo.Engine
	Projects projects.Resolver
	// Org is the pod's org handle: the first path segment of every clone.
	Org string
}

// Meta is one file of a listing.
type Meta struct {
	Path, SHA string
	Size      int64
}

// Content is one file's content and blob sha.
type Content struct {
	Path, Content, SHA string
}

// Bundle is a set of files read at one commit.
type Bundle struct {
	CommitSHA string
	Files     []Content
}

// Lookup is a known project: aep-api's repository for it and the branch tip.
type Lookup struct {
	Owner, Repo, HeadSHA string
}

// Lookup resolves project and reads its branch tip (fetching from origin), so
// a caller learns the project is known and which commit it is at.
func (r Reader) Lookup(ctx context.Context, project string) (*Lookup, error) {
	rep, ref, err := r.resolve(ctx, project)
	if err != nil {
		return nil, err
	}
	head, err := r.Engine.Head(ctx, ref, "")
	if err != nil {
		return nil, repoError(ref, fmt.Errorf("resolve head: %w", err))
	}
	return &Lookup{Owner: rep.Owner, Repo: rep.Repo, HeadSHA: head}, nil
}

// List returns every readable blob at the branch tip whose path has the given
// prefix (empty prefix ⇒ all), sorted by path. Like Bundle it filters through
// the read rules: a path ReadAt would refuse is omitted, so a listing names
// nothing a caller could not read.
func (r Reader) List(ctx context.Context, project, prefix string) ([]Meta, error) {
	ref, err := r.repoRef(ctx, project)
	if err != nil {
		return nil, err
	}
	entries, _, err := r.Engine.List(ctx, ref, "")
	if err != nil {
		return nil, repoError(ref, fmt.Errorf("list files at head: %w", err))
	}
	out := make([]Meta, 0, len(entries))
	for _, e := range entries {
		if !strings.HasPrefix(e.Path, prefix) || validateReadPath(e.Path) != nil {
			continue
		}
		out = append(out, Meta{Path: e.Path, SHA: e.SHA, Size: e.Size})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// ReadAt returns one file's content and blob sha at ref (empty: the branch
// tip). Both gates run before the lookup: validateReadPath decides which paths
// are readable at all, and validateCommit bounds the pin to a hex object name
// so the parameter cannot become a revision-expression browser over history.
func (r Reader) ReadAt(ctx context.Context, project, path, ref string) (*Content, error) {
	if err := validateReadPath(path); err != nil {
		return nil, err
	}
	if err := validateCommit(ref); err != nil {
		return nil, err
	}
	repoRef, err := r.repoRef(ctx, project)
	if err != nil {
		return nil, err
	}
	content, blobSHA, err := r.Engine.ReadFile(ctx, repoRef, ref, path)
	if err != nil {
		if errors.Is(err, repo.ErrPathNotFound) {
			return nil, ErrFileNotFound
		}
		return nil, repoError(repoRef, fmt.Errorf("read %s at %s: %w", path, commitLabel(ref), err))
	}
	return &Content{Path: path, Content: string(content), SHA: blobSHA}, nil
}

// Bundle returns every readable file under prefix at one commit.
//
// It replaces a List-then-Read fan-out for two reasons. SPEED: a read
// addressed by branch name fetches from origin first, under the mirror's
// exclusive lock, so a fan-out over N files pays N serialized round trips;
// resolving the commit once and addressing every read by that sha pays it
// once. COHERENCE: a fan-out resolves the tip per request, so a push landing
// mid-read hands back content and blob shas from two trees, and callers
// precondition their later writes on exactly those shas.
//
// A path the read gate refuses is omitted, never an error: the tree also holds
// application code, so a bundle is a filter over it. It is the gate ReadAt
// applies per file, so a bundle exposes no path a caller could not read alone.
func (r Reader) Bundle(ctx context.Context, project, prefix, ref string) (*Bundle, error) {
	if err := validateCommit(ref); err != nil {
		return nil, err
	}
	repoRef, err := r.repoRef(ctx, project)
	if err != nil {
		return nil, err
	}
	// The one read here that addresses a ref rather than an object, and so
	// the only one that touches the network.
	commit, err := r.Engine.Head(ctx, repoRef, ref)
	if err != nil {
		return nil, repoError(repoRef, fmt.Errorf("resolve %s: %w", commitLabel(ref), err))
	}
	keep := func(path string) bool {
		return strings.HasPrefix(path, prefix) && validateReadPath(path) == nil
	}
	entries, _, err := r.Engine.List(ctx, repoRef, commit)
	if err != nil {
		return nil, repoError(repoRef, fmt.Errorf("list files at %s: %w", commit, err))
	}
	contents, _, err := r.Engine.ReadBundle(ctx, repoRef, commit, keep)
	if err != nil {
		return nil, repoError(repoRef, fmt.Errorf("read bundle at %s: %w", commit, err))
	}
	shaOf := make(map[string]string, len(entries))
	for _, e := range entries {
		shaOf[e.Path] = e.SHA
	}
	out := make([]Content, 0, len(contents))
	for path, content := range contents {
		sha, ok := shaOf[path]
		if !ok {
			// Both reads addressed the same commit, so content without a tree
			// entry means the mirror moved mid-bundle. Fail rather than hand
			// back an empty sha, which the apply gate reads as "must not exist".
			return nil, repoError(repoRef, fmt.Errorf("bundle at %s: %s has content but no tree entry", commit, path))
		}
		out = append(out, Content{Path: path, Content: content, SHA: sha})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return &Bundle{CommitSHA: commit, Files: out}, nil
}

// RepoError is an engine failure on one repository. Repo is its owner/repo
// (never userinfo), safe for a log line; Err carries the git command and its
// stderr, which may name the clone URL, so callers log Repo and a class, not
// Err's text.
type RepoError struct {
	Repo string
	Err  error
}

func (e *RepoError) Error() string { return e.Err.Error() }

func (e *RepoError) Unwrap() error { return e.Err }

// repoError wraps an engine error with the repository it concerns.
func repoError(ref repo.RepoRef, err error) error {
	return &RepoError{Repo: ref.FullName(), Err: err}
}

// repoRef resolves project and addresses its clone (see resolve).
func (r Reader) repoRef(ctx context.Context, project string) (repo.RepoRef, error) {
	_, ref, err := r.resolve(ctx, project)
	return ref, err
}

// resolve resolves project through aep-api (every call, never cached) and
// addresses its clone under the pod's org. The slug is derived from aep-api's
// owner/repo with the function aep-api uses for its git_repositories column.
func (r Reader) resolve(ctx context.Context, project string) (projects.Repository, repo.RepoRef, error) {
	rep, err := r.Projects.Resolve(ctx, project)
	if err != nil {
		return projects.Repository{}, repo.RepoRef{}, err
	}
	slug := naming.SlugForURL("https://github.com/" + rep.Owner + "/" + rep.Repo)
	if slug == "" {
		return projects.Repository{}, repo.RepoRef{}, fmt.Errorf("%w: repository answer is not an owner/repo pair", projects.ErrUnavailable)
	}
	cloneURL, err := withoutUserinfo(rep.CloneURL)
	if err != nil {
		return projects.Repository{}, repo.RepoRef{}, fmt.Errorf("%w: repository answer has an unusable clone URL", projects.ErrUnavailable)
	}
	return rep, repo.RepoRef{
		Org:           r.Org,
		Project:       project,
		RepoSlug:      slug,
		CloneURL:      cloneURL,
		DefaultBranch: rep.DefaultBranch,
	}, nil
}

// withoutUserinfo drops any user:password@ from a clone URL: the engine
// authenticates through askpass only, and a URL is echoed in git's errors and
// argv, where a credential must never be.
func withoutUserinfo(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	u.User = nil
	return u.String(), nil
}

// commitLabel names the pin for an error message.
func commitLabel(ref string) string {
	if ref == "" {
		return "head"
	}
	return ref
}
