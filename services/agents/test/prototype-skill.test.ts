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
 * The prototype skill restates the v1 registry for the model, and
 * `@aep/prototype-model` is the source of truth for it. These pin the two
 * together: a node kind or action the model adds is a compile error here until
 * the skill lists it, and the skill's worked example is a document the write
 * gate accepts — an example the gate refuses teaches the agent to be refused.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { parsePrototypeModel, type PrototypeAction, type PrototypeNodeKind } from "@aep/prototype-model";

const SKILL = fs.readFileSync(
  path.resolve(fileURLToPath(import.meta.url), "../../../../skills/prototype/SKILL.md"),
  "utf8",
);

const NODE_KINDS = {
  stack: true,
  grid: true,
  split: true,
  detail: true,
  breadcrumbs: true,
  tabs: true,
  stepper: true,
  text: true,
  heading: true,
  badge: true,
  stat: true,
  alert: true,
  "empty-state": true,
  button: true,
  link: true,
  form: true,
  "validation-summary": true,
  table: true,
  filters: true,
  timeline: true,
  "approval-panel": true,
  "task-queue": true,
} satisfies Record<PrototypeNodeKind, true>;

const ACTION_KINDS = {
  navigate: true,
  "set-tab": true,
  "set-step": true,
  "select-row": true,
  "show-dialog": true,
  "show-drawer": true,
  "close-overlay": true,
} satisfies Record<PrototypeAction["kind"], true>;

/** The first cell of every `| \`kind\` | … |` table row. */
function tableKinds(): Set<string> {
  return new Set([...SKILL.matchAll(/^\| `([a-z-]+)` \|/gm)].map((m) => m[1]!));
}

test("the skill lists every registry node and every action, and nothing else", () => {
  const listed = tableKinds();
  const known = [...Object.keys(NODE_KINDS), ...Object.keys(ACTION_KINDS)];
  for (const kind of known) assert.ok(listed.has(kind), `the skill does not list \`${kind}\``);
  for (const kind of listed) assert.ok(known.includes(kind), `the skill lists \`${kind}\`, which the model does not have`);
});

test("the skill's worked example is a prototype the write gate accepts", () => {
  const example = /```json\n([\s\S]*?)\n```/.exec(SKILL)?.[1];
  assert.ok(example, "the skill carries a JSON example");
  const doc = JSON.parse(example) as { component: string };
  const result = parsePrototypeModel(doc, { component: doc.component });
  assert.ok(result.ok, result.ok ? "" : JSON.stringify(result.issues));
});

test("the skill names no design-system vendor — the org's design-system skill does", () => {
  assert.doesNotMatch(SKILL, /astryx|@astryxdesign|\boxygen\b|@wso2\/oxygen/i);
});
