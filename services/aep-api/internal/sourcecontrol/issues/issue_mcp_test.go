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
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol/issues"
)

// issueScope is issue #7's token: the project plus the one issue.
var issueScope = auth.IssuesMCPScope{OrgID: "acme", ProjectID: "acme-expenses", IssueNumber: 7}

// issueRecorder is an IssueService recording the issue tools' calls as
// "<op> <org>/<project>#<n> <arg>". Any other method panics (nil embedded
// interface).
type issueRecorder struct {
	sourcecontrol.IssueService
	issue    *sourcecontrol.IssueInfo
	comments []sourcecontrol.IssueComment
	err      error
	calls    []string
	boundOrg string
}

func (f *issueRecorder) record(ctx context.Context, op, org, project string, n int, arg string) error {
	f.calls = append(f.calls, fmt.Sprintf("%s %s/%s#%d %s", op, org, project, n, arg))
	f.boundOrg = tenant.BoundOrgFromContext(ctx)
	return f.err
}

func (f *issueRecorder) GetIssue(ctx context.Context, org, project string, n int) (*sourcecontrol.IssueInfo, error) {
	if err := f.record(ctx, "get", org, project, n, ""); err != nil {
		return nil, err
	}
	return f.issue, nil
}

func (f *issueRecorder) ListIssueComments(ctx context.Context, org, project string, n, limit int) ([]sourcecontrol.IssueComment, error) {
	if err := f.record(ctx, "comments", org, project, n, fmt.Sprint(limit)); err != nil {
		return nil, err
	}
	return f.comments, nil
}

func (f *issueRecorder) CommentIssue(ctx context.Context, org, project string, n int, body string) error {
	return f.record(ctx, "comment", org, project, n, body)
}

func (f *issueRecorder) EditIssueTitle(ctx context.Context, org, project string, n int, title string) error {
	return f.record(ctx, "title", org, project, n, title)
}

func (f *issueRecorder) EditIssueBody(ctx context.Context, org, project string, n int, body string) error {
	return f.record(ctx, "body", org, project, n, body)
}

func (f *issueRecorder) CloseIssue(ctx context.Context, org, project string, n int, comment string) error {
	return f.record(ctx, "close", org, project, n, comment)
}

func (f *issueRecorder) ReopenIssue(ctx context.Context, org, project string, n int) error {
	return f.record(ctx, "reopen", org, project, n, "")
}

// fakeAgentPorts fakes the issue agent's non-issue ports, recording into the
// same call log.
type fakeAgentPorts struct {
	log        *[]string
	components []string
	promoteErr error
	removeErr  error
}

func (p fakeAgentPorts) ListComponents(context.Context, string, string) ([]string, error) {
	return p.components, nil
}

func (p fakeAgentPorts) PromoteAndExecute(_ context.Context, org, project, component string, n int) error {
	*p.log = append(*p.log, fmt.Sprintf("promote %s/%s#%d %s", org, project, n, component))
	return p.promoteErr
}

func (p fakeAgentPorts) RemoveIssueThread(_ context.Context, org, project string, n int) error {
	*p.log = append(*p.log, fmt.Sprintf("remove-thread %s/%s#%d ", org, project, n))
	return p.removeErr
}

// issueRig is the handler over an issueRecorder and fake ports sharing one log.
type issueRig struct {
	issues *issueRecorder
	ports  *fakeAgentPorts
}

func newIssueRig() *issueRig {
	rec := &issueRecorder{}
	return &issueRig{issues: rec, ports: &fakeAgentPorts{log: &rec.calls, components: []string{"api", "web"}}}
}

func (r *issueRig) call(t *testing.T, scope auth.IssuesMCPScope, name string, args map[string]any) (string, bool) {
	t.Helper()
	h := issues.NewUserMCPHandler(r.issues, issues.IssueAgentPorts{
		Promoter: r.ports, Components: r.ports, Threads: r.ports,
	})
	return callToolAs(t, h, scope, name, args)
}

var issueToolNames = []string{
	"get_issue", "list_components", "comment_issue", "edit_issue",
	"close_issue", "reopen_issue", "hand_to_coding_agent",
}

// An issue's token lists exactly the issue tools; none declares a scope
// argument (the session fixes project, org and number) or labels.
func TestIssueMCPListsExactlyTheIssueTools(t *testing.T) {
	names, props := listedTools(t, newHandler(&issueRecorder{}), issueScope)
	if !reflect.DeepEqual(names, issueToolNames) {
		t.Fatalf("tools = %v, want %v", names, issueToolNames)
	}
	for name, p := range props {
		for _, banned := range []string{"project", "org", "namespace", "number", "issueNumber", "labels"} {
			if _, ok := p[banned]; ok {
				t.Errorf("%s declares %q: the scope is fixed by the session", name, banned)
			}
		}
	}
}

// Each token calls only its own set: an issue tool on the Issues view's token,
// and search/create on an issue's token, are tool errors that touch nothing.
func TestIssueMCPToolsOutsideTheScopeAreRefused(t *testing.T) {
	r := newIssueRig()
	for _, name := range issueToolNames {
		if _, isErr := r.call(t, issuesScope, name, map[string]any{"body": "x", "reason": "x", "component": "api"}); !isErr {
			t.Errorf("%s on the Issues view's token: want a tool error", name)
		}
	}
	for _, name := range []string{"search_issues", "create_issue"} {
		if _, isErr := r.call(t, issueScope, name, map[string]any{"title": "x", "body": "y", "kind": "bug"}); !isErr {
			t.Errorf("%s on an issue's token: want a tool error", name)
		}
	}
	if len(r.issues.calls) != 0 {
		t.Errorf("calls = %v, want none", r.issues.calls)
	}
}

// get_issue reads the token's issue — never one the arguments name — with its
// newest 10 comments.
func TestIssueMCPGetIssueReadsTheClaimedIssue(t *testing.T) {
	r := newIssueRig()
	at := time.Date(2026, 10, 8, 9, 30, 0, 0, time.UTC)
	r.issues.issue = &sourcecontrol.IssueInfo{
		Number: 7, Title: "Save does nothing", Body: "Steps…", State: "open",
		Labels: []string{"bug", "src/user"}, URL: "https://github.com/acme/acme-expenses/issues/7",
	}
	r.issues.comments = []sourcecontrol.IssueComment{
		{ID: "c1", Author: "jane", Body: "me too", CreatedAt: at, URL: "u"},
		{ID: "c2", Author: "aep-bot", Body: "status", CreatedAt: at.Add(time.Hour), Machine: true},
	}
	text, isErr := r.call(t, issueScope, "get_issue", map[string]any{"number": 99, "project": "other"})
	if isErr {
		t.Fatalf("get_issue failed: %s", text)
	}
	if want := []string{"get acme/acme-expenses#7 ", "comments acme/acme-expenses#7 10"}; !reflect.DeepEqual(r.issues.calls, want) {
		t.Fatalf("calls = %v, want %v", r.issues.calls, want)
	}
	if r.issues.boundOrg != "acme" {
		t.Errorf("bound org = %q, want acme", r.issues.boundOrg)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"number": float64(7), "title": "Save does nothing", "body": "Steps…", "state": "open",
		"labels": []any{"bug", "src/user"}, "url": "https://github.com/acme/acme-expenses/issues/7",
		"comments": []any{
			map[string]any{"author": "jane", "body": "me too", "createdAt": "2026-10-08T09:30:00Z", "machine": false},
			map[string]any{"author": "aep-bot", "body": "status", "createdAt": "2026-10-08T10:30:00Z", "machine": true},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("get_issue = %s", text)
	}
}

func TestIssueMCPGetIssueNotFoundIsAToolError(t *testing.T) {
	r := newIssueRig()
	r.issues.err = fmt.Errorf("get: %w", sourcecontrol.ErrIssueNotFound)
	text, isErr := r.call(t, issueScope, "get_issue", nil)
	if !isErr || !strings.Contains(text, "not found") {
		t.Fatalf("got %q isError=%v, want a not-found tool error", text, isErr)
	}
}

func TestIssueMCPListComponents(t *testing.T) {
	r := newIssueRig()
	text, isErr := r.call(t, issueScope, "list_components", nil)
	if isErr || text != `{"components":["api","web"]}` {
		t.Fatalf("list_components = %s (isError=%v)", text, isErr)
	}
	r.ports.components = nil
	if text, _ := r.call(t, issueScope, "list_components", nil); text != `{"components":[]}` {
		t.Errorf("list_components with no design = %s, want an empty list", text)
	}
}

// Every write acts on the claimed issue whatever number the arguments carry.
func TestIssueMCPWritesActOnTheClaimedIssueOnly(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		want []string
	}{
		{"comment_issue", map[string]any{"body": "Thanks!"}, []string{"comment acme/acme-expenses#7 Thanks!"}},
		{"edit_issue", map[string]any{"title": "New title"}, []string{"title acme/acme-expenses#7 New title"}},
		{"edit_issue", map[string]any{"body": "New body"}, []string{"body acme/acme-expenses#7 New body"}},
		{"edit_issue", map[string]any{"title": "T", "body": "B"}, []string{"title acme/acme-expenses#7 T", "body acme/acme-expenses#7 B"}},
		{"close_issue", map[string]any{"reason": "Fixed in v2."}, []string{"close acme/acme-expenses#7 Fixed in v2.", "remove-thread acme/acme-expenses#7 "}},
		{"reopen_issue", nil, []string{"reopen acme/acme-expenses#7 "}},
		{"hand_to_coding_agent", map[string]any{"component": "api"}, []string{"promote acme/acme-expenses#7 api"}},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			r := newIssueRig()
			args := map[string]any{"number": 99, "issueNumber": 99, "project": "other", "org": "evil"}
			for k, v := range tc.args {
				args[k] = v
			}
			if text, isErr := r.call(t, issueScope, tc.tool, args); isErr {
				t.Fatalf("%s failed: %s", tc.tool, text)
			}
			if !reflect.DeepEqual(r.issues.calls, tc.want) {
				t.Fatalf("calls = %v, want %v", r.issues.calls, tc.want)
			}
			if r.issues.boundOrg != "" && r.issues.boundOrg != "acme" {
				t.Errorf("bound org = %q, want acme", r.issues.boundOrg)
			}
		})
	}
}

func TestIssueMCPWritesRefuseMissingArguments(t *testing.T) {
	cases := map[string]map[string]any{
		"comment_issue":        {"body": "  "},
		"edit_issue":           {"title": " ", "body": ""},
		"close_issue":          {},
		"hand_to_coding_agent": {},
	}
	for tool, args := range cases {
		r := newIssueRig()
		if _, isErr := r.call(t, issueScope, tool, args); !isErr || len(r.issues.calls) != 0 {
			t.Errorf("%s %v: isError=%v calls=%v, want a tool error and no call", tool, args, isErr, r.issues.calls)
		}
	}
}

// A component the design does not have is refused, and the error names the
// ones it has.
func TestIssueMCPHandOffRefusesAnUnknownComponent(t *testing.T) {
	r := newIssueRig()
	text, isErr := r.call(t, issueScope, "hand_to_coding_agent", map[string]any{"component": "billing"})
	if !isErr || !strings.Contains(text, "api") || !strings.Contains(text, "web") {
		t.Fatalf("got %q isError=%v, want a tool error listing api and web", text, isErr)
	}
	if len(r.issues.calls) != 0 {
		t.Errorf("calls = %v, want no promote", r.issues.calls)
	}
}

// No deployed version: the tool error is the sentence the user needs.
func TestIssueMCPHandOffWithNoDeployedVersion(t *testing.T) {
	r := newIssueRig()
	r.ports.promoteErr = fmt.Errorf("promote: %w", issues.ErrNoDeployedVersion)
	text, isErr := r.call(t, issueScope, "hand_to_coding_agent", map[string]any{"component": "api"})
	want := "Deploy a version first: the coding agent works in a deployed version's milestone."
	if !isErr || text != want {
		t.Fatalf("got %q isError=%v, want the tool error %q", text, isErr, want)
	}
}

// The issue is closed even when its thread cannot be removed; the failure is
// logged, not the agent's problem.
func TestIssueMCPCloseSurvivesAThreadRemovalFailure(t *testing.T) {
	r := newIssueRig()
	r.ports.removeErr = errors.New("agents service down")
	if text, isErr := r.call(t, issueScope, "close_issue", map[string]any{"reason": "Duplicate of #3."}); isErr {
		t.Fatalf("close_issue failed: %s", text)
	}
}

// A failed close removes nothing.
func TestIssueMCPFailedCloseKeepsTheThread(t *testing.T) {
	r := newIssueRig()
	r.issues.err = errors.New("GET https://api.github.com/x: 502")
	text, isErr := r.call(t, issueScope, "close_issue", map[string]any{"reason": "Done."})
	if !isErr || strings.Contains(text, "https://") {
		t.Fatalf("got %q isError=%v, want a short tool error", text, isErr)
	}
	for _, c := range r.issues.calls {
		if strings.HasPrefix(c, "remove-thread") {
			t.Fatalf("calls = %v, want no thread removal", r.issues.calls)
		}
	}
}

// With no remover wired, close_issue still closes.
func TestIssueMCPCloseWithoutARemover(t *testing.T) {
	rec := &issueRecorder{}
	h := issues.NewUserMCPHandler(rec, issues.IssueAgentPorts{})
	if text, isErr := callToolAs(t, h, issueScope, "close_issue", map[string]any{"reason": "Done."}); isErr {
		t.Fatalf("close_issue failed: %s", text)
	}
	if want := []string{"close acme/acme-expenses#7 Done."}; !reflect.DeepEqual(rec.calls, want) {
		t.Fatalf("calls = %v, want %v", rec.calls, want)
	}
}
