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
//	thundersvc.Client      → identity.Directory   (the IdP admin surface)
//	spec.ArtifactService   → identity.DesignReader (roles.json at a tag)
//	identity.EnsureService → provisioning.RolesEnsurer (the build gate's driver)
//	identity.CatalogService → mcpdiscovery.RoleCatalogLister (the design-time
//	                                                          `list_roles` tool)
//
// Keeping the mapping here is what lets `identity` name no client package and
// `provisioning` name no identity entity.
//
// There is deliberately NO seam onto validation's CredentialProvider: a
// validation agent reads the test users' logins from the roles gate ticket the
// build published them in, not from a platform callback.

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
	"github.com/wso2/aep/aep-api/internal/dependencies/mcpdiscovery"
	"github.com/wso2/aep/aep-api/internal/dependencies/provisioning"
	"github.com/wso2/aep/aep-api/internal/identity"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// -- the identity provider ----------------------------------------------------

// thunderDirectory narrows the Thunder admin client to the group/user slice the
// identity domain uses, translating the two wire types.
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
// is where it finds `roles.json`. Reading at the TAG rather than at HEAD is the
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
		Summary:     result.Summary(),
		Refusals:    result.HasRefusals(),
		Credentials: toGateCredentials(result.Credentials),
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
			Username: c.Username, Password: c.Password, Role: c.Role, ColdStart: c.ColdStart,
		})
	}
	return out
}

// -- the design-time role catalog ---------------------------------------------

// roleCatalog maps the identity catalog onto the MCP discovery port, projecting
// through mcpdiscovery's own view type. Every other tool on that surface goes
// through a view projection for the same reason: a field added to a domain
// struct must not be able to reach an LLM prompt by accident.
type roleCatalog struct{ svc *identity.CatalogService }

func (c roleCatalog) ListRoleCatalog(ctx context.Context) ([]mcpdiscovery.RoleCatalogEntry, error) {
	entries, err := c.svc.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]mcpdiscovery.RoleCatalogEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, mcpdiscovery.RoleCatalogEntry{
			Name:            e.Name,
			Description:     e.Description,
			PlatformCreated: e.PlatformCreated,
			MemberCount:     e.MemberCount,
		})
	}
	return out, nil
}

// roleCatalogOrNil keeps "not wired" a genuine nil rather than an interface
// holding a nil pointer — same reason as rolesEnsurerOrNil.
func roleCatalogOrNil(svc *identity.CatalogService) mcpdiscovery.RoleCatalogLister {
	if svc == nil {
		return nil
	}
	return roleCatalog{svc: svc}
}
