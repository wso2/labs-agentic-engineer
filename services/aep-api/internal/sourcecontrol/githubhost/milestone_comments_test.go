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

package githubhost

// The milestone comment read at the unit tier. What is worth pinning is the
// SHAPE the host answers with — one round trip, bucketed by issue, a null
// author surviving as an empty login — and the guards that keep a decorative
// read from becoming an expensive or failing one.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

const commentsResponse = `{"data":{"repository":{"milestone":{"issues":{
  "pageInfo":{"hasNextPage":false},
  "nodes":[
    {"number":12,"comments":{"nodes":[
      {"id":"IC_1","body":"starting the todo-api service","url":"https://gh/c/1",
       "createdAt":"2026-08-24T10:00:00Z","author":{"login":"aep-bot"}},
      {"id":"IC_2","body":"service is green","url":"https://gh/c/2",
       "createdAt":"2026-08-24T10:05:00Z","author":{"login":"aep-bot"}}]}},
    {"number":14,"comments":{"nodes":[
      {"id":"IC_3","body":"left by a since-deleted account","url":"https://gh/c/3",
       "createdAt":"2026-08-24T10:06:00Z","author":null}]}},
    {"number":15,"comments":{"nodes":[]}}
  ]}}}}}`

func TestListMilestoneIssueComments_BucketsByIssueInThreadOrder(t *testing.T) {
	c, cap := newGraphQLFake(t, http.StatusOK, commentsResponse)

	got, err := c.ListMilestoneIssueComments(context.Background(), "acme", "widgets", stubCred{}, 3, 10)
	if err != nil {
		t.Fatalf("ListMilestoneIssueComments: %v", err)
	}

	// An issue whose thread is empty is ABSENT, not present-and-empty: the
	// caller indexes the map and nil is the same answer with no allocation.
	if len(got) != 2 {
		t.Fatalf("buckets = %d (%v), want 2 — issue 15 has no comments and must be absent", len(got), got)
	}
	if len(got[12]) != 2 {
		t.Fatalf("issue 12 comments = %d, want 2", len(got[12]))
	}
	// Thread order, oldest first — `last:` returns the tail of the thread
	// already in order, so nothing here may reverse it.
	if got[12][0].ID != "IC_1" || got[12][1].ID != "IC_2" {
		t.Fatalf("issue 12 order = %s,%s, want IC_1,IC_2", got[12][0].ID, got[12][1].ID)
	}
	first := got[12][0]
	if first.Author != "aep-bot" || first.Body != "starting the todo-api service" ||
		first.URL != "https://gh/c/1" {
		t.Fatalf("comment projection = %+v", first)
	}
	if want := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC); !first.CreatedAt.Equal(want) {
		t.Fatalf("createdAt = %s, want %s", first.CreatedAt, want)
	}

	// A deleted account is a fact about the comment, not a read failure: the
	// comment survives with an empty login.
	if len(got[14]) != 1 {
		t.Fatalf("issue 14 comments = %d, want 1", len(got[14]))
	}
	if got[14][0].Author != "" {
		t.Fatalf("null author = %q, want empty", got[14][0].Author)
	}

	// ONE round trip is the reason this is GraphQL at all.
	var sent struct {
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal([]byte(cap.body), &sent); err != nil {
		t.Fatalf("request body not json: %v (%s)", err, cap.body)
	}
	if sent.Variables["m"] != float64(3) || sent.Variables["c"] != float64(10) {
		t.Fatalf("variables = %v, want milestone 3 / 10 per issue", sent.Variables)
	}
	if sent.Variables["i"] != float64(milestoneIssuePage) {
		t.Fatalf("issue page = %v, want %d", sent.Variables["i"], milestoneIssuePage)
	}
}

// The platform's own comments are BRANDED on write and reported on read, and the
// brand never survives into the body — it exists to be invisible.
func TestListMilestoneIssueComments_ReportsAndStripsTheMachineBrand(t *testing.T) {
	resp := `{"data":{"repository":{"milestone":{"issues":{
      "pageInfo":{"hasNextPage":false},
      "nodes":[
        {"number":12,"comments":{"nodes":[
          {"id":"IC_M","body":"` + sourcecontrol.MachineCommentMarker + `\n✅ Provisioned. Platform resource ready.",
           "url":"https://gh/c/9","createdAt":"2026-08-24T10:00:00Z","author":{"login":"aep-bot"}},
          {"id":"IC_H","body":"watch the auth header","url":"https://gh/c/10",
           "createdAt":"2026-08-24T10:01:00Z","author":{"login":"anjanas"}}]}}
      ]}}}}}`
	c, _ := newGraphQLFake(t, http.StatusOK, resp)

	got, err := c.ListMilestoneIssueComments(context.Background(), "acme", "widgets", stubCred{}, 3, 10)
	if err != nil {
		t.Fatalf("ListMilestoneIssueComments: %v", err)
	}
	bucket := got[12]
	if len(bucket) != 2 {
		t.Fatalf("comments = %d, want both (the HOST reports, it does not filter)", len(bucket))
	}
	if !bucket[0].Machine {
		t.Fatalf("branded comment not reported as machine: %+v", bucket[0])
	}
	if strings.Contains(bucket[0].Body, sourcecontrol.MachineCommentMarker) {
		t.Fatalf("the brand leaked into the body: %q", bucket[0].Body)
	}
	if bucket[0].Body != "✅ Provisioned. Platform resource ready." {
		t.Fatalf("body after stripping = %q", bucket[0].Body)
	}
	// An unbranded comment is a human's (or the agent's) and stays that way.
	if bucket[1].Machine {
		t.Fatalf("unbranded comment reported as machine: %+v", bucket[1])
	}
}

// A note that QUOTES the brand is not a machine comment. The agent is told to
// read issue comments (`gh issue view --comments`, where the raw marker is
// visible), so a body that mentions it further down is a real hazard — and the
// two failures are not symmetric: a machine comment slipping through is visible
// noise somebody can report, while a human note classified as machine is data
// loss nobody can see. Hence the prefix test, and hence this test.
func TestListMilestoneIssueComments_QuotedBrandIsNotAMachineComment(t *testing.T) {
	resp := `{"data":{"repository":{"milestone":{"issues":{
      "pageInfo":{"hasNextPage":false},
      "nodes":[
        {"number":12,"comments":{"nodes":[
          {"id":"IC_Q","body":"the platform posted this above:\n\n> ` +
		sourcecontrol.MachineCommentMarker + `\n> ✅ Provisioned.\n\nso the wiring is in place",
           "url":"https://gh/c/11","createdAt":"2026-08-24T10:00:00Z","author":{"login":"anjanas"}}]}}
      ]}}}}}`
	c, _ := newGraphQLFake(t, http.StatusOK, resp)

	got, err := c.ListMilestoneIssueComments(context.Background(), "acme", "widgets", stubCred{}, 3, 10)
	if err != nil {
		t.Fatalf("ListMilestoneIssueComments: %v", err)
	}
	if len(got[12]) != 1 {
		t.Fatalf("comments = %d, want 1", len(got[12]))
	}
	if got[12][0].Machine {
		t.Fatalf("a comment quoting the brand was classified machine — a human note would vanish: %+v", got[12][0])
	}
	// And its body is untouched: nothing is stripped out of the middle of a
	// comment the platform did not write.
	if !strings.Contains(got[12][0].Body, sourcecontrol.MachineCommentMarker) {
		t.Fatalf("the quoted marker was stripped from a human's body: %q", got[12][0].Body)
	}
}

// perIssue <= 0 is a coherent request — "no comments" — and must not become a
// GraphQL error, nor spend a round trip to find that out.
func TestListMilestoneIssueComments_ZeroPerIssueCostsNoRoundTrip(t *testing.T) {
	c, cap := newGraphQLFake(t, http.StatusOK, commentsResponse)

	got, err := c.ListMilestoneIssueComments(context.Background(), "acme", "widgets", stubCred{}, 3, 0)
	if err != nil {
		t.Fatalf("ListMilestoneIssueComments: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("comments = %v, want empty", got)
	}
	if cap.body != "" {
		t.Fatalf("a round trip was spent for zero comments: %s", cap.body)
	}
}

// GraphQL rejects a `last:` above 100 as a query ERROR rather than trimming it,
// so an oversized ask must be clamped here — a caller's bad number must not turn
// a decorative read into a failed one.
func TestListMilestoneIssueComments_ClampsPerIssueToHostLimit(t *testing.T) {
	c, cap := newGraphQLFake(t, http.StatusOK, commentsResponse)

	if _, err := c.ListMilestoneIssueComments(context.Background(), "acme", "widgets", stubCred{}, 3, 5000); err != nil {
		t.Fatalf("ListMilestoneIssueComments: %v", err)
	}
	var sent struct {
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal([]byte(cap.body), &sent); err != nil {
		t.Fatalf("request body not json: %v", err)
	}
	if sent.Variables["c"] != float64(maxCommentsPerIssue) {
		t.Fatalf("per-issue = %v, want clamped to %d", sent.Variables["c"], maxCommentsPerIssue)
	}
}

// A milestone that does not exist answers null, which the decode must report as
// ErrMilestoneNotFound rather than an empty map — "no such version" and "a
// version whose issues carry no comments" are different answers.
func TestListMilestoneIssueComments_MissingMilestone(t *testing.T) {
	c, _ := newGraphQLFake(t, http.StatusOK, `{"data":{"repository":{"milestone":null}}}`)

	_, err := c.ListMilestoneIssueComments(context.Background(), "acme", "widgets", stubCred{}, 99, 10)
	if err != sourcecontrol.ErrMilestoneNotFound {
		t.Fatalf("err = %v, want ErrMilestoneNotFound", err)
	}
}

func TestListMilestoneIssueComments_RejectsNonPositiveMilestone(t *testing.T) {
	c, cap := newGraphQLFake(t, http.StatusOK, commentsResponse)

	if _, err := c.ListMilestoneIssueComments(context.Background(), "acme", "widgets", stubCred{}, 0, 10); err != sourcecontrol.ErrMilestoneNotFound {
		t.Fatalf("err = %v, want ErrMilestoneNotFound", err)
	}
	if cap.body != "" {
		t.Fatalf("a round trip was spent on milestone 0: %s", cap.body)
	}
}
