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

// issues.go — the issue side of a repository (the IssueOps sub-port): issues
// with labels, comments and a milestone, pull requests, and milestones.
// Issues and pull requests share one number sequence, as on GitHub.

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

type issue struct {
	info      sourcecontrol.IssueInfo
	milestone int
	comments  []sourcecontrol.IssueComment
}

type pull struct {
	state sourcecontrol.PullRequestState
	files []string
}

// The labels the milestone counts read (one label per population, as the
// GraphQL query behind MilestoneIssueCounts counts them).
const (
	labelProvision     = "provision"
	labelAgentWork     = "aep"
	labelDevelopment   = "development"
	labelValidation    = "validation"
	labelSrcValidation = "src/validation"
)

// Issues lists ref's issues (open and closed, pull requests excluded) by
// number.
func (f *Fake) Issues(ref sourcecontrol.RepoRef) []sourcecontrol.IssueInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.state(ref)
	out := make([]sourcecontrol.IssueInfo, 0, len(st.issues))
	for _, n := range slices.Sorted(maps.Keys(st.issues)) {
		out = append(out, cloneInfo(st.issues[n].info))
	}
	return out
}

// Labels answers ref's repository labels: name → colour, as EnsureLabel
// created them.
func (f *Fake) Labels(ref sourcecontrol.RepoRef) map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.state(ref).labels)
}

// SeedIssue files an issue on ref exactly as info describes it (state,
// state reason, body, labels — what GitHub would report) and answers its
// number; info.Number and info.URL are assigned. milestone 0 leaves it out
// of every milestone.
func (f *Fake) SeedIssue(ref sourcecontrol.RepoRef, info sourcecontrol.IssueInfo, milestone int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.state(ref)
	st.nextNumber++
	info.Number = st.nextNumber
	info.URL = "https://github.com/" + ref.Owner + "/" + ref.Repo + "/issues/" + strconv.Itoa(info.Number)
	if info.State == "" {
		info.State = "open"
	}
	st.issues[info.Number] = &issue{info: cloneInfo(info), milestone: milestone}
	return info.Number
}

// SeedPullRequest opens a pull request on ref changing files and answers its
// number.
func (f *Fake) SeedPullRequest(ref sourcecontrol.RepoRef, files []string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.state(ref)
	st.nextNumber++
	st.pulls[st.nextNumber] = &pull{state: sourcecontrol.PullRequestState{State: "open"}, files: slices.Clone(files)}
	return st.nextNumber
}

// issueOp begins op on ref and answers the repository under f.mu: the
// caller must f.mu.Unlock (via the returned func).
func (f *Fake) issueOp(op string, ref sourcecontrol.RepoRef) (*repoState, func(), error) {
	if err := f.begin(Call{Op: op, Ref: ref}); err != nil {
		return nil, nil, err
	}
	f.mu.Lock()
	return f.state(ref), f.mu.Unlock, nil
}

func (st *repoState) issue(number int) (*issue, error) {
	is, ok := st.issues[number]
	if !ok {
		return nil, sourcecontrol.ErrIssueNotFound
	}
	return is, nil
}

func cloneInfo(in sourcecontrol.IssueInfo) sourcecontrol.IssueInfo {
	in.Labels = slices.Clone(in.Labels)
	return in
}

func hasAll(have, want []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

// CreateIssue opens an issue, in its milestone when the request names one.
func (f *Fake) CreateIssue(_ context.Context, ref sourcecontrol.RepoRef, req sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error) {
	st, unlock, err := f.issueOp(OpCreateIssue, ref)
	if err != nil {
		return nil, err
	}
	defer unlock()
	st.nextNumber++
	n := st.nextNumber
	url := "https://github.com/" + ref.Owner + "/" + ref.Repo + "/issues/" + strconv.Itoa(n)
	is := &issue{info: sourcecontrol.IssueInfo{Number: n, Title: req.Title, Body: req.Body, URL: url, State: "open", Labels: slices.Clone(req.Labels)}}
	if req.Milestone != nil {
		is.milestone = *req.Milestone
	}
	st.issues[n] = is
	return &sourcecontrol.IssueResult{Number: n, URL: url, NodeID: "I_" + strconv.Itoa(n)}, nil
}

// ListIssues lists the issues carrying every label, newest first.
func (f *Fake) ListIssues(_ context.Context, ref sourcecontrol.RepoRef, labels []string) ([]sourcecontrol.IssueInfo, error) {
	st, unlock, err := f.issueOp(OpListIssues, ref)
	if err != nil {
		return nil, err
	}
	defer unlock()
	var out []sourcecontrol.IssueInfo
	for _, n := range slices.Backward(slices.Sorted(maps.Keys(st.issues))) {
		if hasAll(st.issues[n].info.Labels, labels) {
			out = append(out, cloneInfo(st.issues[n].info))
		}
	}
	return out, nil
}

// GetIssue reads one issue; ErrIssueNotFound when there is none.
func (f *Fake) GetIssue(_ context.Context, ref sourcecontrol.RepoRef, number int) (*sourcecontrol.IssueInfo, error) {
	st, unlock, err := f.issueOp(OpGetIssue, ref)
	if err != nil {
		return nil, err
	}
	defer unlock()
	is, err := st.issue(number)
	if err != nil {
		return nil, err
	}
	info := cloneInfo(is.info)
	return &info, nil
}

// ListIssueComments answers the newest limit comments, oldest first (limit 0:
// none, as the pod answers).
func (f *Fake) ListIssueComments(_ context.Context, ref sourcecontrol.RepoRef, number, limit int) ([]sourcecontrol.IssueComment, error) {
	st, unlock, err := f.issueOp(OpListIssueComments, ref)
	if err != nil {
		return nil, err
	}
	defer unlock()
	is, err := st.issue(number)
	if err != nil {
		return nil, err
	}
	return newest(is.comments, limit), nil
}

func newest(comments []sourcecontrol.IssueComment, limit int) []sourcecontrol.IssueComment {
	if len(comments) == 0 || limit <= 0 {
		return nil
	}
	if len(comments) > limit {
		comments = comments[len(comments)-limit:]
	}
	return slices.Clone(comments)
}

// EnsureLabel creates the label once; an existing one is success.
func (f *Fake) EnsureLabel(_ context.Context, ref sourcecontrol.RepoRef, name, color string) error {
	st, unlock, err := f.issueOp(OpEnsureLabel, ref)
	if err != nil {
		return err
	}
	defer unlock()
	if _, ok := st.labels[name]; !ok {
		st.labels[name] = color
	}
	return nil
}

// editIssue begins op and applies edit to the issue.
func (f *Fake) editIssue(op string, ref sourcecontrol.RepoRef, number int, edit func(*issue)) error {
	st, unlock, err := f.issueOp(op, ref)
	if err != nil {
		return err
	}
	defer unlock()
	is, err := st.issue(number)
	if err != nil {
		return err
	}
	edit(is)
	return nil
}

// CloseIssue closes the issue as completed, stamping its closedAt from the
// Fake's clock (each closure is a new one, as on GitHub).
func (f *Fake) CloseIssue(_ context.Context, ref sourcecontrol.RepoRef, number int) error {
	return f.editIssue(OpCloseIssue, ref, number, func(is *issue) {
		is.info.State, is.info.StateReason = "closed", "completed"
		is.info.ClosedAt = f.tick().Format(time.RFC3339)
	})
}

// ReopenIssue reopens the issue.
func (f *Fake) ReopenIssue(_ context.Context, ref sourcecontrol.RepoRef, number int) error {
	return f.editIssue(OpReopenIssue, ref, number, func(is *issue) {
		is.info.State, is.info.StateReason, is.info.ClosedAt = "open", "reopened", ""
	})
}

// CommentIssue appends a comment, flagged Machine / Observed by the marker
// that leads its body, as the host adapter reports them.
func (f *Fake) CommentIssue(_ context.Context, ref sourcecontrol.RepoRef, number int, body string) error {
	return f.editIssue(OpCommentIssue, ref, number, func(is *issue) {
		id := "IC_" + strconv.Itoa(number) + "_" + strconv.Itoa(len(is.comments)+1)
		is.comments = append(is.comments, sourcecontrol.IssueComment{
			ID:       id,
			Body:     body,
			Machine:  strings.HasPrefix(body, sourcecontrol.MachineCommentMarker),
			Observed: strings.HasPrefix(body, sourcecontrol.ObservedCommentMarker),
		})
	})
}

// EditIssueBody replaces the body.
func (f *Fake) EditIssueBody(_ context.Context, ref sourcecontrol.RepoRef, number int, body string) error {
	return f.editIssue(OpEditIssueBody, ref, number, func(is *issue) { is.info.Body = body })
}

// EditIssueTitle replaces the title.
func (f *Fake) EditIssueTitle(_ context.Context, ref sourcecontrol.RepoRef, number int, title string) error {
	return f.editIssue(OpEditIssueTitle, ref, number, func(is *issue) { is.info.Title = title })
}

// AddIssueLabels merges labels into the issue's set.
func (f *Fake) AddIssueLabels(_ context.Context, ref sourcecontrol.RepoRef, number int, labels []string) error {
	return f.editIssue(OpAddIssueLabels, ref, number, func(is *issue) {
		for _, l := range labels {
			if !slices.Contains(is.info.Labels, l) {
				is.info.Labels = append(is.info.Labels, l)
			}
		}
	})
}

// RemoveIssueLabel removes one label; an absent one is success.
func (f *Fake) RemoveIssueLabel(_ context.Context, ref sourcecontrol.RepoRef, number int, label string) error {
	return f.editIssue(OpRemoveIssueLabel, ref, number, func(is *issue) {
		is.info.Labels = slices.DeleteFunc(is.info.Labels, func(l string) bool { return l == label })
	})
}

// SetIssueLabels replaces the issue's label set.
func (f *Fake) SetIssueLabels(_ context.Context, ref sourcecontrol.RepoRef, number int, labels []string) error {
	return f.editIssue(OpSetIssueLabels, ref, number, func(is *issue) { is.info.Labels = slices.Clone(labels) })
}

// SetIssueMilestone moves the issue into the milestone.
func (f *Fake) SetIssueMilestone(_ context.Context, ref sourcecontrol.RepoRef, number, milestoneNumber int) error {
	return f.editIssue(OpSetIssueMilestone, ref, number, func(is *issue) { is.milestone = milestoneNumber })
}

// pullOp begins op and answers the pull request.
func (f *Fake) pullOp(op string, ref sourcecontrol.RepoRef, number int) (*pull, func(), error) {
	st, unlock, err := f.issueOp(op, ref)
	if err != nil {
		return nil, nil, err
	}
	pr, ok := st.pulls[number]
	if !ok {
		unlock()
		return nil, nil, &sourcecontrol.HTTPStatusError{StatusCode: 404, Body: "pull request not found"}
	}
	return pr, unlock, nil
}

// GetPullRequest reads the pull request's state.
func (f *Fake) GetPullRequest(_ context.Context, ref sourcecontrol.RepoRef, number int) (*sourcecontrol.PullRequestState, error) {
	pr, unlock, err := f.pullOp(OpGetPullRequest, ref, number)
	if err != nil {
		return nil, err
	}
	defer unlock()
	state := pr.state
	return &state, nil
}

// MergePullRequest merges an open pull request; a closed one is GitHub's 405.
func (f *Fake) MergePullRequest(_ context.Context, ref sourcecontrol.RepoRef, number int) error {
	pr, unlock, err := f.pullOp(OpMergePullRequest, ref, number)
	if err != nil {
		return err
	}
	defer unlock()
	if pr.state.State != "open" {
		return &sourcecontrol.HTTPStatusError{StatusCode: 405, Body: "pull request is not mergeable"}
	}
	h := blobSHA("merge " + ref.Owner + "/" + ref.Repo + "#" + strconv.Itoa(number))
	pr.state = sourcecontrol.PullRequestState{State: "closed", Merged: true, MergeCommitSHA: h}
	return nil
}

// ListPullRequestFiles lists the paths the pull request changes.
func (f *Fake) ListPullRequestFiles(_ context.Context, ref sourcecontrol.RepoRef, number int) ([]string, error) {
	pr, unlock, err := f.pullOp(OpListPullRequestFiles, ref, number)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return slices.Clone(pr.files), nil
}

// milestone answers the milestone numbered n.
func (st *repoState) milestone(n int) (*sourcecontrol.Milestone, error) {
	if n < 1 || n > len(st.milestones) {
		return nil, sourcecontrol.ErrMilestoneNotFound
	}
	return st.milestones[n-1], nil
}

// CreateMilestone mints a milestone, or adopts the one whose title matches
// case-insensitively.
func (f *Fake) CreateMilestone(_ context.Context, ref sourcecontrol.RepoRef, req sourcecontrol.CreateMilestoneRequest) (*sourcecontrol.MilestoneResult, error) {
	st, unlock, err := f.issueOp(OpCreateMilestone, ref)
	if err != nil {
		return nil, err
	}
	defer unlock()
	for _, m := range st.milestones {
		if strings.EqualFold(m.Title, req.Title) {
			return &sourcecontrol.MilestoneResult{Number: m.Number}, nil
		}
	}
	n := len(st.milestones) + 1
	st.milestones = append(st.milestones, &sourcecontrol.Milestone{
		Number: n, Title: req.Title, State: "open", Description: req.Description, NodeID: "MI_" + strconv.Itoa(n),
	})
	return &sourcecontrol.MilestoneResult{Number: n, Created: true}, nil
}

// setMilestoneState begins op and sets the milestone's state.
func (f *Fake) setMilestoneState(op string, ref sourcecontrol.RepoRef, number int, state string) error {
	st, unlock, err := f.issueOp(op, ref)
	if err != nil {
		return err
	}
	defer unlock()
	m, err := st.milestone(number)
	if err != nil {
		return err
	}
	m.State = state
	return nil
}

// CloseMilestone closes the milestone.
func (f *Fake) CloseMilestone(_ context.Context, ref sourcecontrol.RepoRef, number int) error {
	return f.setMilestoneState(OpCloseMilestone, ref, number, "closed")
}

// ReopenMilestone reopens the milestone.
func (f *Fake) ReopenMilestone(_ context.Context, ref sourcecontrol.RepoRef, number int) error {
	return f.setMilestoneState(OpReopenMilestone, ref, number, "open")
}

// ListMilestones lists the milestones in state ("" or "all": every one).
func (f *Fake) ListMilestones(_ context.Context, ref sourcecontrol.RepoRef, state string) ([]sourcecontrol.Milestone, error) {
	st, unlock, err := f.issueOp(OpListMilestones, ref)
	if err != nil {
		return nil, err
	}
	defer unlock()
	var out []sourcecontrol.Milestone
	for _, m := range st.milestones {
		if state == "" || state == "all" || m.State == state {
			out = append(out, *m)
		}
	}
	return out, nil
}

// members answers the milestone's issues in state ("" = open) carrying every
// label, by number.
func (st *repoState) members(number int, state string, labels []string) []*issue {
	if state == "" {
		state = "open"
	}
	var out []*issue
	for _, n := range slices.Sorted(maps.Keys(st.issues)) {
		is := st.issues[n]
		if is.milestone == number && (state == "all" || is.info.State == state) && hasAll(is.info.Labels, labels) {
			out = append(out, is)
		}
	}
	return out
}

// ListMilestoneIssues lists the milestone's issues the filter selects.
func (f *Fake) ListMilestoneIssues(_ context.Context, ref sourcecontrol.RepoRef, filter sourcecontrol.MilestoneIssuesFilter) ([]sourcecontrol.IssueInfo, error) {
	filter.Labels = slices.Clone(filter.Labels)
	if err := f.begin(Call{Op: OpListMilestoneIssues, Ref: ref, Milestone: filter}); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.state(ref)
	if _, err := st.milestone(filter.Number); err != nil {
		return nil, err
	}
	var out []sourcecontrol.IssueInfo
	for _, is := range st.members(filter.Number, filter.State, filter.Labels) {
		out = append(out, cloneInfo(is.info))
	}
	return out, nil
}

// MilestoneIssueCounts counts the milestone's open issues per label.
func (f *Fake) MilestoneIssueCounts(_ context.Context, ref sourcecontrol.RepoRef, number int) (*sourcecontrol.MilestoneIssueCounts, error) {
	st, unlock, err := f.issueOp(OpMilestoneIssueCounts, ref)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err := st.milestone(number); err != nil {
		return nil, err
	}
	count := func(label string) int { return len(st.members(number, "open", []string{label})) }
	return &sourcecontrol.MilestoneIssueCounts{
		OpenProvision:         count(labelProvision),
		OpenTotal:             len(st.members(number, "open", nil)),
		OpenAgentWork:         count(labelAgentWork),
		OpenDevelopment:       count(labelDevelopment),
		OpenValidation:        count(labelValidation),
		OpenValidationRepairs: count(labelSrcValidation),
	}, nil
}

// ListMilestoneIssueComments answers the newest perIssue comments of every
// issue in the milestone, by issue number; an issue without comments (or
// perIssue 0) is absent.
func (f *Fake) ListMilestoneIssueComments(_ context.Context, ref sourcecontrol.RepoRef, number, perIssue int) (map[int][]sourcecontrol.IssueComment, error) {
	st, unlock, err := f.issueOp(OpListMilestoneIssueComments, ref)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err := st.milestone(number); err != nil {
		return nil, err
	}
	out := map[int][]sourcecontrol.IssueComment{}
	for _, is := range st.members(number, "all", nil) {
		if c := newest(is.comments, perIssue); c != nil {
			out[is.info.Number] = c
		}
	}
	return out, nil
}
