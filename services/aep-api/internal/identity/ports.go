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

import (
	"context"
	"errors"
)

// DirectoryID is one object's id on the identity provider — a group, an
// account, a resource server, a resource, an action, a role or an assignment
// principal.
//
// It is OPAQUE. Nothing here parses it, compares it for structure or builds one:
// it is handed back by the directory and handed straight in again. The named
// type exists so a signature can say "an id the directory minted" rather than
// `string`, which is also every handle, name and identifier in this file.
//
// The older group/account verbs still speak plain `string` ids, so converting
// (`DirectoryID(group.ID)`) is deliberate and visible at the one seam where the
// two meet.
type DirectoryID string

// Directory is the slice of the Platform IdP admin client this domain uses.
// It is a narrowing of thundersvc.Client, mapped at the composition root, so
// the identity domain names no client package.
//
// It covers two DIFFERENT ownership regimes, which is why the method set reads
// as two halves:
//
//   - accounts and org groups are ORG-owned and additive. The platform creates
//     what is missing and enrols members into groups it made, and never renames
//     or deletes somebody else's. Their shape is dictated by one IdP
//     constraint: membership can only be set when a group is CREATED. That is
//     why CreateGroup takes members, and why AddMembers exists at all rather
//     than a plain add-one-member call.
//   - the resource server, its permission catalog and the `<project>/<Role>`
//     roles are PROJECT-owned and converge to the spec tag being built. Exactly
//     one project creates them and they carry its name, so the ensure may
//     delete what the design dropped.
//
// The two `Ensure…` verbs are the only ones that decide anything: each is
// find-then-create, and each WRITES NOTHING when the directory already matches
// what was asked for. That is what lets a rebuild of an unchanged tag make zero
// writes, and it is the same bargain AddMembers already strikes (a no-op add
// keeps the group's identity). Everything else is a primitive the ensure
// sequences itself: the converge — which resources, actions and roles to create
// and which to delete — lives in ensure.go, where it can be read as one
// decision against the plan, not spread across an adapter that cannot see the
// plan. The cost of that choice is call count (a list per resource rather than
// one round trip), which is the right trade for a per-build operation that
// mints credentials.
//
// ADAPTER: `internal/app/identity_adapters.go` maps `thundersvc.Client` onto
// this port. The in-memory `fakeDirectory` in fakes_test.go is the behavioural
// model these contracts are written against.
type Directory interface {
	// -- accounts and org groups: additive, org-owned ------------------------

	ListGroups(ctx context.Context) ([]DirectoryGroup, error)
	FindGroupByName(ctx context.Context, name string) (*DirectoryGroup, bool, error)
	// GroupMembers returns the accounts in a group. The catalog reads it for a
	// member count; the ensure does NOT — its add path reads membership itself,
	// inside the lock that makes the read-modify-write safe.
	GroupMembers(ctx context.Context, groupID string) ([]string, error)
	CreateGroup(ctx context.Context, name, description string, memberIDs []string) (DirectoryGroup, error)
	// AddMembers adds members to an existing group, keeping the ones already
	// there. The implementation reads and writes under one lock, because the
	// only membership write the IdP offers is a delete-and-recreate of the whole
	// group. Destructive by construction: the ensure calls it ONLY for a group
	// it owns. Returns the group's new identity.
	//
	// The implementation guarantees the group still exists when this returns,
	// error or not, and never writes back a member the IdP no longer has.
	AddMembers(ctx context.Context, group DirectoryGroup, memberIDs []string) (DirectoryGroup, error)

	// RemoveMembers takes members out of a group, keeping everyone else, under
	// the same lock and the same guarantee as AddMembers. Deleting an account
	// without removing it from its groups first leaves the group pointing at an
	// account that no longer exists — see PanelService.Delete.
	RemoveMembers(ctx context.Context, group DirectoryGroup, memberIDs []string) (DirectoryGroup, error)

	// UserGroups returns the groups an account belongs to, so a delete can
	// un-enrol it from exactly those. An account that is already gone reports
	// no groups rather than an error, which keeps a retried delete working.
	UserGroups(ctx context.Context, userID string) ([]DirectoryGroup, error)

	FindUserByUsername(ctx context.Context, username string) (*DirectoryAccount, bool, error)
	CreateUser(ctx context.Context, username, email, password string) (DirectoryAccount, error)
	SetUserPassword(ctx context.Context, userID, password string) error
	DeleteUser(ctx context.Context, userID string) error

	// -- the project's authorization objects: converge to the tag ------------

	// EnsureResourceServer finds the project's resource server by IDENTIFIER —
	// not by name — and creates it when absent, with the delimiter ":" that
	// makes an action's derived permission read `<resource>:<action>`.
	//
	// The identifier is the identity: it is the access token's `aud`, the
	// gateway's expected audience and the `resource` the SPA asks for, and it is
	// unique across the whole directory (a second resource server carrying it is
	// refused — ErrIdentifierConflict). `name` is a label, seeded at create and
	// never converged, because renaming it would change nothing a token sees.
	//
	// Writes nothing when the resource server is already there.
	EnsureResourceServer(ctx context.Context, identifier, name string) (DirectoryID, error)

	// FindResourceServer returns the project's resource server WITHOUT creating
	// one, by the same identifier EnsureResourceServer looks it up by.
	//
	// It exists for the callers that must not write: a teardown reaching for the
	// object when the platform's own row is missing would, through the Ensure
	// verb, mint a resource server purely so the next line could delete it. A
	// find that answers "absent" is the honest answer to "is there anything to
	// remove".
	FindResourceServer(ctx context.Context, identifier string) (DirectoryID, bool, error)

	// ListResources returns the resource server's resources — the first level of
	// the permission catalog, one entry per `permissions[]` entry in
	// security.json. The ensure diffs this against the tag to decide what to
	// create and what to delete.
	ListResources(ctx context.Context, rs DirectoryID) ([]DirectoryResource, error)
	// CreateResource adds one resource. `handle` is the catalog's resource
	// segment (`claims`); the directory derives the resource's own permission
	// from it verbatim. A handle already used under this resource server is
	// ErrHandleConflict.
	CreateResource(ctx context.Context, rs DirectoryID, handle, name, description string) (DirectoryResource, error)
	// UpdateResource rewrites a resource's display name and description on the
	// directory. The HANDLE is not touched and cannot be: it is the identity,
	// and it is what derives the permission.
	//
	// It exists so the converge can treat prose as CONTENT. Without it the only
	// way to correct a description would be a delete and a create, which the
	// directory cascades through the resource's actions and, from there, out of
	// every role that granted them — a permission outage for a wording change.
	UpdateResource(ctx context.Context, rs, resource DirectoryID, name, description string) error

	// DeleteResource removes a resource the tag no longer declares. The
	// directory refuses one that still has actions, so the ensure deletes
	// leaf-first — see DeleteResourceServer for the whole order.
	DeleteResource(ctx context.Context, rs, resource DirectoryID) error

	// ListActions returns one resource's actions. Handle uniqueness is per
	// PARENT RESOURCE, so `claims:read` and `reports:read` coexist and the list
	// must always be read under a named resource.
	ListActions(ctx context.Context, rs, resource DirectoryID) ([]DirectoryAction, error)
	// CreateAction adds one action under a resource. `handle` is the action
	// segment (`read-all`); the granted permission the directory derives is
	// `<resource handle>:<action handle>`. A duplicate under the SAME resource
	// is ErrHandleConflict; the same handle under a different resource is not.
	//
	// Handles are immutable, so a rename in the design is a delete plus a
	// create — which is exactly what the ensure's diff produces.
	CreateAction(ctx context.Context, rs, resource DirectoryID, handle, name, description string) (DirectoryAction, error)
	// UpdateAction rewrites an action's display name and description; its handle
	// is immutable. See UpdateResource for why a description change must not be
	// a delete.
	UpdateAction(ctx context.Context, rs, resource, action DirectoryID, name, description string) error

	// DeleteAction removes an action the tag no longer declares.
	//
	// The directory CASCADES the delete out of every role that granted the
	// permission, with no error and no other signal. Two consequences the ensure
	// depends on: nothing has to strip permissions off roles first, and a role's
	// grants read before a catalog edit are stale afterwards.
	DeleteAction(ctx context.Context, rs, resource, action DirectoryID) error

	// DeleteResourceServer removes the resource server and everything under it.
	//
	// The directory has no cascade of its own — it refuses a parent that still
	// has children — so the ADAPTER owns the order, which is: every action of
	// every resource, then every resource, then the resource server. Roles are
	// not part of it; they are deleted independently (DeleteRole) because they
	// are project-owned, not because the delete needs it.
	DeleteResourceServer(ctx context.Context, rs DirectoryID) error

	// EnsureRole makes one role's permission set match `permissions` — the
	// catalog handles it grants, all of them on the one resource server `rs`.
	//
	// `id` is the role as the CALLER already found it, and the empty id means
	// "create". The ensure holds the whole role listing (ListRoles) before it
	// converges anything, so passing the id it already has costs nothing and
	// saves this port a second listing per declared role — which on a directory
	// carrying every project's roles is the difference between one read and one
	// read per role.
	//
	// With an id: present and different, the permission set is REPLACED
	// wholesale and the existing assignments survive, so converge is one write
	// per changed role and no re-assignment pass; present and already equal,
	// nothing is written — the rebuild-is-free rule. Without one, the role is
	// created with the whole set.
	//
	// `name` is the project-prefixed `<project>/<Role>` (see RoleName): the
	// name carries the ownership, so two projects cannot converge each other's
	// roles. A permission the catalog does not have is refused by the directory,
	// which is why the resources-and-actions pass runs first.
	EnsureRole(ctx context.Context, id DirectoryID, name, description string, rs DirectoryID, permissions []string) (DirectoryID, error)
	// ListRoles returns every role on the directory as name → id. The ensure
	// filters it by the project prefix (RoleNamePrefix) to find the roles it
	// owns, including the ones the design dropped and must now delete.
	ListRoles(ctx context.Context) ([]RoleRef, error)
	// ListRolePermissions returns the catalog handles a role grants, across every
	// resource server it holds a permission block on.
	//
	// It is the READ half of EnsureRole, and it exists for the console: the
	// scopes a test login's token will carry are the union of the grants of the
	// roles it holds, and no table holds them — the plan that expanded them
	// belongs to one build. Asking the directory is the only answer that is true
	// of the world as it is now.
	ListRolePermissions(ctx context.Context, role DirectoryID) ([]string, error)

	// DeleteRole removes a role the tag no longer declares, assignments and all.
	DeleteRole(ctx context.Context, role DirectoryID) error

	// AssignRole binds a principal — an org group, an account or an application
	// — to a role. Assigning a principal that already holds the role is a no-op
	// on the directory, not an error.
	AssignRole(ctx context.Context, role DirectoryID, principal Principal) error
	// UnassignRole takes the binding away. The principal itself is untouched:
	// un-assigning an org group never deletes the group.
	UnassignRole(ctx context.Context, role DirectoryID, principal Principal) error
	// ListRoleAssignments returns who holds the role, so the ensure can converge
	// the bindings to the design's `assignTo` and the console can name them.
	ListRoleAssignments(ctx context.Context, role DirectoryID) ([]Principal, error)
}

// Sentinels for the two conflicts a converge can legitimately meet — both are
// races, not bugs: another writer created the object between the find and the
// create. The adapter maps the directory's own codes onto them so no caller
// reads a wire code.
var (
	// ErrIdentifierConflict is a second resource server claiming an identifier
	// that is already taken (409 RES-1013). Since the identifier is derived from
	// (org, project), meeting this means either a concurrent ensure of the same
	// project — retry and the find will succeed — or two projects deriving one
	// identifier, which is a naming defect.
	ErrIdentifierConflict = errors.New("identity: a resource server with this identifier already exists")
	// ErrHandleConflict is a resource or action whose handle is already taken
	// under the same parent (409 RES-1014). Uniqueness is per parent resource,
	// so this never fires for the same action handle under a second resource.
	ErrHandleConflict = errors.New("identity: a resource or action with this handle already exists")
)

// DirectoryGroup is one group on the IdP.
type DirectoryGroup struct {
	ID          string
	Name        string
	Description string
	OUID        string
}

// DirectoryAccount is one account on the IdP. No password: the IdP does not
// return one.
type DirectoryAccount struct {
	ID       string
	Username string
	Email    string
}

// RoleRef is one role as name → id. The name is the platform's key — it is what
// security.json declares and what the project prefix makes ownable — and the id
// is the handle for the next call.
type RoleRef struct {
	ID   DirectoryID
	Name string
}

// DirectoryResource is one entry of the permission catalog's first level: a
// noun the project's API exposes, owned by exactly one component.
type DirectoryResource struct {
	ID DirectoryID
	// Handle is the catalog's resource segment — `claims` in `claims:read`.
	Handle      string
	Name        string
	Description string
}

// DirectoryAction is one capability on a resource. The permission a role grants
// is the parent's handle, the delimiter and this handle: `claims:read-all`.
type DirectoryAction struct {
	ID DirectoryID
	// Handle is the action segment alone — `read-all`, not `claims:read-all`.
	Handle      string
	Name        string
	Description string
}

// PrincipalKind is what a role can be assigned TO. The three the design relies
// on are the universal directory concepts; the platform deliberately uses no
// vendor-specific principal, so a second adapter needs no model change.
type PrincipalKind string

const (
	// PrincipalGroup is an org group — how a human comes to hold a project role.
	PrincipalGroup PrincipalKind = "group"
	// PrincipalUser is a single account. The ensure does not use it (people are
	// enrolled through groups); the console's per-account views do.
	PrincipalUser PrincipalKind = "user"
	// PrincipalApp is a service principal — an OAuth client holding a role of
	// kind `service`.
	PrincipalApp PrincipalKind = "app"
)

// Principal is one holder of a role.
type Principal struct {
	Kind PrincipalKind
	ID   DirectoryID
	// Display is the principal's human name, filled in by ListRoleAssignments
	// when the directory can supply it and IGNORED on every write. It exists so
	// the console can render "Finance" without a second lookup, and so a
	// failure can name the group rather than a uuid — an assignment can outlive
	// the group id it names, because editing a group's membership mints a new
	// id.
	Display string
}

// DesignReader reads the design bundle at a spec tag — keys relative to
// specs/design/, so the security document is `security.json`.
type DesignReader interface {
	GetDesignAtTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error)
}
