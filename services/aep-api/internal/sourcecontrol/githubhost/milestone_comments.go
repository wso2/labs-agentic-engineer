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

package githubhost

// The version ledger's comment read — a milestone's issue comments in ONE round
// trip, which is the only reason it is GraphQL (graphql.go states the rule: this
// transport exists only where REST cannot answer a question in one call).
//
// REST offers two shapes and neither works here. Per-issue
// (`/issues/{n}/comments`) is a call per issue, and this read rides a 5s console
// poll — a twelve-issue milestone would spend ~8,600 requests an hour against a
// 5,000/hour budget. Repo-wide (`/issues/comments`) is one call but answers the
// WHOLE repository, pull-request comments included, to be intersected with the
// milestone afterwards, and it spends the REST budget the run loop's own reads
// need. The GraphQL points budget is separate and this query costs ~1 of it.
//
// `milestone.issues` is a pure-issue connection — pull requests hang off
// `milestone.pullRequests` — so PR comments are excluded by construction rather
// than by a filter that could be forgotten.

import (
	"context"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// milestoneIssuePage is how many of a milestone's issues one query covers — one
// page, and only one.
//
// The number is GitHub's own per-connection ceiling (`first:` may not exceed
// 100), not a choice. The single page IS a choice, and it does NOT match the
// coverage of the REST sibling: ListMilestoneIssues walks pages until a short
// one, so it returns every issue in the milestone, while this returns comments
// for the first hundred. On a milestone larger than that, the issues past the
// page get no comments.
//
// That is deliberate for a decorative read whose cost lands on a 5s poll — a
// second page would double it for every tick — but it must never look like
// completeness, which is what the hasNextPage warning below is for.
const milestoneIssuePage = 100

// milestoneIssueCommentsQuery reads the newest `$c` comments of every issue in a
// milestone. What a comment's fields are, and why the tail is the end worth
// asking for, live with commentSelection in comments.go.
const milestoneIssueCommentsQuery = `query($owner: String!, $repo: String!, $m: Int!, $i: Int!, $c: Int!) {
  repository(owner: $owner, name: $repo) {
    milestone(number: $m) {
      issues(first: $i) {
        pageInfo { hasNextPage }
        nodes {
          number
          comments(last: $c) { ` + commentSelection + ` }
        }
      }
    }
  }
}`

// ListMilestoneIssueComments returns the newest perIssue comments of each issue
// in one milestone, bucketed by issue number, oldest first within a bucket.
//
// Issues with no comments are absent from the map — a caller indexing it gets
// nil, which is the same thing an empty slice would have meant and costs no
// allocation per silent issue.
//
// perIssue <= 0 answers an empty map without a round trip: asking for no
// comments is a coherent request and must not become a query error.
func (c *Client) ListMilestoneIssueComments(ctx context.Context, owner, repo string, cred secrets.Credential, number, perIssue int) (map[int][]sourcecontrol.IssueComment, error) {
	if number <= 0 {
		return nil, sourcecontrol.ErrMilestoneNotFound
	}
	if perIssue <= 0 {
		return map[int][]sourcecontrol.IssueComment{}, nil
	}
	if perIssue > maxCommentsPerIssue {
		perIssue = maxCommentsPerIssue
	}

	var data struct {
		Repository *struct {
			Milestone *struct {
				Issues struct {
					PageInfo struct {
						HasNextPage bool `json:"hasNextPage"`
					} `json:"pageInfo"`
					Nodes []struct {
						Number   int `json:"number"`
						Comments struct {
							Nodes []commentNode `json:"nodes"`
						} `json:"comments"`
					} `json:"nodes"`
				} `json:"issues"`
			} `json:"milestone"`
		} `json:"repository"`
	}
	vars := map[string]any{
		"owner": owner, "repo": repo, "m": number,
		"i": milestoneIssuePage, "c": perIssue,
	}
	if err := c.graphQL(ctx, cred, milestoneIssueCommentsQuery, vars, &data); err != nil {
		return nil, err
	}
	if data.Repository == nil || data.Repository.Milestone == nil {
		return nil, sourcecontrol.ErrMilestoneNotFound
	}

	issues := data.Repository.Milestone.Issues
	if issues.PageInfo.HasNextPage {
		// Name the CONSEQUENCE, not just the fact: the issues past this page get
		// no comments at all, and the caller cannot tell that from the map it
		// receives (a missing bucket reads as "this issue has none"). Nothing
		// retries — a second page would double the cost on every tick of a 5s
		// poll — so this line is the only place the shortfall is visible.
		slog.WarnContext(ctx, "githubhost: milestone exceeds one comment page — issues past it carry no comments",
			"owner", owner, "repo", repo, "milestone", number, "covered", milestoneIssuePage)
	}

	out := make(map[int][]sourcecontrol.IssueComment, len(issues.Nodes))
	for _, issue := range issues.Nodes {
		if len(issue.Comments.Nodes) == 0 {
			continue
		}
		out[issue.Number] = toIssueComments(issue.Comments.Nodes)
	}
	return out, nil
}
