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

// The milestone surface at the service tier: the REAL issueService over the
// REAL githubhost client, driven against a gittest.Stub (newIssueSvcOnStub).
// What is being pinned is the wire behaviour GitHub's milestone API forces on
// us — the case-sensitivity split, the 422 recovery, number-not-title
// addressing, pagination, and PR exclusion.

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/gittest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

const milestonesPath = "/repos/acme/widgets/milestones"

// alreadyExists422 is GitHub's verbatim duplicate-title rejection.
const alreadyExists422 = `{"message":"Validation Failed","errors":[{"resource":"Milestone","code":"already_exists","field":"title"}]}`

func TestCreateMilestone_CreatesWhenAbsent(t *testing.T) {
	t.Parallel()
	stub := gittest.NewStub(t)
	stub.On(http.MethodGet, milestonesPath, http.StatusOK, `[]`)
	stub.On(http.MethodPost, milestonesPath, http.StatusCreated, `{"number":4,"title":"v3","node_id":"MI_4"}`)
	svc := newIssueSvcOnStub(t, stub)

	res, err := svc.CreateMilestone(testContext(), "org1", "proj1", sourcecontrol.CreateMilestoneRequest{
		Title: "v3", Description: "spec tag v3",
	})
	if err != nil {
		t.Fatalf("CreateMilestone: %v", err)
	}
	if res.Number != 4 || !res.Created {
		t.Fatalf("result = %+v, want {4 true}", res)
	}

	reqs := stub.Requests()
	// The pre-check spans every state: a closed milestone still owns its title.
	pre := onlyRequest(t, reqs, http.MethodGet, milestonesPath)
	if !strings.Contains(pre.Query, "state=all") {
		t.Fatalf("pre-check query = %q, want it to contain state=all", pre.Query)
	}
	post := onlyRequest(t, reqs, http.MethodPost, milestonesPath)
	var body struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	decodeBody(t, post.Body, &body)
	if body.Title != "v3" || body.Description != "spec tag v3" {
		t.Fatalf("create body = %+v", body)
	}
}

// TestCreateMilestone_RecoversFromAlreadyExists422 is the race path: the
// pre-check saw nothing, a concurrent create won, and GitHub answered 422
// already_exists. The number must be recovered from a re-list — which means the
// same GET route answers differently before and after the POST.
func TestCreateMilestone_RecoversFromAlreadyExists422(t *testing.T) {
	t.Parallel()
	stub := gittest.NewStub(t)
	stub.OnSequence(http.MethodGet, milestonesPath,
		gittest.Response{Status: http.StatusOK, Body: `[]`},
		gittest.Response{Status: http.StatusOK, Body: `[{"number":9,"title":"v3","state":"open"}]`},
	)
	stub.On(http.MethodPost, milestonesPath, http.StatusUnprocessableEntity, alreadyExists422)
	svc := newIssueSvcOnStub(t, stub)

	res, err := svc.CreateMilestone(testContext(), "org1", "proj1", sourcecontrol.CreateMilestoneRequest{Title: "v3"})
	if err != nil {
		t.Fatalf("CreateMilestone: %v", err)
	}
	if res.Number != 9 || res.Created {
		t.Fatalf("result = %+v, want {9 false} (adopted, not created)", res)
	}
	if got := len(requestsMatching(stub.Requests(), http.MethodGet, milestonesPath)); got != 2 {
		t.Fatalf("milestone lists = %d, want 2 (pre-check + recovery)", got)
	}
}

// A 422 that is NOT already_exists is a real rejection, not a duplicate: it
// must surface rather than send us hunting for a milestone that never existed.
func TestCreateMilestone_OtherValidationErrorFails(t *testing.T) {
	t.Parallel()
	stub := gittest.NewStub(t)
	stub.On(http.MethodGet, milestonesPath, http.StatusOK, `[]`)
	stub.On(http.MethodPost, milestonesPath, http.StatusUnprocessableEntity,
		`{"message":"Validation Failed","errors":[{"resource":"Milestone","code":"invalid","field":"due_on"}]}`)
	svc := newIssueSvcOnStub(t, stub)

	if _, err := svc.CreateMilestone(testContext(), "org1", "proj1", sourcecontrol.CreateMilestoneRequest{Title: "v3"}); err == nil {
		t.Fatal("want error for a non-duplicate 422, got nil")
	}
	if got := len(requestsMatching(stub.Requests(), http.MethodGet, milestonesPath)); got != 1 {
		t.Fatalf("milestone lists = %d, want 1 (no recovery attempt)", got)
	}
}

// TestCreateMilestone_CaseInsensitivePreCheckAdoptsTwin is the whole reason the
// pre-check exists: GitHub would happily create "V3" alongside "v3", after
// which the issues-list title filter — which IS case-insensitive — returns
// their merged union forever.
func TestCreateMilestone_CaseInsensitivePreCheckAdoptsTwin(t *testing.T) {
	t.Parallel()
	stub := gittest.NewStub(t)
	stub.On(http.MethodGet, milestonesPath, http.StatusOK, `[{"number":2,"title":"V3","state":"closed"}]`)
	svc := newIssueSvcOnStub(t, stub)

	res, err := svc.CreateMilestone(testContext(), "org1", "proj1", sourcecontrol.CreateMilestoneRequest{Title: "v3"})
	if err != nil {
		t.Fatalf("CreateMilestone: %v", err)
	}
	if res.Number != 2 || res.Created {
		t.Fatalf("result = %+v, want {2 false}", res)
	}
	if got := requestsMatching(stub.Requests(), http.MethodPost, milestonesPath); len(got) != 0 {
		t.Fatalf("POST issued despite a case-twin: %+v", got)
	}
}

func TestCreateMilestone_TitleRequired(t *testing.T) {
	t.Parallel()
	svc := newIssueSvcOnStub(t, gittest.NewStub(t))
	if _, err := svc.CreateMilestone(testContext(), "org1", "proj1", sourcecontrol.CreateMilestoneRequest{Title: "  "}); err == nil {
		t.Fatal("want error for blank title, got nil")
	}
}

func TestCloseMilestone_PatchesStateClosed(t *testing.T) {
	t.Parallel()
	stub := gittest.NewStub(t)
	stub.On(http.MethodPatch, milestonesPath+"/9", http.StatusOK, `{"number":9,"state":"closed"}`)
	svc := newIssueSvcOnStub(t, stub)

	if err := svc.CloseMilestone(testContext(), "org1", "proj1", 9); err != nil {
		t.Fatalf("CloseMilestone: %v", err)
	}
	req := onlyRequest(t, stub.Requests(), http.MethodPatch, milestonesPath+"/9")
	var body map[string]string
	decodeBody(t, req.Body, &body)
	if len(body) != 1 || body["state"] != "closed" {
		t.Fatalf("close patch = %v, want exactly {state: closed}", body)
	}
}

// TestListMilestones_PagesToTheEnd guards the pre-check: a truncated list would
// let a duplicate title through.
func TestListMilestones_PagesToTheEnd(t *testing.T) {
	t.Parallel()
	stub := gittest.NewStub(t)
	full := milestonePage(1, 100)
	stub.OnFunc(http.MethodGet, milestonesPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "1" {
			_, _ = w.Write([]byte(full))
			return
		}
		_, _ = w.Write([]byte(`[{"number":101,"title":"v101","state":"closed"}]`))
	})
	svc := newIssueSvcOnStub(t, stub)

	got, err := svc.ListMilestones(testContext(), "org1", "proj1", "all")
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	if len(got) != 101 {
		t.Fatalf("milestones = %d, want 101 (both pages)", len(got))
	}
	if got[100].Number != 101 || got[100].Title != "v101" || got[100].State != "closed" {
		t.Fatalf("last milestone = %+v", got[100])
	}
}

// TestListMilestoneIssues_FiltersByNumberAndPages pins the three things GitHub
// forces here: the milestone is addressed by NUMBER (a title 422s), labels are
// AND-filtered server-side, and the walk continues past a full page.
func TestListMilestoneIssues_FiltersByNumberAndPages(t *testing.T) {
	t.Parallel()
	stub := gittest.NewStub(t)
	stub.OnFunc(http.MethodGet, "/repos/acme/widgets/issues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "1" {
			_, _ = w.Write([]byte(issuePage(1, 100)))
			return
		}
		_, _ = w.Write([]byte(`[{"number":101,"title":"T101","state":"open","labels":[{"name":"aep"}]}]`))
	})
	svc := newIssueSvcOnStub(t, stub)

	got, err := svc.ListMilestoneIssues(testContext(), "org1", "proj1", sourcecontrol.MilestoneIssuesFilter{
		Number: 9, State: "open", Labels: []string{"aep", "development"},
	})
	if err != nil {
		t.Fatalf("ListMilestoneIssues: %v", err)
	}
	if len(got) != 101 {
		t.Fatalf("issues = %d, want 101 (both pages)", len(got))
	}
	if got[100].Number != 101 || strings.Join(got[100].Labels, ",") != "aep" {
		t.Fatalf("last issue = %+v", got[100])
	}

	reqs := requestsMatching(stub.Requests(), http.MethodGet, "/repos/acme/widgets/issues")
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

// TestListMilestoneIssues_ExcludesPullRequests — GitHub's issues endpoint
// returns member PRs alongside issues. Counting one as an issue is the same
// mistake that makes the milestone's own open_issues field unusable.
func TestListMilestoneIssues_ExcludesPullRequests(t *testing.T) {
	t.Parallel()
	stub := gittest.NewStub(t)
	stub.On(http.MethodGet, "/repos/acme/widgets/issues", http.StatusOK, `[
		{"number":1,"title":"a real issue","state":"open","labels":[]},
		{"number":2,"title":"a member PR","state":"open","labels":[],"pull_request":{"url":"https://api.github.com/repos/acme/widgets/pulls/2"}}
	]`)
	svc := newIssueSvcOnStub(t, stub)

	got, err := svc.ListMilestoneIssues(testContext(), "org1", "proj1", sourcecontrol.MilestoneIssuesFilter{Number: 9})
	if err != nil {
		t.Fatalf("ListMilestoneIssues: %v", err)
	}
	if len(got) != 1 || got[0].Number != 1 {
		t.Fatalf("issues = %+v, want only the non-PR issue #1", got)
	}
}

func TestListMilestoneIssues_NumberRequired(t *testing.T) {
	t.Parallel()
	svc := newIssueSvcOnStub(t, gittest.NewStub(t))
	if _, err := svc.ListMilestoneIssues(testContext(), "org1", "proj1", sourcecontrol.MilestoneIssuesFilter{}); err == nil {
		t.Fatal("want error for a zero milestone number, got nil")
	}
}

// TestMilestoneIssueCounts_SendsAliasedQueryAndParsesCounts pins the dispatch
// predicate: ONE GraphQL round trip carrying every aliased population, and
// never a read of the REST milestone's PR-contaminated open_issues.
//
// One label per alias is the point. The labels: argument matches an issue
// carrying ANY of the listed labels, so a multi-label alias is a union wearing
// a narrower name; with one label each there is no union left to misread, and
// the working sets are plain subtraction in Go.
func TestMilestoneIssueCounts_SendsAliasedQueryAndParsesCounts(t *testing.T) {
	t.Parallel()
	stub := gittest.NewStub(t)
	stub.On(http.MethodPost, "/graphql", http.StatusOK,
		`{"data":{"repository":{"milestone":{
			"provision":{"totalCount":1},
			"allOpen":{"totalCount":9},
			"agentWork":{"totalCount":5},
			"development":{"totalCount":2},
			"validation":{"totalCount":1},
			"srcValidation":{"totalCount":1}
		}}}}`)
	svc := newIssueSvcOnStub(t, stub)

	counts, err := svc.MilestoneIssueCounts(testContext(), "org1", "proj1", 9)
	if err != nil {
		t.Fatalf("MilestoneIssueCounts: %v", err)
	}
	want := sourcecontrol.MilestoneIssueCounts{
		OpenProvision: 1, OpenTotal: 9,
		OpenAgentWork: 5, OpenDevelopment: 2, OpenValidation: 1,
		OpenValidationRepairs: 1,
	}
	if *counts != want {
		t.Fatalf("counts = %+v, want %+v", *counts, want)
	}
	// 5 armed issues, one of which is the validation task: 4 for the dev loop,
	// and 2 once the milestone's planned work is taken out as well.
	if got := counts.OpenDevWork(); got != 4 {
		t.Fatalf("OpenDevWork = %d, want 4", got)
	}
	if got := counts.OpenTaskWork(); got != 2 {
		t.Fatalf("OpenTaskWork = %d, want 2", got)
	}

	req := onlyRequest(t, stub.Requests(), http.MethodPost, "/graphql")
	var payload struct {
		Query     string `json:"query"`
		Variables struct {
			Owner string `json:"owner"`
			Repo  string `json:"repo"`
			M     int    `json:"m"`
		} `json:"variables"`
	}
	decodeBody(t, req.Body, &payload)
	if payload.Variables.Owner != "acme" || payload.Variables.Repo != "widgets" || payload.Variables.M != 9 {
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
	// Exactly six aliased populations, ONE LABEL EACH. A seventh would mean
	// somebody re-added a COMPOSITE alias — the union the old inclusion-exclusion
	// arithmetic needed. Any alias listing more than one label is a wider union
	// than its name claims, and it silently empties a working set.
	if got := strings.Count(payload.Query, "issues(states: [OPEN]"); got != 6 {
		t.Fatalf("query has %d aliased populations, want exactly 6:\n%s", got, payload.Query)
	}
	if got := strings.Count(payload.Query, `", "`); got != 0 {
		t.Fatalf("an alias lists more than one label (%d multi-label alias separators):\n%s", got, payload.Query)
	}
	if req.Header.Get("Authorization") != "Bearer test-token" {
		t.Fatalf("graphql Authorization = %q, want the credential's bearer token", req.Header.Get("Authorization"))
	}
}

// The label vocabulary, spelled here rather than imported: `sourcecontrol` is
// below `delivery` and must not depend on it. The literals are the same ones
// milestoneIssueCountsQuery embeds, which is the coupling under test.
const (
	labelWork  = "aep"
	labelDev   = "development"
	labelBug   = "bug"
	labelGate  = "provision"
	labelValid = "validation"
	// srcValidation is the SOURCE a failed verdict stamps on the repair work it
	// files. It is counted like a kind and subtracted like nothing: the bug-fix
	// loop reads it to decide whether draining its working set reopens the
	// version's validation task, and no working set is defined by it.
	labelSrcValid = "src/validation"
)

// hostCounts answers the populations the REAL host would report for a milestone
// holding these open issues.
//
// It exists because the shipped arithmetic was once wrong for exactly one
// reason: a belief that GraphQL's `labels:` argument intersects. It UNIONS — an
// issue matches when it carries ANY listed label. (The AND-semantics filter is
// the REST `?labels=a,b` parameter, a different API over the same resource.)
// Every case below is therefore stated as issues-and-labels and counted through
// this function, so no case can assert against a population GitHub cannot
// produce.
//
// Each field counts ONE label, exactly as each alias in the query filters on
// one — so this fake would still be honest if the argument were an intersection.
// That is deliberate: the query no longer depends on which of the two it is.
func hostCounts(issues ...[]string) *sourcecontrol.MilestoneIssueCounts {
	carrying := func(want string) int {
		n := 0
		for _, have := range issues {
			if slices.Contains(have, want) {
				n++
			}
		}
		return n
	}
	return &sourcecontrol.MilestoneIssueCounts{
		OpenProvision:         carrying(labelGate),
		OpenAgentWork:         carrying(labelWork),
		OpenDevelopment:       carrying(labelDev),
		OpenValidation:        carrying(labelValid),
		OpenValidationRepairs: carrying(labelSrcValid),
		OpenTotal:             len(issues),
	}
}

// TestMilestoneIssueCounts_WorkingSetArithmetic pins the ONE place each working
// set is computed, over the populations the host actually returns.
//
// Subtraction is exact here where inclusion-exclusion was not, because every
// subtracted kind is a strict SUBSET of the armed population: an armed issue
// carries exactly one kind, so it is counted once and removed at most once.
func TestMilestoneIssueCounts_WorkingSetArithmetic(t *testing.T) {
	t.Parallel()
	var (
		planned = []string{labelWork, labelDev}
		bug     = []string{labelWork, labelBug}
		gate    = []string{labelGate}
		valid   = []string{labelWork, labelValid}
		armed   = []string{labelWork} // armed by a human, not yet classified
		ledger  = []string(nil)
		// A failed verdict's repair work: an ordinary armed bug whose SOURCE
		// records where it came from.
		repair = []string{labelWork, labelBug, labelSrcValid}
	)
	cases := []struct {
		name     string
		counts   *sourcecontrol.MilestoneIssueCounts
		wantDev  int
		wantTask int
	}{
		{"a milestone of planned work", hostCounts(planned, planned, planned), 3, 0},
		{
			// Human-filed issues carrying no arming label are the milestone's
			// LEDGER: they inflate the total and are never work.
			"ledger issues are not work",
			hostCounts(ledger, ledger, ledger, ledger), 0, 0,
		},
		{
			// THE LIVE FAILURE, exactly as it stood: one task alongside one
			// provision gate. Read as an empty working set, the run settles a
			// version nobody built. A gate carries no arming label at all now, so
			// there is nothing left for it to be subtracted from.
			"a gate alongside real work leaves the work visible",
			hostCounts(planned, gate), 1, 0,
		},
		{
			"the validation task is nobody's working set",
			hostCounts(planned, planned, planned, gate, valid, ledger, ledger), 3, 0,
		},
		{
			// The whole reason the task working set exists: a bug-fix run works the
			// deployed version and must not pick up the build's planned work.
			"a bug is in both working sets, planned work only in dev",
			hostCounts(planned, planned, bug), 3, 1,
		},
		{
			// An armed issue with no kind is what a human's adoption produces. It
			// counts as work in BOTH sets — which is what delivery.InDevWorkingSet
			// answers for the same issue, and the safe direction besides: a stall
			// is visible where a silent settle is not.
			"an armed issue with no kind is work",
			hostCounts(armed, valid), 1, 1,
		},
		{
			// Not producible by hostCounts, and that is the point: the clamp guards
			// against a host that answers inconsistently, not against a milestone.
			"an inconsistent host cannot produce negative work",
			&sourcecontrol.MilestoneIssueCounts{OpenAgentWork: 0, OpenValidation: 2, OpenDevelopment: 1},
			0, 0,
		},
		{
			// A source is not a kind and NOTHING subtracts it: a repair bug is an
			// ordinary bug in both working sets, counted once. Reading the source as
			// an exclusion would empty the very working set the repair created.
			"verdict-sourced repair work is ordinary work",
			hostCounts(repair, repair, planned), 3, 2,
		},
		{"an unknown milestone has no work", nil, 0, 0},
	}
	for _, c := range cases {
		if got := c.counts.OpenDevWork(); got != c.wantDev {
			t.Errorf("OpenDevWork(%s) = %d, want %d (counts %+v)", c.name, got, c.wantDev, c.counts)
		}
		if got := c.counts.OpenTaskWork(); got != c.wantTask {
			t.Errorf("OpenTaskWork(%s) = %d, want %d (counts %+v)", c.name, got, c.wantTask, c.counts)
		}
	}
}

// TestMilestoneIssueCounts_AGateHoldsWorkItDoesNotErase is the live regression
// in one assertion pair: the working set of a freshly planned milestone is ONE
// task whether or not its provision gate is still open. The gate holds the
// dispatch (the predicate's other clause, in `delivery`); it must never make
// the milestone read as empty, because empty is what closes the version.
//
// It also pins the case a naive "just arm everything" would break: a gate
// carries NO arming label, so the gate count is the ONLY read that can see it.
// Counting gates through the armed population would make an open gate invisible
// and the run would dispatch straight past its own hold.
func TestMilestoneIssueCounts_AGateHoldsWorkItDoesNotErase(t *testing.T) {
	t.Parallel()
	gated := hostCounts([]string{labelWork, labelDev}, []string{labelGate})
	if got := gated.OpenDevWork(); got != 1 {
		t.Fatalf("working set behind an open gate = %d, want 1 (counts %+v)", got, gated)
	}
	if gated.OpenProvision != 1 {
		t.Fatalf("gates = %d, want 1", gated.OpenProvision)
	}
	if gated.OpenAgentWork != 1 {
		t.Fatalf("armed issues = %d, want 1 — a gate is not armed", gated.OpenAgentWork)
	}
	released := hostCounts([]string{labelWork, labelDev})
	if got := released.OpenDevWork(); got != 1 {
		t.Fatalf("working set after the gate closed = %d, want 1 (counts %+v)", got, released)
	}
	if released.OpenProvision != 0 {
		t.Fatalf("gates after the gate closed = %d, want 0", released.OpenProvision)
	}
}

// A milestone number that no longer resolves (deleted on GitHub) is a
// recoverable state, distinguishable from a transport failure.
func TestMilestoneIssueCounts_MissingMilestone(t *testing.T) {
	t.Parallel()
	stub := gittest.NewStub(t)
	stub.On(http.MethodPost, "/graphql", http.StatusOK, `{"data":{"repository":{"milestone":null}}}`)
	svc := newIssueSvcOnStub(t, stub)

	_, err := svc.MilestoneIssueCounts(testContext(), "org1", "proj1", 404)
	if !errors.Is(err, sourcecontrol.ErrMilestoneNotFound) {
		t.Fatalf("err = %v, want ErrMilestoneNotFound", err)
	}
}

// GraphQL answers 200 with a populated errors[]; the whole array survives as a
// typed error so callers branch on the machine-readable type.
func TestMilestoneIssueCounts_GraphQLErrorIsTyped(t *testing.T) {
	t.Parallel()
	stub := gittest.NewStub(t)
	stub.On(http.MethodPost, "/graphql", http.StatusOK,
		`{"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}],"data":null}`)
	svc := newIssueSvcOnStub(t, stub)

	_, err := svc.MilestoneIssueCounts(testContext(), "org1", "proj1", 9)
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	if !sourcecontrol.IsGraphQLType(err, "RATE_LIMITED") {
		t.Fatalf("err = %v, want a GraphQLError of type RATE_LIMITED", err)
	}
	if sourcecontrol.IsGraphQLType(err, "NOT_FOUND") {
		t.Fatalf("err = %v matched the wrong type", err)
	}
}

// TestCreateIssue_AssignsMilestoneAtCreation — assignment rides issue creation
// (one call, not create-then-patch) and travels as the NUMBER; GitHub 422s a
// title here. Unset means the field never reaches the wire.
func TestCreateIssue_AssignsMilestoneAtCreation(t *testing.T) {
	t.Parallel()
	stub := gittest.NewStub(t)
	stub.On(http.MethodPost, "/repos/acme/widgets/labels", http.StatusCreated, `{}`)
	stub.On(http.MethodPost, "/repos/acme/widgets/issues", http.StatusCreated,
		`{"number":7,"html_url":"https://github.com/acme/widgets/issues/7","node_id":"NODE7"}`)
	svc := newIssueSvcOnStub(t, stub)

	number := 9
	if _, err := svc.CreateIssue(testContext(), "org1", "proj1", sourcecontrol.CreateIssueRequest{
		Title: "Implement auth", Body: "do the thing", Milestone: &number,
	}); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	req := onlyRequest(t, stub.Requests(), http.MethodPost, "/repos/acme/widgets/issues")
	var body struct {
		Milestone *int `json:"milestone"`
	}
	decodeBody(t, req.Body, &body)
	if body.Milestone == nil || *body.Milestone != 9 {
		t.Fatalf("milestone on the wire = %v, want 9", body.Milestone)
	}
}

func TestCreateIssue_OmitsMilestoneWhenUnset(t *testing.T) {
	t.Parallel()
	stub := gittest.NewStub(t)
	stub.On(http.MethodPost, "/repos/acme/widgets/issues", http.StatusCreated,
		`{"number":7,"html_url":"https://github.com/acme/widgets/issues/7"}`)
	svc := newIssueSvcOnStub(t, stub)

	if _, err := svc.CreateIssue(testContext(), "org1", "proj1", sourcecontrol.CreateIssueRequest{Title: "unassigned"}); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	req := onlyRequest(t, stub.Requests(), http.MethodPost, "/repos/acme/widgets/issues")
	var body map[string]any
	decodeBody(t, req.Body, &body)
	if _, present := body["milestone"]; present {
		t.Fatalf("unset milestone leaked onto the wire: %s", req.Body)
	}
	// The aep-api-only dedupe key must stay off the wire too.
	if _, present := body["dedupeKey"]; present {
		t.Fatalf("dedupeKey leaked onto the wire: %s", req.Body)
	}
}

// jsonPage renders a full page of n objects numbered from `from`, so a test can
// arrange the "page was full, keep walking" condition without a 100-entry
// literal. tmpl takes the number twice (number + title suffix).
func jsonPage(tmpl string, from, n int) string {
	items := make([]string, 0, n)
	for i := range n {
		items = append(items, fmt.Sprintf(tmpl, from+i, from+i))
	}
	return "[" + strings.Join(items, ",") + "]"
}

func milestonePage(from, n int) string {
	return jsonPage(`{"number":%d,"title":"v%d","state":"open"}`, from, n)
}

func issuePage(from, n int) string {
	return jsonPage(`{"number":%d,"title":"T%d","state":"open","labels":[]}`, from, n)
}
