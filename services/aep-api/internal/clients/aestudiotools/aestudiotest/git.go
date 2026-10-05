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

package aestudiotest

// git.go — sourcecontrol.Git over an in-memory commit graph: one branch per
// repository, commits addressed by a sha over (parent, tree), annotated tags.

import (
	"context"
	"crypto/sha1" //nolint:gosec // git object ids are sha1 by definition
	"encoding/hex"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// bundleExtPattern is the contract's `ext` item pattern (ae-studio-tools
// internal/v1 openapi.yaml, read-bundle).
var bundleExtPattern = regexp.MustCompile(`^\.[A-Za-z0-9._-]{1,32}$`)

const maxBundleExts = 20

// atPattern is the contract's `at` (ae-studio-tools internal/v1 openapi.yaml,
// parameter At): a tag or a full lowercase sha; "" (the tip) is the omitted
// parameter.
var atPattern = regexp.MustCompile(`^(tags/[A-Za-z0-9._/-]{1,200}|[0-9a-f]{40})$`)

// podRefusal is the error a pod 400 validation_failed becomes in production
// (permanent, so sourcecontrol.IsPermanent classifies it alike).
func podRefusal(op, detail string) error {
	return &aestudiotools.StatusError{Op: op, Status: 400, Code: "validation_failed", Detail: detail}
}

// validAt refuses an `at` the pod refuses, and an `at` beside local (the pod
// reads the mirror's tip with local, else 400).
func validAt(op, at string, local bool) error {
	if at != "" && !atPattern.MatchString(at) {
		return podRefusal(op, "at must be tags/<name> or a full sha")
	}
	if local && at != "" {
		return podRefusal(op, "local reads the default-branch tip, not at")
	}
	return nil
}

// validBundleFilter refuses what the pod's request validator answers 400 to.
func validBundleFilter(filter sourcecontrol.BundleFilter) error {
	if len(filter.Exts) > maxBundleExts {
		return podRefusal(OpReadBundle, "invalid read-bundle filter")
	}
	for _, e := range filter.Exts {
		if !bundleExtPattern.MatchString(e) {
			return podRefusal(OpReadBundle, "invalid read-bundle filter")
		}
	}
	return nil
}

// repoKey is one repository regardless of the DefaultBranch a ref carries.
type repoKey struct{ org, owner, repo string }

func keyOf(ref sourcecontrol.RepoRef) repoKey {
	return repoKey{org: ref.Org, owner: ref.Owner, repo: ref.Repo}
}

// commit is one immutable tree.
type commit struct {
	sha   string
	files map[string]string
}

// repoState is one repository. created is false for a repository only the
// issue side has touched (GitHub state without seeded content): its git
// reads answer ErrRepoNotFound.
type repoState struct {
	created    bool
	head       string
	commits    map[string]*commit
	tags       map[string]sourcecontrol.TagInfo
	issues     map[int]*issue
	pulls      map[int]*pull
	nextNumber int
	milestones []*sourcecontrol.Milestone
	labels     map[string]string
	hooks      map[int64][]string
	// podHook is the id of the hook delivering to the pod's own URL (0: none);
	// every other hook belongs to another integration.
	podHook int64
}

func newRepoState() *repoState {
	return &repoState{
		commits: map[string]*commit{},
		tags:    map[string]sourcecontrol.TagInfo{},
		issues:  map[int]*issue{},
		pulls:   map[int]*pull{},
		labels:  map[string]string{},
		hooks:   map[int64][]string{},
	}
}

// state answers ref's repository, creating its GitHub-side state on first
// use. Caller holds f.mu.
func (f *Fake) state(ref sourcecontrol.RepoRef) *repoState {
	k := keyOf(ref)
	st, ok := f.repos[k]
	if !ok {
		st = newRepoState()
		f.repos[k] = st
	}
	return st
}

// content answers ref's repository when it has git content. Caller holds f.mu.
func (f *Fake) content(ref sourcecontrol.RepoRef) (*repoState, error) {
	st, ok := f.repos[keyOf(ref)]
	if !ok || !st.created {
		return nil, sourcecontrol.ErrRepoNotFound
	}
	return st, nil
}

// SeedRepo creates ref's repository (or resets its content) with one commit
// holding files. Issues, milestones and hooks already on it are kept.
func (f *Fake) SeedRepo(ref sourcecontrol.RepoRef, files map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.state(ref)
	st.created = true
	st.commits = map[string]*commit{}
	st.tags = map[string]sourcecontrol.TagInfo{}
	st.head = st.addCommit("", maps.Clone(files))
}

// addCommit stores a commit of files on parent and answers its sha.
func (st *repoState) addCommit(parent string, files map[string]string) string {
	if files == nil {
		files = map[string]string{}
	}
	h := sha1.New() //nolint:gosec // git object ids are sha1 by definition
	h.Write([]byte(parent + "\n"))
	for _, p := range slices.Sorted(maps.Keys(files)) {
		h.Write([]byte(p + " " + blobSHA(files[p]) + "\n"))
	}
	sha := hex.EncodeToString(h.Sum(nil))
	st.commits[sha] = &commit{sha: sha, files: files}
	return sha
}

// blobSHA is git's blob id of content.
func blobSHA(content string) string {
	h := sha1.New() //nolint:gosec // git object ids are sha1 by definition
	h.Write([]byte("blob " + strconv.Itoa(len(content)) + "\x00" + content))
	return hex.EncodeToString(h.Sum(nil))
}

// resolve answers the commit `at` names: "" the tip, "tags/<name>", or a
// commit sha.
func (st *repoState) resolve(at string) (*commit, error) {
	sha := at
	switch {
	case at == "":
		sha = st.head
	case strings.HasPrefix(at, "tags/"):
		t, ok := st.tags[strings.TrimPrefix(at, "tags/")]
		if !ok {
			return nil, sourcecontrol.ErrRefNotFound
		}
		sha = t.CommitHash
	}
	c, ok := st.commits[sha]
	if !ok {
		return nil, sourcecontrol.ErrRefNotFound
	}
	return c, nil
}

// read begins a Git read and resolves its commit.
func (f *Fake) read(op string, ref sourcecontrol.RepoRef, at string, filter sourcecontrol.BundleFilter, opts []sourcecontrol.ReadOption) (*commit, error) {
	o := sourcecontrol.ReadOptionsOf(opts...)
	if err := f.begin(Call{Op: op, Ref: ref, At: at, Local: o.Local, Filter: filter}); err != nil {
		return nil, err
	}
	// The pod validates before any git work.
	if err := validAt(op, at, o.Local); err != nil {
		return nil, err
	}
	if err := validBundleFilter(filter); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	st, err := f.content(ref)
	if err != nil {
		return nil, err
	}
	return st.resolve(at)
}

// Head resolves at to its commit sha.
func (f *Fake) Head(_ context.Context, ref sourcecontrol.RepoRef, at string, opts ...sourcecontrol.ReadOption) (string, error) {
	c, err := f.read(OpHead, ref, at, sourcecontrol.BundleFilter{}, opts)
	if err != nil {
		return "", err
	}
	return c.sha, nil
}

// List lists every blob at `at`, by path.
func (f *Fake) List(_ context.Context, ref sourcecontrol.RepoRef, at string, opts ...sourcecontrol.ReadOption) ([]sourcecontrol.Entry, string, error) {
	c, err := f.read(OpList, ref, at, sourcecontrol.BundleFilter{}, opts)
	if err != nil {
		return nil, "", err
	}
	entries := make([]sourcecontrol.Entry, 0, len(c.files))
	for _, p := range slices.Sorted(maps.Keys(c.files)) {
		entries = append(entries, sourcecontrol.Entry{Path: p, SHA: blobSHA(c.files[p]), Size: int64(len(c.files[p]))})
	}
	return entries, c.sha, nil
}

// ReadFile reads one file; ErrPathNotFound when the tree has no such file.
func (f *Fake) ReadFile(_ context.Context, ref sourcecontrol.RepoRef, at, path string, opts ...sourcecontrol.ReadOption) ([]byte, string, error) {
	c, err := f.read(OpReadFile, ref, at, sourcecontrol.BundleFilter{}, opts)
	if err != nil {
		return nil, "", err
	}
	content, ok := c.files[path]
	if !ok {
		return nil, "", sourcecontrol.ErrPathNotFound
	}
	return []byte(content), blobSHA(content), nil
}

// ReadBundle reads the files filter selects, as the pod does: exactly Paths
// (absent ones skipped) when named, else the paths under Prefix ending in one
// of Exts.
func (f *Fake) ReadBundle(_ context.Context, ref sourcecontrol.RepoRef, at string, filter sourcecontrol.BundleFilter, opts ...sourcecontrol.ReadOption) (map[string]string, string, error) {
	c, err := f.read(OpReadBundle, ref, at, filter, opts)
	if err != nil {
		return nil, "", err
	}
	files := map[string]string{}
	for p, content := range c.files {
		if bundleKeeps(filter, p) {
			files[p] = content
		}
	}
	return files, c.sha, nil
}

func bundleKeeps(filter sourcecontrol.BundleFilter, path string) bool {
	if len(filter.Paths) > 0 {
		return slices.Contains(filter.Paths, path)
	}
	if !strings.HasPrefix(path, filter.Prefix) {
		return false
	}
	return len(filter.Exts) == 0 || slices.ContainsFunc(filter.Exts, func(e string) bool { return strings.HasSuffix(path, e) })
}

// ListTags lists the tags whose name starts with prefix, by name.
func (f *Fake) ListTags(_ context.Context, ref sourcecontrol.RepoRef, prefix string, opts ...sourcecontrol.ReadOption) ([]sourcecontrol.TagInfo, error) {
	o := sourcecontrol.ReadOptionsOf(opts...)
	if err := f.begin(Call{Op: OpListTags, Ref: ref, Local: o.Local}); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	st, err := f.content(ref)
	if err != nil {
		return nil, err
	}
	var out []sourcecontrol.TagInfo
	for _, name := range slices.Sorted(maps.Keys(st.tags)) {
		if strings.HasPrefix(name, prefix) {
			out = append(out, st.tags[name])
		}
	}
	return out, nil
}

// Tag creates an annotated tag at spec.Target ("" = the tip).
func (f *Fake) Tag(_ context.Context, ref sourcecontrol.RepoRef, spec sourcecontrol.TagSpec) error {
	if err := f.begin(Call{Op: OpTag, Ref: ref, At: spec.Target}); err != nil {
		return err
	}
	if err := validAt(OpTag, spec.Target, false); err != nil {
		return err
	}
	if strings.TrimSpace(spec.Message) == "" {
		return podRefusal(OpTag, "empty tag message")
	}
	f.mu.Lock()
	hook := f.beforeTag
	f.mu.Unlock()
	if hook != nil {
		hook(spec)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	st, err := f.content(ref)
	if err != nil {
		return err
	}
	if _, taken := st.tags[spec.Name]; taken {
		return sourcecontrol.ErrTagAlreadyExists
	}
	c, err := st.resolve(spec.Target)
	if err != nil {
		return err
	}
	st.tags[spec.Name] = sourcecontrol.TagInfo{Name: spec.Name, CommitHash: c.sha, Message: spec.Message, CreatedAt: f.tick()}
	return nil
}

// Commit applies req on the tip in one commit, or answers
// *CommitConflictError naming every path whose baseSha did not hold.
func (f *Fake) Commit(_ context.Context, ref sourcecontrol.RepoRef, req sourcecontrol.CommitRequest) (sourcecontrol.CommitResult, error) {
	committer := req.Committer
	if committer == nil {
		committer = req.Author
	}
	if err := f.begin(Call{Op: OpCommit, Ref: ref, Author: req.Author, Committer: committer}); err != nil {
		return sourcecontrol.CommitResult{}, err
	}
	f.mu.Lock()
	hook := f.beforeCommit
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	if err := validCommit(req); err != nil {
		return sourcecontrol.CommitResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	st, err := f.content(ref)
	if err != nil {
		return sourcecontrol.CommitResult{}, err
	}
	return st.apply(req)
}

// validCommit refuses what the pod answers 400 to: an empty message, no
// change, a repeated path, or a delete without its baseSha.
func validCommit(req sourcecontrol.CommitRequest) error {
	invalid := podRefusal(OpCommit, "invalid commit request")
	if strings.TrimSpace(req.Message) == "" {
		return podRefusal(OpCommit, "empty commit message")
	}
	if len(req.Writes)+len(req.Deletes) == 0 {
		return invalid
	}
	seen := map[string]bool{}
	for _, w := range req.Writes {
		if w.Path == "" || seen[w.Path] {
			return invalid
		}
		seen[w.Path] = true
	}
	for _, d := range req.Deletes {
		if d.Path == "" || d.BaseSHA == "" || seen[d.Path] {
			return invalid
		}
		seen[d.Path] = true
	}
	return nil
}

// apply commits req on the tip. Caller holds f.mu.
func (st *repoState) apply(req sourcecontrol.CommitRequest) (sourcecontrol.CommitResult, error) {
	tip := st.commits[st.head]
	var conflicts []sourcecontrol.Conflict
	current := func(path string) string {
		if content, ok := tip.files[path]; ok {
			return blobSHA(content)
		}
		return ""
	}
	for _, w := range req.Writes {
		if cur := current(w.Path); cur != w.BaseSHA {
			conflicts = append(conflicts, sourcecontrol.Conflict{Path: w.Path, BaseSHA: w.BaseSHA, CurrentSHA: cur})
		}
	}
	for _, d := range req.Deletes {
		if cur := current(d.Path); cur != d.BaseSHA {
			conflicts = append(conflicts, sourcecontrol.Conflict{Path: d.Path, BaseSHA: d.BaseSHA, CurrentSHA: cur})
		}
	}
	if len(conflicts) > 0 {
		return sourcecontrol.CommitResult{}, &sourcecontrol.CommitConflictError{Conflicts: conflicts}
	}

	files := maps.Clone(tip.files)
	written := make([]sourcecontrol.CommittedFile, 0, len(req.Writes))
	for _, w := range req.Writes {
		files[w.Path] = w.Content
		written = append(written, sourcecontrol.CommittedFile{Path: w.Path, SHA: blobSHA(w.Content)})
	}
	for _, d := range req.Deletes {
		delete(files, d.Path)
	}
	if maps.Equal(files, tip.files) {
		return sourcecontrol.CommitResult{CommitSHA: tip.sha, Files: written}, nil
	}
	st.head = st.addCommit(tip.sha, files)
	return sourcecontrol.CommitResult{CommitSHA: st.head, Changed: true, Files: written}, nil
}
