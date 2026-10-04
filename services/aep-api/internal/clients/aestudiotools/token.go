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

package aestudiotools

// token.go — the AE-only M2M token: client_credentials of aep-api's own
// AE-only client (AE_STUDIO_INTERNAL_CLIENT_ID/SECRET) at the IdP token URL.
// The only token source of the adapter; cached, refreshed once on a 401.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// TokenSource hands out the AE-only bearer. Invalidate drops the cached one,
// so the next Token fetches a fresh token.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
	Invalidate()
}

var (
	// errClientCredentialsMissing: the AE-only client id, secret or token URL
	// is not configured (C5). aep-api boots without them; the first call fails.
	errClientCredentialsMissing = fmt.Errorf("%w: client credentials missing", sourcecontrol.ErrAEStudioMisconfigured)
	// errTokenRefused: the IdP refused the AE-only client (400/401).
	errTokenRefused = fmt.Errorf("%w: the IdP refused the client", sourcecontrol.ErrAEStudioMisconfigured)
)

const (
	// tokenExpiryMargin: a cached token this close to expiry is fetched anew.
	tokenExpiryMargin = 60 * time.Second
	// tokenFetchTimeout bounds one token request.
	tokenFetchTimeout = 10 * time.Second
	// tokenBodyLimit bounds the token reply read.
	tokenBodyLimit = 64 << 10
)

// clientCredentials is the client_credentials TokenSource. The secret goes in
// the POST body, as Thunder's confidential clients take it.
type clientCredentials struct {
	tokenURL, clientID, clientSecret string
	http                             *http.Client
	now                              func() time.Time

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// NewClientCredentials is the AE-only client's TokenSource. An empty token
// URL, client id or secret is allowed here (boot never needs them); every
// Token then fails with ErrAEStudioMisconfigured. A nil hc uses a client with
// a 10 s timeout.
func NewClientCredentials(tokenURL, clientID, clientSecret string, hc *http.Client) TokenSource {
	if hc == nil {
		hc = &http.Client{Timeout: tokenFetchTimeout}
	}
	return &clientCredentials{tokenURL: tokenURL, clientID: clientID, clientSecret: clientSecret, http: hc, now: time.Now}
}

func (c *clientCredentials) Token(ctx context.Context) (string, error) {
	if c.tokenURL == "" || c.clientID == "" || c.clientSecret == "" {
		return "", errClientCredentialsMissing
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && c.now().Add(tokenExpiryMargin).Before(c.expiresAt) {
		return c.token, nil
	}
	tok, ttl, err := c.fetch(ctx)
	if err != nil {
		return "", err
	}
	c.token, c.expiresAt = tok, c.now().Add(ttl)
	return tok, nil
}

func (c *clientCredentials) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token, c.expiresAt = "", time.Time{}
}

// fetch asks the IdP for a token. Errors name the status only, never the
// reply (which may echo the request).
func (c *clientCredentials) fetch(ctx context.Context) (string, time.Duration, error) {
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("%w: token request: %w", sourcecontrol.ErrAEStudioMisconfigured, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", 0, ctx.Err()
		}
		return "", 0, fmt.Errorf("%w: token endpoint unreachable: %w", sourcecontrol.ErrAEStudioUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized:
		return "", 0, fmt.Errorf("%w (status %d)", errTokenRefused, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return "", 0, fmt.Errorf("%w: token endpoint answered %d", sourcecontrol.ErrAEStudioUnavailable, resp.StatusCode)
	}
	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, tokenBodyLimit)).Decode(&body); err != nil || body.AccessToken == "" {
		return "", 0, fmt.Errorf("%w: token endpoint answered no access token", sourcecontrol.ErrAEStudioUnavailable)
	}
	return body.AccessToken, time.Duration(body.ExpiresIn) * time.Second, nil
}
