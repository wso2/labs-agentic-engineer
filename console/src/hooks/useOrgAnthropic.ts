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

import { useQuery } from '@tanstack/react-query';
import { orgAnthropicApi, type OrgAnthropicProjection } from '../services/api/orgAnthropic';

/**
 * useOrgAnthropic fetches the per-org Anthropic projection.
 *
 * Used by the tasks-page header to gate the "Implement via Remote Agents"
 * action — disabled with a tooltip pointing at org settings when the
 * status is not `active`. The actual key bytes never reach the console;
 * this hook only sees the prefix / last4 / status projection.
 *
 * See docs/design/anthropic-key-dual-token.md §2.2.
 */
export function useOrgAnthropic(orgId: string | undefined) {
  return useQuery<OrgAnthropicProjection>({
    queryKey: ['orgAnthropic', orgId],
    queryFn: () => orgAnthropicApi.getStatus(orgId as string),
    enabled: !!orgId,
    // Soft TTL — projection changes via the settings page; the user is
    // unlikely to flip back to the tasks page faster than a fresh fetch.
    staleTime: 60 * 1000,
  });
}
