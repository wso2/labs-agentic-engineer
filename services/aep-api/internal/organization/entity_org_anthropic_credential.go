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

// AnthropicRole names which reader an org's Anthropic credential serves. The
// table is keyed (OcOrgID, Role); the only role is the Claude subscription
// (CHECK org_anthropic_credentials_subscription_only).
type AnthropicRole string

// AnthropicRoleCoding is the optional Claude subscription the coding agent
// bills instead of the connection's key, read by coding-agent dispatch alone
// and only while the runtime is Claude Code on Anthropic's own API. Always an
// oauth_token (CHECK-enforced), and it cannot outlive the connection
// (ADR-0036).
const AnthropicRoleCoding AnthropicRole = "coding"

// String renders the role for SQL binding and log fields.
func (r AnthropicRole) String() string { return string(r) }

// AnthropicCredentialKind names HOW a stored credential authenticates, which
// decides how it is validated. It is persisted rather than re-derived, because
// readers of the metadata row never see the secret bytes — they have nothing to
// sniff.
type AnthropicCredentialKind string

const (
	// AnthropicCredentialAPIKey is a Console API key (`sk-ant-api…`),
	// authenticated with the `x-api-key` header. Never stored in this table; it
	// names what a subscription token is not.
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

// SecretStoreKey is the `org_secrets` key holding this role's encrypted bytes:
// "anthropic/coding-key". Distinct from the connection key's modelKeyStoreKey.
func (r AnthropicRole) SecretStoreKey() string {
	return "anthropic/" + string(r) + "-key"
}

// SecretRefEntity is the SM-API `EntityName` this role mirrors under. Distinct
// from the connection key's modelKeySecretEntity so the key and the
// subscription token never share a vault path.
func (r AnthropicRole) SecretRefEntity() string {
	return "anthropic-" + string(r)
}

// OrgAnthropicCredential is the per-org Claude subscription metadata row. The encrypted key bytes themselves live in
// `org_secrets(oc_org_id, key=Role.SecretStoreKey())` alongside the GitHub PAT
// — same `dbStore` (Postgres + AES-256-GCM) plumbing, different `key` value.
// This table stores only non-secret projection fields.
//
// See docs/decisions/ADR-0036-the-coding-credential-is-a-subscription.md.
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
