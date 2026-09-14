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
 * Which preflight items hold the version back, folded to one row per
 * dependency.
 *
 * Preflight reports per COMPONENT, so a dependency three components share
 * arrives three times; the user resolves it once, on its own definition, so the
 * dialog says it once and names who waits on it.
 *
 * Only the resolution kinds appear. An external dependency's config values and
 * a platform resource's approval ride along with the build request and are
 * enforced at the deploy gate (ADR-0023), so they never hold a version back.
 */

import type { components } from "../../../generated/aep-api";

type PreflightItem = components["schemas"]["PreflightItem"];

/** The kinds that block the cut: the design cannot name the dependency yet. */
const BLOCKING_KINDS = new Set<PreflightItem["kind"]>([
  "external-unresolved",
  "external-spec",
  "org-service",
]);

/** One dependency the version waits on. */
export interface BlockingDependency {
  name: string;
  description: string;
  /** The components that declare it, sorted — shown only when more than one. */
  usedBy: string[];
}

export function blockingDependencies(
  items: readonly PreflightItem[],
): BlockingDependency[] {
  const byName = new Map<string, PreflightItem[]>();
  for (const item of items) {
    if (!BLOCKING_KINDS.has(item.kind)) continue;
    const bucket = byName.get(item.dependency);
    if (bucket) bucket.push(item);
    else byName.set(item.dependency, [item]);
  }
  return [...byName.entries()]
    .map(([name, group]) => ({
      name,
      description: group[0]!.description,
      usedBy: [...new Set(group.map((i) => i.component))].sort((a, b) =>
        a.localeCompare(b),
      ),
    }))
    .sort((a, b) => a.name.localeCompare(b.name));
}
