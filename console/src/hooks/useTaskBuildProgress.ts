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

// useTaskBuildProgress mirrors useTaskAgentProgress but reads from
// /progress/build, which surfaces synthetic build_step lines derived
// from WorkflowRun.Status.Tasks[]. Active during `building`; stops on
// final:true.
export function useTaskBuildProgress(
  orgId: string | undefined,
  projectId: string | undefined,
  taskId: string | undefined,
  taskStatus: TaskStatus | undefined,
) {
  // Fetch once for any task past merged so the build_step feed shows
  // historical builds; only poll while the task is actively building.
  const enabled = !!orgId && !!projectId && !!taskId
    && (taskStatus === 'merged' || taskStatus === 'building'
        || taskStatus === 'deployed' || taskStatus === 'failed');
  const isLive = taskStatus === 'building' || taskStatus === 'merged';

  const { lines, final, isLoading, error } = useCursorPolling({
    queryKey: ['taskBuildProgress', orgId, projectId, taskId],
    fetcher: (cursor) => api.getTaskBuildProgress(orgId!, projectId!, taskId!, cursor),
    enabled,
    isLive,
    taskIdentity: taskId,
  });

  return { lines, final, isLoading, error };
}
