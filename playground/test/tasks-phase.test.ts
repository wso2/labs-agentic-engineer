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
 * Tasks-phase tests (docs/design/playground.md §14):
 *  - renderTaskContextFile ⇄ parseTaskContextFile round trip, byte-pinned
 *    against the Go `TaskContextFile.Render` fixture;
 *  - the dedupe key recipe pinned against Go `taskmeta.Key` vectors;
 *  - the plan fold via the REAL task-plan toolset (mock model): create +
 *    update-by-title, replan dedupe, and the no-manifest do-not-commit fence.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { parseTaskContextFile } from "@aep/agent-stream";
import { mockModel, type MockStep } from "@aep/agents/shared/mock-model";
import { tasksCommand } from "../src/commands.js";
import { renderTaskContextFile, taskKey, titleSlug, FsIssueStore } from "../src/ports/issue-store.js";

// Byte-for-byte output of Go taskplan.TaskContextFile.Render for the same
// data (generated from the Go renderer; regenerate if context_file.go changes).
const GO_RENDER_FIXTURE =
  '---\n' +
  'issueNumber: 3\n' +
  'component: "user-service"\n' +
  'title: "Implement \\"auth\\": step 1"\n' +
  'dependsOn: ["auth-service", "db"]\n' +
  'origin: "spec-plan"\n' +
  '---\n' +
  '\n' +
  '> **Rationale:** covers auth\n' +
  '\n' +
  'Scope text here.\n';

test("renderTaskContextFile is byte-identical to the Go renderer and round-trips", () => {
  const rendered = renderTaskContextFile({
    issueNumber: 3,
    component: "user-service",
    title: 'Implement "auth": step 1',
    dependsOn: ["auth-service", "db"],
    body: "> **Rationale:** covers auth\n\nScope text here.",
  });
  assert.equal(rendered, GO_RENDER_FIXTURE);

  const parsed = parseTaskContextFile("tasks/3.md", rendered);
  assert.ok(parsed);
  assert.equal(parsed.issueNumber, 3);
  assert.equal(parsed.component, "user-service");
  assert.equal(parsed.title, 'Implement "auth": step 1');
  assert.deepEqual(parsed.dependsOn, ["auth-service", "db"]);
  assert.match(parsed.body, /Scope text here\./);
});

test("taskKey + titleSlug are pinned to the Go taskmeta vectors", () => {
  // Vectors generated from Go taskmeta.Key/TitleSlug.
  assert.equal(taskKey("proj", "design-v5", "order-service", "Implement order-service"), "d50ee12432fa");
  assert.equal(taskKey("todo-app", "local", "user-service", "Build user service"), "befc656b968c");
  assert.equal(taskKey("p", "d", "t", "🚀"), "ff680200a578"); // empty-slug fallback path
  assert.equal(titleSlug("café déjà 42"), "caf-d-j-42");
  assert.equal(titleSlug("  Trim & collapse!! "), "trim-collapse");
});

const VALID_DESIGN = (name: string): string =>
  JSON.stringify({
    name,
    type: "service",
    version: "0.1.0",
    language: "go",
    buildpack: "docker",
    appPath: `./${name}`,
    entrypoint: "cmd/main.go",
    exposure: "internet",
    dependencies: [],
    description: `${name} component`,
  });

function seedDesignedProject(): string {
  const dir = mkdtempSync(join(tmpdir(), "aep-play-tasks-"));
  mkdirSync(join(dir, "specs/requirements"), { recursive: true });
  writeFileSync(join(dir, "specs/requirements/prd.md"), "# R\n");
  mkdirSync(join(dir, "specs/design/components/user-service"), { recursive: true });
  mkdirSync(join(dir, "specs/design/components/web-frontend"), { recursive: true });
  writeFileSync(join(dir, "specs/design/design.cell"), "title R\n\ncomponent user-service service\ncomponent web-frontend web-application\n");
  writeFileSync(join(dir, "specs/design/components/user-service/design.json"), VALID_DESIGN("user-service"));
  writeFileSync(join(dir, "specs/design/components/web-frontend/design.json"), VALID_DESIGN("web-frontend"));
  return dir;
}

function tempSkills(): string {
  const dir = mkdtempSync(join(tmpdir(), "aep-play-skills-"));
  mkdirSync(join(dir, "task-planning"), { recursive: true });
  writeFileSync(join(dir, "task-planning", "SKILL.md"), "---\nname: task-planning\ndescription: plan tasks\n---\n\nPlan.\n");
  return dir;
}

const planSteps = (component: string, title: string, body: string): MockStep[] => [
  {
    kind: "toolCall",
    toolCallId: `p-${component}`,
    toolName: "planTask",
    input: { component, title, dependsOn: [], rationale: `covers ${component}` },
  },
  {
    kind: "toolCall",
    toolCallId: `u-${component}`,
    toolName: "updateTask",
    input: { ref: { title }, set: { body } },
  },
];

test("plan fold: planTask + updateTask-by-title → issues/<n>.md; replan dedupes; refs to instruction-carried numbers are fenced", async () => {
  const projectDir = seedDesignedProject();
  const skillsDir = tempSkills();
  try {
    const first = mockModel([
      ...planSteps("user-service", "Build user service", "## Scope\nimplement the API"),
      ...planSteps("web-frontend", "Build web frontend", "## Scope\nimplement the UI"),
      { kind: "text", text: "planned." },
    ]);
    const outcome = await tasksCommand(projectDir, { model: first, skillsDir, silent: true });
    assert.equal(outcome.ok, true, outcome.detail);
    assert.equal(outcome.fold?.created.length, 2);

    const issue1 = readFileSync(join(projectDir, "issues/1.md"), "utf8");
    assert.match(issue1, /component: "user-service"/);
    assert.match(issue1, /> \*\*Rationale:\*\* covers user-service/);
    assert.match(issue1, /## Scope/);
    assert.match(issue1, /key: "[0-9a-f]{12}"/);

    // Replan: the model re-plans an already-covered component (same title) and
    // tries updateTask{issueNumber} on an instruction-carried task — the server
    // rejects the issueNumber ref (UNKNOWN_REF, production parity), the
    // duplicate planTask folds to a skip, and nothing new lands.
    const second = mockModel([
      {
        kind: "toolCall",
        toolCallId: "dup",
        toolName: "planTask",
        input: { component: "user-service", title: "Build user service", dependsOn: [], rationale: "dup" },
      },
      {
        kind: "toolCall",
        toolCallId: "fence",
        toolName: "updateTask",
        input: { ref: { issueNumber: 1 }, set: { body: "must not land" } },
      },
      { kind: "text", text: "replanned." },
    ]);
    const replan = await tasksCommand(projectDir, { model: second, skillsDir, silent: true });
    assert.equal(replan.ok, true, replan.detail);
    assert.equal(replan.fold?.created.length, 0);
    // Server-side the duplicate planTask is REJECTED (DUPLICATE_TITLE never ok)
    // or folds as a key-dedupe skip — either way no new file:
    assert.ok(!existsSync(join(projectDir, "issues/3.md")), "no third issue after replan");
    assert.ok(!readFileSync(join(projectDir, "issues/1.md"), "utf8").includes("must not land"), "fenced ref never written");
  } finally {
    rmSync(projectDir, { recursive: true, force: true });
    rmSync(skillsDir, { recursive: true, force: true });
  }
});

test("safeAllocator never clobbers an existing issue file (copied project, fresh counter)", () => {
  const projectDir = seedDesignedProject();
  try {
    mkdirSync(join(projectDir, "issues"), { recursive: true });
    writeFileSync(join(projectDir, "issues", "1.md"), "existing\n");
    writeFileSync(join(projectDir, "issues", "2.md"), "existing\n");
    const store = new FsIssueStore(projectDir, "todo-app");
    let counter = 1; // a project copied without .aep-playground state
    const alloc = store.safeAllocator(
      () => counter,
      (advancedTo) => {
        counter = advancedTo;
      },
    );
    assert.equal(alloc(), 3, "skips past existing files");
    assert.equal(counter, 4);
    assert.equal(readFileSync(join(projectDir, "issues", "1.md"), "utf8"), "existing\n");
  } finally {
    rmSync(projectDir, { recursive: true, force: true });
  }
});

test("no terminal manifest → the fold writes nothing (D14 do-not-commit)", () => {
  const projectDir = seedDesignedProject();
  try {
    const store = new FsIssueStore(projectDir, "todo-app");
    const outcome = store.fold(
      [
        {
          type: "tool-result",
          toolName: "planTask",
          toolCallId: "p1",
          output: { ok: true, op: "plan", component: "user-service", title: "T", dependsOn: [], origin: "spec-plan", rationale: "r" },
        },
        // no manifest part — severed stream
      ],
      () => 1,
    );
    assert.equal(outcome.created.length, 0);
    assert.ok(!existsSync(join(projectDir, "issues")), "no issues dir created");
  } finally {
    rmSync(projectDir, { recursive: true, force: true });
  }
});

test("updateTask rename collision: only the rename is skipped (surfaced), dependsOn/body still land", () => {
  const projectDir = seedDesignedProject();
  try {
    const store = new FsIssueStore(projectDir, "todo-app");
    let n = 1;
    const plan = (component: string, title: string) => ({
      type: "tool-result" as const,
      toolName: "planTask",
      toolCallId: `p-${title}`,
      output: { ok: true, op: "plan", component, title, dependsOn: [], origin: "spec-plan", rationale: "r" },
    });
    const outcome = store.fold(
      [
        plan("user-service", "Task A"),
        plan("webapp", "Task B"),
        {
          type: "tool-result",
          toolName: "updateTask",
          toolCallId: "u1",
          output: {
            ok: true,
            op: "update",
            ref: { title: "Task B" },
            set: { title: "Task A", dependsOn: ["user-service"], body: "revised scope" },
          },
        },
        { type: "manifest", files: {} },
      ],
      () => n++,
    );
    assert.deepEqual(outcome.skippedRenames, ['#2 "Task B" → "Task A"']);
    const issueB = store.list().find((i) => i.issueNumber === 2);
    assert.ok(issueB, "issue 2 exists");
    assert.equal(issueB.title, "Task B", "colliding rename did not apply");
    assert.deepEqual(issueB.dependsOn, ["user-service"], "dependsOn from the same op landed");
    assert.match(issueB.body, /revised scope/, "body from the same op landed");
  } finally {
    rmSync(projectDir, { recursive: true, force: true });
  }
});
