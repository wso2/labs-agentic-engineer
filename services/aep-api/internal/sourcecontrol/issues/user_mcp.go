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
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/mcprpc"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The Issues agent's MCP surface: the console's Issues view chats with an agent
// that can search and file issues on the ONE project the view was opened on.
// auth.IssuesMCPVerifier binds that org + project from the signed token before
// this handler runs; nothing the model sends can name another project, another
// org, or a label. Unknown arguments are ignored.

// userIssueKinds are the kinds the agent may file; each is also the label.
var userIssueKinds = []string{"bug", "feature", "improvement"}

// userReportLabel marks every agent-filed issue as user-reported.
const userReportLabel = "src/user"

// userIssueStates are search_issues' state filter values; "open" is the default.
var userIssueStates = []string{"open", "closed", "all"}

// userSearchBodyRunes caps each hit's body so a search stays a summary.
const userSearchBodyRunes = 500

// NewUserMCPHandler serves the Issues agent's search_issues and create_issue
// tools. A request without the verifier's scope answers 401; a nil issues
// service answers 503.
func NewUserMCPHandler(issues sourcecontrol.IssueService) http.Handler {
	server := mcprpc.Server{
		Name: "aep-issues", Version: "1.0.0", Tools: userTools(),
		Call: func(w http.ResponseWriter, r *http.Request, req mcprpc.Request) {
			callUserTool(w, r, issues, req)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.IssuesMCPScopeFromContext(r.Context()); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if issues == nil {
			http.Error(w, "issue service not configured", http.StatusServiceUnavailable)
			return
		}
		server.Serve(w, r)
	})
}

type userToolArgs struct {
	Query string `json:"query"`
	State string `json:"state"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Kind  string `json:"kind"`
}

// userIssueHit is one search_issues hit as the agent reads it.
type userIssueHit struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	State  string   `json:"state"`
	Labels []string `json:"labels"`
	URL    string   `json:"url"`
	Body   string   `json:"body"`
}

func callUserTool(w http.ResponseWriter, r *http.Request, issues sourcecontrol.IssueService, req mcprpc.Request) {
	scope, _ := auth.IssuesMCPScopeFromContext(r.Context())
	var call struct {
		Name      string       `json:"name"`
		Arguments userToolArgs `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &call); err != nil {
		mcprpc.WriteError(w, req.ID, mcprpc.CodeInvalidParams, "invalid params")
		return
	}
	args := call.Arguments
	ctx := tenant.WithBoundOrg(r.Context(), scope.OrgID)

	switch call.Name {
	case "search_issues":
		state := args.State
		if state == "" {
			state = "open"
		}
		if !slices.Contains(userIssueStates, state) {
			mcprpc.WriteToolError(w, req.ID, "state must be one of open, closed or all")
			return
		}
		found, err := issues.ListIssues(ctx, scope.OrgID, scope.ProjectID, nil)
		if err != nil {
			mcprpc.WriteToolError(w, req.ID, userIssueFailure(r, "search issues", err))
			return
		}
		inState := make([]sourcecontrol.IssueInfo, 0, len(found))
		for _, iss := range found {
			if state == "all" || strings.EqualFold(iss.State, state) {
				inState = append(inState, iss)
			}
		}
		ranked := sourcecontrol.RankIssuesByQuery(inState, args.Query)
		out := make([]userIssueHit, 0, len(ranked))
		for _, iss := range ranked {
			out = append(out, userIssueHit{
				Number: iss.Number, Title: iss.Title, State: iss.State,
				Labels: iss.Labels, URL: iss.URL, Body: truncateRunes(iss.Body, userSearchBodyRunes),
			})
		}
		mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(out))
	case "create_issue":
		if !slices.Contains(userIssueKinds, args.Kind) {
			mcprpc.WriteToolError(w, req.ID, "kind must be one of bug, feature or improvement")
			return
		}
		if strings.TrimSpace(args.Title) == "" || strings.TrimSpace(args.Body) == "" {
			mcprpc.WriteToolError(w, req.ID, "title and body are both required")
			return
		}
		issue, err := issues.CreateIssue(ctx, scope.OrgID, scope.ProjectID, sourcecontrol.CreateIssueRequest{
			Title:  args.Title,
			Body:   args.Body,
			Labels: []string{args.Kind, userReportLabel},
		})
		if err != nil {
			mcprpc.WriteToolError(w, req.ID, userIssueFailure(r, "file the issue", err))
			return
		}
		mcprpc.WriteToolText(w, req.ID, mcprpc.MustJSON(struct {
			Number int    `json:"number"`
			URL    string `json:"url"`
		}{issue.Number, issue.URL}))
	default:
		mcprpc.WriteToolError(w, req.ID, "unknown tool: "+call.Name)
	}
}

// userIssueFailure is the short tool error the agent reads; the cause is
// logged, never returned (it can carry host URLs).
func userIssueFailure(r *http.Request, action string, err error) string {
	if errors.Is(err, sourcecontrol.ErrRepoNotFound) {
		return "the project has no repository yet; nothing was done"
	}
	slog.ErrorContext(r.Context(), "issues agent: could not "+action, "error", err)
	return "could not " + action + " right now; nothing was done"
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
