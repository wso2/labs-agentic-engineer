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

// Package orgconfig holds the org-config surface wire DTOs (GET/PATCH /config —
// the consolidated llm / gitProvider / idp document). HAND-WRITTEN because the
// generator cannot express their semantics: ConfigPatch's sections are
// three-state patch.Field values (absent = keep / null = clear / value =
// replace) and the projections use pointer sections for the wire's
// null-means-not-connected. The contract points `x-go-type: orgconfig.X` at
// these types, so `gen` emits a transparent alias (`type X = orgconfig.X`)
// instead of a wrong generated struct.
//
// This is a pure, gorm-free leaf (platform/patch, platform/modelconn + stdlib), so both the
// generated wire layer (gen, a leaf) and the organization domain import it
// without a cycle — the home the types needed once models/ dissolved (§7).
// Kept field-for-field aligned with packages/contracts/api/v1; gen-api-check
// pins the rest of the contract.
package orgconfig

import (
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/patch"
)

// --- Read side: ConfigProjection (no secret material) -----------------------

// ConfigProjection is the GET /config body: the org's connection state across
// all sections. Secrets are never echoed (write-only), so this shape is
// intentionally distinct from ConfigPatch — which is what forces PATCH-not-PUT
// (a client can't round-trip a full document it can't read back).
//
// A null llm/gitProvider means "not connected" (replacing the legacy
// {status:"not_connected"} sentinel objects); idp is always present because an
// org always has at least the platform default.
//
// agents is never null: every org has an effective runtime whether or not
// anyone has chosen it, so the section carries the platform's default until
// someone does. Its UpdatedBy is what tells the two apart.
type ConfigProjection struct {
	LLM *LLMProjection `json:"llm"` // null = not connected
	// LLMCheck is what the save's probe found; set only on the PATCH response
	// of a save that probed the connection, never on GET.
	LLMCheck *LLMCheck `json:"llmCheck,omitempty"`
	// LLMDisconnectedAt is when the org's model connection was last
	// disconnected; set only while llm is null, so a client can tell "your
	// connection was disconnected" from "none was ever saved".
	LLMDisconnectedAt *time.Time `json:"llmDisconnectedAt,omitempty"`
	// LLMFormats are the formats a connection may speak, never null.
	LLMFormats  []LLMFormatOption      `json:"llmFormats"`
	Agents      AgentsProjection       `json:"agents"`      // always present
	GitProvider *GitProviderProjection `json:"gitProvider"` // null = not connected
	IDP         IDPProjection          `json:"idp"`         // always present
	SreLLM      *SreLlmProjection      `json:"sreLlm"`      // null = no SRE model connection
	// SreAgent is the OpenChoreo SRE agent as the org's settings leave it:
	// which connection it runs on and how its rollout stands. nil when this
	// server does not push the SRE agent's configuration.
	SreAgent *SreAgentProjection `json:"sreAgent"`
}

// --- llm: the organization's model connection --------------------------------
//
// One connection for every agent: an API format, a base URL, a key and a
// model. host, authScheme, contextWindow, outputLimit and promptCache are kept
// off the wire on purpose: dispatch, the runner and the agents service consume
// them from modelconn.Connection, and the console renders none of them.

// LLMProjection is the org's saved model connection. The key is write-only and
// projected only as KeyPreview. A stored connection is usable by construction
// (a save is refused unless its probe passes), so there is no status.
type LLMProjection struct {
	Kind        modelconn.Format `json:"kind" enum:"anthropic,openai-compatible"`
	BaseURL     string           `json:"baseURL"`
	Model       string           `json:"model"`
	KeyPreview  string           `json:"keyPreview"`
	ConnectedAt time.Time        `json:"connectedAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
	// UpdatedBy is nil for a connection carried over by the migration from the
	// Anthropic-only card, which recorded no author for the key.
	UpdatedBy *string `json:"updatedBy"`
	// Priced is whether the platform holds a rate for (host, model), so usage
	// shows dollars rather than tokens only.
	Priced       bool            `json:"priced"`
	Capabilities LLMCapabilities `json:"capabilities"`
}

// LLMCapabilities is the part of modelconn.Capabilities the console reads.
// Computed once, server side (modelconn.CapabilitiesOf).
type LLMCapabilities struct {
	ClaudeSubscription bool                `json:"claudeSubscription"`
	WebSearch          modelconn.WebSearch `json:"webSearch" enum:"anthropic-server-tool,ollama-api,none"`
	ImageInput         modelconn.Tristate  `json:"imageInput" enum:"yes,no,unknown"`
	NativePDF          bool                `json:"nativePdf"`
	GeneratedAgents    bool                `json:"generatedAgents"`
	SREAgent           bool                `json:"sreAgent"`
}

// LLMCapabilitiesFrom projects a connection's capabilities onto the wire.
func LLMCapabilitiesFrom(c modelconn.Capabilities) LLMCapabilities {
	return LLMCapabilities{
		ClaudeSubscription: c.ClaudeSubscription,
		WebSearch:          c.WebSearch,
		ImageInput:         c.ImageInput,
		NativePDF:          c.NativePDF,
		GeneratedAgents:    c.GeneratedAgents,
		SREAgent:           c.SREAgent,
	}
}

// LLMCheck is what probing a connection found: the connection as it would be
// saved, whether the endpoint lists the model, and what it supports. The body
// of POST /config/llm/test, and ConfigProjection.LLMCheck on a probing save.
type LLMCheck struct {
	Kind    modelconn.Format `json:"kind" enum:"anthropic,openai-compatible"`
	BaseURL string           `json:"baseURL"`
	Model   string           `json:"model"`
	// ModelListed is reported, never stored: an unlisted model is a warning.
	ModelListed modelconn.Tristate `json:"modelListed" enum:"yes,no,unknown"`
	// Warning is LLMWarningProviderLimit when the endpoint answered with a
	// rate limit, which proves the key; empty otherwise.
	Warning      string          `json:"warning,omitempty" enum:"provider_limit"`
	Priced       bool            `json:"priced"`
	Capabilities LLMCapabilities `json:"capabilities"`
}

// LLMWarningProviderLimit is LLMCheck.Warning when the probe met a rate limit.
const LLMWarningProviderLimit = "provider_limit"

// LLMFormatOption is one format the Settings card offers (GET /config's
// llmFormats), built from modelconn.Formats. Runtimes are the runtimes on this
// installation that run it.
type LLMFormatOption struct {
	Kind modelconn.Format `json:"kind" enum:"anthropic,openai-compatible"`
	// DefaultBaseURL is nil when the format has no default host.
	DefaultBaseURL *string        `json:"defaultBaseURL"`
	DefaultModel   string         `json:"defaultModel"`
	Runtimes       []AgentRuntime `json:"runtimes"`
}

// --- agents: how the organization's agents run ------------------------------
//
// The coding agent's runtime and an optional Claude subscription the coding
// agent bills instead of the connection's key. The runtime is a plain value
// validated against the contract's enum; the subscription is a write-only
// secret probed against Anthropic. The rule that ties them to the connection
// (a subscription needs Claude Code and Anthropic's own API; Claude Code needs
// the Anthropic format) lives in the organization domain, which owns the
// credentials.

// AgentRuntime is a member of the contract's AgentRuntime enum: which
// coding-agent runtime an organization's cycles run on.
type AgentRuntime string

const (
	AgentRuntimeClaudeCode AgentRuntime = "claude-code"
	AgentRuntimeOpenCode   AgentRuntime = "opencode"
)

// AgentRuntimes are the runtime values the contract's AgentRuntime enum carries,
// the default first. Pinned against the committed contract by a test in this
// package, because a value that drifts out of the enum is rejected by the
// request validator long before any handler sees it.
var AgentRuntimes = []AgentRuntime{AgentRuntimeClaudeCode, AgentRuntimeOpenCode}

// DefaultAgentRuntime is the runtime an org gets until someone chooses: the
// one every installation runs.
const DefaultAgentRuntime = AgentRuntimeClaudeCode

// AgentsProjection is how an organization's agents run, and the moment somebody
// chose it. The model is part of the connection (LLMProjection.Model).
//
// UpdatedAt/UpdatedBy are nil exactly when nobody ever has — which is what tells
// "the platform's default" apart from "somebody chose the same value", a
// distinction the console needs and no other field carries.
type AgentsProjection struct {
	Runtime AgentRuntime `json:"runtime" enum:"claude-code,opencode"`
	// AvailableRuntimes are the runtimes this installation can run, the only
	// ones a save may choose. Runtime can name one missing here: an org that
	// chose it before the installation lost its runner image.
	AvailableRuntimes []AgentRuntime          `json:"availableRuntimes"`
	Subscription      *SubscriptionProjection `json:"subscription"` // null = coding bills the connection's key
	UpdatedAt         *time.Time              `json:"updatedAt"`
	UpdatedBy         *string                 `json:"updatedBy"`
}

// SubscriptionProjection is a stored Claude subscription token, masked.
type SubscriptionProjection struct {
	Kind            string     `json:"kind" enum:"claude"`
	KeyPrefix       string     `json:"keyPrefix"`
	KeyLast4        string     `json:"keyLast4"`
	Status          string     `json:"status"`
	ConnectedAt     time.Time  `json:"connectedAt"`
	LastValidatedAt *time.Time `json:"lastValidatedAt,omitempty"`
	ValidationError *string    `json:"validationError,omitempty"`
}

// SubscriptionKindClaude is the only subscription kind: a Claude plan, billed
// through a `claude setup-token` token.
const SubscriptionKindClaude = "claude"

// SreLlmProjection is the org's SRE model connection: the OpenAI-compatible
// endpoint only the OpenChoreo SRE agent calls, over a Bearer key of its own.
// The key is write-only and projected only as KeyPreview. Like LLMProjection
// a stored connection is usable by construction (a save is refused unless its
// probe passes), so there is no status.
type SreLlmProjection struct {
	BaseURL     string    `json:"baseURL"`
	Host        string    `json:"host"`
	Model       string    `json:"model"`
	KeyPreview  string    `json:"keyPreview"`
	ConnectedAt time.Time `json:"connectedAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	UpdatedBy   string    `json:"updatedBy"`
}

// SreAgentProjection is the OpenChoreo SRE agent as the org's settings leave
// it. Source says which connection it runs on: the SRE model connection
// (override), the org's model connection when that has the sreAgent
// capability (organization), or none. Model and Host are empty on none.
type SreAgentProjection struct {
	Enabled bool   `json:"enabled"`
	Source  string `json:"source" enum:"override,organization,none"`
	Model   string `json:"model"`
	Host    string `json:"host"`
	Status  string `json:"status" enum:"unconfigured,applying,running,failed"`
	// Reason says why the status is what it is; empty when there is nothing
	// to add.
	Reason string `json:"reason,omitempty"`
}

// DefaultAgents is the projection for an org that has never set one. It
// offers the default runtime only: that is the one every installation runs.
func DefaultAgents() AgentsProjection {
	return AgentsProjection{
		Runtime:           DefaultAgentRuntime,
		AvailableRuntimes: []AgentRuntime{DefaultAgentRuntime},
	}
}

// GitProviderProjection carries the org's git-provider connection status. The
// legacy credential kind (user-pat / app-installation) is re-expressed as
// kind=github + mode=pat|app so the provider is role-named, not vendor-pathed.
type GitProviderProjection struct {
	Kind              string     `json:"kind" enum:"github"`
	Mode              string     `json:"mode" enum:"app,pat"`
	GitHubLogin       string     `json:"githubLogin,omitempty"`
	IdentityLogin     string     `json:"identityLogin,omitempty"`
	IdentityName      string     `json:"identityName,omitempty"`
	IdentityEmail     string     `json:"identityEmail,omitempty"`
	InstallationID    *int64     `json:"installationId,omitempty"` // app-mode
	SelectedRepos     []string   `json:"selectedRepos,omitempty"`  // app-mode
	Status            string     `json:"status"`
	ConnectedAt       time.Time  `json:"connectedAt"`
	LastValidatedAt   *time.Time `json:"lastValidatedAt,omitempty"`
	IdentityChangedAt *time.Time `json:"identityChangedAt,omitempty"`
	PrevIdentityLogin *string    `json:"prevIdentityLogin,omitempty"`
}

// IDPProjection carries the org's IDP profile. Fields are carried 1:1 from the
// idp feature's profileSummaryFields; the live client secret is never echoed
// (hasClientSecret reflects whether one is stored).
type IDPProjection struct {
	Kind              string `json:"kind" enum:"platform,asgardeo,custom"`
	Issuer            string `json:"issuer"`
	JWKSURL           string `json:"jwksUrl"`
	PublisherClientID string `json:"publisherClientId"`
	HasClientSecret   bool   `json:"hasClientSecret"`
}

// --- Write side: ConfigPatch (omittable-nullable sections) ------------------

// ConfigPatch is the PATCH /config body. Each section is a three-state
// patch.Field: absent = keep, null = clear (where allowed), present = replace
// the section wholesale (deliberately not RFC 7386 deep-merge — a section with
// write-only fields can't be deep-merged into). llm and agents are the
// exceptions: their fields are individually optional, see LLMPatch and
// AgentsWrite.
type ConfigPatch struct {
	LLM         patch.Field[LLMPatch]         `json:"llm,omitempty"`
	Agents      patch.Field[AgentsWrite]      `json:"agents,omitempty"`
	GitProvider patch.Field[GitProviderWrite] `json:"gitProvider,omitempty"`
	IDP         patch.Field[IDPWrite]         `json:"idp,omitempty"`
	SreLLM      patch.Field[SreLlmWrite]      `json:"sreLlm,omitempty"`
}

// AgentsWrite is the agents section's write shape; its fields are individually
// optional: an omitted field keeps what is stored, so a client changes the
// runtime without re-sending the stored token.
//
// Subscription is itself three-state: absent keeps it, a value sets or
// replaces it, null deletes it. `null` on the whole section resets the runtime
// to the platform's default and deletes the subscription.
type AgentsWrite struct {
	Runtime      AgentRuntime                   `json:"runtime,omitempty" enum:"claude-code,opencode"`
	Subscription patch.Field[SubscriptionWrite] `json:"subscription,omitempty"`
}

// SubscriptionWrite sets or replaces the Claude subscription token. The token
// is write-only: probed against Anthropic, never echoed.
type SubscriptionWrite struct {
	Kind  string `json:"kind" enum:"claude" required:"true"`
	Token string `json:"token" required:"true"`
}

// LLMPatch is the llm section's write shape — and the body of POST
// /config/llm/test — patched field by field: an omitted field keeps the saved
// value; on first connect kind and apiKey are required and an omitted baseURL
// or model takes the format's default (modelconn.Formats).
// APIKey is write-only: probed, never echoed.
type LLMPatch struct {
	Kind    modelconn.Format `json:"kind,omitempty" enum:"anthropic,openai-compatible"`
	BaseURL string           `json:"baseURL,omitempty"`
	APIKey  string           `json:"apiKey,omitempty"`
	Model   string           `json:"model,omitempty"`
}

// SreLlmWrite is the sreLlm section's write shape, patched field by field like
// LLMPatch: an omitted field keeps the saved value. The first save needs all
// three; a save that moves the connection to another host needs APIKey too (a
// stored key is never sent to another host). Format and auth are fixed
// (OpenAI-compatible, Bearer). APIKey is write-only: probed, never echoed.
// null removes the connection, so the SRE agent falls back to the org's.
type SreLlmWrite struct {
	BaseURL *string `json:"baseURL,omitempty"`
	APIKey  *string `json:"apiKey,omitempty"`
	Model   *string `json:"model,omitempty"`
}

// GitProviderWrite is the gitProvider section's write shape. Mode is pat-only:
// App-mode is driven by the connect-sessions action route (OAuth), so it is
// schema-rejected here (the enum has no "app" value), pointing the client at
// the right flow.
type GitProviderWrite struct {
	Kind        string `json:"kind" enum:"github" required:"true"`
	Mode        string `json:"mode" enum:"pat" required:"true"`
	PAT         string `json:"pat" required:"true"` // write-only, probed, never echoed
	GitHubLogin string `json:"githubLogin,omitempty"`
}

// IDPWrite is the idp section's write shape. issuer/jwksUrl are optional: the
// section is replaced wholesale, so an omitted jwksUrl clears it (there is no
// legacy empty-string-means-keep carry-over — org-config-consolidation.md §4).
type IDPWrite struct {
	Kind    string `json:"kind" enum:"platform,asgardeo,custom" required:"true"`
	Issuer  string `json:"issuer,omitempty"`
	JWKSURL string `json:"jwksUrl,omitempty"`
}
