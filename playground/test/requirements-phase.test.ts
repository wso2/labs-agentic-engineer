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
 * Mock-model phase tests (docs/design/playground.md §14): the requirements
 * phase + chat resume, end to end through the REAL in-process service —
 * scripted tool-call streams in, folded project files + persisted
 * conversations out. No tokens.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { mockModel } from "@aep/ae-design-agent/shared/mock-model";
import { requirementsCommand, chatTurn } from "../src/commands.js";
import { openSession } from "../src/engine/session.js";
import { readIdea } from "../src/state/descriptor.js";
import { chatSpec } from "../src/engine/turn-spec.js";

function tempProject(): string {
  return mkdtempSync(join(tmpdir(), "aep-play-test-"));
}

function tempSkills(): string {
  const dir = mkdtempSync(join(tmpdir(), "aep-play-skills-"));
  mkdirSync(join(dir, "demo-skill"), { recursive: true });
  writeFileSync(join(dir, "demo-skill", "SKILL.md"), "---\nname: demo-skill\ndescription: demo\n---\n\nDemo body.\n");
  return dir;
}

test("requirements phase: idea → folded requirements.md + captured descriptor + persisted conversation", async () => {
  const projectDir = tempProject();
  const skillsDir = tempSkills();
  try {
    const model = mockModel([
      {
        kind: "toolCall",
        toolCallId: "t1",
        toolName: "addFile",
        input: { path: "specs/requirements/prd.md", content: "# Requirements\n\n- ceramics catalog\n" },
      },
      { kind: "text", text: "Requirements generated." },
    ]);
    const outcome = await requirementsCommand(projectDir, { model, skillsDir, silent: true, idea: "An online ceramics store" });
    assert.equal(outcome.ok, true, outcome.detail);

    assert.match(readFileSync(join(projectDir, "specs/requirements/prd.md"), "utf8"), /ceramics catalog/);
    // --idea is CAPTURED into the descriptor, so a later /start carries the same idea.
    assert.equal(readIdea(projectDir), "An online ceramics store");
    assert.ok(existsSync(join(projectDir, ".aep-playground/conversations/general.json")), "general conversation persisted");
    assert.ok(existsSync(join(projectDir, ".aep-playground/project.json")), "project state persisted");
  } finally {
    rmSync(projectDir, { recursive: true, force: true });
    rmSync(skillsDir, { recursive: true, force: true });
  }
});

test("chat resumes the SAME general conversation across sessions (console parity)", async () => {
  const projectDir = tempProject();
  const skillsDir = tempSkills();
  try {
    const first = mockModel([
      {
        kind: "toolCall",
        toolCallId: "t1",
        toolName: "addFile",
        input: { path: "specs/requirements/prd.md", content: "# Requirements\n" },
      },
      { kind: "text", text: "done" },
    ]);
    assert.equal((await requirementsCommand(projectDir, { model: first, skillsDir, silent: true, idea: "idea" })).ok, true);
    const afterFirst = JSON.parse(readFileSync(join(projectDir, ".aep-playground/conversations/general.json"), "utf8")) as {
      id: string;
      messages: unknown[];
    };

    // A NEW session (fresh process semantics) — the chat turn must append to
    // the same conversation file, history intact.
    const second = mockModel([{ kind: "text", text: "sure — noted." }]);
    const session = await openSession(projectDir, { model: second, skillsDir });
    try {
      const outcome = await chatTurn(session, chatSpec("add a wishlist requirement"), { silent: true });
      assert.equal(outcome.ok, true, outcome.detail);
    } finally {
      await session.close();
    }
    const afterSecond = JSON.parse(readFileSync(join(projectDir, ".aep-playground/conversations/general.json"), "utf8")) as {
      id: string;
      messages: unknown[];
    };
    assert.equal(afterSecond.id, afterFirst.id, "one general conversation per project");
    assert.ok(afterSecond.messages.length > afterFirst.messages.length, "history grew across sessions");
  } finally {
    rmSync(projectDir, { recursive: true, force: true });
    rmSync(skillsDir, { recursive: true, force: true });
  }
});

test("issues/ never enters a spec-turn snapshot; hand-edits set filesChangedExternally", async () => {
  const projectDir = tempProject();
  const skillsDir = tempSkills();
  try {
    mkdirSync(join(projectDir, "issues"), { recursive: true });
    writeFileSync(join(projectDir, "issues", "1.md"), "---\ntitle: leak?\n---\n");
    mkdirSync(join(projectDir, "specs/requirements"), { recursive: true });
    writeFileSync(join(projectDir, "specs/requirements/prd.md"), "# R\n");

    const model = mockModel([{ kind: "text", text: "ok" }]);
    const session = await openSession(projectDir, { model, skillsDir });
    try {
      const files = session.ws.readSpecFiles();
      assert.ok(!Object.keys(files).some((p) => p.startsWith("issues/")), "issues/ excluded from spec turns");
      assert.ok("specs/requirements/prd.md" in files);
    } finally {
      await session.close();
    }
  } finally {
    rmSync(projectDir, { recursive: true, force: true });
    rmSync(skillsDir, { recursive: true, force: true });
  }
});
