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

// panel.go — the console's Security panel: the read behind it, and the three
// mutations it offers on a test account.
//
// Everything here has to reconcile one awkward pair of facts. The objects are
// SHARED — a role and a test account live at one environment's identity
// provider, and two of that org's projects naming the same one mean the same one
// — but the console reaching them is scoped to a project, in an org. So every
// mutation below is fenced twice, and the fences are different in kind:
//
//   - **The org+project fence.** `test_user_refs` is the only project-scoped row
//     this domain owns, so it is the only thing that can answer "may THIS project
//     act on this account". A username the project does not reference is a
//     NotFound, never an action — without that, any project could rotate the
//     password of any account any other project provisioned, and the shared
//     directory would silently be a shared blast radius.
//
//   - **The ownership fence.** A `test_users` row IS the platform's ownership
//     marker (the IdP takes no custom attributes, so nothing can be stamped on
//     the directory object itself). This is the SAME rule ensure.go refuses on:
//     no row means the platform did not create the account, so it must not reset
//     its password or delete it — a design naming a real person's username must
//     not hand a console button their login.
//
// The read degrades instead of failing: a directory that cannot be reached —
// including an environment with no identity provider bound to it yet — leaves
// `DirectoryAvailable` false and the store-derived fields intact, so the console
// can say "unknown" rather than rendering absence as "does not exist". That is
// why the resolver's pure Scope and its I/O-performing Resolve are separate
// calls: the store rows can still be read when the directory cannot.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// ErrPanelNotFound is the answer to every request that fails either fence: the
// project does not reference the username, or the platform does not own the
// account. ONE sentinel for both, deliberately — telling them apart would tell
// a caller in project A that a username it may not touch exists somewhere else,
// which is the cross-project disclosure the fence exists to prevent.
var ErrPanelNotFound = errors.New("identity: no such test user for this project")

// ErrPasswordChangedNotRecorded is returned when a rotate wrote the new password
// to the directory and then failed to seal it. It is its own error because the
// recovery is specific and a caller cannot guess it: the account's password IS
// the new one, the platform cannot serve it, and the fix is to rotate again.
var ErrPasswordChangedNotRecorded = errors.New(
	"identity: the password was changed on the identity provider but NOT recorded by the platform — " +
		"the account is currently unusable by the platform; rotate again to resynchronise")

// RoleState is one role as it exists on the directory right now, joined against
// the platform's record. The panel shows the WHOLE catalog, not this project's
// slice of it: roles are shared, so "which existing role does this design reuse"
// is the question the panel answers, and it is unanswerable from a filtered list.
type RoleState struct {
	Name            string
	Description     string
	PlatformCreated bool
	MemberCount     int
	// Projects is how many projects on this directory assign a role to the
	// group — the "reused · holds roles in n projects" a role card shows. A
	// group only this project uses reads 1; one nobody has bound yet reads 0.
	Projects int
}

// RoleAssignment is one group a project role is assigned to, and how
// load-bearing that group already is.
//
// The count is deliberately part of the assignment rather than left to the
// reader to look up: "assigned to Finance" and "assigned to Finance, which
// holds roles in two other projects" are different facts to a person deciding
// whether to reuse a group, and only the second is actionable.
type RoleAssignment struct {
	Group string
	// Projects is Store.CountProjectsBindingGroup for Group — DISTINCT projects,
	// so it counts 1 for a group only this project binds.
	Projects int
}

// ProjectRole is one role THIS project owns on the directory: a
// `<project>/<Role>`, the groups it is assigned to, and what it grants.
//
// It is a different thing from RoleState above and deliberately a different
// type. A RoleState is a SHARED org group — anybody's, additive, and the reason
// the panel shows the whole catalog. A ProjectRole is project-OWNED: this
// project's build created it, converges it and deletes it, and no other project
// can hold one by the same name.
type ProjectRole struct {
	// Name is the role as the design declares it (`Approver`), which is what the
	// console renders and what a test user reference names.
	Name string
	// DirectoryName is the name the directory carries — `<project>/Approver`.
	// The prefix is the platform's ownership device, so it is reported for the
	// operator reading a live directory and never used as a label.
	DirectoryName string
	// ResourceServer is the identifier every grant of this role is on: the
	// access token's `aud` and the `resource` a scoped token is asked for.
	ResourceServer string
	// Scopes are the catalog handles the role grants, read from the DIRECTORY
	// and sorted. Empty when the directory could not be asked — no table holds
	// them, so absence here is "unknown", the same as Exists on an account.
	Scopes []string
	// AssignedTo are the groups holding the role, in binding order. Empty is
	// meaningful: it is the normal shape for a self-service role (the app's
	// registration flow assigns it per account) and for a service role (phase 6
	// attaches an application principal).
	AssignedTo []RoleAssignment
}

// TestUserState is one test account THIS project references, with the live
// directory facts folded in.
type TestUserState struct {
	Username string
	// RoleName is the role the account exists FOR — the stored reference's one
	// role, and always Roles[0]. It is the v1 singular and is kept only while
	// the console moves to Roles; phase 5 removes it.
	RoleName string
	// Roles is every project role this login holds: the one it exists for
	// first, then any other role of this project whose group the account is
	// also a member of. The extra ones are read from the DIRECTORY, so a role
	// an administrator granted by hand shows up here — the panel reports the
	// world, not the design.
	Roles []string
	// Scopes is the union of those roles' grants, sorted: the catalog handles
	// this login's access token will carry. Empty when the directory could not
	// be asked, which is "unknown" rather than "none".
	Scopes   []string
	Supplied bool
	// ColdStart is a v1 leftover carried for the wire contract and is always
	// false — see TestUserRef.ColdStart in entities.go. Phase 5 removes it.
	ColdStart bool
	// Exists is presence on the directory. It is meaningless when the panel
	// reports DirectoryAvailable false, which is exactly why that flag exists.
	Exists bool
	// Owned is the ownership marker: the platform holds a sealed password and may
	// reveal, rotate or delete. False means hands off.
	Owned     bool
	RotatedAt *time.Time
	// ReferencingProjects is every project that references this account, and
	// ReferencingCount is how many there are. Both come from ONE read now: an
	// account lives on exactly one org's environment directory, so every project
	// that can reference it is that org's. While a single identity provider
	// served the cluster the count had to be a bare cross-org number with no
	// names, because naming another org's project was a disclosure this org's
	// panel had no licence for.
	ReferencingProjects []string
	ReferencingCount    int
}

// PanelView is the whole read.
type PanelView struct {
	Roles []RoleState
	// ProjectRoles are the roles THIS project owns, which the shared catalog in
	// Roles above deliberately does not contain: the two are different kinds of
	// object with different ownership rules, and folding them into one list
	// would make "which existing group does this design reuse" unanswerable.
	//
	// They are derived from the platform's own binding rows, so they survive a
	// directory outage with everything but their Scopes.
	ProjectRoles []ProjectRole
	TestUsers    []TestUserState
	// DirectoryAvailable is false when the identity provider could not be
	// reached. Roles is then empty and Exists is false throughout — neither means
	// "absent", and the console must say so.
	DirectoryAvailable bool
}

// PasswordDisclosure is what reveal and rotate answer with. It is the only shape
// in this package that carries a plaintext password, and it is never part of a
// read.
type PasswordDisclosure struct {
	Username  string
	Password  string
	RotatedAt *time.Time
}

// PanelService serves the console's Security panel over the store and the
// environment's directory.
type PanelService struct {
	targets TargetResolver
	store   Store
}

// NewPanelService builds the panel. The store is required; the resolver may be
// nil — a stack that cannot reach an identity provider still has the platform's
// own record, and a panel that reports DirectoryAvailable false is a better
// answer than a surface that 503s. Every MUTATION still needs the directory and
// refuses without it, because there is nothing to write to.
func NewPanelService(targets TargetResolver, store Store) *PanelService {
	return &PanelService{targets: targets, store: store}
}

// Enabled reports whether the panel can be served at all. Only the store is
// load-bearing; see NewPanelService.
func (s *PanelService) Enabled() bool { return s != nil && s.store != nil }

// View reads the panel for one project.
//
// Two independent sources, and a directory failure degrades rather than fails:
// the references and ownership come from the store (which is the platform's own
// data and either works or the request is broken), while the role catalog and
// account presence come from the directory (which is a remote system that can be
// down while the rest of the answer is still true and useful).
func (s *PanelService) View(ctx context.Context, orgID, projectID string) (PanelView, error) {
	if s.targets == nil {
		// No resolver at all: there is no environment to scope the platform's own
		// record to either, so the honest answer is an empty, explicitly
		// unavailable panel rather than rows from a directory nobody named.
		return PanelView{DirectoryAvailable: false}, nil
	}
	scope := s.targets.Scope(orgID)
	refs, err := s.store.ListProjectRefs(ctx, scope, projectID)
	if err != nil {
		return PanelView{}, err
	}
	// The project's own roles come from the platform's binding rows, which are
	// readable whether or not the identity provider is: everything about them
	// except what they GRANT is the platform's own data.
	bindings, err := s.store.ListRoleBindings(ctx, scope, projectID)
	if err != nil {
		return PanelView{}, err
	}

	view := PanelView{DirectoryAvailable: true}

	// The live half. A failure here is logged and dropped: DirectoryAvailable
	// carries the fact to the console, which renders "unknown" instead of
	// inventing an absence. An environment with no identity provider bound yet
	// fails exactly here, and reads as "unknown" for the same reason.
	liveAccounts := map[string]string{}
	var directory Directory
	target, terr := s.targets.Resolve(ctx, orgID)
	if terr != nil {
		slog.WarnContext(ctx, "roles panel: no identity provider for this environment, degrading the read",
			"scope", scope.String(), "project", projectID, "error", terr)
		view.DirectoryAvailable = false
	} else {
		roles, rerr := s.rolesFromDirectory(ctx, target)
		if rerr != nil {
			slog.WarnContext(ctx, "roles panel: identity provider unreachable, degrading the read",
				"scope", scope.String(), "project", projectID, "error", rerr)
			view.DirectoryAvailable = false
		} else {
			directory = target.Directory
			view.Roles = roles
			for _, ref := range refs {
				account, found, ferr := target.Directory.FindUserByUsername(ctx, ref.Username)
				if ferr != nil {
					slog.WarnContext(ctx, "roles panel: account presence unavailable",
						"username", ref.Username, "error", ferr)
					view.DirectoryAvailable = false
					continue
				}
				if found {
					// The id, not a bare bool: the roles an account actually holds
					// are read from its group memberships, and only the directory's
					// own id addresses those.
					liveAccounts[ref.Username] = account.ID
				}
			}
		}
	}

	projectRoles, err := s.projectRoles(ctx, scope, orgID, projectID, directory, bindings)
	if err != nil {
		return PanelView{}, err
	}
	view.ProjectRoles = projectRoles

	for _, ref := range refs {
		accountID, exists := liveAccounts[ref.Username]
		state, serr := s.testUserState(ctx, scope, ref, exists)
		if serr != nil {
			return PanelView{}, serr
		}
		state.Roles, state.Scopes = s.heldRoles(ctx, ref, accountID, directory, projectRoles)
		view.TestUsers = append(view.TestUsers, state)
	}
	return view, nil
}

// projectRoles folds the platform's binding rows into one entry per role of
// this project, with the reuse count of every group it is assigned to and, when
// the directory can be reached, what the role grants.
//
// The resource server is READ from the platform's row and DERIVED when there is
// none: the identifier is agreed on by parties that never speak to each other
// (see resource_server.go), so a project whose first build has not run yet can
// still be told the `aud` its tokens will carry. The row wins when it exists,
// because a project built before the derivation changed would otherwise be
// described by a name no token of its has.
func (s *PanelService) projectRoles(
	ctx context.Context, scope Scope, orgID, projectID string,
	directory Directory, bindings []IdPRoleBinding,
) ([]ProjectRole, error) {
	if len(bindings) == 0 {
		return nil, nil
	}
	identifier := ResourceServerIdentifier(orgID, projectID)
	recorded, err := s.store.GetResourceServer(ctx, scope, projectID)
	if err != nil {
		return nil, err
	}
	if recorded != nil && recorded.Identifier != "" {
		identifier = recorded.Identifier
	}

	// One count per GROUP, not per binding: a group two roles of this project
	// are assigned to is one question about that group, and asking it twice
	// would double the store reads for an identical answer.
	counts := map[string]int{}
	countFor := func(group string) (int, error) {
		key := strings.ToLower(group)
		if n, seen := counts[key]; seen {
			return n, nil
		}
		n, cerr := s.store.CountProjectsBindingGroup(ctx, scope, group)
		if cerr != nil {
			return 0, cerr
		}
		counts[key] = n
		return n, nil
	}

	var out []ProjectRole
	index := map[string]int{}
	for _, binding := range bindings {
		i, seen := index[binding.Role]
		if !seen {
			i = len(out)
			index[binding.Role] = i
			out = append(out, ProjectRole{
				Name:           binding.Role,
				DirectoryName:  RoleName(projectID, binding.Role),
				ResourceServer: identifier,
				Scopes:         s.roleScopes(ctx, directory, binding),
			})
		}
		// The empty group name is the "recorded, assigned to nobody" marker, not
		// a group: a self-service role's row carries it so the delete path can
		// find the role, and reporting it as an assignment would invent a group
		// called "".
		if binding.GroupName == "" {
			continue
		}
		projects, cerr := countFor(binding.GroupName)
		if cerr != nil {
			return nil, cerr
		}
		out[i].AssignedTo = append(out[i].AssignedTo, RoleAssignment{
			Group: binding.GroupName, Projects: projects,
		})
	}
	return out, nil
}

// roleScopes reads what one role grants, best-effort.
//
// Best-effort for the same reason the catalog's member count is: it costs one
// call per role, and losing one must not cost the caller the rest of the panel.
// An empty answer reads as "unknown" beside DirectoryAvailable, never as "this
// role grants nothing" — a role that genuinely grants nothing is a design the
// gate refuses.
func (s *PanelService) roleScopes(ctx context.Context, directory Directory, binding IdPRoleBinding) []string {
	if directory == nil || binding.DirectoryRoleID == "" {
		return nil
	}
	scopes, err := directory.ListRolePermissions(ctx, DirectoryID(binding.DirectoryRoleID))
	if err != nil {
		slog.WarnContext(ctx, "roles panel: role grants unavailable", "role", binding.Role, "error", err)
		return nil
	}
	sort.Strings(scopes)
	return scopes
}

// heldRoles answers what ONE test account may do: the roles it holds and the
// union of their grants.
//
// The account's own role — the one the reference says it exists for — comes
// first and is always present, so `Roles[0]` is `RoleName` whatever the
// directory says. Everything after it is read from the account's GROUP
// memberships joined against this project's role assignments, which is how a
// v2 account holding two roles, or one an administrator enrolled by hand,
// becomes visible without a table that knows about it.
//
// The scopes are the union of the roles' grants, deduplicated and sorted, so
// two reads of an unchanged directory return byte-identical rows.
func (s *PanelService) heldRoles(
	ctx context.Context, ref TestUserRef, accountID string,
	directory Directory, roles []ProjectRole,
) (held []string, scopes []string) {
	byName := make(map[string]ProjectRole, len(roles))
	for _, role := range roles {
		byName[strings.ToLower(role.Name)] = role
	}
	seen := map[string]bool{}
	add := func(name string) {
		if name == "" || seen[strings.ToLower(name)] {
			return
		}
		seen[strings.ToLower(name)] = true
		held = append(held, name)
	}
	add(ref.RoleName)

	if directory != nil && accountID != "" {
		groups, err := directory.UserGroups(ctx, accountID)
		if err != nil {
			// Best-effort, like every other per-account directory read here: the
			// account's own role is still reported.
			slog.WarnContext(ctx, "roles panel: account group membership unavailable",
				"username", ref.Username, "error", err)
		} else {
			member := make(map[string]bool, len(groups))
			for _, group := range groups {
				member[strings.ToLower(group.Name)] = true
			}
			// Role order, not group order: the panel's roles are already in the
			// store's (role, group) order, so the answer is stable.
			for _, role := range roles {
				for _, assignment := range role.AssignedTo {
					if member[strings.ToLower(assignment.Group)] {
						add(role.Name)
						break
					}
				}
			}
		}
	}

	granted := map[string]bool{}
	for _, name := range held {
		for _, handle := range byName[strings.ToLower(name)].Scopes {
			granted[handle] = true
		}
	}
	if len(granted) == 0 {
		return held, nil
	}
	scopes = make([]string, 0, len(granted))
	for handle := range granted {
		scopes = append(scopes, handle)
	}
	sort.Strings(scopes)
	return held, scopes
}

// rolesFromDirectory projects the shared catalog join into this package's panel
// view type. The join itself lives in catalog.go and is the SAME one the
// design-time `list_groups` tool reads, so the console and the design agent can
// never disagree about which groups the platform created.
func (s *PanelService) rolesFromDirectory(ctx context.Context, target Target) ([]RoleState, error) {
	entries, err := readCatalog(ctx, target, s.store)
	if err != nil {
		return nil, err
	}
	out := make([]RoleState, 0, len(entries))
	for _, e := range entries {
		out = append(out, RoleState{
			Name:            e.Name,
			Description:     e.Description,
			PlatformCreated: e.PlatformCreated,
			MemberCount:     e.MemberCount,
			Projects:        e.Projects,
		})
	}
	return out, nil
}

// testUserState folds one reference together with the platform's record and the
// referencing counts.
func (s *PanelService) testUserState(ctx context.Context, scope Scope, ref TestUserRef, exists bool) (TestUserState, error) {
	state := TestUserState{
		Username:  ref.Username,
		RoleName:  ref.RoleName,
		Supplied:  ref.Supplied,
		ColdStart: ref.ColdStart,
		Exists:    exists,
	}
	owned, err := s.store.GetTestUser(ctx, scope, ref.Username)
	if err != nil {
		return TestUserState{}, err
	}
	if owned != nil {
		state.Owned = true
		state.RotatedAt = owned.RotatedAt
	}
	others, err := s.store.ProjectsReferencing(ctx, scope, ref.Username)
	if err != nil {
		return TestUserState{}, err
	}
	for _, o := range others {
		state.ReferencingProjects = append(state.ReferencingProjects, o.ProjectID)
	}
	state.ReferencingCount = len(others)
	return state, nil
}

// Reveal discloses a platform-owned account's password.
//
// It reads, but it is a disclosure, and it is fenced exactly as hard as the two
// writes below: the project must reference the username and the platform must
// own the account.
func (s *PanelService) Reveal(ctx context.Context, orgID, projectID, username string) (PasswordDisclosure, error) {
	scope, owned, err := s.resolveOwned(ctx, orgID, projectID, username)
	if err != nil {
		return PasswordDisclosure{}, err
	}
	password, err := s.store.RevealTestUserPassword(ctx, scope, owned.Username)
	if err != nil {
		if errors.Is(err, ErrNoPassword) {
			// No sealed password is the same answer as no row: the platform cannot
			// serve this credential, and saying why in more detail would only
			// describe an account the caller may not have.
			return PasswordDisclosure{}, ErrPanelNotFound
		}
		return PasswordDisclosure{}, err
	}
	return PasswordDisclosure{Username: owned.Username, Password: password, RotatedAt: owned.RotatedAt}, nil
}

// Rotate replaces the account's password and returns the new one.
//
// Directory FIRST, store second. That order is not arbitrary: the directory is
// the thing a sign-in actually consults, so a store write that landed against a
// directory write that did not would have the platform confidently serving a
// password that does not work. The reverse failure — directory changed, seal
// lost — is the survivable one, and it is reported rather than swallowed
// (ErrPasswordChangedNotRecorded) because only a second rotate can fix it.
func (s *PanelService) Rotate(ctx context.Context, orgID, projectID, username string) (PasswordDisclosure, error) {
	scope, owned, err := s.resolveOwned(ctx, orgID, projectID, username)
	if err != nil {
		return PasswordDisclosure{}, err
	}
	target, err := s.mutableDirectory(ctx, orgID)
	if err != nil {
		return PasswordDisclosure{}, err
	}
	password, err := generatePassword()
	if err != nil {
		return PasswordDisclosure{}, err
	}
	if err := target.Directory.SetUserPassword(ctx, owned.ThunderUserID, password); err != nil {
		// Nothing changed anywhere: the old password still works and is still
		// sealed. An ordinary error.
		return PasswordDisclosure{}, fmt.Errorf("rotate password for %q: %w", owned.Username, err)
	}
	if err := s.store.SetTestUserPassword(ctx, scope, owned.Username, password); err != nil {
		slog.ErrorContext(ctx, "roles panel: password rotated on the identity provider but NOT sealed",
			"scope", scope.String(), "project", projectID, "username", owned.Username, "error", err)
		return PasswordDisclosure{}, fmt.Errorf("%w: %w", ErrPasswordChangedNotRecorded, err)
	}
	now := time.Now().UTC()
	return PasswordDisclosure{Username: owned.Username, Password: password, RotatedAt: &now}, nil
}

// DeleteResult is what a delete did. RemainingReferences is how many OTHER
// projects still referenced the account when it went — the honest warning the
// shared directory makes necessary. UnenrolledFrom names the roles the account
// was taken out of on the way, which is the part of the work that is invisible
// from the directory afterwards.
type DeleteResult struct {
	Username            string
	RemainingReferences int
	UnenrolledFrom      []string
}

// Delete removes the account from the directory and forgets the platform's
// record of it.
//
// It does NOT delete the role. Roles are shared and outlive the accounts in
// them: dropping `Support Agent` because one test login went away would take the
// role from every other project that names it, and from every real member of it.
// The additive-only rule that governs the ensure governs this too.
//
// It DOES take the account out of its roles first, and that ordering is the
// whole point. The identity provider has no referential integrity between
// accounts and group membership: deleting an account leaves every group it
// belonged to naming an id that no longer resolves, and the next build that
// tries to enrol somebody into such a role used to DESTROY it — the write is a
// delete-and-recreate, and the recreate is rejected wholesale for the dead
// member. This function is where that corruption was introduced, so it is
// where it has to stop.
//
// Un-enrol first, then delete. The failure modes are not symmetrical:
//
//   - un-enrol fails → nothing has happened, the account is still whole, and
//     the caller can retry. So a failure here ABORTS the delete rather than
//     pressing on: pressing on is precisely how the dangling member is made.
//   - un-enrol succeeds, delete fails → a live account that holds no roles.
//     Harmless and self-repairing, because the next ensure re-enrols it.
//
// Roles the platform does not own are un-enrolled from too. A group somebody
// else made is not ours to curate, but an account being deleted must not be
// left behind in it — that reference would be corruption the platform caused.
func (s *PanelService) Delete(ctx context.Context, orgID, projectID, username string) (DeleteResult, error) {
	scope, owned, err := s.resolveOwned(ctx, orgID, projectID, username)
	if err != nil {
		return DeleteResult{}, err
	}
	target, err := s.mutableDirectory(ctx, orgID)
	if err != nil {
		return DeleteResult{}, err
	}
	// Counted BEFORE the delete: DeleteTestUser drops every reference row, so
	// afterwards the answer is always zero and the warning could never be made.
	others, err := s.store.ProjectsReferencing(ctx, scope, owned.Username)
	if err != nil {
		return DeleteResult{}, err
	}
	remaining := len(others) - 1
	if remaining < 0 {
		remaining = 0
	}
	if remaining > 0 {
		slog.WarnContext(ctx, "roles panel: deleting a test account other projects still reference",
			"scope", scope.String(), "project", projectID, "username", owned.Username, "otherProjects", remaining)
	}
	unenrolled, err := unenrol(ctx, target.Directory, owned)
	if err != nil {
		return DeleteResult{}, err
	}
	if err := target.Directory.DeleteUser(ctx, owned.ThunderUserID); err != nil {
		return DeleteResult{}, fmt.Errorf("delete test user %q: %w", owned.Username, err)
	}
	// Directory first, then the record — the same ordering as rotate, and for the
	// same reason: a forgotten record over a live account is an account nobody
	// can rotate or delete ever again.
	if err := s.store.DeleteTestUser(ctx, scope, owned.Username); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{
		Username:            owned.Username,
		RemainingReferences: remaining,
		UnenrolledFrom:      unenrolled,
	}, nil
}

// unenrol takes an account out of every group it belongs to on the directory
// the account lives in, and reports which ones. See Delete for why this runs
// before the account is deleted and why a failure here stops the delete. It
// takes the directory rather than reading one off the service because the
// panel has none of its own: the directory is the environment's, resolved per
// call.
func unenrol(ctx context.Context, dir Directory, owned *TestUser) ([]string, error) {
	groups, err := dir.UserGroups(ctx, owned.ThunderUserID)
	if err != nil {
		return nil, fmt.Errorf("read the roles of test user %q: %w", owned.Username, err)
	}
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		if _, err := dir.RemoveMembers(ctx, g, []string{owned.ThunderUserID}); err != nil {
			return nil, fmt.Errorf("remove test user %q from role %q: %w", owned.Username, g.Name, err)
		}
		out = append(out, g.Name)
	}
	return out, nil
}

// mutableDirectory resolves the directory a WRITE goes to, and refuses rather
// than degrading. The read may say "unknown" when the identity provider cannot
// be reached; a write has nothing to write to, and pretending otherwise would
// report a rotation that never happened.
func (s *PanelService) mutableDirectory(ctx context.Context, orgID string) (Target, error) {
	if s.targets == nil {
		return Target{}, errors.New("identity: no identity provider is configured; this account cannot be changed")
	}
	target, err := s.targets.Resolve(ctx, orgID)
	if err != nil {
		return Target{}, fmt.Errorf("identity: no identity provider for %s: %w", s.targets.Scope(orgID), err)
	}
	return target, nil
}

// resolveOwned applies BOTH fences and returns the platform's record of the
// account. It is the single gate every mutation goes through, so neither fence
// can be forgotten at one call site.
func (s *PanelService) resolveOwned(ctx context.Context, orgID, projectID, username string) (Scope, *TestUser, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return Scope{}, nil, ErrPanelNotFound
	}
	if s.targets == nil {
		// Nothing can be addressed without an environment, and saying which
		// environment is missing tells a caller nothing it can act on here.
		return Scope{}, nil, ErrPanelNotFound
	}
	scope := s.targets.Scope(orgID)
	// Fence 1 — org + project. The reference rows are the only project-scoped
	// thing here, so they are the only thing that can license the action.
	refs, err := s.store.ListProjectRefs(ctx, scope, projectID)
	if err != nil {
		return Scope{}, nil, err
	}
	referenced := false
	for _, ref := range refs {
		if ref.Username == username {
			referenced = true
			break
		}
	}
	if !referenced {
		return Scope{}, nil, ErrPanelNotFound
	}
	// Fence 2 — ownership. Same rule ensure.go refuses on: no `test_users` row
	// means the platform did not create this account, so it may not touch it.
	owned, err := s.store.GetTestUser(ctx, scope, username)
	if err != nil {
		return Scope{}, nil, err
	}
	if owned == nil {
		return Scope{}, nil, ErrPanelNotFound
	}
	return scope, owned, nil
}
