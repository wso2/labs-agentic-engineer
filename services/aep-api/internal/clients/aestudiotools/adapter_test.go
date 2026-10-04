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

package aestudiotools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

var (
	_ sourcecontrol.Git             = (*Adapter)(nil)
	_ sourcecontrol.TrashOps        = (*Adapter)(nil)
	_ sourcecontrol.SkillsMirrorOps = (*Adapter)(nil)
	_ sourcecontrol.ReferencesOps   = (*Adapter)(nil)
	_ sourcecontrol.IdentityOps     = (*Adapter)(nil)
	_ Turns                         = (*Adapter)(nil)
	_ sourcecontrol.RepoAdmin       = (*Adapter)(nil)
	_ sourcecontrol.IssueOps        = (*Adapter)(nil)
	_ sourcecontrol.WebhookOps      = (*Adapter)(nil)
)

// countingEndpoints resolves every org to url and counts the lookups.
type countingEndpoints struct {
	url   string
	calls int
}

func (e *countingEndpoints) Resolve(context.Context, string) (Target, error) {
	e.calls++
	return Target{BaseURL: e.url, ImpersonateOrg: "ou"}, nil
}

// count answers how many captured lines are the event name.
func (l *logLines) count(name string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(l.buf.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["msg"] == name {
			n++
		}
	}
	return n
}

// onlyKeys reports whether every line of the event carries exactly keys.
func (l *logLines) onlyKeys(name string, keys ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range strings.Split(strings.TrimSpace(l.buf.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) != nil || m["msg"] != name {
			continue
		}
		got := fieldsOf(m)
		if strings.Join(got, ",") != strings.Join(keys, ",") {
			return false
		}
	}
	return true
}

// seen is one request the stub pod received.
type seen struct {
	Method, Path, Query string
	ImpersonateOrg      string
	Body                string
}

// podStub records every request and answers with reply.
type podStub struct {
	mu    sync.Mutex
	reqs  []seen
	reply func(w http.ResponseWriter, r *http.Request)
}

func newPodStub(t *testing.T, reply func(w http.ResponseWriter, r *http.Request)) (*podStub, *httptest.Server) {
	t.Helper()
	p := &podStub{reply: reply}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		p.mu.Lock()
		p.reqs = append(p.reqs, seen{Method: r.Method, Path: r.URL.EscapedPath(), Query: r.URL.Query().Encode(), ImpersonateOrg: r.Header.Get("X-Impersonate-Org"), Body: string(body)})
		p.mu.Unlock()
		p.reply(w, r)
	}))
	t.Cleanup(srv.Close)
	return p, srv
}

func (p *podStub) requests() []seen {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]seen(nil), p.reqs...)
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func TestAdapter_ImpersonatesOUIDAndRefreshesOnce(t *testing.T) {
	var seen []string
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("X-Impersonate-Org"))
		n++
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"sha":"` + strings.Repeat("a", 40) + `"}`))
	}))
	defer srv.Close()
	tok := &countingTokens{}
	a := New(Config{Endpoints: fixedTarget(srv.URL, "ou-123"), Tokens: tok, HTTP: srv.Client()})
	if _, err := a.Head(context.Background(), sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "g"}, ""); err != nil {
		t.Fatal(err)
	}
	if tok.invalidated != 1 || n != 2 || seen[1] != "ou-123" {
		t.Fatalf("invalidated=%d calls=%d header=%v", tok.invalidated, n, seen)
	}
}

func TestEndpointCache_DropsOnTransportError(t *testing.T) {
	ep := &countingEndpoints{url: "http://127.0.0.1:1"} // nothing listens
	a := New(Config{Endpoints: ep, Tokens: &countingTokens{}, HTTP: &http.Client{Timeout: time.Second}})
	ref := sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "g"}
	_, err := a.Head(context.Background(), ref, "")
	if !errors.Is(err, sourcecontrol.ErrAEStudioUnavailable) {
		t.Fatalf("err = %v", err)
	}
	_, _ = a.Head(context.Background(), ref, "")
	if ep.calls != 2 {
		t.Fatalf("resolve calls = %d, want 2 (cache dropped)", ep.calls)
	}
}

func TestAdapter_AuthFailureAfterRefreshIsMisconfigured(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		logs := captureSlog(t)
		a := New(Config{Endpoints: fixedTarget(srv.URL, "ou-123"), Tokens: &countingTokens{}, HTTP: srv.Client()})
		_, err := a.Head(context.Background(), sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "g"}, "")
		srv.Close()
		if !errors.Is(err, sourcecontrol.ErrAEStudioMisconfigured) || !sourcecontrol.IsPermanent(err) {
			t.Fatalf("%d → %v, want a permanent sourcecontrol.ErrAEStudioMisconfigured (C3)", status, err)
		}
		if n := logs.count("ae_studio.auth_failed"); n != 1 || !logs.onlyKeys("ae_studio.auth_failed", "org", "status") {
			t.Fatalf("%d: want one value-free ae_studio.auth_failed {org, status}, got %d", status, n)
		}
	}
}

// K-14: a 403 refusal of the token may come from a stale cached Target (a
// rolled pod of another org at the old URL): the Target is dropped and
// resolved again before the one retry. A 401 keeps it.
func TestAdapter_RefusalDropsTheTargetBeforeTheRetry(t *testing.T) {
	for _, tc := range []struct {
		name        string
		refuse      func(w http.ResponseWriter)
		wantResolve int
	}{
		{"403 org_mismatch", func(w http.ResponseWriter) { writeProblem(w, http.StatusForbidden, "org_mismatch", "") }, 2},
		{"403 without a problem", func(w http.ResponseWriter) { w.WriteHeader(http.StatusForbidden) }, 2},
		{"401", func(w http.ResponseWriter) { w.WriteHeader(http.StatusUnauthorized) }, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := 0
			_, srv := newPodStub(t, func(w http.ResponseWriter, _ *http.Request) {
				n++
				if n == 1 {
					tc.refuse(w)
					return
				}
				writeJSON(w, 200, `{"sha":"`+strings.Repeat("a", 40)+`"}`)
			})
			ep := fixedTarget(srv.URL, "ou-123")
			a := newAdapter(t, ep, &countingTokens{})
			if _, err := a.Head(context.Background(), acmeGreeter, ""); err != nil {
				t.Fatal(err)
			}
			if ep.count() != tc.wantResolve {
				t.Fatalf("resolve calls = %d, want %d", ep.count(), tc.wantResolve)
			}
		})
	}
}

// K-15 / Q-8: the pod's owner verdict is an answer about the repository, not
// a refusal of the token: no refresh, no auth_failed line, a permanent
// ErrOwnerNotAllowed — on git ops as on references.
func TestAdapter_OwnerNotAllowedIsNotAnAuthFailure(t *testing.T) {
	calls := 0
	_, srv := newPodStub(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		writeProblem(w, http.StatusForbidden, "owner_not_allowed", "")
	})
	logs := captureSlog(t)
	tok := &countingTokens{}
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), tok)
	_, err := a.Head(context.Background(), acmeGreeter, "")
	if !errors.Is(err, sourcecontrol.ErrOwnerNotAllowed) || !sourcecontrol.IsPermanent(err) || errors.Is(err, sourcecontrol.ErrAEStudioMisconfigured) {
		t.Fatalf("err = %v, want a permanent ErrOwnerNotAllowed", err)
	}
	if calls != 1 || tok.invalidations() != 0 || logs.count("ae_studio.auth_failed") != 0 {
		t.Fatalf("calls=%d invalidated=%d auth_failed=%d, want no refresh", calls, tok.invalidations(), logs.count("ae_studio.auth_failed"))
	}
}
