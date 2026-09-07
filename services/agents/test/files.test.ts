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
import { buildFileToolSet, LOAD_SKILL, LOAD_SKILL_REFERENCE } from "../src/agents/main/tools/files.js";
import { SkillReadError, type SkillSource } from "../src/agents/main/skill-source.js";
import { testSkillSource, type TestSkill } from "./skill-source.js";

const SKILL_LIST: TestSkill[] = [
  { name: "component-architecture", description: "deriving components", content: "Components live at specs/design/components/<name>/design.json." },
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
    references: {
      "references/schema.md": "rectangles, bound arrows, text labels",
      "assets/logo.png": "<binary bytes, never inlined>",
    },
    binaryReferences: ["assets/logo.png"],
  },
]);

type SkillSourceArg = ReturnType<typeof testSkillSource>;
type LoadSkillExec = (i: { names: string[] }, o: unknown) => Promise<LoadSkillResult>;
const loadSkillExec = (skills: SkillSourceArg): LoadSkillExec =>
  buildFileToolSet(new FileBundle({}), skills).tools[LOAD_SKILL]!.execute as unknown as LoadSkillExec;

test("buildFileToolSet omits loadSkill when no skills are supplied (skill-free = today)", () => {
  const tools = buildFileToolSet(new FileBundle({})).tools;
  assert.equal(LOAD_SKILL in tools, false);
  // The file-mutation tools plus the always-registered UI tools, in
  // declaration order: the HITL questions (console ADR-0012 / #270) and the
  // fire-and-forget plan declaration (ADR-0022 / #576).
  assert.deepEqual(Object.keys(tools), [
    "addFile",
    "editFile",
    "removeFile",
    "ask_question",
    "ask_questions",
    "declare_plan",
  ]);
});

test("buildFileToolSet registers loadSkill when skills are supplied", () => {
  assert.ok(buildFileToolSet(new FileBundle({}), SKILLS).tools[LOAD_SKILL]);
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
  buildFileToolSet(new FileBundle({}), skills).tools[LOAD_SKILL_REFERENCE]!.execute as unknown as LoadSkillRefExec;

test("loadSkill lists a skill's reference paths in a success result", async () => {
  const res = await loadSkillExec(SKILLS_WITH_REFS)({ names: ["excalidraw-diagrams"] }, {});
  assert.equal(res.ok, true);
  if (res.ok) assert.deepEqual(res.skills[0]!.references, ["assets/logo.png", "references/schema.md"]);
});

test("loadSkill omits the references listing for a skill without references", async () => {
  const res = await loadSkillExec(SKILLS_WITH_REFS)({ names: ["openapi-conventions"] }, {});
  assert.equal(res.ok, true);
  if (res.ok) assert.equal(res.skills[0]!.references, undefined);
});

test("buildFileToolSet registers loadSkillReference only when a skill carries references", () => {
  assert.ok(buildFileToolSet(new FileBundle({}), SKILLS_WITH_REFS).tools[LOAD_SKILL_REFERENCE]);
  assert.equal(LOAD_SKILL_REFERENCE in buildFileToolSet(new FileBundle({}), SKILLS).tools, false);
  assert.equal(LOAD_SKILL_REFERENCE in buildFileToolSet(new FileBundle({})).tools, false);
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
    assert.deepEqual(res.available, ["assets/logo.png", "references/schema.md"]);
  }
});

test("loadSkillReference refuses a binary aux file with a corrective error naming the path", async () => {
  const res = await loadSkillRefExec(SKILLS_WITH_REFS)({ name: "excalidraw-diagrams", path: "assets/logo.png" }, {});
  assert.equal(res.ok, false);
  if (!res.ok) {
    assert.equal(res.error, "assets/logo.png is a binary file — it cannot be loaded into context");
    assert.deepEqual(res.available, ["assets/logo.png", "references/schema.md"]);
  }
});

test("loadSkill I/O fault returns could-not-read — never unknown skills", async () => {
  const base = testSkillSource(SKILL_LIST);
  const ioFault: SkillSource = {
    catalog: () => base.catalog(),
    load: (name) => {
      if (name === "openapi-conventions") throw new SkillReadError("/snap/skills/openapi/SKILL.md");
      return base.load(name);
    },
    loadReference: (n, p) => base.loadReference(n, p),
  };
  const res = await loadSkillExec(ioFault as SkillSourceArg)(
    { names: ["component-architecture", "openapi-conventions"] },
    {},
  );
  assert.equal(res.ok, false);
  if (!res.ok) {
    assert.match(res.error, /could not read skill openapi-conventions/);
    assert.doesNotMatch(res.error, /unknown skills/);
    assert.deepEqual(res.missing, []);
    assert.deepEqual(
      res.skills.map((s) => s.name),
      ["component-architecture"],
    );
  }
});

// #576: declaring a plan must NOT end the turn — the agent says what it is
// about to write and then writes it. The call site pairs `hasToolCall` stop
// conditions with the question tools only; here we hold the other half of that
// contract: the tool resolves immediately rather than parking on the user.
test("declare_plan acknowledges and resolves — it never awaits a human", async () => {
  const tools = buildFileToolSet(new FileBundle({})).tools;
  const declare = tools["declare_plan"]!;
  const exec = declare.execute as unknown as (
    input: { paths: string[] },
  ) => Promise<{ status: string; paths: string[] }>;
  const out = await exec({ paths: ["specs/design/design.cell"] });
  assert.equal(out.status, "ok");
  assert.deepEqual(out.paths, ["specs/design/design.cell"]);
});
