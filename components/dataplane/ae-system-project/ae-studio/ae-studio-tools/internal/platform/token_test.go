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

package platform

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// tokenServer is an httptest IdP token endpoint. It checks the client
// authentication shape (Basic header, grant in the body, no secret in the
// body) and hands out tok-1, tok-2, ... with the given expires_in.
type tokenServer struct {
	t         *testing.T
	expiresIn int
	mints     atomic.Int32
	status    int
}

func (s *tokenServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.t.Helper()
	if r.Method != http.MethodPost {
		s.t.Errorf("method = %s", r.Method)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("aep-publisher-acme:s3cr3t"))
	if got := r.Header.Get("Authorization"); got != want {
		s.t.Errorf("Authorization is not the client's Basic credentials")
	}
	if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
		s.t.Errorf("Content-Type = %q", ct)
	}
	body, _ := io.ReadAll(r.Body)
	if string(body) != "grant_type=client_credentials" {
		s.t.Errorf("body = %q, want only the grant", body)
	}
	if strings.Contains(string(body), "s3cr3t") {
		s.t.Errorf("secret in the form body")
	}
	if s.status != 0 {
		w.WriteHeader(s.status)
		return
	}
	n := s.mints.Add(1)
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"access_token":"tok-%d","token_type":"Bearer","expires_in":%d}`, n, s.expiresIn)
}

func newCC(url string) *ClientCredentials {
	return &ClientCredentials{TokenURL: url, ClientID: "aep-publisher-acme", ClientSecret: "s3cr3t"}
}

func TestClientCredentials_MintsWithBasicAuthAndCaches(t *testing.T) {
	ts := &tokenServer{t: t, expiresIn: 3600}
	srv := httptest.NewServer(ts)
	defer srv.Close()
	cc := newCC(srv.URL)

	for i := 0; i < 3; i++ {
		tok, err := cc.Token(context.Background())
		if err != nil || tok != "tok-1" {
			t.Fatalf("call %d: tok=%q err=%v", i, tok, err)
		}
	}
	if ts.mints.Load() != 1 {
		t.Fatalf("mints = %d, want 1 (cached)", ts.mints.Load())
	}
}

func TestClientCredentials_RefreshesSixtySecondsBeforeExpiry(t *testing.T) {
	ts := &tokenServer{t: t, expiresIn: 120}
	srv := httptest.NewServer(ts)
	defer srv.Close()
	cc := newCC(srv.URL)
	now := time.Unix(1_000_000, 0)
	cc.now = func() time.Time { return now }

	if tok, _ := cc.Token(context.Background()); tok != "tok-1" {
		t.Fatalf("tok = %q", tok)
	}
	now = now.Add(59 * time.Second) // 61 s left: still fresh
	if tok, _ := cc.Token(context.Background()); tok != "tok-1" {
		t.Fatalf("at 61 s left tok = %q, want cached tok-1", tok)
	}
	now = now.Add(time.Second) // 60 s left: refresh
	if tok, _ := cc.Token(context.Background()); tok != "tok-2" {
		t.Fatalf("at 60 s left tok = %q, want fresh tok-2", tok)
	}
}

func TestClientCredentials_NoExpiresInIsNotCached(t *testing.T) {
	ts := &tokenServer{t: t, expiresIn: 0}
	srv := httptest.NewServer(ts)
	defer srv.Close()
	cc := newCC(srv.URL)
	_, _ = cc.Token(context.Background())
	if tok, _ := cc.Token(context.Background()); tok != "tok-2" {
		t.Fatalf("tok = %q, want a fresh mint when the lifetime is unknown", tok)
	}
}

func TestClientCredentials_InvalidateForcesAMint(t *testing.T) {
	ts := &tokenServer{t: t, expiresIn: 3600}
	srv := httptest.NewServer(ts)
	defer srv.Close()
	cc := newCC(srv.URL)
	_, _ = cc.Token(context.Background())
	cc.Invalidate()
	if tok, _ := cc.Token(context.Background()); tok != "tok-2" {
		t.Fatalf("tok = %q, want tok-2 after Invalidate", tok)
	}
}

func TestClientCredentials_EndpointErrorNamesStatusNotSecret(t *testing.T) {
	ts := &tokenServer{t: t, status: http.StatusUnauthorized}
	srv := httptest.NewServer(ts)
	defer srv.Close()
	_, err := newCC(srv.URL).Token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "s3cr3t") {
		t.Fatalf("err = %v", err)
	}
}

func TestClientCredentials_ConcurrentCallersShareOneMint(t *testing.T) {
	ts := &tokenServer{t: t, expiresIn: 3600}
	srv := httptest.NewServer(ts)
	defer srv.Close()
	cc := newCC(srv.URL)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = cc.Token(context.Background()) }()
	}
	wg.Wait()
	if ts.mints.Load() != 1 {
		t.Fatalf("mints = %d, want 1", ts.mints.Load())
	}
}

func TestClientCredentials_RefreshOn401(t *testing.T) {
	ts := &tokenServer{t: t, expiresIn: 3600}
	idp := httptest.NewServer(ts)
	defer idp.Close()

	var calls atomic.Int32
	var seen []string
	var mu sync.Mutex
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/internal/v1/ae-studio/projects/greeter/repository" {
			t.Errorf("path = %s", r.URL.Path)
		}
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer tok-2" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"owner":"o","repo":"r","defaultBranch":"main","cloneUrl":"https://x/o/r.git"}`))
	}))
	defer api.Close()

	c, err := NewAEPAPI(api.URL+"/", newCC(idp.URL))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.GetAeStudioProjectRepository(context.Background(), "greeter")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if calls.Load() != 2 || ts.mints.Load() != 2 {
		t.Fatalf("api calls = %d, mints = %d; want 2 and 2 (401 → Invalidate → mint → one retry)", calls.Load(), ts.mints.Load())
	}
	if seen[0] != "Bearer tok-1" || seen[1] != "Bearer tok-2" {
		t.Fatalf("Authorization sequence = %v", seen)
	}
}

func TestNewAEPAPI_RetriesOnlyOnce(t *testing.T) {
	ts := &tokenServer{t: t, expiresIn: 3600}
	idp := httptest.NewServer(ts)
	defer idp.Close()
	var calls atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer api.Close()

	c, _ := NewAEPAPI(api.URL, newCC(idp.URL))
	resp, err := c.GetAeStudioProjectRepository(context.Background(), "greeter")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || calls.Load() != 2 {
		t.Fatalf("status = %d, calls = %d; want the second 401 returned after one retry", resp.StatusCode, calls.Load())
	}
}

func TestNewAEPAPI_TokenFailureIsATransportError(t *testing.T) {
	idp := httptest.NewServer(&tokenServer{t: t, status: http.StatusBadGateway})
	defer idp.Close()
	var calls atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer api.Close()

	c, _ := NewAEPAPI(api.URL, newCC(idp.URL))
	if _, err := c.GetAeStudioProjectRepository(context.Background(), "greeter"); err == nil {
		t.Fatal("want an error when no token can be minted")
	}
	if calls.Load() != 0 {
		t.Fatalf("aep-api called %d times without a token", calls.Load())
	}
}
