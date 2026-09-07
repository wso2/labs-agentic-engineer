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

// Package thundersvc is the BFF-side Thunder admin client. It mints
// `scope=system` access tokens via client_credentials against a Thunder
// OAuth app (aep-system-client) that has the Administrator role
// assigned, then uses those tokens to manage per-org publisher OAuth
// apps via Thunder's /applications endpoint.
//
// Every token request names the System resource server as its `resource`
// indicator — see Config.SystemResourceIdentifier for why a request without
// one gets a valid-looking token that every admin endpoint rejects.
//
// See docs/design/api-platform-integration.md §6.
//
//   - App naming convention: `aep-publisher-<orgHandle>`.
//   - There is no OU UUID argument — every BFF caller passes the OC org
//     handle and we look up the matching Thunder OU once at first call
//     (cached on the client).
//
// The system token has a TTL (Thunder default ~1h); the cache uses a
// 30 s skew so concurrent callers don't all hit the slow path right
// before expiry. singleflight deduplicates the slow path itself.
package thundersvc

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Client is the public surface of the Thunder admin client. All methods
// are idempotent — repeating an EnsurePublisherApp call returns the
// existing client_id without creating a duplicate; repeating Delete
// returns false when the app is gone.
type Client interface {
	// EnsurePublisherApp creates an OAuth2 app named
	// "aep-publisher-{orgHandle}" if it doesn't already exist.
	// Returns the clientId and (on creation only) the clientSecret —
	// Thunder doesn't expose the secret on subsequent reads, so callers
	// MUST persist it to OpenBao on the `created=true` branch. When
	// `created=false`, clientSecret is empty — the caller should look
	// it up in their secret store. When the secret was lost (e.g.
	// OpenBao was wiped), use RegenerateClientSecret to issue a new
	// one.
	//
	// orgOUID is the org's Thunder OU id (the JWT `ouId`). The app is
	// registered under that OU so its client_credentials token carries
	// `ouHandle == orgHandle` — the publisher-token verifier's cross-org
	// check requires this. When orgOUID is empty the default OU is used
	// (single-org / local dev), which only matches when the default OU
	// is the org's OU.
	EnsurePublisherApp(ctx context.Context, orgHandle, orgOUID string) (clientID, clientSecret string, created bool, err error)

	// OUExists reports whether an organization unit with the given id exists
	// in Thunder (GET /organization-units/{id} → 200 exists / 404 absent). It
	// lets callers validate a JWT-supplied `ouId` before trusting it: a
	// stale/phantom ouId must not poison the org→OU mapping (wc- namespace,
	// impersonation) nor trigger a destructive publisher re-registration under
	// a non-existent OU (Thunder 400 APP-1018 → runner cc-token invalid_client).
	OUExists(ctx context.Context, ouID string) (bool, error)

	// DeletePublisherApp deletes the publisher app for the given org.
	// Returns true when the app existed and was deleted, false when it
	// didn't exist (idempotent — both states are success).
	DeletePublisherApp(ctx context.Context, orgHandle string) (bool, error)

	// RegenerateClientSecret issues a fresh client_secret for the
	// existing publisher app. Returns the new secret. The caller MUST
	// rotate it into OpenBao + redeploy any consumer pods that mounted
	// the old value.
	RegenerateClientSecret(ctx context.Context, orgHandle string) (string, error)

	// -- directory (groups + users) --------------------------------------
	//
	// The build-time roles ensure makes a project's declared Roles and Test
	// users real through these. Everything lands in the DEFAULT OU, the same
	// one the thunder-app operator registers each generated app's OAuth
	// client under — a user in another OU is not a user of the app.
	// directory.go carries the Thunder constraints each method works around.

	// ListGroups returns every group in the default OU — the Role catalog the
	// design agent reads before inventing a name.
	ListGroups(ctx context.Context) ([]Group, error)

	// FindGroupByName returns the group with this name (case-insensitive),
	// and whether one exists.
	FindGroupByName(ctx context.Context, name string) (*Group, bool, error)

	// GroupMembers returns the user ids in a group.
	GroupMembers(ctx context.Context, groupID string) ([]string, error)

	// CreateGroup creates a group with exactly these members. It is the only
	// call that can set membership — Thunder ignores `members` on update.
	CreateGroup(ctx context.Context, name, description string, memberIDs []string) (Group, error)

	// AddGroupMembers adds members while preserving the ones already there,
	// reading and writing under one lock. Destructive by construction — the
	// write is a delete-and-recreate — so pass only a group the platform
	// created. Returns the group's NEW id when members were added.
	//
	// Members already in the group that no longer exist on the directory are
	// dropped rather than replayed; an account the CALLER asks to add that does
	// not exist is an error raised before anything is written. Either way the
	// group survives the call — see rewriteGroupMembers in directory.go.
	AddGroupMembers(ctx context.Context, group Group, memberIDs []string) (Group, error)

	// RemoveGroupMembers takes members out of a group, keeping everyone else.
	// The un-enrol half of the pair: deleting an account without calling this
	// first leaves the group pointing at an id that no longer resolves.
	// Returns the group's NEW id when members were removed.
	RemoveGroupMembers(ctx context.Context, group Group, memberIDs []string) (Group, error)

	// UserGroups returns the groups an account belongs to. An account that is
	// already gone reports no groups rather than an error.
	UserGroups(ctx context.Context, userID string) ([]Group, error)

	// FindUserByUsername returns the account with this exact username, and
	// whether one exists.
	FindUserByUsername(ctx context.Context, username string) (*DirectoryUser, bool, error)

	// CreateUser creates a person-type account. The password is write-only
	// afterwards, so the caller must seal its own copy.
	CreateUser(ctx context.Context, username, email, password string) (DirectoryUser, error)

	// SetUserPassword rotates an account's password.
	SetUserPassword(ctx context.Context, userID, password string) error

	// DeleteUser removes an account. Idempotent.
	DeleteUser(ctx context.Context, userID string) error
}

// Config bundles the construction params — a struct rather than
// positional args, which get unwieldy once the list grows past two.
type Config struct {
	BaseURL      string // e.g. http://thunder.openchoreo.localhost:8080
	ClientID     string // OAuth2 client id of the system app
	ClientSecret string // OAuth2 client secret of the system app
	// SystemResourceIdentifier is the OAuth resource indicator naming the
	// resource server that owns the `system` scope: ThunderID's own "System"
	// resource server, conventionally "<public issuer>/mcp" (see the
	// SystemResourceIdentifier function). Sent as `resource` on every token
	// request.
	//
	// ThunderID 1.0.0 resolves a requested scope against a resource server.
	// Without an indicator it uses the server-wide default, and when that
	// default does not define `system` the scope is dropped SILENTLY: the
	// token endpoint answers 200 with a scope-less token and every admin call
	// then 403s, with nothing in either response saying why. Empty sends no
	// indicator, which is only right against a Thunder with no default
	// resource server. fetchSystemToken refuses a scope-less token either
	// way, so the failure is named at mint time rather than as a bare 403.
	SystemResourceIdentifier string
	// HTTPClient — optional override (tests inject a recording client).
	// Defaults to a 30 s-timeout net/http client.
	HTTPClient *http.Client
}

type client struct {
	baseURL    string
	systemID   string
	systemSec  string
	systemAud  string
	httpClient *http.Client

	mu          sync.RWMutex
	cachedToken string
	tokenExpiry time.Time
	tokenSfg    singleflight.Group

	// Default OU id, looked up once on first EnsurePublisherApp call
	// and cached. Thunder's UI nests every org under a root OU
	// ("default") so for v1 we always use that.
	muOU      sync.Mutex
	defaultOU string

	// Per-group-name write serialisation for the directory surface —
	// group membership has no atomic add, so a fan-out must not have two
	// goroutines read-then-write the same group. See lockGroup.
	groupLocks struct {
		mu     sync.Mutex
		byName map[string]*sync.Mutex
	}
}

// New builds a Thunder admin client.
func New(cfg Config) Client {
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &client{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		systemID:   cfg.ClientID,
		systemSec:  cfg.ClientSecret,
		systemAud:  cfg.SystemResourceIdentifier,
		httpClient: hc,
	}
}

// PublisherAppName is the canonical naming function — exposed for the
// idp_service so tests can assert names without re-deriving the prefix.
func PublisherAppName(orgHandle string) string {
	return "aep-publisher-" + orgHandle
}

// SystemResourceIdentifier derives the System resource server's identifier
// from an identity provider's public issuer, following ThunderID's own
// convention ("<publicUrl>/mcp" — its native console derives the same string
// from configuration.server.publicUrl). The identifier is a name, not an
// address: the public issuer is the right input even when admin calls go to an
// in-cluster URL. Empty in, empty out.
func SystemResourceIdentifier(issuer string) string {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	if issuer == "" {
		return ""
	}
	return issuer + "/mcp"
}

// -- system token --------------------------------------------------------

// getSystemToken returns a cached system token or fetches a new one.
// Fast path: RLock + cache hit. Slow path: singleflight dedupe so
// concurrent callers share one round-trip.
func (c *client) getSystemToken(ctx context.Context) (string, error) {
	c.mu.RLock()
	if c.cachedToken != "" && time.Now().Before(c.tokenExpiry) {
		token := c.cachedToken
		c.mu.RUnlock()
		return token, nil
	}
	c.mu.RUnlock()

	result, err, _ := c.tokenSfg.Do("system-token", func() (any, error) {
		c.mu.RLock()
		if c.cachedToken != "" && time.Now().Before(c.tokenExpiry) {
			token := c.cachedToken
			c.mu.RUnlock()
			return token, nil
		}
		c.mu.RUnlock()

		token, expiresIn, err := c.fetchSystemToken(ctx)
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		c.cachedToken = token
		const skew = 30
		if expiresIn > skew {
			c.tokenExpiry = time.Now().Add(time.Duration(expiresIn-skew) * time.Second)
		} else if expiresIn > 0 {
			c.tokenExpiry = time.Now().Add(time.Duration(expiresIn) * time.Second)
		} else {
			c.tokenExpiry = time.Now().Add(time.Minute)
		}
		c.mu.Unlock()
		return token, nil
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

func (c *client) fetchSystemToken(ctx context.Context) (string, int, error) {
	// The system client is registered with
	// `tokenEndpointAuthMethod: client_secret_post` (see
	// single-cluster/thunder-resources/81-aep-system-client.yaml), so
	// client_id + client_secret go in the form body — NOT in a Basic auth
	// header.
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"scope":         {"system"},
		"client_id":     {c.systemID},
		"client_secret": {c.systemSec},
	}
	if c.systemAud != "" {
		// See Config.SystemResourceIdentifier: this is what keeps the
		// `system` scope from being resolved against the wrong resource
		// server and dropped.
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
	if err := assertSystemScope(result.AccessToken, c.systemAud); err != nil {
		return "", 0, err
	}
	return result.AccessToken, result.ExpiresIn, nil
}

// assertSystemScope refuses a token that does not carry the `system` scope.
// Thunder drops an unresolvable scope without an error, so the token itself is
// the only place the failure can be named before it shows up as a 403 on some
// unrelated admin call. An opaque (non-JWT) token is let through: the check
// exists to explain one known trap, not to act as a second authoriser.
func assertSystemScope(token, resource string) error {
	scopes, ok := tokenScopes(token)
	if !ok {
		return nil
	}
	for _, s := range scopes {
		if s == "system" {
			return nil
		}
	}
	return fmt.Errorf("thunder issued a token without the system scope (resource indicator %q): "+
		"the scope was resolved against a resource server that does not define it and dropped; "+
		"the indicator must name the identity provider's System resource server (<public issuer>/mcp)", resource)
}

// tokenScopes reads the scope claim of a JWT without verifying it — the token
// was just handed over by the issuer on the token endpoint, so there is nothing
// to verify it against and no decision rides on it beyond an error message. ok
// is false when the token is not a JWT or its payload is not JSON; an absent
// claim is a readable answer (no scopes), not an unreadable token.
func tokenScopes(token string) ([]string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil, false
	}
	var claims struct {
		Scope any `json:"scope"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, false
	}
	switch v := claims.Scope.(type) {
	case nil:
		return nil, true
	case string:
		return strings.Fields(v), true
	case []any:
		out := make([]string, 0, len(v))
		for _, s := range v {
			if str, ok := s.(string); ok {
				out = append(out, str)
			}
		}
		return out, true
	}
	return nil, false
}

// -- OU resolution --------------------------------------------------------

// getDefaultOUID returns Thunder's default organisation-unit id,
// cached after first successful lookup.
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

// OUExists reports whether the given OU id exists in Thunder. It mints a
// system token then delegates to ouExists.
func (c *client) OUExists(ctx context.Context, ouID string) (bool, error) {
	if ouID == "" {
		return false, nil
	}
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return false, fmt.Errorf("getSystemToken: %w", err)
	}
	return c.ouExists(ctx, token, ouID)
}

// ouExists issues GET /organization-units/{id} with an existing system token:
// 200 → exists, 404 → absent, anything else → error.
func (c *client) ouExists(ctx context.Context, token, ouID string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/organization-units/"+ouID, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("thunder get OU %s: %w", ouID, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	}
	body, _ := io.ReadAll(resp.Body)
	return false, fmt.Errorf("thunder get OU %s returned %d: %s", ouID, resp.StatusCode, string(body))
}

// -- EnsurePublisherApp ---------------------------------------------------

func (c *client) EnsurePublisherApp(ctx context.Context, orgHandle, orgOUID string) (string, string, bool, error) {
	if orgHandle == "" {
		return "", "", false, fmt.Errorf("orgHandle required")
	}
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return "", "", false, fmt.Errorf("getSystemToken: %w", err)
	}
	appName := PublisherAppName(orgHandle)

	internalID, existingClientID, err := c.findApp(ctx, token, appName)
	if err != nil {
		return "", "", false, fmt.Errorf("findApp %q: %w", appName, err)
	}
	if existingClientID != "" {
		// The app exists. Self-heal its token claims first: an app registered
		// before the token config named ouId/ouHandle mints tokens the
		// publisher-token verifier refuses ("ouHandle claim missing"), and
		// the runner reports "mcp auth: unauthorized". The PUT keeps the
		// client secret (ThunderID leaves an omitted secret untouched), so
		// nothing has to be re-mirrored.
		if err := c.ensurePublisherTokenClaims(ctx, token, internalID); err != nil {
			return "", "", false, fmt.Errorf("publisher token claims %q: %w", appName, err)
		}
		// Then its OU: an app created under the wrong
		// OU (e.g. the default OU, before this code registered under the
		// org OU) issues cc tokens whose `ouHandle` won't match the org, so
		// the publisher-token verifier rejects the runner. When we know the
		// org OU and the existing app sits under a different one, delete +
		// recreate it under the org OU. The client_id is deterministic
		// (= app name) so it's unchanged; only the secret rotates, which
		// the caller re-mirrors on the created=true branch.
		if orgOUID == "" {
			slog.WarnContext(ctx, "publisher app OU not verified — org OU unknown (falling back), token ouHandle may not match",
				"appName", appName)
			return existingClientID, "", false, nil
		}
		currentOU, ouErr := c.appOUID(ctx, token, internalID)
		if ouErr != nil {
			// Don't take a destructive heal on an uncertain read; log and
			// keep the existing app so dispatch isn't blocked.
			slog.WarnContext(ctx, "publisher app OU read failed — skipping OU self-heal",
				"appName", appName, "appID", internalID, "error", ouErr)
			return existingClientID, "", false, nil
		}
		if currentOU == "" || currentOU == orgOUID {
			slog.DebugContext(ctx, "publisher app already under correct OU",
				"appName", appName, "ouID", currentOU)
			return existingClientID, "", false, nil
		}
		// Before a DESTRUCTIVE delete+recreate, confirm the target OU actually
		// exists in Thunder. A stale/phantom orgOUID (a JWT carrying an OU that
		// no longer exists) must NOT tear down a working publisher app only to
		// fail recreating it under a non-existent OU (Thunder 400 APP-1018) —
		// that strands the org with no publisher and surfaces downstream as the
		// runner's cc-token `invalid_client` (401). Keep the existing app: its
		// currentOU is real, so its cc token's ouHandle is still valid.
		if exists, exErr := c.ouExists(ctx, token, orgOUID); exErr != nil {
			slog.WarnContext(ctx, "publisher OU self-heal: could not verify the resolved org OU exists — keeping existing app, skipping destructive heal",
				"appName", appName, "appID", internalID, "currentOU", currentOU, "orgOU", orgOUID, "error", exErr)
			return existingClientID, "", false, nil
		} else if !exists {
			slog.ErrorContext(ctx, "publisher OU self-heal: resolved org OU does NOT exist in Thunder — REFUSING destructive re-registration; keeping existing app under its (valid) current OU. The org's thunder_org_uuid is a stale/phantom ouId — fix the org→OU mapping.",
				"appName", appName, "appID", internalID, "currentOU", currentOU, "phantomOrgOU", orgOUID)
			return existingClientID, "", false, nil
		}
		slog.InfoContext(ctx, "publisher app under wrong OU — re-registering under org OU",
			"appName", appName, "appID", internalID, "currentOU", currentOU, "orgOU", orgOUID)
		if _, derr := c.deleteApp(ctx, token, internalID); derr != nil {
			// Thunder has been observed to return 5xx (SSE-5000) on delete
			// even when the app was actually removed server-side. Don't abort
			// the heal on that — re-check by name and, if the app is gone,
			// continue to recreate so the heal completes in a single dispatch
			// (otherwise the org is left with no publisher app until the next
			// run). Only a genuinely-still-present app is a hard failure.
			if _, stillID, ferr := c.findApp(ctx, token, appName); ferr != nil || stillID != "" {
				return "", "", false, fmt.Errorf("heal publisher OU: delete %q (id=%s): %w", appName, internalID, derr)
			}
			slog.WarnContext(ctx, "publisher app delete returned an error but the app is gone — continuing to recreate",
				"appName", appName, "appID", internalID, "deleteErr", derr)
		}
		id, secret, cerr := c.createApp(ctx, token, appName, orgOUID)
		if cerr != nil {
			// The old app is gone; the next dispatch re-enters the create
			// path below and provisions fresh. Surface the error loudly.
			return "", "", false, fmt.Errorf("heal publisher OU: recreate %q under OU %s: %w", appName, orgOUID, cerr)
		}
		slog.InfoContext(ctx, "publisher app re-registered under org OU (secret rotated)",
			"appName", appName, "orgOU", orgOUID)
		return id, secret, true, nil
	}

	// Register the app under the org's own OU so the cc token's `ouHandle`
	// resolves to the org handle (the verifier's cross-org check). Fall
	// back to the default OU only when the org OU is unknown.
	ouID := orgOUID
	if ouID == "" {
		ouID, err = c.getDefaultOUID(ctx, token)
		if err != nil {
			return "", "", false, fmt.Errorf("getDefaultOUID: %w", err)
		}
		slog.WarnContext(ctx, "creating publisher app under DEFAULT OU — org OU unknown; token ouHandle may not match org",
			"appName", appName, "defaultOU", ouID)
	}

	id, secret, err := c.createApp(ctx, token, appName, ouID)
	if err != nil {
		return "", "", false, fmt.Errorf("createApp %q under OU %s: %w", appName, ouID, err)
	}
	slog.InfoContext(ctx, "publisher app created", "appName", appName, "ouID", ouID, "underOrgOU", orgOUID != "")
	return id, secret, true, nil
}

// appOUID reads the OU id an existing Thunder application is registered
// under, so EnsurePublisherApp can detect a wrong-OU app and heal it.
// Returns "" (not an error) when the OU can't be determined from the app
// body — callers treat that as "don't risk a destructive heal".
// ensurePublisherTokenClaims makes sure the app's client_credentials tokens
// carry publisherTokenAttributes, adding the token config with a PUT when they
// do not. Read-check-write: an app that already declares both attributes is
// left alone (no PUT, no churn). The PUT sends the app as read, minus its id,
// with the client config set — the same shape regenerateSecret uses — and
// ThunderID keeps the existing client secret when the payload carries none.
func (c *client) ensurePublisherTokenClaims(ctx context.Context, token, appID string) error {
	app, err := c.getAppByID(ctx, token, appID)
	if err != nil {
		return fmt.Errorf("read app: %w", err)
	}
	cfg := inboundOAuthConfig(app)
	if cfg == nil {
		return fmt.Errorf("app %s has no oauth2 inboundAuthConfig", appID)
	}
	if hasPublisherTokenClaims(cfg) {
		return nil
	}

	tokenCfg, _ := cfg["token"].(map[string]any)
	if tokenCfg == nil {
		tokenCfg = map[string]any{}
		cfg["token"] = tokenCfg
	}
	accessCfg, _ := tokenCfg["accessToken"].(map[string]any)
	if accessCfg == nil {
		accessCfg = map[string]any{}
		tokenCfg["accessToken"] = accessCfg
	}
	accessCfg["clientConfig"] = publisherTokenClientConfig()
	delete(app, "id")

	body, _ := json.Marshal(app)
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
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("thunder put app returned %d: %s", resp.StatusCode, string(respBody))
	}
	slog.InfoContext(ctx, "Thunder publisher token claims added", "appID", appID, "attributes", publisherTokenAttributes)
	return nil
}

// inboundOAuthConfig returns the app's first oauth2 inboundAuthConfig entry's
// config map, or nil.
func inboundOAuthConfig(app map[string]any) map[string]any {
	entries, _ := app["inboundAuthConfig"].([]any)
	for _, e := range entries {
		entry, _ := e.(map[string]any)
		if entry == nil {
			continue
		}
		if t, _ := entry["type"].(string); t != "" && t != "oauth2" {
			continue
		}
		if cfg, _ := entry["config"].(map[string]any); cfg != nil {
			return cfg
		}
	}
	return nil
}

// hasPublisherTokenClaims reports whether the oauth2 config's
// token.accessToken.clientConfig.attributes lists every publisherTokenAttribute.
func hasPublisherTokenClaims(cfg map[string]any) bool {
	tokenCfg, _ := cfg["token"].(map[string]any)
	accessCfg, _ := tokenCfg["accessToken"].(map[string]any)
	clientCfg, _ := accessCfg["clientConfig"].(map[string]any)
	attrs, _ := clientCfg["attributes"].([]any)
	have := map[string]bool{}
	for _, a := range attrs {
		if s, ok := a.(string); ok {
			have[s] = true
		}
	}
	for _, want := range publisherTokenAttributes {
		if !have[want] {
			return false
		}
	}
	return true
}

func (c *client) appOUID(ctx context.Context, token, appID string) (string, error) {
	app, err := c.getAppByID(ctx, token, appID)
	if err != nil {
		return "", err
	}
	for _, k := range []string{"ouId", "ou_id", "organizationUnitId"} {
		if v, ok := app[k].(string); ok && v != "" {
			return v, nil
		}
	}
	return "", nil
}

func (c *client) DeletePublisherApp(ctx context.Context, orgHandle string) (bool, error) {
	if orgHandle == "" {
		return false, fmt.Errorf("orgHandle required")
	}
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return false, fmt.Errorf("getSystemToken: %w", err)
	}
	appName := PublisherAppName(orgHandle)
	internalID, _, err := c.findApp(ctx, token, appName)
	if err != nil {
		return false, err
	}
	if internalID == "" {
		return false, nil
	}
	return c.deleteApp(ctx, token, internalID)
}

func (c *client) RegenerateClientSecret(ctx context.Context, orgHandle string) (string, error) {
	if orgHandle == "" {
		return "", fmt.Errorf("orgHandle required")
	}
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return "", fmt.Errorf("getSystemToken: %w", err)
	}
	appName := PublisherAppName(orgHandle)
	internalID, _, err := c.findApp(ctx, token, appName)
	if err != nil {
		return "", err
	}
	if internalID == "" {
		return "", fmt.Errorf("thunder app %s not found, cannot regenerate secret", appName)
	}
	return c.regenerateSecret(ctx, token, internalID)
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
	var app map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&app); err != nil {
		return nil, fmt.Errorf("thunder get app decode: %w", err)
	}
	return app, nil
}

// -- low-level HTTP -------------------------------------------------------

type thunderApp struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ClientID string `json:"clientId"`
}

func (c *client) findApp(ctx context.Context, token, appName string) (internalID, clientID string, err error) {
	const pageSize = 100
	const maxPages = 100
	for page := 0; page < maxPages; page++ {
		offset := page * pageSize
		apps, perr := c.listAppsPage(ctx, token, offset, pageSize)
		if perr != nil {
			return "", "", perr
		}
		for _, app := range apps {
			if app.Name == appName {
				return app.ID, app.ClientID, nil
			}
		}
		if len(apps) < pageSize {
			return "", "", nil
		}
	}
	return "", "", fmt.Errorf("thunder list apps exceeded %d pages looking for %s", maxPages, appName)
}

func (c *client) listAppsPage(ctx context.Context, token string, offset, limit int) ([]thunderApp, error) {
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

	// Thunder can return either a bare array or a wrapped object.
	var apps []thunderApp
	if jerr := json.Unmarshal(body, &apps); jerr != nil {
		var wrapped struct {
			Applications []thunderApp `json:"applications"`
		}
		if werr := json.Unmarshal(body, &wrapped); werr != nil {
			return nil, fmt.Errorf("thunder list apps decode: %w", jerr)
		}
		apps = wrapped.Applications
	}
	return apps, nil
}

func (c *client) deleteApp(ctx context.Context, token, appID string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/applications/"+appID, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("thunder delete app: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return false, nil
	case http.StatusOK, http.StatusNoContent:
		return true, nil
	}
	body, _ := io.ReadAll(resp.Body)
	return false, fmt.Errorf("thunder delete app returned %d: %s", resp.StatusCode, string(body))
}

// publisherAppType is the ThunderID application type of the org publisher.
// ThunderID 1.0.0 requires `type` on every create (one of browser, fullstack,
// mobile, m2m, mcp, custom; APP-1042 without it), and the publisher is a pure
// client_credentials client with no interactive login — machine to machine.
// The thunder-app operator and every declarative client document under
// deployments/single-cluster/thunder-resources say the same for their clients.
const publisherAppType = "m2m"

// publisherTokenAttributes are the claims a publisher client_credentials token
// must carry. ThunderID 1.0.0 releases an application's OU into a
// client_credentials token only when the application's token config asks for
// it — `token.accessToken.clientConfig.attributes` — and the publisher-token
// verifier requires `ouHandle` (the org fence) on every runner call. Without
// this the runner's token verifies as a signature but is refused as
// "ouHandle claim missing". The declarative platform clients under
// deployments/single-cluster/thunder-resources carry the same two attributes.
var publisherTokenAttributes = []string{"ouId", "ouHandle"}

// publisherTokenValiditySeconds is the publisher access-token lifetime.
const publisherTokenValiditySeconds = 3600

// publisherTokenClientConfig is the client_credentials token config of the
// publisher: the attributes above, and the lifetime.
func publisherTokenClientConfig() map[string]any {
	return map[string]any{
		"validityPeriod": publisherTokenValiditySeconds,
		"attributes":     append([]string(nil), publisherTokenAttributes...),
	}
}

func (c *client) createApp(ctx context.Context, token, appName, ouID string) (string, string, error) {
	payload := map[string]any{
		"name": appName,
		"type": publisherAppType,
		"ouId": ouID,
		"inboundAuthConfig": []map[string]any{
			{
				"type": "oauth2",
				"config": map[string]any{
					"clientId":                appName,
					"grantTypes":              []string{"client_credentials"},
					"tokenEndpointAuthMethod": "client_secret_basic",
					"token": map[string]any{
						"accessToken": map[string]any{"clientConfig": publisherTokenClientConfig()},
					},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/applications", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("thunder create app: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", "", fmt.Errorf("thunder create app returned %d: %s", resp.StatusCode, string(respBody))
	}
	slog.Info("Thunder publisher app created", "appName", appName, "status", resp.StatusCode)

	var result struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
		InboundAuth  []struct {
			Config struct {
				ClientID     string `json:"clientId"`
				ClientSecret string `json:"clientSecret"`
			} `json:"config"`
		} `json:"inboundAuthConfig"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", fmt.Errorf("thunder create app decode: %w", err)
	}
	cid := result.ClientID
	cs := result.ClientSecret
	if len(result.InboundAuth) > 0 {
		if cid == "" {
			cid = result.InboundAuth[0].Config.ClientID
		}
		if cs == "" {
			cs = result.InboundAuth[0].Config.ClientSecret
		}
	}
	if cid == "" {
		return "", "", fmt.Errorf("thunder create app: clientId not found in response: %s", string(respBody))
	}
	return cid, cs, nil
}

func (c *client) regenerateSecret(ctx context.Context, token, appID string) (string, error) {
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/applications/"+appID, nil)
	if err != nil {
		return "", err
	}
	getReq.Header.Set("Authorization", "Bearer "+token)
	getResp, err := c.httpClient.Do(getReq)
	if err != nil {
		return "", fmt.Errorf("thunder get app for secret regeneration: %w", err)
	}
	defer func() { _ = getResp.Body.Close() }()
	getBody, _ := io.ReadAll(getResp.Body)
	if getResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("thunder get app returned %d: %s", getResp.StatusCode, string(getBody))
	}

	var app map[string]any
	if err := json.Unmarshal(getBody, &app); err != nil {
		return "", fmt.Errorf("thunder get app decode: %w", err)
	}
	newSecret, err := generateRandomSecret()
	if err != nil {
		return "", fmt.Errorf("generate client secret: %w", err)
	}
	if err := setInboundClientSecret(app, newSecret); err != nil {
		return "", fmt.Errorf("set client secret in app payload: %w", err)
	}
	delete(app, "id")

	putBody, _ := json.Marshal(app)
	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+"/applications/"+appID, bytes.NewReader(putBody))
	if err != nil {
		return "", err
	}
	putReq.Header.Set("Authorization", "Bearer "+token)
	putReq.Header.Set("Content-Type", "application/json")

	putResp, err := c.httpClient.Do(putReq)
	if err != nil {
		return "", fmt.Errorf("thunder put app for secret regeneration: %w", err)
	}
	defer func() { _ = putResp.Body.Close() }()
	putRespBody, _ := io.ReadAll(putResp.Body)
	if putResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("thunder put app returned %d: %s", putResp.StatusCode, string(putRespBody))
	}

	var out struct {
		InboundAuth []struct {
			Config struct {
				ClientSecret string `json:"clientSecret"`
			} `json:"config"`
		} `json:"inboundAuthConfig"`
	}
	if err := json.Unmarshal(putRespBody, &out); err != nil {
		return "", fmt.Errorf("thunder put app response decode: %w", err)
	}
	if len(out.InboundAuth) == 0 || out.InboundAuth[0].Config.ClientSecret == "" {
		return "", fmt.Errorf("thunder put app response missing clientSecret")
	}
	slog.Info("Thunder client secret regenerated", "appID", appID)
	return out.InboundAuth[0].Config.ClientSecret, nil
}

func setInboundClientSecret(app map[string]any, secret string) error {
	inbound, ok := app["inboundAuthConfig"].([]any)
	if !ok || len(inbound) == 0 {
		return fmt.Errorf("inboundAuthConfig missing or empty")
	}
	entry, ok := inbound[0].(map[string]any)
	if !ok {
		return fmt.Errorf("inboundAuthConfig[0] is not an object")
	}
	cfg, ok := entry["config"].(map[string]any)
	if !ok {
		return fmt.Errorf("inboundAuthConfig[0].config is not an object")
	}
	cfg["clientSecret"] = secret
	return nil
}

func generateRandomSecret() (string, error) {
	b := make([]byte, 48)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b), nil
}
