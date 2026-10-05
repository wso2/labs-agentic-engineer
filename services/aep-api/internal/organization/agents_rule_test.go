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

package organization

// UNIT tier — judgeCard, the one decision behind every save of the AI agents
// card, and draftConnection, the `llm` merge it starts from, as pure
// functions: what a patch does to an org in a given state, and which patches
// it refuses. The DB-backed half (one transaction, nothing written on
// failure) is config_agents_component_test.go.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
	"github.com/wso2/aep/aep-api/internal/platform/patch"
)

const (
	ruleKey      = "sk-ant-api03-RULEtestKeyABCDEFGHIJKLmnop"
	ruleToken    = "sk-ant-oat01-RULEtestTokenABCDEFGHIJKLmn"
	ruleOllamaKy = "ollama-rule-test-key-0123456789"
)

func set[T any](v T) patch.Field[T] { return patch.Field[T]{Sent: true, Value: v} }
func null[T any]() patch.Field[T]   { return patch.Field[T]{Sent: true, Null: true} }

func agentsWrite(runtime orgconfig.AgentRuntime, sub patch.Field[orgconfig.SubscriptionWrite]) patch.Field[orgconfig.AgentsWrite] {
	return set(orgconfig.AgentsWrite{Runtime: runtime, Subscription: sub})
}

func token(t string) patch.Field[orgconfig.SubscriptionWrite] {
	return set(orgconfig.SubscriptionWrite{Kind: orgconfig.SubscriptionKindClaude, Token: t})
}

func llm(w orgconfig.LLMPatch) patch.Field[orgconfig.LLMPatch] { return set(w) }

func onRuntime(r orgconfig.AgentRuntime) *OrgAgentSettings {
	return &OrgAgentSettings{Runtime: r}
}

// anthropicConn is a stored connection on Anthropic's own API.
func anthropicConn() *OrgModelConnection {
	return &OrgModelConnection{
		OcOrgID: "acme", Format: modelconn.FormatAnthropic, BaseURL: modelconn.AnthropicBaseURL,
		Host: modelconn.AnthropicHost, Model: modelconn.DefaultAnthropicModel, AuthScheme: modelconn.AuthXAPIKey,
		ImageInput: modelconn.Yes, UpdatedAt: time.Unix(100, 0),
	}
}

// ollamaConn is a stored connection on Ollama's OpenAI-compatible endpoint.
func ollamaConn() *OrgModelConnection {
	return &OrgModelConnection{
		OcOrgID: "acme", Format: modelconn.FormatOpenAICompatible, BaseURL: "https://ollama.com/v1",
		Host: modelconn.OllamaHost, Model: "gpt-oss:20b", AuthScheme: modelconn.AuthBearer,
		ImageInput: modelconn.No, UpdatedAt: time.Unix(100, 0),
	}
}

// everyRuntime is an installation with a runner image for every runtime, the
// one every rule below is judged on unless it says otherwise.
var everyRuntime = orgconfig.AgentRuntimes

// claudeCodeOnly is an installation deployed without the OpenCode image.
var claudeCodeOnly = []orgconfig.AgentRuntime{orgconfig.AgentRuntimeClaudeCode}

func mustJudge(t *testing.T, s cardState, p orgconfig.ConfigPatch) cardEffects {
	t.Helper()
	return mustJudgeOn(t, everyRuntime, s, p)
}

func mustJudgeOn(t *testing.T, runtimes []orgconfig.AgentRuntime, s cardState, p orgconfig.ConfigPatch) cardEffects {
	t.Helper()
	eff, err := judgeCard(s, runtimes, p)
	if err != nil {
		t.Fatalf("judgeCard refused: %v", err)
	}
	return eff
}

// refusal asserts judgeCard refused p on body.<section> with code.
func refusal(t *testing.T, s cardState, p orgconfig.ConfigPatch, section, code string) *SectionError {
	t.Helper()
	return refusalOn(t, everyRuntime, s, p, section, code)
}

// refusalOn is refusal on an installation that runs runtimes.
func refusalOn(t *testing.T, runtimes []orgconfig.AgentRuntime, s cardState, p orgconfig.ConfigPatch, section, code string) *SectionError {
	t.Helper()
	_, err := judgeCard(s, runtimes, p)
	var se *SectionError
	if !errors.As(err, &se) {
		t.Fatalf("judgeCard = %v, want a SectionError", err)
	}
	if se.Section != section || se.Code != code {
		t.Fatalf("refused on body.%s (%s), want body.%s (%s): %s", se.Section, se.Code, section, code, se.Message)
	}
	return se
}

// --- clause 1: first connect ---------------------------------------------------

// A `{kind, apiKey}` body connects: the format's defaults fill the URL and the
// model.
func TestJudgeCard_FirstConnectFillsTheFormatDefaults(t *testing.T) {
	eff := mustJudge(t, cardState{}, orgconfig.ConfigPatch{LLM: llm(orgconfig.LLMPatch{Kind: "anthropic", APIKey: "  " + ruleKey + "\n"})})
	want := connectionDraft{Format: modelconn.FormatAnthropic, BaseURL: modelconn.AnthropicBaseURL,
		Host: modelconn.AnthropicHost, Model: modelconn.DefaultAnthropicModel, Key: ruleKey}
	if eff.writeConn == nil || *eff.writeConn != want {
		t.Fatalf("draft = %+v, want %+v", eff.writeConn, want)
	}
}

func TestJudgeCard_FirstConnectNeedsFormatKeyAndURL(t *testing.T) {
	for _, w := range []orgconfig.LLMPatch{
		{APIKey: ruleKey},                                 // no format
		{Kind: "anthropic"},                               // no key
		{Kind: "anthropic", APIKey: " \n\t"},              // blank key
		{Kind: "openai-compatible", APIKey: ruleOllamaKy}, // a format with no default URL
	} {
		refusal(t, cardState{settings: onRuntime("opencode")}, orgconfig.ConfigPatch{LLM: llm(w)}, "llm", "llm_field_required")
	}
}

func TestDraftConnection_BaseURLShape(t *testing.T) {
	for _, tc := range []struct {
		format       modelconn.Format
		raw, wantURL string
		wantHost     string
		wantRefusal  bool
	}{
		// A path-less Anthropic URL gains /v1: the SDKs append /messages.
		{format: modelconn.FormatAnthropic, raw: "https://ollama.com", wantURL: "https://ollama.com/v1", wantHost: "ollama.com"},
		{format: modelconn.FormatAnthropic, raw: "https://Ollama.com/v1/", wantURL: "https://ollama.com/v1", wantHost: "ollama.com"},
		{format: modelconn.FormatOpenAICompatible, raw: "https://gw.example.com:8443/api/v1", wantURL: "https://gw.example.com:8443/api/v1", wantHost: "gw.example.com"},
		{format: modelconn.FormatOpenAICompatible, raw: "https://ollama.com", wantURL: "https://ollama.com", wantHost: "ollama.com"},
		// https's default port is implied, never stored.
		{format: modelconn.FormatOpenAICompatible, raw: "https://ollama.com:443/v1", wantURL: "https://ollama.com/v1", wantHost: "ollama.com"},
		{format: modelconn.FormatOpenAICompatible, raw: "http://ollama.com/v1", wantRefusal: true},
		{format: modelconn.FormatOpenAICompatible, raw: "https://user:pw@ollama.com/v1", wantRefusal: true},
		{format: modelconn.FormatOpenAICompatible, raw: "https://ollama.com/v1?x=1", wantRefusal: true},
		{format: modelconn.FormatOpenAICompatible, raw: "https://ollama.com/v1#frag", wantRefusal: true},
		{format: modelconn.FormatOpenAICompatible, raw: "https:///v1", wantRefusal: true},
		{format: modelconn.FormatOpenAICompatible, raw: "ollama.com/v1", wantRefusal: true},
	} {
		d, _, err := draftConnection(nil, orgconfig.LLMPatch{Kind: tc.format, BaseURL: tc.raw, APIKey: ruleOllamaKy})
		if tc.wantRefusal {
			var ve *ValidationError
			if !errors.As(err, &ve) || ve.Code != "llm_base_url_invalid" {
				t.Errorf("%q: err = %v, want llm_base_url_invalid", tc.raw, err)
			}
			continue
		}
		if err != nil || d.BaseURL != tc.wantURL || d.Host != tc.wantHost {
			t.Errorf("%q: draft = %+v (%v), want %s on %s", tc.raw, d, err, tc.wantURL, tc.wantHost)
		}
	}
}

// --- clause 2: a connection edit needs the key -------------------------------------

// A connection edit (format or base URL) rewrites Agent Manager's provider
// whole, so it needs the key in the same save: no stored key is reused, on any
// host, port or format.
func TestJudgeCard_AConnectionEditNeedsTheKey(t *testing.T) {
	onOllama := cardState{conn: ollamaConn(), settings: onRuntime("opencode")}
	for name, w := range map[string]orgconfig.LLMPatch{
		"a host change":                {Kind: "openai-compatible", BaseURL: "https://proxy.example.com/v1"},
		"a port change":                {BaseURL: "https://ollama.com:8443/v1"},
		"a path change":                {BaseURL: "https://ollama.com/api/v1"},
		"a format change on that host": {Kind: "anthropic"},
	} {
		t.Run(name, func(t *testing.T) {
			refusal(t, onOllama, orgconfig.ConfigPatch{LLM: llm(w)}, "llm", "llm_key_required")
		})
	}
	// The https default port is no change.
	eff := mustJudge(t, onOllama, orgconfig.ConfigPatch{LLM: llm(orgconfig.LLMPatch{BaseURL: "https://ollama.com:443/v1"})})
	if eff.writeConn != nil {
		t.Fatalf("draft = %+v, want https://ollama.com:443/v1 to be the stored URL, unchanged", eff.writeConn)
	}
	// With the key, the edit is a draft carrying it.
	eff = mustJudge(t, onOllama, orgconfig.ConfigPatch{LLM: llm(orgconfig.LLMPatch{Kind: "anthropic", APIKey: ruleOllamaKy})})
	if eff.writeConn == nil || eff.writeConn.Key != ruleOllamaKy || eff.writeConn.Format != modelconn.FormatAnthropic {
		t.Fatalf("draft = %+v, want the Anthropic format with the request's key", eff.writeConn)
	}
}

// A model-only save needs no key: the draft is written without one (and
// without a probe, AgentSettingsService.probe).
func TestJudgeCard_AModelOnlySaveNeedsNoKey(t *testing.T) {
	eff := mustJudge(t, cardState{conn: anthropicConn()},
		orgconfig.ConfigPatch{LLM: llm(orgconfig.LLMPatch{Model: "claude-haiku-4-5"})})
	if eff.writeConn == nil || eff.writeConn.Model != "claude-haiku-4-5" || eff.writeConn.Key != "" {
		t.Fatalf("draft = %+v, want the new model with no key", eff.writeConn)
	}
}

// A patch that changes nothing neither probes nor writes.
func TestJudgeCard_AnUnchangedConnectionIsLeftAlone(t *testing.T) {
	eff := mustJudge(t, cardState{conn: anthropicConn()},
		orgconfig.ConfigPatch{LLM: llm(orgconfig.LLMPatch{Kind: "anthropic", Model: modelconn.DefaultAnthropicModel})})
	if eff.writeConn != nil || eff.deleteConn {
		t.Fatalf("effects = %+v, want nothing to do", eff)
	}
}

func TestJudgeCard_KeyShape(t *testing.T) {
	for _, tc := range []struct {
		w    orgconfig.LLMPatch
		code string
	}{
		{orgconfig.LLMPatch{Kind: "openai-compatible", BaseURL: "https://ollama.com/v1", APIKey: "short-key"}, "llm_key_too_short"},
		{orgconfig.LLMPatch{Kind: "anthropic", APIKey: "not-an-anthropic-key-at-all"}, "anthropic_key_invalid"},
		{orgconfig.LLMPatch{Kind: "anthropic", APIKey: ruleToken}, "anthropic_oauth_token_coding_only"},
	} {
		refusal(t, cardState{settings: onRuntime("opencode")}, orgconfig.ConfigPatch{LLM: llm(tc.w)}, "llm", tc.code)
	}
	// Off Anthropic's API a key has no fixed shape: only its length is checked.
	mustJudge(t, cardState{settings: onRuntime("opencode")}, orgconfig.ConfigPatch{LLM: llm(orgconfig.LLMPatch{
		Kind: "anthropic", BaseURL: "https://ollama.com", APIKey: ruleOllamaKy})})
}

// --- clauses 3 and 4: runtimes ---------------------------------------------------

func TestJudgeCard_AFormatNeedsARuntime(t *testing.T) {
	refusalOn(t, claudeCodeOnly, cardState{},
		orgconfig.ConfigPatch{LLM: llm(orgconfig.LLMPatch{Kind: "openai-compatible", BaseURL: "https://ollama.com/v1", APIKey: ruleOllamaKy})},
		"llm", "llm_format_has_no_runtime")
}

func TestJudgeCard_ClaudeCodeNeedsTheAnthropicFormat(t *testing.T) {
	ollama := orgconfig.LLMPatch{Kind: "openai-compatible", BaseURL: "https://ollama.com/v1", APIKey: ruleOllamaKy}
	// The default runtime is Claude Code, so a first OpenAI-compatible connect
	// alone is refused ...
	refusal(t, cardState{}, orgconfig.ConfigPatch{LLM: llm(ollama)}, "agents", "agents_runtime_requires_anthropic_format")
	// ... as is choosing Claude Code on an OpenAI-compatible connection ...
	refusal(t, cardState{conn: ollamaConn(), settings: onRuntime("opencode")},
		orgconfig.ConfigPatch{Agents: agentsWrite("claude-code", patch.Field[orgconfig.SubscriptionWrite]{})},
		"agents", "agents_runtime_requires_anthropic_format")
	// ... and the card's own patch, the format and OpenCode together, passes.
	eff := mustJudge(t, cardState{conn: anthropicConn()}, orgconfig.ConfigPatch{
		LLM: llm(ollama), Agents: agentsWrite("opencode", patch.Field[orgconfig.SubscriptionWrite]{}),
	})
	if eff.writeConn == nil || eff.settings == nil || eff.settings.Runtime != "opencode" {
		t.Fatalf("effects = %+v, want the connection and OpenCode written", eff)
	}
}

// A runtime this installation has no runner image for is refused when a save
// names it: accepted, it would fail every coding dispatch after it.
func TestJudgeCard_AnUnavailableRuntimeIsRefused(t *testing.T) {
	se := refusalOn(t, claudeCodeOnly, cardState{conn: anthropicConn()},
		orgconfig.ConfigPatch{Agents: agentsWrite("opencode", patch.Field[orgconfig.SubscriptionWrite]{})},
		"agents", "agents_runtime_unavailable")
	if !strings.Contains(se.Message, "claude-code") {
		t.Errorf("message %q does not say what IS available", se.Message)
	}
	// Refused before anything else it would do: the stored token stays.
	refusalOn(t, claudeCodeOnly, cardState{conn: anthropicConn(), hasToken: true},
		orgconfig.ConfigPatch{Agents: agentsWrite("opencode", patch.Field[orgconfig.SubscriptionWrite]{})},
		"agents", "agents_runtime_unavailable")
}

// An org already on a runtime the installation lost can still save the rest of
// the card: the runtime it did not name is kept, never substituted.
func TestJudgeCard_AnUnnamedUnavailableRuntimeIsKept(t *testing.T) {
	eff, err := judgeCard(cardState{conn: anthropicConn(), settings: onRuntime("opencode")}, claudeCodeOnly,
		orgconfig.ConfigPatch{LLM: llm(orgconfig.LLMPatch{Model: "claude-haiku-4-5"})})
	if err != nil {
		t.Fatalf("a model-only save was refused over a runtime it did not name: %v", err)
	}
	if eff.settings != nil || eff.writeConn == nil {
		t.Fatalf("effects = %+v, want the model changed and the setting row untouched", eff)
	}
	// And moving off it is always possible.
	eff = mustJudgeOn(t, claudeCodeOnly, cardState{settings: onRuntime("opencode")},
		orgconfig.ConfigPatch{Agents: agentsWrite("claude-code", patch.Field[orgconfig.SubscriptionWrite]{})})
	if eff.settings == nil || eff.settings.Runtime != "claude-code" {
		t.Fatalf("settings = %+v, want claude-code", eff.settings)
	}
}

func TestJudgeCard_UnknownRuntimeIsRefusedByName(t *testing.T) {
	refusal(t, cardState{}, orgconfig.ConfigPatch{Agents: agentsWrite("cursor", patch.Field[orgconfig.SubscriptionWrite]{})},
		"agents", "agents_runtime_unknown")
}

// --- clause 5: the Claude subscription ---------------------------------------------

func TestJudgeCard_SubscriptionNeedsClaudeCode(t *testing.T) {
	// OpenCode chosen in the same patch as a new token.
	refusal(t, cardState{conn: anthropicConn()}, orgconfig.ConfigPatch{Agents: agentsWrite("opencode", token(ruleToken))},
		"agents", "agents_subscription_requires_claude_code")
	// A new token onto an org already on OpenCode.
	refusal(t, cardState{conn: anthropicConn(), settings: onRuntime("opencode")},
		orgconfig.ConfigPatch{Agents: agentsWrite("", token(ruleToken))},
		"agents", "agents_subscription_requires_claude_code")
}

func TestJudgeCard_SubscriptionNeedsAConnection(t *testing.T) {
	refusal(t, cardState{}, orgconfig.ConfigPatch{Agents: agentsWrite("", token(ruleToken))},
		"agents", "agents_subscription_requires_connection")
	refusal(t, cardState{conn: anthropicConn()}, orgconfig.ConfigPatch{
		LLM: null[orgconfig.LLMPatch](), Agents: agentsWrite("", token(ruleToken)),
	}, "agents", "agents_subscription_requires_connection")
}

// A subscription authenticates only against Anthropic's own API: an
// Anthropic-format connection on Ollama cannot carry one.
func TestJudgeCard_SubscriptionNeedsAnthropicsHost(t *testing.T) {
	onOllama := anthropicConn()
	onOllama.BaseURL, onOllama.Host = "https://ollama.com/v1", modelconn.OllamaHost
	refusal(t, cardState{conn: onOllama}, orgconfig.ConfigPatch{Agents: agentsWrite("", token(ruleToken))},
		"agents", "agents_subscription_requires_anthropic_host")
}

// Leaving Anthropic's API deletes the stored token in the same save.
func TestJudgeCard_LeavingAnthropicDeletesTheSubscription(t *testing.T) {
	eff := mustJudge(t, cardState{conn: anthropicConn(), hasToken: true}, orgconfig.ConfigPatch{
		LLM:    llm(orgconfig.LLMPatch{Kind: "openai-compatible", BaseURL: "https://ollama.com/v1", APIKey: ruleOllamaKy}),
		Agents: agentsWrite("opencode", patch.Field[orgconfig.SubscriptionWrite]{}),
	})
	if !eff.deleteToken {
		t.Fatal("moving off Anthropic's API kept a subscription only it accepts")
	}
	// The same host with the Anthropic format and Claude Code still loses it.
	eff = mustJudge(t, cardState{conn: anthropicConn(), hasToken: true}, orgconfig.ConfigPatch{
		LLM: llm(orgconfig.LLMPatch{BaseURL: "https://ollama.com", APIKey: ruleOllamaKy}),
	})
	if !eff.deleteToken {
		t.Fatal("an Anthropic-format move to Ollama kept the subscription")
	}
}

// The connection and the token can arrive in ONE save: the rule is judged on
// the state the patch leaves, not on the one it starts from.
func TestJudgeCard_ConnectionAndTokenInOneSave(t *testing.T) {
	eff := mustJudge(t, cardState{}, orgconfig.ConfigPatch{
		LLM:    llm(orgconfig.LLMPatch{Kind: "anthropic", APIKey: ruleKey}),
		Agents: agentsWrite("", token(ruleToken)),
	})
	if eff.writeConn == nil || eff.writeConn.Key != ruleKey || eff.writeToken != ruleToken {
		t.Fatalf("effects = %+v, want the key and the token written", eff)
	}
	if eff.settings != nil {
		t.Errorf("a subscription-only agents write touched the setting row: %+v", eff.settings)
	}
}

func TestJudgeCard_OpenCodeDeletesTheToken(t *testing.T) {
	eff := mustJudge(t, cardState{conn: anthropicConn(), hasToken: true},
		orgconfig.ConfigPatch{Agents: agentsWrite("opencode", patch.Field[orgconfig.SubscriptionWrite]{})})
	if !eff.deleteToken {
		t.Fatal("choosing OpenCode kept a subscription only Claude Code can present")
	}
	if eff.settings == nil || eff.settings.Runtime != "opencode" {
		t.Fatalf("runtime not written: %+v", eff.settings)
	}
}

func TestJudgeCard_SubscriptionNullRemovesIt(t *testing.T) {
	eff := mustJudge(t, cardState{conn: anthropicConn(), hasToken: true},
		orgconfig.ConfigPatch{Agents: agentsWrite("", null[orgconfig.SubscriptionWrite]())})
	if !eff.deleteToken || eff.settings != nil {
		t.Fatalf("effects = %+v, want only the token deleted", eff)
	}
}

// A model change says nothing about the subscription: it is kept, and the
// token is never needed back.
func TestJudgeCard_AModelChangeKeepsTheToken(t *testing.T) {
	eff := mustJudge(t, cardState{conn: anthropicConn(), hasToken: true, settings: onRuntime("claude-code")},
		orgconfig.ConfigPatch{LLM: llm(orgconfig.LLMPatch{Model: "claude-haiku-4-5"})})
	if eff.deleteToken || eff.writeToken != "" {
		t.Fatalf("effects = %+v, want the token left alone", eff)
	}
}

// A blank token is refused on its own section, never saved as "nothing".
func TestJudgeCard_ABlankTokenIsRefused(t *testing.T) {
	refusal(t, cardState{conn: anthropicConn()}, orgconfig.ConfigPatch{Agents: agentsWrite("", token("   "))},
		"agents", "anthropic_key_missing")
}

// --- clause 6 and the reset ------------------------------------------------------

func TestJudgeCard_DisconnectCascadesToTheToken(t *testing.T) {
	eff := mustJudge(t, cardState{conn: anthropicConn(), hasToken: true}, orgconfig.ConfigPatch{LLM: null[orgconfig.LLMPatch]()})
	if !eff.deleteConn || !eff.deleteToken {
		t.Fatalf("effects = %+v, want the connection and the token deleted", eff)
	}
}

func TestJudgeCard_ResetDeletesSettingAndToken(t *testing.T) {
	eff := mustJudge(t, cardState{conn: anthropicConn(), hasToken: true, settings: onRuntime("claude-code")},
		orgconfig.ConfigPatch{Agents: null[orgconfig.AgentsWrite]()})
	if !eff.deleteSettings || !eff.deleteToken {
		t.Fatalf("effects = %+v, want the setting reset and the token deleted", eff)
	}
	if eff.deleteConn {
		t.Error("resetting the card disconnected the connection")
	}
}

// failingSettingsRepo fails every read.
type failingSettingsRepo struct{}

func (failingSettingsRepo) GetByOrg(context.Context, string) (*OrgAgentSettings, error) {
	return nil, errors.New("connection refused")
}

// A read failure is not "no row". Falling back to the default here would launch
// a run on a runtime the org may have moved off, without ever saying so — see
// the dispatcher's codingAgentEnv for the other half of this rule.
func TestAgentSettings_AReadFailureIsAnErrorNotTheDefaults(t *testing.T) {
	svc := NewAgentSettingsService(failingSettingsRepo{}, nil, nil, nil, nil, everyRuntime)
	if _, err := svc.Effective(context.Background(), "acme"); err == nil {
		t.Fatal("Effective swallowed a storage failure and answered with the defaults")
	}
}

// connectionsAround is what the Agent Manager provider's copy follows: the
// connection on either side of a save.
func TestConnectionsAround(t *testing.T) {
	written := ollamaConn()
	for _, tc := range []struct {
		name                  string
		state                 cardState
		eff                   cardEffects
		written               *OrgModelConnection
		wantBefore, wantAfter bool
		wantHostAfter         string
	}{
		{name: "first connect", written: anthropicConn(), wantAfter: true, wantHostAfter: modelconn.AnthropicHost},
		{name: "host change", state: cardState{conn: anthropicConn()}, written: written, wantBefore: true, wantAfter: true, wantHostAfter: modelconn.OllamaHost},
		{name: "disconnect", state: cardState{conn: anthropicConn()}, eff: cardEffects{deleteConn: true}, wantBefore: true},
		{name: "runtime-only save", state: cardState{conn: anthropicConn()}, wantBefore: true, wantAfter: true, wantHostAfter: modelconn.AnthropicHost},
		{name: "no connection either side", eff: cardEffects{deleteSettings: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, after := connectionsAround(tc.state, tc.eff, tc.written)
			if (before != nil) != tc.wantBefore || (after != nil) != tc.wantAfter {
				t.Fatalf("before=%v after=%v, want before=%v after=%v", before, after, tc.wantBefore, tc.wantAfter)
			}
			if after != nil && after.Host != tc.wantHostAfter {
				t.Fatalf("after = %+v, want host %s", after, tc.wantHostAfter)
			}
		})
	}
}

// Test connection is rationed per org on a sliding window: the eleventh call
// in a minute is refused, and the allowance returns as the window slides.
func TestLLMTestLimiter(t *testing.T) {
	now := time.Unix(1_000, 0)
	l := newLLMTestLimiter(func() time.Time { return now })
	for i := 1; i <= llmTestsPerWindow; i++ {
		if !l.allow("acme") {
			t.Fatalf("call %d refused", i)
		}
		now = now.Add(time.Second)
	}
	if l.allow("acme") {
		t.Fatal("the eleventh call in a minute was allowed")
	}
	if !l.allow("globex") {
		t.Fatal("another org shares acme's allowance")
	}
	now = time.Unix(1_000, 0).Add(llmTestWindow + 500*time.Millisecond) // only the first call has left the window
	if !l.allow("acme") {
		t.Fatal("the allowance did not return as the window slid")
	}
	if l.allow("acme") {
		t.Fatal("only one call had left the window")
	}
}
