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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/organization/aestudio"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// fakeTurnLedger records each batch it is handed and the org it was for.
type fakeTurnLedger struct {
	orgs    []string
	batches [][]spec.TurnRecord
	err     error
}

func (f *fakeTurnLedger) RecordFinished(_ context.Context, org string, recs []spec.TurnRecord) error {
	if f.err != nil {
		return f.err
	}
	f.orgs = append(f.orgs, org)
	f.batches = append(f.batches, recs)
	return nil
}

const turnUsagePath = "/internal/v1/ae-studio/turn-usage"

// turnRecordJSON is a valid wire record; mut edits it before encoding.
func turnRecordJSON(t *testing.T, mut func(map[string]any)) map[string]any {
	t.Helper()
	rec := map[string]any{
		"turnId":              "6f1a2c1e-6c39-4f0e-9a51-7a1d0e5a6b10",
		"project":             "greeter",
		"conversationId":      "0b7d6c55-4c43-4d0e-8b52-1f4f8b0f3c21",
		"kind":                "browser",
		"flow":                "design",
		"status":              "completed",
		"baseRef":             "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"skillsRef":           "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"startedAt":           "2026-10-03T09:00:00Z",
		"finishedAt":          "2026-10-03T09:01:30Z",
		"author":              map[string]any{"id": "ada@example.com", "name": "Ada"},
		"model":               "claude-sonnet-5",
		"modelHost":           "api.anthropic.com",
		"inputTokens":         1000,
		"outputTokens":        100,
		"cacheReadTokens":     10,
		"cacheCreationTokens": 20,
		"contextTokens":       4200,
	}
	if mut != nil {
		mut(rec)
	}
	return rec
}

func turnUsageBody(t *testing.T, recs ...map[string]any) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"records": recs})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func postTurnUsage(t *testing.T, h http.Handler, bearer, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, turnUsagePath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The record reaches the ledger field by field, under the token's org.
func TestTurnUsage_RecordsTheBatchUnderTheTokenOrg(t *testing.T) {
	stack := newInternalStack(t)
	ledger := &fakeTurnLedger{}
	deps := stack.deps
	deps.AEStudioRepositories = aeStudioProjects()
	deps.TurnLedger = ledger
	h := NewHandler(AppParams{InternalDeps: deps})

	rec := postTurnUsage(t, h, "Bearer "+stack.mintStudio("acme"), turnUsageBody(t, turnRecordJSON(t, nil)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body %s)", rec.Code, rec.Body)
	}
	if len(ledger.batches) != 1 || ledger.orgs[0] != "acme" {
		t.Fatalf("ledger calls = %v orgs %v, want one for acme", len(ledger.batches), ledger.orgs)
	}
	ctxTokens := int64(4200)
	want := spec.TurnRecord{
		TurnID:         "6f1a2c1e-6c39-4f0e-9a51-7a1d0e5a6b10",
		Project:        "greeter",
		ConversationID: "0b7d6c55-4c43-4d0e-8b52-1f4f8b0f3c21",
		Kind:           spec.TurnKindBrowser,
		Flow:           "design",
		Status:         "completed",
		BaseRef:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SkillsRef:      "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		StartedAt:      time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC),
		FinishedAt:     time.Date(2026, 10, 3, 9, 1, 30, 0, time.UTC),
		AuthorID:       "ada@example.com",
		AuthorName:     "Ada",
		ModelHost:      "api.anthropic.com",
		Usage: contracts.TokenUsage{
			InputTokens: 1000, OutputTokens: 100, CacheReadTokens: 10, CacheCreationTokens: 20,
			Model: "claude-sonnet-5",
		},
		ContextTokens: &ctxTokens,
	}
	got := ledger.batches[0]
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("record = %+v\nwant     %+v", got, want)
	}
}

// Optional fields stay empty: a failed marketplace turn with no author, no
// project and no context measure.
func TestTurnUsage_OptionalFieldsStayEmpty(t *testing.T) {
	stack := newInternalStack(t)
	ledger := &fakeTurnLedger{}
	deps := stack.deps
	deps.AEStudioRepositories = aeStudioProjects()
	deps.TurnLedger = ledger
	h := NewHandler(AppParams{InternalDeps: deps})

	body := turnUsageBody(t, turnRecordJSON(t, func(r map[string]any) {
		delete(r, "project")
		delete(r, "author")
		delete(r, "contextTokens")
		r["status"] = "failed"
		r["reason"] = "shutdown"
		r["code"] = "shutdown"
	}))
	if rec := postTurnUsage(t, h, "Bearer "+stack.mintStudio("acme"), body); rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body %s)", rec.Code, rec.Body)
	}
	got := ledger.batches[0][0]
	if got.Project != "" || got.AuthorID != "" || got.AuthorName != "" || got.ContextTokens != nil {
		t.Fatalf("record = %+v, want no project, author or context tokens", got)
	}
	if got.Status != "failed" || got.Reason != "shutdown" || got.Code != "shutdown" {
		t.Fatalf("outcome = (%q, %q, %q)", got.Status, got.Reason, got.Code)
	}
}

// Tenancy on ingest (Review Focus 4, the user's D-4 ruling): every record's
// project must be one of the token org's projects. One that is not refuses
// the WHOLE batch with 404 and nothing is written; a record without a project
// (marketplace) is valid. The tools sender drops a 400/404/413/422 batch and
// retries 401/403/409/429/5xx, so a valid batch is never a 4xx and a
// permanently bad one is never a 5xx.
func TestTurnUsage_Tenancy(t *testing.T) {
	stack := newInternalStack(t)
	studio := "Bearer " + stack.mintStudio("acme")
	evil := "Bearer " + stack.mintStudio("evil")
	withProject := func(p string) func(map[string]any) {
		return func(r map[string]any) { r["project"] = p }
	}
	marketplace := func(r map[string]any) { delete(r, "project") }
	other := func(mut func(map[string]any)) map[string]any {
		return turnRecordJSON(t, func(r map[string]any) {
			r["turnId"] = "11111111-2222-4333-8444-555555555555"
			mut(r)
		})
	}
	userJWT := "Bearer " + stack.sign(auth.PublisherClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    pubIssuer,
			Audience:  jwt.ClaimStrings{"aep-console"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		OuHandle: "acme",
	})

	cases := []struct {
		name, bearer string
		records      []map[string]any
		want         int
		wantOrg      string
		wantRecords  int
	}{
		{name: "own project", bearer: studio, records: []map[string]any{turnRecordJSON(t, nil)}, want: 202, wantOrg: "acme", wantRecords: 1},
		{name: "marketplace record", bearer: studio, records: []map[string]any{turnRecordJSON(t, marketplace)}, want: 202, wantOrg: "acme", wantRecords: 1},
		{name: "own project and marketplace", bearer: studio, records: []map[string]any{turnRecordJSON(t, nil), other(marketplace)}, want: 202, wantOrg: "acme", wantRecords: 2},
		{name: "org B token, org A project", bearer: evil, records: []map[string]any{turnRecordJSON(t, nil)}, want: 404},
		{name: "unknown project", bearer: studio, records: []map[string]any{turnRecordJSON(t, withProject("nope"))}, want: 404},
		{name: "one foreign record refuses the batch", bearer: studio, records: []map[string]any{turnRecordJSON(t, nil), other(withProject("ledger"))}, want: 404},
		{name: "org B token, its own project", bearer: evil, records: []map[string]any{turnRecordJSON(t, withProject("ledger"))}, want: 202, wantOrg: "evil", wantRecords: 1},
		{name: "no bearer", records: []map[string]any{turnRecordJSON(t, nil)}, want: 401},
		{name: "user JWT", bearer: userJWT, records: []map[string]any{turnRecordJSON(t, nil)}, want: 401},
		{name: "publisher token of the org", bearer: "Bearer " + stack.mint("acme"), records: []map[string]any{turnRecordJSON(t, nil)}, want: 401},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ledger := &fakeTurnLedger{}
			deps := stack.deps
			deps.AEStudioRepositories = aeStudioProjects()
			deps.TurnLedger = ledger
			h := NewHandler(AppParams{InternalDeps: deps})

			rec := postTurnUsage(t, h, tc.bearer, turnUsageBody(t, tc.records...))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body)
			}
			if tc.want != 202 {
				if len(ledger.batches) != 0 {
					t.Fatalf("a refused batch reached the ledger: %+v", ledger.batches)
				}
				return
			}
			if len(ledger.batches) != 1 || ledger.orgs[0] != tc.wantOrg || len(ledger.batches[0]) != tc.wantRecords {
				t.Fatalf("ledger = orgs %v batches %+v, want one batch of %d for %s", ledger.orgs, ledger.batches, tc.wantRecords, tc.wantOrg)
			}
		})
	}
}

// Statuses the sender acts on: a malformed batch is a 400 it drops (never a
// 5xx it would retry forever); a missing dependency or a store failure is a
// 5xx it retries.
func TestTurnUsage_Statuses(t *testing.T) {
	stack := newInternalStack(t)
	studio := "Bearer " + stack.mintStudio("acme")
	build := func(mut func(*InternalDeps)) http.Handler {
		deps := stack.deps
		deps.AEStudioRepositories = aeStudioProjects()
		deps.TurnLedger = &fakeTurnLedger{}
		if mut != nil {
			mut(&deps)
		}
		return NewHandler(AppParams{InternalDeps: deps})
	}
	valid := turnUsageBody(t, turnRecordJSON(t, nil))
	tooMany := make([]map[string]any, 101)
	for i := range tooMany {
		tooMany[i] = turnRecordJSON(t, nil)
	}

	cases := []struct {
		name, body string
		h          http.Handler
		want       int
	}{
		{name: "no records", body: `{"records":[]}`, want: 400},
		{name: "over the 100-record cap", body: turnUsageBody(t, tooMany...), want: 400},
		{name: "turnId not a uuid", body: turnUsageBody(t, turnRecordJSON(t, func(r map[string]any) { r["turnId"] = "x" })), want: 400},
		{name: "running is not a finished status", body: turnUsageBody(t, turnRecordJSON(t, func(r map[string]any) { r["status"] = "running" })), want: 400},
		{name: "unknown kind", body: turnUsageBody(t, turnRecordJSON(t, func(r map[string]any) { r["kind"] = "chat" })), want: 400},
		{name: "unknown field", body: turnUsageBody(t, turnRecordJSON(t, func(r map[string]any) { r["paths"] = []string{"a"} })), want: 400},
		{name: "negative token count", body: turnUsageBody(t, turnRecordJSON(t, func(r map[string]any) { r["outputTokens"] = -1 })), want: 400},
		// Postgres refuses a NUL byte in text on every attempt: a permanent
		// refusal, so a 400 the sender drops rather than a 500 it retries.
		{name: "NUL byte in a text field", body: turnUsageBody(t, turnRecordJSON(t, func(r map[string]any) { r["flow"] = "de\u0000sign" })), want: 400},
		{name: "NUL byte in the author", body: turnUsageBody(t, turnRecordJSON(t, func(r map[string]any) {
			r["author"] = map[string]any{"id": "ada@example.com", "name": "A\u0000da"}
		})), want: 400},
		{name: "no ledger configured", body: valid, h: build(func(d *InternalDeps) { d.TurnLedger = nil }), want: 503},
		{name: "no project lookup configured", body: valid, h: build(func(d *InternalDeps) { d.AEStudioRepositories = nil }), want: 503},
		{name: "project lookup fails", body: valid, h: build(func(d *InternalDeps) { d.AEStudioRepositories = &fakeProjectRepos{err: errors.New("db down")} }), want: 500},
		{name: "ledger write fails", body: valid, h: build(func(d *InternalDeps) { d.TurnLedger = &fakeTurnLedger{err: errors.New("db down")} }), want: 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := tc.h
			if h == nil {
				h = build(nil)
			}
			if rec := postTurnUsage(t, h, studio, tc.body); rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body)
			}
		})
	}
}

// A project the org owns whose stored repository URL is not a GitHub one is
// still the org's project: its usage is accepted, never a 5xx the sender
// would retry forever (the state is permanent).
func TestTurnUsage_ProjectWithAnUnparseableRepoURL(t *testing.T) {
	stack := newInternalStack(t)
	ledger := &fakeTurnLedger{}
	deps := stack.deps
	deps.AEStudioRepositories = &fakeProjectRepos{err: fmt.Errorf("project greeter: repo url: %w", aestudio.ErrRepositoryURLInvalid)}
	deps.TurnLedger = ledger
	h := NewHandler(AppParams{InternalDeps: deps})

	rec := postTurnUsage(t, h, "Bearer "+stack.mintStudio("acme"), turnUsageBody(t, turnRecordJSON(t, nil)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body %s)", rec.Code, rec.Body)
	}
	if len(ledger.batches) != 1 {
		t.Fatalf("ledger batches = %d, want 1", len(ledger.batches))
	}
}
