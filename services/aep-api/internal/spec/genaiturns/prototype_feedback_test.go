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

package genaiturns

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
)

const feedbackHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func feedbackFixture() *gen.PrototypeFeedbackInput {
	return &gen.PrototypeFeedbackInput{
		PrototypeHash: feedbackHash,
		Component:     "approvals-portal",
		Requests: []gen.PrototypeFeedbackRequest{
			{
				ScreenID:   "screen.queue",
				FlowID:     "flow.approve",
				RoleID:     "approver",
				StateID:    "state.default",
				ElementIds: []string{"btn.approve", "tbl.expenses"},
				Text:       "Put the Approve button on the left.\nMake it primary.",
			},
			{ScreenID: "screen.detail", RoleID: "employee", StateID: "state.empty", Text: "Say why it is empty"},
		},
	}
}

func repeat(c string, n int) string { return strings.Repeat(c, n) }

func TestPrototypeFeedbackForwardsEveryFieldUnchanged(t *testing.T) {
	block, err := prototypeFeedbackFromJSON("/prototype approvals-portal", true, false, feedbackFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := &agentsvc.PrototypeFeedbackBlock{
		PrototypeHash: feedbackHash,
		Component:     "approvals-portal",
		Requests: []agentsvc.PrototypeFeedbackRequest{
			{
				ScreenID:   "screen.queue",
				FlowID:     "flow.approve",
				RoleID:     "approver",
				StateID:    "state.default",
				ElementIDs: []string{"btn.approve", "tbl.expenses"},
				Text:       "Put the Approve button on the left.\nMake it primary.",
			},
			// No elements: the whole screen, sent as an empty list, not null.
			{ScreenID: "screen.detail", RoleID: "employee", StateID: "state.empty", ElementIDs: []string{}, Text: "Say why it is empty"},
		},
	}
	if !reflect.DeepEqual(block, want) {
		t.Fatalf("forwarded batch = %+v, want %+v", block, want)
	}
}

// A turn with no batch must reach the agents service byte-identical to one
// sent before the channel existed: nil, not an empty block.
func TestPrototypeFeedbackAbsent(t *testing.T) {
	block, err := prototypeFeedbackFromJSON("/prototype", true, false, nil)
	if err != nil || block != nil {
		t.Fatalf("block = %+v, err = %v; want nil, nil", block, err)
	}
}

func TestPrototypeFeedbackInstructionMayBeBareOrNameTheComponent(t *testing.T) {
	for _, instruction := range []string{"/prototype", "  /prototype  ", "/prototype approvals-portal"} {
		if _, err := prototypeFeedbackFromJSON(instruction, true, false, feedbackFixture()); err != nil {
			t.Errorf("%q: unexpected error: %v", instruction, err)
		}
	}
}

func TestPrototypeFeedbackRefusesWrongPlacement(t *testing.T) {
	cases := []struct {
		name        string
		instruction string
		collab      bool
		aimed       bool
		wantInMsg   string
	}{
		{"another command", "/design", true, false, "/prototype instruction"},
		{"plain chat", "make it nicer", true, false, "/prototype instruction"},
		{"a command that only starts like it", "/prototypes", true, false, "/prototype instruction"},
		{"another component's name", "/prototype other-app", true, false, "/prototype instruction"},
		{"two components", "/prototype approvals-portal other-app", true, false, "/prototype instruction"},
		{"not a room turn", "/prototype", false, false, "collab"},
		{"an aimed turn", "/prototype", true, true, "anchor"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := prototypeFeedbackFromJSON(tc.instruction, tc.collab, tc.aimed, feedbackFixture())
			assertBadRequest(t, err, tc.wantInMsg)
		})
	}
}

func TestPrototypeFeedbackRefusesMalformedBatchesWhole(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(f *gen.PrototypeFeedbackInput)
		wantInMsg string
	}{
		{"short hash", func(f *gen.PrototypeFeedbackInput) { f.PrototypeHash = feedbackHash[:63] }, "prototypeHash"},
		{"uppercase hash", func(f *gen.PrototypeFeedbackInput) { f.PrototypeHash = strings.ToUpper(feedbackHash) }, "prototypeHash"},
		{"no hash", func(f *gen.PrototypeFeedbackInput) { f.PrototypeHash = "" }, "prototypeHash"},
		{"component that is not one path segment", func(f *gen.PrototypeFeedbackInput) { f.Component = "a/b" }, "component"},
		{"component climbing out", func(f *gen.PrototypeFeedbackInput) { f.Component = ".." }, "component"},
		{"over-long component", func(f *gen.PrototypeFeedbackInput) { f.Component = repeat("c", maxFeedbackID+1) }, "component"},
		{"empty batch", func(f *gen.PrototypeFeedbackInput) { f.Requests = nil }, "requests"},
		{"over 50 requests", func(f *gen.PrototypeFeedbackInput) {
			f.Requests = make([]gen.PrototypeFeedbackRequest, maxFeedbackRequests+1)
			for i := range f.Requests {
				f.Requests[i] = f.Requests[0]
			}
		}, "requests"},
		{"blank screen", func(f *gen.PrototypeFeedbackInput) { f.Requests[0].ScreenID = " " }, "requests[1].screenId"},
		{"blank role", func(f *gen.PrototypeFeedbackInput) { f.Requests[1].RoleID = "" }, "requests[2].roleId"},
		{"blank state", func(f *gen.PrototypeFeedbackInput) { f.Requests[0].StateID = "" }, "requests[1].stateId"},
		{"over-long flow", func(f *gen.PrototypeFeedbackInput) { f.Requests[0].FlowID = repeat("f", maxFeedbackID+1) }, "requests[1].flowId"},
		{"blank element id", func(f *gen.PrototypeFeedbackInput) { f.Requests[0].ElementIds = []string{"a", " "} }, "requests[1].elementIds"},
		{"over-long element id", func(f *gen.PrototypeFeedbackInput) {
			f.Requests[0].ElementIds = []string{repeat("e", maxFeedbackID+1)}
		}, "requests[1].elementIds"},
		{"blank text", func(f *gen.PrototypeFeedbackInput) { f.Requests[1].Text = " \n\t" }, "requests[2].text"},
		{"over-long text", func(f *gen.PrototypeFeedbackInput) { f.Requests[0].Text = repeat("t", maxFeedbackText+1) }, "requests[1].text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := feedbackFixture()
			tc.mutate(f)
			_, err := prototypeFeedbackFromJSON("/prototype", true, false, f)
			assertBadRequest(t, err, tc.wantInMsg)
		})
	}
}

// The kit's ceilings are the inclusive edge: exactly 50 requests, 4000
// characters of text and 200 characters of id are accepted.
func TestPrototypeFeedbackAcceptsTheLimits(t *testing.T) {
	f := feedbackFixture()
	one := f.Requests[0]
	one.Text = repeat("t", maxFeedbackText)
	one.ScreenID = repeat("s", maxFeedbackID)
	one.ElementIds = []string{repeat("e", maxFeedbackID)}
	f.Requests = make([]gen.PrototypeFeedbackRequest, maxFeedbackRequests)
	for i := range f.Requests {
		f.Requests[i] = one
	}
	if _, err := prototypeFeedbackFromJSON("/prototype", true, false, f); err != nil {
		t.Fatalf("a batch at every limit should be accepted: %v", err)
	}
}

// A refused batch never reaches StartTurn: CreateTurn on a handler with no
// service returns the 400 instead of dereferencing it, which it would do on
// the way to opening a turn row.
func TestCreateTurnRefusesABadBatchBeforeAnyTurnStarts(t *testing.T) {
	h := New(nil)
	bad := feedbackFixture()
	bad.Requests = nil
	_, err := h.CreateTurn(context.Background(), gen.CreateTurnRequestObject{
		ProjectName:    "p",
		ConversationID: "c",
		JSONBody:       &gen.TurnInputBody{Instruction: "/prototype", Collab: true, PrototypeFeedback: bad},
	})
	assertBadRequest(t, err, "requests")
}

func assertBadRequest(t *testing.T, err error, wantInMsg string) {
	t.Helper()
	var ae *apierr.Error
	if !errors.As(err, &ae) || ae.Status != http.StatusBadRequest {
		t.Fatalf("err = %v, want a 400", err)
	}
	if !strings.Contains(ae.Error(), wantInMsg) {
		t.Fatalf("error %q does not mention %q", ae.Error(), wantInMsg)
	}
}
