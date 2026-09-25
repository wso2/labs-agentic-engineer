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

package issues

import (
	"context"
	"errors"
	"strings"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// Handler serves create-issue and list-issues.
//
// These back external handoffs: the SRE agent searches and files through MCP;
// CreateIssue owns classification and adoption using trusted transport context.
type Handler struct{ issues sourcecontrol.IssueService }

// New returns the slice's handler. issues may be nil, which degrades both ops to
// 503 — the component harness wires only what the feature under test needs.
func New(issues sourcecontrol.IssueService) *Handler { return &Handler{issues: issues} }

func (h *Handler) CreateIssue(ctx context.Context, request gen.CreateIssueRequestObject) (gen.CreateIssueResponseObject, error) {
	if h.issues == nil {
		return nil, apierr.ServiceUnavailable("issue service not configured")
	}
	org := tenant.BoundOrgFromContext(ctx)

	issue, err := h.issues.CreateIssue(ctx, org, request.ProjectName, sourcecontrol.CreateIssueRequest{
		Title:          request.Body.Title,
		Body:           request.Body.Body,
		Labels:         request.Body.Labels,
		DedupeKey:      request.Body.DedupeKey,
		ComponentName:  request.Body.ComponentName,
		ActionStatuses: request.Body.ActionStatuses,
	})
	if err != nil {
		if errors.Is(err, sourcecontrol.ErrIncidentContextRequired) {
			return nil, apierr.BadRequest(err.Error())
		}
		if errors.Is(err, sourcecontrol.ErrIncidentRecurrenceIneligible) {
			return nil, apierr.Conflict(err.Error())
		}
		if errors.Is(err, sourcecontrol.ErrRepoNotFound) {
			return nil, apierr.NotFound("project repo not found")
		}
		return nil, apierr.Internal("failed to create issue")
	}
	return gen.CreateIssue200JSONResponse(gen.IssueResult{
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

func (h *Handler) ListIssues(ctx context.Context, request gen.ListIssuesRequestObject) (gen.ListIssuesResponseObject, error) {
	if h.issues == nil {
		return nil, apierr.ServiceUnavailable("issue service not configured")
	}
	org := tenant.BoundOrgFromContext(ctx)

	issues, err := h.issues.ListIssues(ctx, org, request.ProjectName, splitLabels(request.Params.Labels))
	if err != nil {
		if errors.Is(err, sourcecontrol.ErrRepoNotFound) {
			return nil, apierr.NotFound("project repo not found")
		}
		return nil, apierr.Internal("failed to list issues")
	}

	ranked := sourcecontrol.RankIssuesByQuery(issues, request.Params.Q)
	out := make([]gen.IssueInfo, 0, len(ranked))
	for _, iss := range ranked {
		out = append(out, gen.IssueInfo{
			Number:          int64(iss.Number),
			Title:           iss.Title,
			Body:            iss.Body,
			URL:             iss.URL,
			State:           iss.State,
			StateReason:     iss.StateReason,
			Labels:          iss.Labels,
			AttentionReason: issueAttentionReason(iss.AttentionReason),
		})
	}
	return gen.ListIssues200JSONResponse(out), nil
}

// issueAttentionReason prevents a domain value outside the public contract's
// closed enum from reaching the console wire. The empty value omits the field.
func issueAttentionReason(value string) gen.IssueInfoAttentionReason {
	switch value {
	case string(gen.UnverifiedFix), string(gen.NoChangeVerdict), string(gen.Escalated):
		return gen.IssueInfoAttentionReason(value)
	default:
		return ""
	}
}

// splitLabels parses the comma-separated `labels` query param, dropping blanks.
func splitLabels(param string) []string {
	if param == "" {
		return nil
	}
	var labels []string
	for _, l := range strings.Split(param, ",") {
		if l = strings.TrimSpace(l); l != "" {
			labels = append(labels, l)
		}
	}
	return labels
}
