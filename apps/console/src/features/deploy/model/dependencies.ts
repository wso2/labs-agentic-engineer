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

// The values an environment's dependencies read: whether each has them (the
// board's "Xero needs values"), and what the Configure card asks for.
//
// Readiness is the platform's answer per environment, and the deploy gate's
// own read: it lists exactly the external dependencies the PROJECT supplies
// values for. One the organization registered keeps its values on the org's
// record and is left out of it; the design read says which those are.

type DependencyReadiness = components["schemas"]["ProjectDependencyReadiness"];
type ValueState = components["schemas"]["ExternalDependencyValueState"];
type ComponentDependencies = components["schemas"]["ComponentDependencies"];
type ConfigKey = components["schemas"]["ConfigKey"];

export interface DependencyLine {
  name: string;
  state: ValueState;
  /** Every value it reads is set here. */
  ready: boolean;
  /** "Xero connected", "Xero needs values". */
  label: string;
}

/** One line per dependency the project supplies values for, in the platform's order. */
export function dependencyLines(readiness: DependencyReadiness): DependencyLine[] {
  return readiness.dependencies.map((d) => {
    const ready = d.state === "configured";
    return { name: d.name, state: d.state, ready, label: ready ? `${d.name} connected` : `${d.name} needs values` };
  });
}

/** The dependencies still missing values, by name; empty when every one has them. */
export function missingValues(lines: readonly DependencyLine[]): string[] {
  return lines.filter((l) => !l.ready).map((l) => l.name);
}

/** "Xero", "Xero and Stripe", "Xero, Stripe and Slack". */
export function nameList(names: readonly string[]): string {
  if (names.length <= 1) return names[0] ?? "";
  return `${names.slice(0, -1).join(", ")} and ${names.at(-1)}`;
}

export interface ConfigureDependency {
  name: string;
  state: ValueState;
  /** The values it reads, each once across the components that use it; a key secret anywhere is secret. */
  keys: ConfigKey[];
}

function isExternal(kind: string): boolean {
  return kind === "external";
}

/**
 * What the Configure card asks for in one environment: each dependency the
 * project supplies values for, with the keys the design declares for it. The
 * same dependency declared by several components is asked for once, its keys
 * merged by name (as the platform merges them, names compared without case).
 */
export function configureDependencies(
  readiness: DependencyReadiness,
  design: readonly ComponentDependencies[],
): ConfigureDependency[] {
  return readiness.dependencies.map((d) => {
    const keys: ConfigKey[] = [];
    for (const component of design) {
      for (const dep of component.dependencies ?? []) {
        if (!isExternal(dep.kind) || dep.name.toLowerCase() !== d.name.toLowerCase()) continue;
        for (const key of dep.config ?? []) {
          const seen = keys.findIndex((k) => k.key === key.key);
          if (seen === -1) keys.push({ ...key });
          else if (key.secret) keys[seen] = { ...keys[seen]!, secret: true };
        }
      }
    }
    return { name: d.name, state: d.state, keys };
  });
}

/**
 * The external dependencies whose values the organization holds (copied from
 * a Registered External resource), by name, once each. They need nothing from
 * the project in any environment.
 */
export function orgHeldDependencies(design: readonly ComponentDependencies[]): string[] {
  const names = new Set<string>();
  for (const component of design) {
    for (const dep of component.dependencies ?? []) {
      if (isExternal(dep.kind) && dep.resourceRef) names.add(dep.name);
    }
  }
  return [...names].sort((a, b) => a.localeCompare(b));
}

/** Whether every value the form asks for is filled: the platform replaces them all. */
export function valuesComplete(keys: readonly ConfigKey[], values: Readonly<Record<string, string>>): boolean {
  return keys.length > 0 && keys.every((k) => (values[k.key] ?? "").trim() !== "");
}
