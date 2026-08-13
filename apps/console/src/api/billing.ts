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

import { env } from "../config/env";
import { getAccessToken } from "../auth/token";

/** Product code billing uses for App Factory subscriptions. */
export const APP_FACTORY_BILLING_PRODUCT = "app-factory";

/**
 * Activate (or no-op refresh) the org's product subscription via the WSO2
 * Cloud billing-user-api. New orgs are created with inactive subscriptions;
 * this GET is the first-login activation path.
 *
 * Returns without calling the network when `BILLING_API_BASE_URL` is unset
 * (local / non-cloud). Deduped for the SPA session so React StrictMode and
 * remounts share one in-flight (or completed) request.
 */
let activationInFlight: Promise<void> | null = null;

export function ensureBillingSubscriptionActivated(
  product: string = APP_FACTORY_BILLING_PRODUCT,
): Promise<void> {
  const base = env.billingApiBaseUrl.trim();
  if (!base) {
    return Promise.resolve();
  }
  if (!activationInFlight) {
    activationInFlight = activateBillingSubscription(base, product).catch(
      (err: unknown) => {
        // Allow a later mount to retry if billing was briefly unavailable.
        activationInFlight = null;
        throw err;
      },
    );
  }
  return activationInFlight;
}

/** @internal exported for tests */
export function resetBillingActivationForTests(): void {
  activationInFlight = null;
}

async function activateBillingSubscription(
  base: string,
  product: string,
): Promise<void> {
  const url = `${base.replace(/\/$/, "")}/api/v1/organization?product=${encodeURIComponent(product)}`;
  const headers: Record<string, string> = { Accept: "application/json" };
  const token = await getAccessToken();
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  const res = await fetch(url, { headers });
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    throw new Error(`Billing API error ${res.status}: ${text}`);
  }
}
