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

// The Projects grid's state, from the pages read so far for one search (after
// the old console's ProjectsList): an org with no projects at all, a search
// that matched none, or the projects so far and whether there are more.
//
// The platform filters a search one page at a time (it asks OpenChoreo for a
// page, then keeps the matches), so a page can come back with no matches and
// a cursor to the next: "no match" is only said once there is no next page.

type Project = components["schemas"]["Project"];
type ProjectList = components["schemas"]["ProjectList"];

/** How many projects the grid asks for at a time. */
export const GRID_PAGE_SIZE = 24;

export type GridView =
  | { kind: "empty" }
  | { kind: "no-match"; search: string }
  | { kind: "list"; projects: Project[]; more: boolean; count: number | null };

export function gridView(pages: readonly Pick<ProjectList, "items">[], search: string, more: boolean): GridView {
  const projects = pages.flatMap((p) => p.items ?? []);
  if (projects.length === 0 && !more) return search ? { kind: "no-match", search } : { kind: "empty" };
  // The org's count only when every project is on screen, and not while a search narrows them.
  return { kind: "list", projects, more, count: !more && !search ? projects.length : null };
}
