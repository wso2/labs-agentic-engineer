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

function fresh(id: string): Conversation {
  return {
    id,
    messages: [{ role: "user", content: "hi" }],
    status: "active",
    createdAt: new Date("2020-01-01T00:00:00Z"),
    updatedAt: new Date("2020-01-01T00:00:00Z"),
  };
}

test("get returns null for an unknown id", async () => {
  const store = new InMemoryConversationStore();
  assert.equal(await store.get("nope"), null);
});

test("save then get round-trips the aggregate", async () => {
  const store = new InMemoryConversationStore();
  await store.save(fresh("c1"));
  const got = await store.get("c1");
  assert.ok(got);
  assert.equal(got.id, "c1");
  assert.equal(got.status, "active");
  assert.equal(got.messages.length, 1);
});

test("get returns a deep copy — mutating it does not leak into the store", async () => {
  const store = new InMemoryConversationStore();
  await store.save(fresh("c1"));

  const a = await store.get("c1");
  assert.ok(a);
  a.messages.push({ role: "assistant", content: "leaked?" });
  a.status = "done";

  const b = await store.get("c1");
  assert.ok(b);
  assert.equal(b.messages.length, 1, "store must not see the mutation (save is the sole commit point)");
  assert.equal(b.status, "active");
});

test("the input object is cloned on save — later mutation does not leak", async () => {
  const store = new InMemoryConversationStore();
  const c = fresh("c1");
  await store.save(c);
  c.messages.push({ role: "assistant", content: "after save" });

  const got = await store.get("c1");
  assert.equal(got?.messages.length, 1);
});

test("save upserts; createdAt is preserved, updatedAt advances", async () => {
  const store = new InMemoryConversationStore();
  await store.save(fresh("c1"));
  const first = await store.get("c1");
  assert.ok(first);

  const next = fresh("c1");
  next.messages = [
    { role: "user", content: "hi" },
    { role: "assistant", content: "hello" },
  ];
  next.status = "done";
  await store.save(next);

  const second = await store.get("c1");
  assert.ok(second);
  assert.equal(second.messages.length, 2);
  assert.equal(second.status, "done");
  // createdAt is the store's first-seen value; updatedAt is store-stamped (>= createdAt).
  assert.equal(second.createdAt.getTime(), first.createdAt.getTime());
  assert.ok(second.updatedAt.getTime() >= second.createdAt.getTime());
});
