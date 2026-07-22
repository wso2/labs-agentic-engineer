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
import { instructions, buildInstructions, buildSkillCatalog } from "../src/agents/main/prompt.js";

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
