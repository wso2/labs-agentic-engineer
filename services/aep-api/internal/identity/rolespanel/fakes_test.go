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

// fakeStore is an in-memory identity.Store. Passwords are kept in the clear —
// the real sealing is the ColumnCipher's job and is covered by the repository's
// own tests; what matters here is WHICH password the panel stored.
type fakeStore struct {
	roles     map[string]identity.IdPRole
	testUsers map[string]identity.TestUser
	passwords map[string]string
	// refs is keyed org/project → the rows that project references.
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

func refKey(orgID, projectID string) string { return orgID + "/" + projectID }

// withRole records a role the platform created.
func (s *fakeStore) withRole(name string) *fakeStore {
	s.roles[strings.ToLower(name)] = identity.IdPRole{Name: name, ThunderGroupID: "grp-" + name}
	return s
}

// withOwnedUser records an account the platform owns, with its sealed password.
func (s *fakeStore) withOwnedUser(username, role, password string) *fakeStore {
	s.testUsers[username] = identity.TestUser{
		Username: username, ThunderUserID: "usr-" + username, RoleName: role,
	}
	s.passwords[username] = password
	return s
}

// withRef records that org/project's design references username. It is the ONLY
// project-scoped fact in this domain, and therefore the whole org+project fence.
func (s *fakeStore) withRef(orgID, projectID, username, role string) *fakeStore {
	k := refKey(orgID, projectID)
	s.refs[k] = append(s.refs[k], identity.TestUserRef{
		OrgID: orgID, ProjectID: projectID, Username: username, RoleName: role,
	})
	return s
}

func (s *fakeStore) GetRole(_ context.Context, name string) (*identity.IdPRole, error) {
	if r, ok := s.roles[strings.ToLower(name)]; ok {
		return &r, nil
	}
	return nil, nil
}

func (s *fakeStore) ListRoles(context.Context) ([]identity.IdPRole, error) {
	out := make([]identity.IdPRole, 0, len(s.roles))
	for _, r := range s.roles {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *fakeStore) UpsertRole(_ context.Context, role identity.IdPRole) error {
	s.roles[strings.ToLower(role.Name)] = role
	return nil
}

func (s *fakeStore) DeleteRole(_ context.Context, name string) error {
	delete(s.roles, strings.ToLower(name))
	return nil
}

func (s *fakeStore) GetTestUser(_ context.Context, username string) (*identity.TestUser, error) {
	if u, ok := s.testUsers[username]; ok {
		return &u, nil
	}
	return nil, nil
}

func (s *fakeStore) UpsertTestUser(_ context.Context, user identity.TestUser, password string) error {
	s.testUsers[user.Username] = user
	s.passwords[user.Username] = password
	return nil
}

func (s *fakeStore) UpdateTestUserFacts(_ context.Context, username, thunderUserID, roleName string) error {
	u, ok := s.testUsers[username]
	if !ok {
		return errors.New("no such account")
	}
	u.ThunderUserID, u.RoleName = thunderUserID, roleName
	s.testUsers[username] = u
	return nil
}

func (s *fakeStore) SetTestUserPassword(_ context.Context, username, password string) error {
	if s.setPasswordErr != nil {
		return s.setPasswordErr
	}
	u, ok := s.testUsers[username]
	if !ok {
		return errors.New("no such account")
	}
	now := time.Now().UTC()
	u.RotatedAt = &now
	s.testUsers[username] = u
	s.passwords[username] = password
	return nil
}

func (s *fakeStore) RevealTestUserPassword(_ context.Context, username string) (string, error) {
	p, ok := s.passwords[username]
	if !ok || p == "" {
		return "", identity.ErrNoPassword
	}
	return p, nil
}

func (s *fakeStore) DeleteTestUser(_ context.Context, username string) error {
	delete(s.testUsers, username)
	delete(s.passwords, username)
	for k, rows := range s.refs {
		var kept []identity.TestUserRef
		for _, r := range rows {
			if r.Username != username {
				kept = append(kept, r)
			}
		}
		s.refs[k] = kept
	}
	return nil
}

func (s *fakeStore) ReplaceProjectRefs(_ context.Context, orgID, projectID string, refs []identity.TestUserRef) error {
	s.refs[refKey(orgID, projectID)] = refs
	return nil
}

func (s *fakeStore) ListProjectRefs(_ context.Context, orgID, projectID string) ([]identity.TestUserRef, error) {
	rows := append([]identity.TestUserRef(nil), s.refs[refKey(orgID, projectID)]...)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].RoleName != rows[j].RoleName {
			return rows[i].RoleName < rows[j].RoleName
		}
		return rows[i].Username < rows[j].Username
	})
	return rows, nil
}

func (s *fakeStore) ProjectsReferencing(_ context.Context, orgID, username string) ([]identity.TestUserRef, error) {
	var out []identity.TestUserRef
	for _, rows := range s.refs {
		for _, r := range rows {
			if r.OrgID == orgID && r.Username == username {
				out = append(out, r)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProjectID < out[j].ProjectID })
	return out, nil
}

// CountReferencing is org-BLIND on purpose, mirroring the real store: it is the
// bare cross-org total that makes the delete warning true.
func (s *fakeStore) CountReferencing(_ context.Context, username string) (int, error) {
	n := 0
	for _, rows := range s.refs {
		for _, r := range rows {
			if r.Username == username {
				n++
			}
		}
	}
	return n, nil
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
