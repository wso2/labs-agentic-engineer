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

package aestudio

import "github.com/wso2/aep/aep-api/internal/platform/modelconn"

// modelConnectionEnv is AE_MODEL_CONNECTION: the org's model connection as
// the design agent's connectionFromEnv parses it (field names and order
// pinned by @aep/agent-stream's TurnConnection), plus the model. The bytes
// are hashed into the pod's pinned parameters, so a change here re-pins and
// rolls every org's pod (TestDesired_ModelConnectionBytesArePinned).
type modelConnectionEnv struct {
	modelConnectionWire
	Model string `json:"model"`
}

// modelConnectionWire is everything the agent needs to build the model
// except the key, which reaches the pod as its own secret.
type modelConnectionWire struct {
	Format     modelconn.Format     `json:"format"`
	BaseURL    string               `json:"baseURL"`
	AuthScheme modelconn.AuthScheme `json:"authScheme"`
	// ContextWindow and OutputLimit are omitted where the model is known to
	// the runtime (Anthropic's own API).
	ContextWindow *int                  `json:"contextWindow,omitempty"`
	OutputLimit   *int                  `json:"outputLimit,omitempty"`
	Capabilities  modelCapabilitiesWire `json:"capabilities"`
}

// modelCapabilitiesWire is modelconn.Capabilities on the wire, pinned by
// @aep/agent-stream's ModelCapabilities.
type modelCapabilitiesWire struct {
	ClaudeCode         bool                `json:"claudeCode"`
	ClaudeSubscription bool                `json:"claudeSubscription"`
	PromptCache        bool                `json:"promptCache"`
	GeneratedAgents    bool                `json:"generatedAgents"`
	NativePDF          bool                `json:"nativePdf"`
	WebSearch          modelconn.WebSearch `json:"webSearch"`
	ImageInput         modelconn.Tristate  `json:"imageInput"`
}

// modelConnectionEnvFor is c as AE_MODEL_CONNECTION carries it, with its
// capabilities computed here (modelconn.CapabilitiesOf) so the agent never
// re-derives them from the host.
func modelConnectionEnvFor(c modelconn.Connection) modelConnectionEnv {
	caps := modelconn.CapabilitiesOf(c)
	return modelConnectionEnv{
		modelConnectionWire: modelConnectionWire{
			Format:        c.Format,
			BaseURL:       c.BaseURL,
			AuthScheme:    c.AuthScheme,
			ContextWindow: c.ContextWindow,
			OutputLimit:   c.OutputLimit,
			Capabilities: modelCapabilitiesWire{
				ClaudeCode:         caps.ClaudeCode,
				ClaudeSubscription: caps.ClaudeSubscription,
				PromptCache:        caps.PromptCache,
				GeneratedAgents:    caps.GeneratedAgents,
				NativePDF:          caps.NativePDF,
				WebSearch:          caps.WebSearch,
				ImageInput:         caps.ImageInput,
			},
		},
		Model: c.Model,
	}
}
