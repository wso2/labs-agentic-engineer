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

// roles_test.go — the grant half, against the status codes and bodies ThunderID
// 1.0.0 was MEASURED to answer with (docs/design/draft/spikes/P1.md §2, §3, §7).
// Shared stub helpers live in resourceservers_test.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// -- reading ------------------------------------------------------------------

// Roles are per-OU, so the listing carries every project's roles and the
// platform's own. It is paged, and a project-qualified name — legal because "/"
// is accepted in a role name (P1 §2) — must survive the round trip intact.
func TestListRoles_PagesAndKeepsQualifiedNames(t *testing.T) {
	all := make([]Role, 0, 120)
	for i := 0; i < 120; i++ {
		all = append(all, Role{ID: fmt.Sprintf("role-%d", i), Name: fmt.Sprintf("p%d/Employee", i), OUID: "default-ou"})
	}
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method != http.MethodGet || r.URL.Path != "/roles" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		start, end := pageWindow(len(all), intQuery(t, r, "offset"), intQuery(t, r, "limit"))
		_ = json.NewEncoder(w).Encode(map[string]any{"totalResults": len(all), "roles": all[start:end]})
	}}
	srv := stub.server(t)
	defer srv.Close()

	got, err := newTestClient(srv.URL).ListRoles(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 120 {
		t.Fatalf("listed %d roles, want 120", len(got))
	}
	if got[119].Name != "p119/Employee" {
		t.Errorf("role name = %q, want the slash-qualified name back verbatim", got[119].Name)
	}
	wantPagingQueries(t, stub.snapshot(), 0, 100)
	wantSystemTokenOnEveryCall(t, stub.snapshot())
}

// Find is a client-side scan, case-insensitive for the same reason
// FindGroupByName is: the platform treats two names differing only in case as
// one role.
func TestFindRoleByName(t *testing.T) {
	roles := []Role{
		{ID: "r-1", Name: "claims/Employee"},
		{ID: "r-2", Name: "claims/Approver"},
	}
	tests := []struct {
		query string
		want  string
	}{
		{"claims/Approver", "r-2"},
		{"CLAIMS/approver", "r-2"},
		{"claims/Auditor", ""},
		{"Approver", ""}, // unqualified: a different role name, not this one
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			stub := &directoryStub{handle: func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				_ = json.NewEncoder(w).Encode(map[string]any{"totalResults": len(roles), "roles": roles})
			}}
			srv := stub.server(t)
			defer srv.Close()

			got, found, err := newTestClient(srv.URL).FindRoleByName(context.Background(), tc.query)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.want == "" {
				if found {
					t.Fatalf("found %+v, want absent", got)
				}
				return
			}
			if !found || got.ID != tc.want {
				t.Fatalf("found=%v role=%+v, want %s", found, got, tc.want)
			}
		})
	}
}

// GET /roles/{id} is the only read that carries the permissions, grouped by the
// resource server that derived them.
func TestGetRole_ReadsPermissionsPerResourceServer(t *testing.T) {
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.URL.Path != "/roles/r-1" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		_ = json.NewEncoder(w).Encode(Role{
			ID: "r-1", Name: "claims/Approver", OUID: "default-ou",
			Permissions: []RolePermission{
				{ResourceServerID: "rs-1", Permissions: []string{"claims:read-all", "claims:approve"}},
			},
		})
	}}
	srv := stub.server(t)
	defer srv.Close()

	got, err := newTestClient(srv.URL).GetRole(context.Background(), "r-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Permissions) != 1 || got.Permissions[0].ResourceServerID != "rs-1" ||
		len(got.Permissions[0].Permissions) != 2 {
		t.Fatalf("permissions = %+v", got.Permissions)
	}
}

// include=display is what lets a caller report "assigned to group X" without a
// lookup per assignment.
func TestListRoleAssignments_AsksForDisplayNames(t *testing.T) {
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if got := r.URL.Query().Get("include"); got != "display" {
			t.Errorf("include = %q, want display", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalResults": 2,
			"assignments": []Assignment{
				{Type: AssigneeGroup, ID: "g-1", Display: "claims Employees"},
				{Type: AssigneeUser, ID: "u-1", Display: "alice"},
			},
		})
	}}
	srv := stub.server(t)
	defer srv.Close()

	got, err := newTestClient(srv.URL).ListRoleAssignments(context.Background(), "r-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0].Type != AssigneeGroup || got[0].Display != "claims Employees" {
		t.Fatalf("assignments = %+v", got)
	}
	wantSystemTokenOnEveryCall(t, stub.snapshot())
}

// -- writing ------------------------------------------------------------------

// Create resolves the default OU, then POSTs name + description + the whole
// permission set. 201 with the role echoed back (P1 §2).
func TestCreateRole_PostsPermissionsAndOU(t *testing.T) {
	stub := &directoryStub{ouID: "ou-7", handle: func(w http.ResponseWriter, r *http.Request, body []byte) {
		if r.Method != http.MethodPost || r.URL.Path != "/roles" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		var in Role
		_ = json.Unmarshal(body, &in)
		in.ID = "r-1"
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(in)
	}}
	srv := stub.server(t)
	defer srv.Close()

	perms := []RolePermission{{ResourceServerID: "rs-1", Permissions: []string{"claims:read", "claims:submit"}}}
	got, err := newTestClient(srv.URL).
		CreateRole(context.Background(), "claims/Employee", "submits claims", perms)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantSeq(t, stub.seq(), "GET /organization-units/tree/default", "POST /roles")
	if got.ID != "r-1" || got.Name != "claims/Employee" {
		t.Fatalf("created = %+v", got)
	}

	var sent map[string]any
	decodeCallBody(t, stub.only(t, http.MethodPost, "/roles"), &sent)
	if sent["ouId"] != "ou-7" {
		t.Errorf("create body ouId = %v, want ou-7", sent["ouId"])
	}
	if sent["name"] != "claims/Employee" {
		t.Errorf("create body name = %v, want the slash-qualified name unescaped", sent["name"])
	}
}

// A role declared before its catalog exists is a legal state and has to go on
// the wire as [] — a nil slice would marshal as null on a field Thunder
// requires.
func TestCreateRole_NoPermissionsSendsAnEmptyList(t *testing.T) {
	stub := &directoryStub{handle: func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Role{ID: "r-1"})
	}}
	srv := stub.server(t)
	defer srv.Close()

	if _, err := newTestClient(srv.URL).CreateRole(context.Background(), "claims/Nobody", "", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw := string(stub.only(t, http.MethodPost, "/roles").Body)
	if !strings.Contains(raw, `"permissions":[]`) {
		t.Errorf("create body %s, want permissions as an empty list", raw)
	}
}

// P1 §2: the two ways a role write is refused, each with its own code. Neither
// is recoverable inside the client — the ensure has to hear which one it was.
func TestCreateRole_RefusalsAreNamed(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		code     string
		message  string
		sentinel error
	}{
		{"duplicate name", http.StatusConflict, "ROL-1004", "Role name conflict", ErrRoleNameConflict},
		{"permission not in the catalog", http.StatusBadRequest, "ROL-1012", "Invalid permissions", ErrInvalidPermissions},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := &directoryStub{handle: func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeThunderError(w, tc.status, tc.code, tc.message)
			}}
			srv := stub.server(t)
			defer srv.Close()

			_, err := newTestClient(srv.URL).CreateRole(context.Background(), "claims/Employee", "", nil)
			wantSentinel(t, err, tc.sentinel, tc.code)
			if !strings.Contains(err.Error(), tc.message) {
				t.Errorf("error %q should carry Thunder's message %q", err, tc.message)
			}
		})
	}
}

// P1 §7: the PUT fully replaces the permission set and carries NO assignments
// field — that is what makes converging a role to its tag one call with no
// re-assignment pass. The test asserts the second half by shape: an assignments
// key in the body would mean the client thinks it has to restate them.
func TestUpdateRole_ReplacesPermissionsAndSendsNoAssignments(t *testing.T) {
	stub := &directoryStub{ouID: "ou-7", handle: func(w http.ResponseWriter, r *http.Request, body []byte) {
		if r.Method != http.MethodPut || r.URL.Path != "/roles/r-1" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		var in Role
		_ = json.Unmarshal(body, &in)
		in.ID = "r-1"
		_ = json.NewEncoder(w).Encode(in)
	}}
	srv := stub.server(t)
	defer srv.Close()

	perms := []RolePermission{{ResourceServerID: "rs-1", Permissions: []string{"claims:read-all"}}}
	got, err := newTestClient(srv.URL).
		UpdateRole(context.Background(), "r-1", "claims/Approver", "approves claims", perms)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Permissions) != 1 || got.Permissions[0].Permissions[0] != "claims:read-all" {
		t.Fatalf("updated = %+v, want exactly the permissions sent", got)
	}

	call := stub.only(t, http.MethodPut, "/roles/r-1")
	var sent map[string]any
	decodeCallBody(t, call, &sent)
	if _, present := sent["assignments"]; present {
		t.Error("the update must not restate assignments — Thunder keeps them")
	}
	if sent["ouId"] != "ou-7" || sent["name"] != "claims/Approver" {
		t.Errorf("update body = %v, want the OU and name required by the contract", sent)
	}
}

// P1 §3: add and remove both answer 204, and remove is a POST to its own
// endpoint — not a DELETE on the collection. The 500 an earlier Thunder build
// answered `assignments/add` with is gone.
func TestRoleAssignments_AddAndRemove(t *testing.T) {
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		switch r.URL.Path {
		case "/roles/r-1/assignments/add", "/roles/r-1/assignments/remove":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}}
	srv := stub.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	ctx := context.Background()
	assignments := []Assignment{
		{Type: AssigneeGroup, ID: "g-1", Display: "claims Employees"},
		{Type: AssigneeUser, ID: "u-1"},
	}
	if err := c.AddRoleAssignments(ctx, "r-1", assignments); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := c.RemoveRoleAssignments(ctx, "r-1", assignments[:1]); err != nil {
		t.Fatalf("remove: %v", err)
	}
	wantSeq(t, stub.seq(), "POST /roles/r-1/assignments/add", "POST /roles/r-1/assignments/remove")

	var sent struct {
		Assignments []map[string]string `json:"assignments"`
	}
	decodeCallBody(t, stub.only(t, http.MethodPost, "/roles/r-1/assignments/add"), &sent)
	if len(sent.Assignments) != 2 {
		t.Fatalf("add body = %+v", sent.Assignments)
	}
	if sent.Assignments[0]["type"] != "group" || sent.Assignments[0]["id"] != "g-1" {
		t.Errorf("assignment = %v, want type+id", sent.Assignments[0])
	}
	// display is read-only server-side: a value read back from
	// ListRoleAssignments must be safe to hand straight to the write.
	if _, present := sent.Assignments[0]["display"]; present {
		t.Error("display must not be sent on an assignment write")
	}
	wantSystemTokenOnEveryCall(t, stub.snapshot())
}

// Nothing to assign is nothing to do: an empty list must not become a request
// whose only possible outcome is a 400.
func TestRoleAssignments_EmptyListIssuesNoRequest(t *testing.T) {
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	}}
	srv := stub.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	if err := c.AddRoleAssignments(context.Background(), "r-1", nil); err != nil {
		t.Errorf("add: %v", err)
	}
	if err := c.RemoveRoleAssignments(context.Background(), "r-1", []Assignment{}); err != nil {
		t.Errorf("remove: %v", err)
	}
	if len(stub.seq()) != 0 {
		t.Errorf("issued %v, want no requests", stub.seq())
	}
}

// SAZ-4030 — "the write would grant permissions the caller does not hold" — has
// never been seen for this platform's system client, whose `system` scope on the
// System resource server bypasses the check (P1 §2). It is mapped anyway so a
// Thunder that DID enforce it would name itself. There is deliberately no
// fallback: the call fails.
//
// It is also a 403, which IsAuthError reports as a credential rejection. That
// overlap is recorded here rather than special-cased: the remedy for a real
// SAZ-4030 (re-read the binding, drop the cached client) is harmless, and the
// sentinel is what tells the two apart.
func TestRoleWrites_GrantNotPermittedIsNamed(t *testing.T) {
	tests := []struct {
		name string
		call func(Client) error
	}{
		{"create", func(c Client) error {
			_, err := c.CreateRole(context.Background(), "claims/Employee", "", nil)
			return err
		}},
		{"update", func(c Client) error {
			_, err := c.UpdateRole(context.Background(), "r-1", "claims/Employee", "", nil)
			return err
		}},
		{"assign", func(c Client) error {
			return c.AddRoleAssignments(context.Background(), "r-1",
				[]Assignment{{Type: AssigneeGroup, ID: "g-1"}})
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := &directoryStub{handle: func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				writeThunderError(w, http.StatusForbidden, "SAZ-4030",
					"The operation would grant permissions that the caller does not hold")
			}}
			srv := stub.server(t)
			defer srv.Close()

			err := tc.call(newTestClient(srv.URL))
			wantSentinel(t, err, ErrGrantNotPermitted, "SAZ-4030")
			if !IsAuthError(err) {
				t.Error("a 403 is still reported as an auth error to the client cache")
			}
		})
	}
}

// Deleting a role is idempotent, and a role that is gone takes its assignments
// with it — a re-run of a project teardown must not fail on the second pass.
func TestDeleteRole_AlreadyGoneIsDone(t *testing.T) {
	stub := &directoryStub{handle: func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method != http.MethodDelete || r.URL.Path != "/roles/r-1" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		writeThunderError(w, http.StatusNotFound, "ROL-1003", "Role not found")
	}}
	srv := stub.server(t)
	defer srv.Close()

	if err := newTestClient(srv.URL).DeleteRole(context.Background(), "r-1"); err != nil {
		t.Errorf("DeleteRole: %v", err)
	}
}

// A role id goes into the path, so it is escaped — as does anything else this
// client puts there. (Ids are uuids today; the escaping is what keeps that from
// being load-bearing.)
func TestRolePaths_EscapeTheID(t *testing.T) {
	stub := &directoryStub{handle: func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(http.StatusNoContent)
	}}
	srv := stub.server(t)
	defer srv.Close()

	if err := newTestClient(srv.URL).DeleteRole(context.Background(), "a/b"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	call := stub.snapshot()[0]
	if call.RawPath != "/roles/a%2Fb" {
		t.Errorf("raw path = %q, want the id escaped", call.RawPath)
	}
}
