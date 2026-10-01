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

// Package github is ae-studio-tools' GitHub REST client, authenticated with
// the org's gitpat.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	apiBaseURL     = "https://api.github.com"
	requestTimeout = 10 * time.Second
	// maxBodyBytes bounds what is read from a GitHub answer.
	maxBodyBytes = 1 << 20
	// defaultRetryAfter is used when GitHub rate-limits without saying for how long.
	defaultRetryAfter = 60 * time.Second
)

// Identity reports the GitHub user the gitpat belongs to.
type Identity interface {
	Whoami(ctx context.Context) (login string, id int64, err error)
}

// ErrRateLimited means GitHub refused the call for rate limiting (a primary
// limit exhausted, or a secondary limit with Retry-After). RetryAfter is how
// long to wait.
type ErrRateLimited struct {
	RetryAfter time.Duration
}

func (e *ErrRateLimited) Error() string {
	return fmt.Sprintf("github rate limited, retry after %s", e.RetryAfter)
}

// StatusError is any other non-2xx answer from GitHub.
type StatusError struct {
	Status int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("github answered %d", e.Status)
}

// Client calls the GitHub REST API with the gitpat as a bearer token. Errors
// name the status or the failure, never the token.
type Client struct {
	baseURL string
	pat     string
	http    *http.Client
}

var _ Identity = (*Client)(nil)

// NewClient returns a client for api.github.com that authenticates with pat.
func NewClient(pat string) *Client {
	return newClient(apiBaseURL, pat)
}

func newClient(baseURL, pat string) *Client {
	return &Client{baseURL: baseURL, pat: pat, http: &http.Client{Timeout: requestTimeout}}
}

// Whoami answers GET /user: the login and numeric id of the gitpat's user.
func (c *Client) Whoami(ctx context.Context) (string, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/user", nil)
	if err != nil {
		return "", 0, fmt.Errorf("github request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.pat)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := c.http.Do(req)
	if err != nil {
		// *url.Error names the method and URL; neither carries the token.
		return "", 0, fmt.Errorf("github call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := statusErr(resp, time.Now()); err != nil {
		return "", 0, err
	}
	var user struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&user); err != nil {
		return "", 0, fmt.Errorf("github user: %w", err)
	}
	if user.Login == "" || user.ID == 0 {
		return "", 0, errors.New("github user: login or id missing")
	}
	return user.Login, user.ID, nil
}

// statusErr maps a non-2xx answer: GitHub signals rate limiting with 403 or
// 429 carrying Retry-After (secondary limits) or X-RateLimit-Remaining: 0
// (primary limit, reset at X-RateLimit-Reset epoch seconds).
func statusErr(resp *http.Response, now time.Time) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
			return &ErrRateLimited{RetryAfter: time.Duration(s) * time.Second}
		}
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			wait := defaultRetryAfter
			if reset, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
				if d := time.Unix(reset, 0).Sub(now); d > 0 {
					// Retry-After is whole seconds; under half a second
					// would round to 0, which means "retry now".
					wait = max(d.Round(time.Second), time.Second)
				}
			}
			return &ErrRateLimited{RetryAfter: wait}
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			return &ErrRateLimited{RetryAfter: defaultRetryAfter}
		}
	}
	return &StatusError{Status: resp.StatusCode}
}
