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
import { testSkillSource, type TestSkill } from "./skill-source.js";
import type { SkillSource } from "../src/agents/main/skill-source.js";
import {
  instructions,
  taskPlanInstructions,
  buildInstructions,
  buildTaskPlanInstructions,
  buildSkillCatalog,
  buildEagerSkillsBlock,
  buildOrgDefaultsBlock,
  buildNarrationBlock,
} from "../src/agents/main/prompt.js";

const SKILL_LIST: TestSkill[] = [
  { name: "a-skill", description: "does A", content: "BODY A — secret guidance" },
  { name: "b-skill", description: "does B", content: "BODY B — secret guidance" },
];
const SKILLS = testSkillSource(SKILL_LIST);

test("no skills → catalog is empty and instructions are byte-identical to base", () => {
  assert.equal(buildSkillCatalog(undefined), "");
  assert.equal(buildSkillCatalog(testSkillSource([])), "");
  assert.equal(buildInstructions(), instructions);
  assert.equal(buildInstructions(testSkillSource([])), instructions);
});

test("catalog lists name+description, appended at the END (base prefix preserved)", () => {
  const out = buildInstructions(SKILLS);
  assert.ok(out.startsWith(instructions), "base instructions stay the cacheable prefix");
  assert.match(out, /# Skills/);
  assert.match(out, /- a-skill: does A/);
  assert.match(out, /- b-skill: does B/);
  assert.match(out, /loadSkill/);
});

test("catalog NEVER inlines skill bodies (progressive disclosure)", () => {
  const out = buildInstructions(SKILLS);
  assert.doesNotMatch(out, /BODY A/);
  assert.doesNotMatch(out, /secret guidance/);
});

test("catalog mentions loadSkillReference only when a skill carries references", () => {
  const withRefs = testSkillSource([
    ...SKILL_LIST,
    {
      name: "c-skill",
      description: "does C",
      content: "see references/deep.md",
      references: { "references/deep.md": "REF BODY — never inlined" },
    },
  ]);
  const out = buildInstructions(withRefs);
  assert.match(out, /loadSkillReference/);
  assert.doesNotMatch(out, /REF BODY/); // reference bodies are third-level, never in the prompt
  assert.doesNotMatch(buildInstructions(SKILLS), /loadSkillReference/); // refs-free library = today's catalog
});

test("eager skills inline resolved bodies into a per-turn block (#335)", () => {
  const block = buildEagerSkillsBlock(SKILLS, ["a-skill"]);
  assert.match(block, /ALREADY LOADED/);
  assert.match(block, /## Skill: a-skill/);
  assert.match(block, /BODY A — secret guidance/);
  assert.doesNotMatch(block, /BODY B/);
});

/**
 * `load()` has three states, and a refusal is neither a body nor a missing name.
 * The design flow inlines `wireframes` and `openapi-conventions`, which an org may
 * legitimately mark coding-only — a refusal there must cost that one body, not the
 * turn.
 */
test("eager skills: an audience refusal skips like a missing name, never throws", () => {
  const refusing: SkillSource = {
    catalog: () => [{ name: "a-skill", description: "does A", hasReferences: false, audience: ["coding"] }],
    load: (name) => (name === "a-skill" ? { refused: true } : undefined),
    loadReference: () => undefined,
  };
  assert.equal(buildEagerSkillsBlock(refusing, ["a-skill"]), "");
  // Mixed: the readable body still lands, the refused one is simply absent.
  const mixed: SkillSource = {
    catalog: () => refusing.catalog(),
    load: (name) => (name === "a-skill" ? { refused: true } : SKILLS.load(name)),
    loadReference: () => undefined,
  };
  const block = buildEagerSkillsBlock(mixed, ["a-skill", "b-skill"]);
  assert.match(block, /## Skill: b-skill/);
  assert.doesNotMatch(block, /## Skill: a-skill/);
});

test("eager skills: unknown names skip; nothing resolved → empty string", () => {
  assert.equal(buildEagerSkillsBlock(SKILLS, ["nope"]), "");
  assert.equal(buildEagerSkillsBlock(SKILLS, []), "");
  assert.equal(buildEagerSkillsBlock(SKILLS, undefined), "");
  assert.equal(buildEagerSkillsBlock(undefined, ["a-skill"]), "");
  const mixed = buildEagerSkillsBlock(SKILLS, ["nope", "b-skill"]);
  assert.match(mixed, /## Skill: b-skill/);
  assert.doesNotMatch(mixed, /nope/);
});

test("eager skills never touch the SYSTEM instructions (cacheable prefix)", () => {
  assert.equal(buildInstructions(SKILLS), instructions + buildSkillCatalog(SKILLS));
});

// --- Organization defaults --------------------------------------------------
//
// The org's standing decisions ride the SYSTEM prompt of every turn rather than
// a per-flow eager block: they answer interview questions the agent would
// otherwise put to the user, and pin providers at design time, on every turn
// regardless of flow.

const ORG: TestSkill = {
  name: "organization",
  description: "The organization's settled decisions.",
  content: "## Authentication & identity\n\nUse Thunder.",
};

test("the org defaults are inlined into the system prompt, body and all", () => {
  const out = buildInstructions(testSkillSource([...SKILL_LIST, ORG]));
  assert.match(out, /# Organization defaults/);
  assert.match(out, /Use Thunder\./, "the BODY is inlined — unlike every other skill");
});

test("the org skill is never catalogued — its body is already in the prompt", () => {
  const skills = testSkillSource([...SKILL_LIST, ORG]);
  // A catalog line would offer a loadSkill round-trip returning text the agent
  // is already holding, and charge for the description a second time.
  assert.ok(!buildSkillCatalog(skills).includes("organization"));
  // Its heading is the composer's alone, so it appears exactly once.
  assert.equal(buildInstructions(skills).match(/# Organization defaults/g)?.length, 1);
});

test("a library of nothing but the org skill renders no catalog at all", () => {
  assert.equal(buildSkillCatalog(testSkillSource([ORG])), "");
});

test("the org defaults trail the catalog, leaving the base prefix intact", () => {
  const skills = testSkillSource([...SKILL_LIST, ORG]);
  assert.equal(buildInstructions(skills), instructions + buildSkillCatalog(skills) + buildOrgDefaultsBlock(skills));
  assert.ok(buildInstructions(skills).startsWith(instructions));
});

test("no org skill in the snapshot → byte-identical to a turn without it", () => {
  // An older org, or one that never seeded the skill: the prompt must not grow
  // an empty heading.
  assert.equal(buildOrgDefaultsBlock(SKILLS), "");
  assert.equal(buildOrgDefaultsBlock(undefined), "");
  assert.equal(buildOrgDefaultsBlock(testSkillSource([{ ...ORG, content: "   " }])), "");
  assert.equal(buildInstructions(SKILLS), instructions + buildSkillCatalog(SKILLS));
});

test("the task planner gets the org defaults too", () => {
  const out = buildTaskPlanInstructions(testSkillSource([ORG]));
  assert.match(out, /# Organization defaults/);
  assert.match(out, /Use Thunder\./);
  assert.ok(out.startsWith(taskPlanInstructions), "the planner's own charter still leads");
});

// --- Narration policy (#580) ------------------------------------------------
//
// The right vocabulary belongs to the SURFACE, not to the skill: the shared
// flow skills are byte-identical everywhere, and the console's caller names its
// surface so the console's narration rules ride that turn's system prompt.

const CONSOLE: TestSkill = {
  name: "console",
  description: "How the agent speaks to someone working in the console.",
  content: "## Never quote a repo path\n\nName the artifact instead.",
};

const WITH_CONSOLE = testSkillSource([...SKILL_LIST, CONSOLE]);

test("a surface-free turn is byte-identical to one composed before surfaces existed", () => {
  assert.equal(buildNarrationBlock(WITH_CONSOLE, undefined), "");
  assert.equal(buildInstructions(WITH_CONSOLE), instructions + buildSkillCatalog(WITH_CONSOLE));
  assert.equal(buildTaskPlanInstructions(WITH_CONSOLE), taskPlanInstructions + buildSkillCatalog(WITH_CONSOLE));
});

test("a console turn inlines the console skill's body as standing policy", () => {
  const out = buildInstructions(WITH_CONSOLE, "console");
  assert.match(out, /# Narration policy/);
  assert.match(out, /Never quote a repo path/, "the BODY is inlined — the agent must not have to load it");
  assert.equal(out.match(/# Narration policy/g)?.length, 1, "the heading is the composer's alone");
});

test("the narration policy is LAST, so it outranks a loaded skill's own narration", () => {
  const skills = testSkillSource([...SKILL_LIST, ORG, CONSOLE]);
  const out = buildInstructions(skills, "console");
  assert.equal(out, instructions + buildSkillCatalog(skills) + buildOrgDefaultsBlock(skills) + buildNarrationBlock(skills, "console"));
  assert.ok(out.indexOf("# Narration policy") > out.indexOf("# Organization defaults"));
  // The base meta-rule is what yields to it; without that clause the block is
  // just one more voice arguing with the loaded skill.
  assert.match(instructions, /unless a standing narration policy in this prompt overrides it/);
});

test("the plan turn gets the policy too — its closing note is read on the same screen", () => {
  assert.match(buildTaskPlanInstructions(WITH_CONSOLE, "console"), /# Narration policy/);
});

test("a narration skill is never catalogued, on any surface", () => {
  // Its body is standing policy, not guidance for a task: a catalog line would
  // offer a round-trip returning either text the agent holds (console turn) or
  // rules addressed to somebody else's user (a local run).
  assert.ok(!buildSkillCatalog(WITH_CONSOLE).includes("console"));
  assert.equal(buildSkillCatalog(testSkillSource([CONSOLE])), "");
});

test("a surface whose skill is missing, refused or empty leaves the prompt untouched", () => {
  assert.equal(buildNarrationBlock(SKILLS, "console"), "", "missing name");
  assert.equal(buildNarrationBlock(undefined, "console"), "", "no skill source at all");
  assert.equal(buildNarrationBlock(testSkillSource([{ ...CONSOLE, content: "   " }]), "console"), "", "empty body");
  const refusing: SkillSource = {
    catalog: () => [],
    load: () => ({ refused: true }),
    loadReference: () => undefined,
  };
  assert.equal(buildNarrationBlock(refusing, "console"), "", "an audience refusal is not a body");
});

test("the eager block carries the override, in the same message as the text it overrides", () => {
  // `design` and `architecture` still mandate a `specs/design/` pointer, and
  // they arrive in the USER prompt — more recent and more specific than the
  // system-prompt policy. The override has to be stated where they are.
  const withSurface = buildEagerSkillsBlock(SKILLS, ["a-skill"], "console");
  assert.match(withSurface, /Narration policy in your system instructions overrides it/);
  assert.ok(
    withSurface.indexOf("Narration policy") > withSurface.indexOf("BODY A"),
    "the override is the block's last word, after the guidance it outranks",
  );
  // A local run gets the block it always got.
  assert.equal(buildEagerSkillsBlock(SKILLS, ["a-skill"]), buildEagerSkillsBlock(SKILLS, ["a-skill"], undefined));
  assert.doesNotMatch(buildEagerSkillsBlock(SKILLS, ["a-skill"]), /Narration policy/);
});
