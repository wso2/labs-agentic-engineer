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

// The issue, milestone and hook wire GitHub forces on the client, pinned
// against a githubtest.Stub: the case-sensitivity split and 422 recovery of
// milestone creation, number-not-title addressing, pagination, PR exclusion,
// the one GraphQL round trip of the dispatch counts, the request bodies of
// the issue writes, the hook's body and its delete. Moved from aep-api's
// service-tier tests (sourcecontrol milestone_ops_test.go,
// issue_service_test.go, webhook_service_test.go, webhook_unregister_test.go),
// which drove the same code in githubhost before phase 4 moved it here.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/github/githubtest"
)

const (
	wireOwner      = "acme"
	wireRepo       = "widgets"
	milestonesPath = "/repos/acme/widgets/milestones"
	issuesPath     = "/repos/acme/widgets/issues"
)

// alreadyExists422 is GitHub's verbatim duplicate-title rejection.
const alreadyExists422 = `{"message":"Validation Failed","errors":[{"resource":"Milestone","code":"already_exists","field":"title"}]}`

// clientOnStub is the real client at stub, REST and GraphQL alike.
func clientOnStub(stub *githubtest.Stub) *Client {
	return New(Config{APIBase: stub.URL(), Token: staticToken("test-token")})
}

// requestsTo answers the recorded requests matching method+path, in order.
func requestsTo(stub *githubtest.Stub, method, path string) []githubtest.RecordedRequest {
	var out []githubtest.RecordedRequest
	for _, r := range stub.Requests() {
		if r.Method == method && r.Path == path {
			out = append(out, r)
		}
	}
	return out
}

// onlyRequestTo asserts exactly one request matches and answers it.
func onlyRequestTo(t *testing.T, stub *githubtest.Stub, method, path string) githubtest.RecordedRequest {
	t.Helper()
	m := requestsTo(stub, method, path)
	if len(m) != 1 {
		t.Fatalf("want exactly 1 %s %s request, got %d (all: %+v)", method, path, len(m), stub.Requests())
	}
	return m[0]
}

func decodeInto(t *testing.T, raw string, dst any) {
	t.Helper()
	if err := json.Unmarshal([]byte(raw), dst); err != nil {
		t.Fatalf("decode request body %q: %v", raw, err)
	}
}

func TestCreateMilestone_CreatesWhenAbsent(t *testing.T) {
	stub := githubtest.NewStub(t)
	stub.On(http.MethodGet, milestonesPath, http.StatusOK, `[]`)
	stub.On(http.MethodPost, milestonesPath, http.StatusCreated, `{"number":4,"title":"v3","node_id":"MI_4"}`)

	res, err := clientOnStub(stub).CreateMilestone(context.Background(), wireOwner, wireRepo, CreateMilestoneRequest{Title: "v3", Description: "spec tag v3"})
	if err != nil {
		t.Fatalf("CreateMilestone: %v", err)
	}
	if res.Number != 4 || !res.Created {
		t.Fatalf("result = %+v, want {4 true}", res)
	}
	// The pre-check spans every state: a closed milestone still owns its title.
	if pre := onlyRequestTo(t, stub, http.MethodGet, milestonesPath); !strings.Contains(pre.Query, "state=all") {
		t.Fatalf("pre-check query = %q, want it to contain state=all", pre.Query)
	}
	var body struct{ Title, Description string }
	decodeInto(t, onlyRequestTo(t, stub, http.MethodPost, milestonesPath).Body, &body)
	if body.Title != "v3" || body.Description != "spec tag v3" {
		t.Fatalf("create body = %+v", body)
	}
}

// The race path: the pre-check saw nothing, a concurrent create won, and
// GitHub answered 422 already_exists. The number is recovered from a re-list,
// so the same GET route answers differently before and after the POST.
func TestCreateMilestone_RecoversFromAlreadyExists422(t *testing.T) {
	stub := githubtest.NewStub(t)
	stub.OnSequence(http.MethodGet, milestonesPath,
		githubtest.Response{Status: http.StatusOK, Body: `[]`},
		githubtest.Response{Status: http.StatusOK, Body: `[{"number":9,"title":"v3","state":"open"}]`},
	)
	stub.On(http.MethodPost, milestonesPath, http.StatusUnprocessableEntity, alreadyExists422)

	res, err := clientOnStub(stub).CreateMilestone(context.Background(), wireOwner, wireRepo, CreateMilestoneRequest{Title: "v3"})
	if err != nil {
		t.Fatalf("CreateMilestone: %v", err)
	}
	if res.Number != 9 || res.Created {
		t.Fatalf("result = %+v, want {9 false} (adopted, not created)", res)
	}
	if got := len(requestsTo(stub, http.MethodGet, milestonesPath)); got != 2 {
		t.Fatalf("milestone lists = %d, want 2 (pre-check + recovery)", got)
	}
}

// A 422 that is NOT already_exists is a real rejection: it surfaces rather
// than sending the client hunting for a milestone that never existed.
func TestCreateMilestone_OtherValidationErrorFails(t *testing.T) {
	stub := githubtest.NewStub(t)
	stub.On(http.MethodGet, milestonesPath, http.StatusOK, `[]`)
	stub.On(http.MethodPost, milestonesPath, http.StatusUnprocessableEntity,
		`{"message":"Validation Failed","errors":[{"resource":"Milestone","code":"invalid","field":"due_on"}]}`)

	if _, err := clientOnStub(stub).CreateMilestone(context.Background(), wireOwner, wireRepo, CreateMilestoneRequest{Title: "v3"}); err == nil {
		t.Fatal("want error for a non-duplicate 422, got nil")
	}
	if got := len(requestsTo(stub, http.MethodGet, milestonesPath)); got != 1 {
		t.Fatalf("milestone lists = %d, want 1 (no recovery attempt)", got)
	}
}

// The whole reason the pre-check exists: GitHub would create "V3" alongside
// "v3", after which the issues-list title filter — which IS case-insensitive —
// returns their merged union forever.
func TestCreateMilestone_CaseInsensitivePreCheckAdoptsTwin(t *testing.T) {
	stub := githubtest.NewStub(t)
	stub.On(http.MethodGet, milestonesPath, http.StatusOK, `[{"number":2,"title":"V3","state":"closed"}]`)

	res, err := clientOnStub(stub).CreateMilestone(context.Background(), wireOwner, wireRepo, CreateMilestoneRequest{Title: "v3"})
	if err != nil {
		t.Fatalf("CreateMilestone: %v", err)
	}
	if res.Number != 2 || res.Created {
		t.Fatalf("result = %+v, want {2 false}", res)
	}
	if got := requestsTo(stub, http.MethodPost, milestonesPath); len(got) != 0 {
		t.Fatalf("POST issued despite a case-twin: %+v", got)
	}
}

func TestCloseMilestone_PatchesStateClosed(t *testing.T) {
	stub := githubtest.NewStub(t)
	stub.On(http.MethodPatch, milestonesPath+"/9", http.StatusOK, `{"number":9,"state":"closed"}`)

	if err := clientOnStub(stub).CloseMilestone(context.Background(), wireOwner, wireRepo, 9); err != nil {
		t.Fatalf("CloseMilestone: %v", err)
	}
	var body map[string]string
	decodeInto(t, onlyRequestTo(t, stub, http.MethodPatch, milestonesPath+"/9").Body, &body)
	if len(body) != 1 || body["state"] != "closed" {
		t.Fatalf("close patch = %v, want exactly {state: closed}", body)
	}
}

// A truncated list would let a duplicate title past the pre-check.
func TestListMilestones_PagesToTheEnd(t *testing.T) {
	stub := githubtest.NewStub(t)
	stub.OnFunc(http.MethodGet, milestonesPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "1" {
			_, _ = w.Write([]byte(jsonPage(`{"number":%d,"title":"v%d","state":"open"}`, 1, 100)))
			return
		}
		_, _ = w.Write([]byte(`[{"number":101,"title":"v101","state":"closed"}]`))
	})

	got, err := clientOnStub(stub).ListMilestones(context.Background(), wireOwner, wireRepo, "all")
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	if len(got) != 101 || got[100].Number != 101 || got[100].Title != "v101" || got[100].State != "closed" {
		t.Fatalf("milestones = %d, last = %+v, want 101 ending at v101 closed", len(got), got[len(got)-1])
	}
}

// The milestone is addressed by NUMBER (a title 422s), labels are AND-filtered
// server-side, and the walk continues past a full page.
func TestListMilestoneIssues_FiltersByNumberAndPages(t *testing.T) {
	stub := githubtest.NewStub(t)
	stub.OnFunc(http.MethodGet, issuesPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "1" {
			_, _ = w.Write([]byte(jsonPage(`{"number":%d,"title":"T%d","state":"open","labels":[]}`, 1, 100)))
			return
		}
		_, _ = w.Write([]byte(`[{"number":101,"title":"T101","state":"open","labels":[{"name":"aep"}]}]`))
	})

	got, err := clientOnStub(stub).ListMilestoneIssues(context.Background(), wireOwner, wireRepo, MilestoneIssuesFilter{
		Number: 9, State: "open", Labels: []string{"aep", "development"},
	})
	if err != nil {
		t.Fatalf("ListMilestoneIssues: %v", err)
	}
	if len(got) != 101 || got[100].Number != 101 || strings.Join(got[100].Labels, ",") != "aep" {
		t.Fatalf("issues = %d, last = %+v", len(got), got[len(got)-1])
	}
	reqs := requestsTo(stub, http.MethodGet, issuesPath)
	if len(reqs) != 2 {
		t.Fatalf("issue list calls = %d, want 2", len(reqs))
	}
	for _, want := range []string{"milestone=9", "state=open", "labels=aep%2Cdevelopment", "page=1"} {
		if !strings.Contains(reqs[0].Query, want) {
			t.Fatalf("page-1 query = %q, want it to contain %q", reqs[0].Query, want)
		}
	}
	if !strings.Contains(reqs[1].Query, "page=2") {
		t.Fatalf("page-2 query = %q, want it to contain page=2", reqs[1].Query)
	}
}

// GitHub's issues endpoint returns member PRs alongside issues; counting one
// as an issue is the mistake that makes the milestone's open_issues unusable.
func TestListMilestoneIssues_ExcludesPullRequests(t *testing.T) {
	stub := githubtest.NewStub(t)
	stub.On(http.MethodGet, issuesPath, http.StatusOK, `[
		{"number":1,"title":"a real issue","state":"open","labels":[]},
		{"number":2,"title":"a member PR","state":"open","labels":[],"pull_request":{"url":"https://api.github.com/repos/acme/widgets/pulls/2"}}
	]`)

	got, err := clientOnStub(stub).ListMilestoneIssues(context.Background(), wireOwner, wireRepo, MilestoneIssuesFilter{Number: 9})
	if err != nil {
		t.Fatalf("ListMilestoneIssues: %v", err)
	}
	if len(got) != 1 || got[0].Number != 1 {
		t.Fatalf("issues = %+v, want only the non-PR issue #1", got)
	}
}

func TestListMilestoneIssues_NumberRequired(t *testing.T) {
	stub := githubtest.NewStub(t)
	if _, err := clientOnStub(stub).ListMilestoneIssues(context.Background(), wireOwner, wireRepo, MilestoneIssuesFilter{}); err == nil {
		t.Fatal("want error for a zero milestone number, got nil")
	}
	if n := len(stub.Requests()); n != 0 {
		t.Fatalf("requests = %d, want none", n)
	}
}

// The dispatch predicate: ONE GraphQL round trip carrying every aliased
// population, ONE label each (GraphQL's labels: argument is a union, so a
// multi-label alias is wider than its name), and never a read of the REST
// milestone's PR-contaminated open_issues.
func TestMilestoneIssueCounts_SendsAliasedQueryAndParsesCounts(t *testing.T) {
	stub := githubtest.NewStub(t)
	stub.On(http.MethodPost, "/graphql", http.StatusOK,
		`{"data":{"repository":{"milestone":{
			"provision":{"totalCount":1},
			"allOpen":{"totalCount":9},
			"agentWork":{"totalCount":5},
			"development":{"totalCount":2},
			"validation":{"totalCount":1},
			"srcValidation":{"totalCount":1}
		}}}}`)

	counts, err := clientOnStub(stub).MilestoneIssueCounts(context.Background(), wireOwner, wireRepo, 9)
	if err != nil {
		t.Fatalf("MilestoneIssueCounts: %v", err)
	}
	want := MilestoneIssueCounts{OpenProvision: 1, OpenTotal: 9, OpenAgentWork: 5, OpenDevelopment: 2, OpenValidation: 1, OpenValidationRepairs: 1}
	if *counts != want {
		t.Fatalf("counts = %+v, want %+v", *counts, want)
	}

	req := onlyRequestTo(t, stub, http.MethodPost, "/graphql")
	var payload struct {
		Query     string `json:"query"`
		Variables struct {
			Owner string `json:"owner"`
			Repo  string `json:"repo"`
			M     int    `json:"m"`
		} `json:"variables"`
	}
	decodeInto(t, req.Body, &payload)
	if payload.Variables.Owner != wireOwner || payload.Variables.Repo != wireRepo || payload.Variables.M != 9 {
		t.Fatalf("variables = %+v, want {acme widgets 9}", payload.Variables)
	}
	for _, want := range []string{
		`provision:     issues(states: [OPEN], labels: ["provision"], first: 1)`,
		`allOpen:       issues(states: [OPEN], first: 1)`,
		`agentWork:     issues(states: [OPEN], labels: ["aep"], first: 1)`,
		`development:   issues(states: [OPEN], labels: ["development"], first: 1)`,
		`validation:    issues(states: [OPEN], labels: ["validation"], first: 1)`,
		`srcValidation: issues(states: [OPEN], labels: ["src/validation"], first: 1)`,
		"milestone(number: $m)",
	} {
		if !strings.Contains(payload.Query, want) {
			t.Fatalf("query missing %q:\n%s", want, payload.Query)
		}
	}
	if strings.Contains(payload.Query, "open_issues") || strings.Contains(payload.Query, "openIssueCount") {
		t.Fatalf("query reads a PR-contaminated count:\n%s", payload.Query)
	}
	if got := strings.Count(payload.Query, "issues(states: [OPEN]"); got != 6 {
		t.Fatalf("query has %d aliased populations, want exactly 6:\n%s", got, payload.Query)
	}
	if got := strings.Count(payload.Query, `", "`); got != 0 {
		t.Fatalf("an alias lists more than one label (%d multi-label alias separators):\n%s", got, payload.Query)
	}
	if req.Header.Get("Authorization") != "Bearer test-token" {
		t.Fatalf("graphql Authorization = %q, want the gitpat's bearer token", req.Header.Get("Authorization"))
	}
}

// A milestone number that no longer resolves (deleted on GitHub) is a
// recoverable state, distinguishable from a transport failure.
func TestMilestoneIssueCounts_MissingMilestone(t *testing.T) {
	stub := githubtest.NewStub(t)
	stub.On(http.MethodPost, "/graphql", http.StatusOK, `{"data":{"repository":{"milestone":null}}}`)

	if _, err := clientOnStub(stub).MilestoneIssueCounts(context.Background(), wireOwner, wireRepo, 404); !errors.Is(err, ErrMilestoneNotFound) {
		t.Fatalf("err = %v, want ErrMilestoneNotFound", err)
	}
}

// GraphQL answers 200 with a populated errors[]; it survives as a typed error
// so callers branch on the machine-readable type.
func TestMilestoneIssueCounts_GraphQLErrorIsTyped(t *testing.T) {
	stub := githubtest.NewStub(t)
	stub.On(http.MethodPost, "/graphql", http.StatusOK,
		`{"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}],"data":null}`)

	_, err := clientOnStub(stub).MilestoneIssueCounts(context.Background(), wireOwner, wireRepo, 9)
	if !IsGraphQLType(err, "RATE_LIMITED") || IsGraphQLType(err, "NOT_FOUND") {
		t.Fatalf("err = %v, want a GraphQLError of type RATE_LIMITED only", err)
	}
}

func TestCreateIssue_SendsTitleBodyLabelsAndMilestone(t *testing.T) {
	stub := githubtest.NewStub(t)
	stub.On(http.MethodPost, issuesPath, http.StatusCreated,
		`{"number":7,"html_url":"https://github.com/acme/widgets/issues/7","node_id":"NODE7"}`)

	number := 9
	res, err := clientOnStub(stub).CreateIssue(context.Background(), wireOwner, wireRepo, CreateIssueRequest{
		Title: "Implement auth", Body: "do the thing", Labels: []string{"aep", "phase-1"}, Milestone: &number,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if res.Number != 7 || res.URL != "https://github.com/acme/widgets/issues/7" || res.NodeID != "NODE7" {
		t.Fatalf("result = %+v, want {7, .../issues/7, NODE7}", res)
	}
	var body struct {
		Title     string   `json:"title"`
		Body      string   `json:"body"`
		Labels    []string `json:"labels"`
		Milestone *int     `json:"milestone"`
	}
	decodeInto(t, onlyRequestTo(t, stub, http.MethodPost, issuesPath).Body, &body)
	if body.Title != "Implement auth" || body.Body != "do the thing" || strings.Join(body.Labels, ",") != "aep,phase-1" {
		t.Fatalf("issue body = %+v", body)
	}
	// Assignment rides creation and travels as the NUMBER; GitHub 422s a title.
	if body.Milestone == nil || *body.Milestone != 9 {
		t.Fatalf("milestone on the wire = %v, want 9", body.Milestone)
	}
}

func TestCreateIssue_OmitsMilestoneWhenUnset(t *testing.T) {
	stub := githubtest.NewStub(t)
	stub.On(http.MethodPost, issuesPath, http.StatusCreated, `{"number":7,"html_url":"https://github.com/acme/widgets/issues/7"}`)

	if _, err := clientOnStub(stub).CreateIssue(context.Background(), wireOwner, wireRepo, CreateIssueRequest{Title: "unassigned"}); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	var body map[string]any
	raw := onlyRequestTo(t, stub, http.MethodPost, issuesPath).Body
	decodeInto(t, raw, &body)
	if _, present := body["milestone"]; present {
		t.Fatalf("unset milestone leaked onto the wire: %s", raw)
	}
}

func TestEnsureLabel_PostsNameAndColour(t *testing.T) {
	stub := githubtest.NewStub(t)
	stub.On(http.MethodPost, "/repos/acme/widgets/labels", http.StatusCreated, `{}`)

	if err := clientOnStub(stub).EnsureLabel(context.Background(), wireOwner, wireRepo, "aep", "0075ca"); err != nil {
		t.Fatalf("EnsureLabel: %v", err)
	}
	var l struct{ Name, Color string }
	decodeInto(t, onlyRequestTo(t, stub, http.MethodPost, "/repos/acme/widgets/labels").Body, &l)
	if l.Name != "aep" || l.Color != "0075ca" {
		t.Fatalf("label = %+v", l)
	}
}

func TestCloseIssue_PatchesClosedCompleted(t *testing.T) {
	stub := githubtest.NewStub(t)
	stub.On(http.MethodPatch, issuesPath+"/7", http.StatusOK, `{}`)

	if err := clientOnStub(stub).CloseIssue(context.Background(), wireOwner, wireRepo, 7); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	var pb struct {
		State       string `json:"state"`
		StateReason string `json:"state_reason"`
	}
	decodeInto(t, onlyRequestTo(t, stub, http.MethodPatch, issuesPath+"/7").Body, &pb)
	if pb.State != "closed" || pb.StateReason != "completed" {
		t.Fatalf("close patch = %+v, want {closed completed}", pb)
	}
}

func TestEditIssueBody_PatchesBody(t *testing.T) {
	stub := githubtest.NewStub(t)
	stub.On(http.MethodPatch, issuesPath+"/7", http.StatusOK, `{}`)

	if err := clientOnStub(stub).EditIssueBody(context.Background(), wireOwner, wireRepo, 7, "replacement body"); err != nil {
		t.Fatalf("EditIssueBody: %v", err)
	}
	var b struct{ Body string }
	decodeInto(t, onlyRequestTo(t, stub, http.MethodPatch, issuesPath+"/7").Body, &b)
	if b.Body != "replacement body" {
		t.Fatalf("edit body = %q", b.Body)
	}
}

// Adoption's write: the milestone travels as a NUMBER on the ordinary issue
// PATCH route.
func TestSetIssueMilestone_PatchesTheNumber(t *testing.T) {
	stub := githubtest.NewStub(t)
	stub.On(http.MethodPatch, issuesPath+"/7", http.StatusOK, `{}`)

	if err := clientOnStub(stub).SetIssueMilestone(context.Background(), wireOwner, wireRepo, 7, 3); err != nil {
		t.Fatalf("SetIssueMilestone: %v", err)
	}
	var b struct{ Milestone int }
	decodeInto(t, onlyRequestTo(t, stub, http.MethodPatch, issuesPath+"/7").Body, &b)
	if b.Milestone != 3 {
		t.Fatalf("milestone = %d, want 3", b.Milestone)
	}
}

func TestListIssues_PassesTheLabelFilter(t *testing.T) {
	stub := githubtest.NewStub(t)
	stub.On(http.MethodGet, issuesPath, http.StatusOK,
		`[{"number":1,"title":"T1","body":"B1","html_url":"U1","state":"open","labels":[{"name":"aep"},{"name":"phase-1"}]}]`)

	issues, err := clientOnStub(stub).ListIssues(context.Background(), wireOwner, wireRepo, []string{"aep"})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 1 || issues[0].Title != "T1" || issues[0].Body != "B1" || issues[0].URL != "U1" || strings.Join(issues[0].Labels, ",") != "aep,phase-1" {
		t.Fatalf("issues = %+v", issues)
	}
	if req := onlyRequestTo(t, stub, http.MethodGet, issuesPath); !strings.Contains(req.Query, "labels=aep") {
		t.Fatalf("query = %q, want it to contain labels=aep", req.Query)
	}
}

// The hook GitHub receives: a "web" hook to the pod's delivery URL, signed
// with the pod's secret, JSON bodies, TLS verification ON (insecure_ssl "0")
// and the asked-for events. The URL, the secret and insecure_ssl are the
// security-relevant parts. Moved from aep-api's webhook_service_test.go.
func TestRegisterWebhook_SendsTheSignedVerifiedHook(t *testing.T) {
	stub := githubtest.NewStub(t)
	stub.On(http.MethodPost, "/repos/acme/widgets/hooks", http.StatusCreated, `{"id":12345}`)
	c := New(Config{APIBase: stub.URL(), Token: staticToken("test-token"), HookURL: "https://tools.example/webhooks/github", HookSecret: "s3cr3t"})

	id, existed, err := c.RegisterWebhook(context.Background(), wireOwner, wireRepo, []string{"pull_request", "push", "issue_comment", "issues"})
	if err != nil || id != 12345 || existed {
		t.Fatalf("RegisterWebhook = (%d, %v, %v), want (12345, false, nil)", id, existed, err)
	}
	var body struct {
		Name   string            `json:"name"`
		Active bool              `json:"active"`
		Events []string          `json:"events"`
		Config map[string]string `json:"config"`
	}
	decodeInto(t, onlyRequestTo(t, stub, http.MethodPost, "/repos/acme/widgets/hooks").Body, &body)
	if body.Name != "web" || !body.Active {
		t.Fatalf("hook = {name:%q active:%v}, want {web true}", body.Name, body.Active)
	}
	if strings.Join(body.Events, ",") != "pull_request,push,issue_comment,issues" {
		t.Fatalf("events = %v", body.Events)
	}
	want := map[string]string{"url": "https://tools.example/webhooks/github", "secret": "s3cr3t", "content_type": "json", "insecure_ssl": "0"}
	for k, v := range want {
		if body.Config[k] != v {
			t.Fatalf("config[%s] = %q, want %q", k, body.Config[k], v)
		}
	}
}

// A hook GitHub says is not there — 404, or 410 once it reaped a failing
// hook — is a successful delete: absence is the post-state asked for.
// Moved from aep-api's webhook_unregister_test.go.
func TestDeleteWebhook_AlreadyGoneIsSuccess(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusNotFound, http.StatusGone} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			stub := githubtest.NewStub(t)
			stub.On(http.MethodDelete, "/repos/acme/widgets/hooks/12345", status, `{"message":"Not Found"}`)
			if err := clientOnStub(stub).DeleteWebhook(context.Background(), wireOwner, wireRepo, 12345); err != nil {
				t.Fatalf("delete answered %d: err = %v, want success", status, err)
			}
		})
	}
	stub := githubtest.NewStub(t)
	stub.On(http.MethodDelete, "/repos/acme/widgets/hooks/12345", http.StatusInternalServerError, `{"message":"boom"}`)
	if err := clientOnStub(stub).DeleteWebhook(context.Background(), wireOwner, wireRepo, 12345); err == nil {
		t.Fatal("a 500 on delete must be reported")
	}
}

// jsonPage renders a full page of n objects numbered from `from`; tmpl takes
// the number twice (number + title suffix).
func jsonPage(tmpl string, from, n int) string {
	items := make([]string, 0, n)
	for i := range n {
		items = append(items, fmt.Sprintf(tmpl, from+i, from+i))
	}
	return "[" + strings.Join(items, ",") + "]"
}
