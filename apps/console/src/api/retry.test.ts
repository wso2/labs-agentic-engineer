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

import { describe, expect, it } from "vitest";
import { ApiRequestError, retryAfterMs } from "./errors";
import { queryRetry, queryRetryDelay } from "./retry";

const unavailable = (ms?: number) =>
  new ApiRequestError({ code: "ae_studio_unavailable", message: "AE Studio is not ready" }, "x", { retryAfterMs: ms });

describe("query retry for aep-api failures", () => {
  it("gives a restarting AE Studio six tries, anything else three", () => {
    expect(queryRetry(5, unavailable())).toBe(true);
    expect(queryRetry(6, unavailable())).toBe(false);
    expect(queryRetry(2, new Error("boom"))).toBe(true);
    expect(queryRetry(3, new Error("boom"))).toBe(false);
  });

  it("does not retry what only the user or an administrator can fix", () => {
    // A retry cannot connect GitHub or fix the configuration: the notice shows at once.
    expect(queryRetry(0, new ApiRequestError({ code: "github_not_connected" }, "x"))).toBe(false);
    expect(queryRetry(0, new ApiRequestError({ code: "ae_studio_misconfigured" }, "x"))).toBe(false);
    expect(queryRetry(0, { code: "github_not_connected" })).toBe(false);
  });

  it("waits the server's Retry-After on ae_studio_unavailable, 5 s without one", () => {
    expect(queryRetryDelay(0, unavailable(7000))).toBe(7000);
    expect(queryRetryDelay(4, unavailable())).toBe(5000);
    // The raw envelope (a query that throws the body) reads the same.
    expect(queryRetryDelay(1, { code: "ae_studio_unavailable" })).toBe(5000);
  });

  it("backs off exponentially otherwise, capped at 30 s", () => {
    expect(queryRetryDelay(0, new Error("x"))).toBe(1000);
    expect(queryRetryDelay(2, new Error("x"))).toBe(4000);
    expect(queryRetryDelay(10, new Error("x"))).toBe(30_000);
  });
});

describe("retryAfterMs", () => {
  it("reads delta-seconds and ignores anything else", () => {
    expect(retryAfterMs(new Response(null, { status: 503, headers: { "Retry-After": "5" } }))).toBe(5000);
    expect(retryAfterMs(new Response(null, { status: 503 }))).toBeUndefined();
    expect(retryAfterMs(new Response(null, { status: 503, headers: { "Retry-After": "soon" } }))).toBeUndefined();
  });
});
