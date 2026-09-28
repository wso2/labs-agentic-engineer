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

// Scoping the prompt appendix to the LEAD, and recording what each session got.
//
// OpenCode 1.18.32 adds every `instructions` file to EVERY session's system
// prompt (session/prompt.ts, the main loop's `instruction.system()`), so a
// builder subagent would read the lead's whole workflow and fan-out glossary.
// Claude Code's preset `append` reaches the lead only. This restores that
// parity: the file stays the carrier (so its place in the lead's prompt is
// unchanged), and the plugin removes it from the system prompt of any session
// whose agent is not the primary one, in `experimental.chat.system.transform`
// (session/llm/request.ts), the last point before the provider call.
//
// The transform is told the session, not the agent, so the agent comes from
// `chat.message`, which fires when a session's user message is created — before
// that session's first model call. A session never seen there is treated as a
// subagent: the appendix is withheld unless the agent is KNOWN to be the lead.
//
// OpenCode formats each file as `Instructions from: <path>\n<content>` and
// joins the parts with "\n"; the block is matched by its content, not its path,
// because the path it prints is its own resolution of ours.

import type { SessionContextRecord } from "../../../lib/run_context.js";

const HEADER = "Instructions from: ";

/** `text` with the instructions block carrying exactly `appendix` removed, or `text` unchanged. */
export function withoutAppendix(text: string, appendix: string): string {
  if (appendix === "") return text;
  let from = 0;
  for (;;) {
    const header = text.indexOf(HEADER, from);
    if (header < 0) return text;
    const eol = text.indexOf("\n", header);
    if (eol < 0) return text;
    if (text.startsWith(appendix, eol + 1)) {
      const start = header > 0 && text[header - 1] === "\n" ? header - 1 : header;
      return text.slice(0, start) + text.slice(eol + 1 + appendix.length);
    }
    from = eol + 1;
  }
}

export interface SessionContextInputs {
  /** The instructions file's content, as OpenCode reads it. */
  appendix: string;
  /** The one agent that keeps it. */
  lead: string;
  record: (record: SessionContextRecord) => void;
}

export interface SessionContext {
  noteAgent(sessionID: string, agent: string | undefined): void;
  /** Mutates `system` in place, as the hook's output is read after it returns. */
  shapeSystem(sessionID: string, system: string[]): void;
  noteSkill(sessionID: string, skill: string): void;
}

export function createSessionContext(inputs: SessionContextInputs): SessionContext {
  const agents = new Map<string, string>();
  // Once per session, at its first model call — the main loop's. A later call
  // in the same session can be the compaction summariser, which carries no
  // instructions at all and would misreport the lead.
  const seen = new Set<string>();
  return {
    noteAgent(sessionID, agent) {
      if (sessionID && agent && !agents.has(sessionID)) agents.set(sessionID, agent);
    },
    shapeSystem(sessionID, system) {
      const agent = agents.get(sessionID);
      if (agent !== inputs.lead) {
        for (let i = 0; i < system.length; i++) system[i] = withoutAppendix(system[i], inputs.appendix);
      }
      if (!sessionID || seen.has(sessionID)) return;
      seen.add(sessionID);
      const appendix = inputs.appendix !== "" && system.some((part) => part.includes(inputs.appendix));
      inputs.record({ session: sessionID, agent: agent ?? "unknown", appendix });
    },
    noteSkill(sessionID, skill) {
      if (skill) inputs.record({ session: sessionID, skill });
    },
  };
}
