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

// issues.go — the issue, milestone and pull-request ops (the IssueOps port
// keyed by RepoRef), one pod call each, each one GitHub call on the pod.

import (
	"context"
	"fmt"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/gen"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// CreateIssue opens an issue (in its milestone when the request names one).
// aep-api's own fields (dedupe key, component, ...) never leave aep-api.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) CreateIssue(ctx context.Context, ref RepoRef, req sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error) {
	if err := validRef(ref); err != nil {
		return nil, err
	}
	body := gen.CreateIssueRequest{Title: req.Title, Body: req.Body, Labels: req.Labels, Milestone: req.Milestone}
	var reply gen.IssueResult
	err := a.do(ctx, ref.Org, "create-issue", &reply, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.CreateIssue(ctx, ref.Owner, ref.Repo, &gen.CreateIssueParams{XImpersonateOrg: org}, body, auth)
	})
	if err != nil {
		return nil, err
	}
	return &sourcecontrol.IssueResult{Number: reply.Number, URL: reply.URL, NodeID: reply.NodeID}, nil
}

// ListIssues lists the issues carrying every label, newest first.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) ListIssues(ctx context.Context, ref RepoRef, labels []string) ([]sourcecontrol.IssueInfo, error) {
	if err := validRef(ref); err != nil {
		return nil, err
	}
	var reply gen.IssueList
	err := a.do(ctx, ref.Org, "list-issues", &reply, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.ListIssues(ctx, ref.Owner, ref.Repo, &gen.ListIssuesParams{Labels: labels, XImpersonateOrg: org}, auth)
	})
	if err != nil {
		return nil, err
	}
	return issuesIn(reply.Issues), nil
}

// GetIssue reads one issue; ErrIssueNotFound when GitHub holds none.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) GetIssue(ctx context.Context, ref RepoRef, number int) (*sourcecontrol.IssueInfo, error) {
	if err := validRef(ref); err != nil {
		return nil, err
	}
	var reply gen.IssueInfo
	err := a.do(ctx, ref.Org, "get-issue", &reply, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.GetIssue(ctx, ref.Owner, ref.Repo, number, &gen.GetIssueParams{XImpersonateOrg: org}, auth)
	})
	if err != nil {
		return nil, err
	}
	info := issueIn(reply)
	return &info, nil
}

// ListIssueComments answers the newest limit comments of one issue, oldest
// first. limit always travels (the pod requires it; 0 answers none).
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) ListIssueComments(ctx context.Context, ref RepoRef, number, limit int) ([]sourcecontrol.IssueComment, error) {
	if err := validRef(ref); err != nil {
		return nil, err
	}
	var reply gen.IssueCommentList
	err := a.do(ctx, ref.Org, "list-issue-comments", &reply, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.ListIssueComments(ctx, ref.Owner, ref.Repo, number, &gen.ListIssueCommentsParams{Limit: limit, XImpersonateOrg: org}, auth)
	})
	if err != nil {
		return nil, err
	}
	return commentsIn(reply.Comments), nil
}

// EnsureLabel creates the label unless it exists.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) EnsureLabel(ctx context.Context, ref RepoRef, name, color string) error {
	if err := validRef(ref); err != nil {
		return err
	}
	return a.do(ctx, ref.Org, "ensure-label", nil, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.EnsureLabel(ctx, ref.Owner, ref.Repo, &gen.EnsureLabelParams{XImpersonateOrg: org}, gen.EnsureLabelRequest{Name: name, Color: color}, auth)
	})
}

// CloseIssue closes the issue as completed.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) CloseIssue(ctx context.Context, ref RepoRef, number int) error {
	return a.writeIssue(ctx, ref, "close-issue", func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.CloseIssue(ctx, ref.Owner, ref.Repo, number, &gen.CloseIssueParams{XImpersonateOrg: org}, auth)
	})
}

// ReopenIssue reopens the issue.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) ReopenIssue(ctx context.Context, ref RepoRef, number int) error {
	return a.writeIssue(ctx, ref, "reopen-issue", func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.ReopenIssue(ctx, ref.Owner, ref.Repo, number, &gen.ReopenIssueParams{XImpersonateOrg: org}, auth)
	})
}

// CommentIssue posts a comment on the issue.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) CommentIssue(ctx context.Context, ref RepoRef, number int, body string) error {
	return a.writeIssue(ctx, ref, "create-issue-comment", func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.CreateIssueComment(ctx, ref.Owner, ref.Repo, number, &gen.CreateIssueCommentParams{XImpersonateOrg: org}, gen.IssueCommentRequest{Body: body}, auth)
	})
}

// EditIssueBody replaces the issue body.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) EditIssueBody(ctx context.Context, ref RepoRef, number int, body string) error {
	return a.writeIssue(ctx, ref, "set-issue-body", func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.SetIssueBody(ctx, ref.Owner, ref.Repo, number, &gen.SetIssueBodyParams{XImpersonateOrg: org}, gen.IssueBodyRequest{Body: body}, auth)
	})
}

// EditIssueTitle replaces the issue title.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) EditIssueTitle(ctx context.Context, ref RepoRef, number int, title string) error {
	return a.writeIssue(ctx, ref, "set-issue-title", func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.SetIssueTitle(ctx, ref.Owner, ref.Repo, number, &gen.SetIssueTitleParams{XImpersonateOrg: org}, gen.IssueTitleRequest{Title: title}, auth)
	})
}

// AddIssueLabels adds labels to the issue.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) AddIssueLabels(ctx context.Context, ref RepoRef, number int, labels []string) error {
	return a.writeIssue(ctx, ref, "add-issue-labels", func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.AddIssueLabels(ctx, ref.Owner, ref.Repo, number, &gen.AddIssueLabelsParams{XImpersonateOrg: org}, gen.IssueLabelsRequest{Labels: nonNil(labels)}, auth)
	})
}

// RemoveIssueLabel removes one label; an absent label is success.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) RemoveIssueLabel(ctx context.Context, ref RepoRef, number int, label string) error {
	return a.writeIssue(ctx, ref, "remove-issue-label", func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.RemoveIssueLabel(ctx, ref.Owner, ref.Repo, number, label, &gen.RemoveIssueLabelParams{XImpersonateOrg: org}, auth)
	})
}

// SetIssueLabels replaces the issue's whole label set (nil: none).
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) SetIssueLabels(ctx context.Context, ref RepoRef, number int, labels []string) error {
	return a.writeIssue(ctx, ref, "set-issue-labels", func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.SetIssueLabels(ctx, ref.Owner, ref.Repo, number, &gen.SetIssueLabelsParams{XImpersonateOrg: org}, gen.IssueLabelsRequest{Labels: nonNil(labels)}, auth)
	})
}

// SetIssueMilestone assigns the issue to a milestone by number.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) SetIssueMilestone(ctx context.Context, ref RepoRef, number, milestoneNumber int) error {
	return a.writeIssue(ctx, ref, "set-issue-milestone", func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.SetIssueMilestone(ctx, ref.Owner, ref.Repo, number, &gen.SetIssueMilestoneParams{XImpersonateOrg: org}, gen.IssueMilestoneRequest{Number: milestoneNumber}, auth)
	})
}

// writeIssue runs one write on an existing issue. GitHub answers a write on
// an issue it does not hold with 404, which the pod relays as github_error
// githubStatus 404 (not issue_not_found): that is ErrIssueNotFound here.
func (a *Adapter) writeIssue(ctx context.Context, ref RepoRef, op string, fn call) error {
	if err := validRef(ref); err != nil {
		return err
	}
	err := a.do(ctx, ref.Org, op, nil, fn)
	if sourcecontrol.IsHTTPStatus(err, http.StatusNotFound) {
		return fmt.Errorf("%w: %w", sourcecontrol.ErrIssueNotFound, err)
	}
	return err
}

// GetPullRequest reads a pull request's state.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) GetPullRequest(ctx context.Context, ref RepoRef, number int) (*sourcecontrol.PullRequestState, error) {
	if err := validRef(ref); err != nil {
		return nil, err
	}
	var reply gen.PullRequestState
	err := a.do(ctx, ref.Org, "get-pull", &reply, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.GetPull(ctx, ref.Owner, ref.Repo, number, &gen.GetPullParams{XImpersonateOrg: org}, auth)
	})
	if err != nil {
		return nil, err
	}
	return &sourcecontrol.PullRequestState{State: reply.State, Merged: reply.Merged, MergeCommitSHA: reply.MergeCommitSha}, nil
}

// MergePullRequest squash-merges an open pull request; GitHub's 405
// (not mergeable) is an HTTPStatusError 405.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) MergePullRequest(ctx context.Context, ref RepoRef, number int) error {
	if err := validRef(ref); err != nil {
		return err
	}
	return a.do(ctx, ref.Org, "merge-pull", nil, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.MergePull(ctx, ref.Owner, ref.Repo, number, &gen.MergePullParams{XImpersonateOrg: org}, auth)
	})
}

// ListPullRequestFiles lists the paths a pull request changes.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) ListPullRequestFiles(ctx context.Context, ref RepoRef, number int) ([]string, error) {
	if err := validRef(ref); err != nil {
		return nil, err
	}
	var reply gen.PullRequestFiles
	err := a.do(ctx, ref.Org, "list-pull-files", &reply, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.ListPullFiles(ctx, ref.Owner, ref.Repo, number, &gen.ListPullFilesParams{XImpersonateOrg: org}, auth)
	})
	if err != nil {
		return nil, err
	}
	return reply.Files, nil
}

// CreateMilestone mints a milestone, or adopts the one with that title
// (case-insensitively).
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) CreateMilestone(ctx context.Context, ref RepoRef, req sourcecontrol.CreateMilestoneRequest) (*sourcecontrol.MilestoneResult, error) {
	if err := validRef(ref); err != nil {
		return nil, err
	}
	var reply gen.MilestoneResult
	err := a.do(ctx, ref.Org, "create-milestone", &reply, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.CreateMilestone(ctx, ref.Owner, ref.Repo, &gen.CreateMilestoneParams{XImpersonateOrg: org}, gen.CreateMilestoneRequest{Title: req.Title, Description: req.Description}, auth)
	})
	if err != nil {
		return nil, err
	}
	return &sourcecontrol.MilestoneResult{Number: reply.Number, Created: reply.Created}, nil
}

// CloseMilestone closes a milestone.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) CloseMilestone(ctx context.Context, ref RepoRef, number int) error {
	if err := validRef(ref); err != nil {
		return err
	}
	return a.do(ctx, ref.Org, "close-milestone", nil, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.CloseMilestone(ctx, ref.Owner, ref.Repo, number, &gen.CloseMilestoneParams{XImpersonateOrg: org}, auth)
	})
}

// ReopenMilestone reopens a closed milestone.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) ReopenMilestone(ctx context.Context, ref RepoRef, number int) error {
	if err := validRef(ref); err != nil {
		return err
	}
	return a.do(ctx, ref.Org, "reopen-milestone", nil, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.ReopenMilestone(ctx, ref.Owner, ref.Repo, number, &gen.ReopenMilestoneParams{XImpersonateOrg: org}, auth)
	})
}

// ListMilestones lists every milestone in state ("open", "closed", "all";
// "" is all).
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) ListMilestones(ctx context.Context, ref RepoRef, state string) ([]sourcecontrol.Milestone, error) {
	if err := validRef(ref); err != nil {
		return nil, err
	}
	var reply gen.MilestoneList
	err := a.do(ctx, ref.Org, "list-milestones", &reply, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.ListMilestones(ctx, ref.Owner, ref.Repo, &gen.ListMilestonesParams{State: optional(gen.ListMilestonesParamsState(state)), XImpersonateOrg: org}, auth)
	})
	if err != nil {
		return nil, err
	}
	var out []sourcecontrol.Milestone
	for _, m := range reply.Milestones {
		out = append(out, sourcecontrol.Milestone{Number: m.Number, Title: m.Title, State: m.State, Description: m.Description, NodeID: m.NodeID})
	}
	return out, nil
}

// ListMilestoneIssues lists a milestone's issues by state and label.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) ListMilestoneIssues(ctx context.Context, ref RepoRef, filter sourcecontrol.MilestoneIssuesFilter) ([]sourcecontrol.IssueInfo, error) {
	if err := validRef(ref); err != nil {
		return nil, err
	}
	var reply gen.IssueList
	err := a.do(ctx, ref.Org, "list-milestone-issues", &reply, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.ListMilestoneIssues(ctx, ref.Owner, ref.Repo, filter.Number, &gen.ListMilestoneIssuesParams{
			State: optional(gen.ListMilestoneIssuesParamsState(filter.State)), Labels: filter.Labels, XImpersonateOrg: org,
		}, auth)
	})
	if err != nil {
		return nil, err
	}
	return issuesIn(reply.Issues), nil
}

// MilestoneIssueCounts answers a milestone's open-issue populations.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) MilestoneIssueCounts(ctx context.Context, ref RepoRef, number int) (*sourcecontrol.MilestoneIssueCounts, error) {
	if err := validRef(ref); err != nil {
		return nil, err
	}
	var r gen.MilestoneIssueCounts
	err := a.do(ctx, ref.Org, "get-milestone-counts", &r, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.GetMilestoneCounts(ctx, ref.Owner, ref.Repo, number, &gen.GetMilestoneCountsParams{XImpersonateOrg: org}, auth)
	})
	if err != nil {
		return nil, err
	}
	return &sourcecontrol.MilestoneIssueCounts{
		OpenProvision: r.OpenProvision, OpenTotal: r.OpenTotal, OpenAgentWork: r.OpenAgentWork,
		OpenDevelopment: r.OpenDevelopment, OpenValidation: r.OpenValidation, OpenValidationRepairs: r.OpenValidationRepairs,
	}, nil
}

// ListMilestoneIssueComments answers the newest perIssue comments of every
// issue in the milestone, by issue number, oldest first; an issue without
// comments is absent. perIssue always travels (the pod requires it).
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) ListMilestoneIssueComments(ctx context.Context, ref RepoRef, number, perIssue int) (map[int][]sourcecontrol.IssueComment, error) {
	if err := validRef(ref); err != nil {
		return nil, err
	}
	var reply gen.MilestoneComments
	err := a.do(ctx, ref.Org, "list-milestone-comments", &reply, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.ListMilestoneComments(ctx, ref.Owner, ref.Repo, number, &gen.ListMilestoneCommentsParams{PerIssue: perIssue, XImpersonateOrg: org}, auth)
	})
	if err != nil {
		return nil, err
	}
	out := map[int][]sourcecontrol.IssueComment{}
	for _, is := range reply.Issues {
		if comments := commentsIn(is.Comments); len(comments) > 0 {
			out[is.Number] = comments
		}
	}
	return out, nil
}

func issueIn(i gen.IssueInfo) sourcecontrol.IssueInfo {
	return sourcecontrol.IssueInfo{
		Number: i.Number, Title: i.Title, Body: i.Body, URL: i.URL, State: i.State,
		StateReason: i.StateReason, ClosedAt: i.ClosedAt, Labels: i.Labels,
	}
}

func issuesIn(in []gen.IssueInfo) []sourcecontrol.IssueInfo {
	var out []sourcecontrol.IssueInfo
	for _, i := range in {
		out = append(out, issueIn(i))
	}
	return out
}

func commentsIn(in []gen.IssueComment) []sourcecontrol.IssueComment {
	var out []sourcecontrol.IssueComment
	for _, c := range in {
		out = append(out, sourcecontrol.IssueComment{
			ID: c.ID, Author: c.Author, Body: c.Body, URL: c.URL, CreatedAt: c.CreatedAt, Machine: c.Machine, Observed: c.Observed,
		})
	}
	return out
}

// nonNil sends an empty list as [] (the pod requires the field).
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
