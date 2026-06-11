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

export interface DispatchIdentity {
  name: string;
  email: string;
  login?: string;
}

// DispatchRequest is the input to a one-shot pod run. The values come from
// ASDLC_* env vars assembled by the Argo Workflow from the WorkflowRun's
// parameters (see app-factory-coding-agent.yaml). No `branchName` field —
// the workspace clones the project's default branch and the agent itself
// creates the feature branch and opens the PR with `Closes #<issueNumber>`
// so the BFF webhook can link the PR back to the task.
export interface DispatchRequest {
  taskId: string;
  orgId: string;
  projectId: string;
  componentName: string;
  repoUrl: string;
  bearer: string;
  identity: DispatchIdentity;
  gitServiceUrl: string;
  prompt: string;
  /** Optional correlation ID for distributed tracing. Forwarded to git-service via credhelper. */
  correlationId?: string;
  /**
   * WS2.4 — full URL for the credentials/refresh endpoint used during
   * workspace bootstrap. Defaults to the legacy
   * `${gitServiceUrl}/api/v1/credentials/refresh`; oneshot.ts overrides
   * to the path-scoped `${platformUrl}/api/v1/tasks/{taskId}/credentials/refresh`
   * when publisher cc creds are present.
   */
  refreshUrl?: string;
}
