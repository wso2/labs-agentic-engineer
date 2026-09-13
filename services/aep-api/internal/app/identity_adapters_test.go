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

package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
	"github.com/wso2/aep/aep-api/internal/identity"
)

// toGateCredentials is the single line the test users' logins cross on their way
// from the identity domain to the ticket that publishes them. It has no
// behaviour, which is exactly why it is worth a test: drop a field here and the
// build still succeeds, the gate still closes, and every published row reads
// "unavailable" — so a validation run signs in as nobody and reports every
// role-gated criterion `not_run`, with nothing anywhere saying why.
func TestToGateCredentialsCarriesEveryField(t *testing.T) {
	got := toGateCredentials([]identity.Credential{
		{Username: "test-team-member", Password: "Aep1!alpha", Roles: []string{"Team Member"}, Scopes: []string{"claims:read"}, ColdStart: true},
		{Username: "test-trainer", Password: "Aep1!beta", Roles: []string{"Trainer"}},
	})

	if len(got) != 2 {
		t.Fatalf("got %d credentials, want 2: %+v", len(got), got)
	}
	if got[0].Username != "test-team-member" {
		t.Errorf("username = %q", got[0].Username)
	}
	if got[0].Password != "Aep1!alpha" {
		t.Errorf("password = %q — an empty one publishes as 'unavailable'", got[0].Password)
	}
	if len(got[0].Roles) != 1 || got[0].Roles[0] != "Team Member" {
		t.Errorf("roles = %v — the agent matches a criterion's role on this column", got[0].Roles)
	}
	// The scopes column is what says which criteria this login can exercise at
	// all. Dropping it publishes a row that reads as a login with no permissions.
	if len(got[0].Scopes) != 1 || got[0].Scopes[0] != "claims:read" {
		t.Errorf("scopes = %v, want the handles the ensure resolved", got[0].Scopes)
	}
	// ColdStart is a v1 LEFTOVER: nothing sets it any more and nothing renders
	// it (the gate ticket dropped its column). The projection must still carry
	// it while the field exists, so this asserts transport and nothing about
	// behaviour — phase 5 deletes the field, this pair of assertions and the
	// cold_start column together.
	if !got[0].ColdStart {
		t.Error("coldStart was dropped by the projection")
	}
	if got[1].ColdStart {
		t.Error("coldStart was invented by the projection")
	}
	if got[1].Password != "Aep1!beta" {
		t.Errorf("second password = %q", got[1].Password)
	}
}

// Nil rather than an empty slice, so the gate's "no accounts, no table" branch
// reads the same whether the ensure returned nothing or was never asked.
func TestToGateCredentialsMapsNothingToNil(t *testing.T) {
	if got := toGateCredentials(nil); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
	if got := toGateCredentials([]identity.Credential{}); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

// ---- the authorization half of the Directory map ---------------------------
//
// Only the verbs with a DECISION in them are tested here. A verb that renames
// two fields is proved by the compiler and by the domain's own fake; what the
// compiler cannot see is the behaviour the port promises and the wire does not
// offer, and there are exactly two of those:
//
//   - `Ensure…` writes NOTHING when the directory already matches, which is what
//     makes a rebuild of an unchanged tag free. Thunder has no upsert, so this
//     is find-compare-write here and nowhere else.
//   - DeleteResourceServer is a leaf-first cascade, because Thunder refuses a
//     parent that still has children.
//
// Driven through an httptest stub so the assertions are about the REQUESTS the
// adapter makes — a write that should not have happened is visible as a POST,
// not as an unchanged in-memory struct.

// thunderStub is the smallest ThunderID admin surface these tests need: the
// token mint (not recorded — it is cached and would only add noise), the
// default-OU lookup every write resolves, and a handler the test supplies.
type thunderStub struct {
	handle func(w http.ResponseWriter, r *http.Request, body []byte)

	mu    sync.Mutex
	calls []string
}

func (s *thunderStub) directory(t *testing.T) identity.Directory {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method == http.MethodPost && r.URL.Path == "/oauth2/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 3600})
			return
		}
		s.mu.Lock()
		s.calls = append(s.calls, r.Method+" "+r.URL.Path)
		s.mu.Unlock()
		if r.Method == http.MethodGet && r.URL.Path == "/organization-units/tree/default" {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "default-ou"})
			return
		}
		s.handle(w, r, body)
	}))
	t.Cleanup(srv.Close)
	return thunderDirectory{c: thundersvc.New(thundersvc.Config{
		BaseURL: srv.URL, ClientID: "aep-system-client", ClientSecret: "s",
		SystemResourceIdentifier: srv.URL + "/mcp",
	})}
}

// writes are the recorded requests that CHANGE the directory.
func (s *thunderStub) writes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, c := range s.calls {
		if strings.HasPrefix(c, "GET ") {
			continue
		}
		out = append(out, c)
	}
	return out
}

// A role whose grants already match is not written, however the two lists are
// ORDERED. Without this, every build would PUT every role — and a PUT replaces
// the permission set, so a converge that churns is a converge that briefly
// revokes. The listing the CALLER holds carries no permissions, so the
// comparison has to reach for GET /roles/{id}; an adapter that compared against
// the listing would find an empty set every time and rewrite the role on every
// build.
func TestThunderDirectoryEnsureRoleWritesNothingWhenTheGrantsAlreadyMatch(t *testing.T) {
	stub := &thunderStub{}
	stub.handle = func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/roles":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totalResults": 1,
				"roles":        []any{map[string]any{"id": "rol-1", "name": "p1/Approver"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/roles/rol-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "rol-1", "name": "p1/Approver", "description": "Approves claims.",
				// Read back in a DIFFERENT order from what the tag declares.
				"permissions": []any{map[string]any{
					"resourceServerId": "rsv-1",
					"permissions":      []string{"claims:read-all", "claims:read"},
				}},
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
	dir := stub.directory(t)

	got, err := dir.EnsureRole(context.Background(), "rol-1", "p1/Approver", "Approves claims.", "rsv-1",
		[]string{"claims:read", "claims:read-all"})
	if err != nil {
		t.Fatalf("EnsureRole: %v", err)
	}
	if got != "rol-1" {
		t.Errorf("role id = %q, want the one already there", got)
	}
	if writes := stub.writes(); len(writes) != 0 {
		t.Errorf("EnsureRole wrote %v for a role that already matches", writes)
	}
}

// A role whose grants differ is REPLACED wholesale, in one PUT carrying the
// whole set. Thunder's update has no merge semantics, so sending a difference
// would revoke everything else; its assignments survive the call, which is what
// makes this the entire converge.
func TestThunderDirectoryEnsureRoleReplacesTheWholePermissionSetWhenItDiffers(t *testing.T) {
	stub := &thunderStub{}
	var put map[string]any
	stub.handle = func(w http.ResponseWriter, r *http.Request, body []byte) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/roles":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totalResults": 1,
				"roles":        []any{map[string]any{"id": "rol-1", "name": "p1/Approver"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/roles/rol-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "rol-1", "name": "p1/Approver", "description": "Approves claims.",
				"permissions": []any{map[string]any{
					"resourceServerId": "rsv-1", "permissions": []string{"claims:read"},
				}},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/roles/rol-1":
			_ = json.Unmarshal(body, &put)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rol-1", "name": "p1/Approver"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
	dir := stub.directory(t)

	if _, err := dir.EnsureRole(context.Background(), "rol-1", "p1/Approver", "Approves claims.", "rsv-1",
		[]string{"claims:read", "claims:read-all"}); err != nil {
		t.Fatalf("EnsureRole: %v", err)
	}
	if got := stub.writes(); len(got) != 1 || got[0] != "PUT /roles/rol-1" {
		t.Fatalf("writes = %v, want exactly the one replacement", got)
	}
	blocks, _ := put["permissions"].([]any)
	if len(blocks) != 1 {
		t.Fatalf("the update carried %d permission blocks, want the project's one resource server", len(blocks))
	}
	block, _ := blocks[0].(map[string]any)
	if block["resourceServerId"] != "rsv-1" {
		t.Errorf("resourceServerId = %v", block["resourceServerId"])
	}
	perms, _ := block["permissions"].([]any)
	if len(perms) != 2 {
		t.Errorf("the update carried %v, want the WHOLE set — a partial one revokes the rest", perms)
	}
}

// A role whose DESCRIPTION drifted is rewritten even when its grants match. The
// description is what the console shows beside the role, so leaving it stale
// would make the directory disagree with the document that is supposed to own it.
func TestThunderDirectoryEnsureRoleRewritesADriftedDescription(t *testing.T) {
	stub := &thunderStub{}
	stub.handle = func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/roles":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totalResults": 1,
				"roles":        []any{map[string]any{"id": "rol-1", "name": "p1/Approver"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/roles/rol-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "rol-1", "name": "p1/Approver", "description": "an older sentence",
				"permissions": []any{map[string]any{
					"resourceServerId": "rsv-1", "permissions": []string{"claims:read"},
				}},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/roles/rol-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rol-1"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
	dir := stub.directory(t)

	if _, err := dir.EnsureRole(context.Background(), "rol-1", "p1/Approver", "Approves claims.", "rsv-1",
		[]string{"claims:read"}); err != nil {
		t.Fatalf("EnsureRole: %v", err)
	}
	if got := stub.writes(); len(got) != 1 || got[0] != "PUT /roles/rol-1" {
		t.Errorf("writes = %v, want the description rewritten", got)
	}
}

// No id means CREATE, and the adapter must not go looking for the role first:
// the caller already listed the directory and found none. A listing here would
// be one full read per declared role for an answer it was handed.
func TestThunderDirectoryEnsureRoleCreatesWithoutRelistingWhenGivenNoID(t *testing.T) {
	stub := &thunderStub{}
	stub.handle = func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/roles":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rol-new", "name": "p1/Approver"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
	dir := stub.directory(t)

	got, err := dir.EnsureRole(context.Background(), "", "p1/Approver", "Approves claims.", "rsv-1",
		[]string{"claims:read"})
	if err != nil {
		t.Fatalf("EnsureRole: %v", err)
	}
	if got != "rol-new" {
		t.Errorf("role id = %q, want the created one", got)
	}
	if writes := stub.writes(); len(writes) != 1 || writes[0] != "POST /roles" {
		t.Errorf("writes = %v, want exactly the create", writes)
	}
}

// A grant on ANOTHER resource server makes the role differ even when the
// handles line up: the role is carrying something the design does not declare,
// and the only way this port can say so is a full replace.
func TestThunderDirectoryEnsureRoleReplacesARoleGrantingOnAnotherResourceServer(t *testing.T) {
	stub := &thunderStub{}
	stub.handle = func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/roles":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totalResults": 1,
				"roles":        []any{map[string]any{"id": "rol-1", "name": "p1/Approver"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/roles/rol-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "rol-1", "name": "p1/Approver", "description": "d",
				"permissions": []any{
					map[string]any{"resourceServerId": "rsv-1", "permissions": []string{"claims:read"}},
					map[string]any{"resourceServerId": "rsv-other", "permissions": []string{"secrets:read"}},
				},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/roles/rol-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rol-1"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
	dir := stub.directory(t)

	if _, err := dir.EnsureRole(context.Background(), "rol-1", "p1/Approver", "d", "rsv-1",
		[]string{"claims:read"}); err != nil {
		t.Fatalf("EnsureRole: %v", err)
	}
	if got := stub.writes(); len(got) != 1 {
		t.Errorf("writes = %v, want the foreign grant replaced away", got)
	}
}

// The resource server is found by IDENTIFIER and created only when absent. The
// identifier is the token audience, so a create that slipped through on a
// project that already has one would be refused by the directory — and if it
// were not, the project would have two audiences.
func TestThunderDirectoryEnsureResourceServerDoesNotCreateOneThatExists(t *testing.T) {
	const identifier = "https://aep.wso2.com/orgs/acme/projects/p1"
	stub := &thunderStub{}
	stub.handle = func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method == http.MethodGet && r.URL.Path == "/resource-servers" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totalResults": 1,
				"resourceServers": []any{
					map[string]any{"id": "rsv-1", "name": "p1", "identifier": identifier},
				},
			})
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}
	dir := stub.directory(t)

	got, err := dir.EnsureResourceServer(context.Background(), identifier, "p1")
	if err != nil {
		t.Fatalf("EnsureResourceServer: %v", err)
	}
	if got != "rsv-1" {
		t.Errorf("id = %q, want the existing resource server", got)
	}
	if writes := stub.writes(); len(writes) != 0 {
		t.Errorf("wrote %v for a resource server that already exists", writes)
	}
}

// The one conflict a converge can legitimately meet — another writer created the
// resource server between the find and the create — reaches the domain as ITS
// sentinel, so no caller above the composition root reads a Thunder error code.
func TestThunderDirectoryMapsTheIdentifierConflictOntoTheDomainsSentinel(t *testing.T) {
	stub := &thunderStub{}
	stub.handle = func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method == http.MethodGet && r.URL.Path == "/resource-servers" {
			_ = json.NewEncoder(w).Encode(map[string]any{"totalResults": 0, "resourceServers": []any{}})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"RES-1013","message":{"defaultValue":"identifier conflict"}}`))
	}
	dir := stub.directory(t)

	_, err := dir.EnsureResourceServer(context.Background(), "https://aep.wso2.com/orgs/acme/projects/p1", "p1")
	if !errors.Is(err, identity.ErrIdentifierConflict) {
		t.Fatalf("error = %v, want identity.ErrIdentifierConflict", err)
	}
}

// FindResourceServer is the read on its own — it never creates. The teardown
// leans on that: through the Ensure verb it would mint a resource server for a
// project that never had one, purely so the next step could delete it.
func TestThunderDirectoryFindResourceServerNeverCreates(t *testing.T) {
	stub := &thunderStub{}
	stub.handle = func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method == http.MethodGet && r.URL.Path == "/resource-servers" {
			_ = json.NewEncoder(w).Encode(map[string]any{"totalResults": 0, "resourceServers": []any{}})
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}
	dir := stub.directory(t)

	id, found, err := dir.FindResourceServer(context.Background(), "https://aep.wso2.com/orgs/acme/projects/p1")
	if err != nil {
		t.Fatalf("FindResourceServer: %v", err)
	}
	if found || id != "" {
		t.Errorf("found = %v, id = %q, want absent", found, id)
	}
	if writes := stub.writes(); len(writes) != 0 {
		t.Errorf("a lookup wrote %v", writes)
	}
}

// The CASCADE, which is the whole content of DeleteResourceServer: every action,
// then every resource, then the server. Thunder has no cascade of its own and
// answers 400 RES-1006 for a parent that still has children — and deleting the
// ROLES first does not help, which is why they are not in this sequence.
func TestThunderDirectoryDeleteResourceServerWalksTheTreeLeafFirst(t *testing.T) {
	stub := &thunderStub{}
	stub.handle = func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/resource-servers/rsv-1/resources":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totalResults": 2,
				"resources": []any{
					map[string]any{"id": "res-claims", "handle": "claims"},
					map[string]any{"id": "res-reports", "handle": "reports"},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/resource-servers/rsv-1/resources/res-claims/actions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totalResults": 1,
				"actions":      []any{map[string]any{"id": "act-read", "handle": "read"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/resource-servers/rsv-1/resources/res-reports/actions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"totalResults": 1,
				"actions":      []any{map[string]any{"id": "act-export", "handle": "export"}},
			})
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
	dir := stub.directory(t)

	if err := dir.DeleteResourceServer(context.Background(), "rsv-1"); err != nil {
		t.Fatalf("DeleteResourceServer: %v", err)
	}
	want := []string{
		"DELETE /resource-servers/rsv-1/resources/res-claims/actions/act-read",
		"DELETE /resource-servers/rsv-1/resources/res-reports/actions/act-export",
		"DELETE /resource-servers/rsv-1/resources/res-claims",
		"DELETE /resource-servers/rsv-1/resources/res-reports",
		"DELETE /resource-servers/rsv-1",
	}
	if got := stub.writes(); !reflect.DeepEqual(got, want) {
		t.Errorf("delete order:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// An assignment write names the principal's KIND on the wire. Sending the wrong
// one binds the role to a different object entirely — group ids and user ids
// come out of the same directory — so the mapping is asserted rather than
// assumed.
func TestThunderDirectoryAssignRoleSendsThePrincipalKind(t *testing.T) {
	for _, tc := range []struct {
		kind identity.PrincipalKind
		want string
	}{
		{identity.PrincipalGroup, "group"},
		{identity.PrincipalUser, "user"},
		{identity.PrincipalApp, "app"},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			stub := &thunderStub{}
			var sent map[string]any
			stub.handle = func(w http.ResponseWriter, r *http.Request, body []byte) {
				_ = json.Unmarshal(body, &sent)
				w.WriteHeader(http.StatusNoContent)
			}
			dir := stub.directory(t)

			if err := dir.AssignRole(context.Background(), "rol-1",
				identity.Principal{Kind: tc.kind, ID: "obj-1", Display: "ignored on write"}); err != nil {
				t.Fatalf("AssignRole: %v", err)
			}
			if got := stub.writes(); len(got) != 1 || got[0] != "POST /roles/rol-1/assignments/add" {
				t.Fatalf("writes = %v", got)
			}
			entries, _ := sent["assignments"].([]any)
			if len(entries) != 1 {
				t.Fatalf("assignments = %v", entries)
			}
			entry, _ := entries[0].(map[string]any)
			if entry["type"] != tc.want || entry["id"] != "obj-1" {
				t.Errorf("sent %v, want type %q id obj-1", entry, tc.want)
			}
			// Display is resolved by the directory on READ and is not a field
			// on the write; sending it would be sending a name as an identity.
			if _, present := entry["display"]; present {
				t.Errorf("the write carried a display name: %v", entry)
			}
		})
	}
}

// A principal kind the domain does not know is REFUSED rather than sent. A role
// assignment is a grant, so a malformed one must fail here and not as a
// validation error the caller cannot act on.
func TestThunderDirectoryRefusesAnUnknownPrincipalKind(t *testing.T) {
	stub := &thunderStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		t.Errorf("an unknown principal kind reached the directory: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}}
	dir := stub.directory(t)

	err := dir.AssignRole(context.Background(), "rol-1", identity.Principal{Kind: "robot", ID: "x"})
	if err == nil {
		t.Fatal("an unknown principal kind was accepted")
	}
	if !strings.Contains(err.Error(), "robot") {
		t.Errorf("error = %q, want it to name the kind", err)
	}
}
