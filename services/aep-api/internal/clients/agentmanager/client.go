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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// ExternalAgentAPIType is Agent Manager's agent type for an agent AEP hosts
// itself. Registering one makes AMP create its own OpenChoreo component of type
// "proxy/"+ExternalAgentAPIType — AMP's record of the agent, which component
// listings drop (openchoreo.isAgentManagerMarker).
const ExternalAgentAPIType = "external-agent-api"

// The scope sets each call family needs. Narrow on purpose: a token is cached
// per set, and asking for everything everywhere would hand every request the
// broadest credential this client can hold.
const (
	scopeProvider = "amp:llm-provider:create amp:llm-provider:read amp:llm-provider:update amp:llm-provider-template:read amp:gateway:read"
	scopeAgent    = "amp:agent:create amp:agent:read amp:agent:update amp:project:read amp:org:view"
	// AN AGENT'S KEY IS NOT A PROVIDER KEY. The two families are gated by
	// different permissions and the pair is not interchangeable: a token
	// carrying `amp:llm-provider:api-key-manage` answers 403 on an agent's own
	// key, and the body ("insufficient permissions") names neither the scope
	// nor the call. An agent's key is issued against its own model-config
	// binding, never against the shared provider, so this is the only key scope
	// the govern stage needs.
	scopeModelKey = "amp:agent:api-key-manage amp:agent:read amp:project:read amp:org:view"
	// A THIRD KEY FAMILY. The tracing token is gated by neither of the two
	// key-manage permissions above: `amp:agent:api-key-manage` authorises an
	// agent's MODEL key and answers 403 on its tracing token. The body says
	// "insufficient permissions" and names no scope, so the only way to tell
	// the three apart is to have written them down.
	scopeTracingToken = "amp:agent:token-manage amp:agent:read amp:project:read amp:org:view"
)

// PermanentError marks a response no retry can fix — a 4xx. The govern stage
// fails its run on one of these rather than retrying until the budget is gone:
// a rejected payload or a missing permission is as wrong on the tenth attempt
// as on the first.
type PermanentError struct {
	Status int
	Body   string
}

func (e *PermanentError) Error() string {
	return fmt.Sprintf("agentmanager: request rejected with %d: %s", e.Status, e.Body)
}

// ProviderTemplate reads one template's metadata: the upstream URL and the auth
// header the provider body must carry.
func (c *client) ProviderTemplate(ctx context.Context, org, template string) (ProviderTemplate, error) {
	tok, err := c.token(ctx, scopeProvider)
	if err != nil {
		return ProviderTemplate{}, err
	}
	var out struct {
		Templates []struct {
			ID       string `json:"id"`
			Metadata struct {
				EndpointURL string `json:"endpointUrl"`
				Auth        struct {
					Type   string `json:"type"`
					Header string `json:"header"`
				} `json:"auth"`
			} `json:"metadata"`
		} `json:"templates"`
	}
	if err := c.do(ctx, tok, http.MethodGet,
		fmt.Sprintf("/orgs/%s/llm-provider-templates?limit=50", url.PathEscape(org)), nil, &out); err != nil {
		return ProviderTemplate{}, err
	}
	for _, t := range out.Templates {
		if t.ID == template {
			return ProviderTemplate{
				ID:          t.ID,
				EndpointURL: t.Metadata.EndpointURL,
				AuthType:    t.Metadata.Auth.Type,
				AuthHeader:  t.Metadata.Auth.Header,
			}, nil
		}
	}
	return ProviderTemplate{}, fmt.Errorf("agentmanager: no provider template %q in org %s", template, org)
}

// EnsureProvider creates the org's provider, or returns the one already there.
//
// The body is the shape AMP's console BUILDS, not the shape its form collects —
// the two differ, and the form's field names answer 400 "Invalid input".
// Setting `gateways` is what deploys it: there is no deploy call. Afterwards
// GET …/llm-providers/{id}/deployments reports DEPLOYED with
// metadata.auto_deployed true, while the provider's own status stays CREATED —
// so provider.status is not a deployment signal.
func (c *client) EnsureProvider(ctx context.Context, in EnsureProviderInput) (ProviderRef, error) {
	tok, err := c.token(ctx, scopeProvider)
	if err != nil {
		return ProviderRef{}, err
	}
	path := fmt.Sprintf("/orgs/%s/llm-providers", url.PathEscape(in.Org))

	var existing struct {
		Providers []struct {
			UUID string `json:"uuid"`
			ID   string `json:"id"`
		} `json:"providers"`
	}
	if err := c.do(ctx, tok, http.MethodGet, path, nil, &existing); err != nil {
		return ProviderRef{}, err
	}
	for _, p := range existing.Providers {
		if p.ID == in.ID {
			// It exists. Re-assert the org's key only when the caller says it
			// changed — the key is masked on read, so the caller is the only
			// one that can know, and a needless PUT redeploys every proxy this
			// provider serves. See EnsureProviderInput.ReassertCredential.
			if in.ReassertCredential {
				if err := c.updateProviderCredential(ctx, tok, p.UUID, in); err != nil {
					return ProviderRef{}, err
				}
			}
			return ProviderRef{UUID: p.UUID, Handle: p.ID, Context: in.Context}, nil
		}
	}

	body := map[string]any{
		"id": in.ID, "name": in.Name, "version": in.Version,
		"context": in.Context, "template": in.Template,
		"upstream": map[string]any{"main": map[string]any{
			"url": in.UpstreamURL,
			"auth": map[string]any{
				"type": in.AuthType, "header": in.AuthHeader, "value": in.APIKey,
			},
		}},
		"security": map[string]any{"enabled": true, "apiKey": map[string]any{
			"enabled": true, "key": "X-API-Key", "in": "header",
		}},
		"gateways":      []string{in.GatewayID},
		"accessControl": map[string]any{"exceptions": []string{}, "mode": "allow_all"},
	}
	var created struct {
		UUID string `json:"uuid"`
		ID   string `json:"id"`
	}
	if err := c.do(ctx, tok, http.MethodPost, path, body, &created); err != nil {
		return ProviderRef{}, err
	}
	if created.ID == "" {
		created.ID = in.ID
	}
	return ProviderRef{UUID: created.UUID, Handle: created.ID, Context: in.Context}, nil
}

// updateProviderCredential writes the org's current key onto an existing
// provider.
func (c *client) updateProviderCredential(ctx context.Context, token, providerUUID string, in EnsureProviderInput) error {
	body := map[string]any{
		"id": in.ID, "name": in.Name, "version": in.Version,
		"context": in.Context, "template": in.Template,
		"upstream": map[string]any{"main": map[string]any{
			"url": in.UpstreamURL,
			"auth": map[string]any{
				"type": in.AuthType, "header": in.AuthHeader, "value": in.APIKey,
			},
		}},
		"security": map[string]any{"enabled": true, "apiKey": map[string]any{
			"enabled": true, "key": "X-API-Key", "in": "header",
		}},
		"gateways":      []string{in.GatewayID},
		"accessControl": map[string]any{"exceptions": []string{}, "mode": "allow_all"},
	}
	// NO `policies` field. Guardrails on this provider are the operator's, and
	// sending the key must not silently drop the PII policy they attached.
	return c.do(ctx, token, http.MethodPut,
		fmt.Sprintf("/orgs/%s/llm-providers/%s", url.PathEscape(in.Org), url.PathEscape(providerUUID)),
		body, nil)
}

// do performs one request and maps the response onto this package's error
// vocabulary: 409 is success, 4xx is permanent, 5xx is worth retrying.
//
// A 401 is retried ONCE with a freshly minted token, because the cached one can
// be dead before it is expired — see invalidateToken. The retry is here rather
// than at the call sites so every one of them gets it, and it is bounded at one
// attempt: a 401 that survives a fresh token is a real authorization fault, and
// retrying it further would spin against the IdP.
func (c *client) do(ctx context.Context, token, method, path string, in, out any) error {
	err := c.doOnce(ctx, token, method, path, in, out)
	var perm *PermanentError
	if !errors.As(err, &perm) || perm.Status != http.StatusUnauthorized {
		return err
	}
	scopes := c.scopesFor(token)
	c.invalidateToken(scopes)
	fresh, terr := c.token(ctx, scopes)
	if terr != nil {
		return err // the original 401 is the more useful answer
	}
	return c.doOnce(ctx, fresh, method, path, in, out)
}

// scopesFor answers which scope set a token was minted for, so a rejection
// invalidates the right cache entry.
func (c *client) scopesFor(token string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cached == token {
		return c.cachedScopes
	}
	return ""
}

func (c *client) doOnce(ctx context.Context, token, method, path string, in, out any) error {
	var rdr io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("agentmanager: encode %s %s: %w", method, path, err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, rdr)
	if err != nil {
		return fmt.Errorf("agentmanager: build %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("agentmanager: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)

	switch {
	case resp.StatusCode == http.StatusConflict:
		// Somebody else created it between our read and our write. Not an
		// error: two deploys racing the same agent is ordinary.
		return nil
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return &PermanentError{Status: resp.StatusCode, Body: string(raw)}
	case resp.StatusCode >= 500:
		return fmt.Errorf("agentmanager: %s %s returned %d", method, path, resp.StatusCode)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("agentmanager: decode %s %s: %w", method, path, err)
		}
	}
	return nil
}

// EnsureAgent registers the agent as externally hosted, or returns the one
// already registered.
//
// The body carries no endpoint, deliberately: AMP's own registration form
// collects name, displayName, description and labels only, and
// …/agents/{agent}/endpoints is a read. The record is a governance identity.
func (c *client) EnsureAgent(ctx context.Context, in EnsureAgentInput) (AgentRef, error) {
	tok, err := c.token(ctx, scopeAgent)
	if err != nil {
		return AgentRef{}, err
	}
	base := fmt.Sprintf("/orgs/%s/projects/%s/agents",
		url.PathEscape(in.Org), url.PathEscape(in.Project))

	var list struct {
		Agents []struct {
			Name string `json:"name"`
		} `json:"agents"`
	}
	if err := c.do(ctx, tok, http.MethodGet, base, nil, &list); err != nil {
		return AgentRef{}, err
	}
	for _, a := range list.Agents {
		if a.Name == in.Name {
			return AgentRef{Name: a.Name}, nil
		}
	}

	body := map[string]any{
		"name": in.Name, "displayName": in.DisplayName, "description": in.Description,
		"provisioning": map[string]any{"type": "external"},
		"agentType":    map[string]any{"type": ExternalAgentAPIType, "subType": "custom-api"},
	}
	var created struct {
		Name string `json:"name"`
	}
	if err := c.do(ctx, tok, http.MethodPost, base, body, &created); err != nil {
		return AgentRef{}, err
	}
	if created.Name == "" {
		created.Name = in.Name
	}
	return AgentRef{Name: created.Name}, nil
}

// modelConfigPath addresses one agent's model configs.
func (c *client) modelConfigPath(org, project, agent string) string {
	return fmt.Sprintf("/orgs/%s/projects/%s/agents/%s/model-configs",
		url.PathEscape(org), url.PathEscape(project), url.PathEscape(agent))
}

// modelConfig is the shape both the list and the create response carry.
type modelConfig struct {
	UUID        string `json:"uuid"`
	Name        string `json:"name"`
	EnvMappings map[string]struct {
		Configuration struct {
			URL string `json:"url"`
		} `json:"configuration"`
	} `json:"envMappings"`
}

// proxyURL is the per-agent address Agent Manager generated for this mapping.
func (m modelConfig) proxyURL(environment string) string {
	return m.EnvMappings[environment].Configuration.URL
}

// EnsureModelConfig binds this agent to the org's provider for one environment.
//
// The binding is what Agent Manager governs by: the console shows the provider
// under the agent, a guardrail attaches to THIS agent's traffic, and usage is
// attributed to it. AMP answers with a proxy of its own — a generated URL that
// is read back rather than constructed, because nothing outside AMP can derive
// it.
func (c *client) EnsureModelConfig(ctx context.Context, in EnsureModelConfigInput) (ModelConfigRef, error) {
	tok, err := c.token(ctx, scopeAgent)
	if err != nil {
		return ModelConfigRef{}, err
	}
	base := c.modelConfigPath(in.Org, in.Project, in.Agent)

	var list struct {
		Configs []modelConfig `json:"configs"`
	}
	if err := c.do(ctx, tok, http.MethodGet, base, nil, &list); err != nil {
		return ModelConfigRef{}, err
	}
	for _, cfg := range list.Configs {
		if cfg.Name == in.Name {
			// THE LIST CARRIES NO envMappings — only the item read does, and
			// the proxy URL lives in there. Trusting the list here returns an
			// empty address for every agent that already has a binding, which
			// is every redeploy after the first.
			return c.readModelConfig(ctx, tok, base, cfg.UUID, in.Environment)
		}
	}

	body := map[string]any{
		"name": in.Name,
		"type": "llm",
		"envMappings": map[string]any{
			in.Environment: map[string]any{
				"providerName":  in.ProviderHandle,
				"configuration": map[string]any{},
			},
		},
		// Declaring the variable names is what lets Agent Manager show an
		// operator which env vars this agent reads its model access from.
		"environmentVariables": []map[string]string{
			{"key": "url", "name": in.URLVar},
			{"key": "apikey", "name": in.APIKeyVar},
		},
	}
	var created modelConfig
	if err := c.do(ctx, tok, http.MethodPost, base, body, &created); err != nil {
		return ModelConfigRef{}, err
	}
	if created.UUID == "" {
		return ModelConfigRef{}, fmt.Errorf("agentmanager: POST %s returned no uuid", base)
	}
	if url := created.proxyURL(in.Environment); url != "" {
		return ModelConfigRef{ConfigID: created.UUID, ProxyURL: url}, nil
	}
	// A create that answered without the mapping is read back rather than
	// treated as a failure: the binding exists either way, and the next deploy
	// would only find it through the list — which carries no proxy URL at all.
	return c.readModelConfig(ctx, tok, base, created.UUID, in.Environment)
}

// readModelConfig reads one binding and takes the proxy URL out of it.
func (c *client) readModelConfig(ctx context.Context, tok, base, uuid, environment string) (ModelConfigRef, error) {
	var got modelConfig
	if err := c.do(ctx, tok, http.MethodGet, base+"/"+url.PathEscape(uuid), nil, &got); err != nil {
		return ModelConfigRef{}, err
	}
	return ModelConfigRef{ConfigID: uuid, ProxyURL: got.proxyURL(environment)}, nil
}

// modelKeyPath addresses the keys issued against one (config, environment).
func (c *client) modelKeyPath(in ModelKeyRef) string {
	return c.modelConfigPath(in.Org, in.Project, in.Agent) +
		fmt.Sprintf("/%s/environments/%s/api-keys",
			url.PathEscape(in.ConfigID), url.PathEscape(in.Environment))
}

// ListModelKeys names the keys this binding holds.
//
// Only NAMES come back — a key's value is returned once, at creation, and never
// again, which is why the govern stage reconciles on presence rather than by
// comparing values.
func (c *client) ListModelKeys(ctx context.Context, in ModelKeyRef) ([]string, error) {
	tok, err := c.token(ctx, scopeModelKey)
	if err != nil {
		return nil, err
	}
	var out struct {
		Keys []struct {
			Name string `json:"name"`
		} `json:"keys"`
	}
	if err := c.do(ctx, tok, http.MethodGet, c.modelKeyPath(in), nil, &out); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(out.Keys))
	for _, k := range out.Keys {
		if k.Name != "" {
			names = append(names, k.Name)
		}
	}
	return names, nil
}

// IssueModelKey creates this agent's key on its own proxy. The value in the
// response is the only copy that will ever exist.
func (c *client) IssueModelKey(ctx context.Context, in ModelKeyRef, keyName string) (IssuedKey, error) {
	return c.writeModelKey(ctx, http.MethodPost, c.modelKeyPath(in), keyName)
}

// RotateModelKey replaces a key whose value AEP cannot read.
//
// The old value is unrecoverable, so rotation is the only route back to a known
// state. It is safe here ONLY because a deploy is in flight to carry the new
// value — the same call from a background loop would cut off a running agent.
func (c *client) RotateModelKey(ctx context.Context, in ModelKeyRef, keyName string) (IssuedKey, error) {
	return c.writeModelKey(ctx, http.MethodPut,
		c.modelKeyPath(in)+"/"+url.PathEscape(keyName), keyName)
}

// IssueTracingToken mints this agent's OTLP credential.
//
// TWO ENDPOINTS LOOK LIKE THIS ONE AND ARE NOT IT.
// `POST …/agents/{a}/tracing-token/regenerate` answers 200 with
// {environmentName, expiresAt, rotatedAt} — expiry metadata and NO token. A
// mint pointed there succeeds, stores nothing, and the agent exports spans it
// cannot authenticate. Only `…/token` discloses the value, and Agent Manager's
// own console says why it is read here and never again: "Copy it now as you
// won't be able to see it again."
//
// `environment` is a QUERY parameter and is required. Omitted, amp-api answers
// 500 "Failed to generate token" — a server error for an incomplete request,
// so nothing in the response suggests the caller left something out.
func (c *client) IssueTracingToken(ctx context.Context, in TracingTokenRef) (TracingToken, error) {
	tok, err := c.token(ctx, scopeTracingToken)
	if err != nil {
		return TracingToken{}, err
	}
	path := fmt.Sprintf("/orgs/%s/projects/%s/agents/%s/token?environment=%s",
		url.PathEscape(in.Org), url.PathEscape(in.Project),
		url.PathEscape(in.Agent), url.QueryEscape(in.Environment))

	// snake_case, unlike every other response this client reads.
	var out struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := c.do(ctx, tok, http.MethodPost, path, nil, &out); err != nil {
		return TracingToken{}, err
	}
	if out.Token == "" {
		// Same reasoning as writeModelKey: a credential we cannot read is
		// worse than none, because the caller would store nothing and believe
		// it had stored something.
		return TracingToken{}, fmt.Errorf("agentmanager: POST %s returned no token", path)
	}
	return TracingToken{Token: out.Token, ExpiresAt: out.ExpiresAt}, nil
}

func (c *client) writeModelKey(ctx context.Context, method, path, name string) (IssuedKey, error) {
	tok, err := c.token(ctx, scopeModelKey)
	if err != nil {
		return IssuedKey{}, err
	}
	var out struct {
		APIKey string `json:"apiKey"`
		KeyID  string `json:"keyId"`
	}
	if err := c.do(ctx, tok, method, path,
		map[string]any{"name": name, "displayName": name}, &out); err != nil {
		return IssuedKey{}, err
	}
	if out.APIKey == "" {
		// A key we cannot read is worse than none: the caller would store
		// nothing and believe it had stored something.
		return IssuedKey{}, fmt.Errorf("agentmanager: %s %s returned no apiKey", method, path)
	}
	return IssuedKey{APIKey: out.APIKey, KeyID: out.KeyID}, nil
}
