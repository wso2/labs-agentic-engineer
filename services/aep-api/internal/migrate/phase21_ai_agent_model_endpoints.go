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

// RunPhase21AIAgentModelEndpoints creates ai_agent_model_endpoints: per (org,
// component, environment), the endpoint the Agent Manager govern stage last
// stored beside a governed ai-agent's key (organization.AIAgentModelEndpoint).
//
// It exists because the endpoint in that secret cannot be read back — the
// secret store is write-only — and the govern stage has to know when a model
// connection switch moved the base path under an agent whose key both sides
// already hold. Non-secret, so a plain table.
//
// No backfill, deliberately: nothing recorded the endpoints before this, and
// the secret cannot be read to fill it. The govern stage reads a missing row
// beside a stored key as "unknown" and rotates that agent's key once on its
// next deploy, which writes the row. Idempotent: CREATE ... IF NOT EXISTS.
func RunPhase21AIAgentModelEndpoints(ctx context.Context, db *gorm.DB) error {
	if err := db.WithContext(ctx).Exec(`
		CREATE TABLE IF NOT EXISTS ai_agent_model_endpoints (
		  oc_org_id   TEXT NOT NULL,
		  component   TEXT NOT NULL,
		  environment TEXT NOT NULL,
		  endpoint    TEXT NOT NULL,
		  updated_at  TIMESTAMPTZ NOT NULL,
		  PRIMARY KEY (oc_org_id, component, environment)
		)`).Error; err != nil {
		return fmt.Errorf("phase21 create ai_agent_model_endpoints: %w", err)
	}
	return nil
}
