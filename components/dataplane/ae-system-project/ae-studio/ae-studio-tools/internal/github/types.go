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

import "time"

// The request and answer types of the client's methods, copied from aep-api's
// sourcecontrol wire DTOs with only the fields GitHub reads or answers;
// aep-api's own context (dedupe keys, handoff fields, attention reasons) stays
// there.

// CreateOrgRepoRequest is what POST /orgs/{owner}/repos (or /user/repos) is
// sent.
type CreateOrgRepoRequest struct {
	Name        string
	Private     bool
	AutoInit    bool
	Description string
}

// CreateIssueRequest is marshalled as POST /repos/{owner}/{repo}/issues'
// body. Milestone is the milestone NUMBER (GitHub answers 422 to a title);
// nil leaves the issue unassigned.
type CreateIssueRequest struct {
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Labels    []string `json:"labels,omitempty"`
	Milestone *int     `json:"milestone,omitempty"`
}

// IssueResult is a created issue.
type IssueResult struct {
	Number int
	URL    string
	NodeID string
}

// IssueInfo is an issue as the list and get reads answer it. ClosedAt is
// GitHub's closed_at, empty while open.
type IssueInfo struct {
	Number      int
	Title       string
	Body        string
	URL         string
	State       string
	StateReason string
	ClosedAt    string
	Labels      []string
}

// PullRequestState is a pull request's open/closed state, whether it merged,
// and the merge commit.
type PullRequestState struct {
	State          string // "open" | "closed"
	Merged         bool
	MergeCommitSHA string
}

// Milestones: one spec version is one milestone. Number is the only stable
// key: create-uniqueness is case-SENSITIVE while the issues-list title filter
// is case-INSENSITIVE, so callers resolve by number and never by title.

// CreateMilestoneRequest is marshalled as POST
// /repos/{owner}/{repo}/milestones' body.
type CreateMilestoneRequest struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// MilestoneResult is a milestone create's outcome. Created is false when one
// with that title (case-insensitively) already existed and Number is it.
type MilestoneResult struct {
	Number  int
	Created bool
}

// Milestone is the subset of a GitHub milestone the platform reads. State is
// display only.
type Milestone struct {
	Number      int
	Title       string
	State       string
	Description string
	NodeID      string
}

// MilestoneIssuesFilter narrows a milestone's issue list. State is "open" |
// "closed" | "all" (empty: GitHub's default, "open"). Labels is AND
// semantics on this REST read; the GraphQL counts filter is a UNION.
type MilestoneIssuesFilter struct {
	Number int
	State  string
	Labels []string
}

// MilestoneIssueCounts is a milestone's OPEN-issue populations in one round
// trip, each the count of ONE label (GraphQL's labels: argument is a union,
// so a multi-label count would be wider than its name). The working-set
// arithmetic over them is aep-api's, not the client's.
type MilestoneIssueCounts struct {
	OpenProvision         int // label "provision" (gates; carry no "aep")
	OpenTotal             int // every open issue
	OpenAgentWork         int // label "aep"
	OpenDevelopment       int // label "development"
	OpenValidation        int // label "validation"
	OpenValidationRepairs int // label "src/validation"
}

// MachineCommentMarker brands a comment the PLATFORM wrote for the agent;
// ObservedCommentMarker one it wrote for a person from what it saw a run do.
// Both are HTML comments (invisible on GitHub), stamped first in the body by
// the writer and stripped on read (classifyComment). The values are shared
// with aep-api's writers and the runner; they must not change.
const (
	MachineCommentMarker  = "<!-- aep:machine -->"
	ObservedCommentMarker = "<!-- aep:observed -->"
)

// IssueComment is one issue comment as GitHub holds it, with the platform's
// brand stripped from Body and reported as Machine or Observed (mutually
// exclusive: only the leading marker counts). Author is a login; empty when
// the account is gone.
type IssueComment struct {
	ID        string
	Author    string
	Body      string
	URL       string
	CreatedAt time.Time
	Machine   bool
	Observed  bool
}
