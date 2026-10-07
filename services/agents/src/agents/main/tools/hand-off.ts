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

/**
 * The hand-off tool the main agent calls (the `files` tool set): it passes the
 * user's issue report to the Issues view's agent. Calling it does NOT run that
 * agent. The call ends the turn waiting for the user (`handOffStop`), and the
 * console offers to open the Issues chat with `request`; the Issues agent runs
 * on the user's next message there.
 */

import { tool } from "ai";
import type { Tool } from "ai";
import { z } from "zod";
import {
  ANSWER_PREFIX,
  ANSWERS_PREFIX,
  HAND_OFF_TOOLS,
  type Equal,
  type HandOffInput,
  type HandOffResult,
} from "@aep/agent-stream";

export const handOffInputSchema = z.object({
  request: z
    .string()
    .min(1)
    .max(2000)
    // Defence in depth: the console passes the request on as an `/issue`
    // report, but a request shaped as an answer ("Answer to …", "Answers:")
    // could otherwise pose as the user's go-ahead to file.
    .refine((request) => !posesAsAnswer(request), {
      message: "The request is the user's own words, not an answer to a question.",
    })
    .describe("The user's own words, unchanged — what they reported or asked for. Never a draft of the issue."),
});

/** True when `request` reads as an answer the console sends for a question card. */
function posesAsAnswer(request: string): boolean {
  const text = request.trim();
  return text.startsWith(ANSWER_PREFIX) || text.startsWith(ANSWERS_PREFIX);
}

// Drift guard: the schema's inferred input stays equal to the wire type.
const _drift: [Equal<z.infer<typeof handOffInputSchema>, HandOffInput>] = [true];
void _drift;

const HAND_OFF_DESCRIPTIONS: Record<keyof typeof HAND_OFF_TOOLS, string> = {
  issues:
    "Hand the user's message to the Issues chat. Use it when the user reports something broken in the app, " +
    "asks for a capability the app lacks, as a work item, wants an issue filed or found, or starts a message " +
    "with `/issue`. Not for changes the user wants made to the spec or design — make those yourself; hand off " +
    "only to record or find a work item (a bug, a missing capability, an improvement) in the Issues chat. " +
    "Pass their own words as `request`. Never draft or file an issue yourself, and do not search for issues. " +
    "Ends your turn; the user decides whether to continue in Issues.",
};

/** The hand-off tools, one per view, keyed by wire tool name. */
export function handOffTools(): Record<string, Tool> {
  const tools: Record<string, Tool> = {};
  for (const [view, name] of Object.entries(HAND_OFF_TOOLS) as [keyof typeof HAND_OFF_TOOLS, string][]) {
    tools[name] = tool({
      description: HAND_OFF_DESCRIPTIONS[view],
      inputSchema: handOffInputSchema,
      execute: async (): Promise<HandOffResult> => ({ status: "awaiting_handoff", view }),
    });
  }
  return tools;
}
