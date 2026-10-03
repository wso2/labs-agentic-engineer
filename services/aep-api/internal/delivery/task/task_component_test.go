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

// Component tier for the task read surface: the REAL contract-first strict
// handler (via componenttest, tenant gate in ENFORCE) over list-tasks /
// get-task, with only the out-of-process edges faked (GitHub issues,
// executions rows). Proves the HTTP contract end to end — derived-status
// shapes, the 404 miss, and the no-claims 401 the API-surface guard exists
// for. The command/plan HTTP operations the retired Huma surface carried
// (plan-tasks, execute-task, hold-task, unhold-task, promote-task-from-issue)
// are not in the committed contract, so their route tests are gone. One
// plan turn per project is the AE Studio pod's lock now (plan_test.go).
package task_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/delivery/execution"
	deliveryhttpapi "github.com/wso2/aep/aep-api/internal/delivery/httpapi"
	"github.com/wso2/aep/aep-api/internal/delivery/task"
	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

const (
	org   = "acme"
	proj  = "widgets"
	tasks = "/api/v1/projects/" + proj + "/tasks"
)

// ---- faked edges -----------------------------------------------------------

type fakeIssues struct {
	mu    sync.Mutex
	byNum map[int]*sourcecontrol.IssueInfo
	// milestone is the one milestone every seeded issue belongs to — enough for
	// the tag-scoped reads here, which never need two versions at once.
	milestone int
	// hostComments is what the host holds per issue; commentReads counts the
	// round trips the surface actually made.
	hostComments map[int][]sourcecontrol.IssueComment
	commentReads int
}

func newIssues(seed ...sourcecontrol.IssueInfo) *fakeIssues {
	f := &fakeIssues{byNum: map[int]*sourcecontrol.IssueInfo{}}
	for i := range seed {
		cp := seed[i]
		f.byNum[cp.Number] = &cp
	}
	return f
}
func (f *fakeIssues) CreateIssue(context.Context, string, string, sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error) {
	return &sourcecontrol.IssueResult{Number: 999, URL: "u"}, nil
}
func (f *fakeIssues) ListIssues(_ context.Context, _, _ string, want []string) ([]sourcecontrol.IssueInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sourcecontrol.IssueInfo
	for _, i := range f.byNum {
		if hasAll(i.Labels, want) {
			out = append(out, *i)
		}
	}
	return out, nil
}
func (f *fakeIssues) GetIssue(_ context.Context, _, _ string, n int) (*sourcecontrol.IssueInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := f.byNum[n]; i != nil {
		cp := *i
		return &cp, nil
	}
	return nil, sourcecontrol.ErrIssueNotFound
}
func (f *fakeIssues) ListMilestoneIssues(_ context.Context, _, _ string, filter sourcecontrol.MilestoneIssuesFilter) ([]sourcecontrol.IssueInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.milestone == 0 || f.milestone != filter.Number {
		return nil, nil
	}
	out := make([]sourcecontrol.IssueInfo, 0, len(f.byNum))
	for _, i := range f.byNum {
		out = append(out, *i)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Number < out[b].Number })
	return out, nil
}
func (f *fakeIssues) ListMilestoneIssueComments(_ context.Context, _, _ string, number, _ int) (map[int][]sourcecontrol.IssueComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commentReads++
	if f.milestone == 0 || f.milestone != number {
		return nil, nil
	}
	out := map[int][]sourcecontrol.IssueComment{}
	for n, cs := range f.hostComments {
		out[n] = append([]sourcecontrol.IssueComment(nil), cs...)
	}
	return out, nil
}
func (f *fakeIssues) ListIssueComments(_ context.Context, _, _ string, number, _ int) ([]sourcecontrol.IssueComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commentReads++
	cs := f.hostComments[number]
	if len(cs) == 0 {
		return nil, nil
	}
	return append([]sourcecontrol.IssueComment(nil), cs...), nil
}

// inMilestone marks the seeded issues as members of one milestone and attaches
// a comment thread, so a `?tag=` read has something to answer with.
func (f *fakeIssues) inMilestone(number int, comments map[int][]sourcecontrol.IssueComment) *fakeIssues {
	f.milestone = number
	f.hostComments = comments
	return f
}

// fakeMilestoneRuns resolves a `?tag=` to a milestone number, standing in for
// the run rows.
type fakeMilestoneRuns map[string]int

func (f fakeMilestoneRuns) MilestoneNumberForTag(_ context.Context, _, _, tag string) (int, bool, error) {
	n, ok := f[tag]
	return n, ok, nil
}
func (f *fakeIssues) CommentIssue(context.Context, string, string, int, string) error { return nil }
func (f *fakeIssues) EditIssueBody(context.Context, string, string, int, string) error {
	return nil
}
func (f *fakeIssues) EditIssueTitle(context.Context, string, string, int, string) error { return nil }
func (f *fakeIssues) AddLabels(_ context.Context, _, _ string, n int, labels []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := f.byNum[n]; i != nil {
		i.Labels = append(i.Labels, labels...)
	}
	return nil
}
func (f *fakeIssues) RemoveLabel(context.Context, string, string, int, string) error { return nil }

// CloseIssue, ReopenIssue and SetIssueMilestone complete delivery.IssueOps — the
// writer's port is the domain's whole issue-write surface, and the plan path uses
// none of the three.
func (f *fakeIssues) CloseIssue(context.Context, string, string, int, string) error { return nil }
func (f *fakeIssues) ReopenIssue(context.Context, string, string, int) error        { return nil }
func (f *fakeIssues) SetIssueMilestone(context.Context, string, string, int, int) error {
	return nil
}

// writer wears the domain's issue-write surface over the fake, which is how the
// plan tap mints.
func (f *fakeIssues) writer() *delivery.IssueWriter { return delivery.NewIssueWriter(f) }

type fakeRepos struct{}

func (fakeRepos) GetRepo(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return &sourcecontrol.GitRepository{OrgID: org, ProjectID: proj, RepoURL: "https://github.com/acme/widgets"}, nil
}

type fakeExecs struct {
	latest  map[int]map[string]*delivery.Execution
	history map[int][]delivery.Execution
}

func (f fakeExecs) LatestPerKindScoped(_ context.Context, _, _ string, n int) (map[string]*delivery.Execution, error) {
	if m := f.latest[n]; m != nil {
		return m, nil
	}
	return map[string]*delivery.Execution{}, nil
}
func (f fakeExecs) LatestPerKindForRepoScoped(_ context.Context, _, _ string) (map[int]map[string]*delivery.Execution, error) {
	if f.latest != nil {
		return f.latest, nil
	}
	return map[int]map[string]*delivery.Execution{}, nil
}
func (f fakeExecs) ListByIssueScoped(_ context.Context, _, _ string, n int) ([]delivery.Execution, error) {
	return f.history[n], nil
}

func hasAll(have, want []string) bool {
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

// taskIssue builds a planned Task as the plan tap writes it: a prose body and
// the `aep` working-set label.
func taskIssue(number int, component, state string, extra ...string) sourcecontrol.IssueInfo {
	labels := append([]string{delivery.LabelAgentWork}, extra...)
	return sourcecontrol.IssueInfo{
		Number: number,
		Title:  "Implement " + component,
		Body:   "Build it.\n\n**Component:** `" + component + "`",
		State:  state,
		URL:    "https://github.com/acme/widgets/issues/1",
		Labels: labels,
	}
}

// ---- harness ---------------------------------------------------------------

func newRig(t *testing.T, iss *fakeIssues, execs fakeExecs) *componenttest.Harness {
	t.Helper()
	return newRigWithRuns(t, iss, execs, nil)
}

// newRigWithRuns is newRig plus a tag→milestone resolver, for the reads that are
// version-scoped.
func newRigWithRuns(t *testing.T, iss *fakeIssues, execs fakeExecs, runs task.MilestoneResolver) *componenttest.Harness {
	t.Helper()
	reads := task.NewReads(iss, fakeRepos{}, execs, runs)
	return componenttest.New(t, componenttest.Options{Deps: edge.Deps{
		Delivery: mustDelivery(deliveryhttpapi.New(deliveryhttpapi.Deps{TaskReads: reads})),
	}})
}

// mustDelivery assembles the delivery domain aggregator for the harness (New
// never errors today; panic keeps the wiring honest if that changes).
func mustDelivery(h *deliveryhttpapi.Handlers, err error) *deliveryhttpapi.Handlers {
	if err != nil {
		panic(err)
	}
	return h
}

// ---- tests -----------------------------------------------------------------

// The list's wire shape: the derived status is the issue's own state, the kind
// chip is label-derived, and attention is [] rather than null (the console maps
// over it unconditionally).
func TestList_WireShape(t *testing.T) {
	closed := taskIssue(2, "order-service", "closed")
	iss := newIssues(taskIssue(1, "user-service", "open"), closed)
	h := newRig(t, iss, fakeExecs{})

	rec := h.AsOrg(org).Get(tasks + "?state=all")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: code %d (%s)", rec.Code, rec.Body.String())
	}
	var views []delivery.TaskView
	_ = json.Unmarshal(rec.Body.Bytes(), &views)
	byNum := map[int]delivery.TaskView{}
	for _, v := range views {
		byNum[v.IssueNumber] = v
	}
	if byNum[1].DerivedStatus != delivery.DerivedStatusPending {
		t.Errorf("open task = %q, want %q", byNum[1].DerivedStatus, delivery.DerivedStatusPending)
	}
	if byNum[2].DerivedStatus != delivery.DerivedStatusMerged {
		t.Errorf("closed task = %q, want %q", byNum[2].DerivedStatus, delivery.DerivedStatusMerged)
	}
	if byNum[1].ExecutorClass != "coding" {
		t.Errorf("task 1 kind = %q, want coding", byNum[1].ExecutorClass)
	}
	if !strings.Contains(rec.Body.String(), `"attention":[]`) {
		t.Errorf("attention must marshal as [] not null: %s", rec.Body.String())
	}
}

func TestGet_IncludesHistory(t *testing.T) {
	iss := newIssues(taskIssue(5, "orders-db", "open", delivery.KindProvision))
	execs := fakeExecs{
		latest:  map[int]map[string]*delivery.Execution{5: {string(taskmeta.KindProvision): row("b", taskmeta.KindProvision, taskmeta.ExecSucceeded, "", 0)}},
		history: map[int][]delivery.Execution{5: {*row("a", taskmeta.KindProvision, taskmeta.ExecFailed, "", -1), *row("b", taskmeta.KindProvision, taskmeta.ExecSucceeded, "", 0)}},
	}
	h := newRig(t, iss, execs)

	rec := h.AsOrg(org).Get(tasks + "/5")
	if rec.Code != http.StatusOK {
		t.Fatalf("get: code %d (%s)", rec.Code, rec.Body.String())
	}
	var d delivery.TaskDetail
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if len(d.ExecutionHistory) != 2 || d.ExecutorClass != "provision" {
		t.Fatalf("get shape wrong: kind=%q history=%d", d.ExecutorClass, len(d.ExecutionHistory))
	}

	miss := h.AsOrg(org).Get(tasks + "/404")
	if miss.Code != http.StatusNotFound {
		t.Errorf("missing task: code %d, want 404", miss.Code)
	}
	if e := componenttest.DecodeEnvelope(t, miss.Body.String()); e.Code != "not_found" {
		t.Errorf("missing task envelope code = %q, want not_found", e.Code)
	}
}

func TestTasks_NoAuth_401(t *testing.T) {
	h := newRig(t, newIssues(), fakeExecs{})
	if rec := h.NoAuth().Get(tasks); rec.Code != http.StatusUnauthorized {
		t.Errorf("no-auth list: code %d, want 401", rec.Code)
	}
}

// row builds a seeded execution with a creation-time offset (hours).
func row(id string, kind taskmeta.ExecutionKind, status taskmeta.ExecutionStatus, reason string, hours int) *delivery.Execution {
	return &delivery.Execution{
		ID: id, Kind: string(kind), Status: string(status), Reason: reason,
		CreatedAt: time.Now().Add(time.Duration(hours) * time.Hour),
	}
}

// Ensure the execution package's progress service compiles into this test's
// import set (the progress route is covered by execution's own tests + the
// GetByIDScoped org fence; the internal S2S surface is not mounted by the
// componenttest harness, so the skills route's cross-tenant fence is asserted
// directly in the execution package).
var _ = execution.NewProgressService
