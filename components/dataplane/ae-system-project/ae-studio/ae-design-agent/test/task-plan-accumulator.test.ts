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
import { TaskPlan } from "../src/agents/main/task-plan-accumulator.js";

/** A design snapshot with three components (no existing Tasks). */
const DESIGN: Record<string, string> = {
  "specs/requirements/prd.md": "# reqs\n",
  "specs/design/design.cell": "title design\n",
  "specs/design/components/order-service/design.json": '{"name":"order-service"}\n',
  "specs/design/components/user-service/design.json": '{"name":"user-service"}\n',
  "specs/design/components/catalog/design.json": '{"name":"catalog"}\n',
};

/** DESIGN + one existing open Task (issue 7) for user-service. */
const WITH_EXISTING: Record<string, string> = {
  ...DESIGN,
  "tasks/7.md":
    "---\nissueNumber: 7\ncomponent: user-service\ntitle: Build user-service\n" +
    "dependsOn: []\norigin: spec-plan\nderivedStatus: pending\n---\n## Scope\nusers.\n",
};

test("planTask records a known component and normalizes the default origin", () => {
  const plan = new TaskPlan(DESIGN);
  const res = plan.planTask({ component: "order-service", title: "Build order-service", dependsOn: ["user-service"], rationale: "the core." });
  assert.equal(res.ok, true);
  if (res.ok) {
    assert.equal(res.origin, "spec-plan"); // defaulted
    assert.deepEqual(res.dependsOn, ["user-service"]);
  }
  assert.equal(plan.plannedTasks().length, 1);
});

test("planTask rejects an unknown component with the known set", () => {
  const res = new TaskPlan(DESIGN).planTask({ component: "billing", title: "x", dependsOn: [], rationale: "y" });
  assert.equal(res.ok, false);
  if (!res.ok) {
    assert.equal(res.code, "UNKNOWN_COMPONENT");
    assert.deepEqual(res.knownComponents, ["catalog", "order-service", "user-service"]);
  }
});

test("planTask rejects a dependsOn naming an unknown component", () => {
  const res = new TaskPlan(DESIGN).planTask({ component: "order-service", title: "x", dependsOn: ["nope"], rationale: "y" });
  assert.equal(res.ok, false);
  if (!res.ok) assert.equal(res.code, "UNKNOWN_COMPONENT");
});

test("planTask rejects a duplicate title (case-insensitive) against a planned Task", () => {
  const plan = new TaskPlan(DESIGN);
  plan.planTask({ component: "order-service", title: "Build It", dependsOn: [], rationale: "a" });
  const res = plan.planTask({ component: "catalog", title: "  build it ", dependsOn: [], rationale: "b" });
  assert.equal(res.ok, false);
  if (!res.ok) {
    assert.equal(res.code, "DUPLICATE_TITLE");
    assert.deepEqual(res.takenTitles, ["Build It"]);
  }
});

test("planTask rejects a title duplicating an existing Task", () => {
  const res = new TaskPlan(WITH_EXISTING).planTask({ component: "order-service", title: "build user-service", dependsOn: [], rationale: "a" });
  assert.equal(res.ok, false);
  if (!res.ok) assert.equal(res.code, "DUPLICATE_TITLE");
});

test("planTask rejects a dependency cycle across planned Tasks", () => {
  const plan = new TaskPlan(DESIGN);
  plan.planTask({ component: "order-service", title: "A", dependsOn: ["user-service"], rationale: "a" });
  const res = plan.planTask({ component: "user-service", title: "B", dependsOn: ["order-service"], rationale: "b" });
  assert.equal(res.ok, false);
  if (!res.ok) {
    assert.equal(res.code, "DEPENDENCY_CYCLE");
    assert.ok(res.cyclePath && res.cyclePath.length >= 2, JSON.stringify(res.cyclePath));
  }
  assert.equal(plan.plannedTasks().length, 1, "the cyclic plan was NOT recorded");
});

test("planTask rejects a self-dependency as a cycle", () => {
  const res = new TaskPlan(DESIGN).planTask({ component: "catalog", title: "C", dependsOn: ["catalog"], rationale: "c" });
  assert.equal(res.ok, false);
  if (!res.ok) assert.equal(res.code, "DEPENDENCY_CYCLE");
});

test("updateTask patches a Task planned this turn by title and writes its body", () => {
  const plan = new TaskPlan(DESIGN);
  plan.planTask({ component: "order-service", title: "Order", dependsOn: [], rationale: "a" });
  const res = plan.updateTask({ ref: { title: "order" }, set: { body: "## Scope\nfull body." } });
  assert.equal(res.ok, true);
  if (res.ok) assert.deepEqual(res.ref, { title: "Order" }); // canonical (pre-rename) identity
  assert.match(plan.plannedTasks()[0]!.body ?? "", /full body/);
});

test("updateTask patches an existing Task by issueNumber", () => {
  const res = new TaskPlan(WITH_EXISTING).updateTask({ ref: { issueNumber: 7 }, set: { rationale: "still needed", body: "note" } });
  assert.equal(res.ok, true);
  if (res.ok) assert.deepEqual(res.ref, { issueNumber: 7 });
});

test("updateTask rejects an unknown ref with the addressable set", () => {
  const plan = new TaskPlan(WITH_EXISTING);
  plan.planTask({ component: "order-service", title: "Order", dependsOn: [], rationale: "a" });
  const res = plan.updateTask({ ref: { title: "nope" }, set: { body: "x" } });
  assert.equal(res.ok, false);
  if (!res.ok) {
    assert.equal(res.code, "UNKNOWN_REF");
    assert.deepEqual(res.addressable, { issueNumbers: [7], plannedTitles: ["Order"] });
  }
});

test("updateTask by issueNumber rejects a non-existent issue (title refs are this-turn only)", () => {
  const res = new TaskPlan(WITH_EXISTING).updateTask({ ref: { issueNumber: 999 }, set: { body: "x" } });
  assert.equal(res.ok, false);
  if (!res.ok) assert.equal(res.code, "UNKNOWN_REF");
});

test("updateTask rejects a rename that collides with another Task", () => {
  const plan = new TaskPlan(DESIGN);
  plan.planTask({ component: "order-service", title: "Order", dependsOn: [], rationale: "a" });
  plan.planTask({ component: "catalog", title: "Catalog", dependsOn: [], rationale: "b" });
  const res = plan.updateTask({ ref: { title: "Order" }, set: { title: "Catalog" } });
  assert.equal(res.ok, false);
  if (!res.ok) assert.equal(res.code, "DUPLICATE_TITLE");
});

test("updateTask allows re-referencing a Task by its new title after a rename", () => {
  const plan = new TaskPlan(DESIGN);
  plan.planTask({ component: "order-service", title: "Order", dependsOn: [], rationale: "a" });
  assert.equal(plan.updateTask({ ref: { title: "Order" }, set: { title: "Order v2" } }).ok, true);
  assert.equal(plan.updateTask({ ref: { title: "Order v2" }, set: { body: "b" } }).ok, true);
});

test("updateTask rejects a dependsOn change that introduces a cycle", () => {
  const plan = new TaskPlan(DESIGN);
  plan.planTask({ component: "order-service", title: "Order", dependsOn: ["user-service"], rationale: "a" });
  plan.planTask({ component: "user-service", title: "User", dependsOn: [], rationale: "b" });
  const res = plan.updateTask({ ref: { title: "User" }, set: { dependsOn: ["order-service"] } });
  assert.equal(res.ok, false);
  if (!res.ok) assert.equal(res.code, "DEPENDENCY_CYCLE");
});

// --- F1: cycle detection is scoped to the op's own component ----------------

/** An a/b/c design; `tasks` is a map of extra `tasks/<n>.md` renderings. */
const abcDesign = (tasks: Record<string, string> = {}): Record<string, string> => ({
  "specs/design/design.cell": "title design\n",
  "specs/design/components/a/design.json": '{"name":"a"}\n',
  "specs/design/components/b/design.json": '{"name":"b"}\n',
  "specs/design/components/c/design.json": '{"name":"c"}\n',
  ...tasks,
});

/** A `tasks/<n>.md` rendering with an inline dependsOn list. */
const taskFile = (n: number, component: string, deps: string[]): string =>
  `---\nissueNumber: ${n}\ncomponent: ${component}\ntitle: T${n} ${component}\ndependsOn: [${deps.join(", ")}]\norigin: spec-plan\n---\nbody\n`;

test("planTask succeeds despite a PRE-EXISTING cycle among untouched existing Tasks", () => {
  // Existing context carries an a→b→a cycle (reactive-birth / human-edited blocks).
  const seed = abcDesign({ "tasks/1.md": taskFile(1, "a", ["b"]), "tasks/2.md": taskFile(2, "b", ["a"]) });

  // A plan for the UNRELATED component c must not be blocked by the a↔b cycle.
  assert.equal(new TaskPlan(seed).planTask({ component: "c", title: "C", dependsOn: [], rationale: "r" }).ok, true);
  // Even depending on `a` is fine: c is not reachable from itself (the a↔b cycle never returns to c).
  assert.equal(new TaskPlan(seed).planTask({ component: "c", title: "C", dependsOn: ["a"], rationale: "r" }).ok, true);
});

test("planTask whose OWN edge closes a cycle is rejected with the cycle path through its component", () => {
  const plan = new TaskPlan(abcDesign());
  assert.equal(plan.planTask({ component: "a", title: "A", dependsOn: ["b"], rationale: "r" }).ok, true);
  const res = plan.planTask({ component: "b", title: "B", dependsOn: ["a"], rationale: "r" });
  assert.equal(res.ok, false);
  if (!res.ok) {
    assert.equal(res.code, "DEPENDENCY_CYCLE");
    assert.deepEqual(res.cyclePath, ["b", "a", "b"]); // the cycle runs through the op's component
  }
});

test("cycle edges use the LATEST existing task per component (stale rendering ignored)", () => {
  // Component a has two existing Tasks: old #1 depends on b, latest #5 depends on nothing.
  const withLatest = abcDesign({ "tasks/1.md": taskFile(1, "a", ["b"]), "tasks/5.md": taskFile(5, "a", []) });
  // b→a is fine: a's LATEST (#5) has no deps, so there is no a→b edge to close the loop.
  assert.equal(new TaskPlan(withLatest).planTask({ component: "b", title: "B", dependsOn: ["a"], rationale: "r" }).ok, true);

  // Control: with ONLY the stale #1 (a→b) present, b→a DOES close a cycle.
  const staleOnly = abcDesign({ "tasks/1.md": taskFile(1, "a", ["b"]) });
  assert.equal(new TaskPlan(staleOnly).planTask({ component: "b", title: "B", dependsOn: ["a"], rationale: "r" }).ok, false);
});
