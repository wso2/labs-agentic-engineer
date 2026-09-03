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
	"encoding/json"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
)

// The aim on a create-turn request (console #666): what the user pointed at in
// a spec document, and what for.
//
// It LOCATES and never carries content (console ADR-0024) — every field here is
// a NAME the agents service hands the model to find in the document it reads in
// its own turn snapshot. That is also why nothing here is size-checked against
// the instruction budget the way a body field would be: an anchor whose size
// grew with the selection would be the carried content the shape exists to
// avoid, and the contract caps `name` for exactly that reason.

// maxAnchorNodes bounds a single selection. A drag across a whole document is a
// legitimate gesture in the editor but not a legitimate aim: past this many
// nodes the user is pointing at the document, not a passage, and the preamble
// stops naming anything the agent can act on.
const maxAnchorNodes = 50

// Field ceilings, mirroring @aep/agent-stream's TURN_AIM_LIMITS: the anchor
// locates and never carries (console ADR-0024), so every field is bounded —
// a locator that grows with the selection is the carried payload the shape
// exists to avoid, arriving through a hand-built request instead of the
// console. Name's 200 mirrors the contract's maxLength.
const (
	maxAnchorFileLen    = 512
	maxAnchorNameLen    = 200
	maxAnchorKindLen    = 64
	maxAnchorContextLen = 512
)

// aimFromJSON converts the generated request fields into the agents-service
// wire block, or nil when the turn carries no aim.
//
// Both fields or neither: an intent with nothing to point at says nothing, and
// an anchor with no intent leaves the agents service guessing which preamble to
// render. Rejecting the half-set case here keeps that guess from ever existing.
func aimFromJSON(anchor gen.TurnAnchor, intent string) (*agentsvc.AimBlock, error) {
	hasAnchor := strings.TrimSpace(anchor.File) != "" || len(anchor.Nodes) > 0
	hasIntent := strings.TrimSpace(intent) != ""
	if !hasAnchor && !hasIntent {
		return nil, nil
	}
	if !hasAnchor || !hasIntent {
		return nil, apierr.BadRequest("anchor and intent must be sent together")
	}
	if strings.TrimSpace(anchor.File) == "" {
		return nil, apierr.BadRequest("anchor.file is required")
	}
	if len(anchor.File) > maxAnchorFileLen {
		return nil, apierr.BadRequest("anchor.file exceeds the size limit")
	}
	if len(anchor.Nodes) == 0 {
		return nil, apierr.BadRequest("anchor.nodes must not be empty")
	}
	if len(anchor.Nodes) > maxAnchorNodes {
		return nil, apierr.BadRequest("anchor.nodes exceeds the limit")
	}
	if intent != string(gen.TurnInputBodyIntentChange) && intent != string(gen.TurnInputBodyIntentDiscuss) {
		return nil, apierr.BadRequest("intent must be change or discuss")
	}
	nodes := make([]agentsvc.AnchorNodeBlock, 0, len(anchor.Nodes))
	for _, n := range anchor.Nodes {
		if strings.TrimSpace(n.Name) == "" || strings.TrimSpace(n.Kind) == "" {
			return nil, apierr.BadRequest("every anchor node needs a name and a kind")
		}
		if len(n.Name) > maxAnchorNameLen || len(n.Kind) > maxAnchorKindLen || len(n.Context) > maxAnchorContextLen {
			return nil, apierr.BadRequest("an anchor node exceeds the size limit")
		}
		nodes = append(nodes, agentsvc.AnchorNodeBlock{
			Name:    n.Name,
			Kind:    n.Kind,
			Context: n.Context,
		})
	}
	return &agentsvc.AimBlock{
		Anchor: agentsvc.AnchorBlock{File: anchor.File, Nodes: nodes},
		Intent: intent,
	}, nil
}

// parseAnchorField decodes the multipart `anchor` part, which the contract
// declares `application/json`: a nested object has no scalar form, so unlike
// `collab` it cannot ride as a plain form value.
func parseAnchorField(raw string) (gen.TurnAnchor, error) {
	var anchor gen.TurnAnchor
	if strings.TrimSpace(raw) == "" {
		return anchor, nil
	}
	if err := json.Unmarshal([]byte(raw), &anchor); err != nil {
		return anchor, apierr.BadRequest("anchor must be valid JSON")
	}
	return anchor, nil
}
