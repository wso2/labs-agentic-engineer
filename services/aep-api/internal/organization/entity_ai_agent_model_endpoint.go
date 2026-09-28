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

// AIAgentModelEndpoint is the endpoint the Agent Manager govern stage last
// stored beside one governed ai-agent's key, per environment: the gateway, the
// agent's own proxy path and the model connection's base path.
//
// A record of a write, not a source for the address; nothing in it is secret.
// Created by migrate's phase21_ai_agent_model_endpoints step.
type AIAgentModelEndpoint struct {
	OcOrgID     string    `gorm:"column:oc_org_id;primaryKey;type:text"`
	Component   string    `gorm:"column:component;primaryKey;type:text"`
	Environment string    `gorm:"column:environment;primaryKey;type:text"`
	Endpoint    string    `gorm:"column:endpoint;not null"`
	UpdatedAt   time.Time `gorm:"column:updated_at;not null"`
}

// TableName pins the table migrate created.
func (AIAgentModelEndpoint) TableName() string { return "ai_agent_model_endpoints" }
