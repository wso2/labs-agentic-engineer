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

/**
 * An environment as the platform describes it: its immutable name, how it is
 * shown, whether it validates, and what it promotes to. The console never
 * decides any of this — it reads the list the BFF served, in the order it
 * served it.
 */
export type EnvironmentInfo = components["schemas"]["EnvironmentDTO"];

export type StepKind = "deployment" | "validation" | "promote";

export interface FlowStepSpec {
  kind: StepKind;
  /** 1-based, as the card numbers them. */
  index: number;
  /** The promote step's target; absent on the others. */
  promotesTo?: string;
}

/** True when nothing follows this environment — the promotion target is the
 *  only thing that says so. `isProduction` is a label, not a position. */
export function isLast(env: EnvironmentInfo): boolean {
  return !env.promotesTo;
}

/**
 * A card's steps are a consequence, never a stored number: Deployment always,
 * Validation when this environment is configured for it, Promote when
 * something follows.
 */
export function stepsFor(env: EnvironmentInfo): FlowStepSpec[] {
  const steps: FlowStepSpec[] = [{ kind: "deployment", index: 1 }];
  if (env.validation === "on") {
    steps.push({ kind: "validation", index: steps.length + 1 });
  }
  if (!isLast(env)) {
    steps.push({
      kind: "promote",
      index: steps.length + 1,
      ...(env.promotesTo ? { promotesTo: env.promotesTo } : {}),
    });
  }
  return steps;
}

export function findEnvironment(list: EnvironmentInfo[], name: string): EnvironmentInfo | undefined {
  return list.find((e) => e.name === name);
}

/** The environment's display name, or the raw segment when the list does not
 *  know it — a URL naming a dead environment still has to render something. */
export function labelOf(env: EnvironmentInfo | undefined, name: string): string {
  return env?.displayName || name;
}

/**
 * Reorder for the settings strip (plan 3). Pure: it renumbers `position` and
 * rewires every `promotesTo` to the new neighbour, because both are
 * consequences of order and a half-renumbered list would draw a broken flow.
 */
export function moveEnvironment(
  list: EnvironmentInfo[],
  from: number,
  to: number,
): EnvironmentInfo[] {
  if (from === to || from < 0 || to < 0 || from >= list.length || to >= list.length) {
    return list.map((e) => ({ ...e }));
  }
  const next = list.map((e) => ({ ...e }));
  const [moved] = next.splice(from, 1);
  if (!moved) return list.map((e) => ({ ...e }));
  next.splice(to, 0, moved);
  return next.map((e, i) => {
    const rest: EnvironmentInfo = { ...e, position: i };
    const following = next[i + 1];
    if (following) {
      rest.promotesTo = following.name;
    } else {
      delete rest.promotesTo;
    }
    return rest;
  });
}
