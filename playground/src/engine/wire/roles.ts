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
 * WHO YOU CAN BE, in one list.
 *
 * The roles the design declares, plus the two callers a walk has to cover that
 * no role name expresses: signed in holding nothing (the app must show
 * NoAccess) and not signed in at all (the app's own sign-in guard must run).
 * The picker, the panel's number keys and the printed `curl` table all read
 * this one list, so they cannot offer different people.
 */

import type { WireRole } from "./plan.js";

export interface RoleEntry {
  /** What the picker and the panel show. */
  label: string;
  /** The role's name; "" for the no-grant caller, null for signed out. */
  role: string | null;
  /** Extra detail for the picker's second line. */
  hint: string;
}

/** The full list, in the design's own order. */
export function roleEntries(roles: WireRole[]): RoleEntry[] {
  return [
    ...roles.map((role) => ({
      label: role.name,
      role: role.name,
      hint: role.usernameGuessed
        ? `${String(role.grants.length)} grant(s) · no testUsers row, so /me/ matches nothing`
        : `${String(role.grants.length)} grant(s) · ${role.username}`,
    })),
    { label: "no role", role: "", hint: "signed in holding nothing — the NoAccess case" },
    { label: "signed out", role: null, hint: "nobody is signed in — the app's own guard runs" },
  ];
}

/** The URL that enters the app as this entry. */
export function entryUrl(base: string, entry: RoleEntry): string {
  const url = new URL(base);
  if (entry.role === null) url.searchParams.set("auth", "out");
  else url.searchParams.set("role", entry.role);
  return url.toString();
}

/** Find the entry a `--role` flag names, or null when nothing matches. */
export function findEntry(entries: RoleEntry[], name: string): RoleEntry | null {
  const wanted = name.trim().toLowerCase();
  if (wanted === "") return entries.find((entry) => entry.role === "") ?? null;
  return (
    entries.find((entry) => entry.role !== null && entry.role.toLowerCase() === wanted) ??
    entries.find((entry) => entry.label.toLowerCase() === wanted) ??
    null
  );
}
