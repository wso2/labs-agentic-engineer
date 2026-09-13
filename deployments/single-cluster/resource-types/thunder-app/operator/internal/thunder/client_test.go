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
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// -- fake Thunder server --------------------------------------------------
//
// A minimal in-memory stand-in for Thunder's admin REST API, speaking the
// camelCase wire shape the deployed ThunderID accepts — mirrored from
// services/aep-api/internal/clients/thundersvc/client.go (live-E2E-validated)
// and the deployments/single-cluster/thunder-resources/ bootstrap documents.
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

	// The server-wide `cors` config, split the way ThunderID splits it:
	// readOnly comes from bootstrap documents, writable from PUT, and the
	// server enforces the union.
	corsReadOnly []string
	corsWritable []string
	corsGetCalls int
	corsPutCalls int
	corsPutErr   int // when non-zero, PUT answers with this status

	// dropTokenAttrs makes the fake discard the identity attributes on BOTH
	// tokens — a stand-in for the next ThunderID contract change.
	dropTokenAttrs bool

	// dropScopes makes the fake swallow the scope allowlist the way it swallows
	// an unrecognised token shape: 200, and the field is simply not there on the
	// read-back.
	dropScopes bool

	// normaliseScopes makes the fake rewrite the stored allowlist the way
	// ThunderID normalises scopeClaims on write (P1 §4: `email_verified` is
	// dropped from scopeClaims.email). Here it appends a server-side extra, so
	// the read-back is a SUPERSET of what was sent — which must not be read as
	// a failed write.
	normaliseScopes bool

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
	mux.HandleFunc("/server-config/cors", f.handleCORS)
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

// handleCORS mirrors ThunderID's /server-config/cors: GET returns all three
// layers, PUT replaces the writable one and returns the new state. A PUT that
// carries `null` is rejected the way the real server rejects it.
func (f *fakeThunder) handleCORS(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch r.Method {
	case http.MethodGet:
		f.corsGetCalls++
	case http.MethodPut:
		f.corsPutCalls++
		if f.corsPutErr != 0 {
			w.WriteHeader(f.corsPutErr)
			return
		}
		var body struct {
			AllowedOrigins *[]string `json:"allowedOrigins"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if body.AllowedOrigins == nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"message": "cors: allowedOrigins must be a list, not null",
			})
			return
		}
		f.corsWritable = *body.AllowedOrigins
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	merged := append(append([]string{}, f.corsReadOnly...), f.corsWritable...)
	writeJSON(w, http.StatusOK, map[string]any{
		"readOnly": map[string]any{"allowedOrigins": f.corsReadOnly},
		"writable": map[string]any{"allowedOrigins": f.corsWritable},
		"merged":   map[string]any{"allowedOrigins": merged},
	})
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

// storeTokenConfig models ThunderID 1.0.0's handling of `token`: it accepts any
// shape with 200 and keeps only what it recognises, dropping the rest without a
// word. Measured against the live server on 2026-09-06:
//
//	idToken.validityPeriod            kept
//	idToken.userAttributes            kept
//	accessToken.userConfig.validityPeriod  kept
//	accessToken.userConfig.attributes      kept
//	accessToken.validityPeriod        DROPPED (replaced by the server default)
//	accessToken.userAttributes        DROPPED
//	accessToken.userConfig.userAttributes  DROPPED
//	accessToken.userConfig.claims          DROPPED
//
// A fake that stored the payload verbatim would agree with whatever this
// package believes and could never catch a wire-shape mistake — which is
// exactly how the access-token attributes went missing unnoticed.
func storeTokenConfig(cfg map[string]any, drop bool) {
	token, ok := cfg["token"].(map[string]any)
	if !ok {
		return
	}
	kept := map[string]any{}
	if id, ok := token["idToken"].(map[string]any); ok {
		idKept := map[string]any{}
		if v, ok := id["validityPeriod"]; ok {
			idKept["validityPeriod"] = v
		}
		if v, ok := id["userAttributes"]; ok && !drop {
			idKept["userAttributes"] = v
		}
		kept["idToken"] = idKept
	}
	if access, ok := token["accessToken"].(map[string]any); ok {
		// Whatever the caller put at the top level is discarded; only
		// userConfig survives, and only its two known keys.
		accessKept := map[string]any{}
		userCfg := map[string]any{"validityPeriod": float64(3600)}
		if uc, ok := access["userConfig"].(map[string]any); ok {
			if v, ok := uc["validityPeriod"]; ok {
				userCfg["validityPeriod"] = v
			}
			if v, ok := uc["attributes"]; ok && !drop {
				userCfg["attributes"] = v
			}
		}
		accessKept["userConfig"] = userCfg
		if cc, ok := access["clientConfig"].(map[string]any); ok {
			accessKept["clientConfig"] = cc
		}
		kept["accessToken"] = accessKept
	}
	cfg["token"] = kept
}

// storeScopes models what ThunderID does to `inboundAuthConfig[oauth2].config
// .scopes`: it stores the array verbatim (measured on 1.0.0, P1 §4) — unless a
// test asks it to misbehave the two ways that matter. A fake that only ever
// echoed the payload back could not tell a stored allowlist from a discarded
// one, which is the thing verifyWritten exists to catch.
func (f *fakeThunder) storeScopes(cfg map[string]any) {
	if cfg == nil {
		return
	}
	if f.dropScopes {
		delete(cfg, "scopes")
		return
	}
	if f.normaliseScopes {
		if have, ok := cfg["scopes"].([]any); ok {
			cfg["scopes"] = append(append([]any{}, have...), "openid")
		}
	}
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
	storeTokenConfig(cfg, f.dropTokenAttrs)
	f.storeScopes(cfg)
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
		storeTokenConfig(cfg, f.dropTokenAttrs)
		f.storeScopes(cfg)
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

// asNumber reads a JSON number out of a decoded request body. encoding/json
// gives every number back as float64, so an int comparison against a body the
// fake observed would never match even when the value is right.
func asNumber(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return -1
	}
}

// anySliceHas reports whether a JSON-decoded array ([]any) contains want.
func anySliceHas(v any, want string) bool {
	arr, _ := v.([]any)
	for _, e := range arr {
		if s, _ := e.(string); s == want {
			return true
		}
	}
	return false
}

// assertIdentityClaimContract verifies the platform identity-claim contract a
// provisioned OAuth app must carry: `groups` in BOTH tokens' userAttributes,
// the `group → [groups]` scopeClaim (what releases groups into the id_token),
// and allowedUserTypes=[Person]. Shared by the create + update (backfill) tests.
func assertIdentityClaimContract(t *testing.T, body map[string]any) {
	t.Helper()
	if !anySliceHas(body["allowedUserTypes"], "Person") {
		t.Errorf("allowedUserTypes = %v, want to include %q", body["allowedUserTypes"], "Person")
	}
	cfg, ok := firstOAuthConfig(body)
	if !ok {
		t.Fatalf("body missing inboundAuthConfig[0].config: %#v", body)
	}
	tok, _ := cfg["token"].(map[string]any)
	if tok == nil {
		t.Fatalf("config.token missing — end-user tokens would carry no attributes: %#v", cfg)
	}

	// The ID token declares its attributes at the top level...
	id, _ := tok["idToken"].(map[string]any)
	if !anySliceHas(id["userAttributes"], "groups") {
		t.Errorf("token.idToken.userAttributes = %v, want to include %q (the SPA reads its role from the id_token)",
			id["userAttributes"], "groups")
	}
	// A long-lived token keeps a returning SPA user signed in across a
	// refresh/new tab; dropping validityPeriod reverts to Thunder's short
	// default and reintroduces the every-visit re-login.
	if id["validityPeriod"] == nil {
		t.Error("token.idToken.validityPeriod missing — SPA session would expire on Thunder's short default")
	}

	// ...and the ACCESS token nests them under userConfig, with the key
	// `attributes`. This asymmetry is ThunderID's, not ours (see
	// tokenClaimConfig): the id-token shape on an access token is accepted
	// with 200 and silently dropped, and the app then 403s on every
	// authorated API call while its login still works.
	access, _ := tok["accessToken"].(map[string]any)
	userCfg, _ := access["userConfig"].(map[string]any)
	if !anySliceHas(userCfg["attributes"], "groups") {
		t.Errorf("token.accessToken.userConfig.attributes = %v, want to include %q "+
			"(the gateway maps this claim to X-User-Groups, which is what the API authorizes on)",
			userCfg["attributes"], "groups")
	}
	if userCfg["validityPeriod"] == nil {
		t.Error("token.accessToken.userConfig.validityPeriod missing — the server would apply its short default")
	}
	// Regression guard: the shape that looks right and does nothing.
	for _, dead := range []string{"userAttributes", "validityPeriod"} {
		if _, present := access[dead]; present {
			t.Errorf("token.accessToken.%s is set — ThunderID ignores it at that path; it belongs under userConfig", dead)
		}
	}

	sc, _ := cfg["scopeClaims"].(map[string]any)
	if !anySliceHas(sc["group"], "groups") {
		t.Errorf("scopeClaims.group = %v, want [groups] — without it, the group scope releases no groups claim", sc["group"])
	}
}

// assertStoredIdentityClaims asserts what the SERVER kept, which is the only
// thing that matters: the outgoing body can be perfect and still leave an app
// with no claims if the shape is one ThunderID does not recognise.
func assertStoredIdentityClaims(t *testing.T, f *fakeThunder, name string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.apps {
		// Matched on the OAuth client id, which is stable across an update;
		// the display name is not part of the PUT body.
		if asString(a.config["clientId"]) != name {
			continue
		}
		if got := idTokenAttributes(a.config); !containsString(got, "groups") {
			t.Errorf("stored id token attributes = %v, want to include groups", got)
		}
		if got := accessTokenAttributes(a.config); !containsString(got, "groups") {
			t.Errorf("stored ACCESS token attributes = %v, want to include groups — "+
				"this is the claim the gateway maps to X-User-Groups", got)
		}
		return
	}
	t.Fatalf("no application with clientId %q on the fake server", name)
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
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

// seedScopes gives an already-seeded app a stored scope allowlist, so a test
// can ask what an update does to one it was told nothing about.
func (f *fakeThunder) seedScopes(id string, scopes []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := f.findByID(id)
	if a == nil {
		return
	}
	stored := make([]any, 0, len(scopes))
	for _, sc := range scopes {
		stored = append(stored, sc)
	}
	a.config["scopes"] = stored
}

// storedScopes reads the allowlist the fake SERVER kept for an app.
func (f *fakeThunder) storedScopes(t *testing.T, clientID string) []string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.apps {
		if asString(a.config["clientId"]) == clientID {
			return stringsOf(a.config["scopes"])
		}
	}
	t.Fatalf("no application with clientId %q on the fake server", clientID)
	return nil
}

// assertWireScopes checks that a create/update body carries the allowlist as a
// JSON array of exactly the desired strings, in order.
func assertWireScopes(t *testing.T, body map[string]any, want []string) {
	t.Helper()
	cfg, ok := firstOAuthConfig(body)
	if !ok {
		t.Fatalf("body missing inboundAuthConfig[0].config: %#v", body)
	}
	raw, isArray := cfg["scopes"].([]any)
	if !isArray {
		t.Fatalf("config.scopes = %#v (%T), want a JSON array of strings — "+
			"the space-joined CRT form must be split before the wire", cfg["scopes"], cfg["scopes"])
	}
	got := stringsOf(raw)
	if len(got) != len(raw) {
		t.Errorf("config.scopes = %#v, want every element to be a string", raw)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("config.scopes = %v, want %v", got, want)
	}
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
	// The allowlist rides on the create as a JSON ARRAY of strings — the CRT
	// parameter is space-joined, ThunderID's contract is a list, and sending the
	// joined form would register one scope literally named "openid profile".
	assertWireScopes(t, f.lastCreateBody, app.Scopes)

	// The identity-claim contract must ride on every created app so end-user
	// tokens carry `groups` (role) + `ou*` (org).
	assertIdentityClaimContract(t, f.lastCreateBody)
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
		Scopes:       []string{"openid", "claims:read"},
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
	// Backfill: an app created before the identity-claim contract existed must
	// self-heal on update — the PUT re-asserts the full contract, not just URIs.
	assertIdentityClaimContract(t, f.lastPutBody)
	assertWireScopes(t, f.lastPutBody, app.Scopes)
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

// -- browser origins (CORS) -------------------------------------------------

// origins the fake server currently enforces, sorted.
func (f *fakeThunder) writable() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	got := append([]string(nil), f.corsWritable...)
	sort.Strings(got)
	return got
}

// SetBrowserOrigins writes exactly the given set into the WRITABLE layer and
// leaves the bootstrap-declared readOnly layer alone. That separation is the
// whole safety argument for touching CORS at runtime: the platform-composed
// `cors` document is a singleton whose redeclaration replaces the entire
// allow-list.
func TestSetBrowserOrigins_WritesTheWritableLayerAndLeavesBootstrapAlone(t *testing.T) {
	f := newFakeThunder(t)
	f.corsReadOnly = []string{"http://localhost:8090"}
	c := newTestClient(f)

	if err := c.SetBrowserOrigins(context.Background(), []string{
		"https://b.example.com", "https://a.example.com",
	}); err != nil {
		t.Fatalf("SetBrowserOrigins: %v", err)
	}

	want := []string{"https://a.example.com", "https://b.example.com"}
	if got := f.writable(); !reflect.DeepEqual(got, want) {
		t.Errorf("writable allowedOrigins = %#v, want %#v", got, want)
	}
	f.mu.Lock()
	readOnly := append([]string(nil), f.corsReadOnly...)
	f.mu.Unlock()
	if !reflect.DeepEqual(readOnly, []string{"http://localhost:8090"}) {
		t.Errorf("readOnly layer = %#v — the bootstrap document must never be touched", readOnly)
	}
}

// Replace, not merge: an origin the CRs no longer name goes away.
func TestSetBrowserOrigins_ReplacesRatherThanAccumulates(t *testing.T) {
	f := newFakeThunder(t)
	f.corsWritable = []string{"https://stale.example.com"}
	c := newTestClient(f)

	if err := c.SetBrowserOrigins(context.Background(), []string{"https://current.example.com"}); err != nil {
		t.Fatalf("SetBrowserOrigins: %v", err)
	}
	if got := f.writable(); !reflect.DeepEqual(got, []string{"https://current.example.com"}) {
		t.Errorf("writable allowedOrigins = %#v, want only the current one", got)
	}
}

// An unchanged set costs a GET and no PUT, so a steady-state reconcile loop
// does not rewrite the server's config on every pass.
func TestSetBrowserOrigins_SkipsThePUTWhenNothingChanged(t *testing.T) {
	f := newFakeThunder(t)
	c := newTestClient(f)
	ctx := context.Background()

	if err := c.SetBrowserOrigins(ctx, []string{"https://a.example.com"}); err != nil {
		t.Fatalf("first SetBrowserOrigins: %v", err)
	}
	// Same set, different order — the list is a set, order carries no meaning.
	if err := c.SetBrowserOrigins(ctx, []string{"https://a.example.com", "https://a.example.com"}); err != nil {
		t.Fatalf("second SetBrowserOrigins: %v", err)
	}

	f.mu.Lock()
	puts := f.corsPutCalls
	f.mu.Unlock()
	if puts != 1 {
		t.Errorf("PUT called %d times, want 1 — an unchanged set must not be rewritten", puts)
	}
}

// The last app leaving an instance must reach the EMPTY list. A nil slice
// marshals to `null`, which ThunderID rejects outright.
func TestSetBrowserOrigins_EmptySetSendsAListNotNull(t *testing.T) {
	f := newFakeThunder(t)
	f.corsWritable = []string{"https://going.example.com"}
	c := newTestClient(f)

	if err := c.SetBrowserOrigins(context.Background(), nil); err != nil {
		t.Fatalf("SetBrowserOrigins(nil): %v", err)
	}
	if got := f.writable(); len(got) != 0 {
		t.Errorf("writable allowedOrigins = %#v, want empty", got)
	}
}

// A failed write is reported, not swallowed: the caller decides what a browser
// that cannot reach the IdP means for the CR.
func TestSetBrowserOrigins_SurfacesAWriteFailure(t *testing.T) {
	f := newFakeThunder(t)
	f.corsPutErr = http.StatusServiceUnavailable
	c := newTestClient(f)

	err := c.SetBrowserOrigins(context.Background(), []string{"https://a.example.com"})
	if err == nil {
		t.Fatal("SetBrowserOrigins returned nil on a 503")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error = %v, want it to carry the status", err)
	}
}

func TestOriginOf(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"http://app.localhost:19080/callback", "http://app.localhost:19080"},
		{"https://app.example.com/callback", "https://app.example.com"},
		{"https://app.example.com", "https://app.example.com"},
		{"  https://app.example.com/callback  ", "https://app.example.com"},
		{"https://app.example.com/callback?x=1#f", "https://app.example.com"},
		// Not something a browser can send an Origin for.
		{"myapp://callback", ""},
		{"/relative/callback", ""},
		{"", ""},
		// The placeholder is wire adaptation inside EnsureApplication and never
		// appears in a CR's spec, so the reconciler never asks for its origin.
		// It parses like any other https URI if it ever did, and pending.invalid
		// is unroutable by construction.
		{placeholderRedirectURI, "https://pending.invalid"},
	}
	for _, tc := range cases {
		if got := OriginOf(tc.in); got != tc.want {
			t.Errorf("OriginOf(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// -- the identity claims must SURVIVE the write ------------------------------

// A created app's claims are asserted where it counts: in what the server kept.
func TestEnsureApplication_StoresIdentityClaimsOnBothTokens(t *testing.T) {
	f := newFakeThunder(t)
	c := newTestClient(f)

	const name = "aep-default-claims-app"
	if _, err := c.EnsureApplication(context.Background(), DesiredApp{
		Name:         name,
		RedirectURIs: []string{"https://app.example.com/callback"},
	}); err != nil {
		t.Fatalf("EnsureApplication: %v", err)
	}
	assertStoredIdentityClaims(t, f, name)
}

// An update re-asserts them, so an app registered by an older operator is
// repaired rather than left half-configured.
func TestEnsureApplication_UpdateRestoresIdentityClaims(t *testing.T) {
	f := newFakeThunder(t)
	c := newTestClient(f)

	const name = "aep-default-existing-claims"
	f.seedApp(name, []string{"https://stale.example.com/callback"})

	if _, err := c.EnsureApplication(context.Background(), DesiredApp{
		Name:         name,
		RedirectURIs: []string{"https://fresh.example.com/callback"},
	}); err != nil {
		t.Fatalf("EnsureApplication: %v", err)
	}
	assertStoredIdentityClaims(t, f, name)
}

// The whole point of reading back: when the IdP drops the claims, the write
// FAILS loudly and names them, instead of returning a client_id for an
// application that will 403 every authorated call in a deployed app.
func TestEnsureApplication_FailsWhenTheIdPDropsTheIdentityClaims(t *testing.T) {
	f := newFakeThunder(t)
	f.dropTokenAttrs = true
	c := newTestClient(f)

	_, err := c.EnsureApplication(context.Background(), DesiredApp{
		Name:         "aep-default-dropped",
		RedirectURIs: []string{"https://app.example.com/callback"},
	})
	if err == nil {
		t.Fatal("EnsureApplication returned nil when the IdP stored no identity claims")
	}
	for _, want := range []string{"groups", "identity claims"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q — the message has to name what vanished", err, want)
		}
	}
}

// An m2m client has no user behind its token, so the user-attribute contract
// does not apply and must not fail its write.
func TestEnsureApplication_ConfidentialClientSkipsTheUserClaimCheck(t *testing.T) {
	f := newFakeThunder(t)
	f.dropTokenAttrs = true
	c := newTestClient(f)

	if _, err := c.EnsureApplication(context.Background(), DesiredApp{
		Name:         "aep-default-m2m",
		ClientType:   "confidential",
		ClientSecret: "s3cret",
	}); err != nil {
		t.Fatalf("confidential EnsureApplication: %v", err)
	}
}

// -- the scope allowlist must SURVIVE the write ------------------------------
//
// What these pin is TRUTHFULNESS, not enforcement. Measured on ThunderID 1.0.0
// (spike P1 §6): the field is stored and read back faithfully and has no effect
// on the authorization-code flow — narrowing it does not narrow the issued
// token. The reason to write it is that the registered application should
// describe its client honestly; the reason to read it back is that a list the
// server quietly discarded describes nothing.

// The allowlist the client asked for is what the server ends up holding.
func TestEnsureApplication_StoresTheScopeAllowlist(t *testing.T) {
	f := newFakeThunder(t)
	c := newTestClient(f)

	const name = "aep-default-scoped-app"
	want := []string{"openid", "profile", "email", "group", "ou", "claims:read", "claims:approve"}
	if _, err := c.EnsureApplication(context.Background(), DesiredApp{
		Name:         name,
		Scopes:       want,
		RedirectURIs: []string{"https://app.example.com/callback"},
	}); err != nil {
		t.Fatalf("EnsureApplication: %v", err)
	}
	if got := f.storedScopes(t, name); !reflect.DeepEqual(got, want) {
		t.Errorf("stored scopes = %v, want %v", got, want)
	}
}

// An app registered before write-through existed picks the allowlist up on its
// next reconcile, like the identity claims do.
func TestEnsureApplication_UpdateBackfillsTheScopeAllowlist(t *testing.T) {
	f := newFakeThunder(t)
	c := newTestClient(f)

	const name = "aep-default-unscoped-app"
	f.seedApp(name, []string{"https://app.example.com/callback"})

	want := []string{"openid", "reports:read"}
	if _, err := c.EnsureApplication(context.Background(), DesiredApp{
		Name:         name,
		Scopes:       want,
		RedirectURIs: []string{"https://app.example.com/callback"},
	}); err != nil {
		t.Fatalf("EnsureApplication: %v", err)
	}
	if got := f.storedScopes(t, name); !reflect.DeepEqual(got, want) {
		t.Errorf("stored scopes = %v, want %v", got, want)
	}
}

// Converge-to-desired, not merge: a handle the design dropped leaves the
// allowlist, exactly as redirect URIs do. (It changes no token — see the block
// comment above — but a record that only ever grows is not a record.)
func TestEnsureApplication_UpdateReplacesTheScopeAllowlist(t *testing.T) {
	f := newFakeThunder(t)
	c := newTestClient(f)

	const name = "aep-default-shrinking-scopes"
	id := f.seedApp(name, []string{"https://app.example.com/callback"})
	f.seedScopes(id, []string{"openid", "claims:read", "claims:retired"})

	want := []string{"openid", "claims:read"}
	if _, err := c.EnsureApplication(context.Background(), DesiredApp{
		Name:         name,
		Scopes:       want,
		RedirectURIs: []string{"https://app.example.com/callback"},
	}); err != nil {
		t.Fatalf("EnsureApplication: %v", err)
	}
	if got := f.storedScopes(t, name); !reflect.DeepEqual(got, want) {
		t.Errorf("stored scopes = %v, want EXACTLY %v (replace, not union with the retired handle)", got, want)
	}
}

// A CR that names no scopes is SILENT, not a statement that the client may
// request nothing: the operator must not narrow an allowlist it was never told
// about. (The CRT always carries a default, so this is the hand-authored CR and
// the platform-client case.)
func TestEnsureApplication_NoDesiredScopesLeavesTheStoredAllowlistAlone(t *testing.T) {
	f := newFakeThunder(t)
	c := newTestClient(f)

	const name = "aep-default-silent-scopes"
	id := f.seedApp(name, []string{"https://app.example.com/callback"})
	existing := []string{"openid", "system"}
	f.seedScopes(id, existing)

	if _, err := c.EnsureApplication(context.Background(), DesiredApp{
		Name:         name,
		RedirectURIs: []string{"https://app.example.com/callback"},
	}); err != nil {
		t.Fatalf("EnsureApplication: %v", err)
	}
	if got := f.storedScopes(t, name); !reflect.DeepEqual(got, existing) {
		t.Errorf("stored scopes = %v, want %v untouched", got, existing)
	}
	// updateApp is a read-modify-write, so the PUT does echo the stored list
	// back — what matters is that it is the list the server already had, not a
	// narrower one this reconcile invented.
	f.mu.Lock()
	defer f.mu.Unlock()
	cfg, _ := firstOAuthConfig(f.lastPutBody)
	if got := stringsOf(cfg["scopes"]); !reflect.DeepEqual(got, existing) {
		t.Errorf("PUT scopes = %v, want the stored %v echoed back untouched", got, existing)
	}
}

// The point of reading back: when the IdP swallows the allowlist, the write
// FAILS and names what vanished, rather than leaving a ThunderApplication that
// claims to be ready and is registered as something else.
func TestEnsureApplication_FailsWhenTheIdPDropsTheScopeAllowlist(t *testing.T) {
	f := newFakeThunder(t)
	f.dropScopes = true
	c := newTestClient(f)

	_, err := c.EnsureApplication(context.Background(), DesiredApp{
		Name:         "aep-default-scopes-dropped",
		Scopes:       []string{"openid", "claims:read"},
		RedirectURIs: []string{"https://app.example.com/callback"},
	})
	if err == nil {
		t.Fatal("EnsureApplication returned nil when the IdP stored no scope allowlist")
	}
	for _, want := range []string{"claims:read", "scope allowlist"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q — the message has to name what vanished", err, want)
		}
	}
}

// The diff is against the READ-BACK and is a subset check, because ThunderID
// normalises on write (P1 §4: scopeClaims.email loses email_verified). A server
// that returns MORE than was sent is having its own opinion, not failing.
func TestEnsureApplication_ToleratesAServerNormalisedReadBack(t *testing.T) {
	f := newFakeThunder(t)
	f.normaliseScopes = true
	c := newTestClient(f)

	if _, err := c.EnsureApplication(context.Background(), DesiredApp{
		Name:         "aep-default-normalised",
		Scopes:       []string{"claims:read"},
		RedirectURIs: []string{"https://app.example.com/callback"},
	}); err != nil {
		t.Fatalf("a read-back carrying more than was sent must not fail the write: %v", err)
	}
}

// An m2m client's allowlist is the one place the field is load-bearing today:
// `system` is granted through it, not through a role, and an app that loses it
// authenticates fine and then 403s on every admin call. So it is written and
// verified for confidential clients too, even though the user-claim contract
// does not apply to them.
func TestEnsureApplication_ConfidentialClientWritesAndVerifiesItsScopes(t *testing.T) {
	f := newFakeThunder(t)
	f.dropTokenAttrs = true // irrelevant to an m2m client; must not fail it
	c := newTestClient(f)

	const name = "aep-default-m2m-scoped"
	if _, err := c.EnsureApplication(context.Background(), DesiredApp{
		Name:         name,
		ClientType:   "confidential",
		ClientSecret: "s3cret",
		Scopes:       []string{"system"},
	}); err != nil {
		t.Fatalf("confidential EnsureApplication: %v", err)
	}
	if got := f.storedScopes(t, name); !reflect.DeepEqual(got, []string{"system"}) {
		t.Errorf("stored scopes = %v, want [system]", got)
	}

	f2 := newFakeThunder(t)
	f2.dropScopes = true
	if _, err := newTestClient(f2).EnsureApplication(context.Background(), DesiredApp{
		Name:         name,
		ClientType:   "confidential",
		ClientSecret: "s3cret",
		Scopes:       []string{"system"},
	}); err == nil {
		t.Fatal("a confidential client that lost `system` must fail the write, not 403 later")
	}
}

// -- access-token lifetime ---------------------------------------------------

// The CR's validityPeriod reaches the ACCESS token and nothing else. A short
// value is how a fixture app exercises the silent renew in minutes (P6 used
// 300 s); the ID token keeps the long default, because shortening the SPA's
// session is not what was asked for.
func TestEnsureApplication_ValidityPeriodSetsTheAccessTokenLifetimeOnly(t *testing.T) {
	f := newFakeThunder(t)
	c := newTestClient(f)

	if _, err := c.EnsureApplication(context.Background(), DesiredApp{
		Name:           "aep-default-short-lived",
		ValidityPeriod: 300,
		RedirectURIs:   []string{"https://app.example.com/callback"},
	}); err != nil {
		t.Fatalf("EnsureApplication: %v", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	cfg, ok := firstOAuthConfig(f.lastCreateBody)
	if !ok {
		t.Fatalf("POST body missing inboundAuthConfig[0].config: %#v", f.lastCreateBody)
	}
	tok, _ := cfg["token"].(map[string]any)
	access, _ := tok["accessToken"].(map[string]any)
	userCfg, _ := access["userConfig"].(map[string]any)
	if got := asNumber(userCfg["validityPeriod"]); got != 300 {
		t.Errorf("token.accessToken.userConfig.validityPeriod = %v, want 300", userCfg["validityPeriod"])
	}
	id, _ := tok["idToken"].(map[string]any)
	if got := asNumber(id["validityPeriod"]); got != defaultTokenValiditySeconds {
		t.Errorf("token.idToken.validityPeriod = %v, want the %d default left alone",
			id["validityPeriod"], defaultTokenValiditySeconds)
	}
}

// Zero means "the CR said nothing", which is every app that is not a
// short-lived fixture: the long default applies.
func TestEnsureApplication_NoValidityPeriodKeepsTheDefault(t *testing.T) {
	f := newFakeThunder(t)
	c := newTestClient(f)

	if _, err := c.EnsureApplication(context.Background(), DesiredApp{
		Name:         "aep-default-default-lifetime",
		RedirectURIs: []string{"https://app.example.com/callback"},
	}); err != nil {
		t.Fatalf("EnsureApplication: %v", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	cfg, _ := firstOAuthConfig(f.lastCreateBody)
	tok, _ := cfg["token"].(map[string]any)
	access, _ := tok["accessToken"].(map[string]any)
	userCfg, _ := access["userConfig"].(map[string]any)
	if got := asNumber(userCfg["validityPeriod"]); got != defaultTokenValiditySeconds {
		t.Errorf("token.accessToken.userConfig.validityPeriod = %v, want %d",
			userCfg["validityPeriod"], defaultTokenValiditySeconds)
	}
}

// An update re-asserts the lifetime too, so patching a live CR down to 300 s
// takes effect on the next reconcile rather than needing a re-create.
func TestEnsureApplication_UpdateReassertsTheAccessTokenLifetime(t *testing.T) {
	f := newFakeThunder(t)
	c := newTestClient(f)

	const name = "aep-default-relifetimed"
	f.seedApp(name, []string{"https://app.example.com/callback"})

	if _, err := c.EnsureApplication(context.Background(), DesiredApp{
		Name:           name,
		ValidityPeriod: 300,
		RedirectURIs:   []string{"https://app.example.com/callback"},
	}); err != nil {
		t.Fatalf("EnsureApplication: %v", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	cfg, _ := firstOAuthConfig(f.lastPutBody)
	tok, _ := cfg["token"].(map[string]any)
	access, _ := tok["accessToken"].(map[string]any)
	userCfg, _ := access["userConfig"].(map[string]any)
	if got := asNumber(userCfg["validityPeriod"]); got != 300 {
		t.Errorf("PUT token.accessToken.userConfig.validityPeriod = %v, want 300", userCfg["validityPeriod"])
	}
}
