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

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/gen"
	"github.com/wso2/aep/ae-studio-tools/internal/github/githubtest"
)

// quietLogs sends the package's log lines to a buffer for the test and
// answers it.
func quietLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// events is the contract's hook events.
func events(names ...string) []gen.HookEventsRequestEvents {
	out := make([]gen.HookEventsRequestEvents, 0, len(names))
	for _, n := range names {
		out = append(out, gen.HookEventsRequestEvents(n))
	}
	return out
}

// problemOf is the problem answer a handler gave, failing the test when the
// answer is not one.
func problemOf(t *testing.T, resp any) problemReply {
	t.Helper()
	p, ok := resp.(problemReply)
	if !ok {
		t.Fatalf("got %#v, want a problem answer", resp)
	}
	return p
}

func TestIssueHandlers_MapGitHubAnswers(t *testing.T) {
	quietLogs(t)
	gh := githubtest.NewStub(t)
	gh.SeedIssue("acme", "greeter", 7, "Task A", []string{"aep"})
	h := NewHandler(New(Config{APIBase: gh.URL(), Token: staticToken("t")}))
	ctx := context.Background()

	got, err := h.GetIssue(ctx, gen.GetIssueRequestObject{Owner: "acme", Repo: "greeter", Number: 7})
	if err != nil {
		t.Fatal(err)
	}
	if got.(gen.GetIssue200JSONResponse).Title != "Task A" {
		t.Fatalf("got %#v", got)
	}
	missing, _ := h.GetIssue(ctx, gen.GetIssueRequestObject{Owner: "acme", Repo: "greeter", Number: 999})
	if p := problemOf(t, missing); p.Status != http.StatusNotFound || p.Code != "issue_not_found" {
		t.Fatalf("got %#v, want 404 issue_not_found", missing)
	}
	gh.FailNext(http.StatusTooManyRequests, map[string]string{"Retry-After": "7"})
	limited, _ := h.ListIssues(ctx, gen.ListIssuesRequestObject{Owner: "acme", Repo: "greeter"})
	if p := problemOf(t, limited); p.Status != http.StatusTooManyRequests || p.Code != "github_rate_limited" || p.RetryAfter != 7 {
		t.Fatalf("got %#v, want 429 with Retry-After 7", limited)
	}
}

// TestHandler_ProblemOnTheWire: a problem answer is application/problem+json
// with the status, the code and, on a rate limit, Retry-After.
func TestHandler_ProblemOnTheWire(t *testing.T) {
	quietLogs(t)
	gh := githubtest.NewStub(t)
	h := NewHandler(New(Config{APIBase: gh.URL(), Token: staticToken("t")}))
	gh.FailNext(http.StatusTooManyRequests, map[string]string{"Retry-After": "7"})
	resp, _ := h.ListIssues(context.Background(), gen.ListIssuesRequestObject{Owner: "acme", Repo: "greeter"})
	rec := httptest.NewRecorder()
	if err := resp.VisitListIssuesResponse(rec); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "7" ||
		rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("status %d, headers %v", rec.Code, rec.Header())
	}
	var body gen.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != "github_rate_limited" || body.Status != 429 {
		t.Fatalf("body %s (%v)", rec.Body, err)
	}
}

// TestHandler_ErrorMap is problemFor: sentinels are 404s, a rate limit
// (REST or GraphQL) 429 with Retry-After (60 when GitHub named none), any
// other GitHub answer 502 github_error with githubStatus (a GraphQL
// NOT_FOUND is 404), a transport failure 502 without one, and a caller that
// left gets its ctx error. The log line names the op and the statuses, never
// GitHub's text.
func TestHandler_ErrorMap(t *testing.T) {
	logs := quietLogs(t)
	h := Handler{}
	ctx := context.Background()
	for _, tc := range []struct {
		name         string
		err          error
		status       int
		code         string
		retryAfter   int
		githubStatus int
	}{
		{"issue", ErrIssueNotFound, 404, "issue_not_found", 0, 0},
		{"milestone", ErrMilestoneNotFound, 404, "milestone_not_found", 0, 0},
		{"rest rate limit", &HTTPStatusError{StatusCode: 429, RetryAfter: 1500 * time.Millisecond}, 429, "github_rate_limited", 2, 0},
		{"rest rate limit no wait", &HTTPStatusError{StatusCode: 429}, 429, "github_rate_limited", 60, 0},
		{"graphql rate limit", &GraphQLError{Errors: []GraphQLErrorDetail{{Type: "RATE_LIMITED"}}}, 429, "github_rate_limited", 60, 0},
		{"rest refusal", &HTTPStatusError{StatusCode: 422, Body: "gh-said-this"}, 502, "github_error", 0, 422},
		{"graphql not found", &GraphQLError{Errors: []GraphQLErrorDetail{{Type: "NOT_FOUND"}}}, 502, "github_error", 0, 404},
		{"graphql other", &GraphQLError{Errors: []GraphQLErrorDetail{{Type: "FORBIDDEN"}}}, 502, "github_error", 0, 0},
		{"transport", context.DeadlineExceeded, 502, "github_error", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := h.problem(ctx, "op", "acme", "greeter", tc.err)
			if err != nil {
				t.Fatal(err)
			}
			if p.Status != tc.status || p.Code != tc.code || p.RetryAfter != tc.retryAfter || p.GithubStatus != tc.githubStatus {
				t.Fatalf("got %+v", p)
			}
		})
	}
	if out := logs.String(); strings.Contains(out, "gh-said-this") || !strings.Contains(out, "github.call_failed") {
		t.Fatalf("logs %q: want github.call_failed without GitHub's text", out)
	}
	gone, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := h.problem(gone, "op", "acme", "greeter", ErrIssueNotFound); err == nil {
		t.Fatal("a cancelled caller must get its ctx error")
	}
}

// TestIssueHandlers_SendWhatGitHubExpects drives every write op once and
// checks the GitHub call it made.
func TestIssueHandlers_SendWhatGitHubExpects(t *testing.T) {
	gh := githubtest.NewStub(t)
	base := "/repos/acme/greeter"
	gh.On("POST", base+"/issues", 201, `{"number":12,"html_url":"https://github.com/acme/greeter/issues/12","node_id":"I_12"}`)
	for _, route := range []string{"PATCH " + base + "/issues/7", "PUT " + base + "/issues/7/labels", "POST " + base + "/issues/7/labels", "DELETE " + base + "/issues/7/labels/src/validation", "PATCH " + base + "/milestones/3"} {
		method, path, _ := strings.Cut(route, " ")
		gh.On(method, path, 200, `{}`)
	}
	gh.On("POST", base+"/issues/7/comments", 201, `{}`)
	gh.On("POST", base+"/labels", 422, `{"message":"Validation Failed","errors":[{"code":"already_exists"}]}`)
	gh.On("PUT", base+"/pulls/5/merge", 200, `{}`)
	h := NewHandler(New(Config{APIBase: gh.URL(), Token: staticToken("t")}))
	ctx := context.Background()
	ms := 3
	created, err := h.CreateIssue(ctx, gen.CreateIssueRequestObject{Owner: "acme", Repo: "greeter",
		Body: &gen.CreateIssueRequest{Title: "T", Body: "B", Labels: []string{"aep"}, Milestone: &ms}})
	if err != nil {
		t.Fatal(err)
	}
	if r := created.(gen.CreateIssue201JSONResponse); r.Number != 12 || r.NodeID != "I_12" || r.URL == "" {
		t.Fatalf("created %+v", r)
	}
	type call struct {
		name string
		do   func() (any, error)
		want string // "METHOD path body-fragment"
	}
	n := gen.Number(7)
	for _, c := range []call{
		{"comment", func() (any, error) {
			return h.CreateIssueComment(ctx, gen.CreateIssueCommentRequestObject{Owner: "acme", Repo: "greeter", Number: n, Body: &gen.IssueCommentRequest{Body: "hi"}})
		}, `POST /issues/7/comments "body":"hi"`},
		{"close", func() (any, error) {
			return h.CloseIssue(ctx, gen.CloseIssueRequestObject{Owner: "acme", Repo: "greeter", Number: n})
		}, `PATCH /issues/7 "state":"closed"`},
		{"reopen", func() (any, error) {
			return h.ReopenIssue(ctx, gen.ReopenIssueRequestObject{Owner: "acme", Repo: "greeter", Number: n})
		}, `PATCH /issues/7 "state":"open"`},
		{"body", func() (any, error) {
			return h.SetIssueBody(ctx, gen.SetIssueBodyRequestObject{Owner: "acme", Repo: "greeter", Number: n, Body: &gen.IssueBodyRequest{Body: "nb"}})
		}, `PATCH /issues/7 "body":"nb"`},
		{"title", func() (any, error) {
			return h.SetIssueTitle(ctx, gen.SetIssueTitleRequestObject{Owner: "acme", Repo: "greeter", Number: n, Body: &gen.IssueTitleRequest{Title: "nt"}})
		}, `PATCH /issues/7 "title":"nt"`},
		{"milestone", func() (any, error) {
			return h.SetIssueMilestone(ctx, gen.SetIssueMilestoneRequestObject{Owner: "acme", Repo: "greeter", Number: n, Body: &gen.IssueMilestoneRequest{Number: 3}})
		}, `PATCH /issues/7 "milestone":3`},
		{"add labels", func() (any, error) {
			return h.AddIssueLabels(ctx, gen.AddIssueLabelsRequestObject{Owner: "acme", Repo: "greeter", Number: n, Body: &gen.IssueLabelsRequest{Labels: []string{"x"}}})
		}, `POST /issues/7/labels "labels":["x"]`},
		{"set labels empty", func() (any, error) {
			return h.SetIssueLabels(ctx, gen.SetIssueLabelsRequestObject{Owner: "acme", Repo: "greeter", Number: n, Body: &gen.IssueLabelsRequest{}})
		}, `PUT /issues/7/labels "labels":[]`},
		{"remove label", func() (any, error) {
			return h.RemoveIssueLabel(ctx, gen.RemoveIssueLabelRequestObject{Owner: "acme", Repo: "greeter", Number: n, Label: "src/validation"})
		}, `DELETE /issues/7/labels/src/validation `},
		{"ensure label", func() (any, error) {
			return h.EnsureLabel(ctx, gen.EnsureLabelRequestObject{Owner: "acme", Repo: "greeter", Body: &gen.EnsureLabelRequest{Name: "aep", Color: "ededed"}})
		}, `POST /labels "color":"ededed"`},
		{"close milestone", func() (any, error) {
			return h.CloseMilestone(ctx, gen.CloseMilestoneRequestObject{Owner: "acme", Repo: "greeter", Number: 3})
		}, `PATCH /milestones/3 "state":"closed"`},
		{"reopen milestone", func() (any, error) {
			return h.ReopenMilestone(ctx, gen.ReopenMilestoneRequestObject{Owner: "acme", Repo: "greeter", Number: 3})
		}, `PATCH /milestones/3 "state":"open"`},
		{"merge", func() (any, error) {
			return h.MergePull(ctx, gen.MergePullRequestObject{Owner: "acme", Repo: "greeter", Number: 5})
		}, `PUT /pulls/5/merge "merge_method":"squash"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			resp, err := c.do()
			if err != nil {
				t.Fatal(err)
			}
			if _, isProblem := resp.(problemReply); isProblem {
				t.Fatalf("got problem %#v", resp)
			}
			reqs := gh.Requests()
			last := reqs[len(reqs)-1]
			method, rest, _ := strings.Cut(c.want, " ")
			path, fragment, _ := strings.Cut(rest, " ")
			if last.Method != method || last.Path != base+path || !strings.Contains(last.Body, fragment) {
				t.Fatalf("GitHub got %s %s %s, want %s", last.Method, last.Path, last.Body, c.want)
			}
		})
	}
}

// TestReadHandlers_ProjectGitHubAnswers covers the reads' shapes: lists,
// milestones, counts, comments (machine/observed reported), pulls.
func TestReadHandlers_ProjectGitHubAnswers(t *testing.T) {
	gh := githubtest.NewStub(t)
	base := "/repos/acme/greeter"
	gh.SeedIssue("acme", "greeter", 7, "Task A", []string{"aep", "development"})
	gh.SeedIssue("acme", "greeter", 8, "Task B", []string{"aep"})
	gh.On("GET", base+"/milestones", 200, `[{"number":3,"title":"v1","state":"open","description":"d","node_id":"M_3"}]`)
	gh.On("GET", base+"/pulls/5", 200, `{"state":"closed","merged":true,"merge_commit_sha":"abc"}`)
	gh.On("GET", base+"/pulls/5/files", 200, `[{"filename":"a.go"},{"filename":"b/c.go"}]`)
	gh.OnFunc("POST", "/graphql", func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&q)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(q.Query, "totalCount"):
			_, _ = w.Write([]byte(`{"data":{"repository":{"milestone":{"provision":{"totalCount":1},"allOpen":{"totalCount":6},"agentWork":{"totalCount":5},"development":{"totalCount":3},"validation":{"totalCount":1},"srcValidation":{"totalCount":2}}}}}`))
		case strings.Contains(q.Query, "milestone(number"):
			_, _ = w.Write([]byte(`{"data":{"repository":{"milestone":{"issues":{"pageInfo":{"hasNextPage":false},"nodes":[{"number":7,"comments":{"nodes":[{"id":"C1","author":{"login":"bot"},"body":"<!-- aep:observed -->ran","url":"u","createdAt":"2026-10-01T10:00:00Z"}]}},{"number":8,"comments":{"nodes":[]}}]}}}}}`))
		default:
			_, _ = w.Write([]byte(`{"data":{"repository":{"issue":{"comments":{"nodes":[{"id":"C2","author":null,"body":"<!-- aep:machine -->note","url":"u2","createdAt":"2026-10-01T11:00:00Z"}]}}}}}`))
		}
	})
	h := NewHandler(New(Config{APIBase: gh.URL(), Token: staticToken("t")}))
	ctx := context.Background()

	list, _ := h.ListIssues(ctx, gen.ListIssuesRequestObject{Owner: "acme", Repo: "greeter", Params: gen.ListIssuesParams{Labels: []string{"development"}}})
	if l := list.(gen.ListIssues200JSONResponse); len(l.Issues) != 1 || l.Issues[0].Number != 7 || !slices.Equal(l.Issues[0].Labels, []string{"aep", "development"}) {
		t.Fatalf("list %+v", list)
	}
	empty, _ := h.ListIssues(ctx, gen.ListIssuesRequestObject{Owner: "acme", Repo: "greeter", Params: gen.ListIssuesParams{Labels: []string{"none"}}})
	if l := empty.(gen.ListIssues200JSONResponse); l.Issues == nil {
		t.Fatal("an empty list must be [] on the wire, not null")
	}
	mss, _ := h.ListMilestones(ctx, gen.ListMilestonesRequestObject{Owner: "acme", Repo: "greeter"})
	if l := mss.(gen.ListMilestones200JSONResponse); len(l.Milestones) != 1 || l.Milestones[0].NodeID != "M_3" {
		t.Fatalf("milestones %+v", mss)
	}
	if q := gh.Requests()[len(gh.Requests())-1].Query; !strings.Contains(q, "state=all") {
		t.Fatalf("milestones query %q, want state=all when omitted", q)
	}
	counts, _ := h.GetMilestoneCounts(ctx, gen.GetMilestoneCountsRequestObject{Owner: "acme", Repo: "greeter", Number: 3})
	if c := counts.(gen.GetMilestoneCounts200JSONResponse); c != (gen.GetMilestoneCounts200JSONResponse{OpenProvision: 1, OpenTotal: 6, OpenAgentWork: 5, OpenDevelopment: 3, OpenValidation: 1, OpenValidationRepairs: 2}) {
		t.Fatalf("counts %+v", c)
	}
	mc, _ := h.ListMilestoneComments(ctx, gen.ListMilestoneCommentsRequestObject{Owner: "acme", Repo: "greeter", Number: 3, Params: gen.ListMilestoneCommentsParams{PerIssue: 5}})
	if l := mc.(gen.ListMilestoneComments200JSONResponse); len(l.Issues) != 1 || l.Issues[0].Number != 7 || !l.Issues[0].Comments[0].Observed || l.Issues[0].Comments[0].Body != "ran" {
		t.Fatalf("milestone comments %+v", mc)
	}
	ic, _ := h.ListIssueComments(ctx, gen.ListIssueCommentsRequestObject{Owner: "acme", Repo: "greeter", Number: 7, Params: gen.ListIssueCommentsParams{Limit: 5}})
	if l := ic.(gen.ListIssueComments200JSONResponse); len(l.Comments) != 1 || !l.Comments[0].Machine || l.Comments[0].Author != "" || l.Comments[0].Body != "note" {
		t.Fatalf("issue comments %+v", ic)
	}
	none, _ := h.ListIssueComments(ctx, gen.ListIssueCommentsRequestObject{Owner: "acme", Repo: "greeter", Number: 7, Params: gen.ListIssueCommentsParams{Limit: 0}})
	if l := none.(gen.ListIssueComments200JSONResponse); l.Comments == nil || len(l.Comments) != 0 {
		t.Fatalf("limit 0 = %+v, want []", none)
	}
	pr, _ := h.GetPull(ctx, gen.GetPullRequestObject{Owner: "acme", Repo: "greeter", Number: 5})
	if p := pr.(gen.GetPull200JSONResponse); p != (gen.GetPull200JSONResponse{State: "closed", Merged: true, MergeCommitSha: "abc"}) {
		t.Fatalf("pull %+v", p)
	}
	files, _ := h.ListPullFiles(ctx, gen.ListPullFilesRequestObject{Owner: "acme", Repo: "greeter", Number: 5})
	if f := files.(gen.ListPullFiles200JSONResponse); !slices.Equal(f.Files, []string{"a.go", "b/c.go"}) {
		t.Fatalf("files %+v", f)
	}
}

func TestCreateRepo_OwnerGuardAndAdopt(t *testing.T) {
	quietLogs(t)
	gh := githubtest.NewStub(t)
	gh.SeedRepo("acme", "e2e-reference")
	h := NewHandler(New(Config{APIBase: gh.URL(), Token: staticToken("t")}), WithOwner("acme"))
	ctx := context.Background()

	other, _ := h.CreateRepo(ctx, gen.CreateRepoRequestObject{Body: &gen.CreateRepoJSONRequestBody{Owner: "evil", Name: "x"}})
	if p := problemOf(t, other); p.Status != http.StatusForbidden || p.Code != "owner_not_allowed" {
		t.Fatalf("got %#v, want 403 owner_not_allowed", other)
	}
	dup, _ := h.CreateRepo(ctx, gen.CreateRepoRequestObject{Body: &gen.CreateRepoJSONRequestBody{Owner: "acme", Name: "e2e-reference"}})
	if p := problemOf(t, dup); p.Status != http.StatusConflict || p.Code != "repo_name_conflict" {
		t.Fatalf("got %#v, want 409 repo_name_conflict", dup)
	}
	adopted, _ := h.CreateRepo(ctx, gen.CreateRepoRequestObject{Body: &gen.CreateRepoJSONRequestBody{Owner: "acme", Name: "e2e-reference", AdoptExisting: true}})
	if r, ok := adopted.(gen.CreateRepo200JSONResponse); !ok || r.CloneURL != "https://github.com/acme/e2e-reference.git" {
		t.Fatalf("got %#v, want adopted coordinates", adopted)
	}
}

func TestRegisterHook_IsAnEnsure(t *testing.T) {
	gh := githubtest.NewStub(t)
	gh.SeedRepo("acme", "greeter")
	h := NewHandler(New(Config{APIBase: gh.URL(), Token: staticToken("t"), HookURL: "https://x/webhooks/github", HookSecret: "s"}), WithOwner("acme"))
	first, _ := h.RegisterHook(context.Background(), gen.RegisterHookRequestObject{Owner: "acme", Repo: "greeter", Body: &gen.RegisterHookJSONRequestBody{Events: events("push")}})
	second, _ := h.RegisterHook(context.Background(), gen.RegisterHookRequestObject{Owner: "acme", Repo: "greeter", Body: &gen.RegisterHookJSONRequestBody{Events: events("push", "issues")}})
	a, b := first.(gen.RegisterHook200JSONResponse), second.(gen.RegisterHook200JSONResponse)
	if a.ID != b.ID || !slices.Equal(gh.HookEvents("acme", "greeter", a.ID), []string{"push", "issues"}) {
		t.Fatalf("second register must reuse hook %d and patch its events", a.ID)
	}
}

// TestRegisterHook_CreatesWithoutAPatch: a new hook is created with the
// events and the URL and secret are the pod's; no PATCH follows.
func TestRegisterHook_CreatesWithoutAPatch(t *testing.T) {
	gh := githubtest.NewStub(t)
	gh.SeedRepo("acme", "greeter")
	h := NewHandler(New(Config{APIBase: gh.URL(), Token: staticToken("t"), HookURL: "https://x/webhooks/github", HookSecret: "s"}), WithOwner("acme"))
	resp, _ := h.RegisterHook(context.Background(), gen.RegisterHookRequestObject{Owner: "acme", Repo: "greeter", Body: &gen.RegisterHookJSONRequestBody{Events: events("push", "issues")}})
	id := resp.(gen.RegisterHook200JSONResponse).ID
	if !slices.Equal(gh.HookEvents("acme", "greeter", id), []string{"push", "issues"}) {
		t.Fatalf("events %v", gh.HookEvents("acme", "greeter", id))
	}
	for _, r := range gh.Requests() {
		if r.Method == http.MethodPatch {
			t.Fatal("a created hook needs no PATCH")
		}
		if r.Method == http.MethodPost && !strings.Contains(r.Body, `"url":"https://x/webhooks/github"`) {
			t.Fatalf("register body %s", r.Body)
		}
	}
}

// TestHookEvents_Delete: delete is 204, also when GitHub no longer has the
// hook.
func TestHookEvents_Delete(t *testing.T) {
	gh := githubtest.NewStub(t)
	gh.SeedRepo("acme", "greeter")
	h := NewHandler(New(Config{APIBase: gh.URL(), Token: staticToken("t"), HookURL: "https://x/webhooks/github", HookSecret: "s"}), WithOwner("acme"))
	ctx := context.Background()
	resp, _ := h.RegisterHook(ctx, gen.RegisterHookRequestObject{Owner: "acme", Repo: "greeter", Body: &gen.RegisterHookJSONRequestBody{Events: events("push")}})
	id := resp.(gen.RegisterHook200JSONResponse).ID
	for range 2 { // the second finds no hook (GitHub 404): still 204
		del, _ := h.DeleteHook(ctx, gen.DeleteHookRequestObject{Owner: "acme", Repo: "greeter", HookID: id})
		if _, ok := del.(gen.DeleteHook204Response); !ok {
			t.Fatalf("delete %#v", del)
		}
	}
}

// TestCreateRepo_Creates: a new name is 201 with GitHub's coordinates; the
// owner compares case-insensitively; the repo is created initialised.
func TestCreateRepo_Creates(t *testing.T) {
	gh := githubtest.NewStub(t)
	h := NewHandler(New(Config{APIBase: gh.URL(), Token: staticToken("t")}), WithOwner("Acme"))
	resp, _ := h.CreateRepo(context.Background(), gen.CreateRepoRequestObject{Body: &gen.CreateRepoJSONRequestBody{Owner: "acme", Name: "greeter", Private: true, Description: "d"}})
	r, ok := resp.(gen.CreateRepo201JSONResponse)
	if !ok || r != (gen.CreateRepo201JSONResponse{Owner: "acme", Repo: "greeter", CloneURL: "https://github.com/acme/greeter.git", DefaultBranch: "main"}) {
		t.Fatalf("got %#v", resp)
	}
	body := gh.Requests()[0].Body
	for _, want := range []string{`"auto_init":true`, `"private":true`, `"description":"d"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("create body %s lacks %s", body, want)
		}
	}
}

// TestCreateRepo_GuardRunsBeforeAnyCall: a foreign owner (or an unset
// connected owner) never reaches GitHub, so the client's /user/repos
// fallback cannot create a repository under the gitpat's own account for
// anyone but the connected owner.
func TestCreateRepo_GuardRunsBeforeAnyCall(t *testing.T) {
	gh := githubtest.NewStub(t)
	gh.On("POST", "/user/repos", 201, `{"name":"x","owner":{"login":"gitpat-user"},"default_branch":"main"}`)
	for _, connected := range []string{"acme", ""} {
		h := NewHandler(New(Config{APIBase: gh.URL(), Token: staticToken("t")}), WithOwner(connected))
		resp, _ := h.CreateRepo(context.Background(), gen.CreateRepoRequestObject{Body: &gen.CreateRepoJSONRequestBody{Owner: "evil", Name: "x", AdoptExisting: true}})
		if p := problemOf(t, resp); p.Code != "owner_not_allowed" {
			t.Fatalf("connected %q: %#v", connected, resp)
		}
	}
	if n := len(gh.Requests()); n != 0 {
		t.Fatalf("a refused owner reached GitHub %d times", n)
	}
}

// TestCreateRepo_NeverCreatesUnderAnotherAccount: when /orgs/{owner}/repos
// is 404 and the gitpat's user is not the owner (the gitpat cannot see the
// org), nothing is created: no POST /user/repos, and the answer is 502
// github_error with githubStatus 404. When the gitpat's user is the owner,
// the /user/repos fallback is that account and is accepted. A repository
// GitHub still answers under another owner is refused (defence in depth).
func TestCreateRepo_NeverCreatesUnderAnotherAccount(t *testing.T) {
	logs := quietLogs(t)
	gh := githubtest.NewStub(t)
	gh.On("POST", "/orgs/acme/repos", 404, `{"message":"Not Found"}`)
	gh.On("GET", "/user", 200, `{"login":"gitpat-user","id":7}`)
	gh.On("POST", "/user/repos", 201, `{"name":"x","owner":{"login":"gitpat-user"},"default_branch":"main"}`)
	h := NewHandler(New(Config{APIBase: gh.URL(), Token: staticToken("t")}), WithOwner("acme"))
	resp, _ := h.CreateRepo(context.Background(), gen.CreateRepoRequestObject{Body: &gen.CreateRepoJSONRequestBody{Owner: "acme", Name: "x"}})
	if p := problemOf(t, resp); p.Status != http.StatusBadGateway || p.Code != "github_error" || p.GithubStatus != http.StatusNotFound {
		t.Fatalf("got %#v, want 502 github_error githubStatus 404", resp)
	}
	for _, r := range gh.Requests() {
		if r.Method == http.MethodPost && r.Path == "/user/repos" {
			t.Fatal("POST /user/repos made for an owner that is not the gitpat's user")
		}
	}

	// The connected owner is the gitpat's user: the fallback is its own
	// account, accepted.
	gh.On("GET", "/user", 200, `{"login":"Acme","id":7}`)
	gh.On("POST", "/user/repos", 201, `{"name":"x","owner":{"login":"Acme"},"default_branch":"trunk"}`)
	resp, _ = h.CreateRepo(context.Background(), gen.CreateRepoRequestObject{Body: &gen.CreateRepoJSONRequestBody{Owner: "acme", Name: "x"}})
	if r, ok := resp.(gen.CreateRepo201JSONResponse); !ok || r.CloneURL != "https://github.com/Acme/x.git" || r.DefaultBranch != "trunk" {
		t.Fatalf("got %#v", resp)
	}

	// Defence in depth: an answer under another owner is refused and logged.
	gh.On("POST", "/user/repos", 201, `{"name":"x","owner":{"login":"someone"},"default_branch":"main"}`)
	resp, _ = h.CreateRepo(context.Background(), gen.CreateRepoRequestObject{Body: &gen.CreateRepoJSONRequestBody{Owner: "acme", Name: "x"}})
	if p := problemOf(t, resp); p.Status != http.StatusForbidden || p.Code != "owner_not_allowed" {
		t.Fatalf("got %#v", resp)
	}
	if !strings.Contains(logs.String(), "github.repo_owner_mismatch") {
		t.Fatalf("logs %q", logs)
	}
}
