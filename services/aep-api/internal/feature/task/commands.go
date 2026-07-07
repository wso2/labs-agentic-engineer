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
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
)

// Commands is the write surface for Task control labels. R4 removes reactive
// dispatch: Temporal owns task execution ordering, while these labels remain
// human/audit controls on the GitHub issue.
type Commands struct {
	issues IssueClient
	repos  RepoResolver
}

// NewCommands wires the command surface.
func NewCommands(issues IssueClient, repos RepoResolver) *Commands {
	return &Commands{issues: issues, repos: repos}
}

// Execute stamps aep:execute for audit/intent only. Temporal dispatches tasks
// from the DevelopmentFlowWorkflow; this command no longer starts work.
func (c *Commands) Execute(ctx context.Context, orgID, projectID string, issueNumber int) error {
	_, issueState, err := c.resolveTaskIssue(ctx, orgID, projectID, issueNumber)
	if err != nil {
		return err
	}
	if !strings.EqualFold(issueState, "open") {
		return ErrIssueClosed
	}
	// Stamp for the audit timeline (best-effort; Temporal owns dispatch).
	if err := c.issues.AddLabels(ctx, orgID, projectID, issueNumber, []string{taskmeta.LabelExecute}); err != nil {
		slog.WarnContext(ctx, "execute: stamp aep:execute failed", "issue", issueNumber, "error", err)
	}
	return nil
}

// Hold stamps aep:hold (level-triggered). Idempotent (204).
func (c *Commands) Hold(ctx context.Context, orgID, projectID string, issueNumber int) error {
	if _, _, err := c.resolveTaskIssue(ctx, orgID, projectID, issueNumber); err != nil {
		return err
	}
	return c.issues.AddLabels(ctx, orgID, projectID, issueNumber, []string{taskmeta.LabelHold})
}

// Unhold removes aep:hold. Temporal owns dependency ordering; no reactive
// re-evaluation is triggered.
func (c *Commands) Unhold(ctx context.Context, orgID, projectID string, issueNumber int) error {
	if _, _, err := c.resolveTaskIssue(ctx, orgID, projectID, issueNumber); err != nil {
		return err
	}
	if err := c.issues.RemoveLabel(ctx, orgID, projectID, issueNumber, taskmeta.LabelHold); err != nil {
		return err
	}
	return nil
}

// resolveTaskIssue resolves the repo full name and finds the Task issue by
// number, returning its GitHub state plus ErrProjectRepoNotFound / ErrTaskNotFound
// as appropriate.
func (c *Commands) resolveTaskIssue(ctx context.Context, orgID, projectID string, issueNumber int) (repoFullName, issueState string, err error) {
	repoFullName, err = resolveRepoFullName(ctx, c.repos, orgID, projectID)
	if err != nil {
		return "", "", err
	}

	issues, err := c.issues.ListIssues(ctx, orgID, projectID, []string{taskmeta.LabelMarker})
	if err != nil {
		return "", "", err
	}
	for i := range issues {
		if issues[i].Number == issueNumber {
			return repoFullName, issues[i].State, nil
		}
	}
	return "", "", ErrTaskNotFound
}
