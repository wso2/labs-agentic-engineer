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
	"context"
	"maps"
	"testing"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

// TestModelEnv is the runner's env contract, connection by connection and
// credential kind by credential kind.
func TestModelEnv(t *testing.T) {
	anthropicOnOllama := ollamaConnection()
	anthropicOnOllama.Format = modelconn.FormatAnthropic
	// Saved with its /v1, as every SDK takes it; only Claude Code wants the root,
	// and its adapter derives it.
	anthropicOnOllama.BaseURL = "https://ollama.com/v1"
	gateway := ollamaConnection()
	gateway.BaseURL, gateway.Host = "https://llm.example.com/v1", "llm.example.com"

	for _, tc := range []struct {
		name    string
		cred    organization.CodingCredential
		env     map[string]string
		keyVar  string
		comment string
	}{
		{
			name: "connection key on Anthropic's own API",
			cred: organization.CodingCredential{Conn: firstPartyConnection(), Kind: organization.CodingCredentialConnectionKey},
			env: map[string]string{
				"AEP_MODEL_FORMAT":      "anthropic",
				"AEP_MODEL_BASE_URL":    "https://api.anthropic.com/v1",
				"AEP_MODEL_AUTH_SCHEME": "x-api-key",
				"AEP_MODEL_WEB_SEARCH":  "anthropic-server-tool",
			},
			keyVar:  "ANTHROPIC_API_KEY",
			comment: "the name a runner image older than the connection reads",
		},
		{
			name: "Claude subscription on Anthropic's own API",
			cred: organization.CodingCredential{Conn: firstPartyConnection(), Kind: organization.CodingCredentialClaudeSubscription},
			env: map[string]string{
				"AEP_MODEL_FORMAT":      "anthropic",
				"AEP_MODEL_BASE_URL":    "https://api.anthropic.com/v1",
				"AEP_MODEL_AUTH_SCHEME": "x-api-key",
				"AEP_MODEL_WEB_SEARCH":  "anthropic-server-tool",
			},
			keyVar: "CLAUDE_CODE_OAUTH_TOKEN",
		},
		{
			name: "OpenAI-compatible on Ollama",
			cred: organization.CodingCredential{Conn: ollamaConnection(), Kind: organization.CodingCredentialConnectionKey},
			env: map[string]string{
				"AEP_MODEL_FORMAT":         "openai-compatible",
				"AEP_MODEL_BASE_URL":       "https://ollama.com/v1",
				"AEP_MODEL_AUTH_SCHEME":    "bearer",
				"AEP_MODEL_CONTEXT_WINDOW": "131072",
				"AEP_MODEL_OUTPUT_LIMIT":   "32768",
				"AEP_MODEL_WEB_SEARCH":     "ollama-api",
			},
			keyVar: "AEP_MODEL_API_KEY",
		},
		{
			name: "Anthropic format on Ollama",
			cred: organization.CodingCredential{Conn: anthropicOnOllama, Kind: organization.CodingCredentialConnectionKey},
			env: map[string]string{
				"AEP_MODEL_FORMAT":         "anthropic",
				"AEP_MODEL_BASE_URL":       "https://ollama.com/v1",
				"AEP_MODEL_AUTH_SCHEME":    "bearer",
				"AEP_MODEL_CONTEXT_WINDOW": "131072",
				"AEP_MODEL_OUTPUT_LIMIT":   "32768",
				"AEP_MODEL_WEB_SEARCH":     "ollama-api",
			},
			keyVar:  "AEP_MODEL_API_KEY",
			comment: "the Anthropic format alone is not Anthropic's API: the key never takes Anthropic's name off it",
		},
		{
			name: "a host with no web search adapter",
			cred: organization.CodingCredential{Conn: gateway, Kind: organization.CodingCredentialConnectionKey},
			env: map[string]string{
				"AEP_MODEL_FORMAT":         "openai-compatible",
				"AEP_MODEL_BASE_URL":       "https://llm.example.com/v1",
				"AEP_MODEL_AUTH_SCHEME":    "bearer",
				"AEP_MODEL_CONTEXT_WINDOW": "131072",
				"AEP_MODEL_OUTPUT_LIMIT":   "32768",
				"AEP_MODEL_WEB_SEARCH":     "none",
			},
			keyVar: "AEP_MODEL_API_KEY",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, keyVar := modelEnv(tc.cred)
			if !maps.Equal(env, tc.env) {
				t.Errorf("env = %v, want %v", env, tc.env)
			}
			if keyVar != tc.keyVar {
				t.Errorf("key variable = %q, want %q (%s)", keyVar, tc.keyVar, tc.comment)
			}
		})
	}
}

// TestDispatch_NonAnthropicConnection_MountsTheConnectionKey: a run on another
// host carries its connection as plain env and its key under the connection's
// own variable, never under a name Claude Code would present to Anthropic.
func TestDispatch_NonAnthropicConnection_MountsTheConnectionKey(t *testing.T) {
	rec := &chainRecorder{}
	anthropic, github := fullSecretRefs()
	conn := ollamaConnection()
	anthropic.conn = &conn
	e := newCodingDispatchExecutor(anthropic, github)
	e.WithPublisherCredentials(fakePublisher{name: "acme-publisher-secrets"}, "http://thunder.example/oauth2/token")
	e.WithOCDispatch(NewOCDispatcher(rec.client()).WithImage("ghcr.io/wso2/aep/remote-worker:latest"))

	launch, err := e.Dispatch(context.Background(), codingMilestoneDispatch())
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if launch.ModelHost != modelconn.OllamaHost {
		t.Errorf("model host = %q, want %q", launch.ModelHost, modelconn.OllamaHost)
	}
	key := anthropicSecretEnv(t, rec.load, anthropic.ref.Name)
	if key.Key != "AEP_MODEL_API_KEY" {
		t.Errorf("connection key mounted as %q, want AEP_MODEL_API_KEY", key.Key)
	}
	for _, name := range []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN"} {
		if hasEnvKey(rec.load, name) {
			t.Errorf("%s must not be on a pod whose connection is not Anthropic's API", name)
		}
	}
	for name, want := range map[string]string{
		"AEP_MODEL_FORMAT":         "openai-compatible",
		"AEP_MODEL_BASE_URL":       "https://ollama.com/v1",
		"AEP_MODEL_AUTH_SCHEME":    "bearer",
		"AEP_MODEL_CONTEXT_WINDOW": "131072",
		"AEP_MODEL_OUTPUT_LIMIT":   "32768",
		"AEP_MODEL_WEB_SEARCH":     "ollama-api",
	} {
		ev := secretEnvByKey(t, rec.load, name)
		if ev.ValueFrom != nil || ev.Value != want {
			t.Errorf("%s = %+v, want the plain value %q", name, ev, want)
		}
	}
}

// TestDispatch_OpenAICompatibleConnection_MountsTheEvaluationKey: evaluation
// runs on any connection. The harness boots the generated agent and its judge
// on the connection's own format, URL, model and auth scheme, so all four ride
// beside the key.
func TestDispatch_OpenAICompatibleConnection_MountsTheEvaluationKey(t *testing.T) {
	rec := &chainRecorder{}
	anthropic, github := fullSecretRefs()
	conn := ollamaConnection()
	anthropic.conn = &conn
	e := newCodingDispatchExecutor(anthropic, github)
	e.WithPublisherCredentials(fakePublisher{name: "acme-publisher-secrets"}, "http://thunder.example/oauth2/token")
	e.WithOCDispatch(NewOCDispatcher(rec.client()).WithImage("ghcr.io/wso2/aep/remote-worker:latest"))

	if _, err := e.Dispatch(context.Background(), codingMilestoneDispatch()); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	ev := secretEnvByKey(t, rec.load, envEvalModelAPIKey)
	if ev.ValueFrom == nil || ev.ValueFrom.SecretKeyRef == nil || ev.ValueFrom.SecretKeyRef.Name != anthropic.defaultRef.Name {
		t.Fatalf("%s = %+v, want a SecretReference to the connection key %q", envEvalModelAPIKey, ev, anthropic.defaultRef.Name)
	}
	assertEvalConnEnv(t, rec.load, map[string]string{
		envEvalModelFormat:     "openai-compatible",
		envEvalModelBaseURL:    "https://ollama.com/v1",
		envEvalModelName:       "gpt-oss:20b",
		envEvalModelAuthScheme: "bearer",
	})
	if !hasEnvKey(rec.load, envEvalKeyManaged) {
		t.Errorf("%s must still declare the platform owns the evaluation key", envEvalKeyManaged)
	}
}
