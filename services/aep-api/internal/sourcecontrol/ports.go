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

package sourcecontrol

import (
	"context"
	"io"
)

// IncidentPorts connects authoritative incident filing to delivery and
// recurrence evidence. A nil adopter cannot claim adoption; a nil recurrence
// port uses the service's GitHub-body evidence writer.
type IncidentPorts struct {
	Adopter    IssueAdopter
	Recurrence IncidentRecurrence
}

// IssueAdopter admits an issue to the existing delivery path. It must return an
// error when delivery cannot accept the issue, including degraded boot.
type IssueAdopter interface {
	AdoptIssue(ctx context.Context, orgID, projectID string, issueNumber int) error
}

// IncidentRecurrence records recurrence evidence before an issue is reopened.
// It owns recurrence eligibility and durable counting; errors leave the issue
// closed. Repeating a failed reopen must not append duplicate evidence.
type IncidentRecurrence interface {
	RecordRecurrence(ctx context.Context, orgID, projectID string, issue IssueInfo, req CreateIssueRequest) (count int64, err error)
}

// The GitHub capability ports.
//
// These interfaces are the seam between sourcecontrol's domain services and
// the org's AE Studio pod, which holds the org's GitHub token and makes every
// GitHub call (clients/aestudiotools' Adapter; aestudiotest.Fake in tests).
// The seam is capability-sliced by CONSUMER need so each domain service
// depends only on the verbs it drives.
//
// Every call takes a RepoRef built from the project's git_repositories row
// (RefForRow), never from client input: the org in it picks the pod, and no
// token crosses this boundary. All ports are concurrency-safe.
//
// The sentinels the implementations return (ErrRepoNameConflict,
// ErrIssueNotFound, HTTPStatusError for a GitHub refusal, the ErrAEStudio*
// family, ...) and the wire DTOs (IssueResult, IssueInfo, GitHubUser, ...)
// are part of this contract — see errors.go and wire.go. Repo CONTENT
// (blobs/trees/commits/refs/tags) never goes through these ports: it runs on
// the Workspace engine (workspace.go / internal/platform/gitfs).

// RepoAdmin is the repository-lifecycle surface. Consumed by repoService
// (CreateRepo / EnsureBareRepo). Repo delete/get are DB operations in
// repoService itself; the pod only provisions the remote repo.
type RepoAdmin interface {
	// CreateOrgRepo creates ref.Owner/ref.Repo (req.Name is not read) and
	// answers its clone URL. A taken name is ErrRepoNameConflict unless
	// req.AdoptExisting, which answers the existing repository instead.
	CreateOrgRepo(ctx context.Context, ref RepoRef, req CreateOrgRepoRequest) (cloneURL string, err error)
}

// IssueOps is the issue surface (create / list / close / comment / labels) plus
// the pull-request and milestone ops that are issue-shaped on the host side —
// GitHub serves pull requests and milestone membership through the issues API.
// Consumed by issueService.
type IssueOps interface {
	CreateIssue(ctx context.Context, ref RepoRef, req CreateIssueRequest) (*IssueResult, error)
	// ListIssues returns issues only, never pull requests, newest first. The
	// adapter bounds its page walk, so a very large repository answers its
	// newest issues and GetIssue reaches the rest.
	ListIssues(ctx context.Context, ref RepoRef, labels []string) ([]IssueInfo, error)
	// GetIssue fetches a single issue by number via GET /issues/{number} — an
	// O(1) lookup, unlike ListIssues which pages the whole repo. Returns
	// ErrIssueNotFound when the host answers 404.
	GetIssue(ctx context.Context, ref RepoRef, number int) (*IssueInfo, error)
	// ListIssueComments returns the newest `limit` comments of ONE issue, OLDEST
	// FIRST — the detail read's narrative, where the milestone sibling on
	// MilestoneOps serves the list. An issue with no comments answers nil, the
	// same thing that read's absent bucket means. Returns ErrIssueNotFound when
	// the host does not hold the issue.
	ListIssueComments(ctx context.Context, ref RepoRef, number, limit int) ([]IssueComment, error)
	// EnsureLabel creates a label in the repository if it does not already exist.
	// It is idempotent — a 422 Unprocessable Entity response (already exists) is treated as success.
	EnsureLabel(ctx context.Context, ref RepoRef, name, color string) error
	// CloseIssue sets the issue state to closed with reason "completed".
	CloseIssue(ctx context.Context, ref RepoRef, number int) error
	// ReopenIssue sets the issue state back to open. Used by the validation task,
	// which the PLATFORM closes at the end of every attempt and which a repeated
	// validation must find again rather than re-file.
	ReopenIssue(ctx context.Context, ref RepoRef, number int) error
	// CommentIssue posts a comment on the issue.
	CommentIssue(ctx context.Context, ref RepoRef, number int, body string) error
	// EditIssueBody replaces the issue body via PATCH /issues/{number}.
	// Used by the tech-lead detail phase to write the LLM-authored body
	// after the placeholder issue was created.
	EditIssueBody(ctx context.Context, ref RepoRef, number int, body string) error
	// EditIssueTitle replaces the issue title via PATCH /issues/{number}.
	// Used by the plan tap when a planned Task is renamed (updateTask).
	EditIssueTitle(ctx context.Context, ref RepoRef, number int, title string) error
	// AddIssueLabels adds labels to an existing issue (merges with current;
	// adding a present label is a no-op). Used to stamp aep:status/* projection
	// and aep:attention flags.
	AddIssueLabels(ctx context.Context, ref RepoRef, number int, labels []string) error
	// RemoveIssueLabel removes one label from an issue. A 404 (already absent)
	// is treated as success.
	RemoveIssueLabel(ctx context.Context, ref RepoRef, number int, label string) error
	// SetIssueLabels replaces the issue's entire label set (labels absent from
	// the slice are removed). Used by block-repair projection when the full set
	// must be authoritative.
	SetIssueLabels(ctx context.Context, ref RepoRef, number int, labels []string) error
	// SetIssueMilestone assigns an existing issue to a milestone by NUMBER
	// (PATCH /issues/{number}). Adoption's write: a bare issue handed to the
	// coding agent joins the deployed version's milestone.
	SetIssueMilestone(ctx context.Context, ref RepoRef, number, milestoneNumber int) error
	// GetPullRequest returns a pull request's live state (open/closed + merged +
	// merge SHA) for the sweep's PR-state reconciliation (§5).
	GetPullRequest(ctx context.Context, ref RepoRef, number int) (*PullRequestState, error)
	// MergePullRequest squash-merges an open pull request — the devflow task
	// workflow's auto merge-pr gate. GitHub's 405 not-mergeable answer (checks
	// pending, conflicts, already merged) is returned as an error for the
	// caller to reconcile against GetPullRequest.
	MergePullRequest(ctx context.Context, ref RepoRef, number int) error
	// ListPullRequestFiles returns the paths of every file changed by a pull
	// request. The path-based build trigger maps these onto the components whose
	// source they touched so a merged PR rebuilds every affected component.
	ListPullRequestFiles(ctx context.Context, ref RepoRef, number int) ([]string, error)

	// CreateMilestone creates a milestone and returns its number, minting it or
	// adopting an existing one with that title. Implementations MUST be
	// idempotent and MUST enforce case-insensitive title uniqueness: the host's
	// own uniqueness check is case-sensitive while its title filters are not,
	// so a case-twin pair would silently merge on every subsequent read.
	CreateMilestone(ctx context.Context, ref RepoRef, req CreateMilestoneRequest) (*MilestoneResult, error)
	// CloseMilestone closes a milestone. Display only — member issues are
	// untouched, and a closed milestone still accepts new ones.
	CloseMilestone(ctx context.Context, ref RepoRef, number int) error
	// ReopenMilestone reopens a closed milestone — the inverse of the above and
	// display only in the same way. A rebuild of an unchanged spec works the SAME
	// milestone a cancel closed, and a version being worked whose milestone reads
	// closed is a lie the console renders.
	ReopenMilestone(ctx context.Context, ref RepoRef, number int) error
	// ListMilestones returns every milestone in the given state
	// ("open" | "closed" | "all"; empty ⇒ "all"). The list must be complete,
	// not a first page — CreateMilestone's uniqueness pre-check reads it.
	ListMilestones(ctx context.Context, ref RepoRef, state string) ([]Milestone, error)
	// ListMilestoneIssues returns a milestone's issues, filtered by state and
	// label. Addressed by milestone NUMBER. Pull requests are excluded.
	ListMilestoneIssues(ctx context.Context, ref RepoRef, filter MilestoneIssuesFilter) ([]IssueInfo, error)
	// MilestoneIssueCounts returns a milestone's open-issue populations — gates,
	// working set and total — in ONE call, the run supervisor's dispatch
	// predicate input. Returns ErrMilestoneNotFound when no milestone carries
	// that number.
	MilestoneIssueCounts(ctx context.Context, ref RepoRef, number int) (*MilestoneIssueCounts, error)
	// ListMilestoneIssueComments returns the newest perIssue comments of every
	// issue in one milestone, bucketed by issue number and OLDEST FIRST within
	// each bucket, in ONE round trip. An issue with no comments is absent from
	// the map rather than present with an empty slice.
	//
	// One call is the whole point: this rides a 5s console poll, and a call per
	// issue would spend a milestone's worth of rate limit on every tick.
	// Returns ErrMilestoneNotFound when no milestone carries that number.
	ListMilestoneIssueComments(ctx context.Context, ref RepoRef, number, perIssue int) (map[int][]IssueComment, error)
}

// WebhookOps is the repo-webhook surface. Consumed by webhookService.
type WebhookOps interface {
	// RegisterWebhook installs the repository webhook delivering to the org's
	// pod (the pod owns the delivery URL and the signing secret) and returns
	// its hook ID; an existing hook to that URL is answered as is.
	RegisterWebhook(ctx context.Context, ref RepoRef, events []string) (hookID int64, err error)
	// UpdateWebhookEvents replaces the subscribed-event list of an existing repo
	// webhook (PATCH /hooks/{id}). RegisterWebhook's already-exists path returns
	// a pre-existing hook without touching its events, so a hook created before
	// "issues" joined the subscription must be PATCHed to add it
	// (docs/design/tasks-github-native.md §9.2 cutover).
	UpdateWebhookEvents(ctx context.Context, ref RepoRef, hookID int64, events []string) error
	// DeleteWebhook removes the hook the platform registered, addressed by the
	// stored hook ID so no other integration's delivery can be caught by it. A
	// hook that is already gone (404, or 410 after GitHub reaped a failing one)
	// is success — the desired post-state is absence.
	DeleteWebhook(ctx context.Context, ref RepoRef, hookID int64) error
}

// The narrow AE Studio adapter ports beside the ones above: each is one pod
// operation a single consumer drives. clients/aestudiotools' Adapter and
// aestudiotest.Fake serve them, next to Git (git.go).

// TrashOps drops a deleted project's repository mirror (and its reference
// documents) from the org's pod.
type TrashOps interface {
	TrashRepo(ctx context.Context, ref RepoRef) error
}

// SkillsMirrorOps mirrors the org skills library's pinned skills into a
// project's repository in one commit.
type SkillsMirrorOps interface {
	MirrorSkills(ctx context.Context, project, skills RepoRef, pinned []string) (CommitResult, error)
}

// ReferencesOps replaces a project's stored reference documents with a
// multipart upload (field `files`) typed by contentType. It owns body: an
// io.Closer is closed on return.
type ReferencesOps interface {
	PutReferences(ctx context.Context, ref RepoRef, contentType string, body io.Reader) error
}

// IdentityOps reads the GitHub user the org's gitpat belongs to.
type IdentityOps interface {
	GitHubIdentity(ctx context.Context, org string) (*GitHubUser, error)
}
