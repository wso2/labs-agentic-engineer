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

package rcaagent

import (
	"context"
	"errors"
	"fmt"

	"github.com/wso2/aep/aep-api/models"
)

// ErrReportNotFound is returned by GetReport when no report exists for the
// given (org, id) — the huma handler maps it to a 404.
var ErrReportNotFound = errors.New("rca agent report not found")

// ErrInvalidReport is returned by CreateReport when the request fails
// validation — the huma handler maps it to a 400.
var ErrInvalidReport = errors.New("invalid rca agent report")

// defaultListLimit matches the console bell's own default (issue #154:
// "last 50, no pagination") when the caller omits limit.
const defaultListLimit = 50

// maxListLimit caps a single page regardless of what the caller asks for.
const maxListLimit = 200

var validClassifications = map[string]bool{
	"code-level":   true,
	"config-level": true,
	"mixed":        true,
	"none":         true,
}

// RcaAgentReportService is the read + write surface backing the console's
// Alerts notification bell and Alerts list/stepper (issues #154, #155, BE
// handshake #156).
type RcaAgentReportService interface {
	CreateReport(ctx context.Context, orgID string, in *models.CreateRcaAgentReportRequest) (*models.RcaAgentReport, error)
	GetReport(ctx context.Context, orgID, id string) (*models.RcaAgentReport, error)
	ListReports(ctx context.Context, orgID, cursor string, limit int) (*models.RcaAgentReportList, error)
}

type rcaAgentReportService struct {
	repo Repository
}

// NewRcaAgentReportService returns a service backed by repo.
func NewRcaAgentReportService(repo Repository) RcaAgentReportService {
	return &rcaAgentReportService{repo: repo}
}

// CreateReport validates and persists a new report. Fields the contract
// marks required are enforced here (not left to a DB NOT NULL 500) so the
// caller gets a precise 400.
func (s *rcaAgentReportService) CreateReport(ctx context.Context, orgID string, in *models.CreateRcaAgentReportRequest) (*models.RcaAgentReport, error) {
	if err := validateCreate(in); err != nil {
		return nil, err
	}
	report, err := s.repo.Create(ctx, orgID, in)
	if err != nil {
		return nil, fmt.Errorf("create report: %w", err)
	}
	return report, nil
}

func validateCreate(in *models.CreateRcaAgentReportRequest) error {
	if in == nil {
		return fmt.Errorf("%w: request body is required", ErrInvalidReport)
	}
	missing := []string{}
	if in.Project == "" {
		missing = append(missing, "project")
	}
	if in.Title == "" {
		missing = append(missing, "title")
	}
	if in.Summary == "" {
		missing = append(missing, "summary")
	}
	if in.Diagnosis == "" {
		missing = append(missing, "diagnosis")
	}
	if in.Classification == "" {
		missing = append(missing, "classification")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: missing required field(s): %v", ErrInvalidReport, missing)
	}
	if !validClassifications[in.Classification] {
		return fmt.Errorf("%w: classification %q must be one of code-level, config-level, mixed, none", ErrInvalidReport, in.Classification)
	}
	return nil
}

// GetReport returns a single report, or ErrReportNotFound when absent.
func (s *rcaAgentReportService) GetReport(ctx context.Context, orgID, id string) (*models.RcaAgentReport, error) {
	report, err := s.repo.Get(ctx, orgID, id)
	if err != nil {
		return nil, fmt.Errorf("get report %q: %w", id, err)
	}
	if report == nil {
		return nil, ErrReportNotFound
	}
	return report, nil
}

// ListReports returns a page of reports, newest first.
func (s *rcaAgentReportService) ListReports(ctx context.Context, orgID, cursor string, limit int) (*models.RcaAgentReportList, error) {
	switch {
	case limit <= 0:
		limit = defaultListLimit
	case limit > maxListLimit:
		limit = maxListLimit
	}
	reports, nextCursor, err := s.repo.List(ctx, orgID, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("list reports: %w", err)
	}
	return &models.RcaAgentReportList{Items: reports, NextCursor: nextCursor}, nil
}
