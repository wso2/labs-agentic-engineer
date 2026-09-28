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

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// stubCred is a fixed-token credential for driving the real client against an
// httptest fake (mirrors the stubCred in feature tests).
type stubCred struct{}

func (stubCred) Token(context.Context) (string, time.Time, error) { return "tok", time.Time{}, nil }
func (stubCred) Identity() secrets.Identity {
	return secrets.Identity{Name: "Bot", Email: "bot@aep.dev", Login: "bot"}
}
func (stubCred) RepoOwner() string                        { return "acme" }
func (stubCred) WebhookStrategy() secrets.WebhookStrategy { return secrets.WebhookPlatform }

// capture records the one request an httptest fake receives.
type capture struct {
	method      string
	escapedPath string
	body        string
}

// newFake starts an httptest server that records the request and replies with
// status+respBody, and returns the real client pointed at it plus the capture.
func newFake(t *testing.T, status int, respBody string) (*Client, *capture) {
	t.Helper()
	cap := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		cap.method = r.Method
		cap.escapedPath = r.URL.EscapedPath()
		cap.body = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	c, ok := NewClient(WithAPIBase(srv.URL)).(*Client)
	if !ok {
		t.Fatalf("NewClient did not return *Client")
	}
	return c, cap
}

func TestRecurrenceStateReasonReads(t *testing.T) {
	for _, reason := range []string{"completed", "not_planned", "reopened", ""} {
		t.Run(reason, func(t *testing.T) {
			payload := `{"number":42,"state":"closed","state_reason":"` + reason + `","closed_at":"2026-09-18T08:00:00Z","labels":[{"name":"incident"}]}`
			client, _ := newFake(t, http.StatusOK, "["+payload+"]")
			issues, err := client.ListIssues(context.Background(), "acme", "repo", stubCred{}, nil)
			if err != nil || len(issues) != 1 {
				t.Fatalf("list = %+v, %v", issues, err)
			}
			if issues[0].StateReason != reason {
				t.Errorf("list state reason = %q, want %q", issues[0].StateReason, reason)
			}
			if issues[0].ClosedAt != "2026-09-18T08:00:00Z" {
				t.Errorf("list closure identity = %q", issues[0].ClosedAt)
			}
			client, _ = newFake(t, http.StatusOK, payload)
			issue, err := client.GetIssue(context.Background(), "acme", "repo", stubCred{}, 42)
			if err != nil {
				t.Fatal(err)
			}
			if issue.StateReason != reason {
				t.Errorf("detail state reason = %q, want %q", issue.StateReason, reason)
			}
			if issue.ClosedAt != "2026-09-18T08:00:00Z" {
				t.Errorf("detail closure identity = %q", issue.ClosedAt)
			}
		})
	}
}

func TestAddIssueLabels(t *testing.T) {
	c, cap := newFake(t, http.StatusOK, `[]`)
	if err := c.AddIssueLabels(context.Background(), "acme", "repo", stubCred{}, 42, []string{"aep:status/pending", "aep:attention"}); err != nil {
		t.Fatalf("AddIssueLabels: %v", err)
	}
	if cap.method != http.MethodPost {
		t.Errorf("method = %s; want POST", cap.method)
	}
	if cap.escapedPath != "/repos/acme/repo/issues/42/labels" {
		t.Errorf("path = %s", cap.escapedPath)
	}
	var got map[string][]string
	if err := json.Unmarshal([]byte(cap.body), &got); err != nil {
		t.Fatalf("body not json: %v (%s)", err, cap.body)
	}
	if len(got["labels"]) != 2 || got["labels"][0] != "aep:status/pending" {
		t.Errorf("labels payload = %v", got["labels"])
	}
}

func TestAddIssueLabelsError(t *testing.T) {
	c, _ := newFake(t, http.StatusForbidden, `{"message":"no"}`)
	if err := c.AddIssueLabels(context.Background(), "acme", "repo", stubCred{}, 1, []string{"x"}); err == nil {
		t.Fatalf("expected error on 403")
	}
}

// TestRemoveIssueLabel checks the label name is path-escaped ('/' → %2F) and
// that 404 (label already absent) is success.
func TestRemoveIssueLabel(t *testing.T) {
	c, cap := newFake(t, http.StatusOK, `[]`)
	if err := c.RemoveIssueLabel(context.Background(), "acme", "repo", stubCred{}, 7, "aep:status/pending"); err != nil {
		t.Fatalf("RemoveIssueLabel: %v", err)
	}
	if cap.method != http.MethodDelete {
		t.Errorf("method = %s; want DELETE", cap.method)
	}
	if cap.escapedPath != "/repos/acme/repo/issues/7/labels/aep:status%2Fpending" {
		t.Errorf("path not escaped correctly: %s", cap.escapedPath)
	}

	c404, _ := newFake(t, http.StatusNotFound, `{"message":"Label does not exist"}`)
	if err := c404.RemoveIssueLabel(context.Background(), "acme", "repo", stubCred{}, 7, "provision"); err != nil {
		t.Fatalf("404 should be treated as success (label already absent): %v", err)
	}

	c500, _ := newFake(t, http.StatusInternalServerError, `{"message":"boom"}`)
	if err := c500.RemoveIssueLabel(context.Background(), "acme", "repo", stubCred{}, 7, "provision"); err == nil {
		t.Fatalf("expected error on 500")
	}
}

func TestSetIssueLabels(t *testing.T) {
	c, cap := newFake(t, http.StatusOK, `[]`)
	if err := c.SetIssueLabels(context.Background(), "acme", "repo", stubCred{}, 3, []string{"aep:task"}); err != nil {
		t.Fatalf("SetIssueLabels: %v", err)
	}
	if cap.method != http.MethodPut {
		t.Errorf("method = %s; want PUT", cap.method)
	}
	if cap.escapedPath != "/repos/acme/repo/issues/3/labels" {
		t.Errorf("path = %s", cap.escapedPath)
	}

	// nil labels must serialize as an explicit empty array (clear), not null.
	cNil, capNil := newFake(t, http.StatusOK, `[]`)
	if err := cNil.SetIssueLabels(context.Background(), "acme", "repo", stubCred{}, 3, nil); err != nil {
		t.Fatalf("SetIssueLabels(nil): %v", err)
	}
	if capNil.body != `{"labels":[]}` {
		t.Errorf("nil labels body = %s; want an explicit empty array", capNil.body)
	}
}

func TestUpdateWebhookEvents(t *testing.T) {
	c, cap := newFake(t, http.StatusOK, `{"id":99}`)
	if err := c.UpdateWebhookEvents(context.Background(), "acme", "repo", stubCred{}, 99, []string{"pull_request", "push", "issues"}); err != nil {
		t.Fatalf("UpdateWebhookEvents: %v", err)
	}
	if cap.method != http.MethodPatch {
		t.Errorf("method = %s; want PATCH", cap.method)
	}
	if cap.escapedPath != "/repos/acme/repo/hooks/99" {
		t.Errorf("path = %s", cap.escapedPath)
	}
	var got map[string][]string
	if err := json.Unmarshal([]byte(cap.body), &got); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	found := false
	for _, e := range got["events"] {
		if e == "issues" {
			found = true
		}
	}
	if !found {
		t.Errorf("events payload missing 'issues': %v", got["events"])
	}
}

// fakeIssuePages serves GET /repos/acme/repo/issues through page(n), returning
// the real client pointed at it and the query of every request received.
func fakeIssuePages(t *testing.T, page func(n int) string) (*Client, *[]url.Values) {
	t.Helper()
	var queries []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query())
		n, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			http.Error(w, "missing page", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, page(n))
	}))
	t.Cleanup(srv.Close)
	c, ok := NewClient(WithAPIBase(srv.URL)).(*Client)
	if !ok {
		t.Fatalf("NewClient did not return *Client")
	}
	return c, &queries
}

// issueListPage renders count issues numbered from first; every prEvery-th
// item (1-based, 0 for none) is a pull request.
func issueListPage(first, count, prEvery int) string {
	items := make([]string, 0, count)
	for i := 0; i < count; i++ {
		n := first + i
		item := fmt.Sprintf(`{"number":%d,"title":"T%d","state":"open","labels":[]`, n, n)
		if prEvery > 0 && (i+1)%prEvery == 0 {
			item += fmt.Sprintf(`,"pull_request":{"url":"https://api.github.com/repos/acme/repo/pulls/%d"}`, n)
		}
		items = append(items, item+"}")
	}
	return "[" + strings.Join(items, ",") + "]"
}

// TestListIssuesExcludesPullRequests: GitHub's issues endpoint answers pull
// requests alongside issues, each carrying a pull_request member.
func TestListIssuesExcludesPullRequests(t *testing.T) {
	c, _ := fakeIssuePages(t, func(int) string { return issueListPage(1, 2, 2) })
	issues, err := c.ListIssues(context.Background(), "acme", "repo", stubCred{}, nil)
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 1 {
		t.Fatalf("issues = %+v, want only the non-PR issue #1", issues)
	}
}

// TestListIssuesFollowsPages: a full page means more follow, and the page
// length (pull requests included) decides the walk, not the kept count.
func TestListIssuesFollowsPages(t *testing.T) {
	c, queries := fakeIssuePages(t, func(n int) string {
		if n == 1 {
			return issueListPage(1, milestonePageSize, 10) // full page, 10 of them PRs
		}
		return issueListPage(101, 1, 0)
	})
	issues, err := c.ListIssues(context.Background(), "acme", "repo", stubCred{}, []string{"incident"})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 91 || issues[90].Number != 101 {
		t.Fatalf("got %d issues (last %+v), want 91 ending at #101", len(issues), issues[len(issues)-1])
	}
	if len(*queries) != 2 {
		t.Fatalf("requests = %d, want 2", len(*queries))
	}
	for i, q := range *queries {
		if q.Get("page") != strconv.Itoa(i+1) || q.Get("labels") != "incident" || q.Get("state") != "all" {
			t.Errorf("request %d query = %v", i+1, q)
		}
	}
}

// TestListIssuesStopsAtPageCap: the walk is bounded so one list call cannot
// spend an unbounded share of the installation's rate budget.
func TestListIssuesStopsAtPageCap(t *testing.T) {
	c, queries := fakeIssuePages(t, func(n int) string {
		return issueListPage((n-1)*milestonePageSize+1, milestonePageSize, 0)
	})
	issues, err := c.ListIssues(context.Background(), "acme", "repo", stubCred{}, nil)
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(*queries) != issueListMaxPages {
		t.Fatalf("requests = %d, want the cap %d", len(*queries), issueListMaxPages)
	}
	if len(issues) != issueListMaxPages*milestonePageSize {
		t.Fatalf("issues = %d, want %d", len(issues), issueListMaxPages*milestonePageSize)
	}
}
