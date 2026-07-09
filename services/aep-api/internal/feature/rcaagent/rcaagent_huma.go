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
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/aep/aep-api/internal/platform/humakit"
	"github.com/wso2/aep/aep-api/models"
)

// --- Inputs / Outputs ------------------------------------------------------
// Inputs embed humakit.OrgScopedInput, whose Resolve binds the active org
// from the verified token (no {orgHandle} path param) and applies the tenant
// gate (the IDOR fence) by construction. Per BE handshake #156, the caller is
// any userJWT holder scoped to the org — including a widened-audience
// service-account token (see docs/developer-guide/sre-handoff-runbook.md
// §1.2 for the precedent) — there is no separate service-auth scheme.

type listReportsInput struct {
	humakit.OrgScopedInput
	Cursor string `query:"cursor" doc:"Opaque pagination cursor"`
	Limit  int    `query:"limit" doc:"Maximum number of items to return (server default when absent)"`
}

type getReportInput struct {
	humakit.OrgScopedInput
	ReportID string `path:"reportId" doc:"RCA report id"`
}

type createReportInput struct {
	humakit.OrgScopedInput
	Body models.CreateRcaAgentReportRequest
}

type reportListOutput struct{ Body *models.RcaAgentReportList }
type reportOutput struct{ Body *models.RcaAgentReport }

// RegisterRcaAgentReports registers the RCA-agent report feature's HTTP
// operations on the Huma API (issues #154, #155, BE handshake #156).
func RegisterRcaAgentReports(api huma.API, svc RcaAgentReportService) {
	const prefix = "/rca-agent/reports"

	huma.Register(api, huma.Operation{
		OperationID: "list-rca-agent-reports",
		Method:      http.MethodGet,
		Path:        prefix,
		Summary:     "List RCA-agent alert reports across the caller's org, newest first",
		Tags:        []string{"RcaAgentReports"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *listReportsInput) (*reportListOutput, error) {
		out, err := svc.ListReports(ctx, in.OrgHandle, in.Cursor, in.Limit)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to list rca-agent reports")
		}
		return &reportListOutput{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-rca-agent-report",
		Method:        http.MethodPost,
		Path:          prefix,
		Summary:       "Record a new RCA-agent alert report (internal/service-authenticated writer; issue #156)",
		Tags:          []string{"RcaAgentReports"},
		Security:      humakit.SecurityUserJWT,
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createReportInput) (*reportOutput, error) {
		out, err := svc.CreateReport(ctx, in.OrgHandle, &in.Body)
		if err != nil {
			if errors.Is(err, ErrInvalidReport) {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return nil, huma.Error500InternalServerError("failed to create rca-agent report")
		}
		return &reportOutput{Body: out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-rca-agent-report",
		Method:      http.MethodGet,
		Path:        prefix + "/{reportId}",
		Summary:     "Get a single RCA-agent alert report",
		Tags:        []string{"RcaAgentReports"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *getReportInput) (*reportOutput, error) {
		out, err := svc.GetReport(ctx, in.OrgHandle, in.ReportID)
		if err != nil {
			if errors.Is(err, ErrReportNotFound) {
				return nil, huma.Error404NotFound("rca-agent report not found")
			}
			return nil, huma.Error500InternalServerError("failed to get rca-agent report")
		}
		return &reportOutput{Body: out}, nil
	})
}
