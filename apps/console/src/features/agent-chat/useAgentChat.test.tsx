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

// The shared project thread (#430): the conversation id is server-resolved,
// the server's history REPLACES the local paint-cache (D6), a rotated thread
// heals by re-resolving, and rotation itself is a server act. These pin the
// behaviors that make the thread genuinely shared — each was impossible or
// wrong under the per-browser localStorage id.

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  addMessage,
  chatKeyFor,
  getMessages,
  replaceMessages,
  upsertQuestionMessage,
} from "./chatStore";
import { useAgentChat } from "./useAgentChat";
import { useConversationLog } from "./useConversationLog";

const ORG = "acme";
const PROJECT = "proj1";
const KEY = chatKeyFor(ORG, PROJECT);

const mockFetchCurrent = vi.fn();
const mockRotate = vi.fn();
vi.mock("./api/conversations", async (importOriginal) => {
  const real = await importOriginal<typeof import("./api/conversations")>();
  return {
    ...real,
    fetchCurrentConversationId: (...a: unknown[]) => mockFetchCurrent(...a),
    rotateConversation: (...a: unknown[]) => mockRotate(...a),
    rotateCurrentConversation: async (_qc: unknown, projectName: string) =>
      mockRotate(projectName),
  };
});

const mockGetHistory = vi.fn();
const mockGetActive = vi.fn();
const mockStartTurn = vi.fn();
vi.mock("./api/turns", async (importOriginal) => {
  const real = await importOriginal<typeof import("./api/turns")>();
  return {
    ...real, // ConversationRotatedError stays REAL — the hook instanceof-checks it
    getConversationMessages: (...a: unknown[]) => mockGetHistory(...a),
    getActiveTurn: (...a: unknown[]) => mockGetActive(...a),
    startCollabTurn: (...a: unknown[]) => mockStartTurn(...a),
  };
});

const mockAttach = vi.fn();
vi.mock("./runTurn", () => ({
  attachAndFoldTurn: (...a: unknown[]) => mockAttach(...a),
}));

vi.mock("./currentUser", () => ({
  useCurrentAuthor: () => ({ id: "u1", displayName: "Ada" }),
}));

// One QueryClient per RENDER TREE, not per wrapper re-render: minting inside
// the component would hand the provider a fresh client on every hook state
// change, silently discarding the cache — and with it the invalidation and
// setQueryData effects these tests exist to observe.
function createWrapper() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

const SERVER_HISTORY = [
  { role: "user", content: "from the server", author: { id: "u2", displayName: "Grace" } },
  { role: "assistant", content: "server reply" },
];

describe("useAgentChat — the shared thread (#430)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    replaceMessages(KEY, []);
    mockFetchCurrent.mockResolvedValue("conv-1");
    mockGetHistory.mockResolvedValue(SERVER_HISTORY);
    mockGetActive.mockResolvedValue(null);
    mockAttach.mockResolvedValue(undefined);
  });

  it("REPLACES the stale local cache with server truth on mount (D6)", async () => {
    // The pre-#430 rule rehydrated only an EMPTY log — a populated local fork
    // silently shadowed everything teammates had said since.
    addMessage(KEY, { role: "user", content: "stale local fork", status: "completed" });

    const { result } = renderHook(() => useAgentChat(ORG, PROJECT), { wrapper: createWrapper() });

    await waitFor(() => expect(result.current.conversationReady).toBe(true));
    await waitFor(() => {
      const contents = getMessages(KEY).map((m) => ("content" in m ? m.content : ""));
      expect(contents).toContain("from the server");
      expect(contents).not.toContain("stale local fork");
    });
    expect(mockGetHistory).toHaveBeenCalledWith(PROJECT, "conv-1");
  });

  // The re-created-project bug: the local cache is keyed by org/project NAME,
  // so a new project reusing a name inherits the dead project's log. Its fresh
  // thread has no history ([] — the api maps the BFF's 404 to "empty"), and
  // empty truth must still REPLACE.
  it("clears the stale cache when the server thread is empty", async () => {
    addMessage(KEY, { role: "user", content: "the dead project's log", status: "completed" });
    mockGetHistory.mockResolvedValue([]);

    const { result } = renderHook(() => useAgentChat(ORG, PROJECT), { wrapper: createWrapper() });

    await waitFor(() => expect(result.current.conversationReady).toBe(true));
    await waitFor(() => expect(getMessages(KEY)).toEqual([]));
  });

  it("keeps the cache when the rehydrate FAILS (null ≠ empty)", async () => {
    addMessage(KEY, { role: "user", content: "still worth painting", status: "completed" });
    mockGetHistory.mockResolvedValue(null); // transient failure

    const { result } = renderHook(() => useAgentChat(ORG, PROJECT), { wrapper: createWrapper() });

    await waitFor(() => expect(result.current.conversationReady).toBe(true));
    await waitFor(() => expect(mockGetHistory).toHaveBeenCalled());
    expect(getMessages(KEY).some((m) => "content" in m && m.content === "still worth painting")).toBe(
      true,
    );
  });

  it("sends against the RESOLVED id, never a local mint", async () => {
    const { result } = renderHook(() => useAgentChat(ORG, PROJECT), { wrapper: createWrapper() });
    await waitFor(() => expect(result.current.conversationReady).toBe(true));

    mockStartTurn.mockResolvedValue("turn-1");
    await act(async () => {
      await result.current.send("hello");
    });

    // The 4th arg is the message's chat attachments (#428) — empty for a plain
    // send, and passed explicitly rather than omitted so the wire shape is one
    // code path.
    await waitFor(() =>
      expect(mockStartTurn).toHaveBeenCalledWith(PROJECT, "conv-1", "hello", [], true),
    );
  });

  it("omits collab when this chat is not a spec workspace", async () => {
    const { result } = renderHook(() => useAgentChat(ORG, PROJECT, { collab: false }), {
      wrapper: createWrapper(),
    });
    await waitFor(() => expect(result.current.conversationReady).toBe(true));

    mockStartTurn.mockResolvedValue("turn-1");
    await act(async () => {
      await result.current.send("hello");
    });

    await waitFor(() =>
      expect(mockStartTurn).toHaveBeenCalledWith(PROJECT, "conv-1", "hello", [], false),
    );
  });

  it("holds sends until the thread id resolves", async () => {
    mockFetchCurrent.mockReturnValue(new Promise(() => {})); // never resolves
    const { result } = renderHook(() => useAgentChat(ORG, PROJECT), { wrapper: createWrapper() });

    expect(result.current.conversationReady).toBe(false);
    await act(async () => {
      await result.current.send("too early");
    });
    expect(mockStartTurn).not.toHaveBeenCalled();
  });

  it("heals a rotated thread: 409 → failed row → re-resolve → fresh history", async () => {
    const { ConversationRotatedError } = await import("./api/turns");
    // Hold the NEW thread's rehydrate until the transient failure rows are
    // asserted — the D6 replace legitimately washes them out right after.
    let releaseFreshHistory: (h: unknown[]) => void = () => {};
    mockGetHistory.mockImplementation((_p: unknown, id: unknown) =>
      id === "conv-2"
        ? new Promise((r) => {
            releaseFreshHistory = r;
          })
        : Promise.resolve(SERVER_HISTORY),
    );

    const { result } = renderHook(() => useAgentChat(ORG, PROJECT), { wrapper: createWrapper() });
    await waitFor(() => expect(result.current.conversationReady).toBe(true));

    // A teammate rotated: the send 409s, the re-resolve answers the NEW id.
    mockStartTurn.mockRejectedValue(new ConversationRotatedError());
    mockFetchCurrent.mockResolvedValue("conv-2");
    await act(async () => {
      await result.current.send("into the demoted thread");
    });

    await waitFor(() => {
      const msgs = getMessages(KEY);
      expect(msgs.some((m) => m.role === "user" && m.status === "failed")).toBe(true);
      expect(
        msgs.some((m) => m.role === "error" && m.content.includes("replaced with a new one")),
      ).toBe(true);
    });
    // The invalidation re-resolves and the effect rehydrates the fresh thread.
    await waitFor(() => expect(mockGetHistory).toHaveBeenCalledWith(PROJECT, "conv-2"));
    releaseFreshHistory([]);
  });

  it("newConversation rotates SERVER-side and clears the local cache", async () => {
    addMessage(KEY, { role: "user", content: "old thread talk", status: "completed" });
    mockRotate.mockResolvedValue("conv-9");
    mockGetHistory.mockResolvedValue([]); // the fresh thread is empty

    const { result } = renderHook(() => useAgentChat(ORG, PROJECT), { wrapper: createWrapper() });
    await waitFor(() => expect(result.current.conversationReady).toBe(true));

    act(() => result.current.newConversation());

    await waitFor(() => expect(mockRotate).toHaveBeenCalledWith(PROJECT));
    await waitFor(() => {
      const contents = getMessages(KEY).map((m) => ("content" in m ? m.content : ""));
      expect(contents).not.toContain("old thread talk");
    });
  });

  it("attaches to a teammate's already-running turn on mount", async () => {
    mockGetActive.mockResolvedValue({
      turnId: "t-77",
      conversationId: "conv-1",
      status: "running",
      useCase: "general",
    });
    // Keep the attachment open so the running state is observable.
    mockAttach.mockReturnValue(new Promise(() => {}));

    const { result } = renderHook(() => useAgentChat(ORG, PROJECT), { wrapper: createWrapper() });

    await waitFor(() => expect(result.current.activeTurnId).toBe("t-77"));
    expect(result.current.isSending).toBe(true);
    expect(mockAttach).toHaveBeenCalled();
  });

  // The rotation escape hatch must actually escape: a running turn that
  // belongs to ANOTHER thread (the one a teammate rotated away, or into) is
  // never folded here — it re-resolves instead, and the effect re-run on the
  // fresh id handles it properly.
  it("never attaches a turn from a different thread — re-resolves instead", async () => {
    mockGetActive.mockResolvedValue({
      turnId: "t-old",
      conversationId: "conv-DEMOTED",
      status: "running",
      useCase: "general",
    });

    const { result } = renderHook(() => useAgentChat(ORG, PROJECT), { wrapper: createWrapper() });
    await waitFor(() => expect(result.current.conversationReady).toBe(true));
    await waitFor(() => expect(mockGetActive).toHaveBeenCalled());

    // Give the mount chain a beat: the attach must never fire.
    await waitFor(() => expect(mockFetchCurrent.mock.calls.length).toBeGreaterThan(1));
    expect(mockAttach).not.toHaveBeenCalled();
    expect(result.current.activeTurnId).toBeUndefined();
  });

  // Local-only rows are the ONE copy of a failed send's text — a refocus
  // rehydrate must not wash them out (the review's finding 6).
  it("preserves failed-send rows across a replace", async () => {
    const { result } = renderHook(() => useAgentChat(ORG, PROJECT), { wrapper: createWrapper() });
    await waitFor(() => expect(result.current.conversationReady).toBe(true));

    mockStartTurn.mockRejectedValue(new Error("502 upstream"));
    await act(async () => {
      await result.current.send("do not lose me");
    });
    await waitFor(() =>
      expect(getMessages(KEY).some((m) => m.role === "user" && m.status === "failed")).toBe(true),
    );

    // A later rehydrate replaces with server truth…
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    await waitFor(() => {
      const contents = getMessages(KEY).map((m) => ("content" in m ? m.content : ""));
      expect(contents).toContain("from the server");
      // …and the failed row and its error survive it.
      expect(contents).toContain("do not lose me");
      expect(contents).toContain("502 upstream");
    });
  });
});

// #562 review: the window between "the server has the turn" and "this client
// knows its id". The turn row exists the moment StartTurn returns 202, but
// `startCollabTurn` has not resolved yet — so `attachedRef` is still false and
// the foreign-turn poll would happily find that turn, attach it, and fold it,
// while `send` was about to fold the very same stream. Two concurrent folds,
// and a second user bubble beside the one the user is already looking at,
// because the optimistic row has no turn id to be recognised by yet.
describe("useAgentChat — a local send in flight", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    replaceMessages(KEY, []);
    mockFetchCurrent.mockResolvedValue("conv-1");
    mockGetHistory.mockResolvedValue([]);
    mockGetActive.mockResolvedValue(null);
    mockAttach.mockResolvedValue(undefined);
    // The poll is a timeout; `shouldAdvanceTime` keeps waitFor working while
    // letting the test fire that timeout on demand.
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  // The optimistic row has no turn id and the server has no record of it, so it
  // survives neither the rehydrate's REPLACE nor its `localOnly` filter (error
  // and failed rows only). A tab switch inside the ~2s dispatch would drop the
  // user's own message, and `settleUserMessage` would then no-op onto an id
  // that no longer exists — the message simply gone until the turn ended.
  it("keeps the user's message through a refocus mid-dispatch", async () => {
    let resolveDispatch: (id: string) => void = () => {};
    mockStartTurn.mockImplementation(
      () => new Promise<string>((res) => { resolveDispatch = res; }),
    );

    const { result } = renderHook(() => useAgentChat(ORG, PROJECT), { wrapper: createWrapper() });
    await waitFor(() => expect(result.current.conversationReady).toBe(true));

    act(() => {
      void result.current.send("tidy the requirements");
    });
    await waitFor(() => expect(getMessages(KEY)).toHaveLength(1));

    // The server still has no record of this turn.
    mockGetHistory.mockResolvedValue([]);
    await act(async () => {
      document.dispatchEvent(new Event("visibilitychange"));
      await Promise.resolve();
      await Promise.resolve();
    });

    expect(
      getMessages(KEY).filter((m) => m.role === "user" && m.content === "tidy the requirements"),
    ).toHaveLength(1);

    act(() => resolveDispatch("t1"));
    await waitFor(() => expect(mockAttach).toHaveBeenCalled());
  });

  it("holds the poll off, and never doubles the user's message", async () => {
    // The dispatch hangs; the turn nonetheless exists server-side.
    let resolveDispatch: (id: string) => void = () => {};
    mockStartTurn.mockImplementation(
      () => new Promise<string>((res) => { resolveDispatch = res; }),
    );

    const { result } = renderHook(() => useAgentChat(ORG, PROJECT), { wrapper: createWrapper() });
    await waitFor(() => expect(result.current.conversationReady).toBe(true));

    act(() => {
      void result.current.send("tidy the requirements");
    });

    // The user's row is up immediately, with no turn id yet.
    await waitFor(() => expect(getMessages(KEY)).toHaveLength(1));

    // The poll now reports the very turn this client is still dispatching.
    mockGetActive.mockResolvedValue({
      turnId: "t1",
      conversationId: "conv-1",
      useCase: "general",
      status: "running",
      createdAt: new Date(0).toISOString(),
      updatedAt: new Date(0).toISOString(),
      instruction: "tidy the requirements",
    });
    await act(async () => {
      vi.advanceTimersByTime(13_000);
      await Promise.resolve();
    });

    act(() => resolveDispatch("t1"));
    await waitFor(() => expect(mockAttach).toHaveBeenCalled());

    // One user row, one fold.
    const users = getMessages(KEY).filter((m) => m.role === "user");
    expect(users).toHaveLength(1);
    expect(mockAttach).toHaveBeenCalledTimes(1);
  });
});


// A turn that ENDS while the user sits on one page.
//
// The log has two sources of truth: the SSE fold appends turn rows into
// `chatStore` live, and the `/messages` query is the authority that REPLACES
// them. The query is `staleTime: Infinity` on the grounds that "the thread only
// moves when a turn ends, and every surface that mounts this has a trigger for
// that" — but the commit itself was never one of those triggers. It refreshed
// the project tree and left the thread cache holding a PRE-TURN snapshot, so
// the next surface to mount the log replayed that snapshot over the rows the
// fold had just painted.
//
// It is a new project that makes this certain rather than merely possible: the
// panel is force-opened at creation and mounts BEFORE the platform's kickoff
// dispatches, so the seeded snapshot is an EMPTY thread — and the kickoff
// always ends on questions. The replay then empties the log outright, which is
// what took the spec workspace back to "Nothing written yet" plus a Retry with
// the agent standing waiting on answers.
describe("useAgentChat — a committed turn refreshes the thread cache", () => {
  const QUESTION = {
    question: "Who raises tickets?",
    options: [{ label: "Staff" }],
  };
  // What the server serves ONCE the kickoff turn is persisted.
  const POST_TURN_HISTORY = [
    { role: "user", content: "/start a helpdesk" },
    {
      role: "assistant",
      content: [
        {
          type: "tool-call",
          toolCallId: "tc-1",
          toolName: "ask_questions",
          input: { questions: [QUESTION] },
        },
      ],
    },
  ];

  beforeEach(() => {
    vi.clearAllMocks();
    replaceMessages(KEY, []);
    mockFetchCurrent.mockResolvedValue("conv-1");
    // The new-project ordering, and the precondition for the whole bug: the
    // thread is server-minted at creation, and the panel's mount read lands
    // BEFORE the platform's kickoff has produced anything. The snapshot the
    // cache is seeded with is therefore the empty thread.
    mockGetHistory.mockResolvedValue([]);
    mockGetActive.mockResolvedValue({
      turnId: "t-kickoff",
      conversationId: "conv-1",
      status: "running",
      useCase: "general",
    });
    mockAttach.mockImplementation(
      async (chatKey: string, _p: string, turnId: string, _s: unknown, onCommitted: () => void) => {
        // The fold paints the question card live. The pointer in the panel
        // exists because of THIS, never the query — which is what lets the two
        // disagree in the first place.
        upsertQuestionMessage(chatKey, {
          role: "question",
          turnId,
          toolCallId: "tc-1",
          questions: [QUESTION],
          streaming: false,
        });
        // The turn is over: the poll must not find it again and rehydrate on
        // its own, which would mask exactly the staleness under test.
        mockGetActive.mockResolvedValue(null);
        // The turn is persisted before the commit frame is emitted, so anyone
        // who asks from here on is served it.
        mockGetHistory.mockResolvedValue(POST_TURN_HISTORY);
        onCommitted();
      },
    );
  });

  it("keeps a question the fold painted when a later surface mounts the log", async () => {
    const Wrapper = createWrapper(); // ONE QueryClient — both surfaces share it
    const panel = renderHook(() => useAgentChat(ORG, PROJECT), { wrapper: Wrapper });
    await waitFor(() => expect(panel.result.current.conversationReady).toBe(true));
    await waitFor(() =>
      expect(getMessages(KEY).some((m) => m.role === "question")).toBe(true),
    );

    // The commit re-reads the thread — BEFORE any navigation. That is the fix:
    // the cache holds post-turn truth while the user is still looking at the
    // pointer, so nothing stale is left for a later surface to replay.
    await waitFor(
      () =>
        expect(
          mockGetHistory.mock.calls.length,
          "the committed turn must re-read the thread",
        ).toBeGreaterThan(1),
      { timeout: 3000 },
    );

    // The click: the spec workspace mounts the log on the same client. It is
    // served fresh data, so the replace lands the question instead of wiping
    // it — and no stale frame paints "Nothing written yet" over the question
    // the agent is standing there waiting on.
    renderHook(() => useConversationLog(ORG, PROJECT), { wrapper: Wrapper });
    await waitFor(() =>
      expect(getMessages(KEY).some((m) => m.role === "question")).toBe(true),
    );
  });
});
