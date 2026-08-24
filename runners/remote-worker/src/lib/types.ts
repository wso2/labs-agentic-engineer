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
// AEP_* env vars assembled by the BFF onto the coding-agent Workload.
// No `branchName` field — the workspace clones the project's default branch
// and the agent itself creates the feature branch and opens the PR with
// `Closes #<issueNumber>` so the BFF webhook can link the PR back to the task.
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
   * WS2.6 — full URL for the credentials/refresh endpoint used during
   * workspace bootstrap. oneshot.ts sets it to the path-scoped
   * `${platformUrl}/internal/v1/executions/{executionId}/credentials/refresh`
   * (taskId carries the execution id, §9.2). Accepts the publisher CC token.
   */
  refreshUrl?: string;
  /**
   * Endpoint Spec Discovery (B1/B2) — the BFF's in-process MCP endpoint
   * (`<platform>/internal/v1/mcp`), stamped unconditionally by the BFF's
   * coding-agent Job template as AEP_MCP_URL. Paired with `mcpToken`; the
   * runner only registers the MCP server when BOTH are present.
   */
  mcpUrl?: string;
  /**
   * Token presented to the BFF MCP endpoint: the minted Thunder publisher
   * access token. Absent when mint failed.
   */
  mcpToken?: string;
  /**
   * Task kind from AEP_TASK_KIND (default "implementation"). Validation
   * tasks preload the `aep:aep-validation` skill body alongside `aep:aep`
   * so the workflow is in context at startup — description-triggered
   * loading proved unreliable for a skill the whole run depends on.
   */
  taskKind: "implementation" | "validation";
  /**
   * Developer diagnostics for this run: the SDK's own debug log, its stderr,
   * and per-token streaming frames for the watchdog. All three land in files
   * beside `claude.log`; none of them reach the progress feed.
   *
   * Stated by the entrypoint, never inferred — the same discipline as skills
   * scope. `local.ts` turns it on, because a playground run is read off disk by
   * the developer who started it. `oneshot.ts` leaves it off, because a pod's
   * files die with the pod and its debug log would hold prompt text.
   * `AEP_RUNNER_DEBUG=1` overrides for the one case that needs it: a stall that
   * only reproduces in the cluster, where a diagnostic you cannot enable is a
   * diagnostic you do not have.
   *
   * Note what does NOT depend on this flag: API retries. Those are reported on
   * every run — see progress/diagnostics.ts for why they are safe to be.
   */
  debug?: boolean;
}
