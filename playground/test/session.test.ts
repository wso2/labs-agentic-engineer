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
 * The session's adapters around the in-process design agent: the dev
 * verifier gates `/v1`, the thread is the project's (rotated by `--fresh`,
 * re-adopted when the agent rotated it), and the one-shot phase verbs tell
 * the agent no interview is possible.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { mockModel } from "@aep/ae-design-agent/shared/mock-model";
import { chatTurn, requirementsCommand } from "../src/commands.js";
import { openSession, type PlaygroundSession } from "../src/engine/session.js";
import { runPlanTurn } from "../src/engine/plan-turn.js";

const HEADLESS_NOTE = /No interview is possible in this run/;

function project(t: { after: (fn: () => void) => void }): { projectDir: string; skillsDir: string } {
  const projectDir = mkdtempSync(join(tmpdir(), "aep-play-session-"));
  const skillsDir = mkdtempSync(join(tmpdir(), "aep-play-session-skills-"));
  mkdirSync(join(projectDir, "specs/requirements"), { recursive: true });
  writeFileSync(join(projectDir, "specs/requirements/prd.md"), "# PRD\n");
  t.after(() => {
    rmSync(projectDir, { recursive: true, force: true });
    rmSync(skillsDir, { recursive: true, force: true });
  });
  return { projectDir, skillsDir };
}

/** The turn's own instruction: the last user message of the model's first call. */
function promptText(model: ReturnType<typeof mockModel>): string {
  const prompt = (model.doStreamCalls[0]?.prompt ?? []) as unknown as { role: string; content: unknown }[];
  return JSON.stringify(prompt.filter((m) => m.role === "user").at(-1)?.content ?? "");
}

async function withSession<T>(projectDir: string, opts: Parameters<typeof openSession>[1], fn: (s: PlaygroundSession) => Promise<T>): Promise<T> {
  const session = await openSession(projectDir, opts);
  try {
    return await fn(session);
  } finally {
    await session.close();
  }
}

test("/v1 admits the session's dev credential only, on loopback", async (t) => {
  const { projectDir, skillsDir } = project(t);
  await withSession(projectDir, { model: mockModel([]), skillsDir }, async (s) => {
    assert.match(s.baseUrl, /^http:\/\/127\.0\.0\.1:\d+$/);
    const url = `${s.baseUrl}/v1/projects/${s.project}/conversations/current`;
    assert.equal((await fetch(url)).status, 401);
    assert.equal((await fetch(url, { headers: { Authorization: "Bearer not-the-secret" } })).status, 401);
    const ok = await fetch(url, { headers: s.headers });
    assert.equal(ok.status, 200);
    assert.equal(((await ok.json()) as { conversationId: string }).conversationId, s.state.conversationUuid, "the kept thread is current");
  });
});

test("--fresh rotates the project's thread: a new id is kept and the old history is dropped", async (t) => {
  const { projectDir, skillsDir } = project(t);
  const first = await withSession(projectDir, { model: mockModel([{ kind: "text", text: "hi" }]), skillsDir }, async (s) => {
    assert.equal((await chatTurn(s, "hello", { silent: true })).ok, true);
    return s.state.conversationUuid;
  });
  const oldFile = join(projectDir, ".aep-playground/conversations", `${first}.json`);
  assert.ok(existsSync(oldFile));
  const fresh = await withSession(projectDir, { model: mockModel([]), skillsDir, fresh: true }, async (s) => s.state.conversationUuid);
  assert.notEqual(fresh, first);
  assert.equal(JSON.parse(readFileSync(join(projectDir, ".aep-playground/project.json"), "utf8")).conversationUuid, fresh);
  assert.ok(!existsSync(oldFile), "the rotated thread's history is gone");
});

test("a send on a thread the agent no longer holds adopts the current one and goes through", async (t) => {
  const { projectDir, skillsDir } = project(t);
  await withSession(projectDir, { model: mockModel([{ kind: "text", text: "ok" }]), skillsDir }, async (s) => {
    const current = s.state.conversationUuid;
    s.state.conversationUuid = "00000000-0000-4000-8000-000000000000"; // stale
    const outcome = await chatTurn(s, "hello", { silent: true });
    assert.equal(outcome.ok, true, outcome.detail);
    assert.equal(s.state.conversationUuid, current);
  });
});

test("the one-shot phase verbs are headless; a chat session is not", async (t) => {
  const { projectDir, skillsDir } = project(t);
  const phase = mockModel([{ kind: "text", text: "done" }]);
  assert.equal((await requirementsCommand(projectDir, { model: phase, skillsDir, silent: true, idea: "an idea" })).ok, true);
  assert.match(promptText(phase), HEADLESS_NOTE);
  assert.match(promptText(phase), /an idea/, "/start carried the descriptor's idea through the lookup");

  const chat = mockModel([{ kind: "text", text: "sure" }]);
  await withSession(projectDir, { model: chat, skillsDir }, async (s) => {
    assert.equal((await chatTurn(s, "add a wishlist", { silent: true })).ok, true);
  });
  assert.doesNotMatch(promptText(chat), HEADLESS_NOTE);
});

test("a Plan turn that fails reports it and is not completed", async (t) => {
  const { projectDir, skillsDir } = project(t);
  const failing = mockModel([]); // a model with nothing to say throws: the turn fails
  t.mock.method(console, "error", () => {}); // the AI SDK logs the provider error it then throws
  await withSession(projectDir, { model: failing, skillsDir }, async (s) => {
    const result = await runPlanTurn(s, []);
    assert.equal(result.completed, false);
    assert.ok(result.error, "the failure is reported");
    assert.ok(result.parts.some((p) => (p as { type: string }).type === "turn-failed"), "the /v1 stream was read beside the socket");
  });
});
