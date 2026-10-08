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

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/delivery/task"
	scissues "github.com/wso2/aep/aep-api/internal/sourcecontrol/issues"
)

// issueAgentPromoter is the issue agent's hand-off (scissues.Promoter) over the
// task surface's promote command — the same one the REST promote route runs.
// It translates the event plane's no-deployed-version refusal into the issues
// package's own sentinel, so sourcecontrol never imports delivery.
type issueAgentPromoter struct{ commands *task.Commands }

func (p issueAgentPromoter) PromoteAndExecute(ctx context.Context, orgID, projectID, componentName string, issueNumber int) error {
	err := p.commands.PromoteAndExecute(ctx, orgID, projectID, componentName, issueNumber)
	if errors.Is(err, delivery.ErrNoDeployedMilestone) {
		return scissues.ErrNoDeployedVersion
	}
	return err
}

// issueThreadsOnClose removes a closed issue's chat thread on GitHub's
// issues.closed webhook (round three §4) — whoever closed it: a user on
// GitHub, a merged pull request, the SRE agent or the platform. Unlike the
// event plane's issues handlers it has no echo filter: the removal is
// idempotent, so the platform's own close (the issue agent's close_issue
// already removed the thread) finds nothing left to do.
type issueThreadsOnClose struct {
	repos interface {
		ByFullName(ctx context.Context, fullName string) (orgID, projectID string, err error)
	}
	threads scissues.IssueThreadRemover
}

func (h issueThreadsOnClose) OnIssueClosed(ctx context.Context, _, _ string, payload []byte) error {
	var p struct {
		Issue struct {
			Number      int             `json:"number"`
			PullRequest json.RawMessage `json:"pull_request"`
		} `json:"issue"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil // malformed delivery — ack, nothing to do
	}
	// A pull request has no issue thread.
	if p.Issue.Number < 1 || p.Issue.PullRequest != nil || p.Repository.FullName == "" {
		return nil
	}
	orgID, projectID, err := h.repos.ByFullName(ctx, p.Repository.FullName)
	if err != nil {
		return fmt.Errorf("resolve the closed issue's project: %w", err)
	}
	if projectID == "" {
		return nil // not one of ours
	}
	if err := h.threads.RemoveIssueThread(ctx, orgID, projectID, p.Issue.Number); err != nil {
		return fmt.Errorf("remove the closed issue's thread: %w", err)
	}
	return nil
}
