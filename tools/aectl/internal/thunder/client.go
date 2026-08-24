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

// Package thunder provides a minimal Thunder admin API client for use by
// aectl platform install. It registers OAuth applications directly via HTTP
// rather than through the thunder-app-operator CRD.
//
// Wire format: camelCase JSON keys throughout, matching Thunder 0.34.0's
// admin REST API as validated against the live cluster. The operator at
// deployments/single-cluster/resource-types/thunder-app/operator/internal/thunder/client.go
// is the authoritative reference for payload shapes.
package thunder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// placeholderRedirectURI is used when a public client has no redirect URIs
// yet. Thunder rejects an empty redirectUris list, so we send an RFC 2606
// unroutable placeholder and replace it on the next reconcile.
const placeholderRedirectURI = "https://pending.invalid/callback"

// apiErrSummary extracts Thunder's structured error fields (code + message)
// from a response body and returns them as a short string. If the body is not
// a Thunder error object (e.g. it's the full application payload, which may
// contain client secrets), only the byte length is reported.
func apiErrSummary(body []byte) string {
	var apiErr struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &apiErr) == nil && (apiErr.Code != "" || apiErr.Message != "") {
		return fmt.Sprintf("code=%s message=%s", apiErr.Code, apiErr.Message)
	}
	return fmt.Sprintf("(%d bytes)", len(body))
}

// tokenValiditySeconds is the access/id token lifetime pinned to 24h,
// matching the seeded Console app bootstrap value.
const tokenValiditySeconds = 86400

// identityUserAttributes is the platform-wide contract for claims included
// in tokens. Matches the operator's identity-claim contract.
var identityUserAttributes = []string{
	"given_name", "family_name", "username", "groups",
	"email", "name", "ouId", "ouName", "ouHandle",
}

// DesiredApp describes the desired state of a Thunder OAuth2 application.
type DesiredApp struct {
	// ClientID is the OAuth client_id and is also used as the Thunder app "name".
	ClientID string
	// ClientType is "public" (PKCE authorization_code) or "confidential" (client_credentials).
	ClientType string
	// ClientSecret is required for confidential clients.
	ClientSecret string
	// RedirectURIs are required for public (PKCE) clients.
	RedirectURIs []string
}

// AdminClient calls Thunder's admin API to create/update OAuth applications.
type AdminClient struct {
	baseURL   string
	token     string
	defaultOU string
	http      *http.Client
}

// New authenticates with Thunder using client_credentials + scope=system and
// returns a client ready to call admin APIs.
func New(ctx context.Context, baseURL, adminClientID, adminClientSecret string) (*AdminClient, error) {
	hc := &http.Client{Timeout: 30 * time.Second}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", adminClientID)
	form.Set("client_secret", adminClientSecret)
	form.Set("scope", "system")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/oauth2/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("thunder token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("thunder token request returned %d: %s", resp.StatusCode, body)
	}

	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return nil, fmt.Errorf("could not parse access token from Thunder response: %s", body)
	}

	c := &AdminClient{baseURL: baseURL, token: tok.AccessToken, http: hc}

	// Resolve and cache the default OU ID once at construction.
	ouID, err := c.fetchDefaultOU(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch default OU: %w", err)
	}
	c.defaultOU = ouID

	return c, nil
}

// EnsureApplication creates or updates a Thunder OAuth application to match
// the desired state. It is idempotent — safe to call on every install/upgrade.
func (c *AdminClient) EnsureApplication(ctx context.Context, app DesiredApp) error {
	internalID, err := c.findAppByClientID(ctx, app.ClientID)
	if err != nil {
		return fmt.Errorf("look up app %q: %w", app.ClientID, err)
	}

	if internalID != "" {
		return c.updateApp(ctx, internalID, app)
	}
	return c.createApp(ctx, app)
}

// AssignAdminRole grants clientID the Thunder "system" permission via a
// dedicated "aep-system" role. It is fully idempotent: if the role exists and
// clientID is already assigned it returns immediately; if the role exists but
// the assignment is missing it adds it via PUT; if the role is absent it
// creates it with the assignment inline (the only reliable path on Thunder
// 0.34 — POST /roles/{id}/assignments/add 500s for app targets and
// POST /role-assignments returns 404).
func (c *AdminClient) AssignAdminRole(ctx context.Context, clientID string) error {
	// Resolve the app's internal ID first — needed on both the create and
	// update paths.
	appID, err := c.findAppByClientID(ctx, clientID)
	if err != nil {
		return fmt.Errorf("find app %q: %w", clientID, err)
	}
	if appID == "" {
		return fmt.Errorf("app %q not found — cannot assign system role", clientID)
	}

	// Check whether the aep-system role already exists.
	roleID, err := c.findRoleByName(ctx, "aep-system")
	if err != nil {
		return fmt.Errorf("check aep-system role: %w", err)
	}
	if roleID != "" {
		// Role exists — verify clientID is in its assignments; add it if not.
		return c.ensureAppInRole(ctx, roleID, appID)
	}

	// Role is absent — resolve the system resource server and create the role
	// with the app assigned inline.
	sysRsID, err := c.findSystemResourceServerID(ctx)
	if err != nil {
		return fmt.Errorf("find system resource server: %w", err)
	}
	if sysRsID == "" {
		return fmt.Errorf("system resource server not found in Thunder")
	}

	payload, _ := json.Marshal(map[string]any{
		"name":        "aep-system",
		"description": "Grants aep-system-client the Thunder system permission.",
		"ouId":        c.defaultOU,
		"permissions": []map[string]any{
			{"resourceServerId": sysRsID, "permissions": []string{"system"}},
		},
		"assignments": []map[string]any{
			{"id": appID, "type": "app"},
		},
	})
	respBody, status, err := c.doRequest(ctx, http.MethodPost, "/roles", payload)
	if err != nil {
		return fmt.Errorf("create aep-system role: %w", err)
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return fmt.Errorf("assign admin role to %q returned %d: %s", clientID, status, apiErrSummary(respBody))
	}
	return nil
}

// -- private helpers ----------------------------------------------------------

func (c *AdminClient) fetchDefaultOU(ctx context.Context) (string, error) {
	body, status, err := c.doRequest(ctx, http.MethodGet, "/organization-units/tree/default", nil)
	if err != nil {
		return "", fmt.Errorf("get default OU: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("get default OU returned %d: %s", status, body)
	}
	var ou struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &ou); err != nil {
		return "", fmt.Errorf("parse OU response: %w", err)
	}
	if ou.ID == "" {
		return "", fmt.Errorf("thunder default OU has no id")
	}
	return ou.ID, nil
}

type appSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ClientID string `json:"clientId"`
}

// findAppByClientID paginates through /applications to find the internal ID of
// the app whose clientId matches. Returns ("", nil) when not found.
func (c *AdminClient) findAppByClientID(ctx context.Context, clientID string) (string, error) {
	const pageSize = 100
	const maxPages = 100
	for page := 0; page < maxPages; page++ {
		reqURL := fmt.Sprintf("/applications?offset=%d&limit=%d", page*pageSize, pageSize)
		body, status, err := c.doRequest(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return "", fmt.Errorf("list applications: %w", err)
		}
		if status != http.StatusOK {
			return "", fmt.Errorf("list applications returned %d: %s", status, body)
		}
		apps, err := parseAppList(body)
		if err != nil {
			return "", err
		}
		for _, a := range apps {
			if a.ClientID == clientID {
				return a.ID, nil
			}
		}
		if len(apps) < pageSize {
			return "", nil // exhausted without finding it
		}
	}
	return "", fmt.Errorf("exceeded %d pages looking for app %q", maxPages, clientID)
}

// parseAppList handles Thunder's inconsistent list responses: bare array or
// {"applications": [...]}.
func parseAppList(data []byte) ([]appSummary, error) {
	var apps []appSummary
	if err := json.Unmarshal(data, &apps); err == nil {
		return apps, nil
	}
	var wrapped struct {
		Applications []appSummary `json:"applications"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return nil, fmt.Errorf("parse applications response: %w", err)
	}
	return wrapped.Applications, nil
}

func (c *AdminClient) createApp(ctx context.Context, app DesiredApp) error {
	payload := c.buildCreatePayload(app)
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal create payload for %q: %w", app.ClientID, err)
	}
	body, status, err := c.doRequest(ctx, http.MethodPost, "/applications", data)
	if err != nil {
		return fmt.Errorf("create app %q: %w", app.ClientID, err)
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return fmt.Errorf("create app %q returned %d: %s", app.ClientID, status, apiErrSummary(body))
	}
	return nil
}

func (c *AdminClient) updateApp(ctx context.Context, internalID string, app DesiredApp) error {
	// Read the full app object so we can PUT it back with minimal diff.
	body, status, err := c.doRequest(ctx, http.MethodGet, "/applications/"+internalID, nil)
	if err != nil {
		return fmt.Errorf("get app %q for update: %w", app.ClientID, err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("get app %q returned %d: %s", app.ClientID, status, apiErrSummary(body))
	}

	var full map[string]any
	if err := json.Unmarshal(body, &full); err != nil {
		return fmt.Errorf("parse app %q for update: %w", app.ClientID, err)
	}

	// Locate the oauth2 inboundAuthConfig entry and patch it.
	cfg, err := extractOAuthConfig(full)
	if err != nil {
		return fmt.Errorf("app %q: %w", app.ClientID, err)
	}

	if app.ClientType == "confidential" {
		if app.ClientSecret != "" {
			cfg["clientSecret"] = app.ClientSecret
		}
	} else {
		cfg["redirectUris"] = wireRedirectURIs(app.RedirectURIs)
		cfg["scopeClaims"] = scopeClaimConfig()
	}
	cfg["token"] = tokenClaimConfig()
	full["allowedUserTypes"] = []string{"Person"}

	data, err := json.Marshal(full)
	if err != nil {
		return fmt.Errorf("marshal update payload for %q: %w", app.ClientID, err)
	}
	respBody, status, err := c.doRequest(ctx, http.MethodPut, "/applications/"+internalID, data)
	if err != nil {
		return fmt.Errorf("update app %q: %w", app.ClientID, err)
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("update app %q returned %d: %s", app.ClientID, status, apiErrSummary(respBody))
	}
	return nil
}

func (c *AdminClient) buildCreatePayload(app DesiredApp) map[string]any {
	base := map[string]any{
		"name":             app.ClientID,
		"ouId":             c.defaultOU,
		"allowedUserTypes": []string{"Person"},
	}

	if app.ClientType == "confidential" {
		base["inboundAuthConfig"] = []map[string]any{
			{
				"type": "oauth2",
				"config": map[string]any{
					"clientId":                app.ClientID,
					"clientSecret":            app.ClientSecret,
					"grantTypes":              []string{"client_credentials"},
					"tokenEndpointAuthMethod": "client_secret_post",
					"pkceRequired":            false,
					"publicClient":            false,
					"token":                   tokenClaimConfig(),
				},
			},
		}
	} else {
		base["inboundAuthConfig"] = []map[string]any{
			{
				"type": "oauth2",
				"config": map[string]any{
					"clientId":                app.ClientID,
					"redirectUris":            wireRedirectURIs(app.RedirectURIs),
					"grantTypes":              []string{"authorization_code", "refresh_token"},
					"responseTypes":           []string{"code"},
					"tokenEndpointAuthMethod": "none",
					"pkceRequired":            true,
					"publicClient":            true,
					"token":                   tokenClaimConfig(),
					"scopeClaims":             scopeClaimConfig(),
				},
			},
		}
	}
	return base
}

// findRoleByName returns the internal ID of the role with the given name, or
// "" if no such role exists.
func (c *AdminClient) findRoleByName(ctx context.Context, name string) (string, error) {
	body, status, err := c.doRequest(ctx, http.MethodGet, "/roles", nil)
	if err != nil {
		return "", fmt.Errorf("list roles: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("list roles returned %d: %s", status, body)
	}
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", fmt.Errorf("parse roles response: %w", err)
	}
	for _, item := range toSlice(raw) {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if m["name"] == name {
			id, _ := m["id"].(string)
			return id, nil
		}
	}
	return "", nil
}

// ensureAppInRole fetches the role at roleID and, if appID is not already in
// its assignments, adds it via PUT /roles/{id}.
func (c *AdminClient) ensureAppInRole(ctx context.Context, roleID, appID string) error {
	body, status, err := c.doRequest(ctx, http.MethodGet, "/roles/"+roleID, nil)
	if err != nil {
		return fmt.Errorf("get role %q: %w", roleID, err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("get role %q returned %d: %s", roleID, status, apiErrSummary(body))
	}
	var full map[string]any
	if err := json.Unmarshal(body, &full); err != nil {
		return fmt.Errorf("parse role %q: %w", roleID, err)
	}

	// Check whether the app is already in the assignments list.
	for _, item := range toSlice(full["assignments"]) {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if m["id"] == appID {
			return nil
		}
	}

	// App not assigned — append it and PUT the role back.
	existing := toSlice(full["assignments"])
	full["assignments"] = append(existing, map[string]any{"id": appID, "type": "app"})
	data, err := json.Marshal(full)
	if err != nil {
		return fmt.Errorf("marshal updated role %q: %w", roleID, err)
	}
	respBody, status, err := c.doRequest(ctx, http.MethodPut, "/roles/"+roleID, data)
	if err != nil {
		return fmt.Errorf("update role %q: %w", roleID, err)
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("update role %q returned %d: %s", roleID, status, apiErrSummary(respBody))
	}
	return nil
}

// findSystemResourceServerID returns the internal ID of the resource server
// whose identifier is "system".
func (c *AdminClient) findSystemResourceServerID(ctx context.Context) (string, error) {
	body, status, err := c.doRequest(ctx, http.MethodGet, "/resource-servers", nil)
	if err != nil {
		return "", fmt.Errorf("list resource servers: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("list resource servers returned %d: %s", status, body)
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
		if m["identifier"] == "system" {
			id, _ := m["id"].(string)
			return id, nil
		}
	}
	return "", nil
}

// doRequest sends an authenticated request and returns (body, statusCode, err).
// A transport error returns a non-nil err. HTTP errors are surfaced via the
// status code; the caller decides whether they are failures.
func (c *AdminClient) doRequest(ctx context.Context, method, path string, body []byte) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, nil
}

// extractOAuthConfig returns the mutable config map from the app's
// inboundAuthConfig[oauth2] entry so the caller can patch it in-place.
func extractOAuthConfig(app map[string]any) (map[string]any, error) {
	listAny, ok := app["inboundAuthConfig"].([]any)
	if !ok || len(listAny) == 0 {
		return nil, fmt.Errorf("inboundAuthConfig missing or empty")
	}
	for _, entryAny := range listAny {
		entry, ok := entryAny.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := entry["type"].(string); t != "oauth2" {
			continue
		}
		cfg, ok := entry["config"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("inboundAuthConfig[oauth2].config is not an object")
		}
		return cfg, nil
	}
	return nil, fmt.Errorf("no inboundAuthConfig entry with type=oauth2")
}

func wireRedirectURIs(desired []string) []any {
	if len(desired) == 0 {
		return []any{placeholderRedirectURI}
	}
	out := make([]any, len(desired))
	for i, u := range desired {
		out[i] = u
	}
	return out
}

func tokenClaimConfig() map[string]any {
	return map[string]any{
		"accessToken": map[string]any{"validityPeriod": tokenValiditySeconds, "userAttributes": identityUserAttributes},
		"idToken":     map[string]any{"validityPeriod": tokenValiditySeconds, "userAttributes": identityUserAttributes},
	}
}

func scopeClaimConfig() map[string]any {
	return map[string]any{
		"profile": []string{"name", "given_name", "family_name", "picture"},
		"email":   []string{"email", "email_verified"},
		"group":   []string{"groups"},
		"ou":      []string{"ouId", "ouName", "ouHandle"},
	}
}

// toSlice normalises Thunder's inconsistent list response formats.
func toSlice(v any) []any {
	if arr, ok := v.([]any); ok {
		return arr
	}
	if m, ok := v.(map[string]any); ok {
		for _, key := range []string{"roles", "applications", "resourceServers", "flows", "data", "list", "items"} {
			if arr, ok := m[key].([]any); ok {
				return arr
			}
		}
	}
	return nil
}
