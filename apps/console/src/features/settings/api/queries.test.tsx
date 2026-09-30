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

import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "../../../generated/aep-api";
import { openaiOrgFixture, sreAgentProjectionFixture } from "../../../mocks/fixtures/settings";

type ConfigProjection = components["schemas"]["ConfigProjection"];

const { mockGET } = vi.hoisted(() => ({ mockGET: vi.fn() }));
vi.mock("../../../api/client", () => ({
  client: { GET: (...args: unknown[]) => mockGET(...args) },
}));

// Imported AFTER the mock so the module under test picks up the stub client.
const { useConfig } = await import("./queries");

function config(over: Partial<ConfigProjection> = {}): ConfigProjection {
  return {
    llm: openaiOrgFixture,
    llmFormats: [],
    agents: {
      runtime: "opencode",
      availableRuntimes: ["claude-code", "opencode"],
      subscription: null,
      updatedAt: null,
      updatedBy: null,
    },
    gitProvider: null,
    idp: {
      kind: "platform",
      issuer: "https://idp.aep.local",
      jwksUrl: "https://idp.aep.local/.well-known/jwks.json",
      hasClientSecret: false,
      publisherClientId: "aep-console",
    },
    sreLlm: null,
    sreAgent: sreAgentProjectionFixture(),
    ...over,
  };
}

function wrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

describe("useConfig", () => {
  beforeEach(() => {
    mockGET.mockReset();
  });

  // The regression this guards: a save's PATCH response reads "applying" right
  // after aep-api pushes it, and with no poll the row's status chip was stuck
  // on "Applying…" until the reader reloaded the page.
  it("re-polls GET /config while the SRE agent is applying, and stops once it resolves", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
      mockGET.mockResolvedValueOnce({
        data: config({ sreAgent: sreAgentProjectionFixture({ source: "organization", status: "applying" }) }),
        error: undefined,
      });

      const { result } = renderHook(() => useConfig(), { wrapper: wrapper(queryClient) });
      await waitFor(() => expect(mockGET).toHaveBeenCalledTimes(1));
      expect(result.current.data?.sreAgent?.status).toBe("applying");

      mockGET.mockResolvedValue({
        data: config({ sreAgent: sreAgentProjectionFixture({ source: "organization", status: "running" }) }),
        error: undefined,
      });
      await act(() => vi.advanceTimersByTimeAsync(5_000));
      await waitFor(() => expect(mockGET).toHaveBeenCalledTimes(2));
      expect(result.current.data?.sreAgent?.status).toBe("running");

      // The status settled, so the next tick must not trigger a third GET.
      await act(() => vi.advanceTimersByTimeAsync(5_000));
      expect(mockGET).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it("never polls when the status starts out running", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
      mockGET.mockResolvedValue({
        data: config({ sreAgent: sreAgentProjectionFixture({ source: "organization", status: "running" }) }),
        error: undefined,
      });

      renderHook(() => useConfig(), { wrapper: wrapper(queryClient) });
      await waitFor(() => expect(mockGET).toHaveBeenCalledTimes(1));

      await act(() => vi.advanceTimersByTimeAsync(5_000));
      expect(mockGET).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });
});
