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
 * Task-plan tool contract — the wire source of truth for the plan turn's domain
 * tools (`planTask`, `updateTask`) and the read-only context convention they run
 * against. Peer of `sse-events.ts`: this module owns the TYPES (inputs, results,
 * error codes, tool names, the `tasks/<issueNumber>.md` rendering shape); the Zod
 * `inputSchema`s live in `../task-tools-schema.ts` (drift-guarded against these
 * types) and the runtime parser in `../task-context.ts`. The agents service, its
 * evals, the console, and the Go BFF plan tap all speak this one definition.
 *
 * ## The task-plan turn's `files` = READ-ONLY context (a §13 default)
 *
 * On a `toolset: "task-plan"` turn nothing mutates `files`; it carries the
 * planner's CURRENT STATE, assembled by the caller (the BFF; the eval mirrors it):
 *
 *  - the spec/design bundle at its tags (`specs/…`), unchanged from a files turn;
 *  - one rendering file **per existing open Task** at `tasks/<issueNumber>.md`.
 *
 * From that snapshot the plan derives two facts, each with ONE definition here so
 * the service's tool validation and the Go assembler render against the same rule:
 *
 *  - **Known components** — the directory names under
 *    `specs/design/components/<name>/…` (`parseKnownComponents`). A `planTask`
 *    may only target a known component, and `dependsOn` names them.
 *  - **Existing Tasks** — the `tasks/<issueNumber>.md` files, each a YAML
 *    frontmatter block (`TaskContextFile`) followed by the Task's markdown body
 *    (`parseTaskContextFile`). A malformed rendering degrades gracefully: it is
 *    skipped and simply isn't addressable, never crashing the turn.
 *
 * ## Result shapes are self-contained (like `OpOk`/`OpErr`, `LoadSkillResult`)
 *
 * The BFF acts on paired tool-call/tool-result frames as they stream, so a
 * SUCCESS echoes the normalized operation (and, for `updateTask`, the resolved
 * ref) — enough to perform the GitHub write without re-deriving from the input —
 * and a FAILURE carries `code` + `message` + a self-correction payload (known
 * components / addressable refs / the cycle path) so the model self-corrects
 * in-turn and the BFF skips the rejected call.
 */

// --- Tool names (frame routing for the BFF plan tap + the console) ----------

/** Create a Task for one design component. */
export const PLAN_TASK = "planTask" as const;
/** Patch a planned-this-turn Task (by title) or an existing Task (by issueNumber). */
export const UPDATE_TASK = "updateTask" as const;

// --- Shared domain values ----------------------------------------------------

/** Where a Task came from. Non-routing metadata; defaults to `spec-plan`. */
export type TaskOrigin = "spec-plan" | "incident" | "manual";

/** The two task-plan operations (mirrors `Op` for file tools). */
export type TaskOp = "plan" | "update";

/** Tool-side validation failures the accumulator returns for in-turn self-correction. */
export type TaskToolErrCode =
  | "UNKNOWN_COMPONENT"
  | "UNKNOWN_REF"
  | "DUPLICATE_TITLE"
  | "DEPENDENCY_CYCLE";

// --- Per-tool input shapes (the `tool-call.input` value) --------------------
//
// WIRE source of truth. The Zod `inputSchema`s in `../task-tools-schema.ts`
// carry a compile-time assert that `z.infer<schema>` stays equal to these.
// Property order there is load-bearing for streaming UX (short routable fields
// first, long strings last) — see that module.

export interface PlanTaskInput {
  /** The design component this Task implements — MUST be a known component. */
  component: string;
  /** Human-facing title; unique (case-insensitive, trimmed) across planned + existing Tasks. */
  title: string;
  /** Dependencies as component names (never issue numbers). */
  dependsOn: string[];
  /** Defaults to `spec-plan` when omitted. */
  origin?: TaskOrigin;
  /** One sentence: why this Task exists. */
  rationale: string;
  /**
   * The feature this Task builds in its component ("F2"), or "foundation" for
   * the component's shared setup and carried product-wide requirements (B3).
   */
  feature?: string;
}

/**
 * How an `updateTask` names its target: `issueNumber` refs a pre-existing Task
 * from the pushed context; `title` refs a Task planned earlier in the SAME turn.
 */
export type TaskRef = { issueNumber: number } | { title: string };

/** The fields an `updateTask` may patch (all optional; `body` is the long one). */
export interface UpdateTaskSet {
  title?: string;
  dependsOn?: string[];
  rationale?: string;
  body?: string;
}

export interface UpdateTaskInput {
  ref: TaskRef;
  set: UpdateTaskSet;
}

// --- Result payloads (the `tool-result.output` value) -----------------------

/** A successful `planTask` — the normalized operation the BFF creates an issue from. */
export interface PlanTaskOk {
  ok: true;
  op: "plan";
  component: string;
  title: string;
  dependsOn: string[];
  /** Normalized: the default `spec-plan` is filled in. */
  origin: TaskOrigin;
  rationale: string;
  /** As PlanTaskInput.feature; absent when the planner gave none. */
  feature?: string;
}

/**
 * A successful `updateTask` — the resolved ref (canonical: the matched Task's
 * pre-rename identity) plus the validated patch the BFF applies to its issue.
 */
export interface UpdateTaskOk {
  ok: true;
  op: "update";
  ref: TaskRef;
  set: UpdateTaskSet;
}

/**
 * A failed task-plan op. Carries `code` + `message` plus the one self-correction
 * payload its code implies (the others stay absent) — mirrors `OpErr`.
 */
export interface TaskToolErr {
  ok: false;
  op: TaskOp;
  code: TaskToolErrCode;
  message: string;
  /** UNKNOWN_COMPONENT: the component names the plan may target. */
  knownComponents?: string[];
  /** UNKNOWN_REF: what IS addressable — existing issue numbers + this-turn titles. */
  addressable?: AddressableRefs;
  /** DUPLICATE_TITLE: the titles already taken (planned + existing). */
  takenTitles?: string[];
  /** DEPENDENCY_CYCLE: the component-name cycle the change would introduce. */
  cyclePath?: string[];
}

/** The addressable refs echoed on an UNKNOWN_REF miss (cf. `LoadSkillResult.available`). */
export interface AddressableRefs {
  /** issueNumbers of the existing open Tasks in context. */
  issueNumbers: number[];
  /** titles of Tasks planned earlier this turn. */
  plannedTitles: string[];
}

export type PlanTaskResult = PlanTaskOk | TaskToolErr;
export type UpdateTaskResult = UpdateTaskOk | TaskToolErr;

// --- The read-only context rendering (`tasks/<issueNumber>.md`) -------------

/**
 * The parsed frontmatter of a `tasks/<issueNumber>.md` rendering. The markdown
 * `body` after the fence is the Task's editable body. Optional fields tolerate a
 * partially-rendered Task; the Go assembler renders against this shape.
 */
export interface TaskContextFile {
  issueNumber: number;
  component: string;
  title: string;
  dependsOn: string[];
  origin: TaskOrigin;
  specTag?: string;
  designTag?: string;
  /** The markdown body after the frontmatter fence. */
  body: string;
}
