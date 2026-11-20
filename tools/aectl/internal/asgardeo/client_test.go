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

package asgardeo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const (
	testAdminID     = "admin-app"
	testAdminSecret = "admin-secret-value"
	testConsoleURL  = "http://console.ae.localhost:8080"
	allScopes       = scopeView + " " + scopeCreate + " " + ScopeRegenerateSecret
)

// fakeApp is an application as fakeOrg stores it.
type fakeApp struct {
	secret string
	oidc   map[string]any
	full   map[string]any
}

// fakeOrg is an Asgardeo organization's token endpoint and management API,
// with the behaviour the client depends on: a chosen secret is honoured at
// creation, never returned, and replaced only by regenerate-secret, which
// refuses a POST without a length.
type fakeOrg struct {
	t      *testing.T
	mu     sync.Mutex
	scope  string
	apps   map[string]*fakeApp // by application id
	nextID int
	// noLocation makes a create answer without a Location header.
	noLocation bool
}

func newFakeOrg(t *testing.T) (*fakeOrg, *Client) {
	t.Helper()
	org := &fakeOrg{t: t, scope: allScopes, apps: map[string]*fakeApp{}}
	return org, org.client(t)
}

// client is a Client of org with the admin credential.
func (o *fakeOrg) client(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewTLSServer(o)
	t.Cleanup(srv.Close)
	c, err := New(context.Background(), Config{
		APIURL: srv.URL + "/api/server/v1/", TokenURL: srv.URL + "/oauth2/token",
		AdminClientID: testAdminID, AdminClientSecret: testAdminSecret, HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func (o *fakeOrg) byClientID(clientID string) (string, *fakeApp) {
	for id, app := range o.apps {
		if app.oidc["clientId"] == clientID {
			return id, app
		}
	}
	return "", nil
}

func (o *fakeOrg) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	writeJSON := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	const api = "/api/server/v1/applications"
	switch {
	case r.URL.Path == "/oauth2/token":
		id, secret, _ := r.BasicAuth()
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "client_credentials" {
			writeJSON(http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
			return
		}
		if id == testAdminID && secret == testAdminSecret {
			writeJSON(http.StatusOK, map[string]string{"access_token": "management-token", "scope": o.scope})
			return
		}
		if _, app := o.byClientID(id); app != nil && app.secret != "" && app.secret == secret {
			writeJSON(http.StatusOK, map[string]string{"access_token": "a-token"})
			return
		}
		writeJSON(http.StatusUnauthorized, map[string]string{"error": "invalid_client", "error_description": "Client credentials are invalid."})
	case r.Header.Get("Authorization") != "Bearer management-token":
		writeJSON(http.StatusUnauthorized, map[string]string{"code": "401", "message": "Unauthorized"})
	case r.URL.Path == api && r.Method == http.MethodGet:
		clientID := strings.TrimPrefix(r.URL.Query().Get("filter"), "clientId eq ")
		list := []map[string]string{}
		for id, app := range o.apps {
			if app.oidc["clientId"] == clientID {
				list = append(list, map[string]string{"id": id})
			}
		}
		writeJSON(http.StatusOK, map[string]any{"totalResults": len(list), "applications": list})
	case r.URL.Path == api && r.Method == http.MethodPost:
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(http.StatusBadRequest, map[string]string{"code": "APP-60001", "message": "Invalid request"})
			return
		}
		oidc := payload["inboundProtocolConfiguration"].(map[string]any)["oidc"].(map[string]any)
		secret, _ := oidc["clientSecret"].(string)
		delete(oidc, "clientSecret")
		o.nextID++
		id := fmt.Sprintf("app-%d", o.nextID)
		o.apps[id] = &fakeApp{secret: secret, oidc: oidc, full: payload}
		if !o.noLocation {
			w.Header().Set("Location", "https://"+r.Host+api+"/"+id)
		}
		w.WriteHeader(http.StatusCreated)
	case strings.HasSuffix(r.URL.Path, "/inbound-protocols/oidc/regenerate-secret") && r.Method == http.MethodPost:
		app := o.apps[strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, api+"/"), "/inbound-protocols/oidc/regenerate-secret")]
		if app == nil {
			writeJSON(http.StatusNotFound, map[string]string{"code": "APP-60006", "message": "Application not found"})
			return
		}
		if r.Header.Get("Content-Length") != "0" {
			w.WriteHeader(http.StatusLengthRequired)
			return
		}
		app.secret = fmt.Sprintf("regenerated-%d-of-%s", len(app.secret), app.oidc["clientId"])
		answer := map[string]any{"clientSecret": app.secret}
		for k, v := range app.oidc {
			answer[k] = v
		}
		writeJSON(http.StatusOK, answer)
	case strings.HasSuffix(r.URL.Path, "/inbound-protocols/oidc") && r.Method == http.MethodGet:
		app := o.apps[strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, api+"/"), "/inbound-protocols/oidc")]
		if app == nil {
			writeJSON(http.StatusNotFound, map[string]string{"code": "APP-60006", "message": "Application not found"})
			return
		}
		writeJSON(http.StatusOK, app.oidc)
	case strings.HasPrefix(r.URL.Path, api+"/") && r.Method == http.MethodGet:
		app := o.apps[strings.TrimPrefix(r.URL.Path, api+"/")]
		if app == nil {
			writeJSON(http.StatusNotFound, map[string]string{"code": "APP-60006", "message": "Application not found"})
			return
		}
		writeJSON(http.StatusOK, app.full)
	default:
		o.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotImplemented)
	}
}

// New refuses a token without the two scopes every run needs, names the
// scope, and never repeats the credential.
func TestNew_RequiresTheViewAndCreateScopes(t *testing.T) {
	for _, missing := range []string{scopeView, scopeCreate} {
		org := &fakeOrg{t: t, apps: map[string]*fakeApp{}, scope: strings.TrimSpace(strings.Replace(allScopes, missing, "", 1))}
		srv := httptest.NewTLSServer(org)
		_, err := New(context.Background(), Config{
			APIURL: srv.URL + "/api/server/v1", TokenURL: srv.URL + "/oauth2/token",
			AdminClientID: testAdminID, AdminClientSecret: testAdminSecret, HTTPClient: srv.Client(),
		})
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), missing) {
			t.Errorf("without %s: err = %v, want one naming the scope", missing, err)
		}
	}
}

// The regenerate scope is optional: a token without it still gives a client,
// which reports that it cannot regenerate and refuses to try.
func TestNew_TheRegenerateScopeIsOptional(t *testing.T) {
	org := &fakeOrg{t: t, apps: map[string]*fakeApp{}, scope: scopeView + " " + scopeCreate}
	c := org.client(t)
	if c.CanRegenerateSecrets() {
		t.Fatal("CanRegenerateSecrets is true without the scope")
	}
	if _, err := c.RegenerateSecret(context.Background(), "app-1"); err == nil || !strings.Contains(err.Error(), ScopeRegenerateSecret) {
		t.Fatalf("err = %v, want one naming the scope", err)
	}
}

func TestNew_RefusesAWrongCredentialWithoutRepeatingIt(t *testing.T) {
	org := &fakeOrg{t: t, apps: map[string]*fakeApp{}, scope: allScopes}
	srv := httptest.NewTLSServer(org)
	defer srv.Close()
	_, err := New(context.Background(), Config{
		APIURL: srv.URL + "/api/server/v1", TokenURL: srv.URL + "/oauth2/token",
		AdminClientID: testAdminID, AdminClientSecret: "a-wrong-secret", HTTPClient: srv.Client(),
	})
	if err == nil || !strings.Contains(err.Error(), "invalid_client") {
		t.Fatalf("err = %v, want the token endpoint's error", err)
	}
	if strings.Contains(err.Error(), "a-wrong-secret") {
		t.Fatalf("the error repeats the secret: %v", err)
	}
}

// The management API is sent the token and the token endpoint the
// credential: neither is reached over plain http.
func TestNew_RefusesPlainHTTP(t *testing.T) {
	for _, cfg := range []Config{
		{APIURL: "http://api.asgardeo.io/t/acme/api/server/v1", TokenURL: "https://api.asgardeo.io/t/acme/oauth2/token"},
		{APIURL: "https://api.asgardeo.io/t/acme/api/server/v1", TokenURL: "http://api.asgardeo.io/t/acme/oauth2/token"},
		{APIURL: "", TokenURL: "https://api.asgardeo.io/t/acme/oauth2/token"},
	} {
		cfg.AdminClientID, cfg.AdminClientSecret = testAdminID, testAdminSecret
		if _, err := New(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "https") {
			t.Errorf("%+v: err = %v, want a refusal that says https", cfg.APIURL, err)
		}
	}
}

func TestFind(t *testing.T) {
	org, c := newFakeOrg(t)
	ctx := context.Background()
	if id, err := c.Find(ctx, "aep-api-client"); err != nil || id != "" {
		t.Fatalf("no application: id %q, err %v; want none", id, err)
	}
	created, err := c.CreateMachine(ctx, "aep-api-client", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if id, err := c.Find(ctx, "aep-api-client"); err != nil || id != created {
		t.Fatalf("Find = %q, %v; want %q", id, err, created)
	}
	// Two applications with one client id are never guessed between.
	org.apps["twin"] = &fakeApp{oidc: map[string]any{"clientId": "aep-api-client"}}
	if _, err := c.Find(ctx, "aep-api-client"); err == nil {
		t.Fatal("two applications with the client id: want an error")
	}
}

// A machine application is created with the chosen secret and with the
// settings AE needs, so its own check finds nothing wrong. The secret works
// at the token endpoint and another one does not.
func TestCreateMachine(t *testing.T) {
	org, c := newFakeOrg(t)
	ctx := context.Background()
	id, err := c.CreateMachine(ctx, "ae-studio-default", "the-chosen-secret")
	if err != nil {
		t.Fatal(err)
	}
	app := org.apps[id]
	if app == nil || app.secret != "the-chosen-secret" {
		t.Fatalf("application %q was not created with the chosen secret", id)
	}
	if app.full["templateId"] != machineTemplate || app.full["name"] != "ae-studio-default" {
		t.Errorf("template %v, name %v", app.full["templateId"], app.full["name"])
	}
	problems, err := c.Problems(ctx, App{ClientID: "ae-studio-default", Kind: Machine}, id)
	if err != nil || len(problems) != 0 {
		t.Fatalf("Problems = %v, %v; want none", problems, err)
	}
	if ok, err := c.SecretWorks(ctx, "ae-studio-default", "the-chosen-secret"); err != nil || !ok {
		t.Fatalf("SecretWorks with the chosen secret = %v, %v", ok, err)
	}
	if ok, err := c.SecretWorks(ctx, "ae-studio-default", "another-secret"); err != nil || ok {
		t.Fatalf("SecretWorks with another secret = %v, %v; want false", ok, err)
	}
	if _, err := c.CreateMachine(ctx, "no-secret", ""); err == nil {
		t.Fatal("a machine application without a secret: want an error")
	}
}

// A create answer without a Location header still yields the id.
func TestCreate_WithoutALocationHeaderLooksTheApplicationUp(t *testing.T) {
	org, c := newFakeOrg(t)
	org.noLocation = true
	id, err := c.CreateMachine(context.Background(), "aep-api-client", "s1")
	if err != nil || org.apps[id] == nil {
		t.Fatalf("id %q, err %v; want the created application's id", id, err)
	}
}

// The console's application is a public client with PKCE, both redirect
// addresses in one escaped entry, the console's origin, and the token
// attributes and claims AE reads.
func TestCreateBrowser(t *testing.T) {
	org, c := newFakeOrg(t)
	ctx := context.Background()
	id, err := c.CreateBrowser(ctx, "aep-console-client", testConsoleURL+"/")
	if err != nil {
		t.Fatal(err)
	}
	oidc := org.apps[id].oidc
	want := `regexp=(http://console\.ae\.localhost:8080|http://console\.ae\.localhost:8080/callback)`
	if got := oidc["callbackURLs"].([]any); len(got) != 1 || got[0] != want {
		t.Errorf("callbackURLs = %v, want [%s]", got, want)
	}
	if got := oidc["allowedOrigins"].([]any); len(got) != 1 || got[0] != testConsoleURL {
		t.Errorf("allowedOrigins = %v", got)
	}
	if oidc["publicClient"] != true {
		t.Error("not a public client")
	}
	if _, has := oidc["clientSecret"]; has || org.apps[id].secret != "" {
		t.Error("the public client was given a secret")
	}
	problems, err := c.Problems(ctx, App{ClientID: "aep-console-client", Kind: Browser, ConsoleURL: testConsoleURL}, id)
	if err != nil || len(problems) != 0 {
		t.Fatalf("Problems = %v, %v; want none", problems, err)
	}
}

func TestCreateBrowser_RefusesAConsoleURLThatWouldChangeTheExpression(t *testing.T) {
	_, c := newFakeOrg(t)
	for _, u := range []string{"console.example.com", "ftp://console.example.com", "https://console.example.com/(a|b)", "https://console example.com", `https://console.example.com\x`} {
		if _, err := c.CreateBrowser(context.Background(), "aep-console-client", u); err == nil {
			t.Errorf("console URL %q: want an error", u)
		}
	}
}

// Problems names every setting that is not what AE needs, for each kind.
func TestProblems_ListsEachWrongSetting(t *testing.T) {
	org, c := newFakeOrg(t)
	ctx := context.Background()

	org.apps["m"] = &fakeApp{oidc: map[string]any{
		"clientId": "someone-else", "grantTypes": []string{"password"}, "accessToken": map[string]any{"type": "Default"},
	}}
	problems, err := c.Problems(ctx, App{ClientID: "aep-api-client", Kind: Machine}, "m")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"client id is someone-else", "access token type is Default, not JWT", "the client_credentials grant is off"} {
		if !containsLine(problems, want) {
			t.Errorf("machine: problems %v lack %q", problems, want)
		}
	}

	org.apps["b"] = &fakeApp{
		oidc: map[string]any{
			"clientId": "aep-console-client", "grantTypes": []string{"authorization_code"}, "publicClient": false,
			"pkce": map[string]any{"mandatory": false}, "callbackURLs": []string{testConsoleURL + "/callback"},
			"allowedOrigins": []string{"https://elsewhere.example.com"},
			"accessToken":    map[string]any{"type": "JWT", "accessTokenAttributes": []string{"email", "groups"}},
		},
		full: map[string]any{"claimConfiguration": map[string]any{"requestedClaims": []map[string]any{
			{"claim": map[string]any{"uri": "http://wso2.org/claims/emailaddress"}},
		}}},
	}
	problems, err = c.Problems(ctx, App{ClientID: "aep-console-client", Kind: Browser, ConsoleURL: testConsoleURL}, "b")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"it is not a public client", "PKCE is not mandatory", "the refresh_token grant is off",
		"the sign-out redirect URL " + testConsoleURL + " is not registered",
		"the origin " + testConsoleURL + " is not allowed",
		"the access token lacks the attribute username",
		"the application may not read the claim http://wso2.org/claims/groups",
	} {
		if !containsLine(problems, want) {
			t.Errorf("browser: problems %v lack %q", problems, want)
		}
	}
	// The sign-in address is a plain entry: it is registered.
	for _, p := range problems {
		if strings.Contains(p, "sign-in") {
			t.Errorf("the sign-in redirect is reported: %s", p)
		}
	}
}

func containsLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

// An address is registered as a plain entry or as an escaped alternative. An
// alternative with unescaped metacharacters also matches look-alike hosts and
// is reported as such.
func TestRedirectProblem(t *testing.T) {
	const addr = "https://console.example.com/callback"
	for name, tc := range map[string]struct {
		entries []string
		want    string
	}{
		"a plain entry":           {[]string{addr}, ""},
		"an escaped alternative":  {[]string{`regexp=(https://console\.example\.com|https://console\.example\.com/callback)`}, ""},
		"an unescaped expression": {[]string{"regexp=(https://console.example.com|https://console.example.com/callback)"}, "unescaped"},
		"not there":               {[]string{"https://other.example.com/callback"}, "is not registered"},
		"no entries":              {nil, "is not registered"},
	} {
		got := redirectProblem(tc.entries, "sign-in", addr)
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q, want it to contain %q", name, got, tc.want)
		}
	}
}

// Regenerating returns the secret Asgardeo chose. The old one stops working,
// and the request carries a length of 0, which Asgardeo requires.
func TestRegenerateSecret(t *testing.T) {
	_, c := newFakeOrg(t)
	ctx := context.Background()
	id, err := c.CreateMachine(ctx, "aep-publisher-default", "the-old-secret")
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := c.RegenerateSecret(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if fresh == "" || fresh == "the-old-secret" {
		t.Fatalf("the regenerated secret is %q", fresh)
	}
	if ok, err := c.SecretWorks(ctx, "aep-publisher-default", fresh); err != nil || !ok {
		t.Fatalf("SecretWorks with the new secret = %v, %v", ok, err)
	}
	if ok, err := c.SecretWorks(ctx, "aep-publisher-default", "the-old-secret"); err != nil || ok {
		t.Fatalf("SecretWorks with the old secret = %v, %v; want false", ok, err)
	}
	if _, err := c.RegenerateSecret(ctx, "no-such-app"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("an unknown application: err = %v, want HTTP 404", err)
	}
}

// An API error is reported by its code, message and description. Nothing
// else of the body reaches the error, so a body that held a secret could not
// leak one.
func TestErrors_NeverCarryTheResponseBody(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			_, _ = io.WriteString(w, `{"access_token":"t","scope":"`+allScopes+`"}`)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"code":"APP-60001","message":"Invalid input","description":"A field is wrong","clientSecret":"leaked-secret"}`)
	}))
	defer srv.Close()
	c, err := New(context.Background(), Config{
		APIURL: srv.URL, TokenURL: srv.URL + "/oauth2/token",
		AdminClientID: testAdminID, AdminClientSecret: testAdminSecret, HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateMachine(context.Background(), "aep-api-client", "sent-secret")
	if err == nil || !strings.Contains(err.Error(), "APP-60001 | Invalid input | A field is wrong") {
		t.Fatalf("err = %v, want the code, message and description", err)
	}
	for _, secret := range []string{"leaked-secret", "sent-secret", testAdminSecret} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("the error holds %q: %v", secret, err)
		}
	}
}

// A field of an error answer is cut: the server chooses its length.
func TestErrors_CutALongField(t *testing.T) {
	long := strings.Repeat("x", 5000)
	got := apiErrorSuffix([]byte(`{"code":"APP-1","message":"` + long + `"}`))
	if len([]rune(got)) > maxErrorFieldChars+20 || !strings.Contains(got, "APP-1") || !strings.HasSuffix(got, "…") {
		t.Fatalf("suffix of %d characters, want the code and a cut message", len([]rune(got)))
	}
}

// The default client follows no redirect: a redirect would carry the token
// to an address that was never checked. The redirect's status is what the
// caller gets.
func TestDefaultHTTPClient_FollowsNoRedirect(t *testing.T) {
	var followed bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed = true }))
	defer target.Close()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer srv.Close()

	hc := defaultHTTPClient()
	hc.Transport = srv.Client().Transport // trusts the test server's certificate
	resp, err := hc.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound || followed {
		t.Fatalf("status %d, followed %v; want 302 and no request to the target", resp.StatusCode, followed)
	}
}
