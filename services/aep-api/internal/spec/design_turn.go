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

// The console's "Generate design" button (SpecView) and its Overview-track
// counterpart (the `?generate=design` signal AppLayout hands to the chat
// panel) both used to send the `/design` flow command through the generic
// create-turn edge — the same operation the marketplace registration
// assistant's chat rides. That meant the one AE permission distinguishing
// "may this caller trigger design generation" from "may this caller send any
// chat message at all" didn't exist: create-turn is gated on ae:design OR
// ae:resource-config, so an OR was the only lever available, and narrowing it
// would have taken ae:resource-config's own legitimate chat surface with it.
//
// generate-design is the fix: a dedicated operation, gated on ae:design alone
// (permission_gate.go), whose only content IS the fixed `/design` command —
// nothing a caller supplies can turn it into anything else.

package spec

import "context"

// designCommand triggers the `/design` flow. Unexported: GenerateDesign's
// caller (internal/spec/genaiturns) never composes an instruction string of
// its own — it asks this package to start the design turn, the same
// information-hiding shape StartKickoff already uses for `/start`.
const designCommand = "/design"

// StartDesignTurn starts the `/design` flow turn on conversationID, returning
// the turn id.
//
// Unlike StartKickoff, this takes the conversation id directly rather than
// resolving "current" itself — GenerateDesign's caller already has it (the
// same path parameter create-turn takes) — and carries no once-only guard:
// regenerating the design is an ordinary, repeatable action, not a
// one-time-per-project event the way the opening kickoff is.
func (s *Service) StartDesignTurn(ctx context.Context, orgID, projectID, conversationID string) (string, error) {
	return s.StartTurn(ctx, orgID, projectID, TurnInput{
		ConversationID: conversationID,
		Instruction:    designCommand,
		Collab:         true,
	})
}
