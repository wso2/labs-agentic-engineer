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
import { api } from '../services/api';
import type { TaskStatus, TaskStatusResponse } from '../services/api';

const TERMINAL: TaskStatus[] = ['deployed', 'rejected', 'failed', 'abandoned'];

const ACTIVE_INTERVAL_MS = 5_000;

// useTaskStatus polls /tasks/{id}/status at 5s while non-terminal,
// stops once the task settles. Tab-visibility gating comes from the
// QueryClient default refetchIntervalInBackground:false.
export function useTaskStatus(orgId: string | undefined, projectId: string | undefined, taskId: string | undefined) {
  return useQuery<TaskStatusResponse>({
    queryKey: ['taskStatus', orgId, projectId, taskId],
    queryFn: () => api.getTaskStatus(orgId!, projectId!, taskId!),
    enabled: !!orgId && !!projectId && !!taskId,
    refetchInterval: (q) => {
      const data = q.state.data;
      if (!data || !data.task) return ACTIVE_INTERVAL_MS;
      return TERMINAL.includes(data.task.status) ? false : ACTIVE_INTERVAL_MS;
    },
  });
}
