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

// The overview's server-sourced interview state (#485). What matters: it works
// on a FRESH LANDING (no chat log in this browser) — running from the active
// poll, waiting questions from a thread rehydrate — and it defers to the live
// chat log the moment one exists.

import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  addMessage,
  chatKeyFor,
  replaceMessages,
  setLocalTurnActive,
} from "./chatStore";
import { useSpecInterview } from "./useSpecInterview";

const ORG = "acme";
const PROJECT = "fresh1";
const KEY = chatKeyFor(ORG, PROJECT);

const mockGetActive = vi.fn();
const mockGetHistory = vi.fn();
vi.mock("./api/turns", async (importOriginal) => {
  const real = await importOriginal<typeof import("./api/turns")>();
  return {
    ...real,
    getActiveTurn: (...a: unknown[]) => mockGetActive(...a),
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
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
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
        questions: [
          { question: "Open browsing?", options: [{ label: "Yes" }] },
          { question: "Photo uploads?", options: [{ label: "Yes" }] },
        ],
      },
    },
  ],
};

describe("useSpecInterview", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    replaceMessages(KEY, []);
    setLocalTurnActive(KEY, false);
    mockGetActive.mockResolvedValue(null);
    mockFetchCurrent.mockResolvedValue("conv-1");
    mockGetHistory.mockResolvedValue([]);
  });

  it("reports running from the active-turn poll on a fresh landing", async () => {
    mockGetActive.mockResolvedValue({ turnId: "t1", status: "running" });

    const { result } = renderHook(() => useSpecInterview(ORG, PROJECT, true), {
      wrapper: wrapper(),
    });
    await waitFor(() => expect(result.current.running).toBe(true));
    // A running turn cannot be awaiting answers, whatever the thread says.
    expect(result.current.questionsWaiting).toBe(0);
    // No questions asked yet — the turn is still preparing them, not drafting.
    expect(result.current.drafting).toBe(false);
  });

  // The drafting stage (live-testing round): questions asked AND answered in
  // the thread while a turn runs means the agent is past the interview and
  // writing the document — the stage lines say "drafting", not "preparing".
  it("reports drafting when the running turn follows an answered interview", async () => {
    mockGetActive.mockResolvedValue({ turnId: "t2", status: "running" });
    mockGetHistory.mockResolvedValue([
      askQuestionsCall,
      { role: "user", content: "Answers: yes to both" },
    ]);

    const { result } = renderHook(() => useSpecInterview(ORG, PROJECT, true), {
      wrapper: wrapper(),
    });
    await waitFor(() => expect(result.current.drafting).toBe(true));
    expect(result.current.questionsWaiting).toBe(0);
  });

  it("derives drafting from the live log once one exists", async () => {
    mockGetActive.mockResolvedValue({ turnId: "t2", status: "running" });
    addMessage(KEY, {
      role: "question",
      turnId: "t1",
      toolCallId: "tc-1",
      questions: [{ question: "Open browsing?", options: [{ label: "Yes" }] }],
    });
    addMessage(KEY, { role: "user", content: "Yes", turnId: "t2", status: "completed" });

    const { result } = renderHook(() => useSpecInterview(ORG, PROJECT, true), {
      wrapper: wrapper(),
    });
    await waitFor(() => expect(result.current.drafting).toBe(true));
    expect(mockGetHistory).not.toHaveBeenCalled();
  });

  it("never reports drafting while questions are still waiting", async () => {
    mockGetActive.mockResolvedValue({ turnId: "t1", status: "running" });
    addMessage(KEY, {
      role: "question",
      turnId: "t1",
      toolCallId: "tc-1",
      questions: [{ question: "Open browsing?", options: [{ label: "Yes" }] }],
    });

    const { result } = renderHook(() => useSpecInterview(ORG, PROJECT, true), {
      wrapper: wrapper(),
    });
    await waitFor(() => expect(result.current.running).toBe(true));
    expect(result.current.questionsWaiting).toBe(1);
    expect(result.current.drafting).toBe(false);
  });

  it("counts waiting questions from the server thread when no local log exists", async () => {
    mockGetHistory.mockResolvedValue([askQuestionsCall]);

    const { result } = renderHook(() => useSpecInterview(ORG, PROJECT, true), {
      wrapper: wrapper(),
    });
    await waitFor(() => expect(result.current.questionsWaiting).toBe(2));
    expect(result.current.running).toBe(false);
    expect(mockGetHistory).toHaveBeenCalledWith(PROJECT, "conv-1");
  });

  it("prefers the live chat log once one exists", async () => {
    // Server says two questions; the local log knows they were answered.
    mockGetHistory.mockResolvedValue([askQuestionsCall]);
    addMessage(KEY, {
      role: "question",
      turnId: "t1",
      toolCallId: "tc-1",
      questions: [{ question: "Open browsing?", options: [{ label: "Yes" }] }],
      answers: [{ selected: ["Yes"] }],
    });

    const { result } = renderHook(() => useSpecInterview(ORG, PROJECT, true), {
      wrapper: wrapper(),
    });
    expect(result.current.questionsWaiting).toBe(0);
    // The log supersedes the rehydrate entirely — no server round-trip.
    expect(mockGetHistory).not.toHaveBeenCalled();
  });

  it("counts unanswered questions from the live log", () => {
    addMessage(KEY, {
      role: "question",
      turnId: "t1",
      toolCallId: "tc-1",
      questions: [{ question: "Open browsing?", options: [{ label: "Yes" }] }],
    });

    const { result } = renderHook(() => useSpecInterview(ORG, PROJECT, true), {
      wrapper: wrapper(),
    });
    expect(result.current.questionsWaiting).toBe(1);
  });

  // Live-testing round 2: submitting interview answers starts a turn this tab
  // knows about immediately, while the active-turn poll below would leave
  // every surface on its pre-turn state for up to a full interval — long
  // enough that the spec view showed "Select a file to view its content."
  // instead of the drafting line for the whole window.
  it("reports running from a turn this tab started, without waiting for the poll", () => {
    const { result } = renderHook(() => useSpecInterview(ORG, PROJECT, true), {
      wrapper: wrapper(),
    });
    expect(result.current.running).toBe(false);

    act(() => setLocalTurnActive(KEY, true));

    expect(result.current.running).toBe(true);
  });

  it("reports drafting the moment the answered interview's turn starts here", () => {
    addMessage(KEY, {
      role: "question",
      turnId: "t1",
      toolCallId: "tc-1",
      questions: [{ question: "Open browsing?", options: [{ label: "Yes" }] }],
    });
    addMessage(KEY, {
      role: "user",
      content: 'Answers:\n- "Open browsing?": Yes',
      turnId: "t2",
      status: "in_flight",
    });

    const { result } = renderHook(() => useSpecInterview(ORG, PROJECT, true), {
      wrapper: wrapper(),
    });
    act(() => setLocalTurnActive(KEY, true));

    expect(result.current.drafting).toBe(true);
  });

  it("stops reporting running when the local turn settles", () => {
    setLocalTurnActive(KEY, true);
    const { result } = renderHook(() => useSpecInterview(ORG, PROJECT, true), {
      wrapper: wrapper(),
    });
    expect(result.current.running).toBe(true);

    act(() => setLocalTurnActive(KEY, false));

    expect(result.current.running).toBe(false);
  });

  // `started` is the DURABLE first-run signal (#485 live-testing round 3): the
  // overview's CTA reads it so it cannot flip back to "Generate spec" — and
  // re-offer to inject a second /start — in the gaps where `running` dips.
  describe("started — the thread has been spoken in", () => {
    it("is false for a project whose thread is empty", async () => {
      const { result } = renderHook(() => useSpecInterview(ORG, PROJECT, true), {
        wrapper: wrapper(),
      });
      await waitFor(() => expect(mockGetHistory).toHaveBeenCalled());
      expect(result.current.started).toBe(false);
    });

    it("stays true between turns — a thread with history, nothing running", async () => {
      mockGetHistory.mockResolvedValue([
        { role: "user", content: "/start" },
        askQuestionsCall,
        { role: "user", content: "Answers: yes to both" },
      ]);

      const { result } = renderHook(() => useSpecInterview(ORG, PROJECT, true), {
        wrapper: wrapper(),
      });
      await waitFor(() => expect(result.current.started).toBe(true));
      expect(result.current.running).toBe(false);
      expect(result.current.questionsWaiting).toBe(0);
    });

    it("is true as soon as the local log has anything", () => {
      addMessage(KEY, { role: "user", content: "/start", status: "completed" });

      const { result } = renderHook(() => useSpecInterview(ORG, PROJECT, true), {
        wrapper: wrapper(),
      });
      expect(result.current.started).toBe(true);
    });
  });

  it("is inert when disabled — no requests, idle state", () => {
    setLocalTurnActive(KEY, true);
    const { result } = renderHook(() => useSpecInterview(ORG, PROJECT, false), {
      wrapper: wrapper(),
    });
    expect(result.current).toEqual({
      running: false,
      questionsWaiting: 0,
      drafting: false,
      started: false,
    });
    expect(mockGetActive).not.toHaveBeenCalled();
    expect(mockFetchCurrent).not.toHaveBeenCalled();
  });
});
