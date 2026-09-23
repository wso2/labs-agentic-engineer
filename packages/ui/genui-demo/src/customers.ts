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

import { useCallback, useEffect, useState } from "react";

export interface Customer {
  id: string;
  name: string;
  contactName: string;
  contactEmail: string;
  contactPhone?: string;
}

/** What the Customers spec reads at /customers. */
export interface CustomersState {
  items: Customer[];
  total: number;
}

/**
 * Host-side data for generated UIs: loads GET /api/customers and hands the
 * list to a view as state. A spec cannot fetch; it can only bind to what the
 * host provides.
 */
export function useCustomers(): { customers: CustomersState; reload: () => Promise<void> } {
  const [customers, setCustomers] = useState<CustomersState>({ items: [], total: 0 });
  const reload = useCallback(async () => {
    try {
      const response = await fetch("/api/customers");
      if (!response.ok) return;
      const items = (await response.json()) as Customer[];
      setCustomers({ items, total: items.length });
    } catch {
      // The list stays as it was; the demo has no error surface for loading.
    }
  }, []);
  useEffect(() => {
    void reload();
  }, [reload]);
  return { customers, reload };
}
