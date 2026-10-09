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
import { describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { configKeys } from "./keys";

const patch = vi.fn();
vi.mock("../../../api/client", () => ({ client: { PATCH: (...args: unknown[]) => patch(...args) } }));

const { useSaveAiSettings } = await import("./queries");

function setup() {
  const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: 0 } } });
  const invalidate = vi.spyOn(queryClient, "invalidateQueries");
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  return { invalidate, ...renderHook(() => useSaveAiSettings(), { wrapper }) };
}

const keys = (spy: { mock: { calls: unknown[][] } }) =>
  spy.mock.calls.map((c) => (c[0] as { queryKey: unknown }).queryKey);

describe("useSaveAiSettings", () => {
  it("re-reads the config after a 502 agent_manager_not_updated: the key was saved", async () => {
    patch.mockResolvedValue({
      error: { code: "agent_manager_not_updated", message: "saved, but Agent Manager did not take it", details: [{ field: "body.llm", message: "x" }] },
    });
    const { result, invalidate } = setup();
    await act(async () => {
      result.current.mutate({ llm: { kind: "anthropic", apiKey: "k" } });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(keys(invalidate)).toContainEqual(configKeys.all);
  });

  it("leaves the cached config alone when a refusal saved nothing", async () => {
    patch.mockResolvedValue({
      error: { code: "secret_store_write_failed", message: "nothing saved", details: [{ field: "body.llm", message: "x" }] },
    });
    const { result, invalidate } = setup();
    await act(async () => {
      result.current.mutate({ llm: { kind: "anthropic", apiKey: "k" } });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(keys(invalidate)).not.toContainEqual(configKeys.all);
  });
});
