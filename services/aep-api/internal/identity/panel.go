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
// SHARED — a role and a test account live at the identity provider's scope, and
// two projects naming the same one mean the same one — but the console reaching
// them is scoped to a project, in an org. So every mutation below is fenced
// twice, and the fences are different in kind:
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
// The read degrades instead of failing: a directory that cannot be reached
// leaves `DirectoryAvailable` false and the store-derived fields intact, so the
// console can say "unknown" rather than rendering absence as "does not exist".

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
}

// TestUserState is one test account THIS project references, with the live
// directory facts folded in.
type TestUserState struct {
	Username  string
	RoleName  string
	Supplied  bool
	ColdStart bool
	// Exists is presence on the directory. It is meaningless when the panel
	// reports DirectoryAvailable false, which is exactly why that flag exists.
	Exists bool
	// Owned is the ownership marker: the platform holds a sealed password and may
	// reveal, rotate or delete. False means hands off.
	Owned     bool
	RotatedAt *time.Time
	// ReferencingProjects is THIS ORG's projects only. ReferencingCount is the
	// total across every org — a bare count, never names, because a project name
	// is one org's data and the shared account does not license disclosing it.
	ReferencingProjects []string
	ReferencingCount    int
}

// PanelView is the whole read.
type PanelView struct {
	Roles     []RoleState
	TestUsers []TestUserState
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
// directory.
type PanelService struct {
	dir   Directory
	store Store
}

// NewPanelService builds the panel. The store is required; the directory may be
// nil — a stack with no identity provider still has the platform's own record,
// and a panel that reports DirectoryAvailable false is a better answer than a
// surface that 503s. Every MUTATION still needs the directory and refuses
// without it, because there is nothing to write to.
func NewPanelService(dir Directory, store Store) *PanelService {
	return &PanelService{dir: dir, store: store}
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
	refs, err := s.store.ListProjectRefs(ctx, orgID, projectID)
	if err != nil {
		return PanelView{}, err
	}

	view := PanelView{DirectoryAvailable: s.dir != nil}

	// The live half. A failure here is logged and dropped: DirectoryAvailable
	// carries the fact to the console, which renders "unknown" instead of
	// inventing an absence.
	liveAccounts := map[string]bool{}
	if s.dir != nil {
		roles, rerr := s.rolesFromDirectory(ctx)
		if rerr != nil {
			slog.WarnContext(ctx, "roles panel: identity provider unreachable, degrading the read",
				"org", orgID, "project", projectID, "error", rerr)
			view.DirectoryAvailable = false
		} else {
			view.Roles = roles
			for _, ref := range refs {
				_, found, ferr := s.dir.FindUserByUsername(ctx, ref.Username)
				if ferr != nil {
					slog.WarnContext(ctx, "roles panel: account presence unavailable",
						"username", ref.Username, "error", ferr)
					view.DirectoryAvailable = false
					continue
				}
				liveAccounts[ref.Username] = found
			}
		}
	}

	for _, ref := range refs {
		state, serr := s.testUserState(ctx, orgID, ref, liveAccounts[ref.Username])
		if serr != nil {
			return PanelView{}, serr
		}
		view.TestUsers = append(view.TestUsers, state)
	}
	return view, nil
}

// rolesFromDirectory projects the shared catalog join into this package's panel
// view type. The join itself lives in catalog.go and is the SAME one the
// design-time `list_roles` tool reads, so the console and the design agent can
// never disagree about which roles the platform created.
func (s *PanelService) rolesFromDirectory(ctx context.Context) ([]RoleState, error) {
	entries, err := readCatalog(ctx, s.dir, s.store)
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
		})
	}
	return out, nil
}

// testUserState folds one reference together with the platform's record and the
// referencing counts.
func (s *PanelService) testUserState(ctx context.Context, orgID string, ref TestUserRef, exists bool) (TestUserState, error) {
	state := TestUserState{
		Username:  ref.Username,
		RoleName:  ref.RoleName,
		Supplied:  ref.Supplied,
		ColdStart: ref.ColdStart,
		Exists:    exists,
	}
	owned, err := s.store.GetTestUser(ctx, ref.Username)
	if err != nil {
		return TestUserState{}, err
	}
	if owned != nil {
		state.Owned = true
		state.RotatedAt = owned.RotatedAt
	}
	others, err := s.store.ProjectsReferencing(ctx, orgID, ref.Username)
	if err != nil {
		return TestUserState{}, err
	}
	for _, o := range others {
		state.ReferencingProjects = append(state.ReferencingProjects, o.ProjectID)
	}
	count, err := s.store.CountReferencing(ctx, ref.Username)
	if err != nil {
		return TestUserState{}, err
	}
	state.ReferencingCount = count
	return state, nil
}

// Reveal discloses a platform-owned account's password.
//
// It reads, but it is a disclosure, and it is fenced exactly as hard as the two
// writes below: the project must reference the username and the platform must
// own the account.
func (s *PanelService) Reveal(ctx context.Context, orgID, projectID, username string) (PasswordDisclosure, error) {
	owned, err := s.resolveOwned(ctx, orgID, projectID, username)
	if err != nil {
		return PasswordDisclosure{}, err
	}
	password, err := s.store.RevealTestUserPassword(ctx, owned.Username)
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
	owned, err := s.resolveOwned(ctx, orgID, projectID, username)
	if err != nil {
		return PasswordDisclosure{}, err
	}
	if s.dir == nil {
		return PasswordDisclosure{}, errors.New("identity: no identity provider is configured; a password cannot be rotated")
	}
	password, err := generatePassword()
	if err != nil {
		return PasswordDisclosure{}, err
	}
	if err := s.dir.SetUserPassword(ctx, owned.ThunderUserID, password); err != nil {
		// Nothing changed anywhere: the old password still works and is still
		// sealed. An ordinary error.
		return PasswordDisclosure{}, fmt.Errorf("rotate password for %q: %w", owned.Username, err)
	}
	if err := s.store.SetTestUserPassword(ctx, owned.Username, password); err != nil {
		slog.ErrorContext(ctx, "roles panel: password rotated on the identity provider but NOT sealed",
			"org", orgID, "project", projectID, "username", owned.Username, "error", err)
		return PasswordDisclosure{}, fmt.Errorf("%w: %w", ErrPasswordChangedNotRecorded, err)
	}
	now := time.Now().UTC()
	return PasswordDisclosure{Username: owned.Username, Password: password, RotatedAt: &now}, nil
}

// DeleteResult is what a delete did. RemainingReferences is how many OTHER
// projects still referenced the account when it went — the honest warning the
// shared directory makes necessary.
type DeleteResult struct {
	Username            string
	RemainingReferences int
}

// Delete removes the account from the directory and forgets the platform's
// record of it.
//
// It does NOT delete the role. Roles are shared and outlive the accounts in
// them: dropping `Support Agent` because one test login went away would take the
// role from every other project that names it, and from every real member of it.
// The additive-only rule that governs the ensure governs this too.
func (s *PanelService) Delete(ctx context.Context, orgID, projectID, username string) (DeleteResult, error) {
	owned, err := s.resolveOwned(ctx, orgID, projectID, username)
	if err != nil {
		return DeleteResult{}, err
	}
	if s.dir == nil {
		return DeleteResult{}, errors.New("identity: no identity provider is configured; an account cannot be deleted")
	}
	// Counted BEFORE the delete: DeleteTestUser drops every reference row, so
	// afterwards the answer is always zero and the warning could never be made.
	total, err := s.store.CountReferencing(ctx, owned.Username)
	if err != nil {
		return DeleteResult{}, err
	}
	remaining := total - 1
	if remaining < 0 {
		remaining = 0
	}
	if remaining > 0 {
		slog.WarnContext(ctx, "roles panel: deleting a test account other projects still reference",
			"org", orgID, "project", projectID, "username", owned.Username, "otherProjects", remaining)
	}
	if err := s.dir.DeleteUser(ctx, owned.ThunderUserID); err != nil {
		return DeleteResult{}, fmt.Errorf("delete test user %q: %w", owned.Username, err)
	}
	// Directory first, then the record — the same ordering as rotate, and for the
	// same reason: a forgotten record over a live account is an account nobody
	// can rotate or delete ever again.
	if err := s.store.DeleteTestUser(ctx, owned.Username); err != nil {
		return DeleteResult{}, err
	}
	return DeleteResult{Username: owned.Username, RemainingReferences: remaining}, nil
}

// resolveOwned applies BOTH fences and returns the platform's record of the
// account. It is the single gate every mutation goes through, so neither fence
// can be forgotten at one call site.
func (s *PanelService) resolveOwned(ctx context.Context, orgID, projectID, username string) (*TestUser, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, ErrPanelNotFound
	}
	// Fence 1 — org + project. The reference rows are the only project-scoped
	// thing here, so they are the only thing that can license the action.
	refs, err := s.store.ListProjectRefs(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	referenced := false
	for _, ref := range refs {
		if ref.Username == username {
			referenced = true
			break
		}
	}
	if !referenced {
		return nil, ErrPanelNotFound
	}
	// Fence 2 — ownership. Same rule ensure.go refuses on: no `test_users` row
	// means the platform did not create this account, so it may not touch it.
	owned, err := s.store.GetTestUser(ctx, username)
	if err != nil {
		return nil, err
	}
	if owned == nil {
		return nil, ErrPanelNotFound
	}
	return owned, nil
}
