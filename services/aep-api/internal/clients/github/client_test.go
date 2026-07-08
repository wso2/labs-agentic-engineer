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
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/credentials"
)

// stubCred is a fixed-token credential for driving the real client against an
// httptest fake (mirrors the stubCred in feature tests).
type stubCred struct{}

func (stubCred) Token(context.Context) (string, time.Time, error) { return "tok", time.Time{}, nil }
func (stubCred) Identity() credentials.Identity {
	return credentials.Identity{Name: "Bot", Email: "bot@aep.dev", Login: "bot"}
}
func (stubCred) RepoOwner() string                            { return "acme" }
func (stubCred) WebhookStrategy() credentials.WebhookStrategy { return credentials.WebhookPlatform }

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
	if err := c404.RemoveIssueLabel(context.Background(), "acme", "repo", stubCred{}, 7, "aep:execute"); err != nil {
		t.Fatalf("404 should be treated as success (label already absent): %v", err)
	}

	c500, _ := newFake(t, http.StatusInternalServerError, `{"message":"boom"}`)
	if err := c500.RemoveIssueLabel(context.Background(), "acme", "repo", stubCred{}, 7, "aep:execute"); err == nil {
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

// routedFake starts an httptest server that dispatches by method, so a test
// can drive a multi-request sequence (merge PUT, then a fallback GET) unlike
// newFake's single fixed response.
func routedFake(t *testing.T, byMethod map[string]struct {
	status int
	body   string
}) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, ok := byMethod[r.Method]
		if !ok {
			t.Fatalf("unexpected method %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.status)
		_, _ = io.WriteString(w, resp.body)
	}))
	t.Cleanup(srv.Close)
	c, ok := NewClient(WithAPIBase(srv.URL)).(*Client)
	if !ok {
		t.Fatalf("NewClient did not return *Client")
	}
	return c
}

func TestMergePullRequest_Success(t *testing.T) {
	c, cap := newFake(t, http.StatusOK, `{"merged":true,"message":"Pull Request successfully merged"}`)
	if err := c.MergePullRequest(context.Background(), "acme", "repo", stubCred{}, 7); err != nil {
		t.Fatalf("MergePullRequest: %v", err)
	}
	if cap.method != http.MethodPut {
		t.Errorf("method = %s; want PUT", cap.method)
	}
	if cap.escapedPath != "/repos/acme/repo/pulls/7/merge" {
		t.Errorf("path = %s", cap.escapedPath)
	}
}

// TestMergePullRequest_AlreadyMerged covers the idempotency fallback: a 405
// from the merge PUT (GitHub's response for "not mergeable right now", which
// also covers "already merged") is not an error if the live PR state shows it
// merged — a Temporal retry of an activity whose success response was lost
// must not fail.
func TestMergePullRequest_AlreadyMerged(t *testing.T) {
	c := routedFake(t, map[string]struct {
		status int
		body   string
	}{
		http.MethodPut: {http.StatusMethodNotAllowed, `{"message":"Pull Request is not mergeable"}`},
		http.MethodGet: {http.StatusOK, `{"state":"closed","merged":true,"merge_commit_sha":"abc123"}`},
	})
	if err := c.MergePullRequest(context.Background(), "acme", "repo", stubCred{}, 7); err != nil {
		t.Fatalf("MergePullRequest(already merged) should not error: %v", err)
	}
}

// TestMergePullRequest_NotMergeable covers the real-blocker case: a 405 whose
// live PR state is NOT merged surfaces the original error.
func TestMergePullRequest_NotMergeable(t *testing.T) {
	c := routedFake(t, map[string]struct {
		status int
		body   string
	}{
		http.MethodPut: {http.StatusMethodNotAllowed, `{"message":"Required status check has not passed"}`},
		http.MethodGet: {http.StatusOK, `{"state":"open","merged":false}`},
	})
	if err := c.MergePullRequest(context.Background(), "acme", "repo", stubCred{}, 7); err == nil {
		t.Fatalf("MergePullRequest should surface the real blocker")
	}
}
