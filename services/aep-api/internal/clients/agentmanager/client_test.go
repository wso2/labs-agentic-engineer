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
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// A mint without a scope parameter is the single most expensive mistake against
// this platform IdP: it returns HTTP 200 and a token whose aud is the client id
// and which carries no scope claim, so amp-api answers 403 on every call
// afterwards with nothing naming the cause. Agent Manager's own AI gateway
// chart ships a bootstrap Job with exactly this bug.
func TestMintRequestsExplicitScopes(t *testing.T) {
	var gotForm url.Values
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 3600})
	}))
	defer idp.Close()

	c := New(Config{TokenURL: idp.URL, ClientID: "amp-publisher-aep",
		ClientSecret: "s", Resource: "urn:wso2:amp"}).(*client)

	tok, err := c.token(context.Background(), "amp:agent:read")
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if tok != "tok" {
		t.Fatalf("token = %q, want tok", tok)
	}
	if got := gotForm.Get("scope"); got != "amp:agent:read" {
		t.Errorf("scope = %q, want the requested scope to be sent", got)
	}
	if got := gotForm.Get("resource"); got != "urn:wso2:amp" {
		t.Errorf("resource = %q, want urn:wso2:amp", got)
	}
}

func TestEnsureProviderSendsTheBuilderShape(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/token"):
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/llm-providers"):
			_ = json.NewEncoder(w).Encode(map[string]any{"providers": []any{}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/llm-providers"):
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"uuid": "prov-uuid", "id": "aep-default-anthropic"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	ref, err := c.EnsureProvider(context.Background(), EnsureProviderInput{
		Org: "default", ID: "aep-default-anthropic", Name: "AEP Default Anthropic",
		Version: "v1.0", Context: "/aep-default-anthropic", Template: "anthropic",
		UpstreamURL: "https://api.anthropic.com", AuthType: "api-key",
		AuthHeader: "x-api-key", APIKey: "sk-ant-secret", GatewayID: "gw-1",
	})
	if err != nil {
		t.Fatalf("EnsureProvider: %v", err)
	}
	if ref.UUID != "prov-uuid" || ref.Handle != "aep-default-anthropic" {
		t.Fatalf("ref = %+v", ref)
	}

	upstream := body["upstream"].(map[string]any)["main"].(map[string]any)
	auth := upstream["auth"].(map[string]any)
	if auth["header"] != "x-api-key" || auth["value"] != "sk-ant-secret" {
		t.Errorf("upstream auth = %+v; the ORG key rides upstream.main.auth.value", auth)
	}
	sec := body["security"].(map[string]any)["apiKey"].(map[string]any)
	if sec["key"] != "X-API-Key" || sec["enabled"] != true {
		t.Errorf("security.apiKey = %+v; this is what lets an agent authenticate with its OWN key", sec)
	}
	gws, ok := body["gateways"].([]any)
	if !ok || len(gws) != 1 || gws[0] != "gw-1" {
		t.Errorf("gateways = %v; setting it is what DEPLOYS the provider (there is no deploy call)", body["gateways"])
	}
	if body["template"] != "anthropic" {
		t.Errorf("template = %v, want the template handle", body["template"])
	}
}

func TestProviderTemplateReadsAuthMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/token"):
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case strings.Contains(r.URL.Path, "/llm-provider-templates"):
			_ = json.NewEncoder(w).Encode(map[string]any{"templates": []map[string]any{
				{"id": "openai", "metadata": map[string]any{"endpointUrl": "https://api.openai.com"}},
				{"id": "anthropic", "metadata": map[string]any{
					"endpointUrl": "https://api.anthropic.com",
					"auth":        map[string]any{"type": "api-key", "header": "x-api-key"},
				}},
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	got, err := c.ProviderTemplate(context.Background(), "default", "anthropic")
	if err != nil {
		t.Fatalf("ProviderTemplate: %v", err)
	}
	if got.AuthHeader != "x-api-key" || got.AuthType != "api-key" ||
		got.EndpointURL != "https://api.anthropic.com" {
		t.Fatalf("template = %+v", got)
	}
}

func TestEnsureAgentRegistersExternallyHosted(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/token"):
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/agents"):
			_ = json.NewEncoder(w).Encode(map[string]any{"agents": []any{}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/agents"):
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "checkout-agent"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	ref, err := c.EnsureAgent(context.Background(), EnsureAgentInput{
		Org: "default", Project: "shop", Name: "checkout-agent",
		DisplayName: "Checkout Agent", Description: "built by AEP",
	})
	if err != nil {
		t.Fatalf("EnsureAgent: %v", err)
	}
	if ref.Name != "checkout-agent" {
		t.Fatalf("ref = %+v", ref)
	}
	prov := body["provisioning"].(map[string]any)
	if prov["type"] != "external" {
		t.Errorf("provisioning.type = %v, want external", prov["type"])
	}
	at := body["agentType"].(map[string]any)
	if at["type"] != "external-agent-api" || at["subType"] != "custom-api" {
		t.Errorf("agentType = %+v", at)
	}
	if _, present := body["url"]; present {
		t.Errorf("registration body carries a url; AMP's own form has no such field")
	}
}

// An agent that already exists must not be created twice: a redeploy is the
// common case, not the exception.
func TestEnsureAgentReusesAnExistingRecord(t *testing.T) {
	posted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/token"):
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/agents"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"agents": []map[string]any{{"name": "checkout-agent"}}})
		case r.Method == http.MethodPost:
			posted = true
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	if _, err := c.EnsureAgent(context.Background(), EnsureAgentInput{
		Org: "default", Project: "shop", Name: "checkout-agent",
	}); err != nil {
		t.Fatalf("EnsureAgent: %v", err)
	}
	if posted {
		t.Error("created a second agent record for one that already exists")
	}
}

func TestIssueModelKeyReturnsTheOneTimeValue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/token"):
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/api-keys"):
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"apiKey": "amp-key-value", "keyId": "agent-key",
				"status": "success", "message": "API key created and broadcasted to 1 gateway(s)"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	got, err := c.IssueModelKey(context.Background(), ModelKeyRef{
		Org: "default", Project: "shop", Agent: "checkout-agent", ConfigID: "cfg-uuid", Environment: "default",
	}, "aep-checkout-agent-default")
	if err != nil {
		t.Fatalf("IssueModelKey: %v", err)
	}
	if got.APIKey != "amp-key-value" || got.KeyID != "agent-key" {
		t.Fatalf("issued = %+v", got)
	}
}

// A response with no apiKey must be an error, never a silent empty string: the
// caller would store nothing while believing the agent had a credential.
func TestIssueModelKeyRefusesAnEmptyValue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/token"):
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		default:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"keyId": "agent-key"})
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	if _, err := c.IssueModelKey(context.Background(), ModelKeyRef{
		Org: "default", Project: "shop", Agent: "checkout-agent", ConfigID: "cfg-uuid", Environment: "default",
	}, "aep-a-default"); err == nil {
		t.Fatal("want an error when AMP returns no apiKey")
	}
}

func TestListModelKeysNamesExistingKeys(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/token"):
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/api-keys"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"keys": []map[string]any{{"name": "aep-checkout-agent-default", "status": "active"}}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	names, err := c.ListModelKeys(context.Background(), ModelKeyRef{
		Org: "default", Project: "shop", Agent: "checkout-agent", ConfigID: "cfg-uuid", Environment: "default",
	})
	if err != nil {
		t.Fatalf("ListModelKeys: %v", err)
	}
	if len(names) != 1 || names[0] != "aep-checkout-agent-default" {
		t.Fatalf("names = %v", names)
	}
}

// A 4xx is permanent: retrying a rejected payload or a missing permission wastes
// the run's budget and reports the wrong cause.
func TestFourXXIsPermanent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/token") {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"FORBIDDEN","message":"insufficient permissions"}`))
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	_, err := c.ListModelKeys(context.Background(), ModelKeyRef{
		Org: "default", Project: "shop", Agent: "checkout-agent", ConfigID: "cfg-uuid", Environment: "default",
	})
	var perm *PermanentError
	if !errors.As(err, &perm) || perm.Status != http.StatusForbidden {
		t.Fatalf("err = %v, want a PermanentError carrying 403", err)
	}
}

// A ROTATED key must reach a provider that already exists — returning early on
// "it exists" is what lets a stale copy go unnoticed until every governed agent
// in the org starts failing at Anthropic.
//
// The write is conditional now (ReassertCredential), because a provider update
// redeploys every proxy bound to it; this covers the case where the caller has
// determined the key changed. Its twin,
// TestEnsureProviderSkipsTheCredentialWriteWhenUnchanged, covers the other.
func TestEnsureProviderReassertsTheKeyWhenItExists(t *testing.T) {
	var put map[string]any
	putPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/token"):
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/llm-providers"):
			_ = json.NewEncoder(w).Encode(map[string]any{"providers": []map[string]any{
				{"uuid": "prov-uuid", "id": "aep-default-anthropic"}}})
		case r.Method == http.MethodPut:
			putPath = r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(&put)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.Method == http.MethodPost:
			t.Error("created a second provider instead of updating the existing one")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	ref, err := c.EnsureProvider(context.Background(), EnsureProviderInput{
		ReassertCredential: true,
		Org:                "default", ID: "aep-default-anthropic", Name: "AEP Default Anthropic",
		Version: "v1.0", Context: "/aep-default-anthropic", Template: "anthropic",
		UpstreamURL: "https://api.anthropic.com", AuthType: "api-key",
		AuthHeader: "x-api-key", APIKey: "sk-ant-ROTATED", GatewayID: "gw-1",
	})
	if err != nil {
		t.Fatalf("EnsureProvider: %v", err)
	}
	if ref.UUID != "prov-uuid" {
		t.Fatalf("ref = %+v", ref)
	}
	if !strings.HasSuffix(putPath, "/llm-providers/prov-uuid") {
		t.Fatalf("PUT path = %q, want the provider's own resource", putPath)
	}
	auth := put["upstream"].(map[string]any)["main"].(map[string]any)["auth"].(map[string]any)
	if auth["value"] != "sk-ant-ROTATED" {
		t.Errorf("upstream auth value = %v, want the rotated key", auth["value"])
	}
	// Guardrails on this provider belong to the operator. Sending policies here
	// would erase the PII policy they attached the moment a key is re-asserted.
	if _, present := put["policies"]; present {
		t.Error("update sent a policies field; it would overwrite the operator's guardrails")
	}
}

// The token endpoint is reached through a gateway that routes by Host.
//
// Without this the request carries the URL's own host — an internal address
// like k3d-openchoreo-serverlb — which matches no vhost, and the gateway
// answers 404. That reads like a wrong token path and is not one: it cost a
// deploy in testing before the header was carried.
func TestMintSendsTheConfiguredHostHeader(t *testing.T) {
	var gotHost string
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
	}))
	defer idp.Close()

	c := New(Config{TokenURL: idp.URL, ClientID: "id", ClientSecret: "s",
		HostHeader: "thunder.openchoreo.localhost"}).(*client)
	if _, err := c.token(context.Background(), "amp:agent:read"); err != nil {
		t.Fatalf("token: %v", err)
	}
	if gotHost != "thunder.openchoreo.localhost" {
		t.Errorf("Host = %q, want the configured vhost", gotHost)
	}
}

// With no host header configured the URL's own host stands — the case where the
// address already routes.
func TestMintLeavesTheHostAloneWhenUnset(t *testing.T) {
	var gotHost string
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
	}))
	defer idp.Close()

	c := New(Config{TokenURL: idp.URL, ClientID: "id", ClientSecret: "s"}).(*client)
	if _, err := c.token(context.Background(), "amp:agent:read"); err != nil {
		t.Fatalf("token: %v", err)
	}
	if gotHost == "thunder.openchoreo.localhost" || gotHost == "" {
		t.Errorf("Host = %q, want the URL's own host", gotHost)
	}
}

// The proxy URL is GENERATED by Agent Manager, per agent. Nothing outside AMP
// can derive it, so creating a binding and failing to read it back would leave
// the agent with no address to call.
func TestEnsureModelConfigReadsBackTheGeneratedProxyURL(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/token"):
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/model-configs"):
			_ = json.NewEncoder(w).Encode(map[string]any{"configs": []any{}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/model-configs"):
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"uuid": "cfg-uuid",
				"envMappings": map[string]any{"default": map[string]any{
					"providerName":  "aep-default-anthropic",
					"configuration": map[string]any{"url": "http://ai-gateway.amp.localhost:8084/aep-checkout-3d748f50"},
				}},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	got, err := c.EnsureModelConfig(context.Background(), EnsureModelConfigInput{
		Org: "default", Project: "shop", Agent: "checkout-agent", Name: "aep-checkout-agent",
		Environment: "default", ProviderHandle: "aep-default-anthropic",
		URLVar: "MODEL_ENDPOINT", APIKeyVar: "MODEL_API_KEY",
	})
	if err != nil {
		t.Fatalf("EnsureModelConfig: %v", err)
	}
	if got.ConfigID != "cfg-uuid" {
		t.Errorf("configID = %q", got.ConfigID)
	}
	if got.ProxyURL != "http://ai-gateway.amp.localhost:8084/aep-checkout-3d748f50" {
		t.Errorf("proxyURL = %q", got.ProxyURL)
	}
	// The environment keys the mapping: a provider is deployed per environment,
	// and a body without it binds the agent to nothing.
	envs, _ := body["envMappings"].(map[string]any)
	if _, ok := envs["default"]; !ok {
		t.Errorf("envMappings = %v, want the environment as its key", envs)
	}
}

// A second deploy must not create a second binding — which would leave the agent
// with two proxies and a key on whichever one AMP listed first.
func TestEnsureModelConfigIsIdempotent(t *testing.T) {
	posted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/token"):
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		// THE LIST CARRIES NO envMappings. This is the real API's shape, and
		// the reason the reuse path has to read the item back: a list-shaped
		// fake here hid an empty MODEL_ENDPOINT on every redeploy after the
		// first.
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/model-configs"):
			_ = json.NewEncoder(w).Encode(map[string]any{"configs": []map[string]any{
				{"uuid": "cfg-uuid", "name": "aep-checkout-agent", "type": "llm"},
			}})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/model-configs/cfg-uuid"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"uuid": "cfg-uuid", "name": "aep-checkout-agent",
				"envMappings": map[string]any{"default": map[string]any{
					"configuration": map[string]any{"url": "http://ai-gateway.amp.localhost:8084/aep-checkout-3d748f50"},
				}},
			})
		default:
			posted = true
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"uuid": "second"})
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	got, err := c.EnsureModelConfig(context.Background(), EnsureModelConfigInput{
		Org: "default", Project: "shop", Agent: "checkout-agent", Name: "aep-checkout-agent",
		Environment: "default", ProviderHandle: "aep-default-anthropic",
	})
	if err != nil {
		t.Fatalf("EnsureModelConfig: %v", err)
	}
	if posted {
		t.Error("created a second binding for one that already exists")
	}
	if got.ProxyURL != "http://ai-gateway.amp.localhost:8084/aep-checkout-3d748f50" {
		t.Errorf("proxyURL = %q — a reused binding must be read back, not taken from the list", got.ProxyURL)
	}
}

// AN AGENT'S KEY IS NOT A PROVIDER KEY, and the difference is only visible as a
// 403 whose body names neither the scope nor the call. This pins the scope the
// key calls actually ask for.
func TestModelKeyCallsRequestTheAgentKeyScope(t *testing.T) {
	var scopes string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/token"):
			_ = r.ParseForm()
			scopes = r.PostFormValue("scope")
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{}})
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	if _, err := c.ListModelKeys(context.Background(), ModelKeyRef{
		Org: "default", Project: "shop", Agent: "checkout-agent",
		ConfigID: "cfg-uuid", Environment: "default",
	}); err != nil {
		t.Fatalf("ListModelKeys: %v", err)
	}
	if !strings.Contains(scopes, "amp:agent:api-key-manage") {
		t.Errorf("requested scopes = %q, want the agent key-manage scope", scopes)
	}
	if strings.Contains(scopes, "amp:llm-provider:api-key-manage") {
		t.Errorf("requested scopes = %q — the PROVIDER key scope does not authorise an agent's key", scopes)
	}
}

// THE OVERNIGHT OUTAGE, in a test.
//
// A token is cached for its full hour, but the platform IdP restarting
// invalidates every token it issued. Without a 401-triggered re-mint the client
// keeps presenting the dead one until its own clock expires it — which is how
// one IdP restart became hours of `401 INVALID_TOKEN` on the govern stage.
func TestA401ReMintsTheTokenAndRetriesOnce(t *testing.T) {
	var minted int
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/token") {
			minted++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": fmt.Sprintf("token-%d", minted), "expires_in": 3600})
			return
		}
		seen = append(seen, r.Header.Get("Authorization"))
		// The first token is the one the IdP restart invalidated.
		if r.Header.Get("Authorization") == "Bearer token-1" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"UNAUTHORIZED","reason":"unauthorized: INVALID_TOKEN"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{}})
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	if _, err := c.ListModelKeys(context.Background(), ModelKeyRef{
		Org: "default", Project: "shop", Agent: "checkout-agent",
		ConfigID: "cfg", Environment: "default",
	}); err != nil {
		t.Fatalf("ListModelKeys: %v", err)
	}
	if minted != 2 {
		t.Errorf("minted %d tokens, want 2 — the rejected one must be re-minted", minted)
	}
	if len(seen) != 2 || seen[1] != "Bearer token-2" {
		t.Errorf("requests = %v, want the retry to carry the FRESH token", seen)
	}
}

// The retry is bounded at one. A 401 that survives a fresh token is a real
// authorization fault, and spinning on it would hammer the IdP.
func TestAPersistent401IsNotRetriedForever(t *testing.T) {
	var minted, calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/token") {
			minted++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": fmt.Sprintf("t%d", minted), "expires_in": 3600})
			return
		}
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"UNAUTHORIZED"}`))
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	_, err := c.ListModelKeys(context.Background(), ModelKeyRef{
		Org: "default", Project: "shop", Agent: "checkout-agent",
		ConfigID: "cfg", Environment: "default",
	})
	var perm *PermanentError
	if !errors.As(err, &perm) || perm.Status != http.StatusUnauthorized {
		t.Fatalf("err = %v, want a PermanentError carrying 401", err)
	}
	if calls != 2 {
		t.Errorf("made %d attempts, want exactly 2 (original + one retry)", calls)
	}
}

// A provider update redeploys every LLM proxy bound to it, and a redeploy is
// the window in which a proxy can lose its broadcast API keys. So an existing
// provider is left alone unless the caller says the credential changed.
func TestEnsureProviderSkipsTheCredentialWriteWhenUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reassert bool
		wantPut  bool
	}{
		{"unchanged: no write", false, false},
		{"changed: write", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			put := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/oauth2/token"):
					_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/llm-providers"):
					_ = json.NewEncoder(w).Encode(map[string]any{"providers": []map[string]any{
						{"uuid": "prov-uuid", "id": "aep-default-anthropic"}}})
				case r.Method == http.MethodPut:
					put = true
					_ = json.NewEncoder(w).Encode(map[string]any{})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()

			c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
			if _, err := c.EnsureProvider(context.Background(), EnsureProviderInput{
				Org: "default", ID: "aep-default-anthropic", APIKey: "sk-ant",
				ReassertCredential: tc.reassert,
			}); err != nil {
				t.Fatalf("EnsureProvider: %v", err)
			}
			if put != tc.wantPut {
				t.Errorf("credential written = %v, want %v", put, tc.wantPut)
			}
		})
	}
}

// The tracing token comes back from a DIFFERENT call than the one whose name
// suggests it. `…/tracing-token/regenerate` answers 200 with expiry metadata
// and no value at all; only `…/token` discloses the token, and only once. A
// mint pointed at the regenerate path therefore succeeds, stores nothing, and
// leaves the agent exporting traces it cannot authenticate.
func TestIssueTracingTokenReadsTheDisclosedValue(t *testing.T) {
	var gotPath, gotQuery, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/token") {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
			return
		}
		gotPath, gotQuery, gotMethod = r.URL.Path, r.URL.RawQuery, r.Method
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": "eyJhbGciOiJSUzI1NiJ9.payload.sig",
			// snake_case, unlike every other response this client reads.
			"expires_at": 1797335009, "issued_at": 1789559009,
		})
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	got, err := c.IssueTracingToken(context.Background(), TracingTokenRef{
		Org: "default", Project: "shop", Agent: "checkout-agent", Environment: "default",
	})
	if err != nil {
		t.Fatalf("IssueTracingToken: %v", err)
	}
	if got.Token != "eyJhbGciOiJSUzI1NiJ9.payload.sig" {
		t.Fatalf("token = %q", got.Token)
	}
	if got.ExpiresAt != 1797335009 {
		t.Errorf("expiresAt = %d, want the disclosed expiry", got.ExpiresAt)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if want := "/orgs/default/projects/shop/agents/checkout-agent/token"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	// Without the environment, amp-api answers 500 "Failed to generate token" —
	// a server error for a missing parameter, so nothing about the response
	// says the request was incomplete.
	if got := gotQuery; got != "environment=default" {
		t.Errorf("query = %q, want environment=default", got)
	}
}

func TestIssueTracingTokenRefusesAnEmptyValue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/token") {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
			return
		}
		// The regenerate endpoint's shape: valid JSON, 200, no token.
		_ = json.NewEncoder(w).Encode(map[string]any{"expiresAt": 1797335009, "rotatedAt": 1789559009})
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	if _, err := c.IssueTracingToken(context.Background(), TracingTokenRef{
		Org: "default", Project: "shop", Agent: "checkout-agent", Environment: "default",
	}); err == nil {
		t.Fatal("want an error when AMP discloses no token")
	}
}

// The tracing token is gated by amp:agent:token-manage, which is a THIRD key
// family: neither the provider key scope nor the agent model-key scope
// authorises it, and the 403 body ("insufficient permissions") names none of
// them.
func TestIssueTracingTokenRequestsTheTokenManageScope(t *testing.T) {
	var scopes string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/token") {
			_ = r.ParseForm()
			scopes = r.PostFormValue("scope")
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "t.o.k", "expires_at": 1})
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	if _, err := c.IssueTracingToken(context.Background(), TracingTokenRef{
		Org: "default", Project: "shop", Agent: "checkout-agent", Environment: "default",
	}); err != nil {
		t.Fatalf("IssueTracingToken: %v", err)
	}
	if !strings.Contains(scopes, "amp:agent:token-manage") {
		t.Errorf("requested scopes = %q, want the token-manage scope", scopes)
	}
}
