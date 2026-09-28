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
// The clauses (model_connection_rule.go merges the `llm` patch itself):
//
//  1. First connect needs a format and a key, plus a URL when the format has no
//     default.
//  2. A host change needs a key; a format change on the same host keeps it.
//  3. A format needs a runtime this installation runs.
//  4. Claude Code needs the Anthropic format.
//  5. The Claude subscription needs `claudeSubscription` (Anthropic's own API)
//     and Claude Code: a save that leaves either deletes the stored token in
//     the same transaction, and a new token the end state cannot use is
//     refused.
//  6. `llm: null` disconnects: the connection, its bytes and the token go.
//
// Beside them, a save may only choose a runtime this installation can run
// (runtimes): one with no runner image would fail every dispatch.

package organization

import (
	"fmt"
	"slices"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// cardState is what a save of the card is judged against: the org's setting
// row (nil = the platform default), its connection (nil = none) and whether it
// holds a Claude subscription.
type cardState struct {
	settings *OrgAgentSettings
	conn     *OrgModelConnection
	hasToken bool
}

// cardEffects is what a save does, in terms of rows. Zero values mean "leave
// it": judgeCard refuses a blank token, so an empty one here is never written,
// and nil settings is never upserted.
type cardEffects struct {
	// writeConn is the connection to probe and then write; nil leaves the
	// stored one as it is.
	writeConn      *connectionDraft
	deleteConn     bool
	settings       *OrgAgentSettings // runtime to upsert; OcOrgID/actor/time stamped by the writer
	deleteSettings bool
	writeToken     string
	deleteToken    bool
}

// judgeCard decides what p does to an org in state s, on an installation that
// runs runtimes, or refuses it with a SectionError naming the section the fix
// belongs in. Pure: every input is an argument, so the same call judges the
// patch before the live probes (on the pool's view) and again under the card's
// lock (on the transaction's).
func judgeCard(s cardState, runtimes []orgconfig.AgentRuntime, p orgconfig.ConfigPatch) (cardEffects, error) {
	var eff cardEffects

	// connAfter is the connection the save leaves, as capabilities read it.
	var connAfter *modelconn.Connection
	if s.conn != nil {
		c := s.conn.Connection()
		connAfter = &c
	}
	if p.LLM.Sent {
		if p.LLM.Null {
			// Clause 6.
			eff.deleteConn = s.conn != nil
			connAfter = nil
		} else {
			draft, changed, err := draftConnection(s.conn, p.LLM.Value)
			if err != nil {
				return cardEffects{}, sectionErrorFrom("llm", err)
			}
			if changed {
				// Clause 3.
				if len(runtimesFor(draft.Format, runtimes)) == 0 {
					return cardEffects{}, sectionErrorFrom("llm", errFormatHasNoRuntime(draft.Format, runtimes))
				}
				eff.writeConn = &draft
				c := draft.connection()
				connAfter = &c
			}
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
			// Reset: back on the platform's default, and the subscription goes
			// with the section it belongs to.
			eff.deleteSettings = s.settings != nil
			runtimeAfter = orgconfig.DefaultAgentRuntime
			tokenAfter = false
		} else {
			w := p.Agents.Value
			settings, err := resolveAgentSettings(runtimes, w)
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

	// Clause 4. The card sends `runtime: opencode` in the patch that switches
	// the format, so the fix is named on agents.
	if connAfter != nil && runtimeAfter == orgconfig.AgentRuntimeClaudeCode && !modelconn.CapabilitiesOf(*connAfter).ClaudeCode {
		return cardEffects{}, sectionErrorFrom("agents", errRuntimeRequiresAnthropicFormat(connAfter.Format))
	}

	// Clause 5. A new token has to be usable in the end state, or the save is
	// refused: silently dropping a token the reader just pasted would be worse
	// than saying why it cannot be kept.
	subscriptionUsable := connAfter != nil && modelconn.CapabilitiesOf(*connAfter).ClaudeSubscription &&
		runtimeAfter == orgconfig.AgentRuntimeClaudeCode
	if newToken != "" {
		switch {
		case connAfter == nil:
			return cardEffects{}, sectionErrorFrom("agents", errSubscriptionRequiresConnection())
		case runtimeAfter != orgconfig.AgentRuntimeClaudeCode:
			return cardEffects{}, sectionErrorFrom("agents", errSubscriptionRequiresClaudeCode())
		case !subscriptionUsable:
			return cardEffects{}, sectionErrorFrom("agents", errSubscriptionRequiresAnthropicHost(connAfter.Host))
		}
		eff.writeToken = newToken
	}
	// A stored token the end state cannot use goes in the same save: OpenCode
	// cannot present one, only Anthropic's own API accepts one, and it cannot
	// outlive the connection it sits beside.
	if !subscriptionUsable {
		tokenAfter = false
	}
	eff.deleteToken = s.hasToken && !tokenAfter && eff.writeToken == ""
	return eff, nil
}

// connectionsAround is the org's model connection on either side of a save,
// nil where it has none: what the Agent Manager provider's copy of the key
// follows (syncModelProvider). written is the row the save wrote, if any.
func connectionsAround(s cardState, eff cardEffects, written *OrgModelConnection) (before, after *modelconn.Connection) {
	if s.conn != nil {
		c := s.conn.Connection()
		before = &c
	}
	switch {
	case written != nil:
		c := written.Connection()
		after = &c
	case !eff.deleteConn:
		after = before
	}
	return before, after
}

// resolveAgentSettings is the row a write leaves, or nil when the write names
// no runtime — a subscription-only save leaves the setting row, and with it
// "who chose the runtime", alone.
func resolveAgentSettings(runtimes []orgconfig.AgentRuntime, w orgconfig.AgentsWrite) (*OrgAgentSettings, error) {
	runtime := orgconfig.AgentRuntime(strings.TrimSpace(string(w.Runtime)))
	if runtime == "" {
		return nil, nil
	}
	if err := validateRuntime(runtime, runtimes); err != nil {
		return nil, err
	}
	return &OrgAgentSettings{Runtime: runtime}, nil
}

// validateRuntime refuses a runtime outside the enum by name: running another
// runtime would bill an organization for one it did not choose and never tell
// it. It refuses one this installation cannot run (runtimes) under its own code:
// the save would succeed and every coding dispatch after it would fail.
func validateRuntime(runtime orgconfig.AgentRuntime, runtimes []orgconfig.AgentRuntime) error {
	if !slices.Contains(orgconfig.AgentRuntimes, runtime) {
		return &ValidationError{
			Code:    "agents_runtime_unknown",
			Message: fmt.Sprintf("runtime %q does not exist (%s)", runtime, runtimeNames(orgconfig.AgentRuntimes)),
		}
	}
	if !slices.Contains(runtimes, runtime) {
		return &ValidationError{
			Code: "agents_runtime_unavailable",
			Message: fmt.Sprintf("runtime %q is not available on this installation, which has no runner "+
				"image for it (available: %s)", runtime, runtimeNames(runtimes)),
		}
	}
	return nil
}

func runtimeNames(runtimes []orgconfig.AgentRuntime) string {
	names := make([]string, 0, len(runtimes))
	for _, r := range runtimes {
		names = append(names, string(r))
	}
	return strings.Join(names, ", ")
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

func errSubscriptionRequiresConnection() *ValidationError {
	return &ValidationError{
		Code: "agents_subscription_requires_connection",
		Message: "a Claude subscription sits beside the organization's model connection and cannot " +
			"exist without it. Save a connection to Anthropic's API in the same save, or first",
	}
}

func errSubscriptionRequiresAnthropicHost(host string) *ValidationError {
	return &ValidationError{
		Code: "agents_subscription_requires_anthropic_host",
		Message: fmt.Sprintf("a Claude subscription authenticates only against Anthropic's own API (%s), "+
			"and the connection points at %s", modelconn.AnthropicHost, host),
	}
}

func errRuntimeRequiresAnthropicFormat(format modelconn.Format) *ValidationError {
	return &ValidationError{
		Code: "agents_runtime_requires_anthropic_format",
		Message: fmt.Sprintf("Claude Code speaks only the Anthropic format, and the connection is %s. "+
			"Choose OpenCode in the same save", format),
	}
}
