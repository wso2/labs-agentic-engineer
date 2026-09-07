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
 * Write-gate behavior for a component's `design.json` dependencies against
 * the cell. The cell is the design's source of truth (ADR-0020) and the
 * architecture skill says `dependencies[]` mirrors its edges — so a
 * dependency whose name is not a node the cell declares is an edge the
 * diagram does not draw: a database the agent discovered while enriching a
 * component and never put in the cell, most often. Caught here, while the
 * agent can still add the node, instead of shipping a diagram that hides a
 * resource the build will provision.
 *
 * Membership only — the half of the mirror one file can judge. Whether every
 * cell edge has a dependency is a whole-bundle question (#695).
 */

import { cellNodeIds, DESIGN_CELL_PATH, type DiagramBundleReader } from "./design-diagrams.js";

export interface ComponentDependencyProblem {
  code: "UNKNOWN_DEPENDENCY";
  message: string;
}

const COMPONENT_DESIGN_RE = /^specs\/design\/components\/([^/]+)\/design\.json$/;

interface Dep {
  kind: string;
  name: string;
  resourceType?: string;
}

/** The dependency entries a document carries; malformed ones are the schema gate's business. */
function dependenciesOf(content: string): Dep[] | null {
  let doc: unknown;
  try {
    doc = JSON.parse(content);
  } catch {
    return null;
  }
  if (typeof doc !== "object" || doc === null || Array.isArray(doc)) return null;
  const raw = (doc as { dependencies?: unknown }).dependencies;
  if (!Array.isArray(raw)) return [];
  const out: Dep[] = [];
  for (const d of raw) {
    if (typeof d !== "object" || d === null) continue;
    const { kind, name, resourceType } = d as Record<string, unknown>;
    if (typeof kind !== "string" || typeof name !== "string" || name === "") continue;
    out.push({ kind, name, ...(typeof resourceType === "string" ? { resourceType } : {}) });
  }
  return out;
}

/** The cell statement that would declare this dependency, by what it is. */
function declarationFor(dep: Dep): string {
  switch (dep.kind) {
    case "component":
      return `a sibling component: \`component ${dep.name} …\` inside the cell`;
    case "platform-resource":
      return dep.resourceType === "thunder-app"
        ? `the identity provider: \`east ${dep.name} as "Thunder Auth" identity-server\``
        : `a project-scoped resource: \`component ${dep.name} as "…" database\` (or cache, …) inside the cell`;
    case "org-service":
      return `another project's service: \`east ${dep.name} as "…" service\``;
    case "external":
      return `a third-party system: \`south ${dep.name} as "…" service\``;
    default:
      return `a node named \`${dep.name}\``;
  }
}

/**
 * Judge a component's dependencies against the cell. `null` for any other
 * path, for a document the schema gate will reject anyway, and for a
 * document whose every dependency names a node the cell declares.
 */
export function checkComponentDependencies(
  path: string,
  content: string,
  bundle: DiagramBundleReader,
): ComponentDependencyProblem | null {
  if (!COMPONENT_DESIGN_RE.test(path)) return null;
  const deps = dependenciesOf(content);
  if (deps === null || deps.length === 0) return null;

  const cellSource = bundle.read(DESIGN_CELL_PATH);
  if (cellSource === undefined || cellSource.trim() === "") {
    return {
      code: "UNKNOWN_DEPENDENCY",
      message: `${path} rejected — it declares dependencies but ${DESIGN_CELL_PATH} is not in the bundle yet, so none of them can be resolved. Write the cell first: the cell is the design's source of truth and every dependency is one of its nodes. The file is unchanged.`,
    };
  }
  const nodes = cellNodeIds(cellSource);
  const declared = new Set([...nodes.components, ...nodes.externals]);
  const unknown = deps.filter((d) => !declared.has(d.name));
  if (unknown.length === 0) return null;

  const list = (xs: string[]) => (xs.length ? xs.join(", ") : "none");
  const what = unknown
    .map((d) => `\`${d.name}\` (${d.kind}) — declare it as ${declarationFor(d)}`)
    .join("; ");
  return {
    code: "UNKNOWN_DEPENDENCY",
    message: `${path} rejected — ${unknown.length === 1 ? "dependency" : "dependencies"} ${what}. The cell declares components: ${list(nodes.components)}; boundary externals: ${list(nodes.externals)}. The cell is the design's source of truth: add the node to ${DESIGN_CELL_PATH} first (editFile the cell — it keeps the diagram in step with what the build will provision), then re-emit this file. The file is unchanged.`,
  };
}
