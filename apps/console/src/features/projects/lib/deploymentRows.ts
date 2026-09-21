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

type Component = components["schemas"]["Component"];
type Deployment = components["schemas"]["Deployment"];

// Deployments board (#216): one column per environment the platform's
// pipeline names, fed by the components list joined client-side with each
// component's release bindings. Absence is information ("what's running AND
// what isn't"), so every component gets a card in the pipeline's ENTRY
// environment even with no binding — that is where a build lands first, so a
// component with nothing bound there is a component that has not shipped.

// "Undeployed" is the distinguished status for an intentional
// spec.state == Undeploy binding (rendered if the backend ever emits it);
// any other reason is OpenChoreo's latest Ready-condition reason, where
// only "Ready" is a settled success and failure is recognizable by its
// wording.
export type StatusKind =
  | "success"
  | "error"
  | "transitional"
  | "undeployed"
  | "unknown";

export function statusKind(status: string | undefined): StatusKind {
  if (!status) return "unknown";
  if (status === "Ready") return "success";
  if (status === "Undeployed") return "undeployed";
  if (/fail|error|degraded/i.test(status)) return "error";
  return "transitional";
}

export type DeploymentCard = {
  componentName: string;
  displayName: string;
  // "notDeployed" marks a component with no binding at all; otherwise the
  // binding's status kind.
  kind: StatusKind | "notDeployed";
  deployment?: Deployment;
};

/** The board, keyed by ENVIRONMENT NAME — the platform's own name for it. A
 *  Map, not an object: environment names come from the platform, and an
 *  environment called `constructor` must not read as a card list. */
export type DeploymentBoard = Map<string, DeploymentCard[]>;

// Each binding lands in the column its own `environment` names; the console
// invents no column the platform did not. `entryEnvironment` — the first of
// the pipeline, where every completed build auto-deploys — is the one column
// that also accounts for components with NO binding, as a greyed
// "Not deployed" card: absence is information exactly where something was
// expected. Pass it from the environments list (`environments[0].name`);
// without it the board is the bindings alone.
//
// Cards keep the COMPONENTS LIST's order — the platform's own, which is also
// the design's — rather than sorting by name: a name sort put a project's API
// above the app it serves, and the Try-it-out page reads top to bottom
// (ADR-0032). A binding for a component the list does not know (a component
// just removed from the design) trails, in the order the bindings came.
export function groupDeploymentCards(
  componentItems: Component[] | null | undefined,
  deploymentItems: Deployment[] | null | undefined,
  entryEnvironment?: string,
): DeploymentBoard {
  const displayNames = new Map<string, string>();
  for (const c of componentItems ?? []) {
    displayNames.set(c.name, c.displayName || c.name);
  }
  const cardOf = (d: Deployment): DeploymentCard => {
    const componentName = d.componentName ?? "";
    return {
      componentName,
      displayName: displayNames.get(componentName) ?? componentName,
      kind: statusKind(d.status),
      deployment: d,
    };
  };

  // environment -> component -> its bindings there.
  const bindings = new Map<string, Map<string, Deployment[]>>();
  const bucket = (environment: string): Map<string, Deployment[]> => {
    const existing = bindings.get(environment);
    if (existing) return existing;
    const fresh = new Map<string, Deployment[]>();
    bindings.set(environment, fresh);
    return fresh;
  };
  if (entryEnvironment) bucket(entryEnvironment);
  for (const d of deploymentItems ?? []) {
    const into = bucket(d.environment ?? "");
    const name = d.componentName ?? "";
    into.set(name, [...(into.get(name) ?? []), d]);
  }

  const board: DeploymentBoard = new Map();
  for (const [environment, perComponent] of bindings) {
    const cards: DeploymentCard[] = [];
    const placed = new Set<string>();
    for (const c of componentItems ?? []) {
      placed.add(c.name);
      const bound = perComponent.get(c.name) ?? [];
      if (bound.length > 0) {
        cards.push(...bound.map(cardOf));
      } else if (environment === entryEnvironment) {
        cards.push({
          componentName: c.name,
          displayName: displayNames.get(c.name) ?? c.name,
          kind: "notDeployed",
        });
      }
    }
    for (const [name, ds] of perComponent) {
      if (!placed.has(name)) cards.push(...ds.map(cardOf));
    }
    board.set(environment, cards);
  }
  return board;
}

// Adaptive-poll signal (the STATUS_ACTIVE/IDLE convention, #183): a binding
// still converging — or not yet reporting a condition — keeps the fast poll.
export function deploymentsAreMoving(
  deploymentItems: Deployment[] | null | undefined,
): boolean {
  return (deploymentItems ?? []).some((d) => {
    const kind = statusKind(d.status);
    return kind === "transitional" || kind === "unknown";
  });
}
