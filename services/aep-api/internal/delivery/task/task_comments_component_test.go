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

package task_test

// The comment field through the REAL HTTP surface — router, request binding and
// JSON encoder included.
//
// This tier exists for one fact the read-path tests cannot reach: `comments`
// defaults to TRUE, and that default lives in the contract's parameter schema
// and the handler's nil check, not in the read. A test that called ListByTag
// directly would pass whatever the caller decided and prove nothing about what
// an unadorned request does.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

func commentThread() map[int][]sourcecontrol.IssueComment {
	at := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	return map[int][]sourcecontrol.IssueComment{
		1: {
			{
				ID: "IC_1", Author: "aep-bot",
				Body:      "delegating user-service to a subagent",
				URL:       "https://github.com/acme/widgets/issues/1#issuecomment-1",
				CreatedAt: at,
			},
			{
				ID: "IC_2", Author: "anjanas",
				Body:      "watch the auth header on this one",
				URL:       "https://github.com/acme/widgets/issues/1#issuecomment-2",
				CreatedAt: at.Add(3 * time.Minute),
			},
		},
	}
}

// An unadorned `?tag=` read carries comments: the contract's default is true, so
// the console gets them without asking. The thread is whatever GitHub holds —
// the agent's note and a human's reply arrive alike, because no author filter
// exists (and none could: the platform comments through the same credential the
// runner is handed).
func TestList_TagScoped_CarriesCommentsByDefault(t *testing.T) {
	iss := newIssues(taskIssue(1, "user-service", "open"), taskIssue(2, "order-service", "open")).
		inMilestone(7, commentThread())
	h := newRigWithRuns(t, iss, fakeExecs{}, fakeMilestoneRuns{"v1": 7})

	rec := h.AsOrg(org).Get(tasks + "?state=all&tag=v1")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: code %d (%s)", rec.Code, rec.Body.String())
	}
	var views []delivery.TaskView
	if err := json.Unmarshal(rec.Body.Bytes(), &views); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	byNum := map[int]delivery.TaskView{}
	for _, v := range views {
		byNum[v.IssueNumber] = v
	}
	got := byNum[1].Comments
	if len(got) != 2 {
		t.Fatalf("issue 1 comments = %d, want 2 (%s)", len(got), rec.Body.String())
	}
	if got[0].ID != "IC_1" || got[1].ID != "IC_2" {
		t.Fatalf("thread order = %s,%s, want IC_1,IC_2", got[0].ID, got[1].ID)
	}
	if got[0].Author != "aep-bot" || got[1].Author != "anjanas" {
		t.Fatalf("authors = %s,%s — no author filtering may be applied", got[0].Author, got[1].Author)
	}
	if got[0].Body != "delegating user-service to a subagent" {
		t.Fatalf("body = %q", got[0].Body)
	}
	if !strings.Contains(rec.Body.String(), `"createdAt":"2026-08-24T10:00:00Z"`) {
		t.Fatalf("createdAt must marshal as RFC3339: %s", rec.Body.String())
	}

	// An issue with no thread omits the field entirely rather than shipping an
	// empty array on every tick of a 5s poll.
	if byNum[2].Comments != nil {
		t.Fatalf("issue 2 has no thread; got %+v", byNum[2].Comments)
	}
	if iss.commentReads != 1 {
		t.Fatalf("comment reads = %d, want exactly 1 for the milestone", iss.commentReads)
	}
}

// `comments=false` is the escape hatch, and it must reach the read: no field on
// the wire, and no round trip spent discovering that.
func TestList_TagScoped_CommentsFalseSkipsThem(t *testing.T) {
	iss := newIssues(taskIssue(1, "user-service", "open")).inMilestone(7, commentThread())
	h := newRigWithRuns(t, iss, fakeExecs{}, fakeMilestoneRuns{"v1": 7})

	rec := h.AsOrg(org).Get(tasks + "?state=all&tag=v1&comments=false")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: code %d (%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"comments"`) {
		t.Fatalf("comments=false still shipped the field: %s", rec.Body.String())
	}
	if iss.commentReads != 0 {
		t.Fatalf("comment reads = %d, want 0 — comments=false must cost nothing", iss.commentReads)
	}
}

// A read spanning versions has no milestone to anchor on, so it never asks —
// even though the default says true. Same rule that keeps ledger issues off the
// untagged list.
func TestList_Untagged_NeverReadsComments(t *testing.T) {
	iss := newIssues(taskIssue(1, "user-service", "open")).inMilestone(7, commentThread())
	h := newRigWithRuns(t, iss, fakeExecs{}, fakeMilestoneRuns{"v1": 7})

	rec := h.AsOrg(org).Get(tasks + "?state=all")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: code %d (%s)", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"comments"`) {
		t.Fatalf("an untagged read shipped comments: %s", rec.Body.String())
	}
	if iss.commentReads != 0 {
		t.Fatalf("comment reads = %d, want 0 on a read spanning versions", iss.commentReads)
	}
}
