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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fakeThunder is an in-memory Thunder /applications API that counts every
// request, so a test can assert how many reads a lookup costs. Like ThunderID
// 1.0.x, its list endpoint returns every application and honours no query
// parameter.
type fakeThunder struct {
	t   *testing.T
	srv *httptest.Server

	mu     sync.Mutex
	apps   map[string]*fakeApp // by entity id
	order  []string            // entity ids in creation order (the list order)
	calls  []string            // "METHOD /path" per request
	nextID int

	lastCreate createBody
	lastPut    map[string]any

	// racer, when set, is an application another writer registers with the
	// same clientId just before this client's create lands: the create then
	// answers 409, as a concurrent create would make it.
	racer *fakeApp
}

type fakeApp struct{ id, clientID, ouID, secret string }

// createBody is the part of a create request the tests assert on.
type createBody struct {
	OUID       string
	ClientID   string
	Type       string
	AuthMethod string
	Attributes []string
}

func newFakeThunder(t *testing.T) *fakeThunder {
	t.Helper()
	th := &fakeThunder{t: t, apps: map[string]*fakeApp{}}
	th.srv = httptest.NewServer(http.HandlerFunc(th.serve))
	t.Cleanup(th.srv.Close)
	return th
}

// app registers an existing application.
func (th *fakeThunder) app(id, clientID, ouID string) {
	th.mu.Lock()
	defer th.mu.Unlock()
	th.apps[id] = &fakeApp{id: id, clientID: clientID, ouID: ouID}
	th.order = append(th.order, id)
}

func (th *fakeThunder) count(call string) int {
	th.mu.Lock()
	defer th.mu.Unlock()
	n := 0
	for _, c := range th.calls {
		if c == call {
			n++
		}
	}
	return n
}

func (th *fakeThunder) serve(w http.ResponseWriter, r *http.Request) {
	th.mu.Lock()
	defer th.mu.Unlock()
	th.calls = append(th.calls, r.Method+" "+r.URL.Path)
	id := strings.TrimPrefix(r.URL.Path, "/applications/")

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/oauth2/token":
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "opaque-system-token", "expires_in": 3600})

	case r.Method == http.MethodGet && r.URL.Path == "/applications":
		if r.URL.RawQuery != "" {
			th.t.Errorf("list sent query %q; Thunder ignores paging, so the scan must not page", r.URL.RawQuery)
		}
		rows := []map[string]any{}
		for _, appID := range th.order {
			a := th.apps[appID]
			rows = append(rows, map[string]any{"id": a.id, "name": a.clientID, "clientId": a.clientID})
		}
		_ = json.NewEncoder(w).Encode(rows)

	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/applications/"):
		if strings.HasPrefix(id, "malformed") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		a, ok := th.apps[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(th.body(a))

	case r.Method == http.MethodPost && r.URL.Path == "/applications":
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		th.lastCreate = decodeCreate(req)
		if th.racer != nil {
			th.apps[th.racer.id] = th.racer
			th.order = append(th.order, th.racer.id)
			th.racer = nil
		}
		for _, a := range th.apps {
			if a.clientID == th.lastCreate.ClientID {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"code":"APP-1022","message":"client id already exists"}`))
				return
			}
		}
		th.nextID++
		a := &fakeApp{
			id:       fmt.Sprintf("gen-%d", th.nextID),
			clientID: th.lastCreate.ClientID,
			ouID:     th.lastCreate.OUID,
			secret:   fmt.Sprintf("created-secret-%d-x7Qp", th.nextID),
		}
		th.apps[a.id] = a
		th.order = append(th.order, a.id)
		resp := th.body(a)
		resp["inboundAuthConfig"].([]map[string]any)[0]["config"].(map[string]any)["clientSecret"] = a.secret
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)

	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/applications/"):
		a, ok := th.apps[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		th.lastPut = req
		if cfg := inboundOAuthConfig(req); cfg != nil {
			if s, _ := cfg["clientSecret"].(string); s != "" {
				a.secret = s
			}
		}
		_ = json.NewEncoder(w).Encode(req)

	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/applications/"):
		if _, ok := th.apps[id]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(th.apps, id)
		th.order = slices.DeleteFunc(th.order, func(s string) bool { return s == id })
		w.WriteHeader(http.StatusNoContent)

	default:
		th.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// body is the GET /applications/{id} shape: the oauth2 config carries the
// clientId, never the secret.
func (th *fakeThunder) body(a *fakeApp) map[string]any {
	return map[string]any{
		"id": a.id, "name": a.clientID, "ouId": a.ouID, "type": "m2m",
		"inboundAuthConfig": []map[string]any{{"type": "oauth2", "config": map[string]any{
			"clientId": a.clientID, "grantTypes": []string{"client_credentials"},
			"tokenEndpointAuthMethod": "client_secret_basic",
		}}},
	}
}

func decodeCreate(req map[string]any) createBody {
	out := createBody{}
	out.OUID, _ = req["ouId"].(string)
	out.Type, _ = req["type"].(string)
	cfg := inboundOAuthConfig(req)
	out.ClientID, _ = cfg["clientId"].(string)
	out.AuthMethod, _ = cfg["tokenEndpointAuthMethod"].(string)
	for _, a := range tokenAttributesOf(req) {
		if s, ok := a.(string); ok {
			out.Attributes = append(out.Attributes, s)
		}
	}
	return out
}

func newFakeClient(t *testing.T, th *fakeThunder) Client {
	t.Helper()
	return New(Config{BaseURL: th.srv.URL, ClientID: "sys", ClientSecret: "sec"})
}

// captureLogs routes the default slog logger into a buffer for the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestStudioAppName(t *testing.T) {
	if got := StudioAppName("default"); got != "ae-studio-default" {
		t.Fatalf("StudioAppName = %q", got)
	}
}

func TestEnsureOrgApp_ByStoredIDOneRequest(t *testing.T) {
	ctx := context.Background()
	th := newFakeThunder(t)
	th.app("app-7", "ae-studio-default", "ou-1")
	c := newFakeClient(t, th)
	got, err := c.EnsureOrgApp(ctx, OrgAppSpec{Name: "ae-studio-default", OUID: "ou-1", StoredID: "app-7"})
	if err != nil || got.EntityID != "app-7" || got.ClientID != "ae-studio-default" || got.Created || got.Secret != "" {
		t.Fatalf("%+v %v", got, err)
	}
	if th.count("GET /applications") != 0 || th.count("GET /applications/app-7") != 1 {
		t.Fatalf("calls %v", th.calls)
	}
}

func TestEnsureOrgApp_MissFallsBackToOneScan(t *testing.T) {
	ctx := context.Background()
	th := newFakeThunder(t)
	for i := 0; i < 150; i++ { // more than one 100-row page; Thunder ignores offset/limit and returns all
		th.app(fmt.Sprintf("a%d", i), fmt.Sprintf("x-%d", i), "ou-0")
	}
	th.app("app-9", "aep-publisher-default", "ou-1")
	c := newFakeClient(t, th)
	got, err := c.EnsureOrgApp(ctx, OrgAppSpec{Name: "aep-publisher-default", OUID: "ou-1", StoredID: "stale"})
	if err != nil || got.EntityID != "app-9" || got.Created || th.count("GET /applications") != 1 {
		t.Fatalf("%+v %v calls=%v", got, err, th.calls)
	}
	if th.count("POST /applications") != 0 {
		t.Fatal("a found app must not be created again")
	}
}

// A stored id that now names a different application (ids are not reused by
// Thunder, but a hand-edited row or a restored database can say anything) is
// a miss, not a match: the scan by clientId decides.
func TestEnsureOrgApp_StoredIDOfAnotherAppIsAMiss(t *testing.T) {
	ctx := context.Background()
	th := newFakeThunder(t)
	th.app("app-1", "aep-publisher-default", "ou-1")
	th.app("app-2", "ae-studio-default", "ou-1")
	got, err := newFakeClient(t, th).EnsureOrgApp(ctx, OrgAppSpec{Name: "ae-studio-default", OUID: "ou-1", StoredID: "app-1"})
	if err != nil || got.EntityID != "app-2" || th.count("GET /applications") != 1 {
		t.Fatalf("%+v %v calls=%v", got, err, th.calls)
	}
}

func TestEnsureOrgApp_CreatesWithClaimsAndNeverLogsSecret(t *testing.T) {
	ctx := context.Background()
	th := newFakeThunder(t)
	logs := captureLogs(t)
	got, err := newFakeClient(t, th).EnsureOrgApp(ctx, OrgAppSpec{Name: "ae-studio-default", OUID: "ou-1"})
	if err != nil {
		t.Fatal(err)
	}
	body := th.lastCreate
	if !got.Created || got.Secret == "" || got.EntityID == "" || got.ClientID != "ae-studio-default" ||
		body.OUID != "ou-1" || body.ClientID != "ae-studio-default" || body.Type != "m2m" ||
		!slices.Equal(body.Attributes, []string{"ouId", "ouHandle"}) || body.AuthMethod != "client_secret_basic" {
		t.Fatalf("%+v %+v", got, body)
	}
	if strings.Contains(logs.String(), got.Secret) {
		t.Fatal("secret in logs")
	}
}

// A 409 on create means another writer registered the clientId between the
// scan and the create: one more scan resolves to that application.
func TestEnsureOrgApp_CreateConflictResolvesByOneScan(t *testing.T) {
	ctx := context.Background()
	th := newFakeThunder(t)
	th.racer = &fakeApp{id: "won", clientID: "ae-studio-default", ouID: "ou-1"}
	got, err := newFakeClient(t, th).EnsureOrgApp(ctx, OrgAppSpec{Name: "ae-studio-default", OUID: "ou-1"})
	if err != nil || got.EntityID != "won" || got.Created || got.Secret != "" {
		t.Fatalf("%+v %v", got, err)
	}
	if th.count("GET /applications") != 2 || th.count("POST /applications") != 1 {
		t.Fatalf("calls %v", th.calls)
	}
}

// The studio app's token must carry the org fence, so it is never registered
// under a guessed OU.
func TestEnsureOrgApp_RequiresNameAndOU(t *testing.T) {
	ctx := context.Background()
	th := newFakeThunder(t)
	c := newFakeClient(t, th)
	for _, spec := range []OrgAppSpec{{OUID: "ou-1"}, {Name: "ae-studio-default"}} {
		if _, err := c.EnsureOrgApp(ctx, spec); err == nil {
			t.Fatalf("spec %+v: want an error", spec)
		}
	}
	if len(th.calls) != 0 {
		t.Fatalf("calls %v", th.calls)
	}
}

func TestAppExists(t *testing.T) {
	ctx := context.Background()
	th := newFakeThunder(t)
	th.app("app-7", "ae-studio-default", "ou-1")
	c := newFakeClient(t, th)

	if id, err := c.AppExists(ctx, OrgAppSpec{Name: "ae-studio-default", StoredID: "app-7"}); err != nil || id != "app-7" {
		t.Fatalf("by stored id: %q %v", id, err)
	}
	if id, err := c.AppExists(ctx, OrgAppSpec{Name: "ae-studio-default"}); err != nil || id != "app-7" {
		t.Fatalf("by scan: %q %v", id, err)
	}
	if id, err := c.AppExists(ctx, OrgAppSpec{Name: "ae-studio-other", StoredID: "gone"}); err != nil || id != "" {
		t.Fatalf("absent: %q %v", id, err)
	}
	if th.count("POST /applications") != 0 {
		t.Fatal("AppExists must never create")
	}
}

func TestSetAppSecret_PutsCallerSecret(t *testing.T) {
	ctx := context.Background()
	th := newFakeThunder(t)
	th.app("app-7", "ae-studio-default", "ou-1")
	logs := captureLogs(t)
	if err := newFakeClient(t, th).SetAppSecret(ctx, "app-7", "s2-caller-chosen"); err != nil {
		t.Fatal(err)
	}
	if th.count("GET /applications/app-7") != 1 || th.count("PUT /applications/app-7") != 1 {
		t.Fatalf("calls %v", th.calls)
	}
	if _, hasID := th.lastPut["id"]; hasID {
		t.Fatal("PUT body must not carry id")
	}
	if got, _ := inboundOAuthConfig(th.lastPut)["clientSecret"].(string); got != "s2-caller-chosen" {
		t.Fatalf("PUT clientSecret = %q", got)
	}
	if th.apps["app-7"].secret != "s2-caller-chosen" {
		t.Fatal("Thunder must hold the caller's secret")
	}
	if strings.Contains(logs.String(), "s2-caller-chosen") {
		t.Fatal("secret in logs")
	}
}

func TestSetAppSecret_MissingAppIsAnError(t *testing.T) {
	ctx := context.Background()
	th := newFakeThunder(t)
	err := newFakeClient(t, th).SetAppSecret(ctx, "gone", "s2-caller-chosen")
	if err == nil || strings.Contains(err.Error(), "s2-caller-chosen") {
		t.Fatalf("err = %v", err)
	}
	if th.count("PUT /applications/gone") != 0 {
		t.Fatal("no PUT for an absent app")
	}
}

// The publisher path reads its app by the stored id too: no list at all when
// the id is still good, and the entity id comes back for the caller to keep.
func TestEnsurePublisherApp_ByStoredIDNoList(t *testing.T) {
	ctx := context.Background()
	th := newFakeThunder(t)
	th.app("pub-1", "aep-publisher-default", "ou-1")
	got, err := newFakeClient(t, th).EnsurePublisherApp(ctx, "default", "ou-1", "pub-1")
	if err != nil || got.EntityID != "pub-1" || got.ClientID != "aep-publisher-default" || got.Created {
		t.Fatalf("%+v %v", got, err)
	}
	if th.count("GET /applications") != 0 {
		t.Fatalf("calls %v", th.calls)
	}
}

func TestEnsurePublisherApp_CreateReturnsEntityID(t *testing.T) {
	ctx := context.Background()
	th := newFakeThunder(t)
	got, err := newFakeClient(t, th).EnsurePublisherApp(ctx, "default", "ou-1", "")
	if err != nil || !got.Created || got.EntityID != "gen-1" || got.Secret == "" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestDeleteAndRegenerate_UseStoredID(t *testing.T) {
	ctx := context.Background()
	th := newFakeThunder(t)
	th.app("pub-1", "aep-publisher-default", "ou-1")
	c := newFakeClient(t, th)
	if _, err := c.RegenerateClientSecret(ctx, "default", "pub-1"); err != nil {
		t.Fatal(err)
	}
	if deleted, err := c.DeletePublisherApp(ctx, "default", "pub-1"); err != nil || !deleted {
		t.Fatalf("delete: %v %v", deleted, err)
	}
	if th.count("GET /applications") != 0 {
		t.Fatalf("calls %v", th.calls)
	}
	if deleted, err := c.DeletePublisherApp(ctx, "default", "pub-1"); err != nil || deleted {
		t.Fatalf("second delete: %v %v", deleted, err)
	}
}

// An app with the studio clientId under another OU (squatted, leftover, the
// org's OU changed) is not the org's: its token would name another org. Both
// lookups refuse it, nothing is created and nothing is rewritten.
func TestEnsureOrgApp_ForeignOUIsRefused(t *testing.T) {
	ctx := context.Background()
	for _, stored := range []string{"app-9", ""} { // by stored id, by scan
		th := newFakeThunder(t)
		th.app("app-9", "ae-studio-default", "ou-9")
		c := newFakeClient(t, th)
		spec := OrgAppSpec{Name: "ae-studio-default", OUID: "ou-1", StoredID: stored}
		if got, err := c.EnsureOrgApp(ctx, spec); !errors.Is(err, ErrAppInForeignOU) || got.EntityID != "" {
			t.Fatalf("stored=%q EnsureOrgApp = %+v, %v; want ErrAppInForeignOU", stored, got, err)
		}
		if id, err := c.AppExists(ctx, spec); !errors.Is(err, ErrAppInForeignOU) || id != "" {
			t.Fatalf("stored=%q AppExists = %q, %v; want ErrAppInForeignOU", stored, id, err)
		}
		if th.count("POST /applications") != 0 || th.count("PUT /applications/app-9") != 0 || th.count("DELETE /applications/app-9") != 0 {
			t.Fatalf("stored=%q: a foreign-OU app must not be created over, rewritten or deleted: %v", stored, th.calls)
		}
	}
}

// A 409 whose winner sits under another OU is refused the same way.
func TestEnsureOrgApp_CreateConflictWithForeignOUIsRefused(t *testing.T) {
	th := newFakeThunder(t)
	th.racer = &fakeApp{id: "won", clientID: "ae-studio-default", ouID: "ou-9"}
	_, err := newFakeClient(t, th).EnsureOrgApp(context.Background(), OrgAppSpec{Name: "ae-studio-default", OUID: "ou-1"})
	if !errors.Is(err, ErrAppInForeignOU) {
		t.Fatalf("err = %v, want ErrAppInForeignOU", err)
	}
}

// A stored id Thunder rejects as malformed (400) is a miss like a 404: the one
// scan still finds the app.
func TestEnsureOrgApp_StoredIDRejectedFallsBackToScan(t *testing.T) {
	th := newFakeThunder(t)
	th.app("app-7", "ae-studio-default", "ou-1")
	got, err := newFakeClient(t, th).EnsureOrgApp(context.Background(), OrgAppSpec{Name: "ae-studio-default", OUID: "ou-1", StoredID: "malformed-id"})
	if err != nil || got.EntityID != "app-7" || got.Created || th.count("GET /applications") != 1 {
		t.Fatalf("%+v %v calls=%v", got, err, th.calls)
	}
}
