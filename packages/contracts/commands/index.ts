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
 * The `/<skill>` chat commands the TS clients issue.
 *
 * This module holds NO prompt text and no grammar. A command is a keyboard
 * shortcut for "this turn is a flow"; the design agent parses it into FACTS —
 * which token, which trailing text, which idea — on a `TurnSpec`. The wording
 * those facts become lives in the design agent
 * (`components/dataplane/ae-system-project/ae-studio/ae-design-agent/src/prompts/turn.ts`), the only thing here that talks to a
 * model. See `components/dataplane/ae-system-project/ae-studio/ae-design-agent/design/ADR-0003`.
 *
 * Who parses: the design agent alone (`turns/start-spec.ts` in
 * ae-design-agent). Every client — the console, the playground, the evals —
 * sends the user's line verbatim and lets the agent classify it; this module
 * only names the commands a client itself issues.
 *
 * Exported as `@aep/contracts/commands` — plain source, no build step.
 */

/**
 * `/start` — the project kickoff, and the one command carrying state no client
 * has. The design agent enriches it with the idea captured in
 * `specs/.agentic-engineer.toml` (the project lookup reads it), a file no
 * client parses and the agent's model cannot read (dot-led segments are
 * stripped from every turn snapshot).
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

/** Marketplace register flow. Same family as `/start` / `/design`. */
export const REGISTER_EXTERNAL_RESOURCE_COMMAND = "/register-external-resource";

/**
 * `/interview F<n>` — interview one feature (the `interview` skill). The
 * console sends it when the user starts a feature's interview; the feature is
 * named by its ID, which a rename never changes.
 */
export const INTERVIEW_COMMAND = "/interview";

/** The line that starts a feature's interview. */
export function interviewCommand(featureId: string): string {
  return `${INTERVIEW_COMMAND} ${featureId}`;
}

/** The feature an `/interview` line names, or null for any other line. */
export function parseInterviewCommand(line: string): { featureId: string } | null {
  const m = /^\/interview\s+(F[0-9]+)\s*$/.exec(line.trim());
  return m ? { featureId: m[1]! } : null;
}

/**
 * `/design F1 F2` — design the named features (the `design` skill); a bare
 * `/design` designs every feature that can be designed. The features a run
 * named are what the build later checks each feature's design against.
 */
export function designCommand(featureIds: readonly string[]): string {
  return featureIds.length > 0 ? `${DESIGN_COMMAND} ${featureIds.join(" ")}` : DESIGN_COMMAND;
}

/** The features a `/design` line names (empty for a bare one), or null for any other line. */
export function parseDesignCommand(line: string): { featureIds: string[] } | null {
  const m = /^\/design(?:\s+([\s\S]*))?$/.exec(line.trim());
  if (!m) return null;
  return { featureIds: [...new Set((m[1] ?? "").match(/\bF[0-9]+\b/g) ?? [])] };
}

/**
 * `/prototype <component>` — write or revise the clickable prototype of one
 * web-application component (the `prototype` skill); a bare `/prototype`
 * covers every web application the design has. A review's feedback rides the
 * same command, as the turn's typed `prototypeFeedback`.
 */
export const PROTOTYPE_COMMAND = "/prototype";

/** The line that makes or revises one component's prototype. */
export function prototypeCommand(component: string): string {
  return `${PROTOTYPE_COMMAND} ${component}`;
}

/** The component a `/prototype` line names (null for a bare one), or null for any other line. */
export function parsePrototypeCommand(line: string): { component: string | null } | null {
  const m = /^\/prototype(?:\s+([A-Za-z0-9][A-Za-z0-9_-]*))?\s*$/.exec(line.trim());
  return m ? { component: m[1] ?? null } : null;
}
