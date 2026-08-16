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

// The spec view's log bootstrap (#485 live-testing round). What matters: the
// question form's data path no longer depends on the chat panel — an empty
// local log is seeded from the server thread, and a log that already exists
// (or fills while the fetch is in flight) is never clobbered.

import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { addMessage, chatKeyFor, getMessages, replaceMessages } from "./chatStore";
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

describe("useChatLogBootstrap", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    replaceMessages(KEY, []);
    mockFetchCurrent.mockResolvedValue("conv-1");
    mockGetHistory.mockResolvedValue([]);
  });

  it("seeds an empty log from the server thread — pending questions included", async () => {
    mockGetHistory.mockResolvedValue([askQuestionsCall]);

    renderHook(() => useChatLogBootstrap(ORG, PROJECT), { wrapper: wrapper() });

    await waitFor(() =>
      expect(getMessages(KEY).some((m) => m.role === "question")).toBe(true),
    );
    expect(mockGetHistory).toHaveBeenCalledWith(PROJECT, "conv-1");
  });

  it("does nothing when a log already exists — the live/cached log wins", async () => {
    addMessage(KEY, { role: "user", content: "/start", status: "completed" });

    renderHook(() => useChatLogBootstrap(ORG, PROJECT), { wrapper: wrapper() });

    // No thread resolve, no history fetch, log untouched.
    await new Promise((r) => setTimeout(r, 0));
    expect(mockFetchCurrent).not.toHaveBeenCalled();
    expect(mockGetHistory).not.toHaveBeenCalled();
    expect(getMessages(KEY)).toHaveLength(1);
  });

  it("never clobbers a log that filled while the fetch was in flight", async () => {
    let resolveHistory!: (v: unknown) => void;
    mockGetHistory.mockReturnValue(new Promise((r) => (resolveHistory = r)));

    renderHook(() => useChatLogBootstrap(ORG, PROJECT), { wrapper: wrapper() });
    await waitFor(() => expect(mockGetHistory).toHaveBeenCalled());

    // The chat panel attached and started folding a live turn meanwhile.
    addMessage(KEY, { role: "assistant", turnId: "t1", content: "streaming…" });
    resolveHistory([askQuestionsCall]);

    await new Promise((r) => setTimeout(r, 0));
    expect(getMessages(KEY)).toHaveLength(1);
    expect(getMessages(KEY)[0]!.role).toBe("assistant");
  });
});
