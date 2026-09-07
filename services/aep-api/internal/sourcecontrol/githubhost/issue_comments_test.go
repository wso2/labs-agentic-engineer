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

// The single-issue comment read. What is worth pinning is that it answers the
// TAIL of a thread in thread order — the newest comment is the whole point of
// the read, and a surface showing the first line of the wrong end would report a
// status hours stale — plus the brand handling it shares with its milestone
// sibling and the guards that keep it cheap.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

const issueCommentsResponse = `{"data":{"repository":{"issue":{"comments":{"nodes":[
  {"id":"IC_1","body":"Starting validation: 12 criteria, 9 to author.","url":"https://gh/c/1",
   "createdAt":"2026-09-04T10:00:00Z","author":{"login":"aep-bot"}},
  {"id":"IC_2","body":"Authoring the last three specs.","url":"https://gh/c/2",
   "createdAt":"2026-09-04T10:40:00Z","author":{"login":"aep-bot"}},
  {"id":"IC_3","body":"left by a since-deleted account","url":"https://gh/c/3",
   "createdAt":"2026-09-04T10:41:00Z","author":null}
]}}}}}`

func TestListIssueComments_AnswersTheThreadTailInOrder(t *testing.T) {
	c, cap := newGraphQLFake(t, http.StatusOK, issueCommentsResponse)

	got, err := c.ListIssueComments(context.Background(), "acme", "widgets", stubCred{}, 42, 10)
	if err != nil {
		t.Fatalf("ListIssueComments: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("comments = %d, want 3", len(got))
	}
	// Oldest first, so the NEWEST — the one a status surface renders — is last.
	// `last:` already returns the tail in order; nothing here may reverse it.
	if got[0].ID != "IC_1" || got[2].ID != "IC_3" {
		t.Fatalf("order = %s..%s, want IC_1..IC_3", got[0].ID, got[2].ID)
	}
	if got[1].Body != "Authoring the last three specs." || got[1].URL != "https://gh/c/2" {
		t.Fatalf("projection = %+v", got[1])
	}
	if want := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC); !got[0].CreatedAt.Equal(want) {
		t.Fatalf("createdAt = %s, want %s", got[0].CreatedAt, want)
	}
	// A deleted account is a fact about the comment, not a read failure.
	if got[2].Author != "" {
		t.Fatalf("null author = %q, want empty", got[2].Author)
	}

	var sent struct {
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal([]byte(cap.body), &sent); err != nil {
		t.Fatalf("request body not json: %v (%s)", err, cap.body)
	}
	if sent.Variables["n"] != float64(42) || sent.Variables["c"] != float64(10) {
		t.Fatalf("variables = %v, want issue 42 / 10 comments", sent.Variables)
	}
}

// The brand is reported and stripped, exactly as the milestone read does it —
// both call isMachineComment, and this pins that the single-issue path does not
// quietly skip it and leak `<!-- aep:machine -->` onto a rendered status line.
func TestListIssueComments_ReportsAndStripsTheMachineBrand(t *testing.T) {
	resp := `{"data":{"repository":{"issue":{"comments":{"nodes":[
      {"id":"IC_M","body":"` + sourcecontrol.MachineCommentMarker + `\nValidation run dispatched.",
       "url":"https://gh/c/9","createdAt":"2026-09-04T10:00:00Z","author":{"login":"aep-bot"}},
      {"id":"IC_H","body":"Healing AC-004-b.","url":"https://gh/c/10",
       "createdAt":"2026-09-04T10:01:00Z","author":{"login":"aep-bot"}}
    ]}}}}}`
	c, _ := newGraphQLFake(t, http.StatusOK, resp)

	got, err := c.ListIssueComments(context.Background(), "acme", "widgets", stubCred{}, 42, 10)
	if err != nil {
		t.Fatalf("ListIssueComments: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("comments = %d, want both (the HOST reports, it does not filter)", len(got))
	}
	if !got[0].Machine {
		t.Fatalf("branded comment not reported as machine: %+v", got[0])
	}
	if strings.Contains(got[0].Body, sourcecontrol.MachineCommentMarker) {
		t.Fatalf("the brand leaked into the body: %q", got[0].Body)
	}
	if got[1].Machine {
		t.Fatalf("unbranded comment reported as machine: %+v", got[1])
	}
}

// An issue with no comments answers nil, matching what the milestone read's
// ABSENT bucket means — one shape for "nothing to say" across both surfaces.
func TestListIssueComments_EmptyThreadIsNil(t *testing.T) {
	c, _ := newGraphQLFake(t, http.StatusOK,
		`{"data":{"repository":{"issue":{"comments":{"nodes":[]}}}}}`)

	got, err := c.ListIssueComments(context.Background(), "acme", "widgets", stubCred{}, 42, 10)
	if err != nil {
		t.Fatalf("ListIssueComments: %v", err)
	}
	if got != nil {
		t.Fatalf("empty thread = %v, want nil", got)
	}
}

// A missing issue is ErrIssueNotFound, so a caller can tell it from a transport
// failure — the same discrimination GetIssue offers.
func TestListIssueComments_MissingIssueIsNotFound(t *testing.T) {
	c, _ := newGraphQLFake(t, http.StatusOK, `{"data":{"repository":{"issue":null}}}`)

	_, err := c.ListIssueComments(context.Background(), "acme", "widgets", stubCred{}, 42, 10)
	if !errors.Is(err, sourcecontrol.ErrIssueNotFound) {
		t.Fatalf("err = %v, want ErrIssueNotFound", err)
	}
}

// The cheap guards: asking for nothing is a coherent request and must not become
// a query error, and an oversized limit is clamped rather than sent — GraphQL
// rejects `last:` above 100 outright, which would turn a decorative read into a
// failing one.
func TestListIssueComments_GuardsCostAndLimit(t *testing.T) {
	t.Run("no round trip for a non-positive limit", func(t *testing.T) {
		c, cap := newGraphQLFake(t, http.StatusOK, issueCommentsResponse)
		got, err := c.ListIssueComments(context.Background(), "acme", "widgets", stubCred{}, 42, 0)
		if err != nil || got != nil {
			t.Fatalf("limit 0 = (%v, %v), want (nil, nil)", got, err)
		}
		if cap.body != "" {
			t.Fatalf("a request was sent for limit 0: %s", cap.body)
		}
	})

	t.Run("oversized limit is clamped", func(t *testing.T) {
		c, cap := newGraphQLFake(t, http.StatusOK, issueCommentsResponse)
		if _, err := c.ListIssueComments(context.Background(), "acme", "widgets", stubCred{}, 42, 5000); err != nil {
			t.Fatalf("ListIssueComments: %v", err)
		}
		var sent struct {
			Variables map[string]any `json:"variables"`
		}
		if err := json.Unmarshal([]byte(cap.body), &sent); err != nil {
			t.Fatalf("request body not json: %v", err)
		}
		if sent.Variables["c"] != float64(maxCommentsPerIssue) {
			t.Fatalf("c = %v, want clamped to %d", sent.Variables["c"], maxCommentsPerIssue)
		}
	})
}
