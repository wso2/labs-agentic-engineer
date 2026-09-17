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
 * Every org group this design touches, judged against the live directory.
 *
 * A design reaches a group two ways and the page must show both: `groups[]`
 * DECLARES one the project introduces, and a role's `assignTo` REFERENCES one
 * the org directory is expected to hold already. The two used to render in
 * different places — declared groups in a card of their own, reused ones only
 * as a chip inside whichever role card happened to name them — so the one
 * question a reader has here ("which of these already exist?") could not be
 * answered by looking at one list. This fold is that one list.
 *
 * Whether a group is new or existing is the DIRECTORY's answer, never the
 * document's: a declared group the platform already created on an earlier build
 * reads `existing`, because that is what Build will find. The document only
 * decides the spelling and the description.
 *
 * A pure fold over the two reads, so the rule is asserted without React.
 */

import type { ProjectRolesLiveState } from "../api/roles";
import { roleEnrolment, roleKind, type SecurityDesign } from "../api/securityDesign";

/** One org group, as the Groups list renders it. */
export interface GroupRow {
  /** The group's name, spelled as the design spells it. */
  name: string;
  /** What the design says the group is. Only a DECLARED group has one. */
  description?: string | undefined;
  /** True when `groups[]` declares it, rather than only an `assignTo` naming it. */
  declared: boolean;
  /**
   * What the directory says: `existing` when it holds the group today, `new`
   * when Build will create it, and `null` when the directory could not be read
   * at all — which renders as no chip rather than a guess.
   */
  status: "new" | "existing" | null;
  /**
   * True only for an `existing` group the PLATFORM created. A hand-made group
   * reads existing too, but Build leaves its membership alone, and the chip's
   * tooltip is where that difference is said.
   */
  platformCreated: boolean;
  /** Members on the directory today, or null when there is no count to show. */
  memberCount: number | null;
  /**
   * How many projects already bind a role to this group — the blast radius of
   * reusing it. Null rather than 0: the BFF omits the count it could not take,
   * and a confident zero would read as "nobody uses this".
   */
  projects: number | null;
  /** The roles in THIS design that assign to it, in declaration order. */
  roles: string[];
}

/** Every group this design declares or assigns a role to, in reading order. */
export function groupRows(
  doc: SecurityDesign,
  live: ProjectRolesLiveState | undefined,
): GroupRow[] {
  const order: string[] = [];
  const draft = new Map<string, GroupRow>();

  const touch = (name: string): GroupRow => {
    const key = name.toLowerCase();
    const existing = draft.get(key);
    if (existing) return existing;
    order.push(key);
    const row: GroupRow = {
      name,
      declared: false,
      status: null,
      platformCreated: false,
      memberCount: null,
      projects: null,
      roles: [],
    };
    draft.set(key, row);
    return row;
  };

  // Declared first, so the groups the project introduces lead the list and
  // carry the spelling and description the design authored.
  for (const group of doc.groups) {
    const row = touch(group.name);
    row.declared = true;
    row.description = group.description;
  }
  for (const role of doc.roles) {
    // A service role's grants reach an application, and a self-service role's
    // enrolment is the app's own; neither takes an org group even if one is
    // written, so neither may put a group in this list.
    if (roleKind(role) !== "user" || roleEnrolment(role) !== "admin") continue;
    for (const group of role.assignTo ?? []) {
      touch(group).roles.push(role.name);
    }
  }

  return order.map((key) => judge(draft.get(key)!, key, live));
}

/**
 * One row against the live directory. Case-insensitive throughout: the design
 * spells a group the way a person writes it and the directory the way it was
 * first created, and `Finance` reusing `finance` is reuse.
 */
function judge(
  row: GroupRow,
  key: string,
  live: ProjectRolesLiveState | undefined,
): GroupRow {
  if (!live?.directoryAvailable) return row;
  const onDirectory = live.roles.find((g) => g.name.toLowerCase() === key);
  if (!onDirectory) return { ...row, status: "new" };
  return {
    ...row,
    status: "existing",
    platformCreated: onDirectory.platformCreated,
    memberCount: positive(onDirectory.memberCount),
    projects: positive(assignmentReach(live, key) ?? onDirectory.projects),
  };
}

/**
 * The project count carried by THIS project's own binding to the group, which
 * the platform records per assignment and is therefore authoritative where the
 * group catalog's aggregate is only close. Undefined before the first Build,
 * when the catalog's count is the best answer there is.
 */
function assignmentReach(
  live: ProjectRolesLiveState,
  key: string,
): number | undefined {
  for (const role of live.projectRoles) {
    const bound = role.assignedTo?.find((a) => a.group.toLowerCase() === key);
    if (bound?.projects !== undefined) return bound.projects;
  }
  return undefined;
}

/** A count worth printing, or null. See `GroupRow.projects` for why not 0. */
function positive(value: number | undefined): number | null {
  return value !== undefined && value > 0 ? value : null;
}

/**
 * The quiet line beside a group: how many people are in it and how far it
 * already reaches. Empty when neither number is known, so the caller renders
 * nothing rather than an orphan separator.
 */
export function groupReachLine(row: GroupRow): string {
  const parts: string[] = [];
  if (row.memberCount !== null) {
    parts.push(`${row.memberCount} ${row.memberCount === 1 ? "member" : "members"}`);
  }
  if (row.projects !== null) {
    parts.push(
      `holds roles in ${row.projects} ${row.projects === 1 ? "project" : "projects"}`,
    );
  }
  return parts.join(" · ");
}
