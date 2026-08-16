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

import { describe, expect, it } from "vitest";
import type { ChatMessage } from "./chatStore";
import { buildFeed, participantsOf, type FeedBlock } from "./feed";

const ME = "me@aep.dev";

const user = (
  id: string,
  content: string,
  extra: Partial<Extract<ChatMessage, { role: "user" }>> = {},
): ChatMessage => ({
  id,
  role: "user",
  content,
  status: "completed",
  ...extra,
});

const assistant = (id: string, content: string, turnId = "history"): ChatMessage => ({
  id,
  role: "assistant",
  turnId,
  content,
});

const tool = (id: string, path: string, turnId: string): ChatMessage => ({
  id,
  role: "tool",
  turnId,
  toolCallId: id,
  status: "done",
  op: "edit",
  path,
  ok: true,
});

// Narrow helpers so assertions read cleanly.
function turnAt(blocks: FeedBlock[], i: number): Extract<FeedBlock, { kind: "turn" }> {
  const b = blocks[i];
  if (b?.kind !== "turn") throw new Error(`block ${i} is not a turn`);
  return b;
}
function userAt(blocks: FeedBlock[], i: number): Extract<FeedBlock, { kind: "user" }> {
  const b = blocks[i];
  if (b?.kind !== "user") throw new Error(`block ${i} is not a user block`);
  return b;
}

describe("buildFeed — grouping", () => {
  it("returns an empty feed for no messages", () => {
    expect(buildFeed([], { currentUserId: ME })).toEqual([]);
  });

  it("pairs a user message with the agent output that follows it", () => {
    const blocks = buildFeed(
      [
        user("u1", "Add a wishlist", { turnId: "t1", author: { id: ME, displayName: "Me" } }),
        assistant("a1", "On it.", "t1"),
        tool("tc1", "specs/requirements.md", "t1"),
      ],
      { currentUserId: ME },
    );
    expect(blocks.map((b) => b.kind)).toEqual(["user", "turn"]);
    expect(turnAt(blocks, 1).items.map((i) => i.kind)).toEqual([
      "message",
      "tool-group",
    ]);
  });

  it("closes the current turn when the next user message starts", () => {
    const blocks = buildFeed(
      [
        user("u1", "first", { turnId: "t1" }),
        assistant("a1", "reply one", "t1"),
        user("u2", "second", { turnId: "t2" }),
        assistant("a2", "reply two", "t2"),
      ],
      { currentUserId: ME },
    );
    expect(blocks.map((b) => b.kind)).toEqual(["user", "turn", "user", "turn"]);
  });

  it("groups consecutive same-file tool calls inside the turn (via toolGrouping)", () => {
    const blocks = buildFeed(
      [
        user("u1", "edit", { turnId: "t1" }),
        tool("tc1", "a/design.json", "t1"),
        tool("tc2", "a/design.json", "t1"),
      ],
      { currentUserId: ME },
    );
    const turn = turnAt(blocks, 1);
    expect(turn.items).toHaveLength(1);
    const only = turn.items[0];
    expect(only?.kind).toBe("tool-group");
    if (only?.kind === "tool-group") expect(only.tools).toHaveLength(2);
  });

  it("renders a narration-only turn (zero activity steps) as one message item", () => {
    const blocks = buildFeed(
      [user("u1", "hi", { turnId: "t1" }), assistant("a1", "hello", "t1")],
      { currentUserId: ME },
    );
    const turn = turnAt(blocks, 1);
    expect(turn.items).toHaveLength(1);
    expect(turn.items[0]?.kind).toBe("message");
  });

  it("keeps an error message inside the turn it belongs to", () => {
    const blocks = buildFeed(
      [
        user("u1", "go", { turnId: "t1" }),
        assistant("a1", "trying", "t1"),
        { id: "e1", role: "error", content: "boom" },
      ],
      { currentUserId: ME },
    );
    const turn = turnAt(blocks, 1);
    expect(turn.items.map((i) => i.kind)).toEqual(["message", "message"]);
    const err = turn.items[1];
    expect(err?.kind === "message" && err.message.role).toBe("error");
  });

  it("leaves a trailing user message with no reply as a standalone user block", () => {
    const blocks = buildFeed([user("u1", "later", { turnId: "t9" })], {
      currentUserId: ME,
    });
    expect(blocks.map((b) => b.kind)).toEqual(["user"]);
  });
});

describe("buildFeed — attribution", () => {
  it("marks the current user's own turn as You (isOwn)", () => {
    const blocks = buildFeed(
      [
        user("u1", "mine", { turnId: "t1", author: { id: ME, displayName: "Me" } }),
        assistant("a1", "ok", "t1"),
      ],
      { currentUserId: ME },
    );
    expect(userAt(blocks, 0).attribution).toEqual({ displayName: "You", isOwn: true });
    expect(turnAt(blocks, 1).attribution).toEqual({ displayName: "You", isOwn: true });
  });

  it("attributes a teammate's turn to their display name (not own)", () => {
    const blocks = buildFeed(
      [
        user("u1", "theirs", {
          turnId: "t1",
          author: { id: "u-sarah", displayName: "Sarah Perera" },
        }),
        assistant("a1", "ok", "t1"),
      ],
      { currentUserId: ME },
    );
    expect(turnAt(blocks, 1).attribution).toEqual({
      displayName: "Sarah Perera",
      isOwn: false,
    });
  });

  it("falls back to You for messages with no author (old persisted logs)", () => {
    const blocks = buildFeed(
      [user("u1", "legacy"), assistant("a1", "ok", "history")],
      { currentUserId: ME },
    );
    expect(userAt(blocks, 0).attribution).toEqual({ displayName: "You", isOwn: true });
    expect(turnAt(blocks, 1).attribution.isOwn).toBe(true);
  });

  it("derives turn attribution from the turnId-linked user message when present", () => {
    const blocks = buildFeed(
      [
        user("u1", "theirs", {
          turnId: "t1",
          author: { id: "u-sarah", displayName: "Sarah Perera" },
        }),
        assistant("a1", "ok", "t1"),
      ],
      { currentUserId: ME },
    );
    expect(turnAt(blocks, 1).attribution.displayName).toBe("Sarah Perera");
  });

  it("falls back to the nearest preceding user message for rehydrated turns (no turnId)", () => {
    // Rehydrated history: user carries no turnId, assistant is turnId "history".
    const blocks = buildFeed(
      [
        user("u1", "add returns", {
          author: { id: "u-sarah", displayName: "Sarah Perera" },
        }),
        assistant("a1", "done", "history"),
      ],
      { currentUserId: ME },
    );
    expect(turnAt(blocks, 1).attribution).toEqual({
      displayName: "Sarah Perera",
      isOwn: false,
    });
  });
});

describe("buildFeed — turn lifecycle status", () => {
  it("marks an in-flight own turn as running", () => {
    const blocks = buildFeed(
      [user("u1", "go", { turnId: "t1", status: "in_flight" }), assistant("a1", "…", "t1")],
      { currentUserId: ME },
    );
    expect(turnAt(blocks, 1).status).toBe("running");
  });

  it("marks a completed turn as committed", () => {
    const blocks = buildFeed(
      [user("u1", "go", { turnId: "t1", status: "completed" }), assistant("a1", "done", "t1")],
      { currentUserId: ME },
    );
    expect(turnAt(blocks, 1).status).toBe("committed");
  });

  it("marks a failed turn as failed", () => {
    const blocks = buildFeed(
      [user("u1", "go", { turnId: "t1", status: "failed" }), assistant("a1", "oops", "t1")],
      { currentUserId: ME },
    );
    expect(turnAt(blocks, 1).status).toBe("failed");
  });

  it("marks a teammate's re-attached running turn as running via activeTurnId", () => {
    // The teammate's triggering message is rehydrated (status completed, no
    // turnId); the live reply streams with the active turnId.
    const blocks = buildFeed(
      [
        user("u1", "wire up tax", { author: { id: "u-sarah", displayName: "Sarah Perera" } }),
        assistant("a1", "working", "live-turn-1"),
      ],
      { currentUserId: ME, activeTurnId: "live-turn-1" },
    );
    const turn = turnAt(blocks, 1);
    expect(turn.status).toBe("running");
    expect(turn.attribution).toEqual({ displayName: "Sarah Perera", isOwn: false });
  });
});

// The first-run transition line (#485 live-testing round) keys off the
// initiating command, which rehydrate preserves verbatim (#463).
describe("buildFeed — startTurn", () => {
  it("flags a turn initiated by /start (bare or with an inline idea)", () => {
    const blocks = buildFeed(
      [user("u1", "/start a rota planner", { turnId: "t1" }), assistant("a1", "Reading…", "t1")],
      { currentUserId: ME },
    );
    expect(turnAt(blocks, 1).startTurn).toBe(true);
  });

  it("does not flag other commands or plain chat", () => {
    for (const content of ["/design", "/startle everyone", "please add a feature"]) {
      const blocks = buildFeed(
        [user("u1", content, { turnId: "t1" }), assistant("a1", "ok", "t1")],
        { currentUserId: ME },
      );
      expect(turnAt(blocks, 1).startTurn).toBe(false);
    }
  });

  it("does not flag a turn with no initiating user message", () => {
    const blocks = buildFeed([assistant("a1", "hello", "t1")], { currentUserId: ME });
    expect(turnAt(blocks, 0).startTurn).toBe(false);
  });
});

describe("participantsOf", () => {
  it("lists distinct authors with the current user first, as You", () => {
    const people = participantsOf(
      [
        user("u1", "a", { author: { id: ME, displayName: "Me" } }),
        user("u2", "b", { author: { id: "u-sarah", displayName: "Sarah Perera" } }),
        user("u3", "c", { author: { id: "u-sarah", displayName: "Sarah Perera" } }),
      ],
      ME,
    );
    expect(people).toEqual([
      { id: ME, displayName: "You", isOwn: true },
      { id: "u-sarah", displayName: "Sarah Perera", isOwn: false },
    ]);
  });

  it("treats an author-less message as the current user (You)", () => {
    const people = participantsOf([user("u1", "legacy")], ME);
    expect(people).toEqual([{ id: ME, displayName: "You", isOwn: true }]);
  });

  it("returns no participants for an empty log", () => {
    expect(participantsOf([], ME)).toEqual([]);
  });
});
