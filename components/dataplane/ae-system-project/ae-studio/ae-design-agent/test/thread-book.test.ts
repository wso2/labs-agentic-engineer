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
import type { Conversation } from "../src/store/conversation-store.js";
import { InMemoryConversationStore } from "../src/store/memory-store.js";
import { ThreadBook } from "../src/conversations/thread-book.js";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const WINDOW = 200_000;

function book(store = new InMemoryConversationStore()) {
  return { store, threads: new ThreadBook({ store, now: () => new Date("2026-10-03T10:00:00Z") }) };
}

function conversation(id: string): Conversation {
  return {
    id,
    messages: [
      { role: "user", content: "COMPOSED PROMPT with skills" },
      { role: "assistant", content: "done" },
    ],
    turns: [
      {
        turnId: "t1",
        text: "add a login page",
        author: { id: "ann@example.com", displayName: "Ann" },
        messageIndex: 0,
        createdAt: new Date("2026-10-03T10:00:00Z"),
      },
    ],
    status: "done",
    createdAt: new Date("2026-10-03T10:00:00Z"),
    updatedAt: new Date("2026-10-03T10:00:00Z"),
  };
}

test("current lazily creates one uuid thread that every member shares", () => {
  const { threads } = book();
  const ann = threads.current("greeter", "Ann");
  const bob = threads.current("greeter", "Bob");
  assert.match(ann.conversationId, UUID);
  assert.deepEqual(bob, ann, "the second member gets the same thread, still credited to its creator");
  assert.deepEqual(ann, {
    conversationId: ann.conversationId,
    createdAt: "2026-10-03T10:00:00.000Z",
    createdBy: "Ann",
    current: true,
  });
});

test("projects have separate threads; an anonymous creator leaves createdBy out", () => {
  const { threads } = book();
  const a = threads.current("greeter");
  const b = threads.current("billing", "Ann");
  assert.notEqual(a.conversationId, b.conversationId);
  assert.equal("createdBy" in a, false);
});

test("history of the current thread before its first turn is []", async () => {
  const { threads } = book();
  const { conversationId } = threads.current("greeter", "Ann");
  assert.deepEqual(await threads.history("greeter", conversationId), []);
});

test("history serves the display projection read through the store", async () => {
  const { store, threads } = book();
  const { conversationId } = threads.current("greeter", "Ann");
  await store.save(conversation(conversationId));
  assert.deepEqual(await threads.history("greeter", conversationId), [
    { role: "user", content: "add a login page", author: { id: "ann@example.com", displayName: "Ann" } },
    { role: "assistant", content: "done" },
  ]);
});

test("history of an unknown id, or of another project's thread, is null", async () => {
  const { threads } = book();
  const { conversationId } = threads.current("greeter", "Ann");
  assert.equal(await threads.history("greeter", "6a2b1c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"), null);
  assert.equal(await threads.history("billing", conversationId), null);
});

test("rotate replaces the thread and drops the old one's messages", async () => {
  const { store, threads } = book();
  const old = threads.current("greeter", "Ann");
  await store.save(conversation(old.conversationId));

  const fresh = await threads.rotate("greeter", "Bob");
  assert.match(fresh.conversationId, UUID);
  assert.notEqual(fresh.conversationId, old.conversationId);
  assert.equal(fresh.createdBy, "Bob");
  assert.deepEqual(threads.current("greeter", "Ann"), fresh);
  assert.equal(await threads.history("greeter", old.conversationId), null);
  assert.equal(await store.get(old.conversationId), null, "the old messages are gone from the store");
  assert.deepEqual(await threads.history("greeter", fresh.conversationId), []);
});

test("rotate with no current thread mints one", async () => {
  const { threads } = book();
  const fresh = await threads.rotate("greeter", "Ann");
  assert.deepEqual(threads.current("greeter"), fresh);
});

test("admit lets a send to the current thread through", async () => {
  const { threads } = book();
  const { conversationId } = threads.current("greeter", "Ann");
  assert.equal(await threads.admit("greeter", conversationId, WINDOW), "ok");
});

test("admit refuses a send to a demoted or unknown id", async () => {
  const { threads } = book();
  const old = threads.current("greeter", "Ann");
  await threads.rotate("greeter", "Ann");
  assert.equal(await threads.admit("greeter", old.conversationId), "rotated");
  assert.equal(await threads.admit("billing", old.conversationId), "rotated", "no thread yet in that project");
});

test("admit rotates once the last context is past 80 % of the window", async () => {
  const { store, threads } = book();
  const full = threads.current("greeter", "Ann");
  await store.save(conversation(full.conversationId));
  threads.noteContextTokens("greeter", full.conversationId, 160_001);

  assert.equal(await threads.admit("greeter", full.conversationId, WINDOW), "rotated");
  const fresh = threads.current("greeter", "Bob");
  assert.notEqual(fresh.conversationId, full.conversationId);
  assert.equal(await store.get(full.conversationId), null);
  assert.equal(await threads.admit("greeter", fresh.conversationId, WINDOW), "ok", "the fresh thread starts empty");
});

test("admit keeps the thread at exactly 80 % (the Go rule is strictly past it)", async () => {
  const { threads } = book();
  const { conversationId } = threads.current("greeter", "Ann");
  threads.noteContextTokens("greeter", conversationId, 160_000);
  assert.equal(await threads.admit("greeter", conversationId, WINDOW), "ok");
});

test("no contextWindow (or a non-positive one) never rotates", async () => {
  const { threads } = book();
  const { conversationId } = threads.current("greeter", "Ann");
  threads.noteContextTokens("greeter", conversationId, 10_000_000);
  assert.equal(await threads.admit("greeter", conversationId), "ok");
  assert.equal(await threads.admit("greeter", conversationId, 0), "ok");
  assert.equal(threads.current("greeter").conversationId, conversationId);
});

test("context tokens noted for a thread that is no longer current are ignored", async () => {
  const { threads } = book();
  const old = threads.current("greeter", "Ann");
  const fresh = await threads.rotate("greeter", "Ann");
  // A turn that ran on the old thread finishes after the rotation.
  threads.noteContextTokens("greeter", old.conversationId, 190_000);
  assert.equal(await threads.admit("greeter", fresh.conversationId, WINDOW), "ok");
});

test("resume adopts a kept thread id as the current thread; an open thread is never replaced", async () => {
  const { store, threads } = book();
  await store.save(conversation("kept-id"));
  assert.equal(threads.resume("p", "kept-id").conversationId, "kept-id");
  assert.equal(threads.current("p").conversationId, "kept-id");
  assert.equal(await threads.admit("p", "kept-id", WINDOW), "ok");
  assert.ok((await threads.history("p", "kept-id"))!.length > 0, "its history is served");
  // Once the project has a thread, resume answers it and changes nothing.
  assert.equal(threads.resume("p", "other-id").conversationId, "kept-id");
});
