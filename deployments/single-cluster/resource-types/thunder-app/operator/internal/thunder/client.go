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
// token to declare/remove per-CR OAuth applications on ONE Thunder instance
// via Thunder's /applications admin REST API. One client per instance: the
// reconciler builds and caches one per (organization, environment) from that
// environment's binding record.
//
// Wire shape: camelCase JSON keys (`ouId`, `inboundAuthConfig`, `clientId`,
// `redirectUris`, `grantTypes`, `responseTypes`, `pkceRequired`,
// `publicClient`, `tokenEndpointAuthMethod`), mirroring the live-E2E-validated
// BFF client services/aep-api/internal/clients/thundersvc/client.go and the
// local-stack bootstrap documents in deployments/single-cluster/
// thunder-resources/ (e.g. 87-aep-console-app.yaml) — the ThunderID instance
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
// derived from a ThunderApplication CR by the reconciler.
type DesiredApp struct {
	// Name is the Thunder application's unique identifier — it doubles as
	// the OAuth2 client_id assigned on create. Set to spec.clientId when
	// provided, otherwise derived as "aep-<cr-namespace>-<cr-name>".
	// EnsureApplication uses it as the lookup key across reconciles.
	Name string
	// DisplayName is the human-readable label from the CR. Carried on the
	// CR but NOT sent on the wire in v1: neither of thundersvc's create
	// functions (createApp/createSPAApp) sends a display-name/description
	// field, and the IdP's schema tolerance for extra keys is
	// unverified — the app's Thunder `name` is set to Name (the
	// deterministic per-CR identity). Task 4's live verification may wire
	// this in if Thunder accepts a separate label field.
	DisplayName string
	// Scopes is the list of scopes this client is expected to request. It is
	// written to `inboundAuthConfig[oauth2].config.scopes` on create AND on
	// update, and read back by verifyWritten.
	//
	// WHAT IT IS NOT: a gate. Measured on ThunderID 1.0.0 (spike P1 §6, three
	// escalating runs down to `["openid"]`): the field is stored faithfully and
	// read back faithfully, and has NO effect on the authorization-code flow —
	// removing a handle from the list does not remove it from the issued token,
	// and an unknown or ungranted handle is dropped silently rather than
	// rejected with `invalid_scope`. The only thing that narrows an end-user
	// token is group → role → permissions, intersected with the resource server
	// named by the request's `resource` indicator. Write-through exists so the
	// registered application is a TRUTHFUL record of what its client asks for
	// (and so it keeps working if ThunderID ever starts enforcing it) — do not
	// describe it as a second control point.
	//
	// The one place the field is load-bearing today is `system` on an m2m
	// client: it is granted through this list, not through a role, which is why
	// single-cluster/thunder-resources/81-aep-system-client.yaml declares it.
	//
	// Empty means the CR said nothing, which is NOT the same as "this client may
	// request nothing": an empty set is left alone rather than written, so the
	// operator never narrows an allowlist it was not told about.
	Scopes []string
	// ValidityPeriod is the ACCESS token lifetime in seconds, written to
	// `token.accessToken.userConfig.validityPeriod`. Zero means "not set by the
	// CR" and falls back to defaultTokenValiditySeconds.
	//
	// It is a per-app knob and not a constant because a short-lived token is the
	// only way to observe a silent renew without waiting out a day: spike P6
	// registered its fixture app at 300 s to measure that a refresh keeps the
	// audience and narrows the scope set. The ID token keeps the long default —
	// it carries the SPA's session identity, not its API authority.
	ValidityPeriod int
	// RedirectURIs is the exact set of allowed OAuth redirect URIs.
	// EnsureApplication REPLACES the app's stored redirect URIs with this
	// set on every call — it does not merge with whatever Thunder
	// currently has (desired-state semantics: the CR is the single source
	// of truth, unlike the aep-api BFF client this package's wire shapes
	// were mirrored from, which merges). An empty set is adapted on the
	// wire to a single unroutable placeholder (see placeholderRedirectURI)
	// because Thunder rejects an empty list; the field itself stays
	// honestly empty. Ignored for confidential (client_credentials) apps.
	RedirectURIs []string
	// ClientType is "public" (default) or "confidential". An empty string
	// is treated as "public" for backwards compatibility.
	ClientType string
	// ClientSecret is the pre-generated OAuth client secret for confidential
	// clients. Ignored for public (PKCE) clients.
	ClientSecret string
}

// AdminClient is the operator-local Thunder admin surface consumed by the
// reconciler (Task 3). Implementations must be safe for concurrent use.
type AdminClient interface {
	// EnsureApplication creates the app if absent (public client, PKCE
	// required, token_endpoint_auth_method=none) or updates scopes/
	// redirectUris to match (PUT is a full replace of those fields —
	// desired state, not merge). Whatever it writes it reads back: see
	// verifyWritten.
	EnsureApplication(ctx context.Context, app DesiredApp) (clientID string, err error)
	// DeleteApplication removes the app by name; absent app is success
	// (idempotent).
	DeleteApplication(ctx context.Context, name string) error
	// SetBrowserOrigins makes `origins` exactly the writable layer of this
	// instance's server-wide CORS allow-list, so the browsers running the
	// apps registered here can call its OIDC endpoints. See cors.go.
	SetBrowserOrigins(ctx context.Context, origins []string) error
}

// Config bundles the client's construction parameters.
type Config struct {
	// BaseURL is Thunder's admin API base, e.g.
	// http://platform-idp-service.platform-idp.svc.cluster.local:8090 (trailing
	// slash tolerated).
	BaseURL string
	// ClientID/ClientSecret are the operator's own system OAuth2 client
	// credentials (client_credentials grant, scope=system).
	ClientID     string
	ClientSecret string
	// SystemResourceIdentifier is the OAuth resource indicator naming
	// ThunderID's own "System" resource server — the one that owns the
	// `system` scope. Conventionally "<thunder public url>/mcp".
	//
	// Required from ThunderID 1.0.0. A client_credentials request carrying an
	// explicit scope now resolves that scope against a resource server, and
	// without a `resource` parameter it falls back to the server-wide default
	// — which this deployment sets to Agent Manager's resource server, since
	// that is what most callers want. amp-resource-server does not define
	// `system`, so the scope is dropped SILENTLY: the token endpoint returns
	// 200 with a perfectly valid token that simply has no scope claim, and
	// every admin call then 403s. Nothing in the token response says why.
	//
	// Empty means "send no resource indicator", which is the pre-1.0.0
	// behaviour and is still correct against a Thunder with no default
	// resource server configured.
	SystemResourceIdentifier string
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
		systemAud:    cfg.SystemResourceIdentifier,
		httpClient:   hc,
	}
}

type client struct {
	baseURL      string
	systemID     string
	systemSecret string
	systemAud    string
	httpClient   *http.Client

	mu          sync.Mutex
	cachedToken string
	tokenExpiry time.Time

	muOU      sync.Mutex
	defaultOU string

	// muCORS serializes the read-modify-write of the server-wide CORS config
	// (see cors.go). One client is shared by every reconcile targeting the
	// same instance, and two concurrent passes computing the origin set from
	// slightly different views of the CRs would otherwise let the loser's PUT
	// stand.
	muCORS sync.Mutex
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
	if c.systemAud != "" {
		// See Config.SystemResourceIdentifier: without this the `system`
		// scope is dropped silently and every admin call 403s.
		data.Set("resource", c.systemAud)
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
// registered under it — a Thunder instance here has a single default OU (v1
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
// per application. Thunder 0.34 uses camelCase keys throughout (list and
// create/update payloads alike).
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
	switch app.ClientType {
	case "", "public", "confidential":
		// valid
	default:
		return "", fmt.Errorf("thunder: unsupported ClientType %q — must be \"public\" or \"confidential\"", app.ClientType)
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
		if err := c.verifyWritten(ctx, token, internalID, app); err != nil {
			return "", err
		}
		return existingClientID, nil
	}

	ouID, err := c.getDefaultOUID(ctx, token)
	if err != nil {
		return "", fmt.Errorf("getDefaultOUID: %w", err)
	}
	var clientID string
	if app.ClientType == "confidential" {
		clientID, err = c.createConfidentialApp(ctx, token, app, ouID)
		if err != nil {
			return "", fmt.Errorf("createConfidentialApp %q: %w", app.Name, err)
		}
	} else {
		clientID, err = c.createApp(ctx, token, app, ouID)
		if err != nil {
			return "", fmt.Errorf("createApp %q: %w", app.Name, err)
		}
	}
	if !needsVerify(app) {
		return clientID, nil
	}
	// The create response does not carry the internal id in a shape this
	// client parses, so the app is looked up again to read it back.
	newInternalID, _, err := c.findApp(ctx, token, app.Name)
	if err != nil {
		return "", fmt.Errorf("findApp after create %q: %w", app.Name, err)
	}
	if err := c.verifyWritten(ctx, token, newInternalID, app); err != nil {
		return "", err
	}
	return clientID, nil
}

// needsVerify reports whether this app has anything for verifyWritten to diff —
// it spares the create path a list + get round trip for an m2m client that
// declared no scopes, which is every confidential app before phase 2.
func needsVerify(app DesiredApp) bool {
	return app.ClientType != "confidential" || len(app.Scopes) > 0
}

// verifyWritten reads the application back ONCE and diffs what the IdP actually
// stored against what this reconcile asked for. Two checks ride on that read:
//
//   - the scope allowlist, for every client type (see DesiredApp.Scopes);
//   - the identity-claim contract, for browser apps only — a confidential client
//     here is an m2m one, whose token has no user behind it and therefore no
//     user attributes to release.
//
// Reading back is the whole defence on this API: ThunderID answers 200 to a
// payload it only partly recognises and keeps the rest to itself, so "the write
// succeeded" says nothing about what the application now is.
func (c *client) verifyWritten(ctx context.Context, token, internalID string, app DesiredApp) error {
	if !needsVerify(app) {
		return nil
	}
	browser := app.ClientType != "confidential"
	if internalID == "" {
		return fmt.Errorf("verify application %q: the IdP returned no application id to read back", app.Name)
	}
	stored, err := c.getAppByID(ctx, token, internalID)
	if err != nil {
		return fmt.Errorf("verify application %q: %w", app.Name, err)
	}
	cfg, err := inboundOAuthConfig(stored)
	if err != nil {
		return fmt.Errorf("verify application %q: %w", app.Name, err)
	}
	if err := verifyScopes(cfg, app); err != nil {
		return err
	}
	if !browser {
		return nil
	}
	return verifyIdentityClaims(cfg, app.Name)
}

// verifyScopes fails when the scope allowlist did not survive the write.
//
// This does not make the allowlist a gate — ThunderID 1.0.0 never enforces it
// (see DesiredApp.Scopes). It makes it HONEST: the point of writing the list is
// that the registered application describes the client truthfully, and a list
// the server quietly discarded describes nothing.
func verifyScopes(cfg map[string]any, app DesiredApp) error {
	if len(app.Scopes) == 0 {
		return nil
	}
	missing := missingScopes(app.Scopes, configuredScopes(cfg))
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf(
		"application %q was written but the IdP did not store its scope allowlist (missing %v) — "+
			"inboundAuthConfig[oauth2].config.scopes is a JSON array of strings; a rejected shape "+
			"is accepted with 200 and dropped",
		app.Name, missing)
}

// createApp registers a new public OAuth2 client: PKCE required, no client
// -- identity-claim contract -------------------------------------------------
//
// Every provisioned OAuth app carries the SAME platform identity claims so
// end-user tokens are role-aware. The authoritative copy of this contract is
// the platform console's bootstrap document,
// deployments/single-cluster/thunder-resources/87-aep-console-app.yaml, and
// identity_contract_test.go asserts this file still agrees with it. It is a
// platform-wide contract, not per-app config — hence a constant here rather
// than a ThunderApplication CR field.
//
//   - identityUserAttributes must land in BOTH tokens, but they are declared
//     at DIFFERENT paths (see tokenClaimConfig). `groups` drives role-based UI
//     and, through the gateway's claim mapping, the API's authorization; `ou*`
//     identifies the org; the rest are profile.
//   - scopeClaimConfig gates which claims Thunder releases per requested scope.
//     `group → [groups]` is load-bearing: verified on Thunder 0.34 that
//     `groups` reaches the ID TOKEN only when the `group` scope is granted
//     (the thunder-app resource type's `scopes` default requests it). Without
//     it, groups appears only in the access token and the SPA (reading the
//     id_token) sees no role.
//   - allowedUserTypes lets org (Person) users authenticate.

// Application types accepted by ThunderID's /applications API. The full enum
// is browser / fullstack / mobile / m2m / mcp / custom; these are the two
// shapes this operator creates. The field is REQUIRED from 1.0.0 — omitting it
// fails the create with APP-1042 — and did not exist in Thunder 0.34.
const (
	appTypeBrowser = "browser"
	appTypeM2M     = "m2m"
)

var (
	identityUserAttributes = []string{
		"given_name", "family_name", "username", "groups",
		"email", "name", "ouId", "ouName", "ouHandle",
	}
	allowedUserTypes = []string{"Person"}
)

// defaultTokenValiditySeconds is the access/id token lifetime (24h) used when
// the CR names none. Thunder's default is short; a SPA whose token expires
// quickly is forced back through a full sign-in redirect (or a silent renew)
// far too often, so pin a long-lived token — the same 86400 the seeded Console
// app uses. The SPA still refreshes via the refresh_token grant; this just
// keeps a returning user signed in across a refresh/new tab without a
// round-trip.
//
// DesiredApp.ValidityPeriod overrides it for the ACCESS token only.
const defaultTokenValiditySeconds = 86400

// tokenClaimConfig returns the oauth2 `token` block for an app whose access
// token lives accessValidity seconds (0 → defaultTokenValiditySeconds). A fresh
// map per call so each app payload owns its copy.
//
// The two tokens declare their attributes at DIFFERENT paths, and getting this
// wrong costs a day:
//
//	idToken     validityPeriod + userAttributes          (top level)
//	accessToken userConfig{ validityPeriod, attributes } (nested, and the key
//	                                                      is `attributes`)
//
// ThunderID 1.0.0 answers 200 to either shape and silently keeps only what it
// recognises. Sending the id-token shape for the access token — which this
// file did until 2026-09-06 — produces an app whose ACCESS token carries no
// `groups`, while its ID token carries all of them. The SPA signs in happily
// and renders the user's role from the id_token; every call it then makes with
// the access token reaches the gateway, whose `groups -> X-User-Groups` claim
// mapping injects nothing, and the API answers 403 "caller has no recognized
// role". Nothing anywhere reports a dropped claim.
//
// `userConfig` mirrors the `clientConfig` an m2m app uses for the same purpose
// (services/aep-api/internal/clients/thundersvc/client.go): per-audience token
// config, attributes under it. verifyIdentityClaims reads the app back after
// every write precisely because neither shape is rejected.
func tokenClaimConfig(accessValidity int) map[string]any {
	if accessValidity <= 0 {
		accessValidity = defaultTokenValiditySeconds
	}
	return map[string]any{
		"accessToken": map[string]any{
			"userConfig": map[string]any{
				"validityPeriod": accessValidity,
				"attributes":     append([]string(nil), identityUserAttributes...),
			},
		},
		"idToken": map[string]any{
			// Deliberately NOT accessValidity: the ID token carries the SPA's
			// session identity, and shortening it would force a full sign-in
			// redirect on an app whose only reason for a short access token is
			// to exercise the silent renew.
			"validityPeriod": defaultTokenValiditySeconds,
			"userAttributes": append([]string(nil), identityUserAttributes...),
		},
	}
}

// accessTokenAttributes / idTokenAttributes read the identity attributes back
// out of an application body, at the two paths tokenClaimConfig writes them.
// They return nil when the path is absent, which is exactly what a silent drop
// looks like.
func accessTokenAttributes(cfg map[string]any) []string {
	token, _ := cfg["token"].(map[string]any)
	access, _ := token["accessToken"].(map[string]any)
	userCfg, _ := access["userConfig"].(map[string]any)
	return stringsOf(userCfg["attributes"])
}

// configuredScopes reads the client's scope allowlist back out of an
// application body, at the path wireScopes writes it. nil when absent, which is
// what a silent drop looks like.
func configuredScopes(cfg map[string]any) []string {
	return stringsOf(cfg["scopes"])
}

func idTokenAttributes(cfg map[string]any) []string {
	token, _ := cfg["token"].(map[string]any)
	id, _ := token["idToken"].(map[string]any)
	return stringsOf(id["userAttributes"])
}

// stringsOf accepts what encoding/json produces for a JSON array of strings.
func stringsOf(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// wireScopes adapts the desired scope list for Thunder's wire. ThunderID's
// application contract types `inboundAuthConfig[oauth2].config.scopes` as a
// JSON ARRAY of strings (spike P1 §4; the space-joined form is the CRT
// parameter's, and the reconciler splits it) — sending the joined string would
// register one scope literally named "openid profile email …".
func wireScopes(desired []string) []any {
	out := make([]any, 0, len(desired))
	for _, s := range desired {
		out = append(out, s)
	}
	return out
}

// setWireScopes writes the desired allowlist onto an oauth2 config, and leaves
// whatever is stored alone when the CR named no scopes — see DesiredApp.Scopes
// for why silence is not the same as an empty allowlist.
func setWireScopes(cfg map[string]any, desired []string) {
	if len(desired) == 0 {
		return
	}
	cfg["scopes"] = wireScopes(desired)
}

// missingScopes returns the desired scopes absent from `have`, preserving the
// desired order so the message is stable.
//
// A SUBSET check, not equality: ThunderID normalises on write (measured on
// 1.0.0 — `scopeClaims.email` sent as ["email","email_verified"] comes back as
// ["email"]), so a read-back that carries more than was sent is the server
// having its own opinion, not a failure. What must never happen silently is a
// scope we asked for going missing.
func missingScopes(want, have []string) []string {
	present := make(map[string]struct{}, len(have))
	for _, s := range have {
		present[s] = struct{}{}
	}
	var missing []string
	for _, w := range want {
		if _, ok := present[w]; !ok {
			missing = append(missing, w)
		}
	}
	return missing
}

// missingIdentityAttributes returns the identity attributes absent from
// `have`, preserving the contract's order so the message is stable.
func missingIdentityAttributes(have []string) []string {
	present := make(map[string]struct{}, len(have))
	for _, a := range have {
		present[a] = struct{}{}
	}
	var missing []string
	for _, want := range identityUserAttributes {
		if _, ok := present[want]; !ok {
			missing = append(missing, want)
		}
	}
	return missing
}

// verifyIdentityClaims fails when the identity attributes did not survive the
// write. cfg is the READ-BACK oauth2 config (see verifyWritten), never the body
// that was sent.
//
// This is not defensive coding for its own sake. ThunderID accepts an
// application payload with 200 and keeps only the fields it recognises, so a
// contract change on its side — or a shape mistake on ours — is indistinguishable
// from success at the point of writing. The whole cost of that silence lands on
// a deployed app, hours later, as a 403 with no mention of a claim anywhere.
// Reading back turns it into a ThunderApplication that refuses to go ready and
// names the attributes that vanished.
func verifyIdentityClaims(cfg map[string]any, name string) error {
	accessMissing := missingIdentityAttributes(accessTokenAttributes(cfg))
	idMissing := missingIdentityAttributes(idTokenAttributes(cfg))
	if len(accessMissing) == 0 && len(idMissing) == 0 {
		return nil
	}
	return fmt.Errorf(
		"application %q was written but the IdP did not store its identity claims "+
			"(access token missing %v, id token missing %v) — ThunderID accepts an "+
			"unrecognised token config with 200 and drops it, so this is a wire-shape "+
			"mismatch, not an outage",
		name, accessMissing, idMissing)
}

// scopeClaimConfig maps OIDC scopes to the claims Thunder releases when that
// scope is granted. A fresh map per call.
func scopeClaimConfig() map[string]any {
	return map[string]any{
		"profile": []string{"name", "given_name", "family_name", "picture"},
		"email":   []string{"email", "email_verified"},
		"group":   []string{"groups"},
		"ou":      []string{"ouId", "ouName", "ouHandle"},
	}
}

// secret, authorization_code + refresh_token grants. The payload's KEY NAMES
// mirror createSPAApp in services/aep-api/internal/clients/thundersvc/client.go
// exactly (values differ where documented: clientId is set to
// DesiredApp.Name — client_id equals app name, the deterministic per-CR
// lookup key — where createSPAApp used the project name).
func (c *client) createApp(ctx context.Context, token string, app DesiredApp, ouID string) (string, error) {
	uris := wireRedirectURIs(app.RedirectURIs)
	oauthConfig := map[string]any{
		"clientId":                app.Name,
		"redirectUris":            uris,
		"grantTypes":              []string{"authorization_code", "refresh_token"},
		"responseTypes":           []string{"code"},
		"tokenEndpointAuthMethod": "none",
		"pkceRequired":            true,
		"publicClient":            true,
		"token":                   tokenClaimConfig(app.ValidityPeriod),
		"scopeClaims":             scopeClaimConfig(),
	}
	setWireScopes(oauthConfig, app.Scopes)
	payload := map[string]any{
		"name": app.Name,
		// REQUIRED from ThunderID 1.0.0 (APP-1042 without it); Thunder 0.34's
		// application API had no such field. The enum is browser / fullstack /
		// mobile / m2m / mcp / custom — "browser" is the public-PKCE
		// redirect client this function creates.
		"type":             appTypeBrowser,
		"ouId":             ouID,
		"allowedUserTypes": allowedUserTypes,
		"inboundAuthConfig": []map[string]any{
			{"type": "oauth2", "config": oauthConfig},
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

// createConfidentialApp registers a new confidential OAuth2 client using the
// client_credentials grant. No PKCE, no redirect URIs — the caller supplies
// the pre-generated secret via app.ClientSecret.
func (c *client) createConfidentialApp(ctx context.Context, token string, app DesiredApp, ouID string) (string, error) {
	oauthConfig := map[string]any{
		"clientId":                app.Name,
		"clientSecret":            app.ClientSecret,
		"grantTypes":              []string{"client_credentials"},
		"tokenEndpointAuthMethod": "client_secret_post",
		"pkceRequired":            false,
		"publicClient":            false,
		"token":                   tokenClaimConfig(app.ValidityPeriod),
	}
	setWireScopes(oauthConfig, app.Scopes)
	payload := map[string]any{
		"name": app.Name,
		// See createApp: required from ThunderID 1.0.0. "m2m" is the
		// client_credentials-only shape this function creates — no interactive
		// login, no redirect URIs.
		"type":             appTypeM2M,
		"ouId":             ouID,
		"allowedUserTypes": allowedUserTypes,
		"inboundAuthConfig": []map[string]any{
			{"type": "oauth2", "config": oauthConfig},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("thunder create confidential app marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/applications", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("thunder create confidential app: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("thunder create confidential app returned %d: %s", resp.StatusCode, string(respBody))
	}

	clientID, cerr := extractClientID(respBody)
	if cerr != nil {
		return app.Name, nil
	}
	return clientID, nil
}

// updateApp implements desired-state semantics: read the full app, re-assert
// the platform identity-claim contract, then write the full object back.
// For public clients, redirect URIs are also replaced (desired-state). For
// confidential clients, redirect URIs are left untouched (they have none).
//
// The claim contract (token userAttributes + scopeClaims + allowedUserTypes)
// is re-applied here — not only on create — so apps provisioned before this
// contract existed self-heal on their next reconcile, rather than staying bare
// (which yields end-user tokens with no `groups` claim → no role in the SPA).
func (c *client) updateApp(ctx context.Context, token, internalID string, app DesiredApp) error {
	full, err := c.getAppByID(ctx, token, internalID)
	if err != nil {
		return err
	}
	cfg, err := inboundOAuthConfig(full)
	if err != nil {
		return fmt.Errorf("app %q: %w", app.Name, err)
	}
	if app.ClientType != "confidential" {
		cfg["redirectUris"] = wireRedirectURIs(app.RedirectURIs)
	} else if app.ClientSecret != "" {
		// Enforce the desired secret so Thunder's stored value always matches
		// the one provisioned in OpenBao → K8s Secret. Without this, a prior
		// bootstrap (a thunder-resources/ document's literal clientSecret) may
		// have set a different hardcoded secret, causing 401s for services
		// reading the OpenBao-provisioned value.
		cfg["clientSecret"] = app.ClientSecret
	}
	cfg["token"] = tokenClaimConfig(app.ValidityPeriod)
	cfg["scopeClaims"] = scopeClaimConfig()
	setWireScopes(cfg, app.Scopes)
	full["allowedUserTypes"] = allowedUserTypes
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
