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

import { env } from '../../config/env';
import { getToken } from './rest';

export interface BillingOrg {
  id?: string;
  name?: string;
  subscription?: unknown;
  [key: string]: unknown;
}

export async function fetchBillingOrg(product: string): Promise<BillingOrg> {
  const base = env.BILLING_API_BASE_URL;
  if (!base) throw new Error('BILLING_API_BASE_URL is not configured');

  const url = `${base}/api/v1/organization?product=${encodeURIComponent(product)}`;
  const headers: Record<string, string> = { Accept: 'application/json' };

  const token = await getToken();
  if (token) headers.Authorization = `Bearer ${token}`;

  const res = await fetch(url, { headers });
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    throw new Error(`Billing API error ${res.status}: ${text}`);
  }
  return res.json() as Promise<BillingOrg>;
}
