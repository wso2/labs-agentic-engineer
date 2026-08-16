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

import { beforeEach, describe, expect, it } from "vitest";

// Node test env: the store only touches localStorage — a Map-backed stub
// keeps the test out of jsdom.
const backing = new Map<string, string>();
globalThis.localStorage = {
  getItem: (k: string) => backing.get(k) ?? null,
  setItem: (k: string, v: string) => void backing.set(k, v),
  removeItem: (k: string) => void backing.delete(k),
  clear: () => backing.clear(),
  key: (i: number) => [...backing.keys()][i] ?? null,
  get length() {
    return backing.size;
  },
} as Storage;
import {
  addMessage,
  appendAssistantText,
  chatKeyFor,
  consumePendingSeed,
  dropTurnOutput,
  getMessages,
  hasDeterministicFlush,
  notifyTurnEnd,
  peekPendingSeed,
  registerDeterministicFlush,
  setPendingSeed,
  setTurnStatus,
  subscribe,
  subscribeSeed,
  subscribeTurnEnd,
  upsertToolMessage,
} from "./chatStore";

let n = 0;
function freshKey(): string {
  n += 1;
  return chatKeyFor("test-org", `proj-${n}`);
}

beforeEach(() => localStorage.clear());

describe("chatStore", () => {
  it("drops a stale question message with no questions[] on load (schema guard)", () => {
    const key = freshKey();
    // A log written by an older build: a `question` message before the
    // questions[] shape. Must not crash the card renderer on load.
    localStorage.setItem(
      key,
      JSON.stringify([
        { id: "m1", role: "user", content: "grill me", status: "completed" },
        { id: "m2", role: "question", turnId: "t1", toolCallId: "tc", question: "old?", options: [{ label: "A" }] },
        { id: "m3", role: "question", turnId: "t1", toolCallId: "tc2", questions: [{ question: "new?", options: [{ label: "B" }] }] },
      ]),
    );
    const msgs = getMessages(key);
    expect(msgs.map((m) => m.id)).toEqual(["m1", "m3"]); // legacy question dropped, valid one kept
  });

  it("appends and notifies subscribers", () => {
    const key = freshKey();
    let notified = 0;
    subscribe(key, () => (notified += 1));
    addMessage(key, { role: "user", content: "hi", turnId: "t1", status: "in_flight" });
    expect(getMessages(key)).toHaveLength(1);
    expect(notified).toBe(1);
  });

  it("accumulates streamed text into one assistant message per turn", () => {
    const key = freshKey();
    appendAssistantText(key, "t1", "Hello ");
    appendAssistantText(key, "t1", "world");
    appendAssistantText(key, "t2", "next turn");
    const msgs = getMessages(key);
    expect(msgs).toHaveLength(2);
    expect(msgs[0]).toMatchObject({ role: "assistant", content: "Hello world" });
    expect(msgs[1]).toMatchObject({ role: "assistant", content: "next turn" });
  });

  it("marks the turn's user bubble on terminal", () => {
    const key = freshKey();
    addMessage(key, { role: "user", content: "do it", turnId: "t1", status: "in_flight" });
    setTurnStatus(key, "t1", "completed");
    expect(getMessages(key)[0]).toMatchObject({ status: "completed" });
  });

  it("dropTurnOutput removes streamed output but keeps the user bubble", () => {
    const key = freshKey();
    addMessage(key, { role: "user", content: "go", turnId: "t1", status: "in_flight" });
    appendAssistantText(key, "t1", "partial");
    addMessage(key, {
      role: "tool",
      turnId: "t1",
      toolCallId: "c1",
      status: "done",
      op: "edit",
      path: "requirements/prd.md",
      ok: true,
    });
    dropTurnOutput(key, "t1");
    const msgs = getMessages(key);
    expect(msgs).toHaveLength(1);
    expect(msgs[0]?.role).toBe("user");
  });

  it("upsertToolMessage inserts a streaming card then flips it to done in place", () => {
    const key = freshKey();
    const base = {
      role: "tool" as const,
      turnId: "t1",
      toolCallId: "c1",
      op: "add",
      path: "specs/requirements/prd.md",
      ok: true,
    };
    upsertToolMessage(key, { ...base, status: "streaming" });
    expect(getMessages(key)).toHaveLength(1);
    expect(getMessages(key)[0]).toMatchObject({ status: "streaming", op: "add" });
    upsertToolMessage(key, { ...base, status: "done" });
    const msgs = getMessages(key);
    expect(msgs).toHaveLength(1); // same card, updated in place — no duplicate row
    expect(msgs[0]).toMatchObject({ status: "done", op: "add" });
  });

  it("upsertToolMessage keeps distinct toolCallIds apart and never matches a blank id", () => {
    const key = freshKey();
    const mk = (toolCallId: string, path: string) => ({
      role: "tool" as const, turnId: "t1", toolCallId, status: "done" as const, op: "add", path, ok: true,
    });
    upsertToolMessage(key, mk("c1", "a.md"));
    upsertToolMessage(key, mk("c2", "b.md"));
    upsertToolMessage(key, mk("", "c.md"));
    upsertToolMessage(key, mk("", "d.md"));
    expect(getMessages(key)).toHaveLength(4);
  });

  it("round-trips author through append and in-memory persist", () => {
    const key = freshKey();
    addMessage(key, {
      role: "user",
      content: "hi team",
      turnId: "t1",
      status: "in_flight",
      author: { id: "u-sarah", displayName: "Sarah Perera" },
    });
    expect(getMessages(key)[0]).toMatchObject({
      author: { id: "u-sarah", displayName: "Sarah Perera" },
    });
  });

  it("loads a persisted author back from localStorage on first read", () => {
    const key = freshKey();
    localStorage.setItem(
      key,
      JSON.stringify([
        {
          id: "m-1",
          role: "user",
          content: "hey",
          status: "completed",
          author: { id: "u-2", displayName: "Bo" },
        },
      ]),
    );
    expect(getMessages(key)[0]).toMatchObject({
      author: { id: "u-2", displayName: "Bo" },
    });
  });

  it("loads legacy persisted messages with no author field intact", () => {
    const key = freshKey();
    localStorage.setItem(
      key,
      JSON.stringify([{ id: "m-1", role: "user", content: "hey", status: "completed" }]),
    );
    const [msg] = getMessages(key);
    expect(msg).toMatchObject({ role: "user", content: "hey" });
    expect((msg as { author?: unknown } | undefined)?.author).toBeUndefined();
  });

  // The conversation id is no longer minted here (#430): it is server-minted,
  // stored against the project, and resolved via api/conversations.ts — the
  // store keeps only the local display log.
});

// pendingSeed (#252 Task 5): the "Resolve via chat" action writes here from a
// different subtree than the panel (dep card / drawer vs. AgentChatPanel,
// siblings under AppLayout) — consumed exactly once, mirroring the ?generate=
// one-shot-fire shape but sourced from the store since the seeded message is
// per-click dynamic content, not a fixed enum signal.
describe("pendingSeed", () => {
  it("is absent until set, then consumed exactly once", () => {
    const key = freshKey();
    expect(peekPendingSeed(key)).toBeNull();
    expect(consumePendingSeed(key)).toBeNull();

    setPendingSeed(key, "resolve dependency A");
    expect(peekPendingSeed(key)).toBe("resolve dependency A");
    expect(peekPendingSeed(key)).toBe("resolve dependency A"); // peek doesn't clear

    expect(consumePendingSeed(key)).toBe("resolve dependency A");
    expect(peekPendingSeed(key)).toBeNull();
    expect(consumePendingSeed(key)).toBeNull(); // already consumed
  });

  it("keeps distinct project keys apart", () => {
    const key1 = freshKey();
    const key2 = freshKey();
    setPendingSeed(key1, "for project 1");
    expect(peekPendingSeed(key2)).toBeNull();
    expect(consumePendingSeed(key1)).toBe("for project 1");
  });

  it("notifies seed subscribers on set, and stops after unsubscribe", () => {
    const key = freshKey();
    let notified = 0;
    const unsubscribe = subscribeSeed(key, () => (notified += 1));
    setPendingSeed(key, "go");
    expect(notified).toBe(1);
    unsubscribe();
    setPendingSeed(key, "go again");
    expect(notified).toBe(1);
  });

  // Minor #2 (fix wave 1): consumePendingSeed used to clear the slot without
  // notifying, so useHasPendingSeed's useSyncExternalStore snapshot could
  // stay stuck `true` after the panel consumed the seed.
  it("also notifies seed subscribers on consume, not just on set", () => {
    const key = freshKey();
    let notified = 0;
    subscribeSeed(key, () => (notified += 1));
    setPendingSeed(key, "go");
    expect(notified).toBe(1);
    consumePendingSeed(key);
    expect(notified).toBe(2);
  });
});

// Turn-end bus (#252 Task 5): "a collab turn's terminal frame arrived" is
// broadcast through this same key-scoped pub/sub (mirroring the message-log
// subscribe() above) so both the chat panel's universal fallback and the
// spec view's deterministic flush (different subtrees, only one of which
// owns the collab connection) can react to the same event.
describe("turn-end bus", () => {
  it("notifies subscribers with the terminal status", () => {
    const key = freshKey();
    const seen: string[] = [];
    subscribeTurnEnd(key, (status) => seen.push(status));
    notifyTurnEnd(key, "completed");
    notifyTurnEnd(key, "failed");
    expect(seen).toEqual(["completed", "failed"]);
  });

  it("keeps distinct project keys apart", () => {
    const key1 = freshKey();
    const key2 = freshKey();
    let key1Fired = false;
    subscribeTurnEnd(key1, () => (key1Fired = true));
    notifyTurnEnd(key2, "completed");
    expect(key1Fired).toBe(false);
  });

  it("stops notifying after unsubscribe", () => {
    const key = freshKey();
    let count = 0;
    const unsubscribe = subscribeTurnEnd(key, () => (count += 1));
    unsubscribe();
    notifyTurnEnd(key, "completed");
    expect(count).toBe(0);
  });
});

// Deterministic-flush registration (#252 Task 5 fix wave 1, Important #1):
// lets useTurnEndDependencyRefresh (the universal fallback, mounted on every
// route) know whether useTurnEndFlush (the deterministic path, mounted only
// in SpecView) is currently live for the same chat key, so the fallback can
// skip its own immediate invalidate and avoid racing ahead of the
// deterministic post-flush invalidate.
describe("deterministic-flush registration", () => {
  it("is false until registered, true while registered, false again after unregister", () => {
    const key = freshKey();
    expect(hasDeterministicFlush(key)).toBe(false);
    const unregister = registerDeterministicFlush(key);
    expect(hasDeterministicFlush(key)).toBe(true);
    unregister();
    expect(hasDeterministicFlush(key)).toBe(false);
  });

  it("keeps distinct keys apart", () => {
    const key1 = freshKey();
    const key2 = freshKey();
    registerDeterministicFlush(key1);
    expect(hasDeterministicFlush(key2)).toBe(false);
  });

  it("ref-counts overlapping registrations for the same key", () => {
    const key = freshKey();
    const unregisterA = registerDeterministicFlush(key);
    const unregisterB = registerDeterministicFlush(key);
    unregisterA();
    expect(hasDeterministicFlush(key)).toBe(true); // one registration still live
    unregisterB();
    expect(hasDeterministicFlush(key)).toBe(false);
  });
});
