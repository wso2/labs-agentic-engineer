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

package agentgovernance

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/agentmanager"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

// TestLiveGovernance drives the real Agent Manager on a local cluster.
//
// Skipped unless AEP_LIVE_AMP=1, because it writes: it registers an agent,
// binds it to the org's provider and issues a key. It exists because every
// request shape in the agentmanager client was read out of Agent Manager's
// console rather than a published spec, and unit tests can only prove we send
// what we MEANT to send. This proves Agent Manager accepts it.
//
// Run against a local stack with:
//
//	AEP_LIVE_AMP=1 AMP_BASE_URL=http://api.amp.localhost:8080/api/v1 \
//	  AMP_TOKEN_URL=http://thunder.openchoreo.localhost:8080/oauth2/token \
//	  go test ./internal/delivery/agentgovernance/ -run TestLiveGovernance -v
func TestLiveGovernance(t *testing.T) {
	if os.Getenv("AEP_LIVE_AMP") != "1" {
		t.Skip("set AEP_LIVE_AMP=1 to run against a live Agent Manager")
	}
	base := envOr("AMP_BASE_URL", "http://api.amp.localhost:8080/api/v1")
	tokenURL := envOr("AMP_TOKEN_URL", "http://thunder.openchoreo.localhost:8080/oauth2/token")
	orgKey := os.Getenv("AEP_LIVE_ORG_ANTHROPIC_KEY")
	if orgKey == "" {
		t.Skip("set AEP_LIVE_ORG_ANTHROPIC_KEY: the provider holds the org's Anthropic key")
	}
	agentName := envOr("AEP_LIVE_AGENT", "aep-live-check-agent")

	keys := &liveKeyStore{}
	g := New(Deps{
		AMP: liveFactory{cfg: agentmanager.Config{
			TokenURL:     tokenURL,
			ClientID:     envOr("AMP_CLIENT_ID", "amp-publisher-aep"),
			ClientSecret: envOr("AMP_CLIENT_SECRET", "amp-publisher-aep-secret"),
			Resource:     "urn:wso2:amp",
		}},
		Keys: keys,
		// In memory, like the key store: this proves Agent Manager's half.
		Endpoints:   &fakeEndpoints{},
		Bindings:    liveBinding{base: base},
		Connections: liveConnection{key: orgKey},
	})

	out, err := g.GovernAgent(context.Background(), delivery.GovernAgentInput{
		OrgID:       envOr("AEP_LIVE_ORG", "default"),
		ProjectID:   envOr("AEP_LIVE_PROJECT", "default"),
		Component:   agentName,
		Environment: envOr("AEP_LIVE_ENV", "default"),
	})
	if err != nil {
		t.Fatalf("GovernAgent against live Agent Manager: %v", err)
	}
	if out.Skipped {
		t.Fatalf("governance skipped: %s", out.Reason)
	}
	if keys.stored == "" {
		t.Fatal("no model key was stored; the agent would deploy with no credential")
	}
	t.Logf("agent %q registered; key issued (%d chars); endpoint %s", agentName, len(keys.stored), keys.url)

	// AEP_LIVE_KEY_OUT lets an operator verify the last hop by hand — the key
	// authenticates against that proxy and nothing else, so a call through it is
	// the only proof that the governed path actually carries traffic.
	if out := os.Getenv("AEP_LIVE_KEY_OUT"); out != "" {
		if err := os.WriteFile(out, []byte(keys.stored+"\n"+keys.url+"\n"), 0o600); err != nil {
			t.Fatalf("write key material: %v", err)
		}
		t.Logf("key + proxy URL written to %s", out)
	}
}

func envOr(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

type liveFactory struct{ cfg agentmanager.Config }

func (f liveFactory) For(baseURL string) agentmanager.Client {
	c := f.cfg
	c.BaseURL = baseURL
	return agentmanager.New(c)
}

type liveBinding struct{ base string }

func (b liveBinding) GetAIGatewayBinding(context.Context, string, string) (openchoreo.AIGatewayBinding, error) {
	return openchoreo.AIGatewayBinding{
		Endpoint:  envOr("AEP_LIVE_GATEWAY_ENDPOINT", "http://ai-gateway.amp.localhost:8084"),
		AdminURL:  b.base,
		GatewayID: envOr("AEP_LIVE_GATEWAY_ID", ""),
	}, nil
}

// liveConnection is an org on Anthropic's own API.
type liveConnection struct{ key string }

func (c liveConnection) Effective(context.Context, string) (modelconn.Connection, string, bool, error) {
	return modelconn.Connection{
		Format: modelconn.FormatAnthropic, BaseURL: modelconn.AnthropicBaseURL,
		Host: modelconn.AnthropicHost, Model: modelconn.DefaultAnthropicModel, AuthScheme: modelconn.AuthXAPIKey,
	}, c.key, true, nil
}

// liveKeyStore keeps the issued key in memory: this test proves Agent Manager's
// half, not SM-API's, and writing a real secret would need a cluster identity
// the test does not have.
type liveKeyStore struct{ stored, url, tracingToken string }

func (s *liveKeyStore) StoredAMPModelKey(context.Context, string, string, string) (string, error) {
	return "", nil
}

func (s *liveKeyStore) WriteAMPModelKey(_ context.Context, _, _, _, apiKey, proxyURL string) (string, string, error) {
	s.stored, s.url = apiKey, proxyURL
	return "amp-model-live", "api-key", nil
}

func (s *liveKeyStore) WriteAMPTracingToken(_ context.Context, _, _, _, token string) error {
	s.tracingToken = token
	return nil
}

// TestLiveOpenAICompatibleProxy proves an OpenAI-compatible connection is
// governable end to end: the provider ProviderInputFor builds is accepted, an
// agent bound to it gets a key, and chat, tool calls and a usage-reporting
// stream pass through that agent's own proxy to the upstream.
//
// A THROWAWAY PROVIDER, not the org's. ProviderID(org) is the org's real
// provider; pointing it at Ollama would move every governed agent in the org.
// So the provider's handle is overridden, and everything else is exactly what
// the governor sends. The publisher client has no delete scope: the provider
// (`aep-fu-openai`, its upstream key overwritten on cleanup), agent
// (`aep-fu-oai-agent`) and binding it leaves behind are removed from Agent
// Manager's console. A rerun reuses them.
//
// The gateway at 8084 is not reachable from the host on the local plane; port
// forward it first:
//
//	kubectl -n default-default port-forward deploy/ai-gateway-default-default-gw-gateway-runtime 18084:8084
//	AEP_LIVE_AMP=1 AEP_LIVE_OLLAMA_KEY=… AEP_LIVE_GATEWAY_ID=<ai gateway uuid> \
//	  AEP_LIVE_GATEWAY_ENDPOINT=http://localhost:18084 \
//	  go test ./internal/delivery/agentgovernance/ -run TestLiveOpenAICompatibleProxy -v
func TestLiveOpenAICompatibleProxy(t *testing.T) {
	if os.Getenv("AEP_LIVE_AMP") != "1" {
		t.Skip("set AEP_LIVE_AMP=1 to run against a live Agent Manager")
	}
	upstreamKey := os.Getenv("AEP_LIVE_OLLAMA_KEY")
	if upstreamKey == "" {
		t.Skip("set AEP_LIVE_OLLAMA_KEY: the provider holds the Ollama key")
	}
	gatewayID := os.Getenv("AEP_LIVE_GATEWAY_ID")
	if gatewayID == "" {
		t.Fatal("AEP_LIVE_GATEWAY_ID is required: setting the provider's gateway is what deploys it")
	}
	ctx := context.Background()
	org, project, env := envOr("AEP_LIVE_ORG", "default"), envOr("AEP_LIVE_PROJECT", "default"), envOr("AEP_LIVE_ENV", "default")
	conn := modelconn.Connection{
		Format: modelconn.FormatOpenAICompatible, BaseURL: "https://ollama.com/v1",
		Host: modelconn.OllamaHost, Model: envOr("AEP_LIVE_MODEL", "gpt-oss:20b"), AuthScheme: modelconn.AuthBearer,
	}

	amp := agentmanager.New(agentmanager.Config{
		BaseURL:      envOr("AMP_BASE_URL", "http://api.amp.localhost:8080/api/v1"),
		TokenURL:     envOr("AMP_TOKEN_URL", "http://thunder.openchoreo.localhost:8080/oauth2/token"),
		ClientID:     envOr("AMP_CLIENT_ID", "amp-publisher-aep"),
		ClientSecret: envOr("AMP_CLIENT_SECRET", "amp-publisher-aep-secret"),
		Resource:     "urn:wso2:amp",
	})
	providerIn, err := ProviderInputFor(org, conn, upstreamKey, gatewayID)
	if err != nil {
		t.Fatalf("ProviderInputFor: %v", err)
	}
	throwaway := func(in agentmanager.EnsureProviderInput) agentmanager.EnsureProviderInput {
		in.ID, in.Context = "aep-fu-openai", "/aep-fu-openai"
		in.Name = "AEP follow-up live check (OpenAI-compatible)"
		return in
	}
	providerIn = throwaway(providerIn)
	providerIn.ReassertCredential = true // a rerun writes the current key onto the provider it left
	provider, err := amp.EnsureProvider(ctx, providerIn)
	if err != nil {
		t.Fatalf("EnsureProvider: %v", err)
	}
	// The provider outlives the test (no delete scope), so the real upstream
	// key does not: it is overwritten with a value that authenticates nowhere.
	t.Cleanup(func() {
		scrubbed, err := ProviderInputFor(org, conn, "cleared-by-aep:live-test", gatewayID)
		if err == nil {
			_, err = amp.UpdateProviderCredential(context.Background(), throwaway(scrubbed))
		}
		if err != nil {
			t.Errorf("scrub the upstream key from %s: %v — overwrite it in Agent Manager's console", providerIn.ID, err)
		}
	})

	const agent = "aep-fu-oai-agent"
	if _, err := amp.EnsureAgent(ctx, agentmanager.EnsureAgentInput{
		Org: org, Project: project, Name: agent, DisplayName: agent, Description: "AEP follow-up live check",
	}); err != nil {
		t.Fatalf("EnsureAgent: %v", err)
	}
	cfg, err := amp.EnsureModelConfig(ctx, agentmanager.EnsureModelConfigInput{
		Org: org, Project: project, Agent: agent, Name: "aep-fu-oai", Environment: env,
		ProviderHandle: provider.Handle, URLVar: ModelEndpointEnvVar, APIKeyVar: ModelAPIKeyEnvVar,
	})
	if err != nil {
		t.Fatalf("EnsureModelConfig: %v", err)
	}
	ref := agentmanager.ModelKeyRef{Org: org, Project: project, Agent: agent, ConfigID: cfg.ConfigID, Environment: env}
	const keyName = "aep-fu-oai-default"
	names, err := amp.ListModelKeys(ctx, ref)
	if err != nil {
		t.Fatalf("ListModelKeys: %v", err)
	}
	issue := amp.IssueModelKey
	for _, n := range names {
		if n == keyName {
			issue = amp.RotateModelKey
		}
	}
	issued, err := issue(ctx, ref, keyName)
	if err != nil {
		t.Fatalf("issue agent key: %v", err)
	}
	endpoint, err := proxyEndpoint(cfg.ProxyURL, envOr("AEP_LIVE_GATEWAY_ENDPOINT", "http://ai-gateway.amp.localhost:8084"), conn.BaseURL)
	if err != nil {
		t.Fatalf("proxyEndpoint: %v", err)
	}
	t.Logf("provider %s, agent %s, endpoint %s", provider.Handle, agent, endpoint)

	agentKey := map[string]string{"API-Key": issued.APIKey}

	// A new agent key answers 401 until the gateway broadcast lands (~20 s),
	// and a binding created moments ago answers 404 until its proxy deploys.
	var toolResp []byte
	deadline := time.Now().Add(60 * time.Second)
	for {
		status, body := liveChat(t, endpoint, agentKey, toolRequest(conn.Model, false))
		if status == http.StatusOK {
			toolResp = body
			break
		}
		settling := status == http.StatusUnauthorized || status == http.StatusNotFound
		if !settling || time.Now().After(deadline) {
			t.Fatalf("chat/completions through the agent proxy: %d %s", status, truncateLive(body))
		}
		time.Sleep(3 * time.Second)
	}
	var completion struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					Function struct{ Name string } `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(toolResp, &completion); err != nil {
		t.Fatalf("decode completion: %v: %s", err, truncateLive(toolResp))
	}
	if len(completion.Choices) == 0 || len(completion.Choices[0].Message.ToolCalls) == 0 ||
		completion.Choices[0].Message.ToolCalls[0].Function.Name != "get_weather" {
		t.Fatalf("no get_weather tool call came back: %s", truncateLive(toolResp))
	}

	// A stream reports usage only when asked, in a final chunk of its own.
	status, stream := liveChat(t, endpoint, agentKey, toolRequest(conn.Model, true))
	if status != http.StatusOK {
		t.Fatalf("streamed chat/completions: %d %s", status, truncateLive(stream))
	}
	if !streamEndsWithUsage(t, stream) {
		t.Fatalf("stream with include_usage ended without a usage chunk: %s", truncateLive(stream))
	}

	// A stray Authorization beside API-Key — what an OpenAI SDK sends when it is
	// handed a placeholder apiKey. Reported either way: it decides whether the
	// generated agent must omit apiKey on the governed path.
	withStray := map[string]string{"API-Key": issued.APIKey, "Authorization": "Bearer unused"}
	status, body := liveChat(t, endpoint, withStray, toolRequest(conn.Model, false))
	t.Logf("API-Key plus a stray Authorization: Bearer unused -> %d", status)
	if status != http.StatusOK {
		t.Errorf("a stray Authorization beside API-Key answered %d: %s", status, truncateLive(body))
	}
}

// toolRequest is a chat that should answer with a get_weather call.
func toolRequest(model string, stream bool) map[string]any {
	req := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "What is the weather in Paris right now? Use the get_weather tool."},
		},
		"tools": []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name":        "get_weather",
				"description": "Current weather for a city.",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"city": map[string]string{"type": "string"}},
					"required":   []string{"city"},
				},
			},
		}},
	}
	if stream {
		req["stream"] = true
		req["stream_options"] = map[string]bool{"include_usage": true}
	}
	return req
}

func liveChat(t *testing.T, endpoint string, headers map[string]string, body map[string]any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, endpoint+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("POST %s/chat/completions: %v", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// streamEndsWithUsage reports whether the last data chunk before [DONE]
// carries usage.
func streamEndsWithUsage(t *testing.T, stream []byte) bool {
	t.Helper()
	var last string
	sc := bufio.NewScanner(bytes.NewReader(stream))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if data, ok := strings.CutPrefix(line, "data:"); ok && strings.TrimSpace(data) != "[DONE]" {
			last = strings.TrimSpace(data)
		}
	}
	var chunk struct {
		Usage *struct {
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(last), &chunk); err != nil {
		return false
	}
	return chunk.Usage != nil && chunk.Usage.PromptTokens > 0
}

func truncateLive(b []byte) string {
	if len(b) > 600 {
		return string(b[:600]) + "…"
	}
	return string(b)
}
