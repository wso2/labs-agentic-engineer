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
 * "Draft first, act on the user's go-ahead", enforced in code. An agent that
 * writes somewhere the user can see (an issue filed, a comment posted) asks
 * first with a question card, and the prompt says to act only on the answer.
 * But anything the model reads (an issue's body, a comment, text the user
 * pasted) can try to talk it past the question. So a write tool is gated on
 * THIS turn's instruction being the user's own answer, which only the console's
 * question card produces, and runs at most once for it.
 *
 * The gates themselves (`issues/filing-gate.ts`, `issue/confirm-gate.ts`) say
 * which tool waits for which answer; this module is how every gate reads an
 * answer and holds a tool.
 */

import type { ToolSet } from "ai";
import { buildAnswerInstruction } from "@aep/agent-stream";

type GatedTool = ToolSet[string];

/** What follows the label when the user added a note to their answer. */
const NOTE_SEPARATOR = " — ";

/**
 * Is `instruction` the user's answer to `question` selecting exactly `option`?
 * Only the single-answer serialization (`buildAnswerInstruction`), anchored at
 * the START of the instruction, with nothing after the label or a note. The
 * batch form is deliberately not accepted: its lines (and any note, which may
 * span lines) can be forged from text the user pastes, so a gate that scanned
 * for them would disagree with the serializer. A batch answer simply gets the
 * refusal, and the agent re-asks with ask_question. Another label ("File it
 * later"), another question, or chat that merely contains the words does not
 * count.
 */
export function answeredWith(instruction: string, question: string, option: string): boolean {
  const single = buildAnswerInstruction(question, [option]);
  const text = instruction.trim();
  return text === single || text.startsWith(single + NOTE_SEPARATOR);
}

/**
 * `tool` unconfirmed: it keeps its description and schema, so the model still
 * knows it exists, but every call fails with `message`, a tool error telling
 * the model what to ask first.
 */
export function refusing(tool: GatedTool, message: string): GatedTool {
  return {
    ...tool,
    execute: async () => {
      throw new Error(message);
    },
  } as GatedTool;
}

/**
 * `tool` confirmed: it runs at most ONCE. The user's go-ahead covers one
 * write, and injected text in an earlier tool result (or a retry after a
 * timeout that did write) must not make a second. A failed first attempt
 * counts, so the agent tells the user and asks again; later calls fail with
 * `message`. The once-state lives in the returned tool, so build it per turn.
 * A tool with no `execute` comes back unchanged.
 */
export function once(tool: GatedTool, message: string): GatedTool {
  const execute = tool.execute;
  if (execute === undefined) return tool;
  let attempted = false;
  return {
    ...tool,
    execute: async (...args: Parameters<typeof execute>) => {
      if (attempted) throw new Error(message);
      attempted = true;
      return await execute(...args);
    },
  } as GatedTool;
}
