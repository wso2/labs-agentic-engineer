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

// OrgCodingAgentSetting is the org's chosen coding-agent runtime and model.
//
// ONE ROW PER ORG, and its ABSENCE is the platform defaults — the same shape
// ADR-0016 chose for the coding-agent key, and for the same reason: a "using the
// defaults" flag can disagree with the values beside it, and then nothing knows
// which of the two is true. An org that has never opened the setting has no row;
// an org that resets the setting has its row deleted, which is why UpdatedBy is
// a reliable answer to "did anybody choose this".
//
// Both values are plain, non-secret strings, which makes this the only org-scope
// table here with nothing to encrypt and nothing to mirror into a secret store.
// They are stored as the contract's own enum values rather than as ids of our
// own, so the row a dispatcher reads is the string it stamps on the Workload.
type OrgCodingAgentSetting struct {
	// OcOrgID is the OC-side org handle, as every other org-scoped table here
	// keys itself. Primary key: one setting per org, no history — the audit
	// trail is the `orgconfig.patched` log line, not a second table.
	OcOrgID string `gorm:"column:oc_org_id;primaryKey;type:text" json:"ocOrgId"`
	// Runtime is a member of the contract's AgentRuntime enum. Validated on the
	// way in against BOTH the enum and what this build can actually run, so a
	// value here is always one a dispatch can honour.
	Runtime string `gorm:"column:runtime;not null" json:"runtime"`
	// Model is a member of the contract's CodingAgentModel enum, which is
	// exactly the set the platform holds a `model_rates` row for.
	Model string `gorm:"column:model;not null" json:"model"`
	// UpdatedBy is the actor from the JWT — the compensating control for the
	// coarse RBAC on /config, same as the section-level audit log.
	UpdatedBy string    `gorm:"column:updated_by;not null" json:"updatedBy"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null" json:"updatedAt"`
}

// TableName pins the GORM table name rather than trusting the pluraliser, the
// same way every other entity in this package does.
func (OrgCodingAgentSetting) TableName() string { return "org_coding_agent_settings" }
