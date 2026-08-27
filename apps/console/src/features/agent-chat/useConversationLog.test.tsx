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

// Filling this browser's chat log WITHOUT the chat panel (#606).
//
// The behaviors here are the ones that made a project awaiting the agent's
// questions look dead: the log was filled only by `AgentChatPanel`, so a
// member arriving on a spec link cold had none, and every surface reading one
// — the spec workspace's question form, the overview's spec card — read an
// empty conversation and said nothing had started.

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  addMessage,
  chatKeyFor,
  claimSendInFlight,
  claimStreamFold,
  getMessages,
  replaceMessages,
} from "./chatStore";
import { useConversationLog } from "./useConversationLog";
import { answerableQuestionIds } from "./questionCards";

const ORG = "acme";
const PROJECT = "proj1";
const KEY = chatKeyFor(ORG, PROJECT);

const mockFetchCurrent = vi.fn();
vi.mock("./api/conversations", async (importOriginal) => {
  const real = await importOriginal<typeof import("./api/conversations")>();
  return { ...real, fetchCurrentConversationId: (...a: unknown[]) => mockFetchCurrent(...a) };
});

const mockGetHistory = vi.fn();
vi.mock("./api/turns", async (importOriginal) => {
  const real = await importOriginal<typeof import("./api/turns")>();
  return { ...real, getConversationMessages: (...a: unknown[]) => mockGetHistory(...a) };
});

function createWrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

/** A thread that ended ON a question — the state the whole issue is about. */
const AWAITING_ANSWERS = [
  { role: "user", content: "/start a helpdesk" },
  {
    role: "assistant",
    content: [
      {
        type: "tool-call",
        toolCallId: "tc-1",
        toolName: "ask_questions",
        input: { questions: [{ question: "Who raises tickets?", options: [{ label: "Staff" }] }] },
      },
    ],
  },
];

beforeEach(() => {
  vi.clearAllMocks();
  replaceMessages(KEY, []);
  mockFetchCurrent.mockResolvedValue("conv-1");
  mockGetHistory.mockResolvedValue(AWAITING_ANSWERS);
});

describe("useConversationLog — a log without the chat panel (#606)", () => {
  it("fills an empty log with the pending question, so the spec view can see it", async () => {
    // The bug, stated as a test: no chat panel has ever mounted for this
    // project, so the log is empty and `useRoomQuestion` has nothing to mirror
    // into the room. Mounting this hook is the whole fix.
    expect(getMessages(KEY)).toHaveLength(0);

    renderHook(() => useConversationLog(ORG, PROJECT), { wrapper: createWrapper() });

    await waitFor(() => {
      const question = getMessages(KEY).find((m) => m.role === "question");
      expect(question).toBeDefined();
    });
    // And it reads as STILL WAITING — which is what turns the empty state into
    // the question form rather than "Nothing written yet" plus a Retry.
    expect(answerableQuestionIds(getMessages(KEY)).size).toBe(1);
  });

  it("reports historyReady only once the server has been asked", async () => {
    const { result } = renderHook(() => useConversationLog(ORG, PROJECT), {
      wrapper: createWrapper(),
    });
    expect(result.current.historyReady).toBe(false);
    await waitFor(() => expect(result.current.historyReady).toBe(true));
  });

  it("reports historyReady even when the read FAILED", async () => {
    // It says "we have asked", not "we know". A guarded seed held forever on a
    // history the server will not serve would strand the only recovery a
    // stalled project has.
    mockGetHistory.mockResolvedValue(null);
    const { result } = renderHook(() => useConversationLog(ORG, PROJECT), {
      wrapper: createWrapper(),
    });
    await waitFor(() => expect(result.current.historyReady).toBe(true));
  });

  it("keeps painting the local log when the read fails", async () => {
    mockGetHistory.mockResolvedValue(null);
    addMessage(KEY, { role: "user", content: "typed locally", status: "completed" });

    const { result } = renderHook(() => useConversationLog(ORG, PROJECT), {
      wrapper: createWrapper(),
    });

    await waitFor(() => expect(result.current.historyReady).toBe(true));
    expect(getMessages(KEY).some((m) => "content" in m && m.content === "typed locally")).toBe(true);
  });

  it("REPLACES a stale local log with server truth", async () => {
    addMessage(KEY, { role: "user", content: "stale local fork", status: "completed" });

    renderHook(() => useConversationLog(ORG, PROJECT), { wrapper: createWrapper() });

    await waitFor(() => {
      const contents = getMessages(KEY).map((m) => ("content" in m ? m.content : ""));
      expect(contents).toContain("/start a helpdesk");
      expect(contents).not.toContain("stale local fork");
    });
  });

  it("keeps a failed send and its error row through the replace", async () => {
    // They exist nowhere server-side; washing them out destroys the one copy of
    // a message the user still needs to retry.
    addMessage(KEY, { role: "user", content: "never reached the server", status: "failed" });
    addMessage(KEY, { role: "error", content: "Lost the agent's stream" });

    renderHook(() => useConversationLog(ORG, PROJECT), { wrapper: createWrapper() });

    await waitFor(() => {
      const contents = getMessages(KEY).map((m) => ("content" in m ? m.content : ""));
      expect(contents).toContain("/start a helpdesk");
      expect(contents).toContain("never reached the server");
      expect(contents).toContain("Lost the agent's stream");
    });
  });

  it("does not replace the log while a turn stream is being folded", async () => {
    // The guard that makes a second writer safe: a replace mid-fold would wash
    // out streamed partials — including the streaming question prefix the room
    // is mirroring.
    const release = claimStreamFold(KEY);
    addMessage(KEY, { role: "assistant", content: "…streaming so far", turnId: "t1" });

    const { result } = renderHook(() => useConversationLog(ORG, PROJECT), {
      wrapper: createWrapper(),
    });

    await waitFor(() => expect(result.current.historyReady).toBe(true));
    const contents = getMessages(KEY).map((m) => ("content" in m ? m.content : ""));
    expect(contents).toContain("…streaming so far");
    expect(contents).not.toContain("/start a helpdesk");
    release();
  });

  it("does not replace the log while a local send is mid-dispatch", async () => {
    // The optimistic row has no turn id and the server has no record of it, so
    // it survives neither the replace nor the local-only filter.
    const release = claimSendInFlight(KEY);
    addMessage(KEY, { role: "user", content: "just typed", status: "in_flight" });

    const { result } = renderHook(() => useConversationLog(ORG, PROJECT), {
      wrapper: createWrapper(),
    });

    await waitFor(() => expect(result.current.historyReady).toBe(true));
    expect(getMessages(KEY).some((m) => "content" in m && m.content === "just typed")).toBe(true);
    release();
  });

  it("re-reads the thread on resync — the caller's own turn-end signal", async () => {
    const { result } = renderHook(() => useConversationLog(ORG, PROJECT), {
      wrapper: createWrapper(),
    });
    await waitFor(() => expect(mockGetHistory).toHaveBeenCalledTimes(1));

    // The agent has since answered its own questions and written the document.
    mockGetHistory.mockResolvedValue([
      ...AWAITING_ANSWERS,
      { role: "user", content: "Use your recommended answers" },
      { role: "assistant", content: "Written." },
    ]);
    await act(async () => {
      result.current.resync();
    });

    await waitFor(() => {
      // The question is no longer answerable, so the spec view stops offering a
      // form over a document that is already written.
      expect(answerableQuestionIds(getMessages(KEY)).size).toBe(0);
    });
  });

  it("supersedes a question the server shows answered, even when the local send says failed", async () => {
    // The mid-turn navigation case: the browser lost the stream and recorded
    // the send as failed, but the server did receive it and the turn ran.
    // `answerableQuestionIds` ignores failed sends on purpose, so only server
    // truth can close this question — which is why the log must be able to
    // reach it without the chat panel.
    addMessage(KEY, { role: "user", content: "Use your recommended answers", status: "failed" });
    addMessage(KEY, { role: "error", content: "Lost the agent's stream" });
    mockGetHistory.mockResolvedValue([
      ...AWAITING_ANSWERS,
      { role: "user", content: "Use your recommended answers" },
      { role: "assistant", content: "Written." },
    ]);

    renderHook(() => useConversationLog(ORG, PROJECT), { wrapper: createWrapper() });

    await waitFor(() => expect(getMessages(KEY).some((m) => m.role === "question")).toBe(true));
    expect(answerableQuestionIds(getMessages(KEY)).size).toBe(0);
  });

  it("asks for nothing without a project", async () => {
    renderHook(() => useConversationLog(ORG, undefined), { wrapper: createWrapper() });
    await new Promise((r) => setTimeout(r, 0));
    expect(mockFetchCurrent).not.toHaveBeenCalled();
    expect(mockGetHistory).not.toHaveBeenCalled();
  });
});
