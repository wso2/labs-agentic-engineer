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

// COMPONENT tier: the issue create/search surface behind the real handler
// chain. This surface backs the SRE/RCA alert handoff via the deployed
// aep-mcp-server (AE-HANDOFF-DESIGN.md) — it was accidentally dropped at the
// contract-first cutover and restored; these tests pin it to the contract so
// it cannot silently vanish again. The wire quirk matters: list items use
// CAPITALIZED keys (Number/Title/…), create's result lowercase — exactly what
// the MCP client parses.
//
// External test package: the harness imports api, which imports sourcecontrol.
package sourcecontrol_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
	"github.com/wso2/aep/aep-api/internal/platform/gittest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol/httpapi"
)

// fakeIssueService is a minimal sourcecontrol.IssueService: create echoes, list
// returns a fixed set for the ranker to filter. The embedded interface panics
// on any other method — these routes must never reach them.
type fakeIssueService struct {
	sourcecontrol.IssueService
	created []sourcecontrol.CreateIssueRequest
	gotOrg  string
	issues  []sourcecontrol.IssueInfo
}

func (f *fakeIssueService) CreateIssue(_ context.Context, org, _ string, req sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error) {
	f.gotOrg = org
	f.created = append(f.created, req)
	return &sourcecontrol.IssueResult{
		Number:         7,
		URL:            "https://github.com/acme/repo/issues/7",
		NodeID:         "n7",
		Classification: "code-level",
		Adopted:        true,
	}, nil
}

func TestIssueComponent_CreatePreservesSREHandoff(t *testing.T) {
	t.Parallel()
	svc := &fakeIssueService{}
	h := componenttest.New(t, componenttest.Options{Deps: edge.Deps{SourceControl: scWith(t, svc)}})

	resp := h.AsOrg("acme").Post("/api/v1/projects/web/issues", `{
  "title": "checkout-api times out calling inventory",
  "body": "## RCA summary\n\nRequest failures spike.\n\n## Root cause\n\nRetry loop is unbounded.",
  "componentName": "checkout-api",
  "actionStatuses": ["revised", "suggested", null]
}`)
	if resp.Code != 200 {
		t.Fatalf("create: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if len(svc.created) != 1 {
		t.Fatalf("create calls = %d, want 1", len(svc.created))
	}
	got := svc.created[0]
	if got.ComponentName != "checkout-api" {
		t.Fatalf("componentName = %q, want checkout-api", got.ComponentName)
	}
	if len(got.ActionStatuses) != 3 || got.ActionStatuses[0] == nil || *got.ActionStatuses[0] != "revised" || got.ActionStatuses[1] == nil || *got.ActionStatuses[1] != "suggested" || got.ActionStatuses[2] != nil {
		t.Fatalf("actionStatuses = %#v, want [revised suggested <nil>] in order", got.ActionStatuses)
	}

	var created struct {
		Classification string `json:"classification"`
		Adopted        bool   `json:"adopted"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Classification != "code-level" || !created.Adopted {
		t.Fatalf("create outcome = %+v, want server-derived classification and adopted", created)
	}
}

func TestIssueComponent_ListAllowsOnlyKnownAttentionReasons(t *testing.T) {
	t.Parallel()
	svc := &fakeIssueService{issues: []sourcecontrol.IssueInfo{
		{
			Number:          1,
			Title:           "verified fix needs review",
			Body:            "body",
			URL:             "u1",
			State:           "open",
			StateReason:     "reopened",
			Labels:          []string{"sre"},
			AttentionReason: "unverified_fix",
		},
		{
			Number:          2,
			Title:           "unknown attention",
			Body:            "body",
			URL:             "u2",
			State:           "open",
			Labels:          []string{"sre"},
			AttentionReason: "unexpected",
		},
	}}
	h := componenttest.New(t, componenttest.Options{Deps: edge.Deps{SourceControl: scWith(t, svc)}})

	resp := h.AsOrg("acme").Get("/api/v1/projects/web/issues")
	if resp.Code != 200 {
		t.Fatalf("list: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2: %s", len(items), resp.Body.String())
	}
	if string(items[0]["StateReason"]) != `"reopened"` || string(items[0]["attentionReason"]) != `"unverified_fix"` {
		t.Fatalf("known attention fields = %s, want StateReason and unverified_fix", resp.Body.String())
	}
	if _, ok := items[1]["attentionReason"]; ok {
		t.Fatalf("unknown attentionReason must be omitted: %s", resp.Body.String())
	}
}

func TestIssueComponent_AttentionFromGitHubEvidence(t *testing.T) {
	t.Parallel()
	stub := gittest.NewStub(t)
	stub.On(http.MethodGet, "/repos/acme/widgets/issues", http.StatusOK, `[
	{"number":1,"title":"review fix","state":"open","state_reason":"reopened","labels":[{"name":"incident"}]},
	{"number":2,"title":"no code change","state":"closed","state_reason":"not_planned","labels":[{"name":"incident"}]},
	{"number":3,"title":"repeated incident","state":"open","state_reason":"reopened","body":"Original\n\n## Recurrence 1\nEvidence\n\n## Recurrence 2\nEvidence\n\n## Recurrence 3\nEvidence","labels":[{"name":"incident"},{"name":"aep"}]},
  {"number":4,"title":"ordinary","state":"open","state_reason":"reopened","labels":[{"name":"bug"}]}
]`)
	h := componenttest.New(t, componenttest.Options{Deps: edge.Deps{SourceControl: scWith(t, newIssueSvcOnStub(t, stub))}})
	resp := h.AsOrg("org1").Get("/api/v1/projects/proj1/issues")
	if resp.Code != 200 {
		t.Fatalf("list status=%d body=%s", resp.Code, resp.Body.String())
	}
	var items []struct {
		Number          int
		StateReason     string
		AttentionReason string `json:"attentionReason"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("items=%s", resp.Body.String())
	}
	want := map[int]string{1: "unverified_fix", 2: "no_change_verdict", 3: "escalated", 4: ""}
	for _, item := range items {
		if item.AttentionReason != want[item.Number] || item.StateReason == "" {
			t.Fatalf("item=%+v want attention=%q", item, want[item.Number])
		}
	}
	if strings.Contains(resp.Body.String(), "ClosedAt") {
		t.Fatalf("internal closure identity leaked: %s", resp.Body.String())
	}
}

func (f *fakeIssueService) ListIssues(_ context.Context, org, _ string, _ []string) ([]sourcecontrol.IssueInfo, error) {
	f.gotOrg = org
	return f.issues, nil
}

// scWith assembles the real sourcecontrol domain around a faked port — the same
// New the composition root calls.
func scWith(t *testing.T, svc sourcecontrol.IssueService) *httpapi.Handlers {
	t.Helper()
	h, err := httpapi.New(sourcecontrol.Deps{Issues: svc})
	if err != nil {
		t.Fatalf("assemble sourcecontrol: %v", err)
	}
	return h
}

func TestIssueComponent_CreateAndList(t *testing.T) {
	t.Parallel()
	svc := &fakeIssueService{issues: []sourcecontrol.IssueInfo{
		{Number: 1, Title: "service1 timeout on checkout", Body: "…", URL: "u1", State: "open", Labels: []string{"sre"}},
		{Number: 2, Title: "docs typo", Body: "…", URL: "u2", State: "open"},
	}}
	// The harness wires the DOMAIN, not a loose service: the edge embeds
	// sourcecontrol's handlers, so this assembles the same graph production does.
	h := componenttest.New(t, componenttest.Options{Deps: edge.Deps{SourceControl: scWith(t, svc)}})

	// Create: org from the verified token, result keys lowercase.
	resp := h.AsOrg("acme").Post("/api/v1/projects/web/issues",
		`{"title":"pod oomkilled","body":"details","labels":["sre"],"dedupeKey":"sre-rca/web"}`)
	if resp.Code != 200 {
		t.Fatalf("create: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if svc.gotOrg != "acme" || len(svc.created) != 1 || svc.created[0].DedupeKey != "sre-rca/web" {
		t.Fatalf("service saw org=%q created=%+v", svc.gotOrg, svc.created)
	}
	var created map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatalf("create body: %v", err)
	}
	for _, k := range []string{"number", "url", "nodeId"} {
		if _, ok := created[k]; !ok {
			t.Fatalf("create body missing %q (lowercase keys are the MCP-consumed wire): %s", k, resp.Body.String())
		}
	}

	// List with a ranked query: CAPITALIZED item keys (the MCP-consumed wire).
	resp = h.AsOrg("acme").Get("/api/v1/projects/web/issues?q=service1%20timeout")
	if resp.Code != 200 {
		t.Fatalf("list: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(resp.Body.Bytes(), &items); err != nil {
		t.Fatalf("list body: %v", err)
	}
	if len(items) != 1 || !strings.Contains(string(items[0]["Title"]), "service1") {
		t.Fatalf("ranked list: got %s", resp.Body.String())
	}
	for _, k := range []string{"Number", "Title", "Body", "URL", "State", "Labels"} {
		if _, ok := items[0][k]; !ok {
			t.Fatalf("list item missing capitalized %q: %s", k, resp.Body.String())
		}
	}

	// Validation: missing required body fields → contract validator's 400.
	resp = h.AsOrg("acme").Post("/api/v1/projects/web/issues", `{}`)
	if resp.Code != 400 {
		t.Fatalf("empty create: want 400, got %d", resp.Code)
	}
	if e := componenttest.DecodeEnvelope(t, resp.Body.String()); e.Code != "validation_failed" {
		t.Fatalf("validation envelope: %s", resp.Body.String())
	}

	// Deny-by-default gate: claimless request is 401.
	if resp := h.NoAuth().Get("/api/v1/projects/web/issues"); resp.Code != 401 {
		t.Fatalf("claimless list: want 401, got %d", resp.Code)
	}

	// Nil service → 503 service_unavailable, not a panic.
	h2 := componenttest.New(t, componenttest.Options{Deps: edge.Deps{}})
	if resp := h2.AsOrg("acme").Get("/api/v1/projects/web/issues"); resp.Code != 503 {
		t.Fatalf("nil svc: want 503, got %d body=%s", resp.Code, resp.Body.String())
	}
}
