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
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// idpServer is a client_credentials token endpoint that checks the form and
// answers access-<n>.
type idpServer struct {
	mu       sync.Mutex
	requests int
	status   int
	expires  int
}

func (s *idpServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests++
	n := s.requests
	status, expires := s.status, s.expires
	s.mu.Unlock()
	if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		http.Error(w, "bad request shape", http.StatusBadRequest)
		return
	}
	_ = r.ParseForm()
	if r.PostForm.Get("grant_type") != "client_credentials" || r.PostForm.Get("client_id") != "ae-studio-internal-client" || r.PostForm.Get("client_secret") != "s3cret" {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	}
	if status != 0 {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"access_token":"access-` + string(rune('0'+n)) + `","token_type":"Bearer","expires_in":` + strconv.Itoa(expires) + `}`))
}

func (s *idpServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

func TestClientCredentials_FetchesCachesAndRefreshes(t *testing.T) {
	idp := &idpServer{expires: 3600}
	srv := httptest.NewServer(idp)
	defer srv.Close()
	ts := NewClientCredentials(srv.URL, "ae-studio-internal-client", "s3cret", nil)
	now := time.Unix(10_000, 0)
	ts.(*clientCredentials).now = func() time.Time { return now }

	for range 2 {
		tok, err := ts.Token(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if tok != "access-1" {
			t.Fatalf("token = %q, want access-1", tok)
		}
	}
	if idp.count() != 1 {
		t.Fatalf("token requests = %d, want 1 (cached)", idp.count())
	}
	ts.Invalidate()
	if tok, _ := ts.Token(context.Background()); tok != "access-2" || idp.count() != 2 {
		t.Fatalf("after Invalidate token=%q requests=%d, want a fresh token", tok, idp.count())
	}
	now = now.Add(3600*time.Second - 30*time.Second) // inside the expiry margin
	if tok, _ := ts.Token(context.Background()); tok != "access-3" {
		t.Fatalf("near expiry token = %q, want a fresh one", tok)
	}
}

func TestClientCredentials_Failures(t *testing.T) {
	t.Run("empty secret is misconfigured without a request", func(t *testing.T) {
		idp := &idpServer{}
		srv := httptest.NewServer(idp)
		defer srv.Close()
		_, err := NewClientCredentials(srv.URL, "ae-studio-internal-client", "", nil).Token(context.Background())
		if !errors.Is(err, ErrAEStudioMisconfigured) || !errors.Is(err, errClientCredentialsMissing) {
			t.Fatalf("err = %v, want ErrAEStudioMisconfigured (client credentials missing)", err)
		}
		if idp.count() != 0 {
			t.Fatal("no token request without a secret")
		}
	})
	t.Run("empty client id or token URL is misconfigured", func(t *testing.T) {
		for _, ts := range []TokenSource{
			NewClientCredentials("http://idp", "", "s3cret", nil),
			NewClientCredentials("", "ae-studio-internal-client", "s3cret", nil),
		} {
			if _, err := ts.Token(context.Background()); !errors.Is(err, errClientCredentialsMissing) {
				t.Fatalf("err = %v, want errClientCredentialsMissing", err)
			}
		}
	})
	t.Run("a refused client is misconfigured, the reply is not echoed", func(t *testing.T) {
		srv := httptest.NewServer(&idpServer{})
		defer srv.Close()
		_, err := NewClientCredentials(srv.URL, "ae-studio-internal-client", "wrong", nil).Token(context.Background())
		if !errors.Is(err, ErrAEStudioMisconfigured) || errors.Is(err, errClientCredentialsMissing) {
			t.Fatalf("err = %v, want ErrAEStudioMisconfigured (token refused)", err)
		}
		if strings.Contains(err.Error(), "invalid_client") || strings.Contains(err.Error(), "wrong") {
			t.Fatalf("err %q echoes the IdP reply or the secret", err)
		}
	})
	t.Run("an IdP 5xx is unavailable", func(t *testing.T) {
		srv := httptest.NewServer(&idpServer{status: http.StatusBadGateway})
		defer srv.Close()
		_, err := NewClientCredentials(srv.URL, "ae-studio-internal-client", "s3cret", nil).Token(context.Background())
		if !errors.Is(err, ErrAEStudioUnavailable) {
			t.Fatalf("err = %v, want ErrAEStudioUnavailable", err)
		}
	})
	t.Run("an unreachable IdP is unavailable", func(t *testing.T) {
		_, err := NewClientCredentials("http://127.0.0.1:1", "ae-studio-internal-client", "s3cret", nil).Token(context.Background())
		if !errors.Is(err, ErrAEStudioUnavailable) {
			t.Fatalf("err = %v, want ErrAEStudioUnavailable", err)
		}
	})
}

func TestAdapter_RefusedClientIsMisconfiguredAndLogged(t *testing.T) {
	srv := identityServer(t, func(http.ResponseWriter, *http.Request, int) { t.Error("no call may leave without a token") })
	idp := httptest.NewServer(&idpServer{})
	defer idp.Close()
	logs := captureSlog(t)
	a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), NewClientCredentials(idp.URL, "ae-studio-internal-client", "wrong", nil))
	_, err := a.GitHubIdentity(context.Background(), "default")
	if !errors.Is(err, ErrAEStudioMisconfigured) || !IsPermanent(err) {
		t.Fatalf("err = %v, want a permanent ErrAEStudioMisconfigured", err)
	}
	lines := logs.events(t, "ae_studio.misconfigured")
	if len(lines) != 1 || lines[0]["reason"] != "token_refused" || lines[0]["org"] != "default" {
		t.Fatalf("ae_studio.misconfigured = %v, want one {org, reason: token_refused}", lines)
	}
}
