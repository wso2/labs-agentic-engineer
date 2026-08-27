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
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/rolesspec"
)

const (
	testOrg     = "org-acme"
	testProject = "proj-expenses"
	testTag     = "v1"
)

// ---- fixtures -------------------------------------------------------------

// userFixture is one authored `testUsers[]` entry.
type userFixture struct{ username, role string }

// rolesJSON renders a minimal roles.json that the real rolesspec schema
// accepts. Building it rather than pasting literals keeps every case one line
// of intent, and keeps the fixtures honest: a schema change breaks these tests
// instead of letting them ensure a document the platform would reject.
func rolesJSON(t *testing.T, coldStartRole string, roles []string, users ...userFixture) string {
	t.Helper()
	roleEntries := make([]any, 0, len(roles))
	for _, name := range roles {
		roleEntries = append(roleEntries, map[string]any{
			"name": name, "description": name + " may read.", "stories": []int{1},
			"grantedBy":   "invitation",
			"permissions": []any{map[string]any{"component": "expense-api", "actions": []string{"read"}}},
		})
	}
	userEntries := make([]any, 0, len(users))
	for _, u := range users {
		userEntries = append(userEntries, map[string]any{"username": u.username, "role": u.role})
	}
	doc := map[string]any{
		"version":          1,
		"coldStartRole":    nil,
		"publicComponents": []string{},
		"roles":            roleEntries,
		"testUsers":        userEntries,
	}
	if coldStartRole != "" {
		doc["coldStartRole"] = coldStartRole
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	// A fixture the platform's own parser rejects would make every assertion
	// below vacuous, so it is checked here rather than discovered as a passing
	// "declared but errored" case.
	if _, err := rolesspec.Parse(raw); err != nil {
		t.Fatalf("fixture is not a valid roles.json: %v", err)
	}
	return string(raw)
}

// harness is one ensure wired to fresh fakes.
type harness struct {
	dir    *fakeDirectory
	store  *fakeStore
	design *fakeDesign
	svc    *EnsureService
}

func newHarness(doc string) *harness {
	h := &harness{dir: newFakeDirectory(), store: newFakeStore(), design: &fakeDesign{bundle: map[string]string{}}}
	if doc != "" {
		h.design.bundle[rolesspec.BundleKey] = doc
	}
	h.svc = NewEnsureService(h.dir, h.store, h.design)
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
func (h *harness) setDoc(doc string) { h.design.bundle[rolesspec.BundleKey] = doc }

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
		"key empty":      {rolesspec.BundleKey: ""},
		"key whitespace": {rolesspec.BundleKey: "  \n\t "},
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

// A roles.json that is present but broken is declared AND an error. The split
// is load-bearing: the caller holds dispatch behind a gate only when the design
// declares roles, so folding these together would let a broken document ship.
func TestEnsureForTagReportsDeclaredWhenTheRolesDocumentDoesNotParse(t *testing.T) {
	for name, doc := range map[string]string{
		"not JSON":         `{`,
		"schema violation": `{"version": 2, "coldStartRole": null, "publicComponents": [], "roles": [], "testUsers": []}`,
		"undeclared coldStart": rolesJSONRaw(`{"version":1,"coldStartRole":"Nobody","publicComponents":[],` +
			`"roles":[{"name":"Viewer","description":"d","stories":[1],"grantedBy":"g",` +
			`"permissions":[{"component":"api","actions":["read"]}]}],"testUsers":[]}`),
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(doc)

			_, declared, err := h.svc.EnsureForTag(context.Background(), testOrg, testProject, testTag)
			if err == nil {
				t.Fatalf("err = nil, want a parse refusal")
			}
			if !declared {
				t.Fatalf("declared = false — the caller would then NOT hold the build for a broken roles.json")
			}
			if !strings.Contains(err.Error(), rolesspec.Path) {
				t.Fatalf("error %q does not name %s", err, rolesspec.Path)
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
	h := newHarness(rolesJSON(t, "Viewer", []string{"Viewer", "Compliance Admin"},
		userFixture{"test-viewer", "Viewer"}))

	result := h.run(t)

	if len(result.RolesCreated) != 2 || !contains(result.RolesCreated, "Viewer") ||
		!contains(result.RolesCreated, "Compliance Admin") {
		t.Fatalf("RolesCreated = %v, want both roles", result.RolesCreated)
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
		if got := len(h.dir.memberSet(role)); got != 1 {
			t.Fatalf("group %q has %d members, want its one test user", role, got)
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
	h := newHarness(rolesJSON(t, "Viewer", []string{"Viewer", "Compliance Admin"},
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

	if len(second.RolesCreated) != 0 || len(second.UsersCreated) != 0 {
		t.Fatalf("second run created %v / %v, want nothing", second.RolesCreated, second.UsersCreated)
	}
	if len(second.RolesReused) != len(first.RolesCreated) || len(second.UsersReused) != len(first.UsersCreated) {
		t.Fatalf("second run reused %v / %v, want everything the first run made",
			second.RolesReused, second.UsersReused)
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
func TestEnsureLeavesAPreExistingDirectoryGroupAlone(t *testing.T) {
	h := newHarness(rolesJSON(t, "", []string{"Administrators"}))
	// On the directory, but with NO idp_roles row: somebody else made it.
	seeded := h.dir.seedGroup("Administrators", "usr-existing-admin")

	result := h.run(t)

	if !contains(result.RolesPreExisting, "Administrators") {
		t.Fatalf("RolesPreExisting = %v, want Administrators", result.RolesPreExisting)
	}
	if contains(result.RolesCreated, "Administrators") || contains(result.RolesReused, "Administrators") {
		t.Fatalf("Administrators was claimed: created=%v reused=%v", result.RolesCreated, result.RolesReused)
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
	if _, recorded := h.store.roles["administrators"]; recorded {
		t.Fatalf("an idp_roles row was written for a group the platform did not create")
	}
	// And its planned test account is NOT created.
	//
	// This is the half that matters most, and the reason classification happens
	// before any account is minted. Creating the account would seal a password
	// and write a test_user_refs row, and the validation credential provider
	// reads a ref as "this is the login for this role" — so it would hand
	// validation an account holding NO group at all, with mock:false, and
	// role-gated criteria would be graded against it. That is precisely the
	// failure this design exists to close, so the account is skipped and
	// reported instead.
	if contains(result.UsersCreated, "test-administrators") {
		t.Fatalf("UsersCreated = %v — an account was minted for a role the platform cannot enrol into", result.UsersCreated)
	}
	if !contains(result.UsersSkipped, "test-administrators") {
		t.Fatalf("UsersSkipped = %v, want test-administrators reported as skipped", result.UsersSkipped)
	}
	if _, created := h.dir.users["test-administrators"]; created {
		t.Fatalf("the account was created on the directory anyway")
	}
	if _, owned := h.store.users["test-administrators"]; owned {
		t.Fatalf("a test_users row was written for an account that can never be enrolled")
	}
	if _, sealed := h.store.passwords["test-administrators"]; sealed {
		t.Fatalf("a sealed password was written for an account that can never be enrolled")
	}
	// No reference either — a ref is what the credential provider serves.
	for _, refs := range h.store.refs {
		for _, ref := range refs {
			if ref.Username == "test-administrators" {
				t.Fatalf("a test_user_refs row was written, so the credential provider would serve this login")
			}
		}
	}
	// The group's membership is untouched.
	if got := h.dir.memberSet("Administrators"); len(got) != 1 || got[0] != "usr-existing-admin" {
		t.Fatalf("members = %v, want only the one that was already there", got)
	}
}

// ---- 7: the refused-account safety property -------------------------------

// REFUSED ACCOUNT. A username that exists on the directory but has no
// test_users row belongs to somebody. It is refused, never adopted: no create,
// no password reset, no enrolment. Otherwise a design naming `jsmith` would
// reset a real person's login and hand it to a validation runner.
func TestEnsureRefusesAnAccountThePlatformDoesNotOwn(t *testing.T) {
	h := newHarness(rolesJSON(t, "Viewer", []string{"Viewer"}, userFixture{"jsmith", "Viewer"}))
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
	if _, owned := h.store.users["jsmith"]; owned {
		t.Fatalf("a test_users row was written for an account the platform does not own")
	}
	// Refusal is per account and does not stop the pass: the role is still made.
	if !contains(result.RolesCreated, "Viewer") {
		t.Fatalf("RolesCreated = %v — one refused account blocked the role around it", result.RolesCreated)
	}
	// And the refused account is nowhere near the group.
	for _, id := range h.dir.memberSet("Viewer") {
		if id == person.ID {
			t.Fatalf("a refused account was enrolled into %q", "Viewer")
		}
	}
	if got := h.dir.memberSet("Viewer"); len(got) != 0 {
		t.Fatalf("Viewer members = %v, want none — its only planned user was refused", got)
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
	h := newHarness(rolesJSON(t, "Viewer", []string{"Viewer"}, userFixture{"test-viewer", "Viewer"}))
	h.run(t)
	firstGroupID := h.dir.groups["viewer"].ID
	if got := h.store.roles["viewer"].ThunderGroupID; got != firstGroupID {
		t.Fatalf("cached group id = %q, want %q after the first build", got, firstGroupID)
	}
	h.dir.calls = nil

	h.setDoc(rolesJSON(t, "Viewer", []string{"Viewer"},
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
	recreatedID := h.dir.groups["viewer"].ID
	if recreatedID == firstGroupID {
		t.Fatalf("the fake directory did not recreate the group — the case under test did not happen")
	}
	// ...and the store's cache followed it.
	if got := h.store.roles["viewer"].ThunderGroupID; got != recreatedID {
		t.Fatalf("cached group id = %q, want the recreated %q", got, recreatedID)
	}
	if got := len(h.dir.memberSet("Viewer")); got != 2 {
		t.Fatalf("Viewer holds %d members, want both accounts", got)
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
	h := newHarness(rolesJSON(t, "", []string{"Viewer"}, userFixture{"test-viewer", "Viewer"}))
	h.run(t)
	sealed := h.store.passwords["test-viewer"]
	if sealed == "" {
		t.Fatalf("the first build sealed no password, so there is nothing to protect")
	}

	// Thunder handed the account a new id, and v2 moves it to another role.
	h.dir.users["test-viewer"] = DirectoryAccount{
		ID: "usr-recreated", Username: "test-viewer", Email: "test-viewer@test-users.invalid",
	}
	h.setDoc(rolesJSON(t, "", []string{"Viewer", "Auditor"}, userFixture{"test-viewer", "Auditor"}))
	reveals, upserts := h.store.revealCalls, h.store.upsertUserCalls

	result := h.run(t)

	if !contains(result.UsersReused, "test-viewer") {
		t.Fatalf("UsersReused = %v, want the account the platform owns", result.UsersReused)
	}
	row := h.store.users["test-viewer"]
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
	if h.store.passwords["test-viewer"] != sealed {
		t.Fatalf("the sealed password changed under a facts-only update")
	}
}

// The reason the facts update exists, stated as a test: an account whose sealed
// password is absent still builds. The reveal-then-reseal path would fail the
// whole run here with ErrNoPassword, for a role correction that never needed
// the credential.
func TestEnsureRefreshesFactsForAnAccountWithNoSealedPassword(t *testing.T) {
	h := newHarness(rolesJSON(t, "", []string{"Viewer"}, userFixture{"test-viewer", "Viewer"}))
	h.run(t)

	// A row written before the seal existed, or one whose password was never
	// generated here.
	h.store.passwords["test-viewer"] = ""
	h.dir.users["test-viewer"] = DirectoryAccount{
		ID: "usr-recreated", Username: "test-viewer", Email: "test-viewer@test-users.invalid",
	}

	result := h.run(t)

	if !contains(result.UsersReused, "test-viewer") {
		t.Fatalf("UsersReused = %v", result.UsersReused)
	}
	if got := h.store.users["test-viewer"].ThunderUserID; got != "usr-recreated" {
		t.Fatalf("thunder_user_id = %q, want the refreshed id", got)
	}
}

// ---- 9: a role deleted out from under us ----------------------------------

// A role recorded in idp_roles but gone from the directory is recreated — and
// the provenance on the surviving row is kept, so the console still credits the
// project that first declared the role rather than whoever happened to rebuild.
func TestEnsureRecreatesAVanishedRoleAndKeepsItsOriginalProvenance(t *testing.T) {
	h := newHarness(rolesJSON(t, "", []string{"Viewer"}))
	h.store.roles["viewer"] = IdPRole{
		Name: "Viewer", ThunderGroupID: "grp-deleted", Description: "first description",
		CreatedByOrg: "org-first", CreatedByProject: "proj-first",
	}

	result := h.run(t)

	if !contains(result.RolesCreated, "Viewer") {
		t.Fatalf("RolesCreated = %v, want the recreated role", result.RolesCreated)
	}
	row := h.store.roles["viewer"]
	if row.ThunderGroupID == "grp-deleted" || row.ThunderGroupID != h.dir.groups["viewer"].ID {
		t.Fatalf("cached group id = %q, want the newly created %q", row.ThunderGroupID, h.dir.groups["viewer"].ID)
	}
	if row.CreatedByOrg != "org-first" || row.CreatedByProject != "proj-first" {
		t.Fatalf("provenance = %s/%s, want the original org-first/proj-first — the rebuilder took the role over",
			row.CreatedByOrg, row.CreatedByProject)
	}
}

// ---- 10: the project references -------------------------------------------

// Exactly one ref per USABLE planned user, with the cold-start and supplied
// flags the console renders. A refused account produces no ref: the project
// does not reference an account the platform did not provision for it.
func TestEnsureWritesOneRefPerUsablePlannedUser(t *testing.T) {
	h := newHarness(rolesJSON(t, "Viewer", []string{"Viewer", "Compliance Admin", "Auditor"},
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
		"test-viewer":           {Username: "test-viewer", RoleName: "Viewer", ColdStart: true, Supplied: false},
		"test-compliance-admin": {Username: "test-compliance-admin", RoleName: "Compliance Admin", ColdStart: false, Supplied: true},
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
	h := newHarness(rolesJSON(t, "", []string{"Viewer", "Auditor"}))
	h.run(t)

	h.setDoc(rolesJSON(t, "", []string{"Viewer"}))
	h.run(t)

	refs, err := h.store.ListProjectRefs(context.Background(), testOrg, testProject)
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
	// The directory object stands, and so does the platform's record of it.
	if _, stillThere := h.dir.groups["auditor"]; !stillThere {
		t.Fatalf("dropping a role from the design deleted the shared directory group")
	}
	if _, stillRecorded := h.store.roles["auditor"]; !stillRecorded {
		t.Fatalf("dropping a role from the design forgot the platform's ownership of it")
	}
}

// ---- 11-12: passwords ------------------------------------------------------

// A new account's password is sealed on the way in and is retrievable
// afterwards, because the IdP will not give it back: GET /users/{id} returns no
// password field, so a credential the platform failed to keep is gone.
func TestEnsureSealsARetrievableDistinctPasswordForEachNewAccount(t *testing.T) {
	h := newHarness(rolesJSON(t, "", []string{"Viewer", "Compliance Admin"}))

	h.run(t)

	ctx := context.Background()
	seen := map[string]string{}
	for _, username := range []string{"test-viewer", "test-compliance-admin"} {
		pw, err := h.store.RevealTestUserPassword(ctx, username)
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
	h := newHarness(rolesJSON(t, "Viewer", []string{"Viewer"}, userFixture{"test-viewer", "Viewer"}))
	first := h.run(t)
	if len(first.Credentials) != 1 || first.Credentials[0].Username != "test-viewer" {
		t.Fatalf("first build published %+v, want the account it created", first.Credentials)
	}
	firstPassword := first.Credentials[0].Password

	h.setDoc(rolesJSON(t, "Viewer", []string{"Viewer", "Auditor"},
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

// The cold-start flag rides along, because the agent needs to know which login
// answers a criterion that names no role.
func TestEnsurePublishesTheColdStartFlag(t *testing.T) {
	h := newHarness(rolesJSON(t, "Viewer", []string{"Viewer", "Auditor"},
		userFixture{"test-viewer", "Viewer"}, userFixture{"test-auditor", "Auditor"}))
	result := h.run(t)

	byName := map[string]Credential{}
	for _, c := range result.Credentials {
		byName[c.Username] = c
	}
	if !byName["test-viewer"].ColdStart {
		t.Errorf("the cold-start role's account was published as not cold-start: %+v", byName["test-viewer"])
	}
	if byName["test-auditor"].ColdStart {
		t.Errorf("a granted role's account was published as cold-start: %+v", byName["test-auditor"])
	}
	if byName["test-viewer"].Role != "Viewer" {
		t.Errorf("role = %q, want the role the account was enrolled into", byName["test-viewer"].Role)
	}
}

// An account the ensure would not touch must not be published. Publishing a
// refused username — one that belongs to a real person — would put a password
// beside somebody else's login in a ticket, for an account whose password the
// platform never set.
func TestEnsurePublishesNoLoginForARefusedOrSkippedAccount(t *testing.T) {
	h := newHarness(rolesJSON(t, "", []string{"Viewer"}, userFixture{"jsmith", "Viewer"}))
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
	h := newHarness(rolesJSON(t, "", []string{"Viewer"}, userFixture{"test-viewer", "Viewer"}))
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
	h := newHarness(rolesJSON(t, "", []string{"Viewer"}, userFixture{"test-viewer", "Viewer"}))
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
	full := newHarness(rolesJSON(t, "", []string{"Viewer"}))
	if !full.svc.Enabled() {
		t.Fatalf("Enabled = false with every collaborator wired")
	}
	cases := map[string]*EnsureService{
		"nil service":   nil,
		"no directory":  NewEnsureService(nil, full.store, full.design),
		"no store":      NewEnsureService(full.dir, nil, full.design),
		"no design":     NewEnsureService(full.dir, full.store, nil),
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
		RolesCreated: []string{"Viewer"}, RolesPreExisting: []string{"Administrators"},
		UsersRefused: []string{"jsmith"},
	}
	got := r.Summary()
	for _, want := range []string{"Roles created: Viewer", "Administrators", "jsmith"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary %q does not mention %q", got, want)
		}
	}
	if strings.Contains(got, "Roles reused") || strings.Contains(got, "Test users created") {
		t.Fatalf("summary %q reports outcomes that did not occur", got)
	}
	if (Result{}).HasRefusals() {
		t.Fatalf("HasRefusals = true for an empty result")
	}
}
