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
  AE_STUDIO_RESTARTING,
  AeStudioNotReadyError,
  PodRequestError,
  designAgent,
  designAgentCall,
  isPodUnavailable,
  onPodOutage,
  setAeStudioUrls,
} = await import("./aeStudio");

const DESIGN = "http://ae-design-agent.mock";
const URLS = { designAgent: DESIGN };
const server = setupServer();

beforeAll(() => server.listen({ onUnhandledRequest: "error" }));
afterEach(() => {
  server.resetHandlers();
  setAeStudioUrls(null);
});
afterAll(() => server.close());

const activeTurn = () =>
  designAgentCall("Couldn't reach the agent", (agent) =>
    agent.GET("/projects/{projectName}/turns/active", { params: { path: { projectName: "p" } } }),
  );

describe("designAgent()", () => {
  it("throws AeStudioNotReadyError until AE Studio's URLs arrive, and again once they are cleared", () => {
    expect(() => designAgent()).toThrow(AeStudioNotReadyError);
    setAeStudioUrls(URLS);
    expect(() => designAgent()).not.toThrow();
    setAeStudioUrls(null);
    expect(() => designAgent()).toThrow(AeStudioNotReadyError);
  });

  it("keeps one client per origin and swaps it when the origin changes", () => {
    setAeStudioUrls(URLS);
    const first = designAgent();
    setAeStudioUrls(URLS);
    expect(designAgent()).toBe(first);
    setAeStudioUrls({ designAgent: "http://other-design.mock" });
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

  it("not being ready reads as AE Studio restarting, and is not an outage of the pod", () => {
    const err = new AeStudioNotReadyError();
    expect(err.message).toBe(AE_STUDIO_RESTARTING);
    expect(isPodUnavailable(err)).toBe(false);
  });
});

describe("designAgentCall", () => {
  it("hands the answer back as openapi-fetch read it", async () => {
    setAeStudioUrls(URLS);
    server.use(
      http.get(`${DESIGN}/v1/projects/p/turns/active`, () =>
        HttpResponse.json(
          { type: "about:blank", title: "Not Found", status: 404, detail: "project p is unknown", code: "project_unknown" },
          { status: 404, headers: { "Content-Type": "application/problem+json" } },
        ),
      ),
    );
    const outages = vi.fn();
    const off = onPodOutage(outages);
    const { error, response } = await activeTurn();
    off();
    expect(response.status).toBe(404);
    expect(error).toMatchObject({ code: "project_unknown" });
    expect(outages).not.toHaveBeenCalled();
  });

  it("tells the outage listeners about a 503, and still hands the answer back", async () => {
    setAeStudioUrls(URLS);
    server.use(
      http.get(`${DESIGN}/v1/projects/p/turns/active`, () =>
        HttpResponse.json(
          { type: "about:blank", title: "Service Unavailable", status: 503, code: "shutting_down" },
          { status: 503, headers: { "Retry-After": "7" } },
        ),
      ),
    );
    const outages = vi.fn();
    const off = onPodOutage(outages);
    const { response } = await activeTurn();
    off();
    expect(response.status).toBe(503);
    expect(outages).toHaveBeenCalledTimes(1);
  });

  it("throws a PodRequestError for no answer at all, and tells the outage listeners", async () => {
    setAeStudioUrls(URLS);
    server.use(http.get(`${DESIGN}/v1/projects/p/turns/active`, () => HttpResponse.error()));
    const outages = vi.fn();
    const off = onPodOutage(outages);
    const err = await activeTurn().catch((e: unknown) => e);
    off();
    expect(err).toBeInstanceOf(PodRequestError);
    expect(err).toMatchObject({ message: "Couldn't reach the agent", status: undefined });
    expect(isPodUnavailable(err)).toBe(true);
    expect(outages).toHaveBeenCalledTimes(1);
  });

  it("is not ready before the URLs arrive, and tells no one", async () => {
    const outages = vi.fn();
    const off = onPodOutage(outages);
    const err = await activeTurn().catch((e: unknown) => e);
    off();
    expect(err).toBeInstanceOf(AeStudioNotReadyError);
    expect(outages).not.toHaveBeenCalled();
  });
});

describe("PodRequestError", () => {
  it("carries the problem's code, detail, status and Retry-After", () => {
    const err = new PodRequestError({ code: "project_unknown", detail: "project p is unknown" }, "x", { status: 404 });
    expect(err).toMatchObject({ message: "project p is unknown", code: "project_unknown", status: 404 });
    expect(isPodUnavailable(err)).toBe(false);
  });

  it("says a 503 the way the console says a restart, keeping its code and Retry-After", () => {
    const err = new PodRequestError({ code: "shutting_down", detail: "draining" }, "x", { status: 503, retryAfterMs: 7000 });
    expect(err).toMatchObject({ message: AE_STUDIO_RESTARTING, code: "shutting_down", status: 503, retryAfterMs: 7000 });
    expect(isPodUnavailable(err)).toBe(true);
  });
});
