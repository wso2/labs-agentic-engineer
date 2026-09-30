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

// Package modelconn is the organization's model connection as every consumer
// reads it: which API format, which URL, which model, how the key
// authenticates, and what the connection supports.
//
// It is a pure leaf — types, the format table and one function — imported by
// the domains that resolve, dispatch or project a connection.
package modelconn

// Format is the API a connection speaks.
type Format string

const (
	FormatAnthropic        Format = "anthropic"
	FormatOpenAICompatible Format = "openai-compatible"
)

// AuthScheme is how the connection's key is presented.
type AuthScheme string

const (
	AuthXAPIKey AuthScheme = "x-api-key"
	AuthBearer  AuthScheme = "bearer"
)

// Tristate is a fact that may be unknown: what a probe could not establish is
// "unknown", never guessed. Producers always set one of the three.
type Tristate string

const (
	Yes     Tristate = "yes"
	No      Tristate = "no"
	Unknown Tristate = "unknown"
)

// Hosts that features are bound to: Anthropic's own API with the Anthropic
// format (the subscription, the web-search server tool, native PDFs) and
// Ollama's, whatever the format (its web-search API).
const (
	AnthropicHost = "api.anthropic.com"
	OllamaHost    = "ollama.com"
)

// AnthropicBaseURL is the Anthropic format's default URL: Anthropic's own API.
const AnthropicBaseURL = "https://" + AnthropicHost + "/v1"

// Connection is the resolved, non-secret connection. The key travels beside
// it, never in it.
type Connection struct {
	Format Format
	// BaseURL is the API root the SDKs take, ending in the version segment,
	// e.g. https://api.anthropic.com/v1 or https://ollama.com/v1.
	BaseURL string
	// Host is BaseURL's host name; capabilities are keyed on it.
	Host       string
	Model      string
	AuthScheme AuthScheme
	// ContextWindow and OutputLimit are nil where the runtime knows the model
	// itself (Anthropic's own API) and today's defaults apply.
	ContextWindow *int
	OutputLimit   *int
	// ImageInput is what the probe learned about image input.
	ImageInput Tristate
}

// WebSearch is how a connection searches the web: a strategy, one
// implementation per value, chosen by host.
type WebSearch string

const (
	WebSearchAnthropicServerTool WebSearch = "anthropic-server-tool"
	WebSearchOllamaAPI           WebSearch = "ollama-api"
	WebSearchNone                WebSearch = "none"
)

// Capabilities is what a connection supports.
type Capabilities struct {
	// ClaudeCode: the coding agent may run on Claude Code (it speaks only the
	// Anthropic format).
	ClaudeCode bool
	// ClaudeSubscription: a Claude subscription token may bill coding runs; it
	// authenticates only against Anthropic's own API.
	ClaudeSubscription bool
	PromptCache        bool
	// GeneratedAgents: generated ai-agents may run on this connection. True on
	// every format: a generated agent reads the connection's format, URL and
	// model from its env, and Agent Manager fronts either format.
	GeneratedAgents bool
	NativePDF       bool
	WebSearch       WebSearch
	ImageInput      Tristate
	// SREAgent: the OpenChoreo SRE agent can call this connection. The stock
	// agent ships langchain-openai only and sends the key as a Bearer token.
	SREAgent bool
}

// CapabilitiesOf is the ONE statement of what a connection supports. A new
// format or host-bound feature is a branch here, not a check in a consumer.
func CapabilitiesOf(c Connection) Capabilities {
	anthropic := c.Format == FormatAnthropic
	firstParty := anthropic && c.Host == AnthropicHost
	caps := Capabilities{
		ClaudeCode:         anthropic,
		ClaudeSubscription: firstParty,
		PromptCache:        anthropic,
		GeneratedAgents:    true,
		NativePDF:          firstParty,
		WebSearch:          WebSearchNone,
		ImageInput:         c.ImageInput,
		SREAgent:           c.Format == FormatOpenAICompatible && c.AuthScheme == AuthBearer,
	}
	switch {
	case firstParty:
		caps.WebSearch, caps.ImageInput = WebSearchAnthropicServerTool, Yes
	case c.Host == OllamaHost:
		caps.WebSearch = WebSearchOllamaAPI
	}
	return caps
}

// FormatOption is one format the Settings card offers, with the values a first
// connect fills in when they are not sent. An empty DefaultBaseURL means the
// format has no default host and the URL is required.
type FormatOption struct {
	Format         Format
	DefaultBaseURL string
	DefaultModel   string
}

// DefaultAnthropicModel is the Anthropic format's default model, and the one
// the platform seeds a `model_rates` row for, so an org that connects with
// defaults has priced usage.
const DefaultAnthropicModel = "claude-sonnet-5"

// Formats is what the card offers; GET /config's llmFormats is built from it.
var Formats = []FormatOption{
	{Format: FormatAnthropic, DefaultBaseURL: AnthropicBaseURL, DefaultModel: DefaultAnthropicModel},
	{Format: FormatOpenAICompatible, DefaultModel: "glm-5.3"},
}

// FormatOptionOf returns f's entry in Formats; ok is false for a format the
// platform does not offer.
func FormatOptionOf(f Format) (FormatOption, bool) {
	for _, o := range Formats {
		if o.Format == f {
			return o, true
		}
	}
	return FormatOption{}, false
}
