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

// The spec view's log bootstrap (#485). What matters: the question form's data
// path does not depend on the chat panel — the log is seeded from the server
// thread and KEPT current while no panel owns it, so a question the agent asks
// while the rail is closed still reaches the room's shared form. A panel that
// attaches takes the log back, and its live fold is never overwritten.

import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  addMessage,
  chatKeyFor,
  getMessages,
  registerChatLogOwner,
  replaceMessages,
} from "./chatStore";
import { useChatLogBootstrap } from "./useChatLogBootstrap";

const ORG = "acme";
const PROJECT = "boot1";
const KEY = chatKeyFor(ORG, PROJECT);

const mockGetHistory = vi.fn();
vi.mock("./api/turns", async (importOriginal) => {
  const real = await importOriginal<typeof import("./api/turns")>();
  return {
    ...real,
    getConversationMessages: (...a: unknown[]) => mockGetHistory(...a),
  };
});

const mockFetchCurrent = vi.fn();
vi.mock("./api/conversations", async (importOriginal) => {
  const real = await importOriginal<typeof import("./api/conversations")>();
  return {
    ...real,
    fetchCurrentConversationId: (...a: unknown[]) => mockFetchCurrent(...a),
  };
});

function wrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
}

const askQuestionsCall = {
  role: "assistant",
  content: [
    {
      type: "tool-call",
      toolName: "ask_questions",
      toolCallId: "tc-1",
      input: {
        questions: [{ question: "Open browsing?", options: [{ label: "Yes" }] }],
      },
    },
  ],
};

const hasQuestion = () => getMessages(KEY).some((m) => m.role === "question");

let releaseOwner: (() => void) | null = null;

describe("useChatLogBootstrap", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    replaceMessages(KEY, []);
    mockFetchCurrent.mockResolvedValue("conv-1");
    mockGetHistory.mockResolvedValue([]);
  });

  afterEach(() => {
    releaseOwner?.();
    releaseOwner = null;
    vi.useRealTimers();
  });

  it("seeds an empty log from the server thread — pending questions included", async () => {
    mockGetHistory.mockResolvedValue([askQuestionsCall]);

    renderHook(() => useChatLogBootstrap(ORG, PROJECT), { wrapper: wrapper() });

    await waitFor(() => expect(hasQuestion()).toBe(true));
    expect(mockGetHistory).toHaveBeenCalledWith(PROJECT, "conv-1");
  });

  // The defect this poll exists for: the user opened the chat once (so the log
  // is no longer empty), then closed the rail — unmounting the only writer.
  // The agent then asked its questions. Nothing brought them in, so the shared
  // form never appeared and the spec view sat on its working state.
  it("keeps the log current while no panel owns it — a question asked with the rail closed still lands", async () => {
    vi.useFakeTimers();
    addMessage(KEY, { role: "user", content: "/start", status: "completed" });
    mockGetHistory.mockResolvedValue([{ role: "user", content: "/start" }]);

    renderHook(() => useChatLogBootstrap(ORG, PROJECT), { wrapper: wrapper() });
    await vi.waitFor(() => expect(mockGetHistory).toHaveBeenCalledTimes(1));
    expect(hasQuestion()).toBe(false);

    // The agent's turn ends on ask_questions while the rail is closed.
    mockGetHistory.mockResolvedValue([{ role: "user", content: "/start" }, askQuestionsCall]);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(12_000);
    });

    expect(hasQuestion()).toBe(true);
  });

  it("stands down while a chat panel owns the log", async () => {
    releaseOwner = registerChatLogOwner(KEY);
    addMessage(KEY, { role: "assistant", turnId: "t1", content: "streaming…" });

    renderHook(() => useChatLogBootstrap(ORG, PROJECT), { wrapper: wrapper() });

    await waitFor(() => expect(mockFetchCurrent).toHaveBeenCalled());
    expect(mockGetHistory).not.toHaveBeenCalled();
    expect(getMessages(KEY)).toHaveLength(1);
  });

  it("never clobbers a log a panel took over while the fetch was in flight", async () => {
    let resolveHistory!: (v: unknown) => void;
    mockGetHistory.mockReturnValue(new Promise((r) => (resolveHistory = r)));

    renderHook(() => useChatLogBootstrap(ORG, PROJECT), { wrapper: wrapper() });
    await waitFor(() => expect(mockGetHistory).toHaveBeenCalled());

    // The chat panel attached and started folding a live turn meanwhile.
    releaseOwner = registerChatLogOwner(KEY);
    addMessage(KEY, { role: "assistant", turnId: "t1", content: "streaming…" });
    resolveHistory([askQuestionsCall]);

    await new Promise((r) => setTimeout(r, 0));
    expect(getMessages(KEY)).toHaveLength(1);
    expect(getMessages(KEY)[0]!.role).toBe("assistant");
  });

  // A failed send exists only here — the thread never saw it, so a refresh
  // from the thread must not be what destroys the text the user needs to retry.
  it("keeps local-only rows across a refresh", async () => {
    addMessage(KEY, { role: "user", content: "retry me", status: "failed" });
    addMessage(KEY, { role: "error", content: "Failed to reach the agent." });
    mockGetHistory.mockResolvedValue([askQuestionsCall]);

    renderHook(() => useChatLogBootstrap(ORG, PROJECT), { wrapper: wrapper() });

    await waitFor(() => expect(hasQuestion()).toBe(true));
    const messages = getMessages(KEY);
    expect(messages.some((m) => m.role === "user" && m.status === "failed")).toBe(true);
    expect(messages.some((m) => m.role === "error")).toBe(true);
  });
});
