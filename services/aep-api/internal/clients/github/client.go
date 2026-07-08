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
// A single *Client satisfies gitrepo.Host — the REST surface (repo / issue /
// pull-request / webhook / app-installation) in client.go. The git-object
// (Git-Data) surface is gone: all repo content reads/writes run on the
// disk-backed Workspace engine (internal/platform/gitfs, see
// docs/design/shared-volume-clone-architecture.md §9 for what stays REST).
// GitHub Projects v2 is dropped (tasks-github-native §4): Tasks are plain
// GitHub issues, so there is no GraphQL board surface.
//
// This is the only place in the codebase that builds Authorization: Bearer
// headers (authHeaders / the App-JWT and pat paths). Selected by GIT_PROVIDER
// in the composition root; a GitLab/Gitea host would be a sibling clients/*
// package satisfying the same ports.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	urlpkg "net/url"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/credentials"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
)

// Client is the GitHub host client — one http.Client, one auth path, serving
// gitrepo.Host as a single implementation. Stateless; concurrent calls are safe.
type Client struct {
	httpClient *http.Client
	// apiBase is the GitHub REST API root, default "https://api.github.com".
	// Overridable only via WithAPIBase (a test seam).
	apiBase string
}

// Option configures the client at construction. Production wiring passes no
// options; the base-URL options are test seams for the gittest tier.
type Option func(*Client)

// WithAPIBase overrides the GitHub REST API base URL. This is a TEST SEAM — it
// lets the gittest tier point the real client at an httptest fake (see
// internal/platform/gittest). Not wired in production.
func WithAPIBase(base string) Option {
	return func(c *Client) { c.apiBase = strings.TrimRight(base, "/") }
}

// NewClient builds the GitHub host client. Production wiring passes no options.
func NewClient(opts ...Option) gitrepo.Host {
	c := &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		apiBase:    "https://api.github.com",
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Compile-time proof the concrete client satisfies the whole provider surface.
var _ gitrepo.Host = (*Client)(nil)

// authHeaders sets the standard GitHub API headers and the Authorization
// header. Token is fetched fresh on every call from the credential —
// long-lived PATs are a no-op here, short-lived App tokens refresh on
// demand through the same path.
func authHeaders(ctx context.Context, req *http.Request, cred credentials.Credential) error {
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
func (c *Client) doJSON(ctx context.Context, method, url, label string, cred credentials.Credential, payload, out any, okStatuses ...int) error {
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
// into out. Any other status returns *gitrepo.HTTPStatusError so callers can
// branch on the code (404 vs 5xx).
func (c *Client) getJSON(ctx context.Context, url string, cred credentials.Credential, out any) error {
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
		return &gitrepo.HTTPStatusError{StatusCode: resp.StatusCode, Body: string(respBody), URL: url}
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) CreateOrgRepo(ctx context.Context, cred credentials.Credential, req gitrepo.CreateOrgRepoRequest) (string, error) {
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
		return "", gitrepo.ErrRepoNameConflict
	}

	return "", fmt.Errorf("github repo create failed (status %d): %s", resp.StatusCode, string(respBody))
}

func (c *Client) createUserRepo(ctx context.Context, cred credentials.Credential, payload map[string]any) (string, error) {
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
		return "", gitrepo.ErrRepoNameConflict
	}
	return "", fmt.Errorf("github user repo create failed (status %d): %s", resp.StatusCode, string(respBody))
}

func (c *Client) CreateIssue(ctx context.Context, owner, repo string, cred credentials.Credential, req gitrepo.CreateIssueRequest) (*gitrepo.IssueResult, error) {
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
		return &gitrepo.IssueResult{Number: created.Number, URL: created.HTMLURL, NodeID: created.NodeID}, nil
	}

	return nil, fmt.Errorf("github issue create failed (status %d): %s", resp.StatusCode, string(respBody))
}

func (c *Client) EnsureLabel(ctx context.Context, owner, repo string, cred credentials.Credential, name, color string) error {
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

func (c *Client) CloseIssue(ctx context.Context, owner, repo string, cred credentials.Credential, number int) error {
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

func (c *Client) EditIssueBody(ctx context.Context, owner, repo string, cred credentials.Credential, number int, body string) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d", owner, repo, number)
	return c.doJSON(ctx, http.MethodPatch, url, "issue edit", cred, map[string]string{"body": body}, nil, http.StatusOK)
}

// EditIssueTitle replaces the issue title via PATCH /issues/{number}. Used by
// the plan tap's updateTask handler when a planned Task is renamed.
func (c *Client) EditIssueTitle(ctx context.Context, owner, repo string, cred credentials.Credential, number int, title string) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d", owner, repo, number)
	return c.doJSON(ctx, http.MethodPatch, url, "issue title edit", cred, map[string]string{"title": title}, nil, http.StatusOK)
}

// GetPullRequest returns the live state of a pull request (GET /pulls/{n}) — the
// sweep's PR-state reconciliation input (docs/design/tasks-github-native.md §5:
// PR state is native GitHub truth healed by the sweep when a webhook is missed).
func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, cred credentials.Credential, number int) (*gitrepo.PullRequestState, error) {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/pulls/%d", owner, repo, number)
	var raw struct {
		State          string `json:"state"` // "open" | "closed"
		Merged         bool   `json:"merged"`
		MergeCommitSHA string `json:"merge_commit_sha"`
	}
	if err := c.getJSON(ctx, url, cred, &raw); err != nil {
		return nil, err
	}
	return &gitrepo.PullRequestState{State: raw.State, Merged: raw.Merged, MergeCommitSHA: raw.MergeCommitSHA}, nil
}

// MergePullRequest merges a pull request via PUT /pulls/{number}/merge
// (auto code-review mode, §R3.5). Idempotent: if the PUT reports the PR is not
// currently mergeable (405 — GitHub's response for "already merged" as well as
// "not mergeable yet"), it re-checks the live PR state and treats an
// already-merged PR as success rather than an error, so a Temporal retry after
// a successful merge whose response was lost does not fail the activity.
func (c *Client) MergePullRequest(ctx context.Context, owner, repo string, cred credentials.Credential, number int) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/pulls/%d/merge", owner, repo, number)
	var out struct {
		Merged  bool   `json:"merged"`
		Message string `json:"message"`
	}
	err := c.doJSON(ctx, http.MethodPut, url, "pull request merge", cred,
		map[string]string{"merge_method": "squash"}, &out, http.StatusOK)
	if err == nil {
		return nil
	}
	// Re-check live state before surfacing the error — a 405 "not mergeable"
	// can mean "already merged" as well as a real blocker.
	if state, gerr := c.GetPullRequest(ctx, owner, repo, cred, number); gerr == nil && state != nil && state.Merged {
		return nil
	}
	return err
}

func (c *Client) CommentIssue(ctx context.Context, owner, repo string, cred credentials.Credential, number int, body string) error {
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

func (c *Client) ListIssues(ctx context.Context, owner, repo string, cred credentials.Credential, labels []string) ([]gitrepo.IssueInfo, error) {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues?state=all&per_page=100", owner, repo)
	if len(labels) > 0 {
		url += "&labels=" + strings.Join(labels, ",")
	}

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
		return nil, fmt.Errorf("github list issues failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var raw []struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		State   string `json:"state"`
		Labels  []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	issues := make([]gitrepo.IssueInfo, 0, len(raw))
	for _, r := range raw {
		labelNames := make([]string, 0, len(r.Labels))
		for _, l := range r.Labels {
			labelNames = append(labelNames, l.Name)
		}
		issues = append(issues, gitrepo.IssueInfo{
			Number: r.Number,
			Title:  r.Title,
			Body:   r.Body,
			URL:    r.HTMLURL,
			State:  r.State,
			Labels: labelNames,
		})
	}
	return issues, nil
}

// AddIssueLabels adds labels to an existing issue via POST
// /repos/{owner}/{repo}/issues/{number}/labels. GitHub merges them with the
// issue's current labels (adding a label already present is a no-op). Used to
// stamp aep:status/* projection and aep:attention flags. 200 is success.
func (c *Client) AddIssueLabels(ctx context.Context, owner, repo string, cred credentials.Credential, number int, labels []string) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d/labels", owner, repo, number)
	return c.doJSON(ctx, http.MethodPost, url, "add issue labels", cred, map[string][]string{"labels": labels}, nil, http.StatusOK)
}

// RemoveIssueLabel removes one label from an issue via DELETE
// /repos/{owner}/{repo}/issues/{number}/labels/{name}. The label name is
// path-escaped (aep:status/* contains ':' and '/'). A 404 is treated as success
// — the label is already absent, which is the desired post-state (idempotent).
// Used to consume the aep:execute command label and clear stale projections.
func (c *Client) RemoveIssueLabel(ctx context.Context, owner, repo string, cred credentials.Credential, number int, label string) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d/labels/%s", owner, repo, number, urlpkg.PathEscape(label))
	// 404 is success: the label is already absent, the desired post-state.
	return c.doJSON(ctx, http.MethodDelete, url, "remove issue label", cred, nil, nil, http.StatusOK, http.StatusNotFound)
}

// SetIssueLabels replaces the issue's entire label set via PUT
// /repos/{owner}/{repo}/issues/{number}/labels. Unlike AddIssueLabels this is
// authoritative — labels absent from the slice are removed. 200 is success.
func (c *Client) SetIssueLabels(ctx context.Context, owner, repo string, cred credentials.Credential, number int, labels []string) error {
	// Never send a nil slice — GitHub then leaves labels unchanged rather than
	// clearing them; an explicit empty array clears.
	if labels == nil {
		labels = []string{}
	}
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d/labels", owner, repo, number)
	return c.doJSON(ctx, http.MethodPut, url, "set issue labels", cred, map[string][]string{"labels": labels}, nil, http.StatusOK)
}

func (c *Client) RegisterWebhook(ctx context.Context, owner, repo string, cred credentials.Credential, deliveryURL, hmacSecret string, events []string) (int64, error) {
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

func (c *Client) findHookByURL(ctx context.Context, owner, repo string, cred credentials.Credential, deliveryURL string) (int64, error) {
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
func (c *Client) UpdateWebhookEvents(ctx context.Context, owner, repo string, cred credentials.Credential, hookID int64, events []string) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/hooks/%d", owner, repo, hookID)
	return c.doJSON(ctx, http.MethodPatch, url, "update webhook events", cred, map[string]any{"events": events}, nil, http.StatusOK)
}

// GetUser performs GET /user using the credential's token. Returns
// HTTPStatusError for non-2xx responses so the validator can branch on
// 401 (revoked) vs 5xx (transient).
func (c *Client) GetUser(ctx context.Context, cred credentials.Credential) (*gitrepo.GitHubUser, error) {
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
		return nil, &gitrepo.HTTPStatusError{StatusCode: resp.StatusCode, Body: string(respBody), URL: url}
	}
	var user gitrepo.GitHubUser
	if err := json.Unmarshal(respBody, &user); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &user, nil
}

// GetAppInstallation calls GET /app/installations/{id}. Authenticated by
// the App JWT (not an installation token) — App-level endpoints reject
// installation tokens. The minter exposes the JWT through SignAppJWT().
func (c *Client) GetAppInstallation(ctx context.Context, minter *credentials.AppTokenMinter, installationID int64) (*gitrepo.AppInstallationInfo, error) {
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
		return nil, &gitrepo.HTTPStatusError{StatusCode: resp.StatusCode, Body: string(respBody), URL: url}
	}
	var info gitrepo.AppInstallationInfo
	if err := json.Unmarshal(respBody, &info); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &info, nil
}

func (c *Client) DeleteInstallation(ctx context.Context, minter *credentials.AppTokenMinter, installationID int64) error {
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
	return &gitrepo.HTTPStatusError{StatusCode: resp.StatusCode, Body: string(respBody), URL: url}
}

// ListAppInstallations pages through GET /app/installations using the
// App JWT. Returns the flat AppInstallationSummary projection. Caps
// the walk at 10 pages × 100 = 1000 installations as a defensive bound;
// real-world dev/single-tenant installs are an order of magnitude under.
func (c *Client) ListAppInstallations(ctx context.Context, minter *credentials.AppTokenMinter) ([]gitrepo.AppInstallationSummary, error) {
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

	all := make([]gitrepo.AppInstallationSummary, 0, 16)
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
			return nil, &gitrepo.HTTPStatusError{StatusCode: resp.StatusCode, Body: string(body), URL: url}
		}
		var items []pageItem
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		for _, it := range items {
			all = append(all, gitrepo.AppInstallationSummary{
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
		return "", &gitrepo.HTTPStatusError{StatusCode: resp.StatusCode, Body: string(body), URL: url}
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
			return nil, &gitrepo.HTTPStatusError{StatusCode: resp.StatusCode, Body: string(body), URL: url}
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
