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

package edge

import (
	"context"
	"errors"

	"github.com/wso2/aep/aep-api/internal/igen"
	"github.com/wso2/aep/aep-api/internal/ops"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol/issues"
)

// The SRE handoff route group (/internal/v1/sre/…): aep-mcp-server lists and
// files a project's issues for the SRE agent, and the handoff records its RCA
// report. internalGate admits only the SRE handoff bearer here and binds its
// one org plus the incident context; the org is always read from the context,
// never the request. The issue ops call the same issue service and error
// mapping as the console's list-issues/create-issue (sourcecontrol/issues).

func (s *internalServer) SreListIssues(ctx context.Context, request igen.SreListIssuesRequestObject) (igen.SreListIssuesResponseObject, error) {
	if s.deps.Issues == nil {
		return nil, errServiceUnavailable("issue service not configured")
	}
	org := tenant.BoundOrgFromContext(ctx)
	list, err := s.deps.Issues.ListIssues(ctx, org, request.ProjectName, issues.SplitLabels(request.Params.Labels))
	if err != nil {
		return nil, issues.ListError(err)
	}
	ranked := sourcecontrol.RankIssuesByQuery(list, request.Params.Q)
	out := make([]igen.IssueInfo, 0, len(ranked))
	for _, iss := range ranked {
		out = append(out, igen.IssueInfo{
			Number:          int64(iss.Number),
			Title:           iss.Title,
			Body:            iss.Body,
			URL:             iss.URL,
			State:           iss.State,
			StateReason:     iss.StateReason,
			Labels:          iss.Labels,
			AttentionReason: sreAttentionReason(iss.AttentionReason),
		})
	}
	return igen.SreListIssues200JSONResponse(out), nil
}

// sreAttentionReason keeps a domain value outside the contract's closed enum
// off the wire (sourcecontrol owns the set); the empty value omits the field.
func sreAttentionReason(value string) igen.IssueInfoAttentionReason {
	if !sourcecontrol.IsContractAttentionReason(value) {
		return ""
	}
	return igen.IssueInfoAttentionReason(value)
}

func (s *internalServer) SreCreateIssue(ctx context.Context, request igen.SreCreateIssueRequestObject) (igen.SreCreateIssueResponseObject, error) {
	if s.deps.Issues == nil {
		return nil, errServiceUnavailable("issue service not configured")
	}
	org := tenant.BoundOrgFromContext(ctx)
	issue, err := s.deps.Issues.CreateIssue(ctx, org, request.ProjectName, sourcecontrol.CreateIssueRequest{
		Title:          request.Body.Title,
		Body:           request.Body.Body,
		Labels:         request.Body.Labels,
		DedupeKey:      request.Body.DedupeKey,
		ComponentName:  request.Body.ComponentName,
		ActionStatuses: request.Body.ActionStatuses,
	})
	if err != nil {
		return nil, issues.CreateError(err)
	}
	return igen.SreCreateIssue200JSONResponse(igen.IssueResult{
		Number:          int64(issue.Number),
		URL:             issue.URL,
		NodeID:          issue.NodeID,
		Deduped:         issue.Deduped,
		Classification:  issue.Classification,
		Suppressed:      issue.Suppressed,
		Reopened:        issue.Reopened,
		Adopted:         issue.Adopted,
		AdoptionError:   issue.AdoptionError,
		RecurrenceCount: issue.RecurrenceCount,
	}), nil
}

func (s *internalServer) SreCreateRcaReport(ctx context.Context, request igen.SreCreateRcaReportRequestObject) (igen.SreCreateRcaReportResponseObject, error) {
	if s.deps.RcaReports == nil {
		return nil, errServiceUnavailable("rca-agent reports not configured")
	}
	org := tenant.BoundOrgFromContext(ctx)
	in := request.Body
	report, err := ops.NewReport(org, ops.NewReportInput{
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
	})
	if err != nil {
		if errors.Is(err, ops.ErrInvalidReport) {
			return nil, apierr.BadRequest(err.Error())
		}
		return nil, errInternal("failed to build rca-agent report")
	}
	if err := s.deps.RcaReports.Create(ctx, report); err != nil {
		return nil, errInternal("failed to create rca-agent report")
	}
	return igen.SreCreateRcaReport201JSONResponse(toIgenRcaReport(*report)), nil
}

// toIgenRcaReport projects the stored report onto the internal wire shape, as
// ops.ToWire does for the public one (igen is a leaf; OrgID never reaches the
// wire).
func toIgenRcaReport(r ops.RcaAgentReport) igen.RcaAgentReport {
	return igen.RcaAgentReport{
		ID:             r.ID,
		Project:        r.Project,
		Component:      r.Component,
		Title:          r.Title,
		Summary:        r.Summary,
		Classification: r.Classification,
		Diagnosis:      r.Diagnosis,
		IssueNumber:    r.IssueNumber,
		IssueURL:       r.IssueURL,
		IssueTitle:     r.IssueTitle,
		IssueExcerpt:   r.IssueExcerpt,
		Dispatched:     r.Dispatched,
		Deployed:       r.Deployed,
		DeployedAt:     r.DeployedAt,
		CreatedAt:      r.CreatedAt,
	}
}
