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

import type { Stage } from '@asdlc/project-status';
import type { ComponentTask, ProjectStatus } from '../services/api';

const ACTIVE_TASK_STATUSES = new Set([
  'pending',
  'on_hold',
  'in_progress',
  'ready_for_review',
  'merged',
]);

export function buildProjectStages(
  status: ProjectStatus | undefined,
  tasks: ComponentTask[],
): Stage[] {
  const specStatus = status?.specStatus ?? '';
  const designStatus = status?.designStatus ?? '';
  const hasSpec = status?.hasSpec ?? false;
  const hasDesign = status?.hasDesign ?? false;

  const requirements: Stage = {
    id: 'requirements',
    name: 'Requirements',
    iteration: hasSpec ? 1 : 0,
    state:
      specStatus === 'generating' ? 'active'
      : hasSpec || specStatus === 'approved' || specStatus === 'draft' ? 'done'
      : 'pending',
    headline:
      specStatus === 'generating' ? 'Drafting'
      : hasSpec ? 'Spec ready'
      : 'Not started',
    help:
      specStatus === 'generating' ? 'Hang tight — your requirements are being drafted'
      : hasSpec ? 'Review and edit the requirements before continuing'
      : 'Describe what you want to build to get started',
  };

  const architecture: Stage = {
    id: 'architecture',
    name: 'Architecture',
    iteration: hasDesign ? 1 : 0,
    state:
      designStatus === 'generating' ? 'active'
      : hasDesign || designStatus === 'approved' || designStatus === 'draft' ? 'done'
      : 'pending',
    headline:
      designStatus === 'generating' ? 'Generating'
      : hasDesign ? 'Design ready'
      : 'Awaits requirements',
    help:
      designStatus === 'generating' ? 'Hang tight — your architecture is being generated'
      : hasDesign ? 'Review the architecture and start generating tasks when ready'
      : 'Approve the requirements to begin generating the architecture',
  };

  const tasksTotal = tasks.length;
  const tasksDone = tasks.filter((t) => t.status === 'merged' || t.status === 'deployed').length;
  const tasksFailed = tasks.some((t) => t.status === 'failed' || t.status === 'rejected');
  const tasksActive = tasks.some((t) => ACTIVE_TASK_STATUSES.has(t.status));
  const tasksAllDeployed = tasksTotal > 0 && tasks.every((t) => t.status === 'deployed');

  const tasksStage: Stage = {
    id: 'tasks',
    name: 'Tasks & Code',
    iteration: tasksDone,
    state:
      tasksActive ? 'active'
      : tasksAllDeployed ? 'done'
      : tasksFailed ? 'blocked'
      : tasksTotal === 0 ? 'pending'
      : 'pending',
    headline:
      tasksTotal === 0 ? 'Awaits design'
      : tasksActive ? 'Generating'
      : `${tasksDone} / ${tasksTotal} done`,
    help:
      tasksFailed ? 'A task failed — review and retry to keep building'
      : tasksActive ? 'Generating tasks and code — this can take a few minutes'
      : tasksAllDeployed ? 'All tasks complete and deployed'
      : tasksTotal === 0 ? 'Approve the architecture to start generating tasks'
      : `${tasksTotal - tasksDone} task(s) left — review and ship the remaining work`,
  };

  const deployedCount = tasks.filter((t) => t.status === 'deployed').length;
  const buildingCount = tasks.filter((t) => t.status === 'building').length;
  const buildFailed = tasks.some((t) => t.status === 'failed');

  const deployment: Stage = {
    id: 'deployment',
    name: 'Deployment',
    iteration: deployedCount,
    state:
      buildingCount > 0 ? 'active'
      : tasksAllDeployed ? 'done'
      : buildFailed ? 'blocked'
      : 'pending',
    headline:
      buildingCount > 0 ? `${buildingCount} building`
      : tasksAllDeployed ? 'Live'
      : deployedCount > 0 ? `${deployedCount} / ${tasksTotal} live`
      : 'Awaits tasks',
    help:
      buildFailed ? 'A build failed — check the logs and rebuild to unblock deployment'
      : buildingCount > 0 ? 'Builds in flight — your app will be live shortly'
      : tasksAllDeployed ? 'Your app is live — open it to see it working'
      : deployedCount > 0 ? `${tasksTotal - deployedCount} component(s) still building`
      : 'Finish the tasks above to begin deploying',
  };

  return [requirements, architecture, tasksStage, deployment];
}
