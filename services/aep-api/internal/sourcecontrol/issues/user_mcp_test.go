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

package issues_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol/issues"
)

// recordingIssues is an IssueService that records the two calls the Issues
// agent's tools make. Any other method panics (nil embedded interface).
type recordingIssues struct {
	sourcecontrol.IssueService
	listed   []sourcecontrol.IssueInfo
	listErr  error
	creates  []createCall
	lists    []createCall
	boundOrg string
}

type createCall struct {
	org, project string
	req          sourcecontrol.CreateIssueRequest
}

func (f *recordingIssues) ListIssues(ctx context.Context, org, project string, labels []string) ([]sourcecontrol.IssueInfo, error) {
	f.lists = append(f.lists, createCall{org: org, project: project, req: sourcecontrol.CreateIssueRequest{Labels: labels}})
	f.boundOrg = tenant.BoundOrgFromContext(ctx)
	return f.listed, f.listErr
}

func (f *recordingIssues) CreateIssue(ctx context.Context, org, project string, req sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error) {
	f.creates = append(f.creates, createCall{org: org, project: project, req: req})
	f.boundOrg = tenant.BoundOrgFromContext(ctx)
	return &sourcecontrol.IssueResult{Number: 15, URL: "https://github.com/acme/acme-expenses/issues/15", NodeID: "secret-node"}, nil
}

// issuesScope is the Issues view's scope: the project, no issue.
var issuesScope = auth.IssuesMCPScope{OrgID: "acme", ProjectID: "acme-expenses"}

// scopedRPC POSTs one JSON-RPC message to the user MCP handler with the
// Issues view's project scope the verifier would bind (none when !scoped).
func scopedRPC(t *testing.T, h http.Handler, scoped bool, msg map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	if !scoped {
		return rpcAs(t, h, nil, msg)
	}
	return rpcAs(t, h, &issuesScope, msg)
}

// rpcAs POSTs one JSON-RPC message bound to scope (nil binds none).
func rpcAs(t *testing.T, h http.Handler, scope *auth.IssuesMCPScope, msg map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(msg)
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/issues/mcp", strings.NewReader(string(body)))
	if scope != nil {
		req = req.WithContext(auth.WithIssuesMCPScope(req.Context(), *scope))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// listedTools answers tools/list under scope: each tool's name and declared
// argument names.
func listedTools(t *testing.T, h http.Handler, scope auth.IssuesMCPScope) ([]string, map[string]map[string]any) {
	t.Helper()
	w := rpcAs(t, h, &scope, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	var resp struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	props := map[string]map[string]any{}
	for _, tool := range resp.Result.Tools {
		names = append(names, tool.Name)
		p, _ := tool.InputSchema["properties"].(map[string]any)
		props[tool.Name] = p
	}
	return names, props
}

func callUserTool(t *testing.T, h http.Handler, name string, args map[string]any) (string, bool) {
	t.Helper()
	return callToolAs(t, h, issuesScope, name, args)
}

// callToolAs calls one tool bound to scope and returns its text and isError.
func callToolAs(t *testing.T, h http.Handler, scope auth.IssuesMCPScope, name string, args map[string]any) (string, bool) {
	t.Helper()
	w := rpcAs(t, h, &scope, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", w.Code, w.Body.String())
	}
	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct{ Message string } `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil || len(resp.Result.Content) != 1 {
		t.Fatalf("not a tool result: %+v", resp)
	}
	return resp.Result.Content[0].Text, resp.Result.IsError
}

// The Issues view's token lists exactly its two tools, and none of them can
// name the scope the session fixed.
// newHandler serves the Issues view's tools over fake; it wires no issue-agent
// ports (the Issues view's tools need none).
func newHandler(fake sourcecontrol.IssueService) http.Handler {
	return issues.NewUserMCPHandler(fake, issues.IssueAgentPorts{})
}

func TestUserMCPListsExactlyTheTwoTools(t *testing.T) {
	names, props := listedTools(t, newHandler(&recordingIssues{}), issuesScope)
	for name, p := range props {
		for _, banned := range []string{"project", "org", "namespace", "labels"} {
			if _, ok := p[banned]; ok {
				t.Errorf("%s declares %q: the scope is fixed by the session", name, banned)
			}
		}
	}
	if !reflect.DeepEqual(names, []string{"search_issues", "create_issue"}) {
		t.Fatalf("tools = %v, want [search_issues create_issue]", names)
	}
}

func TestUserMCPCreateIssueFilesIntoTheScopedProject(t *testing.T) {
	fake := &recordingIssues{}
	h := newHandler(fake)
	text, isErr := callUserTool(t, h, "create_issue", map[string]any{
		"title": "Save button does nothing", "body": "Steps…", "kind": "bug",
		// The model cannot steer these: unknown arguments are ignored.
		"labels": []string{"incident", "aep"}, "project": "other-project", "org": "evil",
	})
	if isErr {
		t.Fatalf("create failed: %s", text)
	}
	want := []createCall{{org: "acme", project: "acme-expenses", req: sourcecontrol.CreateIssueRequest{
		Title: "Save button does nothing", Body: "Steps…", Labels: []string{"bug", "src/user"},
	}}}
	if !reflect.DeepEqual(fake.creates, want) {
		t.Fatalf("CreateIssue calls = %+v, want %+v", fake.creates, want)
	}
	if fake.boundOrg != "acme" {
		t.Errorf("bound org = %q, want acme", fake.boundOrg)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, map[string]any{"number": float64(15), "url": "https://github.com/acme/acme-expenses/issues/15"}) {
		t.Fatalf("result = %s", text)
	}
}

func TestUserMCPCreateIssueRefusesUnknownKind(t *testing.T) {
	for _, kind := range []any{"epic", "", nil, "incident"} {
		fake := &recordingIssues{}
		text, isErr := callUserTool(t, newHandler(fake), "create_issue", map[string]any{
			"title": "x", "body": "y", "kind": kind,
		})
		if !isErr || len(fake.creates) != 0 {
			t.Errorf("kind %v: isError=%v calls=%d text=%q, want a tool error and no call", kind, isErr, len(fake.creates), text)
		}
	}
}

func TestUserMCPCreateIssueRequiresTitleAndBody(t *testing.T) {
	fake := &recordingIssues{}
	h := newHandler(fake)
	if _, isErr := callUserTool(t, h, "create_issue", map[string]any{"body": "y", "kind": "bug"}); !isErr {
		t.Error("missing title should be a tool error")
	}
	if _, isErr := callUserTool(t, h, "create_issue", map[string]any{"title": "x", "kind": "bug"}); !isErr {
		t.Error("missing body should be a tool error")
	}
	if len(fake.creates) != 0 {
		t.Errorf("CreateIssue called %d times", len(fake.creates))
	}
}

func TestUserMCPSearchRanksFiltersAndTruncates(t *testing.T) {
	long := strings.Repeat("é", 700)
	fake := &recordingIssues{listed: []sourcecontrol.IssueInfo{
		{Number: 1, Title: "Login page slow", Body: "nothing", State: "open", URL: "u1"},
		{Number: 2, Title: "Save button broken", Body: long + " save button", State: "open", Labels: []string{"bug"}, URL: "u2"},
		{Number: 3, Title: "Save button old", Body: "save button", State: "closed", URL: "u3"},
	}}
	h := newHandler(fake)

	text, isErr := callUserTool(t, h, "search_issues", map[string]any{"query": "save button", "project": "other", "labels": []string{"x"}})
	if isErr {
		t.Fatalf("search failed: %s", text)
	}
	if len(fake.lists) != 1 || fake.lists[0].org != "acme" || fake.lists[0].project != "acme-expenses" || fake.lists[0].req.Labels != nil {
		t.Fatalf("ListIssues calls = %+v, want acme/acme-expenses with no labels", fake.lists)
	}
	var hits []struct {
		Number int      `json:"number"`
		Title  string   `json:"title"`
		State  string   `json:"state"`
		Labels []string `json:"labels"`
		URL    string   `json:"url"`
		Body   string   `json:"body"`
	}
	if err := json.Unmarshal([]byte(text), &hits); err != nil {
		t.Fatalf("decode %q: %v", text, err)
	}
	if len(hits) == 0 || hits[0].Number != 2 {
		t.Fatalf("hits = %+v, want #2 first (open, default state)", hits)
	}
	for _, hit := range hits {
		if hit.State != "open" {
			t.Errorf("default state filter let through #%d (%s)", hit.Number, hit.State)
		}
	}
	if n := utf8.RuneCountInString(hits[0].Body); n != 500 {
		t.Errorf("body runes = %d, want 500", n)
	}
	if hits[0].URL != "u2" || !reflect.DeepEqual(hits[0].Labels, []string{"bug"}) {
		t.Errorf("hit = %+v", hits[0])
	}

	text, _ = callUserTool(t, h, "search_issues", map[string]any{"query": "save button", "state": "closed"})
	if err := json.Unmarshal([]byte(text), &hits); err != nil || len(hits) != 1 || hits[0].Number != 3 {
		t.Errorf("state closed hits = %s", text)
	}
	text, _ = callUserTool(t, h, "search_issues", map[string]any{"state": "all"})
	if err := json.Unmarshal([]byte(text), &hits); err != nil || len(hits) != 3 {
		t.Errorf("state all hits = %s", text)
	}
	if _, isErr := callUserTool(t, h, "search_issues", map[string]any{"state": "merged"}); !isErr {
		t.Error("an unknown state should be a tool error")
	}
}

func TestUserMCPServiceErrorIsShortToolError(t *testing.T) {
	fake := &recordingIssues{listErr: errors.New("GET https://api.github.com/x?token=ghs_secret: 502")}
	text, isErr := callUserTool(t, newHandler(fake), "search_issues", map[string]any{})
	if !isErr || strings.Contains(text, "ghs_secret") || strings.Contains(text, "https://") {
		t.Fatalf("got %q isError=%v, want a short tool error with no internal detail", text, isErr)
	}
}

func TestUserMCPWithoutScopeIs401(t *testing.T) {
	fake := &recordingIssues{}
	w := scopedRPC(t, newHandler(fake), false, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "create_issue", "arguments": map[string]any{"title": "x", "body": "y", "kind": "bug"}},
	})
	if w.Code != http.StatusUnauthorized || len(fake.creates) != 0 {
		t.Fatalf("status = %d calls = %d, want 401 and no call", w.Code, len(fake.creates))
	}
}

// An empty query must not pour the whole repo into the agent's context: it
// lists the most recent issues (the service's newest-first order), at most 25.
func TestUserMCPSearchCapsResultsWithoutAQuery(t *testing.T) {
	fake := &recordingIssues{}
	for n := 40; n >= 1; n-- { // newest first, as the GitHub list answers
		fake.listed = append(fake.listed, sourcecontrol.IssueInfo{Number: n, Title: "issue", State: "open"})
	}
	text, isErr := callUserTool(t, newHandler(fake), "search_issues", map[string]any{})
	if isErr {
		t.Fatalf("search failed: %s", text)
	}
	var hits []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal([]byte(text), &hits); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 25 {
		t.Fatalf("hits = %d, want 25", len(hits))
	}
	if hits[0].Number != 40 || hits[24].Number != 16 {
		t.Errorf("hits run #%d..#%d, want the 25 most recent (#40..#16)", hits[0].Number, hits[24].Number)
	}
}
