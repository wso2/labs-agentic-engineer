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

package execution

import (
	"context"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
)

// TaskResolver reads GitHub task issues and projects them into TaskFacts. It is
// the non-dispatch replacement for the old Funnel's TaskFactsFor helper: R4
// keeps task fact resolution but removes imperative gate/re-evaluate dispatch.
type TaskResolver struct {
	issues IssueClient
	repos  RepoLookup
}

// NewTaskResolver wires a resolver. Both dependencies are required.
func NewTaskResolver(issues IssueClient, repos RepoLookup) *TaskResolver {
	return &TaskResolver{issues: issues, repos: repos}
}

// TaskFactsFor resolves one Task's live facts by repo full name + issue number
// (org/project resolved from the repo). ok is false when the issue is not a
// listable Task.
func (r *TaskResolver) TaskFactsFor(ctx context.Context, repoFullName string, issueNumber int) (TaskFacts, bool, error) {
	orgID, projectID, err := r.repos.ByFullName(ctx, repoFullName)
	if err != nil {
		return TaskFacts{}, false, fmt.Errorf("resolve repo %q: %w", repoFullName, err)
	}
	issues, err := r.issues.ListIssues(ctx, orgID, projectID, []string{taskmeta.LabelMarker})
	if err != nil {
		return TaskFacts{}, false, fmt.Errorf("list task issues: %w", err)
	}
	for _, issue := range issues {
		if issue.Number != issueNumber {
			continue
		}
		facts, _, ok := factsFromIssue(issue, orgID, projectID, repoFullName)
		return facts, ok, nil
	}
	return TaskFacts{}, false, nil
}
