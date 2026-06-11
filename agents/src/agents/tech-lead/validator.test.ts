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
import {
  validatePlan,
  type PlanItemWithTempId,
  type DiffContext,
} from "./validator.js";
import type { SlimDesignComponent, ExistingTaskSummary } from "./schema.js";

function planItem(
  overrides: Partial<PlanItemWithTempId> & { tempId: string; title: string },
): PlanItemWithTempId {
  return {
    componentName: "todo-api",
    rationale: "core",
    dependsOn: [],
    ...overrides,
  } as PlanItemWithTempId;
}

const design: SlimDesignComponent[] = [
  { name: "todo-api", componentType: "service", language: "Go", dependsOn: [] },
  {
    name: "todo-web",
    componentType: "web-app",
    language: "TypeScript",
    dependsOn: ["todo-api"],
  },
];

test("fresh: empty plan → empty-plan-fresh", () => {
  const issues = validatePlan({
    items: [],
    design,
    existingTasks: [],
    mode: "fresh",
  });
  assert.deepEqual(
    issues.map((i) => i.code),
    ["empty-plan-fresh"],
  );
});

test("fresh: valid 2-item plan passes", () => {
  const items: PlanItemWithTempId[] = [
    planItem({ tempId: "p-0", title: "Implement todo-api" }),
    planItem({
      tempId: "p-1",
      title: "Implement todo-web",
      componentName: "todo-web",
      dependsOn: ["Implement todo-api"],
    }),
  ];
  const issues = validatePlan({ items, design, existingTasks: [], mode: "fresh" });
  assert.deepEqual(issues, []);
});

test("unknown-component flagged", () => {
  const items = [
    planItem({
      tempId: "p-0",
      title: "Implement notify-svc",
      componentName: "notify-svc",
    }),
  ];
  const issues = validatePlan({ items, design, existingTasks: [], mode: "fresh" });
  assert.equal(issues.length, 1);
  assert.equal(issues[0].code, "unknown-component");
  assert.equal(issues[0].componentName, "notify-svc");
  assert.equal(issues[0].tempId, "p-0");
});

test("title-collision flagged", () => {
  const items = [
    planItem({ tempId: "p-0", title: "Implement todo-api" }),
    planItem({ tempId: "p-1", title: "Implement todo-api" }),
  ];
  const issues = validatePlan({ items, design, existingTasks: [], mode: "fresh" });
  const codes = issues.map((i) => i.code);
  assert.ok(codes.includes("title-collision"));
});

test("dangling-dep flagged when dep is neither in batch nor existing", () => {
  const items = [
    planItem({
      tempId: "p-0",
      title: "Implement todo-api",
      dependsOn: ["Set up auth"],
    }),
  ];
  const issues = validatePlan({ items, design, existingTasks: [], mode: "fresh" });
  assert.equal(issues.length, 1);
  assert.equal(issues[0].code, "dangling-dep");
  assert.equal(issues[0].dep, "Set up auth");
});

test("dependsOn resolves to existing non-rejected task", () => {
  const items = [
    planItem({
      tempId: "p-0",
      title: "Add notification endpoint",
      dependsOn: ["Implement todo-api"],
    }),
  ];
  const existing: ExistingTaskSummary[] = [
    {
      title: "Implement todo-api",
      componentName: "todo-api",
      status: "merged",
    },
  ];
  const issues = validatePlan({
    items,
    design,
    existingTasks: existing,
    mode: "incremental",
    diff: { added: [], contractAffectedModified: [], removed: [] },
  });
  assert.deepEqual(issues, []);
});

test("dependsOn cycle flagged", () => {
  const items = [
    planItem({
      tempId: "p-0",
      title: "A",
      dependsOn: ["B"],
    }),
    planItem({
      tempId: "p-1",
      title: "B",
      dependsOn: ["A"],
    }),
  ];
  const issues = validatePlan({ items, design, existingTasks: [], mode: "fresh" });
  const codes = issues.map((i) => i.code);
  assert.ok(codes.includes("depends-on-cycle"));
});

test("incremental: empty plan with trivial diff is allowed", () => {
  const issues = validatePlan({
    items: [],
    design,
    existingTasks: [],
    mode: "incremental",
    diff: { added: [], contractAffectedModified: [], removed: [] },
  });
  assert.deepEqual(issues, []);
});

test("incremental: empty plan with ADDED diff → missing-coverage", () => {
  const diff: DiffContext = {
    added: ["notify-svc"],
    contractAffectedModified: [],
    removed: [],
  };
  const issues = validatePlan({
    items: [],
    design: [
      ...design,
      {
        name: "notify-svc",
        componentType: "service",
        language: "Go",
        dependsOn: [],
      },
    ],
    existingTasks: [],
    mode: "incremental",
    diff,
  });
  assert.equal(issues.length, 1);
  assert.equal(issues[0].code, "missing-coverage");
  assert.equal(issues[0].componentName, "notify-svc");
});

test("incremental: ADDED component covered passes", () => {
  const diff: DiffContext = {
    added: ["notify-svc"],
    contractAffectedModified: [],
    removed: [],
  };
  const items = [
    planItem({
      tempId: "p-0",
      title: "Implement notify-svc",
      componentName: "notify-svc",
    }),
  ];
  const designWithNotify: SlimDesignComponent[] = [
    ...design,
    { name: "notify-svc", componentType: "service", language: "Go", dependsOn: [] },
  ];
  const issues = validatePlan({
    items,
    design: designWithNotify,
    existingTasks: [],
    mode: "incremental",
    diff,
  });
  assert.deepEqual(issues, []);
});

test("incremental: ADDED component NOT covered → missing-coverage", () => {
  const diff: DiffContext = {
    added: ["notify-svc"],
    contractAffectedModified: [],
    removed: [],
  };
  const designWithNotify: SlimDesignComponent[] = [
    ...design,
    { name: "notify-svc", componentType: "service", language: "Go", dependsOn: [] },
  ];
  const items = [
    planItem({
      tempId: "p-0",
      title: "Add metrics to todo-api",
      componentName: "todo-api",
    }),
  ];
  const issues = validatePlan({
    items,
    design: designWithNotify,
    existingTasks: [],
    mode: "incremental",
    diff,
  });
  assert.equal(issues.length, 1);
  assert.equal(issues[0].code, "missing-coverage");
  assert.equal(issues[0].componentName, "notify-svc");
});
