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

package models

import "time"

// OrgAnthropicCredential is the per-org Anthropic API key metadata row. The
// encrypted key bytes themselves live in `org_secrets(oc_org_id, key="anthropic/key")`
// alongside the GitHub PAT — same `dbStore` (Postgres + AES-256-GCM) plumbing,
// different `key` value. This table stores only non-secret projection fields.
//
// See docs/design/anthropic-key-dual-token.md §4.2.
type OrgAnthropicCredential struct {
	OcOrgID         string     `gorm:"primaryKey;type:text" json:"ocOrgId"`
	KeyPrefix       string     `gorm:"type:text;not null;column:key_prefix" json:"keyPrefix"`
	KeyLast4        string     `gorm:"type:text;not null;column:key_last4" json:"keyLast4"`
	Status          string     `gorm:"type:text;not null;default:active;column:status" json:"status"`
	ConnectedAt     time.Time  `gorm:"column:connected_at;not null;default:now()" json:"connectedAt"`
	LastValidatedAt *time.Time `gorm:"column:last_validated_at" json:"lastValidatedAt,omitempty"`
	ValidationError *string    `gorm:"type:text;column:validation_error" json:"validationError,omitempty"`

	// WS2.2 — SM-API triplet. Populated by Connect when SM-API is
	// configured; NULL on rows from the legacy org_secrets-only flow.
	// Dispatch (WS2.3) short-circuits the new path when NULL and falls
	// back to the legacy ClusterWorkflow until WS2.6 deletes it.
	SMAPISecretRefName *string `gorm:"type:text;column:sm_api_secret_ref_name" json:"-"`
	SMAPIKVPath        *string `gorm:"type:text;column:sm_api_kv_path" json:"-"`
	SMAPIProperty      *string `gorm:"type:text;column:sm_api_property" json:"-"`
}

func (OrgAnthropicCredential) TableName() string { return "org_anthropic_credentials" }
