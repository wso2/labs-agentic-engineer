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

// Package github is ae-studio-tools' GitHub client: REST and GraphQL over the
// org's gitpat. It is the only place in the pod that builds a GitHub
// Authorization header (authHeaders, asking Config.Token per request).
//
// Moved from aep-api's sourcecontrol/githubhost (phase 4). Repo content never
// goes through here: it runs on the pod's git engine (internal/repo). GraphQL
// (graphql.go) is used only where REST cannot answer in one call (milestone
// counts and comment reads).
//
// Every non-2xx answer the client does not map to a sentinel is an
// *HTTPStatusError (errors.go); a GraphQL errors[] is a *GraphQLError.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	urlpkg "net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	apiBaseURL     = "https://api.github.com"
	requestTimeout = 30 * time.Second
	// maxBodyBytes bounds what is read from one GitHub answer. A full page of
	// 100 issues with maximal bodies is ~6.5 MiB; a larger answer is cut and
	// fails to decode rather than growing memory without bound.
	maxBodyBytes = 32 << 20
	// maxErrorBodyBytes bounds the GitHub answer an *HTTPStatusError carries.
	maxErrorBodyBytes = 2048
	// defaultRetryAfter is used when GitHub rate-limits without saying for how long.
	defaultRetryAfter = 60 * time.Second
)

// Config is the client's wiring. Token is asked on every request, so a
// rotated gitpat is picked up without a restart. HookURL and HookSecret are
// what RegisterWebhook installs. APIBase defaults to https://api.github.com,
// GraphQLURL to APIBase + "/graphql", HTTP to a client with a 30 s timeout.
type Config struct {
	APIBase, GraphQLURL string
	Token               func(context.Context) (string, error)
	HookURL, HookSecret string
	HTTP                *http.Client
}

// Client is the GitHub REST and GraphQL client over the org's gitpat.
// Stateless past its Config; concurrent calls are safe.
type Client struct {
	cfg             Config
	httpClient      *http.Client
	apiBase         string
	graphqlEndpoint string
}

// New builds the client from cfg, filling the defaults Config names.
func New(cfg Config) *Client {
	c := &Client{cfg: cfg, httpClient: cfg.HTTP, apiBase: strings.TrimRight(cfg.APIBase, "/"), graphqlEndpoint: cfg.GraphQLURL}
	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: requestTimeout}
	}
	if c.apiBase == "" {
		c.apiBase = apiBaseURL
	}
	if c.graphqlEndpoint == "" {
		c.graphqlEndpoint = c.apiBase + "/graphql"
	}
	return c
}

// authHeaders sets the standard GitHub API headers and the bearer token,
// asked of Config.Token on every call.
func (c *Client) authHeaders(ctx context.Context, req *http.Request) error {
	if c.cfg.Token == nil {
		return errors.New("github: no token source configured")
	}
	token, err := c.cfg.Token(ctx)
	if err != nil {
		return fmt.Errorf("resolve token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return nil
}

// response is one GitHub answer with its body read (bounded by maxBodyBytes).
type response struct {
	status int
	header http.Header
	body   []byte
}

// send runs one authenticated request: payload (nil: no body) is marshalled
// as JSON. Any status is returned; only a request that never got an answer
// is an error. Transport errors (*url.Error) name the method and URL, never
// the token.
func (c *Client) send(ctx context.Context, method, url string, payload any) (*response, error) {
	var reqBody io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if err := c.authHeaders(ctx, req); err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github API request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("github API response: %w", err)
	}
	return &response{status: resp.StatusCode, header: resp.Header, body: body}, nil
}

// statusError is the *HTTPStatusError for an answer the caller did not
// accept, at time now. GitHub signals a rate limit with 429, or 403 carrying
// Retry-After (secondary limit) or X-RateLimit-Remaining: 0 (primary limit,
// reset at X-RateLimit-Reset epoch seconds); each is normalised to 429 with
// RetryAfter.
func statusError(r *response, url string, now time.Time) *HTTPStatusError {
	body := r.body
	if len(body) > maxErrorBodyBytes {
		body = body[:maxErrorBodyBytes]
	}
	e := &HTTPStatusError{StatusCode: r.status, Body: string(body), URL: url}
	if r.status != http.StatusForbidden && r.status != http.StatusTooManyRequests {
		return e
	}
	if s, err := strconv.Atoi(r.header.Get("Retry-After")); err == nil && s > 0 {
		e.StatusCode, e.RetryAfter = http.StatusTooManyRequests, time.Duration(s)*time.Second
		return e
	}
	if r.header.Get("X-RateLimit-Remaining") == "0" {
		wait := defaultRetryAfter
		if reset, err := strconv.ParseInt(r.header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			if d := time.Unix(reset, 0).Sub(now); d > 0 {
				// Retry-After is whole seconds; under half a second would
				// round to 0, which means "retry now".
				wait = max(d.Round(time.Second), time.Second)
			}
		}
		e.StatusCode, e.RetryAfter = http.StatusTooManyRequests, wait
		return e
	}
	if r.status == http.StatusTooManyRequests {
		e.RetryAfter = defaultRetryAfter
	}
	return e
}

// doJSON sends payload (nil: no body) and accepts okStatuses; any other
// answer is an *HTTPStatusError. When out is non-nil the accepted body is
// decoded into it.
func (c *Client) doJSON(ctx context.Context, method, url string, payload, out any, okStatuses ...int) error {
	r, err := c.send(ctx, method, url, payload)
	if err != nil {
		return err
	}
	if !slices.Contains(okStatuses, r.status) {
		return statusError(r, url, time.Now())
	}
	if out != nil {
		if err := json.Unmarshal(r.body, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// getJSON is an authenticated GET that accepts 200 and decodes into out.
func (c *Client) getJSON(ctx context.Context, url string, out any) error {
	return c.doJSON(ctx, http.MethodGet, url, nil, out, http.StatusOK)
}

// CreateOrgRepo creates a repo owned by owner and answers it as GitHub holds
// it. A 404 on POST /orgs/{owner}/repos means owner is not an org the gitpat
// can see: when owner IS the gitpat's own user (GET /user, compared
// case-insensitively) the call is retried once on POST /user/repos, which
// creates under that account; otherwise the 404 is the answer, so nothing
// is ever created under an account the caller did not name. A taken name is
// ErrRepoNameConflict.
func (c *Client) CreateOrgRepo(ctx context.Context, owner string, req CreateOrgRepoRequest) (*Repository, error) {
	if owner == "" {
		return nil, errors.New("repo owner is required")
	}
	payload := map[string]any{
		"name":        req.Name,
		"private":     req.Private,
		"auto_init":   req.AutoInit,
		"description": req.Description,
	}
	url := fmt.Sprintf(c.apiBase+"/orgs/%s/repos", owner)
	r, err := c.send(ctx, http.MethodPost, url, payload)
	if err != nil {
		return nil, err
	}
	if r.status == http.StatusNotFound {
		u, err := c.User(ctx)
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(u.Login, owner) {
			return nil, statusError(r, url, time.Now())
		}
		url = c.apiBase + "/user/repos"
		if r, err = c.send(ctx, http.MethodPost, url, payload); err != nil {
			return nil, err
		}
	}
	switch {
	case r.status == http.StatusCreated:
		return decodeRepository(r.body)
	case r.status == http.StatusUnprocessableEntity && bytes.Contains(r.body, []byte("name already exists")):
		return nil, ErrRepoNameConflict
	}
	return nil, statusError(r, url, time.Now())
}

// GetRepo answers owner/name as GitHub holds it (GET /repos/{owner}/{name}).
func (c *Client) GetRepo(ctx context.Context, owner, name string) (*Repository, error) {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s", owner, name)
	r, err := c.send(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if r.status != http.StatusOK {
		return nil, statusError(r, url, time.Now())
	}
	return decodeRepository(r.body)
}

// decodeRepository reads the fields Repository keeps from a GitHub
// repository object.
func decodeRepository(body []byte) (*Repository, error) {
	var raw struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if raw.Name == "" || raw.Owner.Login == "" || raw.DefaultBranch == "" {
		return nil, errors.New("github response missing name, owner or default_branch")
	}
	return &Repository{Owner: raw.Owner.Login, Name: raw.Name, DefaultBranch: raw.DefaultBranch}, nil
}

// CreateIssue opens an issue (POST /repos/{owner}/{repo}/issues).
func (c *Client) CreateIssue(ctx context.Context, owner, repo string, req CreateIssueRequest) (*IssueResult, error) {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues", owner, repo)
	var created struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
		NodeID  string `json:"node_id"`
	}
	if err := c.doJSON(ctx, http.MethodPost, url, req, &created, http.StatusCreated); err != nil {
		return nil, err
	}
	if created.HTMLURL == "" {
		return nil, errors.New("github response missing html_url")
	}
	return &IssueResult{Number: created.Number, URL: created.HTMLURL, NodeID: created.NodeID}, nil
}

// EnsureLabel creates a label if it does not exist; GitHub's 422 (already
// exists) is success.
func (c *Client) EnsureLabel(ctx context.Context, owner, repo string, name, color string) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/labels", owner, repo)
	return c.doJSON(ctx, http.MethodPost, url, map[string]string{"name": name, "color": color}, nil,
		http.StatusCreated, http.StatusUnprocessableEntity)
}

// CloseIssue closes an issue with reason "completed".
func (c *Client) CloseIssue(ctx context.Context, owner, repo string, number int) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d", owner, repo, number)
	return c.doJSON(ctx, http.MethodPatch, url, map[string]string{"state": "closed", "state_reason": "completed"}, nil, http.StatusOK)
}

// EditIssueBody replaces the issue body (PATCH /issues/{number}).
func (c *Client) EditIssueBody(ctx context.Context, owner, repo string, number int, body string) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d", owner, repo, number)
	return c.doJSON(ctx, http.MethodPatch, url, map[string]string{"body": body}, nil, http.StatusOK)
}

// EditIssueTitle replaces the issue title (PATCH /issues/{number}).
func (c *Client) EditIssueTitle(ctx context.Context, owner, repo string, number int, title string) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d", owner, repo, number)
	return c.doJSON(ctx, http.MethodPatch, url, map[string]string{"title": title}, nil, http.StatusOK)
}

// ReopenIssue sets the issue state back to open. It names no state_reason:
// GitHub drops the closed reason itself on reopen. Reopening an open issue is
// a 200 no-op, so callers need no read-before-write.
func (c *Client) ReopenIssue(ctx context.Context, owner, repo string, number int) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d", owner, repo, number)
	return c.doJSON(ctx, http.MethodPatch, url, map[string]string{"state": "open"}, nil, http.StatusOK)
}

// SetIssueMilestone assigns an issue to a milestone by NUMBER (GitHub
// answers 422 to a title).
func (c *Client) SetIssueMilestone(ctx context.Context, owner, repo string, number, milestoneNumber int) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d", owner, repo, number)
	return c.doJSON(ctx, http.MethodPatch, url, map[string]int{"milestone": milestoneNumber}, nil, http.StatusOK)
}

// GetPullRequest answers a pull request's live state (GET /pulls/{n}).
func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (*PullRequestState, error) {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/pulls/%d", owner, repo, number)
	var raw struct {
		State          string `json:"state"` // "open" | "closed"
		Merged         bool   `json:"merged"`
		MergeCommitSHA string `json:"merge_commit_sha"`
	}
	if err := c.getJSON(ctx, url, &raw); err != nil {
		return nil, err
	}
	return &PullRequestState{State: raw.State, Merged: raw.Merged, MergeCommitSHA: raw.MergeCommitSHA}, nil
}

// MergePullRequest squash-merges a pull request (PUT /pulls/{n}/merge).
// GitHub answers 405 when the PR is not mergeable (checks pending,
// conflicts, already merged). So a retry after a lost success answer is not
// a failure, a merge error is reconciled against the live PR state: an
// already-merged PR is success.
func (c *Client) MergePullRequest(ctx context.Context, owner, repo string, number int) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/pulls/%d/merge", owner, repo, number)
	err := c.doJSON(ctx, http.MethodPut, url, map[string]string{"merge_method": "squash"}, nil, http.StatusOK)
	if err == nil {
		return nil
	}
	if state, gerr := c.GetPullRequest(ctx, owner, repo, number); gerr == nil && state.Merged {
		return nil
	}
	return err
}

// ListPullRequestFiles answers the path of every file a pull request changed
// (GET /pulls/{n}/files, 100 per page, until a short page).
func (c *Client) ListPullRequestFiles(ctx context.Context, owner, repo string, number int) ([]string, error) {
	var files []string
	for page := 1; ; page++ {
		url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/pulls/%d/files?per_page=100&page=%d", owner, repo, number, page)
		var raw []struct {
			Filename string `json:"filename"`
		}
		if err := c.getJSON(ctx, url, &raw); err != nil {
			return nil, err
		}
		for _, f := range raw {
			files = append(files, f.Filename)
		}
		if len(raw) < 100 {
			return files, nil
		}
	}
}

// CommentIssue posts a comment on an issue.
func (c *Client) CommentIssue(ctx context.Context, owner, repo string, number int, body string) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d/comments", owner, repo, number)
	return c.doJSON(ctx, http.MethodPost, url, map[string]string{"body": body}, nil, http.StatusCreated)
}

// issueListMaxPages bounds ListIssues' page walk: each page is a request
// against the gitpat's rate budget, and 10 pages keep a 1000-issue
// repository complete.
const issueListMaxPages = 10

// issueWire is the subset of a GitHub issue the list and get reads decode.
type issueWire struct {
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

// info projects the wire issue onto IssueInfo.
func (w issueWire) info() IssueInfo {
	labels := make([]string, 0, len(w.Labels))
	for _, l := range w.Labels {
		labels = append(labels, l.Name)
	}
	return IssueInfo{
		Number: w.Number, Title: w.Title, Body: w.Body, URL: w.HTMLURL,
		State: w.State, StateReason: w.StateReason, ClosedAt: w.ClosedAt, Labels: labels,
	}
}

// ListIssues answers the repository's issues in every state, filtered by
// label (AND), newest first, following pages until a short one or
// issueListMaxPages; past the cap the OLDEST are missing (logged), GetIssue
// still reaches them. Pull requests are dropped.
func (c *Client) ListIssues(ctx context.Context, owner, repo string, labels []string) ([]IssueInfo, error) {
	base := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues?state=all&per_page=%d", owner, repo, milestonePageSize)
	if len(labels) > 0 {
		base += "&labels=" + urlpkg.QueryEscape(strings.Join(labels, ","))
	}
	var issues []IssueInfo
	for page := 1; page <= issueListMaxPages; page++ {
		var raw []issueWire
		if err := c.getJSON(ctx, fmt.Sprintf("%s&page=%d", base, page), &raw); err != nil {
			return nil, err
		}
		for _, r := range raw {
			if r.PullRequest == nil {
				issues = append(issues, r.info())
			}
		}
		// Page length counts PRs too, so it — not len(issues) — decides the walk.
		if len(raw) < milestonePageSize {
			return issues, nil
		}
	}
	slog.WarnContext(ctx, "github.issue_list_capped", "owner", owner, "repo", repo, "pages", issueListMaxPages)
	return issues, nil
}

// GetIssue answers one issue by number; a 404 is ErrIssueNotFound.
func (c *Client) GetIssue(ctx context.Context, owner, repo string, number int) (*IssueInfo, error) {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d", owner, repo, number)
	var raw issueWire
	if err := c.getJSON(ctx, url, &raw); err != nil {
		if IsHTTPStatus(err, http.StatusNotFound) {
			return nil, ErrIssueNotFound
		}
		return nil, err
	}
	info := raw.info()
	return &info, nil
}

// AddIssueLabels adds labels to an issue (merged with its current ones).
func (c *Client) AddIssueLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d/labels", owner, repo, number)
	return c.doJSON(ctx, http.MethodPost, url, map[string][]string{"labels": labels}, nil, http.StatusOK)
}

// RemoveIssueLabel removes one label (path-escaped: an aep: label holds ':'
// and may hold '/'). 404 is success: the label is already absent.
func (c *Client) RemoveIssueLabel(ctx context.Context, owner, repo string, number int, label string) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d/labels/%s", owner, repo, number, urlpkg.PathEscape(label))
	return c.doJSON(ctx, http.MethodDelete, url, nil, nil, http.StatusOK, http.StatusNotFound)
}

// SetIssueLabels replaces the issue's whole label set.
func (c *Client) SetIssueLabels(ctx context.Context, owner, repo string, number int, labels []string) error {
	// A nil slice would marshal as null, which GitHub reads as "unchanged";
	// an explicit empty array clears.
	if labels == nil {
		labels = []string{}
	}
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues/%d/labels", owner, repo, number)
	return c.doJSON(ctx, http.MethodPut, url, map[string][]string{"labels": labels}, nil, http.StatusOK)
}

// RegisterWebhook installs a repo webhook delivering to Config.HookURL,
// signed with Config.HookSecret, and answers its id. GitHub's 422 "Hook
// already exists" (same URL) answers the existing hook's id, found across
// every page of the repo's hooks, with existed set; its config and events
// are left as they are (ReconfigureWebhook replaces both).
func (c *Client) RegisterWebhook(ctx context.Context, owner, repo string, events []string) (id int64, existed bool, err error) {
	payload := map[string]any{
		"name":   "web",
		"active": true,
		"events": events,
		"config": c.hookConfig(),
	}
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/hooks", owner, repo)
	r, err := c.send(ctx, http.MethodPost, url, payload)
	if err != nil {
		return 0, false, err
	}
	switch {
	case r.status == http.StatusCreated:
		var hook struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(r.body, &hook); err != nil {
			return 0, false, fmt.Errorf("decode response: %w", err)
		}
		return hook.ID, false, nil
	case r.status == http.StatusUnprocessableEntity && bytes.Contains(r.body, []byte("Hook already exists")):
		id, err := c.findHookByURL(ctx, owner, repo, c.cfg.HookURL)
		return id, err == nil, err
	}
	return 0, false, statusError(r, url, time.Now())
}

// maxHookPages bounds findHookByURL's walk (GitHub allows 20 hooks per event
// per repo, so one page of 100 is the norm).
const maxHookPages = 10

// findHookByURL answers the id of the repo hook delivering to deliveryURL,
// following Link rel="next" across pages. A next link outside the API base is
// refused: the token is never sent to another host.
func (c *Client) findHookByURL(ctx context.Context, owner, repo string, deliveryURL string) (int64, error) {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/hooks?per_page=100", owner, repo)
	for range maxHookPages {
		r, err := c.send(ctx, http.MethodGet, url, nil)
		if err != nil {
			return 0, err
		}
		if r.status != http.StatusOK {
			return 0, statusError(r, url, time.Now())
		}
		var hooks []struct {
			ID     int64 `json:"id"`
			Config struct {
				URL string `json:"url"`
			} `json:"config"`
		}
		if err := json.Unmarshal(r.body, &hooks); err != nil {
			return 0, fmt.Errorf("decode response: %w", err)
		}
		for _, h := range hooks {
			if h.Config.URL == deliveryURL {
				return h.ID, nil
			}
		}
		next := nextPageURL(r.header)
		if next == "" {
			return 0, errors.New("github: no repo hook delivers to the configured URL")
		}
		if !strings.HasPrefix(next, c.apiBase+"/") {
			return 0, errors.New("github: hook list next page is outside the API base")
		}
		url = next
	}
	return 0, fmt.Errorf("github: hook list longer than %d pages", maxHookPages)
}

// nextPageURL is the rel="next" target of a Link header, or "".
func nextPageURL(h http.Header) string {
	for _, link := range strings.Split(h.Get("Link"), ",") {
		target, params, ok := strings.Cut(link, ";")
		if !ok {
			continue
		}
		for _, p := range strings.Split(params, ";") {
			if strings.TrimSpace(p) == `rel="next"` {
				return strings.Trim(strings.TrimSpace(target), "<>")
			}
		}
	}
	return ""
}

// hookConfig is the config every hook of this pod carries: its delivery URL
// and its signing secret.
func (c *Client) hookConfig() map[string]string {
	return map[string]string{
		"url":          c.cfg.HookURL,
		"content_type": "json",
		"secret":       c.cfg.HookSecret,
		"insecure_ssl": "0",
	}
}

// ReconfigureWebhook replaces an existing hook's whole config and its events
// (PATCH /hooks/{id}) and makes it active. A hook found by URL may have been
// installed with another secret (one a disconnect could not remove, signed
// with the webhook secret the disconnect then deleted); GitHub never returns
// the secret, so it is always sent.
func (c *Client) ReconfigureWebhook(ctx context.Context, owner, repo string, hookID int64, events []string) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/hooks/%d", owner, repo, hookID)
	payload := map[string]any{"active": true, "events": events, "config": c.hookConfig()}
	return c.doJSON(ctx, http.MethodPatch, url, payload, nil, http.StatusOK)
}

// DeleteWebhook removes the hook with the id the platform stored at
// registration, never one found by scanning, so no other integration's hook
// can be caught. 404 and 410 (GitHub reaped a failing hook) are success: the
// hook is already gone.
func (c *Client) DeleteWebhook(ctx context.Context, owner, repo string, hookID int64) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/hooks/%d", owner, repo, hookID)
	return c.doJSON(ctx, http.MethodDelete, url, nil, nil,
		http.StatusNoContent, http.StatusNotFound, http.StatusGone)
}
