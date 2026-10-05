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

package mcp

// remote_git.go is the read-only GitHub REST client behind the two tools the
// pod serves itself (get_remote_git_file_contents, search_remote_git_code),
// copied from aep-api's internal/dependencies/mcpdiscovery/remote_git.go,
// which stays for the legacy runner until phase 5. It exposes exactly two
// GitHub reads:
//
//   - Contents API GET /repos/{owner}/{repo}/contents/{path}?ref=  (file or dir)
//   - Code Search GET /search/code?q=<query>+repo:{owner}/{repo}
//
// There is no create/update/delete/branch/PR surface. Two guardrails bound it:
//
//  1. Owner must be the org's. Every read refuses (ErrOwnerNotInOrg) an owner
//     that is not AE_GITHUB_OWNER (the org's connected GitHub account,
//     case-insensitive), before any network call. An unset AE_GITHUB_OWNER
//     refuses every read.
//  2. The coordinates stay inside that owner's repo: repo must be a plain
//     repository name (ErrInvalidRepoName) and no path segment may be "." or
//     ".." (ErrPathDotSegment). url.PathEscape keeps dots, so `repo ".."` or a
//     path climbing out of contents/ could re-point the read at another
//     owner's repo past guard 1 wherever dot segments get resolved, and a repo
//     carrying a space could smuggle a qualifier past guard 3.
//  3. Search is scoped to that one repo: a query carrying its own scope
//     qualifier is refused (ErrQueryScopeQualifier).
//
// The token is the pod's gitpat (GITHUB_PAT). Reads are bounded: a fixed API
// base, a short timeout and a cap on decoded content.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrOwnerNotInOrg is returned when a requested repo owner is not the org's
// connected GitHub account. It fires before any GitHub request.
var ErrOwnerNotInOrg = errors.New("repo owner is not in org: only the organization's connected GitHub account may be read")

// ErrQueryScopeQualifier is returned when a search query embeds its own
// repo/org/user/fork qualifier. GitHub OR-combines scope qualifiers, so an
// embedded one would widen the search beyond the authorized repo (query
// `"secret repo:acme/other-private"` searches both). Refused before any
// network call, so the appended `repo:{owner}/{repo}` is the sole scope.
var ErrQueryScopeQualifier = errors.New("query must not contain a repo/org/user/fork scope qualifier")

// ErrInvalidRepoName is returned when repo is not a plain GitHub repository
// name (letters, digits, ".", "-", "_"; never "." or ".."). Refused before
// any network call.
var ErrInvalidRepoName = errors.New("invalid repository name")

// ErrPathDotSegment is returned when a path has a "." or ".." segment.
// Refused before any network call.
var ErrPathDotSegment = errors.New(`path must not contain "." or ".." segments`)

// repoNamePattern is the character set of a GitHub repository name.
var repoNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ErrFileTooLargeToInline is returned when the Contents API reports a file it
// cannot inline as base64 (encoding "none", GitHub's signal for a file over
// its ~1 MiB inline limit). There is no blob-API fallback, so such a file is
// refused rather than surfaced as empty content.
var ErrFileTooLargeToInline = errors.New("file too large to inline (GitHub reported encoding=none)")

// scopeQualifierPattern matches a code-search scope qualifier (repo:, org:,
// user:, fork:) anywhere in a query, case-insensitively.
var scopeQualifierPattern = regexp.MustCompile(`(?i)\b(repo|org|user|fork):`)

const (
	defaultGitHubAPIBase = "https://api.github.com"
	// remoteGitTimeout bounds one Contents or Search read.
	remoteGitTimeout = 15 * time.Second
	// defaultMaxContentBytes caps decoded file content (GitHub's own inline
	// Contents limit is 1 MiB).
	defaultMaxContentBytes = 1 << 20
	// maxSearchItems caps the code-search hits surfaced; searchPerPage is the
	// page size asked of GitHub.
	maxSearchItems = 30
	searchPerPage  = 30
	// maxToolFileBytes caps the file content one tool result carries: a tool
	// result is prompt input, and 128 KiB of text is far beyond any OpenAPI
	// document this tool exists to read.
	maxToolFileBytes = 128 << 10
	// maxErrorBodyChars bounds the GitHub error text a tool error quotes.
	maxErrorBodyChars = 512
)

// RemoteGit reads files and searches code in the org's own GitHub repos over
// the REST API. The zero APIBase and HTTP are GitHub and a client with a 15 s
// timeout. Owner and Token are never logged.
type RemoteGit struct {
	// Owner is AE_GITHUB_OWNER, the org's connected GitHub account; empty
	// refuses every read.
	Owner string
	// Token is the gitpat (GITHUB_PAT).
	Token string
	// APIBase overrides GitHub's REST API root (tests point it at a fake).
	APIBase string
	// HTTP is the client for GitHub; nil uses one with remoteGitTimeout.
	HTTP *http.Client

	// maxContentBytes overrides defaultMaxContentBytes (tests).
	maxContentBytes int64
}

// RemoteGitFile is one Contents API read: a file (Content + SHA) or a
// directory (IsDirectory, Entries).
type RemoteGitFile struct {
	Content     string
	SHA         string
	IsDirectory bool
	Entries     []RemoteGitEntry
}

// RemoteGitEntry is one child of a directory read.
type RemoteGitEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "file" | "dir"
	SHA  string `json:"sha"`
}

// RemoteGitSearchHit is one Code Search result.
type RemoteGitSearchHit struct {
	Path string `json:"path"`
	SHA  string `json:"sha"`
}

// authorize enforces the owner guard: owner must equal the org's account,
// case-insensitively (GitHub logins are), and neither may be empty.
func (g RemoteGit) authorize(owner string) error {
	if owner == "" || g.Owner == "" || !strings.EqualFold(owner, g.Owner) {
		return ErrOwnerNotInOrg
	}
	return nil
}

// checkRepo refuses a repo that is not a plain repository name.
func checkRepo(repo string) error {
	if !repoNamePattern.MatchString(repo) || repo == "." || repo == ".." {
		return fmt.Errorf("%w: %q", ErrInvalidRepoName, repo)
	}
	return nil
}

// checkPath refuses a path with a "." or ".." segment. An empty segment
// ("a//b") stays under contents/ and cannot re-root the URL.
func checkPath(p string) error {
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." {
			return fmt.Errorf("%w: %q", ErrPathDotSegment, p)
		}
	}
	return nil
}

// GetFileContents reads one path via the Contents API: a file answers its
// decoded content and sha, a directory its entries. Another owner is refused
// before any network call, as are coordinates that leave the owner's repo.
func (g RemoteGit) GetFileContents(ctx context.Context, owner, repo, path, ref string) (*RemoteGitFile, error) {
	if err := g.authorize(owner); err != nil {
		return nil, err
	}
	if err := checkRepo(repo); err != nil {
		return nil, err
	}
	if err := checkPath(path); err != nil {
		return nil, err
	}
	// Each segment is escaped, the slashes kept; an empty path is the root.
	u := fmt.Sprintf("%s/repos/%s/%s/contents/%s", g.apiBase(),
		url.PathEscape(owner), url.PathEscape(repo), escapePath(path))
	if ref != "" {
		u += "?ref=" + url.QueryEscape(ref)
	}
	body, err := g.get(ctx, u, "contents")
	if err != nil {
		return nil, err
	}
	// The Contents API answers an array for a directory, an object for a file.
	if strings.HasPrefix(strings.TrimLeft(string(body), " \t\r\n"), "[") {
		var dir []RemoteGitEntry
		if err := json.Unmarshal(body, &dir); err != nil {
			return nil, fmt.Errorf("decode directory listing: %w", err)
		}
		return &RemoteGitFile{IsDirectory: true, Entries: dir}, nil
	}
	var file struct {
		SHA      string `json:"sha"`
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(body, &file); err != nil {
		return nil, fmt.Errorf("decode file contents: %w", err)
	}
	content, err := decodeContent(file.Content, file.Encoding, g.maxContent())
	if err != nil {
		return nil, err
	}
	return &RemoteGitFile{Content: content, SHA: file.SHA}, nil
}

// SearchCode runs a code search scoped to owner/repo. Another owner, a repo
// that is not a plain name, or a query carrying its own scope qualifier is
// refused before any network call.
func (g RemoteGit) SearchCode(ctx context.Context, owner, repo, query string) ([]RemoteGitSearchHit, error) {
	if err := g.authorize(owner); err != nil {
		return nil, err
	}
	if err := checkRepo(repo); err != nil {
		return nil, err
	}
	if scopeQualifierPattern.MatchString(query) {
		return nil, ErrQueryScopeQualifier
	}
	q := strings.TrimSpace(query) + fmt.Sprintf(" repo:%s/%s", owner, repo)
	u := fmt.Sprintf("%s/search/code?q=%s&per_page=%d", g.apiBase(), url.QueryEscape(q), searchPerPage)
	body, err := g.get(ctx, u, "code search")
	if err != nil {
		return nil, err
	}
	var out struct {
		Items []RemoteGitSearchHit `json:"items"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode search results: %w", err)
	}
	if len(out.Items) > maxSearchItems {
		out.Items = out.Items[:maxSearchItems]
	}
	return out.Items, nil
}

// get performs an authenticated GET that must answer 200 and returns the
// bounded body.
func (g RemoteGit) get(ctx context.Context, u, label string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	hc := g.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: remoteGitTimeout}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github %s request: %w", label, err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Base64 inflates ~4/3, plus the JSON envelope: twice the content cap
	// keeps well-formed answers whole and refuses an unbounded body.
	body, err := io.ReadAll(io.LimitReader(resp.Body, g.maxContent()*2+(1<<16)))
	if err != nil {
		return nil, fmt.Errorf("read github %s response: %w", label, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github %s failed (status %d): %s", label, resp.StatusCode, truncate(string(body), maxErrorBodyChars))
	}
	return body, nil
}

func (g RemoteGit) apiBase() string {
	if g.APIBase == "" {
		return defaultGitHubAPIBase
	}
	return strings.TrimRight(g.APIBase, "/")
}

func (g RemoteGit) maxContent() int64 {
	if g.maxContentBytes > 0 {
		return g.maxContentBytes
	}
	return defaultMaxContentBytes
}

// remoteGitArgs are the remote-git tools' arguments.
type remoteGitArgs struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Path  string `json:"path"`
	Ref   string `json:"ref"`
	Query string `json:"query"`
}

// callTool runs one remote-git tool and answers its MCP tool result. Every
// failure (a missing argument, the owner guard, GitHub) is a tool error the
// model can read and correct, as aep-api answered it.
func (g RemoteGit) callTool(ctx context.Context, name string, a remoteGitArgs) json.RawMessage {
	switch name {
	case toolGetFileContents:
		if a.Repo == "" {
			return toolError("missing required arguments: owner and repo")
		}
		f, err := g.GetFileContents(ctx, a.Owner, a.Repo, a.Path, a.Ref)
		if err != nil {
			return toolError("get remote git file contents: " + err.Error())
		}
		return toolText(fileView(f))
	default: // toolSearchCode
		if a.Repo == "" || a.Query == "" {
			return toolError("missing required arguments: owner, repo and query")
		}
		hits, err := g.SearchCode(ctx, a.Owner, a.Repo, a.Query)
		if err != nil {
			return toolError("search remote git code: " + err.Error())
		}
		if hits == nil {
			hits = []RemoteGitSearchHit{}
		}
		return toolText(map[string]any{"items": hits})
	}
}

// remoteGitFileView is get_remote_git_file_contents' answer: a file's content
// and sha, or a directory's entries.
type remoteGitFileView struct {
	Content     string           `json:"content,omitempty"`
	SHA         string           `json:"sha,omitempty"`
	IsDirectory bool             `json:"isDirectory"`
	Entries     []RemoteGitEntry `json:"entries,omitempty"`
	// Note explains withheld or shortened content.
	Note string `json:"note,omitempty"`
}

// fileView guards what rides the prompt: binary content (invalid UTF-8 or a
// NUL) is withheld with a note, text over maxToolFileBytes is cut on a rune
// boundary with a note.
func fileView(f *RemoteGitFile) remoteGitFileView {
	v := remoteGitFileView{Content: f.Content, SHA: f.SHA, IsDirectory: f.IsDirectory, Entries: f.Entries}
	switch {
	case f.IsDirectory:
	case !utf8.ValidString(f.Content) || strings.ContainsRune(f.Content, 0):
		v.Content = ""
		v.Note = fmt.Sprintf("binary file (%d bytes) — content withheld; this tool reads text documents", len(f.Content))
	case len(f.Content) > maxToolFileBytes:
		cut := maxToolFileBytes
		for cut > 0 && !utf8.RuneStart(f.Content[cut]) {
			cut--
		}
		v.Content = f.Content[:cut]
		v.Note = fmt.Sprintf("truncated to the first %d of %d bytes", cut, len(f.Content))
	}
	return v
}

// decodeContent base64-decodes the Contents API `content`, enforcing the cap.
// Encoding "none" is GitHub's "too large to inline"; a real empty file comes
// back as base64 with empty content.
func decodeContent(content, encoding string, maxBytes int64) (string, error) {
	if encoding == "none" {
		return "", ErrFileTooLargeToInline
	}
	if content == "" {
		return "", nil
	}
	if encoding != "" && encoding != "base64" {
		return "", fmt.Errorf("unsupported content encoding %q", encoding)
	}
	// GitHub wraps the payload at 60 columns.
	decoded, err := base64.StdEncoding.DecodeString(strings.NewReplacer("\n", "", "\r", "").Replace(content))
	if err != nil {
		return "", fmt.Errorf("decode base64 content: %w", err)
	}
	if int64(len(decoded)) > maxBytes {
		return "", fmt.Errorf("file content %d bytes exceeds cap %d", len(decoded), maxBytes)
	}
	return string(decoded), nil
}

// escapePath path-escapes each segment of a repo-relative path, keeping the
// slashes.
func escapePath(p string) string {
	segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// truncate cuts s to at most n bytes on a rune boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}
