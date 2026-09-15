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
import { specKeys } from "./keys";

const mockGET = vi.fn();
vi.mock("../../../api/client", () => ({
  client: { GET: (...args: unknown[]) => mockGET(...args) },
}));

// Imported AFTER the mock so the module under test picks up the stub client.
const { useDesignDependencies, useSpecFiles } = await import("./queries");

function wrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

describe("useDesignDependencies (#252 Task 9)", () => {
  beforeEach(() => {
    mockGET.mockReset();
  });

  it("keys off specKeys.dependencies(projectName) and returns the endpoint's payload", async () => {
    const payload = [
      {
        componentName: "checkout-api",
        dependencies: [{ kind: "external", name: "stripe", status: "resolved" }],
      },
    ];
    mockGET.mockResolvedValue({ data: payload, error: undefined });
    const queryClient = new QueryClient();

    const { result } = renderHook(() => useDesignDependencies("proj1"), {
      wrapper: wrapper(queryClient),
    });

    await waitFor(() => expect(result.current.data).toEqual(payload));
    expect(mockGET).toHaveBeenCalledWith(
      "/projects/{projectName}/design/dependencies",
      { params: { path: { projectName: "proj1" } } },
    );
    // The exact key Task 5's turn-end freshness invalidation targets —
    // a different key would silently break that wiring.
    expect(queryClient.getQueryData(specKeys.dependencies("proj1"))).toEqual(
      payload,
    );
  });

  it("degrades to [] on a null payload (no design yet)", async () => {
    mockGET.mockResolvedValue({ data: null, error: undefined });
    const queryClient = new QueryClient();

    const { result } = renderHook(() => useDesignDependencies("proj1"), {
      wrapper: wrapper(queryClient),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual([]);
  });

  // The read used to swallow its errors and resolve to [], which made "this
  // project declares no dependencies" and "the console could not find out"
  // indistinguishable. The Builds page's External resources section acts on the
  // difference — it is where a parked deploy sends people to supply values, and
  // a swallowed error removed the whole section. Callers that only decorate
  // degrade at their own use sites (`data ?? []`) instead.
  it("surfaces a fetch error rather than passing it off as an empty design", async () => {
    mockGET.mockResolvedValue({ data: undefined, error: { message: "boom" } });
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    const { result } = renderHook(() => useDesignDependencies("proj1"), {
      wrapper: wrapper(queryClient),
    });

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.data).toBeUndefined();
    expect(result.current.error?.message).toContain("boom");
  });
});

// `projectName` is optional so a caller withholding it on a permission gate
// (OverviewArchitecture's hasSpecAccess ? projectName : undefined) never
// fires a doomed request — see queries.ts's own doc comment on this hook.
describe("useSpecFiles", () => {
  beforeEach(() => {
    mockGET.mockReset();
  });

  it("asks for nothing and stays pending, never erroring, without a project", async () => {
    const queryClient = new QueryClient();
    const { result } = renderHook(() => useSpecFiles(undefined), {
      wrapper: wrapper(queryClient),
    });

    await new Promise((r) => setTimeout(r, 0));
    expect(mockGET).not.toHaveBeenCalled();
    expect(result.current.isPending).toBe(true);
    expect(result.current.isError).toBe(false);
  });

  it("fetches once a project is supplied", async () => {
    mockGET.mockResolvedValue({ data: [], error: undefined });
    const queryClient = new QueryClient();

    const { result } = renderHook(() => useSpecFiles("proj1"), {
      wrapper: wrapper(queryClient),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(mockGET).toHaveBeenCalledWith("/projects/{projectName}/files", {
      params: { path: { projectName: "proj1" } },
    });
  });
});
