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

import { test } from "node:test";
import assert from "node:assert/strict";
import type { HookInput } from "@anthropic-ai/claude-agent-sdk";
import {
  ValidationProgressState,
  createValidationProgressTracker,
  validationProgressUpdates,
  validationRunOutcome,
  type ProgressItemUpdate,
} from "./validation_progress.js";

const STUB = `// spec: tests/validation/test-plan.md § AC-004-a\n`;
const FILLED = `${STUB}\nimport { test, expect } from "@playwright/test";\n\ntest('AC-004-a: shows an error', async ({ page }) => {});\n`;

const updates = (tool: string, input: unknown, state = new ValidationProgressState()) =>
  validationProgressUpdates(tool, input, state);

const preToolUse = (toolName: string, toolInput: unknown, toolUseId = "toolu_01"): HookInput =>
  ({
    hook_event_name: "PreToolUse",
    tool_name: toolName,
    tool_input: toolInput,
    tool_use_id: toolUseId,
    session_id: "s1",
    transcript_path: "/tmp/t",
    cwd: "/workspace/project",
  }) as HookInput;

// ---- the plan commit ------------------------------------------------------

test("validation-progress: the test plan moves every criterion it names to planned", () => {
  const out = updates("Write", {
    file_path: "/workspace/project/tests/validation/test-plan.md",
    content:
      "# Test plan\n\n## AC-001-a — A name box is visible\n\n- Target: hello-web\n\n## AC-002-b — Greeting appears\n",
  });
  assert.deepEqual(out, [
    { itemId: "AC-001-a", status: "planned" },
    { itemId: "AC-002-b", status: "planned" },
  ]);
});

test("validation-progress: a re-validation append never walks a live criterion backwards", () => {
  // SKILL.md has re-validation APPEND sections to the plan, so the file names
  // criteria already in flight. Announcing those as `planned` again would move
  // a row that is being authored back to the start.
  const state = new ValidationProgressState();
  state.record({ itemId: "AC-001-a", status: "authoring" });
  const out = updates(
    "Write",
    {
      file_path: "tests/validation/test-plan.md",
      content: "## AC-001-a — old\n## AC-009-a — new\n",
    },
    state,
  );
  assert.deepEqual(out, [{ itemId: "AC-009-a", status: "planned" }]);
});

// ---- exploring vs authoring ----------------------------------------------

test("validation-progress: a header-only spec is the marker for exploring", () => {
  assert.deepEqual(
    updates("Write", { file_path: "tests/e2e/specs/AC-004-a.spec.ts", content: STUB }),
    [{ itemId: "AC-004-a", status: "exploring" }],
  );
});

test("validation-progress: a spec with a test body is authoring", () => {
  assert.deepEqual(
    updates("Write", { file_path: "tests/e2e/specs/AC-004-a.spec.ts", content: FILLED }),
    [{ itemId: "AC-004-a", status: "authoring" }],
  );
});

test("validation-progress: an Edit is authoring — its input never shows the result", () => {
  assert.deepEqual(
    updates("Edit", { file_path: "tests/e2e/specs/AC-004-a.spec.ts", old_string: "a", new_string: "b" }),
    [{ itemId: "AC-004-a", status: "authoring" }],
  );
});

test("validation-progress: test.describe and test.only still read as a body", () => {
  for (const body of ["test.describe('x', () => {})", "test.only('AC-004-a: x', async () => {})"]) {
    assert.deepEqual(
      updates("Write", { file_path: "tests/e2e/specs/AC-004-a.spec.ts", content: `${STUB}${body}` }),
      [{ itemId: "AC-004-a", status: "authoring" }],
      body,
    );
  }
});

test("validation-progress: files that are not specs say nothing", () => {
  for (const file_path of [
    "tests/e2e/lib/targets.ts",
    "tests/e2e/playwright.config.ts",
    "README.md",
    "tests/e2e/specs/helpers.ts",
  ]) {
    assert.deepEqual(updates("Write", { file_path, content: FILLED }), [], file_path);
  }
});

// ---- healing --------------------------------------------------------------

test("validation-progress: a first-draft failure keeps saying authoring", () => {
  // authoring.md requires a spec to pass twice consecutively, so failing before
  // it ever works is the normal path — not a heal.
  const state = new ValidationProgressState();
  state.record({ itemId: "AC-004-a", status: "running" });
  state.record({ itemId: "AC-004-a", status: "fail" });
  assert.deepEqual(
    updates("Edit", { file_path: "tests/e2e/specs/AC-004-a.spec.ts" }, state),
    [{ itemId: "AC-004-a", status: "authoring" }],
  );
});

test("validation-progress: editing a spec that passed and is NOT failing is still authoring", () => {
  // The gap the first cut had: `everPassed` alone painted Healing on ANY later
  // edit to a working spec — a comment, a lint fix, the polish authoring.md's own
  // second consecutive pass invites. Healing is a spec that worked and BROKE.
  const state = new ValidationProgressState();
  state.record({ itemId: "AC-004-a", status: "pass" });
  assert.deepEqual(
    updates("Edit", { file_path: "tests/e2e/specs/AC-004-a.spec.ts" }, state),
    [{ itemId: "AC-004-a", status: "authoring" }],
  );
});

test("validation-progress: editing a spec that HAD passed is healing", () => {
  const state = new ValidationProgressState();
  state.record({ itemId: "AC-004-a", status: "pass" });
  state.record({ itemId: "AC-004-a", status: "fail" });
  assert.deepEqual(
    updates("Edit", { file_path: "tests/e2e/specs/AC-004-a.spec.ts" }, state),
    [{ itemId: "AC-004-a", status: "healing" }],
  );
});

// ---- the run --------------------------------------------------------------

test("validation-progress: a per-spec test call reports that criterion running", () => {
  assert.deepEqual(
    updates("Bash", { command: "npm test --prefix tests/e2e -- specs/AC-004-a.spec.ts" }),
    [{ itemId: "AC-004-a", status: "running" }],
  );
});

test("validation-progress: a command that merely NAMES a spec is not a run", () => {
  // Without the test-invocation guard this reported the criterion as running.
  for (const command of [
    "cat tests/e2e/specs/AC-004-a.spec.ts",
    "git add tests/e2e/specs/AC-004-a.spec.ts",
    "rm -f tests/e2e/test-results/results.json",
  ]) {
    assert.deepEqual(updates("Bash", { command }), [], command);
  }
});

test("validation-progress: a sharded call reports every spec it runs, once each", () => {
  assert.deepEqual(
    updates("Bash", {
      command: "npm test --prefix tests/e2e -- specs/AC-001-a.spec.ts specs/AC-002-a.spec.ts specs/AC-001-a.spec.ts",
    }),
    [
      { itemId: "AC-001-a", status: "running" },
      { itemId: "AC-002-a", status: "running" },
    ],
  );
});

test("validation-progress: a whole-suite run claims no criterion", () => {
  // healing.md's final full run names no spec, so there is nothing to attribute.
  // Rows hold their last status until report.json settles them.
  assert.deepEqual(updates("Bash", { command: "npm test --prefix tests/e2e" }), []);
});

// ---- outcomes -------------------------------------------------------------

test("validation-progress: a green call passes every spec it ran", () => {
  assert.deepEqual(validationRunOutcome(["AC-001-a", "AC-002-a"], true), [
    { itemId: "AC-001-a", status: "pass" },
    { itemId: "AC-002-a", status: "pass" },
  ]);
});

test("validation-progress: a red single-spec call fails that criterion", () => {
  assert.deepEqual(validationRunOutcome(["AC-001-a"], false), [{ itemId: "AC-001-a", status: "fail" }]);
});

test("validation-progress: a red BATCH invents no per-criterion failure", () => {
  // The exit code says the batch failed, never which member did. Marking all of
  // them failed would put evidence on screen that no run produced.
  assert.deepEqual(validationRunOutcome(["AC-001-a", "AC-002-a"], false), []);
});

// ---- the tracker ----------------------------------------------------------

test("validation-progress: a status is published once, not once per call", () => {
  const seen: ProgressItemUpdate[] = [];
  const tracker = createValidationProgressTracker((u) => seen.push(u));
  const call = preToolUse("Edit", { file_path: "tests/e2e/specs/AC-004-a.spec.ts" });
  for (let i = 0; i < 5; i += 1) void tracker.hook(call, undefined, { signal: new AbortController().signal });
  assert.deepEqual(seen, [{ itemId: "AC-004-a", status: "authoring" }]);
});

test("validation-progress: a run settles from the outcome of its own tool call", async () => {
  const seen: ProgressItemUpdate[] = [];
  const tracker = createValidationProgressTracker((u) => seen.push(u));
  const signal = new AbortController().signal;

  await tracker.hook(
    preToolUse("Bash", { command: "npm test --prefix tests/e2e -- specs/AC-004-a.spec.ts" }, "toolu_run"),
    undefined,
    { signal },
  );
  tracker.settle("toolu_run", false);
  // A tool call that was never a run settles nothing.
  tracker.settle("toolu_other", true);
  // The same id twice must not re-fire: the outcome is consumed.
  tracker.settle("toolu_run", true);

  assert.deepEqual(seen, [
    { itemId: "AC-004-a", status: "running" },
    { itemId: "AC-004-a", status: "fail" },
  ]);
});

test("validation-progress: the hook never blocks or rewrites a call", () => {
  const tracker = createValidationProgressTracker(() => {});
  const out = tracker.hook(
    preToolUse("Write", { file_path: "tests/e2e/specs/AC-004-a.spec.ts", content: FILLED }),
    undefined,
    { signal: new AbortController().signal },
  );
  return Promise.resolve(out).then((decision) => assert.deepEqual(decision, {}));
});

test("validation-progress: a pass recorded through the tracker arms healing", () => {
  const seen: ProgressItemUpdate[] = [];
  const tracker = createValidationProgressTracker((u) => seen.push(u));
  const signal = new AbortController().signal;
  const run = async (tool: string, input: unknown, id: string) => {
    await tracker.hook(preToolUse(tool, input, id), undefined, { signal });
  };

  return (async () => {
    await run("Write", { file_path: "tests/e2e/specs/AC-004-a.spec.ts", content: FILLED }, "t1");
    await run("Bash", { command: "npm test --prefix tests/e2e -- specs/AC-004-a.spec.ts" }, "t2");
    tracker.settle("t2", true);
    await run("Bash", { command: "npm test --prefix tests/e2e -- specs/AC-004-a.spec.ts" }, "t3");
    tracker.settle("t3", false);
    await run("Edit", { file_path: "tests/e2e/specs/AC-004-a.spec.ts" }, "t4");

    assert.deepEqual(
      seen.map((u) => u.status),
      ["authoring", "running", "pass", "running", "fail", "healing"],
    );
  })();
});

test("validation-progress: a run nobody settles leaves the criterion running", () => {
  // What a SEVERED test command now produces. The translator withholds the
  // outcome for a call that never finished (see from-sdk's incompleteCall), so
  // no status arrives — and `running` is the honest resting place, because the
  // command was auto-backgrounded and may still be executing.
  //
  // Before this, a severed call answered `ok: true` and painted `pass` on the
  // criterion, which on a heal re-run means a criterion that was just failing.
  const seen: ProgressItemUpdate[] = [];
  const tracker = createValidationProgressTracker((u) => seen.push(u));
  return tracker
    .hook(preToolUse("Bash", { command: "npm test --prefix tests/e2e -- specs/AC-004-a.spec.ts" }, "toolu_sev"), undefined, {
      signal: new AbortController().signal,
    })
    .then(() => {
      assert.deepEqual(seen, [{ itemId: "AC-004-a", status: "running" }]);
    });
});
