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
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// isMachineComment reports whether a body is one the PLATFORM wrote
// (sourcecontrol.MachineCommentMarker) and returns the body without the brand.
//
// The brand is stripped rather than passed through because it is an
// implementation detail of telling machine from human — every consumer wants the
// prose, and a raw marker leaking into a rendered feed would be a visible
// artefact of a mechanism that exists to be invisible.
//
// Detection is a PREFIX test, and only the leading brand is removed. `Contains`
// would be more forgiving of a body edited on the host afterwards, but it
// misclassifies the case that actually matters: an agent or a person QUOTING a
// platform comment — which the agent is told to read (`gh issue view
// --comments`, where the raw marker is visible) — would have their own note
// silently dropped from the feed. The two failures are not symmetric. A machine
// comment that slips through is visible noise somebody can report; a human note
// classified as machine is data loss nobody can see. So the strict test wins,
// and the writer putting the brand first is what makes it correct.
func isMachineComment(body string) (string, bool) {
	trimmed := strings.TrimLeft(body, " \t\r\n")
	if !strings.HasPrefix(trimmed, sourcecontrol.MachineCommentMarker) {
		return body, false
	}
	rest := strings.TrimPrefix(trimmed, sourcecontrol.MachineCommentMarker)
	return strings.TrimLeft(rest, " \t\r\n"), true
}

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

// maxCommentsPerIssue caps what a caller may ask for per issue. GraphQL's own
// limit on a `last:` argument is 100, and asking for more is a query error
// rather than a truncation, so the clamp belongs here — a caller's oversized N
// must not turn a decorative read into a failed one.
const maxCommentsPerIssue = 100

// milestoneIssueCommentsQuery reads the newest `$c` comments of every issue in a
// milestone.
//
// `comments(last:)` is deliberate rather than `first:`. The interesting comments
// are the recent ones — an agent's latest progress note, the newest diagnostic —
// and `last:` returns the tail of the thread while still answering in
// chronological order, so the consumer needs no reversal. Ordering is not
// requested explicitly because a comment connection has exactly one order.
//
// `author` is nullable and must stay nullable in the decode: GitHub answers null
// for a comment whose account was deleted, and treating that as a decode failure
// would drop every other comment on the issue with it.
const milestoneIssueCommentsQuery = `query($owner: String!, $repo: String!, $m: Int!, $i: Int!, $c: Int!) {
  repository(owner: $owner, name: $repo) {
    milestone(number: $m) {
      issues(first: $i) {
        pageInfo { hasNextPage }
        nodes {
          number
          comments(last: $c) {
            nodes { id body url createdAt author { login } }
          }
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
							Nodes []struct {
								ID        string    `json:"id"`
								Body      string    `json:"body"`
								URL       string    `json:"url"`
								CreatedAt time.Time `json:"createdAt"`
								// Author is null for a deleted account.
								Author *struct {
									Login string `json:"login"`
								} `json:"author"`
							} `json:"nodes"`
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
		bucket := make([]sourcecontrol.IssueComment, 0, len(issue.Comments.Nodes))
		for _, n := range issue.Comments.Nodes {
			login := ""
			if n.Author != nil {
				login = n.Author.Login
			}
			body, machine := isMachineComment(n.Body)
			bucket = append(bucket, sourcecontrol.IssueComment{
				ID:        n.ID,
				Author:    login,
				Body:      body,
				URL:       n.URL,
				CreatedAt: n.CreatedAt,
				Machine:   machine,
			})
		}
		out[issue.Number] = bucket
	}
	return out, nil
}
