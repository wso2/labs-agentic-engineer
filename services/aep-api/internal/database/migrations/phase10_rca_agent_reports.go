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

package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// RunPhase10RcaAgentReports creates rca_agent_reports (models.RcaAgentReport)
// — the store backing the console's Alerts notification bell and Alerts
// list/stepper (issues #154, #155, BE handshake #156). Raw SQL per the house
// migration rule; idempotent via the existence guard.
func RunPhase10RcaAgentReports(ctx context.Context, db *gorm.DB) error {
	if hasTable(db, "rca_agent_reports") {
		return nil
	}
	if err := db.WithContext(ctx).Exec(`
		CREATE TABLE rca_agent_reports (
			id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			org_id          TEXT NOT NULL,
			project         TEXT NOT NULL,
			component       TEXT,
			title           TEXT NOT NULL,
			summary         TEXT NOT NULL,
			classification  TEXT NOT NULL,
			diagnosis       TEXT NOT NULL,
			issue_number    BIGINT,
			issue_url       TEXT,
			issue_title     TEXT,
			issue_excerpt   TEXT,
			dispatched      BOOLEAN NOT NULL DEFAULT false,
			deployed        BOOLEAN NOT NULL DEFAULT false,
			deployed_at     TIMESTAMPTZ,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`).Error; err != nil {
		return fmt.Errorf("phase10_rca_agent_reports: create rca_agent_reports: %w", err)
	}
	if err := db.WithContext(ctx).Exec(`
		CREATE INDEX idx_rca_agent_reports_org_created
			ON rca_agent_reports (org_id, created_at DESC)
	`).Error; err != nil {
		return fmt.Errorf("phase10_rca_agent_reports: create idx_rca_agent_reports_org_created: %w", err)
	}
	return nil
}
