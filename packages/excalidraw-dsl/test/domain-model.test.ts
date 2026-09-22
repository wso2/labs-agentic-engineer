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
import { dslToExcalidraw, tryDslToExcalidraw } from "../src/index.js";

type El = {
  id: string;
  type: string;
  text?: string;
  startBinding?: { elementId: string } | null;
  endBinding?: { elementId: string } | null;
};

const DSL = `entity Order
  id: uuid
  total: decimal
entity LineItem
  sku: string
relation Order -[1..*]-> LineItem "contains"
`;

function elements(dsl: string): El[] {
  return (JSON.parse(dslToExcalidraw(dsl)) as { elements: El[] }).elements;
}

test("an entity renders its name and every attribute", () => {
  const texts = elements(DSL).flatMap((e) => (e.text ? [e.text] : []));
  for (const want of ["Order", "LineItem", "id", "total", "sku"]) {
    assert.ok(texts.some((t) => t.includes(want)), want);
  }
});

test("a relation renders as an arrow bound to both entities", () => {
  const els = elements(DSL);
  const arrows = els.filter((e) => e.type === "arrow");
  assert.equal(arrows.length, 1);
  const bound = [arrows[0]!.startBinding?.elementId, arrows[0]!.endBinding?.elementId];
  assert.notEqual(bound[0], bound[1]);
  for (const id of bound) assert.ok(els.some((e) => e.id === id && e.type === "rectangle"), `bound to ${id}`);
});

test("the scene is deterministic", () => {
  assert.equal(dslToExcalidraw(DSL), dslToExcalidraw(DSL));
});

test("tryDslToExcalidraw reports an empty or entity-less source instead of throwing", () => {
  assert.deepEqual(tryDslToExcalidraw("  \n"), { ok: false, error: "empty DSL source" });
  const res = tryDslToExcalidraw("not a domain model\n");
  assert.equal(res.ok, false);
  if (!res.ok) assert.match(res.error, /no entities/);
});
