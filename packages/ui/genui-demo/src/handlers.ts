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

import { GenUiActionError, type GenUiActionHandlers } from "@aep/ui-genui";

export interface HandlerHooks {
  /** Called after a customer is created, so the host can reload its list. */
  onCustomerCreated?: () => Promise<void> | void;
}

// Most catalog actions just log here; each page shows the outcome of what
// reached them. createCustomer makes a real request.
export const createHandlers = ({ onCustomerCreated }: HandlerHooks = {}): GenUiActionHandlers => ({
  openTask: (params) => console.info("openTask", params),
  approveDependency: (params) => console.info("approveDependency", params),
  rejectDependency: (params) => console.info("rejectDependency", params),
  openDeployments: (params) => console.info("openDeployments", params),
  editExternalResource: (params) => console.info("editExternalResource", params),
  createCustomer: async (params) => {
    // The URL lives here, in the host, never in a spec. The dev server sends
    // /api/customers to the real API or to its stand-in (vite.config.ts).
    const response = await fetch("/api/customers", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        ...params,
        // An optional field left empty is not sent at all.
        contactPhone: params.contactPhone?.trim() || undefined,
      }),
    });
    if (!response.ok) {
      // Assumes the API's error body is { message, fieldErrors }; map it here
      // if the real one differs.
      const body = (await response.json().catch(() => ({}))) as {
        message?: string;
        fieldErrors?: Record<string, string>;
      };
      throw new GenUiActionError(
        body.message ?? `The server refused the request (${response.status}).`,
        body.fieldErrors ?? {},
      );
    }
    await onCustomerCreated?.();
  },
});
