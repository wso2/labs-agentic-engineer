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
		name      string
		input     any
		wantLen   int
		wantNil   bool
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

// TestAssignAdminRole_AlreadyAssigned verifies that AssignAdminRole returns nil
// without issuing a PUT when the app is already in the role's assignments.
func TestAssignAdminRole_AlreadyAssigned(t *testing.T) {
	const (
		appID    = "app-id-sys"
		roleID   = "role-id-sys"
		clientID = "aep-system-client"
	)

	appList, _ := json.Marshal([]appSummary{{ID: appID, Name: clientID, ClientID: clientID}})
	roleList, _ := json.Marshal([]map[string]any{{"id": roleID, "name": "aep-system"}})
	roleDetail, _ := json.Marshal(map[string]any{
		"id":   roleID,
		"name": "aep-system",
		"assignments": []any{
			map[string]any{"id": appID, "type": "app"},
		},
	})

	var putCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/applications":
			_, _ = w.Write(appList)
		case r.Method == http.MethodGet && r.URL.Path == "/roles":
			_, _ = w.Write(roleList)
		case r.Method == http.MethodGet && r.URL.Path == "/roles/"+roleID:
			_, _ = w.Write(roleDetail)
		case r.Method == http.MethodPut && r.URL.Path == "/roles/"+roleID:
			putCalled = true
			w.WriteHeader(http.StatusOK)
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
	if putCalled {
		t.Error("PUT /roles should not be called when app is already assigned")
	}
}

// TestAssignAdminRole_RoleExistsMissingApp verifies that AssignAdminRole issues
// PUT /roles/{id} to add the app when the role exists but doesn't include it,
// and that the PUT body contains the new assignment.
func TestAssignAdminRole_RoleExistsMissingApp(t *testing.T) {
	const (
		appID    = "app-id-sys"
		roleID   = "role-id-sys"
		clientID = "aep-system-client"
	)

	appList, _ := json.Marshal([]appSummary{{ID: appID, Name: clientID, ClientID: clientID}})
	roleList, _ := json.Marshal([]map[string]any{{"id": roleID, "name": "aep-system"}})
	roleDetail, _ := json.Marshal(map[string]any{
		"id":          roleID,
		"name":        "aep-system",
		"assignments": []any{},
	})

	var putCalled bool
	var putAssignments []any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/applications":
			_, _ = w.Write(appList)
		case r.Method == http.MethodGet && r.URL.Path == "/roles":
			_, _ = w.Write(roleList)
		case r.Method == http.MethodGet && r.URL.Path == "/roles/"+roleID:
			_, _ = w.Write(roleDetail)
		case r.Method == http.MethodPut && r.URL.Path == "/roles/"+roleID:
			putCalled = true
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			putAssignments = toSlice(body["assignments"])
			w.WriteHeader(http.StatusOK)
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
	if !putCalled {
		t.Fatal("expected PUT /roles/{id} to add the missing app assignment")
	}
	found := false
	for _, item := range putAssignments {
		m, _ := item.(map[string]any)
		if m["id"] == appID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("PUT body assignments should contain app %q, got %v", appID, putAssignments)
	}
}

// TestAssignAdminRole_RoleMissing verifies that AssignAdminRole creates the
// aep-system role via POST /roles with the app assignment inline when no such
// role exists yet.
func TestAssignAdminRole_RoleMissing(t *testing.T) {
	const (
		appID    = "app-id-sys"
		rsID     = "rs-system-id"
		clientID = "aep-system-client"
	)

	appList, _ := json.Marshal([]appSummary{{ID: appID, Name: clientID, ClientID: clientID}})
	roleList, _ := json.Marshal([]map[string]any{})
	rsList, _ := json.Marshal([]map[string]any{{"id": rsID, "identifier": "system"}})

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
