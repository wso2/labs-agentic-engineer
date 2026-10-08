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

	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/mcprpc"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// An issue's agent: the tools of one issue's own chat. The issue is the one
// the token claims (auth.IssuesMCPScope.IssueNumber) — no tool takes a number.
// Whether a write may run is the agents service's confirmation gate, not this
// handler's: by the time a write lands here the user has confirmed it.

// IssueAgentPorts are what an issue's agent reaches beyond the issue service.
// Promoter and Components are wired in production; a nil one makes its tool a
// tool error. Threads is optional: nil removes nothing on close.
type IssueAgentPorts struct {
	Promoter   Promoter
	Components ComponentLister
	Threads    IssueThreadRemover
}

// Promoter hands an issue to the coding agent: it joins the deployed
// version's milestone and a run picks it up. It answers a HandOffRefusedError
// when the platform declines the issue for a reason the user can act on.
type Promoter interface {
	PromoteAndExecute(ctx context.Context, orgID, projectID, componentName string, issueNumber int) error
}

// HandOffRefusedError is the Promoter's refusal: no deployed version to hand
// the issue to, a closed issue, or one the coding agent does not take on. Its
// Reason is the sentence the user reads, the same the REST promote route
// answers with its 409.
type HandOffRefusedError struct{ Reason string }

func (e HandOffRefusedError) Error() string { return e.Reason }

// ComponentLister lists the project's design components by their real
// (design) names, sorted; empty when the project has no design.
type ComponentLister interface {
	ListComponents(ctx context.Context, orgID, projectID string) ([]string, error)
}

// IssueThreadRemover removes an issue's chat thread once the issue is closed:
// the threads created before the event that caused the removal (before;
// close_issue passes its close), so none started after that event goes.
// It is called from inside that thread's running turn, so it must not wait on
// that turn: it defers the removal to the turn's end. ctx is the tool call's
// request context, cancelled once the call answers — a deferred removal must
// not use it (detach with context.WithoutCancel or start a fresh, bounded
// one). It is idempotent.
type IssueThreadRemover interface {
	RemoveIssueThread(ctx context.Context, orgID, projectID string, issueNumber int, before time.Time) error
}

// issueCommentLimit is how many of the newest comments get_issue carries.
const issueCommentLimit = 10

type issueToolArgs struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	Reason    string `json:"reason"`
	Component string `json:"component"`
}

// issueView is get_issue's answer.
type issueView struct {
	Number   int                `json:"number"`
	Title    string             `json:"title"`
	Body     string             `json:"body"`
	Labels   []string           `json:"labels"`
	State    string             `json:"state"`
	URL      string             `json:"url"`
	Comments []issueCommentView `json:"comments"`
}

type issueCommentView struct {
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
	Machine   bool      `json:"machine"`
}

// issueCall is one issue tool call: the scope it is fenced to, with the org
// bound on its context.
type issueCall struct {
	w      http.ResponseWriter
	r      *http.Request
	ctx    context.Context
	id     json.RawMessage
	org    string
	proj   string
	number int
}

func (c issueCall) fail(text string) { mcprpc.WriteToolError(c.w, c.id, text) }

func (c issueCall) failed(action string, err error) {
	c.fail(userIssueFailure(c.r, action, err))
}

func (c issueCall) done(text string) { mcprpc.WriteToolText(c.w, c.id, text) }

func callIssueTool(w http.ResponseWriter, r *http.Request, issues sourcecontrol.IssueService, agent IssueAgentPorts, req mcprpc.Request) {
	scope, _ := auth.IssuesMCPScopeFromContext(r.Context())
	var call struct {
		Name      string        `json:"name"`
		Arguments issueToolArgs `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &call); err != nil {
		mcprpc.WriteError(w, req.ID, mcprpc.CodeInvalidParams, "invalid params")
		return
	}
	c := issueCall{
		w: w, r: r, ctx: tenant.WithBoundOrg(r.Context(), scope.OrgID), id: req.ID,
		org: scope.OrgID, proj: scope.ProjectID, number: scope.IssueNumber,
	}
	args := call.Arguments

	switch call.Name {
	case "get_issue":
		getIssue(c, issues)
	case "list_components":
		listComponents(c, agent.Components)
	case "comment_issue":
		if strings.TrimSpace(args.Body) == "" {
			c.fail("body is required")
			return
		}
		if err := issues.CommentIssue(c.ctx, c.org, c.proj, c.number, args.Body); err != nil {
			c.failed("post the comment", err)
			return
		}
		c.done(fmt.Sprintf("Posted the comment on #%d.", c.number))
	case "edit_issue":
		editIssue(c, issues, args)
	case "close_issue":
		closeIssue(c, issues, agent.Threads, args.Reason)
	case "reopen_issue":
		if err := issues.ReopenIssue(c.ctx, c.org, c.proj, c.number); err != nil {
			c.failed("reopen the issue", err)
			return
		}
		c.done(fmt.Sprintf("Reopened #%d.", c.number))
	case "hand_to_coding_agent":
		handToCodingAgent(c, agent, args.Component)
	default:
		c.fail("unknown tool: " + call.Name)
	}
}

func getIssue(c issueCall, issues sourcecontrol.IssueService) {
	issue, err := issues.GetIssue(c.ctx, c.org, c.proj, c.number)
	if err != nil {
		c.failed("read the issue", err)
		return
	}
	comments, err := issues.ListIssueComments(c.ctx, c.org, c.proj, c.number, issueCommentLimit)
	if err != nil {
		c.failed("read the issue's comments", err)
		return
	}
	view := issueView{
		Number: issue.Number, Title: issue.Title, Body: issue.Body, Labels: issue.Labels,
		State: issue.State, URL: issue.URL, Comments: make([]issueCommentView, 0, len(comments)),
	}
	if view.Labels == nil {
		view.Labels = []string{}
	}
	for _, cm := range comments {
		view.Comments = append(view.Comments, issueCommentView{
			Author: cm.Author, Body: cm.Body, CreatedAt: cm.CreatedAt, Machine: cm.Machine,
		})
	}
	c.done(mcprpc.MustJSON(view))
}

func listComponents(c issueCall, lister ComponentLister) {
	names, ok := components(c, lister)
	if !ok {
		return
	}
	c.done(mcprpc.MustJSON(struct {
		Components []string `json:"components"`
	}{names}))
}

// components reads the design's component names (never nil), writing the
// tool error itself when it cannot.
func components(c issueCall, lister ComponentLister) ([]string, bool) {
	if lister == nil {
		c.fail("the project's components cannot be read right now; nothing was done")
		return nil, false
	}
	names, err := lister.ListComponents(c.ctx, c.org, c.proj)
	if err != nil {
		c.failed("read the project's components", err)
		return nil, false
	}
	if names == nil {
		names = []string{}
	}
	return names, true
}

func editIssue(c issueCall, issues sourcecontrol.IssueService, args issueToolArgs) {
	title, body := strings.TrimSpace(args.Title), args.Body
	if title == "" && strings.TrimSpace(body) == "" {
		c.fail("give a new title, a new body, or both")
		return
	}
	if title != "" {
		if err := issues.EditIssueTitle(c.ctx, c.org, c.proj, c.number, title); err != nil {
			c.failed("edit the title", err)
			return
		}
	}
	if strings.TrimSpace(body) != "" {
		if err := issues.EditIssueBody(c.ctx, c.org, c.proj, c.number, body); err != nil {
			c.failed("edit the body", err)
			return
		}
	}
	c.done(fmt.Sprintf("Edited #%d.", c.number))
}

// closeIssue posts the reason as a comment, closes the issue, then removes
// its chat thread. The close is what the user confirmed, so a reason comment
// that could not be posted does not stop it — but the thread (the only other
// place the reason lived) goes too, so the answer says the comment was lost.
// A thread the remover could not reach is logged, never the agent's failure.
func closeIssue(c issueCall, issues sourcecontrol.IssueService, threads IssueThreadRemover, reason string) {
	if strings.TrimSpace(reason) == "" {
		c.fail("reason is required: it is posted as the closing comment")
		return
	}
	commentErr := issues.CommentIssue(c.ctx, c.org, c.proj, c.number, reason)
	if commentErr != nil {
		slog.WarnContext(c.ctx, "issue agent: could not post the closing reason",
			"org", c.org, "project", c.proj, "issue", c.number, "error", commentErr)
	}
	if err := issues.CloseIssue(c.ctx, c.org, c.proj, c.number, ""); err != nil {
		c.failed("close the issue", err)
		return
	}
	if threads != nil {
		if err := threads.RemoveIssueThread(c.ctx, c.org, c.proj, c.number, time.Now()); err != nil {
			slog.ErrorContext(c.ctx, "issue agent: could not remove the closed issue's thread",
				"org", c.org, "project", c.proj, "issue", c.number, "error", err)
		}
	}
	if commentErr != nil {
		c.done(fmt.Sprintf("Closed #%d; the reason comment could not be posted.", c.number))
		return
	}
	c.done(fmt.Sprintf("Closed #%d.", c.number))
}

// handToCodingAgent promotes the issue against one of the design's
// components, named as the design names it.
func handToCodingAgent(c issueCall, agent IssueAgentPorts, component string) {
	component = strings.TrimSpace(component)
	if component == "" {
		c.fail("component is required: one of list_components")
		return
	}
	names, ok := components(c, agent.Components)
	if !ok {
		return
	}
	if len(names) == 0 {
		c.fail("the project's design has no components yet; nothing was done")
		return
	}
	match := ""
	for _, name := range names {
		if strings.EqualFold(name, component) {
			match = name
			break
		}
	}
	if match == "" {
		c.fail(fmt.Sprintf("component must be one of: %s", strings.Join(names, ", ")))
		return
	}
	if agent.Promoter == nil {
		c.fail("the coding agent cannot be reached right now; nothing was done")
		return
	}
	if err := agent.Promoter.PromoteAndExecute(c.ctx, c.org, c.proj, match, c.number); err != nil {
		var refused HandOffRefusedError
		if errors.As(err, &refused) {
			c.fail(refused.Reason)
			return
		}
		c.failed("hand the issue to the coding agent", err)
		return
	}
	c.done(fmt.Sprintf("Handed #%d to the coding agent (component %s).", c.number, match))
}
