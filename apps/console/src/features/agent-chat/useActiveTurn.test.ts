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

// Agent activity from the pod: one query on GET /v1/projects/{p}/turns/active
// serves the chat panel's foreign-turn watch and the spec/overview readers.

import { createElement, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { TurnStatus } from "./api/turns";
import { projectKeys } from "../projects/api/keys";

const mockGetActiveTurn = vi.fn<(project: string) => Promise<TurnStatus | null>>();
vi.mock("./api/turns", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./api/turns")>()),
  getActiveTurn: (project: string) => mockGetActiveTurn(project),
}));

let podReady = true;
vi.mock("../ae-studio/api/queries", () => ({
  usePodQueryOptions: () => ({ enabled: podReady, retryDelay: () => 0 }),
}));

const { activeTurnPollDelay, useActiveTurn } = await import("./api/useActiveTurn");

const running = (over: Partial<TurnStatus> = {}): TurnStatus => ({
  turnId: "t-1",
  project: "p",
  conversationId: "c-1",
  kind: "browser",
  flow: "design",
  status: "running",
  instruction: "/design",
  authorId: "sub-1",
  authorDisplayName: "Ada",
  createdAt: new Date(0).toISOString(),
  ...over,
});

function setup() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: queryClient }, children);
  return { queryClient, wrapper };
}

describe("activeTurnPollDelay", () => {
  it("polls every 5 s while a turn runs, else every 12 s", () => {
    expect(activeTurnPollDelay(running())).toBe(5_000);
    expect(activeTurnPollDelay(null)).toBe(12_000);
    expect(activeTurnPollDelay(undefined)).toBe(12_000);
  });
});

describe("useActiveTurn", () => {
  beforeEach(() => {
    mockGetActiveTurn.mockReset();
    podReady = true;
  });

  // Unmount while the fake clock is still installed, so the query intervals
  // are cleared on it rather than surviving into the next test's clock.
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("answers the project's running turn, and null when none runs", async () => {
    mockGetActiveTurn.mockResolvedValueOnce(running());
    const { wrapper } = setup();
    const { result, rerender } = renderHook(({ p }) => useActiveTurn(p), { wrapper, initialProps: { p: "p" } });
    await waitFor(() => expect(result.current.data).toMatchObject({ turnId: "t-1", flow: "design" }));
    expect(mockGetActiveTurn).toHaveBeenCalledWith("p");

    mockGetActiveTurn.mockResolvedValueOnce(null);
    rerender({ p: "q" });
    await waitFor(() => expect(result.current.data).toBeNull());
  });

  it("asks nothing while AE Studio is not ready, or without a project", async () => {
    podReady = false;
    const { wrapper } = setup();
    renderHook(() => useActiveTurn("p"), { wrapper });
    podReady = true;
    renderHook(() => useActiveTurn(""), { wrapper });
    await new Promise((r) => setTimeout(r, 20));
    expect(mockGetActiveTurn).not.toHaveBeenCalled();
  });

  it("polls at 5 s while a turn runs and at 12 s once none does", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockGetActiveTurn.mockResolvedValue(running());
    const { wrapper } = setup();
    const { result } = renderHook(() => useActiveTurn("p"), { wrapper });
    await waitFor(() => expect(result.current.data).not.toBeUndefined());
    expect(mockGetActiveTurn).toHaveBeenCalledTimes(1);

    mockGetActiveTurn.mockResolvedValue(null);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(mockGetActiveTurn).toHaveBeenCalledTimes(2);
    await waitFor(() => expect(result.current.data).toBeNull());

    await act(async () => {
      await vi.advanceTimersByTimeAsync(6_000);
    });
    expect(mockGetActiveTurn).toHaveBeenCalledTimes(2); // idle: not yet
    await act(async () => {
      await vi.advanceTimersByTimeAsync(6_000);
    });
    expect(mockGetActiveTurn).toHaveBeenCalledTimes(3);
  });

  it("takes an observer's own cadence (the chat panel's fast first polls)", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockGetActiveTurn.mockResolvedValue(null);
    const { wrapper } = setup();
    const { result } = renderHook(() => useActiveTurn("p", { pollDelay: () => 2_000 }), { wrapper });
    await waitFor(() => expect(result.current.data).toBeNull());

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_000);
    });
    expect(mockGetActiveTurn).toHaveBeenCalledTimes(2);
  });

  it("keeps the last answer through a failed read", async () => {
    mockGetActiveTurn.mockResolvedValueOnce(running()).mockRejectedValue(new Error("pod restarting"));
    const { wrapper } = setup();
    // Both fields are read during render: react-query re-renders only for
    // the result fields a component reads.
    const { result } = renderHook(
      () => {
        const { data, isError } = useActiveTurn("p", { pollDelay: () => 20 });
        return { data, isError };
      },
      { wrapper },
    );
    await waitFor(() => expect(result.current.data).toMatchObject({ turnId: "t-1" }));

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.data).toMatchObject({ turnId: "t-1" });
  });

  // The git-derived spec facts move when a turn ends; the status poll may be on
  // its idle cadence, so the turn's end refreshes it.
  it("refreshes the project status when the running turn ends", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockGetActiveTurn.mockResolvedValueOnce(running());
    const { queryClient, wrapper } = setup();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useActiveTurn("p"), { wrapper });
    await waitFor(() => expect(result.current.data).toMatchObject({ turnId: "t-1" }));
    expect(invalidate).not.toHaveBeenCalled();

    mockGetActiveTurn.mockResolvedValueOnce(null);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    await waitFor(() => expect(result.current.data).toBeNull());
    expect(invalidate).toHaveBeenCalledWith(
      expect.objectContaining({ queryKey: projectKeys.status("p") }),
      expect.anything(),
    );
  });
});
