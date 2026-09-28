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
 * ProjectDesign → wso2/cell-diagram Project. Second-stage projection (derived
 * from a derived artifact — still pure and deterministic end to end). The
 * shape and id conventions follow the legacy console's buildProjectModel.ts,
 * which fed the same @wso2/cell-diagram component:
 *   - component type is "web-app" (the lib's spelling), not "webapp"
 *   - services are keyed "<name>:api" / "<name>:web"; dependencyIds point at
 *     sibling ":api" services
 *   - connection ids are "default:<projectSegment>:<name>"; a segment other
 *     than "project" renders as EXTERNAL (east bound), so datastores and
 *     off-platform systems group under "external-apis"
 */

import type { ProjectDesign } from "./types.js";

// Minimal structural mirror of @wso2/cell-diagram's Project model — the lib
// isn't a dependency here; the diagram team's renderer owns the real types.
export interface CellDiagramProject {
  id: string;
  name: string;
  modelVersion: string;
  components: CellDiagramComponent[];
}

export interface CellDiagramComponent {
  id: string;
  label: string;
  version: string;
  type: string; // the lib's spelling for known kinds; unknown kinds render as a service box
  buildPack?: string;
  services: Record<string, CellDiagramService>;
  connections: CellDiagramConnection[];
}

export interface CellDiagramService {
  id: string;
  label: string;
  type: string;
  dependencyIds: string[];
  deploymentMetadata: {
    gateways: { internet: { isExposed: boolean }; intranet: { isExposed: boolean } };
  };
}

export interface CellDiagramConnection {
  id: string;
  label: string;
  // "datastore" | "http" | ... — matches @wso2/cell-diagram's ConnectionType.
  // Drives isConnectorConnection: "datastore"/"connector" route to the South
  // bound (or an owned in-cell store); anything else is an East external API.
  type?: string;
  onPlatform?: boolean;
  tooltip?: string;
}

const PROJECT_ID = "project";
const EXTERNAL_SEGMENT = "external-apis";

// Authored ProjectDesignComponent.type → the cell-diagram lib's kind spelling.
// Unmapped kinds fall back to "service" (a plain service box).
const CELL_KIND_BY_TYPE: Record<string, string> = {
  webapp: "web-app",
  "ai-agent": "ai-agent",
};

/** "datastore://postgres" → "postgres" */
const targetOf = (connId: string): string => connId.slice(connId.indexOf("://") + 3);

export function toCellDiagramProject(design: ProjectDesign): CellDiagramProject {
  const componentIds = new Set(design.components.map((c) => c.id));

  const components: CellDiagramComponent[] = design.components.map((comp) => {
    // Split edges: targets that are sibling components stay in-project;
    // everything else (datastores, external systems) goes east as external.
    const internal: string[] = [];
    const connections: CellDiagramConnection[] = [];
    for (const conn of comp.connections) {
      const target = targetOf(conn.id);
      if (componentIds.has(target)) {
        internal.push(target);
        connections.push({ id: `default:${PROJECT_ID}:${target}`, label: target, onPlatform: true });
      } else {
        connections.push({
          id: `default:${EXTERNAL_SEGMENT}:${target}`,
          label: target,
          // Forward Stage 1's edge type + on/off-platform signal — it already
          // computes the exact "datastore"/"http" strings the cell-diagram
          // lib's ConnectionType expects, so no mapping is needed here.
          type: conn.type,
          // exactOptionalPropertyTypes: omit the key rather than set it to undefined.
          ...(conn.onPlatform !== undefined ? { onPlatform: conn.onPlatform } : {}),
          tooltip: conn.onPlatform === false ? `${conn.type} (off-platform)` : conn.type,
        });
      }
    }

    const isWebapp = comp.type === "webapp";
    const serviceKey = `${comp.id}:${comp.type === "webapp" ? "web" : "api"}`;
    const exposure = comp.services?.[comp.id]?.deploymentMetadata.gateways;

    return {
      id: comp.id,
      label: comp.id,
      version: comp.version,
      // Known kinds use the lib's vocabulary; anything else renders as a plain
      // service box (the legacy TYPE_MAP fallback) — the true kind stays in
      // the ProjectDesign, this is a view-layer concession only. The vendored
      // @aep/ui-cell-diagram-react renderer treats a component's `type` as a
      // free-form display string (ParsedComponent.type?: string, no enum) and
      // never validates it against a fixed vocabulary, so "ai-agent" passes
      // straight through instead of falling back to "service".
      type: CELL_KIND_BY_TYPE[comp.type] ?? "service",
      ...(comp.build.language ? { buildPack: comp.build.language } : {}),
      services: {
        [serviceKey]: {
          id: serviceKey,
          label: isWebapp ? "WebApp" : "API",
          type: "HTTP",
          dependencyIds: internal.map((dep) => `${dep}:api`),
          deploymentMetadata: {
            gateways: {
              // Webapps face users; services carry their frontmatter exposure.
              internet: { isExposed: isWebapp ? true : (exposure?.internet.isExposed ?? false) },
              intranet: { isExposed: isWebapp ? false : (exposure?.intranet.isExposed ?? false) },
            },
          },
        },
      },
      connections,
    };
  });

  return { id: PROJECT_ID, name: design.name, modelVersion: "0.2.0", components };
}
