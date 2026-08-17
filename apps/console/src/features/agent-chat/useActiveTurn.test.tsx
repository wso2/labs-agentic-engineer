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

// The server's own answer to "is a turn running right now" (#485). What matters
// is the `resolved` flag: callers gate a send on it, so it must be false until
// the read has actually answered — an unresolved read that reported "no turn"
// is how the second /start got past the guard.

import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useActiveTurn } from "./useActiveTurn";

const PROJECT = "proj1";

const mockGetActive = vi.fn();
vi.mock("./api/turns", async (importOriginal) => {
  const real = await importOriginal<typeof import("./api/turns")>();
  return { ...real, getActiveTurn: (...a: unknown[]) => mockGetActive(...a) };
});

function wrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
}

describe("useActiveTurn", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetActive.mockResolvedValue(null);
  });

  it("is unresolved before the read answers", () => {
    mockGetActive.mockReturnValue(new Promise(() => {})); // never settles
    const { result } = renderHook(() => useActiveTurn(PROJECT), {
      wrapper: wrapper(),
    });

    expect(result.current.resolved).toBe(false);
    expect(result.current.active).toBe(false);
  });

  it("reports an active turn once the read answers", async () => {
    mockGetActive.mockResolvedValue({ turnId: "t1", status: "running" });
    const { result } = renderHook(() => useActiveTurn(PROJECT), {
      wrapper: wrapper(),
    });

    await waitFor(() => expect(result.current.resolved).toBe(true));
    expect(result.current.active).toBe(true);
  });

  it("resolves as inactive when no turn runs", async () => {
    const { result } = renderHook(() => useActiveTurn(PROJECT), {
      wrapper: wrapper(),
    });

    await waitFor(() => expect(result.current.resolved).toBe(true));
    expect(result.current.active).toBe(false);
  });

  // A finished turn is not an active one — the poll's next answer must open the
  // gate the running answer closed.
  it("resolves as inactive for a settled turn", async () => {
    mockGetActive.mockResolvedValue({ turnId: "t1", status: "completed" });
    const { result } = renderHook(() => useActiveTurn(PROJECT), {
      wrapper: wrapper(),
    });

    await waitFor(() => expect(result.current.resolved).toBe(true));
    expect(result.current.active).toBe(false);
  });

  // A failed read resolves too: a gate that waits forever on an unreachable
  // endpoint would swallow the click that opened it.
  it("resolves as inactive when the read fails", async () => {
    mockGetActive.mockRejectedValue(new Error("network down"));
    const { result } = renderHook(() => useActiveTurn(PROJECT), {
      wrapper: wrapper(),
    });

    await waitFor(() => expect(result.current.resolved).toBe(true));
    expect(result.current.active).toBe(false);
  });

  // Opting out is not "the answer is no turn": a disabled read issues no
  // request, so it reports resolved-and-idle and gates nothing.
  it("issues no request when disabled", () => {
    const { result } = renderHook(() => useActiveTurn(PROJECT, false), {
      wrapper: wrapper(),
    });

    expect(mockGetActive).not.toHaveBeenCalled();
    expect(result.current).toEqual({ active: false, resolved: true });
  });
});
