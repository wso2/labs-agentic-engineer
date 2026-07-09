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

// RcaAgentReport is an RCA report from the OpenChoreo SRE/RCA-agent handoff
// (console issues #154, #155). Written once via create-rca-agent-report and
// read back by the console's notification bell and Alerts list/stepper —
// see packages/contracts/api/v1/openapi.yaml for the full field contract.
type RcaAgentReport struct {
	ID        string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	OrgID     string `gorm:"index:idx_rca_agent_reports_org_created,priority:1;not null" json:"-"`
	Project   string `gorm:"not null" json:"project"`
	Component string `json:"component,omitempty"`
	Title     string `gorm:"not null" json:"title"`
	Summary   string `gorm:"not null;type:text" json:"summary"`
	// Classification: code-level, config-level, mixed, or none.
	Classification string `gorm:"not null" json:"classification"`
	Diagnosis      string `gorm:"not null;type:text" json:"diagnosis"`

	IssueNumber  *int64 `json:"issueNumber,omitempty"`
	IssueURL     string `json:"issueUrl,omitempty"`
	IssueTitle   string `json:"issueTitle,omitempty"`
	IssueExcerpt string `gorm:"type:text" json:"issueExcerpt,omitempty"`

	// Dispatched: whether the coding agent has been dispatched for
	// IssueNumber (false in issue-only/manual-dispatch mode until a human
	// dispatches — console issue #155's "Coding Handover" stage).
	Dispatched bool `gorm:"not null;default:false" json:"dispatched"`
	// Deployed: whether the resulting fix has been deployed — the
	// "Verify Fix" threshold, not merely PR-merged.
	Deployed   bool       `gorm:"not null;default:false" json:"deployed"`
	DeployedAt *time.Time `json:"deployedAt,omitempty"`

	CreatedAt time.Time `gorm:"index:idx_rca_agent_reports_org_created,priority:2;not null;default:now()" json:"createdAt"`
}

// TableName pins the table name explicitly (matches GORM's default
// pluralization, kept stable per house convention).
func (RcaAgentReport) TableName() string { return "rca_agent_reports" }

// RcaAgentReportList is the paginated list envelope served by
// list-rca-agent-reports.
type RcaAgentReportList struct {
	Items []RcaAgentReport `json:"items"`
	// NextCursor is the opaque continuation token for the next page;
	// empty/absent on the last page.
	NextCursor string `json:"nextCursor,omitempty" doc:"Cursor for the next page; absent on the last page."`
}

// CreateRcaAgentReportRequest is the write-side payload for
// create-rca-agent-report. Server assigns ID and CreatedAt.
type CreateRcaAgentReportRequest struct {
	Project        string `json:"project"`
	Component      string `json:"component,omitempty"`
	Title          string `json:"title"`
	Summary        string `json:"summary"`
	Classification string `json:"classification"`
	Diagnosis      string `json:"diagnosis"`

	IssueNumber  *int64 `json:"issueNumber,omitempty"`
	IssueURL     string `json:"issueUrl,omitempty"`
	IssueTitle   string `json:"issueTitle,omitempty"`
	IssueExcerpt string `json:"issueExcerpt,omitempty"`

	Dispatched bool       `json:"dispatched,omitempty"`
	Deployed   bool       `json:"deployed,omitempty"`
	DeployedAt *time.Time `json:"deployedAt,omitempty"`
}
