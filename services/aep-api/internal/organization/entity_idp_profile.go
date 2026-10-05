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

import "time"

// OrganizationIDPProfile is the per-org IDP configuration row backing the
// per-org Thunder publisher client (docs/design/api-platform-integration.md).
// One row per OC organisation. Every row has Kind="platform" + Thunder
// issuer/jwks_url until the org brings its own IDP; PublisherClientID and
// PublisherThunderAppID are recorded by the gitpat submit's client ensure.
// No secret, and no reference to one, lives here: the publisher's and the
// studio client's credentials live only in vault, and the org's
// ae-publisher-client / ae-studio-client org_secrets rows record their
// references.
//
// The OrgID field is the OC-side org handle (not a UUID) — matches
// every other place the BFF identifies orgs.
type OrganizationIDPProfile struct {
	ID                  string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	OrgID               string `gorm:"column:org_id;not null;uniqueIndex:one_profile_per_org" json:"orgId"`
	Kind                string `gorm:"not null" json:"kind"` // platform | asgardeo | custom
	Issuer              string `gorm:"not null" json:"issuer"`
	JWKSURL             string `gorm:"column:jwks_url;not null" json:"jwksUrl"`
	AdminCredsSecretRef string `gorm:"column:admin_creds_secret_ref" json:"adminCredsSecretRef,omitempty"`
	PublisherClientID   string `gorm:"column:publisher_client_id" json:"publisherClientId,omitempty"`
	// PublisherThunderAppID is the Thunder entity id of aep-publisher-<org>,
	// recorded by the client ensure. Thunder has no lookup by clientId, so
	// every later read goes by this id and scans the app list only on a miss.
	PublisherThunderAppID string `gorm:"column:publisher_thunder_app_id" json:"-"`
	// StudioClientID and StudioThunderAppID are the AE Studio client
	// (ae-studio-<org>): its clientId and Thunder entity id. Its secret lives
	// only in the org's ae-studio-client SecretReference, never in a column.
	StudioClientID     string    `gorm:"column:studio_client_id" json:"-"`
	StudioThunderAppID string    `gorm:"column:studio_thunder_app_id" json:"-"`
	CreatedAt          time.Time `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt          time.Time `gorm:"column:updated_at" json:"updatedAt"`
}

// TableName pins the GORM table name (the auto-pluraliser would
// produce `organization_idp_profiles` already, but we make it explicit
// to survive any future model package reshuffles).
func (OrganizationIDPProfile) TableName() string { return "organization_idp_profiles" }

// IDPAuditEvent is one row in the append-only audit log of
// publisher-lifecycle operations. Used by the console "Audit" view
// and by incident response when investigating compromised org
// credentials.
type IDPAuditEvent struct {
	ID           int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	OrgID        string    `gorm:"column:org_id;not null;index:idx_idp_audit_events_org_occurred,priority:1" json:"orgId"`
	Action       string    `gorm:"not null" json:"action"` // ensure_publisher | revoke_publisher | update_profile (rows from before phase 6 may say regenerate_secret)
	Actor        string    `gorm:"not null" json:"actor"`  // user email / service principal
	OccurredAt   time.Time `gorm:"column:occurred_at;index:idx_idp_audit_events_org_occurred,priority:2,sort:desc" json:"occurredAt"`
	BeforeState  []byte    `gorm:"column:before_state;type:jsonb" json:"beforeState,omitempty"`
	AfterState   []byte    `gorm:"column:after_state;type:jsonb" json:"afterState,omitempty"`
	ErrorMessage string    `gorm:"column:error_message" json:"errorMessage,omitempty"`
}

func (IDPAuditEvent) TableName() string { return "idp_audit_events" }

// IDPAuditAction string constants. Centralised so the audit-log writers
// can't drift on spelling.
const (
	IDPAuditEnsurePublisher = "ensure_publisher"
	IDPAuditRevokePublisher = "revoke_publisher"
	IDPAuditUpdateProfile   = "update_profile"
)
