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
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// OrgAgentSettings is how an organization's agents run: the one model every
// agent uses and the coding agent's runtime. One row per org; ABSENT means the
// platform defaults, which is a state of its own — an org that chose the same
// values has a row, and its UpdatedBy is what the console reads to tell the two
// apart.
//
// The Claude subscription is not a column here: it is a credential, stored
// beside the org's API key as the `coding` row of org_anthropic_credentials.
type OrgAgentSettings struct {
	// OcOrgID is the OC-side org handle, as every other org-scoped table here
	// keys itself. Primary key: one setting per org, no history — the audit
	// trail is the `orgconfig.patched` log line, not a second table.
	OcOrgID string `gorm:"column:oc_org_id;primaryKey;type:text" json:"ocOrgId"`
	// Runtime is a member of the contract's AgentRuntime enum, validated on the
	// way in, so a value here is always one a dispatch can honour.
	Runtime orgconfig.AgentRuntime `gorm:"column:runtime;not null" json:"runtime"`
	// Model is a member of the contract's AgentModel enum, which is exactly the
	// set the platform holds a `model_rates` row for.
	Model string `gorm:"column:model;not null" json:"model"`
	// UpdatedBy is the actor from the JWT — the compensating control for the
	// coarse RBAC on /config, same as the section-level audit log.
	UpdatedBy string    `gorm:"column:updated_by;not null" json:"updatedBy"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null" json:"updatedAt"`
}

func (OrgAgentSettings) TableName() string { return "org_agent_settings" }
