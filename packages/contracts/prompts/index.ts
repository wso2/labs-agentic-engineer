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
 * The canned phase-generation instructions — ONE source for every client that
 * starts a spec turn (the console's CTAs and the root-level playground; see
 * docs/design/playground.md §9). These are the CLIENT half of the instruction:
 * the server appends its live steering (`steeringByUseCase["general"]` +
 * `collabDepsSteer` + the target suffix — services/aep-api
 * internal/feature/genai) on every turn. Console turns never send a `useCase`,
 * so all of these run as `general` turns server-side.
 *
 * Exported as `@aep/contracts/prompts` — plain source, no build step, so a
 * prompt edit applies on the next playground run.
 */

/**
 * The grilling-interview directive (#270, console ADR-0012): interview-first
 * via the structured `ask_question` tool — one question per turn, options + a
 * recommended answer — with an explicit skip valve (the user can always say
 * "just generate"). Deliberately NOT baked into the generation builders:
 * an interactive client (the console's Generate-spec CTA) opts in with
 * `withGrillingInterview`, so headless/programmatic dispatchers of the same
 * builders (the playground CLI, evals) keep their one-shot, never-interrupted
 * turns.
 */
export const GRILLING_DIRECTIVE =
  "Before writing any files, interview me about this idea with the ask_question tool: " +
  "one question at a time, each with candidate options and the one you recommend, " +
  "working through the idea's ambiguities until the requirements are unambiguous. " +
  "If a grilling skill is available in your catalog, load it first and follow it. " +
  "If I ask you to skip ahead or just generate, stop interviewing and proceed on " +
  "stated assumptions. When the interview is done, proceed with the following. ";

/** Wrap a generation instruction with the interview-first directive (#270). */
export function withGrillingInterview(instruction: string): string {
  return GRILLING_DIRECTIVE + instruction;
}

/**
 * The instruction the "Generate spec" CTA sends into the room turn (#150): an
 * explicit generate command (not the raw idea, which the agent might treat as
 * a chat opener) wrapping the stored create prompt. Falls back to a generic
 * instruction when no prompt was stored (older project / other browser /
 * cleared storage) — the CTA still works and the agent can ask for detail.
 * One-shot by itself; the console wraps it with `withGrillingInterview`.
 */
export function buildSpecGenerationInstruction(prompt: string | null): string {
  const base =
    "Generate a complete requirements specification (requirements/requirements.md) for this project";
  return prompt && prompt.trim()
    ? `${base} based on the following idea:\n\n${prompt.trim()}`
    : `${base}.`;
}

/**
 * The instruction the "Generate / Re-generate design" CTA sends into the room
 * turn (#159): derive the component design from the current requirements. No
 * user prompt — the agent designs FROM the requirements already in the repo;
 * the agent's system prompt carries the design-file structure and schema.
 *
 * The CTA also mints the acceptance oracle in the same turn: console turns
 * run as `general` (no `useCase` is sent), so the server-side design-generate
 * steering that would author `validation-criteria.json` never fires. Asking
 * for it here scopes the oracle to exactly the Generate-design action rather
 * than every chat turn.
 */
export function buildDesignGenerationInstruction(): string {
  return (
    "Generate the complete component design for this project based on the " +
    "current requirements. If a design already exists, regenerate it to match " +
    "the current requirements. Then, as the final step, generate the validation " +
    "criteria."
  );
}
