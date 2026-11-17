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

package thundersvc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// thunderMock is a minimal in-memory Thunder admin API for exercising the
// system token without a live cluster: the token endpoint and one admin read.
type thunderMock struct {
	// tokenCount counts mints; tokenExp, when set, is the exp claim issued;
	// ouUnauthorized answers 401 to that many admin reads.
	tokenCount     int
	tokenExp       int64
	ouUnauthorized int

	// systemRS models ThunderID 1.0.0's scope resolution: when set, the token
	// carries the `system` scope only if the request named it as `resource`;
	// otherwise the scope is dropped and the token issued anyway. Empty means
	// a Thunder with no default resource server, where the scope always
	// resolves.
	systemRS  string
	tokenForm url.Values // the last token request's form body
}

// unsignedJWT builds a three-part token with these claims and a fake signature.
// The client reads the payload only, so no key is involved.
func unsignedJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	return enc(map[string]string{"alg": "none"}) + "." + enc(claims) + ".sig"
}

func (m *thunderMock) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/oauth2/token":
			_ = r.ParseForm()
			m.tokenForm = r.PostForm
			m.tokenCount++
			claims := map[string]any{"iss": "mock", "aud": "urn:mock"}
			if m.tokenExp != 0 {
				claims["exp"] = m.tokenExp
			}
			if m.systemRS == "" || r.PostForm.Get("resource") == m.systemRS {
				claims["scope"] = "system"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": unsignedJWT(t, claims), "expires_in": 3600})

		case r.Method == http.MethodGet && r.URL.Path == "/organization-units/tree/default":
			if m.ouUnauthorized > 0 {
				m.ouUnauthorized--
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "ou-default"})

		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
}

func newTestClient(base string) *client {
	return New(Config{BaseURL: base, ClientID: "sys", ClientSecret: "sec"}).(*client)
}

// adminCall is the cheapest admin call there is: it takes the system token
// (minting one when none is cached) and reads the default OU with it, with
// the OU cache cleared so the read always reaches the server.
func adminCall(c *client) error {
	ctx := context.Background()
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return err
	}
	c.muOU.Lock()
	c.defaultOU = ""
	c.muOU.Unlock()
	_, err = c.getDefaultOUID(ctx, token)
	return err
}

func TestSystemToken_SendsResourceIndicator(t *testing.T) {
	m := &thunderMock{systemRS: "http://idp.example/mcp"}
	srv := m.server(t)
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, ClientID: "sys", ClientSecret: "sec",
		SystemResourceIdentifier: "http://idp.example/mcp"}).(*client)

	if err := adminCall(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for k, want := range map[string]string{
		"grant_type": "client_credentials", "scope": "system",
		"client_id": "sys", "client_secret": "sec", "resource": "http://idp.example/mcp",
	} {
		if got := m.tokenForm.Get(k); got != want {
			t.Errorf("token form %s = %q, want %q", k, got, want)
		}
	}
}

// No identifier configured → no `resource` parameter at all (a Thunder without
// a default resource server resolves `system` on its own).
func TestSystemToken_NoIdentifier_OmitsResource(t *testing.T) {
	m := &thunderMock{}
	srv := m.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	if err := adminCall(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := m.tokenForm["resource"]; present {
		t.Errorf("resource parameter sent without an identifier: %q", m.tokenForm.Get("resource"))
	}
}

// The trap itself: the indicator names the wrong resource server, Thunder
// answers 200 with a scope-less token, and the client must say so at mint time
// instead of letting the next admin call 403 with no explanation.
func TestSystemToken_ScopeDropped_IsNamedAtMint(t *testing.T) {
	m := &thunderMock{systemRS: "http://idp.example/mcp"}
	srv := m.server(t)
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, ClientID: "sys", ClientSecret: "sec",
		SystemResourceIdentifier: "http://elsewhere.example/mcp"}).(*client)

	err := adminCall(c)
	if err == nil {
		t.Fatal("expected the scope-less token to be refused")
	}
	for _, want := range []string{"without the system scope", "http://elsewhere.example/mcp"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
	if c.cachedToken != "" {
		t.Error("a refused token must not be cached")
	}
}

func TestTokenScopes(t *testing.T) {
	jwt := func(claims map[string]any) string { return unsignedJWT(t, claims) }
	cases := []struct {
		name   string
		token  string
		scopes []string
		ok     bool
	}{
		{"space-separated string", jwt(map[string]any{"scope": "openid system"}), []string{"openid", "system"}, true},
		{"list", jwt(map[string]any{"scope": []string{"system"}}), []string{"system"}, true},
		{"absent claim is readable as no scopes", jwt(map[string]any{"aud": "x"}), nil, true},
		{"opaque token is not readable", "opaque-token", nil, false},
		{"unexpected claim type is not readable", jwt(map[string]any{"scope": 42}), nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tokenScopes(tc.token)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if strings.Join(got, " ") != strings.Join(tc.scopes, " ") {
				t.Errorf("scopes = %v, want %v", got, tc.scopes)
			}
		})
	}
}

// A fresh publisher create declares its application type: ThunderID 1.0.0
// rejects a typeless create, and the publisher is a client_credentials-only
// client, so it is m2m.
func TestSystemToken_ExpiryFollowsTheWallClock(t *testing.T) {
	m := &thunderMock{}
	srv := m.server(t)
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, ClientID: "sys", ClientSecret: "sec"}).(*client)
	now := time.Date(2026, 9, 22, 8, 34, 51, 0, time.UTC)
	c.now = func() time.Time { return now }

	if err := adminCall(c); err != nil {
		t.Fatalf("admin call: %v", err)
	}
	now = now.Add(40 * time.Minute)
	if err := adminCall(c); err != nil {
		t.Fatalf("admin call: %v", err)
	}
	if m.tokenCount != 1 {
		t.Fatalf("mints after 40 min = %d, want 1 (still within the hour)", m.tokenCount)
	}
	now = now.Add(20*time.Minute + time.Second) // 60m01s: past expires_in minus the skew
	if err := adminCall(c); err != nil {
		t.Fatalf("admin call: %v", err)
	}
	if m.tokenCount != 2 {
		t.Fatalf("mints after the hour = %d, want 2 (a re-mint)", m.tokenCount)
	}
}

// The token's own exp is the server's verdict and wins over expires_in when
// it is sooner — a clock this side cannot count down more truthfully than that.
func TestSystemToken_TheTokensOwnExpWins(t *testing.T) {
	now := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
	m := &thunderMock{tokenExp: now.Add(2 * time.Minute).Unix()} // expires_in still says 3600
	srv := m.server(t)
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, ClientID: "sys", ClientSecret: "sec"}).(*client)
	c.now = func() time.Time { return now }

	if err := adminCall(c); err != nil {
		t.Fatalf("admin call: %v", err)
	}
	now = now.Add(91 * time.Second) // past exp minus the 30s skew
	if err := adminCall(c); err != nil {
		t.Fatalf("admin call: %v", err)
	}
	if m.tokenCount != 2 {
		t.Fatalf("mints = %d, want 2: exp two minutes out must beat expires_in of an hour", m.tokenCount)
	}
}

// A 401 answered to the system token is Thunder saying it no longer honours
// it — after a restart that dropped issued tokens, or because it expired while
// this side's clock disagreed. The token is evicted so the NEXT call mints
// afresh, instead of every call failing until the cache would have expired.
func TestSystemToken_EvictedWhenThunderAnswers401(t *testing.T) {
	m := &thunderMock{ouUnauthorized: 1}
	srv := m.server(t)
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, ClientID: "sys", ClientSecret: "sec"}).(*client)

	if err := adminCall(c); err == nil {
		t.Fatal("the 401 must surface to this caller")
	}
	if c.cachedToken != "" {
		t.Fatal("a token Thunder refused with 401 must not stay cached")
	}
	if err := adminCall(c); err != nil {
		t.Fatalf("second call = %v, want a fresh mint and success", err)
	}
	if m.tokenCount != 2 {
		t.Fatalf("mints = %d, want 2", m.tokenCount)
	}
}
