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

type FileMeta = components["schemas"]["FileMeta"];

// Paths are the FULL repo-relative path verbatim (`specs/requirements/prd.md`)
// — the same scheme the Files API serves, the collab doc keys, and the agents'
// live-peer writes use. This module only classifies each file into a spec-view
// section; it no longer translates between two path schemes (the stripped
// room-key scheme of #113 decision 2 is retired — it double-prefixed
// agent-created files).

export type SpecGroup = "requirements" | "designs" | "validation";

/**
 * The PRD. Named here because more than one surface has to recognise it: the
 * file list pins it to the top of its group, and the editor carries the code
 * lenses for this path alone.
 */
export const PRD_PATH = "specs/requirements/prd.md";

export interface SpecFileEntry {
  /** Full repo-relative path (e.g. specs/requirements/prd.md) — also the
   *  collab doc key and the Files API read path. */
  path: string;
  /** Git blob sha at HEAD; changes when content changes. */
  sha: string;
  group: SpecGroup;
}

// Spec-view section per folder directly under specs/. Files outside these
// folders are hidden from the view (#113 decision 3).
const GROUP_BY_FOLDER: Record<string, SpecGroup> = {
  requirements: "requirements",
  design: "designs",
  validation: "validation",
};

// Reference documents (#383) are transient turn inputs, never committed
// (ADR-0017), so nothing under here should ever reach the spec view. The guard
// stays anyway: projects created under the feature's v1 DID commit them, and
// without it those paths fall through to the `requirements` group, become
// selectable, and pour a PDF's bytes into the editor pane — the exact incident
// #427 was opened to fix.
const REFERENCES_PREFIX = "specs/requirements/references/";

export function toSpecEntry(meta: FileMeta): SpecFileEntry | null {
  // specs/<folder>/<…file>: needs the prefix, a known folder, and a file name
  // beyond it (segments.length >= 3). A trailing slash means the path names a
  // DIRECTORY, not a file: it clears the length check (the empty last segment
  // counts) and would otherwise become a selectable entry with no file name.
  // Checked before the references branch below, so it holds for every group.
  const segments = meta.path.split("/");
  if (segments[0] !== "specs" || segments.length < 3) return null;
  if (segments[segments.length - 1] === "") return null;
  if (meta.path.startsWith(REFERENCES_PREFIX)) return null;
  const group = GROUP_BY_FOLDER[segments[1] ?? ""];
  if (!group) return null;
  return { path: meta.path, sha: meta.sha, group };
}

export function toSpecEntries(metas: FileMeta[]): SpecFileEntry[] {
  return metas
    .map(toSpecEntry)
    .filter((e): e is SpecFileEntry => e !== null);
}
