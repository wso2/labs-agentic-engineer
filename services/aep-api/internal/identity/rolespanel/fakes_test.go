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
type fakeDirectory struct {
	groups       map[string]identity.DirectoryGroup
	members      map[string][]string
	accounts     map[string]identity.DirectoryAccount
	passwordsSet map[string]string
	deleted      []string
	err          error
}

func newFakeDirectory() *fakeDirectory {
	return &fakeDirectory{
		groups:       map[string]identity.DirectoryGroup{},
		members:      map[string][]string{},
		accounts:     map[string]identity.DirectoryAccount{},
		passwordsSet: map[string]string{},
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
	return group, nil
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

func (d *fakeDirectory) DeleteUser(_ context.Context, userID string) error {
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

var _ identity.Directory = (*fakeDirectory)(nil)
