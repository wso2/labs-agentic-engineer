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

package task

// The comment half of the Task list. Four facts carry the feature and each is
// one test: comments arrive on a milestone-scoped read, the flag genuinely
// gates the round trip, a read spanning versions never makes it, and a host
// that will not answer costs the caller its comments rather than its Tasks.

import (
	"context"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

func comment(id, author, body string, at time.Time) sourcecontrol.IssueComment {
	return sourcecontrol.IssueComment{
		ID: id, Author: author, Body: body,
		URL:       "https://github.com/acme/widgets/issues/1#issuecomment-" + id,
		CreatedAt: at,
	}
}

// The whole point of the feature: a version-scoped read carries each issue's
// thread, projected onto the read DTO in the order the host gave it.
func TestReads_ListByTag_CarriesIssueComments(t *testing.T) {
	base := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	issues := newFakeIssues()
	issues.seedInMilestone(agentIssue(1, "Implement user-service", "brief"), 7)
	issues.seedInMilestone(agentIssue(2, "Implement order-service", "brief"), 7)
	issues.seedComment(1, comment("IC_1", "aep-bot", "delegating user-service to a subagent", base))
	issues.seedComment(1, comment("IC_2", "aep-bot", "user-service builds clean", base.Add(5*time.Minute)))
	runs := fakeMilestones{"v3": 7}

	views, err := newReads(issues, newFakeExecReader(), runs).
		ListByTag(context.Background(), "org1", "proj1", "all", "v3", true)
	if err != nil {
		t.Fatalf("ListByTag(v3): %v", err)
	}
	byNum := map[int][]string{}
	for _, v := range views {
		for _, c := range v.Comments {
			byNum[v.IssueNumber] = append(byNum[v.IssueNumber], c.ID)
		}
	}
	if len(byNum[1]) != 2 || byNum[1][0] != "IC_1" || byNum[1][1] != "IC_2" {
		t.Fatalf("issue 1 comments = %v, want IC_1,IC_2 in thread order", byNum[1])
	}
	// An issue with no thread carries no comments, not an empty array — the
	// field's absence and its emptiness are the same answer here (see
	// commentViews), and the wire tag is omitempty for that reason.
	for _, v := range views {
		if v.IssueNumber == 2 && v.Comments != nil {
			t.Fatalf("issue 2 has no comments; got %+v", v.Comments)
		}
	}

	// The projection is total — nothing is dropped between host and DTO.
	var got = views[0].Comments[0]
	if got.Author != "aep-bot" || got.Body != "delegating user-service to a subagent" ||
		got.URL == "" || !got.CreatedAt.Equal(base) {
		t.Fatalf("comment projection = %+v", got)
	}

	// One read for the whole milestone, asking for the documented cap.
	if issues.commentReads != 1 {
		t.Fatalf("comment reads = %d, want exactly 1 for the milestone", issues.commentReads)
	}
	if issues.lastCommentPerIssue != CommentsPerIssue {
		t.Fatalf("per-issue cap = %d, want %d", issues.lastCommentPerIssue, CommentsPerIssue)
	}
}

// The platform's own comments never reach this surface. They are written for the
// agent, not for a person — a resolved-dependency block, a provisioning note, a
// closing line — and on an issue that has one they would crowd out the narrative
// this field exists to carry.
func TestReads_ListByTag_DropsMachineComments(t *testing.T) {
	base := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	issues := newFakeIssues()
	issues.seedInMilestone(agentIssue(1, "Implement user-service", "brief"), 7)
	issues.seedInMilestone(gateIssue(2, "postgres"), 7)

	machine := comment("IC_M", "aep-bot", "## Component user-service\n<resolved wiring>", base)
	machine.Machine = true
	issues.seedComment(1, machine)
	issues.seedComment(1, comment("IC_H", "aep-bot", "delegating user-service to a subagent", base.Add(time.Minute)))

	// An issue whose ONLY comment is the platform's reads as having none at all,
	// rather than as an empty array — the field has one empty shape, not two.
	onlyMachine := comment("IC_M2", "aep-bot", "✅ Provisioned.", base)
	onlyMachine.Machine = true
	issues.seedComment(2, onlyMachine)

	runs := fakeMilestones{"v3": 7}
	views, err := newReads(issues, newFakeExecReader(), runs).
		ListByTag(context.Background(), "org1", "proj1", "all", "v3", true)
	if err != nil {
		t.Fatalf("ListByTag(v3): %v", err)
	}
	byNum := map[int]delivery.TaskView{}
	for _, v := range views {
		byNum[v.IssueNumber] = v
	}
	got := byNum[1].Comments
	if len(got) != 1 || got[0].ID != "IC_H" {
		t.Fatalf("issue 1 comments = %+v, want only the agent's IC_H", got)
	}
	if byNum[2].Comments != nil {
		t.Fatalf("an issue with only machine comments must carry none, got %+v", byNum[2].Comments)
	}
}

// The two host reads OVERLAP. They are independent once the milestone number is
// known, and this endpoint is polled every few seconds while a version is live —
// taking them in sequence measured at roughly double the latency.
//
// Asserted by construction rather than by a stopwatch: the fake's issue list
// refuses to proceed until the comment read has entered, so a sequential
// implementation cannot finish this test at all.
func TestReads_ListByTag_ReadsIssuesAndCommentsConcurrently(t *testing.T) {
	issues := newFakeIssues().overlapProbe()
	issues.seedInMilestone(agentIssue(1, "Implement user-service", "brief"), 7)
	issues.seedComment(1, comment("IC_1", "aep-bot", "delegating user-service", time.Now()))
	runs := fakeMilestones{"v3": 7}

	views, err := newReads(issues, newFakeExecReader(), runs).
		ListByTag(context.Background(), "org1", "proj1", "all", "v3", true)
	if err != nil {
		t.Fatalf("ListByTag(v3): %v", err)
	}
	if len(views) != 1 || len(views[0].Comments) != 1 {
		t.Fatalf("want the Task with its comment, got %+v", views)
	}
}

// `comments=false` is the way out for a caller that renders none: it must cost
// nothing, which means the round trip is not made at all.
func TestReads_ListByTag_CommentsFalseSkipsTheRoundTrip(t *testing.T) {
	issues := newFakeIssues()
	issues.seedInMilestone(agentIssue(1, "Implement user-service", "brief"), 7)
	issues.seedComment(1, comment("IC_1", "aep-bot", "note", time.Now()))
	runs := fakeMilestones{"v3": 7}

	views, err := newReads(issues, newFakeExecReader(), runs).
		ListByTag(context.Background(), "org1", "proj1", "all", "v3", false)
	if err != nil {
		t.Fatalf("ListByTag(v3): %v", err)
	}
	if len(views) != 1 {
		t.Fatalf("want the Task itself, got %d", len(views))
	}
	if views[0].Comments != nil {
		t.Fatalf("comments were not asked for, got %+v", views[0].Comments)
	}
	if issues.commentReads != 0 {
		t.Fatalf("comment reads = %d, want 0 — the flag must gate the round trip", issues.commentReads)
	}
}

// A read spanning versions has no milestone to anchor the comment fetch on, so
// it never makes it — even asked for. Same rule that keeps ledger issues off the
// untagged list, and it must hold on the FLAG, not on the caller remembering.
func TestReads_ListByTag_UntaggedNeverReadsComments(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(agentIssue(1, "Implement user-service", "brief"))
	issues.seedComment(1, comment("IC_1", "aep-bot", "note", time.Now()))

	views, err := newReads(issues, newFakeExecReader(), nil).
		ListByTag(context.Background(), "org1", "proj1", "all", "", true)
	if err != nil {
		t.Fatalf("ListByTag: %v", err)
	}
	if len(views) != 1 || views[0].Comments != nil {
		t.Fatalf("an untagged read must carry no comments, got %+v", views)
	}
	if issues.commentReads != 0 {
		t.Fatalf("comment reads = %d, want 0 on a read spanning versions", issues.commentReads)
	}
}

// The degrade that matters. This list drives the milestone panel and the run
// card's ability to tell a gate hold from an empty working set; comments are
// decorative. A host that will not answer them must cost the caller its
// comments, never its Tasks.
func TestReads_ListByTag_CommentFailureDoesNotFailTheList(t *testing.T) {
	issues := newFakeIssues()
	issues.seedInMilestone(agentIssue(1, "Implement user-service", "brief"), 7)
	issues.seedInMilestone(gateIssue(2, "postgres"), 7)
	issues.failComments = true
	runs := fakeMilestones{"v3": 7}

	views, err := newReads(issues, newFakeExecReader(), runs).
		ListByTag(context.Background(), "org1", "proj1", "all", "v3", true)
	if err != nil {
		t.Fatalf("a failed comment read must not fail the list: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("want both issues, got %d: %+v", len(views), views)
	}
	for _, v := range views {
		if v.Comments != nil {
			t.Fatalf("issue %d carried comments from a failed read: %+v", v.IssueNumber, v.Comments)
		}
	}
}

// The DETAIL read's comment half. It exists for one issue the list can never
// answer for: buildView drops the validation issue on its kind before comments
// are ever attached, so this path is the only way its narrative reaches a
// console — and that narrative is what the Validation page shows while a run is
// in flight.
func TestReads_Get_CarriesTheValidationIssuesComments(t *testing.T) {
	base := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	issues := newFakeIssues()
	issues.seed(validationIssue(7))
	issues.seedComment(7, comment("c1", "aep-bot", "Starting validation: 12 criteria, 9 to author.", base))
	issues.seedComment(7, comment("c2", "aep-bot", "Healing AC-004-b — the login step raced the redirect.", base.Add(time.Hour)))

	detail, err := newReads(issues, newFakeExecReader(), nil).Get(context.Background(), "org1", "proj1", 7)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(detail.Comments) != 2 {
		t.Fatalf("comments = %d, want 2 — the list hides this issue, so only Get can carry them", len(detail.Comments))
	}
	// Oldest first, so a consumer reading the CURRENT status takes the last.
	if detail.Comments[1].Body != "Healing AC-004-b — the login step raced the redirect." {
		t.Fatalf("newest comment = %q", detail.Comments[1].Body)
	}
	if detail.Comments[0].Author != "aep-bot" || detail.Comments[0].ID != "c1" {
		t.Fatalf("projection = %+v", detail.Comments[0])
	}
}

// The detail read's two host calls OVERLAP, like the list's. Get is polled at
// the same 5s cadence while a validation run is in flight, so a comment round
// trip taken strictly after the issue fetch adds its whole latency to every
// tick — for a field the caller may not even render.
//
// Asserted by construction rather than by a stopwatch, the same way the list's
// is: the fake's GetIssue refuses to proceed until the comment read has
// entered, so an implementation that reverted to sequential cannot finish this
// test at all.
func TestReads_Get_ReadsTheIssueAndItsCommentsConcurrently(t *testing.T) {
	issues := newFakeIssues().overlapProbe()
	issues.seed(validationIssue(7))
	issues.seedComment(7, comment("c1", "aep-bot", "Authoring the last three specs.", time.Now()))

	detail, err := newReads(issues, newFakeExecReader(), nil).Get(context.Background(), "org1", "proj1", 7)
	if err != nil {
		// Get collapses every GetIssue error into ErrTaskNotFound, so the fake's
		// own "the two host reads are sequential" never reaches here. Say it.
		t.Fatalf("Get: %v — a not-found here means the probe timed out: the comment read started AFTER the issue fetch", err)
	}
	if len(detail.Comments) != 1 {
		t.Fatalf("want the Task with its comment, got %+v", detail)
	}
}

// The detail read asks for the SAME depth as the list. Two surfaces showing the
// same thread at different depths would put a comment on one page and not the
// other, for no reader's benefit.
func TestReads_Get_UsesTheSameCommentWindowAsTheList(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(agentIssue(5, "Implement the login endpoint", "## Scope"))

	if _, err := newReads(issues, newFakeExecReader(), nil).Get(context.Background(), "org1", "proj1", 5); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if issues.lastCommentPerIssue != CommentsPerIssue {
		t.Fatalf("asked the host for %d comments, want CommentsPerIssue (%d)",
			issues.lastCommentPerIssue, CommentsPerIssue)
	}
}

// The platform's own comments never reach this surface either. The validation
// issue in particular collects them — the run dispatches against it — so a
// detail read that passed them through would show a machine note as the agent's
// current status.
func TestReads_Get_DropsMachineComments(t *testing.T) {
	base := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	machine := comment("m1", "aep-bot", "Validation run dispatched.", base.Add(time.Hour))
	machine.Machine = true

	issues := newFakeIssues()
	issues.seed(validationIssue(7))
	issues.seedComment(7, comment("c1", "aep-bot", "Authoring the last three specs.", base))
	issues.seedComment(7, machine)

	detail, err := newReads(issues, newFakeExecReader(), nil).Get(context.Background(), "org1", "proj1", 7)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(detail.Comments) != 1 {
		t.Fatalf("comments = %d, want only the agent's", len(detail.Comments))
	}
	if detail.Comments[0].ID != "c1" {
		t.Fatalf("surviving comment = %+v, want the agent's", detail.Comments[0])
	}
}

// A host that will not answer comments costs the caller its narrative, never its
// Task — the same bargain the list makes, and for the same reason: the detail
// page's other halves are not decorative.
func TestReads_Get_CommentFailureDoesNotFailTheRead(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(validationIssue(7))
	issues.failComments = true

	detail, err := newReads(issues, newFakeExecReader(), nil).Get(context.Background(), "org1", "proj1", 7)
	if err != nil {
		t.Fatalf("Get must survive a comment failure, got %v", err)
	}
	if detail.IssueNumber != 7 {
		t.Fatalf("issue = %d, want 7", detail.IssueNumber)
	}
	if detail.Comments != nil {
		t.Fatalf("comments = %v, want nil", detail.Comments)
	}
}
