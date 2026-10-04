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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/igen"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol/webhook"
)

// fakeIngestor stands in for webhook.Ingestor: repos lists each org's
// repositories by full name; it records what reached it.
type fakeIngestor struct {
	repos  map[string][]string
	result webhook.IngestResult
	err    error

	mu    sync.Mutex
	calls []ingestCall
}

type ingestCall struct {
	org, delivery, event string
	body                 []byte
}

func (f *fakeIngestor) Ingest(_ context.Context, org, deliveryID, event string, body []byte) (webhook.IngestResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, ingestCall{org, deliveryID, event, bytes.Clone(body)})
	f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	var p struct {
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	_ = json.Unmarshal(body, &p)
	for _, r := range f.repos[org] {
		if r == p.Repository.FullName {
			if f.result == "" {
				return webhook.IngestDispatched, nil
			}
			return f.result, nil
		}
	}
	return "", webhook.ErrRepositoryUnknown
}

const ingestPath = "/internal/v1/ae-studio/webhook-events"

func postIngest(h http.Handler, bearer, delivery, event string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, ingestPath, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", bearer)
	}
	if delivery != "" {
		req.Header.Set("X-GitHub-Delivery", delivery)
	}
	if event != "" {
		req.Header.Set("X-GitHub-Event", event)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func ingestStack(t *testing.T, ing *fakeIngestor) (internalStack, http.Handler) {
	t.Helper()
	stack := newInternalStack(t)
	deps := stack.deps
	deps.WebhookIngestor = ing
	return stack, NewHandler(AppParams{InternalDeps: deps})
}

// ingest-webhook-event takes only the org's ae-studio client token; the org
// is the token's recorded one, and a repository of another org is 404
// repository_unknown (Review Focus 4).
func TestInternalGate_IngestWebhookEvent(t *testing.T) {
	ing := &fakeIngestor{repos: map[string][]string{"acme": {"acme-gh/greeter"}, "evil": {"evil-gh/ledger"}}}
	stack, h := ingestStack(t, ing)
	greeter := []byte(`{"action":"opened","repository":{"full_name":"acme-gh/greeter"}}`)

	cases := []struct {
		name, bearer, delivery, event string
		body                          []byte
		want                          int
		wantCode, wantResult          string
	}{
		{"own org's repository", "Bearer " + stack.mintStudio("acme"), "d-1", "issues", greeter, 202, "", "dispatched"},
		{"another org's repository", "Bearer " + stack.mintStudio("evil"), "d-2", "issues", greeter, 404, codeRepositoryUnknown, ""},
		{"publisher token of the owning org", "Bearer " + stack.mint("acme"), "d-3", "issues", greeter, 401, CodeUnauthorized, ""},
		{"client with none recorded", "Bearer " + stack.mintStudio(orgWithoutStudioClient), "d-4", "issues", greeter, 401, CodeUnauthorized, ""},
		{"no bearer", "", "d-5", "issues", greeter, 401, CodeUnauthorized, ""},
		{"no delivery id", "Bearer " + stack.mintStudio("acme"), "", "issues", greeter, 400, CodeValidationFailed, ""},
		{"no event", "Bearer " + stack.mintStudio("acme"), "d-6", "", greeter, 400, CodeValidationFailed, ""},
		{"body not an object", "Bearer " + stack.mintStudio("acme"), "d-7", "issues", []byte(`[1]`), 400, CodeValidationFailed, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postIngest(h, tc.bearer, tc.delivery, tc.event, tc.body)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body)
			}
			var got struct{ Code, Result string }
			_ = json.Unmarshal(rec.Body.Bytes(), &got)
			if got.Code != tc.wantCode || got.Result != tc.wantResult {
				t.Fatalf("body = %s, want code %q result %q", rec.Body, tc.wantCode, tc.wantResult)
			}
		})
	}
	for _, c := range ing.calls {
		if c.org != "acme" && c.org != "evil" {
			t.Errorf("ingest got org %q; the org must come from the verified token", c.org)
		}
		if c.delivery == "d-3" || c.delivery == "d-4" || c.delivery == "d-5" {
			t.Errorf("an unauthenticated request reached the ingestor: %+v", c)
		}
	}
}

// The handler hands the ingestor the exact bytes that arrived: no re-encoding
// by the validator or the decoder (Review Focus 3), and the delivery headers
// as sent. A duplicate answers 200, a held delivery 202 held.
func TestInternalRoutes_IngestWebhookEvent(t *testing.T) {
	body := []byte("{ \"zen\" : \"Keep it logically awesome.\",\n  \"repository\":{\"full_name\":\"acme-gh/greeter\"}, \"z\":1, \"a\":[ ] }")
	for _, tc := range []struct {
		result     webhook.IngestResult
		want       int
		wantResult string
	}{
		{webhook.IngestDispatched, 202, "dispatched"},
		{webhook.IngestHeld, 202, "held"},
		{webhook.IngestDuplicate, 200, "duplicate"},
	} {
		t.Run(string(tc.result), func(t *testing.T) {
			ing := &fakeIngestor{repos: map[string][]string{"acme": {"acme-gh/greeter"}}, result: tc.result}
			stack, h := ingestStack(t, ing)
			rec := postIngest(h, "Bearer "+stack.mintStudio("acme"), "72d3162e-cc78-11e3-81ab-4c9367dc0958", "ping", body)
			if rec.Code != tc.want || !strings.Contains(rec.Body.String(), `"result":"`+tc.wantResult+`"`) {
				t.Fatalf("status = %d body = %s, want %d %s", rec.Code, rec.Body, tc.want, tc.wantResult)
			}
			if len(ing.calls) != 1 {
				t.Fatalf("ingest calls = %d, want 1", len(ing.calls))
			}
			c := ing.calls[0]
			if !bytes.Equal(c.body, body) {
				t.Fatalf("ingested bytes differ from the request body:\n got %q\nwant %q", c.body, body)
			}
			if c.org != "acme" || c.delivery != "72d3162e-cc78-11e3-81ab-4c9367dc0958" || c.event != "ping" {
				t.Fatalf("ingest call = %+v", c)
			}
		})
	}
}

// The body cap is 25 MiB for this op (GitHub's maximum), not the 1 MiB
// default; an ingest failure is a 500, an unconfigured ingestor a 503.
func TestInternalRoutes_IngestWebhookEventLimitsAndFailures(t *testing.T) {
	ing := &fakeIngestor{repos: map[string][]string{"acme": {"acme-gh/greeter"}}}
	stack, h := ingestStack(t, ing)
	bearer := "Bearer " + stack.mintStudio("acme")
	padded := func(n int) []byte {
		head := `{"repository":{"full_name":"acme-gh/greeter"},"pad":"`
		return []byte(head + strings.Repeat("x", n-len(head)-2) + `"}`)
	}
	if rec := postIngest(h, bearer, "big-1", "push", padded(5<<20)); rec.Code != 202 {
		t.Fatalf("5 MiB body: status = %d, want 202 (body %s)", rec.Code, rec.Body)
	}
	if rec := postIngest(h, bearer, "big-2", "push", padded(25<<20+1)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("25 MiB + 1 body: status = %d, want 413", rec.Code)
	}
	if internalBodyCaps["ingest-webhook-event"] != 25<<20 {
		t.Fatalf("cap = %d, want 25 MiB", internalBodyCaps["ingest-webhook-event"])
	}

	deps := stack.deps
	deps.WebhookIngestor = &fakeIngestor{err: errors.New("db down")}
	if rec := postIngest(NewHandler(AppParams{InternalDeps: deps}), bearer, "f-1", "push", padded(200)); rec.Code != 500 || strings.Contains(rec.Body.String(), "db down") {
		t.Fatalf("failing ingest: status = %d body = %s, want an opaque 500", rec.Code, rec.Body)
	}
	if rec := postIngest(NewHandler(AppParams{InternalDeps: stack.deps}), bearer, "f-2", "push", padded(200)); rec.Code != 503 {
		t.Fatalf("no ingestor: status = %d, want 503", rec.Code)
	}
}

// Q-1: the org's publisher token is refused on EVERY ae-studio/ op, and the
// org's ae-studio client token clears each one's gate. Walks the gate table,
// so a new ae-studio/ op is covered without editing this test.
func TestInternalGate_AEStudioOpsRefusePublisherToken(t *testing.T) {
	doc, err := igen.GetSpec()
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	stack := newInternalStack(t)
	h := NewHandler(AppParams{InternalDeps: stack.deps}) // no backends: a cleared gate answers 503
	n := 0
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			if internalOpGates[op.OperationID].credential != aeStudioCredential {
				continue
			}
			n++
			url := "/internal/v1" + strings.ReplaceAll(path, "{projectName}", "greeter")
			for _, tc := range []struct {
				name, bearer string
				cleared      bool
			}{
				{"publisher token", "Bearer " + stack.mint("acme"), false},
				{"ae-studio client token", "Bearer " + stack.mintStudio("acme"), true},
			} {
				req := httptest.NewRequest(method, url, strings.NewReader(`{}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", tc.bearer)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if cleared := rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden; cleared != tc.cleared {
					t.Errorf("%s %s with %s: status %d, want cleared=%v (body %s)", method, op.OperationID, tc.name, rec.Code, tc.cleared, rec.Body)
				}
			}
		}
	}
	if n != 5 {
		t.Fatalf("walked %d ae-studio/ ops, want 5", n)
	}
}
