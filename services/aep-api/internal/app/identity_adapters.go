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

package app

// identity_adapters.go — the composition-root mappings for the identity domain.
//
// Three seams, each in one direction:
//
//	thundersvc.Client      → identity.Directory   (one environment's IdP admin
//	                                               surface, built per (org, env)
//	                                               in identity_targets.go)
//	spec.ArtifactService   → identity.DesignReader (security.json at a tag)
//	identity.EnsureService → provisioning.RolesEnsurer (the build gate's driver)
//	identity.CatalogService → mcpdiscovery.GroupCatalogLister (the design-time
//	                                                           `list_groups` tool)
//
// Keeping the mapping here is what lets `identity` name no client package and
// `provisioning` name no identity entity.
//
// There is deliberately NO seam onto validation's CredentialProvider: a
// validation agent reads the test users' logins from the roles gate ticket the
// build published them in, not from a platform callback.

import (
	"context"
	"errors"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
	"github.com/wso2/aep/aep-api/internal/dependencies/mcpdiscovery"
	"github.com/wso2/aep/aep-api/internal/dependencies/provisioning"
	"github.com/wso2/aep/aep-api/internal/identity"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// -- the identity provider ----------------------------------------------------

// thunderDirectory narrows a Thunder admin client to the group/user slice the
// identity domain uses, translating the two wire types.
//
// The client it wraps is per (org, environment): identity_targets.go builds one
// against that environment's own Thunder from the binding on its OpenChoreo
// Environment. This type is the translation only, and knows nothing about which
// instance it is talking to.
type thunderDirectory struct{ c thundersvc.Client }

func toDirectoryGroup(g thundersvc.Group) identity.DirectoryGroup {
	return identity.DirectoryGroup{ID: g.ID, Name: g.Name, Description: g.Description, OUID: g.OUID}
}

func toThunderGroup(g identity.DirectoryGroup) thundersvc.Group {
	return thundersvc.Group{ID: g.ID, Name: g.Name, Description: g.Description, OUID: g.OUID}
}

func (d thunderDirectory) ListGroups(ctx context.Context) ([]identity.DirectoryGroup, error) {
	groups, err := d.c.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]identity.DirectoryGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, toDirectoryGroup(g))
	}
	return out, nil
}

func (d thunderDirectory) FindGroupByName(ctx context.Context, name string) (*identity.DirectoryGroup, bool, error) {
	g, found, err := d.c.FindGroupByName(ctx, name)
	if err != nil || !found {
		return nil, found, err
	}
	out := toDirectoryGroup(*g)
	return &out, true, nil
}

func (d thunderDirectory) GroupMembers(ctx context.Context, groupID string) ([]string, error) {
	return d.c.GroupMembers(ctx, groupID)
}

func (d thunderDirectory) CreateGroup(ctx context.Context, name, description string, memberIDs []string) (identity.DirectoryGroup, error) {
	g, err := d.c.CreateGroup(ctx, name, description, memberIDs)
	if err != nil {
		return identity.DirectoryGroup{}, err
	}
	return toDirectoryGroup(g), nil
}

func (d thunderDirectory) AddMembers(ctx context.Context, group identity.DirectoryGroup, memberIDs []string) (identity.DirectoryGroup, error) {
	g, err := d.c.AddGroupMembers(ctx, toThunderGroup(group), memberIDs)
	if err != nil {
		return identity.DirectoryGroup{}, err
	}
	return toDirectoryGroup(g), nil
}

func (d thunderDirectory) RemoveMembers(ctx context.Context, group identity.DirectoryGroup, memberIDs []string) (identity.DirectoryGroup, error) {
	g, err := d.c.RemoveGroupMembers(ctx, toThunderGroup(group), memberIDs)
	if err != nil {
		return identity.DirectoryGroup{}, err
	}
	return toDirectoryGroup(g), nil
}

func (d thunderDirectory) UserGroups(ctx context.Context, userID string) ([]identity.DirectoryGroup, error) {
	groups, err := d.c.UserGroups(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]identity.DirectoryGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, toDirectoryGroup(g))
	}
	return out, nil
}

func (d thunderDirectory) FindUserByUsername(ctx context.Context, username string) (*identity.DirectoryAccount, bool, error) {
	u, found, err := d.c.FindUserByUsername(ctx, username)
	if err != nil || !found {
		return nil, found, err
	}
	return &identity.DirectoryAccount{ID: u.ID, Username: u.Username, Email: u.Email}, true, nil
}

func (d thunderDirectory) CreateUser(ctx context.Context, username, email, password string) (identity.DirectoryAccount, error) {
	u, err := d.c.CreateUser(ctx, username, email, password)
	if err != nil {
		return identity.DirectoryAccount{}, err
	}
	return identity.DirectoryAccount{ID: u.ID, Username: u.Username, Email: u.Email}, nil
}

func (d thunderDirectory) SetUserPassword(ctx context.Context, userID, password string) error {
	return d.c.SetUserPassword(ctx, userID, password)
}

func (d thunderDirectory) DeleteUser(ctx context.Context, userID string) error {
	return d.c.DeleteUser(ctx, userID)
}

// -- the design read ----------------------------------------------------------

// identityDesignReader gives the ensure the design bundle at a spec tag, which
// is where it finds `security.json`. Reading at the TAG rather than at HEAD is the
// point: the build provisions what the version it is building declares, not
// what somebody has edited since.
//
// The port's method name is GetDesignAtTag; the implementation is deliberately
// GetDesignAtSpecTag. A build knows only the `v<N>` spec tag, and the
// similarly-named GetDesignAtTag next door parses its argument as a legacy
// `v<N>-<M>` design-revision tag and refuses a spec tag outright.
type identityDesignReader struct{ art spec.ArtifactService }

func (r identityDesignReader) GetDesignAtTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error) {
	return r.art.GetDesignAtSpecTag(ctx, orgID, projectID, tag)
}

// -- the build gate's driver --------------------------------------------------

// rolesEnsurer maps the identity ensure onto provisioning's port, flattening
// identity's Result into the summary + refusal flag the gate needs. The
// flattening is what keeps `provisioning` from naming an identity entity.
type rolesEnsurer struct{ svc *identity.EnsureService }

func (e rolesEnsurer) Enabled() bool { return e.svc.Enabled() }

// rolesEnsurerOrNil hands provisioning a TYPED NIL-safe port, or a true nil.
//
// It exists because `provisioning.Deps.Roles` is an interface: assigning a nil
// *EnsureService to it would produce a non-nil interface holding a nil pointer,
// and `s.roles == nil` in the gate would be false. Enabled() would then be the
// only guard, and one forgotten check would panic a build. Returning an
// untyped nil keeps the "not wired" case genuinely nil.
func rolesEnsurerOrNil(svc *identity.EnsureService) provisioning.RolesEnsurer {
	if svc == nil {
		return nil
	}
	return rolesEnsurer{svc: svc}
}

func (e rolesEnsurer) DeclaresRoles(ctx context.Context, orgID, projectID, tag string) (bool, error) {
	return e.svc.DeclaresRoles(ctx, orgID, projectID, tag)
}

func (e rolesEnsurer) EnsureRolesForBuild(ctx context.Context, orgID, projectID, tag string) (provisioning.RolesEnsureOutcome, error) {
	// EnsureForTag also reports whether the design declared roles at all; the
	// gate already asked that (DeclaresRoles) before it got here, so carrying a
	// second answer across this seam would only let the two disagree.
	result, _, err := e.svc.EnsureForTag(ctx, orgID, projectID, tag)
	return provisioning.RolesEnsureOutcome{
		Summary:            result.Summary(),
		Refusals:           result.HasRefusals(),
		Credentials:        toGateCredentials(result.Credentials),
		Issuer:             result.Issuer,
		Environment:        result.Environment,
		ResourceIdentifier: result.ResourceIdentifier,
	}, err
}

// toGateCredentials projects the identity domain's logins onto the provisioning
// port's own type — the same one-way projection every other seam in this file
// makes, and the reason `provisioning` names no identity entity.
func toGateCredentials(creds []identity.Credential) []provisioning.RolesCredential {
	if len(creds) == 0 {
		return nil
	}
	out := make([]provisioning.RolesCredential, 0, len(creds))
	for _, c := range creds {
		out = append(out, provisioning.RolesCredential{
			Username: c.Username, Password: c.Password,
			Roles: c.Roles, Scopes: c.Scopes, ColdStart: c.ColdStart,
		})
	}
	return out
}

// -- the design-time group catalog --------------------------------------------

// groupCatalog maps the identity catalog onto the MCP discovery port, projecting
// through mcpdiscovery's own view type. Every other tool on that surface goes
// through a view projection for the same reason: a field added to a domain
// struct must not be able to reach an LLM prompt by accident.
type groupCatalog struct{ svc *identity.CatalogService }

func (c groupCatalog) ListGroupCatalog(ctx context.Context, orgHandle string) ([]mcpdiscovery.GroupCatalogEntry, error) {
	entries, err := c.svc.List(ctx, orgHandle)
	if err != nil {
		return nil, err
	}
	out := make([]mcpdiscovery.GroupCatalogEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, mcpdiscovery.GroupCatalogEntry{
			Name:            e.Name,
			Description:     e.Description,
			PlatformCreated: e.PlatformCreated,
			MemberCount:     e.MemberCount,
			Projects:        e.Projects,
		})
	}
	return out, nil
}

// groupCatalogOrNil keeps "not wired" a genuine nil rather than an interface
// holding a nil pointer — same reason as rolesEnsurerOrNil.
func groupCatalogOrNil(svc *identity.CatalogService) mcpdiscovery.GroupCatalogLister {
	if svc == nil {
		return nil
	}
	return groupCatalog{svc: svc}
}

// -- the identity provider: the project's authorization objects ---------------
//
// The second half of the Directory map, and the one with decisions in it.
// Everything above translates a type; the verbs below also translate BEHAVIOUR,
// because the port promises two things the wire does not offer:
//
//   - `Ensure…` writes nothing when the directory already matches. Thunder has
//     no upsert, so each is find-then-compare-then-write here. That is what lets
//     a rebuild of an unchanged tag leave the directory untouched.
//   - DeleteResourceServer removes a whole tree. Thunder refuses a parent that
//     still has children, so the ORDER — actions, resources, resource server —
//     is owned by the adapter (thundersvc.DeleteResourceServerCascade).
//
// The converge itself is not here: which resources, actions and roles to create
// and which to delete lives in identity/ensure.go, where it can be read against
// the plan. This file only makes each primitive mean what the port says.

// mapDirectoryError translates the client's conflict sentinels onto the identity
// domain's, so no caller above this line reads a Thunder error code or a wire
// status. The client's error is kept in the message (not in the chain) because
// the domain's sentinel is the one callers branch on and a chain carrying both
// would let a caller match the vendor's by accident.
//
// Everything unmapped passes through unchanged: it is an error the domain has no
// decision for, and inventing a sentinel for it would hide what happened.
func mapDirectoryError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, thundersvc.ErrIdentifierConflict):
		return fmt.Errorf("%w: %v", identity.ErrIdentifierConflict, err)
	case errors.Is(err, thundersvc.ErrHandleConflict):
		return fmt.Errorf("%w: %v", identity.ErrHandleConflict, err)
	}
	// Not a conflict the domain decides on, so it travels as-is — but with the
	// directory's own error code in FRONT of it when it has one. These failures
	// end up in a gate ticket a human reads, where the code is the one part that
	// can be looked up; unprefixed, it is a four-line JSON body whose first
	// field is the only interesting one.
	if code := thundersvc.ErrorCode(err); code != "" {
		return fmt.Errorf("thunder %s: %w", code, err)
	}
	return err
}

// EnsureResourceServer finds the project's resource server by identifier and
// creates it only when it is absent. `name` seeds the label at create and is
// never converged — the identifier is the identity.
func (d thunderDirectory) EnsureResourceServer(ctx context.Context, identifier, name string) (identity.DirectoryID, error) {
	existing, found, err := d.c.FindResourceServerByIdentifier(ctx, identifier)
	if err != nil {
		return "", mapDirectoryError(err)
	}
	if found {
		return identity.DirectoryID(existing.ID), nil
	}
	created, err := d.c.CreateResourceServer(ctx, name, identifier)
	if err != nil {
		return "", mapDirectoryError(err)
	}
	return identity.DirectoryID(created.ID), nil
}

// FindResourceServer is EnsureResourceServer's read half on its own, for a
// caller that must not create one.
func (d thunderDirectory) FindResourceServer(ctx context.Context, identifier string) (identity.DirectoryID, bool, error) {
	existing, found, err := d.c.FindResourceServerByIdentifier(ctx, identifier)
	if err != nil || !found {
		return "", found, mapDirectoryError(err)
	}
	return identity.DirectoryID(existing.ID), true, nil
}

func (d thunderDirectory) ListResources(ctx context.Context, rs identity.DirectoryID) ([]identity.DirectoryResource, error) {
	resources, err := d.c.ListResources(ctx, string(rs))
	if err != nil {
		return nil, mapDirectoryError(err)
	}
	out := make([]identity.DirectoryResource, 0, len(resources))
	for _, r := range resources {
		out = append(out, identity.DirectoryResource{
			ID: identity.DirectoryID(r.ID), Handle: r.Handle, Name: r.Name, Description: r.Description,
		})
	}
	return out, nil
}

func (d thunderDirectory) CreateResource(ctx context.Context, rs identity.DirectoryID, handle, name, description string) (identity.DirectoryResource, error) {
	created, err := d.c.CreateResource(ctx, string(rs), handle, name, description)
	if err != nil {
		return identity.DirectoryResource{}, mapDirectoryError(err)
	}
	return identity.DirectoryResource{
		ID: identity.DirectoryID(created.ID), Handle: created.Handle, Name: created.Name, Description: created.Description,
	}, nil
}

// UpdateResource carries a corrected description to the directory without
// touching the handle. See the port: the alternative is a delete that cascades
// out of every role granting the resource's actions.
func (d thunderDirectory) UpdateResource(ctx context.Context, rs, resource identity.DirectoryID, name, description string) error {
	_, err := d.c.UpdateResource(ctx, string(rs), string(resource), name, description)
	return mapDirectoryError(err)
}

func (d thunderDirectory) DeleteResource(ctx context.Context, rs, resource identity.DirectoryID) error {
	return mapDirectoryError(d.c.DeleteResource(ctx, string(rs), string(resource)))
}

func (d thunderDirectory) ListActions(ctx context.Context, rs, resource identity.DirectoryID) ([]identity.DirectoryAction, error) {
	actions, err := d.c.ListActions(ctx, string(rs), string(resource))
	if err != nil {
		return nil, mapDirectoryError(err)
	}
	out := make([]identity.DirectoryAction, 0, len(actions))
	for _, a := range actions {
		out = append(out, identity.DirectoryAction{
			ID: identity.DirectoryID(a.ID), Handle: a.Handle, Name: a.Name, Description: a.Description,
		})
	}
	return out, nil
}

func (d thunderDirectory) CreateAction(ctx context.Context, rs, resource identity.DirectoryID, handle, name, description string) (identity.DirectoryAction, error) {
	created, err := d.c.CreateAction(ctx, string(rs), string(resource), handle, name, description)
	if err != nil {
		return identity.DirectoryAction{}, mapDirectoryError(err)
	}
	return identity.DirectoryAction{
		ID: identity.DirectoryID(created.ID), Handle: created.Handle, Name: created.Name, Description: created.Description,
	}, nil
}

// UpdateAction is UpdateResource one level down.
func (d thunderDirectory) UpdateAction(ctx context.Context, rs, resource, action identity.DirectoryID, name, description string) error {
	_, err := d.c.UpdateAction(ctx, string(rs), string(resource), string(action), name, description)
	return mapDirectoryError(err)
}

func (d thunderDirectory) DeleteAction(ctx context.Context, rs, resource, action identity.DirectoryID) error {
	return mapDirectoryError(d.c.DeleteAction(ctx, string(rs), string(resource), string(action)))
}

// DeleteResourceServer is the CASCADE, and the order is the whole content of
// this method: every action, then every resource, then the server. Thunder has
// no cascade of its own and answers 400 RES-1006 for a parent that still has
// children (spike P1 §7).
func (d thunderDirectory) DeleteResourceServer(ctx context.Context, rs identity.DirectoryID) error {
	return mapDirectoryError(d.c.DeleteResourceServerCascade(ctx, string(rs)))
}

// EnsureRole makes the role's grants match, writing nothing when they already
// do.
//
// Three outcomes: no id → create; present and different → one PUT that replaces
// the permission set wholesale and LEAVES THE ASSIGNMENTS (P1 §7), which is why
// there is no re-assignment pass; present and equal → no write at all.
//
// The id comes from the caller, which listed the directory's roles before it
// converged anything (see the port): looking the name up again here would be one
// full listing per declared role for an answer already in hand. The comparison
// still needs GetRole — the listing carries the summary only, so it can say a
// role exists but never what it grants.
func (d thunderDirectory) EnsureRole(ctx context.Context, id identity.DirectoryID, name, description string, rs identity.DirectoryID, permissions []string) (identity.DirectoryID, error) {
	block := rolePermissionBlock(string(rs), permissions)
	if id == "" {
		created, cerr := d.c.CreateRole(ctx, name, description, block)
		if cerr != nil {
			return "", mapDirectoryError(cerr)
		}
		return identity.DirectoryID(created.ID), nil
	}
	current, err := d.c.GetRole(ctx, string(id))
	if err != nil {
		return "", mapDirectoryError(err)
	}
	if current.Description == description && grantsMatch(current, string(rs), permissions) {
		return id, nil
	}
	if _, uerr := d.c.UpdateRole(ctx, string(id), name, description, block); uerr != nil {
		return "", mapDirectoryError(uerr)
	}
	return id, nil
}

// rolePermissionBlock wraps the handles as the one permission block a project
// role carries: every grant of a `<project>/<Role>` is on that project's own
// resource server. A role granting nothing carries NO block rather than an empty
// one, so "grants nothing" and "grants nothing on this server" are the same
// object on the wire.
func rolePermissionBlock(rsID string, permissions []string) []thundersvc.RolePermission {
	if len(permissions) == 0 {
		return nil
	}
	return []thundersvc.RolePermission{{ResourceServerID: rsID, Permissions: permissions}}
}

// grantsMatch reports whether the role already grants exactly these handles, and
// grants them on this resource server ALONE.
//
// A block on another resource server makes it false even when the handles line
// up: the role would be carrying a grant this project's design does not declare,
// and a full replace is the only way this port can say so. Order is not part of
// it — the directory stores a set, and rewriting a role because the document
// reordered its grants would churn for nothing.
func grantsMatch(role thundersvc.Role, rsID string, want []string) bool {
	held := map[string]bool{}
	for _, block := range role.Permissions {
		if block.ResourceServerID != rsID {
			return false
		}
		for _, p := range block.Permissions {
			held[p] = true
		}
	}
	wanted := make(map[string]bool, len(want))
	for _, p := range want {
		wanted[p] = true
	}
	if len(held) != len(wanted) {
		return false
	}
	for p := range wanted {
		if !held[p] {
			return false
		}
	}
	return true
}

func (d thunderDirectory) ListRoles(ctx context.Context) ([]identity.RoleRef, error) {
	roles, err := d.c.ListRoles(ctx)
	if err != nil {
		return nil, mapDirectoryError(err)
	}
	out := make([]identity.RoleRef, 0, len(roles))
	for _, r := range roles {
		out = append(out, identity.RoleRef{ID: identity.DirectoryID(r.ID), Name: r.Name})
	}
	return out, nil
}

// ListRolePermissions flattens the role's permission blocks into the handles it
// grants. The blocks are per resource server; a project role holds exactly one,
// but flattening them all is what keeps this a truthful read of a role the
// platform did not create.
func (d thunderDirectory) ListRolePermissions(ctx context.Context, role identity.DirectoryID) ([]string, error) {
	current, err := d.c.GetRole(ctx, string(role))
	if err != nil {
		return nil, mapDirectoryError(err)
	}
	var out []string
	for _, block := range current.Permissions {
		out = append(out, block.Permissions...)
	}
	return out, nil
}

func (d thunderDirectory) DeleteRole(ctx context.Context, role identity.DirectoryID) error {
	return mapDirectoryError(d.c.DeleteRole(ctx, string(role)))
}

func (d thunderDirectory) AssignRole(ctx context.Context, role identity.DirectoryID, principal identity.Principal) error {
	assignment, err := toAssignment(principal)
	if err != nil {
		return err
	}
	return mapDirectoryError(d.c.AddRoleAssignments(ctx, string(role), []thundersvc.Assignment{assignment}))
}

func (d thunderDirectory) UnassignRole(ctx context.Context, role identity.DirectoryID, principal identity.Principal) error {
	assignment, err := toAssignment(principal)
	if err != nil {
		return err
	}
	return mapDirectoryError(d.c.RemoveRoleAssignments(ctx, string(role), []thundersvc.Assignment{assignment}))
}

func (d thunderDirectory) ListRoleAssignments(ctx context.Context, role identity.DirectoryID) ([]identity.Principal, error) {
	assignments, err := d.c.ListRoleAssignments(ctx, string(role))
	if err != nil {
		return nil, mapDirectoryError(err)
	}
	out := make([]identity.Principal, 0, len(assignments))
	for _, a := range assignments {
		out = append(out, identity.Principal{
			// The kind is carried through VERBATIM, including one the domain has
			// no constant for (Thunder also assigns to `agent`). A converge only
			// ever touches the kinds it declares, so an unknown one survives by
			// being unrecognised rather than by being special-cased.
			Kind: identity.PrincipalKind(a.Type), ID: identity.DirectoryID(a.ID), Display: a.Display,
		})
	}
	return out, nil
}

// toAssignment maps a principal onto the wire's assignee type. An unknown kind
// is refused rather than sent: the directory would answer a validation error the
// caller cannot act on, and a role assignment is a grant.
func toAssignment(p identity.Principal) (thundersvc.Assignment, error) {
	switch p.Kind {
	case identity.PrincipalGroup:
		return thundersvc.Assignment{Type: thundersvc.AssigneeGroup, ID: string(p.ID)}, nil
	case identity.PrincipalUser:
		return thundersvc.Assignment{Type: thundersvc.AssigneeUser, ID: string(p.ID)}, nil
	case identity.PrincipalApp:
		return thundersvc.Assignment{Type: thundersvc.AssigneeApp, ID: string(p.ID)}, nil
	}
	return thundersvc.Assignment{}, fmt.Errorf("identity adapter: unknown principal kind %q", p.Kind)
}
