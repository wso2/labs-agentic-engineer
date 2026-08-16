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
import {
  askQuestionsInputSchema,
  askQuestionsTool,
  buildFileTools,
  LOAD_SKILL,
  LOAD_SKILL_REFERENCE,
} from "../src/agents/main/tools/files.js";
import { SkillReadError, type SkillSource } from "../src/agents/main/skill-source.js";
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
  buildFileTools(new FileBundle({}), skills)[LOAD_SKILL]!.execute as unknown as LoadSkillExec;

test("buildFileTools omits loadSkill when no skills are supplied (skill-free = today)", () => {
  const tools = buildFileTools(new FileBundle({}));
  assert.equal(LOAD_SKILL in tools, false);
  // The file-mutation tools plus the always-registered HITL question tools
  // (console ADR-0012 / #270), in declaration order.
  assert.deepEqual(Object.keys(tools), ["addFile", "editFile", "removeFile", "ask_question", "ask_questions"]);
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
  if (res.ok) assert.deepEqual(res.skills[0]!.references, ["assets/logo.png", "references/schema.md"]);
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

// --- grilling sessions: the round contract the model reads (#486) ------------
//
// A session is reachable only if the model can tell a session ROUND from a
// one-form interview while it is drafting the call. Two things carry that: the
// optional `session` checklist, and the tool text that says when to send it.
// Both are asserted here because both were absent from a live deep-dive run.

test("ask_questions accepts a session round and keeps the checklist verbatim", () => {
  const parsed = askQuestionsInputSchema.parse({
    session: {
      title: "Voting & nominations",
      areas: [
        { name: "Eligibility", state: "done" },
        { name: "Quorum", state: "now" },
        { name: "Nominee limits", state: "todo" },
      ],
    },
    questions: [{ question: "What quorum?", options: [] }],
  });
  assert.deepEqual(parsed.session?.areas.map((a) => a.state), ["done", "now", "todo"]);
});

test("ask_questions still accepts a one-form interview with no session", () => {
  const parsed = askQuestionsInputSchema.parse({ questions: [{ question: "Who?", options: [] }] });
  assert.equal(parsed.session, undefined);
});

test("an area state outside done/now/todo is rejected — the console renders three", () => {
  assert.throws(() =>
    askQuestionsInputSchema.parse({
      session: { areas: [{ name: "Quorum", state: "asking" }] },
      questions: [{ question: "What quorum?", options: [] }],
    }),
  );
});

test("session precedes questions, so the checklist streams before the first question", () => {
  assert.deepEqual(Object.keys(askQuestionsInputSchema.shape), ["session", "questions"]);
});

test("the round-size split is stated where the model always reads it", () => {
  // The 8-question ceiling belongs to one-form mode; a session round takes 1–4.
  // Said on the field itself, so it holds even when no skill is loaded.
  assert.match(askQuestionsInputSchema.shape.questions.description ?? "", /1–4/);
  assert.match(askQuestionsInputSchema.shape.session.description ?? "", /EVERY round/);
});

test("the ask_questions tool names the free-text trigger that opens a session", () => {
  // The live failure: "grill me properly on X" produced one 6-question form.
  const description = askQuestionsTool.description;
  assert.ok(
    typeof description === "string",
    "the tool text must be static — it is the model's only always-on copy of the rule",
  );
  assert.match(description, /grill/i);
  assert.match(description, /1–4/);
  assert.match(description, /session/i);
});
