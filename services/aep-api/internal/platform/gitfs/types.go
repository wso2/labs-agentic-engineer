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

// Package gitfs is the git-object engine over the shared workspace volume: one
// bare mirror per repo (`git clone --mirror`, never checked out), plumbing reads
// (rev-parse / ls-tree / cat-file / archive), plumbing writes via a throwaway
// index (read-tree / update-index / write-tree / commit-tree /
// `push --force-with-lease`), annotated tags, local diffs, and immutable
// per-SHA snapshot dirs for the agents mount.
//
// This package is the single definition point for the Workspace and
// SnapshotProvider ports, their DTOs, and the git sentinel errors. The domain
// alias surface lives in internal/feature/gitrepo/workspace.go — consumers
// import sourcecontrol.*, and the arch lock (internal/platform must stay
// feature-free) is honoured because the definitions live here.
package gitfs

import (
	"context"
	"errors"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// Sentinel errors. ErrRefNotFastForward and ErrTagAlreadyExists carry the
// EXACT message strings of the historical gitrepo/errors.go values (which are
// now aliases of these), so `errors.Is` identities and log lines stay stable
// across the REST→mount migration.
var (
	// ErrRefNotFastForward is returned by Mutate when the CAS retry loop
	// exhausts its attempts because origin kept advancing (the analog of the
	// retired REST fast-forward-only ref-update rejection).
	ErrRefNotFastForward = errors.New("github ref: update is not a fast-forward")
	// ErrTagAlreadyExists is returned by Tag when the tag name is already
	// taken (locally after fetch, or rejected by origin on push).
	ErrTagAlreadyExists = errors.New("tag already exists")
	// ErrRefNotFound is returned when `at` (branch, tag, or sha) does not
	// resolve to a commit — the later phases map it to 404.
	ErrRefNotFound = errors.New("git ref not found")
	// ErrPathNotFound is returned by ReadFile / Snapshot.Read when the path
	// does not exist (as a file) in the addressed tree — maps to 404.
	ErrPathNotFound = errors.New("git path not found")
)

// Workspace is the single collapse point for the four REST read walkers, the
// three write paths, tagging, and ref comparison (design D1/D5). All methods
// are safe for concurrent use; cross-process integrity on the shared bare
// mirror is arbitrated by a per-repo flock (design D8) and origin push-CAS.
type Workspace interface {
	// Head resolves `at` to a commit SHA. "" resolves the default-branch tip,
	// "tags/vN" (or a bare tag/branch name) resolves and peels the ref —
	// annotated tags peel to the commit they point at — and a raw 40-hex sha
	// is verified and returned verbatim. Branch/tag addressing fetches origin
	// first; raw shas read local objects, fetching only when missing.
	Head(ctx context.Context, ref RepoRef, at string) (sha string, err error)
	// HeadLocal is Head("") without the origin fetch — the default-branch tip
	// the shared mirror already holds. Correctness-equivalent for
	// platform-written content (Mutate updates the mirror before returning);
	// out-of-band pushes are missed until the next fetch-bearing op. For
	// hot-path reads (the status poll) that must not pay a network round-trip.
	HeadLocal(ctx context.Context, ref RepoRef) (sha string, err error)
	// List returns every blob in the tree at `at` (recursive) plus the
	// resolved commit SHA.
	List(ctx context.Context, ref RepoRef, at string) (entries []Entry, headSHA string, err error)
	// ReadFile returns one blob's content and its blob SHA at `at`.
	ReadFile(ctx context.Context, ref RepoRef, at, path string) (content []byte, blobSHA string, err error)
	// ReadBundle returns path→content for every blob at `at` accepted by
	// keep (nil keeps everything), plus the resolved commit SHA.
	ReadBundle(ctx context.Context, ref RepoRef, at string, keep func(rel string) bool) (files map[string]string, headSHA string, err error)

	// Mutate runs fn against the current default-branch tip and commits its
	// staged Write/Delete overlay as one commit, pushed to origin under
	// `--force-with-lease` (origin stays the CAS arbiter). A non-fast-forward
	// rejection re-fetches and re-runs fn, bounded by opts.Retry; exhaustion
	// surfaces ErrRefNotFastForward. Any error returned by fn aborts
	// immediately with that error — no retry (the caller's 409 path). A
	// no-change fn returns CommitResult{Changed: false} without committing.
	Mutate(ctx context.Context, ref RepoRef, fn func(Tx) error, opts CommitOpts) (CommitResult, error)

	// Tag creates an annotated tag and pushes it; a taken name (locally after
	// fetch, or rejected by origin) returns ErrTagAlreadyExists.
	Tag(ctx context.Context, ref RepoRef, spec TagSpec) error
	// ListTags returns the tags whose name starts with prefix, with the
	// peeled commit SHA and the tag message subject (empty for lightweight
	// tags). Fetches origin tags first.
	ListTags(ctx context.Context, ref RepoRef, prefix string) ([]TagInfo, error)
	// ListTagsLocal is ListTags without the origin fetch — it serves whatever
	// the shared mirror already holds. Correctness-equivalent to ListTags for
	// platform-owned `v*` tags (the mirror is updated on tag creation); for
	// best-effort hot-path reads that must not pay a per-read network fetch.
	ListTagsLocal(ctx context.Context, ref RepoRef, prefix string) ([]TagInfo, error)
	// Diff is the local `git diff base...head` (three-dot: merge-base to
	// head), replacing the remote CompareRefs.
	Diff(ctx context.Context, ref RepoRef, base, head string) (*CompareResult, error)

	// PutReferences replaces the project's stored reference documents — the
	// files attached on the create view, which are never committed and are
	// overlaid into each snapshot instead (references.go / console ADR-0017).
	// A bad name, an oversized document, or too many of them is
	// ErrReferenceRejected, which handlers map to 400.
	PutReferences(ctx context.Context, ref RepoRef, docs []ReferenceDoc) error
	// ListReferences returns the stored document names, sorted. No store —
	// the ordinary case, since most projects attach nothing — is (nil, nil).
	ListReferences(ctx context.Context, ref RepoRef) ([]string, error)
}

// Tx is the staged overlay handed to a Mutate fn. Write/Delete record
// intentions applied in call order; the last operation on a path wins.
type Tx interface {
	// Base is the committed base-tree view (the fetched default-branch tip
	// this attempt builds on). It feeds per-file baseSha preconditions. Valid
	// only for the duration of the fn invocation it was handed to.
	Base() Snapshot
	Write(rel string, content []byte)
	Delete(rel string)
}

// Snapshot is a read-only view of one commit's tree.
type Snapshot interface {
	CommitSHA() string
	// Read returns content and blob SHA; ErrPathNotFound when the path is
	// not a file in this tree.
	Read(rel string) ([]byte, string, error)
	// Walk visits every blob whose path starts with prefix ("" = all).
	Walk(prefix string, fn func(rel, blobSHA string) error) error
}

// SnapshotProvider is the separate optional port for the agents mount
// (design D4): it materializes the immutable plain-file tree
// snapshots/<sha>/ beside the mirror.
type SnapshotProvider interface {
	// Ensure materializes snapshots/<sha>/ iff absent. Idempotent and safe
	// under concurrent double-invocation: staged in tmp/, renamed into place,
	// so a torn dir is never observable at the canonical path.
	Ensure(ctx context.Context, ref RepoRef, sha string) error
}

// RepoRef addresses one repo on the mount. OrgID/ProjectID/RepoSlug are the
// path key (a pure function of the git_repositories row — never of client
// input, design D6); CloneURL/DefaultBranch drive the remote ops; Cred mints
// tokens per remote op (design D7). Cred may be nil for credential-less
// remotes (file:// origins in tests) — askpass injection is then skipped.
type RepoRef struct {
	OrgID         string
	ProjectID     string
	RepoSlug      string
	CloneURL      string
	DefaultBranch string
	Cred          secrets.Credential
}

// Entry is one blob of a tree listing.
type Entry struct {
	Path string
	SHA  string
	Size int64
}

// CommitOpts parametrizes one Mutate: the commit message, the author /
// committer identities (nil falls back to the AEP default identity), and the
// CAS retry policy (design D5 — the one retry loop for all writers).
type CommitOpts struct {
	Message   string
	Author    *GitIdentity
	Committer *GitIdentity
	Retry     RetryPolicy
}

// CommitResult reports what a Mutate did. Changed is false when fn staged
// nothing (or staged content identical to base) — CommitSHA is then the
// unchanged base tip.
type CommitResult struct {
	CommitSHA string
	Changed   bool
}

// TagSpec describes one annotated tag. Target is a commit sha (or any
// resolvable ref; "" targets the default-branch tip). Tagger nil falls back
// to the AEP default identity.
type TagSpec struct {
	Name    string
	Target  string
	Message string
	Tagger  *GitIdentity
}

// TagInfo describes a git tag: the peeled commit it points at and the tag
// message subject (empty for lightweight tags). Field shape matches the
// historical sourcecontrol.TagInfo (now an alias of this type).
type TagInfo struct {
	Name       string `json:"name"`
	CommitHash string `json:"commitHash"`
	Message    string `json:"message,omitempty"`
}

// GitIdentity is a git author/committer/tagger identity. Field names and
// json tags are byte-identical to the historical sourcecontrol.GitIdentity (now an
// alias of this type) so the GitHub client's wire marshaling is unchanged.
// Date is optional (git raw or RFC2822/ISO format accepted by git); empty
// means "now".
type GitIdentity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Date  string `json:"date,omitempty"`
}

// RetryPolicy bounds Mutate's CAS retry loop: plain bounded attempts with a
// jittered backoff (design D5 — the org-keyed leaky bucket is retired, not
// ported). Zero values mean the defaults: 4 attempts, {50,200,800}ms base
// backoff, each delay jittered by ±50%.
type RetryPolicy struct {
	// Attempts is the total number of attempts (not retries). <=0 → 4.
	Attempts int
	// Backoff holds the base delay before each retry; retries beyond its
	// length reuse the last entry. Empty → {50ms, 200ms, 800ms}.
	Backoff []time.Duration
}

const defaultRetryAttempts = 4

var defaultRetryBackoff = []time.Duration{50 * time.Millisecond, 200 * time.Millisecond, 800 * time.Millisecond}

// withDefaults returns the policy with zero values replaced by the defaults.
func (p RetryPolicy) withDefaults() RetryPolicy {
	if p.Attempts <= 0 {
		p.Attempts = defaultRetryAttempts
	}
	if len(p.Backoff) == 0 {
		p.Backoff = defaultRetryBackoff
	}
	return p
}

// CompareResult is the per-file change summary between two refs. Field shape
// matches the historical sourcecontrol.CompareResult (now an alias of this type),
// which mirrored GitHub's GET /compare/{base}...{head} subset.
type CompareResult struct {
	// Status is the overall relationship of head to base: "ahead", "behind",
	// "identical", or "diverged".
	Status       string
	AheadBy      int
	BehindBy     int
	TotalCommits int
	Files        []ChangedFile
	// Truncated is true when the files list is a partial view of the diff.
	// The local diff never truncates; the field is kept for shape
	// compatibility with the GitHub compare wire format.
	Truncated bool
}

// ChangedFile is one entry of a diff's file list. Status uses the
// GitHub-compatible vocabulary: added | removed | modified | renamed |
// copied | changed | unchanged.
type ChangedFile struct {
	Filename  string // current path
	Status    string
	Additions int
	Deletions int
	Changes   int
	Patch     string // unified diff hunk; empty for binary or very large files
}
