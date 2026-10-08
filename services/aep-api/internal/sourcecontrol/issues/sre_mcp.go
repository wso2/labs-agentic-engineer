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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/mcprpc"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The OpenChoreo SRE agent's remediation handoff: an MCP surface with exactly
// two tools, search_related_issues and create_issue (the agent sees them as
// ae_search_related_issues and ae_create_issue, after its "ae" server name).
// They call the issue service in process. The caller is authenticated before
// this handler runs (auth.SREHandoffVerifier).
//
// One SRE agent serves every org on its observability plane, so each call
// names the org: the namespace from the alert it is handling. That argument
// comes from a model that also reads pod logs, so it is a claim, not an
// identity: before either tool reads or writes anything, the observer must
// have recorded an alert for that namespace and project (and, for a create,
// that component) within sreAlertWindow. Only then does the handler bind the
// namespace as the org, with the handoff's incident context, which CreateIssue
// requires before it accepts the componentName and actionStatuses only a
// trusted handoff may send.

// sreHandoffIncidentID is the incident context bound for every SRE-filed
// issue. CreateIssue derives the dedupe key from it with the org, project and
// component, so the handoff dedupes by component, not by a per-alert
// signature: there is no per-request signal this transport could bind that
// would mean anything finer.
const sreHandoffIncidentID = "sre-handoff"

// sreAlertWindow is how recent an alert must be for a tool call to act on
// it, counted back from the call. The agent calls the tools minutes after the
// alert that started its RCA, and a recurring incident brings a fresh alert
// each time, so an hour (the observer's own alert suppression window, the span
// it already treats as one incident) leaves ample room for a queued analysis
// while a long-gone incident authorizes nothing.
const sreAlertWindow = time.Hour

// AlertVerifier confirms against the observer's record that an alert fired
// for namespace, project and component (any component in the project when
// component is empty) since since. clients/observability.AlertQuerier is the
// production implementation.
type AlertVerifier interface {
	RecentAlert(ctx context.Context, namespace, project, component string, since time.Time) (bool, error)
}

// sreHandoffLabels are added to every SRE-filed issue: the handoff is by
// definition a defect report about a live incident.
var sreHandoffLabels = []string{"bug", "incident"}

// The platform's own plan issues. A search hit carrying them is AE's record of
// what to BUILD, which the remediation agent must not read as a defect report.
const (
	platformWorkLabel = "aep"
	platformPlanKind  = "development"
	platformIssueNote = "PLATFORM IMPLEMENTATION RECORD — this issue records AE's original plan for what " +
		"to BUILD. It is not a defect report and it is never grounds for ruling out a code " +
		"change: behaviour can be deliberate and still be worth hardening."
)

// kindsOutrankingPlan are issue kinds that make an aep-labelled issue a real
// report even when it also carries the development label.
var kindsOutrankingPlan = []string{"provision", "validation", "conflict", "bug"}

// NewSREMCPHandler serves the SRE handoff's MCP tools, verifying every call's
// namespace with alerts. issues or alerts nil answers every request 503: the
// tools cannot act, or cannot act safely.
func NewSREMCPHandler(issues sourcecontrol.IssueService, alerts AlertVerifier) http.Handler {
	server := mcprpc.Server{
		Name: "aep-sre-handoff", Version: "1.0.0", Tools: sreTools(),
		Call: func(w http.ResponseWriter, r *http.Request, req mcprpc.Request) {
			callSRETool(w, r, issues, alerts, req)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if issues == nil || alerts == nil {
			http.Error(w, "issue service not configured", http.StatusServiceUnavailable)
			return
		}
		server.Serve(w, r)
	})
}

type sreToolArgs struct {
	Namespace      string    `json:"namespace"`
	Project        string    `json:"project"`
	Query          string    `json:"query"`
	Labels         []string  `json:"labels"`
	Title          string    `json:"title"`
	Body           string    `json:"body"`
	ComponentName  string    `json:"componentName"`
	ActionStatuses []*string `json:"actionStatuses"`
}

func callSRETool(w http.ResponseWriter, r *http.Request, issues sourcecontrol.IssueService, alerts AlertVerifier, req mcprpc.Request) {
	var call struct {
		Name      string      `json:"name"`
		Arguments sreToolArgs `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &call); err != nil {
		mcprpc.WriteError(w, req.ID, mcprpc.CodeInvalidParams, "invalid params")
		return
	}
	args := call.Arguments
	slog.InfoContext(r.Context(), "sre handoff tool call", "namespace", args.Namespace, "tool", call.Name, "project", args.Project)
	if args.Namespace == "" || args.Project == "" {
		mcprpc.WriteToolError(w, req.ID, "missing required argument: namespace and project are both required")
		return
	}
	// search_related_issues needs a recent alert anywhere in the project;
	// create_issue one on the component it files about.
	component := ""
	if call.Name == "create_issue" {
		if args.ComponentName == "" {
			mcprpc.WriteToolError(w, req.ID, "missing required argument: componentName")
			return
		}
		component = args.ComponentName
	}
	if msg, ok := verifyIncident(r.Context(), alerts, args.Namespace, args.Project, component); !ok {
		mcprpc.WriteToolError(w, req.ID, msg)
		return
	}
	org := args.Namespace
	ctx := sourcecontrol.WithIncidentContext(tenant.WithBoundOrg(r.Context(), org), sreHandoffIncidentID)

	switch call.Name {
	case "search_related_issues":
		found, err := issues.ListIssues(ctx, org, args.Project, args.Labels)
		if err != nil {
			mcprpc.WriteToolError(w, req.ID, listIssuesFailure(err))
			return
		}
		ranked := sourcecontrol.RankIssuesByQuery(found, args.Query)
		out := make([]sreIssueView, 0, len(ranked))
		for _, iss := range ranked {
			out = append(out, toSREIssueView(issueInfoWire(iss)))
		}
		mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(out))
	case "create_issue":
		if args.ActionStatuses == nil {
			mcprpc.WriteToolError(w, req.ID, "missing required argument: actionStatuses")
			return
		}
		for _, status := range args.ActionStatuses {
			if status != nil && *status != "revised" && *status != "suggested" {
				mcprpc.WriteToolError(w, req.ID, fmt.Sprintf("actionStatuses: %q is not one of revised, suggested or null", *status))
				return
			}
		}
		issue, err := issues.CreateIssue(ctx, org, args.Project, sourcecontrol.CreateIssueRequest{
			Title:          args.Title,
			Body:           args.Body,
			Labels:         withSREHandoffLabels(args.Labels),
			ComponentName:  args.ComponentName,
			ActionStatuses: args.ActionStatuses,
		})
		if err != nil {
			mcprpc.WriteToolError(w, req.ID, createIssueFailure(err))
			return
		}
		mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(issueResultWire(issue)))
	default:
		mcprpc.WriteToolError(w, req.ID, "unknown tool: "+call.Name)
	}
}

// verifyIncident checks the call's namespace, project and component against
// the observer's recent alerts, answering the tool error to return when it
// does not hold. The agent may name a component as AE's design does
// ("service1") or as OpenChoreo does ("<project>-service1"); the observer
// knows the second, so the first is tried with the project prefix too. An
// observer that cannot answer fails the call closed.
func verifyIncident(ctx context.Context, alerts AlertVerifier, namespace, project, component string) (string, bool) {
	since := time.Now().Add(-sreAlertWindow)
	candidates := []string{component}
	if component != "" && !strings.HasPrefix(component, project+"-") {
		candidates = append(candidates, project+"-"+component)
	}
	for _, c := range candidates {
		ok, err := alerts.RecentAlert(ctx, namespace, project, c, since)
		if err != nil {
			slog.ErrorContext(ctx, "sre handoff: could not verify the incident", "namespace", namespace, "project", project, "error", err)
			return "aep-api 503: could not verify the incident with the observer; nothing was done", false
		}
		if ok {
			return "", true
		}
	}
	slog.WarnContext(ctx, "sre handoff: no recent alert backs the call", "namespace", namespace, "project", project, "component", component)
	return fmt.Sprintf("aep-api 403: no alert in the last %s for namespace %q, project %q%s; nothing was done",
		strings.TrimSuffix(strings.TrimSuffix(sreAlertWindow.String(), "0s"), "0m"), namespace, project, componentClause(component)), false
}

func componentClause(component string) string {
	if component == "" {
		return ""
	}
	return fmt.Sprintf(", component %q", component)
}

// withSREHandoffLabels appends the handoff labels the caller left out.
func withSREHandoffLabels(labels []string) []string {
	out := append([]string(nil), labels...)
	for _, label := range sreHandoffLabels {
		if !containsLabel(out, label) {
			out = append(out, label)
		}
	}
	return out
}

func containsLabel(labels []string, want string) bool {
	for _, label := range labels {
		if strings.EqualFold(strings.TrimSpace(label), want) {
			return true
		}
	}
	return false
}

// sreIssueView is one search hit as the agent reads it: the REST list shape,
// plus PlatformRecord and ReadAs on the platform's own plan issues.
type sreIssueView struct {
	gen.IssueInfo
	PlatformRecord bool   `json:"PlatformRecord,omitempty"`
	ReadAs         string `json:"ReadAs,omitempty"`
}

func toSREIssueView(info gen.IssueInfo) sreIssueView {
	view := sreIssueView{IssueInfo: info}
	if isPlatformPlan(info.Labels) {
		view.PlatformRecord = true
		view.ReadAs = platformIssueNote
	}
	return view
}

func isPlatformPlan(labels []string) bool {
	if !containsLabel(labels, platformWorkLabel) || !containsLabel(labels, platformPlanKind) {
		return false
	}
	for _, kind := range kindsOutrankingPlan {
		if containsLabel(labels, kind) {
			return false
		}
	}
	return true
}

// createIssueFailure is the tool error the agent reads for a failed create:
// the same status and message the REST create answers.
func createIssueFailure(err error) string {
	switch {
	case errors.Is(err, sourcecontrol.ErrIncidentContextRequired):
		return "aep-api 400: " + err.Error()
	case errors.Is(err, sourcecontrol.ErrIncidentRecurrenceIneligible):
		return "aep-api 409: " + err.Error()
	case errors.Is(err, sourcecontrol.ErrRepoNotFound):
		return "aep-api 404: project repo not found"
	default:
		slog.Error("sre handoff: create issue failed", "error", err)
		return "aep-api 500: failed to create issue"
	}
}

func listIssuesFailure(err error) string {
	if errors.Is(err, sourcecontrol.ErrRepoNotFound) {
		return "aep-api 404: project repo not found"
	}
	slog.Error("sre handoff: list issues failed", "error", err)
	return "aep-api 500: failed to list issues"
}
