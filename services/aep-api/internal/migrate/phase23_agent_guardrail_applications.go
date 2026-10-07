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

package migrate

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// RunPhase23AgentGuardrailApplications creates agent_guardrail_applications:
// per (org, project, component, environment), the guardrails the Agent Manager
// govern stage last wrote to a governed ai-agent's binding, and what became of
// each declared one (organization.AgentGuardrailApplication).
//
// The applied names are what lets AEP manage only its own entries on a
// binding an operator may also edit: Agent Manager's policy entries carry no
// owner, so nothing on the binding itself says which are AEP's.
//
// No backfill: nothing applied guardrails before this. Idempotent: CREATE ...
// IF NOT EXISTS.
func RunPhase23AgentGuardrailApplications(ctx context.Context, db *gorm.DB) error {
	if err := db.WithContext(ctx).Exec(`
		CREATE TABLE IF NOT EXISTS agent_guardrail_applications (
		  oc_org_id     TEXT NOT NULL,
		  project       TEXT NOT NULL,
		  component     TEXT NOT NULL,
		  environment   TEXT NOT NULL,
		  applied_names JSONB NOT NULL DEFAULT '[]',
		  outcomes      JSONB NOT NULL DEFAULT '[]',
		  updated_at    TIMESTAMPTZ NOT NULL,
		  PRIMARY KEY (oc_org_id, project, component, environment)
		)`).Error; err != nil {
		return fmt.Errorf("phase23 create agent_guardrail_applications: %w", err)
	}
	return nil
}
