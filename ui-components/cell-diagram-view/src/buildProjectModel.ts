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

import type { Component, ComponentType, Connection, Project } from '@wso2/cell-diagram';

/**
 * One unified dependency entry. The diagram renders `component` /
 * `org-service` siblings as in-cell links and `external` / `org-service`
 * connections as chain-link nodes *outside* the cell. Mirrors the architect's
 * `Dependency` zod schema and the BFF's `models.Dependency`.
 */
export interface CellDiagramDependency {
  kind: 'component' | 'org-service' | 'external' | 'platform-resource' | string;
  name: string;
  description?: string;
  status?: string;
  resourceType?: string;
}

/**
 * Legacy external-API shape (pre-unified-model). Still accepted on read so old
 * designs render; new designs carry these under `dependencies` instead.
 */
export interface CellDiagramDependentApi {
  name: string;
  url: string;
  description?: string;
  authentication?: string;
}

/**
 * Structural shape consumed by {@link buildProjectModel}. Any object with these
 * fields (e.g. the console's `DesignComponent` API type) is acceptable.
 * `dependencies` is the unified model; `dependsOn` / `dependentApis` are
 * legacy read-compat (used only when `dependencies` is absent).
 */
export interface CellDiagramComponent {
  name: string;
  componentType: string;
  language?: string;
  dependencies?: CellDiagramDependency[];
  dependsOn?: string[];
  dependentApis?: CellDiagramDependentApi[];
}

const TYPE_MAP: Record<string, ComponentType> = {
  'web-app': 'web-app' as ComponentType,
  service: 'service' as ComponentType,
};

const PROJECT_ID = 'project';
// Synthetic "project" segment used in the dependent-API connection id. The
// cell-diagram lib treats any Connection whose id's project segment differs
// from the current project's id as external and lays it out on the east
// bound. Keeping a fixed value here groups all external APIs under one
// virtual umbrella in the diagram.
const EXTERNAL_API_PROJECT_SEGMENT = 'external-apis';

function dependentApiConnection(api: CellDiagramDependentApi): Connection {
  const tooltipParts: string[] = [api.url];
  if (api.description) tooltipParts.push(api.description);
  if (api.authentication) tooltipParts.push(`auth: ${api.authentication}`);
  return {
    id: `default:${EXTERNAL_API_PROJECT_SEGMENT}:${api.name}`,
    label: api.name,
    tooltip: tooltipParts.join(' — '),
  };
}

function externalDependencyConnection(dep: CellDiagramDependency): Connection {
  const tooltipParts: string[] = [];
  if (dep.description) tooltipParts.push(dep.description);
  tooltipParts.push(dep.kind);
  if (dep.status && dep.status !== 'resolved') tooltipParts.push(`status: ${dep.status}`);
  return {
    id: `default:${EXTERNAL_API_PROJECT_SEGMENT}:${dep.name}`,
    label: dep.name,
    tooltip: tooltipParts.join(' — ') || dep.name,
  };
}

// Sibling-component dependency names (the in-cell edges). Prefers the unified
// `dependencies` (kind: component); falls back to legacy `dependsOn`.
function siblingNames(comp: CellDiagramComponent): string[] {
  if (Array.isArray(comp.dependencies)) {
    return comp.dependencies.filter((d) => d.kind === 'component').map((d) => d.name);
  }
  return comp.dependsOn || [];
}

export function buildProjectModel(components: CellDiagramComponent[]): Project {
  const mapped: Component[] = components.map((comp) => {
    const siblings = siblingNames(comp);
    const siblingConnections: Connection[] = siblings.map((depName) => ({
      id: `default:${PROJECT_ID}:${depName}`,
      label: depName,
      onPlatform: true,
    }));
    // External nodes: unified external + org-service + platform-resource deps
    // (each is a backing dependency outside the component's own code), else
    // legacy dependentApis. platform-resource (a provisioned db/cache/queue)
    // renders as a chain-link node just like an external API or org-service.
    const externalConnections: Connection[] = Array.isArray(comp.dependencies)
      ? comp.dependencies
          .filter(
            (d) =>
              d.kind === 'external' ||
              d.kind === 'org-service' ||
              d.kind === 'platform-resource',
          )
          .map(externalDependencyConnection)
      : (comp.dependentApis || []).map(dependentApiConnection);

    return {
      id: comp.name,
      label: comp.name,
      version: '1.0.0',
      type: TYPE_MAP[comp.componentType] ?? ('service' as ComponentType),
      buildPack: comp.language,
      services:
        comp.componentType === 'web-app'
          ? {
              [`${comp.name}:web`]: {
                id: `${comp.name}:web`,
                label: 'WebApp',
                type: 'HTTP',
                dependencyIds: siblings.map((dep) => `${dep}:api`),
                deploymentMetadata: {
                  gateways: { internet: { isExposed: true }, intranet: { isExposed: false } },
                },
              },
            }
          : comp.componentType === 'service'
            ? {
                [`${comp.name}:api`]: {
                  id: `${comp.name}:api`,
                  label: 'API',
                  type: 'HTTP',
                  dependencyIds: siblings.map((dep) => `${dep}:api`),
                  deploymentMetadata: {
                    gateways: { internet: { isExposed: false }, intranet: { isExposed: false } },
                  },
                },
              }
            : {},
      connections: [...siblingConnections, ...externalConnections],
    };
  });

  return {
    id: PROJECT_ID,
    name: 'Architecture',
    modelVersion: '0.2.0',
    components: mapped,
  };
}
