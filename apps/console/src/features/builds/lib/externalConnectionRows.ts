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

type ComponentDependencies = components["schemas"]["ComponentDependencies"];
type ConfigKey = components["schemas"]["ConfigKey"];

export interface ExternalConnectionRow {
  id: string;
  name: string;
  description?: string;
  config: ConfigKey[];
}

export type ExternalConnectionValues = Record<
  string,
  Record<string, string>
>;

/**
 * Builds-only projection of external connection schemas. A shared external is
 * configured once, so every consumer's keys are unioned and any secret
 * declaration wins a conflicting plain declaration.
 */
export function externalConnectionRows(
  all: ComponentDependencies[] | null | undefined,
): ExternalConnectionRow[] {
  const byId = new Map<string, ExternalConnectionRow>();

  for (const component of all ?? []) {
    for (const dependency of component.dependencies ?? []) {
      if (dependency.kind !== "external") continue;

      const id = `external:${dependency.name.toLowerCase()}`;
      const existing = byId.get(id);
      if (!existing) {
        byId.set(id, {
          id,
          name: dependency.name,
          ...(dependency.description && {
            description: dependency.description,
          }),
          config: [...(dependency.config ?? [])],
        });
        continue;
      }

      if (!existing.description && dependency.description) {
        existing.description = dependency.description;
      }
      const configByKey = new Map(
        existing.config.map((key, index) => [key.key, index]),
      );
      for (const key of dependency.config ?? []) {
        const index = configByKey.get(key.key);
        if (index === undefined) {
          configByKey.set(key.key, existing.config.length);
          existing.config.push(key);
          continue;
        }
        const current = existing.config[index]!;
        if (key.secret && !current.secret) {
          existing.config[index] = { ...current, ...key, secret: true };
        }
      }
    }
  }

  return [...byId.values()].sort((a, b) => a.name.localeCompare(b.name));
}
