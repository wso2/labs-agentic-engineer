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

import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";

// No session in a unit test: the auth wrapper sends no token.
vi.mock("../auth/token", () => ({
  getAccessToken: () => Promise.resolve(null),
  renewAccessToken: () => Promise.resolve(null),
  redirectToSignIn: () => undefined,
}));

const {
  AeStudioNotReadyError,
  StudioToolsError,
  designAgent,
  isStudioToolsUnavailable,
  setAeStudioUrls,
  studioTools,
  studioToolsRead,
  studioToolsRetryDelay,
} = await import("./aeStudio");

const TOOLS = "http://ae-studio-tools.mock";
const DESIGN = "http://ae-design-agent.mock";
const URLS = { tools: TOOLS, designAgent: DESIGN };
const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  setAeStudioUrls(null);
});
afterAll(() => server.close());

const listFiles = (projectName: string) =>
  studioToolsRead("Failed to load the spec files", (tools) =>
    tools.GET("/projects/{projectName}/files", { params: { path: { projectName } } }),
  );

describe("studioTools()", () => {
  it("throws AeStudioNotReadyError until AE Studio's URLs arrive, and again once they are cleared", () => {
    expect(() => studioTools()).toThrow(AeStudioNotReadyError);
    setAeStudioUrls(URLS);
    expect(() => studioTools()).not.toThrow();
    setAeStudioUrls(null);
    expect(() => studioTools()).toThrow(AeStudioNotReadyError);
  });

  it("keeps one client per tools origin and swaps it when the origin changes", () => {
    setAeStudioUrls(URLS);
    const first = studioTools();
    setAeStudioUrls(URLS);
    expect(studioTools()).toBe(first);
    setAeStudioUrls({ ...URLS, tools: "http://other-tools.mock" });
    expect(studioTools()).not.toBe(first);
  });

  it("calls the pod's /v1 API", async () => {
    setAeStudioUrls(URLS);
    server.use(
      http.get(`${TOOLS}/v1/projects/p/files`, () => HttpResponse.json([{ path: "specs/a.md", sha: "1", size: 1 }])),
    );
    await expect(listFiles("p")).resolves.toEqual([{ path: "specs/a.md", sha: "1", size: 1 }]);
  });
});

describe("designAgent()", () => {
  it("throws AeStudioNotReadyError until AE Studio's URLs arrive, and again once they are cleared", () => {
    expect(() => designAgent()).toThrow(AeStudioNotReadyError);
    setAeStudioUrls(URLS);
    expect(() => designAgent()).not.toThrow();
    setAeStudioUrls(null);
    expect(() => designAgent()).toThrow(AeStudioNotReadyError);
  });

  it("keeps its client while only the tools origin moves, and swaps it when its own origin does", () => {
    setAeStudioUrls(URLS);
    const first = designAgent();
    setAeStudioUrls({ ...URLS, tools: "http://other-tools.mock" });
    expect(designAgent()).toBe(first);
    setAeStudioUrls({ ...URLS, designAgent: "http://other-design.mock" });
    expect(designAgent()).not.toBe(first);
  });

  it("calls the design agent's /v1 API", async () => {
    setAeStudioUrls(URLS);
    server.use(http.get(`${DESIGN}/v1/projects/p/turns/active`, () => new HttpResponse(null, { status: 204 })));
    const { response } = await designAgent().GET("/projects/{projectName}/turns/active", {
      params: { path: { projectName: "p" } },
    });
    expect(response.status).toBe(204);
  });
});

describe("studioToolsRead failures", () => {
  it("carries the problem's code, detail and status", async () => {
    setAeStudioUrls(URLS);
    server.use(
      http.get(`${TOOLS}/v1/projects/p/files`, () =>
        HttpResponse.json(
          { type: "about:blank", title: "Not Found", status: 404, detail: "project p is unknown", code: "project_unknown" },
          { status: 404, headers: { "Content-Type": "application/problem+json" } },
        ),
      ),
    );
    const err = await listFiles("p").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(StudioToolsError);
    expect(err).toMatchObject({ message: "project p is unknown", code: "project_unknown", status: 404 });
    expect(isStudioToolsUnavailable(err)).toBe(false);
  });

  it("reads Retry-After from a 503 and calls it unavailable", async () => {
    setAeStudioUrls(URLS);
    server.use(
      http.get(`${TOOLS}/v1/projects/p/files`, () =>
        HttpResponse.json(
          { type: "about:blank", title: "Service Unavailable", status: 503, code: "aep_api_unavailable" },
          { status: 503, headers: { "Retry-After": "7" } },
        ),
      ),
    );
    const err = await listFiles("p").catch((e: unknown) => e);
    expect(err).toMatchObject({ code: "aep_api_unavailable", status: 503, retryAfterMs: 7000 });
    expect(isStudioToolsUnavailable(err)).toBe(true);
  });

  it("calls a network failure unavailable", async () => {
    setAeStudioUrls(URLS);
    server.use(http.get(`${TOOLS}/v1/projects/p/files`, () => HttpResponse.error()));
    const err = await listFiles("p").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(StudioToolsError);
    expect(err).toMatchObject({ message: "Failed to load the spec files", status: undefined });
    expect(isStudioToolsUnavailable(err)).toBe(true);
  });

  it("is not ready, not unavailable, before the URLs arrive", async () => {
    const err = await listFiles("p").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(AeStudioNotReadyError);
    expect(isStudioToolsUnavailable(err)).toBe(false);
  });
});

describe("studioToolsRetryDelay", () => {
  const unavailable = (retryAfterMs?: number) =>
    new StudioToolsError({ code: "disk_full", title: "Service Unavailable" }, "x", { status: 503, retryAfterMs });

  it("waits Retry-After on a 503, 5 s without one", () => {
    expect(studioToolsRetryDelay(0, unavailable(7000))).toBe(7000);
    expect(studioToolsRetryDelay(0, unavailable())).toBe(5000);
  });

  it("backs off like react-query's default otherwise", () => {
    expect(studioToolsRetryDelay(0, new Error("x"))).toBe(1000);
    expect(studioToolsRetryDelay(2, new Error("x"))).toBe(4000);
    expect(studioToolsRetryDelay(10, new Error("x"))).toBe(30_000);
  });
});
