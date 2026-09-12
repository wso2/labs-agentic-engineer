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

package rolespanel_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/identity"
)

// In-memory doubles for the two ports the panel stands on.
//
// They are in-memory rather than call-recording stubs on purpose: the fences the
// tests exist to prove are STATEFUL (which project references which shared
// account, and whether the platform holds a sealed password for it), and a stub
// that answers a fixed value would let a handler that ignored the fence pass.
// The one thing they do record is directory writes, because "did the directory
// actually change?" is the other half of the rotate and delete assertions.

// panelEnv is the environment fakeTargets resolves every org to — the one
// identity provider these fakes model.
const panelEnv = "default"

// panelOrg is the org whose directory the role and account fixtures belong to.
// It matches the org the component tests authenticate as; a fixture for any
// other org would be a row on a directory those requests never reach, which is
// what the fence tests below are for.
const panelOrg = "acme"

// scopeOf is the (org, environment) key, the same one the resolver hands the
// panel.
func scopeOf(orgID string) identity.Scope {
	return identity.Scope{OrgID: orgID, Environment: panelEnv}
}

// fakeTargets is the identity.TargetResolver: every org resolves to the same
// faked directory, under that org's default scope.
type fakeTargets struct {
	dir identity.Directory
	// err, when set, is what Resolve answers — the environment with no identity
	// provider bound to it. Scope keeps working, which is what lets the panel's
	// read degrade instead of failing.
	err error
}

func newFakeTargets(dir identity.Directory) *fakeTargets { return &fakeTargets{dir: dir} }

func (f *fakeTargets) Scope(orgID string) identity.Scope { return scopeOf(orgID) }

func (f *fakeTargets) Resolve(_ context.Context, orgID string) (identity.Target, error) {
	if f.err != nil {
		return identity.Target{}, f.err
	}
	return identity.Target{
		OrgID: orgID, Environment: panelEnv,
		Issuer:    "http://default-idp.amp.localhost:8080",
		Directory: f.dir,
	}, nil
}

var _ identity.TargetResolver = (*fakeTargets)(nil)

// fakeStore is an in-memory identity.Store. Passwords are kept in the clear —
// the real sealing is the ColumnCipher's job and is covered by the repository's
// own tests; what matters here is WHICH password the panel stored.
//
// Every map is keyed by the SCOPE as well as the name, because the store is: a
// fake that ignored the scope would let a panel reading the wrong environment's
// rows pass.
type fakeStore struct {
	roles     map[string]identity.IdPRole
	testUsers map[string]identity.TestUser
	passwords map[string]string
	// refs is keyed scope/project → the rows that project references.
	refs map[string][]identity.TestUserRef
	// resourceServers and roleBindings are the PROJECT-OWNED half of the store.
	// The panel reads neither — it serves the shared roles and test users — but
	// the store is one interface, so the double models them rather than
	// pretending a caller could not reach them. Both are keyed scope/project.
	resourceServers map[string]identity.IdPResourceServer
	roleBindings    map[string][]identity.IdPRoleBinding

	// setPasswordErr makes the seal fail, which is how the half-applied rotate
	// (directory written, store not) becomes reachable in a test.
	setPasswordErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		roles:     map[string]identity.IdPRole{},
		testUsers: map[string]identity.TestUser{},
		passwords: map[string]string{},
		refs:      map[string][]identity.TestUserRef{},

		resourceServers: map[string]identity.IdPResourceServer{},
		roleBindings:    map[string][]identity.IdPRoleBinding{},
	}
}

func refKey(scope identity.Scope, projectID string) string { return scope.String() + "/" + projectID }

// scopedKey joins the scope with a role name or username, exactly as the
// composite primary key does.
func scopedKey(scope identity.Scope, name string) string { return scope.String() + "|" + name }

// withRole records a role the platform created on panelOrg's directory.
func (s *fakeStore) withRole(name string) *fakeStore {
	scope := scopeOf(panelOrg)
	s.roles[scopedKey(scope, strings.ToLower(name))] = identity.IdPRole{
		OrgID: scope.OrgID, Environment: scope.Environment,
		Name: name, ThunderGroupID: "grp-" + name,
	}
	return s
}

// withOwnedUser records an account the platform owns on panelOrg's directory,
// with its sealed password.
func (s *fakeStore) withOwnedUser(username, role, password string) *fakeStore {
	scope := scopeOf(panelOrg)
	s.testUsers[scopedKey(scope, username)] = identity.TestUser{
		OrgID: scope.OrgID, Environment: scope.Environment,
		Username: username, ThunderUserID: "usr-" + username, RoleName: role,
	}
	s.passwords[scopedKey(scope, username)] = password
	return s
}

// withRef records that org/project's design references username. It is the ONLY
// project-scoped fact in this domain, and therefore the whole org+project fence.
func (s *fakeStore) withRef(orgID, projectID, username, role string) *fakeStore {
	scope := scopeOf(orgID)
	k := refKey(scope, projectID)
	s.refs[k] = append(s.refs[k], identity.TestUserRef{
		OrgID: orgID, Environment: scope.Environment,
		ProjectID: projectID, Username: username, RoleName: role,
	})
	return s
}

// withRoleBinding records that org/project's build assigned its role to a
// group. An EMPTY group is the "recorded, assigned to nobody" marker a
// self-service role carries, and the panel must not report it as a group.
func (s *fakeStore) withRoleBinding(orgID, projectID, role, group, directoryRoleID string) *fakeStore {
	scope := scopeOf(orgID)
	k := refKey(scope, projectID)
	s.roleBindings[k] = append(s.roleBindings[k], identity.IdPRoleBinding{
		OrgID: orgID, Environment: scope.Environment, ProjectID: projectID,
		Role: role, GroupName: group, DirectoryRoleID: directoryRoleID,
	})
	return s
}

// withResourceServer records the resource server an earlier build created for
// a project. Without one the panel derives the identifier, which is the
// never-built-yet case; with one, the recorded identifier wins.
func (s *fakeStore) withResourceServer(orgID, projectID, identifier string) *fakeStore {
	scope := scopeOf(orgID)
	s.resourceServers[refKey(scope, projectID)] = identity.IdPResourceServer{
		OrgID: orgID, Environment: scope.Environment, ProjectID: projectID,
		Identifier: identifier, DirectoryID: "rs-" + projectID,
	}
	return s
}

func (s *fakeStore) GetRole(_ context.Context, scope identity.Scope, name string) (*identity.IdPRole, error) {
	if r, ok := s.roles[scopedKey(scope, strings.ToLower(name))]; ok {
		return &r, nil
	}
	return nil, nil
}

func (s *fakeStore) ListRoles(_ context.Context, scope identity.Scope) ([]identity.IdPRole, error) {
	out := make([]identity.IdPRole, 0, len(s.roles))
	for _, r := range s.roles {
		if r.OrgID != scope.OrgID || r.Environment != scope.Environment {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *fakeStore) UpsertRole(_ context.Context, role identity.IdPRole) error {
	scope := identity.Scope{OrgID: role.OrgID, Environment: role.Environment}
	s.roles[scopedKey(scope, strings.ToLower(role.Name))] = role
	return nil
}

func (s *fakeStore) GetTestUser(_ context.Context, scope identity.Scope, username string) (*identity.TestUser, error) {
	if u, ok := s.testUsers[scopedKey(scope, username)]; ok {
		return &u, nil
	}
	return nil, nil
}

func (s *fakeStore) UpsertTestUser(_ context.Context, user identity.TestUser, password string) error {
	scope := identity.Scope{OrgID: user.OrgID, Environment: user.Environment}
	s.testUsers[scopedKey(scope, user.Username)] = user
	s.passwords[scopedKey(scope, user.Username)] = password
	return nil
}

func (s *fakeStore) UpdateTestUserFacts(_ context.Context, scope identity.Scope, username, thunderUserID, roleName string) error {
	u, ok := s.testUsers[scopedKey(scope, username)]
	if !ok {
		return errors.New("no such account")
	}
	u.ThunderUserID, u.RoleName = thunderUserID, roleName
	s.testUsers[scopedKey(scope, username)] = u
	return nil
}

func (s *fakeStore) SetTestUserPassword(_ context.Context, scope identity.Scope, username, password string) error {
	if s.setPasswordErr != nil {
		return s.setPasswordErr
	}
	u, ok := s.testUsers[scopedKey(scope, username)]
	if !ok {
		return errors.New("no such account")
	}
	now := time.Now().UTC()
	u.RotatedAt = &now
	s.testUsers[scopedKey(scope, username)] = u
	s.passwords[scopedKey(scope, username)] = password
	return nil
}

func (s *fakeStore) RevealTestUserPassword(_ context.Context, scope identity.Scope, username string) (string, error) {
	p, ok := s.passwords[scopedKey(scope, username)]
	if !ok || p == "" {
		return "", identity.ErrNoPassword
	}
	return p, nil
}

func (s *fakeStore) DeleteTestUser(_ context.Context, scope identity.Scope, username string) error {
	delete(s.testUsers, scopedKey(scope, username))
	delete(s.passwords, scopedKey(scope, username))
	for k, rows := range s.refs {
		var kept []identity.TestUserRef
		for _, r := range rows {
			if r.Username != username || r.OrgID != scope.OrgID || r.Environment != scope.Environment {
				kept = append(kept, r)
			}
		}
		s.refs[k] = kept
	}
	return nil
}

func (s *fakeStore) ReplaceProjectRefs(_ context.Context, scope identity.Scope, projectID string, refs []identity.TestUserRef) error {
	s.refs[refKey(scope, projectID)] = refs
	return nil
}

func (s *fakeStore) ListProjectRefs(_ context.Context, scope identity.Scope, projectID string) ([]identity.TestUserRef, error) {
	rows := append([]identity.TestUserRef(nil), s.refs[refKey(scope, projectID)]...)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].RoleName != rows[j].RoleName {
			return rows[i].RoleName < rows[j].RoleName
		}
		return rows[i].Username < rows[j].Username
	})
	return rows, nil
}

// ProjectsReferencing is SCOPE-fenced, mirroring the real store: it answers
// both the names the panel lists and the count its delete warning carries,
// which is sound only because an account exists on exactly one (org,
// environment) directory.
func (s *fakeStore) ProjectsReferencing(_ context.Context, scope identity.Scope, username string) ([]identity.TestUserRef, error) {
	var out []identity.TestUserRef
	for _, rows := range s.refs {
		for _, r := range rows {
			if r.OrgID == scope.OrgID && r.Environment == scope.Environment && r.Username == username {
				out = append(out, r)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProjectID < out[j].ProjectID })
	return out, nil
}

// ---- project-owned directory objects ---------------------------------------
//
// Converge semantics, mirroring the real store: a resource server upsert
// replaces the project's one row, and a bindings replace makes the payload the
// complete set for that project.

func (s *fakeStore) UpsertResourceServer(_ context.Context, rs identity.IdPResourceServer) error {
	scope := identity.Scope{OrgID: rs.OrgID, Environment: rs.Environment}
	s.resourceServers[refKey(scope, rs.ProjectID)] = rs
	return nil
}

func (s *fakeStore) GetResourceServer(_ context.Context, scope identity.Scope, projectID string) (*identity.IdPResourceServer, error) {
	if rs, ok := s.resourceServers[refKey(scope, projectID)]; ok {
		return &rs, nil
	}
	return nil, nil
}

func (s *fakeStore) DeleteResourceServer(_ context.Context, scope identity.Scope, projectID string) error {
	delete(s.resourceServers, refKey(scope, projectID))
	return nil
}

func (s *fakeStore) ReplaceRoleBindings(_ context.Context, scope identity.Scope, projectID string, bindings []identity.IdPRoleBinding) error {
	rows := make([]identity.IdPRoleBinding, 0, len(bindings))
	for _, b := range bindings {
		b.OrgID, b.Environment, b.ProjectID = scope.OrgID, scope.Environment, projectID
		rows = append(rows, b)
	}
	s.roleBindings[refKey(scope, projectID)] = rows
	return nil
}

func (s *fakeStore) ListRoleBindings(_ context.Context, scope identity.Scope, projectID string) ([]identity.IdPRoleBinding, error) {
	rows := append([]identity.IdPRoleBinding(nil), s.roleBindings[refKey(scope, projectID)]...)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Role != rows[j].Role {
			return rows[i].Role < rows[j].Role
		}
		return rows[i].GroupName < rows[j].GroupName
	})
	return rows, nil
}

// CountProjectsBindingGroup is SCOPE-fenced and counts DISTINCT projects, like
// the real store. The empty group name is the "assigned to nobody" marker, not
// a group, so it counts nothing.
func (s *fakeStore) CountProjectsBindingGroup(_ context.Context, scope identity.Scope, groupName string) (int, error) {
	if groupName == "" {
		return 0, nil
	}
	projects := map[string]struct{}{}
	for _, rows := range s.roleBindings {
		for _, b := range rows {
			if b.OrgID == scope.OrgID && b.Environment == scope.Environment && b.GroupName == groupName {
				projects[b.ProjectID] = struct{}{}
			}
		}
	}
	return len(projects), nil
}

// CountProjectsBindingGroups is the same count for every group at once, keyed by
// the LOWERCASED name — the key the store's GROUP BY produces.
func (s *fakeStore) CountProjectsBindingGroups(_ context.Context, scope identity.Scope) (map[string]int, error) {
	projects := map[string]map[string]struct{}{}
	for _, rows := range s.roleBindings {
		for _, b := range rows {
			if b.OrgID != scope.OrgID || b.Environment != scope.Environment || b.GroupName == "" {
				continue
			}
			key := strings.ToLower(b.GroupName)
			if projects[key] == nil {
				projects[key] = map[string]struct{}{}
			}
			projects[key][b.ProjectID] = struct{}{}
		}
	}
	out := make(map[string]int, len(projects))
	for key, ids := range projects {
		out[key] = len(ids)
	}
	return out, nil
}

func (s *fakeStore) DeleteRoleBindings(_ context.Context, scope identity.Scope, projectID string) error {
	delete(s.roleBindings, refKey(scope, projectID))
	return nil
}

var _ identity.Store = (*fakeStore)(nil)

// storedPassword / hasUser / hasRole are the read helpers the component tests
// assert through, so no test has to spell the composite key.
func (s *fakeStore) storedPassword(username string) string {
	return s.passwords[scopedKey(scopeOf(panelOrg), username)]
}

func (s *fakeStore) hasUser(username string) bool {
	_, ok := s.testUsers[scopedKey(scopeOf(panelOrg), username)]
	return ok
}

func (s *fakeStore) hasRole(name string) bool {
	_, ok := s.roles[scopedKey(scopeOf(panelOrg), strings.ToLower(name))]
	return ok
}

// fakeDirectory is the identity provider. `deleted` records the user ids the
// panel asked it to remove, which is how a test tells "the account is gone" from
// "only our row is gone".
//
// `ops` records the ORDER of the mutating calls, because the delete path's
// correctness is an ordering property: un-enrolling an account from its roles
// after deleting it would leave every one of those groups naming an id that no
// longer resolves — the state that destroys a role group on the next build. A
// set of calls cannot express that; a sequence can.
type fakeDirectory struct {
	groups       map[string]identity.DirectoryGroup
	members      map[string][]string
	accounts     map[string]identity.DirectoryAccount
	passwordsSet map[string]string
	// rolePermissions is what each of the project's roles grants, keyed by the
	// directory role id. It is the one project-owned verb the panel DOES reach
	// for: a login's scopes are the union of its roles' grants and no table
	// holds them.
	rolePermissions map[string][]string
	deleted         []string
	ops             []string
	err             error
	// failRemoveMembers fails the un-enrol without failing anything else, so a
	// test can prove the delete ABORTS rather than pressing on.
	failRemoveMembers error
}

func newFakeDirectory() *fakeDirectory {
	return &fakeDirectory{
		groups:          map[string]identity.DirectoryGroup{},
		members:         map[string][]string{},
		accounts:        map[string]identity.DirectoryAccount{},
		passwordsSet:    map[string]string{},
		rolePermissions: map[string][]string{},
	}
}

func (d *fakeDirectory) withGroup(name, description string, memberIDs ...string) *fakeDirectory {
	id := "grp-" + name
	d.groups[strings.ToLower(name)] = identity.DirectoryGroup{ID: id, Name: name, Description: description}
	d.members[id] = memberIDs
	return d
}

func (d *fakeDirectory) withAccount(username string) *fakeDirectory {
	d.accounts[username] = identity.DirectoryAccount{ID: "usr-" + username, Username: username}
	return d
}

// withProjectRole puts one of the project's own roles on the directory, with
// what it grants. Keyed by the directory role id, because that is the only
// handle the panel has for a role: it reads the id off the platform's binding
// row and asks the directory what that role grants.
func (d *fakeDirectory) withProjectRole(roleID string, permissions ...string) *fakeDirectory {
	d.rolePermissions[roleID] = permissions
	return d
}

func (d *fakeDirectory) ListGroups(context.Context) ([]identity.DirectoryGroup, error) {
	if d.err != nil {
		return nil, d.err
	}
	out := make([]identity.DirectoryGroup, 0, len(d.groups))
	for _, g := range d.groups {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (d *fakeDirectory) FindGroupByName(_ context.Context, name string) (*identity.DirectoryGroup, bool, error) {
	if d.err != nil {
		return nil, false, d.err
	}
	g, ok := d.groups[strings.ToLower(name)]
	if !ok {
		return nil, false, nil
	}
	return &g, true, nil
}

func (d *fakeDirectory) GroupMembers(_ context.Context, groupID string) ([]string, error) {
	if d.err != nil {
		return nil, d.err
	}
	return d.members[groupID], nil
}

func (d *fakeDirectory) CreateGroup(_ context.Context, name, description string, memberIDs []string) (identity.DirectoryGroup, error) {
	d.withGroup(name, description, memberIDs...)
	return d.groups[strings.ToLower(name)], nil
}

func (d *fakeDirectory) AddMembers(_ context.Context, group identity.DirectoryGroup, memberIDs []string) (identity.DirectoryGroup, error) {
	d.members[group.ID] = append(d.members[group.ID], memberIDs...)
	d.ops = append(d.ops, "AddMembers:"+group.Name)
	return group, nil
}

func (d *fakeDirectory) RemoveMembers(_ context.Context, group identity.DirectoryGroup, memberIDs []string) (identity.DirectoryGroup, error) {
	d.ops = append(d.ops, "RemoveMembers:"+group.Name)
	if d.failRemoveMembers != nil {
		return identity.DirectoryGroup{}, d.failRemoveMembers
	}
	if d.err != nil {
		return identity.DirectoryGroup{}, d.err
	}
	drop := map[string]bool{}
	for _, id := range memberIDs {
		drop[id] = true
	}
	var remaining []string
	for _, id := range d.members[group.ID] {
		if !drop[id] {
			remaining = append(remaining, id)
		}
	}
	d.members[group.ID] = remaining
	return group, nil
}

func (d *fakeDirectory) UserGroups(_ context.Context, userID string) ([]identity.DirectoryGroup, error) {
	if d.err != nil {
		return nil, d.err
	}
	var out []identity.DirectoryGroup
	for _, g := range d.groups {
		for _, m := range d.members[g.ID] {
			if m == userID {
				out = append(out, g)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (d *fakeDirectory) DeleteGroup(_ context.Context, groupID string) error {
	for k, g := range d.groups {
		if g.ID == groupID {
			delete(d.groups, k)
		}
	}
	return nil
}

func (d *fakeDirectory) FindUserByUsername(_ context.Context, username string) (*identity.DirectoryAccount, bool, error) {
	if d.err != nil {
		return nil, false, d.err
	}
	a, ok := d.accounts[username]
	if !ok {
		return nil, false, nil
	}
	return &a, true, nil
}

func (d *fakeDirectory) CreateUser(_ context.Context, username, email, _ string) (identity.DirectoryAccount, error) {
	a := identity.DirectoryAccount{ID: "usr-" + username, Username: username, Email: email}
	d.accounts[username] = a
	return a, nil
}

func (d *fakeDirectory) SetUserPassword(_ context.Context, userID, password string) error {
	if d.err != nil {
		return d.err
	}
	d.passwordsSet[userID] = password
	return nil
}

// DeleteUser removes the account and, like the real identity provider, leaves
// every group member list that names it untouched. Nothing here repairs that —
// the panel has to have un-enrolled first.
func (d *fakeDirectory) DeleteUser(_ context.Context, userID string) error {
	d.ops = append(d.ops, "DeleteUser:"+userID)
	if d.err != nil {
		return d.err
	}
	for k, a := range d.accounts {
		if a.ID == userID {
			delete(d.accounts, k)
		}
	}
	d.deleted = append(d.deleted, userID)
	return nil
}

// danglingMembers is every member id across every group that no account has.
// The invariant the delete path exists to preserve is that this stays empty.
func (d *fakeDirectory) danglingMembers() []string {
	have := map[string]bool{}
	for _, a := range d.accounts {
		have[a.ID] = true
	}
	var out []string
	for _, g := range d.groups {
		for _, m := range d.members[g.ID] {
			if !have[m] {
				out = append(out, g.Name+"/"+m)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ---- the project-owned half of the port ------------------------------------
//
// The panel's surface is accounts and org groups: it reads and rotates test
// users and lists the roles the platform recorded, and it never touches a
// resource server, a catalog entry or a role assignment — those belong to the
// build-time ensure. The verbs are implemented here only because the fake has
// to satisfy the whole port, and each one REFUSES rather than pretending to
// work, so a panel change that reached for one fails with a sentence instead of
// quietly passing against a stub that answered nothing.

var errNotThePanelsSurface = errors.New("fake directory: the panel does not use the project-owned directory verbs")

func (d *fakeDirectory) EnsureResourceServer(context.Context, string, string) (identity.DirectoryID, error) {
	return "", errNotThePanelsSurface
}

func (d *fakeDirectory) FindResourceServer(context.Context, string) (identity.DirectoryID, bool, error) {
	return "", false, errNotThePanelsSurface
}

func (d *fakeDirectory) ListResources(context.Context, identity.DirectoryID) ([]identity.DirectoryResource, error) {
	return nil, errNotThePanelsSurface
}

func (d *fakeDirectory) CreateResource(context.Context, identity.DirectoryID, string, string, string) (identity.DirectoryResource, error) {
	return identity.DirectoryResource{}, errNotThePanelsSurface
}

func (d *fakeDirectory) UpdateResource(context.Context, identity.DirectoryID, identity.DirectoryID, string, string) error {
	return errNotThePanelsSurface
}

func (d *fakeDirectory) DeleteResource(context.Context, identity.DirectoryID, identity.DirectoryID) error {
	return errNotThePanelsSurface
}

func (d *fakeDirectory) ListActions(context.Context, identity.DirectoryID, identity.DirectoryID) ([]identity.DirectoryAction, error) {
	return nil, errNotThePanelsSurface
}

func (d *fakeDirectory) CreateAction(context.Context, identity.DirectoryID, identity.DirectoryID, string, string, string) (identity.DirectoryAction, error) {
	return identity.DirectoryAction{}, errNotThePanelsSurface
}

func (d *fakeDirectory) UpdateAction(context.Context, identity.DirectoryID, identity.DirectoryID, identity.DirectoryID, string, string) error {
	return errNotThePanelsSurface
}

func (d *fakeDirectory) DeleteAction(context.Context, identity.DirectoryID, identity.DirectoryID, identity.DirectoryID) error {
	return errNotThePanelsSurface
}

func (d *fakeDirectory) DeleteResourceServer(context.Context, identity.DirectoryID) error {
	return errNotThePanelsSurface
}

func (d *fakeDirectory) EnsureRole(context.Context, identity.DirectoryID, string, string, identity.DirectoryID, []string) (identity.DirectoryID, error) {
	return "", errNotThePanelsSurface
}

func (d *fakeDirectory) ListRoles(context.Context) ([]identity.RoleRef, error) {
	return nil, errNotThePanelsSurface
}

// ListRolePermissions is implemented rather than refused: the panel reads a
// login's scopes through it. A role the fixture did not seed answers nothing,
// which is the "role is gone from the directory" case.
func (d *fakeDirectory) ListRolePermissions(_ context.Context, role identity.DirectoryID) ([]string, error) {
	if d.err != nil {
		return nil, d.err
	}
	return append([]string(nil), d.rolePermissions[string(role)]...), nil
}

func (d *fakeDirectory) DeleteRole(context.Context, identity.DirectoryID) error {
	return errNotThePanelsSurface
}

func (d *fakeDirectory) AssignRole(context.Context, identity.DirectoryID, identity.Principal) error {
	return errNotThePanelsSurface
}

func (d *fakeDirectory) UnassignRole(context.Context, identity.DirectoryID, identity.Principal) error {
	return errNotThePanelsSurface
}

func (d *fakeDirectory) ListRoleAssignments(context.Context, identity.DirectoryID) ([]identity.Principal, error) {
	return nil, errNotThePanelsSurface
}

var _ identity.Directory = (*fakeDirectory)(nil)
