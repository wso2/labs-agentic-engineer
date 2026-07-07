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

package execution

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/models"
)

// fakeStore is an in-memory ExecutionStore that reproduces the partial
// admission-mutex semantics (one active row per (repo, issueNumber, kind)).
type fakeStore struct {
	mu   sync.Mutex
	rows []*models.Execution
	next int
}

func newFakeStore() *fakeStore { return &fakeStore{} }

func (s *fakeStore) TryAdmit(_ context.Context, e *models.Execution) (bool, *models.Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.Status == "" {
		e.Status = string(taskmeta.ExecQueued)
	}
	for _, r := range s.rows {
		if r.Repo == e.Repo && r.IssueNumber == e.IssueNumber && r.Kind == e.Kind &&
			taskmeta.ExecutionStatus(r.Status).IsActive() {
			return false, nil, nil
		}
	}
	s.next++
	e.ID = fmt.Sprintf("exec-%d", s.next)
	e.CreatedAt = time.Now().Add(time.Duration(s.next) * time.Millisecond)
	cp := *e
	s.rows = append(s.rows, &cp)
	return true, &cp, nil
}

// markRunning is a test helper: transition a queued row to running (what an
// executor does via StartWithRun in production).
func (s *fakeStore) markRunning(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rows {
		if r.ID == id && r.Status == string(taskmeta.ExecQueued) {
			r.Status = string(taskmeta.ExecRunning)
			return
		}
	}
}

func (s *fakeStore) Finish(_ context.Context, id, status, reason string) (*models.Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rows {
		if r.ID == id && taskmeta.ExecutionStatus(r.Status).IsActive() {
			r.Status = status
			r.Reason = reason
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

func (s *fakeStore) LatestPerKind(_ context.Context, repo string, issueNumber int) (map[string]*models.Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]*models.Execution{}
	for _, r := range s.rows {
		if r.Repo != repo || r.IssueNumber != issueNumber {
			continue
		}
		if cur, ok := out[r.Kind]; !ok || r.CreatedAt.After(cur.CreatedAt) {
			cp := *r
			out[r.Kind] = &cp
		}
	}
	return out, nil
}

func (s *fakeStore) LatestPerKindForRepo(_ context.Context, repo string) (map[int]map[string]*models.Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[int]map[string]*models.Execution{}
	for _, r := range s.rows {
		if r.Repo != repo {
			continue
		}
		byKind := out[r.IssueNumber]
		if byKind == nil {
			byKind = map[string]*models.Execution{}
			out[r.IssueNumber] = byKind
		}
		if cur, ok := byKind[r.Kind]; !ok || r.CreatedAt.After(cur.CreatedAt) {
			cp := *r
			byKind[r.Kind] = &cp
		}
	}
	return out, nil
}

func (s *fakeStore) ListByIssue(_ context.Context, repo string, issueNumber int) ([]models.Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []models.Execution
	for _, r := range s.rows {
		if r.Repo == repo && r.IssueNumber == issueNumber {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (s *fakeStore) ListActive(_ context.Context) ([]models.Execution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []models.Execution
	for _, r := range s.rows {
		if taskmeta.ExecutionStatus(r.Status).IsActive() {
			out = append(out, *r)
		}
	}
	return out, nil
}

// fakeIssues records label/comment mutations and serves a scripted issue list.
type fakeIssues struct {
	mu       sync.Mutex
	list     []gitrepo.IssueInfo
	added    map[int][]string
	removed  map[int][]string
	comments map[int][]string
}

func newFakeIssues(list []gitrepo.IssueInfo) *fakeIssues {
	return &fakeIssues{
		list:     list,
		added:    map[int][]string{},
		removed:  map[int][]string{},
		comments: map[int][]string{},
	}
}

func (f *fakeIssues) ListIssues(_ context.Context, _, _ string, labels []string) ([]gitrepo.IssueInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []gitrepo.IssueInfo
	for _, issue := range f.list {
		if hasAllLabels(issue.Labels, labels) {
			out = append(out, issue)
		}
	}
	return out, nil
}

func (f *fakeIssues) AddLabels(_ context.Context, _, _ string, number int, labels []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added[number] = append(f.added[number], labels...)
	return nil
}

func (f *fakeIssues) RemoveLabel(_ context.Context, _, _ string, number int, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed[number] = append(f.removed[number], label)
	return nil
}

func (f *fakeIssues) CommentIssue(_ context.Context, _, _ string, number int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments[number] = append(f.comments[number], body)
	return nil
}

func hasAllLabels(have, want []string) bool {
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

// fakePRReader serves scripted live PR states by number.
type fakePRReader struct {
	states map[int]*gitrepo.PullRequestState
}

func (f fakePRReader) GetPullRequestState(_ context.Context, _, _ string, number int) (*gitrepo.PullRequestState, error) {
	return f.states[number], nil
}

// fakeRepos resolves any repo full name to one org/project.
type fakeRepos struct{ orgID, projectID string }

func (f fakeRepos) ByFullName(context.Context, string) (string, string, error) {
	return f.orgID, f.projectID, nil
}

func taskIssue(number int, comp string, deps []string, extraLabels []string, state string) gitrepo.IssueInfo {
	block := taskmeta.Block{
		Component: comp,
		DependsOn: deps,
		Origin:    taskmeta.OriginSpecPlan,
		SpecTag:   "req-v1",
		DesignTag: "design-v1",
	}
	body := taskmeta.ComposeBody(block, taskmeta.Human{Rationale: "because", Body: "## Scope\nwork"})
	labels := append(taskmeta.NewTaskLabels(taskmeta.ClassCoding, taskmeta.OriginSpecPlan), extraLabels...)
	return gitrepo.IssueInfo{
		Number: number,
		Title:  "Implement " + comp,
		Body:   body,
		State:  state,
		URL:    fmt.Sprintf("https://github.com/o/r/issues/%d", number),
		Labels: labels,
	}
}

// fakeDesign returns a fixed component-name set and optional per-component
// provisioning deps.
type fakeDesign struct {
	names        map[string]bool
	provisionDep map[string][]string
}

func (f fakeDesign) ComponentNames(context.Context, string, string) (map[string]bool, error) {
	return f.names, nil
}

func (f fakeDesign) ProvisionDepNames(context.Context, string, string) (map[string][]string, error) {
	return f.provisionDep, nil
}

// fakeExecutor records the requests it received and can be told to Start the row
// (the normal success path) or to fail.
type fakeExecutor struct {
	store   *fakeStore
	fail    error
	got     []DispatchRequest
	startOK bool
}

func (e *fakeExecutor) Run(_ context.Context, req DispatchRequest) error {
	e.got = append(e.got, req)
	if e.fail != nil {
		return e.fail
	}
	if e.startOK && e.store != nil {
		e.store.markRunning(req.Execution.ID)
	}
	return nil
}
