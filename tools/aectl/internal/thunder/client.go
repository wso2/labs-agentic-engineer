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
// Wire format: camelCase JSON keys throughout, matching ThunderID 1.0.0's
// admin REST API (oci://ghcr.io/thunder-id/helm-charts/thunderid). Notable
// shapes:
//   - applications require a top-level `type` field (see appType)
//   - access-token claims nest under userConfig (interactive/public apps) or
//     clientConfig (client_credentials/confidential apps) — see
//     tokenClaimConfig
//   - the built-in "System" resource server's `identifier` is an absolute
//     URI derived from the deployment's public URL ("<publicUrl>/mcp"), not
//     the literal string "system" — see SystemResourceIdentifier
//   - an organization unit is read by its handle at
//     /organization-units/tree/<handle> — see ouIDOf
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
	// NoUserAttributes issues the client's access token with no attributes at
	// all. It marks the AE-only internal client: its token must never carry an
	// org (or any identity) the gate could trust.
	NoUserAttributes bool
	// OrgOUHandle marks one of an org's own clients (its publisher and its AE
	// Studio client) and names the handle of the org's organization unit. The
	// client is created under that OU, so its client_credentials token names
	// the org, and with the settings an org client needs (orgClientConfig).
	// Empty is a platform client, under the default OU.
	OrgOUHandle string
}

// defaultOUHandle is the handle of ThunderID's built-in organization unit.
const defaultOUHandle = "default"

// orgTokenAttributes are the claims an org client's client_credentials token
// carries. ThunderID 1.0.0 releases an application's OU into such a token only
// when the application's token config asks for it, and every verifier of an
// org client's token reads the org from it.
var orgTokenAttributes = []string{"ouId", "ouHandle"}

// orgTokenValiditySeconds is the lifetime of an org client's access token.
const orgTokenValiditySeconds = 3600

// orgClientAuthMethod is how an org client sends its secret to the token
// endpoint. The coding runner and the AE Studio pod both use HTTP Basic.
const orgClientAuthMethod = "client_secret_basic"

// AdminClient calls Thunder's admin API to create/update OAuth applications.
type AdminClient struct {
	baseURL   string
	token     string
	defaultOU string
	// ous caches the id of each organization unit looked up by its handle.
	ous  map[string]string
	http *http.Client
}

// SystemResourceIdentifier derives the identifier of ThunderID's own "System"
// resource server — the one that owns the `system` scope — from Thunder's
// PUBLIC URL, following ThunderID's convention ("<publicUrl>/mcp"). It is a
// name, not an address: the public URL is the right input even though this
// client talks to Thunder over a port-forward. Empty in, empty out.
func SystemResourceIdentifier(publicURL string) string {
	publicURL = strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if publicURL == "" {
		return ""
	}
	return publicURL + "/mcp"
}

// New authenticates with Thunder using client_credentials + scope=system and
// returns a client ready to call admin APIs.
//
// systemResource is sent as the OAuth `resource` indicator (see
// SystemResourceIdentifier). ThunderID resolves a requested scope against a
// resource server; without the indicator it uses the server-wide default, and
// when that default does not define `system` the scope is dropped SILENTLY —
// the token endpoint answers 200 and every admin call then 403s. Empty sends no
// indicator, which is only right against a Thunder with no default resource
// server.
func New(ctx context.Context, baseURL, adminClientID, adminClientSecret, systemResource string) (*AdminClient, error) {
	hc := &http.Client{Timeout: 30 * time.Second}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", adminClientID)
	form.Set("client_secret", adminClientSecret)
	form.Set("scope", "system")
	if systemResource != "" {
		form.Set("resource", systemResource)
	}

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
	ouID, err := c.ouIDOf(ctx, defaultOUHandle)
	if err != nil {
		return nil, fmt.Errorf("fetch default OU: %w", err)
	}
	c.defaultOU = ouID

	return c, nil
}

// EnsureApplication creates or updates a Thunder OAuth application to match
// the desired state. It is idempotent — safe to call on every install/upgrade.
func (c *AdminClient) EnsureApplication(ctx context.Context, app DesiredApp) error {
	ouID := c.defaultOU
	if app.OrgOUHandle != "" {
		var err error
		if ouID, err = c.ouIDOf(ctx, app.OrgOUHandle); err != nil {
			return fmt.Errorf("app %q: %w", app.ClientID, err)
		}
	}
	internalID, err := c.findAppByClientID(ctx, app.ClientID)
	if err != nil {
		return fmt.Errorf("look up app %q: %w", app.ClientID, err)
	}

	if internalID != "" {
		return c.updateApp(ctx, internalID, app, ouID)
	}
	return c.createApp(ctx, app, ouID)
}

// -- private helpers ----------------------------------------------------------

// ouIDOf returns the id of the organization unit with the given handle. A
// handle ThunderID does not know is an error: aectl creates clients, never
// organization units.
func (c *AdminClient) ouIDOf(ctx context.Context, handle string) (string, error) {
	if id, ok := c.ous[handle]; ok {
		return id, nil
	}
	body, status, err := c.doRequest(ctx, http.MethodGet, "/organization-units/tree/"+url.PathEscape(handle), nil)
	if err != nil {
		return "", fmt.Errorf("get organization unit %q: %w", handle, err)
	}
	if status == http.StatusNotFound {
		return "", fmt.Errorf("ThunderID has no organization unit with handle %q", handle)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("get organization unit %q returned %d: %s", handle, status, apiErrSummary(body))
	}
	var ou struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &ou); err != nil {
		return "", fmt.Errorf("parse organization unit %q: %w", handle, err)
	}
	if ou.ID == "" {
		return "", fmt.Errorf("organization unit %q has no id", handle)
	}
	if c.ous == nil {
		c.ous = map[string]string{}
	}
	c.ous[handle] = ou.ID
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

func (c *AdminClient) createApp(ctx context.Context, app DesiredApp, ouID string) error {
	payload := buildCreatePayload(app, ouID)
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

func (c *AdminClient) updateApp(ctx context.Context, internalID string, app DesiredApp, ouID string) error {
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

	// An org's client is never moved: one found under another organization
	// unit would mint tokens naming that unit's org.
	if app.OrgOUHandle != "" {
		if got, _ := full["ouId"].(string); got != ouID {
			return fmt.Errorf("app %q exists under another organization unit than %q; remove it and run again", app.ClientID, app.OrgOUHandle)
		}
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
	cfg["token"] = tokenConfigOf(app)
	if app.OrgOUHandle != "" {
		cfg["tokenEndpointAuthMethod"] = orgClientAuthMethod
	}
	full["allowedUserTypes"] = []string{"Person"}
	full["type"] = appType(app.ClientType)

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

func buildCreatePayload(app DesiredApp, ouID string) map[string]any {
	base := map[string]any{
		"name":             app.ClientID,
		"type":             appType(app.ClientType),
		"ouId":             ouID,
		"allowedUserTypes": []string{"Person"},
	}

	if app.ClientType == "confidential" {
		authMethod := "client_secret_post"
		if app.OrgOUHandle != "" {
			authMethod = orgClientAuthMethod
		}
		base["inboundAuthConfig"] = []map[string]any{
			{
				"type": "oauth2",
				"config": map[string]any{
					"clientId":                app.ClientID,
					"clientSecret":            app.ClientSecret,
					"grantTypes":              []string{"client_credentials"},
					"tokenEndpointAuthMethod": authMethod,
					"pkceRequired":            false,
					"publicClient":            false,
					"token":                   tokenConfigOf(app),
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
					"token":                   tokenConfigOf(app),
					"scopeClaims":             scopeClaimConfig(),
				},
			},
		}
	}
	return base
}

// appType maps aectl's ClientType ("confidential"/"public") to the
// application `type` ThunderID 1.0.0 requires at creation — one of browser,
// fullstack, mobile, m2m, mcp, or custom; anything else, including an absent
// field, fails with 400 APP-1042 "Application type is required". "m2m"
// matches this repo's own client_credentials-only documents (e.g.
// deployments/single-cluster/thunder-resources/80-aep-api-client.yaml);
// "browser" matches its public PKCE console client
// (87-aep-console-app.yaml).
func appType(clientType string) string {
	if clientType == "confidential" {
		return "m2m"
	}
	return "browser"
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

// tokenConfigOf is the token config of app: an org client's names its org and
// nothing else (orgTokenAttributes), any other follows tokenClaimConfig.
func tokenConfigOf(app DesiredApp) map[string]any {
	if app.OrgOUHandle != "" {
		return map[string]any{
			"accessToken": map[string]any{
				"clientConfig": map[string]any{
					"validityPeriod": orgTokenValiditySeconds,
					"attributes":     orgTokenAttributes,
				},
			},
		}
	}
	return tokenClaimConfig(app.ClientType, app.NoUserAttributes)
}

// tokenClaimConfig returns ThunderID 1.0.0's token issuance/claims config for
// an app of the given clientType ("confidential" or "public").
//
// Access-token claims nest under userConfig (claims for a token issued to a
// logged-in user — authorization_code) or clientConfig (claims for a token
// issued to the client itself — client_credentials); a flat
// {validityPeriod, userAttributes} directly under accessToken is not a field
// ThunderID 1.0.0 reads.
//
// idToken keeps the flat {validityPeriod, userAttributes} shape, and is
// omitted for confidential/m2m apps: they authenticate via client_credentials
// and never receive an ID token.
func tokenClaimConfig(clientType string, noUserAttributes bool) map[string]any {
	if clientType == "confidential" {
		attrs := identityUserAttributes
		if noUserAttributes {
			attrs = []string{}
		}
		return map[string]any{
			"accessToken": map[string]any{
				"clientConfig": map[string]any{
					"validityPeriod": tokenValiditySeconds,
					"attributes":     attrs,
				},
			},
		}
	}
	return map[string]any{
		"accessToken": map[string]any{
			"userConfig": map[string]any{
				"validityPeriod": tokenValiditySeconds,
				"attributes":     identityUserAttributes,
			},
		},
		"idToken": map[string]any{"validityPeriod": tokenValiditySeconds, "userAttributes": identityUserAttributes},
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
