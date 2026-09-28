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
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/aeperms"
)

// aeStub is a Thunder that starts empty and remembers what was created, so a
// test can assert on the objects an install leaves behind rather than on the
// call sequence that made them.
type aeStub struct {
	resourceServers []map[string]any
	resources       []map[string]any
	actions         []map[string]any
	users           []map[string]any
	groups          []map[string]any
	roles           []map[string]any
	posts           []string // path of every POST, in order
}

func (s *aeStub) handler(t *testing.T) http.Handler {
	t.Helper()
	next := 0
	id := func(prefix string) string {
		next++
		return prefix + "-" + string(rune('a'+next))
	}
	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	body := func(r *http.Request) map[string]any {
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		return m
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if r.Method == http.MethodPost {
			s.posts = append(s.posts, p)
		}
		switch {
		case p == "/resource-servers" && r.Method == http.MethodGet:
			write(w, s.resourceServers)
		case p == "/resource-servers" && r.Method == http.MethodPost:
			m := body(r)
			m["id"] = id("rs")
			s.resourceServers = append(s.resourceServers, m)
			write(w, m)
		case strings.HasSuffix(p, "/resources") && r.Method == http.MethodGet:
			write(w, s.resources)
		case strings.HasSuffix(p, "/resources") && r.Method == http.MethodPost:
			m := body(r)
			m["id"] = id("res")
			s.resources = append(s.resources, m)
			write(w, m)
		case strings.HasSuffix(p, "/actions") && r.Method == http.MethodGet:
			write(w, s.actions)
		case strings.HasSuffix(p, "/actions") && r.Method == http.MethodPost:
			m := body(r)
			m["id"] = id("act")
			s.actions = append(s.actions, m)
			write(w, m)
		case p == "/users" && r.Method == http.MethodGet:
			write(w, s.users)
		case p == "/users" && r.Method == http.MethodPost:
			m := body(r)
			m["id"] = id("usr")
			s.users = append(s.users, m)
			write(w, m)
		case p == "/groups" && r.Method == http.MethodGet:
			write(w, s.groups)
		case p == "/groups" && r.Method == http.MethodPost:
			m := body(r)
			m["id"] = id("grp")
			s.groups = append(s.groups, m)
			write(w, m)
		case p == "/roles" && r.Method == http.MethodGet:
			write(w, s.roles)
		case p == "/roles" && r.Method == http.MethodPost:
			m := body(r)
			m["id"] = id("rol")
			s.roles = append(s.roles, m)
			write(w, m)
		default:
			t.Errorf("unexpected %s %s", r.Method, p)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func runEnsureAE(t *testing.T, s *aeStub, account AEAdminAccount) {
	t.Helper()
	srv := httptest.NewServer(s.handler(t))
	t.Cleanup(srv.Close)
	c := newTestClient(t, srv)
	if err := c.EnsureAEPermissions(context.Background(), "https://idp.example/ae", account); err != nil {
		t.Fatalf("EnsureAEPermissions: %v", err)
	}
}

func seededAdmin() AEAdminAccount {
	return AEAdminAccount{Username: "aeadmin", Password: "pw", Email: "a@b.c", Name: "AE Admin"}
}

// Every permission aep-api's gate can require must exist as an action here, or
// it is a permission no role can hold and no token can carry — the silent
// failure aeperms exists to prevent. Driven off aeperms.Actions rather than a
// literal list so adding a permission there fails here until it is provisioned.
func TestEnsureAEPermissions_CreatesAnActionForEveryPermission(t *testing.T) {
	s := &aeStub{}
	runEnsureAE(t, s, seededAdmin())

	got := map[string]bool{}
	for _, a := range s.actions {
		got[a["handle"].(string)] = true
	}
	for _, want := range aeperms.Actions {
		if !got[want.Handle] {
			t.Errorf("no Thunder action created for %s (handle %q)", want.Permission, want.Handle)
		}
	}
	if len(s.actions) != len(aeperms.Actions) {
		t.Errorf("created %d actions, want %d", len(s.actions), len(aeperms.Actions))
	}
}

// The identifier is what a token's audience and the console's RFC 8707
// resource indicator carry; a resource server created under any other one is
// unreachable from the console.
func TestEnsureAEPermissions_CreatesTheResourceServerOnItsIdentifier(t *testing.T) {
	s := &aeStub{}
	runEnsureAE(t, s, seededAdmin())

	if len(s.resourceServers) != 1 {
		t.Fatalf("want exactly one resource server, got %d", len(s.resourceServers))
	}
	if got := s.resourceServers[0]["identifier"]; got != "https://idp.example/ae" {
		t.Errorf("resource server identifier = %v, want the one passed in", got)
	}
	if got := s.resourceServers[0]["name"]; got != aeperms.ResourceServerName {
		t.Errorf("resource server name = %v, want %q", got, aeperms.ResourceServerName)
	}
}

// Each role carries exactly the permissions aeperms says it does, against the
// resource server just created, and is assigned to the group of the same name
// — which is what the OC AuthzRoleBinding's `groups` entitlement matches on.
func TestEnsureAEPermissions_RolesCarryTheirCatalogPermissions(t *testing.T) {
	s := &aeStub{}
	runEnsureAE(t, s, seededAdmin())

	rsID := s.resourceServers[0]["id"].(string)
	byName := map[string]map[string]any{}
	for _, r := range s.roles {
		byName[r["name"].(string)] = r
	}

	for _, role := range aeperms.Roles() {
		got, ok := byName[role]
		if !ok {
			t.Errorf("role %q was not created", role)
			continue
		}
		block := got["permissions"].([]any)[0].(map[string]any)
		if block["resourceServerId"] != rsID {
			t.Errorf("role %q points at %v, not the ae resource server", role, block["resourceServerId"])
		}
		var keys []string
		for _, p := range block["permissions"].([]any) {
			keys = append(keys, p.(string))
		}
		var want []string
		for _, p := range aeperms.RolePermissions(role) {
			want = append(want, string(p))
		}
		slices.Sort(keys)
		slices.Sort(want)
		if !slices.Equal(keys, want) {
			t.Errorf("role %q permissions = %v, want %v", role, keys, want)
		}

		assignment := got["assignments"].([]any)[0].(map[string]any)
		if assignment["type"] != "group" {
			t.Errorf("role %q is assigned to a %v, not a group", role, assignment["type"])
		}
	}
}

// Thunder honours a group's members only at CREATE, so the account has to
// exist before the group does. Asserted on the call ORDER because that is the
// property that breaks: get it wrong and ae-admin is created empty, and no
// later run can fill it.
func TestEnsureAEPermissions_SeedsTheAdminIntoItsGroupAtCreate(t *testing.T) {
	s := &aeStub{}
	runEnsureAE(t, s, seededAdmin())

	userPost := slices.Index(s.posts, "/users")
	groupPost := slices.Index(s.posts, "/groups")
	if userPost == -1 || groupPost == -1 || userPost > groupPost {
		t.Fatalf("the seeded user must be created before any group: posts = %v", s.posts)
	}

	var admin, developer map[string]any
	for _, g := range s.groups {
		switch g["name"] {
		case aeperms.RoleAdmin:
			admin = g
		case aeperms.RoleDeveloper:
			developer = g
		}
	}
	if admin == nil || developer == nil {
		t.Fatalf("both groups must be created, got %v", s.groups)
	}
	members, _ := admin["members"].([]any)
	if len(members) != 1 {
		t.Fatalf("ae-admin must be created holding the seeded account, got %v", admin["members"])
	}
	if m := members[0].(map[string]any); m["type"] != "user" || m["id"] != s.users[0]["id"] {
		t.Errorf("ae-admin's member is not the seeded account: %v", m)
	}
	// ae-developer is left empty on purpose — a real member is added later.
	if devMembers, _ := developer["members"].([]any); len(devMembers) != 0 {
		t.Errorf("ae-developer must be created empty, got %v", devMembers)
	}
}

// A blank password is how an operator says "this cluster signs in through its
// own IdP" — the permission model is still provisioned, so a real account can
// be added to ae-admin afterwards.
func TestEnsureAEPermissions_NoAccountStillProvisionsTheModel(t *testing.T) {
	s := &aeStub{}
	runEnsureAE(t, s, AEAdminAccount{})

	if len(s.users) != 0 {
		t.Errorf("no account was asked for, but %d were created", len(s.users))
	}
	if len(s.groups) != 2 || len(s.roles) != 2 {
		t.Errorf("the groups and roles must still exist: %d groups, %d roles", len(s.groups), len(s.roles))
	}
	if len(s.actions) != len(aeperms.Actions) {
		t.Errorf("the actions must still exist: got %d", len(s.actions))
	}
}

// A re-run of `aectl platform install` must be a no-op, not a pile of 409s or
// a second copy of everything.
func TestEnsureAEPermissions_IsIdempotent(t *testing.T) {
	s := &aeStub{}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()
	c := newTestClient(t, srv)

	for range 2 {
		if err := c.EnsureAEPermissions(context.Background(), "https://idp.example/ae", seededAdmin()); err != nil {
			t.Fatalf("EnsureAEPermissions: %v", err)
		}
	}

	if len(s.resourceServers) != 1 {
		t.Errorf("resource servers = %d, want 1", len(s.resourceServers))
	}
	if len(s.actions) != len(aeperms.Actions) {
		t.Errorf("actions = %d, want %d", len(s.actions), len(aeperms.Actions))
	}
	if len(s.users) != 1 {
		t.Errorf("users = %d, want 1", len(s.users))
	}
	if len(s.groups) != 2 {
		t.Errorf("groups = %d, want 2", len(s.groups))
	}
	if len(s.roles) != 2 {
		t.Errorf("roles = %d, want 2", len(s.roles))
	}
}

// An existing account's password is not reset by a re-install: somebody may
// have changed it, and an installer silently putting it back would be a way to
// regain access to a cluster rather than a convenience.
func TestEnsureAEPermissions_LeavesAnExistingAccountsPasswordAlone(t *testing.T) {
	s := &aeStub{}
	runEnsureAE(t, s, seededAdmin())
	before := len(s.posts)

	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()
	c := newTestClient(t, srv)
	if err := c.EnsureAEPermissions(context.Background(), "https://idp.example/ae",
		AEAdminAccount{Username: "aeadmin", Password: "a-different-password"}); err != nil {
		t.Fatalf("EnsureAEPermissions: %v", err)
	}

	if len(s.users) != 1 {
		t.Fatalf("users = %d, want 1", len(s.users))
	}
	if got := s.users[0]["credentials"].(map[string]any)["password"]; got != "pw" {
		t.Errorf("the stored password changed to %v — a re-install must not reset it", got)
	}
	if len(s.posts) != before {
		t.Errorf("a re-run wrote %d new objects, want none", len(s.posts)-before)
	}
}

// The identifier must be a URI Thunder will accept; an empty one is refused
// here rather than sent, because Thunder's own rejection (invalid_target, at
// /oauth2/authorize, much later) names nothing an installer could act on.
func TestEnsureAEPermissions_RefusesAnEmptyIdentifier(t *testing.T) {
	s := &aeStub{}
	srv := httptest.NewServer(s.handler(t))
	defer srv.Close()
	c := newTestClient(t, srv)

	err := c.EnsureAEPermissions(context.Background(), "", seededAdmin())
	if err == nil {
		t.Fatal("want an error for an empty resource server identifier")
	}
	if len(s.posts) != 0 {
		t.Errorf("nothing should have been created, got %v", s.posts)
	}
}
