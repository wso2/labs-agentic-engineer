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

// @vitest-environment jsdom

// A turn fired from a spec DOCUMENT rather than the chat composer (#666).
//
// The behaviours here are the two decisions the feature turns on. Change runs
// QUIETLY — the chat panel stays shut, because the document is the feedback and
// opening a panel would cover the very thing the user asked to change — while
// still recording the message, so the log is never a thread with a gap in it.
// Discuss sends the same selection and asks for the panel.

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  canReplaceLog,
  chatKeyFor,
  claimStreamFold,
  getMessages,
  peekChatOpenRequest,
  replaceMessages,
} from "./chatStore";
import { useAnchoredTurn } from "./useAnchoredTurn";

const ORG = "acme";
const PROJECT = "lunch";
const KEY = chatKeyFor(ORG, PROJECT);

const ANCHOR = {
  file: "specs/requirements/PRD.md",
  nodes: [
    { name: "Rounds close automatically.", kind: "paragraph", context: "Product Decisions" },
  ],
};

const mockFetchCurrent = vi.fn();
vi.mock("./api/conversations", async (importOriginal) => {
  const real = await importOriginal<typeof import("./api/conversations")>();
  return { ...real, fetchCurrentConversationId: (...a: unknown[]) => mockFetchCurrent(...a) };
});

const mockStartTurn = vi.fn();
vi.mock("./api/turns", async (importOriginal) => {
  const real = await importOriginal<typeof import("./api/turns")>();
  return { ...real, startCollabTurn: (...a: unknown[]) => mockStartTurn(...a) };
});

// The detached fold (#666): resolved by the test, so "the turn is running" is
// a state the assertions can hold open.
const mockFold = vi.fn();
vi.mock("./runTurn", () => ({
  attachAndFoldTurn: (...a: unknown[]) => mockFold(...a),
}));

vi.mock("./currentUser", () => ({
  useCurrentAuthor: () => ({ id: "u-1", displayName: "Ann" }),
}));

function createWrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

async function mountReady() {
  const view = renderHook(() => useAnchoredTurn(ORG, PROJECT), { wrapper: createWrapper() });
  await waitFor(() => expect(view.result.current.ready).toBe(true));
  return view;
}

beforeEach(() => {
  vi.clearAllMocks();
  replaceMessages(KEY, []);
  mockFetchCurrent.mockResolvedValue("conv-1");
  mockStartTurn.mockResolvedValue("turn-1");
  mockFold.mockResolvedValue(undefined);
});

describe("useAnchoredTurn", () => {
  it("dispatches the typed words with the anchor and the intent beside them", async () => {
    const { result } = await mountReady();
    await result.current.send("make this shorter", { anchor: ANCHOR, intent: "change" });

    expect(mockStartTurn).toHaveBeenCalledWith(PROJECT, "conv-1", "make this shorter", [], true, {
      anchor: ANCHOR,
      intent: "change",
    });
  });

  it("records the message in the log, anchor and all, though nothing on screen moved", async () => {
    const { result } = await mountReady();
    await result.current.send("make this shorter", { anchor: ANCHOR, intent: "change" });

    const row = getMessages(KEY).find((m) => m.role === "user");
    expect(row).toMatchObject({ content: "make this shorter", anchor: ANCHOR, turnId: "turn-1" });
  });

  // The decision this test exists for: the panel is the thing Change must NOT
  // do. Collapsing the two sends into one gesture with different wording is
  // what asking for the panel here would mean.
  it("leaves the chat panel shut for a Change", async () => {
    const before = peekChatOpenRequest(KEY);
    const { result } = await mountReady();
    await result.current.send("make this shorter", { anchor: ANCHOR, intent: "change" });
    expect(peekChatOpenRequest(KEY)).toBe(before);
  });

  it("asks for the panel on a Discuss", async () => {
    const before = peekChatOpenRequest(KEY);
    const { result } = await mountReady();
    await result.current.send("why is this thirty minutes?", { anchor: ANCHOR, intent: "discuss" });
    expect(peekChatOpenRequest(KEY)).toBeGreaterThan(before);
  });

  // Without the claim, the panel mounting on a Discuss rehydrates and REPLACES
  // the log with server truth — which does not know about this row yet. The
  // user's own message would vanish inside the second the dispatch takes.
  it("holds the log against a rehydrate while the dispatch is in flight", async () => {
    let release!: () => void;
    mockStartTurn.mockImplementation(
      () => new Promise<string>((resolve) => { release = () => resolve("turn-1"); }),
    );
    const { result } = await mountReady();
    const sending = result.current.send("shorter", { anchor: ANCHOR, intent: "discuss" });

    await waitFor(() => expect(canReplaceLog(KEY)).toBe(false));
    release();
    await sending;
    await waitFor(() => expect(canReplaceLog(KEY)).toBe(true));
  });

  it("marks the row failed and says why, rather than adding a second copy", async () => {
    mockStartTurn.mockRejectedValue(new Error("An agent turn is already running"));
    const { result } = await mountReady();
    const ok = await result.current.send("shorter", { anchor: ANCHOR, intent: "change" });

    expect(ok).toBe(false);
    const users = getMessages(KEY).filter((m) => m.role === "user");
    expect(users).toHaveLength(1);
    expect(users[0]).toMatchObject({ status: "failed" });
    expect(getMessages(KEY).some((m) => m.role === "error")).toBe(true);
  });

  // The box closes the moment the user sends (#666), so the log is the only
  // place a refused dispatch exists — a failure that lands somewhere closed is
  // a message that silently vanished.
  it("opens the panel onto a failed dispatch, even for a quiet Change", async () => {
    mockStartTurn.mockRejectedValue(new Error("boom"));
    const before = peekChatOpenRequest(KEY);
    const { result } = await mountReady();
    await result.current.send("shorter", { anchor: ANCHOR, intent: "change" });

    expect(peekChatOpenRequest(KEY)).toBeGreaterThan(before);
  });

  // The tag on the user's message blinked: the optimistic row is the only copy
  // until the server journals the turn AT ITS END, and with no claim held for
  // the turn, any rehydrate in that window washed the row out. The fold is
  // what holds the log for the whole turn — same as the panel's own send.
  it("folds the turn, holding the log until the turn ends", async () => {
    let endTurn!: () => void;
    mockFold.mockImplementation(
      () => new Promise<void>((resolve) => { endTurn = () => resolve(); }),
    );
    const { result } = await mountReady();
    await result.current.send("make this shorter", { anchor: ANCHOR, intent: "change" });

    expect(mockFold).toHaveBeenCalledWith(KEY, PROJECT, "turn-1", expect.anything());
    // The dispatch has resolved, and the log is STILL held — by the fold.
    expect(canReplaceLog(KEY)).toBe(false);

    endTurn();
    await waitFor(() => expect(canReplaceLog(KEY)).toBe(true));
  });

  it("does not fold when a fold is already live — the panel got there first", async () => {
    const release = claimStreamFold(KEY);
    try {
      const { result } = await mountReady();
      await result.current.send("make this shorter", { anchor: ANCHOR, intent: "change" });
      expect(mockFold).not.toHaveBeenCalled();
    } finally {
      release();
    }
  });

  it("refuses a send with no words and dispatches nothing", async () => {
    const { result } = await mountReady();
    expect(await result.current.send("   ", { anchor: ANCHOR, intent: "change" })).toBe(false);
    expect(mockStartTurn).not.toHaveBeenCalled();
  });
});
