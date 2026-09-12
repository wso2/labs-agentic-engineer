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

// teardown_test.go — the project delete's identity half.
//
// Two properties carry the whole file, and they pull in opposite directions,
// which is why they are tested against the same fakes the ensure is:
//
//   - everything the project OWNS goes, including what the platform's rows
//     forgot to record;
//   - everything SHARED stays, and the call log is the proof — a test that only
//     checked the groups still existed would pass against an implementation
//     that deleted and recreated one.

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// teardownFixture is a project that has been built at least once: a resource
// server with a catalog, two project roles, one assigned to a group and one to
// an account, and the platform's rows recording all of it.
type teardownFixture struct {
	dir     *fakeDirectory
	store   *fakeStore
	targets *fakeTargets
	svc     *TeardownService

	scope      Scope
	identifier string
	group      DirectoryGroup
	account    DirectoryAccount
	approver   DirectoryID
	employee   DirectoryID
}

const (
	teardownOrg     = "acme"
	teardownProject = "p1"
)

// newTeardownFixture seeds the directory THROUGH THE PORT, exactly as the
// ensure would, so nothing here depends on a shape the real converge cannot
// produce. The call log is cleared afterwards: every call an assertion sees was
// made by the teardown.
func newTeardownFixture(t *testing.T) *teardownFixture {
	t.Helper()
	ctx := context.Background()
	dir := newFakeDirectory()
	rs, _, _ := seedCatalog(t, dir)

	group := dir.seedGroup("Finance")
	account := dir.seedUser("approver-1")

	approver, err := dir.EnsureRole(ctx, "", RoleName(teardownProject, "Approver"), "", rs,
		[]string{"claims:read", "claims:read-all"})
	if err != nil {
		t.Fatalf("seed role Approver: %v", err)
	}
	employee, err := dir.EnsureRole(ctx, "", RoleName(teardownProject, "Employee"), "", rs,
		[]string{"claims:read", "claims:submit"})
	if err != nil {
		t.Fatalf("seed role Employee: %v", err)
	}
	if err := dir.AssignRole(ctx, approver, Principal{Kind: PrincipalGroup, ID: DirectoryID(group.ID)}); err != nil {
		t.Fatalf("seed assignment Approver->Finance: %v", err)
	}
	if err := dir.AssignRole(ctx, employee, Principal{Kind: PrincipalUser, ID: DirectoryID(account.ID)}); err != nil {
		t.Fatalf("seed assignment Employee->approver-1: %v", err)
	}

	targets := newFakeTargets(dir)
	scope := targets.Scope(teardownOrg)
	store := newFakeStore()
	identifier := ResourceServerIdentifier(teardownOrg, teardownProject)

	if err := store.UpsertResourceServer(ctx, IdPResourceServer{
		OrgID: scope.OrgID, Environment: scope.Environment, ProjectID: teardownProject,
		Identifier: identifier, DirectoryID: string(rs),
	}); err != nil {
		t.Fatalf("seed resource-server row: %v", err)
	}
	// The Employee row carries an EMPTY group: a self-service role is recorded
	// and assigned to nobody, and the teardown has to find it by its id all the
	// same.
	if err := store.ReplaceRoleBindings(ctx, scope, teardownProject, []IdPRoleBinding{
		{Role: "Approver", GroupName: "Finance", DirectoryRoleID: string(approver)},
		{Role: "Employee", GroupName: "", DirectoryRoleID: string(employee)},
	}); err != nil {
		t.Fatalf("seed role bindings: %v", err)
	}

	dir.calls, dir.Calls = nil, nil
	return &teardownFixture{
		dir: dir, store: store, targets: targets,
		svc:        NewTeardownService(targets, store),
		scope:      scope,
		identifier: identifier,
		group:      group,
		account:    account,
		approver:   approver,
		employee:   employee,
	}
}

// writeSteps renders the directory WRITES as "<Op> <target>", dropping the id
// lists so an assertion reads as the sequence it is about and does not depend
// on which order the fake minted its ids in.
func (f *teardownFixture) writeSteps() []string {
	var out []string
	for _, c := range f.dir.writes() {
		step := c.Op
		if c.Target != "" {
			step += " " + c.Target
		}
		out = append(out, step)
	}
	return out
}

func assertSteps(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Fatalf("directory writes =\n  %v\nwant\n  %v", got, want)
	}
}

// TestTeardown_RemovesTheProjectsObjectsAndTouchesNoSharedOne is the whole
// contract in one pass: the assignments come off, the roles go, the resource
// server goes with its catalog, the rows are forgotten — and the group and the
// account the roles were assigned to are not written to even once.
func TestTeardown_RemovesTheProjectsObjectsAndTouchesNoSharedOne(t *testing.T) {
	t.Parallel()
	f := newTeardownFixture(t)

	report := f.svc.Teardown(context.Background(), teardownOrg, teardownProject)

	if err := report.Err(); err != nil {
		t.Fatalf("a complete teardown must report no problems, got %v", err)
	}
	// The ORDER is the contract: a role's grants are taken away before the role,
	// and the roles before the catalog their permissions point at.
	assertSteps(t, f.writeSteps(), []string{
		"UnassignRole " + RoleName(teardownProject, "Approver"),
		"DeleteRole " + RoleName(teardownProject, "Approver"),
		"UnassignRole " + RoleName(teardownProject, "Employee"),
		"DeleteRole " + RoleName(teardownProject, "Employee"),
		"DeleteResourceServer " + f.identifier,
	})

	if len(report.RolesDeleted) != 2 || report.AssignmentsRemoved != 2 {
		t.Errorf("report = %s, want 2 roles and 2 assignments", report.Summary())
	}
	if report.ResourceServerDeleted != f.identifier {
		t.Errorf("resource server deleted = %q, want %q", report.ResourceServerDeleted, f.identifier)
	}

	// The directory is empty of everything this project owned — the catalog
	// included, which the resource-server delete cascades.
	if _, ok := f.dir.resourceServer(f.identifier); ok {
		t.Error("the resource server is still on the directory")
	}
	if len(f.dir.roles) != 0 || len(f.dir.resources) != 0 || len(f.dir.actions) != 0 {
		t.Errorf("project-owned objects left behind: %d roles, %d resources, %d actions",
			len(f.dir.roles), len(f.dir.resources), len(f.dir.actions))
	}

	// ...and the shared half is untouched. Existence is not the assertion — a
	// delete-and-recreate would pass that — so the CALL LOG is: no write here
	// names a group or an account.
	for _, op := range []string{"CreateGroup", "AddMembers", "RemoveMembers", "DeleteGroup",
		"CreateUser", "SetUserPassword", "DeleteUser"} {
		if n := f.dir.countOp(op); n != 0 {
			t.Errorf("%s was called %d times: the teardown must never touch a shared object", op, n)
		}
	}
	if _, ok := f.dir.groups[strings.ToLower(f.group.Name)]; !ok {
		t.Error("the group the role was assigned to was deleted")
	}
	if got := f.dir.memberSet(f.group.Name); len(got) != 0 {
		t.Errorf("the group's membership changed: %v", got)
	}
	if _, ok := f.dir.users[f.account.Username]; !ok {
		t.Error("the account the role was assigned to was deleted")
	}

	assertRowsForgotten(t, f)
}

// TestTeardown_DeletesWhatTheRowsNeverRecorded is the backstop. The ensure
// writes its rows AFTER the directory objects, so a pass that died between the
// two leaves a resource server and roles with no record of them at all — and
// the only thing that can still find them is the derivation: the `<project>/`
// role prefix and the resource-server identifier, neither of which any other
// project can produce.
func TestTeardown_DeletesWhatTheRowsNeverRecorded(t *testing.T) {
	t.Parallel()
	f := newTeardownFixture(t)
	// Forget everything the platform recorded, leaving the directory as it is.
	if err := f.store.DeleteRoleBindings(context.Background(), f.scope, teardownProject); err != nil {
		t.Fatalf("clear bindings: %v", err)
	}
	if err := f.store.DeleteResourceServer(context.Background(), f.scope, teardownProject); err != nil {
		t.Fatalf("clear resource-server row: %v", err)
	}

	report := f.svc.Teardown(context.Background(), teardownOrg, teardownProject)

	if err := report.Err(); err != nil {
		t.Fatalf("the backstop must complete, got %v", err)
	}
	assertSteps(t, f.writeSteps(), []string{
		"UnassignRole " + RoleName(teardownProject, "Approver"),
		"DeleteRole " + RoleName(teardownProject, "Approver"),
		"UnassignRole " + RoleName(teardownProject, "Employee"),
		"DeleteRole " + RoleName(teardownProject, "Employee"),
		"DeleteResourceServer " + f.identifier,
	})
	// The resource server was FOUND, not created. The backstop reaches for it
	// through FindResourceServer, the port's read-only lookup; asking through
	// EnsureResourceServer would mean the teardown minted the very object it
	// then deleted.
	if n := f.dir.countOp("CreateResourceServer"); n != 0 {
		t.Errorf("CreateResourceServer called %d times — the backstop must find, not create", n)
	}
	if _, ok := f.dir.resourceServer(f.identifier); ok {
		t.Error("the unrecorded resource server survived the teardown")
	}
	if len(f.dir.roles) != 0 {
		t.Errorf("unrecorded roles survived the teardown: %d left", len(f.dir.roles))
	}
}

// A project that never built has no row AND no object, and the teardown must
// write NOTHING. It is the case the read-only lookup exists for: through
// find-or-create the backstop would mint a resource server for a project that
// never had one, purely so the next step could delete it — a teardown that
// leaves the directory dirtier than it found it whenever it fails midway.
func TestTeardown_WritesNothingForAProjectThatNeverProvisioned(t *testing.T) {
	t.Parallel()
	f := newTeardownFixture(t)
	ctx := context.Background()
	if err := f.store.DeleteRoleBindings(ctx, f.scope, "never-built"); err != nil {
		t.Fatalf("clear bindings: %v", err)
	}

	report := f.svc.Teardown(ctx, teardownOrg, "never-built")

	if err := report.Err(); err != nil {
		t.Fatalf("tearing down a project with nothing provisioned must succeed, got %v", err)
	}
	assertSteps(t, f.writeSteps(), nil)
	if _, ok := f.dir.resourceServer(ResourceServerIdentifier(teardownOrg, "never-built")); ok {
		t.Error("the teardown created a resource server for a project that never had one")
	}
}

// TestTeardown_LeavesAnotherProjectsRolesAlone pins the selector the backstop
// leans on. The prefix is what makes a role ownable; without this a second
// project's roles would be swept up by the first project's delete.
func TestTeardown_LeavesAnotherProjectsRolesAlone(t *testing.T) {
	t.Parallel()
	f := newTeardownFixture(t)
	ctx := context.Background()
	// Another project's resource server and role on the same directory.
	otherRS, err := f.dir.EnsureResourceServer(ctx, ResourceServerIdentifier(teardownOrg, "p2"), "p2")
	if err != nil {
		t.Fatalf("seed the other project's resource server: %v", err)
	}
	otherRole, err := f.dir.EnsureRole(ctx, "", RoleName("p2", "Approver"), "", otherRS, nil)
	if err != nil {
		t.Fatalf("seed the other project's role: %v", err)
	}

	if err := f.svc.Teardown(ctx, teardownOrg, teardownProject).Err(); err != nil {
		t.Fatalf("teardown: %v", err)
	}

	if _, ok := f.dir.roleByName(RoleName("p2", "Approver")); !ok {
		t.Errorf("role %q was deleted by another project's teardown", otherRole)
	}
	if _, ok := f.dir.resourceServer(ResourceServerIdentifier(teardownOrg, "p2")); !ok {
		t.Error("another project's resource server was deleted")
	}
}

// TestTeardown_ADirectoryFailureStillForgetsTheRows: every step is best-effort
// and none returns early. A role that will not delete must not save the rows —
// they are keyed to a project that is going away, and a project recreated under
// the same name would adopt their stale directory ids.
func TestTeardown_ADirectoryFailureStillForgetsTheRows(t *testing.T) {
	t.Parallel()
	f := newTeardownFixture(t)
	boom := errors.New("thunder: 503")
	f.dir.failOn["DeleteRole"] = boom

	report := f.svc.Teardown(context.Background(), teardownOrg, teardownProject)

	if !errors.Is(report.Err(), boom) {
		t.Fatalf("the failure must be reported, got %v", report.Err())
	}
	if len(report.Problems) != 2 {
		t.Errorf("problems = %v, want one per role", report.Problems)
	}
	if len(report.RolesDeleted) != 0 {
		t.Errorf("roles reported deleted that were not: %v", report.RolesDeleted)
	}
	// The steps AFTER the failure still ran: both roles were attempted and the
	// resource server went.
	if report.ResourceServerDeleted != f.identifier {
		t.Errorf("the resource server was not deleted after a role failure: %s", report.Summary())
	}
	assertRowsForgotten(t, f)
}

// TestTeardown_AnUnreachableDirectoryStillForgetsTheRows is the same rule one
// level up: the whole directory half is skipped when the environment's identity
// provider cannot be resolved, and the rows still go. Scope is pure, so the
// rows are addressable even when nothing else is.
func TestTeardown_AnUnreachableDirectoryStillForgetsTheRows(t *testing.T) {
	t.Parallel()
	f := newTeardownFixture(t)
	f.targets.err = errors.New("no thunder binding for this environment")

	report := f.svc.Teardown(context.Background(), teardownOrg, teardownProject)

	if report.Err() == nil {
		t.Fatal("an unresolvable directory must be reported as a problem")
	}
	if got := len(f.dir.writes()); got != 0 {
		t.Errorf("the directory was written to without being resolved: %v", f.writeSteps())
	}
	if report.Scope != f.scope {
		t.Errorf("report scope = %v, want %v", report.Scope, f.scope)
	}
	assertRowsForgotten(t, f)
}

// TestTeardown_NotConfiguredIsANoOp: the composition root wires this feature
// optionally, so an unwired service — and a nil one across the consumer port —
// must answer an empty report rather than panic.
func TestTeardown_NotConfiguredIsANoOp(t *testing.T) {
	t.Parallel()
	var nilSvc *TeardownService
	if nilSvc.Enabled() {
		t.Error("a nil service reports itself enabled")
	}
	if err := nilSvc.TeardownProject(context.Background(), teardownOrg, teardownProject); err != nil {
		t.Errorf("a nil service must be a no-op, got %v", err)
	}
	unwired := NewTeardownService(nil, nil)
	if report := unwired.Teardown(context.Background(), teardownOrg, teardownProject); report.Err() != nil {
		t.Errorf("an unwired service must be a no-op, got %v", report.Err())
	}
}

// TestTeardownProject_ReportsTheJoinedProblems pins the consumer-port shape the
// project delete holds: one error, or nil.
func TestTeardownProject_ReportsTheJoinedProblems(t *testing.T) {
	t.Parallel()
	f := newTeardownFixture(t)
	if err := f.svc.TeardownProject(context.Background(), teardownOrg, teardownProject); err != nil {
		t.Fatalf("a clean teardown must answer nil, got %v", err)
	}

	g := newTeardownFixture(t)
	boom := errors.New("thunder: 503")
	g.dir.failOn["DeleteResourceServer"] = boom
	if err := g.svc.TeardownProject(context.Background(), teardownOrg, teardownProject); !errors.Is(err, boom) {
		t.Fatalf("TeardownProject error = %v, want the directory's", err)
	}
}

// assertRowsForgotten checks the platform's own record is gone — the step that
// runs whatever happened above it.
func assertRowsForgotten(t *testing.T, f *teardownFixture) {
	t.Helper()
	ctx := context.Background()
	bindings, err := f.store.ListRoleBindings(ctx, f.scope, teardownProject)
	if err != nil {
		t.Fatalf("list role bindings: %v", err)
	}
	if len(bindings) != 0 {
		t.Errorf("role binding rows survived the teardown: %v", bindings)
	}
	row, err := f.store.GetResourceServer(ctx, f.scope, teardownProject)
	if err != nil {
		t.Fatalf("get resource server row: %v", err)
	}
	if row != nil {
		t.Errorf("the resource-server row survived the teardown: %+v", row)
	}
}
