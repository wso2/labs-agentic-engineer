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
 * The marketplace conversations' memory bounds: a conversation idle
 * past `MARKETPLACE_IDLE_MS` is evicted, and an owner keeps at most
 * `MARKETPLACE_PER_OWNER` (the oldest-used goes first). An evicted id is
 * unknown (404), so the console's 404 recovery starts a new one, and its
 * messages leave the store. A conversation whose turn runs is never evicted.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import type { Conversation } from "../src/store/conversation-store.js";
import { InMemoryConversationStore } from "../src/store/memory-store.js";
import { MARKETPLACE_IDLE_MS, MARKETPLACE_PER_OWNER, MarketplaceBook } from "../src/conversations/marketplace-book.js";

function rig(busy: ReadonlySet<string> = new Set()) {
  let clock = Date.parse("2026-10-04T10:00:00Z");
  const store = new InMemoryConversationStore();
  const book = new MarketplaceBook(store, { now: () => clock, busy: (id) => busy.has(id) });
  return { store, book, advance: (ms: number) => (clock += ms) };
}

function stored(id: string): Conversation {
  const at = new Date("2026-10-04T10:00:00Z");
  return { id, messages: [{ role: "user", content: "hi" }], turns: [], status: "done", createdAt: at, updatedAt: at };
}

test("a conversation idle past the bound is gone: not owned, history null, messages dropped", async () => {
  const { store, book, advance } = rig();
  const { conversationId } = await book.create("ann");
  await store.save(stored(conversationId));
  advance(MARKETPLACE_IDLE_MS - 1);
  assert.equal(book.owns(conversationId, "ann"), true, "use within the bound keeps it");
  advance(MARKETPLACE_IDLE_MS - 1);
  assert.notEqual(await book.history(conversationId, "ann"), null, "a read is a use");
  advance(MARKETPLACE_IDLE_MS + 1);
  assert.equal(book.owns(conversationId, "ann"), false);
  assert.equal(await book.history(conversationId, "ann"), null);
  assert.equal(await store.get(conversationId), null);
});

test("an owner keeps at most the cap: a new conversation evicts the least recently used", async () => {
  const { store, book, advance } = rig();
  const ids: string[] = [];
  for (let i = 0; i < MARKETPLACE_PER_OWNER; i++) {
    ids.push((await book.create("ann")).conversationId);
    await store.save(stored(ids[i]!));
    advance(1_000);
  }
  book.owns(ids[0]!, "ann"); // the first is used again: the second is now the oldest
  advance(1_000);
  const bob = await book.create("bob");
  const extra = await book.create("ann");
  assert.equal(book.owns(ids[1]!, "ann"), false, "the least recently used went");
  assert.equal(await store.get(ids[1]!), null);
  for (const id of [ids[0]!, ...ids.slice(2), extra.conversationId]) assert.equal(book.owns(id, "ann"), true);
  assert.equal(book.owns(bob.conversationId, "bob"), true, "another owner's conversations do not count");
});

test("a conversation whose turn runs is never evicted", async () => {
  const busy = new Set<string>();
  const { book, advance } = rig(busy);
  const first = await book.create("ann");
  busy.add(first.conversationId);
  advance(MARKETPLACE_IDLE_MS + 1);
  for (let i = 0; i < MARKETPLACE_PER_OWNER; i++) await book.create("ann");
  assert.equal(book.owns(first.conversationId, "ann"), true);
  busy.delete(first.conversationId);
  await book.create("ann");
  assert.equal(book.owns(first.conversationId, "ann"), false, "evicted once its turn ended");
});
