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

import { api } from '../services/api';
import type { TaskStatus } from '../services/api';
import { useCursorPolling } from './useCursorPolling';

// useTaskAgentProgress streams the coding-agent's NDJSON output from
// the BFF's /progress/agent endpoint, accumulating lines locally and
// echoing the cursor on each poll. Polling stops once the response
// returns final:true OR the task moves past `in_progress`.
export function useTaskAgentProgress(
  orgId: string | undefined,
  projectId: string | undefined,
  taskId: string | undefined,
  taskStatus: TaskStatus | undefined,
) {
  // Fetch the agent feed for any task that ever ran (past `pending`),
  // not just in-flight ones — the design intends `final:true` to freeze
  // a populated historical feed. We always do at least one fetch; only
  // polling is gated on the active phase.
  const enabled = !!orgId && !!projectId && !!taskId
    && taskStatus !== undefined
    && taskStatus !== 'pending'
    && taskStatus !== 'on_hold';
  const isLive = taskStatus === 'in_progress';

  const { lines, phase, final, isLoading, error } = useCursorPolling({
    queryKey: ['taskAgentProgress', orgId, projectId, taskId],
    fetcher: (cursor) => api.getTaskAgentProgress(orgId!, projectId!, taskId!, cursor),
    enabled,
    isLive,
    taskIdentity: taskId,
    trackPhase: true,
  });

  return { lines, phase, final, isLoading, error };
}
