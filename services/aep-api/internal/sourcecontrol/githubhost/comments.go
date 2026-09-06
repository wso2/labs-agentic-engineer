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

// What an issue comment IS on the way in, shared by the two reads that fetch
// them: the milestone's whole set (milestone_comments.go) and one issue's thread
// (issue_comments.go). The two differ only in what they ask GitHub for; a
// comment means the same thing to both, and this is the one place that says so.
//
// Keeping it here is not tidiness. A comment's shape is stated THREE times — the
// GraphQL selection set, the decode struct, the projection — and they must agree
// or a field is fetched and silently dropped. Held once, a new field is added in
// one place for both readers; held twice, the second reader is the one nobody
// remembers to change.

import (
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// maxCommentsPerIssue caps what a caller may ask for per issue. GraphQL's own
// limit on a `last:` argument is 100, and asking for more is a query error
// rather than a truncation, so the clamp belongs here — a caller's oversized N
// must not turn a decorative read into a failed one.
const maxCommentsPerIssue = 100

// commentSelection is the selection set both queries embed, so the fields
// requested and the fields decoded cannot drift apart.
//
// `comments(last:)` is deliberate rather than `first:` at both call sites. The
// interesting comments are the recent ones — an agent's latest progress note,
// the newest diagnostic — and `last:` returns the tail of the thread while still
// answering in chronological order, so the consumer needs no reversal. Ordering
// is not requested explicitly because a comment connection has exactly one order.
const commentSelection = `nodes { id body url createdAt author { login } }`

// commentNode is the decode target for commentSelection.
//
// `author` is nullable and must STAY nullable: GitHub answers null for a comment
// whose account was deleted, and treating that as a decode failure would drop
// every other comment on the issue with it.
type commentNode struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"createdAt"`
	Author    *struct {
		Login string `json:"login"`
	} `json:"author"`
}

// toIssueComments projects decoded nodes into the domain type, in the order they
// arrived (oldest first within the window).
//
// A deleted author lands as an empty login rather than an error: who wrote it is
// a fact about the comment, and losing that fact must not lose the comment.
func toIssueComments(nodes []commentNode) []sourcecontrol.IssueComment {
	out := make([]sourcecontrol.IssueComment, 0, len(nodes))
	for _, n := range nodes {
		login := ""
		if n.Author != nil {
			login = n.Author.Login
		}
		body, machine := isMachineComment(n.Body)
		out = append(out, sourcecontrol.IssueComment{
			ID:        n.ID,
			Author:    login,
			Body:      body,
			URL:       n.URL,
			CreatedAt: n.CreatedAt,
			Machine:   machine,
		})
	}
	return out
}

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
