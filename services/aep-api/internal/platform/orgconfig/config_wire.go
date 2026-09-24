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
// This is a pure, gorm-free leaf (only platform/patch + stdlib), so both the
// generated wire layer (gen, a leaf) and the organization domain import it
// without a cycle — the home the types needed once models/ dissolved (§7).
// Kept field-for-field aligned with packages/contracts/api/v1; gen-api-check
// pins the rest of the contract.
package orgconfig

import (
	"time"

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
// agents is never null: every org has an effective model and runtime whether
// or not anyone has chosen them, so the section carries the platform's defaults
// until someone does. Its UpdatedBy is what tells the two apart.
type ConfigProjection struct {
	LLM *LLMProjection `json:"llm"` // null = not connected
	// LLMDisconnectedAt is when the org's API key was last disconnected; set
	// only while llm is null, so a client can tell "your key was disconnected"
	// from "no key was ever set".
	LLMDisconnectedAt *time.Time             `json:"llmDisconnectedAt,omitempty"`
	Agents            AgentsProjection       `json:"agents"`      // always present
	GitProvider       *GitProviderProjection `json:"gitProvider"` // null = not connected
	IDP               IDPProjection          `json:"idp"`         // always present
}

// LLMProjection carries the org's Anthropic API key status. Fields are carried
// 1:1 from organization.AnthropicProjection minus ocOrgId (dropped from all
// projections — the org is implicit from the JWT).
type LLMProjection struct {
	Kind string `json:"kind" enum:"anthropic"`
	// CredentialKind is always api_key: the spec agents are AI SDK calls that
	// cannot present a Claude subscription token, so the org's key is a
	// Console API key and a subscription lives on the agents section.
	CredentialKind  string     `json:"credentialKind" enum:"api_key"`
	KeyPrefix       string     `json:"keyPrefix"`
	KeyLast4        string     `json:"keyLast4"`
	Status          string     `json:"status"`
	ConnectedAt     time.Time  `json:"connectedAt"`
	LastValidatedAt *time.Time `json:"lastValidatedAt,omitempty"`
	ValidationError *string    `json:"validationError,omitempty"`
}

// --- agents: how the organization's agents run ------------------------------
//
// One model for every agent (the spec agents and the coding agent), the coding
// agent's runtime, and an optional Claude subscription the coding agent bills
// instead of the API key. The model and runtime are plain values validated
// against the contract's enums; the subscription is a write-only secret probed
// against Anthropic. The rule that ties them together (a subscription needs
// Claude Code and a connected API key) lives in the organization domain, which
// owns the credentials.

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

// The platform's defaults, and the values an org gets until someone chooses
// otherwise. The model is deliberately one the platform seeds a `model_rates`
// row for: cost stamping is all-or-nothing across a cycle's capture, so an
// unpriced default would blank the cost of every run made by every org that
// never opened the setting.
const (
	DefaultAgentRuntime = AgentRuntimeClaudeCode
	DefaultAgentModel   = "claude-sonnet-5"
)

// AgentModels are the models the contract's AgentModel enum carries — the set
// the platform can price. See the contract's own note.
var AgentModels = []string{"claude-sonnet-5", "claude-haiku-4-5"}

// AgentsProjection is how an organization's agents run, and the moment somebody
// chose it. The one model serves every agent and every call a coding run makes,
// on either runtime.
//
// UpdatedAt/UpdatedBy are nil exactly when nobody ever has — which is what tells
// "the platform's defaults" apart from "somebody chose the same values", a
// distinction the console needs and no other field carries.
type AgentsProjection struct {
	Model        string                  `json:"model" enum:"claude-sonnet-5,claude-haiku-4-5"`
	Runtime      AgentRuntime            `json:"runtime" enum:"claude-code,opencode"`
	Subscription *SubscriptionProjection `json:"subscription"` // null = coding bills the API key
	UpdatedAt    *time.Time              `json:"updatedAt"`
	UpdatedBy    *string                 `json:"updatedBy"`
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

// DefaultAgents is the projection for an org that has never set one.
func DefaultAgents() AgentsProjection {
	return AgentsProjection{
		Model:   DefaultAgentModel,
		Runtime: DefaultAgentRuntime,
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
// write-only fields can't be deep-merged into). agents is the one exception:
// its fields are individually optional, see AgentsWrite.
type ConfigPatch struct {
	LLM         patch.Field[LLMWrite]         `json:"llm,omitempty"`
	Agents      patch.Field[AgentsWrite]      `json:"agents,omitempty"`
	GitProvider patch.Field[GitProviderWrite] `json:"gitProvider,omitempty"`
	IDP         patch.Field[IDPWrite]         `json:"idp,omitempty"`
}

// AgentsWrite is the agents section's write shape, and the ONE section whose
// fields are individually optional: an omitted field keeps what is stored, so a
// client changes the model without restating the runtime and never has to send
// the stored token back.
//
// Subscription is itself three-state: absent keeps it, a value sets or
// replaces it, null deletes it. `null` on the whole section resets the model
// and runtime to the platform's defaults and deletes the subscription.
type AgentsWrite struct {
	Model        string                         `json:"model,omitempty" enum:"claude-sonnet-5,claude-haiku-4-5"`
	Runtime      AgentRuntime                   `json:"runtime,omitempty" enum:"claude-code,opencode"`
	Subscription patch.Field[SubscriptionWrite] `json:"subscription,omitempty"`
}

// SubscriptionWrite sets or replaces the Claude subscription token. The token
// is write-only: probed against Anthropic, never echoed.
type SubscriptionWrite struct {
	Kind  string `json:"kind" enum:"claude" required:"true"`
	Token string `json:"token" required:"true"`
}

// LLMWrite is the llm section's write shape: the org's Anthropic API key. The
// apiKey is write-only: probed against Anthropic, never echoed.
type LLMWrite struct {
	Kind   string `json:"kind" enum:"anthropic" required:"true"`
	APIKey string `json:"apiKey" required:"true"`
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
