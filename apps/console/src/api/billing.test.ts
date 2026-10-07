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

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ensureBillingSubscriptionActivated, resetBillingActivationForTests } from "./billing";

vi.mock("../config/env", () => ({ env: { billingApiBaseUrl: "" } }));
vi.mock("../auth/token", () => ({ getAccessToken: vi.fn(async () => "tok-test") }));

import { env } from "../config/env";

const setBase = (url: string) => {
  (env as { billingApiBaseUrl: string }).billingApiBaseUrl = url;
};

describe("ensureBillingSubscriptionActivated", () => {
  beforeEach(() => {
    resetBillingActivationForTests();
    vi.stubGlobal("fetch", vi.fn());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    resetBillingActivationForTests();
  });

  it("makes no call when BILLING_API_BASE_URL is unset", async () => {
    setBase("");
    await ensureBillingSubscriptionActivated();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("activates once per session, with the signed-in user's token", async () => {
    setBase("https://billing.example/billing-service-user-api/");
    vi.mocked(fetch).mockResolvedValue(new Response("{}", { status: 200 }));

    await Promise.all([ensureBillingSubscriptionActivated(), ensureBillingSubscriptionActivated()]);
    await ensureBillingSubscriptionActivated();

    expect(fetch).toHaveBeenCalledTimes(1);
    const [url, init] = vi.mocked(fetch).mock.calls[0] ?? [];
    expect(url).toBe("https://billing.example/billing-service-user-api/api/v1/organization?product=app-factory");
    expect((init as RequestInit).headers).toMatchObject({ Authorization: "Bearer tok-test" });
  });

  it("tries again on a later call after a failure", async () => {
    setBase("https://billing.example");
    vi.mocked(fetch)
      .mockResolvedValueOnce(new Response("down", { status: 503 }))
      .mockResolvedValueOnce(new Response("{}", { status: 200 }));

    await expect(ensureBillingSubscriptionActivated()).rejects.toThrow("Billing API error 503: down");
    await ensureBillingSubscriptionActivated();
    expect(fetch).toHaveBeenCalledTimes(2);
  });
});
