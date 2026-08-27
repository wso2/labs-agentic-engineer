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

package rolesspec

import (
	"encoding/json"
	"strings"
	"testing"
)

// validDoc is the reference document every case mutates.
const validDoc = `{
  "version": 1,
  "coldStartRole": "Viewer",
  "publicComponents": ["expense-webapp"],
  "roles": [
    {
      "name": "Viewer",
      "description": "Reads own claims.",
      "stories": [1],
      "grantedBy": "first sign-in",
      "permissions": [{"component": "expense-api", "actions": ["read own claims"]}]
    },
    {
      "name": "Compliance Admin",
      "description": "Approves and audits submitted claims.",
      "stories": [3, 7],
      "grantedBy": "Compliance Admin",
      "permissions": [
        {"component": "expense-api", "actions": ["approve claim"]},
        {"component": "expense-webapp", "screens": ["Approvals"]}
      ]
    }
  ],
  "testUsers": [{"username": "test-viewer", "role": "Viewer"}]
}`

func mutate(t *testing.T, edit func(m map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(validDoc), &m); err != nil {
		t.Fatalf("fixture does not parse: %v", err)
	}
	edit(m)
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	return raw
}

func mustParse(t *testing.T, raw []byte) *Document {
	t.Helper()
	doc, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc
}

func TestParseAcceptsAWellFormedDocument(t *testing.T) {
	doc := mustParse(t, []byte(validDoc))
	if doc.Version != 1 {
		t.Fatalf("version = %d, want 1", doc.Version)
	}
	if doc.ColdStartRole == nil || *doc.ColdStartRole != "Viewer" {
		t.Fatalf("coldStartRole = %v, want Viewer", doc.ColdStartRole)
	}
	if len(doc.Roles) != 2 {
		t.Fatalf("roles = %d, want 2", len(doc.Roles))
	}
}

func TestParseRejects(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
		want string
	}{
		{"not JSON", []byte("{"), "not valid JSON"},
		{"version other than 1", mutate(t, func(m map[string]any) { m["version"] = 2 }), "must be 1"},
		{"no roles", mutate(t, func(m map[string]any) {
			m["roles"] = []any{}
			m["testUsers"] = []any{}
			m["coldStartRole"] = nil
		}), "at least 1 items"},
		{"unknown top-level property", mutate(t, func(m map[string]any) { m["password"] = "hunter2" }), "unknown property password"},
		{"unknown test-user property", mutate(t, func(m map[string]any) {
			m["testUsers"] = []any{map[string]any{"username": "test-viewer", "role": "Viewer", "password": "x"}}
		}), "unknown property password"},
		{"test user naming an undeclared role", mutate(t, func(m map[string]any) {
			m["testUsers"] = []any{map[string]any{"username": "test-nobody", "role": "Nobody"}}
		}), "which no roles[] entry declares"},
		{"coldStartRole naming an undeclared role", mutate(t, func(m map[string]any) {
			m["coldStartRole"] = "Nobody"
		}), "not a declared role"},
		{"username the directory cannot hold", mutate(t, func(m map[string]any) {
			m["testUsers"] = []any{map[string]any{"username": "Test Viewer", "role": "Viewer"}}
		}), "usable directory username"},
		{"duplicate username", mutate(t, func(m map[string]any) {
			m["testUsers"] = []any{
				map[string]any{"username": "test-viewer", "role": "Viewer"},
				map[string]any{"username": "test-viewer", "role": "Viewer"},
			}
		}), "listed twice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.raw)
			if err == nil {
				t.Fatalf("Parse accepted %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("message %q does not mention %q", err.Error(), tc.want)
			}
		})
	}
}

func TestParseAllowsANullColdStartRole(t *testing.T) {
	doc := mustParse(t, mutate(t, func(m map[string]any) { m["coldStartRole"] = nil }))
	if doc.ColdStartRole != nil {
		t.Fatalf("coldStartRole = %v, want nil", doc.ColdStartRole)
	}
}

func TestParseRejectsADuplicateRoleNameCaseInsensitively(t *testing.T) {
	raw := mutate(t, func(m map[string]any) {
		roles := m["roles"].([]any)
		dup := map[string]any{
			"name": "viewer", "description": "d", "stories": []any{2}, "grantedBy": "Viewer",
			"permissions": []any{map[string]any{"component": "api", "actions": []any{"read"}}},
		}
		m["roles"] = append(roles, dup)
	})
	if _, err := Parse(raw); err == nil || !strings.Contains(err.Error(), "declared twice") {
		t.Fatalf("err = %v, want a duplicate-name refusal", err)
	}
}

func TestParseRejectsAPermissionThatGrantsNothing(t *testing.T) {
	raw := mutate(t, func(m map[string]any) {
		roles := m["roles"].([]any)
		roles[0].(map[string]any)["permissions"] = []any{map[string]any{"component": "expense-api"}}
	})
	if _, err := Parse(raw); err == nil || !strings.Contains(err.Error(), "grants nothing") {
		t.Fatalf("err = %v, want a grants-nothing refusal", err)
	}
}

// Plan is where the mandatory-test-user promise is kept, so it gets the
// property tests rather than the ensure integration.

func TestPlanSuppliesAUserForEveryRoleTheDesignOmitted(t *testing.T) {
	plan := Plan(mustParse(t, []byte(validDoc)))
	if len(plan.Users) != 2 {
		t.Fatalf("users = %d, want one per role", len(plan.Users))
	}
	byRole := map[string]PlannedUser{}
	for _, u := range plan.Users {
		byRole[u.Role] = u
	}
	if got := byRole["Viewer"]; got.Username != "test-viewer" || got.Supplied {
		t.Fatalf("Viewer user = %+v, want the authored test-viewer", got)
	}
	// The design named no user for Compliance Admin — the platform supplies one.
	if got := byRole["Compliance Admin"]; got.Username != "test-compliance-admin" || !got.Supplied {
		t.Fatalf("Compliance Admin user = %+v, want a supplied test-compliance-admin", got)
	}
}

func TestPlanMarksTheColdStartRolesUser(t *testing.T) {
	plan := Plan(mustParse(t, []byte(validDoc)))
	var cold []string
	for _, u := range plan.Users {
		if u.ColdStart {
			cold = append(cold, u.Username)
		}
	}
	if len(cold) != 1 || cold[0] != "test-viewer" {
		t.Fatalf("cold-start users = %v, want exactly test-viewer", cold)
	}
}

func TestPlanMarksNoUserColdStartWhenTheDocumentNamesNoColdStartRole(t *testing.T) {
	plan := Plan(mustParse(t, mutate(t, func(m map[string]any) { m["coldStartRole"] = nil })))
	for _, u := range plan.Users {
		if u.ColdStart {
			t.Fatalf("%q marked cold-start with no coldStartRole declared", u.Username)
		}
	}
}

func TestPlanKeepsEveryAuthoredUserForARoleWithSeveral(t *testing.T) {
	plan := Plan(mustParse(t, mutate(t, func(m map[string]any) {
		m["testUsers"] = []any{
			map[string]any{"username": "test-viewer", "role": "Viewer"},
			map[string]any{"username": "second-viewer", "role": "Viewer"},
		}
	})))
	got := map[string]bool{}
	for _, u := range plan.Users {
		got[u.Username] = true
	}
	for _, want := range []string{"test-viewer", "second-viewer", "test-compliance-admin"} {
		if !got[want] {
			t.Fatalf("plan %v missing %q", got, want)
		}
	}
}

func TestPlanDisambiguatesAGeneratedNameThatCollidesWithAnAuthoredOne(t *testing.T) {
	// An authored user of role Viewer squats on the name the platform would
	// generate for Compliance Admin. The plan must not carry two entries for
	// one account.
	plan := Plan(mustParse(t, mutate(t, func(m map[string]any) {
		m["testUsers"] = []any{map[string]any{"username": "test-compliance-admin", "role": "Viewer"}}
	})))
	seen := map[string]int{}
	for _, u := range plan.Users {
		seen[u.Username]++
	}
	for name, n := range seen {
		if n > 1 {
			t.Fatalf("username %q planned %d times", name, n)
		}
	}
	var supplied string
	for _, u := range plan.Users {
		if u.Supplied {
			supplied = u.Username
		}
	}
	if supplied != "test-compliance-admin-2" {
		t.Fatalf("supplied username = %q, want the disambiguated form", supplied)
	}
}

func TestRoleSlug(t *testing.T) {
	cases := map[string]string{
		"Compliance Admin": "compliance-admin",
		"  Viewer  ":       "viewer",
		"Ops/Support":      "ops-support",
		"admin":            "admin",
		"!!!":              "role",
	}
	for in, want := range cases {
		if got := RoleSlug(in); got != want {
			t.Fatalf("RoleSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
