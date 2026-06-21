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

package gitrepo

// GitHub artifact-store HTTP methods driving the SaveDesign and
// SaveRequirements flows.
//
// They live on the same `githubClient` struct as the rest of the GitHub HTTP
// surface so the credentials boundary is preserved — no token-bearing value
// crosses BFF↔git-service.
//
// See docs/design/artifact-store-v2.md §8 (save flows) and §9 (concurrency).

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/wso2/asdlc/asdlc-service/internal/credentials"
)

// ----- Typed errors -----

var (
	// ErrRefNotFastForward is returned by UpdateRef when force=false and the
	// requested move would be non-fast-forward (ref tip moved between read
	// and write). Wraps a 422 from GitHub.
	ErrRefNotFastForward = errors.New("github ref: update is not a fast-forward")

	// (ErrTagAlreadyExists is declared in errors.go and reused here; we wrap
	// it on CreateTagRef 422 so save flows can branch on the tag-collision
	// case distinctly from generic 422s.)
)

// ----- Request / response types -----

// GitIdentity mirrors GitHub's author/committer/tagger object. Date is
// optional; GitHub defaults to the request time when omitted. Named with the
// `Git` prefix to avoid collision with the `Identity` type already declared
// in credential_service.go.
type GitIdentity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Date  string `json:"date,omitempty"`
}

// CommitObject is the subset of GET /git/commits we consume.
type CommitObject struct {
	SHA     string
	TreeSHA string
	Parents []string
	Message string
}

// TreeEntry is the wire shape for POST /git/trees entries. SHA is empty for
// deletions (we serialise as `sha: null` on the wire).
type TreeEntry struct {
	Path string
	Mode string // "100644" file, "100755" exec, "040000" dir, "160000" submodule, "120000" symlink
	Type string // "blob" | "tree" | "commit"
	SHA  string // empty → deletion (sha:null)
}

// TreeObject is the subset of GET /git/trees we consume.
type TreeObject struct {
	SHA     string
	Entries []TreeEntryResult
}

// TreeEntryResult is one row of a tree listing. Size is only set for blobs.
type TreeEntryResult struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int64  `json:"size,omitempty"`
}

// CreateCommitRequest is the body of POST /git/commits.
type CreateCommitRequest struct {
	Message   string
	TreeSHA   string
	Parents   []string
	Author    *GitIdentity
	Committer *GitIdentity
}

// CreateTagObjectRequest is the body of POST /git/tags.
type CreateTagObjectRequest struct {
	Tag     string
	Message string
	Object  string // commit SHA
	Type    string // typically "commit"
	Tagger  *GitIdentity
}

// MatchingRef is one row of GET /git/matching-refs/<prefix>.
type MatchingRef struct {
	Ref string `json:"ref"` // e.g. "refs/tags/v1"
	SHA string // pulled out of the nested .object.sha
}

// ----- Implementations -----

func (c *githubClient) GetRef(ctx context.Context, owner, repo string, cred credentials.Credential, ref string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/ref/%s", owner, repo, ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create get-ref request: %w", err)
	}
	if err := authHeaders(ctx, req, cred); err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github get-ref: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return "", &HTTPStatusError{StatusCode: resp.StatusCode, Body: string(body), URL: url}
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github get-ref (status %d): %s", resp.StatusCode, string(body))
	}
	var parsed struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode get-ref: %w", err)
	}
	return parsed.Object.SHA, nil
}

func (c *githubClient) GetCommit(ctx context.Context, owner, repo string, cred credentials.Credential, sha string) (*CommitObject, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/commits/%s", owner, repo, sha)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create get-commit request: %w", err)
	}
	if err := authHeaders(ctx, req, cred); err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github get-commit: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github get-commit (status %d): %s", resp.StatusCode, string(body))
	}
	var parsed struct {
		SHA  string `json:"sha"`
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
		Parents []struct {
			SHA string `json:"sha"`
		} `json:"parents"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode get-commit: %w", err)
	}
	parents := make([]string, 0, len(parsed.Parents))
	for _, p := range parsed.Parents {
		parents = append(parents, p.SHA)
	}
	return &CommitObject{
		SHA:     parsed.SHA,
		TreeSHA: parsed.Tree.SHA,
		Parents: parents,
		Message: parsed.Message,
	}, nil
}

func (c *githubClient) GetTree(ctx context.Context, owner, repo string, cred credentials.Credential, treeSHA string, recursive bool) (*TreeObject, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s", owner, repo, treeSHA)
	if recursive {
		url += "?recursive=true"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create get-tree request: %w", err)
	}
	if err := authHeaders(ctx, req, cred); err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github get-tree: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github get-tree (status %d): %s", resp.StatusCode, string(body))
	}
	var parsed struct {
		SHA       string            `json:"sha"`
		Tree      []TreeEntryResult `json:"tree"`
		Truncated bool              `json:"truncated"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode get-tree: %w", err)
	}
	if parsed.Truncated {
		// specs/ is tiny; truncation means a misuse (saving requirements across
		// a 100k-entry tree). Fail loud so we don't silently miss entries.
		return nil, fmt.Errorf("github get-tree: response truncated (tree too large)")
	}
	return &TreeObject{SHA: parsed.SHA, Entries: parsed.Tree}, nil
}

func (c *githubClient) GetBlob(ctx context.Context, owner, repo string, cred credentials.Credential, sha string) ([]byte, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/blobs/%s", owner, repo, sha)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create get-blob request: %w", err)
	}
	if err := authHeaders(ctx, req, cred); err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github get-blob: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Body: string(body), URL: url}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github get-blob (status %d): %s", resp.StatusCode, string(body))
	}
	var parsed struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode get-blob: %w", err)
	}
	switch parsed.Encoding {
	case "base64":
		// The Git Data blobs API wraps base64 content at 76 cols with "\n";
		// strip whitespace before decoding so StdEncoding doesn't choke.
		clean := strings.NewReplacer("\n", "", "\r", "").Replace(parsed.Content)
		decoded, derr := base64.StdEncoding.DecodeString(clean)
		if derr != nil {
			return nil, fmt.Errorf("decode blob base64: %w", derr)
		}
		return decoded, nil
	case "utf-8", "":
		return []byte(parsed.Content), nil
	default:
		return nil, fmt.Errorf("github get-blob: unsupported encoding %q", parsed.Encoding)
	}
}

func (c *githubClient) CreateBlob(ctx context.Context, owner, repo string, cred credentials.Credential, content []byte) (string, error) {
	payload := map[string]any{
		"content":  base64.StdEncoding.EncodeToString(content),
		"encoding": "base64",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal create-blob: %w", err)
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/blobs", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create create-blob request: %w", err)
	}
	if err := authHeaders(ctx, req, cred); err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github create-blob: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("github create-blob (status %d): %s", resp.StatusCode, string(respBody))
	}
	var parsed struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("decode create-blob: %w", err)
	}
	return parsed.SHA, nil
}

func (c *githubClient) CreateTree(ctx context.Context, owner, repo string, cred credentials.Credential, baseTree string, entries []TreeEntry) (string, error) {
	// Wire entries: deletions go as `sha: null` (not omitted) so GitHub
	// removes the path from the resulting tree. Encode them with a custom
	// shape to make `sha: null` explicit.
	type wireEntry struct {
		Path string  `json:"path"`
		Mode string  `json:"mode"`
		Type string  `json:"type"`
		SHA  *string `json:"sha"` // pointer so a nil shows as `null`
	}
	wire := make([]wireEntry, 0, len(entries))
	for _, e := range entries {
		w := wireEntry{Path: e.Path, Mode: e.Mode, Type: e.Type}
		if e.SHA != "" {
			s := e.SHA
			w.SHA = &s
		}
		wire = append(wire, w)
	}
	payload := map[string]any{
		"tree": wire,
	}
	if baseTree != "" {
		payload["base_tree"] = baseTree
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal create-tree: %w", err)
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create create-tree request: %w", err)
	}
	if err := authHeaders(ctx, req, cred); err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github create-tree: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("github create-tree (status %d): %s", resp.StatusCode, string(respBody))
	}
	var parsed struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("decode create-tree: %w", err)
	}
	return parsed.SHA, nil
}

func (c *githubClient) CreateCommit(ctx context.Context, owner, repo string, cred credentials.Credential, req CreateCommitRequest) (string, error) {
	payload := map[string]any{
		"message": req.Message,
		"tree":    req.TreeSHA,
		"parents": req.Parents,
	}
	if req.Author != nil {
		payload["author"] = req.Author
	}
	if req.Committer != nil {
		payload["committer"] = req.Committer
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal create-commit: %w", err)
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/commits", owner, repo)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create create-commit request: %w", err)
	}
	if err := authHeaders(ctx, httpReq, cred); err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("github create-commit: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("github create-commit (status %d): %s", resp.StatusCode, string(respBody))
	}
	var parsed struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("decode create-commit: %w", err)
	}
	return parsed.SHA, nil
}

func (c *githubClient) UpdateRef(ctx context.Context, owner, repo string, cred credentials.Credential, ref, sha string, force bool) error {
	payload := map[string]any{
		"sha":   sha,
		"force": force,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal update-ref: %w", err)
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/refs/%s", owner, repo, ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create update-ref request: %w", err)
	}
	if err := authHeaders(ctx, req, cred); err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("github update-ref: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	// Non-fast-forward returns 422 with a descriptive message. Treat any 422
	// on this endpoint as a CAS failure so the retry loop can re-anchor.
	if resp.StatusCode == http.StatusUnprocessableEntity {
		return fmt.Errorf("%w: %s", ErrRefNotFastForward, string(respBody))
	}
	return fmt.Errorf("github update-ref (status %d): %s", resp.StatusCode, string(respBody))
}

func (c *githubClient) CreateTagObject(ctx context.Context, owner, repo string, cred credentials.Credential, req CreateTagObjectRequest) (string, error) {
	objType := req.Type
	if objType == "" {
		objType = "commit"
	}
	payload := map[string]any{
		"tag":     req.Tag,
		"message": req.Message,
		"object":  req.Object,
		"type":    objType,
	}
	if req.Tagger != nil {
		payload["tagger"] = req.Tagger
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal create-tag-object: %w", err)
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/tags", owner, repo)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create create-tag-object request: %w", err)
	}
	if err := authHeaders(ctx, httpReq, cred); err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("github create-tag-object: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("github create-tag-object (status %d): %s", resp.StatusCode, string(respBody))
	}
	var parsed struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("decode create-tag-object: %w", err)
	}
	return parsed.SHA, nil
}

func (c *githubClient) CreateTagRef(ctx context.Context, owner, repo string, cred credentials.Credential, tagName, tagObjectSHA string) error {
	payload := map[string]any{
		"ref": "refs/tags/" + tagName,
		"sha": tagObjectSHA,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal create-tag-ref: %w", err)
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/refs", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create create-tag-ref request: %w", err)
	}
	if err := authHeaders(ctx, req, cred); err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("github create-tag-ref: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusCreated {
		return nil
	}
	// 422 with "Reference already exists" is the tag-collision case.
	if resp.StatusCode == http.StatusUnprocessableEntity {
		return fmt.Errorf("%w: %s", ErrTagAlreadyExists, string(respBody))
	}
	return fmt.Errorf("github create-tag-ref (status %d): %s", resp.StatusCode, string(respBody))
}

func (c *githubClient) ListMatchingRefs(ctx context.Context, owner, repo string, cred credentials.Credential, prefix string) ([]MatchingRef, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/matching-refs/%s", owner, repo, prefix)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create list-matching-refs request: %w", err)
	}
	if err := authHeaders(ctx, req, cred); err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github list-matching-refs: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		// Some App installations return 404 instead of 200 + empty list when
		// no refs match. Normalise to "empty list" — same caller intent.
		return []MatchingRef{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github list-matching-refs (status %d): %s", resp.StatusCode, string(body))
	}
	var parsed []struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode list-matching-refs: %w", err)
	}
	out := make([]MatchingRef, 0, len(parsed))
	for _, r := range parsed {
		out = append(out, MatchingRef{Ref: r.Ref, SHA: r.Object.SHA})
	}
	return out, nil
}
