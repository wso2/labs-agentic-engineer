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

// COMPONENT tier — the model connection on GET/PATCH /config and POST
// /config/llm/test, over the same real handler chain, real services, real
// Postgres and fake model endpoint as config_agents_component_test.go.
//
// One row per refusal code and one per transition the card can make: first
// connect on the format's defaults, a host change, a format change on the same
// host, Claude Code on an OpenAI-compatible connection, the subscription on
// leaving Anthropic's API, disconnect, and the minimal `{kind, apiKey}` body.
package organization_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

const (
	testLLMPath = "/api/v1/config/llm/test"
	ollamaKey   = "ollama-component-key-0123456789"
	ollamaKey2  = "ollama-component-key-second-9876"
)

// onOllama is the card's patch that moves the org to Ollama's
// OpenAI-compatible endpoint, runtime and all.
func onOllama(key string) string {
	return `{"llm":{"kind":"openai-compatible","baseURL":"https://ollama.com/v1","apiKey":"` + key + `","model":"gpt-oss:20b"},
		"agents":{"runtime":"opencode"}}`
}

func llmOf(t *testing.T, body []byte) map[string]any {
	t.Helper()
	llm, ok := decodeCfg(t, body)["llm"].(map[string]any)
	if !ok {
		t.Fatalf("llm is not an object: %s", body)
	}
	return llm
}

// keyPreview reads the stored connection's key preview.
func (c *configHarness) keyPreview(t *testing.T, org string) string {
	t.Helper()
	var preview string
	if err := c.db.Raw(`SELECT key_preview FROM org_model_connections WHERE oc_org_id = ?`, org).Scan(&preview).Error; err != nil {
		t.Fatalf("read preview: %v", err)
	}
	return preview
}

// --- transitions -------------------------------------------------------------------

// A `{kind: anthropic, apiKey}` body (seed-dev.sh sends it) connects: the
// server fills the format's defaults, and GET says what they are.
func TestConfigLLM_TheOldBodyConnectsWithTheFormatDefaults(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)

	resp := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey))
	if resp.Code != 200 {
		t.Fatalf("connect: %d %s", resp.Code, resp.Body.String())
	}
	llm := llmOf(t, resp.Body.Bytes())
	caps, _ := llm["capabilities"].(map[string]any)
	if llm["kind"] != "anthropic" || llm["baseURL"] != "https://api.anthropic.com/v1" || llm["model"] != "claude-sonnet-5" ||
		llm["priced"] != true || llm["updatedBy"] != "componenttest-user" {
		t.Fatalf("llm = %v, want Anthropic's API on its default model, priced, saved by the caller", llm)
	}
	if caps["claudeSubscription"] != true || caps["webSearch"] != "anthropic-server-tool" || caps["imageInput"] != "yes" ||
		caps["nativePdf"] != true || caps["generatedAgents"] != true {
		t.Fatalf("first-party capabilities drifted: %v", caps)
	}
	formats := fmt.Sprint(decodeCfg(t, resp.Body.Bytes())["llmFormats"])
	for _, want := range []string{"defaultBaseURL:https://api.anthropic.com/v1", "defaultModel:claude-sonnet-5",
		"defaultBaseURL:<nil>", "defaultModel:glm-5.3", "runtimes:[claude-code opencode]", "runtimes:[opencode]"} {
		if !strings.Contains(formats, want) {
			t.Fatalf("llmFormats = %s, missing %s", formats, want)
		}
	}
}

// A host change needs a key, and with one the connection moves: Bearer, the
// host's own limits and capabilities, and the stored key never sent there.
func TestConfigLLM_AHostChange(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	if r := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey)); r.Code != 200 {
		t.Fatalf("connect: %d %s", r.Code, r.Body.String())
	}

	r := c.h.AsOrg("acme").Patch(configPath, `{"llm":{"kind":"openai-compatible","baseURL":"https://ollama.com/v1"},"agents":{"runtime":"opencode"}}`)
	refused(t, r.Code, r.Body.String(), "llm", "llm_key_required_for_new_host")

	resp := c.h.AsOrg("acme").Patch(configPath, onOllama(ollamaKey))
	if resp.Code != 200 {
		t.Fatalf("move: %d %s", resp.Code, resp.Body.String())
	}
	llm := llmOf(t, resp.Body.Bytes())
	caps := llm["capabilities"].(map[string]any)
	if llm["kind"] != "openai-compatible" || llm["priced"] != false || caps["webSearch"] != "ollama-api" ||
		caps["claudeSubscription"] != false || caps["generatedAgents"] != true || caps["imageInput"] != "no" {
		t.Fatalf("llm on Ollama = %v", llm)
	}
	for _, req := range c.model.seen() {
		if req.Host == "ollama.com" && (strings.Contains(req.APIKey, goodAnthKey) || strings.Contains(req.Authorization, goodAnthKey)) {
			t.Fatalf("the stored Anthropic key was sent to ollama.com: %+v", req)
		}
	}
}

// Ollama serves both formats on one host with one key: switching the format
// there keeps the key, and the probe used the stored one.
func TestConfigLLM_AFormatChangeOnTheSameHostKeepsTheKey(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	if r := c.h.AsOrg("acme").Patch(configPath, onOllama(ollamaKey)); r.Code != 200 {
		t.Fatalf("connect: %d %s", r.Code, r.Body.String())
	}
	preview := c.keyPreview(t, "acme")
	before := len(c.model.seen())

	resp := c.h.AsOrg("acme").Patch(configPath, `{"llm":{"kind":"anthropic"}}`)
	if resp.Code != 200 {
		t.Fatalf("format change: %d %s", resp.Code, resp.Body.String())
	}
	if llm := llmOf(t, resp.Body.Bytes()); llm["kind"] != "anthropic" || llm["baseURL"] != "https://ollama.com/v1" {
		t.Fatalf("llm = %v, want the Anthropic format on the same URL", llm)
	}
	if got := c.keyPreview(t, "acme"); got != preview {
		t.Fatalf("the key changed: %q → %q", preview, got)
	}
	probed := false
	for _, req := range c.model.seen()[before:] {
		probed = probed || req.APIKey == ollamaKey || req.Authorization == "Bearer "+ollamaKey
	}
	if !probed {
		t.Fatal("the format change was not probed with the stored key")
	}
}

// Claude Code speaks only the Anthropic format.
func TestConfigLLM_ClaudeCodeWithOpenAICompatibleIsRefused(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	r := c.h.AsOrg("acme").Patch(configPath, `{"llm":{"kind":"openai-compatible","baseURL":"https://ollama.com/v1","apiKey":"`+ollamaKey+`"}}`)
	refused(t, r.Code, r.Body.String(), "agents", "agents_runtime_requires_anthropic_format")

	if r := c.h.AsOrg("acme").Patch(configPath, onOllama(ollamaKey)); r.Code != 200 {
		t.Fatalf("with OpenCode: %d %s", r.Code, r.Body.String())
	}
	r = c.h.AsOrg("acme").Patch(configPath, `{"agents":{"runtime":"claude-code"}}`)
	refused(t, r.Code, r.Body.String(), "agents", "agents_runtime_requires_anthropic_format")
}

// Leaving Anthropic's API deletes the Claude subscription in the same save.
func TestConfigLLM_LeavingAnthropicDeletesTheSubscription(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	if r := c.h.AsOrg("acme").Patch(configPath, `{"llm":{"kind":"anthropic","apiKey":"`+goodAnthKey+`"},"agents":{"subscription":{"kind":"claude","token":"`+goodToken+`"}}}`); r.Code != 200 {
		t.Fatalf("setup: %d %s", r.Code, r.Body.String())
	}
	resp := c.h.AsOrg("acme").Patch(configPath, onOllama(ollamaKey))
	if resp.Code != 200 || agentsOf(t, resp.Body.Bytes())["subscription"] != nil {
		t.Fatalf("the subscription survived leaving Anthropic's API: %d %s", resp.Code, resp.Body.String())
	}
	if n := c.count(t, `SELECT count(*) FROM org_secrets WHERE oc_org_id = 'acme' AND key = 'anthropic/coding-key'`); n != 0 {
		t.Fatal("the token's bytes survived")
	}
	// And a new one cannot be added while the connection lacks claudeSubscription.
	if r := c.h.AsOrg("acme").Patch(configPath, `{"llm":{"kind":"anthropic"},"agents":{"runtime":"claude-code"}}`); r.Code != 200 {
		t.Fatalf("Anthropic format on Ollama with Claude Code: %d %s", r.Code, r.Body.String())
	}
	r := c.h.AsOrg("acme").Patch(configPath, subscribe(goodToken))
	refused(t, r.Code, r.Body.String(), "agents", "agents_subscription_requires_anthropic_host")
}

// `llm: null` disconnects: the connection, its bytes and the token go.
func TestConfigLLM_NullDisconnects(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	if r := c.h.AsOrg("acme").Patch(configPath, `{"llm":{"kind":"anthropic","apiKey":"`+goodAnthKey+`"},"agents":{"subscription":{"kind":"claude","token":"`+goodToken+`"}}}`); r.Code != 200 {
		t.Fatalf("setup: %d %s", r.Code, r.Body.String())
	}
	resp := c.h.AsOrg("acme").Patch(configPath, `{"llm":null}`)
	if resp.Code != 200 || decodeCfg(t, resp.Body.Bytes())["llm"] != nil {
		t.Fatalf("disconnect: %d %s", resp.Code, resp.Body.String())
	}
	if creds, _, secrets := c.cardRows(t, "acme"); creds+secrets != 0 {
		t.Fatalf("disconnect left rows=%d secrets=%d", creds, secrets)
	}
}

// A disconnect keeps the runtime, so an org that left OpenCode connected
// cannot take a subscription back without choosing Claude Code in the same
// save: deployments/scripts/seed-dev.sh sends the runtime with the token for
// exactly this.
func TestConfigLLM_AfterADisconnectFromOpenCodeASubscriptionNeedsTheRuntime(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	if r := c.h.AsOrg("acme").Patch(configPath, onOllama(ollamaKey)); r.Code != 200 {
		t.Fatalf("setup: %d %s", r.Code, r.Body.String())
	}
	if r := c.h.AsOrg("acme").Patch(configPath, `{"llm":null}`); r.Code != 200 {
		t.Fatalf("disconnect: %d %s", r.Code, r.Body.String())
	}
	connect := `{"llm":{"kind":"anthropic","apiKey":"` + goodAnthKey + `"},"agents":{%s"subscription":{"kind":"claude","token":"` + goodToken + `"}}}`

	r := c.h.AsOrg("acme").Patch(configPath, fmt.Sprintf(connect, ""))
	refused(t, r.Code, r.Body.String(), "agents", "agents_subscription_requires_claude_code")

	r = c.h.AsOrg("acme").Patch(configPath, fmt.Sprintf(connect, `"runtime":"claude-code",`))
	if r.Code != 200 {
		t.Fatalf("seed-dev's body: %d %s", r.Code, r.Body.String())
	}
	if agents := agentsOf(t, r.Body.Bytes()); agents["runtime"] != "claude-code" || agents["subscription"] == nil {
		t.Fatalf("agents after the save: %+v", agents)
	}
}

// An unlisted model is a warning, not a refusal: the save goes
// through with the stored key, and llmCheck says so.
func TestConfigLLM_AnUnlistedModelIsAWarning(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	if r := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey)); r.Code != 200 {
		t.Fatalf("connect: %d %s", r.Code, r.Body.String())
	}
	resp := c.h.AsOrg("acme").Patch(configPath, `{"llm":{"model":"claude-not-listed"}}`)
	if resp.Code != 200 {
		t.Fatalf("model change: %d %s", resp.Code, resp.Body.String())
	}
	check, _ := decodeCfg(t, resp.Body.Bytes())["llmCheck"].(map[string]any)
	if check["modelListed"] != "no" || llmOf(t, resp.Body.Bytes())["model"] != "claude-not-listed" {
		t.Fatalf("llmCheck = %v, want the model saved with modelListed=no", check)
	}
}

// A rate-limited endpoint proves the key: an org whose plan is spent can
// still save, with a warning.
func TestConfigLLM_AProviderLimitIsAWarning(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	c.model.setStatus(http.StatusTooManyRequests)
	resp := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey))
	if resp.Code != 200 {
		t.Fatalf("connect: %d %s", resp.Code, resp.Body.String())
	}
	if check := decodeCfg(t, resp.Body.Bytes())["llmCheck"].(map[string]any); check["warning"] != "provider_limit" {
		t.Fatalf("llmCheck = %v, want warning provider_limit", check)
	}
}

// --- refusals -------------------------------------------------------------------

func TestConfigLLM_Refusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		status   int // what the fake endpoint answers; 0 = 200
		body     string
		wantHTTP int
		section  string
		code     string
	}{
		{name: "no URL on a format without a default", body: `{"llm":{"kind":"openai-compatible","apiKey":"` + ollamaKey + `"},"agents":{"runtime":"opencode"}}`, section: "llm", code: "llm_field_required"},
		{name: "plain http", body: `{"llm":{"kind":"openai-compatible","baseURL":"http://ollama.com/v1","apiKey":"` + ollamaKey + `"},"agents":{"runtime":"opencode"}}`, section: "llm", code: "llm_base_url_invalid"},
		{name: "userinfo in the URL", body: `{"llm":{"kind":"openai-compatible","baseURL":"https://u:p@ollama.com/v1","apiKey":"` + ollamaKey + `"},"agents":{"runtime":"opencode"}}`, section: "llm", code: "llm_base_url_invalid"},
		{name: "a short key", body: `{"llm":{"kind":"openai-compatible","baseURL":"https://ollama.com/v1","apiKey":"short"},"agents":{"runtime":"opencode"}}`, section: "llm", code: "llm_key_too_short"},
		{name: "a key the endpoint rejects", status: http.StatusUnauthorized, body: llmConnect(goodAnthKey), section: "llm", code: "llm_key_rejected"},
		{name: "a 4xx that is not a rate limit", status: http.StatusTeapot, body: llmConnect(goodAnthKey), section: "llm", code: "llm_unexpected_status"},
		{name: "a redirect", status: http.StatusFound, body: llmConnect(goodAnthKey), section: "llm", code: "llm_unreachable"},
		{name: "the endpoint's own 5xx", status: http.StatusServiceUnavailable, body: llmConnect(goodAnthKey), wantHTTP: 502, section: "llm", code: "llm_upstream_error"},
		{name: "a subscription token as the key", body: llmConnect(goodToken), section: "llm", code: "anthropic_oauth_token_coding_only"},
		{name: "a non-Anthropic key on Anthropic's API", body: llmConnect("not-an-anthropic-key-at-all"), section: "llm", code: "anthropic_key_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := newConfigHarness(t)
			if tc.status != 0 {
				c.model.setStatus(tc.status)
			}
			r := c.h.AsOrg("acme").Patch(configPath, tc.body)
			want := tc.wantHTTP
			if want == 0 {
				want = 400
			}
			if r.Code != want || !strings.Contains(r.Body.String(), `"body.`+tc.section+`"`) || !strings.Contains(r.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("want %d on body.%s (%s), got %d %s", want, tc.section, tc.code, r.Code, r.Body.String())
			}
			if creds, settings, secrets := c.cardRows(t, "acme"); creds+settings+secrets != 0 {
				t.Fatalf("a refused save wrote rows=%d settings=%d secrets=%d", creds, settings, secrets)
			}
		})
	}
}

// The production probe client refuses a host that resolves to a private or
// loopback address before any byte leaves.
func TestConfigLLM_APrivateHostIsRefused(t *testing.T) {
	t.Parallel()
	c := newConfigHarnessGuarded(t)
	r := c.h.AsOrg("acme").Patch(configPath, `{"llm":{"kind":"openai-compatible","baseURL":"https://127.0.0.1/v1","apiKey":"`+ollamaKey+`"},"agents":{"runtime":"opencode"}}`)
	refused(t, r.Code, r.Body.String(), "llm", "llm_host_refused")
	if strings.Contains(r.Body.String(), ollamaKey) {
		t.Fatalf("the refusal echoes the key: %s", r.Body.String())
	}
}

// An installation without the OpenCode image runs no OpenAI-compatible
// connection.
func TestConfigLLM_AFormatWithNoRuntimeIsRefused(t *testing.T) {
	t.Parallel()
	c := newConfigHarnessRuntimes(t, []orgconfig.AgentRuntime{orgconfig.AgentRuntimeClaudeCode})
	r := c.h.AsOrg("acme").Patch(configPath, `{"llm":{"kind":"openai-compatible","baseURL":"https://ollama.com/v1","apiKey":"`+ollamaKey+`"}}`)
	refused(t, r.Code, r.Body.String(), "llm", "llm_format_has_no_runtime")
	if formats := fmt.Sprint(decodeCfg(t, c.h.AsOrg("acme").Get(configPath).Body.Bytes())["llmFormats"]); !strings.Contains(formats, "runtimes:[]") {
		t.Fatalf("llmFormats must say no runtime here runs the format: %s", formats)
	}
}

// --- Test connection ---------------------------------------------------------------

func TestConfigLLM_TestConnectionWritesNothing(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)

	r := c.h.AsOrg("acme").Post(testLLMPath, `{"kind":"openai-compatible","baseURL":"https://ollama.com/v1","apiKey":"`+ollamaKey+`","model":"gpt-oss:20b"}`)
	if r.Code != 200 {
		t.Fatalf("test: %d %s", r.Code, r.Body.String())
	}
	check := decodeCfg(t, r.Body.Bytes())
	if check["modelListed"] != "yes" || check["baseURL"] != "https://ollama.com/v1" || check["priced"] != false ||
		check["capabilities"].(map[string]any)["webSearch"] != "ollama-api" {
		t.Fatalf("check = %v", check)
	}
	if strings.Contains(r.Body.String(), ollamaKey) {
		t.Fatalf("the key was echoed: %s", r.Body.String())
	}
	if creds, settings, secrets := c.cardRows(t, "acme"); creds+settings+secrets != 0 {
		t.Fatalf("Test connection wrote rows=%d settings=%d secrets=%d", creds, settings, secrets)
	}
	// A path-less Anthropic URL is normalised to /v1.
	r = c.h.AsOrg("acme").Post(testLLMPath, `{"kind":"anthropic","baseURL":"https://ollama.com","apiKey":"`+ollamaKey+`"}`)
	if r.Code != 200 || decodeCfg(t, r.Body.Bytes())["baseURL"] != "https://ollama.com/v1" {
		t.Fatalf("normalised URL: %d %s", r.Code, r.Body.String())
	}
}

// apiKey may be omitted only on the saved connection's host: the stored key is
// used, server side, and a new host without a key is refused.
func TestConfigLLM_TestConnectionReusesTheStoredKeyOnlyOnItsHost(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	if r := c.h.AsOrg("acme").Patch(configPath, onOllama(ollamaKey)); r.Code != 200 {
		t.Fatalf("connect: %d %s", r.Code, r.Body.String())
	}
	before := len(c.model.seen())
	if r := c.h.AsOrg("acme").Post(testLLMPath, `{"model":"glm-5.3"}`); r.Code != 200 {
		t.Fatalf("test on the saved host: %d %s", r.Code, r.Body.String())
	}
	if req := c.model.seen()[before]; req.Authorization != "Bearer "+ollamaKey {
		t.Fatalf("the stored key was not used: %+v", req)
	}
	r := c.h.AsOrg("acme").Post(testLLMPath, `{"kind":"anthropic","baseURL":"https://api.anthropic.com/v1"}`)
	refused(t, r.Code, r.Body.String(), "llm", "llm_key_required_for_new_host")
	// A first connect cannot be tested without a key either.
	r = c.h.AsOrg("globex").Post(testLLMPath, `{"kind":"anthropic"}`)
	refused(t, r.Code, r.Body.String(), "llm", "llm_field_required")
}

// Ten test calls a minute per org; the eleventh answers 429, and every call is
// logged with the org and the host. Not parallel: it swaps the global logger.
func TestConfigLLM_TestConnectionIsRateLimited(t *testing.T) {
	c := newConfigHarness(t)
	var buf bytes.Buffer
	var mu sync.Mutex
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&lockedWriter{w: &buf, mu: &mu}, nil)))
	defer slog.SetDefault(prev)

	body := `{"kind":"openai-compatible","baseURL":"https://ollama.com/v1","apiKey":"` + ollamaKey2 + `"}`
	for i := 1; i <= 10; i++ {
		if r := c.h.AsOrg("acme").Post(testLLMPath, body); r.Code != 200 {
			t.Fatalf("call %d: %d %s", i, r.Code, r.Body.String())
		}
	}
	r := c.h.AsOrg("acme").Post(testLLMPath, body)
	if r.Code != http.StatusTooManyRequests || !strings.Contains(r.Body.String(), `"code":"llm_test_rate_limited"`) {
		t.Fatalf("the 11th call: %d %s, want 429 llm_test_rate_limited", r.Code, r.Body.String())
	}
	// Another org has its own allowance.
	if r := c.h.AsOrg("globex").Post(testLLMPath, body); r.Code != 200 {
		t.Fatalf("another org was limited: %d %s", r.Code, r.Body.String())
	}

	mu.Lock()
	logs := buf.String()
	mu.Unlock()
	if n := strings.Count(logs, `"msg":"model connection test","org":"acme","host":"ollama.com"`); n != 10 {
		t.Fatalf("want one log line naming org and host per admitted call (10), got %d:\n%s", n, logs)
	}
	if strings.Contains(logs, ollamaKey2) {
		t.Fatal("a test call logged the key")
	}
}

// --- who can read it -----------------------------------------------------------------

// The chat attach control reads llm.capabilities from GET /config, so every
// member who can chat must read it — not only the admin who saved it.
// aep-api has no role gate on /config (the tenant gate alone binds the org),
// so any member of the org reads the same projection.
func TestConfigLLM_EveryMemberReadsTheAttachCapabilities(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	if r := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey)); r.Code != 200 {
		t.Fatalf("connect: %d %s", r.Code, r.Body.String())
	}
	resp := c.h.AsOrg("acme").With(func(cl *auth.Claims) { cl.Subject = "a-developer" }).Get(configPath)
	if resp.Code != 200 {
		t.Fatalf("a member's GET /config: %d %s", resp.Code, resp.Body.String())
	}
	caps := llmOf(t, resp.Body.Bytes())["capabilities"].(map[string]any)
	if caps["imageInput"] != "yes" || caps["nativePdf"] != true {
		t.Fatalf("capabilities = %v", caps)
	}
}

// --- the Agent Manager push -----------------------------------------------------

// failingPublisher is an Agent Manager that answers every publish with err.
type failingPublisher struct{ err error }

func (p *failingPublisher) PublishOrgModelConnection(context.Context, string, modelconn.Connection, string) error {
	return p.err
}

func (p *failingPublisher) ClearOrgModelKey(context.Context, string, modelconn.Connection) error {
	return p.err
}

// A key save whose Agent Manager push fails answers 502
// agent_manager_not_updated, and the key stays saved: the vault write and its
// default-key row are the save, and saving the key again retries the push.
func TestPatchConfig_AgentManagerPushFailureIs502AndTheKeyStaysSaved(t *testing.T) {
	t.Parallel()
	c := newConfigHarnessWithModelProvider(t, &failingPublisher{err: errors.New("amp down")})

	r := c.h.AsOrg("acme").Patch(configPath, `{"llm":{"kind":"anthropic","apiKey":"sk-ant-testkey-0123456789","model":"claude-x"}}`)
	if r.Code != 502 {
		t.Fatalf("want 502, got %d body=%s", r.Code, r.Body.String())
	}
	body := r.Body.String()
	for _, want := range []string{`"code":"agent_manager_not_updated"`, `"body.llm"`,
		"Key saved; Agent Manager was not updated. Save the key again."} {
		if !strings.Contains(body, want) {
			t.Fatalf("the response must carry %s: %s", want, body)
		}
	}
	if strings.Contains(body, "amp down") || strings.Contains(body, "sk-ant-testkey") {
		t.Fatalf("the response leaks the cause or the key: %s", body)
	}
	ref, err := organization.NewOrgSecretRepository(c.db).Get(context.Background(), "acme", organization.OrgSecretDefaultKey)
	if err != nil || ref == nil {
		t.Fatalf("the vault write and its row stay: default-key row = %+v (%v)", ref, err)
	}
	if llm, ok := decodeCfg(t, c.h.AsOrg("acme").Get(configPath).Body.Bytes())["llm"].(map[string]any); !ok || llm["kind"] != "anthropic" {
		t.Fatalf("GET must read the saved connection: %v", llm)
	}
}
