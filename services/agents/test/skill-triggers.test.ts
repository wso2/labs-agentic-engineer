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
 * Reachability drift guards for the platform skill library (#486).
 *
 * A feature that exists only inside a skill body is reachable only if something
 * in the turn leads the model to that body. This service decides what a turn
 * carries, so the "then the skill must say X" half is pinned here, against the
 * real `skills/` library. Each assertion below stands for a live failure: a
 * deep-dive request that ran a one-form interview, and a `/start` close that
 * offered nothing.
 *
 * These pin PROPERTIES, never wording — a skill is prose and meant to be
 * edited. What may not change silently is that the property survives the edit.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { eagerSkillsFor } from "../src/prompts/turn.js";

const SKILLS_DIR = path.resolve(fileURLToPath(import.meta.url), "../../../../skills");

const skill = (name: string): string => fs.readFileSync(path.join(SKILLS_DIR, name, "SKILL.md"), "utf8");

/** The frontmatter `description` — the only part of a skill an unloaded agent sees. */
function description(body: string): string {
  return /^description:\s*(.+)$/m.exec(body)?.[1] ?? "";
}

/** Prose as one line, emphasis stripped — line wrapping is the author's business. */
function flatten(md: string): string {
  return md.replace(/[*_`]/g, "").replace(/\s+/g, " ");
}

test("a chat turn inlines nothing, so `grilling`'s description must carry the depth trigger", () => {
  // "grill me properly on the voting rules" arrives as an ordinary chat turn:
  // no skill is inlined, and the catalog description is the whole trigger.
  assert.deepEqual(eagerSkillsFor({ kind: "chat", text: "grill me properly on the voting rules" }), []);

  const desc = description(skill("grilling"));
  assert.match(desc, /grill/i, "the description must match the user's own word");
  assert.match(desc, /deeper|dig in|pin down/i, "and the depth asks that do not say 'grill'");
});

test("the grilling skill names both ways into session mode", () => {
  const body = skill("grilling");
  const modes = flatten(body.slice(body.indexOf("## Modes")));
  assert.match(modes, /### Session mode/, "session mode is a mode, not a footnote of one-form mode");
  // The defect this replaced: session mode was enterable by the calling flow only.
  assert.match(modes, /user asks for depth/i, "the free-text trigger");
  assert.match(modes, /a flow opens one/i, "and the flow-opened one it already had");
});

test("a session round is bound to 1–4 questions and must carry the checklist", () => {
  const body = flatten(skill("grilling"));
  assert.match(body, /Rounds are 1–4 questions/, "the live run asked 6 in one form");
  assert.match(body, /session field/, "the round is only a session round if it sends the field");
  assert.match(body, /not a session round/);
});

test("the /start close offers the deep dive BEFORE pointing at the next step", () => {
  const body = skill("start");
  const close = body.slice(body.indexOf("## Where this stops"));
  const offer = close.indexOf("grill");
  const pointer = close.indexOf("specs/requirements/prd.md");
  assert.ok(offer >= 0, "the close must offer a grilling session");
  assert.ok(pointer >= 0, "and still point at the PRD to review");
  assert.ok(
    offer < pointer,
    "a close that ends on review-then-/design reads as the turn's only instruction, and the offer is never taken",
  );
});

test("amend's deepen branch runs a session, not a form", () => {
  const body = skill("amend");
  const branch = body.slice(body.indexOf("## Go deeper on a feature"), body.indexOf("## Resolve open questions"));
  // Markdown emphasis and line wrapping are the author's; the rule is not.
  assert.match(flatten(branch), /session mode/);
});
