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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"unicode"

	"github.com/wso2/aep/ae-studio-tools/internal/gen"
)

// The /internal/v1/repos/{owner}/{repo} git content ops (05 §3): thin
// wrappers over the Workspace, addressed by GitHub owner/repo, so aep-api's
// calls and the Room's land on one mirror. No path allow-list (the Files
// read rules are the Room's, not aep-api's). The edge embeds Handler in its
// /internal/v1 server; the gate, the body cap, the request validator and the
// owner guard ran before any method here.

// IdentitySource names the gitpat user a commit is authored by when the
// request names no author (github.CommitAuthor).
type IdentitySource interface {
	Identity(ctx context.Context) (name, email string, err error)
}

// GitHubCloneURL is a GitHub repository's https clone URL.
func GitHubCloneURL(owner, name string) string {
	return "https://github.com/" + owner + "/" + name + ".git"
}

// Handler serves the git content ops over a Workspace.
type Handler struct {
	ws Workspace
	// identity authors a commit whose request names no author; nil (or a
	// failed lookup) commits as the engine's AEP default.
	identity IdentitySource
	// cloneURL addresses an owner/repo's origin (GitHubCloneURL).
	cloneURL func(owner, name string) string
	// owner is the org's connected GitHub account (AE_GITHUB_OWNER), the
	// only owner trash-repo acts for (its owner is in the body, outside the
	// edge's path guard); empty refuses every trash.
	owner string
}

// HandlerOption configures a Handler.
type HandlerOption func(*Handler)

// WithOwner names the org's connected GitHub account.
func WithOwner(owner string) HandlerOption {
	return func(h *Handler) { h.owner = owner }
}

// NewHandler serves ws, cloning owner/repo from cloneURL(owner, repo).
func NewHandler(ws Workspace, identity IdentitySource, cloneURL func(owner, name string) string, opts ...HandlerOption) Handler {
	h := Handler{ws: ws, identity: identity, cloneURL: cloneURL}
	for _, o := range opts {
		o(&h)
	}
	return h
}

// errBadRequest is a request the contract allows but the op refuses (400
// validation_failed); errBadPath one naming a path git cannot hold (400
// path_invalid).
var (
	errBadRequest = errors.New("request refused")
	errBadPath    = errors.New("path refused")
)

// GetHead resolves at (local: the mirror's tip, no fetch) to its commit.
func (h Handler) GetHead(ctx context.Context, req gen.GetHeadRequestObject) (gen.GetHeadResponseObject, error) {
	ref, err := h.ref(req.Owner, req.Repo, req.Params.DefaultBranch)
	if err == nil {
		var sha string
		if sha, err = h.resolve(ctx, ref, req.Params.At, req.Params.Local); err == nil {
			return gen.GetHead200JSONResponse{Sha: sha}, nil
		}
	}
	return h.problem(ctx, "get-head", req.Owner, req.Repo, err)
}

// ListTree lists every file of the commit at names, under prefix.
func (h Handler) ListTree(ctx context.Context, req gen.ListTreeRequestObject) (gen.ListTreeResponseObject, error) {
	ref, err := h.ref(req.Owner, req.Repo, req.Params.DefaultBranch)
	if err == nil {
		var at string
		if at, err = h.pin(ctx, ref, req.Params.At, req.Params.Local); err == nil {
			var entries []Entry
			var sha string
			if entries, sha, err = h.ws.List(ctx, ref, at); err == nil {
				out := []gen.TreeEntry{}
				for _, e := range entries {
					if strings.HasPrefix(e.Path, req.Params.Prefix) {
						out = append(out, gen.TreeEntry{Path: e.Path, Sha: e.SHA, Size: e.Size})
					}
				}
				return gen.ListTree200JSONResponse{CommitSha: sha, Entries: out}, nil
			}
		}
	}
	return h.problem(ctx, "list-tree", req.Owner, req.Repo, err)
}

// ReadFile reads one file of the commit at names. The commit is resolved
// first, so the answer names the commit the content was read at.
func (h Handler) ReadFile(ctx context.Context, req gen.ReadFileRequestObject) (gen.ReadFileResponseObject, error) {
	ref, err := h.ref(req.Owner, req.Repo, req.Params.DefaultBranch)
	if err == nil {
		err = checkPath(req.Path)
	}
	if err == nil {
		var sha string
		if sha, err = h.resolve(ctx, ref, req.Params.At, false); err == nil {
			var content []byte
			var blob string
			if content, blob, err = h.ws.ReadFile(ctx, ref, sha, req.Path); err == nil {
				return gen.ReadFile200JSONResponse{CommitSha: sha, Path: req.Path, Sha: blob, Content: content}, nil
			}
		}
	}
	return h.problem(ctx, "read-file", req.Owner, req.Repo, err)
}

// ReadBundle reads the named files (path), else every file under prefix
// ending in one of ext, at one commit.
func (h Handler) ReadBundle(ctx context.Context, req gen.ReadBundleRequestObject) (gen.ReadBundleResponseObject, error) {
	ref, err := h.ref(req.Owner, req.Repo, req.Params.DefaultBranch)
	var keep func(string) bool
	if err == nil {
		keep, err = bundleFilter(req.Params.Path, req.Params.Prefix, req.Params.Ext)
	}
	if err == nil {
		var at string
		if at, err = h.pin(ctx, ref, req.Params.At, req.Params.Local); err == nil {
			var files map[string]string
			var sha string
			if files, sha, err = h.ws.ReadBundle(ctx, ref, at, keep); err == nil {
				return gen.ReadBundle200JSONResponse{CommitSha: sha, Files: bundleBytes(files)}, nil
			}
		}
	}
	return h.problem(ctx, "read-bundle", req.Owner, req.Repo, err)
}

// bundleBytes is a bundle as the wire carries it: each file's bytes, which
// the JSON encoder sends as base64, so a binary file survives the trip.
func bundleBytes(files map[string]string) map[string][]byte {
	out := make(map[string][]byte, len(files))
	for p, c := range files {
		out[p] = []byte(c)
	}
	return out
}

// bundleFilter keeps exactly paths when any are named, else the paths under
// prefix that end in one of ext (any ending when ext is empty).
func bundleFilter(paths []string, prefix string, ext []string) (func(string) bool, error) {
	if len(paths) > 0 {
		exact := make(map[string]bool, len(paths))
		for _, p := range paths {
			if err := checkPath(p); err != nil {
				return nil, err
			}
			exact[p] = true
		}
		return func(rel string) bool { return exact[rel] }, nil
	}
	return func(rel string) bool {
		if !strings.HasPrefix(rel, prefix) {
			return false
		}
		if len(ext) == 0 {
			return true
		}
		for _, e := range ext {
			if strings.HasSuffix(rel, e) {
				return true
			}
		}
		return false
	}, nil
}

// ListTags lists the tags whose name starts with prefix (local: the
// mirror's, no fetch). The prefix is matched literally, so one carrying a
// character outside the ref-name charset (a glob character would widen the
// for-each-ref pattern) is refused.
func (h Handler) ListTags(ctx context.Context, req gen.ListTagsRequestObject) (gen.ListTagsResponseObject, error) {
	ref, err := h.ref(req.Owner, req.Repo, "")
	if err == nil && !validTagPrefix(req.Params.Prefix) {
		err = fmt.Errorf("%w: tag prefix outside the ref-name charset", errBadRequest)
	}
	if err == nil {
		var tags []TagInfo
		if req.Params.Local {
			tags, err = h.ws.ListTagsLocal(ctx, ref, req.Params.Prefix)
		} else {
			tags, err = h.ws.ListTags(ctx, ref, req.Params.Prefix)
		}
		if err == nil {
			out := make([]gen.Tag, 0, len(tags))
			for _, t := range tags {
				tag := gen.Tag{Name: t.Name, CommitHash: t.CommitHash, Message: t.Message}
				if !t.CreatedAt.IsZero() {
					created := t.CreatedAt
					tag.CreatedAt = &created
				}
				out = append(out, tag)
			}
			return gen.ListTags200JSONResponse{Tags: out}, nil
		}
	}
	return h.problem(ctx, "list-tags", req.Owner, req.Repo, err)
}

// CreateTag cuts an annotated tag at target (the default-branch tip when
// empty) and pushes it.
func (h Handler) CreateTag(ctx context.Context, req gen.CreateTagRequestObject) (gen.CreateTagResponseObject, error) {
	ref, err := h.ref(req.Owner, req.Repo, req.Params.DefaultBranch)
	var spec TagSpec
	if err == nil {
		spec, err = tagSpec(req.Body)
	}
	if err == nil {
		if err = h.ws.Tag(ctx, ref, spec); err == nil {
			return gen.CreateTag201JSONResponse{}, nil
		}
	}
	return h.problem(ctx, "create-tag", req.Owner, req.Repo, err)
}

// tagSpec checks a create-tag body and converts it.
func tagSpec(body *gen.CreateTagRequest) (TagSpec, error) {
	if body == nil {
		return TagSpec{}, fmt.Errorf("%w: no body", errBadRequest)
	}
	if err := checkRefName(body.Name); err != nil {
		return TagSpec{}, err
	}
	if err := checkAt(body.Target); err != nil {
		return TagSpec{}, err
	}
	if strings.TrimSpace(body.Message) == "" {
		return TagSpec{}, fmt.Errorf("%w: empty tag message", errBadRequest)
	}
	tagger, err := identity(body.Tagger)
	if err != nil {
		return TagSpec{}, err
	}
	return TagSpec{Name: body.Name, Target: body.Target, Message: body.Message, Tagger: tagger}, nil
}

// CreateCommit commits the writes then the deletes as one commit on the
// default branch, each pinned to its baseSha. The engine's commit only: no
// scaffolding, soft validation or completions, so warnings is empty.
func (h Handler) CreateCommit(ctx context.Context, req gen.CreateCommitRequestObject) (gen.CreateCommitResponseObject, error) {
	ref, err := h.ref(req.Owner, req.Repo, req.Params.DefaultBranch)
	var c commitRequest
	if err == nil {
		c, err = parseCommit(req.Body)
	}
	if err == nil {
		author := c.author
		if author == nil {
			author = AuthorOf(ctx, h.identity, "create-commit")
		}
		res, conflicts, cerr := h.ws.Commit(ctx, ref, c.writes, c.deletes, c.message, author, c.committer)
		switch {
		case cerr == nil:
			files := make([]gen.CommitFile, 0, len(c.writes))
			for _, w := range c.writes {
				files = append(files, gen.CommitFile{Path: w.Path, Sha: BlobSHA(w.Content)})
			}
			return gen.CreateCommit200JSONResponse{CommitSha: res.CommitSHA, Changed: res.Changed, Files: files, Warnings: []gen.CommitWarning{}}, nil
		case errors.Is(cerr, ErrCommitConflict):
			out := make([]gen.Conflict, 0, len(conflicts))
			for _, cf := range conflicts {
				out = append(out, gen.Conflict{Path: cf.Path, BaseSha: cf.BaseSHA, CurrentSha: cf.CurrentSHA})
			}
			return gen.CreateCommit409ApplicationProblemPlusJSONResponse{
				Type: "about:blank", Title: http.StatusText(http.StatusConflict), Status: http.StatusConflict,
				Code: "conflict", Detail: "a baseSha no longer holds at the tip; nothing was applied", Conflicts: out,
			}, nil
		}
		err = cerr
	}
	return h.problem(ctx, "create-commit", req.Owner, req.Repo, err)
}

// commitRequest is a checked create-commit body in the engine's types.
type commitRequest struct {
	writes            []CommitWrite
	deletes           []CommitDelete
	message           string
	author, committer *GitIdentity
}

// parseCommit checks a create-commit body: a message, at least one write or
// delete, every path one git can hold and named once (a path both written
// and deleted would be deleted), every baseSha empty or a full sha.
func parseCommit(body *gen.CreateCommitRequest) (commitRequest, error) {
	if body == nil {
		return commitRequest{}, fmt.Errorf("%w: no body", errBadRequest)
	}
	if strings.TrimSpace(body.Message) == "" {
		return commitRequest{}, fmt.Errorf("%w: empty commit message", errBadRequest)
	}
	if len(body.Writes)+len(body.Deletes) == 0 {
		return commitRequest{}, fmt.Errorf("%w: no write or delete", errBadRequest)
	}
	seen := map[string]bool{}
	check := func(path, baseSHA string) error {
		if err := checkPath(path); err != nil {
			return err
		}
		if seen[path] {
			return fmt.Errorf("%w: %q named twice", errBadRequest, path)
		}
		seen[path] = true
		if baseSHA != "" && !isHex40(baseSHA) {
			return fmt.Errorf("%w: baseSha of %q is not a full sha", errBadRequest, path)
		}
		return nil
	}
	c := commitRequest{message: body.Message}
	for _, w := range body.Writes {
		if err := check(w.Path, w.BaseSha); err != nil {
			return commitRequest{}, err
		}
		c.writes = append(c.writes, CommitWrite{Path: w.Path, Content: w.Content, BaseSHA: w.BaseSha})
	}
	for _, d := range body.Deletes {
		if err := check(d.Path, d.BaseSha); err != nil {
			return commitRequest{}, err
		}
		c.deletes = append(c.deletes, CommitDelete{Path: d.Path, BaseSHA: d.BaseSha})
	}
	var err error
	if c.author, err = identity(body.Author); err != nil {
		return commitRequest{}, err
	}
	if c.committer, err = identity(body.Committer); err != nil {
		return commitRequest{}, err
	}
	return c, nil
}

// AuthorOf is the gitpat user as src names it, or nil (the engine's AEP
// default) when there is no source or the lookup failed: a failed lookup
// does not gate the commit (20 §5); it is logged under op.
func AuthorOf(ctx context.Context, src IdentitySource, op string) *GitIdentity {
	if src == nil {
		return nil
	}
	name, email, err := src.Identity(ctx)
	if err != nil {
		slog.WarnContext(ctx, "commit.identity_unavailable", "op", op)
		return nil
	}
	return &GitIdentity{Name: name, Email: email}
}

// identity converts an optional request identity: nil when absent, else
// both fields set and free of what git would mangle (controls, angle
// brackets).
func identity(id gen.GitIdentity) (*GitIdentity, error) {
	if id.Name == "" && id.Email == "" {
		return nil, nil
	}
	for _, v := range []string{id.Name, id.Email} {
		if strings.TrimSpace(v) == "" || len(v) > 256 || strings.ContainsAny(v, "<>") || strings.IndexFunc(v, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("%w: invalid git identity", errBadRequest)
		}
	}
	return &GitIdentity{Name: id.Name, Email: id.Email}, nil
}

// ref addresses owner/repo's mirror, cloned from GitHub, with the caller's
// default branch ("" is the engine's main).
func (h Handler) ref(owner, name, branch string) (RepoRef, error) {
	return NewRepoRef(owner, name, branch, h.cloneURL)
}

// NewRepoRef addresses owner/name's mirror, cloned from cloneURL(owner, name),
// with the caller's default branch ("" is the engine's main). A name the
// store cannot hold, or a branch git would refuse, is a 400 for Problem.
func NewRepoRef(owner, name, branch string, cloneURL func(owner, name string) string) (RepoRef, error) {
	ref := RepoRef{Owner: owner, Repo: name, CloneURL: cloneURL(owner, name), DefaultBranch: branch}
	if err := ref.Validate(); err != nil {
		return RepoRef{}, fmt.Errorf("%w: %w", errBadRequest, err)
	}
	if branch != "" {
		if err := checkRefName(branch); err != nil {
			return RepoRef{}, err
		}
	}
	return ref, nil
}

// pin is the address a read uses: at, or with local the mirror's tip (a
// sha, so the read does not fetch either).
func (h Handler) pin(ctx context.Context, ref RepoRef, at string, local bool) (string, error) {
	if err := checkAt(at); err != nil {
		return "", err
	}
	if !local {
		return at, nil
	}
	if at != "" {
		return "", fmt.Errorf("%w: local reads the default-branch tip, not at", errBadRequest)
	}
	return h.ws.HeadLocal(ctx, ref)
}

// resolve is pin resolved to its commit.
func (h Handler) resolve(ctx context.Context, ref RepoRef, at string, local bool) (string, error) {
	pinned, err := h.pin(ctx, ref, at, local)
	if err != nil || local {
		return pinned, err
	}
	return h.ws.Head(ctx, ref, pinned)
}

// checkAt accepts "" (the default-branch tip), tags/<name> or a full sha.
func checkAt(at string) error {
	if at == "" || isHex40(at) {
		return nil
	}
	if name, ok := strings.CutPrefix(at, "tags/"); ok {
		return checkRefName(name)
	}
	return fmt.Errorf("%w: at must be tags/<name> or a full sha", errBadRequest)
}

// checkPath refuses a path no tree can hold (validateTreePath).
func checkPath(path string) error {
	if err := validateTreePath(path); err != nil {
		return fmt.Errorf("%w: %w", errBadPath, err)
	}
	return nil
}

// checkRefName refuses a branch or tag name git would refuse
// (git-check-ref-format), and one starting with '-' (an option to git).
func checkRefName(name string) error {
	if !validRefName(name) {
		return fmt.Errorf("%w: %q is not a valid ref name", errBadRequest, name)
	}
	return nil
}

// problem is Problem as this handler's reply.
func (h Handler) problem(ctx context.Context, op, owner, name string, err error) (problemReply, error) {
	p, err := Problem(ctx, op, owner, name, err)
	return problemReply(p), err
}

// Problem maps a git op's failure on owner/name to its answer, for every
// handler over the engine. The caller leaving is the caller's (its ctx
// error, the generic 500, which it never sees); an engine error's text
// (git's stderr, the clone URL) never reaches the answer or a log line.
func Problem(ctx context.Context, op, owner, name string, err error) (gen.Problem, error) {
	if ctx.Err() != nil {
		return gen.Problem{}, ctx.Err()
	}
	repoName := strings.ToLower(owner + "/" + name)
	switch {
	case errors.Is(err, errBadPath):
		return NewProblem(http.StatusBadRequest, "path_invalid", "the request names a path git cannot hold"), nil
	case errors.Is(err, errBadRequest):
		return NewProblem(http.StatusBadRequest, "validation_failed", "the request does not match the contract"), nil
	case errors.Is(err, ErrRefNotFound):
		return NewProblem(http.StatusNotFound, "ref_not_found", "the ref names no commit in this repository"), nil
	case errors.Is(err, ErrPathNotFound):
		return NewProblem(http.StatusNotFound, "path_not_found", "no such file at this commit"), nil
	case errors.Is(err, ErrTagAlreadyExists):
		return NewProblem(http.StatusConflict, "tag_exists", "the tag name is taken"), nil
	case errors.Is(err, ErrCommitConflict):
		slog.WarnContext(ctx, "repo.commit_conflict", "op", op, "repo", repoName)
		return NewProblem(http.StatusConflict, "conflict", "the tree kept changing under the commit; nothing was applied"), nil
	case errors.Is(err, ErrRefNotFastForward):
		slog.WarnContext(ctx, "repo.not_fast_forward", "op", op, "repo", repoName)
		return NewProblem(http.StatusConflict, "not_fast_forward", "the branch moved during the commit; re-read and retry"), nil
	case errors.Is(err, ErrDiskFull):
		slog.WarnContext(ctx, "repo.disk_full", "op", op, "repo", repoName)
		return NewProblem(http.StatusServiceUnavailable, "disk_full", "the studio's disk is full"), nil
	default:
		status := remoteHTTPStatus(err)
		slog.WarnContext(ctx, "repo.git_failed", "op", op, "repo", repoName, "githubStatus", status)
		p := NewProblem(http.StatusBadGateway, "github_error", "the repository could not be reached")
		if status != 0 {
			p.GithubStatus = status
			p.Detail = fmt.Sprintf("GitHub answered %d", status)
		}
		return p, nil
	}
}

// NewProblem is an RFC 9457 problem with status, code and detail.
func NewProblem(status int, code, detail string) gen.Problem {
	return gen.Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Code: code, Detail: detail}
}

// problemReply is a problem answer for any git op: it satisfies every op's
// generated response interface, which one per-status type per op cannot.
type problemReply gen.Problem

func newProblemReply(status int, code, detail string) problemReply {
	return problemReply(NewProblem(status, code, detail))
}

func (p problemReply) write(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	return json.NewEncoder(w).Encode(gen.Problem(p))
}

// VisitGetHeadResponse implements gen.GetHeadResponseObject.
func (p problemReply) VisitGetHeadResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitListTreeResponse implements gen.ListTreeResponseObject.
func (p problemReply) VisitListTreeResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitReadFileResponse implements gen.ReadFileResponseObject.
func (p problemReply) VisitReadFileResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitReadBundleResponse implements gen.ReadBundleResponseObject.
func (p problemReply) VisitReadBundleResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitListTagsResponse implements gen.ListTagsResponseObject.
func (p problemReply) VisitListTagsResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitCreateTagResponse implements gen.CreateTagResponseObject.
func (p problemReply) VisitCreateTagResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitCreateCommitResponse implements gen.CreateCommitResponseObject.
func (p problemReply) VisitCreateCommitResponse(w http.ResponseWriter) error { return p.write(w) }

// validTagPrefix is the contract's TagPrefix: letters, digits and . _ / -
// only, so no glob character (* ? [ \) reaches a for-each-ref pattern.
func validTagPrefix(prefix string) bool {
	for _, r := range prefix {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '/', r == '-':
		default:
			return false
		}
	}
	return true
}
