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
	"errors"

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
