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
import { cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";

// The platform's own ae-system project (AE Studio's home) is never the org's
// to see: every read of the project list leaves it out.

const BASE = "http://localhost/api/v1";
vi.mock("../../../api/client", async () => {
  const { default: createClient } = await import("openapi-fetch");
  return { client: createClient({ baseUrl: BASE, fetch: (request) => globalThis.fetch(request) }) };
});

const { useProjectPages, useProjects, useRepoProjectNames } = await import("./queries");

const project = (name: string) => ({ name, displayName: name, repoUrl: `https://github.com/acme/${name}`, createdAt: "2026-10-01T09:00:00Z" });

const server = setupServer(
  http.get(`${BASE}/projects`, ({ request }) => {
    const cursor = new URL(request.url).searchParams.get("cursor");
    return cursor
      ? HttpResponse.json({ items: [project("ae-system"), project("ledger")] })
      : HttpResponse.json({ items: [project("shop"), project("ae-system")], nextCursor: "p2" });
  }),
);
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(cleanup);
afterAll(() => server.close());

function wrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

describe("the org's project lists leave ae-system out", () => {
  it("the first page", async () => {
    const { result } = renderHook(() => useProjects(), { wrapper });
    await waitFor(() => expect(result.current.data).toBeDefined());
    expect(result.current.data!.map((p) => p.name)).toEqual(["shop"]);
  });

  it("the names of projects with a repository, every page read", async () => {
    const { result } = renderHook(() => useRepoProjectNames(60_000), { wrapper });
    await waitFor(() => expect(result.current.data).toBeDefined());
    expect(result.current.data).toEqual(["shop", "ledger"]);
  });

  it("the Projects grid's pages", async () => {
    const { result } = renderHook(() => useProjectPages("", 20), { wrapper });
    await waitFor(() => expect(result.current.data).toBeDefined());
    expect(result.current.data!.pages.flatMap((p) => (p.items ?? []).map((i) => i.name))).toEqual(["shop"]);
  });
});
