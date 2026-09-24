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

// agents_rule.go — what one save of the AI agents card does, decided by a pure
// function over the state it saves onto. The `llm` and `agents` sections of
// PATCH /config are judged TOGETHER, on the state the patch leaves, so the order
// the writes happen in can never refuse a valid end state half-way.
//
// The one rule: a Claude subscription needs the Claude Code runtime and a
// connected API key. Everything else follows from it — choosing OpenCode,
// disconnecting the key and resetting the card each delete the stored token in
// the same transaction, and a patch that sets a token the end state could not
// use is refused.

package organization

import (
	"fmt"
	"slices"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// cardState is what a save of the card is judged against: the org's current
// setting row (nil = the platform defaults) and which credentials it holds.
type cardState struct {
	settings *OrgAgentSettings
	hasKey   bool
	hasToken bool
}

// cardEffects is what a save does, in terms of rows. Zero values mean "leave
// it": judgeCard refuses a blank key or token, so an empty one here is never
// written, and nil settings is never upserted.
type cardEffects struct {
	writeKey       string
	deleteKey      bool
	settings       *OrgAgentSettings // model + runtime to upsert; OcOrgID/actor/time stamped by the writer
	deleteSettings bool
	writeToken     string
	deleteToken    bool
}

// judgeCard decides what p does to an org in state s, or refuses it with a
// SectionError naming the section the fix belongs in. Pure: every input is an
// argument, so the same call judges the patch before the live probes (on the
// pool's view) and again under the card's lock (on the transaction's).
func judgeCard(s cardState, p orgconfig.ConfigPatch) (cardEffects, error) {
	var eff cardEffects

	keyAfter := s.hasKey
	if p.LLM.Sent {
		if p.LLM.Null {
			eff.deleteKey = s.hasKey
			keyAfter = false
		} else {
			eff.writeKey = strings.TrimSpace(p.LLM.Value.APIKey)
			if eff.writeKey == "" {
				return cardEffects{}, sectionErrorFrom("llm", errCredentialMissing("an Anthropic API key"))
			}
			keyAfter = true
		}
	}

	runtimeAfter := orgconfig.DefaultAgentRuntime
	if s.settings != nil {
		runtimeAfter = s.settings.Runtime
	}
	tokenAfter := s.hasToken
	newToken := ""
	if p.Agents.Sent {
		if p.Agents.Null {
			// Reset: back on the platform's defaults, and the subscription goes
			// with the section it belongs to.
			eff.deleteSettings = s.settings != nil
			runtimeAfter = orgconfig.DefaultAgentRuntime
			tokenAfter = false
		} else {
			w := p.Agents.Value
			settings, err := resolveAgentSettings(s.settings, w)
			if err != nil {
				return cardEffects{}, sectionErrorFrom("agents", err)
			}
			if settings != nil {
				eff.settings = settings
				runtimeAfter = settings.Runtime
			}
			if w.Subscription.Sent {
				if w.Subscription.Null {
					tokenAfter = false
				} else {
					newToken = strings.TrimSpace(w.Subscription.Value.Token)
					if newToken == "" {
						return cardEffects{}, sectionErrorFrom("agents", errCredentialMissing("a Claude subscription token"))
					}
					tokenAfter = true
				}
			}
		}
	}

	// A new token has to be usable in the end state, or the save is refused:
	// silently dropping a token the reader just pasted would be worse than
	// saying why it cannot be kept.
	if newToken != "" {
		if runtimeAfter != orgconfig.AgentRuntimeClaudeCode {
			return cardEffects{}, sectionErrorFrom("agents", errSubscriptionRequiresClaudeCode())
		}
		if !keyAfter {
			return cardEffects{}, sectionErrorFrom("agents", errSubscriptionRequiresAPIKey())
		}
		eff.writeToken = newToken
	}
	// A stored token the end state cannot use goes in the same save: OpenCode
	// cannot present one, and it cannot outlive the key it sits beside.
	if runtimeAfter != orgconfig.AgentRuntimeClaudeCode || !keyAfter {
		tokenAfter = false
	}
	eff.deleteToken = s.hasToken && !tokenAfter && eff.writeToken == ""
	return eff, nil
}

// resolveAgentSettings is the row a write leaves: each omitted field keeps the
// org's current value (or the platform default when it has none). nil when the
// write names neither field — a subscription-only save leaves the setting row,
// and with it "who chose the model", alone.
func resolveAgentSettings(current *OrgAgentSettings, w orgconfig.AgentsWrite) (*OrgAgentSettings, error) {
	runtime := orgconfig.AgentRuntime(strings.TrimSpace(string(w.Runtime)))
	model := strings.TrimSpace(w.Model)
	if runtime == "" && model == "" {
		return nil, nil
	}
	out := &OrgAgentSettings{Runtime: orgconfig.DefaultAgentRuntime, Model: orgconfig.DefaultAgentModel}
	if current != nil {
		out.Runtime, out.Model = current.Runtime, current.Model
	}
	if runtime != "" {
		if err := validateRuntime(runtime); err != nil {
			return nil, err
		}
		out.Runtime = runtime
	}
	if model != "" {
		if err := validateModel(model); err != nil {
			return nil, err
		}
		out.Model = model
	}
	return out, nil
}

func validateModel(model string) error {
	if slices.Contains(orgconfig.AgentModels, model) {
		return nil
	}
	return &ValidationError{
		Code: "agents_model_unknown",
		Message: fmt.Sprintf("model %q is not one this platform offers (%s)",
			model, strings.Join(orgconfig.AgentModels, ", ")),
	}
}

// validateRuntime refuses a runtime outside the enum by name: running another
// runtime would bill an organization for one it did not choose and never tell it.
func validateRuntime(runtime orgconfig.AgentRuntime) error {
	if slices.Contains(orgconfig.AgentRuntimes, runtime) {
		return nil
	}
	names := make([]string, 0, len(orgconfig.AgentRuntimes))
	for _, r := range orgconfig.AgentRuntimes {
		names = append(names, string(r))
	}
	return &ValidationError{
		Code:    "agents_runtime_unknown",
		Message: fmt.Sprintf("runtime %q does not exist (%s)", runtime, strings.Join(names, ", ")),
	}
}

// errCredentialMissing refuses a credential field sent blank: saving nothing
// and answering 200 would tell the reader a key was stored when none was, and
// a blank key must never count as "connected" for the subscription rule.
func errCredentialMissing(what string) *ValidationError {
	return &ValidationError{Code: "anthropic_key_missing", Message: what + " is required"}
}

func errSubscriptionRequiresClaudeCode() *ValidationError {
	return &ValidationError{
		Code: "agents_subscription_requires_claude_code",
		Message: "a Claude subscription bills the coding agent only on Claude Code, and this save " +
			"leaves the runtime on OpenCode. Choose Claude Code to use the subscription, or save " +
			"OpenCode without one",
	}
}

func errSubscriptionRequiresAPIKey() *ValidationError {
	return &ValidationError{
		Code: "agents_subscription_requires_api_key",
		Message: "a Claude subscription sits beside the organization's Anthropic API key and cannot " +
			"exist without it. Connect the API key in the same save, or first",
	}
}
