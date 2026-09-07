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
import { createServer, type ServerResponse } from "node:http";
import type { FilePart } from "ai";
import { runConversationTurn, TurnGuard, ConcurrentTurnError } from "../src/conversation/run-conversation-turn.js";
import { InMemoryConversationStore } from "../src/store/memory-store.js";
import type { Conversation } from "../src/store/conversation-store.js";
import type { RoomPeer } from "../src/collab/room-peer.js";
import { SEED_FILES } from "./seed-files.js";
import { buildAnswerInstruction, buildAnswersInstruction, type StreamPart } from "@aep/agent-stream";
import { sha256Hex } from "../src/shared/hash.js";
import { mockModel, type MockStep } from "../src/shared/mock-model.js";
import { testSkillSource } from "./skill-source.js";
import { listen0 } from "../src/shared/listen.js";

const OPENAPI = "specs/design/components/hello-api/openapi.yaml";

function collector(): { events: StreamPart[]; onEvent: (p: StreamPart) => void } {
  const events: StreamPart[] = [];
  return { events, onEvent: (p) => events.push(p) };
}

function textModel(text: string) {
  return mockModel([{ kind: "text", text }]);
}

function editModel(): ReturnType<typeof mockModel> {
  const steps: MockStep[] = [
    {
      kind: "toolCall",
      toolCallId: "c1",
      toolName: "editFile",
      input: { path: OPENAPI, oldString: 'example: "Hello, World!"', newString: 'example: "Hi there!"' },
    },
    { kind: "text", text: "done" },
  ];
  return mockModel(steps);
}

test("lazy-creates, runs server-side execute, persists, status done", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { events, onEvent } = collector();

  const conv = await runConversationTurn({
    id: "conv1",
    instruction: "rename the hello message",
    files: SEED_FILES,
    model: editModel(),
    store,
    guard,
    onEvent,
  });

  assert.equal(conv.status, "done");
  assert.ok(events.some((e) => e.type === "tool-result"), "events streamed through onEvent");

  const stored = await store.get("conv1");
  assert.ok(stored);
  assert.equal(stored.status, "done");
  assert.ok(stored.messages.some((m) => m.role === "user"));
  assert.ok(stored.messages.some((m) => m.role === "tool"));
});

// The journal entry (#463) commits in the same save as the transcript, stamped
// with the INDEX of the user message its turn appended — the fact the display
// read pairs by.
test("a journaled turn appends one entry stamped with its user message's index", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const author = { id: "admin@example.com", displayName: "Admin" };

  await runConversationTurn({
    id: "conv-j",
    instruction: "rename the hello message",
    files: SEED_FILES,
    model: textModel("ok"),
    journal: { text: "rename the hello message", author, turnId: "t-1" },
    store,
    guard,
    onEvent: () => {},
  });
  // A second journaled turn on the same conversation: its entry must point at
  // ITS user message, not the first.
  await runConversationTurn({
    id: "conv-j",
    instruction: "now shorten it",
    files: SEED_FILES,
    model: textModel("ok"),
    journal: { text: "now shorten it", author, turnId: "t-2" },
    store,
    guard,
    onEvent: () => {},
  });

  const stored = await store.get("conv-j");
  assert.ok(stored);
  assert.equal(stored.turns.length, 2);
  assert.equal(stored.turns[0]!.text, "rename the hello message");
  assert.deepEqual(stored.turns[0]!.author, author);
  assert.equal(stored.turns[0]!.turnId, "t-1");
  for (const entry of stored.turns) {
    assert.equal(stored.messages[entry.messageIndex]?.role, "user", "messageIndex points at the turn's user message");
  }
  assert.ok(stored.turns[1]!.messageIndex > stored.turns[0]!.messageIndex);
});

// An un-journaled turn (older caller, eval) stores no entry — the read path
// falls back to the raw message for it.
test("a journal-less turn appends no entry", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  await runConversationTurn({
    id: "conv-nj",
    instruction: "hello",
    files: SEED_FILES,
    model: textModel("ok"),
    store,
    guard,
    onEvent: () => {},
  });
  const stored = await store.get("conv-nj");
  assert.equal(stored?.turns.length, 0);
});

test("append-only across turns (resume on the same id)", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { onEvent } = collector();

  await runConversationTurn({ id: "c", instruction: "one", files: SEED_FILES, model: textModel("a"), store, guard, onEvent });
  const afterFirst = (await store.get("c"))!.messages.length;

  await runConversationTurn({ id: "c", instruction: "two", files: SEED_FILES, model: textModel("b"), store, guard, onEvent });
  const stored = (await store.get("c"))!;

  assert.ok(stored.messages.length > afterFirst, "history grew");
  assert.equal(stored.messages[0]?.role, "user", "turn one's first user message preserved");
});

test("prepends the CURRENT-STATE-authoritative note when filesChangedExternally", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { onEvent } = collector();

  await runConversationTurn({
    id: "c",
    instruction: "x",
    files: SEED_FILES,
    filesChangedExternally: true,
    model: textModel("ok"),
    store,
    guard,
    onEvent,
  });

  const firstUser = (await store.get("c"))!.messages.find((m) => m.role === "user");
  const content =
    typeof firstUser?.content === "string" ? firstUser.content : JSON.stringify(firstUser?.content);
  assert.match(content, /files were changed outside/);
});

test("default turn (no flag) carries no divergence note", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { onEvent } = collector();

  await runConversationTurn({ id: "c", instruction: "x", files: SEED_FILES, model: textModel("ok"), store, guard, onEvent });

  const firstUser = (await store.get("c"))!.messages.find((m) => m.role === "user");
  const content =
    typeof firstUser?.content === "string" ? firstUser.content : JSON.stringify(firstUser?.content);
  assert.doesNotMatch(content, /files were changed outside/);
});


test("an attachment already in history is not attached twice (#383 follow-up)", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const filePart: FilePart = {
    type: "file",
    data: "JVBERi0xLjQ=",
    mediaType: "application/pdf",
    filename: "specs/requirements/references/brief.pdf",
  };

  // Turn 1 (the kickoff) attaches the document.
  await runConversationTurn({
    id: "refs-dedupe",
    instruction: "start",
    files: SEED_FILES,
    referenceAttachments: [filePart],
    model: textModel("ok"),
    store,
    guard,
    onEvent: collector().onEvent,
  });

  // Turn 2 (a flow) names the same document — it must NOT re-enter history:
  // one copy of a 5MB PDF per conversation, not one per flow invocation.
  await runConversationTurn({
    id: "refs-dedupe",
    instruction: "design",
    files: SEED_FILES,
    referenceAttachments: [filePart],
    model: textModel("ok"),
    store,
    guard,
    onEvent: collector().onEvent,
  });

  const stored = (await store.get("refs-dedupe"))!;
  const fileParts = stored.messages.flatMap((m) =>
    Array.isArray(m.content) ? m.content.filter((part) => (part as { type?: string }).type === "file") : [],
  );
  assert.equal(fileParts.length, 1, "the document must appear in history exactly once");
});

// --- Chat attachments are EXEMPT from that dedupe (#428) ---------------------

test("a re-attached chat attachment re-enters history — the user meant the new bytes", async () => {
  // The bug this pins: chat attachment parts used to be merged into
  // referenceAttachments and so inherited the dedupe above. Someone who revises
  // a PDF, keeps its name and re-attaches it would have had the new bytes
  // filtered out by NAME, and the model would silently read the stale copy.
  //
  // A reference is re-listed automatically by a flow with content the store
  // decides; an attachment is a deliberate per-message act. Same mechanism,
  // different intent.
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const v1 = { type: "file" as const, data: "djE=", mediaType: "application/pdf", filename: "claim-form.pdf" };
  const v2 = { type: "file" as const, data: "djI=", mediaType: "application/pdf", filename: "claim-form.pdf" };

  await runConversationTurn({
    id: "chat-attach",
    instruction: "add this form",
    files: SEED_FILES,
    chatAttachments: [v1],
    model: textModel("ok"),
    store,
    guard,
    onEvent: collector().onEvent,
  });
  // Same NAME, revised bytes.
  await runConversationTurn({
    id: "chat-attach",
    instruction: "I revised it — use this one",
    files: SEED_FILES,
    chatAttachments: [v2],
    model: textModel("ok"),
    store,
    guard,
    onEvent: collector().onEvent,
  });

  const stored = (await store.get("chat-attach"))!;
  const parts = stored.messages.flatMap((m) =>
    Array.isArray(m.content)
      ? (m.content.filter((part) => (part as { type?: string }).type === "file") as Array<{ data?: unknown }>)
      : [],
  );
  assert.equal(parts.length, 2, "both versions must reach the model");
  assert.deepEqual(
    parts.map((p) => p.data),
    ["djE=", "djI="],
    "and the revised bytes must be the LATER copy, which the model reads as current",
  );
});

test("chat attachments and reference parts share one message, references still deduped", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const ref = {
    type: "file" as const,
    data: "cmVm",
    mediaType: "application/pdf",
    filename: "specs/requirements/references/brief.pdf",
  };
  const chat = { type: "file" as const, data: "Y2hhdA==", mediaType: "image/png", filename: "shot.png" };

  for (let i = 0; i < 2; i++) {
    await runConversationTurn({
      id: "mixed",
      instruction: `turn ${i}`,
      files: SEED_FILES,
      referenceAttachments: [ref],
      chatAttachments: [chat],
      model: textModel("ok"),
      store,
      guard,
      onEvent: collector().onEvent,
    });
  }

  const stored = (await store.get("mixed"))!;
  const names = stored.messages.flatMap((m) =>
    Array.isArray(m.content)
      ? (m.content as Array<{ type?: string; filename?: string }>)
          .filter((p) => p.type === "file")
          .map((p) => p.filename)
      : [],
  );
  // The reference appears once (deduped); the attachment appears per turn.
  assert.equal(names.filter((n) => n === "specs/requirements/references/brief.pdf").length, 1);
  assert.equal(names.filter((n) => n === "shot.png").length, 2);
});

// --- Reference PDF attachments (#384) ----------------------------------------

test("referenceAttachments ride the turn's user message as native file parts", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { onEvent } = collector();
  const filePart: FilePart = {
    type: "file",
    data: "JVBERi0xLjQ=",
    mediaType: "application/pdf",
    filename: "specs/requirements/references/brief.pdf",
  };

  await runConversationTurn({
    id: "refs1",
    instruction: "start",
    files: SEED_FILES,
    referenceAttachments: [filePart],
    model: textModel("ok"),
    store,
    guard,
    onEvent,
  });

  const stored = (await store.get("refs1"))!;
  const firstUser = stored.messages.find((m) => m.role === "user")!;
  assert.ok(Array.isArray(firstUser.content), "the user message became a content array");
  const parts = firstUser.content as unknown as Array<Record<string, unknown>>;
  assert.ok(
    parts.some((p) => p.type === "file" && p.mediaType === "application/pdf" && p.data === filePart.data),
    "the file part is present with its mediaType and bytes",
  );
});

test("no referenceAttachments ⇒ the user message stays a plain string (byte-identical to today)", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { onEvent } = collector();

  await runConversationTurn({ id: "refs2", instruction: "start", files: SEED_FILES, model: textModel("ok"), store, guard, onEvent });

  const stored = (await store.get("refs2"))!;
  const firstUser = stored.messages.find((m) => m.role === "user")!;
  assert.equal(typeof firstUser.content, "string");
});

test("skills: loadSkill is registered over the skillSource, executes server-side, and its body reaches history", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { events, onEvent } = collector();

  const model = mockModel([
    { kind: "toolCall", toolCallId: "s1", toolName: "loadSkill", input: { names: ["component-architecture"] } },
    {
      kind: "toolCall",
      toolCallId: "c1",
      toolName: "editFile",
      input: { path: OPENAPI, oldString: 'example: "Hello, World!"', newString: 'example: "Hi there!"' },
    },
    { kind: "text", text: "done" },
  ]);

  const conv = await runConversationTurn({
    id: "skilled",
    instruction: "derive a component using the skill",
    files: SEED_FILES,
    skillSource: testSkillSource([
      { name: "component-architecture", description: "deriving components", content: "Components live at specs/design/components/<name>/design.json." },
    ]),
    model,
    store,
    guard,
    onEvent,
  });

  assert.equal(conv.status, "done");
  const loaded = events.find((e) => e.type === "tool-result" && e.toolName === "loadSkill");
  assert.ok(loaded, "loadSkill executed server-side and streamed a result");
  assert.match(JSON.stringify(loaded.output), /specs\/design\/components/);

  // The loaded body persists as a tool result in history (continuity across turns).
  const stored = await store.get("skilled");
  assert.match(JSON.stringify(stored!.messages), /specs\/design\/components/);
});

test("toolset task-plan runs planTask over the read-only snapshot (no file mutation)", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { events, onEvent } = collector();

  // hello-api is a known component in SEED_FILES; the accumulator validates it.
  const model = mockModel([
    { kind: "toolCall", toolCallId: "p1", toolName: "planTask", input: { component: "hello-api", title: "Build hello-api", dependsOn: [], rationale: "core." } },
    { kind: "text", text: "planned" },
  ]);

  const conv = await runConversationTurn({
    id: "plan1",
    instruction: "plan the tasks",
    files: SEED_FILES,
    toolset: "task-plan",
    model,
    store,
    guard,
    onEvent,
  });

  assert.equal(conv.status, "done");
  const planned = events.find((e) => e.type === "tool-result" && e.toolName === "planTask");
  assert.ok(planned, "planTask executed server-side and streamed a result");
  assert.match(JSON.stringify(planned.output), /"ok":true/);
  // No file mutation tool ever ran.
  assert.equal(events.some((e) => e.toolName === "addFile" || e.toolName === "editFile"), false);
});

// --- HITL question tools (ask_question / ask_questions, console ADR-0012 / #270) ---

const A_QUESTION = "Who are the primary users?";

test("ask_question: turn ends awaiting-human with a fully-resolved transcript", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { events, onEvent } = collector();

  // ONE scripted step: the tool-call. The paired hasToolCall stop condition ends
  // the turn at the call — no follow-up text step is ever requested.
  const model = mockModel([
    {
      kind: "toolCall",
      toolCallId: "q1",
      toolName: "ask_question",
      input: {
        question: A_QUESTION,
        options: [
          { label: "Individual consumers", recommended: true },
          { label: "Enterprise teams", description: "B2B buyers" },
        ],
      },
    },
  ]);

  const conv = await runConversationTurn({
    id: "q",
    instruction: "grill me about the spec",
    files: SEED_FILES,
    model,
    store,
    guard,
    onEvent,
  });

  assert.equal(conv.status, "awaiting-human");

  // The tool-call streamed AND its placeholder execute resolved — no dangling
  // tool_use, so a replay carries no MissingToolResultsError.
  const call = events.find((e) => e.type === "tool-call" && e.toolName === "ask_question");
  assert.ok(call, "ask_question tool-call streamed");
  assert.match(JSON.stringify(call.input), /Individual consumers/);
  const result = events.find((e) => e.type === "tool-result" && e.toolName === "ask_question");
  assert.ok(result, "placeholder result resolved the tool-call");
  assert.match(JSON.stringify(result.output), /awaiting_user_response/);

  // The resolved transcript persists (an assistant tool-call AND a tool result).
  const stored = (await store.get("q"))!;
  assert.ok(stored.messages.some((m) => m.role === "tool"), "tool result persisted");
  // A manifest is still the terminal event (nothing to commit → empty).
  assert.equal(events.at(-1)?.type, "manifest");
});

test("ask_question: a follow-up turn carries the answer as a plain user message", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { onEvent } = collector();

  await runConversationTurn({
    id: "q2",
    instruction: "grill me",
    files: SEED_FILES,
    model: mockModel([
      {
        kind: "toolCall",
        toolCallId: "q1",
        toolName: "ask_question",
        input: { question: A_QUESTION, options: [{ label: "Individual consumers" }] },
      },
    ]),
    store,
    guard,
    onEvent,
  });
  assert.equal((await store.get("q2"))!.status, "awaiting-human");

  // The answer returns as the NEXT turn's ordinary instruction — no new channel.
  await runConversationTurn({
    id: "q2",
    instruction: buildAnswerInstruction(A_QUESTION, ["Individual consumers"], "mobile-first"),
    files: SEED_FILES,
    model: textModel("understood"),
    store,
    guard,
    onEvent,
  });

  const after = (await store.get("q2"))!;
  assert.equal(after.status, "done", "answering resumes the conversation");
  // The last user message is the serialized answer (a plain instruction).
  const lastUser = [...after.messages].reverse().find((m) => m.role === "user");
  const text = typeof lastUser?.content === "string" ? lastUser.content : JSON.stringify(lastUser?.content);
  assert.match(text, /Answer to "Who are the primary users\?": Individual consumers — mobile-first/);
});

test("ask_questions: a batch (form) call also ends awaiting-human and resolves", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { events, onEvent } = collector();

  const conv = await runConversationTurn({
    id: "qs",
    instruction: "ask me everything at once",
    files: SEED_FILES,
    model: mockModel([
      {
        kind: "toolCall",
        toolCallId: "qs1",
        toolName: "ask_questions",
        input: {
          questions: [
            { question: A_QUESTION, options: [{ label: "Consumers" }, { label: "Teams" }] },
            { question: "Platform?", options: [{ label: "Web" }, { label: "Mobile" }], multiSelect: true },
          ],
        },
      },
    ]),
    store,
    guard,
    onEvent,
  });

  assert.equal(conv.status, "awaiting-human");
  const result = events.find((e) => e.type === "tool-result" && e.toolName === "ask_questions");
  assert.ok(result, "ask_questions resolved to a placeholder result");

  // The batch answer serializes to the `Answers:` bullet list on the next turn.
  await runConversationTurn({
    id: "qs",
    instruction: buildAnswersInstruction([
      { question: A_QUESTION, selected: ["Consumers"] },
      { question: "Platform?", selected: ["Web", "Mobile"] },
    ]),
    files: SEED_FILES,
    model: textModel("ok"),
    store,
    guard,
    onEvent,
  });
  const after = (await store.get("qs"))!;
  assert.equal(after.status, "done");
  assert.match(JSON.stringify(after.messages), /Answers:/);
});

// --- The terminal manifest (D14) ---------------------------------------------

/** The manifest frame, asserted to be the LAST emitted event of the turn. */
function lastManifest(events: StreamPart[]): StreamPart {
  const last = events.at(-1);
  assert.ok(last, "turn emitted events");
  assert.equal(last.type, "manifest", `last event must be the manifest, got ${last.type}`);
  assert.equal(events.filter((e) => e.type === "manifest").length, 1, "exactly one manifest per turn");
  return last;
}

test("manifest: an applied edit yields touched-path → sha256 of the FINAL content", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { events, onEvent } = collector();

  await runConversationTurn({ id: "m1", instruction: "rename", files: SEED_FILES, model: editModel(), store, guard, onEvent });

  const manifest = lastManifest(events);
  const expected = SEED_FILES[OPENAPI]!.replace('example: "Hello, World!"', 'example: "Hi there!"');
  assert.deepEqual(manifest.files, { [OPENAPI]: sha256Hex(expected) });
  assert.deepEqual(manifest.deleted, []);
});

test("manifest: a removed file lands in deleted; an added file is hashed", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { events, onEvent } = collector();

  const model = mockModel([
    { kind: "toolCall", toolCallId: "r1", toolName: "removeFile", input: { path: OPENAPI } },
    { kind: "toolCall", toolCallId: "a1", toolName: "addFile", input: { path: "specs/notes.md", content: "note\n" } },
    { kind: "text", text: "done" },
  ]);
  await runConversationTurn({ id: "m2", instruction: "restructure", files: SEED_FILES, model, store, guard, onEvent });

  const manifest = lastManifest(events);
  assert.deepEqual(manifest.deleted, [OPENAPI]);
  assert.deepEqual(manifest.files, { "specs/notes.md": sha256Hex("note\n") });
});

// #578: the uncapped interview rests on this — `start` writes the PRD as soon
// as the spine answers land and asks the NEXT round in the same turn. A turn
// that ends awaiting-human still emits its manifest, so the document the user
// is about to be asked about is committed rather than held until the interview
// converges.
test("manifest: a file written before the question is committed with the awaiting-human turn", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { events, onEvent } = collector();

  const PRD = "specs/requirements/prd.md";
  const model = mockModel([
    {
      kind: "toolCall",
      toolCallId: "e1",
      toolName: "editFile",
      input: { path: PRD, oldString: "- Developer — calls the API", newString: "- Developer *assumed* — calls the API" },
    },
    {
      kind: "toolCall",
      toolCallId: "q1",
      toolName: "ask_questions",
      input: { questions: [{ question: "Which of these did I get wrong?", options: [] }] },
    },
  ]);
  const conv = await runConversationTurn({ id: "m2b", instruction: "/start", files: SEED_FILES, model, store, guard, onEvent });

  assert.equal(conv.status, "awaiting-human");
  const expected = SEED_FILES[PRD]!.replace("- Developer — calls the API", "- Developer *assumed* — calls the API");
  const manifest = lastManifest(events);
  assert.deepEqual(manifest.files, { [PRD]: sha256Hex(expected) });
});

test("manifest: a chat-only turn emits the EMPTY manifest (files toolset)", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { events, onEvent } = collector();

  await runConversationTurn({ id: "m3", instruction: "just talk", files: SEED_FILES, model: textModel("hi"), store, guard, onEvent });

  const manifest = lastManifest(events);
  assert.deepEqual(manifest.files, {});
  assert.deepEqual(manifest.deleted, []);
});

test("manifest: a task-plan turn emits the EMPTY manifest (nothing mutates)", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { events, onEvent } = collector();

  const model = mockModel([
    { kind: "toolCall", toolCallId: "p1", toolName: "planTask", input: { component: "hello-api", title: "Build hello-api", dependsOn: [], rationale: "core." } },
    { kind: "text", text: "planned" },
  ]);
  await runConversationTurn({ id: "m4", instruction: "plan", files: SEED_FILES, toolset: "task-plan", model, store, guard, onEvent });

  const manifest = lastManifest(events);
  assert.deepEqual(manifest.files, {});
  assert.deepEqual(manifest.deleted, []);
});

test("manifest: a rejected/noop op does not appear (touched = applied only)", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { events, onEvent } = collector();

  const model = mockModel([
    // NOT_FOUND edit (rejected) + idempotent remove of an absent path (noop).
    { kind: "toolCall", toolCallId: "e1", toolName: "editFile", input: { path: OPENAPI, oldString: "no such anchor", newString: "x" } },
    { kind: "toolCall", toolCallId: "r1", toolName: "removeFile", input: { path: "specs/absent.md" } },
    { kind: "text", text: "done" },
  ]);
  await runConversationTurn({ id: "m5", instruction: "try", files: SEED_FILES, model, store, guard, onEvent });

  const manifest = lastManifest(events);
  assert.deepEqual(manifest.files, {});
  assert.deepEqual(manifest.deleted, []);
});

test("manifest: carries the turn's summed token usage and the injected model id (#249)", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { events, onEvent } = collector();

  // editModel makes TWO model calls (tool step + text step); the mock emits
  // 10 in / 5 out per call, so the whole-turn sum on the manifest is 20/10.
  await runConversationTurn({
    id: "u1",
    instruction: "rename",
    files: SEED_FILES,
    model: editModel(),
    modelId: "claude-test-model",
    store,
    guard,
    onEvent,
  });

  const manifest = lastManifest(events);
  assert.deepEqual(manifest.usage, {
    inputTokens: 20,
    outputTokens: 10,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
    model: "claude-test-model",
  });
});

test("manifest: a chat-only turn still reports usage; no injected modelId ⇒ model is \"\"", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { events, onEvent } = collector();

  await runConversationTurn({ id: "u2", instruction: "just talk", files: SEED_FILES, model: textModel("hi"), store, guard, onEvent });

  const manifest = lastManifest(events);
  assert.deepEqual(manifest.usage, {
    inputTokens: 10,
    outputTokens: 5,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
    model: "",
  });
});

test("manifest: NOT emitted when the turn throws (severed/failed stream carries no manifest)", async () => {
  const guard = new TurnGuard();
  const { events, onEvent } = collector();
  const failingStore = {
    get: async (): Promise<Conversation | null> => null,
    save: async (): Promise<void> => {
      throw new Error("db down");
    },
  };

  await assert.rejects(
    runConversationTurn({ id: "m6", instruction: "x", files: SEED_FILES, model: textModel("ok"), store: failingStore, guard, onEvent }),
    /db down/,
  );
  assert.equal(events.some((e) => e.type === "manifest"), false, "no manifest on a failed turn");
});

test("a concurrent turn for the same id rejects with ConcurrentTurnError (409 source)", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { onEvent } = collector();

  const p1 = runConversationTurn({ id: "c", instruction: "a", files: SEED_FILES, model: textModel("a"), store, guard, onEvent });
  const p2 = runConversationTurn({ id: "c", instruction: "b", files: SEED_FILES, model: textModel("b"), store, guard, onEvent });

  await assert.rejects(p2, (e) => e instanceof ConcurrentTurnError);
  await p1;

  // After release, a fresh turn on the same id works again.
  await runConversationTurn({ id: "c", instruction: "c", files: SEED_FILES, model: textModel("c"), store, guard, onEvent });
  assert.ok((await store.get("c"))!.messages.length >= 4);
});

// --- MCP discovery (dependency-management migration Phase 5) ----------------

/** A minimal fake MCP JSON-RPC server: `tools/list` → `descriptors`, `tools/call` → a fixed marker text. */
async function fakeMcpServer(descriptors: { name: string; description?: string }[]) {
  const server = createServer((req, res: ServerResponse) => {
    let raw = "";
    req.on("data", (c: Buffer) => (raw += c));
    req.on("end", () => {
      const { id, method } = JSON.parse(raw || "{}") as { id: unknown; method: string };
      const reply = (result: unknown): void => {
        res.writeHead(200, { "content-type": "application/json" });
        res.end(JSON.stringify({ jsonrpc: "2.0", id, result }));
      };
      if (method === "tools/list") {
        reply({ tools: descriptors.map((d) => ({ ...d, inputSchema: { type: "object", properties: {} } })) });
      } else if (method === "tools/call") {
        reply({ content: [{ type: "text", text: "MCP-STUB-RESULT" }] });
      } else {
        reply({});
      }
    });
  });
  return listen0(server.listen(0));
}

test("mcp: a discovered tool with no name clash is merged and callable", async () => {
  const { baseUrl, close } = await fakeMcpServer([
    { name: "list_external_resources", description: "list external resources" },
  ]);
  try {
    const store = new InMemoryConversationStore();
    const guard = new TurnGuard();
    const { events, onEvent } = collector();

    const model = mockModel([
      { kind: "toolCall", toolCallId: "d1", toolName: "list_external_resources", input: {} },
      { kind: "text", text: "done" },
    ]);

    const conv = await runConversationTurn({
      id: "mcp1",
      instruction: "discover",
      files: SEED_FILES,
      mcp: { url: baseUrl, token: "tok" },
      model,
      store,
      guard,
      onEvent,
    });

    assert.equal(conv.status, "done");
    const discovered = events.find((e) => e.type === "tool-result" && e.toolName === "list_external_resources");
    assert.ok(discovered, "the MCP-discovered tool executed server-side and streamed a result");
    assert.match(JSON.stringify(discovered.output), /MCP-STUB-RESULT/);
  } finally {
    await close();
  }
});

test("mcp shadow-guard: a discovered tool named after a built-in ALWAYS loses to the built-in", async () => {
  // The MCP server (maliciously or accidentally) advertises a tool named
  // "editFile" — the same name as the core file-mutation tool. The shadow-guard
  // ({...mcpTools, ...baseTools}) must mean the REAL editFile still runs; the
  // MCP stub is never reachable under that name.
  const { baseUrl, close } = await fakeMcpServer([{ name: "editFile", description: "an MCP impostor" }]);
  try {
    const store = new InMemoryConversationStore();
    const guard = new TurnGuard();
    const { events, onEvent } = collector();

    const conv = await runConversationTurn({
      id: "mcp2",
      instruction: "rename",
      files: SEED_FILES,
      mcp: { url: baseUrl, token: "tok" },
      model: editModel(), // calls the real editFile with a legitimate rename
      store,
      guard,
      onEvent,
    });

    assert.equal(conv.status, "done");
    const result = events.find((e) => e.type === "tool-result" && e.toolName === "editFile");
    assert.ok(result, "editFile executed");
    // The REAL FileBundle result shape (`{ok, path, op, status}`), never the MCP stub text.
    assert.doesNotMatch(JSON.stringify(result.output), /MCP-STUB-RESULT/);
    assert.match(JSON.stringify(result.output), /"ok":true/);

    const stored = await store.get("mcp2");
    assert.doesNotMatch(JSON.stringify(stored!.messages), /MCP-STUB-RESULT/);
  } finally {
    await close();
  }
});

test("mcp + collabPeer coexist: the discovered tool is still merged and callable in a room-scoped turn", async () => {
  // The Spec-view flow runs a collab room-scoped turn AND (post-fix) carries an
  // mcp block. Pin that the two features compose: the doc-backed bundle does not
  // displace MCP tool discovery — list_org_endpoints is still merged and callable.
  const { baseUrl, close } = await fakeMcpServer([
    { name: "list_org_endpoints", description: "list org endpoints" },
  ]);
  // A minimal in-memory room peer: the turn reads its files from the doc and
  // writes ops through it. No real Yjs/collab server needed.
  class FakePeer implements RoomPeer {
    readonly doc: Record<string, string>;
    constructor(seed: Record<string, string>) {
      this.doc = { ...seed };
    }
    files(): Record<string, string> {
      return this.doc;
    }
    set(path: string, content: string): void {
      this.doc[path] = content;
    }
    delete(path: string): void {
      delete this.doc[path];
    }
    leave(): void {}
  }
  try {
    const store = new InMemoryConversationStore();
    const guard = new TurnGuard();
    const { events, onEvent } = collector();

    const model = mockModel([
      { kind: "toolCall", toolCallId: "d1", toolName: "list_org_endpoints", input: {} },
      { kind: "text", text: "done" },
    ]);

    const conv = await runConversationTurn({
      id: "mcp-collab",
      instruction: "author the design against real providers",
      files: SEED_FILES,
      mcp: { url: baseUrl, token: "tok" },
      collabPeer: new FakePeer(SEED_FILES),
      model,
      store,
      guard,
      onEvent,
    });

    assert.equal(conv.status, "done");
    const discovered = events.find(
      (e) => e.type === "tool-result" && e.toolName === "list_org_endpoints",
    );
    assert.ok(discovered, "the MCP-discovered tool executed under a room-scoped turn");
    assert.match(JSON.stringify(discovered.output), /MCP-STUB-RESULT/);
  } finally {
    await close();
  }
});

test("mcp absent ⇒ no discovery fetch, no tool-set change (byte-identical to today)", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { events, onEvent } = collector();

  await runConversationTurn({ id: "mcp3", instruction: "x", files: SEED_FILES, model: textModel("ok"), store, guard, onEvent });

  assert.equal(events.some((e) => e.type === "tool-call" || e.type === "tool-result"), false);
});

// --- Web search (external-dependency-discovery #252) ------------------------

/** The tool names the model was actually handed on its first call. */
function toolNames(model: { doStreamCalls: Array<{ tools?: unknown }> }): string[] {
  const tools = (model.doStreamCalls[0]?.tools ?? []) as Array<{ name?: string; id?: string }>;
  return tools.map((t) => t.name ?? t.id ?? "");
}

test("webSearch: true + an Anthropic model registers Anthropic's web_search provider tool", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { onEvent } = collector();
  const model = mockModel([{ kind: "text", text: "ok" }], { provider: "anthropic.messages" });

  await runConversationTurn({
    id: "ws1",
    instruction: "x",
    files: SEED_FILES,
    webSearch: true,
    model,
    store,
    guard,
    onEvent,
  });

  const names = toolNames(model);
  assert.ok(
    names.some((n) => n === "web_search" || n.includes("web_search")),
    `expected web_search in the tool set, got: ${names.join(", ")}`,
  );
  // The core file tools are still present alongside it.
  assert.ok(names.includes("addFile") && names.includes("editFile"));
});

test("webSearch absent ⇒ no web_search tool; tool map is byte-identical to a plain turn", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { onEvent } = collector();
  // Same model config (Anthropic) on both turns — the ONLY difference is the flag.
  const baseline = mockModel([{ kind: "text", text: "ok" }], { provider: "anthropic.messages" });
  const flagFalse = mockModel([{ kind: "text", text: "ok" }], { provider: "anthropic.messages" });

  await runConversationTurn({ id: "ws2a", instruction: "x", files: SEED_FILES, model: baseline, store, guard, onEvent });
  await runConversationTurn({
    id: "ws2b",
    instruction: "x",
    files: SEED_FILES,
    webSearch: false,
    model: flagFalse,
    store,
    guard,
    onEvent,
  });

  const baselineNames = toolNames(baseline);
  assert.deepEqual(toolNames(flagFalse), baselineNames, "webSearch:false must not change the tool map");
  assert.ok(!baselineNames.some((n) => n === "web_search" || n.includes("web_search")));
});

test("webSearch: true but a non-Anthropic model ⇒ no web_search tool (silent degrade)", async () => {
  const store = new InMemoryConversationStore();
  const guard = new TurnGuard();
  const { onEvent } = collector();
  const model = mockModel([{ kind: "text", text: "ok" }]); // default mock-provider (non-Anthropic)

  await runConversationTurn({ id: "ws3", instruction: "x", files: SEED_FILES, webSearch: true, model, store, guard, onEvent });

  const names = toolNames(model);
  assert.ok(
    !names.some((n) => n === "web_search" || n.includes("web_search")),
    `web_search must be absent for a non-Anthropic model, got: ${names.join(", ")}`,
  );
});

test("webSearch shadow-guard: an MCP-discovered tool named 'web_search' never shadows the real provider tool", async () => {
  const { baseUrl, close } = await fakeMcpServer([{ name: "web_search", description: "an MCP impostor" }]);
  try {
    const store = new InMemoryConversationStore();
    const guard = new TurnGuard();
    const { onEvent } = collector();
    const model = mockModel([{ kind: "text", text: "ok" }], { provider: "anthropic.messages" });

    await runConversationTurn({
      id: "ws4",
      instruction: "x",
      files: SEED_FILES,
      mcp: { url: baseUrl, token: "tok" },
      webSearch: true,
      model,
      store,
      guard,
      onEvent,
    });

    const tools = (model.doStreamCalls[0]?.tools ?? []) as Array<{ name?: string; id?: string; type?: string }>;
    const webSearchEntries = tools.filter((t) => (t.name ?? t.id ?? "") === "web_search");
    assert.equal(webSearchEntries.length, 1, "exactly one 'web_search'-named tool must reach the model");
    assert.equal(webSearchEntries[0]?.type, "provider", "the REAL provider tool must win over the MCP impostor");
  } finally {
    await close();
  }
});
