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

// Package github is the GitHub implementation of gitrepo's provider ports.
//
// A single *Client satisfies sourcecontrol.Host — the REST surface (repo / issue /
// milestone / pull-request / webhook / app-installation) in client.go and
// milestones.go. The git-object (Git-Data) surface is gone: all repo content
// reads/writes run on the disk-backed Workspace engine (internal/platform/gitfs).
// GitHub Projects v2 is dropped (tasks-github-native §4): Tasks are plain
// GitHub issues, so there is no board surface. GraphQL (graphql.go) survives
// only for the milestone dispatch predicate, which REST cannot answer in one
// call — REST vs GraphQL never leaks past the port either way.
//
// This is the only place in the codebase that builds Authorization: Bearer
// headers (authHeaders / the App-JWT and pat paths). Selected by GIT_PROVIDER
// in the composition root; a GitLab/Gitea host would be a sibling clients/*
// package satisfying the same ports.
package githubhost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	urlpkg "net/url"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// Client is the GitHub host client — one http.Client, one auth path, serving
// sourcecontrol.Host as a single implementation. Stateless; concurrent calls are safe.
type Client struct {
	httpClient *http.Client
	// apiBase is the GitHub REST API root, default "https://api.github.com".
	// Overridable only via WithAPIBase (a test seam).
	apiBase string
	// graphqlEndpoint is the GitHub GraphQL (v4) endpoint, default
	// "https://api.github.com/graphql" — a separate host path, not derivable
	// from apiBase. Overridable only via WithGraphQLEndpoint (a test seam).
	// See graphql.go.
	graphqlEndpoint string
}

// Option configures the client at construction. Production wiring passes no
// options; the base-URL options are test seams for the gittest tier.
type Option func(*Client)

// WithAPIBase overrides the GitHub REST API base URL. This is a TEST SEAM — it
// lets the gittest tier point the real client at an httptest fake (see
// internal/platform/gittest). Not wired in production.
//
//deadcode:keep test seam — points the real client at an httptest server; used by internal/sourcecontrol/*_test.go (cross-package).
func WithAPIBase(base string) Option {
	return func(c *Client) { c.apiBase = strings.TrimRight(base, "/") }
}

// NewClient builds the GitHub host client. Production wiring passes no options.
func NewClient(opts ...Option) sourcecontrol.Host {
	c := &Client{
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		apiBase:         "https://api.github.com",
		graphqlEndpoint: defaultGraphQLEndpoint,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Compile-time proof the concrete client satisfies the whole provider surface.
var _ sourcecontrol.Host = (*Client)(nil)

// authHeaders sets the standard GitHub API headers and the Authorization
// header. Token is fetched fresh on every call from the credential —
// long-lived PATs are a no-op here, short-lived App tokens refresh on
// demand through the same path.
func authHeaders(ctx context.Context, req *http.Request, cred secrets.Credential) error {
	token, _, err := cred.Token(ctx)
	if err != nil {
		return fmt.Errorf("resolve token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return nil
}

// doJSON owns the request loop shared by the JSON verb methods: marshal the
// payload (nil ⇒ no body), build the request, attach auth headers, execute,
// and enforce okStatuses. A disallowed status returns
// "github <label> failed (status %d): <body>"; when out is non-nil the OK
// response body is unmarshalled into it.
func (c *Client) doJSON(ctx context.Context, method, url, label string, cred secrets.Credential, payload, out any, okStatuses ...int) error {
	var reqBody io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if err := authHeaders(ctx, httpReq, cred); err != nil {
		return err
	}
	if payload != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("github API request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	ok := false
	for _, s := range okStatuses {
		if resp.StatusCode == s {
			ok = true
			break
		}
	}
	if !ok {
		return fmt.Errorf("github %s failed (status %d): %s", label, resp.StatusCode, string(respBody))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// getJSON performs an authenticated GET, requires 200, and decodes the body
// into out. Any other status returns *sourcecontrol.HTTPStatusError so callers can
// branch on the code (404 vs 5xx).
func (c *Client) getJSON(ctx context.Context, url string, cred secrets.Credential, out any) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if err := authHeaders(ctx, httpReq, cred); err != nil {
		return err
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("github API request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return &sourcecontrol.HTTPStatusError{StatusCode: resp.StatusCode, Body: string(respBody), URL: url}
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) CreateOrgRepo(ctx context.Context, cred secrets.Credential, req sourcecontrol.CreateOrgRepoRequest) (string, error) {
	owner := cred.RepoOwner()
	if owner == "" {
		return "", fmt.Errorf("credential has no repo owner")
	}

	payload := map[string]any{
		"name":        req.Name,
		"private":     req.Private,
		"auto_init":   req.AutoInit,
		"description": req.Description,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf(c.apiBase+"/orgs/%s/repos", owner)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	if err := authHeaders(ctx, httpReq, cred); err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// User accounts (not orgs) require the /user/repos endpoint instead. Detect
	// 404 on /orgs/.../repos and retry once against /user/repos. This keeps the
	// PAT-mode "owner is a user, not an org" path working without a separate
	// configuration knob.
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("github API request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		// Owner is likely a user account, not an org — try /user/repos.
		return c.createUserRepo(ctx, cred, payload)
	}

	if resp.StatusCode == http.StatusCreated {
		var created struct {
			CloneURL string `json:"clone_url"`
		}
		if err := json.Unmarshal(respBody, &created); err != nil {
			return "", fmt.Errorf("decode response: %w", err)
		}
		if created.CloneURL == "" {
			return "", fmt.Errorf("github response missing clone_url: %s", string(respBody))
		}
		return created.CloneURL, nil
	}

	if resp.StatusCode == http.StatusUnprocessableEntity &&
		strings.Contains(string(respBody), "name already exists") {
		return "", sourcecontrol.ErrRepoNameConflict
	}

	return "", fmt.Errorf("github repo create failed (status %d): %s", resp.StatusCode, string(respBody))
}

func (c *Client) createUserRepo(ctx context.Context, cred secrets.Credential, payload map[string]any) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase+"/user/repos", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	if err := authHeaders(ctx, httpReq, cred); err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("github API request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusCreated {
		var created struct {
			CloneURL string `json:"clone_url"`
		}
		if err := json.Unmarshal(respBody, &created); err != nil {
			return "", fmt.Errorf("decode response: %w", err)
		}
		return created.CloneURL, nil
	}
	if resp.StatusCode == http.StatusUnprocessableEntity &&
		strings.Contains(string(respBody), "name already exists") {
		return "", sourcecontrol.ErrRepoNameConflict
	}
	return "", fmt.Errorf("github user repo create failed (status %d): %s", resp.StatusCode, string(respBody))
}

func (c *Client) CreateIssue(ctx context.Context, owner, repo string, cred secrets.Credential, req sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues", owner, repo)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if err := authHeaders(ctx, httpReq, cred); err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("github API request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusCreated {
		var created struct {
			Number  int    `json:"number"`
			HTMLURL string `json:"html_url"`
			NodeID  string `json:"node_id"`
		}
		if err := json.Unmarshal(respBody, &created); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		if created.HTMLURL == "" {
			return nil, fmt.Errorf("github response missing html_url: %s", string(respBody))
		}
		return &sourcecontrol.IssueResult{Number: created.Number, URL: created.HTMLURL, NodeID: created.NodeID}, nil
	}

	return nil, fmt.Errorf("github issue create failed (status %d): %s", resp.StatusCode, string(respBody))
}

func (c *Client) EnsureLabel(ctx context.Context, owner, repo string, cred secrets.Credential, name, color string) error {
	payload := map[string]string{"name": name, "color": color}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/labels", owner, repo)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if err := authHeaders(ctx, httpReq, cred); err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("github API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusUnprocessableEntity {
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("github label ensure failed (status %d): %s", resp.StatusCode, string(respBody))
}

func (c *Client) CloseIssue(ctx context.Context, owner, repo string, cred secrets.Credential, number int) error {
	payload := map[string]string{"state": "closed", "state_reason": "completed"}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d", owner, repo, number)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if err := authHeaders(ctx, httpReq, cred); err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("github API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("github issue close failed (status %d): %s", resp.StatusCode, string(respBody))
}

func (c *Client) EditIssueBody(ctx context.Context, owner, repo string, cred secrets.Credential, number int, body string) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d", owner, repo, number)
	return c.doJSON(ctx, http.MethodPatch, url, "issue edit", cred, map[string]string{"body": body}, nil, http.StatusOK)
}

// EditIssueTitle replaces the issue title via PATCH /issues/{number}. Used by
// the plan tap's updateTask handler when a planned Task is renamed.
func (c *Client) EditIssueTitle(ctx context.Context, owner, repo string, cred secrets.Credential, number int, title string) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d", owner, repo, number)
	return c.doJSON(ctx, http.MethodPatch, url, "issue title edit", cred, map[string]string{"title": title}, nil, http.StatusOK)
}

// ReopenIssue sets the issue state back to open via PATCH /issues/{number}. The
// inverse of CloseIssue, and it deliberately clears no state_reason: GitHub drops
// the closed reason itself on reopen, and naming one here would be inventing a
// reason for an issue that has none.
//
// Reopening an already-open issue is a 200 no-op on GitHub's side, so callers need
// no read-before-write.
func (c *Client) ReopenIssue(ctx context.Context, owner, repo string, cred secrets.Credential, number int) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d", owner, repo, number)
	return c.doJSON(ctx, http.MethodPatch, url, "issue reopen", cred, map[string]string{"state": "open"}, nil, http.StatusOK)
}

// SetIssueMilestone assigns an existing issue to a milestone via
// PATCH /issues/{number}. The value is the milestone NUMBER — GitHub answers
// 422 to a title here, and the number is the only stable key anyway. Used by
// adoption, which moves a bare issue into the deployed version's milestone.
func (c *Client) SetIssueMilestone(ctx context.Context, owner, repo string, cred secrets.Credential, number, milestoneNumber int) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d", owner, repo, number)
	return c.doJSON(ctx, http.MethodPatch, url, "issue milestone set", cred, map[string]int{"milestone": milestoneNumber}, nil, http.StatusOK)
}

// GetPullRequest returns the live state of a pull request (GET /pulls/{n}) — the
// sweep's PR-state reconciliation input (docs/design/tasks-github-native.md §5:
// PR state is native GitHub truth healed by the sweep when a webhook is missed).
func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, cred secrets.Credential, number int) (*sourcecontrol.PullRequestState, error) {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/pulls/%d", owner, repo, number)
	var raw struct {
		State          string `json:"state"` // "open" | "closed"
		Merged         bool   `json:"merged"`
		MergeCommitSHA string `json:"merge_commit_sha"`
	}
	if err := c.getJSON(ctx, url, cred, &raw); err != nil {
		return nil, err
	}
	return &sourcecontrol.PullRequestState{State: raw.State, Merged: raw.Merged, MergeCommitSHA: raw.MergeCommitSHA}, nil
}

// MergePullRequest squash-merges a pull request (PUT /pulls/{n}/merge) — the
// devflow task workflow's auto-merge adapter. GitHub answers 405 when the PR
// is not mergeable (checks pending, conflicts, already merged). To keep the
// call idempotent under activity retries (a lost success response would make a
// retry hit an already-merged PR and fail), a merge error is reconciled
// against the live PR state: if the PR is already merged, the side effect
// landed and we report success.
func (c *Client) MergePullRequest(ctx context.Context, owner, repo string, cred secrets.Credential, number int) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/pulls/%d/merge", owner, repo, number)
	err := c.doJSON(ctx, http.MethodPut, url, "pull request merge", cred,
		map[string]string{"merge_method": "squash"}, nil, http.StatusOK)
	if err == nil {
		return nil
	}
	if state, gerr := c.GetPullRequest(ctx, owner, repo, cred, number); gerr == nil && state.Merged {
		return nil
	}
	return err
}

// ListPullRequestFiles returns the paths of every file changed by a pull request
// (GET /pulls/{n}/files, paginated 100/page). The path-based build trigger maps
// these paths onto the components whose source directory they touched, so a
// merged PR rebuilds every affected component — not just the one the Task was
// bound to (docs: tasks-github-native.md; the merge webhook carries no file list).
func (c *Client) ListPullRequestFiles(ctx context.Context, owner, repo string, cred secrets.Credential, number int) ([]string, error) {
	var files []string
	for page := 1; ; page++ {
		url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/pulls/%d/files?per_page=100&page=%d", owner, repo, number, page)
		var raw []struct {
			Filename string `json:"filename"`
		}
		if err := c.getJSON(ctx, url, cred, &raw); err != nil {
			return nil, err
		}
		for _, f := range raw {
			files = append(files, f.Filename)
		}
		if len(raw) < 100 {
			break // last (short) page
		}
	}
	return files, nil
}

func (c *Client) CommentIssue(ctx context.Context, owner, repo string, cred secrets.Credential, number int, body string) error {
	payload := map[string]string{"body": body}
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d/comments", owner, repo, number)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if err := authHeaders(ctx, httpReq, cred); err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("github API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("github issue comment failed (status %d): %s", resp.StatusCode, string(respBody))
}

// issueListMaxPages bounds ListIssues' page walk. The console bell polls the
// unfiltered list for every project with an alert, so each page is a request
// against the installation's rate budget on every poll; 10 pages keep a
// 1000-issue repository complete and cap the cost of a larger one.
const issueListMaxPages = 10

// ListIssues returns the repository's issues in every state, filtered by label
// (AND semantics), following pagination until a short page or
// issueListMaxPages. GitHub answers newest first, so a repository past the cap
// loses its OLDEST issues from the list (logged); GetIssue still reaches them.
// Pull requests are dropped: the issues endpoint returns them alongside issues,
// each carrying a pull_request member.
func (c *Client) ListIssues(ctx context.Context, owner, repo string, cred secrets.Credential, labels []string) ([]sourcecontrol.IssueInfo, error) {
	base := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues?state=all&per_page=%d", owner, repo, milestonePageSize)
	if len(labels) > 0 {
		base += "&labels=" + strings.Join(labels, ",")
	}

	var issues []sourcecontrol.IssueInfo
	for page := 1; page <= issueListMaxPages; page++ {
		var raw []struct {
			Number      int    `json:"number"`
			Title       string `json:"title"`
			Body        string `json:"body"`
			HTMLURL     string `json:"html_url"`
			State       string `json:"state"`
			StateReason string `json:"state_reason"`
			ClosedAt    string `json:"closed_at"`
			Labels      []struct {
				Name string `json:"name"`
			} `json:"labels"`
			// PullRequest is present only on pull requests.
			PullRequest *struct{} `json:"pull_request"`
		}
		if err := c.getJSON(ctx, fmt.Sprintf("%s&page=%d", base, page), cred, &raw); err != nil {
			return nil, err
		}
		for _, r := range raw {
			if r.PullRequest != nil {
				continue
			}
			labelNames := make([]string, 0, len(r.Labels))
			for _, l := range r.Labels {
				labelNames = append(labelNames, l.Name)
			}
			issues = append(issues, sourcecontrol.IssueInfo{
				Number:      r.Number,
				Title:       r.Title,
				Body:        r.Body,
				URL:         r.HTMLURL,
				State:       r.State,
				StateReason: r.StateReason,
				ClosedAt:    r.ClosedAt,
				Labels:      labelNames,
			})
		}
		// Page length counts PRs too, so it — not len(issues) — decides the walk.
		if len(raw) < milestonePageSize {
			return issues, nil
		}
	}
	slog.WarnContext(ctx, "githubhost: issue list reached its page cap — older issues are not listed",
		"owner", owner, "repo", repo, "pages", issueListMaxPages)
	return issues, nil
}

// GetIssue fetches a single issue by number via GET
// /repos/{owner}/{repo}/issues/{number} — O(1) in repo size, unlike ListIssues
// (which pages the repo up to issueListMaxPages). A 404 is mapped to
// sourcecontrol.ErrIssueNotFound so callers can distinguish a missing issue from a
// transport failure.
func (c *Client) GetIssue(ctx context.Context, owner, repo string, cred secrets.Credential, number int) (*sourcecontrol.IssueInfo, error) {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d", owner, repo, number)
	var raw struct {
		Number      int    `json:"number"`
		Title       string `json:"title"`
		Body        string `json:"body"`
		HTMLURL     string `json:"html_url"`
		State       string `json:"state"`
		StateReason string `json:"state_reason"`
		ClosedAt    string `json:"closed_at"`
		Labels      []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := c.getJSON(ctx, url, cred, &raw); err != nil {
		if sourcecontrol.IsHTTPStatus(err, http.StatusNotFound) {
			return nil, sourcecontrol.ErrIssueNotFound
		}
		return nil, err
	}
	labelNames := make([]string, 0, len(raw.Labels))
	for _, l := range raw.Labels {
		labelNames = append(labelNames, l.Name)
	}
	return &sourcecontrol.IssueInfo{
		Number:      raw.Number,
		Title:       raw.Title,
		Body:        raw.Body,
		URL:         raw.HTMLURL,
		State:       raw.State,
		StateReason: raw.StateReason,
		ClosedAt:    raw.ClosedAt,
		Labels:      labelNames,
	}, nil
}

// AddIssueLabels adds labels to an existing issue via POST
// /repos/{owner}/{repo}/issues/{number}/labels. GitHub merges them with the
// issue's current labels (adding a label already present is a no-op). Used to
// stamp aep:status/* projection and aep:attention flags. 200 is success.
func (c *Client) AddIssueLabels(ctx context.Context, owner, repo string, cred secrets.Credential, number int, labels []string) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d/labels", owner, repo, number)
	return c.doJSON(ctx, http.MethodPost, url, "add issue labels", cred, map[string][]string{"labels": labels}, nil, http.StatusOK)
}

// RemoveIssueLabel removes one label from an issue via DELETE
// /repos/{owner}/{repo}/issues/{number}/labels/{name}. The label name is
// path-escaped (an aep: label contains ':' and may contain '/'). A 404 is
// treated as success — the label is already absent, which is the desired
// post-state (idempotent).
func (c *Client) RemoveIssueLabel(ctx context.Context, owner, repo string, cred secrets.Credential, number int, label string) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d/labels/%s", owner, repo, number, urlpkg.PathEscape(label))
	// 404 is success: the label is already absent, the desired post-state.
	return c.doJSON(ctx, http.MethodDelete, url, "remove issue label", cred, nil, nil, http.StatusOK, http.StatusNotFound)
}

// SetIssueLabels replaces the issue's entire label set via PUT
// /repos/{owner}/{repo}/issues/{number}/labels. Unlike AddIssueLabels this is
// authoritative — labels absent from the slice are removed. 200 is success.
func (c *Client) SetIssueLabels(ctx context.Context, owner, repo string, cred secrets.Credential, number int, labels []string) error {
	// Never send a nil slice — GitHub then leaves labels unchanged rather than
	// clearing them; an explicit empty array clears.
	if labels == nil {
		labels = []string{}
	}
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d/labels", owner, repo, number)
	return c.doJSON(ctx, http.MethodPut, url, "set issue labels", cred, map[string][]string{"labels": labels}, nil, http.StatusOK)
}

func (c *Client) RegisterWebhook(ctx context.Context, owner, repo string, cred secrets.Credential, deliveryURL, hmacSecret string, events []string) (int64, error) {
	payload := map[string]any{
		"name":   "web",
		"active": true,
		"events": events,
		"config": map[string]string{
			"url":          deliveryURL,
			"content_type": "json",
			"secret":       hmacSecret,
			"insecure_ssl": "0",
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal request: %w", err)
	}
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/hooks", owner, repo)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	if err := authHeaders(ctx, httpReq, cred); err != nil {
		return 0, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("github API request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusCreated {
		var hook struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(respBody, &hook); err != nil {
			return 0, fmt.Errorf("decode response: %w", err)
		}
		return hook.ID, nil
	}
	// Idempotency: GitHub returns 422 "Hook already exists" when the same URL is
	// registered twice. Look up the existing hook and return its ID.
	if resp.StatusCode == http.StatusUnprocessableEntity &&
		strings.Contains(string(respBody), "Hook already exists") {
		return c.findHookByURL(ctx, owner, repo, cred, deliveryURL)
	}
	return 0, fmt.Errorf("github register webhook failed (status %d): %s", resp.StatusCode, string(respBody))
}

func (c *Client) findHookByURL(ctx context.Context, owner, repo string, cred secrets.Credential, deliveryURL string) (int64, error) {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/hooks?per_page=100", owner, repo)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	if err := authHeaders(ctx, httpReq, cred); err != nil {
		return 0, err
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("github API request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("github list hooks failed (status %d): %s", resp.StatusCode, string(respBody))
	}
	var hooks []struct {
		ID     int64             `json:"id"`
		Config map[string]string `json:"config"`
	}
	if err := json.Unmarshal(respBody, &hooks); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}
	for _, h := range hooks {
		if h.Config["url"] == deliveryURL {
			return h.ID, nil
		}
	}
	return 0, fmt.Errorf("hook for url %s not found", deliveryURL)
}

// UpdateWebhookEvents replaces the subscribed event list of an existing repo
// webhook via PATCH /repos/{owner}/{repo}/hooks/{id}. Needed for the §9.2
// cutover: RegisterWebhook's already-exists path returns a pre-existing hook
// without touching its events, so a hook created before "issues" joined the
// subscription must be PATCHed to add it. 200 is success.
func (c *Client) UpdateWebhookEvents(ctx context.Context, owner, repo string, cred secrets.Credential, hookID int64, events []string) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/hooks/%d", owner, repo, hookID)
	return c.doJSON(ctx, http.MethodPatch, url, "update webhook events", cred, map[string]any{"events": events}, nil, http.StatusOK)
}

// DeleteWebhook removes one repo webhook via DELETE
// /repos/{owner}/{repo}/hooks/{id} — the teardown twin of RegisterWebhook.
//
// It is addressed by the hook ID the platform PERSISTED at registration, never
// by scanning the repo's hooks, so it can only ever remove the delivery this
// platform installed. A repository commonly carries hooks belonging to other
// integrations, and a project delete has no business touching them.
//
// 404 and 410 are success, exactly like RemoveIssueLabel's 404: the hook is
// already absent (deleted by hand, or by a previous run of this teardown), which
// is the desired post-state. 410 is what GitHub answers once a hook has been
// auto-disabled and reaped after repeated delivery failures — precisely the
// state an orphaned webhook ends up in.
func (c *Client) DeleteWebhook(ctx context.Context, owner, repo string, cred secrets.Credential, hookID int64) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/hooks/%d", owner, repo, hookID)
	return c.doJSON(ctx, http.MethodDelete, url, "delete webhook", cred, nil, nil,
		http.StatusNoContent, http.StatusNotFound, http.StatusGone)
}

// GetUser performs GET /user using the credential's token. Returns
// HTTPStatusError for non-2xx responses so the validator can branch on
// 401 (revoked) vs 5xx (transient).
func (c *Client) GetUser(ctx context.Context, cred secrets.Credential) (*sourcecontrol.GitHubUser, error) {
	url := c.apiBase + "/user"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if err := authHeaders(ctx, httpReq, cred); err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("github API request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, &sourcecontrol.HTTPStatusError{StatusCode: resp.StatusCode, Body: string(respBody), URL: url}
	}
	var user sourcecontrol.GitHubUser
	if err := json.Unmarshal(respBody, &user); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &user, nil
}

// GetAppInstallation calls GET /app/installations/{id}. Authenticated by
// the App JWT (not an installation token) — App-level endpoints reject
// installation tokens. The minter exposes the JWT through SignAppJWT().
func (c *Client) GetAppInstallation(ctx context.Context, minter *secrets.AppTokenMinter, installationID int64) (*sourcecontrol.AppInstallationInfo, error) {
	if minter == nil {
		return nil, fmt.Errorf("app minter required")
	}
	jwt, err := minter.SignAppJWT(time.Now())
	if err != nil {
		return nil, fmt.Errorf("sign app JWT: %w", err)
	}
	url := fmt.Sprintf(c.apiBase+"/app/installations/%d", installationID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+jwt)
	httpReq.Header.Set("Accept", "application/vnd.github+json")
	httpReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("github API request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, &sourcecontrol.HTTPStatusError{StatusCode: resp.StatusCode, Body: string(respBody), URL: url}
	}
	var info sourcecontrol.AppInstallationInfo
	if err := json.Unmarshal(respBody, &info); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &info, nil
}

func (c *Client) DeleteInstallation(ctx context.Context, minter *secrets.AppTokenMinter, installationID int64) error {
	if minter == nil {
		return fmt.Errorf("app minter required")
	}
	jwt, err := minter.SignAppJWT(time.Now())
	if err != nil {
		return fmt.Errorf("sign app JWT: %w", err)
	}
	url := fmt.Sprintf(c.apiBase+"/app/installations/%d", installationID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+jwt)
	httpReq.Header.Set("Accept", "application/vnd.github+json")
	httpReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("github API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	respBody, _ := io.ReadAll(resp.Body)
	return &sourcecontrol.HTTPStatusError{StatusCode: resp.StatusCode, Body: string(respBody), URL: url}
}

// ListAppInstallations pages through GET /app/installations using the
// App JWT. Returns the flat AppInstallationSummary projection. Caps
// the walk at 10 pages × 100 = 1000 installations as a defensive bound;
// real-world dev/single-tenant installs are an order of magnitude under.
func (c *Client) ListAppInstallations(ctx context.Context, minter *secrets.AppTokenMinter) ([]sourcecontrol.AppInstallationSummary, error) {
	if minter == nil {
		return nil, fmt.Errorf("app minter required")
	}
	jwt, err := minter.SignAppJWT(time.Now())
	if err != nil {
		return nil, fmt.Errorf("sign app JWT: %w", err)
	}

	type pageItem struct {
		ID      int64 `json:"id"`
		Account struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"account"`
	}

	all := make([]sourcecontrol.AppInstallationSummary, 0, 16)
	page := 1
	for {
		url := fmt.Sprintf(c.apiBase+"/app/installations?per_page=100&page=%d", page)
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		httpReq.Header.Set("Authorization", "Bearer "+jwt)
		httpReq.Header.Set("Accept", "application/vnd.github+json")
		httpReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("github API request: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, &sourcecontrol.HTTPStatusError{StatusCode: resp.StatusCode, Body: string(body), URL: url}
		}
		var items []pageItem
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		for _, it := range items {
			all = append(all, sourcecontrol.AppInstallationSummary{
				InstallationID: it.ID,
				AccountLogin:   it.Account.Login,
				AccountType:    it.Account.Type,
			})
		}
		if len(items) < 100 {
			break
		}
		page++
		if page > 10 {
			break
		}
	}
	return all, nil
}

// ExchangeOAuthCode exchanges a GitHub OAuth code for a user-to-server
// access token. Distinct from the App JWT path: this uses the App's
// OAuth client_id + client_secret (Basic-auth style on the form-encoded
// body, per GitHub's docs) and calls the github.com endpoint (not api.github.com).
//
// Returns the access_token string. Empty token + nil error means the
// exchange came back with no token (user revoked or invalid code; GitHub
// returns 200 with an `error` field rather than non-2xx). Caller treats
// empty as authorisation failure.
func (c *Client) ExchangeOAuthCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (string, error) {
	if clientID == "" || clientSecret == "" {
		return "", fmt.Errorf("oauth: client_id/secret required")
	}
	form := strings.NewReader(fmt.Sprintf(
		"client_id=%s&client_secret=%s&code=%s&redirect_uri=%s",
		clientID, clientSecret, code, redirectURI,
	))
	url := "https://github.com/login/oauth/access_token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, form)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("oauth exchange: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", &sourcecontrol.HTTPStatusError{StatusCode: resp.StatusCode, Body: string(body), URL: url}
	}
	var out struct {
		AccessToken      string `json:"access_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("decode oauth response: %w", err)
	}
	if out.Error != "" {
		// GitHub returns 200 + {"error":"bad_verification_code", ...} on
		// invalid/expired codes. Treat as auth failure (empty token).
		return "", fmt.Errorf("oauth exchange: %s: %s", out.Error, out.ErrorDescription)
	}
	return out.AccessToken, nil
}

// GetUserInstallations pages through GET /user/installations with a
// user-to-server access token. Returns the list of installation IDs the
// authenticated user has admin access to (per GitHub's "explicit
// permission" semantics — for orgs that means org admin; for user
// accounts, the user's own account).
//
// Used by BindAppInstallation to verify the user is actually an admin
// of the installation they're trying to bind, closing the cross-tenant
// race on the bind path.
func (c *Client) GetUserInstallations(ctx context.Context, userToken string) ([]int64, error) {
	if userToken == "" {
		return nil, fmt.Errorf("user token required")
	}

	type pageResp struct {
		TotalCount    int `json:"total_count"`
		Installations []struct {
			ID int64 `json:"id"`
		} `json:"installations"`
	}

	all := make([]int64, 0, 4)
	page := 1
	for {
		url := fmt.Sprintf(c.apiBase+"/user/installations?per_page=100&page=%d", page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+userToken)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("github API request: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, &sourcecontrol.HTTPStatusError{StatusCode: resp.StatusCode, Body: string(body), URL: url}
		}
		var pr pageResp
		if err := json.Unmarshal(body, &pr); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		for _, it := range pr.Installations {
			all = append(all, it.ID)
		}
		if len(pr.Installations) < 100 {
			break
		}
		page++
		if page > 10 {
			break
		}
	}
	return all, nil
}
