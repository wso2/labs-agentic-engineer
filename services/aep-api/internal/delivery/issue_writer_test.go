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

package delivery

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// recordingOps is the host as the writer sees it: it records every call in
// ORDER, so a test can assert not only what the writer sent but how many host
// round trips it took to send it.
type recordingOps struct {
	calls    []string
	created  []sourcecontrol.CreateIssueRequest
	closed   []int
	comments []string
	reopened []int
	added    [][]string
	removed  []string
	moved    map[int]int

	result *sourcecontrol.IssueResult
	err    error
}

func (r *recordingOps) CreateIssue(_ context.Context, _, _ string, req sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error) {
	r.calls = append(r.calls, "create")
	r.created = append(r.created, req)
	return r.result, r.err
}

func (r *recordingOps) CloseIssue(_ context.Context, _, _ string, number int, comment string) error {
	r.calls = append(r.calls, "close")
	r.closed = append(r.closed, number)
	r.comments = append(r.comments, comment)
	return r.err
}

func (r *recordingOps) ReopenIssue(_ context.Context, _, _ string, number int) error {
	r.calls = append(r.calls, "reopen")
	r.reopened = append(r.reopened, number)
	return r.err
}

func (r *recordingOps) CommentIssue(_ context.Context, _, _ string, _ int, body string) error {
	r.calls = append(r.calls, "comment")
	r.comments = append(r.comments, body)
	return r.err
}

func (r *recordingOps) AddLabels(_ context.Context, _, _ string, _ int, labels []string) error {
	r.calls = append(r.calls, "addLabels")
	r.added = append(r.added, labels)
	return r.err
}

func (r *recordingOps) RemoveLabel(_ context.Context, _, _ string, _ int, label string) error {
	r.calls = append(r.calls, "removeLabel")
	r.removed = append(r.removed, label)
	return r.err
}

func (r *recordingOps) SetIssueMilestone(_ context.Context, _, _ string, number, milestoneNumber int) error {
	r.calls = append(r.calls, "setMilestone")
	if r.moved == nil {
		r.moved = map[int]int{}
	}
	r.moved[number] = milestoneNumber
	return r.err
}

// The dedupe key must reach the host EXACTLY as the caller spelled it. The host
// normalises it into a `dedupe:<key>` label with a lossy transform (lowercase,
// whitespace runs to "-", hash on overflow) and matches that label against the
// milestone's open issues; a writer that trimmed, lowercased or re-joined the
// key would derive a different label and file a second issue for work that is
// already open — silently, because a mint that files is the normal outcome.
func TestIssueWriter_MintPassesTheDedupeKeyThrough(t *testing.T) {
	ops := &recordingOps{result: &sourcecontrol.IssueResult{Number: 7}}
	w := NewIssueWriter(ops)

	const key = "aep Fix  order-service  ABC123DEF456" // mixed case, double spaces
	if _, _, err := w.Mint(context.Background(), "org", "proj", IssueSpec{
		Title: "t", Body: "b", DedupeKey: key,
	}); err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if got := ops.created[0].DedupeKey; got != key {
		t.Errorf("dedupe key = %q; want it byte-identical to %q", got, key)
	}
}

// The writer must not look before it leaps. The host's CreateIssue holds a
// per-repo lock that makes its own check-then-create atomic; a pre-check here
// would sit outside that lock and reopen the duplicate-issue race the lock
// exists to close. ONE host call per mint is what proves there is no pre-check.
func TestIssueWriter_MintIsOneHostCall(t *testing.T) {
	ops := &recordingOps{result: &sourcecontrol.IssueResult{Number: 7}}

	if _, _, err := NewIssueWriter(ops).Mint(context.Background(), "org", "proj", IssueSpec{
		Title: "t", DedupeKey: "aep conflict pr-4",
	}); err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if want := []string{"create"}; !reflect.DeepEqual(ops.calls, want) {
		t.Errorf("host calls = %v; want %v — anything more is a check-then-create race", ops.calls, want)
	}
}

// Labels arrive in the caller's order and the writer adds none of its own: the
// host appends the derived dedupe label AFTER these, so a reader of the issue
// sees the domain's vocabulary first.
func TestIssueWriter_MintPreservesLabelOrderAndAddsNone(t *testing.T) {
	ops := &recordingOps{result: &sourcecontrol.IssueResult{Number: 7}}
	labels := []string{LabelAgentWork, "phase-2"}

	if _, _, err := NewIssueWriter(ops).Mint(context.Background(), "org", "proj", IssueSpec{
		Title: "t", Labels: labels, DedupeKey: "aep fix svc abc",
	}); err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if got := ops.created[0].Labels; !reflect.DeepEqual(got, labels) {
		t.Errorf("labels = %v; want exactly the caller's %v, in order", got, labels)
	}
}

// An issue with no agent-work label is a real population — the red-main
// incident is filed as a ledger entry nobody is dispatched for — so an empty
// label set must survive the writer rather than being helpfully filled in.
func TestIssueWriter_MintKeepsAnUnlabelledIssueUnlabelled(t *testing.T) {
	ops := &recordingOps{result: &sourcecontrol.IssueResult{Number: 7}}

	if _, _, err := NewIssueWriter(ops).Mint(context.Background(), "org", "proj", IssueSpec{
		Title: "Red main: svc", DedupeKey: "aep red-main svc abc",
	}); err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if got := ops.created[0].Labels; len(got) != 0 {
		t.Errorf("labels = %v; want none — a red main is never auto-dispatched", got)
	}
}

// The milestone rides the CREATE, so an issue is never versionless. Zero means
// "no milestone" and must reach the host as an absent field, not as milestone 0
// (which GitHub answers 422 to).
func TestIssueWriter_MintAssignsTheMilestoneOnTheCreate(t *testing.T) {
	ops := &recordingOps{result: &sourcecontrol.IssueResult{Number: 7}}
	w := NewIssueWriter(ops)

	if _, _, err := w.Mint(context.Background(), "org", "proj", IssueSpec{Title: "t", Milestone: 12}); err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if got := ops.created[0].Milestone; got == nil || *got != 12 {
		t.Errorf("milestone = %v; want 12 on the create itself", got)
	}

	if _, _, err := w.Mint(context.Background(), "org", "proj", IssueSpec{Title: "t"}); err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if got := ops.created[1].Milestone; got != nil {
		t.Errorf("milestone = %v; want unassigned for the zero value", *got)
	}
}

// `deduped` is what tells a caller its work was already open. Reporting it
// wrongly would not break a mint, which is exactly why it is worth pinning.
func TestIssueWriter_MintReportsDedupeFaithfully(t *testing.T) {
	for _, tc := range []struct {
		name string
		res  *sourcecontrol.IssueResult
		num  int
		dup  bool
	}{
		{"filed", &sourcecontrol.IssueResult{Number: 7}, 7, false},
		{"resolved onto an open issue", &sourcecontrol.IssueResult{Number: 4, Deduped: true}, 4, true},
		// A host that answers nothing is not an error: the mint simply produced
		// no number, and every caller already treats 0 as "nothing filed".
		{"no result", nil, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := NewIssueWriter(&recordingOps{result: tc.res})
			number, deduped, err := w.Mint(context.Background(), "org", "proj", IssueSpec{Title: "t"})
			if err != nil {
				t.Fatalf("Mint: %v", err)
			}
			if number != tc.num || deduped != tc.dup {
				t.Errorf("Mint = (%d, %t); want (%d, %t)", number, deduped, tc.num, tc.dup)
			}
		})
	}
}

// A host error reaches the caller with no number, because a caller that read a
// number out of a failed mint would record an issue that does not exist.
func TestIssueWriter_MintReportsHostFailure(t *testing.T) {
	boom := errors.New("github said no")
	w := NewIssueWriter(&recordingOps{err: boom})

	number, deduped, err := w.Mint(context.Background(), "org", "proj", IssueSpec{Title: "t"})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v; want the host's", err)
	}
	if number != 0 || deduped {
		t.Errorf("Mint = (%d, %t); want (0, false) on failure", number, deduped)
	}
}

// The write verbs are pass-throughs, and the assertion worth making about them
// is that they reach the host at all: each one replaced a direct call in a
// sub-package, and a writer that dropped a close would leave a superseded
// milestone full of open work with nothing failing.
func TestIssueWriter_WritesReachTheHost(t *testing.T) {
	ops := &recordingOps{}
	w := NewIssueWriter(ops)
	ctx := context.Background()

	if err := w.Close(ctx, "org", "proj", 3, "Superseded by v2."); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := w.Reopen(ctx, "org", "proj", 4); err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	if err := w.Comment(ctx, "org", "proj", 5, "note"); err != nil {
		t.Fatalf("Comment: %v", err)
	}
	if err := w.Label(ctx, "org", "proj", 6, LabelAgentWork, KindBug); err != nil {
		t.Fatalf("Label: %v", err)
	}
	if err := w.Unlabel(ctx, "org", "proj", 7, LabelCancelled); err != nil {
		t.Fatalf("Unlabel: %v", err)
	}

	want := []string{"close", "reopen", "comment", "addLabels", "removeLabel"}
	if !reflect.DeepEqual(ops.calls, want) {
		t.Fatalf("host calls = %v; want %v", ops.calls, want)
	}
	if ops.closed[0] != 3 || ops.reopened[0] != 4 {
		t.Errorf("close/reopen hit issues %v/%v; want 3/4", ops.closed, ops.reopened)
	}
	if got := ops.added[0]; !reflect.DeepEqual(got, []string{LabelAgentWork, KindBug}) {
		t.Errorf("added labels = %v; want the caller's set in order", got)
	}
	if ops.removed[0] != LabelCancelled {
		t.Errorf("removed label = %q; want %q", ops.removed[0], LabelCancelled)
	}
}

// Labelling nothing is not a host call. A caller may hand the writer a computed
// set, and an empty request would spend a rate-limited round trip to change
// nothing.
func TestIssueWriter_LabelWithNothingIsNoCall(t *testing.T) {
	ops := &recordingOps{}
	if err := NewIssueWriter(ops).Label(context.Background(), "org", "proj", 1); err != nil {
		t.Fatalf("Label: %v", err)
	}
	if len(ops.calls) != 0 {
		t.Errorf("host calls = %v; want none for an empty label set", ops.calls)
	}
}

// Nil tolerance, on every verb. This is not defensive habit: the composition
// root wires the event plane before some of its collaborators exist, and every
// mint site this writer replaced degraded to filing nothing rather than
// panicking on a webhook. A nil writer answers the same way a nil host does.
func TestIssueWriter_NilWriterAndNilHostDegradeToNothing(t *testing.T) {
	ctx := context.Background()
	for name, w := range map[string]*IssueWriter{
		"nil writer": nil,
		"nil host":   NewIssueWriter(nil),
	} {
		t.Run(name, func(t *testing.T) {
			number, deduped, err := w.Mint(ctx, "org", "proj", IssueSpec{Title: "t"})
			if number != 0 || deduped || err != nil {
				t.Errorf("Mint = (%d, %t, %v); want (0, false, nil)", number, deduped, err)
			}
			if err := w.Close(ctx, "org", "proj", 1, ""); err != nil {
				t.Errorf("Close: %v", err)
			}
			if err := w.Reopen(ctx, "org", "proj", 1); err != nil {
				t.Errorf("Reopen: %v", err)
			}
			if err := w.Comment(ctx, "org", "proj", 1, "x"); err != nil {
				t.Errorf("Comment: %v", err)
			}
			if err := w.Label(ctx, "org", "proj", 1, LabelAgentWork); err != nil {
				t.Errorf("Label: %v", err)
			}
			if err := w.Unlabel(ctx, "org", "proj", 1, LabelAgentWork); err != nil {
				t.Errorf("Unlabel: %v", err)
			}
		})
	}
}
