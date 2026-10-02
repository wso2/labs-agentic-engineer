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

// Package repo is the git-object engine behind ae-studio-tools: one bare clone
// per repo on the studio-data volume (`git clone --bare` plus an explicit
// refspec fetch, never checked out), plumbing reads (rev-parse / ls-tree /
// cat-file), and plumbing writes via a throwaway index (read-tree /
// update-index / write-tree / commit-tree / `push --force-with-lease`). It is
// moved from aep-api's gitfs (which stays until phase 4); snapshots, tags,
// references and diffs return with their callers in phases 3 and 4.
package repo

import (
	"context"
	"errors"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/repo/naming"
)

// Sentinel errors.
var (
	// ErrRefNotFastForward is returned by Mutate when the CAS retry loop
	// exhausts its attempts because origin kept advancing (the analog of the
	// retired REST fast-forward-only ref-update rejection).
	ErrRefNotFastForward = errors.New("github ref: update is not a fast-forward")
	// ErrRefNotFound is returned when `at` (branch, tag, or sha) does not
	// resolve to a commit — the later phases map it to 404.
	ErrRefNotFound = errors.New("git ref not found")
	// ErrPathNotFound is returned by ReadFile / Snapshot.Read when the path
	// does not exist (as a file) in the addressed tree — maps to 404.
	ErrPathNotFound = errors.New("git path not found")
)

// Workspace is the read and write surface of the engine. All methods
// are safe for concurrent use; cross-process integrity on the shared bare
// mirror is arbitrated by a per-repo flock and origin push-CAS.
type Workspace interface {
	// Head resolves `at` to a commit SHA. "" resolves the default-branch tip,
	// "tags/vN" (or a bare tag/branch name) resolves and peels the ref —
	// annotated tags peel to the commit they point at — and a raw 40-hex sha
	// is verified and returned verbatim. Branch/tag addressing fetches origin
	// first; raw shas read local objects, fetching only when missing.
	Head(ctx context.Context, ref RepoRef, at string) (sha string, err error)
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

// RepoRef addresses one repo on the volume. Org/Project/RepoSlug are the path
// key (a pure function of the resolved project, never of client input);
// CloneURL/DefaultBranch drive the remote ops. The engine, not the ref, holds
// the Credential.
type RepoRef struct {
	Org           string
	Project       string
	RepoSlug      string
	CloneURL      string
	DefaultBranch string
}

// FullName is the `<owner>/<repo>` name from CloneURL, for log lines; "" when
// the URL is not a GitHub HTTPS URL. It never includes userinfo.
//
//deadcode:keep wired in Task 2.7
func (r RepoRef) FullName() string {
	owner, name := naming.OwnerRepoFromURL(r.CloneURL)
	if owner == "" {
		return ""
	}
	return owner + "/" + name
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

// GitIdentity is a git author/committer/tagger identity. Field names and
// json tags match the GitHub wire shape. Date is optional (git raw or RFC2822/ISO format accepted by git); empty
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
//
//deadcode:keep wired in Task 2.7
func (p RetryPolicy) withDefaults() RetryPolicy {
	if p.Attempts <= 0 {
		p.Attempts = defaultRetryAttempts
	}
	if len(p.Backoff) == 0 {
		p.Backoff = defaultRetryBackoff
	}
	return p
}
