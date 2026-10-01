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

package ops

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// This is the ONE file in the domain permitted to import gorm
// (TestGormFencedToDomainRepository). Slices use the Repository interface; a
// slice that reached for the ORM would have escaped the seam that lets it be
// tested without a database.

// ErrInvalidReport wraps a report NewReport rejects; the sre-create-rca-report
// op (internal/edge/internal_sre.go) maps it to a 400.
//
// There is deliberately NO ErrReportNotFound: Get reports absence as (nil, nil),
// and the getreport slice turns that into the 404. The pre-P1 code carried such
// a sentinel, and porting it would have shipped an exported error nothing
// returns and nothing checks — which the next domain would then copy.
var ErrInvalidReport = errors.New("invalid rca agent report")

// Repository is the org-scoped store backing the console's Alerts notification
// bell and Alerts list/stepper (issues #154, #155). Narrow enough to fake in a
// slice's unit tests without a database.
type Repository interface {
	// Create inserts report, populating its server-assigned ID and CreatedAt.
	Create(ctx context.Context, report *RcaAgentReport) error
	// Get returns one report by (org, id), or (nil, nil) when absent.
	Get(ctx context.Context, orgID, id string) (*RcaAgentReport, error)
	// List returns up to limit reports for orgID, newest first, continuing after
	// cursor; the second result is the cursor for the next page ("" when last).
	List(ctx context.Context, orgID, cursor string, limit int) ([]RcaAgentReport, string, error)
}

type repository struct{ db *gorm.DB }

// NewRepository returns the gorm-backed Repository.
func NewRepository(db *gorm.DB) Repository { return &repository{db: db} }

func (r *repository) Create(ctx context.Context, report *RcaAgentReport) error {
	if report == nil {
		return fmt.Errorf("rca_agent_reports: report is required")
	}
	if report.OrgID == "" {
		return fmt.Errorf("rca_agent_reports: orgID is required")
	}
	if err := r.db.WithContext(ctx).Create(report).Error; err != nil {
		return fmt.Errorf("rca_agent_reports: create: %w", err)
	}
	return nil
}

func (r *repository) Get(ctx context.Context, orgID, id string) (*RcaAgentReport, error) {
	var report RcaAgentReport
	err := r.db.WithContext(ctx).Where("org_id = ? AND id = ?", orgID, id).First(&report).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("rca_agent_reports: get %q: %w", id, err)
	}
	return &report, nil
}

// List paginates by keyset. The sort key is (created_at DESC, id DESC): id is a
// stable tie-breaker so rows sharing a created_at (possible with now() defaults
// or bulk inserts) are never skipped or duplicated across page boundaries. The
// cursor encodes both fields.
func (r *repository) List(ctx context.Context, orgID, cursor string, limit int) ([]RcaAgentReport, string, error) {
	if orgID == "" {
		return nil, "", fmt.Errorf("rca_agent_reports: orgID is required")
	}
	if limit <= 0 {
		limit = 50
	}
	q := r.db.WithContext(ctx).Where("org_id = ?", orgID)
	if cursor != "" {
		// A malformed cursor is treated as no cursor (serve the first page)
		// rather than erroring the whole request.
		if ts, id, ok := decodeReportCursor(cursor); ok {
			// Keyset pagination: everything strictly after (ts, id) in the
			// (created_at DESC, id DESC) order.
			q = q.Where("created_at < ? OR (created_at = ? AND id < ?)", ts, ts, id)
		}
	}
	var reports []RcaAgentReport
	// Fetch one extra row to detect whether a next page exists without a
	// separate COUNT query.
	if err := q.Order("created_at DESC, id DESC").Limit(limit + 1).Find(&reports).Error; err != nil {
		return nil, "", fmt.Errorf("rca_agent_reports: list org %q: %w", orgID, err)
	}
	nextCursor := ""
	if len(reports) > limit {
		last := reports[limit-1]
		nextCursor = encodeReportCursor(last.CreatedAt, last.ID)
		reports = reports[:limit]
	}
	return reports, nextCursor, nil
}

const rfc3339NanoLayout = "2006-01-02T15:04:05.999999999Z07:00"

// encodeReportCursor packs the (created_at, id) keyset watermark into one opaque
// base64 token.
func encodeReportCursor(createdAt time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(createdAt.Format(rfc3339NanoLayout) + "|" + id))
}

// decodeReportCursor reverses encodeReportCursor. ok is false for a malformed
// token (caller then serves the first page).
func decodeReportCursor(cursor string) (createdAt string, id string, ok bool) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", "", false
	}
	ts, id, found := strings.Cut(string(raw), "|")
	if !found || ts == "" || id == "" {
		return "", "", false
	}
	return ts, id, true
}
