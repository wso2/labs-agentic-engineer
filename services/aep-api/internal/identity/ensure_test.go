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

package identity

// ensure_test.go — the build-time ensure, against in-memory fakes.
//
// The ensure mints credentials with no model in the loop, so what it must be is
// PREDICTABLE: the same design ensured twice does nothing the second time, and
// a design that names something the platform did not create touches nothing.
// Those two properties get the longest tests and the loudest names; everything
// else here exists to keep them honest.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/securityspec"
)

const (
	testOrg     = "org-acme"
	testProject = "proj-expenses"
	testTag     = "v1"
)

// ---- fixtures -------------------------------------------------------------

// userFixture is one authored `testUsers[]` entry.
type userFixture struct{ username, role string }

// rolesJSON renders a minimal security.json v2 that the real securityspec
// schema accepts. Building it rather than pasting literals keeps every case one
// line of intent, and keeps the fixtures honest: a schema change breaks these
// tests instead of letting them ensure a document the platform would reject.
//
// Each name is a project role, and `groupFor` is the org group it is assigned
// to. The two names cannot be the same string — securityspec refuses a role that
// is also a declared group, because one is project-scoped and the other org-wide
// — and the group has to be DECLARED, because an `assignTo` that the directory
// cannot resolve and `groups[]` does not introduce fails the gate.
//
// rolesJSONReusingGroups is the other legal shape; see below.
func rolesJSON(t *testing.T, roles []string, users ...userFixture) string {
	t.Helper()
	return buildRolesJSON(t, roles, true, users...)
}

// rolesJSONReusingGroups assigns each role to a group the document does NOT
// declare — the Vendor Portal's reused `Finance`. It is legal exactly when the
// org directory already holds that group, so a caller must SEED it on the fake;
// an assignTo resolving to nothing is the typo case and fails the gate.
//
// Here the group carries the role's own name, which is legal only because it is
// not declared.
func rolesJSONReusingGroups(t *testing.T, roles []string, users ...userFixture) string {
	t.Helper()
	return buildRolesJSON(t, roles, false, users...)
}

// groupFor is the org group the declaring fixture assigns a role to.
func groupFor(role string) string { return role + "s" }

func buildRolesJSON(t *testing.T, roles []string, declareGroups bool, users ...userFixture) string {
	t.Helper()
	roleEntries := make([]any, 0, len(roles))
	groupEntries := make([]any, 0, len(roles))
	for _, name := range roles {
		group := name
		if declareGroups {
			group = groupFor(name)
			groupEntries = append(groupEntries, map[string]any{
				"name": group, "description": "Everyone who is a " + name + ".",
			})
		}
		roleEntries = append(roleEntries, map[string]any{
			"name": name, "description": name + " may read.", "stories": []int{1},
			"grants":   []string{"claims:read"},
			"assignTo": []string{group},
		})
	}
	userEntries := make([]any, 0, len(users))
	for _, u := range users {
		userEntries = append(userEntries, map[string]any{"username": u.username, "roles": []string{u.role}})
	}
	doc := map[string]any{
		"version": 2,
		"permissions": []any{map[string]any{
			"resource": "claims", "component": "expense-api",
			"actions": []any{map[string]any{"handle": "read", "ownership": "own"}},
		}},
		"groups":    groupEntries,
		"roles":     roleEntries,
		"screens":   []any{},
		"testUsers": userEntries,
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	// A fixture the platform's own parser rejects would make every assertion
	// below vacuous, so it is checked here rather than discovered as a passing
	// "declared but errored" case.
	if _, err := securityspec.Parse(raw); err != nil {
		t.Fatalf("fixture is not a valid security.json: %v", err)
	}
	return string(raw)
}

// testScope is the (org, environment) every row this harness writes is keyed
// by — the pair the resolver picks for testOrg.
var testScope = Scope{OrgID: testOrg, Environment: testEnvironment}

// harness is one ensure wired to fresh fakes.
type harness struct {
	dir     *fakeDirectory
	targets *fakeTargets
	store   *fakeStore
	design  *fakeDesign
	svc     *EnsureService
}

func newHarness(doc string) *harness {
	dir := newFakeDirectory()
	h := &harness{
		dir: dir, targets: newFakeTargets(dir),
		store: newFakeStore(), design: &fakeDesign{bundle: map[string]string{}},
	}
	if doc != "" {
		h.design.bundle[securityspec.BundleKey] = doc
	}
	h.svc = NewEnsureService(h.targets, h.store, h.design)
	return h
}

// run ensures the harness's design and fails the test on error.
func (h *harness) run(t *testing.T) Result {
	t.Helper()
	result, declared, err := h.svc.EnsureForTag(context.Background(), testOrg, testProject, testTag)
	if err != nil {
		t.Fatalf("EnsureForTag: %v", err)
	}
	if !declared {
		t.Fatalf("EnsureForTag reported no roles document for a design that carries one")
	}
	return result
}

// setDoc swaps in the next version of the design, for the rebuild cases.
func (h *harness) setDoc(doc string) { h.design.bundle[securityspec.BundleKey] = doc }

func contains(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// ---- 1-3: what "declared" means -------------------------------------------

// A project with no roles document has no sign-in and nothing to ensure. The
// ensure must not touch the directory, and must not report the design as
// declaring roles — the caller mints a dispatch-holding gate off `declared`.
func TestEnsureForTagIsNotDeclaredWhenTheDesignCarriesNoRolesDocument(t *testing.T) {
	cases := map[string]map[string]string{
		"key absent":     {"design.md": "# design"},
		"key empty":      {securityspec.BundleKey: ""},
		"key whitespace": {securityspec.BundleKey: "  \n\t "},
	}
	for name, bundle := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness("")
			h.design.bundle = bundle

			result, declared, err := h.svc.EnsureForTag(context.Background(), testOrg, testProject, testTag)
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if declared {
				t.Fatalf("declared = true for a design with no roles document")
			}
			if len(h.dir.calls) != 0 {
				t.Fatalf("directory calls = %v, want none", h.dir.calls)
			}
			if len(h.store.replaceCalls) != 0 {
				t.Fatalf("refs rewritten %d times, want none", len(h.store.replaceCalls))
			}
			if result.Summary() != "Nothing to provision — the design declares no roles." {
				t.Fatalf("summary = %q", result.Summary())
			}
		})
	}
}

// A security.json that is present but broken is declared AND an error. The split
// is load-bearing: the caller holds dispatch behind a gate only when the design
// declares roles, so folding these together would let a broken document ship.
func TestEnsureForTagReportsDeclaredWhenTheRolesDocumentDoesNotParse(t *testing.T) {
	for name, doc := range map[string]string{
		"not JSON":         `{`,
		"schema violation": `{"version": 2, "permissions": [], "groups": [], "roles": [], "screens": [], "testUsers": []}`,
		"a v1 document": rolesJSONRaw(`{"version":1,"coldStartRole":null,"publicComponents":[],` +
			`"roles":[{"name":"Viewer","description":"d","stories":[1],"grantedBy":"g",` +
			`"permissions":[{"component":"api","actions":["read"]}]}],"testUsers":[],` +
			`"thunder":{"name":"Expense Tracker","type":"browser"}}`),
		"a grant naming no catalog handle": rolesJSONRaw(`{"version":2,` +
			`"permissions":[{"resource":"claims","component":"api","actions":[{"handle":"read","ownership":"own"}]}],` +
			`"groups":[],"roles":[{"name":"Viewer","description":"d","stories":[1],` +
			`"grants":["claims:audit"],"assignTo":["Viewers"]}],"screens":[],"testUsers":[]}`),
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(doc)

			_, declared, err := h.svc.EnsureForTag(context.Background(), testOrg, testProject, testTag)
			if err == nil {
				t.Fatalf("err = nil, want a parse refusal")
			}
			if !declared {
				t.Fatalf("declared = false — the caller would then NOT hold the build for a broken security.json")
			}
			if !strings.Contains(err.Error(), securityspec.Path) {
				t.Fatalf("error %q does not name %s", err, securityspec.Path)
			}
			if len(h.dir.writes()) != 0 {
				t.Fatalf("directory writes = %v, want none for a document that never parsed", h.dir.writes())
			}
		})
	}
}

// rolesJSONRaw is an identity helper that keeps the hand-written literals above
// aligned with the generated ones.
func rolesJSONRaw(s string) string { return s }

// A design that cannot be READ is an error but NOT declared: a transient git
// failure must not hold the build of a project that has no roles at all.
func TestEnsureForTagIsNotDeclaredWhenTheDesignCannotBeRead(t *testing.T) {
	h := newHarness("")
	h.design.err = errDesignUnavailable

	_, declared, err := h.svc.EnsureForTag(context.Background(), testOrg, testProject, testTag)
	if err == nil {
		t.Fatalf("err = nil, want the read failure")
	}
	if declared {
		t.Fatalf("declared = true — a project with no roles would be held by a git hiccup")
	}
	if len(h.dir.calls) != 0 {
		t.Fatalf("directory calls = %v, want none", h.dir.calls)
	}
}

// ---- 4: the fresh build ---------------------------------------------------

// A fresh directory: every account is created first, then every role is created
// COMPLETE with its members in one call. Creating a group empty and then adding
// members would change its id on its very first build for no reason, since the
// IdP's only membership write is a delete-and-recreate.
func TestEnsureCreatesEachRoleCompleteWithItsMembersInOneCall(t *testing.T) {
	h := newHarness(rolesJSON(t, []string{"Viewer", "Compliance Admin"},
		userFixture{"test-viewer", "Viewer"}))

	result := h.run(t)

	if len(result.GroupsCreated) != 2 || !contains(result.GroupsCreated, groupFor("Viewer")) ||
		!contains(result.GroupsCreated, groupFor("Compliance Admin")) {
		t.Fatalf("GroupsCreated = %v, want a group for each role", result.GroupsCreated)
	}
	// The design named a user only for Viewer; the platform supplies the other.
	if len(result.UsersCreated) != 2 || !contains(result.UsersCreated, "test-viewer") ||
		!contains(result.UsersCreated, "test-compliance-admin") {
		t.Fatalf("UsersCreated = %v, want the authored and the supplied account", result.UsersCreated)
	}
	if n := h.dir.countOp("AddMembers"); n != 0 {
		t.Fatalf("AddMembers called %d times on a fresh build — the members belong in CreateGroup", n)
	}

	// Each group came out of ONE create, already holding its member.
	for _, role := range []string{"Viewer", "Compliance Admin"} {
		if got := len(h.dir.memberSet(groupFor(role))); got != 1 {
			t.Fatalf("group %q has %d members, want its one test user", groupFor(role), got)
		}
	}

	// Order: every CreateUser precedes every CreateGroup, because the member ids
	// have to be known before a group can be created complete.
	writes := h.dir.writes()
	lastUser, firstGroup := -1, len(writes)
	for i, c := range writes {
		switch c.Op {
		case "CreateUser":
			lastUser = i
		case "CreateGroup":
			if i < firstGroup {
				firstGroup = i
			}
		}
	}
	if lastUser > firstGroup {
		t.Fatalf("a group was created before an account it holds: %v", writes)
	}

	// And each created group carries exactly the account created for it.
	for _, c := range writes {
		if c.Op != "CreateGroup" {
			continue
		}
		if len(c.Members) != 1 {
			t.Fatalf("CreateGroup %q carried members %v, want exactly one", c.Target, c.Members)
		}
	}
}

// ---- 5: idempotence -------------------------------------------------------

// The single most important property: ensuring an unchanged design a second
// time changes nothing at all. A re-run that recreated an account would hand a
// human a credential that no longer works; one that recreated a group would
// churn an id OpenChoreo's bindings were rendered against.
func TestEnsureIsIdempotent(t *testing.T) {
	h := newHarness(rolesJSON(t, []string{"Viewer", "Compliance Admin"},
		userFixture{"test-viewer", "Viewer"}))

	first := h.run(t)
	groupIDs := map[string]string{}
	for name, g := range h.dir.groups {
		groupIDs[name] = g.ID
	}
	passwords := map[string]string{}
	for name, pw := range h.store.passwords {
		passwords[name] = pw
	}
	h.dir.calls = nil

	second := h.run(t)

	if len(second.GroupsCreated) != 0 || len(second.UsersCreated) != 0 {
		t.Fatalf("second run created %v / %v, want nothing", second.GroupsCreated, second.UsersCreated)
	}
	if len(second.GroupsReused) != len(first.GroupsCreated) || len(second.UsersReused) != len(first.UsersCreated) {
		t.Fatalf("second run reused %v / %v, want everything the first run made",
			second.GroupsReused, second.UsersReused)
	}
	// The ensure still ASKS the directory to add the members (it cannot know
	// they are already there without asking), but nothing may be written: no
	// create, no membership edit, no password.
	if writes := h.dir.writes(); len(writes) != 0 {
		t.Fatalf("second run wrote to the directory: %v", writes)
	}
	for name, g := range h.dir.groups {
		if g.ID != groupIDs[name] {
			t.Fatalf("group %q id churned %s -> %s on an unchanged re-run", name, groupIDs[name], g.ID)
		}
	}
	for name, pw := range h.store.passwords {
		if pw != passwords[name] {
			t.Fatalf("password for %q was rotated on an unchanged re-run", name)
		}
	}
	for _, u := range h.store.users {
		if u.RotatedAt != nil {
			t.Fatalf("account %q was marked rotated on an unchanged re-run", u.Username)
		}
	}
	// The refs are rewritten wholesale every build by design, but they must land
	// on the same set.
	if len(h.store.replaceCalls) != 2 {
		t.Fatalf("ReplaceProjectRefs called %d times, want once per run", len(h.store.replaceCalls))
	}
	if len(h.store.replaceCalls[0]) != len(h.store.replaceCalls[1]) {
		t.Fatalf("refs changed across an unchanged re-run: %v -> %v",
			h.store.replaceCalls[0], h.store.replaceCalls[1])
	}
}

// ---- 6: the pre-existing-role safety property -----------------------------

// PRE-EXISTING ROLE. A group the platform did not create is left ENTIRELY
// alone: not written to, and no member enrolled into it.
//
// `Administrators` is the case that matters. setup-aep.sh maps that group to
// OpenChoreo's admin role, so a design that quite reasonably declares a role
// called `Administrators` must not get a platform-made test account — whose
// password the platform hands to a validation runner — into it. The rule is
// "the platform enrols only into roles it created", keyed off the presence of
// an idp_roles row, so every hand-made group is protected without a denylist.
//
// The account is still created, and holds the PROJECT role directly: see
// TestEnsureBindsARoleToTheAccountWhenItsGroupIsNotOwned for that half. The two
// are not in tension — the project role grants only what this project's catalog
// declares, while membership in somebody else's group is authority the platform
// has no business handing out.
func TestEnsureLeavesAPreExistingDirectoryGroupAlone(t *testing.T) {
	h := newHarness(rolesJSONReusingGroups(t, []string{"Administrators"}))
	// On the directory, but with NO idp_roles row: somebody else made it. The
	// document does not declare it — it names it in an assignTo — which is the
	// only shape that can mean "reuse the group the org already has".
	seeded := h.dir.seedGroup("Administrators", "usr-existing-admin")

	result := h.run(t)

	if !contains(result.GroupsPreExisting, "Administrators") {
		t.Fatalf("RolesPreExisting = %v, want Administrators", result.GroupsPreExisting)
	}
	if contains(result.GroupsCreated, "Administrators") || contains(result.GroupsReused, "Administrators") {
		t.Fatalf("Administrators was claimed: created=%v reused=%v", result.GroupsCreated, result.GroupsReused)
	}
	if n := h.dir.countOp("CreateGroup"); n != 0 {
		t.Fatalf("CreateGroup called %d times on a group that already exists", n)
	}
	if n := h.dir.countOp("AddMembers"); n != 0 {
		t.Fatalf("AddMembers called %d times on a group the platform did not create", n)
	}
	// Nothing at all was written to the group: same id, same members.
	if got := h.dir.groups["administrators"]; got.ID != seeded.ID {
		t.Fatalf("group id changed %s -> %s", seeded.ID, got.ID)
	}
	if got := h.dir.memberSet("Administrators"); len(got) != 1 || got[0] != "usr-existing-admin" {
		t.Fatalf("members = %v, want only the one that was already there", got)
	}
	// The platform also recorded no ownership over it — the next build must
	// reach the same conclusion.
	if _, recorded := h.store.role(testScope, "administrators"); recorded {
		t.Fatalf("an idp_roles row was written for a group the platform did not create")
	}
	// The account exists and is NOT reported as skipped: it holds its role
	// through the direct binding, not through the group.
	if !contains(result.UsersCreated, "test-administrators") {
		t.Fatalf("UsersCreated = %v, want test-administrators", result.UsersCreated)
	}
	if contains(result.UsersSkipped, "test-administrators") {
		t.Fatalf("UsersSkipped = %v — the account is grantable directly", result.UsersSkipped)
	}
	// But it is nowhere near the group.
	for _, id := range h.dir.memberSet("Administrators") {
		if id == h.dir.users["test-administrators"].ID {
			t.Fatalf("the test account was enrolled into a group the platform does not own")
		}
	}
}

// ---- 7: the refused-account safety property -------------------------------

// REFUSED ACCOUNT. A username that exists on the directory but has no
// test_users row belongs to somebody. It is refused, never adopted: no create,
// no password reset, no enrolment. Otherwise a design naming `jsmith` would
// reset a real person's login and hand it to a validation runner.
func TestEnsureRefusesAnAccountThePlatformDoesNotOwn(t *testing.T) {
	h := newHarness(rolesJSON(t, []string{"Viewer"}, userFixture{"jsmith", "Viewer"}))
	person := h.dir.seedUser("jsmith")

	result := h.run(t)

	if !contains(result.UsersRefused, "jsmith") {
		t.Fatalf("UsersRefused = %v, want jsmith", result.UsersRefused)
	}
	if !result.HasRefusals() {
		t.Fatalf("HasRefusals = false with a refused account")
	}
	if contains(result.UsersCreated, "jsmith") || contains(result.UsersReused, "jsmith") {
		t.Fatalf("jsmith was adopted: created=%v reused=%v", result.UsersCreated, result.UsersReused)
	}
	if n := h.dir.countOp("CreateUser"); n != 0 {
		t.Fatalf("CreateUser called %d times for an account that already exists", n)
	}
	if n := h.dir.countOp("SetUserPassword"); n != 0 {
		t.Fatalf("SetUserPassword called %d times on a real person's account", n)
	}
	if pw, held := h.dir.passwords[person.ID]; held {
		t.Fatalf("a password was written for jsmith (%q)", pw)
	}
	if _, owned := h.store.user(testScope, "jsmith"); owned {
		t.Fatalf("a test_users row was written for an account the platform does not own")
	}
	// Refusal is per account and does not stop the pass: the role is still made.
	if !contains(result.GroupsCreated, groupFor("Viewer")) {
		t.Fatalf("GroupsCreated = %v — one refused account blocked the group around it", result.GroupsCreated)
	}
	// And the refused account is nowhere near the group.
	for _, id := range h.dir.memberSet(groupFor("Viewer")) {
		if id == person.ID {
			t.Fatalf("a refused account was enrolled into %q", groupFor("Viewer"))
		}
	}
	if got := h.dir.memberSet(groupFor("Viewer")); len(got) != 0 {
		t.Fatalf("%s members = %v, want none — its only planned user was refused", groupFor("Viewer"), got)
	}
	if !strings.Contains(result.Summary(), "jsmith") {
		t.Fatalf("summary does not surface the refusal: %q", result.Summary())
	}
}

// ---- 8: a rebuild that adds a member --------------------------------------

// v2 adds a second test user to a role the platform already created. The member
// is added, and the cached group id is refreshed — Thunder recreates the group
// on a membership edit, so a stale cached id would point at a group that no
// longer exists.
func TestEnsureAddsANewMemberAndRefreshesTheCachedGroupID(t *testing.T) {
	h := newHarness(rolesJSON(t, []string{"Viewer"}, userFixture{"test-viewer", "Viewer"}))
	h.run(t)
	firstGroupID := h.dir.groups["viewers"].ID
	if row, _ := h.store.role(testScope, groupFor("Viewer")); row.ThunderGroupID != firstGroupID {
		t.Fatalf("cached group id = %q, want %q after the first build", row.ThunderGroupID, firstGroupID)
	}
	h.dir.calls = nil

	h.setDoc(rolesJSON(t, []string{"Viewer"},
		userFixture{"test-viewer", "Viewer"}, userFixture{"second-viewer", "Viewer"}))
	result := h.run(t)

	if !contains(result.UsersCreated, "second-viewer") {
		t.Fatalf("UsersCreated = %v, want the new account", result.UsersCreated)
	}
	newAccount := h.dir.users["second-viewer"]
	var adds []dirCall
	for _, c := range h.dir.calls {
		if c.Op == "AddMembers" {
			adds = append(adds, c)
		}
	}
	if len(adds) != 1 {
		t.Fatalf("AddMembers calls = %d, want exactly one", len(adds))
	}
	if len(adds[0].Members) != 1 || adds[0].Members[0] != newAccount.ID {
		t.Fatalf("AddMembers added %v, want only the new account %q", adds[0].Members, newAccount.ID)
	}
	// The membership edit recreated the group under a new id...
	recreatedID := h.dir.groups["viewers"].ID
	if recreatedID == firstGroupID {
		t.Fatalf("the fake directory did not recreate the group — the case under test did not happen")
	}
	// ...and the store's cache followed it.
	if row, _ := h.store.role(testScope, groupFor("Viewer")); row.ThunderGroupID != recreatedID {
		t.Fatalf("cached group id = %q, want the recreated %q", row.ThunderGroupID, recreatedID)
	}
	if got := len(h.dir.memberSet(groupFor("Viewer"))); got != 2 {
		t.Fatalf("%s holds %d members, want both accounts", groupFor("Viewer"), got)
	}
	// The existing account was neither recreated nor re-passworded.
	if n := h.dir.countOp("CreateUser"); n != 1 {
		t.Fatalf("CreateUser called %d times, want only the new account", n)
	}
}

// An account the platform owns whose facts moved — the design gave it a
// different role, or the directory handed it a new id — has those two columns
// refreshed, and NOTHING else. In particular the stored seal is not rewritten:
// decrypt-then-reseal would change the ciphertext of a password that did not
// change, and an account whose seal is missing would fail the whole build on the
// re-seal rather than simply having its role corrected.
//
// The publication pass reads the password back (once per referenced account, to
// put it in the gate ticket) and that is a different thing: it opens the seal
// without writing one.
func TestEnsureRefreshesAReusedAccountsFactsWithoutTouchingItsPassword(t *testing.T) {
	h := newHarness(rolesJSON(t, []string{"Viewer"}, userFixture{"test-viewer", "Viewer"}))
	h.run(t)
	sealed, _ := h.store.password(testScope, "test-viewer")
	if sealed == "" {
		t.Fatalf("the first build sealed no password, so there is nothing to protect")
	}

	// Thunder handed the account a new id, and v2 moves it to another role.
	h.dir.users["test-viewer"] = DirectoryAccount{
		ID: "usr-recreated", Username: "test-viewer", Email: "test-viewer@test-users.invalid",
	}
	h.setDoc(rolesJSON(t, []string{"Viewer", "Auditor"}, userFixture{"test-viewer", "Auditor"}))
	reveals, upserts := h.store.revealCalls, h.store.upsertUserCalls

	result := h.run(t)

	if !contains(result.UsersReused, "test-viewer") {
		t.Fatalf("UsersReused = %v, want the account the platform owns", result.UsersReused)
	}
	row, _ := h.store.user(testScope, "test-viewer")
	if row.RoleName != "Auditor" {
		t.Fatalf("role_name = %q, want the role v2 gave it", row.RoleName)
	}
	if row.ThunderUserID != "usr-recreated" {
		t.Fatalf("thunder_user_id = %q, want the directory's current id", row.ThunderUserID)
	}
	// Two accounts are referenced at v2, so publication opens two seals. What
	// must NOT happen is a reveal that feeds a write — pinned by the unchanged
	// ciphertext below and by the single whole-row upsert.
	if got := h.store.revealCalls - reveals; got != 2 {
		t.Fatalf("seals opened %d times, want one per referenced account (2)", got)
	}
	// One whole-row write is expected — the supplied account Viewer now needs —
	// but not one for the account whose facts merely moved.
	if got := h.store.upsertUserCalls - upserts; got != 1 {
		t.Fatalf("test_users rewritten %d times, want only the newly created account", got)
	}
	if pw, _ := h.store.password(testScope, "test-viewer"); pw != sealed {
		t.Fatalf("the sealed password changed under a facts-only update")
	}
}

// The reason the facts update exists, stated as a test: an account whose sealed
// password is absent still builds. The reveal-then-reseal path would fail the
// whole run here with ErrNoPassword, for a role correction that never needed
// the credential.
func TestEnsureRefreshesFactsForAnAccountWithNoSealedPassword(t *testing.T) {
	h := newHarness(rolesJSON(t, []string{"Viewer"}, userFixture{"test-viewer", "Viewer"}))
	h.run(t)

	// A row written before the seal existed, or one whose password was never
	// generated here.
	h.store.setPassword(testScope, "test-viewer", "")
	h.dir.users["test-viewer"] = DirectoryAccount{
		ID: "usr-recreated", Username: "test-viewer", Email: "test-viewer@test-users.invalid",
	}

	result := h.run(t)

	if !contains(result.UsersReused, "test-viewer") {
		t.Fatalf("UsersReused = %v", result.UsersReused)
	}
	if row, _ := h.store.user(testScope, "test-viewer"); row.ThunderUserID != "usr-recreated" {
		t.Fatalf("thunder_user_id = %q, want the refreshed id", row.ThunderUserID)
	}
}

// ---- 9: a role deleted out from under us ----------------------------------

// A role recorded in idp_roles but gone from the directory is recreated — and
// the provenance on the surviving row is kept, so the console still credits the
// project that first declared the role rather than whoever happened to rebuild.
func TestEnsureRecreatesAVanishedRoleAndKeepsItsOriginalProvenance(t *testing.T) {
	h := newHarness(rolesJSON(t, []string{"Viewer"}))
	h.store.putRole(testScope, IdPRole{
		Name: groupFor("Viewer"), ThunderGroupID: "grp-deleted", Description: "first description",
		CreatedByOrg: "org-first", CreatedByProject: "proj-first",
	})

	result := h.run(t)

	if !contains(result.GroupsCreated, groupFor("Viewer")) {
		t.Fatalf("GroupsCreated = %v, want the recreated group", result.GroupsCreated)
	}
	row, _ := h.store.role(testScope, groupFor("Viewer"))
	if row.ThunderGroupID == "grp-deleted" || row.ThunderGroupID != h.dir.groups["viewers"].ID {
		t.Fatalf("cached group id = %q, want the newly created %q", row.ThunderGroupID, h.dir.groups["viewers"].ID)
	}
	if row.CreatedByOrg != "org-first" || row.CreatedByProject != "proj-first" {
		t.Fatalf("provenance = %s/%s, want the original org-first/proj-first — the rebuilder took the role over",
			row.CreatedByOrg, row.CreatedByProject)
	}
}

// ---- 10: the project references -------------------------------------------

// Exactly one ref per USABLE planned user, with the supplied flag the console
// renders. A refused account produces no ref: the project does not reference an
// account the platform did not provision for it.
func TestEnsureWritesOneRefPerUsablePlannedUser(t *testing.T) {
	h := newHarness(rolesJSON(t, []string{"Viewer", "Compliance Admin", "Auditor"},
		userFixture{"test-viewer", "Viewer"}, userFixture{"jsmith", "Auditor"}))
	h.dir.seedUser("jsmith") // refused: a real person's account

	h.run(t)

	if len(h.store.replaceCalls) != 1 {
		t.Fatalf("ReplaceProjectRefs called %d times, want once", len(h.store.replaceCalls))
	}
	byUser := map[string]TestUserRef{}
	for _, r := range h.store.replaceCalls[0] {
		if _, dup := byUser[r.Username]; dup {
			t.Fatalf("username %q referenced twice", r.Username)
		}
		byUser[r.Username] = r
	}
	// Viewer's authored user, Compliance Admin's supplied one; Auditor's only
	// planned user was refused, so Auditor contributes nothing.
	want := map[string]TestUserRef{
		"test-viewer":           {Username: "test-viewer", RoleName: "Viewer", Supplied: false},
		"test-compliance-admin": {Username: "test-compliance-admin", RoleName: "Compliance Admin", Supplied: true},
	}
	if len(byUser) != len(want) {
		t.Fatalf("refs = %v, want exactly %d (a refused account contributes none)", byUser, len(want))
	}
	for name, expect := range want {
		got, ok := byUser[name]
		if !ok {
			t.Fatalf("no ref for %q; got %v", name, byUser)
		}
		if got.RoleName != expect.RoleName || got.ColdStart != expect.ColdStart || got.Supplied != expect.Supplied {
			t.Fatalf("ref for %q = %+v, want role=%q coldStart=%v supplied=%v",
				name, got, expect.RoleName, expect.ColdStart, expect.Supplied)
		}
		if got.OrgID != testOrg || got.ProjectID != testProject {
			t.Fatalf("ref for %q is scoped to %s/%s", name, got.OrgID, got.ProjectID)
		}
	}
	if _, refused := byUser["jsmith"]; refused {
		t.Fatalf("a refused account was referenced by the project")
	}
	// Supplied is the platform naming the account, not the design.
	if byUser["test-viewer"].Supplied {
		t.Fatalf("an authored username was marked Supplied")
	}
}

// A role dropped from v2 stops being referenced by this project, while the
// directory object itself stands — the additive-only rule.
func TestEnsureStopsReferencingARoleDroppedFromTheDesign(t *testing.T) {
	h := newHarness(rolesJSON(t, []string{"Viewer", "Auditor"}))
	h.run(t)

	h.setDoc(rolesJSON(t, []string{"Viewer"}))
	h.run(t)

	refs, err := h.store.ListProjectRefs(context.Background(), testScope, testProject)
	if err != nil {
		t.Fatalf("ListProjectRefs: %v", err)
	}
	for _, r := range refs {
		if r.RoleName == "Auditor" {
			t.Fatalf("the project still references the dropped role: %+v", r)
		}
	}
	if len(refs) != 1 {
		t.Fatalf("refs = %v, want only the surviving role", refs)
	}
	// The GROUP behind the dropped role stands, and so does the platform's
	// record of it: a group is shared and additive, however many projects stop
	// naming it. (The project ROLE itself is the other way round — see
	// TestEnsureDeletesAProjectRoleTheTagNoLongerDeclares.)
	if _, stillThere := h.dir.groups["auditors"]; !stillThere {
		t.Fatalf("dropping a role from the design deleted the shared directory group")
	}
	if _, stillRecorded := h.store.role(testScope, groupFor("Auditor")); !stillRecorded {
		t.Fatalf("dropping a role from the design forgot the platform's ownership of the group")
	}
}

// ---- 11-12: passwords ------------------------------------------------------

// A new account's password is sealed on the way in and is retrievable
// afterwards, because the IdP will not give it back: GET /users/{id} returns no
// password field, so a credential the platform failed to keep is gone.
func TestEnsureSealsARetrievableDistinctPasswordForEachNewAccount(t *testing.T) {
	h := newHarness(rolesJSON(t, []string{"Viewer", "Compliance Admin"}))

	h.run(t)

	ctx := context.Background()
	seen := map[string]string{}
	for _, username := range []string{"test-viewer", "test-compliance-admin"} {
		pw, err := h.store.RevealTestUserPassword(ctx, testScope, username)
		if err != nil {
			t.Fatalf("RevealTestUserPassword(%q): %v", username, err)
		}
		if strings.TrimSpace(pw) == "" {
			t.Fatalf("password for %q is empty", username)
		}
		if other, clash := seen[pw]; clash {
			t.Fatalf("%q and %q were given the same password", other, username)
		}
		seen[pw] = username
		// The password the store kept is the one the directory was given, or the
		// account exists with a login nobody holds.
		account := h.dir.users[username]
		if h.dir.passwords[account.ID] != pw {
			t.Fatalf("stored password for %q does not match what the directory was given", username)
		}
	}
}

// ---- publication -----------------------------------------------------------
//
// The logins go into the roles gate's closing comment, and that comment is where
// a validation agent reads the credentials it signs in with. So "what the ensure
// publishes" is a contract, not a log line.

// A rebuild creates nothing and its ticket must still carry every login. This is
// the property that makes the ticket usable at all: keyed to what CHANGED, v2's
// comment would list no accounts and v2's validation could sign in as nobody.
func TestEnsurePublishesEveryLoginOnARebuildNotOnlyTheNewOnes(t *testing.T) {
	h := newHarness(rolesJSON(t, []string{"Viewer"}, userFixture{"test-viewer", "Viewer"}))
	first := h.run(t)
	if len(first.Credentials) != 1 || first.Credentials[0].Username != "test-viewer" {
		t.Fatalf("first build published %+v, want the account it created", first.Credentials)
	}
	firstPassword := first.Credentials[0].Password

	h.setDoc(rolesJSON(t, []string{"Viewer", "Auditor"},
		userFixture{"test-viewer", "Viewer"}, userFixture{"test-auditor", "Auditor"}))
	second := h.run(t)

	if !contains(second.UsersReused, "test-viewer") {
		t.Fatalf("UsersReused = %v, want the account v1 created", second.UsersReused)
	}
	if len(second.Credentials) != 2 {
		t.Fatalf("rebuild published %+v, want a login for BOTH accounts", second.Credentials)
	}
	// Name-ordered, so this is positional.
	if second.Credentials[0].Username != "test-auditor" || second.Credentials[1].Username != "test-viewer" {
		t.Fatalf("credentials are not username-ordered: %+v", second.Credentials)
	}
	if got := second.Credentials[1].Password; got != firstPassword {
		t.Fatalf("the reused account was published with %q, want the password it already signs in with", got)
	}
	if second.Credentials[0].Password == "" {
		t.Fatalf("the account created this run was published with no password")
	}
}

// v2 has NO cold start. Signed-in operations, self-service enrolment and the
// SPA's no-access state replace it, so no account is published as the one a
// caller holds before anybody grants them a role. The column and the flag
// survive on the row until the console stops reading them (phase 5); what must
// not survive is a login being SERVED as the cold-start answer.
func TestEnsurePublishesNoColdStartAccount(t *testing.T) {
	h := newHarness(rolesJSON(t, []string{"Viewer", "Auditor"},
		userFixture{"test-viewer", "Viewer"}, userFixture{"test-auditor", "Auditor"}))
	result := h.run(t)

	byName := map[string]Credential{}
	for _, c := range result.Credentials {
		byName[c.Username] = c
	}
	for name, cred := range byName {
		if cred.ColdStart {
			t.Errorf("account %q was published as cold-start, which v2 removed: %+v", name, cred)
		}
	}
	viewer := byName["test-viewer"]
	if len(viewer.Roles) != 1 || viewer.Roles[0] != "Viewer" {
		t.Errorf("roles = %v, want the role the account was enrolled into", viewer.Roles)
	}
	if len(viewer.Scopes) != 1 || viewer.Scopes[0] != "claims:read" {
		t.Errorf("scopes = %v, want the handles that role grants", viewer.Scopes)
	}
}

// An account the ensure would not touch must not be published. Publishing a
// refused username — one that belongs to a real person — would put a password
// beside somebody else's login in a ticket, for an account whose password the
// platform never set.
func TestEnsurePublishesNoLoginForARefusedOrSkippedAccount(t *testing.T) {
	h := newHarness(rolesJSON(t, []string{"Viewer"}, userFixture{"jsmith", "Viewer"}))
	// A real person's account, already on the directory and not ours.
	h.dir.users["jsmith"] = DirectoryAccount{ID: "usr-jsmith", Username: "jsmith"}
	result := h.run(t)

	if !contains(result.UsersRefused, "jsmith") {
		t.Fatalf("UsersRefused = %v, want the account the platform does not own", result.UsersRefused)
	}
	for _, c := range result.Credentials {
		if c.Username == "jsmith" {
			t.Fatalf("a refused account was published: %+v", c)
		}
	}
}

// A seal that cannot be opened costs the ROW its password, never the build and
// never the row. The account exists and is enrolled; only its publication is
// lost, and the ticket has to say so rather than print a blank that reads as a
// password-less login.
func TestEnsurePublishesAnEmptyPasswordRatherThanFailingWhenTheSealWontOpen(t *testing.T) {
	h := newHarness(rolesJSON(t, []string{"Viewer"}, userFixture{"test-viewer", "Viewer"}))
	h.store.failOn = map[string]error{"RevealTestUserPassword": errors.New("cipher key rotated")}

	result := h.run(t)

	if !contains(result.UsersCreated, "test-viewer") {
		t.Fatalf("UsersCreated = %v — the account itself must still be made", result.UsersCreated)
	}
	if len(result.Credentials) != 1 {
		t.Fatalf("credentials = %+v, want the row anyway", result.Credentials)
	}
	if result.Credentials[0].Password != "" {
		t.Fatalf("password = %q, want empty so the ticket can call it unavailable", result.Credentials[0].Password)
	}
}

// Summary() is the half of the result that reaches LOGS. A password must never
// be in it, however Credentials grows.
func TestSummaryCarriesNoPassword(t *testing.T) {
	h := newHarness(rolesJSON(t, []string{"Viewer"}, userFixture{"test-viewer", "Viewer"}))
	result := h.run(t)
	if len(result.Credentials) == 0 || result.Credentials[0].Password == "" {
		t.Fatalf("the fixture published no password, so this test proves nothing")
	}
	if strings.Contains(result.Summary(), result.Credentials[0].Password) {
		t.Fatalf("Summary() leaked a password:\n%s", result.Summary())
	}
}

// The published password lands in a markdown table cell, so it must not contain
// a character that can break out of one. This pins the generator, which is what
// lets the renderer skip escaping.
func TestGeneratedPasswordCarriesNoMarkdownDelimiter(t *testing.T) {
	for i := 0; i < 200; i++ {
		pw, err := generatePassword()
		if err != nil {
			t.Fatalf("generatePassword: %v", err)
		}
		if strings.ContainsAny(pw, "`|\n\r") {
			t.Fatalf("password %q contains a markdown-hostile character", pw)
		}
	}
}

// The password must be unguessable and unique per account; a fixed prefix keeps
// it acceptable to a mixed-case/digit/symbol password policy.
//
// Unguessable is the property that survives publication: the login is printed
// in the gate ticket on purpose, so the length is not buying secrecy — it is
// buying that a deterministic username plus a shared directory does not add up
// to an account anyone can walk into.
func TestGeneratePasswordIsDistinctAcrossManyAccounts(t *testing.T) {
	const n = 500
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		pw, err := generatePassword()
		if err != nil {
			t.Fatalf("generatePassword: %v", err)
		}
		if seen[pw] {
			t.Fatalf("generatePassword repeated a value after %d draws", i)
		}
		seen[pw] = true
		if len(pw) != passwordChars {
			t.Fatalf("password %q is %d characters, want %d", pw, len(pw), passwordChars)
		}
	}
}

// These passwords are READ — off the ticket, off the panel, and typed by hand in
// a walkthrough — so every character a reader could mistake for another one is
// deliberately absent. A drifted alphabet would put `1` beside `l` again with
// nothing failing.
func TestGeneratedPasswordAvoidsLookalikeCharacters(t *testing.T) {
	// Each of these is confusable with another character, so none may be drawn.
	for _, bad := range []string{"0", "O", "1", "l", "i", "I"} {
		if strings.Contains(passwordAlphabet, bad) {
			t.Errorf("alphabet contains the lookalike %q", bad)
		}
	}
	// The password lands in a markdown table cell and then in a shell export, so
	// the alphabet is lowercase and digits only — nothing that needs quoting,
	// escaping, or a shift key.
	for _, r := range passwordAlphabet {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') {
			t.Errorf("alphabet contains %q, which is neither a lowercase letter nor a digit", r)
		}
	}
	// Uniform sampling with no rejection loop only holds while 256 divides
	// evenly by the alphabet — otherwise the first characters are likelier.
	if 256%len(passwordAlphabet) != 0 {
		t.Fatalf("alphabet of %d biases the modulo draw", len(passwordAlphabet))
	}
	for i := 0; i < 200; i++ {
		pw, err := generatePassword()
		if err != nil {
			t.Fatalf("generatePassword: %v", err)
		}
		for _, r := range pw {
			if !strings.ContainsRune(passwordAlphabet, r) {
				t.Fatalf("password %q contains %q, which is outside the alphabet", pw, r)
			}
		}
	}
}

// The alphabet must carry no duplicate and must divide 256, or the modulo draw
// stops being uniform.
func TestPasswordAlphabetIsUniformlyDrawable(t *testing.T) {
	seen := map[rune]bool{}
	for _, r := range passwordAlphabet {
		if seen[r] {
			t.Errorf("%q appears twice, which makes it likelier than the rest", r)
		}
		seen[r] = true
	}
	if 256%len(passwordAlphabet) != 0 {
		t.Fatalf("alphabet of %d biases the modulo draw", len(passwordAlphabet))
	}
}

// ---- surrounding contract --------------------------------------------------

// testUserEmail must not be deliverable: these accounts should be incapable of
// receiving real mail. `.invalid` is reserved by RFC 2606 for exactly that.
func TestTestUserEmailIsUndeliverable(t *testing.T) {
	got := testUserEmail("test-viewer")
	if !strings.HasSuffix(got, ".invalid") {
		t.Fatalf("email = %q, want a reserved-undeliverable domain", got)
	}
	if !strings.HasPrefix(got, "test-viewer@") {
		t.Fatalf("email = %q, want the username as the local part", got)
	}
}

// Enabled is what the composition root uses to skip the whole feature on a
// stack with no IdP, so it must be false for every missing collaborator rather
// than panicking later.
func TestEnabledIsFalseWithoutEveryCollaborator(t *testing.T) {
	full := newHarness(rolesJSON(t, []string{"Viewer"}))
	if !full.svc.Enabled() {
		t.Fatalf("Enabled = false with every collaborator wired")
	}
	cases := map[string]*EnsureService{
		"nil service":   nil,
		"no resolver":   NewEnsureService(nil, full.store, full.design),
		"no store":      NewEnsureService(full.targets, nil, full.design),
		"no design":     NewEnsureService(full.targets, full.store, nil),
		"nothing wired": NewEnsureService(nil, nil, nil),
	}
	for name, svc := range cases {
		if svc.Enabled() {
			t.Fatalf("Enabled = true for %s", name)
		}
	}
}

// Summary is the gate's closing comment. It reports only what happened, and
// nothing at all when nothing did.
func TestResultSummaryReportsOnlyWhatHappened(t *testing.T) {
	r := Result{
		GroupsCreated: []string{"Viewer"}, GroupsPreExisting: []string{"Administrators"},
		UsersRefused: []string{"jsmith"},
	}
	got := r.Summary()
	for _, want := range []string{"Groups created: Viewer", "Administrators", "jsmith"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary %q does not mention %q", got, want)
		}
	}
	if strings.Contains(got, "Groups reused") || strings.Contains(got, "Test users created") {
		t.Fatalf("summary %q reports outcomes that did not occur", got)
	}
	if (Result{}).HasRefusals() {
		t.Fatalf("HasRefusals = true for an empty result")
	}
}

// ---- 14: WHICH identity provider ------------------------------------------

// The result carries the issuer and the environment, because the gate publishes
// the logins and a password without its issuer names no sign-in anybody can
// reach — there is one identity provider per environment now, and a credential
// minted on one is rejected by every other.
func TestEnsureReportsTheIssuerItProvisionedOn(t *testing.T) {
	h := newHarness(rolesJSON(t, []string{"Viewer"}, userFixture{"test-viewer", "Viewer"}))

	result := h.run(t)

	if result.Issuer != testIssuer {
		t.Fatalf("Issuer = %q, want %q — the gate has nothing to publish beside the password", result.Issuer, testIssuer)
	}
	if result.Environment != testEnvironment {
		t.Fatalf("Environment = %q, want %q", result.Environment, testEnvironment)
	}
	if len(result.Credentials) == 0 {
		t.Fatalf("no credentials to publish, so the issuer assertion proves nothing")
	}
}

// The directory is resolved ONCE per ensure, not once per role: resolving reads
// the environment's binding and its admin credential over the network, and a
// design with a dozen roles must not pay for a dozen of those.
func TestEnsureResolvesTheDirectoryOncePerBuild(t *testing.T) {
	h := newHarness(rolesJSON(t, []string{"Viewer", "Auditor", "Compliance Admin"},
		userFixture{"test-viewer", "Viewer"}, userFixture{"test-auditor", "Auditor"}))

	h.run(t)

	if h.targets.resolved != 1 {
		t.Fatalf("resolved the directory %d times for one ensure, want once", h.targets.resolved)
	}
	if len(h.targets.orgs) != 1 || h.targets.orgs[0] != testOrg {
		t.Fatalf("resolved for %v, want the build's own org %q", h.targets.orgs, testOrg)
	}
}

// An environment with no identity provider bound to it FAILS the ensure, and
// writes nothing on the way. The alternative — falling back to some other
// directory — would create the accounts somewhere their logins do not work,
// publish them, and send validation to a sign-in that rejects every one.
func TestEnsureFailsWhenTheEnvironmentHasNoIdentityProvider(t *testing.T) {
	h := newHarness(rolesJSON(t, []string{"Viewer"}, userFixture{"test-viewer", "Viewer"}))
	h.targets.err = errors.New(`environment "default" of "org-acme" has no Thunder binding`)

	_, declared, err := h.svc.EnsureForTag(context.Background(), testOrg, testProject, testTag)
	if err == nil {
		t.Fatal("EnsureForTag succeeded with no identity provider for the environment")
	}
	if !declared {
		t.Fatal("declared = false — the design does carry a roles document, and the gate branches on that separately")
	}
	if !strings.Contains(err.Error(), "no Thunder binding") {
		t.Fatalf("error %q does not name the missing binding", err)
	}
	if writes := h.dir.writes(); len(writes) != 0 {
		t.Fatalf("the directory was written to anyway: %+v", writes)
	}
	if len(h.store.replaceCalls) != 0 {
		t.Fatalf("references were written for a build that provisioned nothing")
	}
}

// ---- the project's own authorization objects -------------------------------
//
// Passes 3-5 CONVERGE, which is a different promise from the additive one above
// and needs different tests: not only "the right things were made" but "nothing
// else was touched", "the same tag twice writes nothing" and "what the tag
// dropped is gone".
//
// They are driven by the DESIGN'S OWN worked examples rather than by hand-made
// documents. Those three files are the contract's fixtures, shared with the
// agent's write gate, so a change to what a security document may say breaks
// these tests instead of leaving the ensure quietly converging a shape the
// platform no longer accepts.

// designFixture reads one of the design's worked examples. They live with the
// parser that owns them (and with the drift check that keeps them equal to the
// agent's copies), so they are read across the package boundary rather than
// duplicated here — a second copy is a second thing to keep in step.
func designFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "platform", "securityspec", "testdata", name))
	if err != nil {
		t.Fatalf("read the design fixture %s: %v", name, err)
	}
	return string(raw)
}

// seedSharedGroup puts a group on the directory that the PLATFORM created for
// some earlier project — a directory group plus the idp_roles row that is this
// domain's ownership marker. It is the `Finance` case: named by an assignTo,
// never redeclared, and enrolable because the platform made it.
func (h *harness) seedSharedGroup(name string) DirectoryGroup {
	group := h.dir.seedGroup(name)
	h.store.putRole(testScope, IdPRole{
		Name: name, ThunderGroupID: group.ID, Description: "seeded",
		CreatedByOrg: testOrg, CreatedByProject: "proj-earlier",
	})
	return group
}

// catalog reads the resource server's catalog back as handle → action handles,
// sorted, for comparison against what the tag declares.
func (d *fakeDirectory) catalog(t *testing.T, identifier string) map[string][]string {
	t.Helper()
	rs, ok := d.resourceServer(identifier)
	if !ok {
		t.Fatalf("no resource server %q on the fake directory", identifier)
	}
	out := map[string][]string{}
	for _, resource := range d.resources {
		if resource.RS != rs.ID {
			continue
		}
		actions := []string{}
		for _, action := range d.actions {
			if action.Resource == resource.ID {
				actions = append(actions, action.Handle)
			}
		}
		sort.Strings(actions)
		out[resource.Handle] = actions
	}
	return out
}

// catalogProse reads the DESCRIPTION of every catalog object back, keyed by the
// handle a reader would name it with (`claims`, `claims:read`). It is separate
// from catalog() above because the two answer different questions: that one is
// "does the tree match the tag", this one is "does it say what the document
// says".
func (d *fakeDirectory) catalogProse(t *testing.T, identifier string) map[string]string {
	t.Helper()
	rs, ok := d.resourceServer(identifier)
	if !ok {
		t.Fatalf("no resource server %q on the fake directory", identifier)
	}
	out := map[string]string{}
	for _, resource := range d.resources {
		if resource.RS != rs.ID {
			continue
		}
		out[resource.Handle] = resource.Description
		for _, action := range d.actions {
			if action.Resource == resource.ID {
				out[resource.Handle+":"+action.Handle] = action.Description
			}
		}
	}
	return out
}

// catalogIDs is handle → the directory id the object currently has, which is
// how a test tells an UPDATE from a delete-and-create that ends in the same
// shape. A recreated object has a new id; an updated one does not.
func (d *fakeDirectory) catalogIDs() map[string]DirectoryID {
	out := map[string]DirectoryID{}
	for _, resource := range d.resources {
		out[resource.Handle] = resource.ID
		for _, action := range d.actions {
			if action.Resource == resource.ID {
				out[resource.Handle+":"+action.Handle] = action.ID
			}
		}
	}
	return out
}

// assignedGroups is the names of the GROUPS holding a role, sorted. Names, not
// ids, because a group's id changes on every membership edit and the assertion
// is about who holds the role.
func (d *fakeDirectory) assignedGroups(t *testing.T, roleName string) []string {
	t.Helper()
	role, ok := d.roleByName(roleName)
	if !ok {
		t.Fatalf("no role %q on the fake directory", roleName)
	}
	out := []string{}
	for _, principal := range role.Assignments {
		if principal.Kind != PrincipalGroup {
			continue
		}
		out = append(out, d.principalDisplay(principal))
	}
	sort.Strings(out)
	return out
}

// credentialsByName indexes a result's published logins.
func credentialsByName(result Result) map[string]Credential {
	out := map[string]Credential{}
	for _, c := range result.Credentials {
		out[c.Username] = c
	}
	return out
}

// The whole object tree, for each of the design's three worked examples. One
// table rather than three tests because the property is the same in all three
// and the differences are the point: a reused group, a self-service role with no
// assignment and no login, and a service role held by nobody yet.
func TestEnsureLeavesTheObjectTreeTheDesignDeclares(t *testing.T) {
	identifier := ResourceServerIdentifier(testOrg, testProject)
	role := func(name string) string { return RoleName(testProject, name) }

	for _, tc := range []struct {
		fixture string
		// seed are groups an assignTo names that the document does not declare,
		// so the org directory must already hold them.
		seed        []string
		catalog     map[string][]string
		grants      map[string][]string
		assignments map[string][]string
		scopes      map[string][]string
	}{
		{
			fixture: "expense-tracker.json",
			seed:    []string{"Finance"},
			catalog: map[string][]string{
				"claims":  {"approve", "read", "read-all", "reject", "submit"},
				"reports": {"export", "read"},
			},
			grants: map[string][]string{
				role("Employee"): {"claims:read", "claims:submit"},
				role("Approver"): {"claims:approve", "claims:read", "claims:read-all", "claims:reject", "reports:read"},
			},
			assignments: map[string][]string{
				role("Employee"): {"Employees"},
				role("Approver"): {"Finance"},
			},
			scopes: map[string][]string{
				"test-employee": {"claims:read", "claims:submit"},
				"test-approver": {"claims:approve", "claims:read", "claims:read-all", "claims:reject", "reports:read"},
			},
		},
		{
			fixture: "clinic.json",
			catalog: map[string][]string{
				"appointments": {"book", "cancel", "manage", "read", "read-all"},
				"schedule":     {"publish", "read"},
				"records":      {"read"},
			},
			grants: map[string][]string{
				// A self-service role is still a ROLE on the directory: the
				// registration flow assigns it per account, so it has to exist
				// before the first account registers.
				role("Patient"):      {"appointments:book", "appointments:cancel", "appointments:read"},
				role("Receptionist"): {"appointments:manage", "appointments:read", "appointments:read-all", "schedule:publish", "schedule:read"},
				role("Doctor"):       {"appointments:read", "records:read", "schedule:read"},
			},
			assignments: map[string][]string{
				role("Patient"):      {},
				role("Receptionist"): {"Clinic Reception"},
				role("Doctor"):       {"Clinic Doctors"},
			},
			scopes: map[string][]string{
				"test-receptionist": {"appointments:manage", "appointments:read", "appointments:read-all", "schedule:publish", "schedule:read"},
				"test-doctor":       {"appointments:read", "records:read", "schedule:read"},
			},
		},
		{
			fixture: "vendor.json",
			seed:    []string{"Finance"},
			catalog: map[string][]string{
				"orders":   {"confirm", "place", "read"},
				"invoices": {"approve", "read", "read-all", "submit"},
				"payments": {"read", "release"},
			},
			grants: map[string][]string{
				role("Buyer"):    {"invoices:read", "orders:place", "orders:read"},
				role("Supplier"): {"invoices:read", "invoices:submit", "orders:confirm", "orders:read"},
				role("Finance"):  {"invoices:approve", "invoices:read", "invoices:read-all", "payments:read", "payments:release"},
				// A service role holds its grants with no principal attached:
				// phase 6 binds the app. It exists now so the workload it names
				// has something to be assigned to.
				role("reconciliation-job"): {"invoices:read", "invoices:read-all", "payments:read"},
			},
			assignments: map[string][]string{
				role("Buyer"):              {"Buyers"},
				role("Supplier"):           {"Suppliers"},
				role("Finance"):            {"Finance"},
				role("reconciliation-job"): {},
			},
			scopes: map[string][]string{
				"test-buyer":    {"invoices:read", "orders:place", "orders:read"},
				"test-supplier": {"invoices:read", "invoices:submit", "orders:confirm", "orders:read"},
				"test-finance":  {"invoices:approve", "invoices:read", "invoices:read-all", "payments:read", "payments:release"},
			},
		},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			h := newHarness(designFixture(t, tc.fixture))
			for _, name := range tc.seed {
				h.seedSharedGroup(name)
			}

			result := h.run(t)

			if result.ResourceIdentifier != identifier {
				t.Errorf("ResourceIdentifier = %q, want the derived %q", result.ResourceIdentifier, identifier)
			}
			if got := h.dir.catalog(t, identifier); !reflect.DeepEqual(got, tc.catalog) {
				t.Errorf("catalog = %v\nwant %v", got, tc.catalog)
			}
			for name, want := range tc.grants {
				if got := h.dir.grants(t, name); !reflect.DeepEqual(got, want) {
					t.Errorf("role %q grants %v, want %v", name, got, want)
				}
			}
			if got := len(h.dir.roles); got != len(tc.grants) {
				t.Errorf("the directory holds %d roles, want the %d the design declares", got, len(tc.grants))
			}
			for name, want := range tc.assignments {
				if got := h.dir.assignedGroups(t, name); !reflect.DeepEqual(got, want) {
					t.Errorf("role %q is assigned to %v, want %v", name, got, want)
				}
			}
			creds := credentialsByName(result)
			if len(creds) != len(tc.scopes) {
				t.Errorf("published logins = %v, want one per admin-enrolment role", result.Credentials)
			}
			for username, want := range tc.scopes {
				if got := creds[username].Scopes; !reflect.DeepEqual(got, want) {
					t.Errorf("%s publishes scopes %v, want the union of its roles' grants %v", username, got, want)
				}
			}

			// The row that lets a project delete find the tree again.
			row, err := h.store.GetResourceServer(context.Background(), testScope, testProject)
			if err != nil || row == nil {
				t.Fatalf("GetResourceServer = %v, %v — the resource server was not recorded", row, err)
			}
			if row.Identifier != identifier {
				t.Errorf("recorded identifier = %q, want %q", row.Identifier, identifier)
			}
			rs, _ := h.dir.resourceServer(identifier)
			if row.DirectoryID != string(rs.ID) {
				t.Errorf("recorded directory id = %q, want %q", row.DirectoryID, rs.ID)
			}

			// One binding row per (role, group), and one carrying an EMPTY group
			// for a role the build assigned to nobody.
			bindings, err := h.store.ListRoleBindings(context.Background(), testScope, testProject)
			if err != nil {
				t.Fatalf("ListRoleBindings: %v", err)
			}
			want := 0
			for _, groups := range tc.assignments {
				want += max(len(groups), 1)
			}
			if len(bindings) != want {
				t.Errorf("role bindings = %+v, want %d", bindings, want)
			}
		})
	}
}

// A ROLE WHOSE ONLY GROUP IS SOMEBODY ELSE'S is bound to the test ACCOUNT.
//
// This is the design's own `Approver → Finance (reused)`: `Finance` is on the
// directory and the platform has no row for it, so the enrolment rule refuses to
// put a disposable account into it. Before, that left `test-approver` with no
// login at all and the ticket said so in one line — a published credential set
// that silently omitted half the project's roles.
//
// The resolution keeps both halves: the group is untouched (no membership
// write, same id, same members) and the account holds `<project>/Approver`
// directly, as a user principal. `test-employee` is the control: its group is
// declared, the platform creates it, and it holds its role through ENROLMENT
// with no user principal at all.
func TestEnsureBindsARoleToTheAccountWhenItsGroupIsNotOwned(t *testing.T) {
	h := newHarness(designFixture(t, "expense-tracker.json"))
	// Seeded on the DIRECTORY ONLY — no idp_roles row, so the platform does not
	// own it. (h.seedSharedGroup is the other half of this pair: it writes the
	// row, and the enrolment path is what the rest of these tests exercise.)
	finance := h.dir.seedGroup("Finance", "usr-existing-finance")

	result := h.run(t)

	if !contains(result.GroupsPreExisting, "Finance") {
		t.Fatalf("GroupsPreExisting = %v, want Finance", result.GroupsPreExisting)
	}
	if !contains(result.UsersCreated, "test-approver") {
		t.Fatalf("UsersCreated = %v, want test-approver", result.UsersCreated)
	}
	if len(result.UsersSkipped) != 0 {
		t.Fatalf("UsersSkipped = %v, want none — every role here is grantable", result.UsersSkipped)
	}

	// The group is exactly as it was found.
	if n := h.dir.countOp("AddMembers"); n != 0 {
		t.Fatalf("AddMembers called %d times on a group the platform does not own", n)
	}
	if got := h.dir.groups["finance"]; got.ID != finance.ID {
		t.Fatalf("Finance's id changed %s -> %s", finance.ID, got.ID)
	}
	if got := h.dir.memberSet("Finance"); !reflect.DeepEqual(got, []string{"usr-existing-finance"}) {
		t.Fatalf("Finance members = %v, want only the one that was already there", got)
	}

	// The role is held two ways, and both are right: by the group (that is what
	// `assignTo` asks for, and it grants the org's real Finance people) and by
	// the test account itself.
	approver, ok := h.dir.roleByName(RoleName(testProject, "Approver"))
	if !ok {
		t.Fatalf("no %q on the directory", RoleName(testProject, "Approver"))
	}
	account := h.dir.users["test-approver"]
	if !holdsRole(approver.Assignments, Principal{Kind: PrincipalGroup, ID: DirectoryID(finance.ID)}) {
		t.Errorf("Approver assignments = %+v, want the Finance group", approver.Assignments)
	}
	if !holdsRole(approver.Assignments, Principal{Kind: PrincipalUser, ID: DirectoryID(account.ID)}) {
		t.Errorf("Approver assignments = %+v, want the test account as a user principal", approver.Assignments)
	}

	// The control: an owned group grants through enrolment, so nothing is bound
	// to the account directly.
	employee, ok := h.dir.roleByName(RoleName(testProject, "Employee"))
	if !ok {
		t.Fatalf("no %q on the directory", RoleName(testProject, "Employee"))
	}
	for _, p := range employee.Assignments {
		if p.Kind == PrincipalUser {
			t.Errorf("Employee holds a user principal %+v — its group is owned, so enrolment covers it", p)
		}
	}

	// And the published login carries the role's scopes, which is the whole
	// point: validation has to know what this account can exercise.
	var cred Credential
	for _, c := range result.Credentials {
		if c.Username == "test-approver" {
			cred = c
		}
	}
	if cred.Username == "" {
		t.Fatalf("no credential published for test-approver: %+v", result.Credentials)
	}
	if cred.Password == "" {
		t.Errorf("test-approver's password was not published")
	}
	if !reflect.DeepEqual(cred.Roles, []string{"Approver"}) {
		t.Errorf("credential roles = %v, want [Approver]", cred.Roles)
	}
	want := []string{"claims:approve", "claims:read", "claims:read-all", "claims:reject", "reports:read"}
	if !reflect.DeepEqual(cred.Scopes, want) {
		t.Errorf("credential scopes = %v, want %v", cred.Scopes, want)
	}
}

// The direct binding converges like everything else: a rebuild re-reads the
// assignments, sees the account already holds the role and writes nothing. A
// blind re-assign would show up here as one AssignRole per build, which is the
// churn the rebuild property exists to forbid.
func TestEnsureRebuildDoesNotReassignTheAccountBoundDirectly(t *testing.T) {
	h := newHarness(designFixture(t, "expense-tracker.json"))
	h.dir.seedGroup("Finance", "usr-existing-finance")
	h.run(t)
	h.dir.calls = nil
	h.dir.Calls = nil

	h.run(t)

	if writes := h.dir.writeLines(); len(writes) != 0 {
		t.Fatalf("the rebuild wrote to the directory:\n  %s", strings.Join(writes, "\n  "))
	}
}

// THE REBUILD PROPERTY, for the converging half. The same tag built twice makes
// no directory write at all — no create, no update, no assignment, no delete —
// and the binding rows come out identical. Without it every rebuild would churn
// the whole permission catalog, and deleting an action cascades out of every
// role that granted it, so churn here is not cosmetic: it would briefly revoke
// every grant in the project on every build.
func TestEnsureRebuildingTheSameTagWritesNothingToTheDirectory(t *testing.T) {
	h := newHarness(designFixture(t, "expense-tracker.json"))
	h.seedSharedGroup("Finance")
	h.run(t)
	h.dir.calls = nil
	h.dir.Calls = nil

	h.run(t)

	if writes := h.dir.writeLines(); len(writes) != 0 {
		t.Fatalf("a rebuild of the same tag wrote to the directory:\n  %s", strings.Join(writes, "\n  "))
	}
	if len(h.store.replaceBindingCalls) != 2 {
		t.Fatalf("ReplaceRoleBindings called %d times, want once per build", len(h.store.replaceBindingCalls))
	}
	if !reflect.DeepEqual(h.store.replaceBindingCalls[0], h.store.replaceBindingCalls[1]) {
		t.Errorf("the rebuild rewrote different bindings:\n  %+v\n  %+v",
			h.store.replaceBindingCalls[0], h.store.replaceBindingCalls[1])
	}
}

// A handle the next version drops is deleted, and NOTHING else is. The narrowness
// is the assertion: a converge that rebuilt the catalog would pass a "the tree
// matches the tag" test and still revoke every grant in the project on the way
// through, because deleting an action cascades out of every role that granted it.
func TestEnsureDeletesOnlyTheActionTheNextTagDropped(t *testing.T) {
	h := newHarness(designFixture(t, "expense-tracker.json"))
	h.seedSharedGroup("Finance")
	h.run(t)
	h.dir.calls = nil
	h.dir.Calls = nil

	h.setDoc(strings.Replace(designFixture(t, "expense-tracker.json"),
		`        { "handle": "export", "ownership": "any", "description": "Download CSV" }`, "", 1))
	// The action above is the last in its list, so the comma before it goes too.
	h.setDoc(strings.NewReplacer(
		`{ "handle": "read", "ownership": "any", "description": "Monthly totals" },`,
		`{ "handle": "read", "ownership": "any", "description": "Monthly totals" }`,
		`        { "handle": "export", "ownership": "any", "description": "Download CSV" }`, "",
	).Replace(designFixture(t, "expense-tracker.json")))

	h.run(t)

	if got := h.dir.writeLines(); !reflect.DeepEqual(got, []string{"DeleteAction reports:export"}) {
		t.Fatalf("the re-tag wrote:\n  %s\nwant exactly the one dropped action", strings.Join(got, "\n  "))
	}
	if got := h.dir.catalog(t, ResourceServerIdentifier(testOrg, testProject))["reports"]; !reflect.DeepEqual(got, []string{"read"}) {
		t.Errorf("reports actions = %v, want only read", got)
	}
}

// The directory objects carry the DOCUMENT'S prose, not a second spelling of
// their handles. A resource created as `claims` with an empty description is the
// same permission, so nothing breaks — which is exactly why this needs a test:
// an operator reading the identity provider, or a support engineer answering
// "what does claims:read-all let somebody do", sees only what the build wrote.
//
// The NAME is the handle, deliberately: a security document authors no display
// name, and inventing one would be a second spelling only this code could
// correct.
func TestEnsureCarriesTheDocumentsProseOntoTheDirectoryCatalog(t *testing.T) {
	h := newHarness(designFixture(t, "expense-tracker.json"))
	h.seedSharedGroup("Finance")

	h.run(t)

	want := map[string]string{
		"claims":          "Expense claims and their approval",
		"claims:read":     "See own claims",
		"claims:read-all": "See every claim",
		"claims:submit":   "Create and send a claim",
		"claims:approve":  "Approve a submitted claim",
		"claims:reject":   "Reject a submitted claim",
		// The document describes `reports` with nothing, and nothing is what the
		// directory gets: the ensure never invents prose.
		"reports":        "",
		"reports:read":   "Monthly totals",
		"reports:export": "Download CSV",
	}
	if got := h.dir.catalogProse(t, ResourceServerIdentifier(testOrg, testProject)); !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog prose = %+v\nwant %+v", got, want)
	}
	for _, resource := range h.dir.resources {
		if resource.Name != resource.Handle {
			t.Errorf("resource %q is named %q, want the handle", resource.Handle, resource.Name)
		}
	}
	for _, action := range h.dir.actions {
		if action.Name != action.Handle {
			t.Errorf("action %q is named %q, want the handle", action.Handle, action.Name)
		}
	}
}

// A DESCRIPTION change alone is an update and nothing else.
//
// The handle is the identity, so there is no diff here that could produce a
// delete — and it must stay that way: deleting an action cascades the permission
// it derived out of every role that granted it, so re-authoring a sentence would
// revoke access until the role pass ran again. The assertion is that the objects
// keep their IDS, which is the only way to tell an update from a
// delete-and-create that happens to end in the same shape.
func TestEnsureRewritesAChangedDescriptionWithoutRecreatingTheObject(t *testing.T) {
	h := newHarness(designFixture(t, "expense-tracker.json"))
	h.seedSharedGroup("Finance")
	h.run(t)
	before := h.dir.catalogIDs()
	h.dir.calls = nil
	h.dir.Calls = nil

	h.setDoc(strings.NewReplacer(
		`"description": "Expense claims and their approval"`,
		`"description": "Expense claims, and who may approve them"`,
		`{ "handle": "read", "ownership": "any", "description": "Monthly totals" }`,
		`{ "handle": "read", "ownership": "any", "description": "Monthly totals, per team" }`,
	).Replace(designFixture(t, "expense-tracker.json")))

	h.run(t)

	want := []string{"UpdateResource claims", "UpdateAction reports:read"}
	if got := h.dir.writeLines(); !reflect.DeepEqual(got, want) {
		t.Fatalf("the re-tag wrote:\n  %s\nwant exactly %v", strings.Join(got, "\n  "), want)
	}
	if got := h.dir.catalogIDs(); !reflect.DeepEqual(got, before) {
		t.Errorf("the objects were recreated:\n  %+v\n  %+v", before, got)
	}
	prose := h.dir.catalogProse(t, ResourceServerIdentifier(testOrg, testProject))
	if prose["claims"] != "Expense claims, and who may approve them" {
		t.Errorf("resource description = %q, want the re-authored one", prose["claims"])
	}
	if prose["reports:read"] != "Monthly totals, per team" {
		t.Errorf("action description = %q, want the re-authored one", prose["reports:read"])
	}
	// The grant the updated action derives is untouched — the point of not
	// deleting it.
	if got := h.dir.grants(t, RoleName(testProject, "Approver")); !slices.Contains(got, "reports:read") {
		t.Errorf("Approver grants %v, want reports:read to have survived the description change", got)
	}
}

// An assignTo the org directory cannot resolve is a TYPO, and the gate fails
// naming it. Creating the group instead would be worse than useless: it would
// mint an org-wide group nobody asked for, assign a project role to it, and
// leave the role the design meant to reach unreachable by anybody — a build that
// succeeds and an app nobody can sign in to.
func TestEnsureFailsNamingAnAssignToGroupThatResolvesToNothing(t *testing.T) {
	h := newHarness(strings.Replace(designFixture(t, "expense-tracker.json"),
		`"assignTo": ["Finance"]`, `"assignTo": ["Fnance"]`, 1))
	h.seedSharedGroup("Finance")

	_, _, err := h.svc.EnsureForTag(context.Background(), testOrg, testProject, testTag)
	if err == nil {
		t.Fatal("a role assigned to a group that does not exist provisioned successfully")
	}
	if !strings.Contains(err.Error(), "Fnance") {
		t.Errorf("error = %q, want it to name the group that could not be resolved", err)
	}
	// It fails BEFORE anything is written: classification reads the whole design
	// first, so a typo costs no half-provisioned project and no minted password.
	if writes := h.dir.writeLines(); len(writes) != 0 {
		t.Errorf("the failing build wrote to the directory:\n  %s", strings.Join(writes, "\n  "))
	}
}

// A role the next version drops is unassigned and deleted. It is project-OWNED —
// `<project>/` in its name, created by this project and no other — which is what
// makes deleting it safe, and is exactly the opposite of the org group behind it.
func TestEnsureUnassignsAndDeletesAProjectRoleTheTagNoLongerDeclares(t *testing.T) {
	h := newHarness(designFixture(t, "expense-tracker.json"))
	h.seedSharedGroup("Finance")
	h.run(t)
	dropped := RoleName(testProject, "Approver")
	if _, present := h.dir.roleByName(dropped); !present {
		t.Fatalf("the first build did not create %q", dropped)
	}
	h.dir.calls = nil
	h.dir.Calls = nil

	// v2 keeps only Employee. Its own test user goes with it, and so does the
	// `reports` resource nothing grants any more.
	h.setDoc(dropRoleFromExpenseTracker(t))
	result := h.run(t)

	if _, present := h.dir.roleByName(dropped); present {
		t.Errorf("%q survived a version that no longer declares it", dropped)
	}
	if !contains(result.RolesDeleted, "Approver") {
		t.Errorf("RolesDeleted = %v, want the dropped role", result.RolesDeleted)
	}
	// Unassigned BEFORE the delete: the directory drops the assignments either
	// way, but a delete that fails after the unassign leaves a role granting
	// nothing, while the reverse leaves a live grant behind.
	var order []string
	for _, c := range h.dir.writes() {
		if c.Op == "UnassignRole" || c.Op == "DeleteRole" {
			order = append(order, c.Op)
		}
	}
	if !reflect.DeepEqual(order, []string{"UnassignRole", "DeleteRole"}) {
		t.Errorf("role removal order = %v, want the assignments off first", order)
	}
	// The GROUP it was assigned to is untouched — shared, additive, and here
	// reused from another project.
	if _, stillThere := h.dir.groups["finance"]; !stillThere {
		t.Errorf("deleting a project role deleted the shared group it was assigned to")
	}
	// And the binding rows follow the directory.
	bindings, err := h.store.ListRoleBindings(context.Background(), testScope, testProject)
	if err != nil {
		t.Fatalf("ListRoleBindings: %v", err)
	}
	for _, b := range bindings {
		if b.Role == "Approver" {
			t.Errorf("a binding row survived the role: %+v", b)
		}
	}
}

// A RESOURCE RENAMED between tags. Handles are the identity and are immutable on
// the directory, so a rename is a delete plus a create — and the ORDER is what
// this pins: creates first, deletes leaf-first, and the roles re-PUT afterwards.
//
// Any other order loses access. Deleting `reports` first would cascade
// `reports:read` out of every role that granted it AND leave the role granting
// nothing until the role pass ran; creating the new tree first means the role
// pass can grant `statements:read` in the same build.
func TestEnsureRenamingAResourceCreatesBeforeItDeletes(t *testing.T) {
	h := newHarness(designFixture(t, "expense-tracker.json"))
	h.seedSharedGroup("Finance")
	h.run(t)
	h.dir.calls = nil
	h.dir.Calls = nil

	// `reports` becomes `statements`, actions and grants and screen with it.
	h.setDoc(strings.ReplaceAll(designFixture(t, "expense-tracker.json"), "reports", "statements"))
	h.run(t)

	want := []string{
		"CreateResource statements",
		"CreateAction statements:read",
		"CreateAction statements:export",
		"DeleteAction reports:read",
		"DeleteAction reports:export",
		"DeleteResource reports",
		"UpdateRole " + RoleName(testProject, "Approver") +
			" [claims:read,claims:read-all,claims:approve,claims:reject,statements:read]",
	}
	if got := h.dir.writeLines(); !reflect.DeepEqual(got, want) {
		t.Fatalf("the rename wrote:\n  %s\nwant:\n  %s",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	// And the old handle is gone from the catalog, not merely unused.
	catalog := h.dir.catalog(t, ResourceServerIdentifier(testOrg, testProject))
	if _, present := catalog["reports"]; present {
		t.Errorf("the old resource survived the rename: %+v", catalog)
	}
	if got := catalog["statements"]; !reflect.DeepEqual(got, []string{"export", "read"}) {
		t.Errorf("statements actions = %v, want the renamed tree", got)
	}
}

// An `assignTo` CHANGED between tags is one Unassign and one Assign, and
// nothing else. The role keeps its id and its permission set — reassignment is
// not a rewrite — and the binding rows follow.
func TestEnsureConvergesARoleWhoseAssignToChanged(t *testing.T) {
	h := newHarness(designFixture(t, "expense-tracker.json"))
	h.seedSharedGroup("Finance")
	h.run(t)
	role, _ := h.dir.roleByName(RoleName(testProject, "Approver"))
	// Finance's id AFTER the first build: enrolling the account into a group the
	// platform owns is a delete-and-recreate, so the id the role now holds is
	// not the seeded one.
	finance := h.dir.groups["finance"]

	// The group Approver moves to, owned by the platform and ALREADY holding the
	// account — so the move is purely a reassignment. A membership edit would be
	// correct too, but it mints a new group id and shows up as a second
	// unassign/assign pair on every role that group holds, which is a different
	// property with its own test.
	account := h.dir.users["test-approver"]
	auditors := h.dir.seedGroup("Auditors", account.ID)
	h.store.putRole(testScope, IdPRole{
		Name: "Auditors", ThunderGroupID: auditors.ID, Description: "seeded",
		CreatedByOrg: testOrg, CreatedByProject: "proj-earlier",
	})
	h.dir.calls = nil
	h.dir.Calls = nil

	h.setDoc(strings.Replace(designFixture(t, "expense-tracker.json"),
		`"assignTo": ["Finance"]`, `"assignTo": ["Auditors"]`, 1))
	h.run(t)

	approver := RoleName(testProject, "Approver")
	want := []string{
		"UnassignRole " + approver + " [group:" + finance.ID + "]",
		"AssignRole " + approver + " [group:" + auditors.ID + "]",
	}
	if got := h.dir.writeLines(); !reflect.DeepEqual(got, want) {
		t.Fatalf("the assignTo change wrote:\n  %s\nwant:\n  %s",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	after, _ := h.dir.roleByName(approver)
	if after.ID != role.ID {
		t.Errorf("the role was recreated: %q then %q", role.ID, after.ID)
	}
	if holdsRole(after.Assignments, Principal{Kind: PrincipalGroup, ID: DirectoryID(finance.ID)}) {
		t.Errorf("Approver still holds Finance: %+v", after.Assignments)
	}
	if !holdsRole(after.Assignments, Principal{Kind: PrincipalGroup, ID: DirectoryID(auditors.ID)}) {
		t.Errorf("Approver does not hold Auditors: %+v", after.Assignments)
	}
	// The binding rows say the same thing.
	bindings, err := h.store.ListRoleBindings(context.Background(), testScope, testProject)
	if err != nil {
		t.Fatalf("ListRoleBindings: %v", err)
	}
	for _, b := range bindings {
		if b.Role == "Approver" && b.GroupName != "Auditors" {
			t.Errorf("binding row = %+v, want the new group", b)
		}
	}
}

// A principal this platform cannot WRITE does not stop a role from being
// deleted. Thunder also assigns to `agent`, and somebody can put one on a role
// from its own console; the adapter refuses to map that kind onto the wire, so
// the unassign fails. The role is being deleted anyway — DeleteRole takes its
// assignments with it — so failing the whole build over the courtesy unassign
// would let one hand-made assignment block every future build of the project.
func TestEnsureDeletesADroppedRoleDespiteAPrincipalItCannotUnassign(t *testing.T) {
	h := newHarness(designFixture(t, "expense-tracker.json"))
	h.seedSharedGroup("Finance")
	h.run(t)
	dropped := RoleName(testProject, "Approver")
	// Assigned behind the port, because nothing above it can write this kind.
	h.dir.seedAssignment(t, dropped, Principal{Kind: PrincipalKind("agent"), ID: "agt-1", Display: "nightly-reconciler"})
	h.dir.calls = nil
	h.dir.Calls = nil

	h.setDoc(dropRoleFromExpenseTracker(t))
	result := h.run(t)

	if _, present := h.dir.roleByName(dropped); present {
		t.Fatalf("%q survived: one unmappable principal blocked the delete", dropped)
	}
	if !contains(result.RolesDeleted, "Approver") {
		t.Errorf("RolesDeleted = %v, want the dropped role", result.RolesDeleted)
	}
	// The principals it COULD unassign were still unassigned — the skip is per
	// principal, not a bail-out of the pass.
	var unassigned int
	for _, c := range h.dir.writes() {
		if c.Op == "UnassignRole" {
			unassigned++
		}
	}
	if unassigned != 1 {
		t.Errorf("UnassignRole ran %d times, want the one mappable principal", unassigned)
	}
}

// dropRoleFromExpenseTracker is the fixture with Approver, its test user and the
// `reports` resource nothing grants any more taken out — the "a version removes
// a role" edit, done to the design's own example rather than to a hand-made one.
func dropRoleFromExpenseTracker(t *testing.T) string {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(designFixture(t, "expense-tracker.json")), &doc); err != nil {
		t.Fatalf("unmarshal the fixture: %v", err)
	}
	doc["roles"] = []any{doc["roles"].([]any)[0]}
	doc["testUsers"] = []any{doc["testUsers"].([]any)[0]}
	doc["screens"] = []any{doc["screens"].([]any)[0], doc["screens"].([]any)[1]}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := securityspec.Parse(raw); err != nil {
		t.Fatalf("the edited fixture is not a valid security.json: %v", err)
	}
	return string(raw)
}

// A USER principal on a project role survives a converge. Nothing in
// security.json can describe one — they come from self-service registration
// (phase 7) and from an administrator assigning a role by hand — so a converge
// that treated "not in assignTo" as "remove" would revoke every one of them on
// every build, silently, and the account would simply stop working.
func TestEnsureLeavesUserAndAppPrincipalsOnARoleAlone(t *testing.T) {
	h := newHarness(designFixture(t, "expense-tracker.json"))
	h.seedSharedGroup("Finance")
	h.run(t)

	ctx := context.Background()
	approver := RoleName(testProject, "Approver")
	role, _ := h.dir.roleByName(approver)
	person := h.dir.seedUser("a-real-person")
	if err := h.dir.AssignRole(ctx, role.ID, Principal{Kind: PrincipalUser, ID: DirectoryID(person.ID)}); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	if err := h.dir.AssignRole(ctx, role.ID, Principal{Kind: PrincipalApp, ID: "app-reconciler"}); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	h.dir.calls = nil
	h.dir.Calls = nil

	h.run(t)

	kinds := map[PrincipalKind]int{}
	after, _ := h.dir.roleByName(approver)
	for _, p := range after.Assignments {
		kinds[p.Kind]++
	}
	if kinds[PrincipalUser] != 1 || kinds[PrincipalApp] != 1 {
		t.Errorf("assignments after the converge = %+v, want the user and the app principal kept", after.Assignments)
	}
	if kinds[PrincipalGroup] != 1 {
		t.Errorf("group assignments = %d, want the one the design declares", kinds[PrincipalGroup])
	}
	if writes := h.dir.writeLines(); len(writes) != 0 {
		t.Errorf("the converge wrote:\n  %s\nwant nothing — no principal it does not declare is its to move",
			strings.Join(writes, "\n  "))
	}
}
