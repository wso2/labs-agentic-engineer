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

import type { GenUiPropsOf, GenUiTone } from "./components.js";

/** How a platform state reads on screen: the console's words and a tone. */
export interface GenUiStateLabel {
  readonly label: string;
  readonly tone: GenUiTone;
}

type TaskState = GenUiPropsOf<"TaskList">["tasks"][number]["state"];
type DeploymentState =
  GenUiPropsOf<"Deployments">["environments"][number]["state"];
type ScenarioResult =
  GenUiPropsOf<"ValidationResults">["scenarios"][number]["result"];

// Shared by every design system, so the same state never reads differently
// depending on which one renders it.
export const taskStateLabels: Record<TaskState, GenUiStateLabel> = {
  pending: { label: "Pending", tone: "neutral" },
  in_progress: { label: "In progress", tone: "info" },
  pr_sent: { label: "PR sent", tone: "info" },
  blocked: { label: "Blocked", tone: "warning" },
  merged: { label: "Merged", tone: "success" },
};

export const deploymentStateLabels: Record<DeploymentState, GenUiStateLabel> = {
  not_deployed: { label: "Not deployed", tone: "neutral" },
  deploying: { label: "Deploying", tone: "info" },
  deployed: { label: "Deployed", tone: "success" },
  failed: { label: "Failed", tone: "error" },
};

export const scenarioResultLabels: Record<ScenarioResult, GenUiStateLabel> = {
  passed: { label: "Passed", tone: "success" },
  partial: { label: "Partial", tone: "warning" },
  failed: { label: "Failed", tone: "error" },
  skipped: { label: "Skipped", tone: "neutral" },
};
