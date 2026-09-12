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

// resourceservers.go — the permission CATALOG half of the Thunder admin
// surface: one resource server per project, its resources, and their actions.
// roles.go grants what this file declares; directory.go decides who holds a
// role. Everything lands in the DEFAULT organization unit, like the rest of the
// client.
//
// The shape, and why it matters to the generated app:
//
//	resource server   identifier = an absolute URI = the access token's `aud`
//	  resource        handle "claims"        → permission "claims"
//	    action        handle "read"          → permission "claims:read"
//	    action        handle "submit"        → permission "claims:submit"
//
// Thunder DERIVES the permission string from the handle hierarchy and the
// resource server's delimiter; the platform never writes a permission string
// into the catalog, it writes handles and reads back what Thunder derived. That
// is why the delimiter is pinned to ":" at create time (it is immutable
// afterwards) — the whole contract, the generated SPA's scope list and the
// gateway's per-operation policies all spell scopes `resource:action`.
//
// Four Thunder facts shape this file, all measured against a running ThunderID
// 1.0.0 rather than read from documentation (docs/design/draft/spikes/P1.md):
//
//  1. **Handle uniqueness is per PARENT, not per resource server.** `read`
//     exists under both `claims` and `reports`; a second `read` under `claims`
//     is 409 RES-1014 (ErrHandleConflict).
//
//  2. **The identifier is the audience and is globally unique.** A second
//     resource server claiming it is 409 RES-1013 (ErrIdentifierConflict).
//     There is no find-by-identifier endpoint, so FindResourceServerByIdentifier
//     scans the listing — the same shape as FindGroupByName.
//
//  3. **Delete is leaf-first and the error says nothing about which leaf.**
//     Deleting a resource server that still has resources, or a resource that
//     still has actions, is 400 RES-1006 (ErrHasDependencies).
//     DeleteResourceServerCascade walks the tree in the one order that works:
//     every action, then every resource, then the resource server. Roles are
//     NOT a dependency — deleting them first does not unblock anything.
//
//  4. **Deleting an action cascades OUT of every role that grants it**, 204,
//     silently. Nothing has to strip a role's permissions before a catalog edit
//     — but nothing may cache a role's grants across one either.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// ResourceServer is one project's permission namespace. Identifier is the
// absolute URI clients send as the `resource` indicator and that Thunder puts
// in the access token's `aud`; ID is Thunder's own uuid, which is what a role's
// permission block references.
type ResourceServer struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Identifier  string `json:"identifier"`
	Type        string `json:"type,omitempty"`
	OUID        string `json:"ouId,omitempty"`
	Delimiter   string `json:"delimiter,omitempty"`
	IsReadOnly  bool   `json:"isReadOnly,omitempty"`
}

// Resource is one noun in the catalog ("claims"). Permission is what Thunder
// derived from the handle — read it, never compose it.
type Resource struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Handle      string `json:"handle"`
	Description string `json:"description,omitempty"`
	Parent      string `json:"parent,omitempty"`
	Permission  string `json:"permission"`
}

// Action is one verb under a resource ("read"). Permission is the full scope
// handle Thunder derived — "claims:read" — and is the string that appears in a
// role's permission list, in the token's `scope` claim and in the gateway's
// per-operation policy.
type Action struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Handle      string `json:"handle"`
	Description string `json:"description,omitempty"`
	Permission  string `json:"permission"`
}

// resourceServerType is the metadata-only `type` every resource server this
// platform creates carries. It changes no behaviour (the audience-restriction
// comes from the identifier being an absolute URI) but it is immutable after
// creation, so it is set once here rather than left to Thunder's CUSTOM default.
const resourceServerType = "API"

// permissionDelimiter separates the levels of a derived permission. Pinned at
// create time because Thunder will not change it afterwards, and because the
// whole platform — contract, generated SPA, gateway policy — spells a scope
// `resource:action`.
const permissionDelimiter = ":"

// -- resource servers ---------------------------------------------------------

// FindResourceServerByIdentifier returns the resource server claiming this
// identifier, and whether one exists.
//
// Thunder offers no lookup by identifier, so this scans the listing the way
// FindGroupByName does. The comparison is EXACT: the identifier is a URI that
// ends up verbatim in a token's `aud` and in every client's `resource`
// parameter, so two identifiers differing in case are two different audiences,
// not one.
func (c *client) FindResourceServerByIdentifier(ctx context.Context, identifier string) (*ResourceServer, bool, error) {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("getSystemToken: %w", err)
	}
	servers, err := c.listResourceServersWith(ctx, token)
	if err != nil {
		return nil, false, err
	}
	for i := range servers {
		if servers[i].Identifier == identifier {
			return &servers[i], true, nil
		}
	}
	return nil, false, nil
}

// listResourceServersWith pages every resource server visible to the system
// client.
func (c *client) listResourceServersWith(ctx context.Context, token string) ([]ResourceServer, error) {
	return pageAll("resource servers", func(offset int) ([]ResourceServer, int, error) {
		var body struct {
			TotalResults    int              `json:"totalResults"`
			ResourceServers []ResourceServer `json:"resourceServers"`
		}
		path := fmt.Sprintf("/resource-servers?limit=%d&offset=%d", directoryPageSize, offset)
		if err := c.doJSON(ctx, token, http.MethodGet, path, nil, &body); err != nil {
			return nil, 0, err
		}
		return body.ResourceServers, body.TotalResults, nil
	})
}

// CreateResourceServer registers a project's permission namespace in the
// default OU. A second server claiming the same identifier fails with
// ErrIdentifierConflict, a second one claiming the same name with
// ErrNameConflict — both are 409s, which is why the caller branches on the
// sentinel rather than the status.
func (c *client) CreateResourceServer(ctx context.Context, name, identifier string) (ResourceServer, error) {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return ResourceServer{}, fmt.Errorf("getSystemToken: %w", err)
	}
	ou, err := c.getDefaultOUID(ctx, token)
	if err != nil {
		return ResourceServer{}, err
	}
	req := map[string]any{
		"name":       name,
		"identifier": identifier,
		"type":       resourceServerType,
		"ouId":       ou,
		"delimiter":  permissionDelimiter,
	}
	var created ResourceServer
	if err := c.doJSON(ctx, token, http.MethodPost, "/resource-servers", req, &created); err != nil {
		return ResourceServer{}, err
	}
	return created, nil
}

// DeleteResourceServer removes an EMPTY resource server. A server that still
// has resources fails with ErrHasDependencies — use DeleteResourceServerCascade
// unless the tree is known to be empty. Idempotent: already gone is done.
func (c *client) DeleteResourceServer(ctx context.Context, rsID string) error {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return fmt.Errorf("getSystemToken: %w", err)
	}
	return c.deleteIfPresent(ctx, token, "/resource-servers/"+url.PathEscape(rsID))
}

// DeleteResourceServerCascade removes a resource server and everything under
// it, in the only order Thunder accepts: every action, then every resource,
// then the server itself (P1 §7). Each layer is deleted in full before the next
// one starts — a depth-by-depth walk, not a per-branch one — because that is
// the sequence the cascade was measured in.
//
// It walks the catalog the platform writes, which is one level deep (resources
// carrying actions). A NESTED resource, which nothing here creates, would not
// be reached by the resource pass and its parent's delete would answer
// ErrHasDependencies — surfaced, not swallowed.
//
// Roles are not part of this: they are not a dependency of the server, and
// deleting an action already cascades out of every role granting it.
func (c *client) DeleteResourceServerCascade(ctx context.Context, rsID string) error {
	resources, err := c.ListResources(ctx, rsID)
	if err != nil {
		return fmt.Errorf("cascade delete resource server: list resources: %w", err)
	}
	for _, res := range resources {
		actions, err := c.ListActions(ctx, rsID, res.ID)
		if err != nil {
			return fmt.Errorf("cascade delete resource server: list actions of %q: %w", res.Handle, err)
		}
		for _, act := range actions {
			if err := c.DeleteAction(ctx, rsID, res.ID, act.ID); err != nil {
				return fmt.Errorf("cascade delete resource server: delete action %q: %w", act.Handle, err)
			}
		}
	}
	for _, res := range resources {
		if err := c.DeleteResource(ctx, rsID, res.ID); err != nil {
			return fmt.Errorf("cascade delete resource server: delete resource %q: %w", res.Handle, err)
		}
	}
	if err := c.DeleteResourceServer(ctx, rsID); err != nil {
		return fmt.Errorf("cascade delete resource server: %w", err)
	}
	return nil
}

// -- resources ----------------------------------------------------------------

// ListResources returns the TOP-LEVEL resources of a resource server — the only
// level this platform writes. Thunder pages children separately, behind a
// parentId filter this client does not use.
func (c *client) ListResources(ctx context.Context, rsID string) ([]Resource, error) {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("getSystemToken: %w", err)
	}
	return pageAll("resources", func(offset int) ([]Resource, int, error) {
		var body struct {
			TotalResults int        `json:"totalResults"`
			Resources    []Resource `json:"resources"`
		}
		path := fmt.Sprintf("/resource-servers/%s/resources?limit=%d&offset=%d",
			url.PathEscape(rsID), directoryPageSize, offset)
		if err := c.doJSON(ctx, token, http.MethodGet, path, nil, &body); err != nil {
			return nil, 0, err
		}
		return body.Resources, body.TotalResults, nil
	})
}

// CreateResource adds a top-level resource. The handle is what the permission
// is derived from and is immutable; the name is display only. A handle already
// used under this server fails with ErrHandleConflict.
func (c *client) CreateResource(ctx context.Context, rsID, handle, name, description string) (Resource, error) {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return Resource{}, fmt.Errorf("getSystemToken: %w", err)
	}
	req := map[string]any{"handle": handle, "name": name, "description": description}
	var created Resource
	path := "/resource-servers/" + url.PathEscape(rsID) + "/resources"
	if err := c.doJSON(ctx, token, http.MethodPost, path, req, &created); err != nil {
		return Resource{}, err
	}
	return created, nil
}

// UpdateResource rewrites a resource's display name and description.
//
// Only those two are mutable: the handle and the parent are fixed at creation,
// because the handle is what the permission is derived from. That is exactly
// why this exists — without it a corrected description would have to be a
// delete-and-create, which would take the resource's actions with it and, by
// cascade, the permissions every role granted from them.
func (c *client) UpdateResource(ctx context.Context, rsID, resourceID, name, description string) (Resource, error) {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return Resource{}, fmt.Errorf("getSystemToken: %w", err)
	}
	req := map[string]any{"name": name, "description": description}
	var updated Resource
	path := "/resource-servers/" + url.PathEscape(rsID) + "/resources/" + url.PathEscape(resourceID)
	if err := c.doJSON(ctx, token, http.MethodPut, path, req, &updated); err != nil {
		return Resource{}, err
	}
	return updated, nil
}

// DeleteResource removes a resource that has no actions left; one that still
// has them fails with ErrHasDependencies. Idempotent.
func (c *client) DeleteResource(ctx context.Context, rsID, resourceID string) error {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return fmt.Errorf("getSystemToken: %w", err)
	}
	return c.deleteIfPresent(ctx, token,
		"/resource-servers/"+url.PathEscape(rsID)+"/resources/"+url.PathEscape(resourceID))
}

// -- actions ------------------------------------------------------------------

// ListActions returns the actions under one resource. Their Permission fields
// are the scope handles the rest of the platform spells `resource:action`.
func (c *client) ListActions(ctx context.Context, rsID, resourceID string) ([]Action, error) {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("getSystemToken: %w", err)
	}
	return pageAll("actions", func(offset int) ([]Action, int, error) {
		var body struct {
			TotalResults int      `json:"totalResults"`
			Actions      []Action `json:"actions"`
		}
		path := fmt.Sprintf("/resource-servers/%s/resources/%s/actions?limit=%d&offset=%d",
			url.PathEscape(rsID), url.PathEscape(resourceID), directoryPageSize, offset)
		if err := c.doJSON(ctx, token, http.MethodGet, path, nil, &body); err != nil {
			return nil, 0, err
		}
		return body.Actions, body.TotalResults, nil
	})
}

// CreateAction adds a verb under a resource. Uniqueness is per PARENT: the same
// handle under a different resource is fine, the same handle under this one is
// ErrHandleConflict.
func (c *client) CreateAction(ctx context.Context, rsID, resourceID, handle, name, description string) (Action, error) {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return Action{}, fmt.Errorf("getSystemToken: %w", err)
	}
	req := map[string]any{"handle": handle, "name": name, "description": description}
	var created Action
	path := "/resource-servers/" + url.PathEscape(rsID) + "/resources/" + url.PathEscape(resourceID) + "/actions"
	if err := c.doJSON(ctx, token, http.MethodPost, path, req, &created); err != nil {
		return Action{}, err
	}
	return created, nil
}

// UpdateAction rewrites an action's display name and description, the only two
// mutable fields — see UpdateResource for why the handle is not one, and why a
// description change must never become a delete.
//
// `kind` is deliberately absent from the body: Thunder fixes it at creation and
// refuses it on an update.
func (c *client) UpdateAction(ctx context.Context, rsID, resourceID, actionID, name, description string) (Action, error) {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return Action{}, fmt.Errorf("getSystemToken: %w", err)
	}
	req := map[string]any{"name": name, "description": description}
	var updated Action
	path := "/resource-servers/" + url.PathEscape(rsID) +
		"/resources/" + url.PathEscape(resourceID) +
		"/actions/" + url.PathEscape(actionID)
	if err := c.doJSON(ctx, token, http.MethodPut, path, req, &updated); err != nil {
		return Action{}, err
	}
	return updated, nil
}

// DeleteAction removes a verb. Thunder cascades the removal out of every role
// that granted it, with no error and no 409. Idempotent.
func (c *client) DeleteAction(ctx context.Context, rsID, resourceID, actionID string) error {
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return fmt.Errorf("getSystemToken: %w", err)
	}
	return c.deleteIfPresent(ctx, token,
		"/resource-servers/"+url.PathEscape(rsID)+
			"/resources/"+url.PathEscape(resourceID)+
			"/actions/"+url.PathEscape(actionID))
}
