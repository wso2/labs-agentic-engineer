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

package edge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/github/githubtest"
)

// The /internal/v1 issue, milestone and pull request ops through the real
// routes: cap → gate → validator → owner guard → github.Handler. The
// handler's own behaviour is internal/github's handler_test; these pin the
// wiring.

const ghRepoPath = "/internal/v1/repos/acme-gh/greeter"

// gitHubRequests is one valid request per GitHub op, under base.
func gitHubRequests(h *harness, base string) map[string]func() *httptest.ResponseRecorder {
	get := func(p string) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder { return h.do("GET", base+p, h.m2m(), "ou-1", nil) }
	}
	send := func(method, p, body string) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder { return h.doJSON(method, base+p, body) }
	}
	return map[string]func() *httptest.ResponseRecorder{
		"list-issues":             get("/issues?labels=aep"),
		"create-issue":            send("POST", "/issues", `{"title":"t","body":"b"}`),
		"get-issue":               get("/issues/7"),
		"list-issue-comments":     get("/issues/7/comments?limit=5"),
		"create-issue-comment":    send("POST", "/issues/7/comments", `{"body":"b"}`),
		"close-issue":             send("POST", "/issues/7/close", ``),
		"reopen-issue":            send("POST", "/issues/7/reopen", ``),
		"set-issue-body":          send("PUT", "/issues/7/body", `{"body":"b"}`),
		"set-issue-title":         send("PUT", "/issues/7/title", `{"title":"t"}`),
		"set-issue-milestone":     send("PUT", "/issues/7/milestone", `{"number":3}`),
		"add-issue-labels":        send("POST", "/issues/7/labels", `{"labels":["x"]}`),
		"set-issue-labels":        send("PUT", "/issues/7/labels", `{"labels":[]}`),
		"remove-issue-label":      send("DELETE", "/issues/7/labels/x", ``),
		"ensure-label":            send("POST", "/labels", `{"name":"aep","color":"ededed"}`),
		"list-milestones":         get("/milestones?state=open"),
		"create-milestone":        send("POST", "/milestones", `{"title":"v1"}`),
		"close-milestone":         send("POST", "/milestones/3/close", ``),
		"reopen-milestone":        send("POST", "/milestones/3/reopen", ``),
		"list-milestone-issues":   get("/milestones/3/issues?state=all&labels=aep"),
		"get-milestone-counts":    get("/milestones/3/counts"),
		"list-milestone-comments": get("/milestones/3/comments?perIssue=5"),
		"get-pull":                get("/pulls/5"),
		"merge-pull":              send("POST", "/pulls/5/merge", ``),
		"list-pull-files":         get("/pulls/5/files"),
	}
}

func TestInternalGitHub_OwnerGuardCoversEveryOp(t *testing.T) {
	gh := githubtest.NewStub(t)
	h := newHarness(t, withGitHubAPI(gh))
	for name, op := range gitHubRequests(h, "/internal/v1/repos/someone-else/greeter") {
		if rec := op(); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"code":"owner_not_allowed"`) {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if n := len(gh.Requests()); n != 0 {
		t.Fatalf("a refused owner's request reached GitHub %d times", n)
	}
}

// TestInternalGitHub_EveryOpIsRouted: every op passes the validator with a
// valid request and reaches GitHub (the stub answers 404 to all but the
// seeded issue, so a 2xx, 404 or 502 proves the route; a 400 or a 405 would
// be the wiring's).
func TestInternalGitHub_EveryOpIsRouted(t *testing.T) {
	gh := githubtest.NewStub(t)
	gh.SeedIssue("acme-gh", "greeter", 7, "Task A", []string{"aep"})
	h := newHarness(t, withGitHubAPI(gh))
	for name, op := range gitHubRequests(h, ghRepoPath) {
		before := len(gh.Requests())
		rec := op()
		if rec.Code == http.StatusBadRequest || rec.Code == http.StatusNotFound && !strings.Contains(rec.Body.String(), "_not_found") ||
			rec.Code == http.StatusMethodNotAllowed || rec.Code >= 500 && rec.Code != http.StatusBadGateway {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
		if len(gh.Requests()) == before {
			t.Fatalf("%s: never reached GitHub (%d)", name, rec.Code)
		}
	}
	rec := h.do("GET", ghRepoPath+"/issues/7", h.m2m(), "ou-1", nil)
	if rec.Code != http.StatusOK || jsonBody(t, rec.Body.Bytes())["title"] != "Task A" {
		t.Fatalf("get-issue: %d %s", rec.Code, rec.Body.String())
	}
}

// TestInternalGitHub_LabelWithSlash: a label holding '/' travels as one
// percent-encoded segment and reaches GitHub as that label.
func TestInternalGitHub_LabelWithSlash(t *testing.T) {
	gh := githubtest.NewStub(t)
	gh.On("DELETE", "/repos/acme-gh/greeter/issues/7/labels/src/validation", 200, `[]`)
	h := newHarness(t, withGitHubAPI(gh))
	rec := h.doJSON("DELETE", ghRepoPath+"/issues/7/labels/src%2Fvalidation", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	reqs := gh.Requests()
	if len(reqs) != 1 || reqs[0].Path != "/repos/acme-gh/greeter/issues/7/labels/src/validation" {
		t.Fatalf("GitHub got %+v", reqs)
	}
}

// TestInternalGitHub_RateLimitOnTheWire: GitHub's rate limit is 429
// github_rate_limited with Retry-After.
func TestInternalGitHub_RateLimitOnTheWire(t *testing.T) {
	gh := githubtest.NewStub(t)
	h := newHarness(t, withGitHubAPI(gh))
	gh.FailNext(http.StatusForbidden, map[string]string{"Retry-After": "7"})
	rec := h.do("GET", ghRepoPath+"/milestones", h.m2m(), "ou-1", nil)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "7" ||
		!strings.Contains(rec.Body.String(), `"code":"github_rate_limited"`) {
		t.Fatalf("%d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}
}

// TestInternalGitHub_ValidatorRefuses: the contract's rules hold before any
// GitHub call.
func TestInternalGitHub_ValidatorRefuses(t *testing.T) {
	gh := githubtest.NewStub(t)
	h := newHarness(t, withGitHubAPI(gh))
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"unknown field":     h.doJSON("POST", ghRepoPath+"/issues", `{"title":"t","body":"b","assignee":"x"}`),
		"blank title":       h.doJSON("POST", ghRepoPath+"/issues", `{"title":"","body":"b"}`),
		"limit missing":     h.do("GET", ghRepoPath+"/issues/7/comments", h.m2m(), "ou-1", nil),
		"limit over 100":    h.do("GET", ghRepoPath+"/issues/7/comments?limit=101", h.m2m(), "ou-1", nil),
		"number zero":       h.do("GET", ghRepoPath+"/issues/0", h.m2m(), "ou-1", nil),
		"bad color":         h.doJSON("POST", ghRepoPath+"/labels", `{"name":"aep","color":"#ededed"}`),
		"blank milestone":   h.doJSON("POST", ghRepoPath+"/milestones", `{"title":"  "}`),
		"bad state":         h.do("GET", ghRepoPath+"/milestones?state=draft", h.m2m(), "ou-1", nil),
		"label too long":    h.doJSON("POST", ghRepoPath+"/issues/7/labels", `{"labels":["`+strings.Repeat("x", 51)+`"]}`),
		"milestone missing": h.doJSON("PUT", ghRepoPath+"/issues/7/milestone", `{}`),
	} {
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if n := len(gh.Requests()); n != 0 {
		t.Fatalf("a refused request reached GitHub %d times", n)
	}
}
