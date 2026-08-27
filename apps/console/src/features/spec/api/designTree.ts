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
  /** Design files directly under design/ (e.g. design.md). */
  overview: SpecFileEntry[];
  hasComponents: boolean;
  /** Whether a project-level design.cell exists (drives the Architecture tab). */
  hasCellDsl: boolean;
  /** Whether either half of the security design exists (drives the Security entry). */
  hasSecurity: boolean;
  components: DesignComponentNode[];
}

/** The project-level cell-diagram DSL path (rendered via the Architecture tab, never as a file). */
export const DESIGN_CELL_PATH = "specs/design/design.cell";

/**
 * The two halves of the security design. They are ONE rail entry with two tabs,
 * not two documents: a user thinks about security as one subject, and the split
 * exists because the platform has to parse half of it, which is not their
 * problem. `lexicon.md` holds the same mapping in words.
 */
export const SECURITY_MD_PATH = "specs/design/security.md";
export const ROLES_JSON_PATH = "specs/design/roles.json";

const SECURITY_PATHS: readonly string[] = [SECURITY_MD_PATH, ROLES_JSON_PATH];

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
  const hasSecurity = design.some((f) => SECURITY_PATHS.includes(f.path));
  // design.cell is surfaced through the Architecture tab (streaming cell
  // diagram), never as a raw text file — keep it out of the overview list.
  const overview = design
    .filter(
      (f) =>
        componentOf(f.path) === null &&
        f.path !== DESIGN_CELL_PATH &&
        !SECURITY_PATHS.includes(f.path),
    )
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
    hasComponents: components.length > 0,
    hasCellDsl,
    hasSecurity,
    components,
  };
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
