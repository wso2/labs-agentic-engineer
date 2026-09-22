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

import { render, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { useTurnEndDependencyRefresh } from "./useTurnEndDependencyRefresh";
import { notifyTurnEnd } from "./chatStore";
import { useTurnEndFlush } from "../spec/collab/useTurnEndFlush";
import { specKeys } from "../spec/api/keys";
import { projectKeys } from "../projects/api/keys";
import { FRESHNESS_POLL_DELAY_MS } from "../spec/api/dependencyFreshness";

const KEY = "aep.chat.v1.acme.proj1";

function wrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

describe("useTurnEndDependencyRefresh — universal fallback (#252 Task 5)", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("invalidates dependencies + preflight immediately on turn-end, plus a follow-up poll", () => {
    const queryClient = new QueryClient();
    const spy = vi.spyOn(queryClient, "invalidateQueries");
    renderHook(() => useTurnEndDependencyRefresh(KEY, "proj1"), {
      wrapper: wrapper(queryClient),
    });

    notifyTurnEnd(KEY, "completed", "turn-1");
    expect(spy).toHaveBeenCalledWith({ queryKey: specKeys.dependencies("proj1") });
    expect(spy).toHaveBeenCalledWith({ queryKey: projectKeys.buildPreflight("proj1") });
    const callsAfterFirst = spy.mock.calls.length;

    vi.advanceTimersByTime(FRESHNESS_POLL_DELAY_MS);
    expect(spy.mock.calls.length).toBeGreaterThan(callsAfterFirst); // the short-poll follow-up fired
  });

  it("ignores turn-end events for a different project's chat key", () => {
    const queryClient = new QueryClient();
    const spy = vi.spyOn(queryClient, "invalidateQueries");
    renderHook(() => useTurnEndDependencyRefresh(KEY, "proj1"), {
      wrapper: wrapper(queryClient),
    });

    notifyTurnEnd("aep.chat.v1.acme.some-other-proj", "completed", "turn-1");
    expect(spy).not.toHaveBeenCalled();
  });

  it("stops reacting after unmount (no dangling subscription)", () => {
    const queryClient = new QueryClient();
    const spy = vi.spyOn(queryClient, "invalidateQueries");
    const { unmount } = renderHook(
      () => useTurnEndDependencyRefresh(KEY, "proj1"),
      { wrapper: wrapper(queryClient) },
    );
    unmount();
    notifyTurnEnd(KEY, "completed", "turn-1");
    expect(spy).not.toHaveBeenCalled();
  });
});

// Important #1 (fix wave 1): chatStore dispatches turn-end subscribers
// SYNCHRONOUSLY, but useTurnEndFlush's connected branch invalidates only
// after an ASYNC collab.flush() resolves. When both hooks are mounted for
// the same chatKey (chat open on the Spec route — the common case), this
// hook's immediate invalidate used to fire before the flush landed, briefly
// showing the freshly-resolved dependency's OLD status. Coordinated via
// chatStore's registerDeterministicFlush/hasDeterministicFlush.
describe("coordination with useTurnEndFlush (Important #1 fix, #252 Task 5)", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("skips its own immediate invalidate while useTurnEndFlush is mounted for the same key; only the post-flush invalidate lands", async () => {
    const queryClient = new QueryClient();
    const spy = vi.spyOn(queryClient, "invalidateQueries");
    const flush = vi.fn().mockResolvedValue(undefined);

    function BothHooksMounted() {
      useTurnEndDependencyRefresh(KEY, "proj1");
      useTurnEndFlush(KEY, "proj1", { status: "connected", flush });
      return null;
    }

    render(
      <QueryClientProvider client={queryClient}>
        <BothHooksMounted />
      </QueryClientProvider>,
    );

    notifyTurnEnd(KEY, "completed", "turn-1");
    // Pre-flush: neither hook should have invalidated yet.
    expect(spy).not.toHaveBeenCalled();
    expect(flush).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(0); // let the flush() microtask resolve
    // Post-flush: the deterministic path's invalidate has landed.
    expect(spy).toHaveBeenCalledWith({ queryKey: specKeys.dependencies("proj1") });
    expect(spy).toHaveBeenCalledWith({ queryKey: projectKeys.buildPreflight("proj1") });
  });

  it("still invalidates immediately when useTurnEndFlush is NOT mounted for that key", () => {
    const queryClient = new QueryClient();
    const spy = vi.spyOn(queryClient, "invalidateQueries");
    renderHook(() => useTurnEndDependencyRefresh(KEY, "proj1"), {
      wrapper: wrapper(queryClient),
    });

    notifyTurnEnd(KEY, "completed", "turn-1");
    expect(spy).toHaveBeenCalledWith({ queryKey: specKeys.dependencies("proj1") });
    expect(spy).toHaveBeenCalledWith({ queryKey: projectKeys.buildPreflight("proj1") });
  });
});
