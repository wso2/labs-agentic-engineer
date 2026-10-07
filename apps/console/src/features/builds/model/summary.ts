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

import type { DeployStage } from "../../deploy/api/deploy";
import type { ValidationOutcome } from "./validation";

// The Build card's lines about where its version stands beyond the build: its
// rollout, and its validation (which has a card of its own). The rollout line
// is copied from the old console's build page (deploymentNote). The deploy aggregate names the
// version that reached the write target; every state it can be in gets its
// own sentence, so the card never says "deploys as its tasks merge" while the
// version is already rolling out.

export function rolloutLine(
  version: string,
  deploy: Pick<DeployStage, "status" | "version"> | undefined,
  parked: boolean,
): string {
  if (parked) return `${version} is built and waits for its dependencies' values before it deploys.`;
  if (deploy?.version !== version) return `${version} deploys as its tasks merge.`;
  switch (deploy.status) {
    case "deployed":
      return `${version} is live.`;
    case "deploying":
      return `${version} is rolling out now.`;
    case "failed":
      return `${version} failed to deploy.`;
    default:
      return `${version} deploys as its tasks merge.`;
  }
}

/**
 * The line pointing to the version's validation: its result once settled,
 * that it is under way, or when it will run.
 */
export function validationLine(
  version: string,
  outcome: Pick<ValidationOutcome, "passed" | "total" | "failing"> | null,
  validating: boolean,
): string {
  if (outcome) {
    if (outcome.failing.length > 0) {
      return `${version}'s validation: ${outcome.passed} of ${outcome.total} scenarios pass, ${outcome.failing.length} failing.`;
    }
    return `${version} passed validation: all ${outcome.total} scenarios.`;
  }
  if (validating) return `${version} is being validated.`;
  return `${version} is validated once every task has merged and it deploys.`;
}
