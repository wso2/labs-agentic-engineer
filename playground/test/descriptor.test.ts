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
 * The project descriptor (`specs/.agentic-engineer.toml`) and the `/start`
 * kickoff it feeds: the playground's half of what aep-api does server-side.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { DESCRIPTOR_PATH, descriptorFile, readIdea, writeDescriptor } from "../src/state/descriptor.js";
import { classifyChatInput } from "../src/tui/chat-commands.js";
import { readProjectFiles } from "../src/kit/project-fs.js";
import { renderPart } from "../src/kit/render.js";

function tempProject(): string {
  return mkdtempSync(join(tmpdir(), "aep-desc-test-"));
}

// The idea is free text a user typed — quotes, apostrophes, backslashes and
// newlines must survive the write→read cycle. This is why a real TOML encoder
// is used rather than hand-rolled key writing.
test("descriptor round-trips awkward idea text", () => {
  const dir = tempProject();
  try {
    const idea = 'A "claims" tracker for Ops.\nDon\'t lose receipts — path C:\\temp matters.\n\n[not-a-section]\nidea = "not a key"';
    writeDescriptor(dir, "expense-tracker", idea);
    assert.equal(readIdea(dir), idea);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("readIdea returns null when there is no descriptor", () => {
  const dir = tempProject();
  try {
    assert.equal(readIdea(dir), null);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

// A hand-mangled descriptor must not break a run: losing the idea costs one
// question from the start skill, which is cheaper than a crash.
test("readIdea degrades to null on malformed TOML", () => {
  const dir = tempProject();
  try {
    const file = descriptorFile(dir);
    mkdirSync(dirname(file), { recursive: true });
    writeFileSync(file, "this is not = = toml [[[", "utf8");
    assert.equal(readIdea(dir), null);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

// THE load-bearing property: the descriptor is invisible to the agent. The
// snapshot walk strips dot-led segments at every depth, so the model can never
// read the idea for itself — it only ever arrives through the /start expansion.
test("the descriptor never enters a turn snapshot", () => {
  const dir = tempProject();
  try {
    writeDescriptor(dir, "expense-tracker", "an expense claim tracker");
    mkdirSync(join(dir, "specs/requirements"), { recursive: true });
    writeFileSync(join(dir, "specs/requirements/prd.md"), "# Reqs\n", "utf8");

    const files = readProjectFiles(dir);
    assert.ok(files["specs/requirements/prd.md"], "ordinary spec files still ride");
    assert.equal(files[DESCRIPTOR_PATH], undefined, "the descriptor must never reach the agent");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

// --- /start goes to the design agent verbatim ---------------------------------

test("/start is sent verbatim, with or without an inline idea (the design agent parses it)", () => {
  assert.deepEqual(classifyChatInput("/start"), { kind: "turn", instruction: "/start" });
  assert.deepEqual(classifyChatInput("/start a rota planner  "), { kind: "turn", instruction: "/start a rota planner" });
});

// A `/start` interview ends its turn on a question card, whose tool result is a
// placeholder carrying `status` but no `ok`. Rendering that as an error was
// telling every new user their first command failed.
test("a pending question card renders as awaiting, not as an error", () => {
  const written: string[] = [];
  const original = process.stdout.write.bind(process.stdout);
  (process.stdout as { write: unknown }).write = (chunk: string) => {
    written.push(String(chunk));
    return true;
  };
  try {
    renderPart({
      type: "tool-result",
      toolCallId: "t1",
      toolName: "ask_questions",
      output: { status: "awaiting_user_response", questions: [] },
    } as unknown as Parameters<typeof renderPart>[0]);
  } finally {
    (process.stdout as { write: unknown }).write = original;
  }
  const out = written.join("");
  assert.match(out, /awaiting your answer/);
  assert.doesNotMatch(out, /error/);
});
