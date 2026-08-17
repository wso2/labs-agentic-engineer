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
	"time"
)

// ThunderClient is a minimal HTTP client for Thunder's admin API.
type ThunderClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient authenticates against Thunder using client_credentials + scope=system
// and returns a client ready to call admin APIs.
func NewClient(ctx context.Context, thunderURL, adminClientID, adminClientSecret string) (*ThunderClient, error) {
	hc := &http.Client{Timeout: 30 * time.Second}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", adminClientID)
	form.Set("client_secret", adminClientSecret)
	form.Set("scope", "system")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		thunderURL+"/oauth2/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token request returned %d: %s", resp.StatusCode, body)
	}

	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return nil, fmt.Errorf("could not parse access token from response: %s", body)
	}

	return &ThunderClient{baseURL: thunderURL, token: tok.AccessToken, http: hc}, nil
}

// GetDefaultOU returns the ID of the organisation unit with the given handle.
func (c *ThunderClient) GetDefaultOU(ctx context.Context, handle string) (string, error) {
	body, err := c.get(ctx, "/organization-units/tree/"+handle)
	if err != nil {
		return "", fmt.Errorf("fetch OU %q: %w", handle, err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse OU response: %w", err)
	}
	id, _ := result["id"].(string)
	if id == "" {
		return "", fmt.Errorf("OU %q not found in response: %s", handle, body)
	}
	return id, nil
}

// GetAuthFlowID returns the ID of the authentication flow with the given handle.
func (c *ThunderClient) GetAuthFlowID(ctx context.Context, handle string) (string, error) {
	body, err := c.get(ctx, "/flows?flowType=AUTHENTICATION&limit=200")
	if err != nil {
		return "", fmt.Errorf("fetch auth flows: %w", err)
	}

	// Response may be an array or a wrapper object with a list field.
	var raw interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", fmt.Errorf("parse flows response: %w", err)
	}
	items := toSlice(raw)
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if m["handle"] == handle {
			if id, ok := m["id"].(string); ok && id != "" {
				return id, nil
			}
		}
	}
	return "", fmt.Errorf("auth flow %q not found", handle)
}

// ListApps returns a map of client_id → application ID for all registered apps.
func (c *ThunderClient) ListApps(ctx context.Context) (map[string]string, error) {
	body, err := c.get(ctx, "/applications?limit=200")
	if err != nil {
		return nil, fmt.Errorf("list applications: %w", err)
	}

	var raw interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse applications: %w", err)
	}

	result := map[string]string{}
	for _, item := range toSlice(raw) {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		clientID, _ := m["client_id"].(string)
		id, _ := m["id"].(string)
		if clientID != "" && id != "" {
			result[clientID] = id
		}
	}
	return result, nil
}

// UpsertApp creates or updates an OAuth application.
func (c *ThunderClient) UpsertApp(ctx context.Context, existingApps map[string]string, clientID string, payload map[string]interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if appID, exists := existingApps[clientID]; exists {
		_, err = c.request(ctx, http.MethodPut, "/applications/"+appID, data)
		return err
	}
	_, err = c.request(ctx, http.MethodPost, "/applications", data)
	return err
}

// AssignAdminRole assigns the given application to the "Administrator" role in Thunder.
func (c *ThunderClient) AssignAdminRole(ctx context.Context, apps map[string]string, systemClientID string) error {
	body, err := c.get(ctx, "/roles?limit=200")
	if err != nil {
		return fmt.Errorf("list roles: %w", err)
	}
	var raw interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("parse roles: %w", err)
	}

	adminRoleID := ""
	for _, item := range toSlice(raw) {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if m["name"] == "Administrator" {
			adminRoleID, _ = m["id"].(string)
			break
		}
	}
	if adminRoleID == "" {
		return nil // Administrator role not present — skip silently
	}

	appID, ok := apps[systemClientID]
	if !ok {
		return fmt.Errorf("system client %q not found in applications list", systemClientID)
	}

	payload, _ := json.Marshal(map[string]string{
		"role_id":        adminRoleID,
		"application_id": appID,
	})
	_, err = c.request(ctx, http.MethodPost, "/role-assignments", payload)
	return err
}

// get performs a GET request and returns the response body.
func (c *ThunderClient) get(ctx context.Context, path string) ([]byte, error) {
	return c.request(ctx, http.MethodGet, path, nil)
}

// request performs an authenticated HTTP request and returns the body.
func (c *ThunderClient) request(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s returned %d: %s", method, path, resp.StatusCode, respBody)
	}
	return respBody, nil
}

// toSlice normalises Thunder's inconsistent list response formats into a []interface{}.
func toSlice(v interface{}) []interface{} {
	if arr, ok := v.([]interface{}); ok {
		return arr
	}
	if m, ok := v.(map[string]interface{}); ok {
		for _, key := range []string{"applications", "flows", "data", "list", "items"} {
			if arr, ok := m[key].([]interface{}); ok {
				return arr
			}
		}
	}
	return nil
}
