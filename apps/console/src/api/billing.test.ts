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
import {
  ensureBillingSubscriptionActivated,
  resetBillingActivationForTests,
} from "./billing";

vi.mock("../config/env", () => ({
  env: {
    billingApiBaseUrl: "",
    authMode: "mock",
  },
}));

vi.mock("../auth/token", () => ({
  getAccessToken: vi.fn(async () => "tok-test"),
}));

import { env } from "../config/env";

describe("ensureBillingSubscriptionActivated", () => {
  beforeEach(() => {
    resetBillingActivationForTests();
    vi.stubGlobal("fetch", vi.fn());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    resetBillingActivationForTests();
  });

  it("does not call the network when BILLING_API_BASE_URL is empty", async () => {
    (env as { billingApiBaseUrl: string }).billingApiBaseUrl = "";
    await ensureBillingSubscriptionActivated();
    expect(fetch).not.toHaveBeenCalled();
  });

  it("calls billing once when the URL is set, even if invoked twice", async () => {
    (env as { billingApiBaseUrl: string }).billingApiBaseUrl =
      "https://billing.example/billing-service-user-api";
    vi.mocked(fetch).mockResolvedValue(new Response("{}", { status: 200 }));

    await Promise.all([
      ensureBillingSubscriptionActivated(),
      ensureBillingSubscriptionActivated(),
    ]);

    expect(fetch).toHaveBeenCalledTimes(1);
    expect(vi.mocked(fetch).mock.calls[0]?.[0]).toBe(
      "https://billing.example/billing-service-user-api/api/v1/organization?product=app-factory",
    );
  });
});
