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
import { FileBundle, type LoadSkillResult, type LoadSkillReferenceResult } from "@aep/agent-stream";
import { buildFileTools, LOAD_SKILL, LOAD_SKILL_REFERENCE } from "../src/agents/main/tools/files.js";
import { testSkillSource, type TestSkill } from "./skill-source.js";

const SKILL_LIST: TestSkill[] = [
  { name: "component-architecture", description: "deriving components", content: "Components live at specs/design/components/<name>/design.md." },
  { name: "openapi-conventions", description: "openapi", content: "operationId is lowerCamelCase" },
];
const SKILLS = testSkillSource(SKILL_LIST);

/** A library where one skill carries reference files (agentskills.io structure). */
const SKILLS_WITH_REFS = testSkillSource([
  ...SKILL_LIST,
  {
    name: "excalidraw-diagrams",
    description: "architecture diagrams",
    content: "For the JSON element vocabulary read references/schema.md.",
    references: { "references/schema.md": "rectangles, bound arrows, text labels" },
  },
]);

type SkillSourceArg = ReturnType<typeof testSkillSource>;
type LoadSkillExec = (i: { names: string[] }, o: unknown) => Promise<LoadSkillResult>;
const loadSkillExec = (skills: SkillSourceArg): LoadSkillExec =>
  buildFileTools(new FileBundle({}), skills)[LOAD_SKILL]!.execute as unknown as LoadSkillExec;

test("buildFileTools omits loadSkill when no skills are supplied (skill-free = today)", () => {
  const tools = buildFileTools(new FileBundle({}));
  assert.equal(LOAD_SKILL in tools, false);
  // The file-mutation tools are always present, in declaration order.
  assert.deepEqual(Object.keys(tools), ["addFile", "editFile", "removeFile"]);
});

test("buildFileTools registers loadSkill when skills are supplied", () => {
  assert.ok(buildFileTools(new FileBundle({}), SKILLS)[LOAD_SKILL]);
});

test("loadSkill returns every requested body in one call, in request order", async () => {
  const res = await loadSkillExec(SKILLS)({ names: ["component-architecture", "openapi-conventions"] }, {});
  assert.equal(res.ok, true);
  if (res.ok) {
    assert.deepEqual(
      res.skills.map((s) => s.name),
      ["component-architecture", "openapi-conventions"],
    );
    assert.match(res.skills[0]!.content, /specs\/design\/components/);
  }
});

test("loadSkill miss is self-correctable AND partial: resolved bodies + missing + available", async () => {
  const res = await loadSkillExec(SKILLS)({ names: ["component-architecture", "nope"] }, {});
  assert.equal(res.ok, false);
  if (!res.ok) {
    assert.match(res.error, /unknown skills: nope/);
    // What resolved is still delivered — the model re-calls for the miss only.
    assert.deepEqual(res.skills.map((s) => s.name), ["component-architecture"]);
    assert.deepEqual(res.missing, ["nope"]);
    assert.deepEqual(res.available, ["component-architecture", "openapi-conventions"]);
  }
});

// --- references (agentskills.io structure: SKILL.md + references/) ----------

type LoadSkillRefExec = (i: { name: string; path: string }, o: unknown) => Promise<LoadSkillReferenceResult>;
const loadSkillRefExec = (skills: SkillSourceArg): LoadSkillRefExec =>
  buildFileTools(new FileBundle({}), skills)[LOAD_SKILL_REFERENCE]!.execute as unknown as LoadSkillRefExec;

test("loadSkill lists a skill's reference paths in a success result", async () => {
  const res = await loadSkillExec(SKILLS_WITH_REFS)({ names: ["excalidraw-diagrams"] }, {});
  assert.equal(res.ok, true);
  if (res.ok) assert.deepEqual(res.skills[0]!.references, ["references/schema.md"]);
});

test("loadSkill omits the references listing for a skill without references", async () => {
  const res = await loadSkillExec(SKILLS_WITH_REFS)({ names: ["openapi-conventions"] }, {});
  assert.equal(res.ok, true);
  if (res.ok) assert.equal(res.skills[0]!.references, undefined);
});

test("buildFileTools registers loadSkillReference only when a skill carries references", () => {
  assert.ok(buildFileTools(new FileBundle({}), SKILLS_WITH_REFS)[LOAD_SKILL_REFERENCE]);
  assert.equal(LOAD_SKILL_REFERENCE in buildFileTools(new FileBundle({}), SKILLS), false);
  assert.equal(LOAD_SKILL_REFERENCE in buildFileTools(new FileBundle({})), false);
});

test("loadSkillReference returns a reference body", async () => {
  const res = await loadSkillRefExec(SKILLS_WITH_REFS)(
    { name: "excalidraw-diagrams", path: "references/schema.md" },
    {},
  );
  assert.equal(res.ok, true);
  if (res.ok) assert.match(res.content, /bound arrows/);
});

test("loadSkillReference miss on unknown skill lists the skills that have references", async () => {
  const res = await loadSkillRefExec(SKILLS_WITH_REFS)({ name: "nope", path: "references/schema.md" }, {});
  assert.equal(res.ok, false);
  if (!res.ok) {
    assert.match(res.error, /unknown skill: nope/);
    assert.deepEqual(res.available, ["excalidraw-diagrams"]);
  }
});

test("loadSkillReference miss on unknown path lists that skill's reference paths", async () => {
  const res = await loadSkillRefExec(SKILLS_WITH_REFS)(
    { name: "excalidraw-diagrams", path: "references/nope.md" },
    {},
  );
  assert.equal(res.ok, false);
  if (!res.ok) {
    assert.match(res.error, /unknown reference/);
    assert.deepEqual(res.available, ["references/schema.md"]);
  }
});
