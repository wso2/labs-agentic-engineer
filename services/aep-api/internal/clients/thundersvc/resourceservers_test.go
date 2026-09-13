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

// resourceservers_test.go — the catalog half, driven against an httptest stub
// that answers with the status codes and bodies ThunderID 1.0.0 was MEASURED to
// answer with (docs/design/draft/spikes/P1.md). Every conflict case below cites
// the code it reproduces; none of them are invented.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// -- shared assertions --------------------------------------------------------

// thunderErrorBody is the error shape every ThunderID 1.0.0 endpoint answers
// with — {code, message{key,defaultValue}, description{…}}. The stubs build
// their failures with it so the code-to-sentinel mapping is exercised through a
// real body rather than a hand-made one.
func thunderErrorBody(code, defaultValue string) string {
	b, _ := json.Marshal(map[string]any{
		"code":        code,
		"message":     map[string]string{"key": "error." + code, "defaultValue": defaultValue},
		"description": map[string]string{"key": "error." + code + "_description", "defaultValue": defaultValue},
	})
	return string(b)
}

func writeThunderError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(thunderErrorBody(code, msg)))
}

// wantSystemTokenOnEveryCall asserts the admin credential reached every request
// the client made. The token is minted once and cached, so a method that forgot
// to attach it would still "work" against a permissive stub — this is the only
// place the header is checked.
func wantSystemTokenOnEveryCall(t *testing.T, calls []directoryCall) {
	t.Helper()
	if len(calls) == 0 {
		t.Fatal("no requests recorded")
	}
	for _, c := range calls {
		if c.Auth != "Bearer tok" {
			t.Errorf("%s: Authorization = %q, want %q", c, c.Auth, "Bearer tok")
		}
	}
}

// wantSentinel asserts err maps to exactly this sentinel and carries Thunder's
// own code, and that it does NOT map to a sibling sentinel sharing its status —
// the reason the mapping keys on the code and not on 409/400.
func wantSentinel(t *testing.T, err error, sentinel error, code string, notAlso ...error) {
	t.Helper()
	if err == nil {
		t.Fatalf("want error %v, got nil", sentinel)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("error %v does not match sentinel %v", err, sentinel)
	}
	if got := ErrorCode(err); got != code {
		t.Errorf("ErrorCode = %q, want %q", got, code)
	}
	for _, other := range notAlso {
		if errors.Is(err, other) {
			t.Errorf("error %v must not also match %v", err, other)
		}
	}
}

// -- resource servers: find ---------------------------------------------------

// The identifier lookup is a client-side scan of the listing, so it has to walk
// past OTHER projects' resource servers — and past a page boundary — before it
// can answer "absent".
func TestFindResourceServerByIdentifier_ScansTheListing(t *testing.T) {
	all := make([]ResourceServer, 0, 150)
	for i := 0; i < 150; i++ {
		all = append(all, ResourceServer{
			ID:         fmt.Sprintf("rs-%d", i),
			Name:       fmt.Sprintf("project-%d", i),
			Identifier: fmt.Sprintf("https://aep.wso2.com/orgs/acme/projects/p%d", i),
			Delimiter:  ":",
		})
	}
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method != http.MethodGet || r.URL.Path != "/resource-servers" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		start, end := pageWindow(len(all), intQuery(t, r, "offset"), intQuery(t, r, "limit"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalResults": len(all), "resourceServers": all[start:end],
		})
	}}
	srv := stub.server(t)
	defer srv.Close()

	got, found, err := newTestClient(srv.URL).
		FindResourceServerByIdentifier(context.Background(), "https://aep.wso2.com/orgs/acme/projects/p120")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found || got == nil || got.ID != "rs-120" {
		t.Fatalf("found=%v server=%+v, want rs-120", found, got)
	}
	wantPagingQueries(t, stub.snapshot(), 0, 100)
	wantSystemTokenOnEveryCall(t, stub.snapshot())
}

// An identifier nobody claims is "absent", not an error — the ensure's
// find-or-create depends on that being the answer.
func TestFindResourceServerByIdentifier_Absent(t *testing.T) {
	stub := &directoryStub{handle: func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalResults": 1,
			"resourceServers": []ResourceServer{
				{ID: "rs-1", Identifier: "https://aep.wso2.com/orgs/acme/projects/other"},
			},
		})
	}}
	srv := stub.server(t)
	defer srv.Close()

	got, found, err := newTestClient(srv.URL).
		FindResourceServerByIdentifier(context.Background(), "https://aep.wso2.com/orgs/acme/projects/mine")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found || got != nil {
		t.Fatalf("found=%v server=%+v, want absent", found, got)
	}
}

// The identifier is an audience: two URIs differing only in case are two
// different audiences, so the scan must NOT fold case the way the group lookup
// does.
func TestFindResourceServerByIdentifier_IsCaseSensitive(t *testing.T) {
	stub := &directoryStub{handle: func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalResults": 1,
			"resourceServers": []ResourceServer{
				{ID: "rs-1", Identifier: "https://aep.wso2.com/orgs/acme/projects/Claims"},
			},
		})
	}}
	srv := stub.server(t)
	defer srv.Close()

	_, found, err := newTestClient(srv.URL).
		FindResourceServerByIdentifier(context.Background(), "https://aep.wso2.com/orgs/acme/projects/claims")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("a differently-cased identifier must not match")
	}
}

// -- resource servers: create -------------------------------------------------

// Create resolves the default OU first, then POSTs with the pinned delimiter —
// immutable afterwards, and the thing that makes every derived permission read
// `resource:action`.
func TestCreateResourceServer_PinsDelimiterAndOU(t *testing.T) {
	stub := &directoryStub{ouID: "ou-7", handle: func(w http.ResponseWriter, r *http.Request, body []byte) {
		if r.Method != http.MethodPost || r.URL.Path != "/resource-servers" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		var in map[string]any
		_ = json.Unmarshal(body, &in)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "rs-1", "name": in["name"], "identifier": in["identifier"],
			"type": in["type"], "ouId": in["ouId"], "delimiter": in["delimiter"],
			"isReadOnly": false,
		})
	}}
	srv := stub.server(t)
	defer srv.Close()

	got, err := newTestClient(srv.URL).CreateResourceServer(context.Background(),
		"acme/claims", "https://aep.wso2.com/orgs/acme/projects/claims")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantSeq(t, stub.seq(), "GET /organization-units/tree/default", "POST /resource-servers")

	var sent map[string]any
	decodeCallBody(t, stub.only(t, http.MethodPost, "/resource-servers"), &sent)
	for k, want := range map[string]string{
		"name":       "acme/claims",
		"identifier": "https://aep.wso2.com/orgs/acme/projects/claims",
		"delimiter":  ":",
		"type":       "API",
		"ouId":       "ou-7",
	} {
		if got, _ := sent[k].(string); got != want {
			t.Errorf("create body %s = %q, want %q", k, got, want)
		}
	}
	if got.ID != "rs-1" || got.Delimiter != ":" {
		t.Errorf("created = %+v, want id rs-1 with delimiter \":\"", got)
	}
	wantSystemTokenOnEveryCall(t, stub.snapshot())
}

// P1 §1: a second resource server claiming a taken identifier is 409 RES-1013.
// A taken NAME is also a 409 (RES-1004) — the caller cannot tell them apart
// from the status, which is why the sentinel keys on Thunder's code.
func TestCreateResourceServer_ConflictCodesAreDistinguished(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		message  string
		sentinel error
		notAlso  error
	}{
		{"duplicate identifier", "RES-1013", "Identifier conflict", ErrIdentifierConflict, ErrNameConflict},
		{"duplicate name", "RES-1004", "Name conflict", ErrNameConflict, ErrIdentifierConflict},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := &directoryStub{handle: func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeThunderError(w, http.StatusConflict, tc.code, tc.message)
			}}
			srv := stub.server(t)
			defer srv.Close()

			_, err := newTestClient(srv.URL).CreateResourceServer(context.Background(),
				"acme/claims", "https://aep.wso2.com/orgs/acme/projects/claims")
			wantSentinel(t, err, tc.sentinel, tc.code, tc.notAlso)
			if !strings.Contains(err.Error(), tc.message) {
				t.Errorf("error %q should carry Thunder's message %q", err, tc.message)
			}
		})
	}
}

// -- resources and actions ----------------------------------------------------

func TestCreateResourceAndAction_PostHandleUnderTheRightParent(t *testing.T) {
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, body []byte) {
		var in map[string]any
		_ = json.Unmarshal(body, &in)
		handle, _ := in["handle"].(string)
		w.WriteHeader(http.StatusCreated)
		switch r.URL.Path {
		case "/resource-servers/rs-1/resources":
			_ = json.NewEncoder(w).Encode(Resource{
				ID: "res-1", Name: in["name"].(string), Handle: handle, Permission: handle,
			})
		case "/resource-servers/rs-1/resources/res-1/actions":
			_ = json.NewEncoder(w).Encode(Action{
				ID: "act-1", Name: in["name"].(string), Handle: handle, Permission: "claims:" + handle,
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}}
	srv := stub.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	res, err := c.CreateResource(context.Background(), "rs-1", "claims", "Claims", "claim records")
	if err != nil {
		t.Fatalf("CreateResource: %v", err)
	}
	if res.ID != "res-1" || res.Permission != "claims" {
		t.Errorf("resource = %+v, want the derived permission read back", res)
	}
	act, err := c.CreateAction(context.Background(), "rs-1", "res-1", "read", "Read", "read a claim")
	if err != nil {
		t.Fatalf("CreateAction: %v", err)
	}
	// The permission is DERIVED by Thunder from the handle hierarchy — the
	// client reads it back, it never composes it.
	if act.Permission != "claims:read" {
		t.Errorf("action permission = %q, want the server-derived \"claims:read\"", act.Permission)
	}

	var sent map[string]any
	decodeCallBody(t, stub.only(t, http.MethodPost, "/resource-servers/rs-1/resources/res-1/actions"), &sent)
	for k, want := range map[string]string{"handle": "read", "name": "Read", "description": "read a claim"} {
		if got, _ := sent[k].(string); got != want {
			t.Errorf("action body %s = %q, want %q", k, got, want)
		}
	}
	wantSystemTokenOnEveryCall(t, stub.snapshot())
}

// A description correction is a PUT of the mutable halves alone. The handle is
// deliberately absent from both bodies: Thunder fixes it at creation, and a
// client that resent it would be asking for a rename the directory would have
// to refuse — while the alternative to this verb is a delete that cascades the
// permission out of every role granting it.
func TestUpdateResourceAndAction_PutNameAndDescriptionOnly(t *testing.T) {
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, body []byte) {
		var in map[string]any
		_ = json.Unmarshal(body, &in)
		if r.Method != http.MethodPut {
			t.Errorf("unexpected %s %s, want a PUT", r.Method, r.URL.Path)
		}
		switch r.URL.Path {
		case "/resource-servers/rs-1/resources/res-1":
			_ = json.NewEncoder(w).Encode(Resource{
				ID: "res-1", Name: in["name"].(string), Handle: "claims",
				Description: in["description"].(string), Permission: "claims",
			})
		case "/resource-servers/rs-1/resources/res-1/actions/act-1":
			_ = json.NewEncoder(w).Encode(Action{
				ID: "act-1", Name: in["name"].(string), Handle: "read",
				Description: in["description"].(string), Permission: "claims:read",
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}}
	srv := stub.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	res, err := c.UpdateResource(context.Background(), "rs-1", "res-1", "claims", "Expense claims")
	if err != nil {
		t.Fatalf("UpdateResource: %v", err)
	}
	if res.Handle != "claims" || res.Description != "Expense claims" {
		t.Errorf("resource = %+v, want the handle untouched and the new description", res)
	}
	act, err := c.UpdateAction(context.Background(), "rs-1", "res-1", "act-1", "read", "See own claims")
	if err != nil {
		t.Fatalf("UpdateAction: %v", err)
	}
	if act.Permission != "claims:read" || act.Description != "See own claims" {
		t.Errorf("action = %+v, want the derived permission and the new description", act)
	}

	for _, tc := range []struct{ path, name, description string }{
		{"/resource-servers/rs-1/resources/res-1", "claims", "Expense claims"},
		{"/resource-servers/rs-1/resources/res-1/actions/act-1", "read", "See own claims"},
	} {
		var sent map[string]any
		decodeCallBody(t, stub.only(t, http.MethodPut, tc.path), &sent)
		if got, _ := sent["name"].(string); got != tc.name {
			t.Errorf("%s body name = %q, want %q", tc.path, got, tc.name)
		}
		if got, _ := sent["description"].(string); got != tc.description {
			t.Errorf("%s body description = %q, want %q", tc.path, got, tc.description)
		}
		if _, present := sent["handle"]; present {
			t.Errorf("%s body carries a handle; the handle is immutable and must not be sent", tc.path)
		}
	}
	wantSystemTokenOnEveryCall(t, stub.snapshot())
}

// P1 §1: handle uniqueness is per PARENT resource — `read` under `claims` and
// `read` under `reports` coexist; a second `read` under `claims` is 409
// RES-1014. The client has no opinion about which is which; it reports the
// conflict the parent it POSTed to raised.
func TestCreateAction_DuplicateHandleUnderTheSameParent(t *testing.T) {
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.URL.Path == "/resource-servers/rs-1/resources/res-claims/actions" {
			writeThunderError(w, http.StatusConflict, "RES-1014", "Handle conflict")
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Action{ID: "act-9", Handle: "read", Permission: "reports:read"})
	}}
	srv := stub.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	_, err := c.CreateAction(context.Background(), "rs-1", "res-claims", "read", "Read", "")
	wantSentinel(t, err, ErrHandleConflict, "RES-1014", ErrIdentifierConflict)

	// Same handle, different parent: not a conflict.
	act, err := c.CreateAction(context.Background(), "rs-1", "res-reports", "read", "Read", "")
	if err != nil {
		t.Fatalf("the same handle under another resource must be accepted: %v", err)
	}
	if act.Permission != "reports:read" {
		t.Errorf("action = %+v, want reports:read", act)
	}
}

func TestListResourcesAndActions_Page(t *testing.T) {
	resources := []Resource{
		{ID: "res-1", Handle: "claims", Permission: "claims"},
		{ID: "res-2", Handle: "reports", Permission: "reports"},
	}
	actions := []Action{
		{ID: "act-1", Handle: "read", Permission: "claims:read"},
		{ID: "act-2", Handle: "submit", Permission: "claims:submit"},
	}
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch r.URL.Path {
		case "/resource-servers/rs-1/resources":
			start, end := pageWindow(len(resources), intQuery(t, r, "offset"), intQuery(t, r, "limit"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totalResults": len(resources), "resources": resources[start:end],
			})
		case "/resource-servers/rs-1/resources/res-1/actions":
			start, end := pageWindow(len(actions), intQuery(t, r, "offset"), intQuery(t, r, "limit"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totalResults": len(actions), "actions": actions[start:end],
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}}
	srv := stub.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	gotRes, err := c.ListResources(context.Background(), "rs-1")
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(gotRes) != 2 || gotRes[1].Handle != "reports" {
		t.Errorf("resources = %+v", gotRes)
	}
	gotAct, err := c.ListActions(context.Background(), "rs-1", "res-1")
	if err != nil {
		t.Fatalf("ListActions: %v", err)
	}
	if len(gotAct) != 2 || gotAct[0].Permission != "claims:read" {
		t.Errorf("actions = %+v", gotAct)
	}
	wantSystemTokenOnEveryCall(t, stub.snapshot())
}

// -- delete -------------------------------------------------------------------

// P1 §7: deleting a resource server that still has a resource tree is 400
// RES-1006, and so is deleting a resource that still has actions. The status is
// a 400, which says nothing on its own — the caller needs the code, and the
// message has to survive into the error for whoever reads the build log.
func TestDelete_HasDependencies(t *testing.T) {
	tests := []struct {
		name string
		call func(Client) error
		msg  string
	}{
		{"resource server with resources", func(c Client) error {
			return c.DeleteResourceServer(context.Background(), "rs-1")
		}, "Cannot delete resource server"},
		{"resource with actions", func(c Client) error {
			return c.DeleteResource(context.Background(), "rs-1", "res-1")
		}, "Cannot delete resource"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := &directoryStub{handle: func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeThunderError(w, http.StatusBadRequest, "RES-1006", tc.msg)
			}}
			srv := stub.server(t)
			defer srv.Close()

			err := tc.call(newTestClient(srv.URL))
			wantSentinel(t, err, ErrHasDependencies, "RES-1006")
			if !strings.Contains(err.Error(), tc.msg) {
				t.Errorf("error %q should carry Thunder's message %q", err, tc.msg)
			}
		})
	}
}

// Every delete is idempotent: a retried teardown, or a cascade re-run after a
// partial failure, must not fail on "already gone".
func TestDelete_AlreadyGoneIsDone(t *testing.T) {
	stub := &directoryStub{handle: func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		writeThunderError(w, http.StatusNotFound, "RES-1003", "Resource server not found")
	}}
	srv := stub.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	ctx := context.Background()
	if err := c.DeleteAction(ctx, "rs-1", "res-1", "act-1"); err != nil {
		t.Errorf("DeleteAction: %v", err)
	}
	if err := c.DeleteResource(ctx, "rs-1", "res-1"); err != nil {
		t.Errorf("DeleteResource: %v", err)
	}
	if err := c.DeleteResourceServer(ctx, "rs-1"); err != nil {
		t.Errorf("DeleteResourceServer: %v", err)
	}
}

// The cascade of record (P1 §7): EVERY action, then EVERY resource, then the
// server. The order is asserted from the stub's request log rather than from
// counts, because the ordering IS the behaviour — any other sequence answers
// 400 RES-1006 against a real Thunder.
func TestDeleteResourceServerCascade_WalksLeafFirst(t *testing.T) {
	resources := []Resource{{ID: "res-claims", Handle: "claims"}, {ID: "res-reports", Handle: "reports"}}
	actions := map[string][]Action{
		"res-claims":  {{ID: "act-read", Handle: "read"}, {ID: "act-submit", Handle: "submit"}},
		"res-reports": {{ID: "act-export", Handle: "export"}},
	}
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/resource-servers/rs-1/resources":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totalResults": len(resources), "resources": resources,
			})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/actions"):
			res := strings.Split(r.URL.Path, "/")[4]
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totalResults": len(actions[res]), "actions": actions[res],
			})
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}}
	srv := stub.server(t)
	defer srv.Close()

	if err := newTestClient(srv.URL).DeleteResourceServerCascade(context.Background(), "rs-1"); err != nil {
		t.Fatalf("cascade: %v", err)
	}
	wantSeq(t, stub.seq(),
		"GET /resource-servers/rs-1/resources",
		"GET /resource-servers/rs-1/resources/res-claims/actions",
		"DELETE /resource-servers/rs-1/resources/res-claims/actions/act-read",
		"DELETE /resource-servers/rs-1/resources/res-claims/actions/act-submit",
		"GET /resource-servers/rs-1/resources/res-reports/actions",
		"DELETE /resource-servers/rs-1/resources/res-reports/actions/act-export",
		"DELETE /resource-servers/rs-1/resources/res-claims",
		"DELETE /resource-servers/rs-1/resources/res-reports",
		"DELETE /resource-servers/rs-1",
	)
	wantSystemTokenOnEveryCall(t, stub.snapshot())
}

// A dependency error mid-walk stops the cascade and names the object, rather
// than blundering on to a delete that is certain to fail for the same reason.
func TestDeleteResourceServerCascade_SurfacesTheFailingObject(t *testing.T) {
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/resource-servers/rs-1/resources":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totalResults": 1, "resources": []Resource{{ID: "res-claims", Handle: "claims"}},
			})
		case r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"totalResults": 0, "actions": []Action{}})
		default:
			// A nested child nothing here created: the resource still has a
			// dependency the walk did not reach.
			writeThunderError(w, http.StatusBadRequest, "RES-1006", "Cannot delete resource")
		}
	}}
	srv := stub.server(t)
	defer srv.Close()

	err := newTestClient(srv.URL).DeleteResourceServerCascade(context.Background(), "rs-1")
	wantSentinel(t, err, ErrHasDependencies, "RES-1006")
	if !strings.Contains(err.Error(), "claims") {
		t.Errorf("error %q should name the resource it failed on", err)
	}
	for _, c := range stub.snapshot() {
		if c.Method == http.MethodDelete && c.Path == "/resource-servers/rs-1" {
			t.Error("the resource server must not be deleted after a failed child delete")
		}
	}
}

// -- token plumbing -----------------------------------------------------------

// The catalog surface mints its token the same way the rest of the client does:
// client_secret_post credentials, scope=system, and the System resource server
// as the `resource` indicator — without which ThunderID drops the scope
// silently and every call here 403s with nothing saying why.
func TestCatalogCalls_MintWithTheResourceIndicator(t *testing.T) {
	var tokenForm url.Values
	// assertSystemScope refuses a token that does not carry scope=system, so the
	// stub mints the real shape rather than an opaque string.
	token := unsignedJWT(t, map[string]any{"scope": "system"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			_ = r.ParseForm()
			tokenForm = r.PostForm
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": token, "expires_in": 3600,
			})
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("%s %s: Authorization = %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"totalResults": 0, "resourceServers": []ResourceServer{}})
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, ClientID: "sys", ClientSecret: "sec",
		SystemResourceIdentifier: "http://idp.example/mcp"})
	if _, _, err := c.FindResourceServerByIdentifier(context.Background(), "https://x"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for k, want := range map[string]string{
		"grant_type": "client_credentials", "scope": "system",
		"client_id": "sys", "client_secret": "sec", "resource": "http://idp.example/mcp",
	} {
		if got := tokenForm.Get(k); got != want {
			t.Errorf("token form %s = %q, want %q", k, got, want)
		}
	}
}
