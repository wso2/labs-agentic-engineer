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

// Package asgardeo is a minimal client of an Asgardeo organization's
// Application Management API, for `aectl platform install` and
// `aectl platform sync-clients` with idp.clients asgardeo. It creates AE's
// OAuth applications with the settings AE needs and checks the ones that
// exist.
//
// What the API allows shapes the client:
//   - an application is created with a client id and a client secret that the
//     caller chooses;
//   - a secret cannot be read back, so SecretWorks asks the organization's
//     token endpoint for a token with it;
//   - a secret cannot be set on an application that exists, only regenerated
//     (RegenerateSecret): Asgardeo chooses the new value and returns it, and
//     the old secret stops working at once.
//
// The admin credential, the management token and every client secret stay in
// memory. No error of this package holds a response body: an error answer is
// reported by its own error fields only (code, message and description of
// the management API; error and error_description of the token endpoint),
// each cut to maxErrorFieldChars.
package asgardeo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
)

// The scopes of the Application Management API the admin application is
// authorized for. View and create are required. ScopeRegenerateSecret is
// needed only to regenerate a secret.
const (
	scopeView   = "internal_application_mgt_view"
	scopeCreate = "internal_application_mgt_create"
	// ScopeRegenerateSecret is the scope RegenerateSecret needs.
	ScopeRegenerateSecret = "internal_application_mgt_client_secret_create"
)

const (
	// machineTemplate and browserTemplate are Asgardeo's application
	// templates for a machine-to-machine application and a single-page
	// application.
	machineTemplate = "m2m-application"
	browserTemplate = "6a90e4b0-fbff-42d7-bfde-1efd98f07cd7"

	// accessTokenSeconds and refreshTokenSeconds are the token lifetimes of
	// the applications this client creates.
	accessTokenSeconds  = 3600
	refreshTokenSeconds = 86400

	// maxResponseBytes bounds what is read of one response.
	maxResponseBytes = 1 << 20
	// maxErrorFieldChars bounds each field of an error answer that an error
	// of this package repeats.
	maxErrorFieldChars = 200
)

// tokenAttributes are what the console application's access token must
// carry, and requestedClaims the local claims behind them that the
// application must be allowed to read.
var (
	tokenAttributes = []string{"email", "family_name", "given_name", "groups", "username"}
	requestedClaims = []string{
		"http://wso2.org/claims/emailaddress",
		"http://wso2.org/claims/groups",
		"http://wso2.org/claims/givenname",
		"http://wso2.org/claims/lastname",
		"http://wso2.org/claims/username",
	}
)

// Kind is the kind of an application.
type Kind string

const (
	// Machine is a confidential client that signs in with the
	// client_credentials grant.
	Machine Kind = "machine"
	// Browser is the console's public client: authorization code with PKCE.
	Browser Kind = "browser"
)

// App is an application as AE needs it.
type App struct {
	ClientID string
	Kind     Kind
	// ConsoleURL is the console's public URL (Browser only). The application
	// redirects to <ConsoleURL>/callback after sign-in and to the console's
	// origin after sign-out.
	ConsoleURL string
}

// Config is what New needs.
type Config struct {
	// APIURL is the organization's management API:
	// https://api.asgardeo.io/t/<organization>/api/server/v1.
	APIURL string
	// TokenURL is the organization's token endpoint, as its discovery
	// document names it.
	TokenURL string
	// AdminClientID and AdminClientSecret are the credential of the
	// organization's admin application.
	AdminClientID     string
	AdminClientSecret string
	// HTTPClient is optional. The default has a 30 s timeout and follows no
	// redirect.
	HTTPClient *http.Client
}

// Client calls the management API with one management token.
type Client struct {
	api      string
	tokenURL string
	token    string
	granted  []string
	http     *http.Client
}

// New gets a management token with the admin credential and returns a client.
// It stops when the token lacks the view or the create scope. Both addresses
// must be https: the first is sent the token, the second the credential.
func New(ctx context.Context, cfg Config) (*Client, error) {
	for name, addr := range map[string]string{"the management API": cfg.APIURL, "the token endpoint": cfg.TokenURL} {
		if u, err := url.Parse(addr); err != nil || u.Scheme != "https" || u.Host == "" {
			return nil, fmt.Errorf("asgardeo: the address of %s must be an https URL", name)
		}
	}
	if cfg.AdminClientID == "" || cfg.AdminClientSecret == "" {
		return nil, errors.New("asgardeo: the admin application's client id and secret are required")
	}
	c := &Client{api: strings.TrimRight(cfg.APIURL, "/"), tokenURL: cfg.TokenURL, http: cfg.HTTPClient}
	if c.http == nil {
		c.http = defaultHTTPClient()
	}

	form := url.Values{"grant_type": {"client_credentials"}, "scope": {strings.Join([]string{scopeView, scopeCreate, ScopeRegenerateSecret}, " ")}}
	status, body, err := c.tokenRequest(ctx, cfg.AdminClientID, cfg.AdminClientSecret, form)
	if err != nil {
		return nil, fmt.Errorf("asgardeo: management token request: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("asgardeo: the management token request answered HTTP %d%s", status, oauthErrorSuffix(body))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return nil, errors.New("asgardeo: the management token response holds no access token")
	}
	c.token, c.granted = tok.AccessToken, strings.Fields(tok.Scope)
	for _, scope := range []string{scopeView, scopeCreate} {
		if !slices.Contains(c.granted, scope) {
			return nil, fmt.Errorf("asgardeo: the admin application was not granted %s: authorize it for the Application Management API (its API Authorization tab) with that scope", scope)
		}
	}
	return c, nil
}

// defaultHTTPClient is the client New uses when it is given none. It follows
// no redirect: only the configured addresses were checked to be https, and a
// redirect would carry the token or the credential to an address nothing
// checked. The redirect's own status is reported instead.
func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// CanRegenerateSecrets reports whether the management token carries the scope
// RegenerateSecret needs.
func (c *Client) CanRegenerateSecrets() bool {
	return slices.Contains(c.granted, ScopeRegenerateSecret)
}

// Find returns the id of the application with clientID, or "" when the
// organization has none.
func (c *Client) Find(ctx context.Context, clientID string) (string, error) {
	q := url.Values{"filter": {"clientId eq " + clientID}, "limit": {"2"}}
	status, body, _, err := c.do(ctx, http.MethodGet, "/applications?"+q.Encode(), nil)
	if err != nil {
		return "", fmt.Errorf("asgardeo: look up %s: %w", clientID, err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("asgardeo: looking up %s answered HTTP %d%s", clientID, status, apiErrorSuffix(body))
	}
	var list struct {
		TotalResults int `json:"totalResults"`
		Applications []struct {
			ID string `json:"id"`
		} `json:"applications"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return "", fmt.Errorf("asgardeo: looking up %s: the answer is not the list of applications", clientID)
	}
	if list.TotalResults > 1 || len(list.Applications) > 1 {
		return "", fmt.Errorf("asgardeo: more than one application has the client id %s", clientID)
	}
	if len(list.Applications) == 0 {
		return "", nil
	}
	return list.Applications[0].ID, nil
}

// CreateMachine creates a machine application with clientID and secret and
// returns its id.
func (c *Client) CreateMachine(ctx context.Context, clientID, secret string) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("asgardeo: create %s: a client secret is required", clientID)
	}
	return c.create(ctx, clientID, map[string]any{
		"name": clientID, "templateId": machineTemplate,
		"inboundProtocolConfiguration": map[string]any{"oidc": map[string]any{
			"clientId": clientID, "clientSecret": secret,
			"grantTypes": []string{"client_credentials"},
			"accessToken": map[string]any{
				"type":                                  "JWT",
				"userAccessTokenExpiryInSeconds":        accessTokenSeconds,
				"applicationAccessTokenExpiryInSeconds": accessTokenSeconds,
			},
		}},
	})
}

// CreateBrowser creates the console's application with clientID and returns
// its id: a public client with mandatory PKCE, a JWT access token that
// carries tokenAttributes, and the console's two redirect addresses.
func (c *Client) CreateBrowser(ctx context.Context, clientID, consoleURL string) (string, error) {
	addr, err := consoleAddressesOf(consoleURL)
	if err != nil {
		return "", fmt.Errorf("asgardeo: create %s: %w", clientID, err)
	}
	claims := make([]map[string]any, 0, len(requestedClaims))
	mappings := make([]map[string]any, 0, len(requestedClaims))
	for _, uri := range requestedClaims {
		claims = append(claims, map[string]any{"claim": map[string]any{"uri": uri}, "mandatory": false})
		mappings = append(mappings, map[string]any{"applicationClaim": uri, "localClaim": map[string]any{"uri": uri}})
	}
	return c.create(ctx, clientID, map[string]any{
		"name": clientID, "templateId": browserTemplate,
		"advancedConfigurations": map[string]any{"skipLoginConsent": true, "skipLogoutConsent": true},
		"claimConfiguration": map[string]any{
			"dialect":         "LOCAL",
			"requestedClaims": claims,
			"claimMappings":   mappings,
			"subject":         map[string]any{"claim": map[string]any{"uri": "http://wso2.org/claims/userid"}},
		},
		"inboundProtocolConfiguration": map[string]any{"oidc": map[string]any{
			"clientId": clientID, "grantTypes": []string{"authorization_code", "refresh_token"}, "publicClient": true,
			"pkce": map[string]any{"mandatory": true, "supportPlainTransformAlgorithm": false},
			// Asgardeo stores several redirect addresses as one entry that
			// it matches as a regular expression, so each is escaped.
			"callbackURLs":   []string{addr.redirectEntry()},
			"allowedOrigins": []string{addr.origin},
			"accessToken": map[string]any{
				"type": "JWT", "bindingType": "sso-session", "revokeTokensWhenIDPSessionTerminated": true,
				"validateTokenBinding": false, "userAccessTokenExpiryInSeconds": accessTokenSeconds,
				"applicationAccessTokenExpiryInSeconds": accessTokenSeconds, "accessTokenAttributes": tokenAttributes,
			},
			"refreshToken": map[string]any{
				"expiryInSeconds": refreshTokenSeconds, "renewRefreshToken": true, "extendRenewedRefreshTokenExpiryTime": true,
			},
		}},
	})
}

// create POSTs an application and returns the id its Location header names.
func (c *Client) create(ctx context.Context, clientID string, payload map[string]any) (string, error) {
	status, body, header, err := c.do(ctx, http.MethodPost, "/applications", payload)
	if err != nil {
		return "", fmt.Errorf("asgardeo: create %s: %w", clientID, err)
	}
	if status != http.StatusCreated {
		return "", fmt.Errorf("asgardeo: creating %s answered HTTP %d%s", clientID, status, apiErrorSuffix(body))
	}
	if loc, err := url.Parse(header.Get("Location")); err == nil {
		if id := path.Base(loc.Path); id != "" && id != "." && id != "/" && id != "applications" {
			return id, nil
		}
	}
	id, err := c.Find(ctx, clientID)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("asgardeo: %s was created but the organization does not list it", clientID)
	}
	return id, nil
}

// oidcSettings is the part of an application's OIDC configuration that
// Problems reads. It never holds a secret: the API does not return one.
type oidcSettings struct {
	ClientID       string   `json:"clientId"`
	GrantTypes     []string `json:"grantTypes"`
	PublicClient   bool     `json:"publicClient"`
	CallbackURLs   []string `json:"callbackURLs"`
	AllowedOrigins []string `json:"allowedOrigins"`
	PKCE           struct {
		Mandatory bool `json:"mandatory"`
	} `json:"pkce"`
	AccessToken struct {
		Type       string   `json:"type"`
		Attributes []string `json:"accessTokenAttributes"`
	} `json:"accessToken"`
}

// Problems reads the application appID back and lists every setting that is
// not what AE needs. An empty list means all are right. It changes nothing.
func (c *Client) Problems(ctx context.Context, app App, appID string) ([]string, error) {
	appPath := "/applications/" + url.PathEscape(appID)
	status, body, _, err := c.do(ctx, http.MethodGet, appPath+"/inbound-protocols/oidc", nil)
	if err != nil {
		return nil, fmt.Errorf("asgardeo: read the OIDC settings of %s: %w", app.ClientID, err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("asgardeo: reading the OIDC settings of %s answered HTTP %d%s", app.ClientID, status, apiErrorSuffix(body))
	}
	var oidc oidcSettings
	if err := json.Unmarshal(body, &oidc); err != nil {
		return nil, fmt.Errorf("asgardeo: the OIDC settings of %s are not in the expected form", app.ClientID)
	}

	var problems []string
	if oidc.ClientID != app.ClientID {
		problems = append(problems, fmt.Sprintf("client id is %s", oidc.ClientID))
	}
	if oidc.AccessToken.Type != "JWT" {
		problems = append(problems, fmt.Sprintf("access token type is %s, not JWT", oidc.AccessToken.Type))
	}
	if app.Kind == Machine {
		if !slices.Contains(oidc.GrantTypes, "client_credentials") {
			problems = append(problems, "the client_credentials grant is off")
		}
		return problems, nil
	}

	addr, err := consoleAddressesOf(app.ConsoleURL)
	if err != nil {
		return nil, fmt.Errorf("asgardeo: check %s: %w", app.ClientID, err)
	}
	if !oidc.PublicClient {
		problems = append(problems, "it is not a public client")
	}
	if !oidc.PKCE.Mandatory {
		problems = append(problems, "PKCE is not mandatory")
	}
	for _, grant := range []string{"authorization_code", "refresh_token"} {
		if !slices.Contains(oidc.GrantTypes, grant) {
			problems = append(problems, fmt.Sprintf("the %s grant is off", grant))
		}
	}
	for _, r := range []struct{ what, url string }{{"sign-in", addr.callback}, {"sign-out", addr.postLogout}} {
		if p := redirectProblem(oidc.CallbackURLs, r.what, r.url); p != "" {
			problems = append(problems, p)
		}
	}
	if !slices.Contains(oidc.AllowedOrigins, addr.origin) {
		problems = append(problems, fmt.Sprintf("the origin %s is not allowed", addr.origin))
	}
	for _, attr := range tokenAttributes {
		if !slices.Contains(oidc.AccessToken.Attributes, attr) {
			problems = append(problems, fmt.Sprintf("the access token lacks the attribute %s", attr))
		}
	}

	status, body, _, err = c.do(ctx, http.MethodGet, appPath, nil)
	if err != nil {
		return nil, fmt.Errorf("asgardeo: read %s: %w", app.ClientID, err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("asgardeo: reading %s answered HTTP %d%s", app.ClientID, status, apiErrorSuffix(body))
	}
	var full struct {
		ClaimConfiguration struct {
			RequestedClaims []struct {
				Claim struct {
					URI string `json:"uri"`
				} `json:"claim"`
			} `json:"requestedClaims"`
		} `json:"claimConfiguration"`
	}
	if err := json.Unmarshal(body, &full); err != nil {
		return nil, fmt.Errorf("asgardeo: %s is not in the expected form", app.ClientID)
	}
	var allowed []string
	for _, rc := range full.ClaimConfiguration.RequestedClaims {
		allowed = append(allowed, rc.Claim.URI)
	}
	for _, uri := range requestedClaims {
		if !slices.Contains(allowed, uri) {
			problems = append(problems, fmt.Sprintf("the application may not read the claim %s", uri))
		}
	}
	return problems, nil
}

// SecretWorks reports whether the organization issues a token to clientID
// with secret. It is how a secret is compared with the one an application
// has, since the API never returns a secret. false means the organization
// refused the client's credentials; any other failure is an error.
func (c *Client) SecretWorks(ctx context.Context, clientID, secret string) (bool, error) {
	status, body, err := c.tokenRequest(ctx, clientID, secret, url.Values{"grant_type": {"client_credentials"}})
	if err != nil {
		return false, fmt.Errorf("asgardeo: token request as %s: %w", clientID, err)
	}
	if status == http.StatusOK {
		return true, nil
	}
	var oauth struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &oauth) == nil && oauth.Error == "invalid_client" {
		return false, nil
	}
	return false, fmt.Errorf("asgardeo: the token request as %s answered HTTP %d%s", clientID, status, oauthErrorSuffix(body))
}

// RegenerateSecret has Asgardeo replace the secret of the application appID
// and returns the new one. The old secret, and the tokens issued with it,
// stop working at once.
func (c *Client) RegenerateSecret(ctx context.Context, appID string) (string, error) {
	if !c.CanRegenerateSecrets() {
		return "", fmt.Errorf("asgardeo: the admin application was not granted %s: authorize it for the Application Management API (its API Authorization tab) with that scope", ScopeRegenerateSecret)
	}
	// The request has no body. do sends an explicit length of 0 for it:
	// Asgardeo answers HTTP 411 to a POST without one.
	status, body, _, err := c.do(ctx, http.MethodPost, "/applications/"+url.PathEscape(appID)+"/inbound-protocols/oidc/regenerate-secret", nil)
	if err != nil {
		return "", fmt.Errorf("asgardeo: regenerate the secret: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("asgardeo: regenerating the secret answered HTTP %d%s", status, apiErrorSuffix(body))
	}
	var out struct {
		ClientSecret string `json:"clientSecret"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.ClientSecret == "" {
		return "", errors.New("asgardeo: the answer to regenerating the secret holds no secret")
	}
	return out.ClientSecret, nil
}

// do sends one request to the management API with the management token. A
// nil payload sends an empty body with a length of 0.
func (c *Client) do(ctx context.Context, method, pathAndQuery string, payload any) (int, []byte, http.Header, error) {
	var body io.Reader = http.NoBody
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.api+pathAndQuery, body)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.send(req)
}

// tokenRequest POSTs form to the token endpoint with HTTP Basic client
// authentication.
func (c *Client) tokenRequest(ctx context.Context, clientID, secret string, form url.Values) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, err
	}
	req.SetBasicAuth(clientID, secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	status, body, _, err := c.send(req)
	return status, body, err
}

// send performs req and reads at most maxResponseBytes of the answer.
func (c *Client) send(req *http.Request) (int, []byte, http.Header, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("%s %s: read the answer: %w", req.Method, req.URL.Host, err)
	}
	return resp.StatusCode, body, resp.Header, nil
}

// apiErrorSuffix is ": code | message | description" from a management API
// error body, or "" when the body is not one. The body itself is never
// reported.
func apiErrorSuffix(body []byte) string {
	var e struct {
		Code        string `json:"code"`
		Message     string `json:"message"`
		Description string `json:"description"`
	}
	if json.Unmarshal(body, &e) != nil {
		return ""
	}
	return joinedSuffix(" | ", e.Code, e.Message, e.Description)
}

// oauthErrorSuffix is ": error: error_description" from a token endpoint
// error body, or "" when the body is not one.
func oauthErrorSuffix(body []byte) string {
	var e struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if json.Unmarshal(body, &e) != nil {
		return ""
	}
	return joinedSuffix(": ", e.Error, e.Description)
}

func joinedSuffix(sep string, parts ...string) string {
	var set []string
	for _, p := range parts {
		if r := []rune(p); len(r) > maxErrorFieldChars {
			p = string(r[:maxErrorFieldChars]) + "…"
		}
		if p != "" {
			set = append(set, p)
		}
	}
	if len(set) == 0 {
		return ""
	}
	return ": " + strings.Join(set, sep)
}

// consoleAddresses are the console's addresses an application registers.
type consoleAddresses struct {
	// callback is where the console is sent after sign-in, postLogout where
	// it asks to be sent after sign-out (its origin), and origin the origin
	// its requests come from.
	callback, postLogout, origin string
}

// consoleAddressesOf derives them from the console's public URL. A URL with
// a space, '|', a parenthesis or a backslash is refused: the redirect
// addresses are stored in one regular expression, where those would change
// what it matches.
func consoleAddressesOf(consoleURL string) (consoleAddresses, error) {
	u, err := url.Parse(consoleURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return consoleAddresses{}, fmt.Errorf("the console's public URL %q must be an absolute http or https URL", consoleURL)
	}
	if strings.ContainsAny(consoleURL, " \t\r\n|()\\") {
		return consoleAddresses{}, fmt.Errorf("the console's public URL %q must not contain spaces, '|', parentheses or backslashes", consoleURL)
	}
	base := strings.TrimRight(consoleURL, "/")
	origin := u.Scheme + "://" + u.Host
	return consoleAddresses{callback: base + "/callback", postLogout: origin, origin: origin}, nil
}

// redirectEntry is the one callbackURLs entry that holds both redirect
// addresses, each escaped so that it matches that address and nothing else:
// an unescaped "." would also accept a look-alike host.
func (a consoleAddresses) redirectEntry() string {
	return "regexp=(" + regexp.QuoteMeta(a.postLogout) + "|" + regexp.QuoteMeta(a.callback) + ")"
}

// unescape removes the backslash in front of each escaped character.
var unescape = regexp.MustCompile(`\\(.)`)

// redirectProblem is what is wrong with how addr is registered in entries,
// or "". An address counts as registered when it is a plain entry, or an
// alternative of a regexp=(a|b) entry escaped as redirectEntry escapes it.
// An alternative that names the address with its metacharacters unescaped is
// reported: it also matches other hosts.
func redirectProblem(entries []string, what, addr string) string {
	var alternatives []string
	for _, e := range entries {
		if e == addr {
			return ""
		}
		if strings.HasPrefix(e, "regexp=(") && strings.HasSuffix(e, ")") {
			alternatives = append(alternatives, strings.Split(e[len("regexp=("):len(e)-1], "|")...)
		}
	}
	escaped := regexp.QuoteMeta(addr)
	if slices.Contains(alternatives, escaped) {
		return ""
	}
	for _, alt := range alternatives {
		if unescape.ReplaceAllString(alt, "$1") == addr {
			return fmt.Sprintf("the %s redirect URL %s is registered as a regular expression with unescaped characters, which also matches look-alike hosts; register it as %s", what, addr, escaped)
		}
	}
	return fmt.Sprintf("the %s redirect URL %s is not registered", what, addr)
}
