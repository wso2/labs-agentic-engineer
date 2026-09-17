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

import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

const mockGET = vi.fn();
vi.mock("../../../api/client", () => ({
  client: { GET: (...args: unknown[]) => mockGET(...args) },
}));

// Imported AFTER the mock so the module under test picks up the stub client.
const { useValidationReport } = await import("./queries");

function wrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

function query() {
  return mockGET.mock.calls[0]?.[1]?.params?.query;
}

describe("useValidationReport", () => {
  beforeEach(() => {
    mockGET.mockReset();
    mockGET.mockResolvedValue({
      data: { path: "p", content: "{}", sha: "blob" },
      error: undefined,
    });
  });

  // The correctness fix: the report sits at ONE path that every run overwrites, so
  // an unpinned read hands a historical run the newest run's results — and a run
  // whose agent committed no report would silently inherit its predecessor's.
  it("pins the read to the validation cycle's merge commit", async () => {
    const { result } = renderHook(
      () =>
        useValidationReport("proj1", "v1", true, "tests/acceptance/report.json", "abc123def456"),
      { wrapper: wrapper(new QueryClient()) },
    );

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(query()).toEqual({ ref: "abc123def456" });
  });

  // Without a cycle SHA there is nothing to pin to, and sending `ref: undefined`
  // would be a malformed request rather than a tip read.
  it("omits ref entirely when no merge commit is known", async () => {
    const { result } = renderHook(
      () => useValidationReport("proj1", "v1", true, "tests/acceptance/report.json"),
      { wrapper: wrapper(new QueryClient()) },
    );

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(query()).toBeUndefined();
  });

  // Two runs' reports must not collide in the cache. Keying on the SHA is what
  // makes each run's report its own immutable entry.
  it("caches per merge commit, so two runs never share an entry", async () => {
    const queryClient = new QueryClient();
    const path = "tests/acceptance/report.json";

    const first = renderHook(
      () => useValidationReport("proj1", "v1", true, path, "sha-run-1"),
      { wrapper: wrapper(queryClient) },
    );
    await waitFor(() => expect(first.result.current.isSuccess).toBe(true));

    const second = renderHook(
      () => useValidationReport("proj1", "v1", true, path, "sha-run-2"),
      { wrapper: wrapper(queryClient) },
    );
    await waitFor(() => expect(second.result.current.isSuccess).toBe(true));

    expect(mockGET).toHaveBeenCalledTimes(2);
    expect(mockGET.mock.calls[1]?.[1]?.params?.query).toEqual({ ref: "sha-run-2" });
  });
});
