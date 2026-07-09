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

package repositories

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/models"
)

// ErrRcaAgentReportNotFound is returned when no report exists for the given
// (org, id).
var ErrRcaAgentReportNotFound = errors.New("rca agent report not found")

// RcaAgentReportRepository is the org-scoped store backing the console's
// Alerts notification bell and Alerts list/stepper (issues #154, #155).
type RcaAgentReportRepository struct {
	db *gorm.DB
}

// NewRcaAgentReportRepository returns a repository backed by db.
func NewRcaAgentReportRepository(db *gorm.DB) *RcaAgentReportRepository {
	return &RcaAgentReportRepository{db: db}
}

// Create inserts a new report for orgID, returning it with its
// server-assigned ID and CreatedAt populated.
func (r *RcaAgentReportRepository) Create(ctx context.Context, orgID string, in *models.CreateRcaAgentReportRequest) (*models.RcaAgentReport, error) {
	if orgID == "" {
		return nil, fmt.Errorf("rca_agent_reports: orgID is required")
	}
	report := &models.RcaAgentReport{
		OrgID:          orgID,
		Project:        in.Project,
		Component:      in.Component,
		Title:          in.Title,
		Summary:        in.Summary,
		Classification: in.Classification,
		Diagnosis:      in.Diagnosis,
		IssueNumber:    in.IssueNumber,
		IssueURL:       in.IssueURL,
		IssueTitle:     in.IssueTitle,
		IssueExcerpt:   in.IssueExcerpt,
		Dispatched:     in.Dispatched,
		Deployed:       in.Deployed,
		DeployedAt:     in.DeployedAt,
	}
	if err := r.db.WithContext(ctx).Create(report).Error; err != nil {
		return nil, fmt.Errorf("rca_agent_reports: create: %w", err)
	}
	return report, nil
}

// Get returns a single report by (org, id), or (nil, nil) when absent.
func (r *RcaAgentReportRepository) Get(ctx context.Context, orgID, id string) (*models.RcaAgentReport, error) {
	var report models.RcaAgentReport
	err := r.db.WithContext(ctx).Where("org_id = ? AND id = ?", orgID, id).First(&report).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("rca_agent_reports: get %q: %w", id, err)
	}
	return &report, nil
}

// List returns up to limit reports for orgID, newest first, optionally
// continuing after cursor (an opaque RFC3339Nano CreatedAt watermark from a
// previous page's NextCursor). Returns the page plus the cursor for the next
// page, or "" on the last page.
func (r *RcaAgentReportRepository) List(ctx context.Context, orgID string, cursor string, limit int) ([]models.RcaAgentReport, string, error) {
	if orgID == "" {
		return nil, "", fmt.Errorf("rca_agent_reports: orgID is required")
	}
	if limit <= 0 {
		limit = 50
	}
	q := r.db.WithContext(ctx).Where("org_id = ?", orgID)
	if cursor != "" {
		q = q.Where("created_at < ?", cursor)
	}
	var reports []models.RcaAgentReport
	// Fetch one extra row to detect whether a next page exists without a
	// separate COUNT query.
	if err := q.Order("created_at DESC").Limit(limit + 1).Find(&reports).Error; err != nil {
		return nil, "", fmt.Errorf("rca_agent_reports: list org %q: %w", orgID, err)
	}
	nextCursor := ""
	if len(reports) > limit {
		nextCursor = reports[limit-1].CreatedAt.Format(rfc3339NanoLayout)
		reports = reports[:limit]
	}
	return reports, nextCursor, nil
}

const rfc3339NanoLayout = "2006-01-02T15:04:05.999999999Z07:00"
