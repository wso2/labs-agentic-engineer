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
 * What a validation run is doing, per acceptance criterion, while it does it.
 *
 * A validation cycle gets 7200s. Until it merges its pull request and the
 * platform reads `tests/validation/report.json`, the console can say nothing
 * about any individual criterion — every row reads "Pending" for up to two
 * hours. This turns the tool calls the agent is ALREADY making into
 * `progress_item` events, so each row says what is happening to it now.
 *
 * INFERRED, NOT DECLARED. Nothing here asks the agent to report anything. Every
 * status is read off a call it must make anyway to do the work: the test plan it
 * has to commit, the spec file it has to write, the command it has to run. An
 * instruction to "report your progress" is one the agent can quietly skip and
 * nobody would notice for a whole run; a spec file it never writes is a spec
 * file that does not exist.
 *
 * The join key is the criterion id in the SPEC FILENAME and the TEST TITLE —
 * the same key `aep-validation/scripts/generate-report.mjs` joins on. Sharing it
 * is the point: a live row and its eventual report row cannot end up keyed
 * differently, because there is only one way to name a criterion.
 *
 * What this deliberately does NOT do:
 *
 *   - Attribute browser exploration. `playwright-cli` calls carry URLs and
 *     element refs, never a criterion id. Exploration is instead marked by the
 *     spec STUB the skill writes before exploring — a file whose mandatory
 *     `// spec:` header exists but whose body does not yet. That ordering is the
 *     only skill change this needs, and it moves a line the skill already
 *     requires rather than adding one.
 *   - Decide a verdict. `report.json` is the authority (its digest is what the
 *     delivery workflow reads), and these statuses are retired by it the moment
 *     it lands. A row lying for a minute costs nothing; a wrong verdict mints
 *     wrong repair issues.
 */

import type { HookCallback, PreToolUseHookInput } from "@anthropic-ai/claude-agent-sdk";
import type { ProgressItemStatus } from "./progress/schema.js";

/** One row's status changed. */
export interface ProgressItemUpdate {
  itemId: string;
  status: ProgressItemStatus;
}

/**
 * The criterion id, exactly as generate-report.mjs matches it — `AC-001-a`.
 * Not exported as one shared constant with the report generator because that
 * script is a skill asset copied into project repos and this is runner code;
 * the shape is pinned by tests on both sides instead.
 */
const AC_ID = "AC-\\d{3}-[a-z]";

/** `tests/e2e/specs/AC-001-a.spec.ts` and the other extensions Playwright takes. */
const SPEC_FILE = new RegExp(`(${AC_ID})\\.spec\\.[cm]?[tj]sx?$`);

/** Every criterion a spec path or a shell command names. */
const SPEC_MENTION = new RegExp(`(${AC_ID})\\.spec\\.[cm]?[tj]sx?\\b`, "g");

/** A test-plan section header — `## AC-001-a — A text box ...`. */
const PLAN_SECTION = new RegExp(`^##\\s+(${AC_ID})\\b`, "gm");

/** The plan artifact, which SKILL.md requires committed before any spec. */
const PLAN_PATH = /(^|\/)tests\/validation\/test-plan\.md$/;

/**
 * A shell call that actually RUNS tests, rather than one that merely names a
 * spec file. Without this, `cat specs/AC-001-a.spec.ts` would report the
 * criterion as running.
 */
const TEST_RUN = /\b(?:npm|pnpm|yarn)\s+(?:run\s+)?test\b|\bplaywright\s+test\b/;

/** A spec file with a body — Playwright's own entry point, however it is spelled. */
const HAS_TEST_BLOCK = /\btest\s*(?:\.\w+)*\s*\(/;

/** Tools whose input names a file the agent is authoring. */
const WRITE_TOOLS = new Set(["Write", "Edit", "NotebookEdit"]);

interface ItemState {
  last: ProgressItemStatus;
  /**
   * Whether this criterion has ever reached `pass`. It is what separates a
   * heal from a first draft: healing.md scopes a heal to a spec that WORKED and
   * then broke, while authoring.md requires a spec to pass twice consecutively
   * before it counts — so failing on the way to a first pass is the normal
   * path, and calling that healing would make a healthy run read as a
   * struggling one.
   */
  everPassed: boolean;
}

/**
 * Per-run memory: what each criterion last reported, and whether it has ever
 * passed. Small and monotonic — no ordering assumptions, so an agent that
 * batches its work instead of taking one criterion at a time still produces
 * honest rows, just several at once.
 */
export class ValidationProgressState {
  private readonly items = new Map<string, ItemState>();
  /** Tool call id → the criteria that call is running, for its outcome. */
  private readonly runs = new Map<string, string[]>();

  status(itemId: string): ProgressItemStatus | undefined {
    return this.items.get(itemId)?.last;
  }

  /**
   * Whether an edit to this item is a HEAL rather than a first draft: it worked
   * once, and right now it does not.
   *
   * BOTH halves are load-bearing. Without the prior pass, step 6's ordinary
   * write-run-fix loop reads as distress — authoring.md requires a spec to pass
   * twice consecutively, so failing on the way to a first pass is the normal
   * path. Without the current failure, ANY later edit to a working spec reads as
   * a heal: a comment, a lint fix, the polish authoring.md's own second pass
   * invites. Healing is what healing.md scopes it to — a spec that worked and
   * broke — and nothing else.
   */
  isHealing(itemId: string): boolean {
    const state = this.items.get(itemId);
    return state?.everPassed === true && state.last === "fail";
  }

  /**
   * Record a status, returning it only if it is NEWS. A criterion edited five
   * times in a row is one row that has not changed, and five identical lines on
   * the feed would say nothing the first did not.
   */
  record(update: ProgressItemUpdate): ProgressItemUpdate | undefined {
    const prev = this.items.get(update.itemId);
    if (prev?.last === update.status) return undefined;
    this.items.set(update.itemId, {
      last: update.status,
      everPassed: (prev?.everPassed ?? false) || update.status === "pass",
    });
    return update;
  }

  noteRun(toolUseId: string, itemIds: string[]): void {
    if (itemIds.length > 0) this.runs.set(toolUseId, itemIds);
  }

  takeRun(toolUseId: string): string[] | undefined {
    const ids = this.runs.get(toolUseId);
    if (ids) this.runs.delete(toolUseId);
    return ids;
  }
}

function inputString(toolInput: unknown, key: string): string {
  if (!toolInput || typeof toolInput !== "object") return "";
  const v = (toolInput as Record<string, unknown>)[key];
  return typeof v === "string" ? v : "";
}

function matchAll(re: RegExp, text: string): string[] {
  // Fresh lastIndex per call: these are module-level /g regexes and a leftover
  // index silently skips the first match of the next call.
  re.lastIndex = 0;
  const out: string[] = [];
  for (let m = re.exec(text); m !== null; m = re.exec(text)) {
    if (m[1] && !out.includes(m[1])) out.push(m[1]);
  }
  return out;
}

/**
 * What a tool call about to be dispatched says about the criteria it touches.
 *
 * Separated from the hook plumbing so the whole derivation is testable against
 * plain objects — no SDK, no session, no workspace.
 */
export function validationProgressUpdates(
  toolName: string,
  toolInput: unknown,
  state: ValidationProgressState,
): ProgressItemUpdate[] {
  if (toolName === "Bash") {
    const command = inputString(toolInput, "command");
    if (!TEST_RUN.test(command)) return [];
    return matchAll(SPEC_MENTION, command).map((itemId) => ({ itemId, status: "running" as const }));
  }

  if (!WRITE_TOOLS.has(toolName)) return [];

  const filePath = inputString(toolInput, "file_path") || inputString(toolInput, "notebook_path");
  if (filePath === "") return [];

  if (PLAN_PATH.test(filePath)) {
    // The plan lists every criterion the run intends to cover, so one write
    // moves the whole board off "Pending" at once. Only criteria with no status
    // yet: SKILL.md has a re-validation APPEND to this file, and re-announcing
    // a criterion already being authored as `planned` would walk its row
    // backwards.
    return matchAll(PLAN_SECTION, inputString(toolInput, "content"))
      .filter((itemId) => state.status(itemId) === undefined)
      .map((itemId) => ({ itemId, status: "planned" as const }));
  }

  const spec = SPEC_FILE.exec(filePath);
  if (!spec?.[1]) return [];
  const itemId = spec[1];

  // `Write` hands over the whole file, so a stub — the mandatory `// spec:`
  // header with no test body yet — is visible as such. That is the skill's
  // marker for "this criterion has been picked up and is being explored
  // against the live app", which is otherwise the longest unobservable stretch
  // of the run.
  //
  // `Edit` carries old_string/new_string, never the result, so it cannot be
  // classified this way and always counts as work on the body.
  if (toolName === "Write" && !HAS_TEST_BLOCK.test(inputString(toolInput, "content"))) {
    return [{ itemId, status: "exploring" }];
  }

  return [{ itemId, status: state.isHealing(itemId) ? "healing" : "authoring" }];
}

/**
 * How a finished test command settles the criteria it ran.
 *
 * A failing multi-spec call is the one case with no honest answer: the exit
 * code says the batch failed, never which member did. Reporting all of them
 * failed would invent evidence, so they keep their `running` status and are
 * corrected by `report.json` when it lands — which is the authority regardless.
 * SKILL.md runs one spec per call, so this is the rare path, not the normal one.
 */
export function validationRunOutcome(itemIds: string[], ok: boolean): ProgressItemUpdate[] {
  if (ok) return itemIds.map((itemId) => ({ itemId, status: "pass" as const }));
  if (itemIds.length === 1 && itemIds[0]) return [{ itemId: itemIds[0], status: "fail" }];
  return [];
}

export interface ValidationProgressTracker {
  /** PreToolUse hook: the statuses a call announces before it runs. */
  hook: HookCallback;
  /** Called by the SDK translator when a tool call settles. */
  settle(toolUseId: string, ok: boolean): void;
}

/**
 * Build the tracker for ONE validation run. Not reusable across runs — it
 * carries that run's per-criterion history, and mixing two runs would report a
 * heal for a criterion that only ever passed in the other one.
 *
 * `onUpdate` receives each status that is news, so the caller owns emission and
 * this module owns no I/O.
 */
export function createValidationProgressTracker(
  onUpdate: (update: ProgressItemUpdate) => void,
): ValidationProgressTracker {
  const state = new ValidationProgressState();

  const publish = (updates: ProgressItemUpdate[]): void => {
    for (const u of updates) {
      const news = state.record(u);
      if (news) onUpdate(news);
    }
  };

  return {
    hook: async (input) => {
      const hookInput = input as PreToolUseHookInput;
      if (hookInput?.hook_event_name !== "PreToolUse") return {};

      const updates = validationProgressUpdates(hookInput.tool_name, hookInput.tool_input, state);
      if (updates.length === 0) return {};

      // Remembered BEFORE publishing, and keyed by the tool call, because the
      // outcome arrives with nothing but that id — the command's text is long
      // gone by the time the result comes back.
      if (updates[0]?.status === "running") {
        state.noteRun(
          hookInput.tool_use_id,
          updates.map((u) => u.itemId),
        );
      }
      publish(updates);

      // Never a decision. This hook observes; the tools it watches are the ones
      // the run needs, and a progress feature that could block a write would be
      // a worse bargain than no progress feature.
      return {};
    },

    settle: (toolUseId, ok) => {
      const itemIds = state.takeRun(toolUseId);
      if (!itemIds) return;
      publish(validationRunOutcome(itemIds, ok));
    },
  };
}
