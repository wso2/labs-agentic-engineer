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
 * Reading `specs/design/security.json` from the console.
 *
 * Everything here is a PURE function over the document text — parse and the
 * planned-user helpers the panel needs to promise usernames. The panel does
 * the rendering; the design agent writes the document in chat.
 *
 * Incomplete JSON objects are empty, not a parse error. JSON is not streamed.
 *
 * The shape is `SecurityDesign` from `@aep/agent-stream` — the same definition
 * the design agent's write gate and the BFF's save gate validate against, so
 * the console cannot invent a fourth idea of what the file looks like.
 */

import {
  checkSecurityDesign,
  securityDesignSchema,
  type SecurityDesign,
} from "@aep/agent-stream";

export type { SecurityDesign };

/** The one authored security document, as the write gate addresses it. */
const SECURITY_DESIGN_PATH = "specs/design/security.json";

export type ParsedSecurity =
  | { kind: "ok"; doc: SecurityDesign }
  | { kind: "empty" }
  | { kind: "invalid"; message: string };

/**
 * Parse the document text. Missing or blank content is `empty` (a rail concern);
 * a present but incomplete object (e.g. `{}`) is also `empty`, and the panel
 * explains that in words rather than showing a parse failure.
 *
 * A document that DECLARES a version this console cannot read is neither: a
 * version-1 file is finished, it is just the previous schema, so it is
 * `invalid` and carries the write gate's own migration sentence — the reader is
 * told exactly what the design agent is told when it writes one.
 */
export function parseSecurityDesign(
  text: string | null | undefined,
): ParsedSecurity {
  if (text === null || text === undefined || text.trim() === "")
    return { kind: "empty" };
  let raw: unknown;
  try {
    raw = JSON.parse(text);
  } catch (e) {
    return {
      kind: "invalid",
      message: e instanceof Error ? e.message : String(e),
    };
  }
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
    return { kind: "empty" };
  }
  if ("version" in raw && (raw as { version?: unknown }).version !== 2) {
    const problem = checkSecurityDesign(SECURITY_DESIGN_PATH, text);
    if (problem) {
      return { kind: "invalid", message: unprefixed(problem.message) };
    }
  }
  const res = securityDesignSchema.safeParse(raw);
  if (!res.success) return { kind: "empty" };
  return { kind: "ok", doc: res.data };
}

/**
 * The gate prefixes its messages with the path it checked; the panel already
 * names the document it failed to read, so the prefix is dropped.
 */
function unprefixed(message: string): string {
  const prefix = `${SECURITY_DESIGN_PATH}: `;
  return message.startsWith(prefix) ? message.slice(prefix.length) : message;
}

/** Serialise a document back to the on-disk form: 2-space indent, trailing newline. */
export function serializeSecurityDesign(doc: SecurityDesign): string {
  return `${JSON.stringify(doc, null, 2)}\n`;
}

/**
 * One test user as the panel shows it, under ONE of its roles. A v2 test user
 * may hold several, so the same username appears once per role it holds — the
 * panel lists users inside role cards, not the other way round.
 */
export interface PlannedUser {
  username: string;
  role: string;
  /** True when the design named none and this is the name the build will generate. */
  supplied: boolean;
}

/**
 * The complete set of test users this design will have after Build — the
 * authored ones, plus one generated name for every role that OWES a login and
 * was given none (see `needsTestUser`).
 *
 * This is a LINE-FOR-LINE mirror of `securityspec.Plan` in the BFF, and it has
 * to be: the panel promises the user a username, and the build has to create
 * that exact name. It is written as one whole-document pass rather than a
 * per-role lookup for the reason the per-role version got wrong — a generated
 * name has to join the taken set as it is minted, or two roles whose names
 * slug identically (`Ops Support` and `Ops/Support`) are both promised
 * `test-ops-support` while the build actually creates `test-ops-support` and
 * `test-ops-support-2`.
 */
export function planUsers(doc: SecurityDesign): PlannedUser[] {
  const taken = new Set(doc.testUsers.map((u) => u.username));
  const byRole = new Map<string, string[]>();
  for (const u of doc.testUsers) {
    for (const roleName of u.roles) {
      const key = roleName.toLowerCase();
      byRole.set(key, [...(byRole.get(key) ?? []), u.username]);
    }
  }

  const out: PlannedUser[] = [];
  // The forEach index is the ordinal the collision suffix uses, so it counts
  // DECLARED roles — including the ones that owe no login — exactly as the Go
  // `for i, role := range doc.Roles` does.
  doc.roles.forEach((role, i) => {
    const authored = byRole.get(role.name.toLowerCase()) ?? [];
    if (authored.length > 0) {
      for (const username of authored) {
        out.push({ username, role: role.name, supplied: false });
      }
      return;
    }
    if (!needsTestUser(role)) return;
    const name = supplyUsername(role.name, i, taken);
    taken.add(name);
    out.push({ username: name, role: role.name, supplied: true });
  });
  return out;
}

/**
 * Whether the build owes this role a login — `securityspec.Role.NeedsTestUser`
 * in the BFF, with the same defaults applied.
 *
 * Only an admin-enrolment user role: a self-service role's accounts come from
 * the application's own registration flow, and a service role's principal is an
 * application, not a person. Promising either a `test-…` name would name an
 * account the build never creates.
 */
function needsTestUser(role: SecurityDesign["roles"][number]): boolean {
  return (role.kind ?? "user") === "user" && (role.enrolment ?? "admin") === "admin";
}

/** The planned users for one role. */
export function plannedUsersFor(
  doc: SecurityDesign,
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
  doc: SecurityDesign,
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
