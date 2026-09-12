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

package thundersvc

// roles.go — the GRANT half of the Thunder admin surface. A role names a set of
// permissions from the catalog resourceservers.go declares, and is assigned to
// the groups directory.go maintains. That chain — group → role → permissions,
// intersected with the resource server named by the token request's `resource`
// indicator — is the ONLY thing that narrows a generated app's access token.
// (The application's own `scopes` allowlist looks like a second gate and is
// not: ThunderID 1.0.0 stores it, reads it back and never enforces it,
// docs/design/draft/spikes/P1.md §6.)
//
// Five Thunder facts shape this file, all measured against a running ThunderID
// 1.0.0 rather than read from documentation (P1 §2, §3, §7):
//
//  1. **`/` is legal in a role name.** Role names are per-OU, not per-project,
//     so the platform qualifies them — `<project>/<Role>` — and Thunder echoes
//     the name back verbatim. A duplicate is 409 ROL-1004 (ErrRoleNameConflict).
//
//  2. **A permission string must already exist in the catalog.** A role write
//     naming an action that has not been created is 400 ROL-1012
//     (ErrInvalidPermissions), so the catalog pass runs before the role pass.
//
//  3. **PUT /roles/{id} fully REPLACES the permissions and KEEPS the
//     assignments.** Converging a role to what the tag declares is therefore one
//     PUT, with no re-assignment pass afterwards. UpdateRole's signature takes
//     the whole permission set for exactly that reason: there is no "add one
//     permission" call, and pretending otherwise would silently drop the rest.
//
//  4. **Assignments add/remove answer 204**, not 200 and not the 500 an earlier
//     Thunder build did (the `deployments/scripts/setup-local.sh` workaround is
//     stale). Removal has its own endpoint; it is not a DELETE.
//
//  5. **403 SAZ-4030 ("the write would grant permissions the caller does not
//     hold") never fires for this platform's system client**, whose `system`
//     scope on the System resource server bypasses the check. It is still
//     mapped — ErrGrantNotPermitted — so that a Thunder which did enforce it
//     against us would name itself instead of surfacing as an unexplained 403.
//     There is deliberately NO fallback path.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Role is one grant bundle in the directory. Permissions is empty on a role
// that came out of ListRoles — Thunder's listing carries the summary only, and
// GetRole is the call that reads the grants.
type Role struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	OUID        string           `json:"ouId,omitempty"`
	Permissions []RolePermission `json:"permissions,omitempty"`
}

// RolePermission is the permissions a role grants ON ONE resource server.
// Thunder groups them this way because a permission string ("claims:read") is
// only meaningful relative to the server that derived it — the same string
// under two projects' servers is two different grants.
type RolePermission struct {
	ResourceServerID string   `json:"resourceServerId"`
	Permissions      []string `json:"permissions"`
}

// Assignment is one principal holding a role. Display is populated only by
// ListRoleAssignments (Thunder resolves it on request) and is ignored on write.
type Assignment struct {
	Type    AssigneeType `json:"type"`
	ID      string       `json:"id"`
	Display string       `json:"display,omitempty"`
}

// AssigneeType is what kind of principal an assignment names. The platform
// writes three of the four — a group (how a person comes to hold a project
// role), a user (a test account whose role assigns only to a group the platform
// does not own) and an app (a service identity) — but Thunder's model is wider
// and the wire values are fixed by the contract.
//
// The set is written out IN FULL, `agent` included, because this type decodes
// what comes BACK: ListRoleAssignments carries a principal's kind verbatim, and
// a reader of an assignment made by hand on a Thunder console has to be able to
// name what it found. A constant nothing sends is the difference between "a
// kind this client does not model" and "a kind the platform does not write".
type AssigneeType string

const (
	AssigneeUser  AssigneeType = "user"
	AssigneeGroup AssigneeType = "group"
	AssigneeApp   AssigneeType = "app"
	// AssigneeAgent is never written by this platform; it completes the wire
	// enum for the read path above.
	AssigneeAgent AssigneeType = "agent"
)

// -- reading ------------------------------------------------------------------

// ListRoles returns every role in the directory, without their permissions —
// the name → id map the ensure needs before it can converge anything. Roles are
// per-OU and this platform qualifies its own names (`<project>/<Role>`), so the
// listing carries other projects' roles and the platform's own.
func (c *client) ListRoles(ctx context.Context) ([]Role, error) {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("getSystemToken: %w", err)
	}
	return c.listRolesWith(ctx, token)
}

func (c *client) listRolesWith(ctx context.Context, token string) ([]Role, error) {
	return pageAll("roles", func(offset int) ([]Role, int, error) {
		var body struct {
			TotalResults int    `json:"totalResults"`
			Roles        []Role `json:"roles"`
		}
		path := fmt.Sprintf("/roles?limit=%d&offset=%d", directoryPageSize, offset)
		if err := c.doJSON(ctx, token, http.MethodGet, path, nil, &body); err != nil {
			return nil, 0, err
		}
		return body.Roles, body.TotalResults, nil
	})
}

// FindRoleByName returns the role with this name, and whether one exists.
// Thunder has no lookup by name, so this scans the listing the way
// FindGroupByName does, and for the same reason matches case-insensitively:
// the platform treats two role names differing only in case as one role.
func (c *client) FindRoleByName(ctx context.Context, name string) (*Role, bool, error) {
	roles, err := c.ListRoles(ctx)
	if err != nil {
		return nil, false, err
	}
	for i := range roles {
		if strings.EqualFold(roles[i].Name, name) {
			return &roles[i], true, nil
		}
	}
	return nil, false, nil
}

// GetRole reads one role WITH its permissions — the only call that returns
// them. Assignments are not included; ListRoleAssignments is a separate read.
func (c *client) GetRole(ctx context.Context, roleID string) (Role, error) {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return Role{}, fmt.Errorf("getSystemToken: %w", err)
	}
	var role Role
	if err := c.doJSON(ctx, token, http.MethodGet, "/roles/"+url.PathEscape(roleID), nil, &role); err != nil {
		return Role{}, err
	}
	return role, nil
}

// ListRoleAssignments returns the principals holding a role, with their display
// names — `include=display` gives a group's name in the same round trip, which
// is what lets a caller report "role X is assigned to group Y" without a second
// lookup per assignment.
func (c *client) ListRoleAssignments(ctx context.Context, roleID string) ([]Assignment, error) {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("getSystemToken: %w", err)
	}
	return pageAll("role assignments", func(offset int) ([]Assignment, int, error) {
		var body struct {
			TotalResults int          `json:"totalResults"`
			Assignments  []Assignment `json:"assignments"`
		}
		path := fmt.Sprintf("/roles/%s/assignments?include=display&limit=%d&offset=%d",
			url.PathEscape(roleID), directoryPageSize, offset)
		if err := c.doJSON(ctx, token, http.MethodGet, path, nil, &body); err != nil {
			return nil, 0, err
		}
		return body.Assignments, body.TotalResults, nil
	})
}

// -- writing ------------------------------------------------------------------

// CreateRole creates a role in the default OU granting exactly these
// permissions. Every permission string must already exist in the catalog
// (ErrInvalidPermissions otherwise), and the name must be free in the OU
// (ErrRoleNameConflict otherwise).
func (c *client) CreateRole(ctx context.Context, name, description string, permissions []RolePermission) (Role, error) {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return Role{}, fmt.Errorf("getSystemToken: %w", err)
	}
	ou, err := c.getDefaultOUID(ctx, token)
	if err != nil {
		return Role{}, err
	}
	req := map[string]any{
		"name":        name,
		"description": description,
		"ouId":        ou,
		"permissions": nonNilPermissions(permissions),
	}
	var created Role
	if err := c.doJSON(ctx, token, http.MethodPost, "/roles", req, &created); err != nil {
		return Role{}, err
	}
	return created, nil
}

// UpdateRole REPLACES a role's name, description and permission set with what
// is passed. It is a full replacement by design, not by accident: Thunder's PUT
// has no merge semantics, so a caller that sent one permission would revoke the
// others.
//
// The role's ASSIGNMENTS survive — the update request has no assignments field
// and Thunder leaves them alone — so converging a role to its tag is this one
// call, with no re-assignment pass afterwards.
func (c *client) UpdateRole(ctx context.Context, roleID, name, description string, permissions []RolePermission) (Role, error) {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return Role{}, fmt.Errorf("getSystemToken: %w", err)
	}
	ou, err := c.getDefaultOUID(ctx, token)
	if err != nil {
		return Role{}, err
	}
	req := map[string]any{
		"name":        name,
		"description": description,
		"ouId":        ou,
		"permissions": nonNilPermissions(permissions),
	}
	var updated Role
	if err := c.doJSON(ctx, token, http.MethodPut, "/roles/"+url.PathEscape(roleID), req, &updated); err != nil {
		return Role{}, err
	}
	return updated, nil
}

// DeleteRole removes a role. Its assignments go with it; the catalog it granted
// does not. Idempotent — already gone is done.
func (c *client) DeleteRole(ctx context.Context, roleID string) error {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return fmt.Errorf("getSystemToken: %w", err)
	}
	return c.deleteIfPresent(ctx, token, "/roles/"+url.PathEscape(roleID))
}

// AddRoleAssignments gives a role to these principals. Additive: principals
// already holding it stay, and re-adding one is not an error — which is what
// makes the build-time ensure safe to repeat.
func (c *client) AddRoleAssignments(ctx context.Context, roleID string, assignments []Assignment) error {
	return c.writeAssignments(ctx, roleID, "add", assignments)
}

// RemoveRoleAssignments takes a role away from these principals, leaving the
// rest. It is a POST to its own endpoint, not a DELETE on the collection.
func (c *client) RemoveRoleAssignments(ctx context.Context, roleID string, assignments []Assignment) error {
	return c.writeAssignments(ctx, roleID, "remove", assignments)
}

// writeAssignments is the shared body of the two assignment writes — the same
// request shape and the same 204, differing only in the verb in the path.
//
// Nothing to write is nothing to do: an empty list would otherwise send a
// pointless request whose only possible outcome is an error.
func (c *client) writeAssignments(ctx context.Context, roleID, verb string, assignments []Assignment) error {
	if len(assignments) == 0 {
		return nil
	}
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return fmt.Errorf("getSystemToken: %w", err)
	}
	// Display is read-only on Thunder's side; send only what identifies the
	// principal so a value read back from ListRoleAssignments can be handed
	// straight to this call.
	body := make([]map[string]string, 0, len(assignments))
	for _, a := range assignments {
		body = append(body, map[string]string{"type": string(a.Type), "id": a.ID})
	}
	path := "/roles/" + url.PathEscape(roleID) + "/assignments/" + verb
	return c.doJSON(ctx, token, http.MethodPost, path, map[string]any{"assignments": body}, nil)
}

// nonNilPermissions keeps a nil slice from marshalling as JSON null on a field
// Thunder requires. "No permissions" is a legal state — a role declared before
// its catalog exists — and has to go on the wire as [].
func nonNilPermissions(in []RolePermission) []RolePermission {
	if in == nil {
		return []RolePermission{}
	}
	return in
}
