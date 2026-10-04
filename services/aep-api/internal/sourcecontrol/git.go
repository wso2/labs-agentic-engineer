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

package sourcecontrol

import "context"

// git.go — the Git port: a project's repository content (reads, tags, raw
// commits) as the org's AE Studio pod serves it. aep-api holds no clone; the
// pod owns the mirror and GitHub access. Served by clients/aestudiotools'
// Adapter, and in tests by aestudiotest.Fake.

// RepoRef addresses one repository: the org (whose AE Studio pod serves it),
// the GitHub owner and repository, and its default branch (the pod reads and
// commits on it). RefForRow / RepoRefFor build it from a git_repositories row.
type RepoRef struct{ Org, Owner, Repo, DefaultBranch string }

// ReadOptions tunes one read.
type ReadOptions struct {
	// Local reads the pod's mirror as it is, without fetching GitHub first.
	Local bool
}

// ReadOption sets a ReadOptions field.
type ReadOption func(*ReadOptions)

// Local reads the pod's mirror without fetching first.
//
//deadcode:keep first production caller in Task 4.14 (status snapshot reads)
func Local() ReadOption { return func(o *ReadOptions) { o.Local = true } }

// ReadOptionsOf folds opts into one ReadOptions; Git implementations call it.
//
//deadcode:keep first production caller in Task 4.12 (the aestudiotools adapter)
func ReadOptionsOf(opts ...ReadOption) ReadOptions {
	var o ReadOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// BundleFilter selects the files one ReadBundle returns.
type BundleFilter struct {
	Prefix string   // "" = whole tree
	Exts   []string // empty = any extension
	Paths  []string // exact paths; when set, Prefix and Exts are ignored
}

// FileWrite writes one file. BaseSHA is the blob sha the path must have at
// the tip; "" means the path must not exist.
type FileWrite struct{ Path, Content, BaseSHA string }

// FileDelete deletes one file whose blob at the tip is BaseSHA.
type FileDelete struct{ Path, BaseSHA string }

// CommitRequest is one raw commit on the default branch: every write and
// delete lands in one commit or none does.
type CommitRequest struct {
	Writes  []FileWrite
	Deletes []FileDelete
	Message string
	Author  *GitIdentity // nil = the pod's gitpat identity
	// Committer nil = Author (the pod's default).
	Committer *GitIdentity
}

// CommittedFile is a written path and its new blob sha.
type CommittedFile struct{ Path, SHA string }

// CommitWarning is a non-fatal note the pod attached to a path.
type CommitWarning struct{ Path, Code, Message string }

// CommitResult reports a Commit. Changed is false when the request changed
// nothing; CommitSHA is then the unchanged tip. Files lists the writes in
// request order.
type CommitResult struct {
	CommitSHA string
	Changed   bool
	Files     []CommittedFile
	Warnings  []CommitWarning
}

// Conflict is one path whose baseSha did not match the tip. CurrentSHA is ""
// when the path is absent at the tip.
type Conflict struct{ Path, BaseSHA, CurrentSHA string }

// Git is a repository's content through the org's AE Studio pod. `at` is ""
// (the default-branch tip), "tags/<name>" or a 40-hex commit sha. Reads fetch
// GitHub first unless Local() is passed. Errors: ErrRepoNotFound,
// ErrRefNotFound, ErrPathNotFound, ErrTagAlreadyExists, *CommitConflictError,
// ErrOwnerNotAllowed, *RateLimitedError, *HTTPStatusError (a GitHub failure)
// and the AE Studio sentinels (errors.go).
type Git interface {
	// Head resolves at to its commit sha.
	Head(ctx context.Context, ref RepoRef, at string, opts ...ReadOption) (string, error)
	// List lists every blob of the tree at `at`, by path, with the commit sha
	// read.
	List(ctx context.Context, ref RepoRef, at string, opts ...ReadOption) (entries []Entry, commitSHA string, err error)
	// ReadFile reads one file and its blob sha.
	ReadFile(ctx context.Context, ref RepoRef, at, path string, opts ...ReadOption) (content []byte, blobSHA string, err error)
	// ReadBundle reads the files f selects at one commit, keyed by full path.
	ReadBundle(ctx context.Context, ref RepoRef, at string, f BundleFilter, opts ...ReadOption) (files map[string]string, commitSHA string, err error)
	// ListTags lists the tags whose name starts with prefix.
	ListTags(ctx context.Context, ref RepoRef, prefix string, opts ...ReadOption) ([]TagInfo, error)
	// Tag creates an annotated tag; ErrTagAlreadyExists when the name is taken.
	Tag(ctx context.Context, ref RepoRef, spec TagSpec) error
	// Commit applies req in one commit on the default branch, or answers
	// *CommitConflictError naming every path whose baseSha did not hold.
	Commit(ctx context.Context, ref RepoRef, req CommitRequest) (CommitResult, error)
}
