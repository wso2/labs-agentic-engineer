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

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

func TestIssueOps_RequestAndReply(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	milestone := 3
	const issueJSON = `{"number":7,"title":"t","body":"b","url":"u","state":"closed","stateReason":"completed","closedAt":"2026-01-02T03:04:05Z","labels":["a"]}`
	issue := sourcecontrol.IssueInfo{Number: 7, Title: "t", Body: "b", URL: "u", State: "closed", StateReason: "completed", ClosedAt: "2026-01-02T03:04:05Z", Labels: []string{"a"}}
	const commentJSON = `{"id":"c1","author":"ann","body":"hi","url":"cu","createdAt":"2026-01-02T03:04:05Z","machine":true,"observed":false}`
	comment := sourcecontrol.IssueComment{ID: "c1", Author: "ann", Body: "hi", URL: "cu", CreatedAt: at, Machine: true}
	none := func(err error) (any, error) { return nil, err }
	runOps(t, []opCase{
		{
			name: "create issue", method: "POST", path: "/repos/acme/greeter/issues", status: 201,
			body:  `{"title":"t","body":"b","labels":["x"],"milestone":3}`,
			reply: `{"number":5,"url":"u","nodeId":"N"}`, want: &sourcecontrol.IssueResult{Number: 5, URL: "u", NodeID: "N"},
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return a.CreateIssue(ctx, trunkRef, sourcecontrol.CreateIssueRequest{Title: "t", Body: "b", Labels: []string{"x"}, Milestone: &milestone, DedupeKey: "aep-only"})
			},
		},
		{
			name: "list issues", method: "GET", path: "/repos/acme/greeter/issues", query: "labels=a&labels=b",
			reply: `{"issues":[` + issueJSON + `]}`, want: []sourcecontrol.IssueInfo{issue},
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return a.ListIssues(ctx, trunkRef, []string{"a", "b"})
			},
		},
		{
			name: "get issue", method: "GET", path: "/repos/acme/greeter/issues/7",
			reply: issueJSON, want: &issue,
			call: func(ctx context.Context, a *Adapter) (any, error) { return a.GetIssue(ctx, trunkRef, 7) },
		},
		{
			name: "list issue comments always sends limit", method: "GET", path: "/repos/acme/greeter/issues/7/comments", query: "limit=0",
			reply: `{"comments":[]}`, want: []sourcecontrol.IssueComment(nil),
			call: func(ctx context.Context, a *Adapter) (any, error) { return a.ListIssueComments(ctx, trunkRef, 7, 0) },
		},
		{
			name: "list issue comments", method: "GET", path: "/repos/acme/greeter/issues/7/comments", query: "limit=20",
			reply: `{"comments":[` + commentJSON + `]}`, want: []sourcecontrol.IssueComment{comment},
			call: func(ctx context.Context, a *Adapter) (any, error) { return a.ListIssueComments(ctx, trunkRef, 7, 20) },
		},
		{
			name: "ensure label", method: "POST", path: "/repos/acme/greeter/labels", status: 204,
			body: `{"name":"aep:status/x","color":"ededed"}`,
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return none(a.EnsureLabel(ctx, trunkRef, "aep:status/x", "ededed"))
			},
		},
		{
			name: "close issue", method: "POST", path: "/repos/acme/greeter/issues/7/close", status: 204,
			call: func(ctx context.Context, a *Adapter) (any, error) { return none(a.CloseIssue(ctx, trunkRef, 7)) },
		},
		{
			name: "reopen issue", method: "POST", path: "/repos/acme/greeter/issues/7/reopen", status: 204,
			call: func(ctx context.Context, a *Adapter) (any, error) { return none(a.ReopenIssue(ctx, trunkRef, 7)) },
		},
		{
			name: "comment issue", method: "POST", path: "/repos/acme/greeter/issues/7/comments", status: 204, body: `{"body":"hi"}`,
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return none(a.CommentIssue(ctx, trunkRef, 7, "hi"))
			},
		},
		{
			name: "edit issue body", method: "PUT", path: "/repos/acme/greeter/issues/7/body", status: 204, body: `{"body":"new"}`,
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return none(a.EditIssueBody(ctx, trunkRef, 7, "new"))
			},
		},
		{
			name: "edit issue title", method: "PUT", path: "/repos/acme/greeter/issues/7/title", status: 204, body: `{"title":"T"}`,
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return none(a.EditIssueTitle(ctx, trunkRef, 7, "T"))
			},
		},
		{
			name: "add labels", method: "POST", path: "/repos/acme/greeter/issues/7/labels", status: 204, body: `{"labels":["a","b"]}`,
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return none(a.AddIssueLabels(ctx, trunkRef, 7, []string{"a", "b"}))
			},
		},
		{
			name: "set labels to none", method: "PUT", path: "/repos/acme/greeter/issues/7/labels", status: 204, body: `{"labels":[]}`,
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return none(a.SetIssueLabels(ctx, trunkRef, 7, nil))
			},
		},
		{
			name: "remove a label with a slash", method: "DELETE", path: "/repos/acme/greeter/issues/7/labels/src%2Fvalidation", status: 204,
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return none(a.RemoveIssueLabel(ctx, trunkRef, 7, "src/validation"))
			},
		},
		{
			name: "set milestone", method: "PUT", path: "/repos/acme/greeter/issues/7/milestone", status: 204, body: `{"number":3}`,
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return none(a.SetIssueMilestone(ctx, trunkRef, 7, 3))
			},
		},
		{
			name: "get pull", method: "GET", path: "/repos/acme/greeter/pulls/9",
			reply: `{"state":"closed","merged":true,"mergeCommitSha":"` + sha40 + `"}`,
			want:  &sourcecontrol.PullRequestState{State: "closed", Merged: true, MergeCommitSHA: sha40},
			call:  func(ctx context.Context, a *Adapter) (any, error) { return a.GetPullRequest(ctx, trunkRef, 9) },
		},
		{
			name: "merge pull", method: "POST", path: "/repos/acme/greeter/pulls/9/merge", status: 204,
			call: func(ctx context.Context, a *Adapter) (any, error) { return none(a.MergePullRequest(ctx, trunkRef, 9)) },
		},
		{
			name: "list pull files", method: "GET", path: "/repos/acme/greeter/pulls/9/files",
			reply: `{"files":["a","b"]}`, want: []string{"a", "b"},
			call: func(ctx context.Context, a *Adapter) (any, error) { return a.ListPullRequestFiles(ctx, trunkRef, 9) },
		},
		{
			name: "create milestone", method: "POST", path: "/repos/acme/greeter/milestones", status: 201,
			body: `{"title":"v1","description":"d"}`, reply: `{"number":2,"created":true}`, want: &sourcecontrol.MilestoneResult{Number: 2, Created: true},
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return a.CreateMilestone(ctx, trunkRef, sourcecontrol.CreateMilestoneRequest{Title: "v1", Description: "d"})
			},
		},
		{
			name: "close milestone", method: "POST", path: "/repos/acme/greeter/milestones/2/close", status: 204,
			call: func(ctx context.Context, a *Adapter) (any, error) { return none(a.CloseMilestone(ctx, trunkRef, 2)) },
		},
		{
			name: "reopen milestone", method: "POST", path: "/repos/acme/greeter/milestones/2/reopen", status: 204,
			call: func(ctx context.Context, a *Adapter) (any, error) { return none(a.ReopenMilestone(ctx, trunkRef, 2)) },
		},
		{
			name: "list milestones", method: "GET", path: "/repos/acme/greeter/milestones", query: "state=open",
			reply: `{"milestones":[{"number":2,"title":"v1","state":"open","description":"d","nodeId":"M"}]}`,
			want:  []sourcecontrol.Milestone{{Number: 2, Title: "v1", State: "open", Description: "d", NodeID: "M"}},
			call:  func(ctx context.Context, a *Adapter) (any, error) { return a.ListMilestones(ctx, trunkRef, "open") },
		},
		{
			name: "list every milestone", method: "GET", path: "/repos/acme/greeter/milestones",
			reply: `{"milestones":[]}`, want: []sourcecontrol.Milestone(nil),
			call: func(ctx context.Context, a *Adapter) (any, error) { return a.ListMilestones(ctx, trunkRef, "") },
		},
		{
			name: "list milestone issues", method: "GET", path: "/repos/acme/greeter/milestones/2/issues", query: "labels=x&state=open",
			reply: `{"issues":[` + issueJSON + `]}`, want: []sourcecontrol.IssueInfo{issue},
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return a.ListMilestoneIssues(ctx, trunkRef, sourcecontrol.MilestoneIssuesFilter{Number: 2, State: "open", Labels: []string{"x"}})
			},
		},
		{
			name: "milestone counts", method: "GET", path: "/repos/acme/greeter/milestones/2/counts",
			reply: `{"openProvision":1,"openTotal":9,"openAgentWork":5,"openDevelopment":2,"openValidation":1,"openValidationRepairs":3}`,
			want:  &sourcecontrol.MilestoneIssueCounts{OpenProvision: 1, OpenTotal: 9, OpenAgentWork: 5, OpenDevelopment: 2, OpenValidation: 1, OpenValidationRepairs: 3},
			call:  func(ctx context.Context, a *Adapter) (any, error) { return a.MilestoneIssueCounts(ctx, trunkRef, 2) },
		},
		{
			name: "milestone comments always sends perIssue", method: "GET", path: "/repos/acme/greeter/milestones/2/comments", query: "perIssue=5",
			reply: `{"issues":[{"number":1,"comments":[` + commentJSON + `]},{"number":4,"comments":[]}]}`,
			want:  map[int][]sourcecontrol.IssueComment{1: {comment}},
			call: func(ctx context.Context, a *Adapter) (any, error) {
				return a.ListMilestoneIssueComments(ctx, trunkRef, 2, 5)
			},
		},
	})
}

// A write on an issue GitHub does not hold answers 502 github_error with
// githubStatus 404 (not issue_not_found): the adapter names it.
func TestIssueOps_MissingIssueOnAWriteIsIssueNotFound(t *testing.T) {
	_, srv := newPodStub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Bad Gateway","status":502,"code":"github_error","githubStatus":404}`))
	})
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"close":     func() error { return a.CloseIssue(ctx, trunkRef, 7) },
		"reopen":    func() error { return a.ReopenIssue(ctx, trunkRef, 7) },
		"comment":   func() error { return a.CommentIssue(ctx, trunkRef, 7, "x") },
		"body":      func() error { return a.EditIssueBody(ctx, trunkRef, 7, "x") },
		"title":     func() error { return a.EditIssueTitle(ctx, trunkRef, 7, "x") },
		"add":       func() error { return a.AddIssueLabels(ctx, trunkRef, 7, []string{"x"}) },
		"set":       func() error { return a.SetIssueLabels(ctx, trunkRef, 7, []string{"x"}) },
		"remove":    func() error { return a.RemoveIssueLabel(ctx, trunkRef, 7, "x") },
		"milestone": func() error { return a.SetIssueMilestone(ctx, trunkRef, 7, 2) },
	} {
		if err := call(); !errors.Is(err, sourcecontrol.ErrIssueNotFound) || !sourcecontrol.IsPermanent(err) {
			t.Errorf("%s: err = %v, want a permanent ErrIssueNotFound", name, err)
		}
	}
	if _, err := a.GetPullRequest(ctx, trunkRef, 9); errors.Is(err, sourcecontrol.ErrIssueNotFound) || !sourcecontrol.IsHTTPStatus(err, 404) {
		t.Errorf("a missing pull request stays an HTTPStatusError 404, got %v", err)
	}
}
