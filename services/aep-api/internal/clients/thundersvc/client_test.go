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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// thunderMock is a minimal in-memory Thunder admin API for exercising
// EnsurePublisherApp's OU self-heal without a live cluster.
type thunderMock struct {
	appID    string // internal id of the existing publisher app ("" = none)
	appName  string
	clientID string
	ouID     string // OU the existing app is registered under

	deleted      bool
	deleteStatus int    // override delete response code (0 = 204); app is removed regardless
	createdOU    string // OU passed to the create call
	createdType  string // application type passed to the create call
	createdAttrs []any  // token.accessToken.clientConfig.attributes in the create call

	// hasTokenClaims makes GET /applications/{id} report an app whose token
	// config already lists ouId/ouHandle; putCount/putAttrs record PUTs.
	hasTokenClaims bool
	putCount       int
	putAttrs       []any
	createCount    int
	ouMissing      bool // when true, GET /organization-units/{id} returns 404 (phantom OU)

	// systemRS models ThunderID 1.0.0's scope resolution: when set, the token
	// carries the `system` scope only if the request named it as `resource`;
	// otherwise the scope is dropped and the token issued anyway. Empty means
	// a Thunder with no default resource server, where the scope always
	// resolves.
	systemRS  string
	tokenForm url.Values // the last token request's form body

	// mints counts token-endpoint calls; issued holds every token handed out,
	// oldest first. expIn, when non-zero, stamps an `exp` claim that many
	// seconds from now (negative = already expired). revoked names tokens the
	// admin endpoints refuse with 401, the way ThunderID refuses an expired one.
	mints     int
	issued    []string
	expIn     int64
	revoked   map[string]bool
	revokeAll bool // every token is refused: a Thunder that will not accept this client at all
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
		if r.Method == http.MethodPost && r.URL.Path == "/oauth2/token" {
			_ = r.ParseForm()
			m.tokenForm = r.PostForm
			m.mints++
			claims := map[string]any{"iss": "mock", "aud": "urn:mock", "jti": fmt.Sprintf("t%d", m.mints)}
			if m.systemRS == "" || r.PostForm.Get("resource") == m.systemRS {
				claims["scope"] = "system"
			}
			if m.expIn != 0 {
				claims["exp"] = time.Now().Unix() + m.expIn
			}
			tok := unsignedJWT(t, claims)
			m.issued = append(m.issued, tok)
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": tok, "expires_in": 3600})
			return
		}
		// Every admin endpoint below authenticates the bearer first. A revoked
		// token is answered the way ThunderID answers an expired one: 401 with
		// AUTH-4010, no hint of the cause.
		if bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); m.revokeAll || m.revoked[bearer] {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"AUTH-4010","message":{"key":"error.auth.unauthorized","defaultValue":"Unauthorized"}}`))
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/applications":
			var apps []map[string]any
			if m.appID != "" {
				apps = append(apps, map[string]any{"id": m.appID, "name": m.appName, "clientId": m.clientID})
			}
			_ = json.NewEncoder(w).Encode(apps)

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/applications/"):
			cfg := map[string]any{"clientId": m.clientID, "grantTypes": []string{"client_credentials"}}
			if m.hasTokenClaims {
				cfg["token"] = map[string]any{"accessToken": map[string]any{
					"clientConfig": map[string]any{"validityPeriod": 3600, "attributes": []string{"ouId", "ouHandle"}},
				}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": m.appID, "name": m.appName, "ouId": m.ouID,
				"inboundAuthConfig": []map[string]any{{"type": "oauth2", "config": cfg}},
			})

		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/applications/"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			m.putCount++
			m.putAttrs = tokenAttributesOf(body)
			if _, hasID := body["id"]; hasID {
				t.Errorf("PUT body must not carry id")
			}
			_ = json.NewEncoder(w).Encode(body)

		case r.Method == http.MethodGet && r.URL.Path == "/organization-units/tree/default":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "default-ou"})

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/organization-units/"):
			// OU-existence check (ouExists) used by the non-destructive heal
			// guard. By default the target OU exists (200); ouMissing models a
			// stale/phantom ouId (404).
			if m.ouMissing {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": strings.TrimPrefix(r.URL.Path, "/organization-units/")})

		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/applications/"):
			m.deleted = true
			m.appID = "" // removed server-side regardless of the response code
			if m.deleteStatus != 0 {
				w.WriteHeader(m.deleteStatus)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodPost && r.URL.Path == "/applications":
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				OuID string `json:"ouId"`
				Type string `json:"type"`
				Rest map[string]any
			}
			_ = json.Unmarshal(raw, &body)
			_ = json.Unmarshal(raw, &body.Rest)
			// ThunderID 1.0.0: an application must declare its type. A create
			// without one is a 400 APP-1042, which is what a payload written
			// against an older Thunder produced on the real IdP.
			if body.Type == "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"code":"APP-1042","message":{"key":"error.applicationservice.application_type_required","defaultValue":"Application type is required"}}`))
				return
			}
			m.createdType = body.Type
			m.createdOU = body.OuID
			m.createdAttrs = tokenAttributesOf(body.Rest)
			m.createCount++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"clientId": m.appName, "clientSecret": "fresh-secret",
			})

		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
}

func newTestClient(base string) *client {
	return New(Config{BaseURL: base, ClientID: "sys", ClientSecret: "sec"}).(*client)
}

// Wrong OU → delete + recreate under the org OU, created=true, secret rotated.
func TestEnsurePublisherApp_HealsWrongOU(t *testing.T) {
	m := &thunderMock{appID: "app-1", appName: "aep-publisher-org1", clientID: "aep-publisher-org1", ouID: "default-ou"}
	srv := m.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	id, secret, created, err := c.EnsurePublisherApp(context.Background(), "org1", "org-ou-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.deleted {
		t.Error("expected the wrong-OU app to be deleted")
	}
	if m.createdOU != "org-ou-1" {
		t.Errorf("recreated under OU %q, want org-ou-1", m.createdOU)
	}
	if !created || secret != "fresh-secret" {
		t.Errorf("want created=true with rotated secret, got created=%v secret=%q", created, secret)
	}
	if id != "aep-publisher-org1" {
		t.Errorf("client_id changed: got %q", id)
	}
}

// Wrong OU + Thunder returns 500 on delete but the app is actually gone →
// the heal must still recreate under the org OU (one-pass durability).
func TestEnsurePublisherApp_HealsWrongOU_DeleteReturns500ButGone(t *testing.T) {
	m := &thunderMock{appID: "app-1", appName: "aep-publisher-org1", clientID: "aep-publisher-org1", ouID: "default-ou", deleteStatus: 500}
	srv := m.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	_, secret, created, err := c.EnsurePublisherApp(context.Background(), "org1", "org-ou-1")
	if err != nil {
		t.Fatalf("heal must tolerate a 500-but-deleted delete, got error: %v", err)
	}
	if m.createdOU != "org-ou-1" || !created || secret != "fresh-secret" {
		t.Errorf("want recreate under org-ou-1 (created+secret), got ou=%q created=%v secret=%q", m.createdOU, created, secret)
	}
}

// Phantom org OU (resolved OU does NOT exist in Thunder) → the non-destructive
// heal guard MUST keep the working app rather than delete it and fail to
// recreate under a non-existent OU (Thunder 400 APP-1018). Regression test for
// the runner publisher cc-token invalid_client root cause.
func TestEnsurePublisherApp_PhantomOU_KeepsExistingApp(t *testing.T) {
	m := &thunderMock{appID: "app-1", appName: "aep-publisher-org1", clientID: "aep-publisher-org1", ouID: "real-ou", ouMissing: true}
	srv := m.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	id, secret, created, err := c.EnsurePublisherApp(context.Background(), "org1", "phantom-ou")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.deleted {
		t.Error("MUST NOT delete the working app when the resolved target OU is phantom")
	}
	if created || secret != "" {
		t.Errorf("expected no recreate (created=false, empty secret), got created=%v secret=%q", created, secret)
	}
	if id != "aep-publisher-org1" {
		t.Errorf("should return the existing client_id, got %q", id)
	}
}

// Correct OU → no delete, no recreate, created=false.
func TestEnsurePublisherApp_CorrectOU_NoHeal(t *testing.T) {
	m := &thunderMock{appID: "app-1", appName: "aep-publisher-org1", clientID: "aep-publisher-org1", ouID: "org-ou-1"}
	srv := m.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	_, _, created, err := c.EnsurePublisherApp(context.Background(), "org1", "org-ou-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.deleted || m.createCount != 0 {
		t.Errorf("must not heal when OU already matches (deleted=%v creates=%d)", m.deleted, m.createCount)
	}
	if created {
		t.Error("want created=false for an existing correct-OU app")
	}
}

// No existing app + org OU known → create under the org OU.
func TestEnsurePublisherApp_CreatesUnderOrgOU(t *testing.T) {
	m := &thunderMock{appName: "aep-publisher-org1"}
	srv := m.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	_, _, created, err := c.EnsurePublisherApp(context.Background(), "org1", "org-ou-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created || m.createdOU != "org-ou-1" {
		t.Errorf("want fresh create under org-ou-1, got created=%v ou=%q", created, m.createdOU)
	}
	if m.deleted {
		t.Error("must not delete when no app existed")
	}
}

// -- system token: resource indicator ----------------------------------------

// The token request names the System resource server as `resource`, alongside
// the client_secret_post credentials, and the resulting token is accepted.
func TestSystemToken_SendsResourceIndicator(t *testing.T) {
	m := &thunderMock{systemRS: "http://idp.example/mcp"}
	srv := m.server(t)
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, ClientID: "sys", ClientSecret: "sec",
		SystemResourceIdentifier: "http://idp.example/mcp"}).(*client)

	if _, err := c.OUExists(context.Background(), "ou-1"); err != nil {
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

	if _, err := c.OUExists(context.Background(), "ou-1"); err != nil {
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

	_, err := c.OUExists(context.Background(), "ou-1")
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

func TestSystemResourceIdentifier(t *testing.T) {
	for in, want := range map[string]string{
		"http://thunder.openchoreo.localhost:8080": "http://thunder.openchoreo.localhost:8080/mcp",
		"https://idp.example.com/":                 "https://idp.example.com/mcp",
		"  https://idp.example.com//  ":            "https://idp.example.com/mcp",
		"":                                         "",
	} {
		if got := SystemResourceIdentifier(in); got != want {
			t.Errorf("SystemResourceIdentifier(%q) = %q, want %q", in, got, want)
		}
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
func TestEnsurePublisherApp_CreateDeclaresM2MType(t *testing.T) {
	m := &thunderMock{appName: "aep-publisher-org1"}
	srv := m.server(t)
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, secret, created, err := c.EnsurePublisherApp(context.Background(), "org1", "org-ou-1")
	if err != nil {
		t.Fatalf("EnsurePublisherApp: %v", err)
	}
	if !created || secret != "fresh-secret" {
		t.Fatalf("created=%v secret=%q, want a fresh create", created, secret)
	}
	if m.createdType != "m2m" {
		t.Fatalf("create payload type = %q, want m2m", m.createdType)
	}
	if m.createdOU != "org-ou-1" {
		t.Fatalf("create payload ouId = %q, want org-ou-1", m.createdOU)
	}
	assertPublisherAttrs(t, "create", m.createdAttrs)
}

// tokenAttributesOf digs inboundAuthConfig[0].config.token.accessToken.clientConfig.attributes
// out of an application payload (nil when absent).
func tokenAttributesOf(app map[string]any) []any {
	entries, _ := app["inboundAuthConfig"].([]any)
	if len(entries) == 0 {
		return nil
	}
	entry, _ := entries[0].(map[string]any)
	cfg, _ := entry["config"].(map[string]any)
	tokenCfg, _ := cfg["token"].(map[string]any)
	access, _ := tokenCfg["accessToken"].(map[string]any)
	clientCfg, _ := access["clientConfig"].(map[string]any)
	attrs, _ := clientCfg["attributes"].([]any)
	return attrs
}

func assertPublisherAttrs(t *testing.T, what string, attrs []any) {
	t.Helper()
	if len(attrs) != 2 || attrs[0] != "ouId" || attrs[1] != "ouHandle" {
		t.Fatalf("%s token attributes = %v, want [ouId ouHandle]", what, attrs)
	}
}

// An existing publisher whose token config predates the claim attributes is
// healed in place: one PUT adds ouId/ouHandle, the client_id is unchanged and
// nothing is recreated (the secret stays, so nothing is re-mirrored).
func TestEnsurePublisherApp_HealsMissingTokenClaims(t *testing.T) {
	m := &thunderMock{appID: "app-1", appName: "aep-publisher-org1", clientID: "aep-publisher-org1", ouID: "org-ou-1"}
	srv := m.server(t)
	defer srv.Close()

	id, secret, created, err := newTestClient(srv.URL).EnsurePublisherApp(context.Background(), "org1", "org-ou-1")
	if err != nil {
		t.Fatalf("EnsurePublisherApp: %v", err)
	}
	if created || secret != "" || id != "aep-publisher-org1" {
		t.Fatalf("id=%q secret=%q created=%v, want the existing app untouched apart from its token config", id, secret, created)
	}
	if m.putCount != 1 {
		t.Fatalf("putCount=%d, want exactly one PUT adding the token claims", m.putCount)
	}
	assertPublisherAttrs(t, "PUT", m.putAttrs)
	if m.deleted || m.createCount != 0 {
		t.Fatalf("heal must not recreate the app (deleted=%v createCount=%d)", m.deleted, m.createCount)
	}
}

// An app that already declares the claim attributes is left alone.
func TestEnsurePublisherApp_KeepsTokenClaimsWhenPresent(t *testing.T) {
	m := &thunderMock{appID: "app-1", appName: "aep-publisher-org1", clientID: "aep-publisher-org1", ouID: "org-ou-1", hasTokenClaims: true}
	srv := m.server(t)
	defer srv.Close()

	if _, _, _, err := newTestClient(srv.URL).EnsurePublisherApp(context.Background(), "org1", "org-ou-1"); err != nil {
		t.Fatalf("EnsurePublisherApp: %v", err)
	}
	if m.putCount != 0 {
		t.Fatalf("putCount=%d, want no PUT when the claims are already declared", m.putCount)
	}
}
