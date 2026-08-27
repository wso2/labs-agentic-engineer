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

import { ANSWER_PREFIX, ANSWERS_PREFIX } from "@aep/agent-stream";
import { REGISTER_EXTERNAL_RESOURCE_COMMAND } from "@aep/contracts/commands";
import { MARKETPLACE_CHAT_PROJECT } from "../../features/marketplace/constants";

const UNSURE_REMAINDER = "an API";

/** Stripe-like sure draft. Never includes env values or secret bytes. */
const SURE_DRAFT_INPUT = {
  name: "stripe",
  description: "Payments API",
  consumptionInstructions: "Use the secret key as Bearer.",
  config: [{ key: "API_KEY", description: "Secret API key", secret: true }],
  resourceDocs: [{ type: "openapi", url: "https://example.com/stripe/openapi.yaml" }],
};

/** Same shape as the grill `ask_question` payload in agent-chat.ts. */
const UNSURE_QUESTION = {
  question: "What should this Registered External resource be called?",
  detail:
    "A vague 'an API' is not enough to draft a schema — the name is the resource identity, so I'm asking it first.",
  options: [
    {
      label: "stripe",
      description:
        "A payments API. Consumption uses a secret key as Bearer; the value stays on the form, not in chat.",
      recommended: true,
    },
    {
      label: "Something else",
      description: "Type the resource name in the text field below.",
      freeText: true,
    },
  ],
  multiSelect: false,
};

function isAnswer(instruction: string): boolean {
  return instruction.startsWith(ANSWER_PREFIX) || instruction.startsWith(ANSWERS_PREFIX);
}

function sureDraftFrames(turnId: string): unknown[] {
  return [
    {
      type: "text-delta",
      delta:
        "I'll register this as a Registered External resource. If anything is unclear I'll ask — I won't invent a schema or put secret values in the draft.",
    },
    { type: "tool-input-start", id: `draft-${turnId}`, toolName: "draftExternalResource" },
    {
      type: "tool-call",
      toolCallId: `draft-${turnId}`,
      toolName: "draftExternalResource",
      input: SURE_DRAFT_INPUT,
    },
    { type: "turn-committed", noChanges: true },
  ];
}

function unsureQuestionFrames(turnId: string): unknown[] {
  return [
    {
      type: "text-delta",
      delta:
        "That is too vague to draft a Registered External resource — I need to ask first. I will not invent a schema or put secret values in the draft.",
    },
    { type: "tool-call", toolCallId: `q-${turnId}`, toolName: "ask_question", input: UNSURE_QUESTION },
    {
      type: "tool-result",
      toolName: "ask_question",
      toolCallId: `q-${turnId}`,
      input: UNSURE_QUESTION,
      output: { status: "awaiting_user_response", question: UNSURE_QUESTION.question },
    },
    { type: "turn-committed", noChanges: true },
  ];
}

/**
 * Scripted Marketplace register stream. Returns null when this turn is not
 * a register-command or a marketplace answer (real-project answers stay on
 * the /start grill path).
 */
export function registerChatFrames(
  instruction: string,
  projectName?: string,
  turnId = "mock-turn",
): unknown[] | null {
  const trimmed = instruction.trim();
  if (trimmed.startsWith(REGISTER_EXTERNAL_RESOURCE_COMMAND)) {
    const remainder = trimmed.slice(REGISTER_EXTERNAL_RESOURCE_COMMAND.length).trim();
    if (remainder === UNSURE_REMAINDER) {
      return unsureQuestionFrames(turnId);
    }
    return sureDraftFrames(turnId);
  }
  if (projectName === MARKETPLACE_CHAT_PROJECT && isAnswer(instruction)) {
    return sureDraftFrames(turnId);
  }
  return null;
}
