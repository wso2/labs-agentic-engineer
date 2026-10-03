/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

// Multi-user chat scenarios (#130 follow-up: task 2). Scenario switch, same
// convention as `aep:mock:projects` (see fixtures/projects.ts): toggle in
// the browser devtools:
//   localStorage.setItem('aep:mock:chat', 'multiuser' | 'teammate-turn')
import type { components } from "../../generated/ae-design-agent";
import { MOCK_USER } from "../../auth/mockSession";

type TurnStatus = components["schemas"]["TurnStatus"];

export type ChatScenario = "multiuser" | "teammate-turn";

export interface MockChatAuthor {
  id: string;
  displayName: string;
}

interface MockConversationMessage {
  role: string;
  content: unknown;
  author?: MockChatAuthor;
}

// Mirrors the identity useCurrentAuthor() derives for the mock session
// (the session's sub — see currentUser.ts) so "the signed-in user" in the
// mock history is literally the same author the send path stamps.
export const MOCK_CHAT_USER: MockChatAuthor = {
  id: MOCK_USER.id,
  displayName: MOCK_USER.name,
};

export const MOCK_TEAMMATE: MockChatAuthor = {
  id: "u-sarah",
  displayName: "Sarah Perera",
};

export const MOCK_TEAMMATE_TURN_ID = "mock-turn-teammate-1";

// Two authors, including one teammate-initiated (and already-answered) turn.
// The reply's content mixes a text part with a non-text (tool-result) part —
// rehydrate is text-only (see history.ts), so the tool part is inert here,
// same as a real backend's would be.
const settledHistory: MockConversationMessage[] = [
  {
    role: "user",
    content: "Let's scope the checkout flow first.",
    author: MOCK_CHAT_USER,
  },
  {
    role: "assistant",
    content: "Sure — I'll start drafting the requirements doc.",
  },
  {
    role: "user",
    content: "Can you add a returns-policy section too?",
    author: MOCK_TEAMMATE,
  },
  {
    role: "assistant",
    content: [
      { type: "text", text: "Added a returns section to the draft." },
      {
        type: "tool-result",
        toolName: "addFile",
        toolCallId: "tc-history-1",
        output: {
          ok: true,
          op: "edit",
          path: "specs/requirements/prd.md",
        },
      },
    ],
  },
];

/** `multiuser`: settled history, two authors, no running turn. */
export const multiuserHistory: MockConversationMessage[] = settledHistory;

/**
 * `teammate-turn`: same history, plus a teammate-initiated turn that's still
 * running — its triggering message is in history (no reply yet); the reply
 * streams live from the turn endpoints (see activeTeammateTurn below).
 */
export const teammateTurnHistory: MockConversationMessage[] = [
  ...settledHistory,
  {
    role: "user",
    content: "One more thing — can you wire up tax calculation too?",
    author: MOCK_TEAMMATE,
  },
];

/**
 * The running turn a teammate started, for GET .../turns/active, in the
 * project's current thread. No `instruction`: its triggering message is
 * already in the rehydrated history above.
 */
export function activeTeammateTurn(projectName: string, conversationId: string): TurnStatus {
  return {
    turnId: MOCK_TEAMMATE_TURN_ID,
    project: projectName,
    conversationId,
    kind: "browser",
    flow: "",
    status: "running",
    instruction: "",
    authorId: MOCK_TEAMMATE.id,
    authorDisplayName: MOCK_TEAMMATE.displayName,
    createdAt: new Date().toISOString(),
  };
}
