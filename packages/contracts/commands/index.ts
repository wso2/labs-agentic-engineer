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
 * The `/<skill>` chat-command grammar, shared by every TS surface.
 *
 * This module holds NO prompt text. A command is a keyboard shortcut for "this
 * turn is a flow", and parsing it yields FACTS — which token, which trailing
 * text, which idea — that a caller puts on a `TurnSpec`. The wording those
 * facts become lives in the agents service
 * (`services/agents/src/prompts/turn.ts`), the only thing here that talks to a
 * model. See `services/agents/design/ADR-0003`.
 *
 * Who parses, and why it is not one place:
 *
 *  - `aep-api` (Go) parses in production — it has to, since the token decides
 *    whether to read the project descriptor and whether the turn gets a
 *    BFF-signed MCP bearer. Its copy of the grammar is `slashCommandPattern`
 *    in `internal/spec/start_command.go`.
 *  - The playground and the evals parse here, because they play the server role
 *    when running without the platform.
 *  - The console parses NOTHING: it sends the user's line verbatim and lets the
 *    server classify it.
 *
 * A regex cannot be shared across Go and TS without a generator, and the two
 * never see the same input, so the duplication is deliberate: a divergence
 * shows up as the playground routing a line differently from production, not as
 * a wrong prompt reaching a model.
 *
 * Exported as `@aep/contracts/commands` — plain source, no build step.
 */

/**
 * `/start` — the project kickoff, and the one command carrying state no client
 * has. The BFF enriches it with the idea captured in
 * `specs/.agentic-engineer.toml`, a file no client parses and the agent cannot
 * read (dot-led segments are stripped from every turn snapshot).
 *
 * Note what this deliberately is NOT: a `useCase`. That field is part of the
 * conversation identity, so signalling the kickoff through it would put the
 * turn in a different conversation from the chat around it — and `/start` runs
 * an interview whose answers are ordinary chat turns, which would then land
 * somewhere the questions were never asked.
 */
export const START_COMMAND = "/start";

/** The design CTA's command. */
export const DESIGN_COMMAND = "/design";

/**
 * Parse `/start [idea]`. Returns the inline idea (`""` when the command was
 * bare), or null when the line is not the command at all.
 *
 * The grammar mirrors Go's `parseStartCommand` and is deliberately narrow — the
 * exact token, alone or followed by whitespace — so prose that merely mentions
 * "/start" is never eaten.
 */
export function parseStartCommand(line: string): { inlineIdea: string } | null {
  const trimmed = line.trim();
  if (trimmed === START_COMMAND) return { inlineIdea: "" };
  if (trimmed.startsWith(START_COMMAND + " ")) {
    return { inlineIdea: trimmed.slice(START_COMMAND.length + 1).trim() };
  }
  return null;
}

/**
 * Parse `/<command> [text]` into the flow it names, or null for ordinary chat.
 *
 * The token comes back as `skill` because that is the `TurnSpec` field it
 * fills. Most tokens ARE the skill name; the few that name a branch of one
 * (`/feature` → the amend skill's add-a-feature branch) resolve in the agents
 * service, since which playbook a user's intent means is wording.
 *
 * Deliberately narrow, so real chat is never eaten: a single leading `/`, then
 * a skill-name token (`[a-z0-9-]+`) that must end at whitespace or the message
 * end, optionally followed by free text. A bare `/`, a mid-message slash (`fix
 * the /spec route`), `//spec`, or a trailing-punctuation token (`/spec.`) are
 * all left as literal chat.
 *
 * No catalog lookup: an unknown `<token>` is not an error here. It reaches the
 * agent as a flow whose `loadSkill` reports not-found — a better failure than a
 * client-side allowlist that goes stale against the org's own catalog
 * (validation/autocomplete is parked, wso2/labs-agentic-engineer#325).
 *
 * Callers MUST resolve their own reserved control words (the playground's
 * `/menu`, `/quit`, `/grill`, …) BEFORE calling this — those are
 * surface-specific and not skill loads.
 */
export function parseFlowCommand(line: string): { skill: string; text: string } | null {
  const m = /^\/([a-z0-9-]+)(?:\s+([\s\S]+))?$/.exec(line.trim());
  if (!m) return null;
  return { skill: m[1]!, text: m[2]?.trim() ?? "" };
}
