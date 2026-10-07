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

package aestudiotools

// git.go — the sourcecontrol.Git port over the pod's git content ops. Every
// op names the project's default branch (defaultBranch; omitted, the pod
// reads main). Reads at a 40-hex sha are served from the read cache
// (cache.go); a read at "" or a tag makes the hop and is stored under the
// sha it answered.

import (
	"context"
	"maps"
	"net/http"
	"slices"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/gen"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// Head resolves at to its commit sha. Never cached: "" and a tag move.
func (a *Adapter) Head(ctx context.Context, ref RepoRef, at string, opts ...sourcecontrol.ReadOption) (string, error) {
	if err := validRef(ref); err != nil {
		return "", err
	}
	o := sourcecontrol.ReadOptionsOf(opts...)
	var body gen.Head
	err := a.do(ctx, ref.Org, "get-head", &body, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.GetHead(ctx, ref.Owner, ref.Repo, &gen.GetHeadParams{At: optional(at), Local: optional(o.Local), DefaultBranch: optional(ref.DefaultBranch), XImpersonateOrg: org}, auth)
	})
	return body.Sha, err
}

// listing is list-tree's cached answer.
type listing struct {
	entries []sourcecontrol.Entry
	sha     string
}

// List lists every blob of the tree at `at`.
func (a *Adapter) List(ctx context.Context, ref RepoRef, at string, opts ...sourcecontrol.ReadOption) ([]sourcecontrol.Entry, string, error) {
	if err := validRef(ref); err != nil {
		return nil, "", err
	}
	const op = "list-tree"
	if v, ok := a.cached(ref, at, op, nil); ok {
		l := v.(listing)
		return slices.Clone(l.entries), l.sha, nil
	}
	o := sourcecontrol.ReadOptionsOf(opts...)
	var body gen.Tree
	err := a.do(ctx, ref.Org, op, &body, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.ListTree(ctx, ref.Owner, ref.Repo, &gen.ListTreeParams{At: optional(at), Local: optional(o.Local), DefaultBranch: optional(ref.DefaultBranch), XImpersonateOrg: org}, auth)
	})
	if err != nil {
		return nil, "", err
	}
	var entries []sourcecontrol.Entry
	size := int64(0)
	for _, e := range body.Entries {
		entries = append(entries, sourcecontrol.Entry{Path: e.Path, SHA: e.Sha, Size: e.Size})
		size += int64(len(e.Path) + len(e.Sha) + 8)
	}
	a.store(ref, body.CommitSha, op, nil, listing{entries: slices.Clone(entries), sha: body.CommitSha}, size)
	return entries, body.CommitSha, nil
}

// file is read-file's cached answer.
type file struct {
	content []byte
	blobSHA string
}

// ReadFile reads one file and its blob sha. read-file has no local form, so
// a Local() read of the tip first resolves the mirror's tip (Head, Local)
// and reads at that sha, which is what local means on the pod's other reads.
func (a *Adapter) ReadFile(ctx context.Context, ref RepoRef, at, path string, opts ...sourcecontrol.ReadOption) ([]byte, string, error) {
	if err := validRef(ref); err != nil {
		return nil, "", err
	}
	const op = "read-file"
	if at == "" && sourcecontrol.ReadOptionsOf(opts...).Local {
		sha, err := a.Head(ctx, ref, "", sourcecontrol.Local())
		if err != nil {
			return nil, "", err
		}
		at = sha
	}
	args := struct {
		Path string `json:"path"`
	}{path}
	if v, ok := a.cached(ref, at, op, args); ok {
		f := v.(file)
		return slices.Clone(f.content), f.blobSHA, nil
	}
	var body gen.FileContent
	err := a.do(ctx, ref.Org, op, &body, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.ReadFile(ctx, ref.Owner, ref.Repo, path, &gen.ReadFileParams{At: optional(at), DefaultBranch: optional(ref.DefaultBranch), XImpersonateOrg: org}, auth, literalSlashes)
	})
	if err != nil {
		return nil, "", err
	}
	a.store(ref, body.CommitSha, op, args, file{content: slices.Clone(body.Content), blobSHA: body.Sha}, int64(len(body.Content)+len(body.Sha)))
	return body.Content, body.Sha, nil
}

// bundle is read-bundle's cached answer.
type bundle struct {
	files map[string]string
	sha   string
}

// ReadBundle reads the files f selects at one commit.
func (a *Adapter) ReadBundle(ctx context.Context, ref RepoRef, at string, f sourcecontrol.BundleFilter, opts ...sourcecontrol.ReadOption) (map[string]string, string, error) {
	if err := validRef(ref); err != nil {
		return nil, "", err
	}
	const op = "read-bundle"
	args := bundleArgs(f)
	if v, ok := a.cached(ref, at, op, args); ok {
		b := v.(bundle)
		return maps.Clone(b.files), b.sha, nil
	}
	o := sourcecontrol.ReadOptionsOf(opts...)
	var body gen.Bundle
	err := a.do(ctx, ref.Org, op, &body, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.ReadBundle(ctx, ref.Owner, ref.Repo, &gen.ReadBundleParams{
			At: optional(at), Local: optional(o.Local), Prefix: optional(f.Prefix), Ext: f.Exts, Path: f.Paths, DefaultBranch: optional(ref.DefaultBranch), XImpersonateOrg: org,
		}, auth)
	})
	if err != nil {
		return nil, "", err
	}
	// Each file arrives base64-decoded; string(c) keeps its bytes exactly,
	// binary ones included, and the cache counts the decoded size.
	files := make(map[string]string, len(body.Files))
	size := int64(0)
	for p, c := range body.Files {
		files[p] = string(c)
		size += int64(len(p) + len(c))
	}
	a.store(ref, body.CommitSha, op, args, bundle{files: maps.Clone(files), sha: body.CommitSha}, size)
	return files, body.CommitSha, nil
}

// ListTags lists the tags whose name starts with prefix. Never cached: tags
// come and go. Tags are repository-wide, so no default branch is named.
func (a *Adapter) ListTags(ctx context.Context, ref RepoRef, prefix string, opts ...sourcecontrol.ReadOption) ([]sourcecontrol.TagInfo, error) {
	if err := validRef(ref); err != nil {
		return nil, err
	}
	o := sourcecontrol.ReadOptionsOf(opts...)
	var body gen.TagList
	err := a.do(ctx, ref.Org, "list-tags", &body, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.ListTags(ctx, ref.Owner, ref.Repo, &gen.ListTagsParams{Prefix: prefix, Local: optional(o.Local), XImpersonateOrg: org}, auth)
	})
	if err != nil {
		return nil, err
	}
	var tags []sourcecontrol.TagInfo
	for _, t := range body.Tags {
		info := sourcecontrol.TagInfo{Name: t.Name, CommitHash: t.CommitHash, Message: t.Message, Body: t.Body}
		if t.CreatedAt != nil {
			info.CreatedAt = *t.CreatedAt
		}
		tags = append(tags, info)
	}
	return tags, nil
}

// Tag creates an annotated tag; ErrTagAlreadyExists when the name is taken.
func (a *Adapter) Tag(ctx context.Context, ref RepoRef, spec sourcecontrol.TagSpec) error {
	if err := validRef(ref); err != nil {
		return err
	}
	req := gen.CreateTagRequest{Name: spec.Name, Target: spec.Target, Message: spec.Message, Tagger: identityOf(spec.Tagger)}
	return a.do(ctx, ref.Org, "create-tag", nil, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.CreateTag(ctx, ref.Owner, ref.Repo, &gen.CreateTagParams{DefaultBranch: optional(ref.DefaultBranch), XImpersonateOrg: org}, req, auth)
	})
}

// Commit applies req in one commit on the default branch; a failed baseSha
// is *CommitConflictError naming every such path.
func (a *Adapter) Commit(ctx context.Context, ref RepoRef, req sourcecontrol.CommitRequest) (sourcecontrol.CommitResult, error) {
	if err := validRef(ref); err != nil {
		return sourcecontrol.CommitResult{}, err
	}
	body := gen.CreateCommitRequest{Message: req.Message, Author: identityOf(req.Author), Committer: identityOf(req.Committer)}
	for _, w := range req.Writes {
		body.Writes = append(body.Writes, gen.CommitWrite{Path: w.Path, Content: []byte(w.Content), BaseSha: w.BaseSHA})
	}
	for _, d := range req.Deletes {
		body.Deletes = append(body.Deletes, gen.CommitDelete{Path: d.Path, BaseSha: d.BaseSHA})
	}
	var reply gen.CommitResult
	err := a.do(ctx, ref.Org, "create-commit", &reply, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.CreateCommit(ctx, ref.Owner, ref.Repo, &gen.CreateCommitParams{DefaultBranch: optional(ref.DefaultBranch), XImpersonateOrg: org}, body, auth)
	})
	if err != nil {
		return sourcecontrol.CommitResult{}, err
	}
	res := sourcecontrol.CommitResult{CommitSHA: reply.CommitSha, Changed: reply.Changed}
	for _, f := range reply.Files {
		res.Files = append(res.Files, sourcecontrol.CommittedFile{Path: f.Path, SHA: f.Sha})
	}
	for _, w := range reply.Warnings {
		res.Warnings = append(res.Warnings, sourcecontrol.CommitWarning{Path: w.Path, Code: w.Code, Message: w.Message})
	}
	return res, nil
}

// identityOf is a git identity on the wire; nil is the zero value the
// generated client omits (the pod's default).
func identityOf(id *sourcecontrol.GitIdentity) gen.GitIdentity {
	if id == nil {
		return gen.GitIdentity{}
	}
	return gen.GitIdentity{Name: id.Name, Email: id.Email}
}

// cached answers the read op of ref at `at` from the read cache. Only a
// 40-hex at addresses immutable content; anything else makes the hop.
func (a *Adapter) cached(ref RepoRef, at, op string, args any) (any, bool) {
	if !isCommitSHA(at) {
		return nil, false
	}
	return a.reads.get(readKey(ref, at, op, args))
}

// store keeps an answer read at sha (what the pod answered, never the
// requested at) when sha is a 40-hex commit id.
func (a *Adapter) store(ref RepoRef, sha, op string, args, value any, size int64) {
	if !isCommitSHA(sha) {
		return
	}
	a.reads.put(readKey(ref, sha, op, args), value, size)
}
