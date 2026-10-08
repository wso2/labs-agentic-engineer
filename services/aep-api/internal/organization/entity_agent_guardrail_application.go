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
	"encoding/json"
	"time"
)

// AgentGuardrailApplication records what the Agent Manager govern stage did
// with one governed ai-agent's declared guardrails in one environment.
//
// AppliedNames are the policies AEP itself wrote to the agent's binding, and
// the only entries it may later replace or remove: every other policy on the
// binding is an operator's. Outcomes are what became of each declared
// guardrail ([{policy, status, reason}]), for the Deployments page.
//
// Keyed on the project too: a component name repeats across projects, and each
// is its own agent. Created by migrate's phase23_agent_guardrail_applications.
type AgentGuardrailApplication struct {
	OcOrgID      string          `gorm:"column:oc_org_id;primaryKey;type:text"`
	Project      string          `gorm:"column:project;primaryKey;type:text"`
	Component    string          `gorm:"column:component;primaryKey;type:text"`
	Environment  string          `gorm:"column:environment;primaryKey;type:text"`
	AppliedNames []string        `gorm:"column:applied_names;type:jsonb;serializer:json;not null"`
	Outcomes     json.RawMessage `gorm:"column:outcomes;type:jsonb;not null"`
	UpdatedAt    time.Time       `gorm:"column:updated_at;not null"`
}

// TableName pins the table migrate created.
func (AgentGuardrailApplication) TableName() string { return "agent_guardrail_applications" }
