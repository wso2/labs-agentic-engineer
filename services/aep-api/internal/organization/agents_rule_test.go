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
// card, as a pure function: what a patch does to an org in a given state, and
// which patches it refuses. The DB-backed half (one transaction, nothing
// written on failure) is config_agents_component_test.go.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
	"github.com/wso2/aep/aep-api/internal/platform/patch"
)

const (
	ruleKey   = "sk-ant-api03-RULEtestKeyABCDEFGHIJKLmnop"
	ruleToken = "sk-ant-oat01-RULEtestTokenABCDEFGHIJKLmn"
)

func set[T any](v T) patch.Field[T] { return patch.Field[T]{Sent: true, Value: v} }
func null[T any]() patch.Field[T]   { return patch.Field[T]{Sent: true, Null: true} }

func agentsWrite(runtime orgconfig.AgentRuntime, model string, sub patch.Field[orgconfig.SubscriptionWrite]) patch.Field[orgconfig.AgentsWrite] {
	return set(orgconfig.AgentsWrite{Runtime: runtime, Model: model, Subscription: sub})
}

func token(t string) patch.Field[orgconfig.SubscriptionWrite] {
	return set(orgconfig.SubscriptionWrite{Kind: orgconfig.SubscriptionKindClaude, Token: t})
}

func onRuntime(r orgconfig.AgentRuntime) *OrgAgentSettings {
	return &OrgAgentSettings{Runtime: r, Model: orgconfig.DefaultAgentModel}
}

func mustJudge(t *testing.T, s cardState, p orgconfig.ConfigPatch) cardEffects {
	t.Helper()
	eff, err := judgeCard(s, p)
	if err != nil {
		t.Fatalf("judgeCard refused: %v", err)
	}
	return eff
}

// refusal asserts judgeCard refused p on body.<section> with code.
func refusal(t *testing.T, s cardState, p orgconfig.ConfigPatch, section, code string) {
	t.Helper()
	_, err := judgeCard(s, p)
	var se *SectionError
	if !errors.As(err, &se) {
		t.Fatalf("judgeCard = %v, want a SectionError", err)
	}
	if se.Section != section || se.Code != code {
		t.Fatalf("refused on body.%s (%s), want body.%s (%s): %s", se.Section, se.Code, section, code, se.Message)
	}
}

func TestJudgeCard_SubscriptionNeedsClaudeCode(t *testing.T) {
	withKey := cardState{hasKey: true}
	// OpenCode chosen in the same patch as a new token.
	refusal(t, withKey, orgconfig.ConfigPatch{Agents: agentsWrite("opencode", "", token(ruleToken))},
		"agents", "agents_subscription_requires_claude_code")
	// A new token onto an org already on OpenCode.
	refusal(t, cardState{hasKey: true, settings: onRuntime("opencode")},
		orgconfig.ConfigPatch{Agents: agentsWrite("", "", token(ruleToken))},
		"agents", "agents_subscription_requires_claude_code")
}

func TestJudgeCard_SubscriptionNeedsAnAPIKey(t *testing.T) {
	// No key at all.
	refusal(t, cardState{}, orgconfig.ConfigPatch{Agents: agentsWrite("", "", token(ruleToken))},
		"agents", "agents_subscription_requires_api_key")
	// Disconnecting the key in the same patch.
	refusal(t, cardState{hasKey: true}, orgconfig.ConfigPatch{
		LLM:    null[orgconfig.LLMWrite](),
		Agents: agentsWrite("", "", token(ruleToken)),
	}, "agents", "agents_subscription_requires_api_key")
}

// The key and the token can arrive in ONE save: the rule is judged on the state
// the patch leaves, not on the one it starts from.
func TestJudgeCard_KeyAndTokenInOneSave(t *testing.T) {
	eff := mustJudge(t, cardState{}, orgconfig.ConfigPatch{
		LLM:    set(orgconfig.LLMWrite{Kind: "anthropic", APIKey: "  " + ruleKey + "\n"}),
		Agents: agentsWrite("", "", token(ruleToken)),
	})
	if eff.writeKey != ruleKey || eff.writeToken != ruleToken {
		t.Fatalf("effects = %+v, want the trimmed key and the token written", eff)
	}
	if eff.settings != nil {
		t.Errorf("a subscription-only agents write touched the setting row: %+v", eff.settings)
	}
}

func TestJudgeCard_OpenCodeDeletesTheToken(t *testing.T) {
	eff := mustJudge(t, cardState{hasKey: true, hasToken: true},
		orgconfig.ConfigPatch{Agents: agentsWrite("opencode", "", patch.Field[orgconfig.SubscriptionWrite]{})})
	if !eff.deleteToken {
		t.Fatal("choosing OpenCode kept a subscription only Claude Code can present")
	}
	if eff.settings == nil || eff.settings.Runtime != "opencode" {
		t.Fatalf("runtime not written: %+v", eff.settings)
	}
}

func TestJudgeCard_DisconnectCascadesToTheToken(t *testing.T) {
	eff := mustJudge(t, cardState{hasKey: true, hasToken: true}, orgconfig.ConfigPatch{LLM: null[orgconfig.LLMWrite]()})
	if !eff.deleteKey || !eff.deleteToken {
		t.Fatalf("effects = %+v, want the key and the token deleted", eff)
	}
}

func TestJudgeCard_ResetDeletesSettingAndToken(t *testing.T) {
	eff := mustJudge(t, cardState{hasKey: true, hasToken: true, settings: onRuntime("claude-code")},
		orgconfig.ConfigPatch{Agents: null[orgconfig.AgentsWrite]()})
	if !eff.deleteSettings || !eff.deleteToken {
		t.Fatalf("effects = %+v, want the setting reset and the token deleted", eff)
	}
	if eff.deleteKey {
		t.Error("resetting the card disconnected the API key")
	}
}

func TestJudgeCard_SubscriptionNullRemovesIt(t *testing.T) {
	eff := mustJudge(t, cardState{hasKey: true, hasToken: true},
		orgconfig.ConfigPatch{Agents: agentsWrite("", "", null[orgconfig.SubscriptionWrite]())})
	if !eff.deleteToken || eff.settings != nil {
		t.Fatalf("effects = %+v, want only the token deleted", eff)
	}
}

// A model change says nothing about the subscription: it is kept, and the
// token is never needed back.
func TestJudgeCard_AModelChangeKeepsTheToken(t *testing.T) {
	eff := mustJudge(t, cardState{hasKey: true, hasToken: true, settings: onRuntime("claude-code")},
		orgconfig.ConfigPatch{Agents: agentsWrite("", "claude-haiku-4-5", patch.Field[orgconfig.SubscriptionWrite]{})})
	if eff.deleteToken || eff.writeToken != "" {
		t.Fatalf("effects = %+v, want the token left alone", eff)
	}
	if eff.settings == nil || eff.settings.Model != "claude-haiku-4-5" || eff.settings.Runtime != "claude-code" {
		t.Fatalf("settings = %+v, want the model changed and the runtime kept", eff.settings)
	}
}

func TestJudgeCard_UnknownValuesAreRefusedByName(t *testing.T) {
	refusal(t, cardState{}, orgconfig.ConfigPatch{Agents: agentsWrite("cursor", "", patch.Field[orgconfig.SubscriptionWrite]{})},
		"agents", "agents_runtime_unknown")
	_, err := judgeCard(cardState{}, orgconfig.ConfigPatch{Agents: agentsWrite("", "claude-opus-5", patch.Field[orgconfig.SubscriptionWrite]{})})
	var se *SectionError
	if !errors.As(err, &se) || se.Code != "agents_model_unknown" {
		t.Fatalf("judgeCard(claude-opus-5) = %v, want agents_model_unknown", err)
	}
	if !strings.Contains(se.Message, "claude-sonnet-5") {
		t.Errorf("message %q does not say what IS on offer", se.Message)
	}
}

// failingSettingsRepo fails every read.
type failingSettingsRepo struct{}

func (failingSettingsRepo) GetByOrg(context.Context, string) (*OrgAgentSettings, error) {
	return nil, errors.New("connection refused")
}

// A read failure is not "no row". Falling back to the defaults here would launch
// a run on a model the org may have moved off, and bill it, without ever saying
// so — see the dispatcher's codingAgentEnv for the other half of this rule.
func TestAgentSettings_AReadFailureIsAnErrorNotTheDefaults(t *testing.T) {
	svc := NewAgentSettingsService(failingSettingsRepo{}, nil, nil, nil)
	if _, err := svc.Effective(context.Background(), "acme"); err == nil {
		t.Fatal("Effective swallowed a storage failure and answered with the defaults")
	}
	if _, err := svc.Model(context.Background(), "acme"); err == nil {
		t.Fatal("Model swallowed a storage failure and answered with the default")
	}
}

// A blank credential is refused on its own section: it must never count as a
// connected key for the subscription rule, nor answer 200 while saving nothing.
func TestJudgeCard_BlankCredentialsAreRefused(t *testing.T) {
	blankKey := set(orgconfig.LLMWrite{Kind: "anthropic", APIKey: " \n\t"})
	refusal(t, cardState{}, orgconfig.ConfigPatch{LLM: blankKey}, "llm", "anthropic_key_missing")
	refusal(t, cardState{}, orgconfig.ConfigPatch{LLM: blankKey, Agents: agentsWrite("", "", token(ruleToken))},
		"llm", "anthropic_key_missing")
	refusal(t, cardState{hasKey: true}, orgconfig.ConfigPatch{Agents: agentsWrite("", "", token("   "))},
		"agents", "anthropic_key_missing")
}
