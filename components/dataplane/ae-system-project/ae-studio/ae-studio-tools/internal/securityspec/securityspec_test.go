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

// Moved from services/aep-api/internal/platform/securityspec/securityspec_test.go;
// the aep-api copy is deleted in phase 4.

package securityspec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The three documents the design walks end to end — copied byte-for-byte from
// the agent's fixtures (packages/agent-stream/test/fixtures/security), so the
// two gates are exercised against the SAME documents and a rule that only one
// side accepts shows up as a fixture that only one side parses.
const (
	expenseTracker = "expense-tracker.json"
	clinic         = "clinic.json"
	vendorPortal   = "vendor.json"
)

// agentFixtureDir is where those documents are copied FROM. securityspec →
// internal → ae-studio-tools → ae-studio → ae-system-project → dataplane →
// components → repo root.
const agentFixtureDir = "../../../../../../../packages/agent-stream/test/fixtures/security"

// The copy is only worth anything while it IS a copy. Nothing in either build
// notices a fixture edited on one side — the rules would simply be exercised
// against two different documents and agree on neither — so the equality is
// asserted directly, in both directions: a file added to one tree and not the
// other is as much of a drift as a changed byte.
func TestFixturesAreByteIdenticalToTheAgentsOwn(t *testing.T) {
	walk := func(root string) map[string]string {
		t.Helper()
		out := map[string]string{}
		if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			body, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			out[filepath.ToSlash(rel)] = string(body)
			return nil
		}); err != nil {
			t.Fatalf("walk %s — layout drift?: %v", root, err)
		}
		return out
	}
	ours, theirs := walk("testdata"), walk(agentFixtureDir)
	if len(theirs) == 0 {
		t.Fatalf("no agent fixtures found under %s — the path moved", agentFixtureDir)
	}

	for rel, want := range theirs {
		got, present := ours[rel]
		if !present {
			t.Errorf("%s exists in the agent's fixtures and not in testdata — copy it", rel)
			continue
		}
		if got != want {
			t.Errorf("testdata/%s has drifted from the agent's copy; re-copy it from %s/%s", rel, agentFixtureDir, rel)
		}
	}
	for rel := range ours {
		if _, present := theirs[rel]; !present {
			t.Errorf("testdata/%s has no agent counterpart — these fixtures are a copy, not a fork", rel)
		}
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

// mutate parses a fixture, lets the case edit it as free-form JSON, and hands
// back the bytes — so a case can express "this document but with X broken"
// without restating 60 lines of catalog.
func mutate(t *testing.T, name string, edit func(m map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(fixture(t, name), &m); err != nil {
		t.Fatalf("fixture %s does not parse: %v", name, err)
	}
	edit(m)
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	return raw
}

func roleNamed(t *testing.T, m map[string]any, name string) map[string]any {
	t.Helper()
	for _, entry := range m["roles"].([]any) {
		role := entry.(map[string]any)
		if role["name"] == name {
			return role
		}
	}
	t.Fatalf("fixture has no role %q", name)
	return nil
}

func TestParseAcceptsEveryDesignFixture(t *testing.T) {
	for _, name := range []string{expenseTracker, clinic, vendorPortal} {
		t.Run(name, func(t *testing.T) {
			doc, err := Parse(fixture(t, name))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if doc.Version != 3 {
				t.Fatalf("version = %d, want 3", doc.Version)
			}
			if len(doc.Permissions) == 0 || len(doc.Roles) == 0 {
				t.Fatalf("fixture carries no catalog or no roles: %+v", doc)
			}
		})
	}
}

// The Vendor Portal reuses the org group `Finance` as the name of a role — legal
// precisely because that group is NOT declared in this document's groups[]: the
// rule is about one document naming one string two ways, not about a role name
// that happens to exist somewhere on the directory.
func TestParseAcceptsARoleNamedAfterAnUndeclaredOrgGroup(t *testing.T) {
	doc, err := Parse(fixture(t, vendorPortal))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var found bool
	for _, role := range doc.Roles {
		if role.Name == "Finance" {
			found = true
		}
	}
	if !found {
		t.Fatalf("fixture no longer carries the Finance role — the case it pins is gone")
	}
}

func TestParseRejects(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		edit    func(t *testing.T, m map[string]any)
		want    string
	}{
		{
			name:    "a grant naming no catalog handle",
			fixture: expenseTracker,
			edit: func(t *testing.T, m map[string]any) {
				roleNamed(t, m, "Employee")["grants"] = []any{"claims:read", "claims:audit"}
			},
			want: `grants "claims:audit"`,
		},
		{
			name:    "assignableBy naming no declared role",
			fixture: expenseTracker,
			edit: func(t *testing.T, m map[string]any) {
				roleNamed(t, m, "Approver")["assignableBy"] = []any{"Auditor"}
			},
			want: `"Auditor"`,
		},
		{
			name:    "an admin-enrolment user role with no assignTo",
			fixture: expenseTracker,
			edit: func(t *testing.T, m map[string]any) {
				delete(roleNamed(t, m, "Employee"), "assignTo")
			},
			want: "with no assignTo",
		},
		{
			name:    "a self-service role carrying assignTo",
			fixture: clinic,
			edit: func(t *testing.T, m map[string]any) {
				roleNamed(t, m, "Patient")["assignTo"] = []any{"Clinic Reception"}
			},
			want: "self-service",
		},
		{
			name:    "a service role carrying assignTo",
			fixture: vendorPortal,
			edit: func(t *testing.T, m map[string]any) {
				roleNamed(t, m, "reconciliation-job")["assignTo"] = []any{"Buyers"}
			},
			want: "service role",
		},
		{
			name:    "a test user holding a role nothing declares",
			fixture: expenseTracker,
			edit: func(t *testing.T, m map[string]any) {
				m["testUsers"] = []any{map[string]any{"username": "test-nobody", "roles": []any{"Nobody"}}}
			},
			want: `holds role "Nobody"`,
		},
		{
			name:    "a test user holding a service role",
			fixture: vendorPortal,
			edit: func(t *testing.T, m map[string]any) {
				m["testUsers"] = []any{map[string]any{"username": "test-job", "roles": []any{"reconciliation-job"}}}
			},
			want: "service role",
		},
		{
			name:    "the same username twice",
			fixture: expenseTracker,
			edit: func(t *testing.T, m map[string]any) {
				m["testUsers"] = []any{
					map[string]any{"username": "test-employee", "roles": []any{"Employee"}},
					map[string]any{"username": "test-employee", "roles": []any{"Approver"}},
				}
			},
			want: "listed twice",
		},
		{
			name:    "a username the directory cannot hold",
			fixture: expenseTracker,
			edit: func(t *testing.T, m map[string]any) {
				m["testUsers"] = []any{map[string]any{"username": "Test Employee", "roles": []any{"Employee"}}}
			},
			want: "usable directory username",
		},
		{
			name:    "the same resource declared twice",
			fixture: expenseTracker,
			edit: func(t *testing.T, m map[string]any) {
				perms := m["permissions"].([]any)
				m["permissions"] = append(perms, map[string]any{
					"resource":  "claims",
					"component": "expense-api",
					"actions":   []any{map[string]any{"handle": "purge"}},
				})
			},
			want: "declared twice",
		},
		{
			name:    "the same action handle twice on one resource",
			fixture: expenseTracker,
			edit: func(t *testing.T, m map[string]any) {
				perm := m["permissions"].([]any)[0].(map[string]any)
				perm["actions"] = append(perm["actions"].([]any),
					map[string]any{"handle": "read"})
			},
			want: `action "read" twice`,
		},
		{
			name:    "a role name that repeats another case-insensitively",
			fixture: expenseTracker,
			edit: func(t *testing.T, m map[string]any) {
				roleNamed(t, m, "Approver")["name"] = "EMPLOYEE"
			},
			want: "declared twice",
		},
		{
			name:    "a role named after a group THIS document declares",
			fixture: expenseTracker,
			edit: func(t *testing.T, m map[string]any) {
				roleNamed(t, m, "Employee")["name"] = "Employees"
			},
			want: "also declared in groups[]",
		},
		{
			name:    "a handle segment that is not lowercase",
			fixture: expenseTracker,
			edit: func(t *testing.T, m map[string]any) {
				m["permissions"].([]any)[1].(map[string]any)["resource"] = "Reports"
			},
			want: `permissions.1.resource: must be lowercase letters`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(mutate(t, tc.fixture, func(m map[string]any) { tc.edit(t, m) }))
			if err == nil {
				t.Fatalf("want a refusal, got none")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("message %q does not carry %q", err.Error(), tc.want)
			}
		})
	}
}

// A v1 document is refused with ONE sentence naming the fields v2 removed, not a
// schema dump in which the real cause is one issue among six. Designs are
// regenerated, so there is no migration to offer.
func TestParseRefusesAVersion1Document(t *testing.T) {
	const v1 = `{
      "version": 1,
      "coldStartRole": "Viewer",
      "publicComponents": ["web"],
      "roles": [{"name":"Viewer","description":"Reads.","stories":["F1.1"],"grantedBy":"first sign-in",
                 "permissions":[{"component":"api","actions":["read"]}]}],
      "testUsers": [{"username":"test-viewer","role":"Viewer"}],
      "thunder": {"name":"Expense Tracker","type":"browser"}
    }`
	_, err := Parse([]byte(v1))
	if err == nil {
		t.Fatal("a v1 document must not parse")
	}
	for _, field := range []string{
		"coldStartRole", "publicComponents", "thunder",
		"roles[].grantedBy", "roles[].permissions", "testUsers[].role",
	} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("the refusal does not name %s: %s", field, err.Error())
		}
	}
}

// A half-migrated file is v1 too: it says 2 but still carries a removed field,
// and the schema's unknown-key refusal would name the key without saying why it
// is gone.
func TestParseRefusesAHalfMigratedDocument(t *testing.T) {
	raw := mutate(t, expenseTracker, func(m map[string]any) { m["coldStartRole"] = "Employee" })
	_, err := Parse(raw)
	if err == nil {
		t.Fatal("a document still carrying coldStartRole must not parse")
	}
	if !strings.Contains(err.Error(), "coldStartRole") {
		t.Fatalf("the refusal does not name the removed field: %s", err.Error())
	}
}

// v3 carries NO screen table: a screen's gate is the scope of the operation
// that LOADS it, and that scope is already in openapi.yaml (ADR-0033). The
// schema's `additionalProperties: false` is what refuses a document that still
// carries one, and the refusal has to NAME the key — the agent's gate answers
// the same document with Zod's unrecognized-key message, so both gates say
// "screens".
func TestParseRefusesAVersion3DocumentThatStillCarriesScreens(t *testing.T) {
	raw := mutate(t, expenseTracker, func(m map[string]any) {
		m["screens"] = []any{
			map[string]any{"component": "expense-webapp", "screen": "My Claims", "requires": "claims:read"},
		}
	})
	_, err := Parse(raw)
	if err == nil {
		t.Fatal("a v3 document carrying screens[] must not parse")
	}
	if !strings.Contains(err.Error(), "screens") {
		t.Fatalf("the refusal does not name the key: %s", err.Error())
	}
}

func TestParseRejectsUnknownKeysAndMalformedJSON(t *testing.T) {
	if _, err := Parse([]byte(`{"version":3,`)); err == nil {
		t.Fatal("malformed JSON must not parse")
	}
	raw := mutate(t, expenseTracker, func(m map[string]any) { m["password"] = "hunter2" })
	if _, err := Parse(raw); err == nil {
		t.Fatal("an unknown key must not parse — that is what keeps a secret out of the file mechanically")
	}
}
