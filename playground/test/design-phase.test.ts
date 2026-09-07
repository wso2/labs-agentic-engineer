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
 * Design-phase mock tests: gate, folded design bundle (incl. the model-authored
 * skillsPinned through the REAL write-gate), derived artifacts, `check`.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { mockModel } from "@aep/agents/shared/mock-model";
import { designCommand } from "../src/commands.js";
import { checkProject } from "../src/engine/check.js";

const VALID_DESIGN = {
  name: "user-service",
  type: "service",
  version: "0.1.0",
  language: "go",
  buildpack: "docker",
  appPath: "./user-service",
  entrypoint: "cmd/main.go",
  exposure: "internet",
  dependencies: [],
  description: "Manages users",
  skillsPinned: ["go"],
};

const FLOW_DSL = `screen Login
  input "Email"
  button "Sign In" primary
`;

function seedProject(): string {
  const dir = mkdtempSync(join(tmpdir(), "aep-play-design-"));
  mkdirSync(join(dir, "specs/requirements"), { recursive: true });
  writeFileSync(join(dir, "specs/requirements/prd.md"), "# Requirements\n\n- login\n");
  return dir;
}

function tempSkills(): string {
  const dir = mkdtempSync(join(tmpdir(), "aep-play-skills-"));
  mkdirSync(join(dir, "architecture"), { recursive: true });
  writeFileSync(
    join(dir, "architecture", "SKILL.md"),
    "---\nname: architecture\ndescription: architecture flow\n---\n\nAuthor skillsPinned in design.json.\n",
  );
  return dir;
}

test("design phase: folds the bundle, gates skillsPinned shape, derives .excalidraw, check passes", async () => {
  const projectDir = seedProject();
  const skillsDir = tempSkills();
  try {
    const model = mockModel([
      { kind: "toolCall", toolCallId: "d1", toolName: "addFile", input: { path: "specs/design/domain-model.md", content: "# Domain model\n" } },
      {
        kind: "toolCall",
        toolCallId: "d2",
        toolName: "addFile",
        input: { path: "specs/design/components/user-service/design.json", content: JSON.stringify(VALID_DESIGN, null, 2) },
      },
      {
        kind: "toolCall",
        toolCallId: "d3",
        toolName: "addFile",
        input: { path: "specs/design/components/user-service/wireframes.dsl", content: FLOW_DSL },
      },
      { kind: "text", text: "Design generated." },
    ]);
    const outcome = await designCommand(projectDir, { model, skillsDir, silent: true });
    assert.equal(outcome.ok, true, outcome.detail);

    const design = JSON.parse(readFileSync(join(projectDir, "specs/design/components/user-service/design.json"), "utf8")) as {
      skillsPinned?: string[];
    };
    assert.deepEqual(design.skillsPinned, ["go"], "model-authored skillsPinned landed");
    assert.ok(
      existsSync(join(projectDir, "specs/design/components/user-service/wireframes.excalidraw")),
      "derived .excalidraw materialized",
    );
    assert.ok(
      existsSync(join(projectDir, "specs/design/cell-diagram.gen.json")),
      "aggregate cell-diagram projected as design files streamed in",
    );
    assert.ok(checkProject(projectDir).every((f) => f.ok), "check is green");
  } finally {
    rmSync(projectDir, { recursive: true, force: true });
    rmSync(skillsDir, { recursive: true, force: true });
  }
});

test("design phase is blocked without requirements", async () => {
  const projectDir = mkdtempSync(join(tmpdir(), "aep-play-design-"));
  try {
    const outcome = await designCommand(projectDir, { silent: true, model: mockModel([{ kind: "text", text: "x" }]) });
    assert.equal(outcome.ok, false);
    assert.match(outcome.detail ?? "", /requirements/);
  } finally {
    rmSync(projectDir, { recursive: true, force: true });
  }
});
