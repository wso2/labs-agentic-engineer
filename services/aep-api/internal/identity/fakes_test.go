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
//     what keeps an unchanged re-run from churning the group's identity;
//   - **the IdP has no referential integrity between accounts and group
//     membership.** DeleteUser here leaves the member lists that name the
//     account exactly as they were, and a create carrying an id no account
//     has is REFUSED — both as the real one behaves. A fake where those two
//     could not disagree is why a code path with tests to spare still
//     destroyed nine role groups in production: the state that breaks it was
//     unrepresentable, so no test could ask for it.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
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
	// NoOp marks a call that reached the client and changed nothing: an
	// AddMembers whose ids were all already in the group (the IdP client returns
	// early, so the group keeps its identity), or an AssignRole for a principal
	// that already holds the role. It is the difference between "the ensure
	// asked" and "the directory changed".
	NoOp bool
}

// line renders one call for the Calls log, in a format a test can spell out in
// full:
//
//	"<Op> <target>"            — the common case, target being the object's NAME
//	                             (group, username, role, identifier, handle),
//	                             never its id, so a failure is readable
//	"<Op> <target> [a,b]"      — when the call carried a list: the member ids of
//	                             a group write, or the permission handles of a
//	                             role write
//	"<Op> <target> (noop)"     — reached the directory, changed nothing
func (c dirCall) line() string {
	out := c.Op
	if c.Target != "" {
		out += " " + c.Target
	}
	if len(c.Members) > 0 {
		out += " [" + strings.Join(c.Members, ",") + "]"
	}
	if c.NoOp {
		out += " (noop)"
	}
	return out
}

// writeOps are the calls that change the directory. Lookups are recorded too
// (a lost lookup would be a real behaviour change) but they say nothing about
// what the ensure did, so order and count assertions filter to these.
//
// The `Ensure…` verbs are NOT here: each is a find that may or may not write,
// and it records the write it actually performed (CreateResourceServer,
// CreateRole, UpdateRole) as a separate call. That split is what makes
// "a rebuild of the same tag writes nothing" an assertion about the directory
// rather than about how many times the ensure asked.
var writeOps = map[string]bool{
	"CreateGroup": true, "AddMembers": true, "RemoveMembers": true, "DeleteGroup": true,
	"CreateUser": true, "SetUserPassword": true, "DeleteUser": true,
	"CreateResourceServer": true, "DeleteResourceServer": true,
	"CreateResource": true, "UpdateResource": true, "DeleteResource": true,
	"CreateAction": true, "UpdateAction": true, "DeleteAction": true,
	"CreateRole": true, "UpdateRole": true, "DeleteRole": true,
	"AssignRole": true, "UnassignRole": true,
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

	// resourceServers, resources, actions and roles are the project-owned half.
	// They are SLICES, not maps: the converge's output is order-sensitive to
	// read, and a listing that came back in map order would let a test pass for
	// a reason no directory reproduces.
	resourceServers []fakeResourceServer
	resources       []fakeResource
	actions         []fakeAction
	roles           []fakeRole

	calls []dirCall
	// Calls is the same log rendered one line per call — see dirCall.line. It is
	// what a converge test asserts on, because the interesting property is a
	// SEQUENCE ("these two lookups and nothing else"), which reads as a string
	// slice and not as a struct comparison.
	Calls  []string
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
// the `Administrators` case. The member ids are taken as given: seeding one
// that no account has is how a test asks for the dangling-member state.
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

func (d *fakeDirectory) record(c dirCall) {
	d.calls = append(d.calls, c)
	d.Calls = append(d.Calls, c.line())
}

func (d *fakeDirectory) fail(op string) error { return d.failOn[op] }

// writes returns the recorded calls that changed the directory, in order.
func (d *fakeDirectory) writes() []dirCall {
	var out []dirCall
	for _, c := range d.calls {
		if !writeOps[c.Op] {
			continue
		}
		// A no-op call reached the client but changed nothing, so it is not a
		// write — an AddMembers whose ids were all there, an AssignRole for a
		// principal that already holds the role.
		if c.NoOp {
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
	// GRP-1007: the IdP rejects the whole create for one member it does not
	// have. The client is expected to have resolved the ids first.
	for _, id := range memberIDs {
		if !d.hasAccountID(id) {
			return DirectoryGroup{}, fmt.Errorf("fake directory: group %q: invalid user member id %q", name, id)
		}
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
	// An account to ENROL that the directory does not have is an error; a
	// member the group merely carries and the directory has lost is dropped.
	// The real client draws exactly this line — see rewriteGroupMembers.
	for _, id := range added {
		if !d.hasAccountID(id) {
			return DirectoryGroup{}, fmt.Errorf("fake directory: group %q: invalid user member id %q", group.Name, id)
		}
	}
	delete(d.members, group.ID)
	d.nextID++
	recreated := DirectoryGroup{
		ID: fmt.Sprintf("grp-%d", d.nextID), Name: group.Name,
		Description: group.Description, OUID: group.OUID,
	}
	d.groups[strings.ToLower(group.Name)] = recreated
	d.members[recreated.ID] = append(d.liveMembers(existing), added...)
	return recreated, nil
}

// hasAccountID reports whether any account on the directory has this id. The
// port speaks ids and the accounts are keyed by username, so the reverse
// lookup is what makes the referential rule checkable.
func (d *fakeDirectory) hasAccountID(id string) bool {
	for _, a := range d.users {
		if a.ID == id {
			return true
		}
	}
	return false
}

// liveMembers is the real client's guarantee, modelled: a member the directory
// no longer has is dropped from a rewrite rather than replayed into it.
func (d *fakeDirectory) liveMembers(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if d.hasAccountID(id) {
			out = append(out, id)
		}
	}
	return out
}

// RemoveMembers is the un-enrol half: the same delete-and-recreate, minus the
// ids being removed, with a new group id when anything changed.
func (d *fakeDirectory) RemoveMembers(_ context.Context, group DirectoryGroup, memberIDs []string) (DirectoryGroup, error) {
	drop := map[string]bool{}
	for _, id := range memberIDs {
		drop[id] = true
	}
	existing := d.members[group.ID]
	var remaining, removed []string
	for _, id := range existing {
		if drop[id] {
			removed = append(removed, id)
			continue
		}
		remaining = append(remaining, id)
	}
	d.record(dirCall{Op: "RemoveMembers", Target: group.Name, Members: removed, NoOp: len(removed) == 0})
	if err := d.fail("RemoveMembers"); err != nil {
		return DirectoryGroup{}, err
	}
	if len(removed) == 0 {
		return group, nil
	}
	delete(d.members, group.ID)
	d.nextID++
	recreated := DirectoryGroup{
		ID: fmt.Sprintf("grp-%d", d.nextID), Name: group.Name,
		Description: group.Description, OUID: group.OUID,
	}
	d.groups[strings.ToLower(group.Name)] = recreated
	// The survivors go through the same liveness filter a real rewrite applies.
	d.members[recreated.ID] = d.liveMembers(remaining)
	return recreated, nil
}

// UserGroups is the reverse membership read. An account the directory does not
// have reports NO groups rather than an error — the real one 404s, and a
// retried delete must not fail on its opening move.
func (d *fakeDirectory) UserGroups(_ context.Context, userID string) ([]DirectoryGroup, error) {
	d.record(dirCall{Op: "UserGroups", Target: userID})
	if err := d.fail("UserGroups"); err != nil {
		return nil, err
	}
	if !d.hasAccountID(userID) {
		return nil, nil
	}
	var out []DirectoryGroup
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

// ---- directory: the project's authorization objects ------------------------

// The second half of the fake models the project-owned tree — resource server,
// resources, actions, roles, assignments — and every rule below was MEASURED on
// the directory (spike P1 §1, §2, §7). They are reproduced rather than smoothed
// over because the converge is written around each one:
//
//   - handle uniqueness is per PARENT RESOURCE: `claims:read` and
//     `reports:read` coexist; a second `read` under `claims` is refused;
//   - a resource-server identifier is unique across the directory;
//   - a role's permission set is REPLACED wholesale, and its assignments
//     SURVIVE the replacement — which is what lets converge be one write per
//     changed role with no re-assignment pass;
//   - granting a permission that no action derives is REFUSED, which is what
//     makes "actions before roles" a testable ordering and not a convention;
//   - deleting an action CASCADES out of every role that granted it, silently;
//   - a resource that still has actions cannot be deleted, so the tree is
//     walked leaf-first.
//
// DeleteResourceServer is the one place the fake is deliberately coarser than
// the wire: the port promises the ADAPTER walks the tree leaf-first, so here it
// is a single call that removes the subtree. The order itself is pinned where
// it is implemented, by thundersvc's httptest stub.

type fakeResourceServer struct {
	ID         DirectoryID
	Identifier string
	Name       string
}

type fakeResource struct {
	ID          DirectoryID
	RS          DirectoryID
	Handle      string
	Name        string
	Description string
}

type fakeAction struct {
	ID          DirectoryID
	Resource    DirectoryID
	Handle      string
	Name        string
	Description string
}

// fakeRole keeps Permissions and Assignments in separate fields for the same
// reason the directory does: one write replaces the first and cannot touch the
// second.
type fakeRole struct {
	ID          DirectoryID
	Name        string
	Description string
	RS          DirectoryID
	Permissions []string
	Assignments []Principal
}

// mint hands out an id in the directory's own opaque shape. Nothing may read
// structure out of it; the prefix is there so a failure message says what kind
// of object went missing.
func (d *fakeDirectory) mint(prefix string) DirectoryID {
	d.nextID++
	return DirectoryID(fmt.Sprintf("%s-%d", prefix, d.nextID))
}

func (d *fakeDirectory) rsIndex(id DirectoryID) int {
	for i := range d.resourceServers {
		if d.resourceServers[i].ID == id {
			return i
		}
	}
	return -1
}

func (d *fakeDirectory) rsIndexByIdentifier(identifier string) int {
	for i := range d.resourceServers {
		if d.resourceServers[i].Identifier == identifier {
			return i
		}
	}
	return -1
}

func (d *fakeDirectory) resourceIndex(id DirectoryID) int {
	for i := range d.resources {
		if d.resources[i].ID == id {
			return i
		}
	}
	return -1
}

func (d *fakeDirectory) roleIndex(id DirectoryID) int {
	for i := range d.roles {
		if d.roles[i].ID == id {
			return i
		}
	}
	return -1
}

// roleIndexByName matches case-insensitively, as the group lookup does. The
// platform derives both the document's role name and the directory's from one
// string, so the two can never differ by case in practice; matching loosely is
// the choice that cannot end with two roles for one declaration.
// ListRolePermissions reads back what a role grants, as the console does.
func (d *fakeDirectory) ListRolePermissions(_ context.Context, role DirectoryID) ([]string, error) {
	d.record(dirCall{Op: "ListRolePermissions", Target: d.roleLabel(role)})
	if err := d.fail("ListRolePermissions"); err != nil {
		return nil, err
	}
	i := d.roleIndex(role)
	if i < 0 {
		return nil, fmt.Errorf("fake directory: no role %q", role)
	}
	return append([]string(nil), d.roles[i].Permissions...), nil
}

func (d *fakeDirectory) roleIndexByName(name string) int {
	for i := range d.roles {
		if strings.EqualFold(d.roles[i].Name, name) {
			return i
		}
	}
	return -1
}

// rsLabel / resourceLabel / actionLabel / roleLabel render an id as the NAME the
// call named, so the Calls log reads as the design does rather than as a list of
// uuids.
func (d *fakeDirectory) rsLabel(id DirectoryID) string {
	if i := d.rsIndex(id); i >= 0 {
		return d.resourceServers[i].Identifier
	}
	return string(id)
}

func (d *fakeDirectory) resourceLabel(id DirectoryID) string {
	if i := d.resourceIndex(id); i >= 0 {
		return d.resources[i].Handle
	}
	return string(id)
}

func (d *fakeDirectory) actionLabel(a fakeAction) string {
	return d.resourceLabel(a.Resource) + ":" + a.Handle
}

func (d *fakeDirectory) roleLabel(id DirectoryID) string {
	if i := d.roleIndex(id); i >= 0 {
		return d.roles[i].Name
	}
	return string(id)
}

// derivedPermissions is every permission handle the resource server's catalog
// currently derives — `<resource handle>:<action handle>`, the delimiter being
// the ":" the resource server was created with. It is the set a role's grants
// are checked against.
func (d *fakeDirectory) derivedPermissions(rs DirectoryID) map[string]bool {
	out := map[string]bool{}
	for _, a := range d.actions {
		i := d.resourceIndex(a.Resource)
		if i < 0 || d.resources[i].RS != rs {
			continue
		}
		out[d.resources[i].Handle+":"+a.Handle] = true
	}
	return out
}

// resourceServer reads a resource server back by identifier, for assertions.
func (d *fakeDirectory) resourceServer(identifier string) (fakeResourceServer, bool) {
	if i := d.rsIndexByIdentifier(identifier); i >= 0 {
		return d.resourceServers[i], true
	}
	return fakeResourceServer{}, false
}

// roleByName reads a role back for assertions.
func (d *fakeDirectory) roleByName(name string) (fakeRole, bool) {
	if i := d.roleIndexByName(name); i >= 0 {
		return d.roles[i], true
	}
	return fakeRole{}, false
}

func (d *fakeDirectory) EnsureResourceServer(_ context.Context, identifier, name string) (DirectoryID, error) {
	d.record(dirCall{Op: "EnsureResourceServer", Target: identifier})
	if err := d.fail("EnsureResourceServer"); err != nil {
		return "", err
	}
	if i := d.rsIndexByIdentifier(identifier); i >= 0 {
		// Found: the name is NOT converged. The identifier is the identity.
		return d.resourceServers[i].ID, nil
	}
	return d.createResourceServer(identifier, name)
}

// createResourceServer is the write half of EnsureResourceServer, split out so
// a test can drive the create the find can never reach: a second resource
// server claiming one identifier, which the directory answers 409 RES-1013 and
// the port promises as ErrIdentifierConflict.
func (d *fakeDirectory) createResourceServer(identifier, name string) (DirectoryID, error) {
	d.record(dirCall{Op: "CreateResourceServer", Target: identifier})
	if err := d.fail("CreateResourceServer"); err != nil {
		return "", err
	}
	if d.rsIndexByIdentifier(identifier) >= 0 {
		return "", ErrIdentifierConflict
	}
	rs := fakeResourceServer{ID: d.mint("rsv"), Identifier: identifier, Name: name}
	d.resourceServers = append(d.resourceServers, rs)
	return rs.ID, nil
}

// FindResourceServer is a LOOKUP and is recorded as one: it never appears in
// writes(), which is what lets a teardown test assert that a project with
// nothing provisioned wrote nothing at all.
func (d *fakeDirectory) FindResourceServer(_ context.Context, identifier string) (DirectoryID, bool, error) {
	d.record(dirCall{Op: "FindResourceServer", Target: identifier})
	if err := d.fail("FindResourceServer"); err != nil {
		return "", false, err
	}
	if i := d.rsIndexByIdentifier(identifier); i >= 0 {
		return d.resourceServers[i].ID, true, nil
	}
	return "", false, nil
}

func (d *fakeDirectory) ListResources(_ context.Context, rs DirectoryID) ([]DirectoryResource, error) {
	d.record(dirCall{Op: "ListResources", Target: d.rsLabel(rs)})
	if err := d.fail("ListResources"); err != nil {
		return nil, err
	}
	if d.rsIndex(rs) < 0 {
		return nil, fmt.Errorf("fake directory: no resource server %q", rs)
	}
	var out []DirectoryResource
	for _, r := range d.resources {
		if r.RS != rs {
			continue
		}
		out = append(out, DirectoryResource{ID: r.ID, Handle: r.Handle, Name: r.Name, Description: r.Description})
	}
	return out, nil
}

func (d *fakeDirectory) CreateResource(_ context.Context, rs DirectoryID, handle, name, description string) (DirectoryResource, error) {
	d.record(dirCall{Op: "CreateResource", Target: handle})
	if err := d.fail("CreateResource"); err != nil {
		return DirectoryResource{}, err
	}
	if d.rsIndex(rs) < 0 {
		return DirectoryResource{}, fmt.Errorf("fake directory: no resource server %q", rs)
	}
	for _, r := range d.resources {
		if r.RS == rs && r.Handle == handle {
			return DirectoryResource{}, ErrHandleConflict
		}
	}
	res := fakeResource{ID: d.mint("res"), RS: rs, Handle: handle, Name: name, Description: description}
	d.resources = append(d.resources, res)
	return DirectoryResource{ID: res.ID, Handle: handle, Name: name, Description: description}, nil
}

// UpdateResource rewrites the mutable halves and leaves the handle — and the
// resource's actions — exactly where they are. That is the whole point of the
// verb: a description correction must not become a delete that cascades.
func (d *fakeDirectory) UpdateResource(_ context.Context, rs, resource DirectoryID, name, description string) error {
	d.record(dirCall{Op: "UpdateResource", Target: d.resourceLabel(resource)})
	if err := d.fail("UpdateResource"); err != nil {
		return err
	}
	i := d.resourceIndex(resource)
	if i < 0 || d.resources[i].RS != rs {
		return fmt.Errorf("fake directory: no resource %q on resource server %q", resource, rs)
	}
	d.resources[i].Name, d.resources[i].Description = name, description
	return nil
}

// DeleteResource refuses a resource that still has actions — the directory's
// RES-1006 "cannot delete … that has dependencies". It is why the ensure's
// delete pass walks the tree leaf-first.
func (d *fakeDirectory) DeleteResource(_ context.Context, rs, resource DirectoryID) error {
	d.record(dirCall{Op: "DeleteResource", Target: d.resourceLabel(resource)})
	if err := d.fail("DeleteResource"); err != nil {
		return err
	}
	i := d.resourceIndex(resource)
	if i < 0 || d.resources[i].RS != rs {
		return fmt.Errorf("fake directory: no resource %q on resource server %q", resource, rs)
	}
	for _, a := range d.actions {
		if a.Resource == resource {
			return fmt.Errorf("fake directory: resource %q has dependencies", d.resources[i].Handle)
		}
	}
	d.resources = append(d.resources[:i], d.resources[i+1:]...)
	return nil
}

func (d *fakeDirectory) ListActions(_ context.Context, rs, resource DirectoryID) ([]DirectoryAction, error) {
	d.record(dirCall{Op: "ListActions", Target: d.resourceLabel(resource)})
	if err := d.fail("ListActions"); err != nil {
		return nil, err
	}
	i := d.resourceIndex(resource)
	if i < 0 || d.resources[i].RS != rs {
		return nil, fmt.Errorf("fake directory: no resource %q on resource server %q", resource, rs)
	}
	var out []DirectoryAction
	for _, a := range d.actions {
		if a.Resource != resource {
			continue
		}
		out = append(out, DirectoryAction{ID: a.ID, Handle: a.Handle, Name: a.Name, Description: a.Description})
	}
	return out, nil
}

// CreateAction enforces the measured uniqueness rule: the handle must be free
// under THIS resource, and a handle in use under a sibling resource is not a
// conflict.
func (d *fakeDirectory) CreateAction(_ context.Context, rs, resource DirectoryID, handle, name, description string) (DirectoryAction, error) {
	d.record(dirCall{Op: "CreateAction", Target: d.resourceLabel(resource) + ":" + handle})
	if err := d.fail("CreateAction"); err != nil {
		return DirectoryAction{}, err
	}
	i := d.resourceIndex(resource)
	if i < 0 || d.resources[i].RS != rs {
		return DirectoryAction{}, fmt.Errorf("fake directory: no resource %q on resource server %q", resource, rs)
	}
	for _, a := range d.actions {
		if a.Resource == resource && a.Handle == handle {
			return DirectoryAction{}, ErrHandleConflict
		}
	}
	act := fakeAction{ID: d.mint("act"), Resource: resource, Handle: handle, Name: name, Description: description}
	d.actions = append(d.actions, act)
	return DirectoryAction{ID: act.ID, Handle: handle, Name: name, Description: description}, nil
}

// UpdateAction is UpdateResource one level down: name and description only, and
// no cascade of any kind.
func (d *fakeDirectory) UpdateAction(_ context.Context, rs, resource, action DirectoryID, name, description string) error {
	i := d.actionIndex(action)
	label := string(action)
	if i >= 0 {
		label = d.actionLabel(d.actions[i])
	}
	d.record(dirCall{Op: "UpdateAction", Target: label})
	if err := d.fail("UpdateAction"); err != nil {
		return err
	}
	ri := d.resourceIndex(resource)
	if i < 0 || ri < 0 || d.actions[i].Resource != resource || d.resources[ri].RS != rs {
		return fmt.Errorf("fake directory: no action %q on resource %q", action, resource)
	}
	d.actions[i].Name, d.actions[i].Description = name, description
	return nil
}

// DeleteAction cascades: the permission it derived disappears from every role
// that granted it, with no error and no other signal. A converge that read a
// role's grants before the catalog edit is holding stale data afterwards.
func (d *fakeDirectory) DeleteAction(_ context.Context, rs, resource, action DirectoryID) error {
	i := d.actionIndex(action)
	label := string(action)
	if i >= 0 {
		label = d.actionLabel(d.actions[i])
	}
	d.record(dirCall{Op: "DeleteAction", Target: label})
	if err := d.fail("DeleteAction"); err != nil {
		return err
	}
	ri := d.resourceIndex(resource)
	if i < 0 || ri < 0 || d.actions[i].Resource != resource || d.resources[ri].RS != rs {
		return fmt.Errorf("fake directory: no action %q on resource %q", action, resource)
	}
	permission := d.actionLabel(d.actions[i])
	d.actions = append(d.actions[:i], d.actions[i+1:]...)
	d.revokeEverywhere(permission)
	return nil
}

func (d *fakeDirectory) actionIndex(id DirectoryID) int {
	for i := range d.actions {
		if d.actions[i].ID == id {
			return i
		}
	}
	return -1
}

// revokeEverywhere is the directory's cascade: a permission that no longer
// exists is dropped from every role's grant list.
func (d *fakeDirectory) revokeEverywhere(permission string) {
	for i := range d.roles {
		kept := make([]string, 0, len(d.roles[i].Permissions))
		for _, p := range d.roles[i].Permissions {
			if p != permission {
				kept = append(kept, p)
			}
		}
		d.roles[i].Permissions = kept
	}
}

// DeleteResourceServer removes the subtree. The port's contract is that the
// ADAPTER owns the leaf-first order the directory demands, so this is one call
// here; deleting the actions still cascades their permissions out of every role.
func (d *fakeDirectory) DeleteResourceServer(_ context.Context, rs DirectoryID) error {
	d.record(dirCall{Op: "DeleteResourceServer", Target: d.rsLabel(rs)})
	if err := d.fail("DeleteResourceServer"); err != nil {
		return err
	}
	i := d.rsIndex(rs)
	if i < 0 {
		return fmt.Errorf("fake directory: no resource server %q", rs)
	}
	var keptResources []fakeResource
	for _, r := range d.resources {
		if r.RS != rs {
			keptResources = append(keptResources, r)
			continue
		}
		var keptActions []fakeAction
		for _, a := range d.actions {
			if a.Resource != r.ID {
				keptActions = append(keptActions, a)
				continue
			}
			d.revokeEverywhere(r.Handle + ":" + a.Handle)
		}
		d.actions = keptActions
	}
	d.resources = keptResources
	d.resourceServers = append(d.resourceServers[:i], d.resourceServers[i+1:]...)
	return nil
}

// EnsureRole is find-by-name-then-converge. Three outcomes, and which one
// happened is visible in the call log:
//
//	absent          → CreateRole
//	present, differs → UpdateRole, permissions replaced wholesale, ASSIGNMENTS KEPT
//	present, equal   → no write at all
//
// Permission order is not part of the comparison: the directory stores a set,
// and a converge that rewrote a role because the document reordered its grants
// would churn for nothing.
func (d *fakeDirectory) EnsureRole(_ context.Context, id DirectoryID, name, description string, rs DirectoryID, permissions []string) (DirectoryID, error) {
	d.record(dirCall{Op: "EnsureRole", Target: name})
	if err := d.fail("EnsureRole"); err != nil {
		return "", err
	}
	if d.rsIndex(rs) < 0 {
		return "", fmt.Errorf("fake directory: no resource server %q", rs)
	}
	// The caller says which role this is, and the empty id means "create". The
	// fake CHECKS the caller rather than trusting it: an id that names no role,
	// or one whose name disagrees with `name`, is a defect in the converge that
	// would otherwise surface as a silent create of a duplicate role.
	if id != "" {
		if i := d.roleIndex(id); i < 0 {
			return "", fmt.Errorf("fake directory: EnsureRole was given the unknown role id %q", id)
		} else if !strings.EqualFold(d.roles[i].Name, name) {
			return "", fmt.Errorf("fake directory: role id %q is %q, not %q", id, d.roles[i].Name, name)
		}
	} else if i := d.roleIndexByName(name); i >= 0 {
		return "", fmt.Errorf("fake directory: EnsureRole was given no id for %q, which already exists as %q", name, d.roles[i].ID)
	}
	// ROL-1012: a grant the catalog does not derive is refused outright, which
	// is what forces the resources-and-actions pass to run first.
	catalog := d.derivedPermissions(rs)
	for _, p := range permissions {
		if !catalog[p] {
			return "", fmt.Errorf("fake directory: role %q: permission %q does not exist", name, p)
		}
	}
	granted := append([]string(nil), permissions...)
	i := d.roleIndexByName(name)
	if i < 0 {
		d.record(dirCall{Op: "CreateRole", Target: name, Members: granted})
		if err := d.fail("CreateRole"); err != nil {
			return "", err
		}
		role := fakeRole{ID: d.mint("rol"), Name: name, Description: description, RS: rs, Permissions: granted}
		d.roles = append(d.roles, role)
		return role.ID, nil
	}
	if sameSet(d.roles[i].Permissions, granted) && d.roles[i].Description == description {
		return d.roles[i].ID, nil
	}
	d.record(dirCall{Op: "UpdateRole", Target: name, Members: granted})
	if err := d.fail("UpdateRole"); err != nil {
		return "", err
	}
	// The replacement touches the permission set and the description only: the
	// role keeps its id and, crucially, its assignments.
	d.roles[i].Permissions = granted
	d.roles[i].Description = description
	d.roles[i].RS = rs
	return d.roles[i].ID, nil
}

// mappablePrincipal mirrors the adapter's `toAssignment`: the three kinds this
// platform can put on the wire. Anything else — a principal somebody assigned
// on the identity provider's own console — can be READ back but not written.
func mappablePrincipal(kind PrincipalKind) bool {
	switch kind {
	case PrincipalGroup, PrincipalUser, PrincipalApp:
		return true
	}
	return false
}

// seedAssignment puts a principal on a role behind the port, for the states only
// somebody else's write can produce.
func (d *fakeDirectory) seedAssignment(t *testing.T, roleName string, p Principal) {
	t.Helper()
	i := d.roleIndexByName(roleName)
	if i < 0 {
		t.Fatalf("fake directory: no role %q to seed an assignment on", roleName)
	}
	d.roles[i].Assignments = append(d.roles[i].Assignments, p)
}

// sameSet compares two grant lists as sets.
func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	left, right := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(left)
	sort.Strings(right)
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func (d *fakeDirectory) ListRoles(_ context.Context) ([]RoleRef, error) {
	d.record(dirCall{Op: "ListRoles"})
	if err := d.fail("ListRoles"); err != nil {
		return nil, err
	}
	out := make([]RoleRef, 0, len(d.roles))
	for _, r := range d.roles {
		out = append(out, RoleRef{ID: r.ID, Name: r.Name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (d *fakeDirectory) DeleteRole(_ context.Context, role DirectoryID) error {
	d.record(dirCall{Op: "DeleteRole", Target: d.roleLabel(role)})
	if err := d.fail("DeleteRole"); err != nil {
		return err
	}
	i := d.roleIndex(role)
	if i < 0 {
		return fmt.Errorf("fake directory: no role %q", role)
	}
	// Assignments go with the role; the principals themselves are untouched.
	d.roles = append(d.roles[:i], d.roles[i+1:]...)
	return nil
}

// AssignRole is additive and idempotent: binding a principal that already holds
// the role reaches the directory and changes nothing, which is a no-op call and
// not a write.
func (d *fakeDirectory) AssignRole(_ context.Context, role DirectoryID, principal Principal) error {
	if !mappablePrincipal(principal.Kind) {
		return fmt.Errorf("fake directory: unknown principal kind %q", principal.Kind)
	}
	i := d.roleIndex(role)
	held := i >= 0 && holdsRole(d.roles[i].Assignments, principal)
	d.record(dirCall{
		Op: "AssignRole", Target: d.roleLabel(role),
		Members: []string{principalLabel(principal)}, NoOp: held,
	})
	if err := d.fail("AssignRole"); err != nil {
		return err
	}
	if i < 0 {
		return fmt.Errorf("fake directory: no role %q", role)
	}
	if held {
		return nil
	}
	d.roles[i].Assignments = append(d.roles[i].Assignments, Principal{Kind: principal.Kind, ID: principal.ID})
	return nil
}

func (d *fakeDirectory) UnassignRole(_ context.Context, role DirectoryID, principal Principal) error {
	// The adapter refuses a kind it cannot map onto the wire BEFORE the call
	// leaves the process (toAssignment), so the fake refuses it here and records
	// nothing — a call that never reached a directory must not read as one that
	// did. Thunder also assigns to `agent`, which is how such a principal comes
	// to be on a role the platform owns.
	if !mappablePrincipal(principal.Kind) {
		return fmt.Errorf("fake directory: unknown principal kind %q", principal.Kind)
	}
	i := d.roleIndex(role)
	held := i >= 0 && holdsRole(d.roles[i].Assignments, principal)
	d.record(dirCall{
		Op: "UnassignRole", Target: d.roleLabel(role),
		Members: []string{principalLabel(principal)}, NoOp: !held,
	})
	if err := d.fail("UnassignRole"); err != nil {
		return err
	}
	if i < 0 {
		return fmt.Errorf("fake directory: no role %q", role)
	}
	if !held {
		return nil
	}
	var kept []Principal
	for _, p := range d.roles[i].Assignments {
		if p.Kind == principal.Kind && p.ID == principal.ID {
			continue
		}
		kept = append(kept, p)
	}
	d.roles[i].Assignments = kept
	return nil
}

// ListRoleAssignments fills Display the way the directory's `include=display`
// does, so a caller can name a principal without a second lookup — and can see
// that an assignment outlived the group id it names, which happens on every
// membership edit.
func (d *fakeDirectory) ListRoleAssignments(_ context.Context, role DirectoryID) ([]Principal, error) {
	d.record(dirCall{Op: "ListRoleAssignments", Target: d.roleLabel(role)})
	if err := d.fail("ListRoleAssignments"); err != nil {
		return nil, err
	}
	i := d.roleIndex(role)
	if i < 0 {
		return nil, fmt.Errorf("fake directory: no role %q", role)
	}
	out := make([]Principal, 0, len(d.roles[i].Assignments))
	for _, p := range d.roles[i].Assignments {
		p.Display = d.principalDisplay(p)
		out = append(out, p)
	}
	return out, nil
}

// principalDisplay resolves a principal's human name, and answers "" for one
// whose object the directory no longer has.
func (d *fakeDirectory) principalDisplay(p Principal) string {
	switch p.Kind {
	case PrincipalGroup:
		for _, g := range d.groups {
			if DirectoryID(g.ID) == p.ID {
				return g.Name
			}
		}
	case PrincipalUser:
		for _, a := range d.users {
			if DirectoryID(a.ID) == p.ID {
				return a.Username
			}
		}
	}
	return ""
}

func holdsRole(assignments []Principal, p Principal) bool {
	for _, a := range assignments {
		if a.Kind == p.Kind && a.ID == p.ID {
			return true
		}
	}
	return false
}

// principalLabel is how a principal reads in the call log: `group:grp-3`.
func principalLabel(p Principal) string { return string(p.Kind) + ":" + string(p.ID) }

// ---- targets --------------------------------------------------------------

// testEnvironment is the environment every fake resolver answers with. It is a
// second, DIFFERENT environment from any org handle on purpose: a test that
// passes with the two confused would prove nothing about the (org, environment)
// key.
const testEnvironment = "default"

// testIssuer is the environment tier's public issuer — the thing a published
// login is only valid at.
const testIssuer = "http://default-idp.amp.localhost:8080"

// fakeTargets is the TargetResolver: it hands every org the same directory,
// under the (org, testEnvironment) scope, and records what it was asked for.
type fakeTargets struct {
	dir Directory
	// err, when set, is what Resolve answers — the "this environment has no
	// identity provider" case. Scope keeps working, which is what lets the panel
	// degrade instead of failing.
	err error
	// resolved counts Resolve calls, so a test can see the directory being
	// looked up once per operation rather than per role.
	resolved int
	// orgs records every org Resolve was asked for, in order.
	orgs []string
}

func newFakeTargets(dir Directory) *fakeTargets { return &fakeTargets{dir: dir} }

func (f *fakeTargets) Scope(orgID string) Scope {
	return Scope{OrgID: orgID, Environment: testEnvironment}
}

func (f *fakeTargets) Resolve(_ context.Context, orgID string) (Target, error) {
	f.resolved++
	f.orgs = append(f.orgs, orgID)
	if f.err != nil {
		return Target{}, f.err
	}
	return Target{
		OrgID: orgID, Environment: testEnvironment,
		Issuer: testIssuer, Directory: f.dir,
	}, nil
}

// ---- store ----------------------------------------------------------------

// fakeStore is an in-memory Store. It seals nothing — the password map IS the
// sealed column — because the seal is the repository's job and is pinned by the
// DB tier; here the point is only WHICH password reached the store, and when.
type fakeStore struct {
	// roles is keyed by scope + lowercased name, matching the real store's
	// composite key and its case-insensitive GetRole. Every map here carries the
	// scope in its key for the same reason the table does: two environments'
	// rows must not be able to answer each other's reads.
	roles map[string]IdPRole
	users map[string]TestUser
	// passwords is the sealed column, keyed by scope + username.
	passwords map[string]string
	// refs is keyed by scope + project.
	refs map[string][]TestUserRef
	// resourceServers and roleBindings are the project-owned half of the record,
	// both keyed by scope + project, both CONVERGE stores: an upsert replaces
	// the project's one resource-server row and a replace makes its payload the
	// project's complete set of bindings.
	resourceServers map[string]IdPResourceServer
	roleBindings    map[string][]IdPRoleBinding

	// replaceCalls records each ReplaceProjectRefs payload, so a test can assert
	// the ensure rewrote the whole set exactly once.
	replaceCalls [][]TestUserRef
	// replaceBindingCalls does the same for ReplaceRoleBindings.
	replaceBindingCalls [][]IdPRoleBinding
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
		resourceServers: map[string]IdPResourceServer{},
		roleBindings:    map[string][]IdPRoleBinding{},
		failOn:          map[string]error{},
	}
}

func refKey(scope Scope, projectID string) string { return scope.String() + "/" + projectID }

// scopedKey is how every map here is keyed: the (org, environment) pair, then
// the name. Lowercasing is the caller's business — role names are matched
// case-insensitively, usernames exactly — so this only joins.
func scopedKey(scope Scope, name string) string { return scope.String() + "|" + name }

// role / user / password are the read helpers the tests assert through, so a
// test never has to spell the composite key.
func (s *fakeStore) role(scope Scope, name string) (IdPRole, bool) {
	row, ok := s.roles[scopedKey(scope, strings.ToLower(name))]
	return row, ok
}

func (s *fakeStore) putRole(scope Scope, role IdPRole) {
	role.OrgID, role.Environment = scope.OrgID, scope.Environment
	s.roles[scopedKey(scope, strings.ToLower(role.Name))] = role
}

// putBinding records that a project assigned one of its roles to a group,
// without going through ReplaceRoleBindings — the seeding a test does when the
// interesting state is what an EARLIER project's build already left behind.
func (s *fakeStore) putBinding(scope Scope, projectID, role, group string) {
	k := refKey(scope, projectID)
	s.roleBindings[k] = append(s.roleBindings[k], IdPRoleBinding{
		OrgID: scope.OrgID, Environment: scope.Environment, ProjectID: projectID,
		Role: role, GroupName: group, DirectoryRoleID: "rol-" + projectID + "-" + role,
	})
}

func (s *fakeStore) user(scope Scope, username string) (TestUser, bool) {
	row, ok := s.users[scopedKey(scope, username)]
	return row, ok
}

func (s *fakeStore) password(scope Scope, username string) (string, bool) {
	pw, ok := s.passwords[scopedKey(scope, username)]
	return pw, ok
}

func (s *fakeStore) setPassword(scope Scope, username, password string) {
	s.passwords[scopedKey(scope, username)] = password
}

func (s *fakeStore) GetRole(_ context.Context, scope Scope, name string) (*IdPRole, error) {
	if err := s.failOn["GetRole"]; err != nil {
		return nil, err
	}
	row, ok := s.role(scope, name)
	if !ok {
		return nil, nil
	}
	return &row, nil
}

func (s *fakeStore) ListRoles(_ context.Context, scope Scope) ([]IdPRole, error) {
	out := make([]IdPRole, 0, len(s.roles))
	for _, r := range s.roles {
		if r.scope() != scope {
			continue
		}
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
	s.roles[scopedKey(role.scope(), strings.ToLower(role.Name))] = role
	return nil
}

func (s *fakeStore) GetTestUser(_ context.Context, scope Scope, username string) (*TestUser, error) {
	if err := s.failOn["GetTestUser"]; err != nil {
		return nil, err
	}
	row, ok := s.user(scope, username)
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
	s.users[scopedKey(user.scope(), user.Username)] = user
	s.passwords[scopedKey(user.scope(), user.Username)] = password
	return nil
}

// UpdateTestUserFacts touches the two metadata columns and nothing else — in
// particular it does not go near the password map, which is what makes the
// "reuse never reads the credential" assertion meaningful.
func (s *fakeStore) UpdateTestUserFacts(_ context.Context, scope Scope, username, thunderUserID, roleName string) error {
	if err := s.failOn["UpdateTestUserFacts"]; err != nil {
		return err
	}
	row, ok := s.user(scope, username)
	if !ok {
		return fmt.Errorf("fake store: no account %q on %s", username, scope)
	}
	row.ThunderUserID, row.RoleName = thunderUserID, roleName
	s.users[scopedKey(scope, username)] = row
	return nil
}

func (s *fakeStore) SetTestUserPassword(_ context.Context, scope Scope, username, password string) error {
	if _, ok := s.user(scope, username); !ok {
		return fmt.Errorf("fake store: no account %q on %s", username, scope)
	}
	s.setPassword(scope, username, password)
	return nil
}

func (s *fakeStore) RevealTestUserPassword(_ context.Context, scope Scope, username string) (string, error) {
	s.revealCalls++
	if err := s.failOn["RevealTestUserPassword"]; err != nil {
		return "", err
	}
	pw, ok := s.password(scope, username)
	if !ok || pw == "" {
		return "", ErrNoPassword
	}
	return pw, nil
}

func (s *fakeStore) DeleteTestUser(_ context.Context, scope Scope, username string) error {
	delete(s.users, scopedKey(scope, username))
	delete(s.passwords, scopedKey(scope, username))
	for key, rows := range s.refs {
		var kept []TestUserRef
		for _, r := range rows {
			if r.Username != username || r.OrgID != scope.OrgID || r.Environment != scope.Environment {
				kept = append(kept, r)
			}
		}
		s.refs[key] = kept
	}
	return nil
}

func (s *fakeStore) ReplaceProjectRefs(_ context.Context, scope Scope, projectID string, refs []TestUserRef) error {
	if err := s.failOn["ReplaceProjectRefs"]; err != nil {
		return err
	}
	stamped := make([]TestUserRef, 0, len(refs))
	for _, r := range refs {
		r.OrgID, r.Environment, r.ProjectID = scope.OrgID, scope.Environment, projectID
		stamped = append(stamped, r)
	}
	s.refs[refKey(scope, projectID)] = stamped
	s.replaceCalls = append(s.replaceCalls, stamped)
	return nil
}

func (s *fakeStore) ListProjectRefs(_ context.Context, scope Scope, projectID string) ([]TestUserRef, error) {
	return s.refs[refKey(scope, projectID)], nil
}

func (s *fakeStore) ProjectsReferencing(_ context.Context, scope Scope, username string) ([]TestUserRef, error) {
	var out []TestUserRef
	for _, rows := range s.refs {
		for _, r := range rows {
			if r.OrgID == scope.OrgID && r.Environment == scope.Environment && r.Username == username {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

// ---- store: the project-owned directory objects ----------------------------
//
// Converge semantics, mirroring the real store: a resource-server upsert
// replaces the project's one row, and a bindings replace makes the payload the
// complete set for that project — so a role the design dropped disappears by
// being absent from the next payload, not by a delete the caller has to
// remember.

func (s *fakeStore) UpsertResourceServer(_ context.Context, rs IdPResourceServer) error {
	if err := s.failOn["UpsertResourceServer"]; err != nil {
		return err
	}
	s.resourceServers[refKey(rs.scope(), rs.ProjectID)] = rs
	return nil
}

func (s *fakeStore) GetResourceServer(_ context.Context, scope Scope, projectID string) (*IdPResourceServer, error) {
	if err := s.failOn["GetResourceServer"]; err != nil {
		return nil, err
	}
	row, ok := s.resourceServers[refKey(scope, projectID)]
	if !ok {
		return nil, nil
	}
	return &row, nil
}

// DeleteResourceServer is idempotent, like the real one: deleting nothing is
// success, so a retried project delete does not fail on its second attempt.
func (s *fakeStore) DeleteResourceServer(_ context.Context, scope Scope, projectID string) error {
	delete(s.resourceServers, refKey(scope, projectID))
	return nil
}

func (s *fakeStore) ReplaceRoleBindings(_ context.Context, scope Scope, projectID string, bindings []IdPRoleBinding) error {
	if err := s.failOn["ReplaceRoleBindings"]; err != nil {
		return err
	}
	stamped := make([]IdPRoleBinding, 0, len(bindings))
	for _, b := range bindings {
		b.OrgID, b.Environment, b.ProjectID = scope.OrgID, scope.Environment, projectID
		stamped = append(stamped, b)
	}
	s.roleBindings[refKey(scope, projectID)] = stamped
	s.replaceBindingCalls = append(s.replaceBindingCalls, stamped)
	return nil
}

func (s *fakeStore) ListRoleBindings(_ context.Context, scope Scope, projectID string) ([]IdPRoleBinding, error) {
	rows := append([]IdPRoleBinding(nil), s.roleBindings[refKey(scope, projectID)]...)
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
// a group, so it counts nothing, and the name is compared WITHOUT CASE because
// that is what the store does.
func (s *fakeStore) CountProjectsBindingGroup(_ context.Context, scope Scope, groupName string) (int, error) {
	if err := s.failOn["CountProjectsBindingGroup"]; err != nil {
		return 0, err
	}
	if groupName == "" {
		return 0, nil
	}
	projects := map[string]struct{}{}
	for _, rows := range s.roleBindings {
		for _, b := range rows {
			if b.scope() == scope && strings.EqualFold(b.GroupName, groupName) {
				projects[b.ProjectID] = struct{}{}
			}
		}
	}
	return len(projects), nil
}

// CountProjectsBindingGroups is the same count for every group at once, keyed by
// the LOWERCASED name — the key the store's GROUP BY produces.
func (s *fakeStore) CountProjectsBindingGroups(_ context.Context, scope Scope) (map[string]int, error) {
	if err := s.failOn["CountProjectsBindingGroups"]; err != nil {
		return nil, err
	}
	projects := map[string]map[string]struct{}{}
	for _, rows := range s.roleBindings {
		for _, b := range rows {
			if b.scope() != scope || b.GroupName == "" {
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

func (s *fakeStore) DeleteRoleBindings(_ context.Context, scope Scope, projectID string) error {
	delete(s.roleBindings, refKey(scope, projectID))
	return nil
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

// ---- what the directory fake promises --------------------------------------
//
// The fake IS the contract the converge is written against, so the directory
// facts it reproduces are pinned here rather than left to be discovered by a
// failing ensure test. Every assertion below corresponds to a behaviour
// measured on Thunder 1.0.0; if one of them ever has to change, the ensure that
// leans on it has to be re-read, not just re-run.

// seedCatalog puts the spike's own catalog on the fake — two resources, five
// actions — and hands back the ids by handle.
func seedCatalog(t *testing.T, d *fakeDirectory) (DirectoryID, map[string]DirectoryID, map[string]DirectoryID) {
	t.Helper()
	ctx := context.Background()
	rs, err := d.EnsureResourceServer(ctx, ResourceServerIdentifier("acme", "p1"), "p1")
	if err != nil {
		t.Fatalf("EnsureResourceServer: %v", err)
	}
	resources := map[string]DirectoryID{}
	actions := map[string]DirectoryID{}
	for _, seed := range []struct {
		resource string
		actions  []string
	}{
		{"claims", []string{"read", "read-all", "submit"}},
		{"reports", []string{"read", "export"}},
	} {
		res, err := d.CreateResource(ctx, rs, seed.resource, seed.resource, "")
		if err != nil {
			t.Fatalf("CreateResource %q: %v", seed.resource, err)
		}
		resources[seed.resource] = res.ID
		for _, handle := range seed.actions {
			act, err := d.CreateAction(ctx, rs, res.ID, handle, handle, "")
			if err != nil {
				t.Fatalf("CreateAction %s:%s: %v", seed.resource, handle, err)
			}
			actions[seed.resource+":"+handle] = act.ID
		}
	}
	return rs, resources, actions
}

// grants reads a role's permission set back, sorted, for comparison.
func (d *fakeDirectory) grants(t *testing.T, name string) []string {
	t.Helper()
	role, ok := d.roleByName(name)
	if !ok {
		t.Fatalf("no role %q on the fake directory", name)
	}
	out := append([]string(nil), role.Permissions...)
	sort.Strings(out)
	return out
}

// writeLines renders the writes for a failure message.
func (d *fakeDirectory) writeLines() []string {
	var out []string
	for _, c := range d.writes() {
		out = append(out, c.line())
	}
	return out
}

func TestFakeDirectoryFindsAResourceServerByIdentifierAndCreatesItOnlyOnce(t *testing.T) {
	d := newFakeDirectory()
	ctx := context.Background()
	identifier := ResourceServerIdentifier("acme", "p1")

	first, err := d.EnsureResourceServer(ctx, identifier, "p1")
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	second, err := d.EnsureResourceServer(ctx, identifier, "a different label entirely")
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if first != second {
		t.Fatalf("the identifier did not identify the resource server: %q then %q", first, second)
	}
	if got := len(d.writes()); got != 1 {
		t.Fatalf("want exactly one write (the create), got %d: %v", got, d.writeLines())
	}
	// The label is seeded at create and never converged: nothing a token sees
	// depends on it.
	rs, ok := d.resourceServer(identifier)
	if !ok || rs.Name != "p1" {
		t.Fatalf("the second ensure rewrote the name: %+v", rs)
	}
}

// The find is what keeps the ensure off this path; the code is here because a
// concurrent ensure of the same project can still race into it.
func TestFakeDirectoryRefusesASecondResourceServerOnOneIdentifier(t *testing.T) {
	d := newFakeDirectory()
	identifier := ResourceServerIdentifier("acme", "p1")
	if _, err := d.createResourceServer(identifier, "p1"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := d.createResourceServer(identifier, "p1 again"); !errors.Is(err, ErrIdentifierConflict) {
		t.Fatalf("want ErrIdentifierConflict, got %v", err)
	}
}

func TestFakeDirectoryScopesActionHandleUniquenessToTheParentResource(t *testing.T) {
	d := newFakeDirectory()
	ctx := context.Background()
	rs, resources, _ := seedCatalog(t, d)

	// `read` already exists under BOTH claims and reports — the seed created it
	// twice — so the sibling case is proven by the seed itself. A second `read`
	// under one resource is the conflict.
	if _, err := d.CreateAction(ctx, rs, resources["claims"], "read", "read", ""); !errors.Is(err, ErrHandleConflict) {
		t.Fatalf("want ErrHandleConflict for a duplicate under one resource, got %v", err)
	}
	if _, err := d.CreateResource(ctx, rs, "claims", "claims", ""); !errors.Is(err, ErrHandleConflict) {
		t.Fatalf("want ErrHandleConflict for a duplicate resource handle, got %v", err)
	}
}

// A grant no action derives is refused outright, which is what makes
// "resources and actions before roles" an ordering the ensure cannot get wrong
// silently.
func TestFakeDirectoryRefusesAGrantTheCatalogDoesNotDerive(t *testing.T) {
	d := newFakeDirectory()
	ctx := context.Background()
	rs, _, _ := seedCatalog(t, d)

	_, err := d.EnsureRole(ctx, "", RoleName("p1", "Approver"), "", rs, []string{"claims:read", "claims:approve"})
	if err == nil {
		t.Fatal("want a refusal for a permission no action derives, got none")
	}
	if !strings.Contains(err.Error(), "claims:approve") {
		t.Fatalf("the refusal does not name the permission: %v", err)
	}
	if _, ok := d.roleByName(RoleName("p1", "Approver")); ok {
		t.Fatal("the role was created despite the refused grant")
	}
}

func TestFakeDirectoryRoleConvergeReplacesPermissionsAndKeepsAssignments(t *testing.T) {
	d := newFakeDirectory()
	ctx := context.Background()
	rs, _, _ := seedCatalog(t, d)
	name := RoleName("p1", "Employee")

	role, err := d.EnsureRole(ctx, "", name, "Files claims", rs, []string{"claims:read", "claims:submit"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	group := d.seedGroup("Employees")
	if err := d.AssignRole(ctx, role, Principal{Kind: PrincipalGroup, ID: DirectoryID(group.ID)}); err != nil {
		t.Fatalf("assign: %v", err)
	}

	// The tag drops one grant and adds another.
	same, err := d.EnsureRole(ctx, role, name, "Files claims", rs, []string{"claims:read", "reports:read"})
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if same != role {
		t.Fatalf("the converge minted a new role id: %q then %q", role, same)
	}
	if got, want := d.grants(t, name), []string{"claims:read", "reports:read"}; !sameSet(got, want) {
		t.Fatalf("permissions were not replaced wholesale: got %v, want %v", got, want)
	}
	// The load-bearing half: a permission write is not an assignment write.
	held, err := d.ListRoleAssignments(ctx, role)
	if err != nil {
		t.Fatalf("assignments: %v", err)
	}
	if len(held) != 1 || held[0].ID != DirectoryID(group.ID) || held[0].Display != "Employees" {
		t.Fatalf("the converge lost or mangled the assignment: %+v", held)
	}
}

// The property task 2.5 asserts against a whole build, pinned here against the
// directory alone: everything already matching the tag means nothing is
// written, so a rebuild cannot churn ids or mint credentials.
func TestFakeDirectoryConvergingAnUnchangedCatalogWritesNothing(t *testing.T) {
	d := newFakeDirectory()
	ctx := context.Background()
	rs, _, _ := seedCatalog(t, d)
	name := RoleName("p1", "Employee")
	role, err := d.EnsureRole(ctx, "", name, "Files claims", rs, []string{"claims:read", "claims:submit"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	group := d.seedGroup("Employees")
	if err := d.AssignRole(ctx, role, Principal{Kind: PrincipalGroup, ID: DirectoryID(group.ID)}); err != nil {
		t.Fatalf("assign: %v", err)
	}

	// Everything the first pass did, asked for a second time — the shape a
	// rebuild of the same tag has.
	before := len(d.writes())
	if _, err := d.EnsureResourceServer(ctx, ResourceServerIdentifier("acme", "p1"), "p1"); err != nil {
		t.Fatalf("re-ensure resource server: %v", err)
	}
	// Grant order is the document's, and reordering it is not a change.
	if _, err := d.EnsureRole(ctx, role, name, "Files claims", rs, []string{"claims:submit", "claims:read"}); err != nil {
		t.Fatalf("re-ensure role: %v", err)
	}
	if err := d.AssignRole(ctx, role, Principal{Kind: PrincipalGroup, ID: DirectoryID(group.ID)}); err != nil {
		t.Fatalf("re-assign: %v", err)
	}
	if got := len(d.writes()); got != before {
		t.Fatalf("the second pass wrote %d time(s): %v", got-before, d.writeLines()[before:])
	}
}

// Thunder answers 204 and silently drops the permission from every role, so
// there is no strip-first pass — and a role's grants read before a catalog edit
// are stale afterwards.
func TestFakeDirectoryDeletingAnActionCascadesOutOfEveryRole(t *testing.T) {
	d := newFakeDirectory()
	ctx := context.Background()
	rs, resources, actions := seedCatalog(t, d)
	employee, err := d.EnsureRole(ctx, "", RoleName("p1", "Employee"), "", rs, []string{"claims:read", "claims:submit"})
	if err != nil {
		t.Fatalf("employee: %v", err)
	}
	if _, err := d.EnsureRole(ctx, "", RoleName("p1", "Approver"), "", rs, []string{"claims:read", "claims:read-all"}); err != nil {
		t.Fatalf("approver: %v", err)
	}

	if err := d.DeleteAction(ctx, rs, resources["claims"], actions["claims:read"]); err != nil {
		t.Fatalf("delete action: %v", err)
	}
	if got, want := d.grants(t, RoleName("p1", "Employee")), []string{"claims:submit"}; !sameSet(got, want) {
		t.Fatalf("employee grants: got %v, want %v", got, want)
	}
	if got, want := d.grants(t, RoleName("p1", "Approver")), []string{"claims:read-all"}; !sameSet(got, want) {
		t.Fatalf("approver grants: got %v, want %v", got, want)
	}
	// And the role survives the cascade — only the grant went.
	if _, err := d.ListRoleAssignments(ctx, employee); err != nil {
		t.Fatalf("the role did not survive its permission being deleted: %v", err)
	}
}

func TestFakeDirectoryRefusesToDeleteAResourceThatStillHasActions(t *testing.T) {
	d := newFakeDirectory()
	ctx := context.Background()
	rs, resources, actions := seedCatalog(t, d)

	if err := d.DeleteResource(ctx, rs, resources["reports"]); err == nil {
		t.Fatal("want a refusal for a resource that still has actions, got none")
	}
	for _, handle := range []string{"reports:read", "reports:export"} {
		if err := d.DeleteAction(ctx, rs, resources["reports"], actions[handle]); err != nil {
			t.Fatalf("delete %s: %v", handle, err)
		}
	}
	if err := d.DeleteResource(ctx, rs, resources["reports"]); err != nil {
		t.Fatalf("leaf-first delete: %v", err)
	}
}

// The port promises the ADAPTER owns the leaf-first walk, so one call removes
// the subtree — and the actions it removes still cascade out of the roles.
func TestFakeDirectoryDeleteResourceServerRemovesTheSubtreeAndItsGrants(t *testing.T) {
	d := newFakeDirectory()
	ctx := context.Background()
	rs, _, _ := seedCatalog(t, d)
	name := RoleName("p1", "Employee")
	if _, err := d.EnsureRole(ctx, "", name, "", rs, []string{"claims:read"}); err != nil {
		t.Fatalf("role: %v", err)
	}

	if err := d.DeleteResourceServer(ctx, rs); err != nil {
		t.Fatalf("delete resource server: %v", err)
	}
	if _, ok := d.resourceServer(ResourceServerIdentifier("acme", "p1")); ok {
		t.Fatal("the resource server is still there")
	}
	if len(d.resources) != 0 || len(d.actions) != 0 {
		t.Fatalf("the subtree survived: %d resource(s), %d action(s)", len(d.resources), len(d.actions))
	}
	if got := d.grants(t, name); len(got) != 0 {
		t.Fatalf("the role kept grants whose actions are gone: %v", got)
	}
}

func TestFakeDirectoryUnassignLeavesThePrincipalAlone(t *testing.T) {
	d := newFakeDirectory()
	ctx := context.Background()
	rs, _, _ := seedCatalog(t, d)
	role, err := d.EnsureRole(ctx, "", RoleName("p1", "Employee"), "", rs, []string{"claims:read"})
	if err != nil {
		t.Fatalf("role: %v", err)
	}
	group := d.seedGroup("Employees")
	principal := Principal{Kind: PrincipalGroup, ID: DirectoryID(group.ID)}
	if err := d.AssignRole(ctx, role, principal); err != nil {
		t.Fatalf("assign: %v", err)
	}

	if err := d.UnassignRole(ctx, role, principal); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	held, err := d.ListRoleAssignments(ctx, role)
	if err != nil {
		t.Fatalf("assignments: %v", err)
	}
	if len(held) != 0 {
		t.Fatalf("the binding survived: %+v", held)
	}
	if _, ok := d.groups[strings.ToLower("Employees")]; !ok {
		t.Fatal("un-assigning a role deleted the org group")
	}
}

// The call log's format is a contract of its own: the converge tests read it,
// so it is spelled out here rather than described.
func TestFakeDirectoryCallLogFormat(t *testing.T) {
	d := newFakeDirectory()
	ctx := context.Background()
	identifier := ResourceServerIdentifier("acme", "p1")
	rs, err := d.EnsureResourceServer(ctx, identifier, "p1")
	if err != nil {
		t.Fatalf("resource server: %v", err)
	}
	res, err := d.CreateResource(ctx, rs, "claims", "claims", "")
	if err != nil {
		t.Fatalf("resource: %v", err)
	}
	if _, err := d.CreateAction(ctx, rs, res.ID, "read", "read", ""); err != nil {
		t.Fatalf("action: %v", err)
	}
	role, err := d.EnsureRole(ctx, "", RoleName("p1", "Employee"), "", rs, []string{"claims:read"})
	if err != nil {
		t.Fatalf("role: %v", err)
	}
	group := d.seedGroup("Employees")
	principal := Principal{Kind: PrincipalGroup, ID: DirectoryID(group.ID)}
	for range 2 {
		if err := d.AssignRole(ctx, role, principal); err != nil {
			t.Fatalf("assign: %v", err)
		}
	}

	want := []string{
		"EnsureResourceServer " + identifier,
		"CreateResourceServer " + identifier,
		"CreateResource claims",
		"CreateAction claims:read",
		"EnsureRole p1/Employee",
		"CreateRole p1/Employee [claims:read]",
		"AssignRole p1/Employee [group:" + group.ID + "]",
		"AssignRole p1/Employee [group:" + group.ID + "] (noop)",
	}
	if len(d.Calls) != len(want) {
		t.Fatalf("call log:\n got %v\nwant %v", d.Calls, want)
	}
	for i := range want {
		if d.Calls[i] != want[i] {
			t.Fatalf("call %d: got %q, want %q", i, d.Calls[i], want[i])
		}
	}
	// The second assignment reached the directory and changed nothing, so it is
	// a call but not a write.
	if got := len(d.writes()); got != 5 {
		t.Fatalf("want 5 writes (the second assignment is not one), got %d: %v", got, d.writeLines())
	}
}
