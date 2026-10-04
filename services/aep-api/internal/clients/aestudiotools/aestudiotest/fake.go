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

// Package aestudiotest is an in-memory stand-in for an org's ae-studio-tools,
// behind the same ports as aestudiotools.Adapter: sourcecontrol.Git, the
// GitHub sub-ports (repos, issues, milestones, pull requests, hooks), the
// narrow adapter ports and the turn port. Commit and blob shas are git's own
// shape, commits honour baseSha, and FailOrg / FailOp inject the adapter's
// failures. Test support only.
package aestudiotest

import (
	"context"
	"sync"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// Operation names: one per adapter method. FailOp takes them and Calls
// reports them.
const (
	// Git.
	OpHead       = "head"
	OpList       = "list"
	OpReadFile   = "read-file"
	OpReadBundle = "read-bundle"
	OpListTags   = "tags"
	OpTag        = "tag"
	OpCommit     = "commit"

	// Repository lifecycle and the narrow adapter ports.
	OpCreateRepo     = "create-repo"
	OpTrashRepo      = "trash-repo"
	OpMirrorSkills   = "mirror-skills"
	OpPutReferences  = "put-references"
	OpGitHubIdentity = "github-identity"
	OpStartTurn      = "start-turn"

	// Issues and pull requests.
	OpCreateIssue          = "create-issue"
	OpListIssues           = "list-issues"
	OpGetIssue             = "get-issue"
	OpListIssueComments    = "list-issue-comments"
	OpEnsureLabel          = "ensure-label"
	OpCloseIssue           = "close-issue"
	OpReopenIssue          = "reopen-issue"
	OpCommentIssue         = "comment-issue"
	OpEditIssueBody        = "edit-issue-body"
	OpEditIssueTitle       = "edit-issue-title"
	OpAddIssueLabels       = "add-issue-labels"
	OpRemoveIssueLabel     = "remove-issue-label"
	OpSetIssueLabels       = "set-issue-labels"
	OpSetIssueMilestone    = "set-issue-milestone"
	OpGetPullRequest       = "get-pull-request"
	OpMergePullRequest     = "merge-pull-request"
	OpListPullRequestFiles = "list-pull-request-files"

	// Milestones.
	OpCreateMilestone            = "create-milestone"
	OpCloseMilestone             = "close-milestone"
	OpReopenMilestone            = "reopen-milestone"
	OpListMilestones             = "list-milestones"
	OpListMilestoneIssues        = "list-milestone-issues"
	OpMilestoneIssueCounts       = "milestone-issue-counts"
	OpListMilestoneIssueComments = "list-milestone-issue-comments"

	// Hooks.
	OpRegisterWebhook     = "register-webhook"
	OpUpdateWebhookEvents = "update-webhook-events"
	OpDeleteWebhook       = "delete-webhook"
)

// Call is one port call the Fake saw, failed ones included. Ref is the ref
// exactly as passed, DefaultBranch included (the Fake keys state without it,
// so assert it here). At, Local and Filter are set on the Git reads that take
// them; Author and Committer on Commit (Committer defaulted to Author, as the
// pod does); Tagger on Tag; Skills and Pinned on MirrorSkills; Repo on CreateOrgRepo;
// Milestone on ListMilestoneIssues; Ref.Org alone on the org-wide ops
// (GitHubIdentity).
type Call struct {
	Op        string
	Ref       sourcecontrol.RepoRef
	At        string
	Local     bool
	Filter    sourcecontrol.BundleFilter
	Author    *sourcecontrol.GitIdentity
	Committer *sourcecontrol.GitIdentity
	Tagger    *sourcecontrol.GitIdentity
	Skills    sourcecontrol.RepoRef
	Pinned    []string
	Repo      sourcecontrol.CreateOrgRepoRequest
	Milestone sourcecontrol.MilestoneIssuesFilter
}

// TurnCall is one StartTurn the fake saw.
type TurnCall struct {
	Ref     aestudiotools.RepoRef
	Request aestudiotools.TurnRequest
}

// Fake is the in-memory pod. The zero value is not usable; call New.
type Fake struct {
	mu           sync.Mutex
	failOp       map[string]error
	failOrg      map[string]error
	calls        []Call
	beforeCommit func()
	beforeTag    func(sourcecontrol.TagSpec)
	repos        map[repoKey]*repoState
	references   map[repoKey][]string
	identities   map[string]*sourcecontrol.GitHubUser
	turns        []TurnCall
	script       []aestudiotools.TurnEvent
	clock        time.Time
	nextHookID   int64
}

var (
	_ sourcecontrol.Git             = (*Fake)(nil)
	_ sourcecontrol.TrashOps        = (*Fake)(nil)
	_ sourcecontrol.SkillsMirrorOps = (*Fake)(nil)
	_ sourcecontrol.ReferencesOps   = (*Fake)(nil)
	_ sourcecontrol.IdentityOps     = (*Fake)(nil)
	_ aestudiotools.Turns           = (*Fake)(nil)
)

// New is an empty pod: no repositories, turns complete at once, no
// references, no identity.
func New() *Fake {
	return &Fake{
		failOp:     map[string]error{},
		failOrg:    map[string]error{},
		repos:      map[repoKey]*repoState{},
		references: map[repoKey][]string{},
		identities: map[string]*sourcecontrol.GitHubUser{},
		clock:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// FailOp makes every later call of op return err; a nil err clears it.
func (f *Fake) FailOp(op string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	setOrClear(f.failOp, op, err)
}

// FailOrg makes every later call for org return err (before any FailOp); a
// nil err clears it. sourcecontrol.ErrAEStudioAbsent, ErrAEStudioUnavailable
// and ErrAEStudioMisconfigured are what the adapter answers for an org.
func (f *Fake) FailOrg(org string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	setOrClear(f.failOrg, org, err)
}

// BeforeCommit runs fn at the start of every later Commit that is not
// failed by FailOrg / FailOp, outside the Fake's lock: fn may read and commit
// itself (a concurrent writer racing the caller). A nil fn clears it.
func (f *Fake) BeforeCommit(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beforeCommit = fn
}

// BeforeTag runs fn with the requested tag at the start of every later Tag
// that is not failed by FailOrg / FailOp, outside the Fake's lock: fn may tag
// itself (a pusher claiming the name first). A nil fn clears it.
func (f *Fake) BeforeTag(fn func(sourcecontrol.TagSpec)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beforeTag = fn
}

// Calls lists every port call so far, in order.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Call, len(f.calls))
	copy(out, f.calls)
	return out
}

// SetIdentity sets the org's GitHub identity; GitHubIdentity answers a copy.
func (f *Fake) SetIdentity(org string, u *sourcecontrol.GitHubUser) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u == nil {
		delete(f.identities, org)
		return
	}
	cp := *u
	f.identities[org] = &cp
}

// begin records a call and answers the injected failure for it, if any. The
// caller holds no lock.
func (f *Fake) begin(c Call) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	if err := f.failOrg[c.Ref.Org]; err != nil {
		return err
	}
	return f.failOp[c.Op]
}

// tick is the Fake's clock: one second per stamped event, so creation order
// is total and deterministic. Caller holds f.mu.
func (f *Fake) tick() time.Time {
	f.clock = f.clock.Add(time.Second)
	return f.clock
}

func setOrClear(m map[string]error, key string, err error) {
	if err == nil {
		delete(m, key)
		return
	}
	m[key] = err
}

// GitHubIdentity answers the identity SetIdentity gave the org, else a 502
// github_error (the pod's answer when GitHub refuses the gitpat).
func (f *Fake) GitHubIdentity(_ context.Context, org string) (*sourcecontrol.GitHubUser, error) {
	if err := f.begin(Call{Op: OpGitHubIdentity, Ref: sourcecontrol.RepoRef{Org: org}}); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.identities[org]
	if !ok {
		return nil, &sourcecontrol.HTTPStatusError{StatusCode: 502, Body: "github_error"}
	}
	cp := *u
	return &cp, nil
}
