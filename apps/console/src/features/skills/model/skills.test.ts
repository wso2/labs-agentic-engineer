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

import { describe, expect, it } from "vitest";
import type { components } from "../../../generated/aep-api";
import { inConflict, normalizeKind, skillRows, takeableUpdates } from "./skills";

type SkillSummary = components["schemas"]["SkillSummary"];
type SkillUpdate = components["schemas"]["SkillUpdate"];

function skill(name: string, kind: string, description: string, enabled = true): SkillSummary {
  return { name, kind, description, enabled, editable: kind !== "platform", deletable: kind !== "platform", contentSha: "x", required: false };
}

const skills = [
  skill("react-webapp", "org", "Build a React web app"),
  skill("aep", "platform", "The coding run's workflow"),
  skill("pdf-tools", "imported", "Read and write PDFs", false),
  skill("go", "custom", "Go services"),
];

const updates: SkillUpdate[] = [
  { name: "react-webapp", state: "conflict" },
  { name: "go", state: "update" },
  { name: "security-design", state: "update" },
  { name: "ballerina", state: "overridden" },
];

describe("skillRows", () => {
  it("lists every skill by name, each with its kind and tags", () => {
    expect(skillRows(skills, updates, { query: "", kind: "all" }).map((r) => [r.skill.name, r.kind, r.disabled, r.toReview])).toEqual([
      ["aep", "platform", false, false],
      ["go", "org", false, false],
      ["pdf-tools", "imported", true, false],
      ["react-webapp", "org", false, true],
    ]);
  });

  it("keeps the chosen kind only, the retired custom counting as org", () => {
    expect(skillRows(skills, [], { query: "", kind: "org" }).map((r) => r.skill.name)).toEqual(["go", "react-webapp"]);
    expect(skillRows(skills, [], { query: "", kind: "platform" }).map((r) => r.skill.name)).toEqual(["aep"]);
  });

  it("searches the name, the description and the kind's label, ignoring case", () => {
    expect(skillRows(skills, [], { query: "PDF", kind: "all" }).map((r) => r.skill.name)).toEqual(["pdf-tools"]);
    expect(skillRows(skills, [], { query: "workflow", kind: "all" }).map((r) => r.skill.name)).toEqual(["aep"]);
    expect(skillRows(skills, [], { query: "imported", kind: "all" }).map((r) => r.skill.name)).toEqual(["pdf-tools"]);
    expect(skillRows(skills, [], { query: "web", kind: "imported" })).toEqual([]);
  });
});

describe("platform updates", () => {
  it("takes only the skills the org never edited", () => {
    expect(takeableUpdates(updates)).toEqual(["go", "security-design"]);
    expect(takeableUpdates([])).toEqual([]);
  });

  it("knows a skill both the platform and the org changed", () => {
    expect(inConflict(updates, "react-webapp")).toBe(true);
    expect(inConflict(updates, "go")).toBe(false);
    expect(inConflict(updates, "ballerina")).toBe(false);
  });
});

describe("normalizeKind", () => {
  it("reads an unknown kind as org", () => {
    expect(normalizeKind("platform")).toBe("platform");
    expect(normalizeKind("builtin")).toBe("org");
  });
});
