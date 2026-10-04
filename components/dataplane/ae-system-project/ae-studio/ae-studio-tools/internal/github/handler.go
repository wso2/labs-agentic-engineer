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

package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/wso2/aep/ae-studio-tools/internal/gen"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// The /internal/v1 GitHub ops (04 §3): POST /repos, and under
// /repos/{owner}/{repo} the issue, milestone, pull request and hook ops. One
// Client call each (two for an adopt or a hook ensure), one shared error map
// (problem), no retries (04 §12; aep-api decides). The edge embeds Handler
// in its /internal/v1 server; the gate, the body cap, the request validator
// and the owner guard (for the path-scoped ops) ran before any method here.

// Handler serves the GitHub ops over the gitpat's Client.
type Handler struct {
	gh *Client
	// owner is the org's connected GitHub account (AE_GITHUB_OWNER), the
	// only owner create-repo creates for; empty refuses every create.
	owner string
}

// Option configures a Handler.
type Option func(*Handler)

// WithOwner names the org's connected GitHub account.
func WithOwner(owner string) Option {
	return func(h *Handler) { h.owner = owner }
}

// NewHandler serves gh.
func NewHandler(gh *Client, opts ...Option) Handler {
	h := Handler{gh: gh}
	for _, o := range opts {
		o(&h)
	}
	return h
}

// ownerAllowed reports whether owner is the connected account. GitHub owner
// names are case-insensitive; an unset account matches nothing.
func (h Handler) ownerAllowed(owner string) bool {
	return h.owner != "" && strings.EqualFold(h.owner, owner)
}

func ownerNotAllowed() problemReply {
	return newProblemReply(http.StatusForbidden, "owner_not_allowed", "the repository's owner is not the org's connected GitHub account")
}

// CreateRepo creates owner/name, initialised, under the connected account.
// The owner is checked before any GitHub call, so the client's /user/repos
// fallback only ever runs for the connected owner; a repository GitHub
// answers under any other owner (the fallback landing on the gitpat's own
// account) is refused too. A taken name is 409 unless adoptExisting, which
// answers the existing repository (200).
func (h Handler) CreateRepo(ctx context.Context, req gen.CreateRepoRequestObject) (gen.CreateRepoResponseObject, error) {
	b := req.Body
	if !h.ownerAllowed(b.Owner) {
		return ownerNotAllowed(), nil
	}
	created := true
	got, err := h.gh.CreateOrgRepo(ctx, b.Owner, CreateOrgRepoRequest{Name: b.Name, Private: b.Private, AutoInit: true, Description: b.Description})
	if errors.Is(err, ErrRepoNameConflict) {
		if !b.AdoptExisting {
			return newProblemReply(http.StatusConflict, "repo_name_conflict", "the repository name is taken"), nil
		}
		created = false
		got, err = h.gh.GetRepo(ctx, b.Owner, b.Name)
	}
	if err != nil {
		return h.problem(ctx, "create-repo", b.Owner, b.Name, err)
	}
	if !h.ownerAllowed(got.Owner) {
		slog.WarnContext(ctx, "github.repo_owner_mismatch", "owner", b.Owner, "githubOwner", got.Owner, "repo", strings.ToLower(got.Owner+"/"+got.Name))
		return ownerNotAllowed(), nil
	}
	coords := gen.RepoCoordinates{Owner: got.Owner, Repo: got.Name, CloneURL: repo.GitHubCloneURL(got.Owner, got.Name), DefaultBranch: got.DefaultBranch}
	if created {
		return gen.CreateRepo201JSONResponse(coords), nil
	}
	return gen.CreateRepo200JSONResponse(coords), nil
}

// RegisterHook ensures the studio's hook on the repository with these
// events: GitHub creates it, or, when a hook for the pod's URL exists, its
// events are replaced. A failed replace fails the call (the hook keeps its
// old events; aep-api's retry ensures again).
func (h Handler) RegisterHook(ctx context.Context, req gen.RegisterHookRequestObject) (gen.RegisterHookResponseObject, error) {
	events := hookEvents(req.Body.Events)
	id, existed, err := h.gh.RegisterWebhook(ctx, req.Owner, req.Repo, events)
	if err == nil && existed {
		err = h.gh.UpdateWebhookEvents(ctx, req.Owner, req.Repo, id, events)
	}
	if err != nil {
		return h.problem(ctx, "register-hook", req.Owner, req.Repo, err)
	}
	return gen.RegisterHook200JSONResponse{ID: id}, nil
}

// UpdateHookEvents replaces a hook's events.
func (h Handler) UpdateHookEvents(ctx context.Context, req gen.UpdateHookEventsRequestObject) (gen.UpdateHookEventsResponseObject, error) {
	if err := h.gh.UpdateWebhookEvents(ctx, req.Owner, req.Repo, req.HookID, hookEvents(req.Body.Events)); err != nil {
		return h.problem(ctx, "update-hook-events", req.Owner, req.Repo, err)
	}
	return gen.UpdateHookEvents204Response{}, nil
}

// hookEvents is the contract's event enum as GitHub's event names.
func hookEvents(in []gen.HookEventsRequestEvents) []string {
	out := make([]string, 0, len(in))
	for _, e := range in {
		out = append(out, string(e))
	}
	return out
}

// DeleteHook deletes a hook; one GitHub no longer has is success.
func (h Handler) DeleteHook(ctx context.Context, req gen.DeleteHookRequestObject) (gen.DeleteHookResponseObject, error) {
	if err := h.gh.DeleteWebhook(ctx, req.Owner, req.Repo, req.HookID); err != nil {
		return h.problem(ctx, "delete-hook", req.Owner, req.Repo, err)
	}
	return gen.DeleteHook204Response{}, nil
}

// ListIssues answers the repository's issues, newest first.
func (h Handler) ListIssues(ctx context.Context, req gen.ListIssuesRequestObject) (gen.ListIssuesResponseObject, error) {
	issues, err := h.gh.ListIssues(ctx, req.Owner, req.Repo, req.Params.Labels)
	if err != nil {
		return h.problem(ctx, "list-issues", req.Owner, req.Repo, err)
	}
	return gen.ListIssues200JSONResponse{Issues: issuesOut(issues)}, nil
}

// CreateIssue opens an issue.
func (h Handler) CreateIssue(ctx context.Context, req gen.CreateIssueRequestObject) (gen.CreateIssueResponseObject, error) {
	b := req.Body
	res, err := h.gh.CreateIssue(ctx, req.Owner, req.Repo, CreateIssueRequest{Title: b.Title, Body: b.Body, Labels: b.Labels, Milestone: b.Milestone})
	if err != nil {
		return h.problem(ctx, "create-issue", req.Owner, req.Repo, err)
	}
	return gen.CreateIssue201JSONResponse{Number: res.Number, URL: res.URL, NodeID: res.NodeID}, nil
}

// GetIssue answers one issue.
func (h Handler) GetIssue(ctx context.Context, req gen.GetIssueRequestObject) (gen.GetIssueResponseObject, error) {
	issue, err := h.gh.GetIssue(ctx, req.Owner, req.Repo, req.Number)
	if err != nil {
		return h.problem(ctx, "get-issue", req.Owner, req.Repo, err)
	}
	return gen.GetIssue200JSONResponse(issueOut(*issue)), nil
}

// ListIssueComments answers an issue's newest comments, oldest first.
func (h Handler) ListIssueComments(ctx context.Context, req gen.ListIssueCommentsRequestObject) (gen.ListIssueCommentsResponseObject, error) {
	comments, err := h.gh.ListIssueComments(ctx, req.Owner, req.Repo, req.Number, req.Params.Limit)
	if err != nil {
		return h.problem(ctx, "list-issue-comments", req.Owner, req.Repo, err)
	}
	return gen.ListIssueComments200JSONResponse{Comments: commentsOut(comments)}, nil
}

// CreateIssueComment comments on an issue.
func (h Handler) CreateIssueComment(ctx context.Context, req gen.CreateIssueCommentRequestObject) (gen.CreateIssueCommentResponseObject, error) {
	if err := h.gh.CommentIssue(ctx, req.Owner, req.Repo, req.Number, req.Body.Body); err != nil {
		return h.problem(ctx, "create-issue-comment", req.Owner, req.Repo, err)
	}
	return gen.CreateIssueComment204Response{}, nil
}

// CloseIssue closes an issue as completed.
func (h Handler) CloseIssue(ctx context.Context, req gen.CloseIssueRequestObject) (gen.CloseIssueResponseObject, error) {
	if err := h.gh.CloseIssue(ctx, req.Owner, req.Repo, req.Number); err != nil {
		return h.problem(ctx, "close-issue", req.Owner, req.Repo, err)
	}
	return gen.CloseIssue204Response{}, nil
}

// ReopenIssue reopens an issue.
func (h Handler) ReopenIssue(ctx context.Context, req gen.ReopenIssueRequestObject) (gen.ReopenIssueResponseObject, error) {
	if err := h.gh.ReopenIssue(ctx, req.Owner, req.Repo, req.Number); err != nil {
		return h.problem(ctx, "reopen-issue", req.Owner, req.Repo, err)
	}
	return gen.ReopenIssue204Response{}, nil
}

// SetIssueBody replaces an issue's body.
func (h Handler) SetIssueBody(ctx context.Context, req gen.SetIssueBodyRequestObject) (gen.SetIssueBodyResponseObject, error) {
	if err := h.gh.EditIssueBody(ctx, req.Owner, req.Repo, req.Number, req.Body.Body); err != nil {
		return h.problem(ctx, "set-issue-body", req.Owner, req.Repo, err)
	}
	return gen.SetIssueBody204Response{}, nil
}

// SetIssueTitle replaces an issue's title.
func (h Handler) SetIssueTitle(ctx context.Context, req gen.SetIssueTitleRequestObject) (gen.SetIssueTitleResponseObject, error) {
	if err := h.gh.EditIssueTitle(ctx, req.Owner, req.Repo, req.Number, req.Body.Title); err != nil {
		return h.problem(ctx, "set-issue-title", req.Owner, req.Repo, err)
	}
	return gen.SetIssueTitle204Response{}, nil
}

// SetIssueMilestone assigns an issue to a milestone by number.
func (h Handler) SetIssueMilestone(ctx context.Context, req gen.SetIssueMilestoneRequestObject) (gen.SetIssueMilestoneResponseObject, error) {
	if err := h.gh.SetIssueMilestone(ctx, req.Owner, req.Repo, req.Number, req.Body.Number); err != nil {
		return h.problem(ctx, "set-issue-milestone", req.Owner, req.Repo, err)
	}
	return gen.SetIssueMilestone204Response{}, nil
}

// AddIssueLabels adds labels to an issue.
func (h Handler) AddIssueLabels(ctx context.Context, req gen.AddIssueLabelsRequestObject) (gen.AddIssueLabelsResponseObject, error) {
	if err := h.gh.AddIssueLabels(ctx, req.Owner, req.Repo, req.Number, req.Body.Labels); err != nil {
		return h.problem(ctx, "add-issue-labels", req.Owner, req.Repo, err)
	}
	return gen.AddIssueLabels204Response{}, nil
}

// SetIssueLabels replaces an issue's labels.
func (h Handler) SetIssueLabels(ctx context.Context, req gen.SetIssueLabelsRequestObject) (gen.SetIssueLabelsResponseObject, error) {
	if err := h.gh.SetIssueLabels(ctx, req.Owner, req.Repo, req.Number, req.Body.Labels); err != nil {
		return h.problem(ctx, "set-issue-labels", req.Owner, req.Repo, err)
	}
	return gen.SetIssueLabels204Response{}, nil
}

// RemoveIssueLabel removes one label from an issue.
func (h Handler) RemoveIssueLabel(ctx context.Context, req gen.RemoveIssueLabelRequestObject) (gen.RemoveIssueLabelResponseObject, error) {
	if err := h.gh.RemoveIssueLabel(ctx, req.Owner, req.Repo, req.Number, req.Label); err != nil {
		return h.problem(ctx, "remove-issue-label", req.Owner, req.Repo, err)
	}
	return gen.RemoveIssueLabel204Response{}, nil
}

// EnsureLabel creates a label unless it exists.
func (h Handler) EnsureLabel(ctx context.Context, req gen.EnsureLabelRequestObject) (gen.EnsureLabelResponseObject, error) {
	if err := h.gh.EnsureLabel(ctx, req.Owner, req.Repo, req.Body.Name, req.Body.Color); err != nil {
		return h.problem(ctx, "ensure-label", req.Owner, req.Repo, err)
	}
	return gen.EnsureLabel204Response{}, nil
}

// ListMilestones answers the milestones in a state (all when omitted).
func (h Handler) ListMilestones(ctx context.Context, req gen.ListMilestonesRequestObject) (gen.ListMilestonesResponseObject, error) {
	milestones, err := h.gh.ListMilestones(ctx, req.Owner, req.Repo, string(req.Params.State))
	if err != nil {
		return h.problem(ctx, "list-milestones", req.Owner, req.Repo, err)
	}
	out := make([]gen.Milestone, 0, len(milestones))
	for _, m := range milestones {
		out = append(out, gen.Milestone{Number: m.Number, Title: m.Title, State: m.State, Description: m.Description, NodeID: m.NodeID})
	}
	return gen.ListMilestones200JSONResponse{Milestones: out}, nil
}

// CreateMilestone creates a milestone unless one has the title.
func (h Handler) CreateMilestone(ctx context.Context, req gen.CreateMilestoneRequestObject) (gen.CreateMilestoneResponseObject, error) {
	res, err := h.gh.CreateMilestone(ctx, req.Owner, req.Repo, CreateMilestoneRequest{Title: req.Body.Title, Description: req.Body.Description})
	if err != nil {
		return h.problem(ctx, "create-milestone", req.Owner, req.Repo, err)
	}
	return gen.CreateMilestone200JSONResponse{Number: res.Number, Created: res.Created}, nil
}

// CloseMilestone closes a milestone.
func (h Handler) CloseMilestone(ctx context.Context, req gen.CloseMilestoneRequestObject) (gen.CloseMilestoneResponseObject, error) {
	if err := h.gh.CloseMilestone(ctx, req.Owner, req.Repo, req.Number); err != nil {
		return h.problem(ctx, "close-milestone", req.Owner, req.Repo, err)
	}
	return gen.CloseMilestone204Response{}, nil
}

// ReopenMilestone reopens a milestone.
func (h Handler) ReopenMilestone(ctx context.Context, req gen.ReopenMilestoneRequestObject) (gen.ReopenMilestoneResponseObject, error) {
	if err := h.gh.ReopenMilestone(ctx, req.Owner, req.Repo, req.Number); err != nil {
		return h.problem(ctx, "reopen-milestone", req.Owner, req.Repo, err)
	}
	return gen.ReopenMilestone204Response{}, nil
}

// ListMilestoneIssues answers a milestone's issues.
func (h Handler) ListMilestoneIssues(ctx context.Context, req gen.ListMilestoneIssuesRequestObject) (gen.ListMilestoneIssuesResponseObject, error) {
	issues, err := h.gh.ListMilestoneIssues(ctx, req.Owner, req.Repo, MilestoneIssuesFilter{
		Number: req.Number, State: string(req.Params.State), Labels: req.Params.Labels,
	})
	if err != nil {
		return h.problem(ctx, "list-milestone-issues", req.Owner, req.Repo, err)
	}
	return gen.ListMilestoneIssues200JSONResponse{Issues: issuesOut(issues)}, nil
}

// GetMilestoneCounts answers a milestone's open-issue populations.
func (h Handler) GetMilestoneCounts(ctx context.Context, req gen.GetMilestoneCountsRequestObject) (gen.GetMilestoneCountsResponseObject, error) {
	c, err := h.gh.MilestoneIssueCounts(ctx, req.Owner, req.Repo, req.Number)
	if err != nil {
		return h.problem(ctx, "get-milestone-counts", req.Owner, req.Repo, err)
	}
	return gen.GetMilestoneCounts200JSONResponse{
		OpenProvision: c.OpenProvision, OpenTotal: c.OpenTotal, OpenAgentWork: c.OpenAgentWork,
		OpenDevelopment: c.OpenDevelopment, OpenValidation: c.OpenValidation, OpenValidationRepairs: c.OpenValidationRepairs,
	}, nil
}

// ListMilestoneComments answers the newest comments of each of a
// milestone's issues, in issue-number order.
func (h Handler) ListMilestoneComments(ctx context.Context, req gen.ListMilestoneCommentsRequestObject) (gen.ListMilestoneCommentsResponseObject, error) {
	byIssue, err := h.gh.ListMilestoneIssueComments(ctx, req.Owner, req.Repo, req.Number, req.Params.PerIssue)
	if err != nil {
		return h.problem(ctx, "list-milestone-comments", req.Owner, req.Repo, err)
	}
	numbers := make([]int, 0, len(byIssue))
	for n := range byIssue {
		numbers = append(numbers, n)
	}
	slices.Sort(numbers)
	out := make([]gen.IssueComments, 0, len(numbers))
	for _, n := range numbers {
		out = append(out, gen.IssueComments{Number: n, Comments: commentsOut(byIssue[n])})
	}
	return gen.ListMilestoneComments200JSONResponse{Issues: out}, nil
}

// GetPull answers a pull request's state.
func (h Handler) GetPull(ctx context.Context, req gen.GetPullRequestObject) (gen.GetPullResponseObject, error) {
	pr, err := h.gh.GetPullRequest(ctx, req.Owner, req.Repo, req.Number)
	if err != nil {
		return h.problem(ctx, "get-pull", req.Owner, req.Repo, err)
	}
	return gen.GetPull200JSONResponse{State: pr.State, Merged: pr.Merged, MergeCommitSha: pr.MergeCommitSHA}, nil
}

// MergePull squash-merges a pull request (already merged is success).
func (h Handler) MergePull(ctx context.Context, req gen.MergePullRequestObject) (gen.MergePullResponseObject, error) {
	if err := h.gh.MergePullRequest(ctx, req.Owner, req.Repo, req.Number); err != nil {
		return h.problem(ctx, "merge-pull", req.Owner, req.Repo, err)
	}
	return gen.MergePull204Response{}, nil
}

// ListPullFiles answers every path a pull request changed.
func (h Handler) ListPullFiles(ctx context.Context, req gen.ListPullFilesRequestObject) (gen.ListPullFilesResponseObject, error) {
	files, err := h.gh.ListPullRequestFiles(ctx, req.Owner, req.Repo, req.Number)
	if err != nil {
		return h.problem(ctx, "list-pull-files", req.Owner, req.Repo, err)
	}
	if files == nil {
		files = []string{}
	}
	return gen.ListPullFiles200JSONResponse{Files: files}, nil
}

func issueOut(i IssueInfo) gen.IssueInfo {
	labels := i.Labels
	if labels == nil {
		labels = []string{}
	}
	return gen.IssueInfo{
		Number: i.Number, Title: i.Title, Body: i.Body, URL: i.URL, State: i.State,
		StateReason: i.StateReason, ClosedAt: i.ClosedAt, Labels: labels,
	}
}

func issuesOut(issues []IssueInfo) []gen.IssueInfo {
	out := make([]gen.IssueInfo, 0, len(issues))
	for _, i := range issues {
		out = append(out, issueOut(i))
	}
	return out
}

func commentsOut(comments []IssueComment) []gen.IssueComment {
	out := make([]gen.IssueComment, 0, len(comments))
	for _, c := range comments {
		out = append(out, gen.IssueComment{
			ID: c.ID, Author: c.Author, Body: c.Body, URL: c.URL, CreatedAt: c.CreatedAt,
			Machine: c.Machine, Observed: c.Observed,
		})
	}
	return out
}

// problem maps a failed GitHub call to its answer (04 §12): the missing
// subject 404, a rate limit (REST, or GraphQL RATE_LIMITED) 429 with
// Retry-After, anything else 502 github_error with GitHub's status when it
// answered one (a GraphQL NOT_FOUND names 404). A caller that left gets its
// ctx error (the generic 500 it never sees). The log line carries the op,
// the repository and the statuses, never GitHub's text.
func (h Handler) problem(ctx context.Context, op, owner, repo string, err error) (problemReply, error) {
	if ctx.Err() != nil {
		return problemReply{}, ctx.Err()
	}
	var p problemReply
	var se *HTTPStatusError
	switch {
	case errors.Is(err, ErrIssueNotFound):
		return newProblemReply(http.StatusNotFound, "issue_not_found", "no issue carries that number"), nil
	case errors.Is(err, ErrMilestoneNotFound):
		return newProblemReply(http.StatusNotFound, "milestone_not_found", "no milestone carries that number"), nil
	case IsGraphQLType(err, "RATE_LIMITED"):
		p = rateLimited(defaultRetryAfter.Seconds())
	default:
		if wait, limited := RateLimited(err); limited {
			p = rateLimited(wait.Seconds())
			break
		}
		p = newProblemReply(http.StatusBadGateway, "github_error", "GitHub could not complete the call")
		switch {
		case errors.As(err, &se):
			p.GithubStatus = se.StatusCode
		case IsGraphQLType(err, "NOT_FOUND"):
			p.GithubStatus = http.StatusNotFound
		}
		if p.GithubStatus != 0 {
			p.Detail = fmt.Sprintf("GitHub answered %d", p.GithubStatus)
		}
	}
	slog.WarnContext(ctx, "github.call_failed", "op", op, "repo", strings.ToLower(owner+"/"+repo),
		"status", p.Status, "githubStatus", p.GithubStatus)
	return p, nil
}

// rateLimited is the 429 answer, waiting seconds rounded up (GitHub's default
// wait when it named none).
func rateLimited(seconds float64) problemReply {
	p := newProblemReply(http.StatusTooManyRequests, "github_rate_limited", "GitHub rate-limited the gitpat")
	p.RetryAfter = int(math.Ceil(seconds))
	if p.RetryAfter <= 0 {
		p.RetryAfter = int(defaultRetryAfter.Seconds())
	}
	return p
}

// problemReply is a problem answer for any GitHub op: it satisfies every
// op's generated response interface, which one per-status type per op
// cannot. RetryAfter (seconds) is sent as Retry-After when set.
type problemReply struct {
	gen.Problem
	RetryAfter int
}

func newProblemReply(status int, code, detail string) problemReply {
	return problemReply{Problem: gen.Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Code: code, Detail: detail}}
}

func (p problemReply) write(w http.ResponseWriter) error {
	if p.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(p.RetryAfter))
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	return json.NewEncoder(w).Encode(p.Problem)
}

// VisitListIssuesResponse implements gen.ListIssuesResponseObject.
func (p problemReply) VisitListIssuesResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitCreateIssueResponse implements gen.CreateIssueResponseObject.
func (p problemReply) VisitCreateIssueResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitGetIssueResponse implements gen.GetIssueResponseObject.
func (p problemReply) VisitGetIssueResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitListIssueCommentsResponse implements gen.ListIssueCommentsResponseObject.
func (p problemReply) VisitListIssueCommentsResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitCreateIssueCommentResponse implements gen.CreateIssueCommentResponseObject.
func (p problemReply) VisitCreateIssueCommentResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitCloseIssueResponse implements gen.CloseIssueResponseObject.
func (p problemReply) VisitCloseIssueResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitReopenIssueResponse implements gen.ReopenIssueResponseObject.
func (p problemReply) VisitReopenIssueResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitSetIssueBodyResponse implements gen.SetIssueBodyResponseObject.
func (p problemReply) VisitSetIssueBodyResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitSetIssueTitleResponse implements gen.SetIssueTitleResponseObject.
func (p problemReply) VisitSetIssueTitleResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitSetIssueMilestoneResponse implements gen.SetIssueMilestoneResponseObject.
func (p problemReply) VisitSetIssueMilestoneResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitAddIssueLabelsResponse implements gen.AddIssueLabelsResponseObject.
func (p problemReply) VisitAddIssueLabelsResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitSetIssueLabelsResponse implements gen.SetIssueLabelsResponseObject.
func (p problemReply) VisitSetIssueLabelsResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitRemoveIssueLabelResponse implements gen.RemoveIssueLabelResponseObject.
func (p problemReply) VisitRemoveIssueLabelResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitEnsureLabelResponse implements gen.EnsureLabelResponseObject.
func (p problemReply) VisitEnsureLabelResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitListMilestonesResponse implements gen.ListMilestonesResponseObject.
func (p problemReply) VisitListMilestonesResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitCreateMilestoneResponse implements gen.CreateMilestoneResponseObject.
func (p problemReply) VisitCreateMilestoneResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitCloseMilestoneResponse implements gen.CloseMilestoneResponseObject.
func (p problemReply) VisitCloseMilestoneResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitReopenMilestoneResponse implements gen.ReopenMilestoneResponseObject.
func (p problemReply) VisitReopenMilestoneResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitListMilestoneIssuesResponse implements gen.ListMilestoneIssuesResponseObject.
func (p problemReply) VisitListMilestoneIssuesResponse(w http.ResponseWriter) error {
	return p.write(w)
}

// VisitGetMilestoneCountsResponse implements gen.GetMilestoneCountsResponseObject.
func (p problemReply) VisitGetMilestoneCountsResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitListMilestoneCommentsResponse implements gen.ListMilestoneCommentsResponseObject.
func (p problemReply) VisitListMilestoneCommentsResponse(w http.ResponseWriter) error {
	return p.write(w)
}

// VisitGetPullResponse implements gen.GetPullResponseObject.
func (p problemReply) VisitGetPullResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitMergePullResponse implements gen.MergePullResponseObject.
func (p problemReply) VisitMergePullResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitListPullFilesResponse implements gen.ListPullFilesResponseObject.
func (p problemReply) VisitListPullFilesResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitCreateRepoResponse implements gen.CreateRepoResponseObject.
func (p problemReply) VisitCreateRepoResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitRegisterHookResponse implements gen.RegisterHookResponseObject.
func (p problemReply) VisitRegisterHookResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitUpdateHookEventsResponse implements gen.UpdateHookEventsResponseObject.
func (p problemReply) VisitUpdateHookEventsResponse(w http.ResponseWriter) error { return p.write(w) }

// VisitDeleteHookResponse implements gen.DeleteHookResponseObject.
func (p problemReply) VisitDeleteHookResponse(w http.ResponseWriter) error { return p.write(w) }
