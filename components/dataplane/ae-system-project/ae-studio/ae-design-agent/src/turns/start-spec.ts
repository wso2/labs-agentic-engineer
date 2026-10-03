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
 * `/<command>` flow commands, recognised in the pod (07 §1; port of aep-api's
 * `A/spec/start_command.go`). Clients send commands verbatim and this turns
 * one into a `TurnSpec`: what the turn is for. The instruction wording is
 * composed later, in `prompts/turn.ts` (ADR-0003).
 *
 * `/start` carries state the agent cannot read itself: the idea captured in
 * `specs/.agentic-engineer.toml`, whose dot-led segment keeps it out of the
 * agent's snapshot reads. The project lookup reads it in ae-studio-tools and
 * answers it as `idea`. The idea only rides the first `/start` turn; after
 * that it is in the conversation history.
 *
 * Flows are not a conversation-identity dimension: a flow runs an interview
 * whose answers are ordinary chat turns in the same conversation.
 */

import type { TurnSpec } from "@aep/agent-stream";
import { REFERENCES_PREFIX } from "../conversation/load-workspace.js";

/** The one token carrying lookup state beyond the token. */
const START_COMMAND = "/start";

/**
 * Deliberately narrow so real chat is never eaten: a single leading `/`, a
 * skill-name token ending at whitespace or the message end, optional free
 * text after it. A bare `/`, a mid-message slash, `//x`, or trailing
 * punctuation on the token all fail the match and pass through as chat.
 */
const SLASH_COMMAND_PATTERN = /^\/([a-z0-9-]+)(?:\s+([\s\S]+))?$/;

/** What the project lookup answered that a turn spec needs. */
export interface TurnLookupInputs {
  /** The descriptor's idea, absent when the project has none. */
  idea?: string;
  /** The stored reference document names. */
  references: readonly string[];
}

/**
 * Classifies a raw instruction. Non-command text is a chat turn with an
 * empty flow, sent verbatim and reference-free (the documents are already in
 * the history from the kickoff). A `/<command>` rides on as its token: most
 * tokens are a skill name, and the few that name a branch of one
 * (`/feature`) resolve in `prompts/turn.ts`. `/start` and flow turns carry
 * the reference documents as their snapshot paths (09 §2), so a flow's
 * artifacts are grounded in what the user attached. `/start` also carries
 * the idea: typed inline wins, else the lookup's; none, and the start skill
 * asks the user.
 */
export function turnSpecFor(raw: string, lookup: TurnLookupInputs): { spec: TurnSpec; flow: string } {
  const m = SLASH_COMMAND_PATTERN.exec(raw.trim());
  if (m === null) return { spec: { kind: "chat", text: raw }, flow: "" };
  const token = m[1] as string;
  const rest = (m[2] ?? "").trim();
  const references = referencePaths(lookup.references);

  if (`/${token}` === START_COMMAND) {
    const idea = (rest !== "" ? rest : (lookup.idea ?? "")).trim();
    return {
      spec: { kind: "start", ...(idea !== "" ? { idea } : {}), ...(references ? { references } : {}) },
      flow: token,
    };
  }
  return {
    spec: { kind: "flow", skill: token, ...(rest !== "" ? { text: rest } : {}), ...(references ? { references } : {}) },
    flow: token,
  };
}

/**
 * What a turn's display record says: the instruction verbatim for every turn
 * but a bare `/start` that resolved an idea. The platform fires that kickoff
 * at project creation, so nobody typed the idea; a transcript opening on
 * `/start` alone would show the user a command they never issued, so the
 * resolved idea is appended. Only the idea the same turn resolved is
 * appended, so the line never claims something the agent did not receive.
 */
export function startTurnSummary(instruction: string, spec: TurnSpec): string {
  if (spec.kind !== "start" || !spec.idea) return instruction;
  if (instruction.trim() === START_COMMAND) return `${START_COMMAND} ${spec.idea}`;
  return instruction;
}

/**
 * The stored reference documents as the sorted paths they occupy in the
 * turn's snapshot, or `undefined` when there are none, so the field drops out
 * of the spec and the turn is identical to one without references.
 */
function referencePaths(names: readonly string[]): string[] | undefined {
  if (names.length === 0) return undefined;
  return names.map((name) => REFERENCES_PREFIX + name).sort();
}
