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

package securityspec

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
// platform → internal → aep-api → services → repo root.
const agentFixtureDir = "../../../../../packages/agent-stream/test/fixtures/security"

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
			if doc.Version != 2 {
				t.Fatalf("version = %d, want 2", doc.Version)
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
					"actions":   []any{map[string]any{"handle": "purge", "ownership": "any"}},
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
					map[string]any{"handle": "read", "ownership": "any"})
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
		{
			name:    "a screen requiring a handle the catalog does not declare",
			fixture: expenseTracker,
			edit: func(t *testing.T, m map[string]any) {
				m["screens"].([]any)[0].(map[string]any)["requires"] = "claims:audit"
			},
			want: `requires "claims:audit"`,
		},
		{
			// Decision B1, and the defect the design's own example carried: the
			// "all" handle widens the ROWS the read operation returns; a role
			// holding it without the read reaches a screen it cannot load.
			name:    "read-all without read",
			fixture: expenseTracker,
			edit: func(t *testing.T, m map[string]any) {
				roleNamed(t, m, "Approver")["grants"] = []any{
					"claims:read-all", "claims:approve", "claims:reject", "reports:read",
				}
			},
			want: `grants "claims:read-all" without "claims:read"`,
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
      "roles": [{"name":"Viewer","description":"Reads.","stories":[1],"grantedBy":"first sign-in",
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

func TestParseRejectsUnknownKeysAndMalformedJSON(t *testing.T) {
	if _, err := Parse([]byte(`{"version":2,`)); err == nil {
		t.Fatal("malformed JSON must not parse")
	}
	raw := mutate(t, expenseTracker, func(m map[string]any) { m["password"] = "hunter2" })
	if _, err := Parse(raw); err == nil {
		t.Fatal("an unknown key must not parse — that is what keeps a secret out of the file mechanically")
	}
}

func TestPlanExpandsTheCatalogAndTheRoles(t *testing.T) {
	doc, err := Parse(fixture(t, expenseTracker))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	plan := Plan(doc)

	wantHandles := []string{
		"claims:read", "claims:read-all", "claims:submit", "claims:approve", "claims:reject",
		"reports:read", "reports:export",
	}
	if !slices.Equal(plan.Handles, wantHandles) {
		t.Fatalf("handles = %v, want %v", plan.Handles, wantHandles)
	}
	if got := plan.Grants["Employee"]; !slices.Equal(got, []string{"claims:read", "claims:submit"}) {
		t.Fatalf("Employee grants = %v", got)
	}
}

// The plan carries the catalog TWICE on purpose: flat, because that is the
// client's scope allowlist and the set grants are checked against, and as the
// two-level tree the directory objects are created from. The tree is the only
// copy that carries the document's prose, and the reason it exists: a build
// rebuilding the tree from the flat handles could only create objects named
// after their handles with no description at all.
func TestPlanCarriesTheCatalogAsATreeWithTheDocumentsProse(t *testing.T) {
	doc, err := Parse(fixture(t, expenseTracker))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	plan := Plan(doc)

	want := []PlannedResource{
		{
			Handle: "claims", Description: "Expense claims and their approval",
			Actions: []PlannedAction{
				{Handle: "read", Description: "See own claims"},
				{Handle: "read-all", Description: "See every claim"},
				{Handle: "submit", Description: "Create and send a claim"},
				{Handle: "approve", Description: "Approve a submitted claim"},
				{Handle: "reject", Description: "Reject a submitted claim"},
			},
		},
		{
			// A resource the document described with nothing carries nothing —
			// the plan never invents prose.
			Handle: "reports",
			Actions: []PlannedAction{
				{Handle: "read", Description: "Monthly totals"},
				{Handle: "export", Description: "Download CSV"},
			},
		},
	}
	if !reflect.DeepEqual(plan.Catalog, want) {
		t.Fatalf("catalog = %+v\nwant %+v", plan.Catalog, want)
	}

	// The two views must name the same handles in the same order, or the client's
	// allowlist and the directory's catalog could disagree about what exists.
	var flattened []string
	for _, resource := range plan.Catalog {
		for _, action := range resource.Actions {
			flattened = append(flattened, resource.Handle+":"+action.Handle)
		}
	}
	if !slices.Equal(flattened, plan.Handles) {
		t.Fatalf("catalog flattens to %v, want the plan's handles %v", flattened, plan.Handles)
	}
}

// Groups are the document's own declarations plus every name a role assigns to —
// `Finance` is reused from the org directory and never appears in groups[].
func TestPlanUnionsDeclaredGroupsAndAssignToNames(t *testing.T) {
	doc, err := Parse(fixture(t, expenseTracker))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	plan := Plan(doc)
	if len(plan.Groups) != 2 {
		t.Fatalf("groups = %+v, want Employees and Finance", plan.Groups)
	}
	if plan.Groups[0].Name != "Employees" || !plan.Groups[0].Declared {
		t.Fatalf("first group = %+v, want the declared Employees", plan.Groups[0])
	}
	if plan.Groups[1].Name != "Finance" || plan.Groups[1].Declared {
		t.Fatalf("second group = %+v, want Finance, reused and undeclared", plan.Groups[1])
	}
	if plan.Groups[1].Description != "" {
		t.Fatalf("a reused group must carry no description — the platform never rewrites somebody else's: %+v", plan.Groups[1])
	}
}

func TestPlanGivesEachUserItsGroupsAndScopes(t *testing.T) {
	doc, err := Parse(fixture(t, expenseTracker))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	plan := Plan(doc)
	byName := map[string]PlannedUser{}
	for _, u := range plan.Users {
		byName[u.Username] = u
	}
	approver, ok := byName["test-approver"]
	if !ok {
		t.Fatalf("no test-approver in %+v", plan.Users)
	}
	if !slices.Equal(approver.Groups, []string{"Finance"}) {
		t.Fatalf("groups = %v, want [Finance]", approver.Groups)
	}
	want := []string{"claims:read", "claims:read-all", "claims:approve", "claims:reject", "reports:read"}
	if !slices.Equal(approver.Scopes, want) {
		t.Fatalf("scopes = %v, want %v", approver.Scopes, want)
	}
}

// One account, several roles: the groups and the scopes come out as a UNION in
// first-seen order, not as a concatenation with repeats.
func TestPlanUnionsTheRolesOfOneAccount(t *testing.T) {
	raw := mutate(t, expenseTracker, func(m map[string]any) {
		m["testUsers"] = []any{map[string]any{
			"username": "test-both", "roles": []any{"Employee", "Approver"},
		}}
	})
	doc, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	plan := Plan(doc)
	var both PlannedUser
	for _, u := range plan.Users {
		if u.Username == "test-both" {
			both = u
		}
	}
	if !slices.Equal(both.Groups, []string{"Employees", "Finance"}) {
		t.Fatalf("groups = %v", both.Groups)
	}
	want := []string{"claims:read", "claims:submit", "claims:read-all", "claims:approve", "claims:reject", "reports:read"}
	if !slices.Equal(both.Scopes, want) {
		t.Fatalf("scopes = %v, want %v", both.Scopes, want)
	}
}

func TestPlanSuppliesALoginForARoleTheDesignLeftWithout(t *testing.T) {
	raw := mutate(t, expenseTracker, func(m map[string]any) {
		m["testUsers"] = []any{map[string]any{"username": "test-employee", "roles": []any{"Employee"}}}
	})
	doc, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	plan := Plan(doc)
	var supplied []PlannedUser
	for _, u := range plan.Users {
		if u.Supplied {
			supplied = append(supplied, u)
		}
	}
	if len(supplied) != 1 || supplied[0].Username != "test-approver" {
		t.Fatalf("want one supplied account for Approver, got %+v", supplied)
	}
	if !slices.Equal(supplied[0].Groups, []string{"Finance"}) {
		t.Fatalf("a supplied account still gets its role's groups: %+v", supplied[0])
	}
}

// A self-service role's accounts come from the app's registration flow and a
// service role is held by an application principal, so neither owes a login.
func TestPlanSuppliesNoLoginForSelfServiceOrServiceRoles(t *testing.T) {
	for _, tc := range []struct{ name, fixture, role string }{
		{"self-service", clinic, "Patient"},
		{"service", vendorPortal, "reconciliation-job"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := Parse(fixture(t, tc.fixture))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			for _, u := range Plan(doc).Users {
				if slices.Contains(u.Roles, tc.role) {
					t.Fatalf("%s role %q got the login %q", tc.name, tc.role, u.Username)
				}
			}
		})
	}
}

func TestPlanDisambiguatesAGeneratedNameThatCollidesWithAnAuthoredOne(t *testing.T) {
	// `test-approver` is authored for the EMPLOYEE role, so the Approver's own
	// supplied name must not collide with it.
	raw := mutate(t, expenseTracker, func(m map[string]any) {
		m["testUsers"] = []any{map[string]any{"username": "test-approver", "roles": []any{"Employee"}}}
	})
	doc, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	seen := map[string]bool{}
	for _, u := range Plan(doc).Users {
		if seen[u.Username] {
			t.Fatalf("the plan carries two entries for %q: %+v", u.Username, Plan(doc).Users)
		}
		seen[u.Username] = true
	}
}

// The property the whole expansion exists for: every role the platform owes a
// login gets one, and every handle a screen requires is granted by some role —
// so nothing a user can reach is unreachable, and nothing reachable is
// unexercisable. A self-service role is the deliberate exception: its accounts
// come from the app's own registration flow, not from the build.
func TestPlanGivesEveryReachableScopeARoleAndEveryRoleALogin(t *testing.T) {
	for _, name := range []string{expenseTracker, clinic, vendorPortal} {
		t.Run(name, func(t *testing.T) {
			doc, err := Parse(fixture(t, name))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			plan := Plan(doc)

			granted := map[string]bool{}
			for _, handles := range plan.Grants {
				for _, handle := range handles {
					granted[handle] = true
				}
			}
			for _, screen := range doc.Screens {
				if handle, required := screen.RequiresHandle(); required && !granted[handle] {
					t.Errorf("screen %q requires %q, which no role grants", screen.Screen, handle)
				}
			}

			withLogin := map[string]bool{}
			for _, u := range plan.Users {
				for _, role := range u.Roles {
					withLogin[strings.ToLower(role)] = true
				}
			}
			for _, role := range doc.Roles {
				if role.NeedsTestUser() && !withLogin[strings.ToLower(role.Name)] {
					t.Errorf("role %q owes a login and the plan carries none", role.Name)
				}
			}
		})
	}
}

func TestClientScopesAreTheOIDCScopesPlusTheWholeCatalog(t *testing.T) {
	doc, err := Parse(fixture(t, expenseTracker))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got := ClientScopes(doc)
	if !strings.HasPrefix(got, "openid profile email group ou ") {
		t.Fatalf("client scopes must start with the OIDC scopes: %q", got)
	}
	for _, handle := range CatalogHandles(doc) {
		if !slices.Contains(strings.Fields(got), handle) {
			t.Fatalf("client scopes %q omit the catalog handle %q", got, handle)
		}
	}
}

func TestRoleSlug(t *testing.T) {
	cases := map[string]string{
		"Compliance Admin": "compliance-admin",
		"  Viewer  ":       "viewer",
		"Ops/Support":      "ops-support",
		"---":              "role",
	}
	for in, want := range cases {
		if got := RoleSlug(in); got != want {
			t.Errorf("RoleSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

// `"My Claims"` in security.json and `screen MyClaims` in the DSL are the same
// screen: the grammar takes no space, the design file is written for a reader,
// and neither format changes.
func TestNormalizeScreenName(t *testing.T) {
	for _, pair := range [][2]string{
		{"My Claims", "MyClaims"},
		{"Submit Claim", "submit-claim"},
		{"Front desk", "FrontDesk"},
	} {
		if NormalizeScreenName(pair[0]) != NormalizeScreenName(pair[1]) {
			t.Errorf("%q and %q must normalize alike, got %q and %q",
				pair[0], pair[1], NormalizeScreenName(pair[0]), NormalizeScreenName(pair[1]))
		}
	}
	if NormalizeScreenName("Reports") == NormalizeScreenName("Report") {
		t.Error("normalization must not collapse two different screen names")
	}
}
