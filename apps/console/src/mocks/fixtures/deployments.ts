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

import type { components } from "../../generated/aep-api";
import type { ProjectScenario } from "./project";

type ProjectDeployment = components["schemas"]["ProjectDeployment"];
type ProjectDeploymentDetail = components["schemas"]["ProjectDeploymentDetail"];
type DeploymentComponent = components["schemas"]["DeploymentComponent"];
type RuntimeLogs = components["schemas"]["RuntimeLogs"];

// Fixtures for the Deployments board and the deployment detail page (ADR-0020).
// Keyed by the same project scenario as everything else, so `aep:mock:project`
// drives all four surfaces together and they can never tell different stories.

const V1_COMMIT = { sha: "4e8a0d61c7b2", branch: "main" };

/** v1 live in development, validation passed — the promotable case. */
const devLive: ProjectDeployment = {
  id: "dep-dev-v1",
  tag: "v1",
  milestoneTitle: "Browse the catalog and check out",
  environment: "development",
  status: "live",
  deployedAt: "2026-07-12T10:54:00Z",
  durationSeconds: 221,
  commit: V1_COMMIT,
  validation: { state: "passed", passed: 24, total: 24 },
};

/** Same environment, still being judged — the promote gate must hold here. */
const devValidating: ProjectDeployment = {
  id: "dep-dev-v2",
  tag: "v2",
  milestoneTitle: "Payment provider and receipts",
  environment: "development",
  status: "validating",
  deployedAt: "2026-07-11T15:20:00Z",
  durationSeconds: 178,
  commit: { sha: "7b31d04e9f16", branch: "main" },
  validation: { state: "running", passed: 18, total: 24 },
};

/** A deployment that never came up. Validation never ran — not 0 / 0. */
const devFailed: ProjectDeployment = {
  id: "dep-dev-failed",
  tag: "v1",
  milestoneTitle: "Browse the catalog and check out",
  environment: "development",
  status: "failed",
  deployedAt: "2026-07-09T11:06:00Z",
  durationSeconds: 51,
  commit: { sha: "2c07ab8f5d31", branch: "main" },
  validation: { state: "not_run" },
};

/** Replaced by a later deployment in the same environment. */
const devSuperseded: ProjectDeployment = {
  id: "dep-dev-superseded",
  tag: "v1",
  milestoneTitle: "Browse the catalog and check out",
  environment: "development",
  status: "superseded",
  deployedAt: "2026-07-08T09:48:00Z",
  durationSeconds: 192,
  commit: { sha: "9f4e112a7c60", branch: "main" },
  validation: { state: "failed", passed: 22, total: 24 },
};

const prodLive: ProjectDeployment = {
  id: "dep-prod-v1",
  tag: "v1",
  milestoneTitle: "Browse the catalog and check out",
  environment: "production",
  status: "live",
  deployedAt: "2026-07-12T08:30:00Z",
  durationSeconds: 264,
  commit: V1_COMMIT,
  validation: { state: "passed", passed: 24, total: 24 },
};

// The history every populated scenario shares, newest first. devLive leads, so
// the board's DEFAULT state is the promotable one — the question the page is
// opened with. The validating deployment is reachable via the `deploying`
// scenario, where it is what is actually happening.
const devHistory = [devLive, devValidating, devFailed, devSuperseded];

export const projectDeployments: Record<
  Exclude<ProjectScenario, "error">,
  ProjectDeployment[]
> = {
  // Nothing built means nothing deployed — both environment cards empty and no
  // table. This is the state the page must open in for a new project.
  fresh: [],
  spec: [],
  "spec-failed": [],
  "repo-error": [],
  // A build in flight has not shipped anything new yet; v1 is what is running.
  building: [devLive, devSuperseded],
  // Mid-rollout: something is on its way into development.
  deploying: [
    { ...devValidating, status: "deploying", validation: { state: "not_run" } },
    devLive,
  ],
  // Development live and promotable, with the full history behind it.
  deployed: devHistory,
  // The rollout that did not come up. Nothing is live in development, so the
  // card shows its failed state rather than an empty one.
  "deploy-failed": [devFailed, devSuperseded],
};

// Production is deliberately absent from every scenario above, so the "Nothing
// deployed" production card — the design's second column, and the state a real
// project spends most of its life in — is what a reader meets by default.
// Reach the promoted case with:
//   localStorage.setItem('aep:mock:deployments', 'production')
export const projectDeploymentsWithProduction: ProjectDeployment[] = [
  prodLive,
  ...devHistory,
];

const componentsFor = (tag: string): DeploymentComponent[] => [
  {
    name: "storefront",
    kind: "webapp",
    status: "Ready",
    releaseName: `storefront-${tag}-4e8a0d6`,
    endpointUrl: "https://shop.dev.demo-shop.localhost",
  },
  {
    name: "catalog-api",
    kind: "service",
    status: "Ready",
    releaseName: `catalog-api-${tag}-4e8a0d6`,
    endpointUrl: "https://api.dev.demo-shop.localhost/catalog",
  },
  {
    name: "orders-api",
    kind: "service",
    status: "Ready",
    releaseName: `orders-api-${tag}-4e8a0d6`,
    endpointUrl: "https://api.dev.demo-shop.localhost/orders",
  },
  {
    // No endpoint: a worker has no front door, so its row must offer nothing
    // rather than a dead button.
    name: "order-worker",
    kind: "worker",
    status: "Ready",
    releaseName: `order-worker-${tag}-4e8a0d6`,
  },
];

const ALL_DEPLOYMENTS: ProjectDeployment[] = [
  prodLive,
  ...devHistory,
  { ...devValidating, status: "deploying", validation: { state: "not_run" } },
];

export function deploymentDetail(
  deploymentId: string,
): ProjectDeploymentDetail | null {
  const deployment = ALL_DEPLOYMENTS.find((d) => d.id === deploymentId);
  if (!deployment) return null;
  // A failed deployment put nothing in the environment — its component list is
  // empty, which is what the detail page's own empty state is written against.
  const components =
    deployment.status === "failed" ? [] : componentsFor(deployment.tag);
  return { deployment, components };
}

// Named after the component that is actually being read, so an expanded
// storefront row does not print "starting catalog-api".
const logLines = (
  componentName: string,
): Array<{ timestamp: string; level: string; message: string }> => {
  const route = componentName.replace(/-api$/, "");
  return [
    { timestamp: "2026-07-10T10:52:31Z", level: "info", message: `starting ${componentName} v1 (4e8a0d6)` },
    { timestamp: "2026-07-10T10:52:33Z", level: "info", message: `connected to postgres ${route}@dev` },
    { timestamp: "2026-07-10T10:53:12Z", level: "info", message: "secret stripe-key mounted from OpenBao" },
    { timestamp: "2026-07-10T10:53:47Z", level: "info", message: "listening on :8080 — readiness probe passing" },
    { timestamp: "2026-07-10T11:04:02Z", level: "info", message: `GET /${route}/products 200 18ms` },
    { timestamp: "2026-07-10T11:04:26Z", level: "warn", message: `GET /${route}/products?q= 200 412ms — slow query` },
    { timestamp: "2026-07-10T11:07:51Z", level: "info", message: `POST /${route}/products 201 34ms` },
  ];
};

/**
 * A component's runtime log over the requested window.
 *
 * `order-worker` returns nothing on purpose: "this window is empty" is a state
 * the page has to render as a note, not as an error, and it needs a fixture to
 * be reachable from.
 */
export function runtimeLogs(
  componentName: string,
  windowSeconds: number,
): RuntimeLogs {
  if (componentName === "order-worker") {
    return { entries: [], windowSeconds };
  }
  const lines = logLines(componentName);
  // A shorter window returns fewer lines, so the picker visibly does something.
  const kept = windowSeconds <= 900 ? lines.slice(-3) : lines;
  return {
    entries: kept,
    windowSeconds,
    truncated: kept.length < lines.length,
  };
}
