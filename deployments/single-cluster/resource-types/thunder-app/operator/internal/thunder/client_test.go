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
	"strconv"
	"strings"
	"sync"
	"testing"
)

// -- fake Thunder server --------------------------------------------------
//
// A minimal in-memory stand-in for Thunder's admin REST API, speaking the
// camelCase wire shape the deployed Thunder 0.34.0 accepts — mirrored from
// services/aep-api/internal/clients/thundersvc/client.go (live-E2E-validated)
// and deployments/single-cluster/values-thunder.yaml's 59-aep-oauth-apps.sh.
// It records every request so tests can assert on exact bodies observed by
// the client, independent of client.go's internals. The key assertions are
// exact-case: they MUST fail if snake_case keys ever reappear on the wire.

type fakeApp struct {
	id     string
	name   string
	desc   string
	ouID   string
	config map[string]any // inboundAuthConfig[0].config, mutable
}

type fakeThunder struct {
	mu     sync.Mutex
	apps   []*fakeApp
	nextID int

	tokenCalls  int
	createCalls int
	getCalls    int
	putCalls    int
	deleteCalls int
	listCalls   int

	lastCreateBody map[string]any
	lastPutBody    map[string]any

	srv *httptest.Server
}

func newFakeThunder(t *testing.T) *fakeThunder {
	t.Helper()
	f := &fakeThunder{}
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", f.handleToken)
	mux.HandleFunc("/organization-units/tree/default", f.handleOU)
	mux.HandleFunc("/applications", f.handleApplications)
	mux.HandleFunc("/applications/", f.handleApplicationByID)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeThunder) handleToken(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.tokenCalls++
	f.mu.Unlock()

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if r.PostForm.Get("grant_type") != "client_credentials" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if r.PostForm.Get("client_id") == "" || r.PostForm.Get("client_secret") == "" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": "fake-system-token",
		"expires_in":   3600,
	})
}

func (f *fakeThunder) handleOU(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"id": "ou-default"})
}

func (f *fakeThunder) handleApplications(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		f.handleList(w, r)
	case http.MethodPost:
		f.handleCreate(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (f *fakeThunder) handleList(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++

	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 100
	}

	entries := make([]map[string]any, 0)
	for i := offset; i < len(f.apps) && i < offset+limit; i++ {
		a := f.apps[i]
		entries = append(entries, map[string]any{
			"id":       a.id,
			"name":     a.name,
			"clientId": a.config["clientId"],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"totalResults": len(f.apps),
		"applications": entries,
	})
}

func (f *fakeThunder) handleCreate(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	f.lastCreateBody = body

	f.nextID++
	id := fmt.Sprintf("app-id-%d", f.nextID)

	cfg, _ := firstOAuthConfig(body)
	a := &fakeApp{
		id:     id,
		name:   asString(body["name"]),
		desc:   asString(body["description"]),
		ouID:   asString(body["ouId"]),
		config: cfg,
	}
	f.apps = append(f.apps, a)
	writeJSON(w, http.StatusCreated, a.render())
}

func (f *fakeThunder) handleApplicationByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/applications/")
	if id == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		f.mu.Lock()
		f.getCalls++
		a := f.findByID(id)
		f.mu.Unlock()
		if a == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, a.render())
	case http.MethodPut:
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		a := f.findByID(id)
		if a == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		f.putCalls++
		f.lastPutBody = body
		cfg, _ := firstOAuthConfig(body)
		a.name = asString(body["name"])
		a.desc = asString(body["description"])
		a.config = cfg
		writeJSON(w, http.StatusOK, a.render())
	case http.MethodDelete:
		f.mu.Lock()
		defer f.mu.Unlock()
		f.deleteCalls++
		idx := -1
		for i, a := range f.apps {
			if a.id == id {
				idx = i
				break
			}
		}
		if idx == -1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		f.apps = append(f.apps[:idx], f.apps[idx+1:]...)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (f *fakeThunder) findByID(id string) *fakeApp {
	for _, a := range f.apps {
		if a.id == id {
			return a
		}
	}
	return nil
}

func (a *fakeApp) render() map[string]any {
	return map[string]any{
		"id":          a.id,
		"name":        a.name,
		"description": a.desc,
		"ouId":        a.ouID,
		"inboundAuthConfig": []map[string]any{
			{"type": "oauth2", "config": a.config},
		},
	}
}

func firstOAuthConfig(body map[string]any) (map[string]any, bool) {
	list, ok := body["inboundAuthConfig"].([]any)
	if !ok || len(list) == 0 {
		return nil, false
	}
	entry, ok := list[0].(map[string]any)
	if !ok {
		return nil, false
	}
	cfg, ok := entry["config"].(map[string]any)
	return cfg, ok
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// -- helper: seed an app directly into the fake store (bypassing the client) --

func (f *fakeThunder) seedApp(clientID string, redirectURIs []string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := fmt.Sprintf("app-id-%d", f.nextID)
	uris := make([]any, 0, len(redirectURIs))
	for _, u := range redirectURIs {
		uris = append(uris, u)
	}
	f.apps = append(f.apps, &fakeApp{
		id:   id,
		name: "Seeded App",
		ouID: "ou-default",
		config: map[string]any{
			"clientId":                clientID,
			"redirectUris":            uris,
			"grantTypes":              []any{"authorization_code"},
			"responseTypes":           []any{"code"},
			"tokenEndpointAuthMethod": "none",
			"pkceRequired":            true,
			"publicClient":            true,
		},
	})
	return id
}

// -- tests ------------------------------------------------------------------

func newTestClient(f *fakeThunder) AdminClient {
	return New(Config{
		BaseURL:      f.srv.URL,
		ClientID:     "test-system-client",
		ClientSecret: "test-system-secret",
		HTTPClient:   f.srv.Client(),
	})
}

// (a) EnsureApplication on an empty store creates the app: a POST is
// observed carrying publicClient/pkceRequired/tokenEndpointAuthMethod
// plus the desired redirect URIs, and the assigned clientId is returned.
// Key assertions are exact-case camelCase — a snake_case regression fails.
func TestEnsureApplication_CreatesWhenAbsent(t *testing.T) {
	f := newFakeThunder(t)
	c := newTestClient(f)

	app := DesiredApp{
		Name:         "aep-default-my-app",
		DisplayName:  "My App",
		Scopes:       []string{"openid", "profile"},
		RedirectURIs: []string{"https://my-app.example.com/callback"},
	}

	gotClientID, err := c.EnsureApplication(context.Background(), app)
	if err != nil {
		t.Fatalf("EnsureApplication: %v", err)
	}
	if gotClientID != app.Name {
		t.Errorf("clientID = %q, want %q (clientId is deterministic = DesiredApp.Name)", gotClientID, app.Name)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createCalls != 1 {
		t.Fatalf("createCalls = %d, want 1", f.createCalls)
	}
	cfg, ok := firstOAuthConfig(f.lastCreateBody)
	if !ok {
		t.Fatalf("POST body missing inboundAuthConfig[0].config (camelCase key required): %#v", f.lastCreateBody)
	}
	if cfg["publicClient"] != true {
		t.Errorf("publicClient = %v, want true", cfg["publicClient"])
	}
	if cfg["pkceRequired"] != true {
		t.Errorf("pkceRequired = %v, want true", cfg["pkceRequired"])
	}
	if cfg["tokenEndpointAuthMethod"] != "none" {
		t.Errorf("tokenEndpointAuthMethod = %v, want %q", cfg["tokenEndpointAuthMethod"], "none")
	}
	if cfg["clientId"] != app.Name {
		t.Errorf("config.clientId = %v, want %q", cfg["clientId"], app.Name)
	}
	uris, _ := cfg["redirectUris"].([]any)
	if len(uris) != 1 || uris[0] != "https://my-app.example.com/callback" {
		t.Errorf("redirectUris = %v, want [https://my-app.example.com/callback] — real URIs must be sent verbatim, never the placeholder", uris)
	}
	grants, _ := cfg["grantTypes"].([]any)
	if len(grants) != 2 || grants[0] != "authorization_code" || grants[1] != "refresh_token" {
		t.Errorf("grantTypes = %v, want [authorization_code refresh_token] (exact thundersvc createSPAApp parity — dropping refresh_token silently changes SPA token renewal)", grants)
	}
	if got := asString(f.lastCreateBody["name"]); got != app.Name {
		t.Errorf("POST body name = %q, want %q (Thunder app name = DesiredApp.Name)", got, app.Name)
	}
	if _, present := f.lastCreateBody["description"]; present {
		t.Errorf("POST body carries %q — thundersvc's create functions don't send it and Thunder's schema tolerance is unverified", "description")
	}
	if v := asString(f.lastCreateBody["ouId"]); v == "" {
		t.Errorf("POST body ouId = %v, want the resolved default OU id (camelCase key)", f.lastCreateBody["ouId"])
	}
	// Guard against a snake_case regression: none of the old keys may appear.
	for _, stale := range []string{"public_client", "pkce_required", "token_endpoint_auth_method", "client_id", "redirect_uris"} {
		if _, present := cfg[stale]; present {
			t.Errorf("POST body carries snake_case key %q — Thunder 0.34.0 speaks camelCase (thundersvc parity)", stale)
		}
	}
	for _, stale := range []string{"inbound_auth_config", "ou_id"} {
		if _, present := f.lastCreateBody[stale]; present {
			t.Errorf("POST body carries snake_case key %q — Thunder 0.34.0 speaks camelCase (thundersvc parity)", stale)
		}
	}
	// NOTE: no wire-level "scopes"/"scope" assertion here — Thunder 0.34.0's
	// application inboundAuthConfig has no per-app scope allowlist field
	// (see DesiredApp.Scopes doc comment in client.go). Asserting one would
	// mean inventing a field Thunder doesn't accept.
}

// (b) EnsureApplication against an already-existing app with stale
// redirectUris issues GET + PUT (no POST); the PUT body carries EXACTLY the
// desired redirect URIs (replace, not merge — the CR is the source of
// truth), and the existing client_id is returned unchanged.
func TestEnsureApplication_UpdatesExistingReplacesRedirectURIs(t *testing.T) {
	f := newFakeThunder(t)
	c := newTestClient(f)

	const name = "aep-default-existing-app"
	f.seedApp(name, []string{"https://stale.example.com/callback"})

	app := DesiredApp{
		Name:         name,
		DisplayName:  "Existing App",
		RedirectURIs: []string{"https://fresh.example.com/callback"},
	}

	gotClientID, err := c.EnsureApplication(context.Background(), app)
	if err != nil {
		t.Fatalf("EnsureApplication: %v", err)
	}
	if gotClientID != name {
		t.Errorf("clientID = %q, want %q (existing app's clientId)", gotClientID, name)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createCalls != 0 {
		t.Errorf("createCalls = %d, want 0 (must not re-create an existing app)", f.createCalls)
	}
	if f.getCalls == 0 {
		t.Errorf("getCalls = 0, want >=1 (read-modify-write requires a GET)")
	}
	if f.putCalls != 1 {
		t.Fatalf("putCalls = %d, want 1", f.putCalls)
	}
	cfg, ok := firstOAuthConfig(f.lastPutBody)
	if !ok {
		t.Fatalf("PUT body missing inboundAuthConfig[0].config (camelCase key required): %#v", f.lastPutBody)
	}
	uris, _ := cfg["redirectUris"].([]any)
	if len(uris) != 1 || uris[0] != "https://fresh.example.com/callback" {
		t.Errorf("PUT redirectUris = %v, want EXACTLY [https://fresh.example.com/callback] (replace, not union with the stale value; no placeholder when real URIs exist)", uris)
	}
	if _, present := cfg["redirect_uris"]; present {
		t.Errorf("PUT body carries snake_case key %q — want redirectUris", "redirect_uris")
	}
}

// Create with NO desired redirect URIs must still succeed: Thunder rejects
// an empty redirectUris list on authorization_code apps (APP-1024), but the
// app has to exist before its consumer's URL is known, so the wire carries
// exactly the unroutable placeholder instead.
func TestEnsureApplication_EmptyRedirectURIsSendsPlaceholderOnCreate(t *testing.T) {
	f := newFakeThunder(t)
	c := newTestClient(f)

	app := DesiredApp{Name: "aep-default-pre-deploy-app"}

	gotClientID, err := c.EnsureApplication(context.Background(), app)
	if err != nil {
		t.Fatalf("EnsureApplication: %v", err)
	}
	if gotClientID != app.Name {
		t.Errorf("clientID = %q, want %q (wire adaptation must not leak into return values)", gotClientID, app.Name)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createCalls != 1 {
		t.Fatalf("createCalls = %d, want 1", f.createCalls)
	}
	cfg, ok := firstOAuthConfig(f.lastCreateBody)
	if !ok {
		t.Fatalf("POST body missing inboundAuthConfig[0].config: %#v", f.lastCreateBody)
	}
	uris, _ := cfg["redirectUris"].([]any)
	if len(uris) != 1 || uris[0] != placeholderRedirectURI {
		t.Errorf("POST redirectUris = %v, want exactly [%s] (empty desired set → placeholder, or Thunder rejects with APP-1024)", uris, placeholderRedirectURI)
	}
}

// Update from real URIs down to an empty desired set must PUT the
// placeholder, not an empty list (same APP-1024 constraint on update).
func TestEnsureApplication_EmptyRedirectURIsSendsPlaceholderOnUpdate(t *testing.T) {
	f := newFakeThunder(t)
	c := newTestClient(f)

	const name = "aep-default-shrinking-app"
	f.seedApp(name, []string{"https://real.example.com/callback"})

	gotClientID, err := c.EnsureApplication(context.Background(), DesiredApp{Name: name})
	if err != nil {
		t.Fatalf("EnsureApplication: %v", err)
	}
	if gotClientID != name {
		t.Errorf("clientID = %q, want %q", gotClientID, name)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createCalls != 0 {
		t.Errorf("createCalls = %d, want 0", f.createCalls)
	}
	if f.putCalls != 1 {
		t.Fatalf("putCalls = %d, want 1", f.putCalls)
	}
	cfg, ok := firstOAuthConfig(f.lastPutBody)
	if !ok {
		t.Fatalf("PUT body missing inboundAuthConfig[0].config: %#v", f.lastPutBody)
	}
	uris, _ := cfg["redirectUris"].([]any)
	if len(uris) != 1 || uris[0] != placeholderRedirectURI {
		t.Errorf("PUT redirectUris = %v, want exactly [%s] (empty desired set → placeholder, or Thunder rejects with APP-1024)", uris, placeholderRedirectURI)
	}
}

// (c) DeleteApplication on a name that was never created is a no-op success
// (idempotent) — and must not attempt a DELETE call against a nonexistent id.
func TestDeleteApplication_MissingIsSuccess(t *testing.T) {
	f := newFakeThunder(t)
	c := newTestClient(f)

	if err := c.DeleteApplication(context.Background(), "aep-default-never-existed"); err != nil {
		t.Fatalf("DeleteApplication on missing app returned error: %v", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteCalls != 0 {
		t.Errorf("deleteCalls = %d, want 0 (app was never found, so no DELETE should be issued)", f.deleteCalls)
	}
}

// (d) The system token is fetched once and reused across two separate
// operations (cache with skew, not a fresh token per call).
func TestSystemToken_CachedAcrossOperations(t *testing.T) {
	f := newFakeThunder(t)
	c := newTestClient(f)
	ctx := context.Background()

	app := DesiredApp{
		Name:         "aep-default-cache-check-app",
		RedirectURIs: []string{"https://cache-check.example.com/callback"},
	}
	if _, err := c.EnsureApplication(ctx, app); err != nil {
		t.Fatalf("EnsureApplication: %v", err)
	}
	if err := c.DeleteApplication(ctx, "aep-default-some-other-missing-app"); err != nil {
		t.Fatalf("DeleteApplication: %v", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tokenCalls != 1 {
		t.Errorf("tokenCalls = %d, want 1 (token must be cached across operations)", f.tokenCalls)
	}
}
