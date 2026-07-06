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
 * Pins the post-unification prompt contract (ADR-0005): the dependency-authoring
 * judgment is carried by the pushed `high-level-architecture` skill body, NOT
 * inlined in the system prompt. The system prompt keeps only wire/output
 * scaffolding + a pointer to the skill; buildUserPrompt inlines the skill body
 * under "Platform skills — MUST consult".
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { systemPrompt, buildUserPrompt } from "./prompt.js";
import { DesignDoc } from "./doc.js";
import type { ArchitectInput } from "./schema.js";

const baseInput: ArchitectInput = {
  projectName: "orders",
  spec: "# Orders\nOne service.",
};

// A stand-in for the real skill body aep-api pushes — the test asserts the body
// text is transplanted verbatim into the assembled prompt, not its exact prose.
const HLA_SKILL = {
  name: "high-level-architecture",
  description: "Turn requirements into a design.",
  body: "# High-level architecture\n\nSENTINEL: persistence ⇒ platform-resource trigger and org-service verbatim catalog name.",
};

test("systemPrompt no longer inlines the four-kinds dependency judgment", () => {
  // The judgment moved to the skill. The prompt must NOT restate the discovery
  // ordering / verbatim-name / config-derivation prose that used to live inline.
  assert.ok(
    !systemPrompt.includes("External dependency discovery (web_search)"),
    "the web_search discovery walkthrough must move to the skill",
  );
  assert.ok(
    !systemPrompt.includes("Reuse-first"),
    "the reuse-first discovery ordering must move to the skill",
  );
  assert.ok(
    !systemPrompt.includes("securitySchemes"),
    "the securityScheme→config derivation must move to the skill",
  );
  assert.ok(
    !systemPrompt.includes("SPA secret rule"),
    "the web-app secret rule must move to the skill",
  );
});

test("systemPrompt keeps the skill pointer + the wire-contract invariant", () => {
  // Scaffolding that STAYS: point at the skill for authoring judgment, and keep
  // the never-emit-status/reason wire invariant (schema + route enforce it too).
  assert.ok(
    systemPrompt.includes("high-level-architecture"),
    "the prompt must point at the high-level-architecture skill for dependency authoring",
  );
  assert.match(systemPrompt, /never emit a `status` or `reason`/);
});

test("buildUserPrompt inlines the pushed high-level-architecture skill body", () => {
  const doc = DesignDoc.fromPrevious(undefined);
  const prompt = buildUserPrompt({ ...baseInput, highLevelArchitectureSkill: HLA_SKILL }, doc);

  assert.ok(
    prompt.includes("## Platform skills — MUST consult before designing"),
    "the platform-skills section must render when a skill is pushed",
  );
  assert.ok(prompt.includes("### high-level-architecture"), "the skill heading must render");
  assert.ok(
    prompt.includes("SENTINEL: persistence ⇒ platform-resource trigger"),
    "the full skill body must be transplanted into the prompt",
  );
});

test("buildUserPrompt inlines high-level-architecture BEFORE the stack builtins", () => {
  const doc = DesignDoc.fromPrevious(undefined);
  const prompt = buildUserPrompt(
    {
      ...baseInput,
      highLevelArchitectureSkill: HLA_SKILL,
      builtinSkills: [{ name: "go", description: "Go stack", body: "GO_SKILL_BODY" }],
    },
    doc,
  );
  assert.ok(prompt.indexOf("### high-level-architecture") < prompt.indexOf("### go"), "HLA leads the platform skills");
});

test("buildUserPrompt without a pushed HLA skill degrades to the compact fallback (planner parity)", () => {
  const doc = DesignDoc.fromPrevious(undefined);
  const prompt = buildUserPrompt(baseInput, doc);
  // The section still renders — carrying the fallback — so the scaffolding's
  // "judgment lives in the high-level-architecture Platform skill below"
  // pointer never dangles (the drift that motivated ADR-0005).
  assert.ok(prompt.includes("## Platform skills — MUST consult"), "fallback section renders");
  assert.ok(prompt.includes("### high-level-architecture"), "fallback keeps the skill heading the pointer targets");
  assert.ok(prompt.includes("compact fallback"), "fallback body present");
  assert.ok(prompt.includes("EXACTLY AND VERBATIM"), "org-service verbatim-name rule survives degradation");
  assert.ok(prompt.includes("never invent instance parameters"), "no-invented-params rule survives degradation");
});
