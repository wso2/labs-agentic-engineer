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
  PUBLIC_SCREEN,
  roleGrants,
  securityDesignSchema,
  type SecurityDesign,
} from "@aep/agent-stream";

import { DESIGN_CELL_PATH } from "./designTree";

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

/** One role of the document, spelled out so the selectors below can name it. */
export type SecurityRole = SecurityDesign["roles"][number];

/** Which ROWS an action reaches — `securityspec.Ownership`. */
export type Ownership = SecurityDesign["permissions"][number]["actions"][number]["ownership"];

/** What a role is assigned TO. Absent in the document means `user`. */
export type RoleKind = "user" | "service";

/** How somebody comes to hold a role. Absent in the document means `admin`. */
export type Enrolment = "admin" | "self-service";

/**
 * The role's kind with the default applied — `securityspec.Role.RoleKind`.
 * Exported because the default lives in ONE place: a panel comparing
 * `role.kind === "user"` would silently drop every role that omitted the field.
 */
export function roleKind(role: SecurityRole): RoleKind {
  return role.kind ?? "user";
}

/** How somebody comes to hold the role — `securityspec.Role.EnrolmentKind`. */
export function roleEnrolment(role: SecurityRole): Enrolment {
  return role.enrolment ?? "admin";
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
export function needsTestUser(role: SecurityRole): boolean {
  return roleKind(role) === "user" && roleEnrolment(role) === "admin";
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

// ---------------------------------------------------------------------------
// The Security page's matrix, derived
// ---------------------------------------------------------------------------
//
// Rows are the permission catalog; columns are the roles; a cell is a grant.
// Everything below is a pure fold over one parsed document, so the panel is a
// renderer and the shape of the page is testable without React.

/** One catalog action as a matrix row. */
export interface MatrixRow {
  /** The full catalog handle — `claims:read`. This is what a token carries. */
  handle: string;
  /** The resource half — `claims`. */
  resource: string;
  /** The action half — `read`. */
  action: string;
  /** The component that OWNS the resource, as `design.cell` names it. */
  component: string;
  /** Which rows the action reaches. Required by the schema, never inferred. */
  ownership: Ownership;
  /** The document's prose, or "" when it authored none (the schema forbids ""). */
  description: string;
  /**
   * The roles that grant this handle, in DECLARATION order, service roles
   * included — so a service column reads off the same row as a user column.
   * Empty is the matrix's own "granted by nobody"; the build gate's
   * `securityReferenceFindings` is what turns that into a warning sentence,
   * because only the gate can see the sibling specs a handle might be used by.
   */
  grantedBy: string[];
}

/** One resource of the catalog, with its actions — a banded group of rows. */
export interface MatrixResourceGroup {
  /** The resource handle — `claims`. */
  resource: string;
  /** The component that owns it. */
  component: string;
  /** The document's prose, or "" when it authored none. */
  description: string;
  rows: MatrixRow[];
}

/** One role as a matrix column. */
export interface MatrixColumn {
  /** The role name, verbatim — the key `MatrixRow.grantedBy` holds. */
  name: string;
  kind: RoleKind;
  enrolment: Enrolment;
  /** The declaration itself, for the role card the column heads. */
  role: SecurityRole;
}

/** One screen named by the baseline rows. */
export interface BaselineScreen {
  component: string;
  screen: string;
}

/**
 * The two rows under the catalog: what any signed-in account reaches, and what
 * is reachable before sign-in.
 *
 * **What this document knows.** `security.json` carries `screens[]` and nothing
 * else about reachability: a screen whose `requires` is `null` is the
 * signed-in baseline, and one whose `requires` is `"public"` is open.
 *
 * **What it does NOT know.** An OPERATION's protection is authored in that
 * component's `specs/design/components/<component>/openapi.yaml`, in the
 * operation's `security` block — never here. So the design's "any signed-in
 * user · GET /me" row is only half derivable from this document: the screens
 * come from here, the operations must come from the component contracts the
 * panel reads separately (`useSpecFileContent` + `@aep/ui-openapi-view`'s
 * parsed `protection`). These fields are deliberately screens-only rather than
 * pretending an empty operation list means "no unscoped operations".
 */
export interface SecurityBaseline {
  /** Screens with `requires: null` — any signed-in account reaches them. */
  signedInScreens: BaselineScreen[];
  /** Screens with `requires: "public"` — reachable before sign-in. */
  publicScreens: BaselineScreen[];
}

/** The whole matrix: banded rows, the columns they are scored against. */
export interface SecurityMatrix {
  /** The catalog, grouped by resource, in declaration order. */
  groups: MatrixResourceGroup[];
  /**
   * The user-kind roles, in declaration order — the matrix's columns. A
   * self-service role is user-kind and IS a column; how somebody comes to hold
   * it changes the role card, not what the role may do.
   */
  columns: MatrixColumn[];
  /**
   * The service-kind roles, in declaration order. Kept apart because they have
   * no login and no group, so a role card renders them differently — the panel
   * may still append them to the grid (the design's Vendor Portal picture
   * does), which works because `MatrixRow.grantedBy` scores every role.
   */
  serviceColumns: MatrixColumn[];
  baseline: SecurityBaseline;
}

/**
 * The screens-only baseline. See `SecurityBaseline` for what this document
 * cannot answer.
 */
export function baselineScreens(doc: SecurityDesign): SecurityBaseline {
  const signedInScreens: BaselineScreen[] = [];
  const publicScreens: BaselineScreen[] = [];
  for (const screen of doc.screens) {
    const entry = { component: screen.component, screen: screen.screen };
    if (screen.requires === null) signedInScreens.push(entry);
    else if (screen.requires === PUBLIC_SCREEN) publicScreens.push(entry);
  }
  return { signedInScreens, publicScreens };
}

/**
 * Fold one document into the matrix the Security page draws.
 *
 * The catalog is projected straight through with no folding: a resource
 * declared twice and an action repeated under one resource are refused by the
 * write gate, where the author can still fix them, so nothing here has to
 * reconcile a document that got past it.
 */
export function securityMatrix(doc: SecurityDesign): SecurityMatrix {
  // The shared fold, so "does this role hold that handle?" is answered the same
  // way here, in the write gate and in the build gate.
  const grants = roleGrants(doc);

  const groups = doc.permissions.map((permission) => ({
    resource: permission.resource,
    component: permission.component,
    description: permission.description ?? "",
    rows: permission.actions.map((action) => {
      const handle = `${permission.resource}:${action.handle}`;
      return {
        handle,
        resource: permission.resource,
        action: action.handle,
        component: permission.component,
        ownership: action.ownership,
        description: action.description ?? "",
        grantedBy: doc.roles
          .filter((role) => grants.get(role.name)?.has(handle) === true)
          .map((role) => role.name),
      };
    }),
  }));

  const columns: MatrixColumn[] = [];
  const serviceColumns: MatrixColumn[] = [];
  for (const role of doc.roles) {
    const kind = roleKind(role);
    const column: MatrixColumn = {
      name: role.name,
      kind,
      enrolment: roleEnrolment(role),
      role,
    };
    (kind === "service" ? serviceColumns : columns).push(column);
  }

  return { groups, columns, serviceColumns, baseline: baselineScreens(doc) };
}

/**
 * Whether `column` grants `row` — the cell. A separate function rather than an
 * `includes` at the call site so the panel never has to know that the score is
 * kept on the row and keyed by the role's verbatim name.
 */
export function isGranted(row: MatrixRow, column: MatrixColumn): boolean {
  return row.grantedBy.includes(column.name);
}

/**
 * The handles a role grants, as authored and in authored order.
 *
 * Nothing is widened: `X:read-all` implying `X:read` is a GATE rule, checked
 * and reported against the authored list (and the gateway matches scopes
 * exactly), so applying it silently here would draw a mark the token will not
 * carry. An unknown role name reads as an empty list.
 */
export function grantsOf(doc: SecurityDesign, roleName: string): string[] {
  const key = roleName.toLowerCase();
  const role = doc.roles.find((r) => r.name.toLowerCase() === key);
  return role ? [...role.grants] : [];
}

/** The roles granting `handle`, in declaration order, service roles included. */
export function rolesGranting(doc: SecurityDesign, handle: string): string[] {
  const grants = roleGrants(doc);
  return doc.roles
    .filter((role) => grants.get(role.name)?.has(handle) === true)
    .map((role) => role.name);
}

/**
 * The sibling spec files the referential cross-checks read for THIS document.
 *
 * `securityReferenceFindings` needs a `{ read(path) }` over the rest of the
 * design, and which files that is depends on the document: `design.cell` always,
 * the OpenAPI contract of every component that OWNS a resource (that is the
 * spec a catalog handle can be judged against), and the wireframes of every
 * component a screen names. Deriving the list here rather than inside the hook
 * keeps "which files does this document depend on?" a question answerable
 * without React, and testable.
 *
 * Both OpenAPI spellings are listed because the rules try `.yaml` then `.yml`;
 * a caller resolves whichever exists and answers `undefined` for the other.
 * Paths come back deduplicated, in a stable order.
 */
export function referencePaths(doc: SecurityDesign): string[] {
  const paths = new Set<string>([DESIGN_CELL_PATH]);
  for (const component of new Set(doc.permissions.map((p) => p.component))) {
    paths.add(`${componentDir(component)}/openapi.yaml`);
    paths.add(`${componentDir(component)}/openapi.yml`);
  }
  for (const component of new Set(doc.screens.map((s) => s.component))) {
    paths.add(`${componentDir(component)}/wireframes.dsl`);
  }
  return [...paths];
}

function componentDir(component: string): string {
  return `specs/design/components/${component}`;
}
