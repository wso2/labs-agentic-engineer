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
 *   3. skill-load / chat  — /spec, /design, /grilling, /<skill> become a FLOW
 *                           turn via the shared `parseFlowCommand`; anything
 *                           else is a plain chat turn sent verbatim.
 *
 * Phase-runners are matched BEFORE the generic skill loader so `/task` runs the
 * task-plan phase rather than becoming "Load the task skill and follow it".
 */

import { parseStartCommand, parseFlowCommand } from "@aep/contracts/commands";
import type { TurnSpec } from "@aep/agent-stream";
import { chatSpec, flowSpec } from "../engine/turn-spec.js";

export type PhaseName = "task" | "code" | "wire" | "validate" | "undo";

export type ChatIntent =
  | { kind: "control"; name: "menu" | "quit" | "help" }
  | { kind: "phase"; name: PhaseName; arg?: string }
  | { kind: "start"; inlineIdea?: string }
  | { kind: "turn"; turn: TurnSpec };

const PHASES = new Set<PhaseName>(["task", "code", "wire", "validate", "undo"]);

const isPhase = (s: string): s is PhaseName => (PHASES as Set<string>).has(s);

/** Classify one raw chat line into a control / phase / turn intent. */
export function classifyChatInput(line: string): ChatIntent {
  const trimmed = line.trim();

  if (trimmed === "/menu" || trimmed === "/threads") return { kind: "control", name: "menu" };
  if (trimmed === "/quit") return { kind: "control", name: "quit" };
  if (trimmed === "/help") return { kind: "control", name: "help" };

  // `/start` is resolved before the generic skill loader because it is not a
  // plain skill load: the captured idea has to ride with it, and that read is
  // I/O this pure classifier must not do. The caller
  // (chat.ts) resolves the idea and builds the spec via `startSpec`. The
  // grammar itself is shared via @aep/contracts/commands.
  const start = parseStartCommand(trimmed);
  if (start) return start.inlineIdea ? { kind: "start", inlineIdea: start.inlineIdea } : { kind: "start" };

  // A phase-runner is `/<name>` with an optional argument, where <name> is one
  // of the reserved phases (pure letters, so `/code-all` is NOT a phase — it
  // falls through to the skill loader).
  const m = /^\/([a-z]+)(?:\s+(\S[\s\S]*))?$/.exec(trimmed);
  const name = m?.[1];
  if (name && isPhase(name)) {
    const arg = m?.[2]?.trim();
    return arg ? { kind: "phase", name, arg } : { kind: "phase", name };
  }

  const flow = parseFlowCommand(trimmed);
  return { kind: "turn", turn: flow ? flowSpec(flow.skill, flow.text) : chatSpec(trimmed) };
}
