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

package thunder

// AE permission provisioning: the `ae` resource server and its actions, the
// ae-admin/ae-developer groups and roles, and the seeded admin account.
//
// This is here, in the installer, rather than in a ThunderID bootstrap
// document, because ThunderID reads its bootstrap folder exactly ONCE, at
// install, from a pre-install hook Job. A document can therefore only ever
// seed an IdP that AEP itself installs — but the platform IdP is shared
// infrastructure (ADR-0027/ADR-0028): OpenChoreo installs it, Agent Manager
// publishes into it, and on a converged cluster AEP arrives at one that
// already exists. "Make sure these objects are present in whatever IdP we
// found" is an admin-API job, and this is the one component on every install
// path that runs after Thunder is up and holds admin credentials.
//
// Every Ensure* below is get-or-create against a live Thunder, so a re-run of
// `aectl platform install` is a no-op rather than a conflict.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/wso2/aep/aep-api/aeperms"
)

// AEAdminAccount is the account to seed into ae-admin, or the zero value to
// seed none. Username is fixed by the caller; Password is supplied at install
// (prompted, or from the environment) and never defaulted here — a credential
// this package chose would be the same on every install that ever ran it.
type AEAdminAccount struct {
	Username string
	Password string
	Email    string
	Name     string
}

// EnsureAEPermissions provisions the whole AE authorization model into
// Thunder: the resource server named by aeperms, one action per permission,
// the two groups, the seeded admin (when account is set), and the two roles
// bound to their groups.
//
// ORDER IS LOAD-BEARING. The user is created before the groups because Thunder
// honours a group's `members` list only at group-CREATE time — a later update
// is silently ignored (the same constraint aep-api's own directory client
// works around by recreating a group). Creating ae-admin before the account
// exists would therefore leave an empty group that no later run could fill.
//
// resourceServerIdentifier must be the same value the console sends as its
// RFC 8707 resource indicator and aep-api accepts as an audience; the platform
// Helm chart derives all three from Thunder's public URL.
func (c *AdminClient) EnsureAEPermissions(
	ctx context.Context,
	resourceServerIdentifier string,
	account AEAdminAccount,
) error {
	rsID, err := c.ensureAEResourceServer(ctx, resourceServerIdentifier)
	if err != nil {
		return err
	}
	if err := c.ensureAEActions(ctx, rsID); err != nil {
		return err
	}

	var memberIDs []string
	if account.Username != "" {
		userID, uErr := c.ensureAEUser(ctx, account)
		if uErr != nil {
			return uErr
		}
		memberIDs = []string{userID}
	}

	for _, role := range aeperms.Roles() {
		// Only ae-admin gets the seeded account; ae-developer is created
		// empty, for a real member to be added to later.
		var members []string
		if role == aeperms.RoleAdmin {
			members = memberIDs
		}
		groupID, gErr := c.ensureAEGroup(ctx, role, members)
		if gErr != nil {
			return gErr
		}
		if err := c.ensureAERole(ctx, role, rsID, groupID); err != nil {
			return err
		}
	}
	return nil
}

// ensureAEResourceServer returns the `ae` resource server's internal ID,
// creating it if absent. Matched on IDENTIFIER rather than name: the
// identifier is what a token's audience and a client's resource indicator
// carry, so it is the field that must be unique in practice, and Thunder
// enforces it as such.
func (c *AdminClient) ensureAEResourceServer(ctx context.Context, identifier string) (string, error) {
	if identifier == "" {
		return "", fmt.Errorf("the ae resource server needs an identifier — Thunder rejects a non-URI one outright")
	}
	id, err := c.findResourceServerByIdentifier(ctx, identifier)
	if err != nil {
		return "", err
	}
	if id != "" {
		return id, nil
	}

	payload, _ := json.Marshal(map[string]any{
		"name":        aeperms.ResourceServerName,
		"identifier":  identifier,
		"description": "Permissions for aep-api's console-facing endpoints.",
		"ouId":        c.defaultOU,
	})
	body, status, err := c.doRequest(ctx, http.MethodPost, "/resource-servers", payload)
	if err != nil {
		return "", fmt.Errorf("create ae resource server: %w", err)
	}
	// A 409 means something else claimed the identifier between the lookup and
	// here — re-read rather than fail, so two installers racing settle on one.
	if status == http.StatusConflict {
		return c.findResourceServerByIdentifier(ctx, identifier)
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return "", fmt.Errorf("create ae resource server returned %d: %s", status, apiErrSummary(body))
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		return "", fmt.Errorf("parse created ae resource server: %w", err)
	}
	return created.ID, nil
}

func (c *AdminClient) findResourceServerByIdentifier(ctx context.Context, identifier string) (string, error) {
	body, status, err := c.doRequest(ctx, http.MethodGet, "/resource-servers", nil)
	if err != nil {
		return "", fmt.Errorf("list resource servers: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("list resource servers returned %d: %s", status, apiErrSummary(body))
	}
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", fmt.Errorf("parse resource servers response: %w", err)
	}
	for _, item := range toSlice(raw) {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if m["identifier"] == identifier {
			id, _ := m["id"].(string)
			return id, nil
		}
	}
	return "", nil
}

// ensureAEActions creates the AE resource and any of its actions that are
// missing. Additive by design: an install of a newer aectl against a Thunder
// provisioned by an older one adds the permissions that appeared in between,
// which is the whole reason this is a per-action reconcile rather than a
// create-once.
func (c *AdminClient) ensureAEActions(ctx context.Context, rsID string) error {
	resourceID, err := c.ensureAEResource(ctx, rsID)
	if err != nil {
		return err
	}

	have, err := c.listAEActionHandles(ctx, rsID, resourceID)
	if err != nil {
		return err
	}
	for _, action := range aeperms.Actions {
		if _, ok := have[action.Handle]; ok {
			continue
		}
		payload, _ := json.Marshal(map[string]any{
			"name":        action.Name,
			"handle":      action.Handle,
			"description": action.Description,
		})
		path := "/resource-servers/" + rsID + "/resources/" + resourceID + "/actions"
		body, status, aErr := c.doRequest(ctx, http.MethodPost, path, payload)
		if aErr != nil {
			return fmt.Errorf("create ae action %q: %w", action.Handle, aErr)
		}
		if status == http.StatusConflict {
			continue // another installer got there first
		}
		if status != http.StatusOK && status != http.StatusCreated {
			return fmt.Errorf("create ae action %q returned %d: %s", action.Handle, status, apiErrSummary(body))
		}
	}
	return nil
}

// ensureAEResource returns the internal ID of the single resource the AE
// actions hang off, creating it if absent. Thunder composes a permission key
// as "<resource handle>:<action handle>", which is what makes this resource's
// handle the "ae" prefix every permission carries.
func (c *AdminClient) ensureAEResource(ctx context.Context, rsID string) (string, error) {
	path := "/resource-servers/" + rsID + "/resources"
	body, status, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", fmt.Errorf("list ae resources: %w", err)
	}
	if status == http.StatusOK {
		var raw any
		if err := json.Unmarshal(body, &raw); err == nil {
			for _, item := range toSlice(raw) {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if m["handle"] == aeperms.ResourceHandle {
					id, _ := m["id"].(string)
					return id, nil
				}
			}
		}
	}

	payload, _ := json.Marshal(map[string]any{
		"name":        aeperms.Resource,
		"handle":      aeperms.ResourceHandle,
		"description": "AE permission actions",
	})
	body, status, err = c.doRequest(ctx, http.MethodPost, path, payload)
	if err != nil {
		return "", fmt.Errorf("create ae resource: %w", err)
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return "", fmt.Errorf("create ae resource returned %d: %s", status, apiErrSummary(body))
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		return "", fmt.Errorf("parse created ae resource: %w", err)
	}
	return created.ID, nil
}

func (c *AdminClient) listAEActionHandles(ctx context.Context, rsID, resourceID string) (map[string]struct{}, error) {
	path := "/resource-servers/" + rsID + "/resources/" + resourceID + "/actions"
	body, status, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("list ae actions: %w", err)
	}
	have := map[string]struct{}{}
	// A resource with no actions yet may answer 404 rather than an empty list;
	// either way there is nothing to skip.
	if status != http.StatusOK {
		return have, nil
	}
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse ae actions response: %w", err)
	}
	for _, item := range toSlice(raw) {
		if m, ok := item.(map[string]any); ok {
			if h, ok := m["handle"].(string); ok {
				have[h] = struct{}{}
			}
		}
	}
	return have, nil
}

// ensureAEUser returns the seeded admin's internal ID, creating the account if
// absent. An existing account's password is left alone: re-running the
// installer must not silently reset a credential somebody has since changed.
func (c *AdminClient) ensureAEUser(ctx context.Context, account AEAdminAccount) (string, error) {
	id, err := c.findUserByUsername(ctx, account.Username)
	if err != nil {
		return "", err
	}
	if id != "" {
		return id, nil
	}

	// password appears in BOTH attributes and credentials, deliberately:
	// Thunder's schema validation checks `attributes` for required credential
	// fields at create time and rejects the request without it, while
	// `credentials.password` is the one that stores a usable hashed login.
	payload, _ := json.Marshal(map[string]any{
		"type":     "Person",
		"ouId":     c.defaultOU,
		"password": account.Password,
		"attributes": map[string]any{
			"username": account.Username,
			"password": account.Password,
			"sub":      account.Username,
			"email":    account.Email,
			"name":     account.Name,
		},
		"credentials": map[string]any{"password": account.Password},
	})
	body, status, err := c.doRequest(ctx, http.MethodPost, "/users", payload)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", account.Username, err)
	}
	if status == http.StatusConflict {
		return c.findUserByUsername(ctx, account.Username)
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return "", fmt.Errorf("create %s returned %d: %s", account.Username, status, apiErrSummary(body))
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		return "", fmt.Errorf("parse created user: %w", err)
	}
	return created.ID, nil
}

func (c *AdminClient) findUserByUsername(ctx context.Context, username string) (string, error) {
	body, status, err := c.doRequest(ctx, http.MethodGet, "/users", nil)
	if err != nil {
		return "", fmt.Errorf("list users: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("list users returned %d: %s", status, apiErrSummary(body))
	}
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", fmt.Errorf("parse users response: %w", err)
	}
	for _, item := range toSlice(raw) {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		attrs, _ := m["attributes"].(map[string]any)
		if attrs != nil && attrs["username"] == username {
			id, _ := m["id"].(string)
			return id, nil
		}
	}
	return "", nil
}

// ensureAEGroup returns the group's internal ID, creating it with memberIDs if
// absent. Members are passed at CREATE because that is the only time Thunder
// honours them — see EnsureAEPermissions' ordering note. An existing group's
// membership is left exactly as it is: whoever an operator has since added to
// ae-admin is not something an installer re-run should revise.
func (c *AdminClient) ensureAEGroup(ctx context.Context, name string, memberIDs []string) (string, error) {
	id, err := c.findGroupByName(ctx, name)
	if err != nil {
		return "", err
	}
	if id != "" {
		return id, nil
	}

	members := make([]map[string]any, 0, len(memberIDs))
	for _, m := range memberIDs {
		members = append(members, map[string]any{"id": m, "type": "user"})
	}
	payload, _ := json.Marshal(map[string]any{
		"name":        name,
		"description": aeperms.RoleDescriptions[name],
		"ouId":        c.defaultOU,
		"members":     members,
	})
	body, status, err := c.doRequest(ctx, http.MethodPost, "/groups", payload)
	if err != nil {
		return "", fmt.Errorf("create group %q: %w", name, err)
	}
	if status == http.StatusConflict {
		return c.findGroupByName(ctx, name)
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return "", fmt.Errorf("create group %q returned %d: %s", name, status, apiErrSummary(body))
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		return "", fmt.Errorf("parse created group: %w", err)
	}
	return created.ID, nil
}

func (c *AdminClient) findGroupByName(ctx context.Context, name string) (string, error) {
	body, status, err := c.doRequest(ctx, http.MethodGet, "/groups", nil)
	if err != nil {
		return "", fmt.Errorf("list groups: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("list groups returned %d: %s", status, apiErrSummary(body))
	}
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", fmt.Errorf("parse groups response: %w", err)
	}
	for _, item := range toSlice(raw) {
		if m, ok := item.(map[string]any); ok && m["name"] == name {
			id, _ := m["id"].(string)
			return id, nil
		}
	}
	return "", nil
}

// ensureAERole creates the role holding its permissions and assigned to its
// group, if a role of that name does not already exist. An existing role is
// left untouched: its permission set is the thing an operator may have
// deliberately narrowed, and an installer that re-widened it on every run
// would be a privilege-escalation path, not a convenience.
func (c *AdminClient) ensureAERole(ctx context.Context, role, rsID, groupID string) error {
	existing, err := c.findRoleByName(ctx, role)
	if err != nil {
		return fmt.Errorf("check role %q: %w", role, err)
	}
	if existing != "" {
		return nil
	}

	perms := aeperms.RolePermissions(role)
	keys := make([]string, len(perms))
	for i, p := range perms {
		keys[i] = string(p)
	}
	payload, _ := json.Marshal(map[string]any{
		"name":        role,
		"description": aeperms.RoleDescriptions[role],
		"ouId":        c.defaultOU,
		"permissions": []map[string]any{
			{"resourceServerId": rsID, "permissions": keys},
		},
		"assignments": []map[string]any{
			{"id": groupID, "type": "group"},
		},
	})
	body, status, err := c.doRequest(ctx, http.MethodPost, "/roles", payload)
	if err != nil {
		return fmt.Errorf("create role %q: %w", role, err)
	}
	if status == http.StatusConflict {
		return nil
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return fmt.Errorf("create role %q returned %d: %s", role, status, apiErrSummary(body))
	}
	return nil
}
