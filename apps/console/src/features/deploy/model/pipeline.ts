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

import type { components } from "../../../generated/aep-api";
import { dependencyLines, type DependencyLine } from "./dependencies";

// The Deploy Page's board: one column per environment the platform's pipeline
// names, in promotion order, each saying what runs there. Pure, from the
// reads the page makes (the old console's deploymentRows.ts and
// deploymentLedger.ts, trimmed to the board): the environments, the
// components, every component's deployments, the deploy aggregate, and each
// environment's dependency readiness.
//
// The platform keeps current state only: a deployment is what an environment
// runs now. The deploy aggregate names one version, the one the newest build
// rolled out to the pipeline's first environment, where every build lands;
// no read names the version anywhere else, so later columns show what runs
// without a version.

type Environment = components["schemas"]["EnvironmentDTO"];
type Component = components["schemas"]["Component"];
type Deployment = components["schemas"]["Deployment"];
type DeployStage = components["schemas"]["DeployStage"];
type DependencyReadiness = components["schemas"]["ProjectDependencyReadiness"];

/** A deployment's state, from OpenChoreo's latest Ready-condition reason. */
export type DeploymentKind = "ready" | "failed" | "converging";

export function deploymentKind(status: string | undefined): DeploymentKind {
  if (status === "Ready") return "ready";
  if (status && /fail|error|degraded/i.test(status)) return "failed";
  return "converging";
}

const KIND_LABEL: Record<string, string> = {
  "web-application": "web app",
  service: "service",
  "ai-agent": "agent",
};

/** A component's type as a person says it ("web app"); empty when the type is unknown. */
export function componentKindLabel(type: string | undefined): string {
  return (type && KIND_LABEL[type]) ?? "";
}

/** Whether any deployment is still converging, which keeps its poll close. */
export function deploymentsAreMoving(deployments: Deployment[]): boolean {
  return deployments.some((d) => deploymentKind(d.status) === "converging");
}

/**
 * The pipeline in promotion order. The platform serves it ordered and says so
 * with each environment's `position`; ordering by it keeps the board right
 * whatever order the list arrives in.
 */
export function promotionOrder(environments: readonly Environment[]): Environment[] {
  return [...environments].sort((a, b) => a.position - b.position);
}

export interface ColumnComponent {
  name: string;
  displayName: string;
  /** "web-application", "service", "ai-agent". */
  type: string | undefined;
  /** Absent in the first environment for a component that has not deployed yet. */
  deployment: Deployment | undefined;
  kind: DeploymentKind | "not-deployed";
  /** Where it is reached in this environment, once it is. */
  url: string | null;
}

export type ColumnTone = "success" | "info" | "error" | "neutral";

export interface EnvironmentColumn {
  name: string;
  label: string;
  /** The first environment, where every build lands. */
  entry: boolean;
  /** The version running here, where a read names it (the first environment only). */
  version: string | null;
  /** Whether anything is deployed here. */
  running: boolean;
  state: { label: string; tone: ColumnTone };
  components: ColumnComponent[];
  /** Each external dependency's values here; null until the read answers. */
  dependencies: DependencyLine[] | null;
  /** The environment this one promotes to; null on the last. */
  next: { name: string; label: string } | null;
}

export interface BoardInput {
  environments: readonly Environment[];
  components: readonly Component[];
  deployments: readonly Deployment[];
  deploy: Pick<DeployStage, "status" | "version"> | undefined;
  readiness: ReadonlyMap<string, DependencyReadiness | undefined>;
}

function columnComponents(input: BoardInput, environment: string, entry: boolean): ColumnComponent[] {
  const here = input.deployments.filter((d) => d.environment === environment);
  const known = new Set(input.components.map((c) => c.name));
  const rows: ColumnComponent[] = [];
  // The components list's order is the design's, so an app comes before the API it calls.
  for (const c of input.components) {
    const deployment = here.find((d) => d.componentName === c.name);
    // Absence is information only where something was expected: the first environment.
    if (!deployment && !entry) continue;
    rows.push({
      name: c.name,
      displayName: c.displayName || c.name,
      type: c.type,
      deployment,
      kind: deployment ? deploymentKind(deployment.status) : "not-deployed",
      url: deployment?.endpointUrl || null,
    });
  }
  // A deployment of a component the design no longer has still runs here.
  for (const d of here) {
    if (!d.componentName || known.has(d.componentName)) continue;
    rows.push({
      name: d.componentName,
      displayName: d.componentName,
      type: undefined,
      deployment: d,
      kind: deploymentKind(d.status),
      url: d.endpointUrl || null,
    });
  }
  return rows;
}

function foldedState(rows: ColumnComponent[]): EnvironmentColumn["state"] {
  const deployed = rows.filter((r) => r.deployment);
  if (deployed.length === 0) return { label: "Nothing running", tone: "neutral" };
  if (deployed.some((r) => r.kind === "failed")) return { label: "Deploy failed", tone: "error" };
  if (deployed.some((r) => r.kind === "converging")) return { label: "Deploying", tone: "info" };
  return { label: "Running", tone: "success" };
}

/**
 * What an environment says about itself. The first answers from the deploy
 * aggregate while it tracks a rollout, as the Builds card does, so the two
 * never disagree; every other environment (and the first when the aggregate
 * tracks none) folds its deployments.
 */
function columnState(rows: ColumnComponent[], entry: boolean, deploy: BoardInput["deploy"]): EnvironmentColumn["state"] {
  if (entry && deploy) {
    if (deploy.status === "deploying") return { label: "Deploying", tone: "info" };
    if (deploy.status === "failed") return { label: "Deploy failed", tone: "error" };
    if (deploy.status === "deployed") return { label: "Running", tone: "success" };
  }
  return foldedState(rows);
}

/** The board: a column per environment, in promotion order. */
export function environmentColumns(input: BoardInput): EnvironmentColumn[] {
  const ordered = promotionOrder(input.environments);
  const labelOf = (name: string) => ordered.find((e) => e.name === name)?.displayName || name;
  return ordered.map((env, i) => {
    const entry = i === 0;
    const components = columnComponents(input, env.name, entry);
    const readiness = input.readiness.get(env.name);
    return {
      name: env.name,
      label: env.displayName || env.name,
      entry,
      version: entry && input.deploy?.version ? input.deploy.version : null,
      running: components.some((c) => c.deployment),
      state: columnState(components, entry, input.deploy),
      components,
      dependencies: readiness ? dependencyLines(readiness) : null,
      next: env.promotesTo ? { name: env.promotesTo, label: labelOf(env.promotesTo) } : null,
    };
  });
}
