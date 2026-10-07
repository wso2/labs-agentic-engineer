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
import { act } from "react";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { queryRetry, queryRetryDelay } from "./retry";

// The aep-api reads it serves through the org's AE Studio (git, issues,
// skills) answer 503 `ae_studio_unavailable` with a Retry-After while AE
// Studio restarts. Each must fail with that code and that pace, so the query
// retry waits it out (api/retry.ts) and the gate re-reads AE Studio.

const BASE = "http://localhost/api/v1";
vi.mock("./client", async () => {
  const { default: createClient } = await import("openapi-fetch");
  return { client: createClient({ baseUrl: BASE, fetch: (request) => globalThis.fetch(request) }) };
});

const { useValidationSnapshot } = await import("../features/builds/api/runs");
const { useDesignDependencies } = await import("../features/deploy/api/deploy");
const { useSpecState } = await import("../features/spec/api/specModel");
const { useBuilds } = await import("../features/builds/api/builds");
const { useVersionTasks } = await import("../features/builds/api/tasks");
const { useProjectIssues, useIssueDetail } = await import("../features/issues/api/issues");
const { useSkills, useSkillUpdates, useSkill } = await import("../features/skills/api/skills");

const server = setupServer(
  http.get(`${BASE}/*`, () =>
    HttpResponse.json(
      { code: "ae_studio_unavailable", message: "AE Studio is not ready — try again in a few seconds" },
      { status: 503, headers: { "Retry-After": "5" } },
    ),
  ),
);
beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(cleanup);
afterAll(() => server.close());

function wrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

const reads: [string, () => { error: unknown }][] = [
  // Retried while AE Studio restarts (below), so its first failure is the reason.
  ["a validation attempt's report", () => ({ error: useValidationSnapshot("shop", "v1", "c1", true, true).failureReason })],
  ["the design's dependencies", () => useDesignDependencies("shop")],
  ["the spec state", () => useSpecState("shop")],
  ["the builds (versions)", () => useBuilds("shop")],
  ["a version's tasks", () => useVersionTasks("shop", "v1", false)],
  ["the project's issues", () => useProjectIssues("shop")],
  ["one issue", () => useIssueDetail("shop", 7)],
  ["the skills", () => useSkills()],
  ["the platform's skill updates", () => useSkillUpdates()],
  ["one skill", () => useSkill("tdd")],
];

describe("an aep-api read served through AE Studio, while AE Studio restarts", () => {
  it.each(reads)("%s fails with the code and the server's Retry-After", async (_, read) => {
    const { result } = renderHook(read, { wrapper });
    await waitFor(() => expect(result.current.error).toBeTruthy());
    expect(result.current.error).toMatchObject({ code: "ae_studio_unavailable", retryAfterMs: 5000 });
  });
});

describe("a validation attempt's report", () => {
  it("is final when missing, but waits out an AE Studio restart at the server's pace", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      let calls = 0;
      server.use(
        http.get(`${BASE}/projects/shop/validations/v1/cycles/c1/report`, () => {
          calls += 1;
          return calls === 1
            ? HttpResponse.json({ code: "ae_studio_unavailable", message: "not ready" }, { status: 503, headers: { "Retry-After": "5" } })
            : HttpResponse.json({ code: "not_found", message: "no report" }, { status: 404 });
        }),
      );
      const queryClient = new QueryClient({ defaultOptions: { queries: { retry: queryRetry, retryDelay: queryRetryDelay } } });
      const { result } = renderHook(() => useValidationSnapshot("shop", "v1", "c1", true, true), {
        wrapper: ({ children }) => <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>,
      });
      await waitFor(() => expect(calls).toBe(1));
      await act(() => vi.advanceTimersByTimeAsync(4_900));
      expect(calls).toBe(1);
      await act(() => vi.advanceTimersByTimeAsync(200));
      await waitFor(() => expect(result.current.error).toMatchObject({ code: "not_found" }));
      expect(calls).toBe(2);
    } finally {
      vi.useRealTimers();
    }
  });
});
