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

package thunder

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// newTestClient returns a pre-authenticated AdminClient pointing at srv.
// It bypasses New() so tests don't need a mock token endpoint.
func newTestClient(t *testing.T, srv *httptest.Server) *AdminClient {
	t.Helper()
	return &AdminClient{
		baseURL:   srv.URL,
		token:     "test-token",
		defaultOU: "ou-123",
		http:      srv.Client(),
	}
}

// TestParseAppList covers the bare-array and wrapped-object response shapes,
// and confirms that an unrecognised payload returns an error.
func TestParseAppList(t *testing.T) {
	apps := []appSummary{{ID: "id1", Name: "app1", ClientID: "client1"}}

	tests := []struct {
		name    string
		input   any
		wantID  string
		wantErr bool
	}{
		{
			name:   "bare array",
			input:  apps,
			wantID: "client1",
		},
		{
			name:   "wrapped applications object",
			input:  map[string]any{"applications": apps},
			wantID: "client1",
		},
		{
			name:    "unrecognised format errors",
			input:   "not-an-array",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, _ := json.Marshal(tc.input)
			got, err := parseAppList(data)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseAppList error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if len(got) != 1 || got[0].ClientID != tc.wantID {
				t.Fatalf("got %+v, want clientId=%q", got, tc.wantID)
			}
		})
	}
}

// TestExtractOAuthConfig covers the branches: valid entry, missing config,
// no oauth2 entry among others, and config field not a map.
func TestExtractOAuthConfig(t *testing.T) {
	tests := []struct {
		name    string
		app     map[string]any
		wantKey string // a key we expect in the returned map
		wantErr bool
	}{
		{
			name: "valid oauth2 entry",
			app: map[string]any{
				"inboundAuthConfig": []any{
					map[string]any{"type": "oauth2", "config": map[string]any{"clientId": "foo"}},
				},
			},
			wantKey: "clientId",
		},
		{
			name:    "missing inboundAuthConfig",
			app:     map[string]any{},
			wantErr: true,
		},
		{
			name: "no oauth2 entry among others",
			app: map[string]any{
				"inboundAuthConfig": []any{
					map[string]any{"type": "saml", "config": map[string]any{}},
				},
			},
			wantErr: true,
		},
		{
			name: "config is not a map",
			app: map[string]any{
				"inboundAuthConfig": []any{
					map[string]any{"type": "oauth2", "config": "not-a-map"},
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := extractOAuthConfig(tc.app)
			if (err != nil) != tc.wantErr {
				t.Fatalf("extractOAuthConfig error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if _, ok := cfg[tc.wantKey]; !ok {
				t.Fatalf("expected key %q in config %v", tc.wantKey, cfg)
			}
		})
	}
}

// TestToSlice verifies each normalisation branch: bare slice, known wrapper
// key, unknown key (returns nil), and nil input.
func TestToSlice(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		wantLen int
		wantNil bool
	}{
		{
			name:    "bare slice passthrough",
			input:   []any{"a", "b"},
			wantLen: 2,
		},
		{
			name:    "wrapped with known key applications",
			input:   map[string]any{"applications": []any{"x"}},
			wantLen: 1,
		},
		{
			name:    "wrapped with known key roles",
			input:   map[string]any{"roles": []any{"r1", "r2"}},
			wantLen: 2,
		},
		{
			name:    "unknown wrapper key returns nil",
			input:   map[string]any{"unknown": []any{"x"}},
			wantNil: true,
		},
		{
			name:    "nil returns nil",
			input:   nil,
			wantNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toSlice(tc.input)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %v", got)
				}
				return
			}
			if len(got) != tc.wantLen {
				t.Fatalf("got len %d, want %d: %v", len(got), tc.wantLen, got)
			}
		})
	}
}

// TestEnsureApplication_Create verifies that when no app exists (empty list),
// EnsureApplication issues POST /applications.
func TestEnsureApplication_Create(t *testing.T) {
	listBody := `[]`
	var postCalled bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/applications":
			_, _ = fmt.Fprint(w, listBody)
		case r.Method == http.MethodPost && r.URL.Path == "/applications":
			postCalled = true
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.EnsureApplication(context.Background(), DesiredApp{
		ClientID:     "new-client",
		ClientType:   "confidential",
		ClientSecret: "s3cr3t",
	}); err != nil {
		t.Fatalf("EnsureApplication: %v", err)
	}
	if !postCalled {
		t.Error("expected POST /applications to be called for a new app")
	}
}

// TestEnsureApplication_Update verifies that when the app already exists,
// EnsureApplication fetches the full object and issues PUT /applications/{id}.
func TestEnsureApplication_Update(t *testing.T) {
	const (
		appID    = "app-id-existing"
		clientID = "existing-client"
	)

	listBody, _ := json.Marshal([]appSummary{{ID: appID, Name: clientID, ClientID: clientID}})
	fullBody, _ := json.Marshal(map[string]any{
		"id":   appID,
		"name": clientID,
		"inboundAuthConfig": []any{
			map[string]any{
				"type":   "oauth2",
				"config": map[string]any{"clientId": clientID, "clientSecret": "old-secret"},
			},
		},
	})

	var putCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/applications":
			_, _ = w.Write(listBody)
		case r.Method == http.MethodGet && r.URL.Path == "/applications/"+appID:
			_, _ = w.Write(fullBody)
		case r.Method == http.MethodPut && r.URL.Path == "/applications/"+appID:
			putCalled = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.EnsureApplication(context.Background(), DesiredApp{
		ClientID:     clientID,
		ClientType:   "confidential",
		ClientSecret: "new-secret",
	}); err != nil {
		t.Fatalf("EnsureApplication update: %v", err)
	}
	if !putCalled {
		t.Error("expected PUT /applications/{id} to be called for an existing app")
	}
}

// TestFindAppByClientID_Pagination verifies that findAppByClientID follows
// pages: a full first page (100 items, none matching) causes a second request,
// which returns the target on a short page.
func TestFindAppByClientID_Pagination(t *testing.T) {
	const (
		target   = "target-client"
		pageSize = 100
	)

	// Build page 1: exactly pageSize non-matching apps.
	page1 := make([]appSummary, pageSize)
	for i := range page1 {
		page1[i] = appSummary{
			ID:       fmt.Sprintf("id-%d", i),
			Name:     fmt.Sprintf("app-%d", i),
			ClientID: fmt.Sprintf("client-%d", i),
		}
	}
	page1Body, _ := json.Marshal(page1)
	page2Body, _ := json.Marshal([]appSummary{{ID: "target-id", Name: target, ClientID: target}})

	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Query().Get("offset"), "0") || r.URL.Query().Get("offset") == "" {
			_, _ = w.Write(page1Body)
		} else {
			_, _ = w.Write(page2Body)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	id, err := c.findAppByClientID(context.Background(), target)
	if err != nil {
		t.Fatalf("findAppByClientID: %v", err)
	}
	if id != "target-id" {
		t.Errorf("got id %q, want %q", id, "target-id")
	}
	if requestCount != 2 {
		t.Errorf("expected 2 page requests, got %d", requestCount)
	}
}

// TestFindAppByClientID_TerminatesOnShortPage verifies that a page with fewer
// than 100 items stops pagination without issuing a second request.
func TestFindAppByClientID_TerminatesOnShortPage(t *testing.T) {
	shortBody, _ := json.Marshal([]appSummary{{ID: "id-1", Name: "other", ClientID: "other"}})

	requestCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(shortBody)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	id, err := c.findAppByClientID(context.Background(), "missing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "" {
		t.Errorf("expected empty id for missing client, got %q", id)
	}
	if requestCount != 1 {
		t.Errorf("expected 1 request (short page terminates), got %d", requestCount)
	}
}

func TestSystemResourceIdentifier(t *testing.T) {
	for in, want := range map[string]string{
		"http://thunder.openchoreo.localhost:8080": "http://thunder.openchoreo.localhost:8080/mcp",
		"https://idp.example.com/":                 "https://idp.example.com/mcp",
		"":                                         "",
	} {
		if got := SystemResourceIdentifier(in); got != want {
			t.Errorf("SystemResourceIdentifier(%q) = %q, want %q", in, got, want)
		}
	}
}

// New names the System resource server on the token request, alongside the
// client_secret_post credentials, and omits the parameter when given none.
func TestNew_TokenRequestCarriesResourceIndicator(t *testing.T) {
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			_ = r.ParseForm()
			form = r.PostForm
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok"})
		case "/organization-units/tree/default":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "ou-123"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	if _, err := New(context.Background(), srv.URL, "sys", "sec", "http://idp.example/mcp"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for k, want := range map[string]string{
		"grant_type": "client_credentials", "scope": "system", "client_id": "sys",
		"client_secret": "sec", "resource": "http://idp.example/mcp",
	} {
		if got := form.Get(k); got != want {
			t.Errorf("token form %s = %q, want %q", k, got, want)
		}
	}

	if _, err := New(context.Background(), srv.URL, "sys", "sec", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := form["resource"]; present {
		t.Errorf("resource parameter sent without an identifier: %q", form.Get("resource"))
	}
}

// TestAppType verifies aectl's ClientType maps to a ThunderID 1.0.0 app
// `type` value it actually accepts (browser/fullstack/mobile/m2m/mcp/custom).
func TestAppType(t *testing.T) {
	if got := appType("confidential"); got != "m2m" {
		t.Errorf("appType(confidential) = %q, want %q", got, "m2m")
	}
	if got := appType("public"); got != "browser" {
		t.Errorf("appType(public) = %q, want %q", got, "browser")
	}
}

// TestTokenClaimConfig verifies the claims nesting ThunderID 1.0.0 actually
// reads: clientConfig for a confidential/m2m app's access token, userConfig
// (plus a flat idToken) for a public/browser app's.
func TestTokenClaimConfig(t *testing.T) {
	t.Run("confidential nests under clientConfig, omits idToken", func(t *testing.T) {
		cfg := tokenClaimConfig("confidential", false)
		at, ok := cfg["accessToken"].(map[string]any)
		if !ok {
			t.Fatalf("accessToken missing or not a map: %v", cfg)
		}
		cc, ok := at["clientConfig"].(map[string]any)
		if !ok {
			t.Fatalf("accessToken.clientConfig missing or not a map: %v", at)
		}
		if cc["validityPeriod"] != tokenValiditySeconds {
			t.Errorf("clientConfig.validityPeriod = %v, want %v", cc["validityPeriod"], tokenValiditySeconds)
		}
		if !reflect.DeepEqual(cc["attributes"], identityUserAttributes) {
			t.Errorf("clientConfig.attributes = %v, want %v", cc["attributes"], identityUserAttributes)
		}
		if _, present := at["userConfig"]; present {
			t.Errorf("confidential accessToken should not carry userConfig: %v", at)
		}
		if _, present := cfg["idToken"]; present {
			t.Errorf("confidential app should not request an idToken: %v", cfg)
		}
	})

	t.Run("public nests under userConfig, keeps a flat idToken", func(t *testing.T) {
		cfg := tokenClaimConfig("public", false)
		at, ok := cfg["accessToken"].(map[string]any)
		if !ok {
			t.Fatalf("accessToken missing or not a map: %v", cfg)
		}
		uc, ok := at["userConfig"].(map[string]any)
		if !ok {
			t.Fatalf("accessToken.userConfig missing or not a map: %v", at)
		}
		if uc["validityPeriod"] != tokenValiditySeconds {
			t.Errorf("userConfig.validityPeriod = %v, want %v", uc["validityPeriod"], tokenValiditySeconds)
		}
		if !reflect.DeepEqual(uc["attributes"], identityUserAttributes) {
			t.Errorf("userConfig.attributes = %v, want %v", uc["attributes"], identityUserAttributes)
		}
		idToken, ok := cfg["idToken"].(map[string]any)
		if !ok {
			t.Fatalf("idToken missing or not a map: %v", cfg)
		}
		if idToken["validityPeriod"] != tokenValiditySeconds {
			t.Errorf("idToken.validityPeriod = %v, want %v", idToken["validityPeriod"], tokenValiditySeconds)
		}
		if !reflect.DeepEqual(idToken["userAttributes"], identityUserAttributes) {
			t.Errorf("idToken.userAttributes = %v, want %v", idToken["userAttributes"], identityUserAttributes)
		}
	})
}

// TestBuildCreatePayload_SetsType verifies the create payload carries the
// top-level `type` field ThunderID 1.0.0 requires.
func TestBuildCreatePayload_SetsType(t *testing.T) {
	confidential := buildCreatePayload(DesiredApp{ClientID: "svc", ClientType: "confidential", ClientSecret: "s"}, "ou-123")
	if confidential["type"] != "m2m" {
		t.Errorf("confidential payload type = %v, want %q", confidential["type"], "m2m")
	}

	public := buildCreatePayload(DesiredApp{ClientID: "app", ClientType: "public"}, "ou-123")
	if public["type"] != "browser" {
		t.Errorf("public payload type = %v, want %q", public["type"], "browser")
	}
}

func TestTokenClaimConfig_NoUserAttributes(t *testing.T) {
	cfg := tokenClaimConfig("confidential", true)
	attrs := cfg["accessToken"].(map[string]any)["clientConfig"].(map[string]any)["attributes"].([]string)
	if len(attrs) != 0 {
		t.Fatalf("AE-only client must carry no token attributes: %v", attrs)
	}
}

func TestBuildCreatePayload_NoUserAttributes(t *testing.T) {
	p := buildCreatePayload(DesiredApp{ClientID: "x", ClientType: "confidential", ClientSecret: "s", NoUserAttributes: true}, "ou")
	cfg := p["inboundAuthConfig"].([]map[string]any)[0]["config"].(map[string]any)
	token := cfg["token"].(map[string]any)
	attrs := token["accessToken"].(map[string]any)["clientConfig"].(map[string]any)["attributes"].([]string)
	if len(attrs) != 0 {
		t.Fatalf("create payload must carry no token attributes: %v", attrs)
	}
}

// --- an org's clients ----------------------------------------------------------

// orgClientServer answers the admin API for one org client: the org's
// organization unit by its handle, an app list, and a create or an update,
// whose body it captures.
type orgClientServer struct {
	ous      map[string]string // handle → id
	existing map[string]any    // the app GET /applications/{id} answers; nil = none
	sent     map[string]any    // the body of the POST or PUT
	method   string
}

func (o *orgClientServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/organization-units/tree/"):
			id, ok := o.ous[strings.TrimPrefix(r.URL.Path, "/organization-units/tree/")]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
		case r.Method == http.MethodGet && r.URL.Path == "/applications":
			apps := []appSummary{}
			if o.existing != nil {
				apps = append(apps, appSummary{ID: "app-1", Name: "aep-publisher-acme", ClientID: "aep-publisher-acme"})
			}
			_ = json.NewEncoder(w).Encode(apps)
		case r.Method == http.MethodGet && r.URL.Path == "/applications/app-1":
			_ = json.NewEncoder(w).Encode(o.existing)
		case (r.Method == http.MethodPost && r.URL.Path == "/applications") || (r.Method == http.MethodPut && r.URL.Path == "/applications/app-1"):
			o.method = r.Method
			_ = json.NewDecoder(r.Body).Decode(&o.sent)
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
}

func orgPublisher() DesiredApp {
	return DesiredApp{ClientID: "aep-publisher-acme", ClientType: "confidential", ClientSecret: "stored-secret", OrgOUHandle: "acme"}
}

// oauthConfigOf is the oauth2 config of a sent application body.
func oauthConfigOf(t *testing.T, app map[string]any) map[string]any {
	t.Helper()
	cfg, err := extractOAuthConfig(app)
	if err != nil {
		t.Fatalf("sent body has no oauth2 config: %v", err)
	}
	return cfg
}

// An org's client is created under the org's organization unit, found by its
// handle, with the secret aectl chose and the settings an org client needs:
// HTTP Basic at the token endpoint and a token that names the org and nothing
// else.
func TestEnsureApplication_OrgClientIsCreatedUnderTheOrgsOU(t *testing.T) {
	o := &orgClientServer{ous: map[string]string{"acme": "ou-acme"}}
	srv := o.start(t)
	defer srv.Close()

	if err := newTestClient(t, srv).EnsureApplication(context.Background(), orgPublisher()); err != nil {
		t.Fatalf("EnsureApplication: %v", err)
	}
	if o.method != http.MethodPost {
		t.Fatalf("method = %q, want a create", o.method)
	}
	if o.sent["ouId"] != "ou-acme" || o.sent["type"] != "m2m" {
		t.Fatalf("created under ouId %v as type %v, want the org's OU and m2m", o.sent["ouId"], o.sent["type"])
	}
	cfg := oauthConfigOf(t, o.sent)
	if cfg["clientId"] != "aep-publisher-acme" || cfg["clientSecret"] != "stored-secret" {
		t.Fatalf("client id %v: the client is not created with the chosen secret", cfg["clientId"])
	}
	if cfg["tokenEndpointAuthMethod"] != "client_secret_basic" {
		t.Fatalf("auth method = %v, want client_secret_basic", cfg["tokenEndpointAuthMethod"])
	}
	token := cfg["token"].(map[string]any)["accessToken"].(map[string]any)["clientConfig"].(map[string]any)
	if got := fmt.Sprint(token["attributes"]); got != "[ouId ouHandle]" {
		t.Fatalf("token attributes = %s, want [ouId ouHandle]", got)
	}
	if token["validityPeriod"] != float64(3600) {
		t.Fatalf("token validity = %v, want 3600", token["validityPeriod"])
	}
}

// aectl creates clients, never organization units: a handle ThunderID does
// not know stops the registration before any application call.
func TestEnsureApplication_OrgClientNeedsAnExistingOU(t *testing.T) {
	o := &orgClientServer{ous: map[string]string{}}
	srv := o.start(t)
	defer srv.Close()

	err := newTestClient(t, srv).EnsureApplication(context.Background(), orgPublisher())
	if err == nil || !strings.Contains(err.Error(), `"acme"`) {
		t.Fatalf("err = %v, want one naming the unknown handle", err)
	}
	if o.method != "" {
		t.Fatalf("a %s was sent for a client with no organization unit", o.method)
	}
}

// An existing org client under the org's OU is updated in place: its secret
// and its settings are set again, so a re-run converges it.
func TestEnsureApplication_OrgClientIsUpdatedInPlace(t *testing.T) {
	o := &orgClientServer{
		ous: map[string]string{"acme": "ou-acme"},
		existing: map[string]any{
			"id": "app-1", "name": "aep-publisher-acme", "ouId": "ou-acme",
			"inboundAuthConfig": []any{map[string]any{"type": "oauth2", "config": map[string]any{"clientId": "aep-publisher-acme", "clientSecret": "old"}}},
		},
	}
	srv := o.start(t)
	defer srv.Close()

	if err := newTestClient(t, srv).EnsureApplication(context.Background(), orgPublisher()); err != nil {
		t.Fatalf("EnsureApplication: %v", err)
	}
	if o.method != http.MethodPut {
		t.Fatalf("method = %q, want an update", o.method)
	}
	cfg := oauthConfigOf(t, o.sent)
	if cfg["clientSecret"] != "stored-secret" || cfg["tokenEndpointAuthMethod"] != "client_secret_basic" {
		t.Fatalf("the update did not set the secret and the auth method (method %v)", cfg["tokenEndpointAuthMethod"])
	}
}

// An org client found under another organization unit is never moved or
// changed: its tokens would name that unit's org.
func TestEnsureApplication_OrgClientUnderAnotherOUIsRefused(t *testing.T) {
	o := &orgClientServer{
		ous: map[string]string{"acme": "ou-acme"},
		existing: map[string]any{
			"id": "app-1", "name": "aep-publisher-acme", "ouId": "ou-other",
			"inboundAuthConfig": []any{map[string]any{"type": "oauth2", "config": map[string]any{"clientId": "aep-publisher-acme"}}},
		},
	}
	srv := o.start(t)
	defer srv.Close()

	err := newTestClient(t, srv).EnsureApplication(context.Background(), orgPublisher())
	if err == nil || !strings.Contains(err.Error(), "another organization unit") {
		t.Fatalf("err = %v, want a refusal naming the other organization unit", err)
	}
	if o.method != "" {
		t.Fatalf("a %s was sent for a client under another organization unit", o.method)
	}
}

// A platform client keeps the default OU and its own settings.
func TestBuildCreatePayload_PlatformClientIsNotAnOrgClient(t *testing.T) {
	p := buildCreatePayload(DesiredApp{ClientID: "svc", ClientType: "confidential", ClientSecret: "s"}, "ou-default")
	cfg := oauthConfigOf(t, map[string]any{"inboundAuthConfig": []any{map[string]any(p["inboundAuthConfig"].([]map[string]any)[0])}})
	if p["ouId"] != "ou-default" || cfg["tokenEndpointAuthMethod"] != "client_secret_post" {
		t.Fatalf("ouId %v, auth method %v; want the default OU and client_secret_post", p["ouId"], cfg["tokenEndpointAuthMethod"])
	}
}
