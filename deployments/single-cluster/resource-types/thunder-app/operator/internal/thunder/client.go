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

// Package thunder is the operator-local Thunder admin client. It mints a
// `scope=system` access token via the client_credentials grant against a
// Thunder OAuth app that the operator authenticates as, then uses that
// token to declare/remove per-CR OAuth applications on the platform's
// shared Thunder instance via Thunder's /applications admin REST API.
//
// Wire shape: camelCase JSON keys (`ouId`, `inboundAuthConfig`, `clientId`,
// `redirectUris`, `grantTypes`, `responseTypes`, `pkceRequired`,
// `publicClient`, `tokenEndpointAuthMethod`), mirroring the live-E2E-validated
// BFF client services/aep-api/internal/clients/thundersvc/client.go and the
// local-stack bootstrap payloads in deployments/single-cluster/
// values-thunder.yaml (59-aep-oauth-apps.sh) — the Thunder 0.34.0 instance
// this operator actually targets speaks camelCase. (The snake_case format
// described by the unused wso2-agentic-engineer-bundle chart's
// thunder-bootstrap.sh is NOT what that instance accepts; do not mirror it.)
//
// This package intentionally does NOT import services/aep-api — it is a
// separate Go module by design
// (deployments/single-cluster/resource-types/thunder-app/operator), so the
// minimal request/response shapes needed here are copied rather than
// shared.
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
	"sync"
	"time"
)

// DesiredApp is the desired state of a Thunder OAuth2 application, as
// derived from a ThunderApplication CR by the reconciler (Task 3).
type DesiredApp struct {
	// Name is the Thunder application's unique identifier — it doubles as
	// the OAuth2 client_id assigned on create — derived by the reconciler
	// as "aep-<cr-namespace>-<cr-name>". EnsureApplication uses it as the
	// lookup key to find the same app again across reconciles.
	Name string
	// DisplayName is the human-readable label from the CR. Carried on the
	// CR but NOT sent on the wire in v1: neither of thundersvc's create
	// functions (createApp/createSPAApp) sends a display-name/description
	// field, and Thunder 0.34.0's schema tolerance for extra keys is
	// unverified — the app's Thunder `name` is set to Name (the
	// deterministic per-CR identity). Task 4's live verification may wire
	// this in if Thunder accepts a separate label field.
	DisplayName string
	// Scopes is accepted for interface completeness but is currently a
	// no-op on the wire: Thunder 0.34.0's application inboundAuthConfig
	// has no per-app OAuth scope allowlist field. Checked against both of
	// this repo's Thunder bootstrap scripts —
	// deployments/helm-charts/wso2-agentic-engineer-bundle/files/thunder-bootstrap.sh
	// and deployments/single-cluster/values-thunder.yaml's embedded
	// 59-aep-oauth-apps.sh — neither sets a scope field for any
	// application (confidential or public/PKCE). The advertised scope set
	// is carried independently by the thunder-app ClusterResourceType's
	// `outputs.scopes` value, so leaving this field a no-op here does not
	// break that path. If a future Thunder version adds such a field,
	// wire it in here — do not invent one now.
	Scopes []string
	// RedirectURIs is the exact set of allowed OAuth redirect URIs.
	// EnsureApplication REPLACES the app's stored redirect URIs with this
	// set on every call — it does not merge with whatever Thunder
	// currently has (desired-state semantics: the CR is the single source
	// of truth, unlike the aep-api BFF client this package's wire shapes
	// were mirrored from, which merges). An empty set is adapted on the
	// wire to a single unroutable placeholder (see placeholderRedirectURI)
	// because Thunder rejects an empty list; the field itself stays
	// honestly empty.
	RedirectURIs []string
}

// AdminClient is the operator-local Thunder admin surface consumed by the
// reconciler (Task 3). Implementations must be safe for concurrent use.
type AdminClient interface {
	// EnsureApplication creates the app if absent (public client, PKCE
	// required, token_endpoint_auth_method=none) or updates scopes/
	// redirectUris to match (PUT is a full replace of those fields —
	// desired state, not merge).
	EnsureApplication(ctx context.Context, app DesiredApp) (clientID string, err error)
	// DeleteApplication removes the app by name; absent app is success
	// (idempotent).
	DeleteApplication(ctx context.Context, name string) error
}

// Config bundles the client's construction parameters.
type Config struct {
	// BaseURL is Thunder's admin API base, e.g.
	// http://thunder-service.thunder.svc.cluster.local:8090 (trailing
	// slash tolerated).
	BaseURL string
	// ClientID/ClientSecret are the operator's own system OAuth2 client
	// credentials (client_credentials grant, scope=system).
	ClientID     string
	ClientSecret string
	// HTTPClient — optional override (tests inject one pointed at an
	// httptest.Server). Defaults to a 30s-timeout net/http client.
	HTTPClient *http.Client
}

// placeholderRedirectURI is sent on the wire whenever the desired app has
// NO redirect URIs. Thunder rejects creating/updating an authorization_code
// app with an empty redirectUris list (error APP-1024), but our flow must
// mint the app before any real redirect URI exists — the consuming SPA's
// public URL appears only after it deploys, and component dispatch gates on
// this resource being ready, so empty-URIs-blocks-ready would deadlock
// provisioning. ".invalid" is an RFC 2606 reserved TLD: syntactically valid
// for Thunder, unroutable by construction (no OAuth redirect can ever land
// on it). It is wire adaptation only — the DesiredApp/CR stay honestly
// empty, and it is replaced automatically on the next reconcile once real
// URIs land.
const placeholderRedirectURI = "https://pending.invalid/callback"

// wireRedirectURIs adapts the desired redirect-URI set for Thunder's wire:
// the exact desired values when present, [placeholderRedirectURI] when empty
// (see that constant's doc comment).
func wireRedirectURIs(desired []string) []any {
	if len(desired) == 0 {
		return []any{placeholderRedirectURI}
	}
	uris := make([]any, 0, len(desired))
	for _, u := range desired {
		uris = append(uris, u)
	}
	return uris
}

// New builds a Thunder admin client.
func New(cfg Config) AdminClient {
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &client{
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		systemID:     cfg.ClientID,
		systemSecret: cfg.ClientSecret,
		httpClient:   hc,
	}
}

type client struct {
	baseURL      string
	systemID     string
	systemSecret string
	httpClient   *http.Client

	mu          sync.Mutex
	cachedToken string
	tokenExpiry time.Time

	muOU      sync.Mutex
	defaultOU string
}

// -- system token -----------------------------------------------------------

// getSystemToken returns a cached token or fetches a new one. Cached with a
// 30s expiry skew so a call landing just before real expiry doesn't get a
// token that dies mid-flight.
func (c *client) getSystemToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cachedToken != "" && time.Now().Before(c.tokenExpiry) {
		return c.cachedToken, nil
	}

	token, expiresIn, err := c.fetchSystemToken(ctx)
	if err != nil {
		return "", err
	}
	c.cachedToken = token
	const skew = 30
	switch {
	case expiresIn > skew:
		c.tokenExpiry = time.Now().Add(time.Duration(expiresIn-skew) * time.Second)
	case expiresIn > 0:
		c.tokenExpiry = time.Now().Add(time.Duration(expiresIn) * time.Second)
	default:
		c.tokenExpiry = time.Now().Add(time.Minute)
	}
	return token, nil
}

func (c *client) fetchSystemToken(ctx context.Context) (string, int, error) {
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"scope":         {"system"},
		"client_id":     {c.systemID},
		"client_secret": {c.systemSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/oauth2/token", strings.NewReader(data.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("thunder token request build: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("thunder token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", 0, fmt.Errorf("thunder token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", 0, fmt.Errorf("thunder token decode: %w", err)
	}
	if result.AccessToken == "" {
		return "", 0, fmt.Errorf("thunder returned empty access_token")
	}
	return result.AccessToken, result.ExpiresIn, nil
}

// -- OU resolution ------------------------------------------------------------

// getDefaultOUID returns Thunder's default organisation-unit id, cached
// after the first successful lookup. All apps this operator manages are
// registered under it — the platform Thunder has a single default OU (v1
// scope; see ThunderApplicationSpec's instanceRef note).
func (c *client) getDefaultOUID(ctx context.Context, token string) (string, error) {
	c.muOU.Lock()
	defer c.muOU.Unlock()
	if c.defaultOU != "" {
		return c.defaultOU, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/organization-units/tree/default", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("thunder get default OU: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("thunder get default OU returned %d: %s", resp.StatusCode, string(body))
	}

	var ou struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ou); err != nil {
		return "", fmt.Errorf("thunder OU decode: %w", err)
	}
	if ou.ID == "" {
		return "", fmt.Errorf("thunder default OU has no id")
	}
	c.defaultOU = ou.ID
	return ou.ID, nil
}

// -- app lookup + pagination ---------------------------------------------

// thunderAppSummary is the flattened entry Thunder's list endpoint returns
// per application — clientId is surfaced at the top level there (Thunder
// 0.34 includes it in the list view) even though on a full GET/PUT it
// lives nested under inboundAuthConfig[0].config.clientId. Mirrors
// thundersvc's thunderApp struct.
type thunderAppSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ClientID string `json:"clientId"`
}

// findApp locates the app whose OAuth client_id equals name. EnsureApplication
// assigns client_id = DesiredApp.Name on create (see createApp), so this
// doubles as the stable per-CR lookup key across reconciles.
func (c *client) findApp(ctx context.Context, token, name string) (internalID, clientID string, err error) {
	const pageSize = 100
	const maxPages = 100
	for page := 0; page < maxPages; page++ {
		offset := page * pageSize
		apps, perr := c.listAppsPage(ctx, token, offset, pageSize)
		if perr != nil {
			return "", "", perr
		}
		for _, app := range apps {
			if app.ClientID == name {
				return app.ID, app.ClientID, nil
			}
		}
		if len(apps) < pageSize {
			return "", "", nil
		}
	}
	return "", "", fmt.Errorf("thunder list apps exceeded %d pages looking for %s", maxPages, name)
}

func (c *client) listAppsPage(ctx context.Context, token string, offset, limit int) ([]thunderAppSummary, error) {
	reqURL := fmt.Sprintf("%s/applications?offset=%d&limit=%d", c.baseURL, offset, limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("thunder list apps: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("thunder list apps returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("thunder list apps read body: %w", err)
	}

	// Thunder can return either a bare array or {"applications": [...]}.
	var apps []thunderAppSummary
	if jerr := json.Unmarshal(body, &apps); jerr != nil {
		var wrapped struct {
			Applications []thunderAppSummary `json:"applications"`
		}
		if werr := json.Unmarshal(body, &wrapped); werr != nil {
			return nil, fmt.Errorf("thunder list apps decode: %w", jerr)
		}
		apps = wrapped.Applications
	}
	return apps, nil
}

// -- EnsureApplication --------------------------------------------------------

func (c *client) EnsureApplication(ctx context.Context, app DesiredApp) (string, error) {
	if app.Name == "" {
		return "", fmt.Errorf("thunder: DesiredApp.Name is required")
	}
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return "", fmt.Errorf("getSystemToken: %w", err)
	}

	internalID, existingClientID, err := c.findApp(ctx, token, app.Name)
	if err != nil {
		return "", fmt.Errorf("findApp %q: %w", app.Name, err)
	}
	if existingClientID != "" {
		if err := c.updateApp(ctx, token, internalID, app); err != nil {
			return "", fmt.Errorf("update app %q: %w", app.Name, err)
		}
		return existingClientID, nil
	}

	ouID, err := c.getDefaultOUID(ctx, token)
	if err != nil {
		return "", fmt.Errorf("getDefaultOUID: %w", err)
	}
	clientID, err := c.createApp(ctx, token, app, ouID)
	if err != nil {
		return "", fmt.Errorf("createApp %q: %w", app.Name, err)
	}
	return clientID, nil
}

// createApp registers a new public OAuth2 client: PKCE required, no client
// secret, authorization_code + refresh_token grants. The payload's KEY NAMES
// mirror createSPAApp in services/aep-api/internal/clients/thundersvc/client.go
// exactly (values differ where documented: clientId is set to
// DesiredApp.Name — client_id equals app name, the deterministic per-CR
// lookup key — where createSPAApp used the project name).
func (c *client) createApp(ctx context.Context, token string, app DesiredApp, ouID string) (string, error) {
	uris := wireRedirectURIs(app.RedirectURIs)
	payload := map[string]any{
		"name": app.Name,
		"ouId": ouID,
		"inboundAuthConfig": []map[string]any{
			{
				"type": "oauth2",
				"config": map[string]any{
					"clientId":                app.Name,
					"redirectUris":            uris,
					"grantTypes":              []string{"authorization_code", "refresh_token"},
					"responseTypes":           []string{"code"},
					"tokenEndpointAuthMethod": "none",
					"pkceRequired":            true,
					"publicClient":            true,
				},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("thunder create app marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/applications", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("thunder create app: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("thunder create app returned %d: %s", resp.StatusCode, string(respBody))
	}

	clientID, cerr := extractClientID(respBody)
	if cerr != nil {
		// Intentionally lenient on 2xx: the app WAS created, and we asked
		// Thunder to assign clientId = app.Name — so if the create response
		// doesn't echo the clientId back in a shape we recognize, fall back
		// to what we sent rather than failing (and re-reconciling) a create
		// that already succeeded server-side.
		return app.Name, nil
	}
	return clientID, nil
}

// updateApp implements desired-state semantics: read the full app, replace
// its redirect URIs with EXACTLY the desired set (not a merge — the CR is
// the single source of truth; the placeholder stands in when the set is
// empty, see placeholderRedirectURI), then write the full object back.
func (c *client) updateApp(ctx context.Context, token, internalID string, app DesiredApp) error {
	full, err := c.getAppByID(ctx, token, internalID)
	if err != nil {
		return err
	}
	cfg, err := inboundOAuthConfig(full)
	if err != nil {
		return fmt.Errorf("app %q: %w", app.Name, err)
	}
	cfg["redirectUris"] = wireRedirectURIs(app.RedirectURIs)
	return c.putAppByID(ctx, token, internalID, full)
}

func (c *client) getAppByID(ctx context.Context, token, appID string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/applications/"+appID, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("thunder get app: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("thunder get app returned %d: %s", resp.StatusCode, string(body))
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("thunder get app decode: %w", err)
	}
	return out, nil
}

func (c *client) putAppByID(ctx context.Context, token, appID string, app map[string]any) error {
	body, err := json.Marshal(app)
	if err != nil {
		return fmt.Errorf("thunder put app marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+"/applications/"+appID, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("thunder put app: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		rb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("thunder put app returned %d: %s", resp.StatusCode, string(rb))
	}
	return nil
}

// inboundOAuthConfig returns the mutable oauth2 config map from a Thunder
// application body. It errors rather than synthesizing a shape we didn't
// read — a missing inboundAuthConfig means the app isn't the OAuth2
// public client this operator expects, and silently fabricating one would
// change the app's auth shape as a side effect of an update.
func inboundOAuthConfig(app map[string]any) (map[string]any, error) {
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
			return nil, fmt.Errorf("inboundAuthConfig[oauth2].config not an object")
		}
		return cfg, nil
	}
	return nil, fmt.Errorf("no inboundAuthConfig entry with type=oauth2")
}

// extractClientID reads the OAuth clientId out of a create-application
// response body: Thunder's own top level (if present) or the nested
// inboundAuthConfig[0].config.clientId it otherwise echoes back. Mirrors
// createSPAApp's response decoding in thundersvc.
func extractClientID(respBody []byte) (string, error) {
	var result struct {
		ClientID          string `json:"clientId"`
		InboundAuthConfig []struct {
			Config struct {
				ClientID string `json:"clientId"`
			} `json:"config"`
		} `json:"inboundAuthConfig"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("thunder create app decode: %w", err)
	}
	cid := result.ClientID
	if cid == "" && len(result.InboundAuthConfig) > 0 {
		cid = result.InboundAuthConfig[0].Config.ClientID
	}
	if cid == "" {
		return "", fmt.Errorf("thunder create app: clientId not found in response: %s", string(respBody))
	}
	return cid, nil
}

// -- DeleteApplication --------------------------------------------------------

func (c *client) DeleteApplication(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("thunder: name is required")
	}
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return fmt.Errorf("getSystemToken: %w", err)
	}
	internalID, _, err := c.findApp(ctx, token, name)
	if err != nil {
		return fmt.Errorf("findApp %q: %w", name, err)
	}
	if internalID == "" {
		// Already gone — idempotent success.
		return nil
	}
	return c.deleteApp(ctx, token, internalID)
}

func (c *client) deleteApp(ctx context.Context, token, appID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/applications/"+appID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("thunder delete app: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNotFound, http.StatusOK, http.StatusNoContent:
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("thunder delete app returned %d: %s", resp.StatusCode, string(body))
}
