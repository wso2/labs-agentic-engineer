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

/** The product code WSO2 Cloud billing knows this platform's subscriptions by. */
const APP_FACTORY_BILLING_PRODUCT = "app-factory";

/**
 * Activate (or, once active, harmlessly refresh) the org's product
 * subscription through WSO2 Cloud's billing-user-api. A new org's
 * subscription starts inactive; this GET at sign-in is what activates it.
 *
 * Makes no call when `BILLING_API_BASE_URL` is unset (local, or any install
 * outside WSO2 Cloud). One request per SPA session: StrictMode and remounts
 * share the one in flight or done; a failure clears it, so a later mount
 * tries again.
 */
let activationInFlight: Promise<void> | null = null;

export function ensureBillingSubscriptionActivated(): Promise<void> {
  const base = env.billingApiBaseUrl.trim();
  if (!base) return Promise.resolve();
  activationInFlight ??= activateBillingSubscription(base, APP_FACTORY_BILLING_PRODUCT).catch((err: unknown) => {
    activationInFlight = null;
    throw err;
  });
  return activationInFlight;
}

/**
 * Forget the session's activation, so each test starts from none.
 * @knipkeep test seam: the module's once-per-session state, reset between tests
 */
export function resetBillingActivationForTests(): void {
  activationInFlight = null;
}

async function activateBillingSubscription(base: string, product: string): Promise<void> {
  const url = `${base.replace(/\/$/, "")}/api/v1/organization?product=${encodeURIComponent(product)}`;
  const headers: Record<string, string> = { Accept: "application/json" };
  const token = await getAccessToken();
  if (token) headers.Authorization = `Bearer ${token}`;
  const res = await fetch(url, { headers });
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    throw new Error(`Billing API error ${res.status}: ${text}`);
  }
}
