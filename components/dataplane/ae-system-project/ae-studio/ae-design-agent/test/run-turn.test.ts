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
import type { FilePart, ModelMessage } from "ai";
import { runTurn } from "../src/agents/main/run-turn.js";
import { buildFileToolSet } from "../src/agents/main/tools/files.js";
import { FileBundle, type StreamPart } from "@aep/agent-stream";
import { SEED_FILES } from "./seed-files.js";
import { mockModel } from "../src/shared/mock-model.js";

const OPENAPI = "specs/design/components/hello-api/openapi.yaml";

test("runTurn streams events, runs server-side execute, appends messages, returns usage", async () => {
  const bundle = new FileBundle(SEED_FILES);
  const tools = buildFileToolSet(bundle).tools;
  const model = mockModel([
    {
      kind: "toolCall",
      toolCallId: "call_1",
      toolName: "editFile",
      input: { path: OPENAPI, oldString: 'example: "Hello, World!"', newString: 'example: "Hi there!"' },
      text: "Updating the example.",
    },
    { kind: "text", text: "Done." },
  ]);

  const events: StreamPart[] = [];
  const messages: ModelMessage[] = [];
  const res = await runTurn({
    model,
    instructions: "test",
    tools,
    messages,
    prompt: "change it",
    onEvent: (p) => events.push(p),
  });

  // Tools run SERVER-SIDE inside the one stream → the canonical bundle is mutated.
  assert.ok(bundle.read(OPENAPI)!.includes('"Hi there!"'));

  // Events are forwarded raw: a tool-call and its tool-result both appear.
  assert.ok(events.some((e) => e.type === "tool-call"), "expected a tool-call event");
  assert.ok(events.some((e) => e.type === "tool-result"), "expected a tool-result event");
  assert.ok(events.some((e) => e.type === "text-delta"), "expected text-delta events");

  // messages mutated in place: user turn first, then assistant + tool parts.
  assert.equal(messages[0]?.role, "user");
  assert.ok(messages.some((m) => m.role === "assistant"), "expected an assistant message");
  assert.ok(messages.some((m) => m.role === "tool"), "expected a tool result message");

  // usage + finishReason surfaced (the whole-turn sum).
  assert.equal(res.finishReason, "stop");
  assert.ok((res.usage.outputTokens ?? 0) > 0, "expected output tokens in usage");
});

test("runTurn appends only (history grows across turns)", async () => {
  const bundle = new FileBundle(SEED_FILES);
  const messages: ModelMessage[] = [];

  await runTurn({
    model: mockModel([{ kind: "text", text: "first" }]),
    instructions: "t",
    tools: buildFileToolSet(bundle).tools,
    messages,
    prompt: "turn one",
  });
  const afterFirst = messages.length;
  assert.ok(afterFirst >= 2, "user + assistant after turn one");

  await runTurn({
    model: mockModel([{ kind: "text", text: "second" }]),
    instructions: "t",
    tools: buildFileToolSet(bundle).tools,
    messages,
    prompt: "turn two",
  });

  // Append-only: turn two never rewrites turn one's messages.
  assert.ok(messages.length > afterFirst);
  assert.equal(messages[0]?.role, "user");
  assert.equal((messages[afterFirst] as ModelMessage | undefined)?.role, "user");
});

// --- Reference PDF attachments (#384) ----------------------------------------

test("runTurn attaches file parts alongside the text prompt when fileParts is given", async () => {
  const bundle = new FileBundle(SEED_FILES);
  const messages: ModelMessage[] = [];
  const filePart: FilePart = { type: "file", data: "JVBERi0xLjQ=", mediaType: "application/pdf", filename: "brief.pdf" };

  await runTurn({
    model: mockModel([{ kind: "text", text: "ok" }]),
    instructions: "t",
    tools: buildFileToolSet(bundle).tools,
    messages,
    prompt: "read the brief",
    fileParts: [filePart],
  });

  const first = messages[0];
  assert.equal(first?.role, "user");
  assert.ok(Array.isArray(first?.content), "content becomes an array when a file part rides along");
  const content = first!.content as unknown as Array<Record<string, unknown>>;
  assert.deepEqual(content[0], { type: "text", text: "read the brief" });
  assert.deepEqual(content[1], filePart);
});

test("runTurn with no fileParts keeps the plain string content (byte-identical to before this feature)", async () => {
  const bundle = new FileBundle(SEED_FILES);
  const messages: ModelMessage[] = [];

  await runTurn({
    model: mockModel([{ kind: "text", text: "ok" }]),
    instructions: "t",
    tools: buildFileToolSet(bundle).tools,
    messages,
    prompt: "change it",
  });

  assert.equal(messages[0]?.content, "change it");
});

test("runTurn with an EMPTY fileParts array also keeps the plain string content", async () => {
  const bundle = new FileBundle(SEED_FILES);
  const messages: ModelMessage[] = [];

  await runTurn({
    model: mockModel([{ kind: "text", text: "ok" }]),
    instructions: "t",
    tools: buildFileToolSet(bundle).tools,
    messages,
    prompt: "change it",
    fileParts: [],
  });

  assert.equal(messages[0]?.content, "change it");
});
