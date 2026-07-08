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

package task

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
)

// RegisterFunc is the webhook-router registration seam (the same closure shape
// app.go passes so this package imports nothing from feature/webhook).
type RegisterFunc func(event, action string, h func(ctx context.Context, event, action string, payload []byte) error)

// CycleStarter is the minimal issue-fast-path port WebhookEvents needs
// (§R2.1) — satisfied by *cycle.Service. Kept as a consumer-side port so this
// package does not import feature/cycle, matching execution.TaskSignaler's
// pattern for the pull_request half.
type CycleStarter interface {
	OnIssueTaskOpened(ctx context.Context, orgHandle, projectName string, issueNumber int) error
}

// WebhookEvents holds the issues.* handlers — the GitHub-facing half of webhook
// handling (§9.2). It reacts to task birth (a labeled issue IS a Task, from
// anyone), command labels stamped by external actors, machine-block validation
// and repair, and close/reopen. The pull_request handlers are the platform half
// (feature/execution) — the §1 split is a package boundary.
//
// Echo suppression is a receiver invariant (§9.2): every platform write —
// status/attention labels, aep:execute consumption, block repair — fires an
// issues.* delivery right back. All handlers drop deliveries whose sender is the
// platform's own identity, and block repair writes only when the canonical
// re-serialization differs, so repair converges in one step (no edit ping-pong).
type WebhookEvents struct {
	issues         IssueClient
	repos          RepoLocator
	platformSender string
	cycle          CycleStarter // nil disables the issue fast-path (§R2.1)
}

// NewWebhookEvents wires the issues.* handlers. platformSender is the platform's
// GitHub identity (App bot login, e.g. "aep-platform[bot]"); empty disables echo
// suppression (dev without an App).
func NewWebhookEvents(issues IssueClient, repos RepoLocator, platformSender string) *WebhookEvents {
	return &WebhookEvents{issues: issues, repos: repos, platformSender: platformSender}
}

// WithCycle enables the GitHub-issue fast-path (§R2.1): a newly opened Task
// issue on an issue-first project bootstraps a development cycle straight to
// implement. Optional — nil-safe, mirrors execution.Events.WithTaskSignaler.
func (e *WebhookEvents) WithCycle(cycle CycleStarter) *WebhookEvents {
	e.cycle = cycle
	return e
}

// RegisterHandlers installs the issues.* handlers on the webhook router.
func (e *WebhookEvents) RegisterHandlers(register RegisterFunc) {
	register("issues", "opened", e.OnOpenedOrEdited)
	register("issues", "edited", e.OnOpenedOrEdited)
	register("issues", "labeled", e.OnLabeled)
	register("issues", "unlabeled", e.OnUnlabeled)
	register("issues", "closed", e.noop)   // "let it finish" — no write (§4)
	register("issues", "reopened", e.noop) // dispatch re-enables on next execute
}

type issuesPayload struct {
	Action string `json:"action"`
	Issue  struct {
		Number int    `json:"number"`
		State  string `json:"state"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	} `json:"issue"`
	Label struct {
		Name string `json:"name"`
	} `json:"label"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
}

func (p issuesPayload) labelNames() []string {
	out := make([]string, 0, len(p.Issue.Labels))
	for _, l := range p.Issue.Labels {
		out = append(out, l.Name)
	}
	return out
}

func (e *WebhookEvents) isEcho(sender string) bool {
	return e.platformSender != "" && strings.EqualFold(sender, e.platformSender)
}

func (e *WebhookEvents) noop(context.Context, string, string, []byte) error { return nil }

// OnOpenedOrEdited validates and repairs the machine block (§2). Repair writes
// the canonical body only when it differs from the current one (single-step
// convergence); a mangled or missing block on a Task flags aep:attention.
func (e *WebhookEvents) OnOpenedOrEdited(ctx context.Context, _, _ string, payload []byte) error {
	var p issuesPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil
	}
	if e.isEcho(p.Sender.Login) || p.Repository.FullName == "" {
		return nil
	}
	labels := taskmeta.ParseLabels(p.labelNames())
	if !labels.IsTask {
		return nil // not a Task — inert
	}
	orgID, projectID, err := e.repos.ByFullName(ctx, p.Repository.FullName)
	if err != nil {
		return err
	}

	// Issue fast-path (§R2.1): only on a genuine new-issue delivery, never on
	// an edit of an existing one (this handler serves both actions), and
	// BEFORE the machine-block repair below — a human filing a bare Task issue
	// with no block yet must still bootstrap the cycle; the block-repair /
	// attention-flagging paths below are independent and both still run.
	if p.Action == "opened" && e.cycle != nil {
		if cerr := e.cycle.OnIssueTaskOpened(ctx, orgID, projectID, p.Issue.Number); cerr != nil {
			slog.WarnContext(ctx, "issue fast-path cycle start failed", "issue", p.Issue.Number, "error", cerr)
		}
	}

	repaired, changed, err := taskmeta.Repair(p.Issue.Body)
	switch {
	case errors.Is(err, taskmeta.ErrNoBlock):
		return e.flagAttention(ctx, orgID, projectID, p.Issue.Number,
			"This Task has no machine block (`<!-- aep:task/v1 ... -->`). Add one or close the issue.")
	case errors.Is(err, taskmeta.ErrMangledBlock):
		return e.flagAttention(ctx, orgID, projectID, p.Issue.Number,
			"This Task's machine block is corrupt and could not be repaired automatically. Fix it by hand.")
	case err != nil:
		return err
	}
	if changed {
		if werr := e.issues.EditIssueBody(ctx, orgID, projectID, p.Issue.Number, repaired); werr != nil {
			slog.WarnContext(ctx, "block repair write failed", "issue", p.Issue.Number, "error", werr)
		}
	}
	return nil
}

// OnLabeled keeps legacy command-label deliveries inert. Temporal owns task
// dispatch; aep:execute labels are audit/intent only in R4.
func (e *WebhookEvents) OnLabeled(ctx context.Context, _, _ string, payload []byte) error {
	var p issuesPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil
	}
	if e.isEcho(p.Sender.Login) || p.Repository.FullName == "" {
		return nil
	}
	if p.Label.Name != taskmeta.LabelExecute {
		return nil
	}
	return nil
}

// OnUnlabeled is inert for hold release in R4; Temporal owns task ordering.
func (e *WebhookEvents) OnUnlabeled(ctx context.Context, _, _ string, payload []byte) error {
	var p issuesPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil
	}
	if e.isEcho(p.Sender.Login) {
		return nil
	}
	if p.Label.Name != taskmeta.LabelHold {
		return nil
	}
	return nil
}

func (e *WebhookEvents) flagAttention(ctx context.Context, orgID, projectID string, number int, msg string) error {
	flagAttention(ctx, e.issues, orgID, projectID, number, msg)
	return nil
}

// flagAttention stamps aep:attention and posts a human-facing comment
// (best-effort — failures are logged, never propagated). Shared by the
// issues.* handlers and the plan tap's write-failure surfacing.
func flagAttention(ctx context.Context, issues IssueClient, orgID, projectID string, number int, msg string) {
	if err := issues.AddLabels(ctx, orgID, projectID, number, []string{taskmeta.LabelAttention}); err != nil {
		slog.WarnContext(ctx, "flag attention failed", "issue", number, "error", err)
	}
	if err := issues.CommentIssue(ctx, orgID, projectID, number, "⚠️ "+msg); err != nil {
		slog.WarnContext(ctx, "attention comment failed", "issue", number, "error", err)
	}
}
