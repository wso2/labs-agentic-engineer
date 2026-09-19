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

import { renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { useTurnEndFlush } from "./useTurnEndFlush";
import { flushRoomBeforeDispatch, notifyTurnEnd } from "../../agent-chat/chatStore";
import { specKeys } from "../api/keys";
import { projectKeys } from "../../projects/api/keys";
import { FRESHNESS_POLL_DELAY_MS } from "../api/dependencyFreshness";

const KEY = "aep.chat.v1.acme.proj1";

function wrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

describe("useTurnEndFlush — deterministic room-flush closure (#252 Task 5)", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("connected: flushes the room then invalidates immediately, no follow-up poll", async () => {
    const queryClient = new QueryClient();
    const spy = vi.spyOn(queryClient, "invalidateQueries");
    const flush = vi.fn().mockResolvedValue(undefined);
    renderHook(
      () => useTurnEndFlush(KEY, "proj1", { status: "connected", flush }),
      { wrapper: wrapper(queryClient) },
    );

    notifyTurnEnd(KEY, "completed");
    expect(flush).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(0); // let the flush() microtask resolve
    expect(spy).toHaveBeenCalledWith({ queryKey: specKeys.dependencies("proj1") });
    expect(spy).toHaveBeenCalledWith({ queryKey: projectKeys.buildPreflight("proj1") });
    const callsRightAfterFlush = spy.mock.calls.length;

    // No follow-up poll needed on the happy path.
    vi.advanceTimersByTime(FRESHNESS_POLL_DELAY_MS);
    expect(spy.mock.calls.length).toBe(callsRightAfterFlush);
  });

  // The room lives here and the sender does not (the chat panel is a sibling
  // subtree), so this registration is the only way a turn about to be
  // dispatched can land the room first. Registering the CLAIM without the
  // flush would leave that path silently inert (#575 follow-up).
  it("registers the flush the pre-dispatch path calls", async () => {
    const queryClient = new QueryClient();
    const flush = vi.fn().mockResolvedValue(undefined);
    const { unmount } = renderHook(
      () => useTurnEndFlush(KEY, "proj1", { status: "connected", flush }),
      { wrapper: wrapper(queryClient) },
    );

    await flushRoomBeforeDispatch(KEY);
    expect(flush).toHaveBeenCalledTimes(1);

    // And it goes with the view: a send from a route with no room waits for
    // nothing.
    unmount();
    await flushRoomBeforeDispatch(KEY);
    expect(flush).toHaveBeenCalledTimes(1);
  });

  it("flush rejects: falls back to refetch-on-turn-done + a short poll", async () => {
    const queryClient = new QueryClient();
    const spy = vi.spyOn(queryClient, "invalidateQueries");
    const flush = vi.fn().mockRejectedValue(new Error("Timed out"));
    renderHook(
      () => useTurnEndFlush(KEY, "proj1", { status: "connected", flush }),
      { wrapper: wrapper(queryClient) },
    );

    notifyTurnEnd(KEY, "completed");
    await vi.advanceTimersByTimeAsync(0); // let the flush() rejection settle
    expect(spy).toHaveBeenCalled();
    const callsAfterFallbackStarts = spy.mock.calls.length;

    vi.advanceTimersByTime(FRESHNESS_POLL_DELAY_MS);
    expect(spy.mock.calls.length).toBeGreaterThan(callsAfterFallbackStarts);
  });

  it("room not connected (e.g. offline/solo): skips flush, falls back to poll", () => {
    const queryClient = new QueryClient();
    const spy = vi.spyOn(queryClient, "invalidateQueries");
    const flush = vi.fn().mockResolvedValue(undefined);
    renderHook(
      () => useTurnEndFlush(KEY, "proj1", { status: "offline", flush }),
      { wrapper: wrapper(queryClient) },
    );

    notifyTurnEnd(KEY, "failed");
    expect(flush).not.toHaveBeenCalled();
    expect(spy).toHaveBeenCalledWith({ queryKey: specKeys.dependencies("proj1") });
  });

  it("always reads the latest collab via a ref (no stale-closure flush)", () => {
    const queryClient = new QueryClient();
    const flushV1 = vi.fn().mockResolvedValue(undefined);
    const flushV2 = vi.fn().mockResolvedValue(undefined);
    const { rerender } = renderHook(
      ({ collab }) => useTurnEndFlush(KEY, "proj1", collab),
      {
        wrapper: wrapper(queryClient),
        initialProps: { collab: { status: "connected" as const, flush: flushV1 } },
      },
    );
    rerender({ collab: { status: "connected" as const, flush: flushV2 } });

    notifyTurnEnd(KEY, "completed");
    expect(flushV1).not.toHaveBeenCalled();
    expect(flushV2).toHaveBeenCalledTimes(1);
  });
});
