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
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/prototypespec"
)

// A prototype review batch on a create-turn request (#817): the reviewer's
// queued requests on ONE web-application prototype, sent as a single
// `/prototype` turn so the agent rewrites the file once.
//
// Validated here and forwarded UNCHANGED — the agents service words it, this
// side never does (the same rule as the aim, and the reason neither ever rides
// `instruction`). The create-turn route skips the edge's schema check (it takes
// multipart too, see edge/validator.go), so every contract bound is enforced
// here, before a turn row exists: a malformed batch is a 400, never a turn that
// fails later with nobody told why.

// prototypeCommand is the one instruction a batch may ride — exactly the
// command, nothing after it (@aep/contracts/commands PROTOTYPE_COMMAND).
const prototypeCommand = "/prototype"

// The contract's ceilings (PrototypeFeedbackInput / PrototypeAnnotationInput),
// mirrored by @aep/agent-stream's PROTOTYPE_FEEDBACK_LIMITS.
const (
	maxPrototypeAnnotations  = 50
	maxPrototypeComponentIDs = 50
	maxPrototypeAnnotationID = 128
	maxPrototypeModelIDLen   = 200
	maxPrototypeRequestLen   = 4000
)

// prototypeFeedbackFromJSON converts the request's batch into the agents-service
// wire block, or nil when the turn carries none. aimed says the request also
// carries document aiming (anchor/intent), which a batch excludes.
func prototypeFeedbackFromJSON(fb *gen.PrototypeFeedbackInput, instruction string, aimed bool) (*agentsvc.PrototypeFeedbackBlock, error) {
	if fb == nil {
		return nil, nil
	}
	if strings.TrimSpace(instruction) != prototypeCommand {
		return nil, apierr.BadRequest("prototypeFeedback is only valid on a /prototype instruction")
	}
	if aimed {
		return nil, apierr.BadRequest("prototypeFeedback cannot be combined with anchor or intent")
	}
	if !isPrototypePath(fb.PrototypePath) {
		return nil, apierr.BadRequest("prototypeFeedback.prototypePath must be specs/design/components/<component>/prototype.json")
	}
	if len(fb.Annotations) == 0 || len(fb.Annotations) > maxPrototypeAnnotations {
		return nil, apierr.BadRequest(fmt.Sprintf("prototypeFeedback.annotations must hold 1 to %d annotations", maxPrototypeAnnotations))
	}
	seen := make(map[string]bool, len(fb.Annotations))
	annotations := make([]agentsvc.PrototypeAnnotationBlock, 0, len(fb.Annotations))
	for _, a := range fb.Annotations {
		block, err := prototypeAnnotation(a)
		if err != nil {
			return nil, err
		}
		if seen[a.ID] {
			return nil, apierr.BadRequest("prototypeFeedback has a duplicate annotation id: " + a.ID)
		}
		seen[a.ID] = true
		annotations = append(annotations, block)
	}
	return &agentsvc.PrototypeFeedbackBlock{PrototypePath: fb.PrototypePath, Annotations: annotations}, nil
}

func prototypeAnnotation(a gen.PrototypeAnnotationInput) (agentsvc.PrototypeAnnotationBlock, error) {
	if blank(a.ID) || blank(a.ScreenID) || blank(a.StateID) || blank(a.Request) {
		return agentsvc.PrototypeAnnotationBlock{}, apierr.BadRequest("every prototype annotation needs an id, screenId, stateId and request")
	}
	if int(a.PrototypeSchemaVersion) != prototypespec.SchemaVersion {
		return agentsvc.PrototypeAnnotationBlock{}, apierr.BadRequest(fmt.Sprintf("prototype annotation prototypeSchemaVersion must be %d", prototypespec.SchemaVersion))
	}
	if a.FlowID != nil && blank(*a.FlowID) {
		return agentsvc.PrototypeAnnotationBlock{}, apierr.BadRequest("prototype annotation flowId must be a flow id or null")
	}
	if len(a.ComponentIds) > maxPrototypeComponentIDs {
		return agentsvc.PrototypeAnnotationBlock{}, apierr.BadRequest(fmt.Sprintf("a prototype annotation holds at most %d componentIds", maxPrototypeComponentIDs))
	}
	for _, c := range a.ComponentIds {
		if blank(c) || len(c) > maxPrototypeModelIDLen {
			return agentsvc.PrototypeAnnotationBlock{}, apierr.BadRequest("prototype annotation componentIds must be non-empty ids")
		}
	}
	if len(a.ID) > maxPrototypeAnnotationID || len(a.ScreenID) > maxPrototypeModelIDLen ||
		len(a.StateID) > maxPrototypeModelIDLen || (a.FlowID != nil && len(*a.FlowID) > maxPrototypeModelIDLen) ||
		len(a.Request) > maxPrototypeRequestLen {
		return agentsvc.PrototypeAnnotationBlock{}, apierr.BadRequest("a prototype annotation exceeds the size limit")
	}
	// Never nil on the wire: an empty list is the whole-screen request.
	components := append([]string{}, a.ComponentIds...)
	return agentsvc.PrototypeAnnotationBlock{
		ID:                     a.ID,
		PrototypeSchemaVersion: int(a.PrototypeSchemaVersion),
		ScreenID:               a.ScreenID,
		FlowID:                 a.FlowID,
		StateID:                a.StateID,
		ComponentIDs:           components,
		Request:                a.Request,
	}, nil
}

// isPrototypePath is the contract's pattern, judged by the model's own slot
// rule (prototypespec.BundleComponent — the rule the save gate applies), so a
// batch can only ever name a file the gates recognise as a prototype.
func isPrototypePath(p string) bool {
	key, ok := strings.CutPrefix(p, "specs/design/")
	if !ok {
		return false
	}
	_, ok = prototypespec.BundleComponent(key)
	return ok
}

func blank(s string) bool { return strings.TrimSpace(s) == "" }
