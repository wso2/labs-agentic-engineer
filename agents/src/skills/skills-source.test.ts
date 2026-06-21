// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

import { test } from "node:test";
import assert from "node:assert/strict";

import type { ArchitectInput } from "../agents/architect/schema.js";
import type {
  TechLeadDetailItem,
  TechLeadPlanInput,
} from "../agents/tech-lead/schema.js";
import {
  resolveArchitectSkills,
  setArchitectSkillsResolver,
  resetArchitectSkillsResolver,
  resolveTechLeadPlanSkills,
  setTechLeadPlanSkillsResolver,
  resetTechLeadPlanSkillsResolver,
  resolveTechLeadDetailSkills,
  setTechLeadDetailSkillsResolver,
  resetTechLeadDetailSkillsResolver,
} from "./skills-source.js";

const architectInput = {
  builtinSkills: [{ name: "go", description: "Go conventions", body: "BODY" }],
  orgSkills: [{ name: "pci", description: "PCI rules" }],
  skillsApplied: ["go"],
} as unknown as ArchitectInput;

test("architect: default resolver reads skills from the request body", () => {
  const s = resolveArchitectSkills(architectInput);
  assert.equal(s.builtinSkills.length, 1);
  assert.equal(s.builtinSkills[0].name, "go");
  assert.deepEqual(s.skillsApplied, ["go"]);
});

test("architect: injected resolver replaces the request body (test seam)", () => {
  setArchitectSkillsResolver(() => ({
    builtinSkills: [{ name: "fake", description: "d", body: "B" }],
    orgSkills: [],
    skillsApplied: ["fake"],
  }));
  try {
    const s = resolveArchitectSkills(architectInput);
    assert.equal(s.builtinSkills[0].name, "fake");
    assert.deepEqual(s.skillsApplied, ["fake"]);
  } finally {
    resetArchitectSkillsResolver();
  }
  // After reset, the default request-body source is restored.
  assert.equal(resolveArchitectSkills(architectInput).builtinSkills[0].name, "go");
});

test("architect: default resolver tolerates absent skill fields", () => {
  const empty = {} as unknown as ArchitectInput;
  const s = resolveArchitectSkills(empty);
  assert.deepEqual(s.builtinSkills, []);
  assert.deepEqual(s.orgSkills, []);
  assert.deepEqual(s.skillsApplied, []);
});

test("tech-lead plan: default + injected resolver", () => {
  const input = {
    attachedSkills: [{ name: "go", description: "Go" }],
  } as unknown as TechLeadPlanInput;
  assert.equal(resolveTechLeadPlanSkills(input)[0].name, "go");

  setTechLeadPlanSkillsResolver(() => [{ name: "x", description: "y" }]);
  try {
    assert.equal(resolveTechLeadPlanSkills(input)[0].name, "x");
  } finally {
    resetTechLeadPlanSkillsResolver();
  }
  assert.equal(resolveTechLeadPlanSkills(input)[0].name, "go");
});

test("tech-lead detail: default + injected resolver", () => {
  const item = {
    skillsResolved: [{ name: "go", description: "Go", body: "B" }],
  } as unknown as TechLeadDetailItem;
  assert.equal(resolveTechLeadDetailSkills(item)[0].name, "go");

  setTechLeadDetailSkillsResolver(() => [{ name: "z", description: "d", body: "B2" }]);
  try {
    assert.equal(resolveTechLeadDetailSkills(item)[0].name, "z");
  } finally {
    resetTechLeadDetailSkillsResolver();
  }
  assert.equal(resolveTechLeadDetailSkills(item)[0].name, "go");
});
