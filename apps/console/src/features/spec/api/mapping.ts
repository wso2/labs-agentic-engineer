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

// The Files API serves repo-relative `specs/...` paths (#113 decision 2);
// the console's file model, collab room keys (`Y.Map('files')`), and
// components all keep the historical unprefixed scheme (`requirements/prd.md`).
// This module is the single place where the two schemes meet.

export type SpecGroup = "requirements" | "designs" | "validation";

export interface SpecFileEntry {
  /** Unprefixed path (e.g. requirements/prd.md) — the collab room key. */
  path: string;
  /** Git blob sha at HEAD; changes when content changes. */
  sha: string;
  group: SpecGroup;
}

const SPECS_PREFIX = "specs/";

// Spec-view section per top-level folder under specs/. Files outside these
// folders are hidden from the view (#113 decision 3).
const GROUP_BY_FOLDER: Record<string, SpecGroup> = {
  requirements: "requirements",
  design: "designs",
  validation: "validation",
};

export function toSpecEntry(meta: FileMeta): SpecFileEntry | null {
  if (!meta.path.startsWith(SPECS_PREFIX)) return null;
  const path = meta.path.slice(SPECS_PREFIX.length);
  const folder = path.split("/")[0] ?? "";
  const group = GROUP_BY_FOLDER[folder];
  if (!group || !path.slice(folder.length + 1)) return null;
  return { path, sha: meta.sha, group };
}

export function toSpecEntries(metas: FileMeta[]): SpecFileEntry[] {
  return metas
    .map(toSpecEntry)
    .filter((e): e is SpecFileEntry => e !== null);
}

/** Unprefixed path → the repo-relative path the Files API expects. */
export function toRepoPath(path: string): string {
  return SPECS_PREFIX + path;
}
