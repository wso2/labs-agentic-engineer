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

// Package openchoreo stores install-time secrets through OpenChoreo's Secret
// API, as aectl's own client (aectl-openchoreo-client). It holds the two
// requests aectl needs, written by hand: aep-api's generated OpenChoreo client
// is internal to the aep-api module.
package openchoreo

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

// TargetPlane is the plane whose secret store OpenChoreo writes a value to.
type TargetPlane struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// TokenSource returns a bearer token for OpenChoreo's API.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// ClientCredentials gets a token by the client-credentials grant, sending the
// client id and secret in the form body (client_secret_post, the method
// aectl registers its platform clients with). HTTP nil uses a client with a
// 10s timeout and Go's standard certificate verification.
type ClientCredentials struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	HTTP         *http.Client
}

// Client calls OpenChoreo's API at BaseURL with Token's bearer.
type Client struct {
	BaseURL string
	Token   TokenSource
	HTTP    *http.Client
}

// maxBody bounds what is read of a response: these answers are small.
const maxBody = 1 << 20

func httpClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (cc ClientCredentials) Token(ctx context.Context) (string, error) {
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {cc.ClientID},
		"client_secret": {cc.ClientSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cc.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("token request for %s: %w", cc.ClientID, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient(cc.HTTP).Do(req)
	if err != nil {
		return "", fmt.Errorf("token request for %s at %s: %w", cc.ClientID, cc.TokenURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request for %s at %s answered %s: %s", cc.ClientID, cc.TokenURL, resp.Status, strings.TrimSpace(string(body)))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return "", fmt.Errorf("token request for %s at %s returned no access token", cc.ClientID, cc.TokenURL)
	}
	return tok.AccessToken, nil
}

// PutSecret stores data as the Opaque secret name in namespace on plane:
// it creates the secret, and replaces its data when it already exists (409).
// OpenChoreo creates the SecretReference of the same name. Any other answer
// is an error carrying OpenChoreo's status and message.
func (c *Client) PutSecret(ctx context.Context, namespace, name string, plane TargetPlane, data map[string]string) error {
	token, err := c.Token.Token(ctx)
	if err != nil {
		return err
	}
	base := strings.TrimRight(c.BaseURL, "/") + "/api/v1alpha1/namespaces/" + url.PathEscape(namespace) + "/secrets"
	status, msg, err := c.send(ctx, http.MethodPost, base, token, map[string]any{
		"secretName": name, "secretType": "Opaque", "data": data, "targetPlane": plane,
	})
	if err != nil {
		return err
	}
	switch status {
	case http.StatusCreated, http.StatusOK:
		return nil
	case http.StatusConflict:
		status, msg, err = c.send(ctx, http.MethodPut, base+"/"+url.PathEscape(name), token, map[string]any{"data": data})
		if err != nil {
			return err
		}
		if status == http.StatusOK {
			return nil
		}
		return fmt.Errorf("openchoreo: update secret %s/%s answered %d %s: %s", namespace, name, status, http.StatusText(status), msg)
	}
	return fmt.Errorf("openchoreo: create secret %s/%s answered %d %s: %s", namespace, name, status, http.StatusText(status), msg)
}

// send makes one JSON request and returns the status and OpenChoreo's error
// message (its body's `error`, else the trimmed body).
func (c *Client) send(ctx context.Context, method, target, token string, payload any) (int, string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, "", fmt.Errorf("openchoreo: encode %s %s: %w", method, target, err)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(raw))
	if err != nil {
		return 0, "", fmt.Errorf("openchoreo: build %s %s: %w", method, target, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient(c.HTTP).Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("openchoreo: %s %s: %w", method, target, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return resp.StatusCode, e.Error, nil
	}
	return resp.StatusCode, strings.TrimSpace(string(body)), nil
}
