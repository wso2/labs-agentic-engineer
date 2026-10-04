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

package sourcecontrol_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// widgets is the repository (org1, proj1) resolves to.
var widgets = sourcecontrol.RepoRef{Org: "org1", Owner: "acme", Repo: "widgets", DefaultBranch: "main"}

// newIssueSvcOnFake wires a REAL issueService at the in-memory pod. The repo
// row resolves (org1, proj1) → github.com/acme/widgets. Tasks are plain
// GitHub issues (Projects v2 dropped) — no board ops on creation.
func newIssueSvcOnFake(t *testing.T) (sourcecontrol.IssueService, *aestudiotest.Fake) {
	t.Helper()
	repo := newFakeRepoRepo()
	repo.preload(&sourcecontrol.GitRepository{
		OrgID: "org1", ProjectID: "proj1",
		RepoURL: "https://github.com/acme/widgets",
	})
	f := aestudiotest.New()
	return sourcecontrol.NewIssueService(repo, f), f
}

// ops lists the operations the Fake saw, in order.
func ops(f *aestudiotest.Fake) []string {
	var out []string
	for _, c := range f.Calls() {
		out = append(out, c.Op)
	}
	return out
}

// issueNo answers one issue of ref by number.
func issueNo(t *testing.T, f *aestudiotest.Fake, ref sourcecontrol.RepoRef, n int) sourcecontrol.IssueInfo {
	t.Helper()
	for _, is := range f.Issues(ref) {
		if is.Number == n {
			return is
		}
	}
	t.Fatalf("issue #%d not on %s/%s", n, ref.Owner, ref.Repo)
	return sourcecontrol.IssueInfo{}
}

// commentsOn answers every comment on issue n of ref, oldest first.
func commentsOn(t *testing.T, f *aestudiotest.Fake, ref sourcecontrol.RepoRef, n int) []sourcecontrol.IssueComment {
	t.Helper()
	cs, err := f.ListIssueComments(testContext(), ref, n, 100)
	if err != nil {
		t.Fatalf("comments on #%d: %v", n, err)
	}
	return cs
}

// A completed incident recurs: its evidence is appended to the issue body
// BEFORE the issue reopens, so a failed reopen never loses it.
func TestRecurrence_PreservesEvidenceBeforeReopen(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	ctx := sourcecontrol.WithIncidentContext(testContext(), "alert-123")
	first, err := svc.CreateIssue(ctx, "org1", "proj1", sourcecontrol.CreateIssueRequest{
		Title: "Recurring timeout", ComponentName: "checkout", Body: "Original RCA evidence",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.CloseIssue(testContext(), widgets, first.Number); err != nil {
		t.Fatal(err)
	}
	before := len(f.Calls())

	result, err := svc.CreateIssue(ctx, "org1", "proj1", sourcecontrol.CreateIssueRequest{
		Title: "Recurring timeout", ComponentName: "checkout", Body: "## Evidence\nNew timeout trace",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Reopened || result.Number != first.Number || result.RecurrenceCount != 1 || result.Adopted || result.AdoptionError == "" {
		t.Fatalf("result=%+v", result)
	}
	got := issueNo(t, f, widgets, first.Number)
	if !strings.HasPrefix(got.Body, "Original RCA evidence\n\n## Recurrence 1\n") || !strings.HasSuffix(got.Body, "## Evidence\nNew timeout trace") {
		t.Fatalf("evidence=%s", got.Body)
	}
	if got.State != "open" {
		t.Fatalf("state = %q, want open", got.State)
	}
	writes := slices.DeleteFunc(ops(f)[before:], func(op string) bool { return op == aestudiotest.OpListIssues })
	if want := []string{aestudiotest.OpEditIssueBody, aestudiotest.OpReopenIssue}; !slices.Equal(writes, want) {
		t.Fatalf("writes = %v, want %v (evidence before reopen)", writes, want)
	}
}

func TestCreateIssue_SendsTitleBodyLabelsAndParsesResult(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)

	res, err := svc.CreateIssue(testContext(), "org1", "proj1", sourcecontrol.CreateIssueRequest{
		Title:  "Implement auth",
		Body:   "do the thing",
		Labels: []string{"aep", "phase-1"},
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if res.Number != 1 || res.URL != "https://github.com/acme/widgets/issues/1" || res.NodeID == "" {
		t.Fatalf("result = %+v", res)
	}

	// The issue carries title/body/labels verbatim.
	got := issueNo(t, f, widgets, 1)
	if got.Title != "Implement auth" || got.Body != "do the thing" {
		t.Fatalf("issue title/body = %q/%q", got.Title, got.Body)
	}
	if strings.Join(got.Labels, ",") != "aep,phase-1" {
		t.Fatalf("issue labels = %v, want [aep phase-1]", got.Labels)
	}

	// Both labels are ensured up-front, before the create, with their mapped
	// colours.
	if want := []string{aestudiotest.OpEnsureLabel, aestudiotest.OpEnsureLabel, aestudiotest.OpCreateIssue}; !slices.Equal(ops(f), want) {
		t.Fatalf("ops = %v, want %v", ops(f), want)
	}
	if labels := f.Labels(widgets); labels["aep"] != "0075ca" || labels["phase-1"] != "ededed" || len(labels) != 2 {
		t.Fatalf("labels = %v", labels)
	}
}

func TestCreateIssue_TitleRequired(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	if _, err := svc.CreateIssue(testContext(), "org1", "proj1", sourcecontrol.CreateIssueRequest{Title: "  "}); err == nil {
		t.Fatal("want error for blank title, got nil")
	}
	if len(f.Calls()) != 0 {
		t.Fatalf("nothing must reach the pod, got %v", ops(f))
	}
}

func TestCreateIssue_RepoNotFound(t *testing.T) {
	t.Parallel()
	// A project with no row → resolveRef surfaces ErrRepoNotFound.
	f := aestudiotest.New()
	svc := sourcecontrol.NewIssueService(newFakeRepoRepo(), f)
	_, err := svc.CreateIssue(testContext(), "org1", "proj1", sourcecontrol.CreateIssueRequest{Title: "x"})
	if !errors.Is(err, sourcecontrol.ErrRepoNotFound) {
		t.Fatalf("err = %v, want ErrRepoNotFound", err)
	}
	if len(f.Calls()) != 0 {
		t.Fatalf("nothing must reach the pod, got %v", ops(f))
	}
}

// The pod's answer for an org whose AE Studio is down or not connected
// reaches the caller as is (no fallback), so the edge can map it.
func TestCreateIssue_PodFailureReachesTheCaller(t *testing.T) {
	t.Parallel()
	for _, want := range []error{sourcecontrol.ErrAEStudioUnavailable, sourcecontrol.ErrAEStudioAbsent} {
		svc, f := newIssueSvcOnFake(t)
		f.FailOrg("org1", want)
		if _, err := svc.CreateIssue(testContext(), "org1", "proj1", sourcecontrol.CreateIssueRequest{Title: "x"}); !errors.Is(err, want) {
			t.Fatalf("err = %v, want %v", err, want)
		}
	}
}

// openIssue files one plain issue through the Fake and answers its number.
func openIssue(t *testing.T, f *aestudiotest.Fake) int {
	t.Helper()
	res, err := f.CreateIssue(testContext(), widgets, sourcecontrol.CreateIssueRequest{Title: "seed"})
	if err != nil {
		t.Fatal(err)
	}
	return res.Number
}

func TestCloseIssue_CommentsThenCloses(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	n := openIssue(t, f)

	if err := svc.CloseIssue(testContext(), "org1", "proj1", n, "closing this out"); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	// Read the call log before the assertions below add their own reads.
	if got, want := ops(f), []string{aestudiotest.OpCreateIssue, aestudiotest.OpCommentIssue, aestudiotest.OpCloseIssue}; !slices.Equal(got, want) {
		t.Fatalf("ops = %v, want the comment before the close", got)
	}
	cs := commentsOn(t, f, widgets, n)
	// The prose is the caller's; the machine brand rides in front of it (see
	// TestCloseIssue_ClosingCommentIsBrandedAsMachine).
	if len(cs) != 1 || !strings.Contains(cs[0].Body, "closing this out") {
		t.Fatalf("comments = %+v", cs)
	}
	got := issueNo(t, f, widgets, n)
	if got.State != "closed" || got.StateReason != "completed" {
		t.Fatalf("issue = %+v, want closed completed", got)
	}
}

func TestCloseIssue_NoCommentWhenBlank(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	n := openIssue(t, f)

	if err := svc.CloseIssue(testContext(), "org1", "proj1", n, "   "); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	if got := commentsOn(t, f, widgets, n); len(got) != 0 {
		t.Fatalf("comment posted despite blank comment: %+v", got)
	}
	if issueNo(t, f, widgets, n).State != "closed" {
		t.Fatal("issue not closed")
	}
}

// Every platform comment is BRANDED as machine-written on the way out. This
// service is the only adapter platform comment writes pass through and there is
// no user-facing comment write on the API, so the brand here is exactly the
// statement "the platform wrote this" — which is the only way a reader can tell
// it from the coding agent's own notes (both post under the org's credential).
func TestCommentIssue_PostsBodyBrandedAsMachine(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	n := openIssue(t, f)

	if err := svc.CommentIssue(testContext(), "org1", "proj1", n, "a comment"); err != nil {
		t.Fatalf("CommentIssue: %v", err)
	}
	cs := commentsOn(t, f, widgets, n)
	if len(cs) != 1 || !strings.HasPrefix(cs[0].Body, sourcecontrol.MachineCommentMarker) {
		t.Fatalf("comment not branded: %+v", cs)
	}
	if !strings.Contains(cs[0].Body, "a comment") {
		t.Fatalf("the caller's prose did not survive branding: %q", cs[0].Body)
	}
}

// A closing comment is a platform comment too, and takes the same brand — it was
// the most visible machine comment on an issue before this existed.
func TestCloseIssue_ClosingCommentIsBrandedAsMachine(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	n := openIssue(t, f)

	if err := svc.CloseIssue(testContext(), "org1", "proj1", n, "✅ Provisioned."); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	if cs := commentsOn(t, f, widgets, n); len(cs) != 1 || !strings.HasPrefix(cs[0].Body, sourcecontrol.MachineCommentMarker) {
		t.Fatalf("closing comment not branded: %+v", cs)
	}
}

// Branding is idempotent. A caller may compose a body from another platform
// comment, and a retry may re-send one — neither may stack markers, which would
// leave a visible artefact once the read strips only what it expects.
func TestCommentIssue_BrandingDoesNotStack(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	n := openIssue(t, f)

	already := sourcecontrol.MachineCommentMarker + "\nalready branded"
	if err := svc.CommentIssue(testContext(), "org1", "proj1", n, already); err != nil {
		t.Fatalf("CommentIssue: %v", err)
	}
	cs := commentsOn(t, f, widgets, n)
	if c := strings.Count(cs[0].Body, sourcecontrol.MachineCommentMarker); c != 1 {
		t.Fatalf("marker appears %d times, want 1: %q", c, cs[0].Body)
	}
}

// A platform comment that QUOTES the marker further down is not already branded,
// and must still get one. The check is a prefix test for exactly this reason: the
// read side detects on the prefix too, so treating a mention as a brand would
// leave the body unbranded AND unhidden — the platform's own text surfacing in a
// feed built to exclude it.
func TestCommentIssue_BrandsABodyThatMerelyQuotesTheMarker(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	n := openIssue(t, f)

	quoting := "the earlier note said:\n\n> " + sourcecontrol.MachineCommentMarker + "\n> done"
	if err := svc.CommentIssue(testContext(), "org1", "proj1", n, quoting); err != nil {
		t.Fatalf("CommentIssue: %v", err)
	}
	if cs := commentsOn(t, f, widgets, n); !strings.HasPrefix(cs[0].Body, sourcecontrol.MachineCommentMarker) {
		t.Fatalf("a body quoting the marker was left unbranded: %q", cs[0].Body)
	}
}

func TestCommentIssue_BodyRequired(t *testing.T) {
	t.Parallel()
	svc, _ := newIssueSvcOnFake(t)
	if err := svc.CommentIssue(testContext(), "org1", "proj1", 7, "  "); err == nil {
		t.Fatal("want error for blank comment body, got nil")
	}
}

func TestListIssues_FiltersByLabelAndProjectsAttention(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	if _, err := f.CreateIssue(testContext(), widgets, sourcecontrol.CreateIssueRequest{Title: "T1", Body: "B1", Labels: []string{"aep", "phase-1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.CreateIssue(testContext(), widgets, sourcecontrol.CreateIssueRequest{Title: "other", Labels: []string{"bug"}}); err != nil {
		t.Fatal(err)
	}

	issues, err := svc.ListIssues(testContext(), "org1", "proj1", []string{"aep"})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1", len(issues))
	}
	got := issues[0]
	if got.Number != 1 || got.Title != "T1" || got.Body != "B1" || got.State != "open" || strings.Join(got.Labels, ",") != "aep,phase-1" {
		t.Fatalf("issue = %+v", got)
	}
}

func TestEditIssueBody_ReplacesBody(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	n := openIssue(t, f)

	if err := svc.EditIssueBody(testContext(), "org1", "proj1", n, "replacement body"); err != nil {
		t.Fatalf("EditIssueBody: %v", err)
	}
	if got := issueNo(t, f, widgets, n).Body; got != "replacement body" {
		t.Fatalf("edit body = %q", got)
	}
}

// TestSetIssueMilestone_MovesTheIssue pins adoption's write: the milestone
// travels as a NUMBER (GitHub 422s a title here, and the number is the only
// stable key).
func TestSetIssueMilestone_MovesTheIssue(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)
	n := openIssue(t, f)
	m, err := f.CreateMilestone(testContext(), widgets, sourcecontrol.CreateMilestoneRequest{Title: "v1"})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.SetIssueMilestone(testContext(), "org1", "proj1", n, m.Number); err != nil {
		t.Fatalf("SetIssueMilestone: %v", err)
	}
	members, err := f.ListMilestoneIssues(testContext(), widgets, sourcecontrol.MilestoneIssuesFilter{Number: m.Number})
	if err != nil || len(members) != 1 || members[0].Number != n {
		t.Fatalf("milestone members = %+v err=%v", members, err)
	}
}

func TestSetIssueMilestone_NumberRequired(t *testing.T) {
	t.Parallel()
	svc, f := newIssueSvcOnFake(t)

	if err := svc.SetIssueMilestone(testContext(), "org1", "proj1", 7, 0); err == nil {
		t.Fatal("a zero milestone number must be refused before any request")
	}
	if len(f.Calls()) != 0 {
		t.Fatalf("nothing must be sent, got %v", ops(f))
	}
}

func TestParseOwnerRepo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in          string
		owner, repo string
		wantErr     bool
	}{
		{"https://github.com/acme/widgets", "acme", "widgets", false},
		{"https://github.com/acme/widgets.git", "acme", "widgets", false},
		{"git@github.com:acme/widgets.git", "acme", "widgets", false},
		{"https://github.com/acme/widgets/", "acme", "widgets", false},
		{"https://github.com/acme/widgets.git/", "acme", "widgets", false},
		{"  https://github.com/acme/widgets  ", "acme", "widgets", false},
		{"https://ghe.example.com/acme/widgets.git", "acme", "widgets", false},
		{"https://gitlab.com/aep/repo", "aep", "repo", false},
		{"not-a-url", "", "", true},
		{"", "", "", true},
		{"acme/widgets", "", "", true},
		{"https://github.com/onlyowner", "", "", true},
		{"https://github.com/acme/widgets/extra", "", "", true},
		{"https://github.com/acme/widgets/tree/main", "", "", true},
		{"https://github.com/", "", "", true},
		{"https://", "", "", true},
		{"https:", "", "", true},
		{"https:///acme/widgets", "", "", true},
		{"https://gitlab.com/aep", "", "", true},
		{"git@github.com:onlyowner", "", "", true},
	}
	for _, c := range cases {
		owner, repo, err := sourcecontrol.ParseOwnerRepo(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseOwnerRepo(%q): want error, got %s/%s", c.in, owner, repo)
			}
			continue
		}
		if err != nil || owner != c.owner || repo != c.repo {
			t.Errorf("ParseOwnerRepo(%q) = %s/%s (err %v), want %s/%s", c.in, owner, repo, err, c.owner, c.repo)
		}
	}
}
