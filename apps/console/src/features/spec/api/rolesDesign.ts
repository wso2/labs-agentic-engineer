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
 * Reading and editing `specs/design/roles.json` from the console.
 *
 * Everything here is a PURE function over the document text — parse, and
 * document-in / document-out edits. The panel does the rendering, the caller
 * does the writing (through the collab room, so the room's committer stays the
 * only writer to committed truth). Keeping the transformations pure is what
 * makes them testable without a Yjs doc or a browser.
 *
 * The shape is `RolesDesign` from `@aep/agent-stream` — the same definition the
 * design agent's write gate and the BFF's save gate validate against, so the
 * console cannot invent a fourth idea of what the file looks like.
 */

import { rolesDesignSchema, type RolesDesign } from "@aep/agent-stream";

export type { RolesDesign };

export type ParsedRoles =
  | { kind: "ok"; doc: RolesDesign }
  | { kind: "absent" }
  | { kind: "invalid"; message: string };

/**
 * Parse the document text. An absent or blank file is `absent`, not an error:
 * a design with no sign-in legitimately has no roles document, and the panel
 * says so in words rather than showing a parse failure.
 */
export function parseRolesDesign(text: string | null | undefined): ParsedRoles {
  if (text === null || text === undefined || text.trim() === "")
    return { kind: "absent" };
  let raw: unknown;
  try {
    raw = JSON.parse(text);
  } catch (e) {
    return {
      kind: "invalid",
      message: e instanceof Error ? e.message : String(e),
    };
  }
  const res = rolesDesignSchema.safeParse(raw);
  if (!res.success) {
    const first = res.error.issues[0];
    return {
      kind: "invalid",
      message: first
        ? `${first.path.join(".") || "(root)"}: ${first.message}`
        : "does not match the schema",
    };
  }
  return { kind: "ok", doc: res.data };
}

/** Serialise a document back to the on-disk form: 2-space indent, trailing newline. */
export function serializeRolesDesign(doc: RolesDesign): string {
  return `${JSON.stringify(doc, null, 2)}\n`;
}

/** One test user as the panel shows it, including the ones the build will supply. */
export interface PlannedUser {
  username: string;
  role: string;
  /** True when the design named none and this is the name the build will generate. */
  supplied: boolean;
}

/**
 * The complete set of test users this design will have after Build — the
 * authored ones, plus one generated name for every role the design gave none.
 *
 * This is a LINE-FOR-LINE mirror of `rolesspec.Plan` in the BFF, and it has to
 * be: the panel promises the user a username, and the build has to create that
 * exact name. It is written as one whole-document pass rather than a per-role
 * lookup for the reason the per-role version got wrong — a generated name has to
 * join the taken set as it is minted, or two roles whose names slug identically
 * (`Ops Support` and `Ops/Support`) are both promised `test-ops-support` while
 * the build actually creates `test-ops-support` and `test-ops-support-2`.
 */
export function planUsers(doc: RolesDesign): PlannedUser[] {
  const taken = new Set(doc.testUsers.map((u) => u.username));
  const byRole = new Map<string, typeof doc.testUsers>();
  for (const u of doc.testUsers) {
    const key = u.role.toLowerCase();
    byRole.set(key, [...(byRole.get(key) ?? []), u]);
  }

  const out: PlannedUser[] = [];
  doc.roles.forEach((role, i) => {
    const authored = byRole.get(role.name.toLowerCase()) ?? [];
    if (authored.length > 0) {
      for (const u of authored) {
        out.push({ username: u.username, role: role.name, supplied: false });
      }
      return;
    }
    const name = supplyUsername(role.name, i, taken);
    taken.add(name);
    out.push({ username: name, role: role.name, supplied: true });
  });
  return out;
}

/** The planned users for one role. */
export function plannedUsersFor(
  doc: RolesDesign,
  roleName: string,
): PlannedUser[] {
  return planUsers(doc).filter(
    (u) => u.role.toLowerCase() === roleName.toLowerCase(),
  );
}

/**
 * The username the build generates for a role with no authored test user.
 * `ordinal` disambiguates when the natural name is already taken — by an
 * authored user, or by a name supplied to an earlier-declared role.
 */
function supplyUsername(
  roleName: string,
  ordinal: number,
  taken: ReadonlySet<string>,
): string {
  const base = `test-${roleSlug(roleName)}`;
  return taken.has(base) ? `${base}-${ordinal + 1}` : base;
}

/**
 * The name the build will give a role with no authored test user, resolved
 * against the whole document so it agrees with what `planUsers` shows.
 */
export function suppliedUsernameFor(
  doc: RolesDesign,
  roleName: string,
): string {
  const planned = planUsers(doc).find(
    (u) => u.role.toLowerCase() === roleName.toLowerCase() && u.supplied,
  );
  return planned?.username ?? `test-${roleSlug(roleName)}`;
}

/** Lowercase a role name into the username-safe form: "Compliance Admin" → "compliance-admin". */
export function roleSlug(name: string): string {
  const s = name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return s === "" ? "role" : s;
}

/**
 * Add a test user to a role.
 *
 * When the role had none, its supplied name was only ever a promise — adding
 * one authored user replaces it, which is why nothing needs removing first.
 */
export function addTestUser(
  doc: RolesDesign,
  roleName: string,
  username: string,
): string {
  const role = doc.roles.find(
    (r) => r.name.toLowerCase() === roleName.toLowerCase(),
  );
  const next: RolesDesign = {
    ...doc,
    testUsers: [...doc.testUsers, { username, role: role?.name ?? roleName }],
  };
  return serializeRolesDesign(next);
}

/**
 * Rename a test user.
 *
 * A rename of a name the design did not carry — the build's supplied name, shown
 * in the panel before it exists — is an ADD, because that is what the user means
 * by typing over it.
 */
export function renameTestUser(
  doc: RolesDesign,
  from: string,
  to: string,
): string {
  const existing = doc.testUsers.find((u) => u.username === from);
  if (!existing) {
    const role = roleOfSuppliedUser(doc, from);
    return role === null
      ? serializeRolesDesign(doc)
      : addTestUser(doc, role, to);
  }
  const next: RolesDesign = {
    ...doc,
    testUsers: doc.testUsers.map((u) =>
      u.username === from ? { ...u, username: to } : u,
    ),
  };
  return serializeRolesDesign(next);
}

/**
 * Remove a test user from the DESIGN. It does not delete the account: directory
 * objects are shared and outlive the projects that name them, so deleting one is
 * a separate, explicit action.
 */
export function removeTestUser(doc: RolesDesign, username: string): string {
  const next: RolesDesign = {
    ...doc,
    testUsers: doc.testUsers.filter((u) => u.username !== username),
  };
  return serializeRolesDesign(next);
}

/** The role whose supplied (not-yet-authored) username is `username`, or null. */
function roleOfSuppliedUser(doc: RolesDesign, username: string): string | null {
  for (const role of doc.roles) {
    if (suppliedUsernameFor(doc, role.name) === username) {
      const authored = doc.testUsers.some(
        (u) => u.role.toLowerCase() === role.name.toLowerCase(),
      );
      if (!authored) return role.name;
    }
  }
  return null;
}
