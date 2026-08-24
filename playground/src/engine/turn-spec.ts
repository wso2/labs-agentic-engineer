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
 * Turn specs — what a playground turn is FOR.
 *
 * The playground plays the server's role (it talks to the agents service
 * directly, with no aep-api in between), which means it decides the FACTS of a
 * turn: which flow, which idea, which existing Tasks are context. It composes
 * no prompt text at all — the agents service does that
 * (`services/agents/src/prompts/turn.ts`), which is what makes a playground run
 * and a production turn the same turn.
 *
 * This module used to be `engine/compose.ts`, a mirror of the Go composer built
 * from generated strings so the two could not drift. There is nothing left to
 * mirror: the wording exists once, in the service that sends it to the model.
 * See `services/agents/design/ADR-0003`.
 */

import type { PlanContextFile, TurnSpec } from "@aep/agent-stream";

/** An ordinary chat line, sent as the user typed it. */
export function chatSpec(text: string): TurnSpec {
  return { kind: "chat", text };
}

/**
 * A `/<skill>` flow, with any trailing text the user typed after it. References
 * ride exactly as on `startSpec` — a flow generates artifacts that must be
 * grounded in an attached sketch or document; no documents, no key.
 */
export function flowSpec(skill: string, text?: string, references?: string[]): TurnSpec {
  const spec: TurnSpec = text?.trim() ? { kind: "flow", skill, text: text.trim() } : { kind: "flow", skill };
  return references?.length ? { ...spec, references } : spec;
}

/**
 * The kickoff. A blank idea carries nothing — the start skill then opens by
 * asking for one, exactly as it does for a project with no descriptor. The
 * attached reference documents ride the same way: no documents, no key, so the
 * turn is byte-identical to one from before that channel existed (aep-api's
 * `references` field is `omitempty` for the same reason).
 */
export function startSpec(idea: string | null | undefined, references?: string[]): TurnSpec {
  const trimmed = (idea ?? "").trim();
  const spec: TurnSpec = trimmed === "" ? { kind: "start" } : { kind: "start", idea: trimmed };
  return references?.length ? { ...spec, references } : spec;
}

/**
 * A plan turn. The existing-Task renders are platform state — they cannot ride
 * the workspace snapshot, so they travel on the turn, exactly as aep-api sends
 * them in production (`planContextFor`, internal/delivery/task/plan.go).
 */
export function planSpec(contextFiles: Record<string, string>): TurnSpec {
  const paths = Object.keys(contextFiles).sort();
  if (paths.length === 0) return { kind: "plan" };
  const taskContext: PlanContextFile[] = paths.map((path) => ({ path, body: contextFiles[path]! }));
  return { kind: "plan", taskContext };
}
