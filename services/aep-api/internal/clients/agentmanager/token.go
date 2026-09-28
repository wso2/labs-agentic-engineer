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

package agentmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// client is the Client implementation. One per Agent Manager base URL.
type client struct {
	cfg  Config
	http *http.Client

	mu           sync.Mutex
	cached       string
	cachedScopes string
	cachedUntil  time.Time
}

// New builds a client. It performs no I/O.
func New(cfg Config) Client {
	return &client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}
}

// token mints a client_credentials token carrying the requested scopes.
//
// THE SCOPE PARAMETER IS NOT OPTIONAL. A mint without it returns 200 and a
// token whose aud is the client id and whose scope claim is absent; amp-api
// then refuses every request with 403 "insufficient permissions" and says
// nothing about why. Requesting scopes explicitly is also what makes the
// audience come back as the resource server's (urn:wso2:amp), which is the
// value amp-api's KEY_MANAGER_AUDIENCE allows.
//
// Cached per scope set, because a token minted for one scope set cannot serve
// another and silently degrades to a 403 if reused. See invalidateToken for why
// the cache must also be droppable before its expiry.
func (c *client) token(ctx context.Context, scopes string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cached != "" && c.cachedScopes == scopes && time.Now().Before(c.cachedUntil) {
		return c.cached, nil
	}
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("scope", scopes)
	if c.cfg.Resource != "" {
		form.Set("resource", c.cfg.Resource)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("agentmanager: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.cfg.ClientID, c.cfg.ClientSecret)
	if c.cfg.HostHeader != "" {
		// req.Host, not a header: Go writes the URL's host into the request
		// line otherwise, and the gateway routes on this one.
		req.Host = c.cfg.HostHeader
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("agentmanager: mint token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("agentmanager: token endpoint returned %d", resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("agentmanager: decode token: %w", err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("agentmanager: token endpoint returned no access_token")
	}
	ttl := time.Duration(out.ExpiresIn) * time.Second
	if ttl <= time.Minute {
		ttl = time.Hour
	}
	c.cached, c.cachedScopes = out.AccessToken, scopes
	c.cachedUntil = time.Now().Add(ttl - time.Minute)
	return out.AccessToken, nil
}

// invalidateToken drops a cached token the server has rejected.
//
// EXPIRY IS NOT THE ONLY WAY A TOKEN DIES. The platform IdP restarting
// invalidates every token it has issued, and this cache holds one for its full
// hour — so without this, one restart turns into up to an hour of 401s on a
// credential the client keeps presenting because its own clock says it is
// still good. That is exactly how an IdP restart became an overnight outage of
// the govern stage: `401 UNAUTHORIZED / INVALID_TOKEN`, repeated, on a token
// nothing would re-mint.
//
// Scoped to the entry it rejected: another scope set's token may still be
// valid, and dropping it would re-mint it for nothing.
func (c *client) invalidateToken(scopes string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cachedScopes == scopes {
		c.cached, c.cachedUntil = "", time.Time{}
	}
}
