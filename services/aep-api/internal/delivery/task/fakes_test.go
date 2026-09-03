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
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// fakeIssues is an in-memory IssueClient: issues keyed by number, recording
// label/comment/body/title mutations.
type fakeIssues struct {
	mu           sync.Mutex
	byNumber     map[int]*sourcecontrol.IssueInfo
	nextNum      int
	created      []sourcecontrol.CreateIssueRequest
	comments     map[int][]string
	failCreate   bool
	failEditBody bool
	// milestoneOf records the milestone each seeded issue belongs to, so
	// ListMilestoneIssues can answer the plan turn's membership read.
	milestoneOf map[int]int
	// closed/reopened/moved record the three writes the plan path must never make.
	closed   []int
	reopened []int
	moved    []string
	// hostComments is what the HOST holds on each issue — the read side, as
	// distinct from `comments`, which records the bodies this fake was asked to
	// write. commentReads/lastCommentPerIssue prove whether the read happened at
	// all and what cap it asked for; failComments makes it fail.
	hostComments        map[int][]sourcecontrol.IssueComment
	commentReads        int
	lastCommentPerIssue int
	failComments        bool
	// The overlap probe (see the two milestone reads below): the comment read
	// closes commentStarted on entry, and the issue list waits on
	// awaitCommentStart. Both nil unless a test wires them with overlapProbe.
	commentStarted    chan struct{}
	awaitCommentStart chan struct{}
}

// overlapProbe makes the issue list block until the comment read has started,
// so a test can assert the two host reads OVERLAP without measuring time.
func (f *fakeIssues) overlapProbe() *fakeIssues {
	ch := make(chan struct{})
	f.commentStarted = ch
	f.awaitCommentStart = ch
	return f
}

// writer is the fake wearing the domain's issue-write surface, which is how the
// plan tap mints through it.
func (f *fakeIssues) writer() *delivery.IssueWriter { return delivery.NewIssueWriter(f) }

func newFakeIssues() *fakeIssues {
	return &fakeIssues{byNumber: map[int]*sourcecontrol.IssueInfo{}, nextNum: 100, comments: map[int][]string{}, milestoneOf: map[int]int{}}
}

func (f *fakeIssues) seed(issue sourcecontrol.IssueInfo) *fakeIssues {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := issue
	f.byNumber[issue.Number] = &cp
	if issue.Number >= f.nextNum {
		f.nextNum = issue.Number + 1
	}
	return f
}

func (f *fakeIssues) CreateIssue(_ context.Context, _, _ string, req sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failCreate {
		return nil, fmt.Errorf("boom: create failed")
	}
	n := f.nextNum
	f.nextNum++
	f.created = append(f.created, req)
	f.byNumber[n] = &sourcecontrol.IssueInfo{Number: n, Title: req.Title, Body: req.Body, State: "open", Labels: req.Labels, URL: fmt.Sprintf("https://github.com/o/r/issues/%d", n)}
	if req.Milestone != nil {
		f.milestoneOf[n] = *req.Milestone
	}
	return &sourcecontrol.IssueResult{Number: n, URL: f.byNumber[n].URL}, nil
}

func (f *fakeIssues) ListIssues(_ context.Context, _, _ string, labels []string) ([]sourcecontrol.IssueInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sourcecontrol.IssueInfo
	for _, issue := range f.byNumber {
		if issueHasAll(issue.Labels, labels) {
			out = append(out, *issue)
		}
	}
	return out, nil
}

// seedInMilestone seeds an issue as a member of a milestone.
func (f *fakeIssues) seedInMilestone(issue sourcecontrol.IssueInfo, milestone int) *fakeIssues {
	f.seed(issue)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.milestoneOf[issue.Number] = milestone
	return f
}

func (f *fakeIssues) ListMilestoneIssues(_ context.Context, _, _ string, filter sourcecontrol.MilestoneIssuesFilter) ([]sourcecontrol.IssueInfo, error) {
	// The concurrency probe: block the issue list until the comment read has
	// STARTED. Sequential reads would leave this waiting forever, so the test
	// asserting overlap fails by timing out rather than by a stopwatch. Waited
	// on before the lock is taken — the comment read needs it too.
	if f.awaitCommentStart != nil {
		select {
		case <-f.awaitCommentStart:
		case <-time.After(2 * time.Second):
			return nil, fmt.Errorf("comment read never started: the two host reads are sequential")
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sourcecontrol.IssueInfo
	for n, issue := range f.byNumber {
		if f.milestoneOf[n] != filter.Number {
			continue
		}
		if filter.State != "" && filter.State != "all" && !strings.EqualFold(issue.State, filter.State) {
			continue
		}
		if !issueHasAll(issue.Labels, filter.Labels) {
			continue
		}
		out = append(out, *issue)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

// seedComment appends a host comment to an issue, in thread order.
func (f *fakeIssues) seedComment(number int, c sourcecontrol.IssueComment) *fakeIssues {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hostComments == nil {
		f.hostComments = map[int][]sourcecontrol.IssueComment{}
	}
	f.hostComments[number] = append(f.hostComments[number], c)
	return f
}

func (f *fakeIssues) ListMilestoneIssueComments(_ context.Context, _, _ string, number, perIssue int) (map[int][]sourcecontrol.IssueComment, error) {
	// Announce the start before contending for the lock, so the issue list this
	// releases is not waiting on a lock this call is about to take.
	if f.commentStarted != nil {
		close(f.commentStarted)
		f.commentStarted = nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commentReads++
	f.lastCommentPerIssue = perIssue
	if f.failComments {
		return nil, fmt.Errorf("boom: comment read failed")
	}
	out := map[int][]sourcecontrol.IssueComment{}
	for n, cs := range f.hostComments {
		if f.milestoneOf[n] != number || len(cs) == 0 {
			continue
		}
		// The host answers the NEWEST perIssue, still in thread order — mirror
		// that here so a test can prove the tail is what survives the cap.
		if perIssue > 0 && len(cs) > perIssue {
			cs = cs[len(cs)-perIssue:]
		}
		out[n] = append([]sourcecontrol.IssueComment(nil), cs...)
	}
	return out, nil
}

func (f *fakeIssues) GetIssue(_ context.Context, _, _ string, number int) (*sourcecontrol.IssueInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := f.byNumber[number]; i != nil {
		cp := *i
		return &cp, nil
	}
	return nil, sourcecontrol.ErrIssueNotFound
}

func (f *fakeIssues) CommentIssue(_ context.Context, _, _ string, number int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments[number] = append(f.comments[number], body)
	return nil
}

func (f *fakeIssues) EditIssueBody(_ context.Context, _, _ string, number int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failEditBody {
		return fmt.Errorf("boom: edit body failed")
	}
	if i := f.byNumber[number]; i != nil {
		i.Body = body
	}
	return nil
}

func (f *fakeIssues) EditIssueTitle(_ context.Context, _, _ string, number int, title string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := f.byNumber[number]; i != nil {
		i.Title = title
	}
	return nil
}

func (f *fakeIssues) AddLabels(_ context.Context, _, _ string, number int, labels []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := f.byNumber[number]; i != nil {
		i.Labels = append(i.Labels, labels...)
	}
	return nil
}

func (f *fakeIssues) RemoveLabel(_ context.Context, _, _ string, number int, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.byNumber[number]
	if i == nil {
		return nil
	}
	var kept []string
	for _, l := range i.Labels {
		if l != label {
			kept = append(kept, l)
		}
	}
	i.Labels = kept
	return nil
}

// SetIssueMilestone exists so the fake satisfies delivery.IssueOps. The plan tap
// assigns a milestone by RIDING the create, never by a follow-up move, so a
// planner that called this would be spending a second GitHub request per Task.
func (f *fakeIssues) SetIssueMilestone(_ context.Context, _, _ string, number, milestoneNumber int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moved = append(f.moved, fmt.Sprintf("%d>m%d", number, milestoneNumber))
	return nil
}

// CloseIssue and ReopenIssue exist so the fake satisfies delivery.IssueOps —
// the writer's port is the domain's WHOLE issue-write surface, while the plan
// tap only ever mints and edits. Both record their calls so a test can assert
// the planner never closes or reopens anything.
func (f *fakeIssues) CloseIssue(_ context.Context, _, _ string, number int, comment string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, number)
	if comment != "" {
		f.comments[number] = append(f.comments[number], comment)
	}
	if i := f.byNumber[number]; i != nil {
		i.State = "closed"
	}
	return nil
}

func (f *fakeIssues) ReopenIssue(_ context.Context, _, _ string, number int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reopened = append(f.reopened, number)
	if i := f.byNumber[number]; i != nil {
		i.State = "open"
	}
	return nil
}

func (f *fakeIssues) labelsOf(number int) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := f.byNumber[number]; i != nil {
		return append([]string{}, i.Labels...)
	}
	return nil
}

func (f *fakeIssues) bodyOf(number int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := f.byNumber[number]; i != nil {
		return i.Body
	}
	return ""
}

func issueHasAll(have, want []string) bool {
	set := map[string]bool{}
	for _, l := range have {
		set[l] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

// fakeRepos returns a fixed repo row.
type fakeRepos struct {
	repo *sourcecontrol.GitRepository
}

func (f fakeRepos) GetRepo(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	if f.repo == nil {
		return nil, sourcecontrol.ErrRepoNotFound
	}
	return f.repo, nil
}

func defaultRepo() *sourcecontrol.GitRepository {
	return &sourcecontrol.GitRepository{OrgID: "org1", ProjectID: "proj1", RepoURL: "https://github.com/o/r"}
}

// fakeExecReader serves seeded execution rows.
type fakeExecReader struct {
	latest  map[int]map[string]*delivery.Execution // issueNumber → kind → row
	history map[int][]delivery.Execution
}

func newFakeExecReader() *fakeExecReader {
	return &fakeExecReader{latest: map[int]map[string]*delivery.Execution{}, history: map[int][]delivery.Execution{}}
}

func (f *fakeExecReader) put(number int, e delivery.Execution) *fakeExecReader {
	if f.latest[number] == nil {
		f.latest[number] = map[string]*delivery.Execution{}
	}
	cp := e
	f.latest[number][e.Kind] = &cp
	f.history[number] = append(f.history[number], e)
	return f
}

func (f *fakeExecReader) LatestPerKindScoped(_ context.Context, _, _ string, number int) (map[string]*delivery.Execution, error) {
	if m := f.latest[number]; m != nil {
		return m, nil
	}
	return map[string]*delivery.Execution{}, nil
}

func (f *fakeExecReader) LatestPerKindForRepoScoped(_ context.Context, _, _ string) (map[int]map[string]*delivery.Execution, error) {
	return f.latest, nil
}

func (f *fakeExecReader) ListByIssueScoped(_ context.Context, _, _ string, number int) ([]delivery.Execution, error) {
	return f.history[number], nil
}

// fakeMilestones resolves a `?tag=` to a milestone number, standing in for the
// run rows. A tag it does not know is "this platform never built that version".
type fakeMilestones map[string]int

func (f fakeMilestones) MilestoneNumberForTag(_ context.Context, _, _, tag string) (int, bool, error) {
	n, ok := f[tag]
	return n, ok, nil
}

// fakeAdopter records the issues handed to the coding agent.
type fakeAdopter struct {
	adopted []int
	err     error
}

func (f *fakeAdopter) AdoptIssue(_ context.Context, _, _ string, issueNumber int) error {
	if f.err != nil {
		return f.err
	}
	f.adopted = append(f.adopted, issueNumber)
	return nil
}

// fakeEnsurer is a ComponentEnsurer that knows a fixed component set.
type fakeEnsurer struct {
	known map[string]bool
	calls []string
}

func (f *fakeEnsurer) EnsureComponent(_ context.Context, _, _, component string) error {
	f.calls = append(f.calls, component)
	if !f.known[component] {
		return fmt.Errorf("component %q not found in the design", component)
	}
	return nil
}

// agentIssue builds a seeded Task issue as the plan tap writes one: prose body,
// the `aep` working-set label, and nothing else.
func agentIssue(number int, title, body string) sourcecontrol.IssueInfo {
	return sourcecontrol.IssueInfo{
		Number: number,
		Title:  title,
		Body:   body,
		State:  "open",
		URL:    fmt.Sprintf("https://github.com/o/r/issues/%d", number),
		Labels: []string{delivery.LabelAgentWork, delivery.KindDevelopment},
	}
}

// gateIssue builds a seeded dispatch gate: kind `provision`, and deliberately
// NOT armed — nothing may work a gate.
func gateIssue(number int, depName string) sourcecontrol.IssueInfo {
	return sourcecontrol.IssueInfo{
		Number: number,
		Title:  "Provide configuration: " + depName,
		Body:   "Provide this dependency's configuration values in the architecture drawer.",
		State:  "open",
		URL:    fmt.Sprintf("https://github.com/o/r/issues/%d", number),
		Labels: []string{delivery.KindProvision},
	}
}

// validationIssue builds the project's seeded validation task: ARMED like any
// other agent work, and of a kind no working set includes.
func validationIssue(number int) sourcecontrol.IssueInfo {
	return sourcecontrol.IssueInfo{
		Number: number,
		Title:  "Validate the deployed system against its validation criteria",
		Body:   "Author e2e tests and run them against the deployed system.",
		State:  "open",
		URL:    fmt.Sprintf("https://github.com/o/r/issues/%d", number),
		Labels: []string{delivery.LabelAgentWork, delivery.KindValidation},
	}
}

// bugIssue builds a platform-minted defect: armed, kind `bug`, sourced from the
// build that found it. It is `coding` to the console like planned work, and
// tells itself apart from it only by its kind.
func bugIssue(number int, title string) sourcecontrol.IssueInfo {
	return sourcecontrol.IssueInfo{
		Number: number,
		Title:  title,
		Body:   "The build failed and failed again on a re-trigger.",
		State:  "open",
		URL:    fmt.Sprintf("https://github.com/o/r/issues/%d", number),
		Labels: []string{delivery.LabelAgentWork, delivery.KindBug, delivery.SrcBuild},
	}
}

// ledgerIssue is a bare human issue: no aep label at all, so it is never worked
// and never listed as a Task.
func ledgerIssue(number int, title string) sourcecontrol.IssueInfo {
	return sourcecontrol.IssueInfo{
		Number: number,
		Title:  title,
		Body:   "Filed by a human.",
		State:  "open",
		URL:    fmt.Sprintf("https://github.com/o/r/issues/%d", number),
	}
}
