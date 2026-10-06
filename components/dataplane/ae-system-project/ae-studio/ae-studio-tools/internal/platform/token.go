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

// Package platform holds ae-studio-tools' clients for the AEP platform: the
// IdP token sources and the generated aep-api client.
package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// refreshBefore is how long before expiry a cached token is replaced.
	refreshBefore = 60 * time.Second
	tokenTimeout  = 10 * time.Second
	// maxTokenBody bounds what is read from the token endpoint.
	maxTokenBody = 64 << 10
)

// ErrClientRejected means the token endpoint refused the client itself
// (400/401: invalid_client, a wrong or rotated secret): a configuration
// fault, not a transient outage.
var ErrClientRejected = errors.New("token endpoint rejected the client")

// ClientCredentials mints and caches an OAuth2 client_credentials token. It
// authenticates with client_secret_basic (the org's ae-studio client takes
// Basic auth): the id and secret go only in the Authorization header, the
// form body carries only the grant. The token is refreshed 60 s before it
// expires, and on demand after Invalidate (the caller saw a 401). A token
// whose response names no lifetime is not cached. Errors name the endpoint's
// status, never the secret or the token.
type ClientCredentials struct {
	TokenURL, ClientID, ClientSecret string
	// HTTP is the client for the token endpoint; nil uses one with a 10 s timeout.
	HTTP *http.Client

	mu     sync.Mutex
	token  string
	expiry time.Time
	// now is the clock; nil is time.Now. Tests set it.
	now func() time.Time
}

// tokenResponse is the part of an RFC 6749 §5.1 answer this client reads.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

// Token returns a cached token that is good for more than 60 s, or mints a
// new one. Concurrent callers share one mint.
func (c *ClientCredentials) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock()
	if c.token != "" && now.Before(c.expiry.Add(-refreshBefore)) {
		return c.token, nil
	}
	tr, err := c.mint(ctx)
	if err != nil {
		return "", err
	}
	c.token, c.expiry = "", time.Time{}
	if tr.ExpiresIn > 0 {
		c.token, c.expiry = tr.AccessToken, now.Add(time.Duration(tr.ExpiresIn)*time.Second)
	}
	return tr.AccessToken, nil
}

// Invalidate drops the cached token, so the next Token call mints.
func (c *ClientCredentials) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token, c.expiry = "", time.Time{}
}

func (c *ClientCredentials) mint(ctx context.Context) (tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader("grant_type=client_credentials"))
	if err != nil {
		return tokenResponse{}, fmt.Errorf("token request: %w", err)
	}
	req.SetBasicAuth(c.ClientID, c.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: tokenTimeout}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("token endpoint unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxTokenBody))
		if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized {
			return tokenResponse{}, fmt.Errorf("%w: token endpoint answered %d", ErrClientRejected, resp.StatusCode)
		}
		return tokenResponse{}, fmt.Errorf("token endpoint answered %d", resp.StatusCode)
	}
	var tr tokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxTokenBody)).Decode(&tr); err != nil {
		return tokenResponse{}, errors.New("token endpoint answered an unreadable body")
	}
	if tr.AccessToken == "" {
		return tokenResponse{}, errors.New("token endpoint answered no access_token")
	}
	return tr, nil
}

func (c *ClientCredentials) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}
