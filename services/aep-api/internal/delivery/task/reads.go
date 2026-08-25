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

package task

import (
	"context"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The task read DTOs (Lineage, ExecutionView, TaskView, TaskDetail) live in the
// delivery ROOT (delivery/read_views.go) so the build sub-package can name
// TaskView through a port without importing this one. This service produces
// them; both it and build import only the root.

// Reads is the live read path: GitHub issues, read now, with no cache and no
// read model.
//
// What it reads is MILESTONE MEMBERSHIP plus LABELS, and nothing else. Issue
// bodies are prose — the platform authors them for the agent and never parses
// them back — so a Task view carries the facts GitHub holds (number, title,
// state, labels, body) joined with the execution rows the platform still owns
// for provisioning gates. Everything the retired machine block used to supply
// (component, dependsOn, lineage, origin, the ten-value derived status) is
// either gone or now a property of the RUN, which is read from the run rows.
type Reads struct {
	issues IssueClient
	repos  RepoResolver
	execs  ExecutionReader
	runs   MilestoneResolver
}

// NewReads wires the read path. runs may be nil — a `?tag=` query then cannot
// be resolved to a milestone and answers empty rather than guessing.
//
// The repo row is still read on every call: it is the tenant fence (a project
// with no repository has no Tasks) and it names the executions rows' repo key.
func NewReads(issues IssueClient, repos RepoResolver, execs ExecutionReader, runs MilestoneResolver) *Reads {
	return &Reads{issues: issues, repos: repos, execs: execs, runs: runs}
}

// ListByTag returns a project's Tasks, filtered by GitHub state ("open" |
// "closed" | "all"; default "open") and optionally scoped to one spec/build
// version tag.
//
// THE TAG IS MILESTONE MEMBERSHIP. It resolves `v<N>` to a milestone NUMBER
// through the platform's own run rows and then lists that milestone — never by
// matching titles against GitHub, whose milestone titles are renamable and
// whose title filters are case-insensitive while its create-uniqueness is not.
// An unknown tag is an empty list, not an error: a version this platform never
// built has no Tasks by definition.
//
// An empty tag returns every version, which costs two label-scoped queries
// because GitHub's `labels=` is AND-semantics and the two populations (armed
// work, dispatch gates) carry disjoint labels — a gate deliberately carries no
// arming label, so no single query can reach both.
//
// The validation task is excluded at this boundary, as it always was: it is a
// phase of the run, not an implementation Task, and it surfaces on the
// deployment surface with the run's verdict. It is excluded by KIND now that it
// carries the arming label like any other agent work.
//
// Bare LEDGER issues — human-filed issues that joined the milestone carrying
// none of the platform's labels — are returned by the tag-scoped read and only
// by it. They are never worked and never stall settle (§7), but they are part
// of the version's ledger and the console sections them apart from agent work.
// The untagged read cannot see them: it is two label queries, and a ledger
// issue is defined by carrying no label to query on. Milestone membership is
// the only handle there is.
//
// COMMENTS follow that same handle. withComments asks for each issue's newest
// comments, and it is honoured only on a tag-scoped read for exactly the reason
// ledger issues are only visible there: the comment read is anchored on one
// milestone, and a query spanning every version has no bounded set of issues to
// ask about. A caller that renders no comments passes false and the read costs
// what it always did.
func (r *Reads) ListByTag(ctx context.Context, orgID, projectID, state, tag string, withComments bool) ([]delivery.TaskView, error) {
	_, owner, name, err := resolveProjectRepo(ctx, r.repos, orgID, projectID)
	if err != nil {
		return nil, err
	}
	repoFullName := owner + "/" + name

	milestoneNumber, err := r.milestoneForTag(ctx, orgID, projectID, tag)
	if err != nil {
		return nil, err
	}

	// The comment read goes FIRST and runs alongside the issue list. The two are
	// independent once the milestone number is in hand, they are both host round
	// trips, and this endpoint is POLLED — taking them in sequence measured at
	// roughly double the latency of taking them together.
	commentsCh := r.issueCommentsAsync(ctx, orgID, projectID, milestoneNumber, withComments)

	issues, specTag, err := r.taskIssues(ctx, orgID, projectID, tag, milestoneNumber)
	if err != nil {
		return nil, err
	}

	// One batch query for the whole repo's latest-per-kind rows (not one per
	// issue); a load failure degrades to empty executions, as before.
	execsByIssue, err := r.execs.LatestPerKindForRepoScoped(ctx, orgID, repoFullName)
	if err != nil {
		slog.WarnContext(ctx, "reads: load executions failed", "repo", repoFullName, "error", err)
		execsByIssue = map[int]map[string]*delivery.Execution{}
	}

	commentsByIssue := <-commentsCh

	out := make([]delivery.TaskView, 0, len(issues))
	for _, issue := range issues {
		if !matchesState(issue.State, state) {
			continue
		}
		view, ok := buildView(issue, specTag, execsByIssue[issue.Number], commentsByIssue[issue.Number], tag != "")
		if !ok {
			continue
		}
		out = append(out, view)
	}
	return out, nil
}

// CommentsPerIssue is how many of an issue's newest comments the HOST IS ASKED
// FOR on a tag-scoped read. It is not how many are returned.
//
// It is the window the machine filter runs INSIDE, and that ordering is forced:
// GraphQL has no predicate for "comments without this marker", so the read takes
// the newest N and commentViews drops the platform's own afterwards. The
// consequence is exact and worth stating rather than discovering — an issue
// whose newest N comments are ALL platform-written answers empty, while the
// narrative sits just outside the window. A larger N is the only lever on that.
//
// So N is generous rather than minimal: the platform posts a few comments per
// issue (a resolved-dependency block, a provisioning note, a closing line), and
// 10 leaves room behind them without doubling what a 5s poll carries.
const CommentsPerIssue = 10

// issueCommentsAsync starts the milestone's comment read and hands back the
// channel its result will arrive on.
//
// The channel is BUFFERED with room for the one value, so the goroutine always
// completes and exits even when the caller abandons the read after an error on
// the issue list — there is nothing to leak.
//
// A read that was not asked for, or that has no milestone to anchor on, answers
// on the same channel without starting anything: the caller then has one shape
// to handle instead of a nil-channel special case.
func (r *Reads) issueCommentsAsync(ctx context.Context, orgID, projectID string, milestoneNumber int, withComments bool) <-chan map[int][]sourcecontrol.IssueComment {
	ch := make(chan map[int][]sourcecontrol.IssueComment, 1)
	if !withComments || milestoneNumber <= 0 {
		ch <- nil
		return ch
	}
	go func() { ch <- r.issueComments(ctx, orgID, projectID, milestoneNumber) }()
	return ch
}

// issueComments reads the milestone's comment buckets, or answers nil.
//
// It DEGRADES rather than fails, like the executions load above and for a
// stronger reason: this list drives the milestone panel and the run card's
// ability to tell a gate hold from an empty working set, while comments are
// decorative. A host that will not answer them must cost the caller its
// comments, never its Tasks.
func (r *Reads) issueComments(ctx context.Context, orgID, projectID string, milestoneNumber int) map[int][]sourcecontrol.IssueComment {
	comments, err := r.issues.ListMilestoneIssueComments(ctx, orgID, projectID, milestoneNumber, CommentsPerIssue)
	if err != nil {
		slog.WarnContext(ctx, "reads: load issue comments failed",
			"project", projectID, "milestone", milestoneNumber, "error", err)
		return nil
	}
	return comments
}

// Get returns one Task with its full Execution history. The issue is fetched by
// number (O(1)); a number that is not a Task of this project is
// ErrTaskNotFound.
func (r *Reads) Get(ctx context.Context, orgID, projectID string, issueNumber int) (*delivery.TaskDetail, error) {
	_, owner, name, err := resolveProjectRepo(ctx, r.repos, orgID, projectID)
	if err != nil {
		return nil, err
	}
	repoFullName := owner + "/" + name

	issue, err := r.issues.GetIssue(ctx, orgID, projectID, issueNumber)
	if err != nil || issue == nil {
		return nil, ErrTaskNotFound
	}

	execs, err := r.execs.LatestPerKindScoped(ctx, orgID, repoFullName, issueNumber)
	if err != nil {
		slog.WarnContext(ctx, "reads: load executions failed", "repo", repoFullName, "error", err)
		execs = map[string]*delivery.Execution{}
	}
	// Get serves the validation issue and a bare ledger issue too: the list hides
	// them, but a detail page reached by number must still answer, so the
	// population filter is deliberately not applied here.
	view := bareView(*issue, "")
	view.Executions = latestViews(execs)

	history, err := r.execs.ListByIssueScoped(ctx, orgID, repoFullName, issueNumber)
	if err != nil {
		return nil, err
	}
	hv := make([]delivery.ExecutionView, 0, len(history))
	for i := range history {
		hv = append(hv, executionView(&history[i]))
	}
	return &delivery.TaskDetail{TaskView: view, ExecutionHistory: hv}, nil
}

// milestoneForTag resolves a `?tag=` to the milestone NUMBER it names, or 0.
//
// Zero is the answer to three different questions — no tag was given, no run
// rows can resolve one, and this platform never built that version — and the
// callers treat all three alike: no milestone means no milestone-scoped read.
// Only a resolver ERROR propagates, because that one says nothing about whether
// the version exists.
//
// It is split out from taskIssues so the milestone is known BEFORE either host
// read starts, which is what lets the two run at the same time.
func (r *Reads) milestoneForTag(ctx context.Context, orgID, projectID, tag string) (int, error) {
	if tag == "" || r.runs == nil {
		return 0, nil
	}
	number, found, err := r.runs.MilestoneNumberForTag(ctx, orgID, projectID, tag)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, nil // a version this platform never built has no Tasks
	}
	return number, nil
}

// taskIssues resolves the requested population and the version tag every
// returned issue belongs to (empty when the query spans versions).
//
// milestoneNumber is the already-resolved answer from milestoneForTag rather
// than something re-derived here: resolving it twice would be a second database
// read and a second chance for the two answers to disagree about which version
// this read is describing.
func (r *Reads) taskIssues(ctx context.Context, orgID, projectID, tag string, milestoneNumber int) ([]sourcecontrol.IssueInfo, string, error) {
	if tag == "" {
		issues, err := r.allVersionIssues(ctx, orgID, projectID)
		return issues, "", err
	}
	if milestoneNumber <= 0 {
		return nil, tag, nil
	}
	issues, err := r.issues.ListMilestoneIssues(ctx, orgID, projectID, sourcecontrol.MilestoneIssuesFilter{
		Number: milestoneNumber,
		State:  "all", // state filtering is matchesState's job, so "closed" works too
	})
	return issues, tag, err
}

// allVersionIssues is the untagged query: armed work plus dispatch gates across
// every milestone. Two calls, because the two populations carry disjoint labels
// and GitHub's label filter is AND-semantics — one query naming both would
// demand an issue carrying both and return nothing.
//
// The armed query reaches every workable kind at once (planned work, bugs,
// conflicts, the validation task) because the arming label is what they share;
// the kinds are then told apart per issue, in taskKind.
func (r *Reads) allVersionIssues(ctx context.Context, orgID, projectID string) ([]sourcecontrol.IssueInfo, error) {
	work, err := r.issues.ListIssues(ctx, orgID, projectID, []string{delivery.LabelAgentWork})
	if err != nil {
		return nil, err
	}
	gates, err := r.issues.ListIssues(ctx, orgID, projectID, []string{delivery.KindProvision})
	if err != nil {
		return nil, err
	}
	seen := make(map[int]bool, len(work))
	out := make([]sourcecontrol.IssueInfo, 0, len(work)+len(gates))
	for _, group := range [][]sourcecontrol.IssueInfo{work, gates} {
		for _, issue := range group {
			if seen[issue.Number] {
				continue
			}
			seen[issue.Number] = true
			out = append(out, issue)
		}
	}
	return out, nil
}

// buildView projects one live issue onto a TaskView. ok is false when the issue
// is not part of the requested population: the validation task is always hidden,
// and a bare ledger issue is included only when the caller scoped the read to
// one milestone, which is the only way a ledger issue is discoverable at all.
//
// The validation task is hidden on its KIND, not on the absence of an arming
// label — it carries one, like every other issue an agent is dispatched at.
func buildView(issue sourcecontrol.IssueInfo, specTag string, execs map[string]*delivery.Execution,
	comments []sourcecontrol.IssueComment, milestoneScoped bool) (delivery.TaskView, bool) {
	if delivery.IsValidationWork(issue.Labels) {
		return delivery.TaskView{}, false
	}
	if !delivery.HasLabel(issue.Labels, delivery.LabelAgentWork) &&
		!delivery.IsDispatchGate(issue.Labels) &&
		!milestoneScoped {
		return delivery.TaskView{}, false
	}
	view := bareView(issue, specTag)
	// A dispatch gate's provisioning run still keeps an execution row; agent work
	// has none — its pull request lives on the run's cycle record instead.
	view.Executions = latestViews(execs)
	view.Comments = commentViews(comments)
	return view, true
}

// commentViews projects the host's comments onto the read DTO, preserving order
// and DROPPING the platform's own.
//
// A machine comment is the platform talking to the agent — a resolved dependency
// block, a provisioning note, a closing line. It is written for a reader that is
// not a person, it is often long, and on an issue that has one it would crowd
// out the narrative this field exists to carry. The host brands them on write
// and reports the brand on read (sourcecontrol.MachineCommentMarker); what to do
// about it is this surface's policy, and this surface shows only what a person
// wrote or an agent said.
//
// nil out covers four cases: comments were not asked for, the host could not
// answer, the issue has none, and every one it has is the platform's. That is
// deliberate — a consumer cannot act differently on any of them, and inventing
// an empty slice for one would put a distinction on the wire nothing can rely
// on.
func commentViews(comments []sourcecontrol.IssueComment) []delivery.IssueComment {
	if len(comments) == 0 {
		return nil
	}
	out := make([]delivery.IssueComment, 0, len(comments))
	for _, c := range comments {
		if c.Machine {
			continue
		}
		out = append(out, delivery.IssueComment{
			ID:        c.ID,
			Author:    c.Author,
			Body:      c.Body,
			URL:       c.URL,
			CreatedAt: c.CreatedAt,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// bareView is the projection every Task shares: what GitHub holds about the
// issue, and nothing inferred.
func bareView(issue sourcecontrol.IssueInfo, specTag string) delivery.TaskView {
	return delivery.TaskView{
		IssueNumber:   issue.Number,
		Title:         issue.Title,
		IssueURL:      issue.URL,
		ExecutorClass: executorClass(issue.Labels),
		Kind:          delivery.KindOf(issue.Labels),
		Body:          issue.Body,
		DependsOn:     []string{},
		Lineage:       delivery.Lineage{SpecTag: specTag},
		DerivedStatus: derivedStatus(issue.State),
		// attention is contractually "[] when clean" — the console maps over it
		// directly, and a nil slice would marshal as JSON null.
		Attention:  []string{},
		Executions: map[string]delivery.ExecutionView{},
	}
}

// executorClass says WHO works an issue, which is the axis the console sections
// its issue list on: the platform (a dispatch gate), the validation loop, a
// coding agent, or nobody at all (a bare human issue — the ledger).
//
// It is deliberately COARSER than the issue's kind. Planned work, a bug and a
// merge conflict are all `coding`: they are dispatched the same way, into the
// same cycle, and a console section per kind would split one population three
// ways for no reader's benefit. The finer fact travels beside it as Kind, for
// the row's own chip.
//
// An issue with no kind but an arming label is `coding` — the same reading every
// working-set predicate gives it (delivery.InDevWorkingSet), so a row cannot
// claim to be un-worked while the loop is working it.
func executorClass(labels []string) string {
	switch {
	case delivery.IsDispatchGate(labels):
		return "provision"
	case delivery.IsValidationWork(labels):
		return "validation"
	case delivery.HasLabel(labels, delivery.LabelAgentWork):
		return "coding"
	default:
		return "ledger"
	}
}

// derivedStatus is the whole derived-status algebra now: the issue is open, or
// it is closed. See delivery.DerivedStatusPending for why the vocabulary is a
// subset of the retired ten rather than two new strings.
func derivedStatus(issueState string) string {
	if strings.EqualFold(issueState, "open") {
		return delivery.DerivedStatusPending
	}
	return delivery.DerivedStatusMerged
}

func latestViews(execs map[string]*delivery.Execution) map[string]delivery.ExecutionView {
	out := make(map[string]delivery.ExecutionView, len(execs))
	for kind, e := range execs {
		if e == nil {
			continue
		}
		out[kind] = executionView(e)
	}
	return out
}

func executionView(e *delivery.Execution) delivery.ExecutionView {
	return delivery.ExecutionView{
		ID:        e.ID,
		Kind:      e.Kind,
		Status:    e.Status,
		RunName:   e.RunName,
		Reason:    e.Reason,
		CreatedAt: e.CreatedAt,
		StartedAt: e.StartedAt,
		EndedAt:   e.EndedAt,
	}
}

func matchesState(issueState, filter string) bool {
	open := strings.EqualFold(issueState, "open")
	switch strings.ToLower(filter) {
	case "", "open":
		return open
	case "closed":
		return !open
	case "all":
		return true
	default:
		return open
	}
}
