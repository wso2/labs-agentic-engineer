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

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

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
  ensureUserMessage,
  getMessages,
  hasDeterministicFlush,
  notifyTurnEnd,
  peekPendingSeed,
  registerDeterministicFlush,
  setPendingSeed,
  settleUserMessage,
  setTurnStatus,
  subscribe,
  subscribeSeed,
  subscribeTurnEnd,
  upsertToolMessage,
  clearFailedSends,
  claimSendInFlight,
  claimStreamFold,
  hasLocalTurnActivity,
  subscribeLocalTurnActivity,
  SEED_ACTIVITY_TTL_MS,
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
    expect(peekPendingSeed(key)?.message).toBe("resolve dependency A");
    expect(peekPendingSeed(key)?.message).toBe("resolve dependency A"); // peek doesn't clear

    expect(consumePendingSeed(key)?.message).toBe("resolve dependency A");
    expect(peekPendingSeed(key)).toBeNull();
    expect(consumePendingSeed(key)).toBeNull(); // already consumed
  });

  it("keeps distinct project keys apart", () => {
    const key1 = freshKey();
    const key2 = freshKey();
    setPendingSeed(key1, "for project 1");
    expect(peekPendingSeed(key2)).toBeNull();
    expect(consumePendingSeed(key1)?.message).toBe("for project 1");
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

// #562: the two writes a turn nobody in this browser sent depends on.
describe("chatStore — an optimistic send, and a turn this browser didn't send", () => {
  // The send paints BEFORE the dispatch answers, so the row starts with no
  // turn id and is settled once one comes back.
  it("settles an optimistic row with the turn it became", () => {
    const key = freshKey();
    const id = addMessage(key, { role: "user", content: "tidy the spec", status: "in_flight" });

    settleUserMessage(key, id, { turnId: "t1" });

    const row = getMessages(key)[0];
    expect(row).toMatchObject({ role: "user", turnId: "t1", status: "in_flight" });
  });

  // The row the user is already looking at becomes the failed one — a second
  // copy beside it would read as two sends.
  it("fails the same row rather than adding another", () => {
    const key = freshKey();
    const id = addMessage(key, { role: "user", content: "tidy the spec", status: "in_flight" });

    settleUserMessage(key, id, { failed: true });

    expect(getMessages(key)).toHaveLength(1);
    expect(getMessages(key)[0]).toMatchObject({ status: "failed" });
  });

  it("paints the row that started a turn this browser did not send", () => {
    const key = freshKey();
    ensureUserMessage(key, {
      role: "user",
      content: "/start an expense tracker",
      turnId: "t1",
      status: "in_flight",
      author: { id: "them@x.com", displayName: "Them" },
    });

    expect(getMessages(key)[0]).toMatchObject({
      content: "/start an expense tracker",
      author: { id: "them@x.com", displayName: "Them" },
    });
  });

  // Every path that reaches it runs more than once — mount, the poll, and a
  // re-attach after a dropped stream.
  it("is idempotent on the turn id", () => {
    const key = freshKey();
    const row = {
      role: "user" as const,
      content: "/start an expense tracker",
      turnId: "t1",
      status: "in_flight" as const,
    };
    ensureUserMessage(key, row);
    ensureUserMessage(key, row);

    expect(getMessages(key)).toHaveLength(1);
  });

  // The sender's own row already carries the id, so re-attaching to their own
  // turn must not duplicate what they typed.
  it("leaves the sender's own row alone", () => {
    const key = freshKey();
    const id = addMessage(key, { role: "user", content: "mine", status: "in_flight" });
    settleUserMessage(key, id, { turnId: "t1" });

    ensureUserMessage(key, {
      role: "user",
      content: "mine",
      turnId: "t1",
      status: "in_flight",
    });

    expect(getMessages(key)).toHaveLength(1);
  });
});

describe("clearFailedSends", () => {
  // A fresh key per case: the store caches logs in a module-level Map, so
  // clearing localStorage alone would not reset it.
  let n = 0;
  let KEY = "";
  beforeEach(() => {
    n += 1;
    KEY = `aep.chat.v1.acme.failed-sends-${n}`;
  });

  it("drops the failed user row and the error row a refused send left", () => {
    // Both are LOCAL-ONLY: the rehydrate re-appends them after the server
    // history on every refocus, so without a bound a failure stays pinned below
    // newer turns forever and reads as a retry.
    addMessage(KEY, { role: "user", content: "went nowhere", status: "failed" });
    addMessage(KEY, { role: "error", content: "Failed to reach the agent." });
    clearFailedSends(KEY);
    expect(getMessages(KEY)).toEqual([]);
  });

  it("keeps delivered rows — only the failure is history", () => {
    addMessage(KEY, { role: "user", content: "landed", turnId: "t1", status: "completed" });
    addMessage(KEY, { role: "assistant", turnId: "t1", content: "done" });
    addMessage(KEY, { role: "user", content: "went nowhere", status: "failed" });
    clearFailedSends(KEY);
    expect(getMessages(KEY).map((m) => m.role)).toEqual(["user", "assistant"]);
    expect(getMessages(KEY)[0]).toMatchObject({ content: "landed" });
  });

  it("keeps an in-flight row, which has not failed", () => {
    addMessage(KEY, { role: "user", content: "running", turnId: "t2", status: "in_flight" });
    clearFailedSends(KEY);
    expect(getMessages(KEY)).toHaveLength(1);
  });

  it("is a no-op when there is nothing to drop", () => {
    addMessage(KEY, { role: "user", content: "landed", turnId: "t1", status: "completed" });
    const before = getMessages(KEY);
    clearFailedSends(KEY);
    // Same array identity: a needless persist would remount the whole log.
    expect(getMessages(KEY)).toBe(before);
  });
});

// #635: the chain a submitted send rides from form to turn — seed waiting,
// dispatch in flight, stream being folded. Any stage live means this browser
// holds evidence of a turn the status endpoint may not report yet.
describe("local turn activity", () => {
  it("is live through each stage of a send, and collapses when the last releases", () => {
    const key = freshKey();
    expect(hasLocalTurnActivity(key)).toBe(false);

    setPendingSeed(key, "answers");
    expect(hasLocalTurnActivity(key)).toBe(true);

    // Consumption hands over to the send claim with no dead stage between.
    const releaseSend = claimSendInFlight(key);
    consumePendingSeed(key);
    expect(hasLocalTurnActivity(key)).toBe(true);

    const releaseFold = claimStreamFold(key);
    releaseSend();
    expect(hasLocalTurnActivity(key)).toBe(true);

    releaseFold();
    expect(hasLocalTurnActivity(key)).toBe(false);
  });

  it("collapses when a send dies before a turn exists", () => {
    const key = freshKey();
    const release = claimSendInFlight(key);
    expect(hasLocalTurnActivity(key)).toBe(true);
    release();
    expect(hasLocalTurnActivity(key)).toBe(false);
  });

  it("notifies on every edge: seed set/consumed, claim taken/released", () => {
    const key = freshKey();
    let fired = 0;
    const unsubscribe = subscribeLocalTurnActivity(key, () => {
      fired += 1;
    });
    setPendingSeed(key, "answers");
    consumePendingSeed(key);
    const release = claimSendInFlight(key);
    release();
    expect(fired).toBe(4);

    unsubscribe();
    setPendingSeed(key, "again");
    expect(fired).toBe(4);
    consumePendingSeed(key);
  });

  it("keeps distinct keys apart", () => {
    const key1 = freshKey();
    const key2 = freshKey();
    const release = claimSendInFlight(key1);
    expect(hasLocalTurnActivity(key2)).toBe(false);
    release();
  });

  // The seed is the one stage with no failure path of its own: its consumer
  // sits behind gates an outage can hold shut, and an unconsumed seed would
  // pin a working state that HIDES Retry. So only the seed's contribution
  // expires — and expiry is an edge subscribers hear about, or the pane
  // would hold the stale state until an unrelated re-render.
  describe("seed TTL", () => {
    beforeEach(() => vi.useFakeTimers());
    afterEach(() => vi.useRealTimers());

    it("expires a waiting seed from the signal, and notifies the edge", () => {
      const key = freshKey();
      let fired = 0;
      const unsubscribe = subscribeLocalTurnActivity(key, () => {
        fired += 1;
      });
      setPendingSeed(key, "answers");
      expect(hasLocalTurnActivity(key)).toBe(true);
      const firedBeforeExpiry = fired;

      vi.advanceTimersByTime(SEED_ACTIVITY_TTL_MS);
      expect(hasLocalTurnActivity(key)).toBe(false);
      expect(fired).toBe(firedBeforeExpiry + 1);

      // The seed itself is untouched by the lapse — still there to consume.
      expect(peekPendingSeed(key)).toEqual({ message: "answers", guarded: false });
      unsubscribe();
      consumePendingSeed(key);
    });

    it("does not expire the claims — their release paths own their end", () => {
      const key = freshKey();
      const release = claimSendInFlight(key);
      vi.advanceTimersByTime(SEED_ACTIVITY_TTL_MS * 2);
      expect(hasLocalTurnActivity(key)).toBe(true);
      release();
    });

    it("a fresh seed restarts the clock", () => {
      const key = freshKey();
      setPendingSeed(key, "first");
      vi.advanceTimersByTime(SEED_ACTIVITY_TTL_MS - 1000);
      setPendingSeed(key, "second");
      vi.advanceTimersByTime(2000);
      expect(hasLocalTurnActivity(key)).toBe(true);
      vi.advanceTimersByTime(SEED_ACTIVITY_TTL_MS);
      expect(hasLocalTurnActivity(key)).toBe(false);
      consumePendingSeed(key);
    });
  });
});
