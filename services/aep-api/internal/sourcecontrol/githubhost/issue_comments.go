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

// ONE issue's newest comments — the detail read's sibling to the milestone read
// in milestone_comments.go, and GraphQL for a different reason than that one.
//
// The milestone read is GraphQL because REST cannot cover N issues in one call.
// This covers a single issue, which REST answers in one call — but from the
// WRONG END. `GET /repos/{o}/{r}/issues/{n}/comments` pages a thread oldest
// first with no sort parameter, so the newest comment (the only one a status
// surface wants) sits on the last page, and finding that page costs a second
// request against a `Link: rel="last"` header. `comments(last:)` asks for the
// tail directly, so the question REST needs two calls for takes one here —
// which is the bar graphql.go sets for using this transport at all.

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// issueCommentsQuery reads the newest `$c` comments of ONE issue. The fields and
// the reason the tail is the end worth asking for live with commentSelection in
// comments.go, shared with the milestone query.
const issueCommentsQuery = `query($owner: String!, $repo: String!, $n: Int!, $c: Int!) {
  repository(owner: $owner, name: $repo) {
    issue(number: $n) {
      comments(last: $c) { ` + commentSelection + ` }
    }
  }
}`

// ListIssueComments returns the newest `limit` comments of one issue, oldest
// first, with the platform's own brand stripped and flagged (isMachineComment).
//
// An issue with no comments answers nil rather than an empty slice, matching
// what the milestone read's absent bucket means, so both surfaces read alike.
//
// limit <= 0 answers nil without a round trip: asking for no comments is a
// coherent request and must not become a query error.
func (c *Client) ListIssueComments(ctx context.Context, owner, repo string, cred secrets.Credential, number, limit int) ([]sourcecontrol.IssueComment, error) {
	if number <= 0 {
		return nil, sourcecontrol.ErrIssueNotFound
	}
	if limit <= 0 {
		return nil, nil
	}
	if limit > maxCommentsPerIssue {
		limit = maxCommentsPerIssue
	}

	var data struct {
		Repository *struct {
			Issue *struct {
				Comments struct {
					Nodes []commentNode `json:"nodes"`
				} `json:"comments"`
			} `json:"issue"`
		} `json:"repository"`
	}
	vars := map[string]any{"owner": owner, "repo": repo, "n": number, "c": limit}
	if err := c.graphQL(ctx, cred, issueCommentsQuery, vars, &data); err != nil {
		return nil, err
	}
	if data.Repository == nil || data.Repository.Issue == nil {
		return nil, sourcecontrol.ErrIssueNotFound
	}

	nodes := data.Repository.Issue.Comments.Nodes
	if len(nodes) == 0 {
		return nil, nil
	}
	return toIssueComments(nodes), nil
}
