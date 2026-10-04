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
	"fmt"
	"sync"
	"testing"
)

// --- minimal fakes (embed the interface; only the methods the dedup path
// actually calls are implemented — the rest would panic if reached, which is
// the point: the test asserts the path stays on the dedup branch) ---

type fakeRepoRepo struct{ RepoRepository }

func (fakeRepoRepo) GetByOrgAndProjectID(_ context.Context, org, proj string) (*GitRepository, error) {
	return &GitRepository{OrgID: org, ProjectID: proj, RepoURL: "https://github.com/o/r"}, nil
}

// fakeGitHub records created issues in memory and filters ListIssues by label,
// exactly as the real GitHub label query would.
type fakeGitHub struct {
	IssueOps
	mu          sync.Mutex
	issues      []IssueInfo
	createCount int
	nextNum     int
}

func (f *fakeGitHub) ListIssues(_ context.Context, _ RepoRef, labels []string) ([]IssueInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []IssueInfo
	for _, iss := range f.issues {
		for _, want := range labels {
			if hasLabel(iss, want) {
				out = append(out, iss)
				break
			}
		}
	}
	return out, nil
}

func (f *fakeGitHub) EnsureLabel(context.Context, RepoRef, string, string) error {
	return nil
}

func (f *fakeGitHub) CreateIssue(_ context.Context, _ RepoRef, req CreateIssueRequest) (*IssueResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCount++
	f.nextNum++
	n := f.nextNum
	f.issues = append(f.issues, IssueInfo{Number: n, Title: req.Title, State: "open", Labels: req.Labels})
	return &IssueResult{Number: n, URL: fmt.Sprintf("https://github.com/o/r/issues/%d", n)}, nil
}

func hasLabel(iss IssueInfo, want string) bool {
	for _, l := range iss.Labels {
		if l == want {
			return true
		}
	}
	return false
}

func newDedupService(gh *fakeGitHub) IssueService {
	return NewIssueService(fakeRepoRepo{}, gh)
}

func req(title, key string) CreateIssueRequest {
	return CreateIssueRequest{Title: title, Body: "b", DedupeKey: key}
}

// The core guarantee: a second create with the same open dedupe key returns
// the existing issue instead of filing a new one.
func TestCreateIssueDedup_SecondCallReturnsExisting(t *testing.T) {
	gh := &fakeGitHub{}
	svc := newDedupService(gh)
	ctx := context.Background()

	first, err := svc.CreateIssue(ctx, "org", "proj", req("timeout in service1", "sre-rca/service1"))
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if first.Deduped {
		t.Fatal("first create should NOT be deduped")
	}

	second, err := svc.CreateIssue(ctx, "org", "proj", req("timeout again", "sre-rca/service1"))
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if !second.Deduped {
		t.Fatal("second create with same open key MUST be deduped")
	}
	if second.Number != first.Number {
		t.Errorf("deduped issue should point at #%d, got #%d", first.Number, second.Number)
	}
	if gh.createCount != 1 {
		t.Errorf("expected exactly 1 GitHub create, got %d", gh.createCount)
	}
}

// Different keys are independent incidents — both create.
func TestCreateIssueDedup_DifferentKeyCreatesNew(t *testing.T) {
	gh := &fakeGitHub{}
	svc := newDedupService(gh)
	ctx := context.Background()
	if _, err := svc.CreateIssue(ctx, "org", "proj", req("a", "sre-rca/service1")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateIssue(ctx, "org", "proj", req("b", "sre-rca/service2")); err != nil {
		t.Fatal(err)
	}
	if gh.createCount != 2 {
		t.Errorf("distinct keys should both create; got %d creates", gh.createCount)
	}
}

// A CLOSED issue with the key does not block a fresh incident — recurrence
// after a fix files a new issue.
func TestCreateIssueDedup_ClosedKeyDoesNotBlock(t *testing.T) {
	gh := &fakeGitHub{
		issues:  []IssueInfo{{Number: 1, State: "closed", Labels: []string{dedupeLabelFor("sre-rca/service1")}}},
		nextNum: 1,
	}
	svc := newDedupService(gh)
	res, err := svc.CreateIssue(context.Background(), "org", "proj", req("recurrence", "sre-rca/service1"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Deduped {
		t.Error("a closed keyed issue must NOT dedupe a new incident")
	}
	if gh.createCount != 1 {
		t.Errorf("expected a fresh create, got %d", gh.createCount)
	}
}

// The correctness guarantee under concurrency: N simultaneous creates with the
// same key must collapse to exactly ONE GitHub issue (the per-repo lock making
// check-then-create atomic). This is the race that produced duplicate SRE/RCA
// issues; without the lock, createCount would be > 1.
func TestCreateIssueDedup_ConcurrentCollapsesToOne(t *testing.T) {
	gh := &fakeGitHub{}
	svc := newDedupService(gh)
	const n = 8

	var wg sync.WaitGroup
	results := make([]*IssueResult, n)
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = svc.CreateIssue(context.Background(), "org", "proj",
				req(fmt.Sprintf("run %d", i), "sre-rca/service1"))
		}(i)
	}
	wg.Wait()

	created, deduped := 0, 0
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("run %d errored: %v", i, errs[i])
		}
		if results[i].Deduped {
			deduped++
		} else {
			created++
		}
	}
	if gh.createCount != 1 {
		t.Errorf("expected exactly 1 GitHub issue from %d concurrent runs, got %d", n, gh.createCount)
	}
	if created != 1 || deduped != n-1 {
		t.Errorf("expected 1 created + %d deduped, got %d created + %d deduped", n-1, created, deduped)
	}
}
