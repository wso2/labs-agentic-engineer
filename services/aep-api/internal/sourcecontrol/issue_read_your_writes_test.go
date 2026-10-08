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
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// laggingGitHub models GitHub's list endpoint: it answers newest-first with
// every requested label required, but withholds the issues in `hidden` — the
// ones created inside the endpoint's indexing delay.
type laggingGitHub struct {
	fakeGitHub
	hidden map[int]bool
}

func (g *laggingGitHub) ListIssues(_ context.Context, _, _ string, _ secrets.Credential, labels []string) ([]IssueInfo, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []IssueInfo
	for i := len(g.issues) - 1; i >= 0; i-- {
		iss := g.issues[i]
		if g.hidden[iss.Number] {
			continue
		}
		all := true
		for _, want := range labels {
			if !hasLabel(iss, want) {
				all = false
			}
		}
		if all {
			out = append(out, iss)
		}
	}
	return out, nil
}

// The host accepts the platform's writes; the lag only affects what its list shows.
func (g *laggingGitHub) CloseIssue(context.Context, string, string, secrets.Credential, int) error {
	return nil
}
func (g *laggingGitHub) ReopenIssue(context.Context, string, string, secrets.Credential, int) error {
	return nil
}
func (g *laggingGitHub) EditIssueTitle(context.Context, string, string, secrets.Credential, int, string) error {
	return nil
}
func (g *laggingGitHub) EditIssueBody(context.Context, string, string, secrets.Credential, int, string) error {
	return nil
}

// lagFixture is a service over a lagging GitHub with a controllable clock.
type lagFixture struct {
	gh  *laggingGitHub
	svc *issueService
	now time.Time
}

func newLagFixture(seeded ...IssueInfo) *lagFixture {
	gh := &laggingGitHub{hidden: map[int]bool{}}
	for _, iss := range seeded {
		gh.issues = append(gh.issues, iss)
		gh.nextNum = iss.Number
	}
	f := &lagFixture{gh: gh, now: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	f.svc = NewIssueService(fakeRepoRepo{}, gh, fakeResolver{})
	f.svc.recent.now = func() time.Time { return f.now }
	return f
}

// file creates an issue and, like GitHub inside its lag window, hides it from
// the list.
func (f *lagFixture) file(t *testing.T, title string, labels ...string) int {
	t.Helper()
	res, err := f.svc.CreateIssue(context.Background(), "org", "proj", CreateIssueRequest{Title: title, Body: "b " + title, Labels: labels})
	if err != nil {
		t.Fatalf("create %q: %v", title, err)
	}
	f.gh.hidden[res.Number] = true
	return res.Number
}

func (f *lagFixture) list(t *testing.T, labels ...string) []IssueInfo {
	t.Helper()
	out, err := f.svc.ListIssues(context.Background(), "org", "proj", labels)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return out
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestListIssues_ReadsItsOwnWrites(t *testing.T) {
	f := newLagFixture(
		IssueInfo{Number: 1, Title: "old", State: "open"},
		IssueInfo{Number: 2, Title: "newer", State: "open"},
	)
	n := f.file(t, "just filed", "bug")

	got := f.list(t)
	if want := []int{n, 2, 1}; !equalInts(numbers(got), want) {
		t.Fatalf("order = %v, want %v (newest first, remembered issue first)", numbers(got), want)
	}
	first := got[0]
	if first.Title != "just filed" || first.Body != "b just filed" || first.State != "open" ||
		first.URL == "" || len(first.Labels) != 1 || first.Labels[0] != "bug" {
		t.Errorf("remembered issue built wrong: %+v", first)
	}
}

func TestListIssues_RememberedIssueHonoursLabelFilter(t *testing.T) {
	f := newLagFixture()
	bug := f.file(t, "a bug", "bug", "src/user")
	f.file(t, "a feature", "feature")

	if got := numbers(f.list(t, "bug")); !equalInts(got, []int{bug}) {
		t.Errorf("filter bug = %v, want [%d]", got, bug)
	}
	if got := f.list(t, "bug", "feature"); len(got) != 0 {
		t.Errorf("filter needing both labels = %v, want none", numbers(got))
	}
}

func TestListIssues_ForgetsACreationAfterTheWindow(t *testing.T) {
	f := newLagFixture()
	f.file(t, "stale")

	f.now = f.now.Add(59 * time.Second)
	if got := f.list(t); len(got) != 1 {
		t.Fatalf("at +59s want the remembered issue, got %v", numbers(got))
	}
	f.now = f.now.Add(2 * time.Second)
	if got := f.list(t); len(got) != 0 {
		t.Errorf("at +61s want it dropped, got %v", numbers(got))
	}
}

func TestListIssues_NoDuplicateOnceGitHubCatchesUp(t *testing.T) {
	f := newLagFixture()
	n := f.file(t, "caught up")

	if got := f.list(t); len(got) != 1 {
		t.Fatalf("while lagging want 1, got %v", numbers(got))
	}
	delete(f.gh.hidden, n)
	if got := numbers(f.list(t)); !equalInts(got, []int{n}) {
		t.Errorf("after catch-up = %v, want [%d] exactly once", got, n)
	}
	// And the entry is spent: hiding it again does not resurrect it.
	f.gh.hidden[n] = true
	if got := f.list(t); len(got) != 0 {
		t.Errorf("a spent entry came back: %v", numbers(got))
	}
}

func TestListIssues_MultipleRememberedStayNewestFirst(t *testing.T) {
	f := newLagFixture(IssueInfo{Number: 1, Title: "old", State: "open"})
	a := f.file(t, "a")
	b := f.file(t, "b")

	if got, want := numbers(f.list(t)), []int{b, a, 1}; !equalInts(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestListIssues_RememberedIssuesAreScopedToTheirRepo(t *testing.T) {
	f := newLagFixture()
	f.file(t, "mine")

	f.svc.repo = fakeRepoRepoAt{} // same store, now a different repository
	out, err := f.svc.ListIssues(context.Background(), "org", "proj", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("another repository listed %v", numbers(out))
	}
}

type fakeRepoRepoAt struct{ RepoRepository }

func (fakeRepoRepoAt) GetByOrgAndProjectID(_ context.Context, org, proj string) (*GitRepository, error) {
	return &GitRepository{OrgID: org, ProjectID: proj, RepoURL: "https://github.com/o/elsewhere"}, nil
}

// The platform's own writes during the lag window must show in the list.

func (f *lagFixture) only(t *testing.T) IssueInfo {
	t.Helper()
	got := f.list(t)
	if len(got) != 1 {
		t.Fatalf("want exactly the remembered issue, got %v", numbers(got))
	}
	return got[0]
}

func TestListIssues_RememberedIssueFollowsClose(t *testing.T) {
	f := newLagFixture()
	n := f.file(t, "to close")
	f.now = f.now.Add(5 * time.Second)

	if err := f.svc.CloseIssue(context.Background(), "org", "proj", n, ""); err != nil {
		t.Fatal(err)
	}
	got := f.only(t)
	if got.State != "closed" || got.StateReason != "completed" || got.ClosedAt != f.now.UTC().Format(time.RFC3339) {
		t.Errorf("after close: state=%q reason=%q closedAt=%q", got.State, got.StateReason, got.ClosedAt)
	}

	if err := f.svc.ReopenIssue(context.Background(), "org", "proj", n); err != nil {
		t.Fatal(err)
	}
	got = f.only(t)
	if got.State != "open" || got.StateReason != "" || got.ClosedAt != "" {
		t.Errorf("after reopen: state=%q reason=%q closedAt=%q", got.State, got.StateReason, got.ClosedAt)
	}
}

func TestListIssues_RememberedIssueFollowsEdits(t *testing.T) {
	f := newLagFixture()
	n := f.file(t, "old title")

	if err := f.svc.EditIssueTitle(context.Background(), "org", "proj", n, "new title"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.EditIssueBody(context.Background(), "org", "proj", n, "new body"); err != nil {
		t.Fatal(err)
	}
	got := f.only(t)
	if got.Title != "new title" || got.Body != "new body" {
		t.Errorf("after edits: title=%q body=%q", got.Title, got.Body)
	}
}

func TestRecentIssues_UpdateOfAnAbsentIssueIsANoOp(t *testing.T) {
	f := newLagFixture()
	f.svc.recent.update("o", "r", 99, func(i *IssueInfo) { i.Title = "ghost" })
	if got := f.list(t); len(got) != 0 {
		t.Errorf("update invented an entry: %v", numbers(got))
	}
}

func TestRecentIssues_SweepsQuietRepositories(t *testing.T) {
	r := &recentIssues{}
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	r.now = func() time.Time { return now }
	r.remember("o", "quiet", IssueInfo{Number: 1})
	now = now.Add(2 * recentIssueWindow)
	r.remember("o", "busy", IssueInfo{Number: 1})

	if len(r.byRepo) != 1 {
		t.Errorf("tracked repos = %d, want 1 (the quiet one swept)", len(r.byRepo))
	}
}

func TestCreateIssueDedup_SeesTheFirstDuringTheLag(t *testing.T) {
	f := newLagFixture()
	ctx := context.Background()
	first, err := f.svc.CreateIssue(ctx, "org", "proj", CreateIssueRequest{Title: "x", DedupeKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	f.gh.hidden[first.Number] = true // GitHub's list has not caught up

	second, err := f.svc.CreateIssue(ctx, "org", "proj", CreateIssueRequest{Title: "x again", DedupeKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Deduped || second.Number != first.Number {
		t.Errorf("second = %+v, want deduped to #%d", second, first.Number)
	}
	if f.gh.createCount != 1 {
		t.Errorf("GitHub creates = %d, want 1", f.gh.createCount)
	}
}
