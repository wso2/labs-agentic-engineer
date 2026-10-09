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
 * Chat is the playground's control surface: one typed line classified into an
 * intent (pure — no I/O, so the taxonomy is unit-pinned). Precedence is
 * load-bearing:
 *
 *   1. control words     — /menu, /quit, /help (the loop's own affordances)
 *   2. phase-runners      — /task, /code, /wire, /validate, /undo (invoke the
 *                           existing engine commands, NOT chat turns)
 *   3. a turn             — anything else, sent verbatim: the design agent
 *                           parses `/start`, `/design`, `/<skill>` into a flow
 *                           (as it does for the console), and the rest is chat.
 *
 * Phase-runners are matched BEFORE anything goes to the agent, so `/task` runs the
 * task-plan phase rather than reaching the agent as a `/task` flow.
 */


export type PhaseName = "task" | "code" | "wire" | "validate" | "undo";

export type ChatIntent =
  | { kind: "control"; name: "menu" | "quit" | "help" }
  | { kind: "phase"; name: PhaseName; arg?: string }
  | { kind: "turn"; instruction: string };

const PHASES = new Set<PhaseName>(["task", "code", "wire", "validate", "undo"]);

const isPhase = (s: string): s is PhaseName => (PHASES as Set<string>).has(s);

/** Classify one raw chat line into a control / phase / turn intent. */
export function classifyChatInput(line: string): ChatIntent {
  const trimmed = line.trim();

  if (trimmed === "/menu" || trimmed === "/threads") return { kind: "control", name: "menu" };
  if (trimmed === "/quit") return { kind: "control", name: "quit" };
  if (trimmed === "/help") return { kind: "control", name: "help" };

  // A phase-runner is `/<name>` with an optional argument, where <name> is one
  // of the reserved phases (pure letters, so `/code-all` is NOT a phase — it
  // goes to the agent as a turn).
  const m = /^\/([a-z]+)(?:\s+(\S[\s\S]*))?$/.exec(trimmed);
  const name = m?.[1];
  if (name && isPhase(name)) {
    const arg = m?.[2]?.trim();
    return arg ? { kind: "phase", name, arg } : { kind: "phase", name };
  }

  return { kind: "turn", instruction: trimmed };
}
