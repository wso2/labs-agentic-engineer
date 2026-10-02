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
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { specKeys } from "./keys";

const mockGET = vi.fn();
vi.mock("../../../api/client", () => ({
  client: { GET: (...args: unknown[]) => mockGET(...args) },
}));

// No session in a unit test: the studio-tools client's auth wrapper sends no token.
vi.mock("../../../auth/token", () => ({
  getAccessToken: () => Promise.resolve(null),
  renewAccessToken: () => Promise.resolve(null),
  redirectToSignIn: () => undefined,
}));

// Imported AFTER the mock so the module under test picks up the stub client.
const { useDesignDependencies, useSpecFileContent, useSpecFiles } = await import("./queries");
const { setAeStudioUrls } = await import("../../../api/aeStudio");

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

// Spec files are read from the org's ae-studio-tools pod, at the URL the
// GET /ae-studio answer names, and only once that answer is `ready`.
describe("spec file reads from ae-studio-tools /v1", () => {
  const TOOLS = "http://ae-studio-tools.mock";
  const urls = { tools: TOOLS, designAgent: "http://ae-design-agent.mock", collab: "ws://ae-collab.mock" };
  const server = setupServer();

  // GET /ae-studio (aep-api, the stub client) answers with `state`.
  function aeStudioAnswers(state: "ready" | "provisioning") {
    mockGET.mockImplementation((path: string) =>
      Promise.resolve(
        path === "/ae-studio"
          ? { data: state === "ready" ? { state, urls } : { state }, error: undefined }
          : { data: undefined, error: { message: `unexpected ${path}` } },
      ),
    );
  }

  beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
  beforeEach(() => mockGET.mockReset());
  afterEach(() => {
    vi.useRealTimers();
    server.resetHandlers();
    setAeStudioUrls(null);
  });
  afterAll(() => server.close());

  it("lists and reads spec files from ae-studio-tools /v1", async () => {
    aeStudioAnswers("ready");
    // A path the spec view lists (specs/<known folder>/<file>); toSpecEntries
    // drops the rest.
    server.use(
      http.get(`${TOOLS}/v1/projects/p/files`, () => HttpResponse.json([{ path: "specs/requirements/prd.md", sha: "1", size: 1 }])),
      http.get(`${TOOLS}/v1/projects/p/files/specs/requirements/prd.md`, () =>
        HttpResponse.json({ path: "specs/requirements/prd.md", sha: "1", content: "# a" }),
      ),
    );
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });

    const list = renderHook(() => useSpecFiles("p"), { wrapper: wrapper(queryClient) });
    await waitFor(() => expect(list.result.current.data?.[0]?.path).toBe("specs/requirements/prd.md"));

    const read = renderHook(() => useSpecFileContent("p", { path: "specs/requirements/prd.md", sha: "1" }), {
      wrapper: wrapper(queryClient),
    });
    await waitFor(() => expect(read.result.current.data?.content).toBe("# a"));
    // Nothing went to aep-api but the AE Studio state read.
    expect(mockGET.mock.calls.map((c) => c[0])).toEqual(["/ae-studio"]);
  });

  it("makes no files request while AE Studio is provisioning", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    aeStudioAnswers("provisioning");
    const seen = vi.fn();
    server.use(
      http.get(`${TOOLS}/v1/*`, () => {
        seen();
        return HttpResponse.json([]);
      }),
    );
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });

    const list = renderHook(() => useSpecFiles("p"), { wrapper: wrapper(queryClient) });
    const read = renderHook(() => useSpecFileContent("p", { path: "specs/a.md", sha: "1" }), {
      wrapper: wrapper(queryClient),
    });
    await waitFor(() => expect(mockGET).toHaveBeenCalledWith("/ae-studio"));
    // Two provisioning polls later, still nothing asked of the pod.
    await act(() => vi.advanceTimersByTimeAsync(4000));
    expect(seen).not.toHaveBeenCalled();
    expect(list.result.current.fetchStatus).toBe("idle");
    expect(read.result.current.fetchStatus).toBe("idle");
  });
});
