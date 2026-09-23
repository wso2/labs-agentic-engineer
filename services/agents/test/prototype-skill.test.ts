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

/** The skill's prose with line wrapping undone, so a phrase matches across a break. */
const PROSE = SKILL.replace(/\s+/g, " ");

// Live bug (#817): asked for an account button and sign-out on a screen, the
// design agent declined, claiming the platform draws a user menu on every
// screen with a navigation — true of the BUILT app's shell, false of the
// review renderer. The skill has to say exactly what the renderer draws.
test("the skill says the review renderer draws only the model's nodes and its named navigation", () => {
  assert.match(PROSE, /renderer draws only/i);
  assert.match(PROSE, /navigationId/);
  assert.match(PROSE, /no header, user menu, account menu, sign-out/i);
  // The reviewer's ask for such chrome is modelled, or declined out loud.
  assert.match(PROSE, /model it in the registry/i);
  assert.match(PROSE, /outside the v1 registry/i);
});

test("the skill scopes the built app's shell to the build, not the review renderer", () => {
  assert.match(PROSE, /describes? the built application, not the review renderer/i);
  assert.match(PROSE, /wireframes\.dsl/);
});

test("a feedback revision answers every annotation — applied, or declined with the registry reason", () => {
  assert.match(PROSE, /every annotation is either applied or explicitly declined/i);
  assert.match(PROSE, /registry/i);
});

test("the skill asks for validation-error and failure states where the API has error responses", () => {
  assert.match(PROSE, /validation errors?/i);
  assert.match(PROSE, /error responses?/i);
  assert.match(PROSE, /state\.validation-error/);
  assert.match(PROSE, /state\.failed/);
  assert.match(PROSE, /state\.delayed/);
});

// The design-system skill rides with `/prototype` too, and its "implementing a
// wireframe" mapping (header, user menu, footer) is where the false claim came
// from. It must say it is the coding run's, and that the renderer draws none of it.
const DESIGN_SYSTEM = fs.readFileSync(
  path.resolve(fileURLToPath(import.meta.url), "../../../../skills/oxygen-ui-design-system/SKILL.md"),
  "utf8",
);

test("the design-system skill's wireframe mapping is scoped to the coding run", () => {
  const section = (/## Implementing a wireframe with Oxygen\n([\s\S]*?)\n\|/.exec(DESIGN_SYSTEM)?.[1] ?? "").replace(/\s+/g, " ");
  assert.match(section, /coding run/i);
  assert.match(section, /review renderer draws none of/i);
});
