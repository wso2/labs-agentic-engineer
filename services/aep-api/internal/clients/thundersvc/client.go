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
//   - Org app naming: `aep-publisher-<orgHandle>` and `ae-studio-<orgHandle>`
//     (clientId = name), registered in the org's OU.
//   - Thunder has no lookup by name or clientId, so callers store each app's
//     entity id and pass it back; a miss costs one full list scan (apps.go).
//   - The publisher falls back to the default OU when the caller knows no
//     org OU (looked up once, cached on the client).
//
// The system token has a TTL (Thunder default ~1h); the cache uses a
// 30 s skew so concurrent callers don't all hit the slow path right
// before expiry. singleflight deduplicates the slow path itself.
package thundersvc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Client is the public surface of the Thunder admin client: the directory,
// resource-server and role calls the identity domain makes on an
// environment's Thunder. It registers no OAuth application: the platform
// IdP's clients are created before aep-api runs.
type Client interface {
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

	// -- permission catalog (resource servers, resources, actions) --------
	//
	// One resource server per project: its identifier is the access-token
	// audience the generated app's clients ask for, its resources and actions
	// derive the scope handles the token's `scope` claim carries.
	// resourceservers.go carries the Thunder constraints, the cascade order
	// and the conflict codes.

	// FindResourceServerByIdentifier returns the resource server claiming this
	// identifier (an absolute URI), and whether one exists.
	FindResourceServerByIdentifier(ctx context.Context, identifier string) (*ResourceServer, bool, error)

	// CreateResourceServer registers a project's permission namespace with the
	// platform-wide ":" delimiter. A taken identifier is ErrIdentifierConflict.
	CreateResourceServer(ctx context.Context, name, identifier string) (ResourceServer, error)

	// DeleteResourceServer removes an EMPTY resource server; one that still has
	// resources answers ErrHasDependencies. Idempotent.
	DeleteResourceServer(ctx context.Context, rsID string) error

	// DeleteResourceServerCascade removes a resource server and its whole tree
	// in the order Thunder accepts: actions, then resources, then the server.
	DeleteResourceServerCascade(ctx context.Context, rsID string) error

	// ListResources returns the top-level resources of a resource server.
	ListResources(ctx context.Context, rsID string) ([]Resource, error)

	// CreateResource adds a resource. Its handle derives the permission prefix
	// and is immutable; a handle already used here is ErrHandleConflict.
	CreateResource(ctx context.Context, rsID, handle, name, description string) (Resource, error)

	// UpdateResource rewrites a resource's display name and description. The
	// handle and the parent are immutable, so this is the ONLY way a corrected
	// description reaches the directory without a delete that would cascade
	// through the resource's actions and every role granting them.
	UpdateResource(ctx context.Context, rsID, resourceID, name, description string) (Resource, error)

	// DeleteResource removes a resource with no actions left; one that still
	// has them answers ErrHasDependencies. Idempotent.
	DeleteResource(ctx context.Context, rsID, resourceID string) error

	// ListActions returns the actions under one resource. Their Permission
	// fields are the derived scope handles ("claims:read").
	ListActions(ctx context.Context, rsID, resourceID string) ([]Action, error)

	// CreateAction adds an action under a resource. Handle uniqueness is per
	// parent resource — a duplicate under THIS resource is ErrHandleConflict.
	CreateAction(ctx context.Context, rsID, resourceID, handle, name, description string) (Action, error)

	// UpdateAction rewrites an action's display name and description; its handle
	// and kind are immutable. See UpdateResource.
	UpdateAction(ctx context.Context, rsID, resourceID, actionID, name, description string) (Action, error)

	// DeleteAction removes an action. Thunder cascades the removal out of every
	// role that granted it. Idempotent.
	DeleteAction(ctx context.Context, rsID, resourceID, actionID string) error

	// -- roles and assignments --------------------------------------------
	//
	// group → role → permissions ∩ the requested resource server is the only
	// thing that narrows a generated app's access token. roles.go carries the
	// Thunder constraints.

	// ListRoles returns every role in the directory WITHOUT its permissions —
	// the name → id map the ensure converges against.
	ListRoles(ctx context.Context) ([]Role, error)

	// FindRoleByName returns the role with this name (case-insensitive), and
	// whether one exists. Role names may contain "/" (`<project>/<Role>`).
	FindRoleByName(ctx context.Context, name string) (*Role, bool, error)

	// GetRole reads one role with its permissions.
	GetRole(ctx context.Context, roleID string) (Role, error)

	// ListRoleAssignments returns the principals holding a role, with display
	// names resolved.
	ListRoleAssignments(ctx context.Context, roleID string) ([]Assignment, error)

	// CreateRole creates a role granting exactly these permissions. Every
	// permission must already exist in the catalog (else ErrInvalidPermissions).
	CreateRole(ctx context.Context, name, description string, permissions []RolePermission) (Role, error)

	// UpdateRole REPLACES a role's name, description and permission set. Its
	// assignments survive, so converging a role is this one call.
	UpdateRole(ctx context.Context, roleID, name, description string, permissions []RolePermission) (Role, error)

	// DeleteRole removes a role and its assignments. Idempotent.
	DeleteRole(ctx context.Context, roleID string) error

	// AddRoleAssignments gives a role to these principals (additive, 204).
	AddRoleAssignments(ctx context.Context, roleID string, assignments []Assignment) error

	// RemoveRoleAssignments takes a role away from these principals, leaving
	// the rest.
	RemoveRoleAssignments(ctx context.Context, roleID string, assignments []Assignment) error
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
	// tokenExpiry is wall-clock time; see systemTokenExpiry.
	tokenExpiry time.Time
	tokenSfg    singleflight.Group
	// now is the clock the cache is judged by; tests substitute it.
	now func() time.Time

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
	c := &client{
		baseURL:   strings.TrimRight(cfg.BaseURL, "/"),
		systemID:  cfg.ClientID,
		systemSec: cfg.ClientSecret,
		systemAud: cfg.SystemResourceIdentifier,
		now:       time.Now,
	}
	// A shallow copy, so the eviction hook is this client's and never a
	// side-effect on an http.Client the caller handed in and still uses.
	wrapped := *hc
	wrapped.Transport = &tokenEvictingTransport{next: hc.Transport, client: c}
	c.httpClient = &wrapped
	return c
}

// tokenEvictingTransport is the one place the cache learns that Thunder has
// stopped honouring the system token. Every admin call in this package sends
// the token as a bearer through this client, so a 401 answered to THAT bearer
// is evicted here, once, for all of them — the next call mints afresh instead
// of failing for the rest of the cached lifetime.
//
// Why this can happen with a token the cache still thinks is valid: the
// identity provider may have restarted and dropped the tokens it issued, or
// the process may have been paused (a laptop's VM asleep) so that the wall
// clock ran on while its own clock did not. Neither is visible from inside;
// the 401 is the only signal, so it is honoured. A 403 is not: that is a
// scope or registration problem a fresh token would carry unchanged.
type tokenEvictingTransport struct {
	next   http.RoundTripper
	client *client
}

func (t *tokenEvictingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	next := t.next
	if next == nil {
		next = http.DefaultTransport
	}
	resp, err := next.RoundTrip(req)
	if err == nil && resp.StatusCode == http.StatusUnauthorized {
		if bearer, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer "); ok {
			t.client.evictSystemToken(bearer)
		}
	}
	return resp, err
}

// evictSystemToken drops the cached system token if it is the one given, so a
// concurrent mint that already replaced it is not thrown away as well.
func (c *client) evictSystemToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if token != "" && c.cachedToken == token {
		c.cachedToken = ""
		c.tokenExpiry = time.Time{}
	}
}

// -- system token --------------------------------------------------------

// getSystemToken returns a cached system token or fetches a new one.
// Fast path: RLock + cache hit. Slow path: singleflight dedupe so
// concurrent callers share one round-trip.
// Validity is judged by systemTokenExpiry: wall-clock, against the token's own exp.
func (c *client) getSystemToken(ctx context.Context) (string, error) {
	c.mu.RLock()
	if c.cachedToken != "" && c.now().Before(c.tokenExpiry) {
		token := c.cachedToken
		c.mu.RUnlock()
		return token, nil
	}
	c.mu.RUnlock()

	result, err, _ := c.tokenSfg.Do("system-token", func() (any, error) {
		c.mu.RLock()
		if c.cachedToken != "" && c.now().Before(c.tokenExpiry) {
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
		c.tokenExpiry = systemTokenExpiry(token, expiresIn, c.now())
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

// systemTokenExpiry is when the cache stops trusting a token: the earlier of
// the token's own `exp` and now+expires_in, less a skew — so a renew is in
// flight before Thunder starts refusing it.
//
// The result carries NO monotonic reading (Round(0)), which makes every later
// Before() a wall-clock comparison. time.Now() also holds a monotonic reading
// and Before() prefers it when both sides have one — exactly wrong here:
// Thunder judges the token by wall time, and a process that was paused (its VM
// asleep) sees its monotonic clock stand still while the wall clock, and the
// token's expiry, run on. The token's own `exp` is the server's verdict rather
// than a duration this side has to count down correctly.
func systemTokenExpiry(token string, expiresIn int, now time.Time) time.Time {
	const skew = 30 * time.Second
	now = now.Round(0)
	var until time.Time
	switch {
	case expiresIn > 0:
		until = now.Add(time.Duration(expiresIn) * time.Second)
	default:
		until = now.Add(time.Minute)
	}
	if exp, ok := tokenExp(token); ok && exp.Before(until) {
		until = exp
	}
	if until.Sub(now) > skew {
		return until.Add(-skew)
	}
	return until
}

// tokenExp reads a JWT's `exp` claim as wall-clock time. An opaque token, or
// one without the claim, answers false and the caller falls back to expires_in.
func tokenExp(token string) (time.Time, bool) {
	payload, ok := tokenPayload(token)
	if !ok {
		return time.Time{}, false
	}
	var claims struct {
		Exp float64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp <= 0 {
		return time.Time{}, false
	}
	return time.Unix(int64(claims.Exp), 0), true
}

// tokenPayload decodes the claims segment of a JWT; false for an opaque token.
func tokenPayload(token string) ([]byte, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil, false
	}
	return payload, true
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
	payload, ok := tokenPayload(token)
	if !ok {
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
