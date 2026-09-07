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

import type { SpecFileEntry } from "./mapping";

/** What the content pane should render for the current sidebar selection. */
export type SpecSelection =
  | { kind: "file"; path: string }
  | { kind: "cell-diagram" }
  | { kind: "security" }
  | { kind: "wireframe"; component: string; dslPath: string };

export interface DesignComponentNode {
  name: string;
  /** Browsable files (design.json, openapi.yaml, …) — excludes the raw .dsl. */
  files: SpecFileEntry[];
  /** The component's wireframes .dsl path, or null if it has none. */
  wireframeDslPath: string | null;
}

export interface DesignSection {
  /** Design files directly under design/ (e.g. domain-model.md) — the flat rows. */
  overview: SpecFileEntry[];
  /** Key flows under design/flows/ — one file per flow, the rail's first group. */
  flows: SpecFileEntry[];
  hasComponents: boolean;
  /** Whether a project-level design.cell exists (drives the Architecture tab). */
  hasCellDsl: boolean;
  /** Whether specs/design/security.json exists (drives the Security rail entry). */
  hasSecurity: boolean;
  components: DesignComponentNode[];
}

/** The project-level cell-diagram DSL path (rendered via the Architecture tab, never as a file). */
export const DESIGN_CELL_PATH = "specs/design/design.cell";

/** The security design document — one file, one rail entry. */
export const SECURITY_JSON_PATH = "specs/design/security.json";

/** The domain model — one ER diagram, one rail entry. */
export const DOMAIN_MODEL_PATH = "specs/design/domain-model.md";

const FLOW_RE = /^specs\/design\/flows\/[^/]+\.md$/;

/** Is this a key-flow document (`specs/design/flows/<slug>.md`)? */
export function isFlow(path: string): boolean {
  return FLOW_RE.test(path);
}

function hideFromOverview(path: string): boolean {
  return path === DESIGN_CELL_PATH || path === SECURITY_JSON_PATH;
}

// SpecFileEntry.path is the full repo-relative path (mapping.ts's current
// scheme — the unprefixed room-key scheme it retired), so this must match
// the `specs/` prefix too.
const COMPONENT_RE = /^specs\/design\/components\/([^/]+)\//;

/** Component name for a `specs/design/components/<name>/…` path, else null. */
export function componentOf(path: string): string | null {
  return COMPONENT_RE.exec(path)?.[1] ?? null;
}

function isDsl(path: string): boolean {
  return path.endsWith(".dsl");
}

/**
 * Group the Designs files into an overview list + per-component nodes. The raw
 * `.dsl` sources are not listed as files; each becomes its component's wireframe
 * entry (rendered as a diagram, not shown as text). Components and their files
 * are sorted by path for a stable tree.
 */
export function buildDesignSection(files: SpecFileEntry[]): DesignSection {
  const design = files.filter((f) => f.group === "designs");
  const hasCellDsl = design.some((f) => f.path === DESIGN_CELL_PATH);
  const hasSecurity = design.some((f) => f.path === SECURITY_JSON_PATH);
  // design.cell is surfaced through the Architecture tab (streaming cell
  // diagram), never as a raw text file. security.json is the Security rail
  // entry, not an overview row.
  const overview = design
    .filter((f) => componentOf(f.path) === null && !isFlow(f.path) && !hideFromOverview(f.path))
    .sort((a, b) => a.path.localeCompare(b.path));
  const flows = design
    .filter((f) => isFlow(f.path))
    .sort((a, b) => a.path.localeCompare(b.path));

  const byComponent = new Map<string, DesignComponentNode>();
  for (const f of design) {
    const name = componentOf(f.path);
    if (name === null) continue;
    let node = byComponent.get(name);
    if (!node) {
      node = { name, files: [], wireframeDslPath: null };
      byComponent.set(name, node);
    }
    if (isDsl(f.path)) node.wireframeDslPath = f.path;
    else node.files.push(f);
  }

  const components = [...byComponent.values()].sort((a, b) =>
    a.name.localeCompare(b.name),
  );
  for (const c of components) c.files.sort((a, b) => a.path.localeCompare(b.path));

  return {
    overview,
    flows,
    hasComponents: components.length > 0,
    hasCellDsl,
    hasSecurity,
    components,
  };
}

/**
 * The selection that WATCHES a path being written (#576, ADR-0026) — the same
 * routing the rail's own rows use: the cell opens as the Architecture diagram,
 * security.json opens the Security entry, a wireframe `.dsl` opens as its
 * component's diagram, and everything else is the file itself. One definition,
 * so follow-the-write can never land somewhere a click on the rail would not
 * have gone.
 */
export function followSelection(path: string): SpecSelection {
  if (path === DESIGN_CELL_PATH) return { kind: "cell-diagram" };
  if (path === SECURITY_JSON_PATH) return { kind: "security" };
  const component = componentOf(path);
  if (component && isDsl(path)) {
    return { kind: "wireframe", component, dslPath: path };
  }
  return { kind: "file", path };
}

/** Stable string identity for a selection (React keys + selected-state compare). */
export function selectionKey(sel: SpecSelection): string {
  switch (sel.kind) {
    case "file":
      return `file:${sel.path}`;
    case "cell-diagram":
      return "cell-diagram";
    case "security":
      return "security";
    case "wireframe":
      return `wireframe:${sel.component}`;
  }
}
