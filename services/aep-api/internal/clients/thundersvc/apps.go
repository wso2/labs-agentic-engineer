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

package thundersvc

// Org applications: the per-org client_credentials apps AEP registers in the
// org's OU (aep-publisher-<org>, ae-studio-<org>), found by the Thunder entity
// id the caller stored, with one full list scan when that id misses.
//
// ThunderID 1.0.x has no server-side lookup by name or clientId: GET
// /applications reads no query parameter and returns up to
// thunderListLimit rows in one response. So a lookup is GET
// /applications/{storedID} (one request), and only on a miss (no id yet, a
// 404, or the id now names another app) one GET /applications scanned by
// clientId, which Thunder keeps unique across entities.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// OrgApp is one org application as Ensure found or made it. Secret is set
// only when Created: Thunder returns a client secret once, on create.
type OrgApp struct {
	EntityID string
	ClientID string
	Secret   string
	Created  bool
}

// OrgAppSpec names an org application. Name is both the application name and
// its clientId; OUID is the org's Thunder OU the app is registered under (its
// token's ouId/ouHandle); StoredID is the entity id the caller recorded last
// time, "" when it has none.
type OrgAppSpec struct {
	Name     string
	OUID     string
	StoredID string
}

// StudioAppName is the AE Studio client of an org.
//
//deadcode:keep wired by Task 1.13 (EnsureClient ensures the ae-studio-<org> app)
func StudioAppName(orgHandle string) string {
	return "ae-studio-" + orgHandle
}

// thunderListLimit is how many rows ThunderID 1.0.x returns from GET
// /applications at most (MaxCompositeStoreRecords). A scan that gets this
// many back may have missed the app it looks for.
const thunderListLimit = 1000

// errAppConflict is a create Thunder refused because the clientId is taken.
var errAppConflict = errors.New("thunder application clientId already exists")

//deadcode:keep wired by Task 1.13 (EnsureClient creates the ae-studio-<org> app)
func (c *client) EnsureOrgApp(ctx context.Context, spec OrgAppSpec) (OrgApp, error) {
	if spec.Name == "" || spec.OUID == "" {
		return OrgApp{}, fmt.Errorf("org app: name and OU required")
	}
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return OrgApp{}, fmt.Errorf("getSystemToken: %w", err)
	}
	id, err := c.findOrgApp(ctx, token, spec)
	if err != nil {
		return OrgApp{}, fmt.Errorf("find app %q: %w", spec.Name, err)
	}
	if id != "" {
		return OrgApp{EntityID: id, ClientID: spec.Name}, nil
	}
	return c.createOrgApp(ctx, token, spec.Name, spec.OUID)
}

//deadcode:keep wired by Task 1.13 (EnsureClient checks the app before healing its secret)
func (c *client) AppExists(ctx context.Context, spec OrgAppSpec) (string, error) {
	if spec.Name == "" {
		return "", fmt.Errorf("org app: name required")
	}
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return "", fmt.Errorf("getSystemToken: %w", err)
	}
	id, err := c.findOrgApp(ctx, token, spec)
	if err != nil {
		return "", fmt.Errorf("find app %q: %w", spec.Name, err)
	}
	return id, nil
}

//deadcode:keep wired by Task 1.13 (EnsureClient heals a lost secret after the vault write)
func (c *client) SetAppSecret(ctx context.Context, entityID, secret string) error {
	if entityID == "" || secret == "" {
		return fmt.Errorf("set app secret: entity id and secret required")
	}
	token, err := c.getSystemToken(ctx)
	if err != nil {
		return fmt.Errorf("getSystemToken: %w", err)
	}
	kept, err := c.putAppSecret(ctx, token, entityID, secret)
	if err != nil {
		return err
	}
	if kept != "" && kept != secret {
		return fmt.Errorf("thunder app %s kept a different client secret than the one sent", entityID)
	}
	slog.InfoContext(ctx, "thunder.app_secret_set", "appID", entityID)
	return nil
}

// findOrgApp returns the entity id of the app spec names, "" when absent:
// the stored id first, then one scan by clientId.
func (c *client) findOrgApp(ctx context.Context, token string, spec OrgAppSpec) (string, error) {
	if spec.StoredID != "" {
		app, found, err := c.getApp(ctx, token, spec.StoredID)
		if err != nil {
			return "", err
		}
		if found && appClientID(app) == spec.Name {
			return spec.StoredID, nil
		}
		slog.InfoContext(ctx, "thunder.app_stored_id_miss", "appName", spec.Name, "storedID", spec.StoredID, "found", found)
	}
	return c.scanApps(ctx, token, spec.Name)
}

// appClientID is the clientId of an application body: its oauth2 config's,
// else a top-level one.
func appClientID(app map[string]any) string {
	if cfg := inboundOAuthConfig(app); cfg != nil {
		if id, _ := cfg["clientId"].(string); id != "" {
			return id
		}
	}
	id, _ := app["clientId"].(string)
	return id
}

// getApp reads one application by entity id; found is false on a 404.
func (c *client) getApp(ctx context.Context, token, appID string) (map[string]any, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/applications/"+url.PathEscape(appID), nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("thunder get app: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, false, nil
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, false, fmt.Errorf("thunder get app returned %d: %s", resp.StatusCode, string(body))
	}
	var app map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&app); err != nil {
		return nil, false, fmt.Errorf("thunder get app decode: %w", err)
	}
	return app, true, nil
}

// thunderApp is one row of GET /applications.
type thunderApp struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ClientID string `json:"clientId"`
}

// scanApps reads the whole application list once and returns the entity id
// of the app with this clientId, "" when none has it.
func (c *client) scanApps(ctx context.Context, token, clientID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/applications", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("thunder list apps: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("thunder list apps read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("thunder list apps returned %d: %s", resp.StatusCode, string(body))
	}

	// Thunder can return either a bare array or a wrapped object.
	var apps []thunderApp
	if jerr := json.Unmarshal(body, &apps); jerr != nil {
		var wrapped struct {
			Applications []thunderApp `json:"applications"`
		}
		if werr := json.Unmarshal(body, &wrapped); werr != nil {
			return "", fmt.Errorf("thunder list apps decode: %w", jerr)
		}
		apps = wrapped.Applications
	}
	if len(apps) >= thunderListLimit {
		slog.WarnContext(ctx, "thunder.app_list_truncated", "rows", len(apps), "appName", clientID)
	}
	for _, app := range apps {
		if app.ClientID == clientID {
			return app.ID, nil
		}
	}
	return "", nil
}

// createOrgApp registers the app; a 409 (another writer registered the
// clientId since the lookup) resolves to that app by one more scan.
func (c *client) createOrgApp(ctx context.Context, token, name, ouID string) (OrgApp, error) {
	id, clientID, secret, err := c.createApp(ctx, token, name, ouID)
	if errors.Is(err, errAppConflict) {
		existing, serr := c.scanApps(ctx, token, name)
		if serr != nil {
			return OrgApp{}, fmt.Errorf("create app %q: conflict, then %w", name, serr)
		}
		if existing == "" {
			return OrgApp{}, fmt.Errorf("create app %q: %w, but no app has that clientId", name, err)
		}
		slog.InfoContext(ctx, "thunder.app_create_conflict_resolved", "appName", name, "appID", existing)
		return OrgApp{EntityID: existing, ClientID: name}, nil
	}
	if err != nil {
		return OrgApp{}, fmt.Errorf("create app %q under OU %s: %w", name, ouID, err)
	}
	slog.InfoContext(ctx, "thunder.app_created", "appName", name, "appID", id, "ouID", ouID)
	return OrgApp{EntityID: id, ClientID: clientID, Secret: secret, Created: true}, nil
}

// createApp registers an m2m client_credentials app under ouID whose clientId
// is its name and whose tokens carry the org fence (publisherTokenAttributes).
// Both org apps share this shape. Returns the entity id, the clientId and the
// one-time client secret; errAppConflict when the clientId is taken.
func (c *client) createApp(ctx context.Context, token, appName, ouID string) (string, string, string, error) {
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
		return "", "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("thunder create app: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
	case http.StatusConflict:
		return "", "", "", errAppConflict
	default:
		return "", "", "", fmt.Errorf("thunder create app returned %d: %s", resp.StatusCode, string(respBody))
	}

	// The success body carries the client secret: it never goes into an error
	// or a log line from here on.
	var result struct {
		ID           string `json:"id"`
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
		return "", "", "", fmt.Errorf("thunder create app: response is not an application")
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
	if result.ID == "" || cid == "" {
		return "", "", "", fmt.Errorf("thunder create app: response has no id or clientId")
	}
	return result.ID, cid, cs, nil
}

// putAppSecret rewrites an app with this client secret: GET, set
// inboundAuthConfig[0].config.clientSecret, PUT without the id. Returns the
// secret Thunder echoes back, "" when it echoes none.
func (c *client) putAppSecret(ctx context.Context, token, appID, secret string) (string, error) {
	app, found, err := c.getApp(ctx, token, appID)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("thunder app %s not found", appID)
	}
	if err := setInboundClientSecret(app, secret); err != nil {
		return "", fmt.Errorf("set client secret in app payload: %w", err)
	}
	delete(app, "id")

	putBody, _ := json.Marshal(app)
	putReq, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+"/applications/"+url.PathEscape(appID), bytes.NewReader(putBody))
	if err != nil {
		return "", err
	}
	putReq.Header.Set("Authorization", "Bearer "+token)
	putReq.Header.Set("Content-Type", "application/json")

	putResp, err := c.httpClient.Do(putReq)
	if err != nil {
		return "", fmt.Errorf("thunder put app secret: %w", err)
	}
	defer func() { _ = putResp.Body.Close() }()
	putRespBody, _ := io.ReadAll(putResp.Body)
	if putResp.StatusCode != http.StatusOK {
		detail := string(putRespBody)
		if strings.Contains(detail, secret) {
			detail = "(response withheld: it carries the secret)"
		}
		return "", fmt.Errorf("thunder put app secret returned %d: %s", putResp.StatusCode, detail)
	}

	var out struct {
		InboundAuth []struct {
			Config struct {
				ClientSecret string `json:"clientSecret"`
			} `json:"config"`
		} `json:"inboundAuthConfig"`
	}
	if err := json.Unmarshal(putRespBody, &out); err != nil {
		return "", fmt.Errorf("thunder put app secret: response is not an application")
	}
	if len(out.InboundAuth) == 0 {
		return "", nil
	}
	return out.InboundAuth[0].Config.ClientSecret, nil
}
