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
	"fmt"
	"regexp"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
)

// The prototype feedback on a create-turn request: a reviewer's batch of
// requests on ONE web-application prototype, revised in a single `/prototype`
// turn. Like the aim it carries facts and no wording: this file validates the
// batch and forwards it unchanged, and the agents service alone words it into a
// revision brief (services/agents/src/prompts/turn.ts).
//
// A malformed batch is refused whole, here, before any turn row exists: one the
// agent applied only in part would read as sent and done.

// Ceilings mirror the prototype kit's (MAX_FEEDBACK_REQUESTS, MAX_FEEDBACK_TEXT,
// MAX_FEEDBACK_ID in @wso2/prototype-kit/feedback, which the agents service and
// the console share) and the contract's PrototypeFeedbackInput. Lengths are
// counted as the kit counts them, in UTF-16 code units (see utf16Len). The
// shared table packages/prototype-kit/test/fixtures/feedback-cases.json holds
// the rules and limits on both sides (prototype_feedback_test.go).
const (
	maxFeedbackRequests = 50
	maxFeedbackText     = 4000
	maxFeedbackID       = 200
)

// prototypeCommand is the one command a feedback batch may ride.
const prototypeCommand = "/prototype"

var (
	// feedbackHashPattern is the kit's prototype hash: 64 lowercase hex.
	feedbackHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// feedbackComponentPattern keeps the component one path segment of
	// specs/design/components/<component>/. Mirrors the contract's pattern and
	// the agents service's guard.
	feedbackComponentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
)

// prototypeFeedbackFromJSON checks the request's batch and converts it into the
// agents-service wire block, or returns nil when the turn carries none.
//
// Where it may ride: a room turn (collab) whose command is `/prototype`, and
// never together with an aim. Room turns only because the room's committer is
// the one path an agent's revision reaches git by; an aim aims at a selection
// in a document, the batch at stable prototype ids, and a turn has one aim.
// The instruction is the bare command or the command followed by the batch's
// own component, which is what the console sends.
func prototypeFeedbackFromJSON(instruction string, collab bool, aimed bool, fb *gen.PrototypeFeedbackInput) (*agentsvc.PrototypeFeedbackBlock, error) {
	if fb == nil {
		return nil, nil
	}
	if err := checkFeedbackPlacement(instruction, collab, aimed, fb.Component); err != nil {
		return nil, err
	}
	if !feedbackHashPattern.MatchString(fb.PrototypeHash) {
		return nil, apierr.BadRequest("prototypeFeedback.prototypeHash must be the 64-character lowercase hex revision hash")
	}
	if len(fb.Component) > maxFeedbackID || !feedbackComponentPattern.MatchString(fb.Component) {
		return nil, apierr.BadRequest("prototypeFeedback.component must name a web-application component")
	}
	if len(fb.Requests) == 0 || len(fb.Requests) > maxFeedbackRequests {
		return nil, apierr.BadRequest(fmt.Sprintf("prototypeFeedback.requests must hold 1 to %d requests", maxFeedbackRequests))
	}
	block := &agentsvc.PrototypeFeedbackBlock{
		PrototypeHash: fb.PrototypeHash,
		Component:     fb.Component,
		Requests:      make([]agentsvc.PrototypeFeedbackRequest, 0, len(fb.Requests)),
	}
	for i, r := range fb.Requests {
		req, err := feedbackRequest(i+1, r)
		if err != nil {
			return nil, err
		}
		block.Requests = append(block.Requests, req)
	}
	return block, nil
}

func checkFeedbackPlacement(instruction string, collab, aimed bool, component string) error {
	words := strings.Fields(instruction)
	if len(words) == 0 || words[0] != prototypeCommand || len(words) > 2 || (len(words) == 2 && words[1] != component) {
		return apierr.BadRequest("prototypeFeedback is only valid on a /prototype instruction, bare or followed by its component")
	}
	if !collab {
		return apierr.BadRequest("prototypeFeedback must be a collab (room) turn: only the room's committer saves the revision, so send it with collab: true")
	}
	if aimed {
		return apierr.BadRequest("prototypeFeedback cannot be combined with anchor and intent")
	}
	return nil
}

func feedbackRequest(n int, r gen.PrototypeFeedbackRequest) (agentsvc.PrototypeFeedbackRequest, error) {
	bad := func(field, rule string) (agentsvc.PrototypeFeedbackRequest, error) {
		return agentsvc.PrototypeFeedbackRequest{}, apierr.BadRequest(fmt.Sprintf("prototypeFeedback.requests[%d].%s %s", n, field, rule))
	}
	for _, id := range []struct{ name, value string }{{"screenId", r.ScreenID}, {"roleId", r.RoleID}, {"stateId", r.StateID}} {
		if !validFeedbackID(id.value) {
			return bad(id.name, fmt.Sprintf("must be a non-empty string of at most %d characters", maxFeedbackID))
		}
	}
	if r.FlowID != "" && !validFeedbackID(r.FlowID) {
		return bad("flowId", fmt.Sprintf("must be a non-empty string of at most %d characters", maxFeedbackID))
	}
	for _, id := range r.ElementIds {
		if !validFeedbackID(id) {
			return bad("elementIds", fmt.Sprintf("must hold non-empty strings of at most %d characters", maxFeedbackID))
		}
	}
	if strings.TrimSpace(r.Text) == "" {
		return bad("text", "is empty")
	}
	if utf16Len(r.Text) > maxFeedbackText {
		return bad("text", fmt.Sprintf("exceeds %d characters", maxFeedbackText))
	}
	elementIDs := r.ElementIds
	if elementIDs == nil {
		elementIDs = []string{}
	}
	return agentsvc.PrototypeFeedbackRequest{
		ScreenID:   r.ScreenID,
		FlowID:     r.FlowID,
		RoleID:     r.RoleID,
		StateID:    r.StateID,
		ElementIDs: elementIDs,
		Text:       r.Text,
	}, nil
}

func validFeedbackID(id string) bool {
	return strings.TrimSpace(id) != "" && utf16Len(id) <= maxFeedbackID
}

// utf16Len is a string's length as JavaScript's `string.length` reads it, in
// UTF-16 code units: a character outside the Basic Multilingual Plane (an
// emoji) counts two. The kit, the agents service and the console count so, and
// a batch must be judged the same wherever it is checked.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2 // a surrogate pair
		} else {
			n++
		}
	}
	return n
}
