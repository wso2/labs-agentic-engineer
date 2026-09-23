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
 * The two enterprise fixtures are the proof that the v1 registry covers real
 * workflow applications, and the shared input every later layer tests against
 * (the Go save gate, the agent scenario, the console's mock mode). They are
 * published as `@aep/prototype-model/fixtures/<name>.json`.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

import {
  parsePrototypeModel,
  stablePrototypeJson,
  type PrototypeAction,
  type PrototypeModelV1,
  type PrototypeNode,
} from "../src/index.ts";

const FIXTURES = ["expense-approval", "integration-monitor"] as const;

function raw(name: string): string {
  return readFileSync(new URL(`../fixtures/${name}.json`, import.meta.url), "utf8");
}

function model(name: string): PrototypeModelV1 {
  const result = parsePrototypeModel(JSON.parse(raw(name)));
  assert.equal(result.ok, true, result.ok ? "" : JSON.stringify(result.issues, null, 2));
  return (result as { ok: true; model: PrototypeModelV1 }).model;
}

/** The v1 registry, as the spec names it. */
const REGISTRY = [
  "app-shell", "stack", "grid", "split", "detail", "side-nav", "top-nav", "breadcrumbs", "tabs", "stepper",
  "text", "heading", "badge", "stat", "alert", "empty-state", "button", "link", "form", "field",
  "validation-summary", "table", "filters", "timeline", "approval-panel", "task-queue", "dialog", "drawer",
] as const;

const ACTIONS: PrototypeAction["kind"][] = [
  "navigate", "set-tab", "set-step", "select-row", "show-dialog", "show-drawer", "close-overlay",
];

interface Usage {
  registry: Set<string>;
  actions: Set<string>;
  showIn: number;
  errorIn: number;
}

/** What a model exercises: registry entries, action kinds, and display-state hooks. */
function usage(m: PrototypeModelV1): Usage {
  const u: Usage = { registry: new Set(), actions: new Set(), showIn: 0, errorIn: 0 };
  const action = (a: PrototypeAction | undefined) => a && u.actions.add(a.kind);
  const nodes = (list: PrototypeNode[]) => list.forEach(node);
  function node(n: PrototypeNode): void {
    u.registry.add(n.kind);
    if (n.showIn) u.showIn++;
    switch (n.kind) {
      case "stack":
      case "grid":
        return nodes(n.content);
      case "split":
        return nodes([...n.left, ...n.right]);
      case "tabs":
        return n.tabs.forEach((t) => nodes(t.content));
      case "stepper":
        return n.steps.forEach((s) => nodes(s.content));
      case "breadcrumbs":
        return n.items.forEach((i) => action(i.action));
      case "heading":
        return n.actions?.forEach((b) => action(b.action));
      case "empty-state":
        return action(n.action?.action);
      case "button":
      case "link":
        return action(n.action);
      case "form":
      case "filters":
        for (const f of n.fields) {
          u.registry.add("field");
          if (f.errorIn) u.errorIn++;
        }
        if (n.kind === "form") n.actions.forEach((b) => action(b.action));
        return;
      case "approval-panel":
        return n.actions.forEach((b) => action(b.action));
      case "table":
      case "task-queue":
        return action(n.onRow);
      default:
        return;
    }
  }
  for (const nav of m.navigation) {
    u.registry.add(nav.kind);
    nav.items.forEach((i) => action(i.action));
  }
  for (const s of m.screens) {
    if (s.navigationId) u.registry.add("app-shell");
    nodes(s.content);
    for (const o of s.overlays ?? []) {
      u.registry.add(o.kind);
      nodes(o.content);
      if (o.kind === "dialog") o.actions.forEach((b) => action(b.action));
    }
  }
  return u;
}

for (const name of FIXTURES) {
  test(`${name} is a valid prototype`, () => {
    model(name);
  });

  test(`${name} is checked in in its serialized form`, () => {
    assert.equal(raw(name), stablePrototypeJson(model(name)));
  });

  test(`${name} round-trips byte for byte`, () => {
    const once = stablePrototypeJson(model(name));
    const again = parsePrototypeModel(JSON.parse(once));
    assert.equal(again.ok, true);
    if (again.ok) assert.equal(stablePrototypeJson(again.model), once);
  });

  test(`${name} declares roles, flows, and display states`, () => {
    const m = model(name);
    assert.ok(m.roles.length >= 2, "role-specific views need more than one role");
    assert.ok(m.flows.length >= 2, "named flows");
    assert.ok(m.states.length >= 3, "display states beyond the default");
    const u = usage(m);
    assert.ok(u.showIn > 0, "some content appears only in some display states");
  });
}

test("together the fixtures exercise every registry entry and every action", () => {
  const all = FIXTURES.map((n) => usage(model(n)));
  const registry = new Set(all.flatMap((u) => [...u.registry]));
  const actions = new Set(all.flatMap((u) => [...u.actions]));
  assert.deepEqual(REGISTRY.filter((k) => !registry.has(k)), [], "registry entries no fixture uses");
  assert.deepEqual(ACTIONS.filter((k) => !actions.has(k)), [], "actions no fixture uses");
});

test("the expense fixture shows field errors only in its validation state", () => {
  const u = usage(model("expense-approval"));
  assert.ok(u.errorIn > 0);
  assert.ok(model("expense-approval").states.some((s) => s.id === "state.invalid"));
});

test("the integration fixture has failed, delayed, and healthy states", () => {
  const ids = model("integration-monitor").states.map((s) => s.id);
  for (const id of ["state.failed", "state.delayed", "state.healthy"]) assert.ok(ids.includes(id), id);
});

test("serialization is canonical: key order in the input does not matter", () => {
  const m = model("expense-approval");
  const reversed = reverseKeys(m) as PrototypeModelV1;
  assert.equal(stablePrototypeJson(reversed), stablePrototypeJson(m));
});

test("serialization is two-space JSON with one trailing newline", () => {
  const out = stablePrototypeJson(model("expense-approval"));
  assert.ok(out.endsWith("}\n") && !out.endsWith("\n\n"));
  assert.ok(out.startsWith('{\n  "schemaVersion": 1,\n  "component": "approvals-portal",'));
});

test("serialization refuses an invalid model instead of writing it", () => {
  const m = { ...model("expense-approval"), defaultScreenId: "screen.ghost" };
  assert.throws(() => stablePrototypeJson(m), /UNKNOWN_REFERENCE/);
});

function reverseKeys(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(reverseKeys);
  if (value && typeof value === "object") {
    return Object.fromEntries(Object.entries(value).reverse().map(([k, v]) => [k, reverseKeys(v)]));
  }
  return value;
}
