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

// fakes_test.go — the in-memory stand-ins the ensure tests drive.
//
// They are not mocks with expectations: they are working implementations that
// keep state and RECORD every call in order, because most of what this domain
// promises is about calls NOT made — no CreateGroup on somebody else's group,
// no CreateUser on a real person's account, no second create on a re-run. An
// expectation-style mock can only assert what did happen.
//
// fakeDirectory reproduces the two IdP behaviours the ensure is written around,
// because a fake that smooths them over would make the ensure look correct for
// the wrong reason:
//
//   - membership is settable only at create, so AddMembers is a
//     delete-and-recreate that hands back a NEW group id;
//   - that recreate is skipped when every id is already in the group, which is
//     what keeps an unchanged re-run from churning the group's identity.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ---- directory ------------------------------------------------------------

// dirCall is one recorded call on the fake directory.
type dirCall struct {
	// Op is the interface method name.
	Op string
	// Target is the group or user the call named, for readable failures.
	Target string
	// Members is the id list a CreateGroup/AddMembers carried.
	Members []string
	// NoOp marks an AddMembers whose ids were all already in the group. The IdP
	// client returns early in that case, so the group keeps its identity — the
	// difference between "the ensure asked" and "the directory changed".
	NoOp bool
}

// writeOps are the calls that change the directory. Lookups are recorded too
// (a lost lookup would be a real behaviour change) but they say nothing about
// what the ensure did, so order and count assertions filter to these.
var writeOps = map[string]bool{
	"CreateGroup": true, "AddMembers": true, "DeleteGroup": true,
	"CreateUser": true, "SetUserPassword": true, "DeleteUser": true,
}

type fakeDirectory struct {
	// groups is keyed by lowercased name: the directory treats two names
	// differing only in case as one group, as the rest of the platform does.
	groups map[string]DirectoryGroup
	// members is keyed by group id, so a recreate leaves the old id's list
	// behind exactly as a real delete-and-recreate would.
	members map[string][]string
	users   map[string]DirectoryAccount
	// passwords records what was handed to the directory, keyed by account id.
	// It is the only way to see that a real person's password was never touched.
	passwords map[string]string

	calls  []dirCall
	nextID int

	// failOn injects a failure for one method name, for the error paths.
	failOn map[string]error
}

func newFakeDirectory() *fakeDirectory {
	return &fakeDirectory{
		groups:    map[string]DirectoryGroup{},
		members:   map[string][]string{},
		users:     map[string]DirectoryAccount{},
		passwords: map[string]string{},
		failOn:    map[string]error{},
	}
}

// seedGroup puts a group on the directory that the platform did not create —
// the `Administrators` case.
func (d *fakeDirectory) seedGroup(name string, memberIDs ...string) DirectoryGroup {
	d.nextID++
	g := DirectoryGroup{ID: fmt.Sprintf("grp-%d", d.nextID), Name: name, Description: "seeded"}
	d.groups[strings.ToLower(name)] = g
	d.members[g.ID] = append([]string(nil), memberIDs...)
	return g
}

// seedUser puts an account on the directory that the platform does not own —
// the `jsmith` case.
func (d *fakeDirectory) seedUser(username string) DirectoryAccount {
	d.nextID++
	a := DirectoryAccount{ID: fmt.Sprintf("usr-%d", d.nextID), Username: username, Email: username + "@example.com"}
	d.users[username] = a
	return a
}

func (d *fakeDirectory) record(c dirCall) { d.calls = append(d.calls, c) }

func (d *fakeDirectory) fail(op string) error { return d.failOn[op] }

// writes returns the recorded calls that changed the directory, in order.
func (d *fakeDirectory) writes() []dirCall {
	var out []dirCall
	for _, c := range d.calls {
		if !writeOps[c.Op] {
			continue
		}
		// A no-op AddMembers reached the client but changed nothing, so it is not
		// a write.
		if c.Op == "AddMembers" && c.NoOp {
			continue
		}
		out = append(out, c)
	}
	return out
}

// countOp counts every recorded call of an op, no-ops included.
func (d *fakeDirectory) countOp(op string) int {
	n := 0
	for _, c := range d.calls {
		if c.Op == op {
			n++
		}
	}
	return n
}

// memberSet returns the current members of a group by name.
func (d *fakeDirectory) memberSet(name string) []string {
	g, ok := d.groups[strings.ToLower(name)]
	if !ok {
		return nil
	}
	out := append([]string(nil), d.members[g.ID]...)
	sort.Strings(out)
	return out
}

func (d *fakeDirectory) ListGroups(_ context.Context) ([]DirectoryGroup, error) {
	d.record(dirCall{Op: "ListGroups"})
	if err := d.fail("ListGroups"); err != nil {
		return nil, err
	}
	out := make([]DirectoryGroup, 0, len(d.groups))
	for _, g := range d.groups {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (d *fakeDirectory) GroupMembers(_ context.Context, groupID string) ([]string, error) {
	d.record(dirCall{Op: "GroupMembers", Target: groupID})
	if err := d.fail("GroupMembers"); err != nil {
		return nil, err
	}
	return append([]string(nil), d.members[groupID]...), nil
}

func (d *fakeDirectory) FindGroupByName(_ context.Context, name string) (*DirectoryGroup, bool, error) {
	d.record(dirCall{Op: "FindGroupByName", Target: name})
	if err := d.fail("FindGroupByName"); err != nil {
		return nil, false, err
	}
	g, ok := d.groups[strings.ToLower(name)]
	if !ok {
		return nil, false, nil
	}
	found := g
	return &found, true, nil
}

func (d *fakeDirectory) CreateGroup(_ context.Context, name, description string, memberIDs []string) (DirectoryGroup, error) {
	d.record(dirCall{Op: "CreateGroup", Target: name, Members: append([]string(nil), memberIDs...)})
	if err := d.fail("CreateGroup"); err != nil {
		return DirectoryGroup{}, err
	}
	if _, exists := d.groups[strings.ToLower(name)]; exists {
		return DirectoryGroup{}, fmt.Errorf("fake directory: group %q already exists", name)
	}
	d.nextID++
	g := DirectoryGroup{ID: fmt.Sprintf("grp-%d", d.nextID), Name: name, Description: description}
	d.groups[strings.ToLower(name)] = g
	d.members[g.ID] = append([]string(nil), memberIDs...)
	return g, nil
}

// AddMembers reproduces the IdP's only membership write: delete the group and
// recreate it with the union, which mints a NEW group id. When the union is the
// set already there the client returns the group untouched, so an unchanged
// re-run cannot churn the id.
func (d *fakeDirectory) AddMembers(_ context.Context, group DirectoryGroup, memberIDs []string) (DirectoryGroup, error) {
	existing := d.members[group.ID]
	have := map[string]bool{}
	for _, id := range existing {
		have[id] = true
	}
	var added []string
	for _, id := range memberIDs {
		if !have[id] {
			have[id] = true
			added = append(added, id)
		}
	}
	d.record(dirCall{Op: "AddMembers", Target: group.Name, Members: added, NoOp: len(added) == 0})
	if err := d.fail("AddMembers"); err != nil {
		return DirectoryGroup{}, err
	}
	if len(added) == 0 {
		return group, nil
	}
	delete(d.members, group.ID)
	d.nextID++
	recreated := DirectoryGroup{
		ID: fmt.Sprintf("grp-%d", d.nextID), Name: group.Name,
		Description: group.Description, OUID: group.OUID,
	}
	d.groups[strings.ToLower(group.Name)] = recreated
	d.members[recreated.ID] = append(append([]string(nil), existing...), added...)
	return recreated, nil
}

func (d *fakeDirectory) DeleteGroup(_ context.Context, groupID string) error {
	d.record(dirCall{Op: "DeleteGroup", Target: groupID})
	if err := d.fail("DeleteGroup"); err != nil {
		return err
	}
	for key, g := range d.groups {
		if g.ID == groupID {
			delete(d.groups, key)
			delete(d.members, groupID)
		}
	}
	return nil
}

func (d *fakeDirectory) FindUserByUsername(_ context.Context, username string) (*DirectoryAccount, bool, error) {
	d.record(dirCall{Op: "FindUserByUsername", Target: username})
	if err := d.fail("FindUserByUsername"); err != nil {
		return nil, false, err
	}
	a, ok := d.users[username]
	if !ok {
		return nil, false, nil
	}
	found := a
	return &found, true, nil
}

func (d *fakeDirectory) CreateUser(_ context.Context, username, email, password string) (DirectoryAccount, error) {
	d.record(dirCall{Op: "CreateUser", Target: username})
	if err := d.fail("CreateUser"); err != nil {
		return DirectoryAccount{}, err
	}
	if _, exists := d.users[username]; exists {
		return DirectoryAccount{}, fmt.Errorf("fake directory: user %q already exists", username)
	}
	d.nextID++
	a := DirectoryAccount{ID: fmt.Sprintf("usr-%d", d.nextID), Username: username, Email: email}
	d.users[username] = a
	d.passwords[a.ID] = password
	return a, nil
}

func (d *fakeDirectory) SetUserPassword(_ context.Context, userID, password string) error {
	d.record(dirCall{Op: "SetUserPassword", Target: userID})
	if err := d.fail("SetUserPassword"); err != nil {
		return err
	}
	d.passwords[userID] = password
	return nil
}

func (d *fakeDirectory) DeleteUser(_ context.Context, userID string) error {
	d.record(dirCall{Op: "DeleteUser", Target: userID})
	if err := d.fail("DeleteUser"); err != nil {
		return err
	}
	for name, a := range d.users {
		if a.ID == userID {
			delete(d.users, name)
			delete(d.passwords, userID)
		}
	}
	return nil
}

// ---- store ----------------------------------------------------------------

// fakeStore is an in-memory Store. It seals nothing — the password map IS the
// sealed column — because the seal is the repository's job and is pinned by the
// DB tier; here the point is only WHICH password reached the store, and when.
type fakeStore struct {
	// roles is keyed by lowercased name, matching the real store's
	// case-insensitive GetRole.
	roles map[string]IdPRole
	users map[string]TestUser
	// passwords is the sealed column, keyed by username.
	passwords map[string]string
	// refs is keyed by org + "/" + project.
	refs map[string][]TestUserRef

	// replaceCalls records each ReplaceProjectRefs payload, so a test can assert
	// the ensure rewrote the whole set exactly once.
	replaceCalls [][]TestUserRef
	// revealCalls counts openings of the sealed password. Reuse must not read a
	// credential it has no reason to read, and counting is the only way to see
	// a read that had no visible effect.
	revealCalls int
	// upsertUserCalls counts whole-row writes to test_users, which rewrite the
	// sealed column.
	upsertUserCalls int

	failOn map[string]error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		roles: map[string]IdPRole{}, users: map[string]TestUser{},
		passwords: map[string]string{}, refs: map[string][]TestUserRef{},
		failOn: map[string]error{},
	}
}

func refKey(orgID, projectID string) string { return orgID + "/" + projectID }

func (s *fakeStore) GetRole(_ context.Context, name string) (*IdPRole, error) {
	if err := s.failOn["GetRole"]; err != nil {
		return nil, err
	}
	row, ok := s.roles[strings.ToLower(name)]
	if !ok {
		return nil, nil
	}
	return &row, nil
}

func (s *fakeStore) ListRoles(_ context.Context) ([]IdPRole, error) {
	out := make([]IdPRole, 0, len(s.roles))
	for _, r := range s.roles {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// UpsertRole overwrites wholesale, unlike the real store's on-conflict clause
// that pins created_by_*. Keeping the fake dumb is deliberate: it means a
// provenance assertion here is about what the ENSURE passed down, not about a
// clause in the SQL. The clause itself is pinned in the DB tier.
func (s *fakeStore) UpsertRole(_ context.Context, role IdPRole) error {
	if err := s.failOn["UpsertRole"]; err != nil {
		return err
	}
	s.roles[strings.ToLower(role.Name)] = role
	return nil
}

func (s *fakeStore) DeleteRole(_ context.Context, name string) error {
	delete(s.roles, strings.ToLower(name))
	return nil
}

func (s *fakeStore) GetTestUser(_ context.Context, username string) (*TestUser, error) {
	if err := s.failOn["GetTestUser"]; err != nil {
		return nil, err
	}
	row, ok := s.users[username]
	if !ok {
		return nil, nil
	}
	return &row, nil
}

func (s *fakeStore) UpsertTestUser(_ context.Context, user TestUser, password string) error {
	if err := s.failOn["UpsertTestUser"]; err != nil {
		return err
	}
	s.upsertUserCalls++
	s.users[user.Username] = user
	s.passwords[user.Username] = password
	return nil
}

// UpdateTestUserFacts touches the two metadata columns and nothing else — in
// particular it does not go near the password map, which is what makes the
// "reuse never reads the credential" assertion meaningful.
func (s *fakeStore) UpdateTestUserFacts(_ context.Context, username, thunderUserID, roleName string) error {
	if err := s.failOn["UpdateTestUserFacts"]; err != nil {
		return err
	}
	row, ok := s.users[username]
	if !ok {
		return fmt.Errorf("fake store: no account %q", username)
	}
	row.ThunderUserID, row.RoleName = thunderUserID, roleName
	s.users[username] = row
	return nil
}

func (s *fakeStore) SetTestUserPassword(_ context.Context, username, password string) error {
	if _, ok := s.users[username]; !ok {
		return fmt.Errorf("fake store: no account %q", username)
	}
	s.passwords[username] = password
	return nil
}

func (s *fakeStore) RevealTestUserPassword(_ context.Context, username string) (string, error) {
	s.revealCalls++
	if err := s.failOn["RevealTestUserPassword"]; err != nil {
		return "", err
	}
	pw, ok := s.passwords[username]
	if !ok || pw == "" {
		return "", ErrNoPassword
	}
	return pw, nil
}

func (s *fakeStore) DeleteTestUser(_ context.Context, username string) error {
	delete(s.users, username)
	delete(s.passwords, username)
	for key, rows := range s.refs {
		var kept []TestUserRef
		for _, r := range rows {
			if r.Username != username {
				kept = append(kept, r)
			}
		}
		s.refs[key] = kept
	}
	return nil
}

func (s *fakeStore) ReplaceProjectRefs(_ context.Context, orgID, projectID string, refs []TestUserRef) error {
	if err := s.failOn["ReplaceProjectRefs"]; err != nil {
		return err
	}
	stamped := make([]TestUserRef, 0, len(refs))
	for _, r := range refs {
		r.OrgID, r.ProjectID = orgID, projectID
		stamped = append(stamped, r)
	}
	s.refs[refKey(orgID, projectID)] = stamped
	s.replaceCalls = append(s.replaceCalls, stamped)
	return nil
}

func (s *fakeStore) ListProjectRefs(_ context.Context, orgID, projectID string) ([]TestUserRef, error) {
	return s.refs[refKey(orgID, projectID)], nil
}

func (s *fakeStore) ProjectsReferencing(_ context.Context, orgID, username string) ([]TestUserRef, error) {
	var out []TestUserRef
	for _, rows := range s.refs {
		for _, r := range rows {
			if r.OrgID == orgID && r.Username == username {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

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

// fakeDesign is the design bundle at a tag. `calls` counts reads, so a test can
// pin that the ensure does not go back to git twice in one build.
type fakeDesign struct {
	bundle map[string]string
	err    error
	calls  int
}

func (f *fakeDesign) GetDesignAtTag(_ context.Context, _, _, _ string) (map[string]string, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.bundle, nil
}

var errDesignUnavailable = errors.New("git: transient read failure")
