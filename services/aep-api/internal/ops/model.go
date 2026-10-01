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

// Package ops is the Incident RCA domain: it captures RCA-agent incident
// reports and correlates them with live Task executions.
//
// It is a pure leaf — no other domain depends on it — and it reads the
// Execution store only through the ExecutionReader port (ports.go), never by
// touching another domain's tables.
//
// Layout (the house shape, §2):
//
//	model.go       the entity it owns
//	repository.go  the persistence port + its gorm impl — the ONLY file with gorm
//	ports.go       what it needs FROM other domains, in its OWN vocabulary
//	module.go      Deps — the typed ports the domain is constructed from
//	httpapi/       the aggregator + this domain's composition (see its doc.go)
//	<slice>/       one folder per use-case
package ops

import "time"

// RcaAgentReport is an RCA report from the OpenChoreo SRE/RCA-agent handoff
// (console issues #154, #155). Written once via sre-create-rca-report (the
// internal SRE handoff op, built by NewReport) and read
// back by the console's notification bell and Alerts list/stepper.
//
// This is the DOMAIN entity, not the wire type. gen.RcaAgentReport is generated
// from the contract and the slices map between the two. The two were one type
// before P1 (the contract carried `x-go-type: models.RcaAgentReport`), which is
// no longer possible: internal/gen must stay a leaf, so it cannot import a
// domain — see httpapi/doc.go.
//
// The split has a security dividend: OrgID exists here and NOT on the wire type,
// so the tenant key cannot be serialised into a response even by accident.
type RcaAgentReport struct {
	ID    string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	OrgID string `gorm:"index:idx_rca_agent_reports_org_created,priority:1;not null"`

	Project   string `gorm:"not null"`
	Component string
	Title     string `gorm:"not null"`
	Summary   string `gorm:"not null;type:text"`
	// Classification: code-level, config-level, mixed, or none.
	Classification string `gorm:"not null"`
	Diagnosis      string `gorm:"not null;type:text"`

	IssueNumber  *int64
	IssueURL     string
	IssueTitle   string
	IssueExcerpt string `gorm:"type:text"`

	// Dispatched: whether the coding agent has been dispatched for IssueNumber
	// (false in issue-only/manual-dispatch mode until a human dispatches —
	// console issue #155's "Coding Handover" stage).
	Dispatched bool `gorm:"not null;default:false"`
	// Deployed: whether the resulting fix has been deployed — the "Verify Fix"
	// threshold, not merely PR-merged.
	Deployed   bool `gorm:"not null;default:false"`
	DeployedAt *time.Time

	CreatedAt time.Time `gorm:"index:idx_rca_agent_reports_org_created,priority:2;not null;default:now()"`
}

// TableName pins the table name explicitly (matches GORM's default
// pluralization, kept stable per house convention).
func (RcaAgentReport) TableName() string { return "rca_agent_reports" }
