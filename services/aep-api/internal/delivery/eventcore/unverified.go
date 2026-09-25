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

package eventcore

import (
	"context"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// keepUnverifiedIssuesOpen runs on a merged PR's pull_request.closed delivery,
// for both agent and human merges. Disarming before reopening keeps the issue
// out of the normal working set throughout the transition.
//
// It assumes GitHub's closing-keyword closure is already visible when this
// runs. GitHub does not order webhook deliveries, so a closure that lands
// after these writes would leave the issue closed as completed instead of
// open as an unverified fix; handling that ordering is not done here.
func (e *Events) keepUnverifiedIssuesOpen(ctx context.Context, orgID, projectID string, prNumber int, resolves []int) error {
	if e.p.Issues == nil || e.p.Writer == nil {
		return nil
	}
	for _, number := range resolves {
		issue, err := e.p.Issues.GetIssue(ctx, orgID, projectID, number)
		if err != nil {
			return err
		}
		if issue == nil || !sourcecontrol.HasIncidentLabel(issue.Labels) ||
			sourcecontrol.IsNoChangeVerdict(*issue) || sourcecontrol.IsUnverifiedFix(*issue) {
			continue
		}
		if err := e.p.Writer.Unlabel(ctx, orgID, projectID, number, delivery.LabelAgentWork); err != nil {
			return err
		}
		if err := e.p.Writer.Reopen(ctx, orgID, projectID, number); err != nil {
			return err
		}
		if err := e.p.Writer.Comment(ctx, orgID, projectID, number, fmt.Sprintf(
			"Pull request #%d merged without an explicit high-confidence declaration. This issue remains open as an unverified fix and is no longer assigned to the coding agent. Review the fix and close the issue if it resolves the incident.", prNumber)); err != nil {
			return err
		}
	}
	return nil
}
