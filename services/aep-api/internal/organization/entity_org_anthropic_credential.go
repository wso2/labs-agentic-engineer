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

import (
	"strings"
	"time"
)

// AnthropicRole names which reader an org's Anthropic key serves. An org always
// has at most one row per role, so (OcOrgID, Role) is the table's primary key.
type AnthropicRole string

const (
	// AnthropicRoleDefault is the org's Anthropic key. EVERY reader uses it —
	// the design agent, the coding agent, the RCA agent — unless a more
	// specific key overrides it. An org without one cannot run any agent;
	// there is no platform-provided fallback.
	AnthropicRoleDefault AnthropicRole = "default"

	// AnthropicRoleCoding is the optional coding-agent key: an OVERRIDE on the
	// default, read by coding-agent dispatch and by nothing else. Its ABSENCE
	// is what "reuse the default key" means — no row, no flag (ADR-0016).
	AnthropicRoleCoding AnthropicRole = "coding"
)

// String renders the role for SQL binding and log fields.
func (r AnthropicRole) String() string { return string(r) }

// AnthropicCredentialKind names HOW a stored credential authenticates, which
// decides both how it is validated and which environment variable it must be
// delivered as. It is persisted rather than re-derived, because dispatch reads
// the metadata row and never the secret bytes — it has nothing to sniff.
type AnthropicCredentialKind string

const (
	// AnthropicCredentialAPIKey is a Console API key (`sk-ant-api…`),
	// authenticated with the `x-api-key` header. The only kind the design
	// agent can use: it is an AI SDK model call, which speaks API keys.
	AnthropicCredentialAPIKey AnthropicCredentialKind = "api_key"

	// AnthropicCredentialOAuth is a long-lived Claude Code OAuth token from
	// `claude setup-token`, authenticated with `Authorization: Bearer`. It
	// bills a Claude subscription (Pro/Max/Team/Enterprise) instead of API
	// credits, and only the coding agent can use one — it is a Claude Code
	// session, and Claude Code is what knows how to present the token.
	AnthropicCredentialOAuth AnthropicCredentialKind = "oauth_token"
)

// String renders the kind for SQL binding and log fields.
func (k AnthropicCredentialKind) String() string { return string(k) }

// oauthTokenPrefix is what `claude setup-token` mints. Anything else carrying
// the `sk-ant-` shape is treated as a Console API key.
const oauthTokenPrefix = "sk-ant-oat"

// AnthropicCredentialKindOf classifies a raw credential by its prefix. The two
// kinds are issued by different systems with non-overlapping prefixes, so the
// value itself is the most reliable discriminator available — asking a user to
// also declare the kind only creates a second source of truth that can
// disagree with the key they pasted.
func AnthropicCredentialKindOf(key string) AnthropicCredentialKind {
	if strings.HasPrefix(key, oauthTokenPrefix) {
		return AnthropicCredentialOAuth
	}
	return AnthropicCredentialAPIKey
}

// RunnerEnvVar is the environment variable a coding run must receive this
// credential as.
//
// The two are mutually exclusive by necessity, not by preference: Claude Code
// ranks `ANTHROPIC_API_KEY` ABOVE `CLAUDE_CODE_OAUTH_TOKEN`, so a container
// holding both would authenticate with the API key and silently ignore the
// token. Mounting exactly one is what makes the org's choice actually take
// effect. See docs/decisions/ADR-0016 and
// https://code.claude.com/docs/en/authentication#authentication-precedence.
func (k AnthropicCredentialKind) RunnerEnvVar() string {
	if k == AnthropicCredentialOAuth {
		return "CLAUDE_CODE_OAUTH_TOKEN"
	}
	return "ANTHROPIC_API_KEY"
}

// SecretStoreKey is the `org_secrets` key holding this role's encrypted bytes.
// The default role keeps the historical "anthropic/key" so no secret data has
// to move; only a new role introduces a new key.
func (r AnthropicRole) SecretStoreKey() string {
	if r == AnthropicRoleCoding {
		return "anthropic/coding-key"
	}
	return "anthropic/key"
}

// SecretRefEntity is the SM-API `EntityName` this role mirrors under. Distinct
// per role so the two keys can never land on the same vault path.
func (r AnthropicRole) SecretRefEntity() string {
	if r == AnthropicRoleCoding {
		return "anthropic-coding"
	}
	return "anthropic"
}

// OrgAnthropicCredential is the per-org, per-role Anthropic API key metadata
// row. The encrypted key bytes themselves live in
// `org_secrets(oc_org_id, key=Role.SecretStoreKey())` alongside the GitHub PAT
// — same `dbStore` (Postgres + AES-256-GCM) plumbing, different `key` value.
// This table stores only non-secret projection fields.
//
// See docs/decisions/ADR-0016-coding-agent-key-is-an-override-not-a-peer.md.
type OrgAnthropicCredential struct {
	OcOrgID         string                  `gorm:"primaryKey;type:text" json:"ocOrgId"`
	Role            AnthropicRole           `gorm:"primaryKey;type:text;not null;default:default" json:"role"`
	CredentialKind  AnthropicCredentialKind `gorm:"type:text;not null;default:api_key;column:credential_kind" json:"credentialKind"`
	KeyPrefix       string                  `gorm:"type:text;not null;column:key_prefix" json:"keyPrefix"`
	KeyLast4        string                  `gorm:"type:text;not null;column:key_last4" json:"keyLast4"`
	Status          string                  `gorm:"type:text;not null;default:active;column:status" json:"status"`
	ConnectedAt     time.Time               `gorm:"column:connected_at;not null;default:now()" json:"connectedAt"`
	LastValidatedAt *time.Time              `gorm:"column:last_validated_at" json:"lastValidatedAt,omitempty"`
	ValidationError *string                 `gorm:"type:text;column:validation_error" json:"validationError,omitempty"`

	// Secret-ref triplet. Populated by Connect when a secrets provider is
	// configured; NULL when unset. Dispatch short-circuits the refs path
	// when NULL.
	SecretRefName     *string `gorm:"type:text;column:secret_ref_name" json:"-"`
	SecretRefKVPath   *string `gorm:"type:text;column:secret_ref_kv_path" json:"-"`
	SecretRefProperty *string `gorm:"type:text;column:secret_ref_property" json:"-"`
}

func (OrgAnthropicCredential) TableName() string { return "org_anthropic_credentials" }
