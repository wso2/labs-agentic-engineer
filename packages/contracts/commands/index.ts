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
