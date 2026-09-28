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

package codingagent

import (
	"strconv"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

// The model connection as a coding Job carries it: the runner's env contract,
// read by exactly one module there (`remote-worker/src/lib/model_connection.ts`).
// Plain values, never secrets: the connection is what the key is FOR, and the
// key itself rides a secret ref under the variable modelKeyEnvVar names.
const (
	envModelFormat        = "AEP_MODEL_FORMAT"
	envModelBaseURL       = "AEP_MODEL_BASE_URL"
	envModelAuthScheme    = "AEP_MODEL_AUTH_SCHEME"
	envModelContextWindow = "AEP_MODEL_CONTEXT_WINDOW"
	envModelOutputLimit   = "AEP_MODEL_OUTPUT_LIMIT"
	envModelWebSearch     = "AEP_MODEL_WEB_SEARCH"

	// envModelAPIKey carries the connection's key on any connection other than
	// Anthropic's own API. Each runtime adapter maps it to what its binary
	// reads (Claude Code's ANTHROPIC_AUTH_TOKEN or ANTHROPIC_API_KEY, OpenCode's
	// provider block), so this layer never names a runtime's variable for it.
	envModelAPIKey = "AEP_MODEL_API_KEY"

	// envClaudeCodeOAuthToken carries a Claude subscription: a Claude Code
	// session token, which only that runtime can present.
	envClaudeCodeOAuthToken = "CLAUDE_CODE_OAUTH_TOKEN"
)

// modelEnv is the ONE mapping from a coding run's resolved credential to the
// runner's env contract: the connection as plain values, and the variable the
// one mounted credential must arrive under.
//
// The connection's values are stamped on every dispatch, Anthropic's own API
// included, where they equal what the runner assumes when they are absent — so
// every Job proves the plumbing, and none depends on a default. The context
// window and the output limit are the exception: absent on api.anthropic.com
// (the connection carries none), where each runtime knows Claude's own.
func modelEnv(cred organization.CodingCredential) (env map[string]string, keyEnvVar string) {
	conn := cred.Conn
	env = map[string]string{
		envModelFormat:     string(conn.Format),
		envModelBaseURL:    conn.BaseURL,
		envModelAuthScheme: string(conn.AuthScheme),
		envModelWebSearch:  string(modelconn.CapabilitiesOf(conn).WebSearch),
	}
	if conn.ContextWindow != nil {
		env[envModelContextWindow] = strconv.Itoa(*conn.ContextWindow)
	}
	if conn.OutputLimit != nil {
		env[envModelOutputLimit] = strconv.Itoa(*conn.OutputLimit)
	}
	return env, modelKeyEnvVar(cred)
}

// modelKeyEnvVar names the variable a coding run's one model credential is
// mounted under. Exactly one is mounted (ADR-0036): Claude Code ranks
// ANTHROPIC_API_KEY above CLAUDE_CODE_OAUTH_TOKEN, so a pod holding both would
// bill the key and ignore the org's subscription.
//
// A connection key on Anthropic's own API keeps ANTHROPIC_API_KEY (ADR-0038
// §6); every other host gets AEP_MODEL_API_KEY.
func modelKeyEnvVar(cred organization.CodingCredential) string {
	switch {
	case cred.Kind == organization.CodingCredentialClaudeSubscription:
		return envClaudeCodeOAuthToken
	case cred.Conn.Format == modelconn.FormatAnthropic && cred.Conn.Host == modelconn.AnthropicHost:
		return envAnthropicAPIKey
	default:
		return envModelAPIKey
	}
}

// evalModelEnv is the connection the build's agent-evaluation key is for, as
// plain values beside it: the harness boots the generated agent and runs its
// judge on the same connection the deployed agent will use, on any format.
// Composed only alongside the key (see evaluationKeyRef).
func evalModelEnv(conn modelconn.Connection) map[string]string {
	return map[string]string{
		envEvalModelFormat:     string(conn.Format),
		envEvalModelBaseURL:    conn.BaseURL,
		envEvalModelName:       conn.Model,
		envEvalModelAuthScheme: string(conn.AuthScheme),
	}
}
