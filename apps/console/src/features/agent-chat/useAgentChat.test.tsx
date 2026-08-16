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
import { beforeEach, describe, expect, it, vi } from "vitest";
import { addMessage, chatKeyFor, getMessages, replaceMessages } from "./chatStore";
import { useAgentChat } from "./useAgentChat";

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
    act(() => result.current.send("hello"));

    await waitFor(() => expect(mockStartTurn).toHaveBeenCalledWith(PROJECT, "conv-1", "hello"));
  });

  it("holds sends until the thread id resolves", () => {
    mockFetchCurrent.mockReturnValue(new Promise(() => {})); // never resolves
    const { result } = renderHook(() => useAgentChat(ORG, PROJECT), { wrapper: createWrapper() });

    expect(result.current.conversationReady).toBe(false);
    act(() => result.current.send("too early"));
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
    act(() => result.current.send("into the demoted thread"));

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
    act(() => result.current.send("do not lose me"));
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
