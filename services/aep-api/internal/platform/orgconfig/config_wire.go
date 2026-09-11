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
// all three sections. Secrets are never echoed (write-only), so this shape is
// intentionally distinct from ConfigPatch — which is what forces PATCH-not-PUT
// (a client can't round-trip a full document it can't read back).
//
// A null llm/gitProvider means "not connected" (replacing the legacy
// {status:"not_connected"} sentinel objects); idp is always present because an
// org always has at least the platform default.
//
// codingLlm is the one section whose null does NOT mean "not connected": the
// coding agent key is an OVERRIDE on llm, so null means the coding agent reuses
// llm's key (ADR-0016). It is still a required field so a client can tell
// "reuse" from "the server is too old to know about the section".
//
// codingAgent is never null at all: every org has an effective runtime and model
// whether or not anyone has chosen one, so the section carries the platform's
// defaults until someone does. Its UpdatedBy is what tells the two apart.
type ConfigProjection struct {
	LLM         *LLMProjection         `json:"llm"`         // null = not connected
	CodingLLM   *LLMProjection         `json:"codingLlm"`   // null = reuse LLM's key
	CodingAgent CodingAgentProjection  `json:"codingAgent"` // always present
	GitProvider *GitProviderProjection `json:"gitProvider"` // null = not connected
	IDP         IDPProjection          `json:"idp"`         // always present
}

// LLMProjection carries the org's LLM connection status. Fields are carried 1:1
// from orgcreds.AnthropicProjection minus ocOrgId (dropped from all
// projections — the org is implicit from the JWT).
type LLMProjection struct {
	Kind string `json:"kind" enum:"anthropic"`
	// CredentialKind distinguishes a Console API key from a Claude Code OAuth
	// token (`claude setup-token`), which bills a Claude subscription instead
	// of API credits. Only codingLlm can be an oauth_token; llm is always an
	// api_key, because the design agent is an AI SDK call that cannot present
	// a bearer token.
	CredentialKind  string     `json:"credentialKind" enum:"api_key,oauth_token"`
	KeyPrefix       string     `json:"keyPrefix"`
	KeyLast4        string     `json:"keyLast4"`
	Status          string     `json:"status"`
	ConnectedAt     time.Time  `json:"connectedAt"`
	LastValidatedAt *time.Time `json:"lastValidatedAt,omitempty"`
	ValidationError *string    `json:"validationError,omitempty"`
}

// --- codingAgent: the one section that is a plain setting -------------------
//
// Every other section here is credential-shaped: a write-only secret, an
// external probe before it is persisted, and a projection that cannot echo what
// was written. This one carries no secret and has nothing to probe, so its
// validation is entirely local — the values have to be members of the contract's
// enums, and the runtime has to be one this build can actually run.

// The platform's defaults, and the values an org gets until someone chooses
// otherwise. `DefaultCodingAgentModel` is deliberately a model the platform
// seeds a `model_rates` row for: cost stamping is all-or-nothing across a
// cycle's capture, so an unpriced default would blank the cost of every run made
// by every org that never opened the setting.
const (
	DefaultAgentRuntime     = "claude-code"
	DefaultCodingAgentModel = "claude-sonnet-5"
)

// AgentRuntimes are the runtime values the contract's AgentRuntime enum carries.
//
// MEMBERSHIP IS NOT AVAILABILITY. `opencode` is here because the design carries
// it and a client should be able to render the choice; whether this build can
// run one is `SupportedAgentRuntimes` below, and the two are deliberately
// different lists. Pinned against the committed contract by a test in this
// package, because a value that drifts out of the enum is rejected by the
// request validator long before any handler sees it.
var AgentRuntimes = []string{"claude-code", "opencode"}

// SupportedAgentRuntimes are the runtimes this build ships an adapter for.
//
// One, today. The runner's `runtime/registry.ts` refuses the other by name with
// a reason; this is the same refusal one layer earlier, so an organization is
// told at the moment it chooses rather than by a pod that fails to start three
// hours later.
var SupportedAgentRuntimes = []string{"claude-code"}

// CodingAgentModels are the models the contract's CodingAgentModel enum carries
// — narrower than the list any runtime can serve, and narrow for one reason: the
// platform only offers a model it can price. See the contract's own note.
var CodingAgentModels = []string{"claude-sonnet-5", "claude-haiku-4-5"}

// CodingAgentProjection is the runtime and model an organization's coding runs
// use, and the moment somebody chose them.
//
// UpdatedAt/UpdatedBy are nil exactly when nobody ever has — which is what tells
// "the platform's defaults" apart from "somebody chose the same values", a
// distinction the console needs and no other field carries.
type CodingAgentProjection struct {
	Runtime   string     `json:"runtime" enum:"claude-code,opencode"`
	Model     string     `json:"model" enum:"claude-sonnet-5,claude-haiku-4-5"`
	UpdatedAt *time.Time `json:"updatedAt"`
	UpdatedBy *string    `json:"updatedBy"`
}

// DefaultCodingAgent is the projection for an org that has never set one.
func DefaultCodingAgent() CodingAgentProjection {
	return CodingAgentProjection{Runtime: DefaultAgentRuntime, Model: DefaultCodingAgentModel}
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
// write-only fields can't be deep-merged into).
type ConfigPatch struct {
	LLM         patch.Field[LLMWrite]         `json:"llm,omitempty"`
	CodingLLM   patch.Field[LLMWrite]         `json:"codingLlm,omitempty"`
	CodingAgent patch.Field[CodingAgentWrite] `json:"codingAgent,omitempty"`
	GitProvider patch.Field[GitProviderWrite] `json:"gitProvider,omitempty"`
	IDP         patch.Field[IDPWrite]         `json:"idp,omitempty"`
}

// CodingAgentWrite is the codingAgent section's write shape, and the ONE section
// whose fields are individually optional.
//
// Every other section is replaced wholesale because it holds a write-only secret
// that cannot be deep-merged into. This one holds two plain values a client CAN
// read back, and the two change for different reasons — an org tunes its model
// far more often than it moves runtime — so an omitted field keeps what is
// there rather than resetting it. `null` on the section still resets both, which
// is the state "put me back on the platform's defaults" and is not the same as
// never having chosen (the projection's updatedBy is what tells them apart).
type CodingAgentWrite struct {
	Runtime string `json:"runtime,omitempty" enum:"claude-code,opencode"`
	Model   string `json:"model,omitempty" enum:"claude-sonnet-5,claude-haiku-4-5"`
}

// LLMWrite is the write shape of both LLM sections — llm (the org's default
// key) and codingLlm (the coding agent's override). The apiKey is write-only:
// probed against Anthropic, never echoed in any projection.
//
// codingLlm's three states read differently from every other section's, because
// the section models an override rather than a connection: absent = keep,
// null = REMOVE the override (the coding agent goes back to reusing the default
// key), value = set/rotate it. There is no "disconnected coding agent" state to
// clear into. See ADR-0016.
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
