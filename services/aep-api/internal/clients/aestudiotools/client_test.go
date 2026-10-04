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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// fixedEndpoints answers one Target for every org and counts the lookups.
type fixedEndpoints struct {
	mu     sync.Mutex
	target Target
	err    error
	calls  int
}

func fixedTarget(baseURL, ou string) *fixedEndpoints {
	return &fixedEndpoints{target: Target{BaseURL: baseURL, ImpersonateOrg: ou}}
}

func (e *fixedEndpoints) Resolve(context.Context, string) (Target, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	return e.target, e.err
}

func (e *fixedEndpoints) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

// countingTokens hands out tok-1, tok-2, … : a new token after each Invalidate.
type countingTokens struct {
	mu          sync.Mutex
	issued      int
	invalidated int
	err         error
}

func (c *countingTokens) Token(context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return "", c.err
	}
	if c.issued == c.invalidated {
		c.issued++
	}
	return "tok-" + string(rune('0'+c.issued)), nil
}

func (c *countingTokens) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.invalidated = c.issued
}

func (c *countingTokens) invalidations() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.invalidated
}

// logLines captures the default slog logger as JSON lines for one test. Tests
// that use it do not run in parallel.
type logLines struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logLines) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func captureSlog(t *testing.T) *logLines {
	t.Helper()
	l := &logLines{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(l, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return l
}

// events returns every captured line whose msg is name.
func (l *logLines) events(t *testing.T, name string) []map[string]any {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(l.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		if m["msg"] == name {
			out = append(out, m)
		}
	}
	return out
}

// fieldsOf lists a log line's own fields (without time, level and msg).
func fieldsOf(m map[string]any) []string {
	var keys []string
	for k := range m {
		if k != "time" && k != "level" && k != "msg" {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}

func writeProblem(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code, "detail": detail})
}

func newAdapter(t *testing.T, ep Endpoints, tokens TokenSource) *Adapter {
	t.Helper()
	return New(Config{Endpoints: ep, Tokens: tokens, HTTP: &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}})
}

var acmeGreeter = RepoRef{Org: "default", Owner: "acme", Repo: "greeter"}

func identityServer(t *testing.T, handle func(w http.ResponseWriter, r *http.Request, n int)) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		k := n
		mu.Unlock()
		handle(w, r, k)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAdapter_ImpersonatesOUID(t *testing.T) {
	var gotOrg, gotAuth string
	srv := identityServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		gotOrg, gotAuth = r.Header.Get("X-Impersonate-Org"), r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"login":"acme-bot","id":7}`))
	})
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
	if _, err := a.GitHubIdentity(context.Background(), "default"); err != nil {
		t.Fatal(err)
	}
	if gotOrg != "ou-123" {
		t.Fatalf("X-Impersonate-Org = %q, want the OU id ou-123 (R13), never the org name", gotOrg)
	}
	if gotAuth != "Bearer tok-1" {
		t.Fatalf("Authorization = %q, want the AE-only token", gotAuth)
	}
}

func TestAdapter_RefreshesTokenOnceOn401(t *testing.T) {
	var auths []string
	srv := identityServer(t, func(w http.ResponseWriter, r *http.Request, n int) {
		auths = append(auths, r.Header.Get("Authorization"))
		if n == 1 {
			writeProblem(w, http.StatusUnauthorized, "unauthorized", "")
			return
		}
		_, _ = w.Write([]byte(`{"login":"acme-bot","id":7}`))
	})
	tok := &countingTokens{}
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), tok)
	id, err := a.GitHubIdentity(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if id.Login != "acme-bot" || id.ID != 7 {
		t.Fatalf("identity = %+v", id)
	}
	if tok.invalidations() != 1 || !slices.Equal(auths, []string{"Bearer tok-1", "Bearer tok-2"}) {
		t.Fatalf("invalidated=%d auths=%v, want one refresh and a retry with the new token", tok.invalidations(), auths)
	}
}

func TestAdapter_AuthFailureIsMisconfiguredAndLogged(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			srv := identityServer(t, func(w http.ResponseWriter, _ *http.Request, n int) {
				calls = n
				writeProblem(w, status, "org_mismatch", "")
			})
			logs := captureSlog(t)
			tok := &countingTokens{}
			a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), tok)
			_, err := a.GitHubIdentity(context.Background(), "default")
			if !errors.Is(err, sourcecontrol.ErrAEStudioMisconfigured) || !sourcecontrol.IsPermanent(err) {
				t.Fatalf("err = %v, want a permanent sourcecontrol.ErrAEStudioMisconfigured (C3)", err)
			}
			if calls != 2 || tok.invalidations() != 2 {
				t.Fatalf("calls=%d invalidated=%d, want one retry with a fresh token and both refused tokens dropped", calls, tok.invalidations())
			}
			lines := logs.events(t, "ae_studio.auth_failed")
			if len(lines) != 1 {
				t.Fatalf("ae_studio.auth_failed lines = %d, want 1", len(lines))
			}
			if got := fieldsOf(lines[0]); !slices.Equal(got, []string{"org", "status"}) {
				t.Fatalf("ae_studio.auth_failed fields = %v, want [org status] only", got)
			}
			if lines[0]["org"] != "default" || lines[0]["status"] != float64(status) || lines[0]["level"] != "ERROR" {
				t.Fatalf("ae_studio.auth_failed = %v", lines[0])
			}
		})
	}
	t.Run("503 stays unavailable", func(t *testing.T) {
		srv := identityServer(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
			writeProblem(w, http.StatusServiceUnavailable, "idp_unavailable", "")
		})
		logs := captureSlog(t)
		a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
		_, err := a.GitHubIdentity(context.Background(), "default")
		if !errors.Is(err, sourcecontrol.ErrAEStudioUnavailable) || errors.Is(err, sourcecontrol.ErrAEStudioMisconfigured) || sourcecontrol.IsPermanent(err) {
			t.Fatalf("err = %v, want a retryable sourcecontrol.ErrAEStudioUnavailable", err)
		}
		if n := len(logs.events(t, "ae_studio.auth_failed")); n != 0 {
			t.Fatalf("ae_studio.auth_failed lines = %d on a 503", n)
		}
	})
}

func TestAdapter_MissingClientCredentialsFailLoudOnce(t *testing.T) {
	reached := false
	srv := identityServer(t, func(w http.ResponseWriter, _ *http.Request, _ int) { reached = true })
	idp := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	defer idp.Close()
	logs := captureSlog(t)
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), NewClientCredentials(idp.URL, "ae-studio-internal-client", "", nil))
	for range 2 {
		_, err := a.GitHubIdentity(context.Background(), "default")
		if !errors.Is(err, sourcecontrol.ErrAEStudioMisconfigured) || !sourcecontrol.IsPermanent(err) {
			t.Fatalf("err = %v, want a permanent sourcecontrol.ErrAEStudioMisconfigured (C5)", err)
		}
	}
	if reached {
		t.Fatal("no request may leave without client credentials")
	}
	lines := logs.events(t, "ae_studio.misconfigured")
	if len(lines) != 1 {
		t.Fatalf("ae_studio.misconfigured lines = %d, want exactly 1", len(lines))
	}
	if got := fieldsOf(lines[0]); !slices.Equal(got, []string{"org", "reason"}) {
		t.Fatalf("ae_studio.misconfigured fields = %v, want [org reason]", got)
	}
	if lines[0]["org"] != "default" || lines[0]["reason"] != "client_credentials_missing" || lines[0]["level"] != "ERROR" {
		t.Fatalf("ae_studio.misconfigured = %v", lines[0])
	}
	if n := len(logs.events(t, "ae_studio.auth_failed")); n != 0 {
		t.Fatalf("missing credentials must not log ae_studio.auth_failed, got %d", n)
	}
}

func TestEndpointCache_ReusesTargetForAbout30s(t *testing.T) {
	srv := identityServer(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		_, _ = w.Write([]byte(`{"login":"acme-bot","id":7}`))
	})
	ep := fixedTarget(srv.URL, "ou-123")
	a := newAdapter(t, ep, &countingTokens{})
	now := time.Unix(1_000, 0)
	a.endpoints.now = func() time.Time { return now }
	for range 3 {
		if _, err := a.GitHubIdentity(context.Background(), "default"); err != nil {
			t.Fatal(err)
		}
	}
	if ep.count() != 1 {
		t.Fatalf("resolve calls = %d, want 1 (cached)", ep.count())
	}
	now = now.Add(31 * time.Second)
	if _, err := a.GitHubIdentity(context.Background(), "default"); err != nil {
		t.Fatal(err)
	}
	if ep.count() != 2 {
		t.Fatalf("resolve calls = %d, want 2 after the TTL", ep.count())
	}
}

func TestEndpointCache_DropsOnDialError(t *testing.T) {
	ep := fixedTarget("http://127.0.0.1:1", "ou-123") // nothing listens
	a := newAdapter(t, ep, &countingTokens{})
	_, err := a.GitHubIdentity(context.Background(), "default")
	if !errors.Is(err, sourcecontrol.ErrAEStudioUnavailable) {
		t.Fatalf("err = %v, want sourcecontrol.ErrAEStudioUnavailable", err)
	}
	_, _ = a.GitHubIdentity(context.Background(), "default")
	if ep.count() != 2 {
		t.Fatalf("resolve calls = %d, want 2 (cache dropped after the dial error)", ep.count())
	}
}

func TestEndpointCache_DoesNotCacheRefusals(t *testing.T) {
	ep := &fixedEndpoints{err: sourcecontrol.ErrAEStudioAbsent}
	a := newAdapter(t, ep, &countingTokens{})
	for range 2 {
		if _, err := a.GitHubIdentity(context.Background(), "default"); !errors.Is(err, sourcecontrol.ErrAEStudioAbsent) {
			t.Fatalf("err = %v, want sourcecontrol.ErrAEStudioAbsent", err)
		}
	}
	if ep.count() != 2 {
		t.Fatalf("resolve calls = %d, want 2 (an absent studio is not cached)", ep.count())
	}
}

// A unary call cut by the Adapter's own call timeout is a pod that did not
// answer: ErrAEStudioUnavailable, and the Target is dropped. The caller's
// shorter deadline is the caller's: the Target stays.
func TestAdapter_CallTimeoutIsUnavailableTheCallersDeadlineIsNot(t *testing.T) {
	srv := identityServer(t, func(_ http.ResponseWriter, r *http.Request, _ int) { <-r.Context().Done() })
	ep := fixedTarget(srv.URL, "ou-123")
	a := New(Config{Endpoints: ep, Tokens: &countingTokens{}, CallTimeout: 200 * time.Millisecond})

	if _, err := a.GitHubIdentity(context.Background(), "default"); !errors.Is(err, sourcecontrol.ErrAEStudioUnavailable) {
		t.Fatalf("err = %v, want sourcecontrol.ErrAEStudioUnavailable at the call timeout", err)
	}
	if ep.count() != 1 {
		t.Fatalf("resolve calls = %d, want 1", ep.count())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := a.GitHubIdentity(ctx, "default")
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, sourcecontrol.ErrAEStudioUnavailable) {
		t.Fatalf("err = %v, want the caller's deadline", err)
	}
	_, _ = a.GitHubIdentity(ctx, "default")
	if ep.count() != 2 {
		t.Fatalf("resolve calls = %d, want 2 (dropped once, by the call timeout only)", ep.count())
	}
}
