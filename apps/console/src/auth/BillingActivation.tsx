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

import { useEffect } from "react";
import { ensureBillingSubscriptionActivated } from "../api/billing";

/**
 * After a signed-in session exists, fire the cloud billing first-login
 * activation once (no-op when BILLING_API_BASE_URL is unset). Failures are
 * logged only — project create will still surface a 402 if activation never
 * succeeded.
 */
export function BillingActivation(): null {
  useEffect(() => {
    void ensureBillingSubscriptionActivated().catch((err: unknown) => {
      console.warn("[billing] subscription activation failed:", err);
    });
  }, []);
  return null;
}
