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

/**
 * Helpers for calling the agents-service tech-lead routes directly.
 * Mirrors helpers/architect.ts; reuses the same Service JWT.
 *
 * Pre-req: kubectl port-forward
 *   svc/app-factory-agents-service 13400:3400
 *   -n dp-wso2cloud-app-factory-development-bad5f211
 */

import { getServiceToken } from './architect';

const AGENTS_URL = process.env.AGENTS_SERVICE_URL || 'http://localhost:13400';

export type SlimComponent = {
  name: string;
  componentType: string;
  language: string;
  dependsOn: string[];
};

export type ExistingTaskSummary = {
  issueNumber?: number;
  title: string;
  componentName: string;
  status: string;
};

export type ValidatorDiffContext = {
  added: string[];
  contractAffectedModified: string[];
  removed: string[];
};

export type TechLeadPlanInput = {
  projectName: string;
  spec: string;
  slimDesign: SlimComponent[];
  specDiff?: string;
  designDiff?: string;
  existingTasks?: ExistingTaskSummary[];
  mode: 'fresh' | 'incremental';
  diff?: ValidatorDiffContext;
};

export type TechLeadDetailItem = {
  taskId: string;
  componentName: string;
  title: string;
  rationale: string;
  designSlice: string;
  depSummaries: SlimComponent[];
  existingTitlesForComponent: { title: string; status: string }[];
};

export type TechLeadDetailInput = {
  projectName: string;
  spec: string;
  items: TechLeadDetailItem[];
};

export async function callTechLeadPlan(
  input: TechLeadPlanInput,
): Promise<Response> {
  const token = await getServiceToken();
  return fetch(`${AGENTS_URL}/v1/agents/tech-lead/plan`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'text/event-stream',
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify(input),
  });
}

export async function callTechLeadDetail(
  input: TechLeadDetailInput,
): Promise<Response> {
  const token = await getServiceToken();
  return fetch(`${AGENTS_URL}/v1/agents/tech-lead/detail`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'text/event-stream',
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify(input),
  });
}
