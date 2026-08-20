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

import type { ReactNode } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { specKeys } from "../../spec/api/keys";
import { projectKeys } from "./keys";

const mockGET = vi.fn();
const mockPOST = vi.fn();
vi.mock("../../../api/client", () => ({
  client: {
    GET: (...args: unknown[]) => mockGET(...args),
    POST: (...args: unknown[]) => mockPOST(...args),
  },
}));

// Imported after the HTTP boundary is stubbed so the real query and mutation
// implementations retain their normal QueryClient behavior.
const {
  useComponentDependencyStatuses,
  useProjectDependencyReadiness,
  useSaveConnectionValues,
} = await import("./queries");

function wrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

describe("project dependency readiness", () => {
  beforeEach(() => {
    mockGET.mockReset();
    mockPOST.mockReset();
  });

  it("reads development readiness from the project dependency endpoint", async () => {
    const payload = { configured: false, dependencies: [] };
    mockGET.mockResolvedValue({ data: payload, error: undefined });
    const queryClient = new QueryClient();

    const { result } = renderHook(
      () => useProjectDependencyReadiness("acme"),
      { wrapper: wrapper(queryClient) },
    );

    await waitFor(() => expect(result.current.data).toEqual(payload));
    expect(mockGET).toHaveBeenCalledWith(
      "/projects/{projectName}/dependencies/readiness",
      {
        params: {
          path: { projectName: "acme" },
          query: { environment: "development" },
        },
      },
    );
    expect(
      queryClient.getQueryData(
        projectKeys.dependencyReadiness("acme", "development"),
      ),
    ).toEqual(payload);
  });

  it("keeps readiness for different environments in separate cache entries", async () => {
    const development = { configured: false, dependencies: [] };
    const production = { configured: true, dependencies: [] };
    mockGET
      .mockResolvedValueOnce({ data: development, error: undefined })
      .mockResolvedValueOnce({ data: production, error: undefined });
    const queryClient = new QueryClient();

    const devHook = renderHook(
      () => useProjectDependencyReadiness("acme", "development"),
      { wrapper: wrapper(queryClient) },
    );
    await waitFor(() => expect(devHook.result.current.data).toEqual(development));
    const prodHook = renderHook(
      () => useProjectDependencyReadiness("acme", "production"),
      { wrapper: wrapper(queryClient) },
    );
    await waitFor(() => expect(prodHook.result.current.data).toEqual(production));

    expect(mockGET).toHaveBeenCalledTimes(2);
    expect(
      queryClient.getQueryData(
        projectKeys.dependencyReadiness("acme", "development"),
      ),
    ).toEqual(development);
    expect(
      queryClient.getQueryData(
        projectKeys.dependencyReadiness("acme", "production"),
      ),
    ).toEqual(production);
  });
});

describe("component dependency statuses", () => {
  beforeEach(() => {
    mockGET.mockReset();
    mockPOST.mockReset();
  });

  it("reads each generated per-component status endpoint in development", async () => {
    mockGET
      .mockResolvedValueOnce({
        data: {
          outputs: [],
          ready: true,
          status: "Ready",
          valueState: "configured",
        },
        error: undefined,
      })
      .mockResolvedValueOnce({
        data: {
          outputs: [],
          ready: false,
          status: "Pending",
          valueState: "unset",
        },
        error: undefined,
      });
    const queryClient = new QueryClient();

    const { result } = renderHook(
      () =>
        useComponentDependencyStatuses("acme", [
          { componentName: "storefront", dependencyName: "stripe" },
          { componentName: "worker", dependencyName: "stripe" },
        ]),
      { wrapper: wrapper(queryClient) },
    );

    await waitFor(() => expect(result.current.isPending).toBe(false));
    expect(result.current.statuses).toEqual([
      {
        componentName: "storefront",
        dependencyName: "stripe",
        status: {
          outputs: [],
          ready: true,
          status: "Ready",
          valueState: "configured",
        },
      },
      {
        componentName: "worker",
        dependencyName: "stripe",
        status: {
          outputs: [],
          ready: false,
          status: "Pending",
          valueState: "unset",
        },
      },
    ]);
    expect(mockGET).toHaveBeenNthCalledWith(
      1,
      "/projects/{projectName}/components/{componentName}/dependencies/{depName}/status",
      {
        params: {
          path: {
            projectName: "acme",
            componentName: "storefront",
            depName: "stripe",
          },
          query: { environment: "development" },
        },
      },
    );
    expect(mockGET).toHaveBeenNthCalledWith(
      2,
      "/projects/{projectName}/components/{componentName}/dependencies/{depName}/status",
      {
        params: {
          path: {
            projectName: "acme",
            componentName: "worker",
            depName: "stripe",
          },
          query: { environment: "development" },
        },
      },
    );
    expect(
      queryClient.getQueryData(
        projectKeys.componentDependencyStatus(
          "acme",
          "storefront",
          "stripe",
          "development",
        ),
      ),
    ).toMatchObject({ valueState: "configured" });
  });

  it("refetches every component status through one rules-safe callback", async () => {
    mockGET.mockResolvedValue({
      data: {
        outputs: [],
        ready: true,
        status: "Ready",
        valueState: "configured",
      },
      error: undefined,
    });
    const queryClient = new QueryClient();
    const { result } = renderHook(
      () =>
        useComponentDependencyStatuses("acme", [
          { componentName: "storefront", dependencyName: "stripe" },
          { componentName: "worker", dependencyName: "stripe" },
        ]),
      { wrapper: wrapper(queryClient) },
    );
    await waitFor(() => expect(result.current.isPending).toBe(false));
    mockGET.mockClear();

    await act(async () => {
      await result.current.refetch();
    });

    expect(mockGET).toHaveBeenCalledTimes(2);
  });
});

describe("useSaveConnectionValues", () => {
  beforeEach(() => {
    mockPOST.mockResolvedValue({ data: undefined, error: undefined });
  });

  it("refreshes Builds readiness and component status after a successful save", async () => {
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useSaveConnectionValues("acme"), {
      wrapper: wrapper(queryClient),
    });

    await act(async () => {
      await result.current.mutateAsync({
        name: "stripe",
        environment: "development",
        values: { API_KEY: "secret" },
      });
    });

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: projectKeys.dependencyReadinessRoot("acme"),
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: projectKeys.componentDependencyStatuses("acme"),
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: specKeys.dependencies("acme"),
    });
  });

  it("surfaces the API error message when saving values fails", async () => {
    mockPOST.mockResolvedValue({
      data: undefined,
      error: { code: "save_failed", message: "Vault write failed" },
    });
    const queryClient = new QueryClient({
      defaultOptions: { mutations: { retry: false } },
    });
    const { result } = renderHook(() => useSaveConnectionValues("acme"), {
      wrapper: wrapper(queryClient),
    });

    await expect(
      result.current.mutateAsync({
        name: "stripe",
        environment: "development",
        values: { API_KEY: "secret" },
      }),
    ).rejects.toThrow("Vault write failed");
  });
});
