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

// TestAssignAdminRole_AlreadyAssigned verifies that AssignAdminRole returns
// nil without calling the assignments/add endpoint when the app is already
// in the role's assignments (read from GET /roles/{id}/assignments — its own
// sub-resource on ThunderID 1.0.0, not a field on the role object returned by
// GET /roles/{id}; see ensureAppInRole's comment).
func TestAssignAdminRole_AlreadyAssigned(t *testing.T) {
	const (
		appID    = "app-id-sys"
		roleID   = "role-id-sys"
		clientID = "aep-system-client"
	)

	appList, _ := json.Marshal([]appSummary{{ID: appID, Name: clientID, ClientID: clientID}})
	roleList, _ := json.Marshal([]map[string]any{{"id": roleID, "name": "aep-system"}})
	assignments, _ := json.Marshal(map[string]any{
		"assignments": []any{
			map[string]any{"id": appID, "type": "app"},
		},
	})

	var addCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/applications":
			_, _ = w.Write(appList)
		case r.Method == http.MethodGet && r.URL.Path == "/roles":
			_, _ = w.Write(roleList)
		case r.Method == http.MethodGet && r.URL.Path == "/roles/"+roleID+"/assignments":
			_, _ = w.Write(assignments)
		case r.Method == http.MethodPost && r.URL.Path == "/roles/"+roleID+"/assignments/add":
			addCalled = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.AssignAdminRole(context.Background(), clientID); err != nil {
		t.Fatalf("AssignAdminRole: %v", err)
	}
	if addCalled {
		t.Error("assignments/add should not be called when app is already assigned")
	}
}

// TestAssignAdminRole_RoleExistsMissingApp verifies that AssignAdminRole calls
// POST /roles/{id}/assignments/add to add the app when the role exists but
// its assignments (from GET /roles/{id}/assignments) don't include it yet,
// and that the request body carries the new assignment.
func TestAssignAdminRole_RoleExistsMissingApp(t *testing.T) {
	const (
		appID    = "app-id-sys"
		roleID   = "role-id-sys"
		clientID = "aep-system-client"
	)

	appList, _ := json.Marshal([]appSummary{{ID: appID, Name: clientID, ClientID: clientID}})
	roleList, _ := json.Marshal([]map[string]any{{"id": roleID, "name": "aep-system"}})
	assignments, _ := json.Marshal(map[string]any{"assignments": []any{}})

	var addCalled bool
	var addedAssignments []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/applications":
			_, _ = w.Write(appList)
		case r.Method == http.MethodGet && r.URL.Path == "/roles":
			_, _ = w.Write(roleList)
		case r.Method == http.MethodGet && r.URL.Path == "/roles/"+roleID+"/assignments":
			_, _ = w.Write(assignments)
		case r.Method == http.MethodPost && r.URL.Path == "/roles/"+roleID+"/assignments/add":
			addCalled = true
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			addedAssignments = toSlice(body["assignments"])
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.AssignAdminRole(context.Background(), clientID); err != nil {
		t.Fatalf("AssignAdminRole: %v", err)
	}
	if !addCalled {
		t.Fatal("expected POST /roles/{id}/assignments/add to add the missing app assignment")
	}
	found := false
	for _, item := range addedAssignments {
		m, _ := item.(map[string]any)
		if m["id"] == appID && m["type"] == "app" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("assignments/add body should contain {id: %q, type: \"app\"}, got %v", appID, addedAssignments)
	}
}

// TestAssignAdminRole_RoleMissing verifies that AssignAdminRole creates the
// aep-system role via POST /roles with the app assignment inline when no such
// role exists yet.
//
// The resource-servers fixture uses an absolute-URI identifier, matching
// what ThunderID 1.0.0 actually returns — "system" is only the HANDLE of the
// one resource nested inside the resource server, a different field.
func TestAssignAdminRole_RoleMissing(t *testing.T) {
	const (
		appID                    = "app-id-sys"
		rsID                     = "rs-system-id"
		clientID                 = "aep-system-client"
		systemResourceIdentifier = "http://thunder.example.com/mcp"
	)

	appList, _ := json.Marshal([]appSummary{{ID: appID, Name: clientID, ClientID: clientID}})
	roleList, _ := json.Marshal([]map[string]any{})
	rsList, _ := json.Marshal([]map[string]any{{"id": rsID, "name": "System", "identifier": systemResourceIdentifier}})

	var postCalled bool
	var postAssignments []any
	var postName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/applications":
			_, _ = w.Write(appList)
		case r.Method == http.MethodGet && r.URL.Path == "/roles":
			_, _ = w.Write(roleList)
		case r.Method == http.MethodGet && r.URL.Path == "/resource-servers":
			_, _ = w.Write(rsList)
		case r.Method == http.MethodPost && r.URL.Path == "/roles":
			postCalled = true
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			postName, _ = body["name"].(string)
			postAssignments = toSlice(body["assignments"])
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	c.systemResourceIdentifier = systemResourceIdentifier
	if err := c.AssignAdminRole(context.Background(), clientID); err != nil {
		t.Fatalf("AssignAdminRole: %v", err)
	}
	if !postCalled {
		t.Fatal("expected POST /roles to create the missing aep-system role")
	}
	if postName != "aep-system" {
		t.Errorf("expected role name %q, got %q", "aep-system", postName)
	}
	found := false
	for _, item := range postAssignments {
		m, _ := item.(map[string]any)
		if m["id"] == appID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("POST body assignments should contain app %q, got %v", appID, postAssignments)
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
		cfg := tokenClaimConfig("confidential")
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
		cfg := tokenClaimConfig("public")
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
	c := &AdminClient{defaultOU: "ou-123"}

	confidential := c.buildCreatePayload(DesiredApp{ClientID: "svc", ClientType: "confidential", ClientSecret: "s"})
	if confidential["type"] != "m2m" {
		t.Errorf("confidential payload type = %v, want %q", confidential["type"], "m2m")
	}

	public := c.buildCreatePayload(DesiredApp{ClientID: "app", ClientType: "public"})
	if public["type"] != "browser" {
		t.Errorf("public payload type = %v, want %q", public["type"], "browser")
	}
}

// TestFindSystemResourceServerID_MatchesByIdentifier verifies the primary
// match path: the resource server whose identifier equals the value New()
// captured (the same one sent as the OAuth `resource` indicator).
func TestFindSystemResourceServerID_MatchesByIdentifier(t *testing.T) {
	const identifier = "http://thunder.example.com/mcp"
	rsList, _ := json.Marshal([]map[string]any{
		{"id": "rs-other", "name": "Other", "identifier": "http://thunder.example.com/other"},
		{"id": "rs-system", "name": "System", "identifier": identifier},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(rsList)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	c.systemResourceIdentifier = identifier
	id, err := c.findSystemResourceServerID(context.Background())
	if err != nil {
		t.Fatalf("findSystemResourceServerID: %v", err)
	}
	if id != "rs-system" {
		t.Errorf("got id %q, want %q", id, "rs-system")
	}
}

// TestFindSystemResourceServerID_FallsBackToName verifies that with no
// systemResourceIdentifier set (New() received no public URL), the lookup
// falls back to matching name=="System" rather than failing outright.
func TestFindSystemResourceServerID_FallsBackToName(t *testing.T) {
	rsList, _ := json.Marshal([]map[string]any{
		{"id": "rs-system", "name": "System", "identifier": "https://localhost:8090/mcp"},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(rsList)
	}))
	defer srv.Close()

	c := newTestClient(t, srv) // systemResourceIdentifier left empty
	id, err := c.findSystemResourceServerID(context.Background())
	if err != nil {
		t.Fatalf("findSystemResourceServerID: %v", err)
	}
	if id != "rs-system" {
		t.Errorf("got id %q, want %q", id, "rs-system")
	}
}
